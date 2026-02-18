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

type TaskResponse struct {
	TaskID          string                 `json:"task_id"`
	WSID            string                 `json:"wsid"`
	Op              string                 `json:"op"`
	Status          string                 `json:"status"`
	IdempotencyKey  string                 `json:"idempotency_key"`
	RequestHash     string                 `json:"request_hash"`
	CreatedAt       string                 `json:"created_at"`
	StartedAt       string                 `json:"started_at,omitempty"`
	EndedAt         string                 `json:"ended_at,omitempty"`
	Attempt         int32                  `json:"attempt"`
	MaxAttempts     int32                  `json:"max_attempts"`
	NextRunAt       string                 `json:"next_run_at"`
	TimeoutSeconds  int32                  `json:"timeout_seconds"`
	CancelRequested bool                   `json:"cancel_requested"`
	Params          map[string]interface{} `json:"params"`
	Result          map[string]interface{} `json:"result,omitempty"`
	Error           map[string]interface{} `json:"error,omitempty"`
}

// ListTasks lists tasks with filters.
func (a *API) ListTasks(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	limit := parseLimit(r.URL.Query().Get("limit"), 20, 100)
	wsid := r.URL.Query().Get("wsid")
	status := r.URL.Query().Get("status")
	op := r.URL.Query().Get("op")
	cursor := parseCursor(r.URL.Query().Get("cursor"))

	tasks, err := a.queries.ListTasks(ctx, store.ListTasksParams{
		Limit:  int32(limit),
		Wsid:   textFromString(wsid),
		Status: textFromString(status),
		Op:     textFromString(op),
		Cursor: cursor,
	})
	if err != nil {
		a.log.Error("list tasks failed", zap.Error(err))
		WriteError(w, core.NewAppError(core.ErrInternal, "failed to list tasks"))
		return
	}

	resp := make([]TaskResponse, len(tasks))
	for i, t := range tasks {
		resp[i] = taskToResponse(t)
	}

	var nextCursor string
	if len(tasks) == limit {
		last := tasks[len(tasks)-1]
		nextCursor = encodeCursor(last.CreatedAt)
	}

	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"tasks":       resp,
		"next_cursor": nextCursor,
	})
}

// GetTask gets a single task by ID.
func (a *API) GetTask(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	taskID := chi.URLParam(r, "task_id")

	task, err := a.queries.GetTask(ctx, taskID)
	if err != nil {
		WriteError(w, core.NewAppError(core.ErrNotFound, "task not found"))
		return
	}

	WriteJSON(w, http.StatusOK, taskToResponse(task))
}

// CancelTask cancels a task.
func (a *API) CancelTask(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	taskID := chi.URLParam(r, "task_id")

	task, err := a.queries.GetTask(ctx, taskID)
	if err != nil {
		WriteError(w, core.NewAppError(core.ErrNotFound, "task not found"))
		return
	}

	// If already terminal, return current state
	if isTerminalStatus(task.Status) {
		WriteJSON(w, http.StatusOK, taskToResponse(task))
		return
	}

	// If PENDING, cancel directly
	if task.Status == string(core.TaskPending) {
		_, err = a.queries.CancelPendingTask(ctx, taskID)
		if err != nil {
			WriteError(w, core.NewAppError(core.ErrInternal, "failed to cancel task"))
			return
		}
		task.Status = string(core.TaskCanceled)
	} else {
		// If RUNNING, request cancel
		_, err = a.queries.RequestCancelRunningTask(ctx, taskID)
		if err != nil {
			WriteError(w, core.NewAppError(core.ErrInternal, "failed to request cancel"))
			return
		}
		task.CancelRequested = true
	}

	_ = a.writeAudit(ctx, task.Wsid, "task.cancel", &taskID, nil)

	WriteJSON(w, http.StatusOK, taskToResponse(task))
}

func taskToResponse(t store.WvsTask) TaskResponse {
	var params, result, errMsg map[string]interface{}
	json.Unmarshal(t.Params, &params)
	if t.Result != nil {
		json.Unmarshal(t.Result, &result)
	}
	if t.Error != nil {
		json.Unmarshal(t.Error, &errMsg)
	}

	return TaskResponse{
		TaskID:          t.TaskID,
		WSID:            t.Wsid,
		Op:              t.Op,
		Status:          t.Status,
		IdempotencyKey:  t.IdempotencyKey,
		RequestHash:     t.RequestHash,
		CreatedAt:       t.CreatedAt.Time.Format("2006-01-02T15:04:05Z"),
		StartedAt:       formatTime(t.StartedAt),
		EndedAt:         formatTime(t.EndedAt),
		Attempt:         t.Attempt,
		MaxAttempts:     t.MaxAttempts,
		NextRunAt:       t.NextRunAt.Time.Format("2006-01-02T15:04:05Z"),
		TimeoutSeconds:  t.TimeoutSeconds,
		CancelRequested: t.CancelRequested,
		Params:          params,
		Result:          result,
		Error:           errMsg,
	}
}

func formatTime(t pgtype.Timestamptz) string {
	if !t.Valid {
		return ""
	}
	return t.Time.Format("2006-01-02T15:04:05Z")
}

func isTerminalStatus(status string) bool {
	switch status {
	case string(core.TaskSucceeded), string(core.TaskCanceled), string(core.TaskDead):
		return true
	}
	return false
}
