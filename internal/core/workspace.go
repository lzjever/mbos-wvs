package core

import "time"

type WorkspaceState string

const (
	WorkspaceProvisioning WorkspaceState = "PROVISIONING"
	WorkspaceActive       WorkspaceState = "ACTIVE"
	WorkspaceInitFailed   WorkspaceState = "INIT_FAILED"
	WorkspaceDisabled     WorkspaceState = "DISABLED"
)

type Workspace struct {
	WSID              string         `json:"wsid"`
	RootPath          string         `json:"root_path"`
	Owner             string         `json:"owner"`
	State             WorkspaceState `json:"state"`
	CurrentSnapshotID *string        `json:"current_snapshot_id"`
	CurrentPath       string         `json:"current_path"`
	CreatedAt         time.Time      `json:"created_at"`
	UpdatedAt         time.Time      `json:"updated_at"`
}
