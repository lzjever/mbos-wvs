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
