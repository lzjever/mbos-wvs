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
