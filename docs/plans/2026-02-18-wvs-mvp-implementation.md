# WVS MVP Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build the complete WVS MVP — API, worker, executor, CLI, migrations, docker-compose demo, and smoke tests — delivering a working snapshot/rollback system for AI agent workspaces.

**Architecture:** Three-tier decoupled design: wvs-api (HTTP control plane) creates tasks, wvs-worker (scheduler) dequeues and orchestrates via advisory locks, executor (gRPC execution plane) performs JuiceFS clone/quiesce/symlink operations. All async writes use idempotency keys. PostgreSQL SKIP LOCKED queue with exponential backoff retry.

**Tech Stack:** Go 1.22+, chi (HTTP), pgx+sqlc (DB), grpc-go, JuiceFS CE (real FUSE clone), prometheus/client_golang, zap (logging), envconfig, UUID v7, golang-migrate, docker-compose, vmagent+vmsingle.

**Source of Truth:** `wvs-tech-docs-20260218-2035.md` — every API, DDL field, error code, and behavior rule comes from this document. Do not invent anything not specified there.

---

## Task 0: Project Scaffolding + Go Module Init

**Files:**
- Create: `go.mod`, `go.sum`
- Create: `Makefile`
- Create: `buf.gen.yaml`, `buf.yaml`
- Create: `sqlc.yaml`
- Create: `.golangci.yml`
- Create: directory tree for all `cmd/`, `internal/`, `proto/`, `migrations/`, `configs/`, `scripts/`

**Step 1: Initialize Go module and directory structure**

```bash
cd /home/percy/works/mbos-wvs
go mod init github.com/lzjever/mbos-wvs
```

Create full directory tree:
```
cmd/wvs-api/
cmd/wvs-worker/
cmd/wvs-executor/
cmd/wvsctl/
cmd/wvs-migrate/
internal/api/
internal/api/middleware/
internal/core/
internal/store/
internal/store/queries/
internal/worker/
internal/executor/
internal/executorclient/
internal/observability/
proto/executor/v1/
migrations/
configs/vmagent/
configs/vmauth/
configs/vmalert/
configs/grafana/provisioning/datasources/
configs/grafana/provisioning/dashboards/
configs/grafana/dashboards/
scripts/
```

**Step 2: Create Makefile**

```makefile
.PHONY: generate lint test build docker up down smoke-test

generate:                    ## Generate sqlc + protobuf code
	sqlc generate
	buf generate

lint:                        ## Code check
	golangci-lint run ./...
	buf lint

test:                        ## Run tests
	go test -race -count=1 ./...

build:                       ## Build all binaries
	CGO_ENABLED=0 go build -o bin/wvs-api ./cmd/wvs-api
	CGO_ENABLED=0 go build -o bin/wvs-worker ./cmd/wvs-worker
	CGO_ENABLED=0 go build -o bin/wvs-executor ./cmd/wvs-executor
	CGO_ENABLED=0 go build -o bin/wvsctl ./cmd/wvsctl

docker:                      ## Build Docker images
	docker build -t yourorg/wvs-api -f Dockerfile.api .
	docker build -t yourorg/wvs-worker -f Dockerfile.worker .
	docker build -t yourorg/wvs-executor -f Dockerfile.executor .

up:                          ## Start demo environment
	docker compose up -d

down:                        ## Stop demo environment
	docker compose down -v

smoke-test:                  ## End-to-end smoke test
	docker compose up -d --wait
	bash scripts/smoke-test.sh
```

**Step 3: Create buf.yaml and buf.gen.yaml**

`buf.yaml`:
```yaml
version: v2
modules:
  - path: proto
lint:
  use:
    - STANDARD
breaking:
  use:
    - FILE
```

`buf.gen.yaml`:
```yaml
version: v2
plugins:
  - remote: buf.build/protocolbuffers/go
    out: gen/go
    opt: paths=source_relative
  - remote: buf.build/grpc/go
    out: gen/go
    opt: paths=source_relative
```

**Step 4: Create sqlc.yaml**

```yaml
version: "2"
sql:
  - engine: "postgresql"
    queries: "internal/store/queries/"
    schema: "migrations/"
    gen:
      go:
        package: "store"
        out: "internal/store"
        sql_package: "pgx/v5"
        emit_json_tags: true
        emit_empty_slices: true
```

**Step 5: Create .golangci.yml**

```yaml
run:
  timeout: 5m
linters:
  enable:
    - errcheck
    - govet
    - staticcheck
    - unused
    - gosimple
    - ineffassign
```

**Step 6: Commit**

```bash
git add -A
git commit -m "chore: scaffold project structure, go.mod, Makefile, tooling configs"
```

---

## Task 1: Database Migration (DDL)

**Files:**
- Create: `migrations/000001_init_schema.up.sql`
- Create: `migrations/000001_init_schema.down.sql`

**Step 1: Write the up migration**

Copy the DDL exactly from `wvs-tech-docs-20260218-2035.md` docs/05 section. The SQL is already complete and reviewed:

`migrations/000001_init_schema.up.sql`:
```sql
CREATE SCHEMA IF NOT EXISTS wvs;

CREATE TABLE wvs.workspaces (
  wsid                TEXT PRIMARY KEY CHECK (wsid ~ '^[a-z0-9][a-z0-9-]{1,61}[a-z0-9]$'),
  root_path           TEXT NOT NULL UNIQUE,
  owner               TEXT NOT NULL,
  state               TEXT NOT NULL DEFAULT 'PROVISIONING'
                      CHECK (state IN ('PROVISIONING', 'ACTIVE', 'INIT_FAILED', 'DISABLED')),
  current_snapshot_id TEXT,
  current_path        TEXT NOT NULL,
  created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE wvs.snapshots (
  snapshot_id         TEXT PRIMARY KEY,
  wsid                TEXT NOT NULL REFERENCES wvs.workspaces(wsid),
  fs_path             TEXT NOT NULL,
  message             TEXT,
  created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
  deleted_at          TIMESTAMPTZ
);

CREATE INDEX idx_snapshots_wsid_created ON wvs.snapshots(wsid, created_at DESC);
CREATE UNIQUE INDEX uq_snapshots_wsid_fspath ON wvs.snapshots(wsid, fs_path);

ALTER TABLE wvs.workspaces
  ADD CONSTRAINT fk_workspaces_current_snapshot
  FOREIGN KEY (current_snapshot_id)
  REFERENCES wvs.snapshots(snapshot_id);

CREATE TABLE wvs.tasks (
  task_id             TEXT PRIMARY KEY,
  wsid                TEXT NOT NULL REFERENCES wvs.workspaces(wsid),
  op                  TEXT NOT NULL CHECK (op IN ('init_workspace', 'snapshot_create', 'snapshot_drop', 'set_current')),
  status              TEXT NOT NULL DEFAULT 'PENDING'
                      CHECK (status IN ('PENDING', 'RUNNING', 'SUCCEEDED', 'FAILED', 'CANCELED', 'DEAD')),
  idempotency_key     TEXT NOT NULL,
  request_hash        TEXT NOT NULL,
  created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
  started_at          TIMESTAMPTZ,
  ended_at            TIMESTAMPTZ,
  attempt             INT NOT NULL DEFAULT 0,
  max_attempts        INT NOT NULL DEFAULT 5,
  next_run_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
  timeout_seconds     INT NOT NULL DEFAULT 300,
  cancel_requested    BOOLEAN NOT NULL DEFAULT false,
  params              JSONB NOT NULL DEFAULT '{}'::jsonb,
  result              JSONB,
  error               JSONB
);

CREATE UNIQUE INDEX uq_tasks_idempotency ON wvs.tasks(wsid, op, idempotency_key);
CREATE INDEX idx_tasks_dequeue ON wvs.tasks(status, next_run_at, created_at)
  WHERE status IN ('PENDING', 'FAILED');
CREATE INDEX idx_tasks_wsid ON wvs.tasks(wsid, created_at DESC);

CREATE TABLE wvs.audit (
  event_id            BIGSERIAL PRIMARY KEY,
  ts                  TIMESTAMPTZ NOT NULL DEFAULT now(),
  wsid                TEXT,
  actor               JSONB NOT NULL,
  action              TEXT NOT NULL,
  request_id          TEXT,
  task_id             TEXT,
  payload             JSONB NOT NULL
);

CREATE INDEX idx_audit_wsid_ts ON wvs.audit(wsid, ts DESC);
CREATE INDEX idx_audit_task ON wvs.audit(task_id) WHERE task_id IS NOT NULL;
```

**Step 2: Write the down migration**

`migrations/000001_init_schema.down.sql`:
```sql
DROP TABLE IF EXISTS wvs.audit;
DROP TABLE IF EXISTS wvs.tasks;
ALTER TABLE wvs.workspaces DROP CONSTRAINT IF EXISTS fk_workspaces_current_snapshot;
DROP TABLE IF EXISTS wvs.snapshots;
DROP TABLE IF EXISTS wvs.workspaces;
DROP SCHEMA IF EXISTS wvs;
```

**Step 3: Validate migration runs against a local PG**

```bash
# Using docker for a quick PG instance
docker run --rm -d --name wvs-pg-test -e POSTGRES_USER=wvs -e POSTGRES_PASSWORD=wvs_pass -e POSTGRES_DB=wvs -p 5499:5432 postgres:16-alpine
sleep 3
go run -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest \
  -path migrations/ -database "postgres://wvs:wvs_pass@localhost:5499/wvs?sslmode=disable" up
# Verify tables exist
docker exec wvs-pg-test psql -U wvs -c "SELECT table_name FROM information_schema.tables WHERE table_schema='wvs';"
docker stop wvs-pg-test
```

Expected output: tables `workspaces`, `snapshots`, `tasks`, `audit`.

**Step 4: Commit**

```bash
git add migrations/
git commit -m "feat: add initial database migration (4 tables, indexes, constraints)"
```

---

## Task 2: gRPC Proto Definition

**Files:**
- Create: `proto/executor/v1/executor.proto`

**Step 1: Write the proto file**

The tech docs define 4 task types: INIT_WORKSPACE, SNAPSHOT_CREATE, SNAPSHOT_DROP, SET_CURRENT.

`proto/executor/v1/executor.proto`:
```protobuf
syntax = "proto3";

package executor.v1;

option go_package = "github.com/lzjever/mbos-wvs/gen/go/executor/v1;executorv1";

service ExecutorService {
  rpc ExecuteTask(ExecuteTaskRequest) returns (ExecuteTaskResponse);
}

enum TaskOp {
  TASK_OP_UNSPECIFIED = 0;
  TASK_OP_INIT_WORKSPACE = 1;
  TASK_OP_SNAPSHOT_CREATE = 2;
  TASK_OP_SNAPSHOT_DROP = 3;
  TASK_OP_SET_CURRENT = 4;
}

message ExecuteTaskRequest {
  string task_id = 1;
  string wsid = 2;
  TaskOp op = 3;
  // Params vary by op:
  // INIT_WORKSPACE: owner
  // SNAPSHOT_CREATE: snapshot_id, message
  // SNAPSHOT_DROP: snapshot_id
  // SET_CURRENT: snapshot_id, new_live_id
  map<string, string> params = 4;
}

message ExecuteTaskResponse {
  bool success = 1;
  bool noop = 2;
  string error_code = 3;
  string error_message = 4;
  // Results vary by op:
  // SNAPSHOT_CREATE: snapshot_id, fs_path
  // SET_CURRENT: current_path
  map<string, string> results = 5;
}
```

**Step 2: Generate Go code**

```bash
buf generate
```

Expected: files in `gen/go/executor/v1/`.

**Step 3: Commit**

```bash
git add proto/ gen/ buf.yaml buf.gen.yaml
git commit -m "feat: add gRPC proto for executor service (4 task ops)"
```

---

## Task 3: Core Domain Models (`internal/core/`)

**Files:**
- Create: `internal/core/workspace.go`
- Create: `internal/core/snapshot.go`
- Create: `internal/core/task.go`
- Create: `internal/core/audit.go`
- Create: `internal/core/errors.go`
- Create: `internal/core/idempotency.go`
- Create: `internal/core/idempotency_test.go`
- Create: `internal/core/id.go`

These are pure domain types — no DB imports, no framework deps.

**Step 1: Write domain types**

`internal/core/workspace.go`:
```go
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
```

`internal/core/snapshot.go`:
```go
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
```

`internal/core/task.go`:
```go
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
```

`internal/core/audit.go`:
```go
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
```

`internal/core/errors.go`:
```go
package core

import "fmt"

type ErrorCode string

const (
    ErrBadRequest             ErrorCode = "WVS_BAD_REQUEST"
    ErrNotFound               ErrorCode = "WVS_NOT_FOUND"
    ErrConflictLocked         ErrorCode = "WVS_CONFLICT_LOCKED"
    ErrConflictIdempotent     ErrorCode = "WVS_CONFLICT_IDEMPOTENT_MISMATCH"
    ErrConflictExists         ErrorCode = "WVS_CONFLICT_EXISTS"
    ErrConflictSnapshotInUse  ErrorCode = "WVS_CONFLICT_SNAPSHOT_IN_USE"
    ErrGone                   ErrorCode = "WVS_GONE"
    ErrPreconditionFailed     ErrorCode = "WVS_PRECONDITION_FAILED"
    ErrInternal               ErrorCode = "WVS_INTERNAL"
    ErrExecutorError          ErrorCode = "WVS_EXECUTOR_ERROR"
    ErrExecutorTimeout        ErrorCode = "WVS_EXECUTOR_TIMEOUT"
)

// HTTPStatus returns the HTTP status code for this error code.
func (e ErrorCode) HTTPStatus() int {
    switch e {
    case ErrBadRequest:
        return 400
    case ErrNotFound:
        return 404
    case ErrConflictLocked, ErrConflictIdempotent, ErrConflictExists, ErrConflictSnapshotInUse:
        return 409
    case ErrGone:
        return 410
    case ErrPreconditionFailed:
        return 412
    case ErrExecutorError:
        return 502
    case ErrExecutorTimeout:
        return 504
    default:
        return 500
    }
}

type AppError struct {
    Code    ErrorCode `json:"code"`
    Message string    `json:"message"`
}

func (e *AppError) Error() string {
    return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func NewAppError(code ErrorCode, msg string) *AppError {
    return &AppError{Code: code, Message: msg}
}
```

`internal/core/idempotency.go`:
```go
package core

import (
    "crypto/sha256"
    "encoding/json"
    "fmt"
    "sort"
)

// ComputeRequestHash computes SHA-256(sorted_json(body) + method + path).
func ComputeRequestHash(body json.RawMessage, method, path string) string {
    sorted := sortedJSON(body)
    h := sha256.New()
    h.Write(sorted)
    h.Write([]byte(method))
    h.Write([]byte(path))
    return fmt.Sprintf("%x", h.Sum(nil))
}

// sortedJSON recursively sorts JSON object keys.
func sortedJSON(data json.RawMessage) []byte {
    var obj map[string]json.RawMessage
    if err := json.Unmarshal(data, &obj); err != nil {
        // Not an object (array, string, number, etc.) — return as-is compact.
        var v interface{}
        if err2 := json.Unmarshal(data, &v); err2 != nil {
            return data
        }
        b, _ := json.Marshal(v)
        return b
    }
    keys := make([]string, 0, len(obj))
    for k := range obj {
        keys = append(keys, k)
    }
    sort.Strings(keys)

    result := []byte("{")
    for i, k := range keys {
        if i > 0 {
            result = append(result, ',')
        }
        kb, _ := json.Marshal(k)
        result = append(result, kb...)
        result = append(result, ':')
        result = append(result, sortedJSON(obj[k])...)
    }
    result = append(result, '}')
    return result
}
```

`internal/core/id.go`:
```go
package core

import "github.com/google/uuid"

// NewID generates a UUID v7 (time-ordered).
func NewID() string {
    id, err := uuid.NewV7()
    if err != nil {
        // Fallback to v4 if v7 fails (should not happen).
        return uuid.New().String()
    }
    return id.String()
}
```

**Step 2: Write idempotency test**

`internal/core/idempotency_test.go`:
```go
package core

import (
    "encoding/json"
    "testing"
)

func TestComputeRequestHash_Deterministic(t *testing.T) {
    body := json.RawMessage(`{"message":"hello","wsid":"ws-1"}`)
    h1 := ComputeRequestHash(body, "POST", "/v1/workspaces/ws-1/snapshots")
    h2 := ComputeRequestHash(body, "POST", "/v1/workspaces/ws-1/snapshots")
    if h1 != h2 {
        t.Fatalf("same input produced different hashes: %s vs %s", h1, h2)
    }
}

func TestComputeRequestHash_KeyOrderIrrelevant(t *testing.T) {
    body1 := json.RawMessage(`{"wsid":"ws-1","message":"hello"}`)
    body2 := json.RawMessage(`{"message":"hello","wsid":"ws-1"}`)
    h1 := ComputeRequestHash(body1, "POST", "/v1/workspaces/ws-1/snapshots")
    h2 := ComputeRequestHash(body2, "POST", "/v1/workspaces/ws-1/snapshots")
    if h1 != h2 {
        t.Fatalf("different key order produced different hashes: %s vs %s", h1, h2)
    }
}

func TestComputeRequestHash_DifferentBody(t *testing.T) {
    body1 := json.RawMessage(`{"message":"hello"}`)
    body2 := json.RawMessage(`{"message":"world"}`)
    h1 := ComputeRequestHash(body1, "POST", "/v1/workspaces/ws-1/snapshots")
    h2 := ComputeRequestHash(body2, "POST", "/v1/workspaces/ws-1/snapshots")
    if h1 == h2 {
        t.Fatal("different bodies produced same hash")
    }
}

func TestComputeRequestHash_DifferentMethod(t *testing.T) {
    body := json.RawMessage(`{"message":"hello"}`)
    h1 := ComputeRequestHash(body, "POST", "/v1/workspaces/ws-1/snapshots")
    h2 := ComputeRequestHash(body, "DELETE", "/v1/workspaces/ws-1/snapshots")
    if h1 == h2 {
        t.Fatal("different methods produced same hash")
    }
}
```

**Step 3: Run tests**

```bash
go test ./internal/core/ -v -run TestComputeRequestHash
```

Expected: all 4 tests PASS.

**Step 4: Commit**

```bash
git add internal/core/
git commit -m "feat: add core domain models (workspace, snapshot, task, audit, errors, idempotency)"
```

---

## Task 4: sqlc Queries + Store Layer (`internal/store/`)

**Files:**
- Create: `internal/store/queries/workspaces.sql`
- Create: `internal/store/queries/snapshots.sql`
- Create: `internal/store/queries/tasks.sql`
- Create: `internal/store/queries/audit.sql`
- Create: `internal/store/db.go` (connection pool wrapper)
- Generated: `internal/store/*.go` (by sqlc)

**Step 1: Write sqlc query files**

`internal/store/queries/workspaces.sql`:
```sql
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
```

`internal/store/queries/snapshots.sql`:
```sql
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
    AND params->>'snapshot_id' = $2
) AS referenced;
```

`internal/store/queries/tasks.sql`:
```sql
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
```

`internal/store/queries/audit.sql`:
```sql
-- name: InsertAudit :one
INSERT INTO wvs.audit (wsid, actor, action, request_id, task_id, payload)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;
```

**Step 2: Write DB connection wrapper**

`internal/store/db.go`:
```go
package store

import (
    "context"
    "fmt"

    "github.com/jackc/pgx/v5/pgxpool"
)

func NewPool(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
    config, err := pgxpool.ParseConfig(dsn)
    if err != nil {
        return nil, fmt.Errorf("parse dsn: %w", err)
    }
    config.MaxConns = 20
    pool, err := pgxpool.NewWithConfig(ctx, config)
    if err != nil {
        return nil, fmt.Errorf("create pool: %w", err)
    }
    if err := pool.Ping(ctx); err != nil {
        pool.Close()
        return nil, fmt.Errorf("ping db: %w", err)
    }
    return pool, nil
}
```

**Step 3: Generate sqlc code**

```bash
sqlc generate
```

Expected: generated files in `internal/store/` (models.go, querier.go, db.go, etc.)

**Step 4: Verify it compiles**

```bash
go build ./internal/store/...
```

**Step 5: Commit**

```bash
git add internal/store/ sqlc.yaml
git commit -m "feat: add sqlc queries and store layer (workspaces, snapshots, tasks, audit)"
```

---

## Task 5: Observability Foundation (`internal/observability/`)

**Files:**
- Create: `internal/observability/logger.go`
- Create: `internal/observability/metrics.go`

**Step 1: Write logger setup**

`internal/observability/logger.go`:
```go
package observability

import (
    "go.uber.org/zap"
    "go.uber.org/zap/zapcore"
)

func NewLogger(level string) (*zap.Logger, error) {
    cfg := zap.NewProductionConfig()
    cfg.EncoderConfig.TimeKey = "ts"
    cfg.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
    lvl, err := zapcore.ParseLevel(level)
    if err != nil {
        lvl = zapcore.InfoLevel
    }
    cfg.Level = zap.NewAtomicLevelAt(lvl)
    return cfg.Build()
}

// TaskLogger returns a child logger with task-context fields.
func TaskLogger(base *zap.Logger, taskID, wsid, op string) *zap.Logger {
    return base.With(
        zap.String("task_id", taskID),
        zap.String("wsid", wsid),
        zap.String("op", op),
    )
}
```

**Step 2: Write metrics registration**

`internal/observability/metrics.go` — register all metrics from docs/09:

```go
package observability

import "github.com/prometheus/client_golang/prometheus"

var (
    // wvs-api metrics
    HTTPRequestsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
        Name: "wvs_http_requests_total",
        Help: "Total HTTP requests",
    }, []string{"route", "method", "code"})

    HTTPRequestDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
        Name:    "wvs_http_request_duration_seconds",
        Help:    "HTTP request latency",
        Buckets: prometheus.DefBuckets,
    }, []string{"route", "method"})

    ActiveRequests = prometheus.NewGauge(prometheus.GaugeOpts{
        Name: "wvs_active_requests",
        Help: "Current in-flight requests",
    })

    // wvs-worker metrics
    TaskTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
        Name: "wvs_task_total",
        Help: "Task completion count",
    }, []string{"op", "status"})

    TaskDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
        Name:    "wvs_task_duration_seconds",
        Help:    "Task end-to-end duration",
        Buckets: []float64{0.1, 0.5, 1, 2, 5, 10, 30, 60, 120, 300},
    }, []string{"op"})

    TaskQueueDepth = prometheus.NewGauge(prometheus.GaugeOpts{
        Name: "wvs_task_queue_depth",
        Help: "Pending + retryable FAILED tasks",
    })

    TaskRetryTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
        Name: "wvs_task_retry_total",
        Help: "Task retry count",
    }, []string{"op"})

    LockWaitSeconds = prometheus.NewHistogram(prometheus.HistogramOpts{
        Name:    "wvs_lock_wait_seconds",
        Help:    "Advisory lock wait time",
        Buckets: []float64{0.001, 0.01, 0.05, 0.1, 0.5, 1, 5},
    })

    DequeueEmptyTotal = prometheus.NewCounter(prometheus.CounterOpts{
        Name: "wvs_dequeue_empty_total",
        Help: "Empty poll count",
    })

    WorkspaceStateTransitions = prometheus.NewCounterVec(prometheus.CounterOpts{
        Name: "wvs_workspace_state_transitions_total",
        Help: "Workspace state transition count",
    }, []string{"from", "to"})

    // executor metrics
    CloneDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
        Name:    "wvs_clone_duration_seconds",
        Help:    "JuiceFS clone duration",
        Buckets: []float64{0.1, 0.5, 1, 2, 5, 10, 30, 60},
    }, []string{"op"})

    CloneEntriesTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
        Name: "wvs_clone_entries_total",
        Help: "Clone directory entry count",
    }, []string{"op"})

    CloneFailTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
        Name: "wvs_clone_fail_total",
        Help: "Clone failure count",
    }, []string{"reason"})

    QuiesceWaitSeconds = prometheus.NewHistogram(prometheus.HistogramOpts{
        Name:    "wvs_quiesce_wait_seconds",
        Help:    "Wait for agent ack duration",
        Buckets: []float64{0.1, 0.5, 1, 2, 5, 10, 30},
    })

    QuiesceTimeoutTotal = prometheus.NewCounter(prometheus.CounterOpts{
        Name: "wvs_quiesce_timeout_total",
        Help: "Quiesce timeout count",
    })

    SwitchDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
        Name:    "wvs_switch_duration_seconds",
        Help:    "Symlink switch duration",
        Buckets: []float64{0.0001, 0.001, 0.01, 0.1},
    })

    ExecutorActiveTasks = prometheus.NewGauge(prometheus.GaugeOpts{
        Name: "wvs_executor_active_tasks",
        Help: "Currently executing tasks",
    })
)

func RegisterAll(reg prometheus.Registerer) {
    reg.MustRegister(
        HTTPRequestsTotal, HTTPRequestDuration, ActiveRequests,
        TaskTotal, TaskDuration, TaskQueueDepth, TaskRetryTotal,
        LockWaitSeconds, DequeueEmptyTotal, WorkspaceStateTransitions,
        CloneDuration, CloneEntriesTotal, CloneFailTotal,
        QuiesceWaitSeconds, QuiesceTimeoutTotal, SwitchDuration, ExecutorActiveTasks,
    )
}
```

**Step 3: Verify compilation**

```bash
go build ./internal/observability/...
```

**Step 4: Commit**

```bash
git add internal/observability/
git commit -m "feat: add observability foundation (zap logger + all prometheus metrics)"
```

---

## Task 6: Executor gRPC Server (`internal/executor/` + `cmd/wvs-executor/`)

**Files:**
- Create: `internal/executor/config.go`
- Create: `internal/executor/server.go`
- Create: `internal/executor/quiesce.go`
- Create: `internal/executor/init_workspace.go`
- Create: `internal/executor/snapshot_create.go`
- Create: `internal/executor/snapshot_drop.go`
- Create: `internal/executor/set_current.go`
- Create: `internal/executor/switch.go`
- Create: `internal/executor/clone.go`
- Create: `cmd/wvs-executor/main.go`

**Step 1: Write executor config**

`internal/executor/config.go`:
```go
package executor

import "time"

type Config struct {
    MountPath       string        `envconfig:"EXECUTOR_MOUNT_PATH" default:"/ws"`
    GRPCAddr        string        `envconfig:"EXECUTOR_GRPC_ADDR" default:"0.0.0.0:7070"`
    MetricsAddr     string        `envconfig:"EXECUTOR_METRICS_ADDR" default:"0.0.0.0:9092"`
    TaskTimeout     time.Duration `envconfig:"EXECUTOR_TASK_TIMEOUT" default:"300s"`
    QuiesceTimeout  time.Duration `envconfig:"EXECUTOR_QUIESCE_TIMEOUT" default:"30s"`
    ShutdownTimeout time.Duration `envconfig:"EXECUTOR_SHUTDOWN_TIMEOUT" default:"300s"`
    JFSMetaURL      string        `envconfig:"JFS_META_URL" required:"true"`
    MinioEndpoint   string        `envconfig:"MINIO_ENDPOINT" required:"true"`
    MinioAccessKey  string        `envconfig:"MINIO_ACCESS_KEY" required:"true"`
    MinioSecretKey  string        `envconfig:"MINIO_SECRET_KEY" required:"true"`
    MinioBucket     string        `envconfig:"MINIO_BUCKET" default:"jfs-data"`
}
```

**Step 2: Write quiesce protocol**

`internal/executor/quiesce.go`:
```go
package executor

import (
    "context"
    "encoding/json"
    "fmt"
    "os"
    "path/filepath"
    "time"

    "go.uber.org/zap"
)

type QuiesceState string

const (
    QuiesceRunning       QuiesceState = "RUNNING"
    QuiesceRequestFreeze QuiesceState = "REQUEST_FREEZE"
    QuiesceFrozen        QuiesceState = "FROZEN"
    QuiesceRequestResume QuiesceState = "REQUEST_RESUME"
)

type ControlFile struct {
    State     QuiesceState `json:"state"`
    Timestamp string       `json:"timestamp"`
    TaskID    string       `json:"task_id,omitempty"`
}

// Quiesce writes REQUEST_FREEZE to control.json and polls for FROZEN ack.
// Returns nil on success. On timeout, writes REQUEST_RESUME and returns error.
func Quiesce(ctx context.Context, wsRoot string, taskID string, timeout time.Duration, log *zap.Logger) error {
    controlPath := filepath.Join(wsRoot, ".wvs", "control.json")

    // Ensure .wvs directory exists
    if err := os.MkdirAll(filepath.Dir(controlPath), 0755); err != nil {
        return fmt.Errorf("mkdir .wvs: %w", err)
    }

    // Write REQUEST_FREEZE
    if err := writeControl(controlPath, QuiesceRequestFreeze, taskID); err != nil {
        return fmt.Errorf("write REQUEST_FREEZE: %w", err)
    }
    log.Info("quiesce: REQUEST_FREEZE written", zap.String("path", controlPath))

    // Poll for FROZEN
    deadline := time.Now().Add(timeout)
    ticker := time.NewTicker(100 * time.Millisecond)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            _ = writeControl(controlPath, QuiesceRequestResume, taskID)
            return ctx.Err()
        case <-ticker.C:
            if time.Now().After(deadline) {
                _ = writeControl(controlPath, QuiesceRequestResume, taskID)
                return fmt.Errorf("quiesce timeout after %s", timeout)
            }
            state, err := readControlState(controlPath)
            if err != nil {
                continue
            }
            if state == QuiesceFrozen {
                log.Info("quiesce: FROZEN ack received")
                return nil
            }
        }
    }
}

// Resume writes REQUEST_RESUME to control.json.
func Resume(wsRoot string, taskID string) error {
    controlPath := filepath.Join(wsRoot, ".wvs", "control.json")
    return writeControl(controlPath, QuiesceRequestResume, taskID)
}

func writeControl(path string, state QuiesceState, taskID string) error {
    cf := ControlFile{
        State:     state,
        Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
        TaskID:    taskID,
    }
    data, err := json.Marshal(cf)
    if err != nil {
        return err
    }
    return os.WriteFile(path, data, 0644)
}

func readControlState(path string) (QuiesceState, error) {
    data, err := os.ReadFile(path)
    if err != nil {
        return "", err
    }
    var cf ControlFile
    if err := json.Unmarshal(data, &cf); err != nil {
        return "", err
    }
    return cf.State, nil
}
```

**Step 3: Write clone wrapper**

`internal/executor/clone.go`:
```go
package executor

import (
    "context"
    "fmt"
    "os/exec"
    "time"

    "go.uber.org/zap"
    "github.com/lzjever/mbos-wvs/internal/observability"
)

// Clone runs `juicefs clone src dst` via the mounted FUSE path.
func Clone(ctx context.Context, src, dst, op string, log *zap.Logger) error {
    start := time.Now()
    log.Info("clone: starting", zap.String("src", src), zap.String("dst", dst))

    cmd := exec.CommandContext(ctx, "juicefs", "clone", src, dst)
    output, err := cmd.CombinedOutput()
    duration := time.Since(start).Seconds()
    observability.CloneDuration.WithLabelValues(op).Observe(duration)

    if err != nil {
        observability.CloneFailTotal.WithLabelValues("exec_error").Inc()
        return fmt.Errorf("juicefs clone failed: %w, output: %s", err, string(output))
    }

    log.Info("clone: completed", zap.Float64("duration_s", duration))
    return nil
}
```

**Step 4: Write symlink switch**

`internal/executor/switch.go`:
```go
package executor

import (
    "fmt"
    "os"
    "path/filepath"
    "time"

    "go.uber.org/zap"
    "github.com/lzjever/mbos-wvs/internal/observability"
)

// SwitchCurrent atomically replaces the `current` symlink.
// Uses rename(2) which is atomic on POSIX.
func SwitchCurrent(wsRoot, newTarget string, log *zap.Logger) error {
    start := time.Now()
    currentLink := filepath.Join(wsRoot, "current")
    tmpLink := currentLink + ".tmp"

    // Create temp symlink pointing to new target
    os.Remove(tmpLink)
    if err := os.Symlink(newTarget, tmpLink); err != nil {
        return fmt.Errorf("symlink tmp: %w", err)
    }

    // Atomic rename
    if err := os.Rename(tmpLink, currentLink); err != nil {
        os.Remove(tmpLink)
        return fmt.Errorf("rename symlink: %w", err)
    }

    observability.SwitchDuration.Observe(time.Since(start).Seconds())
    log.Info("switch: current updated", zap.String("target", newTarget))
    return nil
}
```

**Step 5: Write task handlers**

`internal/executor/init_workspace.go`:
```go
package executor

import (
    "context"
    "fmt"
    "os"
    "path/filepath"

    "go.uber.org/zap"
)

func (s *Server) initWorkspace(ctx context.Context, wsid string, params map[string]string, log *zap.Logger) (map[string]string, error) {
    wsRoot := filepath.Join(s.cfg.MountPath, wsid)

    // Idempotency: if directory structure already exists, succeed
    currentLink := filepath.Join(wsRoot, "current")
    if _, err := os.Lstat(currentLink); err == nil {
        log.Info("init_workspace: already initialized, noop")
        target, _ := os.Readlink(currentLink)
        return map[string]string{"current_path": target}, nil
    }

    // Create directory structure: /ws/<wsid>/live/initial/ and /ws/<wsid>/.wvs/
    initialID := "initial"
    livePath := filepath.Join(wsRoot, "live", initialID)
    snapshotsDir := filepath.Join(wsRoot, "snapshots")
    wvsDir := filepath.Join(wsRoot, ".wvs")

    for _, dir := range []string{livePath, snapshotsDir, wvsDir} {
        if err := os.MkdirAll(dir, 0755); err != nil {
            return nil, fmt.Errorf("mkdir %s: %w", dir, err)
        }
    }

    // Create current symlink -> live/initial
    relTarget := filepath.Join("live", initialID)
    if err := os.Symlink(relTarget, currentLink); err != nil {
        return nil, fmt.Errorf("symlink current: %w", err)
    }

    log.Info("init_workspace: completed", zap.String("current", relTarget))
    return map[string]string{"current_path": filepath.Join(wsRoot, relTarget)}, nil
}
```

`internal/executor/snapshot_create.go`:
```go
package executor

import (
    "context"
    "encoding/json"
    "fmt"
    "os"
    "path/filepath"
    "time"

    "go.uber.org/zap"
    "github.com/lzjever/mbos-wvs/internal/observability"
)

type SnapshotMeta struct {
    SnapshotID string `json:"snapshot_id"`
    WSID       string `json:"wsid"`
    CreatedAt  string `json:"created_at"`
    Message    string `json:"message,omitempty"`
}

func (s *Server) snapshotCreate(ctx context.Context, wsid string, params map[string]string, log *zap.Logger) (map[string]string, error) {
    snapshotID := params["snapshot_id"]
    message := params["message"]
    wsRoot := filepath.Join(s.cfg.MountPath, wsid)
    dstPath := filepath.Join(wsRoot, "snapshots", snapshotID)

    // Idempotency: check if snapshot dir + meta already exist
    metaPath := filepath.Join(dstPath, ".wvs", "snapshot.json")
    if _, err := os.Stat(metaPath); err == nil {
        log.Info("snapshot_create: already exists, noop")
        return map[string]string{"snapshot_id": snapshotID, "fs_path": dstPath}, nil
    }

    // Resolve current target
    currentLink := filepath.Join(wsRoot, "current")
    srcPath, err := filepath.EvalSymlinks(currentLink)
    if err != nil {
        return nil, fmt.Errorf("resolve current symlink: %w", err)
    }

    // Quiesce
    start := time.Now()
    if err := Quiesce(ctx, wsRoot, params["task_id"], s.cfg.QuiesceTimeout, log); err != nil {
        observability.QuiesceTimeoutTotal.Inc()
        return nil, fmt.Errorf("quiesce failed: %w", err)
    }
    observability.QuiesceWaitSeconds.Observe(time.Since(start).Seconds())
    defer func() { _ = Resume(wsRoot, params["task_id"]) }()

    // Clone
    if err := Clone(ctx, srcPath, dstPath, "snapshot_create", log); err != nil {
        return nil, err
    }

    // Write snapshot metadata
    meta := SnapshotMeta{
        SnapshotID: snapshotID,
        WSID:       wsid,
        CreatedAt:  time.Now().UTC().Format(time.RFC3339),
        Message:    message,
    }
    if err := os.MkdirAll(filepath.Join(dstPath, ".wvs"), 0755); err != nil {
        return nil, fmt.Errorf("mkdir snapshot .wvs: %w", err)
    }
    metaData, _ := json.MarshalIndent(meta, "", "  ")
    if err := os.WriteFile(metaPath, metaData, 0644); err != nil {
        return nil, fmt.Errorf("write snapshot.json: %w", err)
    }

    return map[string]string{"snapshot_id": snapshotID, "fs_path": dstPath}, nil
}
```

`internal/executor/set_current.go`:
```go
package executor

import (
    "context"
    "fmt"
    "os"
    "path/filepath"
    "time"

    "go.uber.org/zap"
    "github.com/lzjever/mbos-wvs/internal/observability"
)

func (s *Server) setCurrent(ctx context.Context, wsid string, params map[string]string, log *zap.Logger) (map[string]string, error) {
    snapshotID := params["snapshot_id"]
    newLiveID := params["new_live_id"]
    wsRoot := filepath.Join(s.cfg.MountPath, wsid)

    srcPath := filepath.Join(wsRoot, "snapshots", snapshotID)
    dstPath := filepath.Join(wsRoot, "live", newLiveID)
    relTarget := filepath.Join("live", newLiveID)

    // Idempotency: if current already points to target
    currentLink := filepath.Join(wsRoot, "current")
    if target, err := os.Readlink(currentLink); err == nil && target == relTarget {
        log.Info("set_current: already pointing to target, noop")
        return map[string]string{"current_path": filepath.Join(wsRoot, relTarget)}, nil
    }

    // Verify source snapshot exists
    if _, err := os.Stat(srcPath); os.IsNotExist(err) {
        return nil, fmt.Errorf("snapshot dir not found: %s", srcPath)
    }

    // Quiesce
    start := time.Now()
    if err := Quiesce(ctx, wsRoot, params["task_id"], s.cfg.QuiesceTimeout, log); err != nil {
        observability.QuiesceTimeoutTotal.Inc()
        return nil, fmt.Errorf("quiesce failed: %w", err)
    }
    observability.QuiesceWaitSeconds.Observe(time.Since(start).Seconds())
    defer func() { _ = Resume(wsRoot, params["task_id"]) }()

    // Clone snapshot to new live directory
    if err := Clone(ctx, srcPath, dstPath, "set_current", log); err != nil {
        return nil, err
    }

    // Atomic switch current symlink
    if err := SwitchCurrent(wsRoot, relTarget, log); err != nil {
        return nil, err
    }

    return map[string]string{"current_path": filepath.Join(wsRoot, relTarget)}, nil
}
```

`internal/executor/snapshot_drop.go`:
```go
package executor

import (
    "context"
    "fmt"
    "os"
    "path/filepath"

    "go.uber.org/zap"
)

func (s *Server) snapshotDrop(ctx context.Context, wsid string, params map[string]string, log *zap.Logger) (map[string]string, error) {
    snapshotID := params["snapshot_id"]
    wsRoot := filepath.Join(s.cfg.MountPath, wsid)
    targetPath := filepath.Join(wsRoot, "snapshots", snapshotID)

    // Idempotency: if directory already gone, succeed
    if _, err := os.Stat(targetPath); os.IsNotExist(err) {
        log.Info("snapshot_drop: directory already removed, noop")
        return map[string]string{}, nil
    }

    // Remove directory tree
    if err := os.RemoveAll(targetPath); err != nil {
        return nil, fmt.Errorf("remove snapshot dir: %w", err)
    }

    log.Info("snapshot_drop: directory removed", zap.String("path", targetPath))
    return map[string]string{}, nil
}
```

**Step 6: Write gRPC server**

`internal/executor/server.go`:
```go
package executor

import (
    "context"
    "fmt"

    "go.uber.org/zap"
    "github.com/lzjever/mbos-wvs/internal/observability"
    pb "github.com/lzjever/mbos-wvs/gen/go/executor/v1"
)

type Server struct {
    pb.UnimplementedExecutorServiceServer
    cfg Config
    log *zap.Logger
}

func NewServer(cfg Config, log *zap.Logger) *Server {
    return &Server{cfg: cfg, log: log}
}

func (s *Server) ExecuteTask(ctx context.Context, req *pb.ExecuteTaskRequest) (*pb.ExecuteTaskResponse, error) {
    log := s.log.With(
        zap.String("task_id", req.TaskId),
        zap.String("wsid", req.Wsid),
        zap.String("op", req.Op.String()),
    )
    log.Info("executor: task received")
    observability.ExecutorActiveTasks.Inc()
    defer observability.ExecutorActiveTasks.Dec()

    var results map[string]string
    var err error

    switch req.Op {
    case pb.TaskOp_TASK_OP_INIT_WORKSPACE:
        results, err = s.initWorkspace(ctx, req.Wsid, req.Params, log)
    case pb.TaskOp_TASK_OP_SNAPSHOT_CREATE:
        results, err = s.snapshotCreate(ctx, req.Wsid, req.Params, log)
    case pb.TaskOp_TASK_OP_SNAPSHOT_DROP:
        results, err = s.snapshotDrop(ctx, req.Wsid, req.Params, log)
    case pb.TaskOp_TASK_OP_SET_CURRENT:
        results, err = s.setCurrent(ctx, req.Wsid, req.Params, log)
    default:
        return &pb.ExecuteTaskResponse{
            Success:      false,
            ErrorCode:    "UNKNOWN_OP",
            ErrorMessage: fmt.Sprintf("unknown op: %s", req.Op),
        }, nil
    }

    if err != nil {
        log.Error("executor: task failed", zap.Error(err))
        return &pb.ExecuteTaskResponse{
            Success:      false,
            ErrorCode:    "EXECUTOR_ERROR",
            ErrorMessage: err.Error(),
        }, nil
    }

    log.Info("executor: task succeeded")
    return &pb.ExecuteTaskResponse{
        Success: true,
        Results: results,
    }, nil
}
```

**Step 7: Write executor main**

`cmd/wvs-executor/main.go`:
```go
package main

import (
    "context"
    "fmt"
    "net"
    "net/http"
    "os"
    "os/signal"
    "syscall"

    "github.com/kelseyhightower/envconfig"
    "github.com/prometheus/client_golang/prometheus"
    "github.com/prometheus/client_golang/prometheus/promhttp"
    "go.uber.org/zap"
    "google.golang.org/grpc"

    "github.com/lzjever/mbos-wvs/internal/executor"
    "github.com/lzjever/mbos-wvs/internal/observability"
    pb "github.com/lzjever/mbos-wvs/gen/go/executor/v1"
)

func main() {
    var cfg executor.Config
    if err := envconfig.Process("", &cfg); err != nil {
        fmt.Fprintf(os.Stderr, "config: %v\n", err)
        os.Exit(1)
    }

    log, _ := observability.NewLogger(os.Getenv("WVS_LOG_LEVEL"))
    defer log.Sync()

    reg := prometheus.DefaultRegisterer
    observability.RegisterAll(reg)

    // Metrics HTTP server
    mux := http.NewServeMux()
    mux.Handle("/metrics", promhttp.Handler())
    mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
    go func() {
        log.Info("metrics server starting", zap.String("addr", cfg.MetricsAddr))
        if err := http.ListenAndServe(cfg.MetricsAddr, mux); err != nil {
            log.Fatal("metrics server failed", zap.Error(err))
        }
    }()

    // gRPC server
    lis, err := net.Listen("tcp", cfg.GRPCAddr)
    if err != nil {
        log.Fatal("listen failed", zap.Error(err))
    }

    srv := grpc.NewServer()
    pb.RegisterExecutorServiceServer(srv, executor.NewServer(cfg, log))

    ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
    defer cancel()

    go func() {
        log.Info("gRPC server starting", zap.String("addr", cfg.GRPCAddr))
        if err := srv.Serve(lis); err != nil {
            log.Fatal("grpc serve failed", zap.Error(err))
        }
    }()

    <-ctx.Done()
    log.Info("shutting down executor")
    srv.GracefulStop()
}
```

**Step 8: Verify compilation**

```bash
go build ./cmd/wvs-executor/
```

**Step 9: Commit**

```bash
git add internal/executor/ cmd/wvs-executor/
git commit -m "feat: add executor gRPC server (init, snapshot_create, snapshot_drop, set_current, quiesce, clone)"
```

---

## Task 7: Worker Dequeue Loop + Task Dispatch (`internal/worker/` + `cmd/wvs-worker/`)

**Files:**
- Create: `internal/worker/config.go`
- Create: `internal/worker/worker.go`
- Create: `internal/worker/dispatch.go`
- Create: `internal/executorclient/client.go`
- Create: `cmd/wvs-worker/main.go`

**Step 1: Write worker config**

`internal/worker/config.go`:
```go
package worker

import "time"

type Config struct {
    DBDSN           string        `envconfig:"WVS_DB_DSN" required:"true"`
    ExecutorAddrs   string        `envconfig:"EXECUTOR_ADDRS" required:"true"`
    MetricsAddr     string        `envconfig:"WVS_METRICS_ADDR" default:"0.0.0.0:9091"`
    LogLevel        string        `envconfig:"WVS_LOG_LEVEL" default:"info"`
    PollInterval    time.Duration `envconfig:"WORKER_POLL_INTERVAL" default:"1s"`
    IdleBackoff     time.Duration `envconfig:"WORKER_IDLE_BACKOFF" default:"5s"`
    ShutdownTimeout time.Duration `envconfig:"WORKER_SHUTDOWN_TIMEOUT" default:"120s"`
}
```

**Step 2: Write executor gRPC client**

`internal/executorclient/client.go`:
```go
package executorclient

import (
    "context"
    "fmt"

    "google.golang.org/grpc"
    "google.golang.org/grpc/credentials/insecure"

    pb "github.com/lzjever/mbos-wvs/gen/go/executor/v1"
)

type Client struct {
    conn   *grpc.ClientConn
    client pb.ExecutorServiceClient
}

func New(addr string) (*Client, error) {
    conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
    if err != nil {
        return nil, fmt.Errorf("dial executor %s: %w", addr, err)
    }
    return &Client{conn: conn, client: pb.NewExecutorServiceClient(conn)}, nil
}

func (c *Client) ExecuteTask(ctx context.Context, req *pb.ExecuteTaskRequest) (*pb.ExecuteTaskResponse, error) {
    return c.client.ExecuteTask(ctx, req)
}

func (c *Client) Close() error {
    return c.conn.Close()
}
```

**Step 3: Write dispatch logic**

`internal/worker/dispatch.go` — maps core.Task to executor gRPC call, handles post-execution DB updates:

```go
package worker

import (
    "context"
    "encoding/json"
    "fmt"
    "time"

    "go.uber.org/zap"

    "github.com/lzjever/mbos-wvs/internal/core"
    "github.com/lzjever/mbos-wvs/internal/observability"
    "github.com/lzjever/mbos-wvs/internal/store"
    "github.com/lzjever/mbos-wvs/internal/executorclient"
    pb "github.com/lzjever/mbos-wvs/gen/go/executor/v1"
)

var opMap = map[core.TaskOp]pb.TaskOp{
    core.OpInitWorkspace:  pb.TaskOp_TASK_OP_INIT_WORKSPACE,
    core.OpSnapshotCreate: pb.TaskOp_TASK_OP_SNAPSHOT_CREATE,
    core.OpSnapshotDrop:   pb.TaskOp_TASK_OP_SNAPSHOT_DROP,
    core.OpSetCurrent:     pb.TaskOp_TASK_OP_SET_CURRENT,
}

func (w *Worker) dispatch(ctx context.Context, task *store.Task, log *zap.Logger) {
    start := time.Now()
    defer func() {
        observability.TaskDuration.WithLabelValues(task.Op).Observe(time.Since(start).Seconds())
    }()

    // Parse params
    var params map[string]string
    _ = json.Unmarshal(task.Params, &params)
    if params == nil {
        params = map[string]string{}
    }
    params["task_id"] = task.TaskID

    // Special handling: set_current noop check
    if core.TaskOp(task.Op) == core.OpSetCurrent {
        if noop, err := w.checkSetCurrentNoop(ctx, task, params); err == nil && noop {
            return
        }
    }

    // Special handling: snapshot_drop — mark deleted_at in same lock txn (already done by caller)

    // Call executor
    pbOp, ok := opMap[core.TaskOp(task.Op)]
    if !ok {
        w.failTask(ctx, task, fmt.Errorf("unknown op: %s", task.Op), log)
        return
    }

    resp, err := w.executor.ExecuteTask(ctx, &pb.ExecuteTaskRequest{
        TaskId: task.TaskID,
        Wsid:   task.Wsid,
        Op:     pbOp,
        Params: params,
    })
    if err != nil {
        w.failTask(ctx, task, fmt.Errorf("executor call: %w", err), log)
        return
    }
    if !resp.Success {
        w.failTask(ctx, task, fmt.Errorf("%s: %s", resp.ErrorCode, resp.ErrorMessage), log)
        return
    }

    // Post-execution updates
    w.onSuccess(ctx, task, resp.Results, log)
}

func (w *Worker) checkSetCurrentNoop(ctx context.Context, task *store.Task, params map[string]string) (bool, error) {
    ws, err := w.queries.GetWorkspace(ctx, task.Wsid)
    if err != nil {
        return false, err
    }
    if ws.CurrentSnapshotID != nil && *ws.CurrentSnapshotID == params["snapshot_id"] {
        result, _ := json.Marshal(map[string]interface{}{"noop": true})
        _ = w.queries.CompleteTask(ctx, store.CompleteTaskParams{
            TaskID: task.TaskID,
            Status: string(core.TaskSucceeded),
            Result: result,
        })
        observability.TaskTotal.WithLabelValues(task.Op, string(core.TaskSucceeded)).Inc()
        return true, nil
    }
    return false, nil
}

func (w *Worker) onSuccess(ctx context.Context, task *store.Task, results map[string]string, log *zap.Logger) {
    resultJSON, _ := json.Marshal(results)

    switch core.TaskOp(task.Op) {
    case core.OpInitWorkspace:
        _ = w.queries.UpdateWorkspaceState(ctx, store.UpdateWorkspaceStateParams{
            Wsid: task.Wsid, State: string(core.WorkspaceActive),
        })
        observability.WorkspaceStateTransitions.WithLabelValues("PROVISIONING", "ACTIVE").Inc()

    case core.OpSnapshotCreate:
        // Insert snapshot record
        var params map[string]string
        _ = json.Unmarshal(task.Params, &params)
        _, _ = w.queries.CreateSnapshot(ctx, store.CreateSnapshotParams{
            SnapshotID: params["snapshot_id"],
            Wsid:       task.Wsid,
            FsPath:     results["fs_path"],
            Message:    nilIfEmpty(params["message"]),
        })

    case core.OpSetCurrent:
        var params map[string]string
        _ = json.Unmarshal(task.Params, &params)
        snapshotID := params["snapshot_id"]
        _ = w.queries.UpdateWorkspaceCurrent(ctx, store.UpdateWorkspaceCurrentParams{
            Wsid:              task.Wsid,
            CurrentSnapshotID: &snapshotID,
            CurrentPath:       results["current_path"],
        })

    case core.OpSnapshotDrop:
        // deleted_at already written in the lock transaction
    }

    _ = w.queries.CompleteTask(ctx, store.CompleteTaskParams{
        TaskID: task.TaskID,
        Status: string(core.TaskSucceeded),
        Result: resultJSON,
    })
    observability.TaskTotal.WithLabelValues(task.Op, string(core.TaskSucceeded)).Inc()
    log.Info("task succeeded")
}

func (w *Worker) failTask(ctx context.Context, task *store.Task, taskErr error, log *zap.Logger) {
    errJSON, _ := json.Marshal(map[string]string{"error": taskErr.Error()})

    if task.Attempt >= task.MaxAttempts {
        _ = w.queries.MarkTaskDead(ctx, store.MarkTaskDeadParams{TaskID: task.TaskID, Error: errJSON})
        observability.TaskTotal.WithLabelValues(task.Op, string(core.TaskDead)).Inc()
        // If init_workspace, mark workspace INIT_FAILED
        if core.TaskOp(task.Op) == core.OpInitWorkspace {
            _ = w.queries.UpdateWorkspaceState(ctx, store.UpdateWorkspaceStateParams{
                Wsid: task.Wsid, State: string(core.WorkspaceInitFailed),
            })
            observability.WorkspaceStateTransitions.WithLabelValues("PROVISIONING", "INIT_FAILED").Inc()
        }
        log.Error("task dead", zap.Error(taskErr))
    } else {
        _ = w.queries.FailTask(ctx, store.FailTaskParams{TaskID: task.TaskID, Error: errJSON})
        observability.TaskTotal.WithLabelValues(task.Op, string(core.TaskFailed)).Inc()
        observability.TaskRetryTotal.WithLabelValues(task.Op).Inc()
        log.Warn("task failed, will retry", zap.Error(taskErr), zap.Int("attempt", int(task.Attempt)))
    }
}

func nilIfEmpty(s string) *string {
    if s == "" {
        return nil
    }
    return &s
}
```

**Step 4: Write main worker loop**

`internal/worker/worker.go`:
```go
package worker

import (
    "context"
    "encoding/json"
    "time"

    "github.com/jackc/pgx/v5/pgxpool"
    "go.uber.org/zap"

    "github.com/lzjever/mbos-wvs/internal/core"
    "github.com/lzjever/mbos-wvs/internal/observability"
    "github.com/lzjever/mbos-wvs/internal/store"
    "github.com/lzjever/mbos-wvs/internal/executorclient"
)

type Worker struct {
    pool     *pgxpool.Pool
    queries  *store.Queries
    executor *executorclient.Client
    cfg      Config
    log      *zap.Logger
}

func New(pool *pgxpool.Pool, executor *executorclient.Client, cfg Config, log *zap.Logger) *Worker {
    return &Worker{
        pool:     pool,
        queries:  store.New(pool),
        executor: executor,
        cfg:      cfg,
        log:      log,
    }
}

func (w *Worker) Run(ctx context.Context) {
    w.log.Info("worker started")
    for {
        select {
        case <-ctx.Done():
            w.log.Info("worker stopping")
            return
        default:
        }

        task, err := w.queries.DequeueTask(ctx)
        if err != nil {
            // No task available
            observability.DequeueEmptyTotal.Inc()
            select {
            case <-ctx.Done():
                return
            case <-time.After(w.cfg.IdleBackoff):
                continue
            }
        }

        log := w.log.With(
            zap.String("task_id", task.TaskID),
            zap.String("wsid", task.Wsid),
            zap.String("op", task.Op),
            zap.Int("attempt", int(task.Attempt)),
        )
        log.Info("task dequeued")

        // Check cancel_requested
        if task.CancelRequested {
            errJSON, _ := json.Marshal(map[string]string{"error": "canceled"})
            _ = w.queries.CompleteTask(ctx, store.CompleteTaskParams{
                TaskID: task.TaskID,
                Status: string(core.TaskCanceled),
                Error:  errJSON,
            })
            log.Info("task canceled")
            continue
        }

        // Execute within advisory lock scope
        w.executeWithLock(ctx, &task, log)

        // Update queue depth metric
        if depth, err := w.queries.GetQueueDepth(ctx); err == nil {
            observability.TaskQueueDepth.Set(float64(depth))
        }
    }
}

func (w *Worker) executeWithLock(ctx context.Context, task *store.Task, log *zap.Logger) {
    // Use a transaction for advisory lock scope
    tx, err := w.pool.Begin(ctx)
    if err != nil {
        w.failTask(ctx, task, err, log)
        return
    }
    defer tx.Rollback(ctx)

    qtx := w.queries.WithTx(tx)

    // Acquire workspace lock
    lockStart := time.Now()
    if err := qtx.AcquireWorkspaceLock(ctx, task.Wsid); err != nil {
        w.failTask(ctx, task, err, log)
        return
    }
    observability.LockWaitSeconds.Observe(time.Since(lockStart).Seconds())

    // snapshot_drop special handling: mark deleted_at within lock txn
    if core.TaskOp(task.Op) == core.OpSnapshotDrop {
        var params map[string]string
        _ = json.Unmarshal(task.Params, &params)
        snapshotID := params["snapshot_id"]

        // Re-check references within lock
        referenced, err := qtx.IsSnapshotReferencedByTasks(ctx, store.IsSnapshotReferencedByTasksParams{
            Wsid:       task.Wsid,
            SnapshotID: snapshotID,
        })
        if err != nil || referenced {
            w.failTask(ctx, task, fmt.Errorf("snapshot still referenced"), log)
            return
        }

        // Mark deleted_at within this transaction
        if err := qtx.MarkSnapshotDeleted(ctx, snapshotID); err != nil {
            w.failTask(ctx, task, err, log)
            return
        }
    }

    if err := tx.Commit(ctx); err != nil {
        w.failTask(ctx, task, err, log)
        return
    }

    // Now dispatch to executor (outside lock)
    w.dispatch(ctx, task, log)
}
```

Note: The `store.Task` type and `store.Queries` methods will be generated by sqlc. The exact field names may need adjustment after generation. The `w.queries.WithTx(tx)` pattern is standard sqlc.

**Step 5: Write worker main**

`cmd/wvs-worker/main.go`:
```go
package main

import (
    "context"
    "fmt"
    "net/http"
    "os"
    "os/signal"
    "syscall"

    "github.com/kelseyhightower/envconfig"
    "github.com/prometheus/client_golang/prometheus"
    "github.com/prometheus/client_golang/prometheus/promhttp"
    "go.uber.org/zap"

    "github.com/lzjever/mbos-wvs/internal/executorclient"
    "github.com/lzjever/mbos-wvs/internal/observability"
    "github.com/lzjever/mbos-wvs/internal/store"
    "github.com/lzjever/mbos-wvs/internal/worker"
)

func main() {
    var cfg worker.Config
    if err := envconfig.Process("", &cfg); err != nil {
        fmt.Fprintf(os.Stderr, "config: %v\n", err)
        os.Exit(1)
    }

    log, _ := observability.NewLogger(cfg.LogLevel)
    defer log.Sync()

    reg := prometheus.DefaultRegisterer
    observability.RegisterAll(reg)

    ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
    defer cancel()

    pool, err := store.NewPool(ctx, cfg.DBDSN)
    if err != nil {
        log.Fatal("db connect failed", zap.Error(err))
    }
    defer pool.Close()

    exec, err := executorclient.New(cfg.ExecutorAddrs)
    if err != nil {
        log.Fatal("executor connect failed", zap.Error(err))
    }
    defer exec.Close()

    // Metrics server
    mux := http.NewServeMux()
    mux.Handle("/metrics", promhttp.Handler())
    mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
    go func() {
        log.Info("metrics server starting", zap.String("addr", cfg.MetricsAddr))
        if err := http.ListenAndServe(cfg.MetricsAddr, mux); err != nil {
            log.Fatal("metrics server failed", zap.Error(err))
        }
    }()

    w := worker.New(pool, exec, cfg, log)
    w.Run(ctx)
}
```

**Step 6: Verify compilation**

```bash
go build ./internal/worker/ ./internal/executorclient/ ./cmd/wvs-worker/
```

**Step 7: Commit**

```bash
git add internal/worker/ internal/executorclient/ cmd/wvs-worker/
git commit -m "feat: add worker dequeue loop, task dispatch, executor client"
```

---

## Task 8: HTTP API (`internal/api/` + `cmd/wvs-api/`)

**Files:**
- Create: `internal/api/config.go`
- Create: `internal/api/router.go`
- Create: `internal/api/middleware/request_id.go`
- Create: `internal/api/middleware/metrics.go`
- Create: `internal/api/middleware/recover.go`
- Create: `internal/api/response.go`
- Create: `internal/api/workspace_handler.go`
- Create: `internal/api/snapshot_handler.go`
- Create: `internal/api/current_handler.go`
- Create: `internal/api/task_handler.go`
- Create: `internal/api/health_handler.go`
- Create: `cmd/wvs-api/main.go`

This is the largest task. Key points from tech docs:
- All async write endpoints require `Idempotency-Key` header (400 if missing)
- Cursor-based pagination with `cursor` + `limit` (default 20, max 100)
- 13 error codes mapping to specific HTTP statuses
- POST /v1/workspaces → 202
- POST /v1/workspaces/{wsid}/retry-init → 202
- GET /v1/workspaces, GET /v1/workspaces/{wsid}
- DELETE /v1/workspaces/{wsid} → 200 (sync, with disable-after rules)
- POST /v1/workspaces/{wsid}/snapshots → 202
- GET /v1/workspaces/{wsid}/snapshots
- DELETE /v1/workspaces/{wsid}/snapshots/{snapshot_id} → 202
- GET /v1/workspaces/{wsid}/current
- POST /v1/workspaces/{wsid}/current:set → 202
- GET /v1/tasks, GET /v1/tasks/{task_id}
- POST /v1/tasks/{task_id}:cancel → 200

Implementation follows the standard pattern: handler reads request → validates → checks idempotency → creates task (for async) or performs action (for sync) → writes audit → returns response.

Each handler is a separate file. The router wires them together with chi. Middleware handles request_id generation, metrics instrumentation, and panic recovery.

**Full code for each file is lengthy — the implementing agent should follow the API contract in `wvs-tech-docs-20260218-2035.md` docs/04 exactly.** Key implementation notes:

1. **Idempotency flow**: For every async write handler:
   - Extract `Idempotency-Key` from header (400 if missing)
   - Compute `request_hash = SHA-256(sorted_json(body) + method + path)`
   - Try `GetTaskByIdempotencyKey` — if found and hash matches, return existing task
   - If found and hash differs, return 409 `WVS_CONFLICT_IDEMPOTENT_MISMATCH`
   - If not found, `CreateTask` and return 202

2. **Workspace disable**: Synchronous — check no active tasks, update state, return 200. Gate all write endpoints with workspace state check (return 410 if DISABLED).

3. **Pagination**: Parse `cursor` (opaque base64-encoded timestamp) and `limit` from query params. Return `next_cursor` if more results.

**Step N: Verify all endpoints compile and basic handler tests pass**

```bash
go build ./internal/api/ ./cmd/wvs-api/
```

**Step N+1: Commit**

```bash
git add internal/api/ cmd/wvs-api/
git commit -m "feat: add HTTP API (workspace, snapshot, current, task handlers + middleware)"
```

---

## Task 9: wvsctl CLI (`cmd/wvsctl/`)

**Files:**
- Create: `cmd/wvsctl/main.go`
- Create: `cmd/wvsctl/client.go`
- Create: `cmd/wvsctl/workspace.go`
- Create: `cmd/wvsctl/snapshot.go`
- Create: `cmd/wvsctl/current.go`
- Create: `cmd/wvsctl/task.go`
- Create: `cmd/wvsctl/obs.go`
- Create: `cmd/wvsctl/output.go`

Uses `cobra` for CLI framework. Commands from tech docs:

```
wvsctl workspace create|get|list|retry-init|disable
wvsctl snapshot create|list|drop
wvsctl current get|set
wvsctl task list|get|watch
wvsctl obs summary|latency|queue|quiesce
```

Key features:
- Default table output + `--json` flag
- `--wait` flag on write commands: polls task status until terminal
- `--api-url` flag (default `http://localhost:8080`)
- All write commands print `task_id` and follow-up query command
- Error output includes WVS error code

The `client.go` is a thin HTTP client wrapper that calls the API endpoints, parses JSON responses, and returns typed structs.

The `obs` commands query vmsingle directly (`http://localhost:8428/api/v1/query`) for metrics like task_success_rate, P95 latency, queue depth, quiesce timeout rate.

**Commit:**

```bash
git add cmd/wvsctl/
git commit -m "feat: add wvsctl CLI (workspace, snapshot, current, task, obs commands)"
```

---

## Task 10: Docker Build + Compose + Configs

**Files:**
- Create: `Dockerfile.api`
- Create: `Dockerfile.worker`
- Create: `Dockerfile.executor`
- Create: `docker-compose.yml` (from tech docs, already defined)
- Create: `configs/vmagent/prometheus.yml`

**Step 1: Write multi-stage Dockerfiles**

`Dockerfile.api`:
```dockerfile
FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /bin/wvs-api ./cmd/wvs-api
COPY migrations/ /migrations/

FROM alpine:3.19
RUN apk add --no-cache ca-certificates
COPY --from=build /bin/wvs-api /usr/local/bin/wvs-api
COPY --from=build /migrations/ /migrations/
# Also include migrate binary for wvs-migrate service
RUN apk add --no-cache curl && \
    curl -L https://github.com/golang-migrate/migrate/releases/download/v4.17.0/migrate.linux-amd64.tar.gz | tar xvz -C /usr/local/bin/
ENTRYPOINT ["wvs-api"]
```

`Dockerfile.worker`:
```dockerfile
FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /bin/wvs-worker ./cmd/wvs-worker

FROM alpine:3.19
RUN apk add --no-cache ca-certificates
COPY --from=build /bin/wvs-worker /usr/local/bin/wvs-worker
ENTRYPOINT ["wvs-worker"]
```

`Dockerfile.executor`:
```dockerfile
FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /bin/wvs-executor ./cmd/wvs-executor

FROM ubuntu:22.04
RUN apt-get update && apt-get install -y fuse3 curl && rm -rf /var/lib/apt/lists/*
# Install JuiceFS
RUN curl -sSL https://d.juicefs.com/install | sh -
COPY --from=build /bin/wvs-executor /usr/local/bin/wvs-executor
# Entrypoint: format volume if needed, mount, then start executor
COPY scripts/executor-entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh
ENTRYPOINT ["/entrypoint.sh"]
```

**Step 2: Write executor entrypoint script**

`scripts/executor-entrypoint.sh`:
```bash
#!/usr/bin/env bash
set -euo pipefail

MOUNT_PATH="${EXECUTOR_MOUNT_PATH:-/ws}"

# Format JuiceFS volume (idempotent - skips if already formatted)
juicefs format \
  --storage minio \
  --bucket "${MINIO_ENDPOINT}/${MINIO_BUCKET}" \
  --access-key "${MINIO_ACCESS_KEY}" \
  --secret-key "${MINIO_SECRET_KEY}" \
  "${JFS_META_URL}" \
  wvs-data 2>/dev/null || true

# Mount JuiceFS
mkdir -p "${MOUNT_PATH}"
juicefs mount \
  "${JFS_META_URL}" \
  "${MOUNT_PATH}" \
  --metrics "0.0.0.0:9567" \
  --background

# Wait for mount to be ready
for i in $(seq 1 30); do
  if mountpoint -q "${MOUNT_PATH}"; then
    echo "JuiceFS mounted at ${MOUNT_PATH}"
    break
  fi
  sleep 1
done

# Start executor
exec wvs-executor
```

**Step 3: Write vmagent config**

`configs/vmagent/prometheus.yml` (from tech docs):
```yaml
global:
  scrape_interval: 15s

scrape_configs:
  - job_name: wvs-api
    static_configs:
      - targets: ["wvs-api:9090"]
  - job_name: wvs-worker
    static_configs:
      - targets: ["wvs-worker:9091"]
  - job_name: wvs-executor
    static_configs:
      - targets: ["executor:9092"]
  - job_name: juicefs-mount
    static_configs:
      - targets: ["executor:9567"]

remote_write:
  - url: "http://vmsingle:8428/api/v1/write"
```

**Step 4: Write docker-compose.yml**

Copy exactly from tech docs (already fully defined in the document, including wvs-migrate service and GA profile annotations).

**Step 5: Verify docker build**

```bash
make docker
```

**Step 6: Commit**

```bash
git add Dockerfile.* docker-compose.yml configs/ scripts/executor-entrypoint.sh
git commit -m "feat: add Dockerfiles, docker-compose, vmagent config, executor entrypoint"
```

---

## Task 11: Smoke Test Script

**Files:**
- Create: `scripts/smoke-test.sh`

Copy the smoke test script exactly from tech docs docs/12 section. Adjust `wvsctl` binary path as needed (assumes it's in PATH or `./bin/wvsctl`).

**Commit:**

```bash
git add scripts/smoke-test.sh
git commit -m "feat: add end-to-end smoke test script"
```

---

## Task 12: Integration Tests

**Files:**
- Create: `internal/store/store_test.go` (uses testcontainers for PG)
- Create: `internal/api/api_test.go` (HTTP handler tests)

**Key test scenarios:**

1. **Store tests** (with real PG via testcontainers):
   - Create workspace → verify state PROVISIONING
   - Dequeue task → verify status RUNNING, attempt incremented
   - Idempotency: duplicate insert → unique constraint violation
   - Advisory lock: concurrent dequeue → only one succeeds per workspace
   - Snapshot reference check → blocks delete of referenced snapshot

2. **API tests** (httptest + mock store):
   - POST /v1/workspaces without Idempotency-Key → 400
   - POST /v1/workspaces with valid body → 202
   - GET /v1/workspaces/{wsid} not found → 404
   - DELETE /v1/workspaces/{wsid} when DISABLED → 200 (idempotent)
   - POST on DISABLED workspace → 410

**Commit:**

```bash
git add internal/store/store_test.go internal/api/api_test.go
git commit -m "test: add store integration tests and API handler tests"
```

---

## Task 13: Final Wiring + Full Demo Verify

**Step 1: Run full build**

```bash
make build
```

**Step 2: Run all tests**

```bash
make test
```

**Step 3: Build docker images**

```bash
make docker
```

**Step 4: Start demo**

```bash
make up
# Wait for all services healthy
docker compose ps
```

**Step 5: Run smoke test**

```bash
make smoke-test
```

Expected: `=== ALL SMOKE TESTS PASSED ===`

**Step 6: Verify metrics**

```bash
curl -s http://localhost:8428/api/v1/query?query=wvs_task_total | jq .
curl -s http://localhost:8428/api/v1/query?query=wvs_http_requests_total | jq .
```

**Step 7: Final commit**

```bash
git add -A
git commit -m "feat: complete WVS MVP — all components wired, demo verified, smoke tests pass"
```

---

## Dependency Graph (Parallel Execution Guide)

```
Task 0: Scaffolding
  ├─→ Task 1: Migrations (DDL)
  ├─→ Task 2: gRPC Proto
  └─→ Task 3: Core Domain Models
        ├─→ Task 4: sqlc Store Layer (needs Task 1 + Task 3)
        ├─→ Task 5: Observability (needs Task 3)
        └─→ Task 6: Executor (needs Task 2 + Task 3 + Task 5)
              └─→ Task 7: Worker (needs Task 4 + Task 5 + Task 6)
                    └─→ Task 8: API (needs Task 4 + Task 5 + Task 7)
                          └─→ Task 9: wvsctl (needs Task 8)
                                └─→ Task 10: Docker + Compose
                                      └─→ Task 11: Smoke Test
                                            └─→ Task 12: Integration Tests
                                                  └─→ Task 13: Final Verify
```

**Maximum parallelism at Layer 1:** Tasks 4, 5, 6 can run concurrently after Task 3 completes (Task 6 also needs Task 2).
