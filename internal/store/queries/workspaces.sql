-- name: CreateWorkspace :one
INSERT INTO wvs.workspaces (wsid, root_path, owner, state, current_path, created_at, updated_at)
VALUES ($1, $2, $3, 'PROVISIONING', $4, now(), now())
RETURNING *;

-- name: GetWorkspace :one
SELECT * FROM wvs.workspaces WHERE wsid = $1;

-- name: ListWorkspaces :many
SELECT * FROM wvs.workspaces
WHERE (sqlc.narg('cursor')::timestamptz IS NULL OR created_at < sqlc.narg('cursor')::timestamptz)
ORDER BY created_at DESC
LIMIT $1;

-- name: UpdateWorkspaceState :exec
UPDATE wvs.workspaces SET state = $2, updated_at = now() WHERE wsid = $1;

-- name: UpdateWorkspaceCurrent :exec
UPDATE wvs.workspaces
SET current_snapshot_id = $2, current_path = $3, updated_at = now()
WHERE wsid = $1;

-- name: DisableWorkspace :one
UPDATE wvs.workspaces SET state = 'DISABLED', updated_at = now()
WHERE wsid = $1
RETURNING *;
