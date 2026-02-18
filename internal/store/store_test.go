package store

import (
	"context"
	"testing"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestStoreIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	pgContainer, err := postgres.Run(ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("wvs"),
		postgres.WithUsername("wvs"),
		postgres.WithPassword("wvs_pass"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections"),
		),
	)
	if err != nil {
		t.Fatalf("failed to start container: %s", err)
	}
	defer pgContainer.Terminate(ctx)

	connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("failed to get connection string: %s", err)
	}

	pool, err := NewPool(ctx, connStr)
	if err != nil {
		t.Fatalf("failed to connect: %s", err)
	}
	defer pool.Close()

	// Run migrations manually or use embed
	_, err = pool.Exec(ctx, `
		CREATE SCHEMA IF NOT EXISTS wvs;
		CREATE TABLE wvs.workspaces (
			wsid TEXT PRIMARY KEY,
			root_path TEXT NOT NULL UNIQUE,
			owner TEXT NOT NULL,
			state TEXT NOT NULL DEFAULT 'PROVISIONING',
			current_snapshot_id TEXT,
			current_path TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		);
		CREATE TABLE wvs.tasks (
			task_id TEXT PRIMARY KEY,
			wsid TEXT NOT NULL REFERENCES wvs.workspaces(wsid),
			op TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'PENDING',
			idempotency_key TEXT NOT NULL,
			request_hash TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			started_at TIMESTAMPTZ,
			ended_at TIMESTAMPTZ,
			attempt INT NOT NULL DEFAULT 0,
			max_attempts INT NOT NULL DEFAULT 5,
			next_run_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			timeout_seconds INT NOT NULL DEFAULT 300,
			cancel_requested BOOLEAN NOT NULL DEFAULT false,
			params JSONB NOT NULL DEFAULT '{}'::jsonb,
			result JSONB,
			error JSONB
		);
	`)
	if err != nil {
		t.Fatalf("failed to run migrations: %s", err)
	}

	queries := New(pool)

	t.Run("CreateWorkspace", func(t *testing.T) {
		ws, err := queries.CreateWorkspace(ctx, CreateWorkspaceParams{
			Wsid:        "test-ws-1",
			RootPath:    "/ws/test-ws-1",
			Owner:       "test-user",
			CurrentPath: "/ws/test-ws-1",
		})
		if err != nil {
			t.Fatalf("failed to create workspace: %s", err)
		}
		if ws.Wsid != "test-ws-1" {
			t.Errorf("expected wsid test-ws-1, got %s", ws.Wsid)
		}
		if ws.State != "PROVISIONING" {
			t.Errorf("expected state PROVISIONING, got %s", ws.State)
		}
	})

	t.Run("GetWorkspace", func(t *testing.T) {
		ws, err := queries.GetWorkspace(ctx, "test-ws-1")
		if err != nil {
			t.Fatalf("failed to get workspace: %s", err)
		}
		if ws.Owner != "test-user" {
			t.Errorf("expected owner test-user, got %s", ws.Owner)
		}
	})

	t.Run("CreateTask", func(t *testing.T) {
		task, err := queries.CreateTask(ctx, CreateTaskParams{
			TaskID:         "task-1",
			Wsid:           "test-ws-1",
			Op:             "init_workspace",
			IdempotencyKey: "key-1",
			RequestHash:    "hash-1",
			Params:         []byte("{}"),
			MaxAttempts:    5,
			TimeoutSeconds: 300,
		})
		if err != nil {
			t.Fatalf("failed to create task: %s", err)
		}
		if task.Status != "PENDING" {
			t.Errorf("expected status PENDING, got %s", task.Status)
		}
	})

	t.Run("GetTaskByIdempotencyKey", func(t *testing.T) {
		task, err := queries.GetTaskByIdempotencyKey(ctx, GetTaskByIdempotencyKeyParams{
			Wsid:           "test-ws-1",
			Op:             "init_workspace",
			IdempotencyKey: "key-1",
		})
		if err != nil {
			t.Fatalf("failed to get task by idempotency key: %s", err)
		}
		if task.TaskID != "task-1" {
			t.Errorf("expected task_id task-1, got %s", task.TaskID)
		}
	})
}
