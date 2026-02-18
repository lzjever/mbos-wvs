package core

import (
	"encoding/json"
	"time"
)

type AuditEvent struct {
	EventID   int64           `json:"event_id"`
	Ts        time.Time       `json:"ts"`
	WSID      *string         `json:"wsid,omitempty"`
	Actor     json.RawMessage `json:"actor"`
	Action    string          `json:"action"`
	RequestID *string         `json:"request_id,omitempty"`
	TaskID    *string         `json:"task_id,omitempty"`
	Payload   json.RawMessage `json:"payload"`
}
