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
