package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"go.uber.org/zap"

	"github.com/lzjever/mbos-wvs/internal/observability"
)

type SnapshotMeta struct {
	SnapshotID string `json:"snapshot_id"`
	WSID       string `json:"wsid"`
	CreatedAt  string `json:"created_at"`
	Message    string `json:"message,omitempty"`
}

func (s *Server) snapshotCreate(ctx context.Context, wsid string, params map[string]string, log *zap.Logger) (map[string]string, error) {
	snapshotID := params["snapshot_id"]
	message := params["message"]
	wsRoot := filepath.Join(s.cfg.MountPath, wsid)
	dstPath := filepath.Join(wsRoot, "snapshots", snapshotID)

	// Idempotency: check if snapshot dir + meta already exist
	metaPath := filepath.Join(dstPath, ".wvs", "snapshot.json")
	if _, err := os.Stat(metaPath); err == nil {
		log.Info("snapshot_create: already exists, noop")
		return map[string]string{"snapshot_id": snapshotID, "fs_path": dstPath}, nil
	}

	// Resolve current target
	currentLink := filepath.Join(wsRoot, "current")
	srcPath, err := filepath.EvalSymlinks(currentLink)
	if err != nil {
		return nil, fmt.Errorf("resolve current symlink: %w", err)
	}

	// Quiesce
	start := time.Now()
	if err := Quiesce(ctx, wsRoot, params["task_id"], s.cfg.QuiesceTimeout, log); err != nil {
		observability.QuiesceTimeoutTotal.Inc()
		return nil, fmt.Errorf("quiesce failed: %w", err)
	}
	observability.QuiesceWaitSeconds.Observe(time.Since(start).Seconds())
	defer func() { _ = Resume(wsRoot, params["task_id"]) }()

	// Clone
	if err := Clone(ctx, srcPath, dstPath, "snapshot_create", log); err != nil {
		return nil, err
	}

	// Write snapshot metadata
	meta := SnapshotMeta{
		SnapshotID: snapshotID,
		WSID:       wsid,
		CreatedAt:  time.Now().UTC().Format(time.RFC3339),
		Message:    message,
	}
	if err := os.MkdirAll(filepath.Join(dstPath, ".wvs"), 0755); err != nil {
		return nil, fmt.Errorf("mkdir snapshot .wvs: %w", err)
	}
	metaData, _ := json.MarshalIndent(meta, "", "  ")
	if err := os.WriteFile(metaPath, metaData, 0644); err != nil {
		return nil, fmt.Errorf("write snapshot.json: %w", err)
	}

	return map[string]string{"snapshot_id": snapshotID, "fs_path": dstPath}, nil
}
