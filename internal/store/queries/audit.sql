-- name: InsertAudit :one
INSERT INTO wvs.audit (wsid, actor, action, request_id, task_id, payload)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;
