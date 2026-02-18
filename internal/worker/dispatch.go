package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"go.uber.org/zap"

	"github.com/lzjever/mbos-wvs/internal/core"
	"github.com/lzjever/mbos-wvs/internal/observability"
	"github.com/lzjever/mbos-wvs/internal/store"
	pb "github.com/lzjever/mbos-wvs/gen/go/executor/v1"
)

var opMap = map[core.TaskOp]pb.TaskOp{
	core.OpInitWorkspace:  pb.TaskOp_TASK_OP_INIT_WORKSPACE,
	core.OpSnapshotCreate: pb.TaskOp_TASK_OP_SNAPSHOT_CREATE,
	core.OpSnapshotDrop:   pb.TaskOp_TASK_OP_SNAPSHOT_DROP,
	core.OpSetCurrent:     pb.TaskOp_TASK_OP_SET_CURRENT,
}

func (w *Worker) dispatch(ctx context.Context, task *store.WvsTask, log *zap.Logger) {
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

func (w *Worker) checkSetCurrentNoop(ctx context.Context, task *store.WvsTask, params map[string]string) (bool, error) {
	ws, err := w.queries.GetWorkspace(ctx, task.Wsid)
	if err != nil {
		return false, err
	}
	if ws.CurrentSnapshotID.Valid && ws.CurrentSnapshotID.String == params["snapshot_id"] {
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

func (w *Worker) onSuccess(ctx context.Context, task *store.WvsTask, results map[string]string, log *zap.Logger) {
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
			Message:    textFromString(params["message"]),
		})

	case core.OpSetCurrent:
		var params map[string]string
		_ = json.Unmarshal(task.Params, &params)
		snapshotID := params["snapshot_id"]
		_ = w.queries.UpdateWorkspaceCurrent(ctx, store.UpdateWorkspaceCurrentParams{
			Wsid:              task.Wsid,
			CurrentSnapshotID: textFromString(snapshotID),
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

func (w *Worker) failTask(ctx context.Context, task *store.WvsTask, taskErr error, log *zap.Logger) {
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

func textFromString(s string) pgtype.Text {
	if s == "" {
		return pgtype.Text{Valid: false}
	}
	return pgtype.Text{String: s, Valid: true}
}
