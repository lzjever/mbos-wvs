package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"go.uber.org/zap"

	"github.com/lzjever/mbos-wvs/internal/core"
	"github.com/lzjever/mbos-wvs/internal/store"
)

type CreateSnapshotRequest struct {
	Message string `json:"message,omitempty"`
}

type SnapshotResponse struct {
	SnapshotID string `json:"snapshot_id"`
	WSID       string `json:"wsid"`
	FSPath     string `json:"fs_path"`
	Message    string `json:"message,omitempty"`
	CreatedAt  string `json:"created_at"`
}

// ListSnapshots lists snapshots for a workspace.
func (a *API) ListSnapshots(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	wsid := chi.URLParam(r, "wsid")
	limit := parseLimit(r.URL.Query().Get("limit"), 20, 100)
	cursor := parseCursor(r.URL.Query().Get("cursor"))

	// Check workspace exists
	_, err := a.queries.GetWorkspace(ctx, wsid)
	if err != nil {
		WriteError(w, core.NewAppError(core.ErrNotFound, "workspace not found"))
		return
	}

	snapshots, err := a.queries.ListSnapshots(ctx, store.ListSnapshotsParams{
		Wsid:   wsid,
		Limit:  int32(limit),
		Cursor: cursor,
	})
	if err != nil {
		a.log.Error("list snapshots failed", zap.Error(err))
		WriteError(w, core.NewAppError(core.ErrInternal, "failed to list snapshots"))
		return
	}

	resp := make([]SnapshotResponse, len(snapshots))
	for i, s := range snapshots {
		resp[i] = snapshotToResponse(s)
	}

	var nextCursor string
	if len(snapshots) == limit {
		last := snapshots[len(snapshots)-1]
		nextCursor = encodeCursor(last.CreatedAt)
	}

	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"snapshots":   resp,
		"next_cursor": nextCursor,
	})
}

// CreateSnapshot creates a snapshot (async).
func (a *API) CreateSnapshot(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	wsid := chi.URLParam(r, "wsid")

	// Check workspace exists and is active
	ws, err := a.queries.GetWorkspace(ctx, wsid)
	if err != nil {
		WriteError(w, core.NewAppError(core.ErrNotFound, "workspace not found"))
		return
	}
	if ws.State == string(core.WorkspaceDisabled) {
		WriteError(w, core.NewAppError(core.ErrGone, "workspace is disabled"))
		return
	}

	// Check Idempotency-Key
	idempotencyKey := r.Header.Get("Idempotency-Key")
	if idempotencyKey == "" {
		WriteError(w, core.NewAppError(core.ErrBadRequest, "Idempotency-Key header required"))
		return
	}

	var req CreateSnapshotRequest
	json.NewDecoder(r.Body).Decode(&req)

	body, _ := json.Marshal(req)
	requestHash := core.ComputeRequestHash(body, "POST", "/v1/workspaces/"+wsid+"/snapshots")

	// Check idempotency
	existingTask, _ := a.queries.GetTaskByIdempotencyKey(ctx, store.GetTaskByIdempotencyKeyParams{
		Wsid:           wsid,
		Op:             string(core.OpSnapshotCreate),
		IdempotencyKey: idempotencyKey,
	})
	if existingTask.TaskID != "" {
		if existingTask.RequestHash == requestHash {
			WriteAccepted(w, existingTask.TaskID, "/v1/tasks/")
			return
		}
		WriteError(w, core.NewAppError(core.ErrConflictIdempotent, "idempotency key mismatch"))
		return
	}

	// Create task
	taskID := core.NewID()
	snapshotID := core.NewID()
	params, _ := json.Marshal(map[string]string{
		"snapshot_id": snapshotID,
		"message":     req.Message,
	})

	_, err = a.queries.CreateTask(ctx, store.CreateTaskParams{
		TaskID:         taskID,
		Wsid:           wsid,
		Op:             string(core.OpSnapshotCreate),
		IdempotencyKey: idempotencyKey,
		RequestHash:    requestHash,
		Params:         params,
		MaxAttempts:    5,
		TimeoutSeconds: 300,
	})
	if err != nil {
		a.log.Error("create snapshot task failed", zap.Error(err))
		WriteError(w, core.NewAppError(core.ErrInternal, "failed to create task"))
		return
	}

	_ = a.writeAudit(ctx, wsid, "snapshot.create", &taskID, req)

	WriteAccepted(w, taskID, "/v1/tasks/")
}

// DropSnapshot drops a snapshot (async).
func (a *API) DropSnapshot(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	wsid := chi.URLParam(r, "wsid")
	snapshotID := chi.URLParam(r, "snapshot_id")

	// Check workspace exists
	ws, err := a.queries.GetWorkspace(ctx, wsid)
	if err != nil {
		WriteError(w, core.NewAppError(core.ErrNotFound, "workspace not found"))
		return
	}
	if ws.State == string(core.WorkspaceDisabled) {
		WriteError(w, core.NewAppError(core.ErrGone, "workspace is disabled"))
		return
	}

	// Check snapshot exists
	snap, err := a.queries.GetSnapshot(ctx, snapshotID)
	if err != nil {
		WriteError(w, core.NewAppError(core.ErrNotFound, "snapshot not found"))
		return
	}
	if snap.DeletedAt.Valid {
		// Idempotent - already deleted
		WriteJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
		return
	}

	// Can't drop current snapshot
	if ws.CurrentSnapshotID.Valid && ws.CurrentSnapshotID.String == snapshotID {
		WriteError(w, core.NewAppError(core.ErrConflictSnapshotInUse, "cannot drop current snapshot"))
		return
	}

	// Check Idempotency-Key
	idempotencyKey := r.Header.Get("Idempotency-Key")
	if idempotencyKey == "" {
		WriteError(w, core.NewAppError(core.ErrBadRequest, "Idempotency-Key header required"))
		return
	}

	body, _ := json.Marshal(map[string]string{"snapshot_id": snapshotID})
	requestHash := core.ComputeRequestHash(body, "DELETE", "/v1/workspaces/"+wsid+"/snapshots/"+snapshotID)

	// Check idempotency
	existingTask, _ := a.queries.GetTaskByIdempotencyKey(ctx, store.GetTaskByIdempotencyKeyParams{
		Wsid:           wsid,
		Op:             string(core.OpSnapshotDrop),
		IdempotencyKey: idempotencyKey,
	})
	if existingTask.TaskID != "" {
		if existingTask.RequestHash == requestHash {
			WriteAccepted(w, existingTask.TaskID, "/v1/tasks/")
			return
		}
		WriteError(w, core.NewAppError(core.ErrConflictIdempotent, "idempotency key mismatch"))
		return
	}

	// Create task
	taskID := core.NewID()
	params, _ := json.Marshal(map[string]string{"snapshot_id": snapshotID})

	_, err = a.queries.CreateTask(ctx, store.CreateTaskParams{
		TaskID:         taskID,
		Wsid:           wsid,
		Op:             string(core.OpSnapshotDrop),
		IdempotencyKey: idempotencyKey,
		RequestHash:    requestHash,
		Params:         params,
		MaxAttempts:    5,
		TimeoutSeconds: 300,
	})
	if err != nil {
		a.log.Error("create drop task failed", zap.Error(err))
		WriteError(w, core.NewAppError(core.ErrInternal, "failed to create task"))
		return
	}

	_ = a.writeAudit(ctx, wsid, "snapshot.drop", &taskID, map[string]string{"snapshot_id": snapshotID})

	WriteAccepted(w, taskID, "/v1/tasks/")
}

func snapshotToResponse(s store.WvsSnapshot) SnapshotResponse {
	var msg string
	if s.Message.Valid {
		msg = s.Message.String
	}
	return SnapshotResponse{
		SnapshotID: s.SnapshotID,
		WSID:       s.Wsid,
		FSPath:     s.FsPath,
		Message:    msg,
		CreatedAt:  s.CreatedAt.Time.Format("2006-01-02T15:04:05Z"),
	}
}

func textFromString(s string) pgtype.Text {
	if s == "" {
		return pgtype.Text{Valid: false}
	}
	return pgtype.Text{String: s, Valid: true}
}
