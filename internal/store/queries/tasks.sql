-- name: CreateTask :one
INSERT INTO wvs.tasks (task_id, wsid, op, status, idempotency_key, request_hash, params, max_attempts, timeout_seconds)
VALUES ($1, $2, $3, 'PENDING', $4, $5, $6, $7, $8)
RETURNING *;

-- name: GetTask :one
SELECT * FROM wvs.tasks WHERE task_id = $1;

-- name: GetTaskByIdempotencyKey :one
SELECT * FROM wvs.tasks WHERE wsid = $1 AND op = $2 AND idempotency_key = $3;

-- name: ListTasks :many
SELECT * FROM wvs.tasks
WHERE (sqlc.narg('wsid')::text IS NULL OR wsid = sqlc.narg('wsid')::text)
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status')::text)
  AND (sqlc.narg('op')::text IS NULL OR op = sqlc.narg('op')::text)
  AND (sqlc.narg('cursor')::timestamptz IS NULL OR created_at < sqlc.narg('cursor')::timestamptz)
ORDER BY created_at DESC
LIMIT $1;

-- name: DequeueTask :one
WITH picked AS (
  SELECT task_id
  FROM wvs.tasks
  WHERE status IN ('PENDING', 'FAILED')
    AND next_run_at <= now()
    AND attempt < max_attempts
  ORDER BY created_at
  FOR UPDATE SKIP LOCKED
  LIMIT 1
)
UPDATE wvs.tasks t
SET status = 'RUNNING', started_at = now(), attempt = attempt + 1
FROM picked
WHERE t.task_id = picked.task_id
RETURNING t.*;

-- name: CompleteTask :exec
UPDATE wvs.tasks
SET status = $2, ended_at = now(), result = $3, error = $4
WHERE task_id = $1;

-- name: FailTask :exec
UPDATE wvs.tasks
SET status = 'FAILED', ended_at = now(), error = $2,
    next_run_at = now() + make_interval(secs => least(5 * power(2, attempt - 1), 300))
WHERE task_id = $1;

-- name: MarkTaskDead :exec
UPDATE wvs.tasks SET status = 'DEAD', ended_at = now(), error = $2 WHERE task_id = $1;

-- name: CancelPendingTask :one
UPDATE wvs.tasks SET status = 'CANCELED', ended_at = now()
WHERE task_id = $1 AND status = 'PENDING'
RETURNING *;

-- name: RequestCancelRunningTask :one
UPDATE wvs.tasks SET cancel_requested = true
WHERE task_id = $1 AND status = 'RUNNING'
RETURNING *;

-- name: AcquireWorkspaceLock :exec
SELECT pg_advisory_xact_lock(hashtext($1));

-- name: CountActiveTasks :one
SELECT count(*) FROM wvs.tasks
WHERE wsid = $1 AND status IN ('PENDING', 'RUNNING', 'FAILED') AND attempt < max_attempts;

-- name: GetQueueDepth :one
SELECT count(*) FROM wvs.tasks
WHERE status IN ('PENDING', 'FAILED') AND next_run_at <= now() AND attempt < max_attempts;
