package executor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"go.uber.org/zap"
)

func (s *Server) snapshotDrop(ctx context.Context, wsid string, params map[string]string, log *zap.Logger) (map[string]string, error) {
	snapshotID := params["snapshot_id"]
	wsRoot := filepath.Join(s.cfg.MountPath, wsid)
	targetPath := filepath.Join(wsRoot, "snapshots", snapshotID)

	// Idempotency: if directory already gone, succeed
	if _, err := os.Stat(targetPath); os.IsNotExist(err) {
		log.Info("snapshot_drop: directory already removed, noop")
		return map[string]string{}, nil
	}

	// Remove directory tree
	if err := os.RemoveAll(targetPath); err != nil {
		return nil, fmt.Errorf("remove snapshot dir: %w", err)
	}

	log.Info("snapshot_drop: directory removed", zap.String("path", targetPath))
	return map[string]string{}, nil
}
