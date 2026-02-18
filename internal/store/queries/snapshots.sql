-- name: CreateSnapshot :one
INSERT INTO wvs.snapshots (snapshot_id, wsid, fs_path, message, created_at)
VALUES ($1, $2, $3, $4, now())
RETURNING *;

-- name: GetSnapshot :one
SELECT * FROM wvs.snapshots WHERE snapshot_id = $1;

-- name: ListSnapshots :many
SELECT * FROM wvs.snapshots
WHERE wsid = $1 AND deleted_at IS NULL
  AND (sqlc.narg('cursor')::timestamptz IS NULL OR created_at < sqlc.narg('cursor')::timestamptz)
ORDER BY created_at DESC
LIMIT $2;

-- name: MarkSnapshotDeleted :exec
UPDATE wvs.snapshots SET deleted_at = now() WHERE snapshot_id = $1;

-- name: IsSnapshotReferencedByTasks :one
SELECT EXISTS(
  SELECT 1 FROM wvs.tasks
  WHERE wsid = $1
    AND status IN ('PENDING', 'RUNNING', 'FAILED')
    AND attempt < max_attempts
    AND op != 'snapshot_drop'
    AND params->>'snapshot_id' = sqlc.narg('snapshot_id')::text
) AS referenced;
