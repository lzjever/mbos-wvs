package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"go.uber.org/zap"

	"github.com/lzjever/mbos-wvs/internal/core"
	"github.com/lzjever/mbos-wvs/internal/store"
)

type CreateWorkspaceRequest struct {
	WSID     string `json:"wsid"`
	RootPath string `json:"root_path"`
	Owner    string `json:"owner"`
}

type WorkspaceResponse struct {
	WSID              string `json:"wsid"`
	RootPath          string `json:"root_path"`
	Owner             string `json:"owner"`
	State             string `json:"state"`
	CurrentSnapshotID string `json:"current_snapshot_id,omitempty"`
	CurrentPath       string `json:"current_path"`
	CreatedAt         string `json:"created_at"`
	UpdatedAt         string `json:"updated_at"`
}

// ListWorkspaces lists all workspaces with pagination.
func (a *API) ListWorkspaces(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	limit := parseLimit(r.URL.Query().Get("limit"), 20, 100)
	cursor := parseCursor(r.URL.Query().Get("cursor"))

	workspaces, err := a.queries.ListWorkspaces(ctx, store.ListWorkspacesParams{
		Limit:  int32(limit),
		Cursor: cursor,
	})
	if err != nil {
		a.log.Error("list workspaces failed", zap.Error(err))
		WriteError(w, core.NewAppError(core.ErrInternal, "failed to list workspaces"))
		return
	}

	resp := make([]WorkspaceResponse, len(workspaces))
	for i, ws := range workspaces {
		resp[i] = workspaceToResponse(ws)
	}

	// Build next cursor
	var nextCursor string
	if len(workspaces) == limit {
		last := workspaces[len(workspaces)-1]
		nextCursor = encodeCursor(last.CreatedAt)
	}

	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"workspaces":  resp,
		"next_cursor": nextCursor,
	})
}

// GetWorkspace gets a single workspace by wsid.
func (a *API) GetWorkspace(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	wsid := chi.URLParam(r, "wsid")

	ws, err := a.queries.GetWorkspace(ctx, wsid)
	if err != nil {
		WriteError(w, core.NewAppError(core.ErrNotFound, "workspace not found"))
		return
	}

	WriteJSON(w, http.StatusOK, workspaceToResponse(ws))
}

// CreateWorkspace creates a new workspace (async).
func (a *API) CreateWorkspace(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Check Idempotency-Key header
	idempotencyKey := r.Header.Get("Idempotency-Key")
	if idempotencyKey == "" {
		WriteError(w, core.NewAppError(core.ErrBadRequest, "Idempotency-Key header required"))
		return
	}

	var req CreateWorkspaceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, core.NewAppError(core.ErrBadRequest, "invalid request body"))
		return
	}

	// Validate request
	if req.WSID == "" || req.RootPath == "" || req.Owner == "" {
		WriteError(w, core.NewAppError(core.ErrBadRequest, "wsid, root_path, and owner are required"))
		return
	}

	// Compute request hash
	body, _ := json.Marshal(req)
	requestHash := core.ComputeRequestHash(body, "POST", "/v1/workspaces")

	// Check idempotency
	existingTask, _ := a.queries.GetTaskByIdempotencyKey(ctx, store.GetTaskByIdempotencyKeyParams{
		Wsid:           req.WSID,
		Op:             string(core.OpInitWorkspace),
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

	// Create workspace record (PROVISIONING state)
	_, err := a.queries.CreateWorkspace(ctx, store.CreateWorkspaceParams{
		Wsid:        req.WSID,
		RootPath:    req.RootPath,
		Owner:       req.Owner,
		CurrentPath: req.RootPath, // Initial current_path
	})
	if err != nil {
		a.log.Error("create workspace failed", zap.Error(err))
		WriteError(w, core.NewAppError(core.ErrInternal, "failed to create workspace"))
		return
	}

	// Create init_workspace task
	taskID := core.NewID()
	params, _ := json.Marshal(map[string]string{"owner": req.Owner})
	_, err = a.queries.CreateTask(ctx, store.CreateTaskParams{
		TaskID:         taskID,
		Wsid:           req.WSID,
		Op:             string(core.OpInitWorkspace),
		IdempotencyKey: idempotencyKey,
		RequestHash:    requestHash,
		Params:         params,
		MaxAttempts:    5,
		TimeoutSeconds: 300,
	})
	if err != nil {
		a.log.Error("create task failed", zap.Error(err))
		WriteError(w, core.NewAppError(core.ErrInternal, "failed to create task"))
		return
	}

	// Write audit log
	_ = a.writeAudit(ctx, req.WSID, "workspace.create", &taskID, req)

	WriteAccepted(w, taskID, "/v1/tasks/")
}

// RetryInit retries a failed workspace initialization.
func (a *API) RetryInit(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	wsid := chi.URLParam(r, "wsid")

	ws, err := a.queries.GetWorkspace(ctx, wsid)
	if err != nil {
		WriteError(w, core.NewAppError(core.ErrNotFound, "workspace not found"))
		return
	}

	if ws.State != string(core.WorkspaceInitFailed) {
		WriteError(w, core.NewAppError(core.ErrPreconditionFailed, "workspace is not in INIT_FAILED state"))
		return
	}

	// Reset workspace to PROVISIONING
	_ = a.queries.UpdateWorkspaceState(ctx, store.UpdateWorkspaceStateParams{
		Wsid:  wsid,
		State: string(core.WorkspaceProvisioning),
	})

	// Create new init task
	idempotencyKey := core.NewID()
	taskID := core.NewID()
	params, _ := json.Marshal(map[string]string{"owner": ws.Owner})
	requestHash := core.ComputeRequestHash(params, "POST", "/v1/workspaces/"+wsid+"/retry-init")

	_, err = a.queries.CreateTask(ctx, store.CreateTaskParams{
		TaskID:         taskID,
		Wsid:           wsid,
		Op:             string(core.OpInitWorkspace),
		IdempotencyKey: idempotencyKey,
		RequestHash:    requestHash,
		Params:         params,
		MaxAttempts:    5,
		TimeoutSeconds: 300,
	})
	if err != nil {
		a.log.Error("create retry task failed", zap.Error(err))
		WriteError(w, core.NewAppError(core.ErrInternal, "failed to create task"))
		return
	}

	WriteAccepted(w, taskID, "/v1/tasks/")
}

// DisableWorkspace disables a workspace (sync).
func (a *API) DisableWorkspace(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	wsid := chi.URLParam(r, "wsid")

	ws, err := a.queries.GetWorkspace(ctx, wsid)
	if err != nil {
		WriteError(w, core.NewAppError(core.ErrNotFound, "workspace not found"))
		return
	}

	if ws.State == string(core.WorkspaceDisabled) {
		// Idempotent - already disabled
		WriteJSON(w, http.StatusOK, workspaceToResponse(ws))
		return
	}

	// Check for active tasks
	count, _ := a.queries.CountActiveTasks(ctx, wsid)
	if count > 0 {
		WriteError(w, core.NewAppError(core.ErrConflictLocked, "workspace has active tasks"))
		return
	}

	ws, err = a.queries.DisableWorkspace(ctx, wsid)
	if err != nil {
		a.log.Error("disable workspace failed", zap.Error(err))
		WriteError(w, core.NewAppError(core.ErrInternal, "failed to disable workspace"))
		return
	}

	_ = a.writeAudit(ctx, wsid, "workspace.disable", nil, nil)

	WriteJSON(w, http.StatusOK, workspaceToResponse(ws))
}

func workspaceToResponse(ws store.WvsWorkspace) WorkspaceResponse {
	var snapshotID string
	if ws.CurrentSnapshotID.Valid {
		snapshotID = ws.CurrentSnapshotID.String
	}
	return WorkspaceResponse{
		WSID:              ws.Wsid,
		RootPath:          ws.RootPath,
		Owner:             ws.Owner,
		State:             ws.State,
		CurrentSnapshotID: snapshotID,
		CurrentPath:       ws.CurrentPath,
		CreatedAt:         ws.CreatedAt.Time.Format("2006-01-02T15:04:05Z"),
		UpdatedAt:         ws.UpdatedAt.Time.Format("2006-01-02T15:04:05Z"),
	}
}

func parseLimit(s string, defaultVal, maxVal int) int {
	if s == "" {
		return defaultVal
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 {
		return defaultVal
	}
	if n > maxVal {
		return maxVal
	}
	return n
}

func parseCursor(s string) pgtype.Timestamptz {
	if s == "" {
		return pgtype.Timestamptz{Valid: false}
	}
	// Decode base64 cursor to timestamp
	t, err := decodeCursor(s)
	if err != nil {
		return pgtype.Timestamptz{Valid: false}
	}
	return pgtype.Timestamptz{Time: t, Valid: true}
}
