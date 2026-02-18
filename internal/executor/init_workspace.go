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
		return map[string]string{"current_path": filepath.Join(wsRoot, target)}, nil
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
