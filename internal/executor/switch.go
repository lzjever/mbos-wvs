package executor

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"go.uber.org/zap"

	"github.com/lzjever/mbos-wvs/internal/observability"
)

// SwitchCurrent atomically replaces the `current` symlink.
// Uses rename(2) which is atomic on POSIX.
func SwitchCurrent(wsRoot, newTarget string, log *zap.Logger) error {
	start := time.Now()
	currentLink := filepath.Join(wsRoot, "current")
	tmpLink := currentLink + ".tmp"

	// Create temp symlink pointing to new target
	os.Remove(tmpLink)
	if err := os.Symlink(newTarget, tmpLink); err != nil {
		return fmt.Errorf("symlink tmp: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tmpLink, currentLink); err != nil {
		os.Remove(tmpLink)
		return fmt.Errorf("rename symlink: %w", err)
	}

	observability.SwitchDuration.Observe(time.Since(start).Seconds())
	log.Info("switch: current updated", zap.String("target", newTarget))
	return nil
}
