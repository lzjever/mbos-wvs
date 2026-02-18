package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"time"

	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	"github.com/lzjever/mbos-wvs/internal/api/middleware"
	"github.com/lzjever/mbos-wvs/internal/store"
)

type API struct {
	pool    *pgxpool.Pool
	queries *store.Queries
	log     *zap.Logger
}

func NewAPI(pool *pgxpool.Pool, log *zap.Logger) *API {
	return &API{
		pool:    pool,
		queries: store.New(pool),
		log:     log,
	}
}

func (a *API) Router() chi.Router {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.Metrics)
	r.Use(middleware.Recoverer(a.log))
	r.Use(middleware.Logger)
	r.Use(chiMiddleware.AllowContentType("application/json"))

	// Health endpoints
	r.Get("/healthz", a.HealthHandler)
	r.Get("/readyz", a.ReadyHandler)

	// API v1 routes
	r.Route("/v1", func(r chi.Router) {
		// Workspaces
		r.Get("/workspaces", a.ListWorkspaces)
		r.Post("/workspaces", a.CreateWorkspace)
		r.Get("/workspaces/{wsid}", a.GetWorkspace)
		r.Delete("/workspaces/{wsid}", a.DisableWorkspace)
		r.Post("/workspaces/{wsid}/retry-init", a.RetryInit)

		// Snapshots
		r.Get("/workspaces/{wsid}/snapshots", a.ListSnapshots)
		r.Post("/workspaces/{wsid}/snapshots", a.CreateSnapshot)
		r.Delete("/workspaces/{wsid}/snapshots/{snapshot_id}", a.DropSnapshot)

		// Current
		r.Get("/workspaces/{wsid}/current", a.GetCurrent)
		r.Post("/workspaces/{wsid}/current:set", a.SetCurrent)

		// Tasks
		r.Get("/tasks", a.ListTasks)
		r.Get("/tasks/{task_id}", a.GetTask)
		r.Post("/tasks/{task_id}:cancel", a.CancelTask)
	})

	return r
}

// writeAudit writes an audit log entry.
func (a *API) writeAudit(ctx context.Context, wsid string, action string, taskID *string, payload interface{}) error {
	var taskIDVal pgtype.Text
	if taskID != nil {
		taskIDVal = pgtype.Text{String: *taskID, Valid: true}
	}

	payloadBytes, _ := json.Marshal(payload)
	actor, _ := json.Marshal(map[string]string{"source": "api"})

	_, err := a.queries.InsertAudit(ctx, store.InsertAuditParams{
		Wsid:    pgtype.Text{String: wsid, Valid: true},
		Actor:   actor,
		Action:  action,
		TaskID:  taskIDVal,
		Payload: payloadBytes,
	})
	return err
}

// encodeCursor encodes a timestamp as a base64 cursor.
func encodeCursor(t pgtype.Timestamptz) string {
	if !t.Valid {
		return ""
	}
	return base64.StdEncoding.EncodeToString([]byte(t.Time.Format(time.RFC3339Nano)))
}

// decodeCursor decodes a base64 cursor to a timestamp.
func decodeCursor(s string) (time.Time, error) {
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return time.Time{}, err
	}
	return time.Parse(time.RFC3339Nano, string(b))
}
