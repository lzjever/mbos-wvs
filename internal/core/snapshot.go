package core

import "time"

type Snapshot struct {
	SnapshotID string     `json:"snapshot_id"`
	WSID       string     `json:"wsid"`
	FSPath     string     `json:"fs_path"`
	Message    *string    `json:"message"`
	CreatedAt  time.Time  `json:"created_at"`
	DeletedAt  *time.Time `json:"deleted_at,omitempty"`
}
