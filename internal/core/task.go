package core

import (
	"encoding/json"
	"time"
)

type TaskOp string

const (
	OpInitWorkspace  TaskOp = "init_workspace"
	OpSnapshotCreate TaskOp = "snapshot_create"
	OpSnapshotDrop   TaskOp = "snapshot_drop"
	OpSetCurrent     TaskOp = "set_current"
)

type TaskStatus string

const (
	TaskPending   TaskStatus = "PENDING"
	TaskRunning   TaskStatus = "RUNNING"
	TaskSucceeded TaskStatus = "SUCCEEDED"
	TaskFailed    TaskStatus = "FAILED"
	TaskCanceled  TaskStatus = "CANCELED"
	TaskDead      TaskStatus = "DEAD"
)

type Task struct {
	TaskID          string          `json:"task_id"`
	WSID            string          `json:"wsid"`
	Op              TaskOp          `json:"op"`
	Status          TaskStatus      `json:"status"`
	IdempotencyKey  string          `json:"idempotency_key"`
	RequestHash     string          `json:"request_hash"`
	CreatedAt       time.Time       `json:"created_at"`
	StartedAt       *time.Time      `json:"started_at,omitempty"`
	EndedAt         *time.Time      `json:"ended_at,omitempty"`
	Attempt         int             `json:"attempt"`
	MaxAttempts     int             `json:"max_attempts"`
	NextRunAt       time.Time       `json:"next_run_at"`
	TimeoutSeconds  int             `json:"timeout_seconds"`
	CancelRequested bool            `json:"cancel_requested"`
	Params          json.RawMessage `json:"params"`
	Result          json.RawMessage `json:"result,omitempty"`
	Error           json.RawMessage `json:"error,omitempty"`
}

// IsRetryable returns true if task can be retried.
func (t *Task) IsRetryable() bool {
	return t.Status == TaskFailed && t.Attempt < t.MaxAttempts
}

// IsTerminal returns true if task is in a final state.
func (t *Task) IsTerminal() bool {
	switch t.Status {
	case TaskSucceeded, TaskCanceled, TaskDead:
		return true
	}
	return false
}
