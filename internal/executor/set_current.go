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
