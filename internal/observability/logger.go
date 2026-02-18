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
