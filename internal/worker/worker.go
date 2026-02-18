package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
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

func (w *Worker) executeWithLock(ctx context.Context, task *store.WvsTask, log *zap.Logger) {
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
			SnapshotID: pgtype.Text{String: snapshotID, Valid: true},
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
