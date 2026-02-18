package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/lzjever/mbos-wvs/internal/core"
	"github.com/lzjever/mbos-wvs/internal/store"
)

type SetCurrentRequest struct {
	SnapshotID string `json:"snapshot_id"`
}

type CurrentResponse struct {
	WSID              string `json:"wsid"`
	CurrentSnapshotID string `json:"current_snapshot_id"`
	CurrentPath       string `json:"current_path"`
}

// GetCurrent gets the current snapshot for a workspace.
func (a *API) GetCurrent(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	wsid := chi.URLParam(r, "wsid")

	ws, err := a.queries.GetWorkspace(ctx, wsid)
	if err != nil {
		WriteError(w, core.NewAppError(core.ErrNotFound, "workspace not found"))
		return
	}

	var snapshotID string
	if ws.CurrentSnapshotID.Valid {
		snapshotID = ws.CurrentSnapshotID.String
	}

	WriteJSON(w, http.StatusOK, CurrentResponse{
		WSID:              ws.Wsid,
		CurrentSnapshotID: snapshotID,
		CurrentPath:       ws.CurrentPath,
	})
}

// SetCurrent sets the current snapshot (async).
func (a *API) SetCurrent(w http.ResponseWriter, r *http.Request) {
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

	var req SetCurrentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, core.NewAppError(core.ErrBadRequest, "invalid request body"))
		return
	}

	if req.SnapshotID == "" {
		WriteError(w, core.NewAppError(core.ErrBadRequest, "snapshot_id required"))
		return
	}

	// Check snapshot exists
	snap, err := a.queries.GetSnapshot(ctx, req.SnapshotID)
	if err != nil || snap.DeletedAt.Valid {
		WriteError(w, core.NewAppError(core.ErrNotFound, "snapshot not found"))
		return
	}

	// Check if already current (noop)
	if ws.CurrentSnapshotID.Valid && ws.CurrentSnapshotID.String == req.SnapshotID {
		WriteJSON(w, http.StatusOK, map[string]interface{}{
			"noop":              true,
			"current_snapshot":  req.SnapshotID,
			"current_path":      ws.CurrentPath,
		})
		return
	}

	body, _ := json.Marshal(req)
	requestHash := core.ComputeRequestHash(body, "POST", "/v1/workspaces/"+wsid+"/current:set")

	// Check idempotency
	existingTask, _ := a.queries.GetTaskByIdempotencyKey(ctx, store.GetTaskByIdempotencyKeyParams{
		Wsid:           wsid,
		Op:             string(core.OpSetCurrent),
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
	newLiveID := uuid.New().String()[:8]
	params, _ := json.Marshal(map[string]string{
		"snapshot_id": req.SnapshotID,
		"new_live_id": newLiveID,
	})

	_, err = a.queries.CreateTask(ctx, store.CreateTaskParams{
		TaskID:         taskID,
		Wsid:           wsid,
		Op:             string(core.OpSetCurrent),
		IdempotencyKey: idempotencyKey,
		RequestHash:    requestHash,
		Params:         params,
		MaxAttempts:    5,
		TimeoutSeconds: 300,
	})
	if err != nil {
		a.log.Error("create set_current task failed", zap.Error(err))
		WriteError(w, core.NewAppError(core.ErrInternal, "failed to create task"))
		return
	}

	_ = a.writeAudit(ctx, wsid, "current.set", &taskID, req)

	WriteAccepted(w, taskID, "/v1/tasks/")
}
