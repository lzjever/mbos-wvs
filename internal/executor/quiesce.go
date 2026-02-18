package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"go.uber.org/zap"
)

type QuiesceState string

const (
	QuiesceRunning       QuiesceState = "RUNNING"
	QuiesceRequestFreeze QuiesceState = "REQUEST_FREEZE"
	QuiesceFrozen        QuiesceState = "FROZEN"
	QuiesceRequestResume QuiesceState = "REQUEST_RESUME"
)

type ControlFile struct {
	State     QuiesceState `json:"state"`
	Timestamp string       `json:"timestamp"`
	TaskID    string       `json:"task_id,omitempty"`
}

// Quiesce writes REQUEST_FREEZE to control.json and polls for FROZEN ack.
// Returns nil on success. On timeout, writes REQUEST_RESUME and returns error.
func Quiesce(ctx context.Context, wsRoot string, taskID string, timeout time.Duration, log *zap.Logger) error {
	controlPath := filepath.Join(wsRoot, ".wvs", "control.json")

	// Ensure .wvs directory exists
	if err := os.MkdirAll(filepath.Dir(controlPath), 0755); err != nil {
		return fmt.Errorf("mkdir .wvs: %w", err)
	}

	// Write REQUEST_FREEZE
	if err := writeControl(controlPath, QuiesceRequestFreeze, taskID); err != nil {
		return fmt.Errorf("write REQUEST_FREEZE: %w", err)
	}
	log.Info("quiesce: REQUEST_FREEZE written", zap.String("path", controlPath))

	// Poll for FROZEN
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			_ = writeControl(controlPath, QuiesceRequestResume, taskID)
			return ctx.Err()
		case <-ticker.C:
			if time.Now().After(deadline) {
				_ = writeControl(controlPath, QuiesceRequestResume, taskID)
				return fmt.Errorf("quiesce timeout after %s", timeout)
			}
			state, err := readControlState(controlPath)
			if err != nil {
				continue
			}
			if state == QuiesceFrozen {
				log.Info("quiesce: FROZEN ack received")
				return nil
			}
		}
	}
}

// Resume writes REQUEST_RESUME to control.json.
func Resume(wsRoot string, taskID string) error {
	controlPath := filepath.Join(wsRoot, ".wvs", "control.json")
	return writeControl(controlPath, QuiesceRequestResume, taskID)
}

func writeControl(path string, state QuiesceState, taskID string) error {
	cf := ControlFile{
		State:     state,
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		TaskID:    taskID,
	}
	data, err := json.Marshal(cf)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func readControlState(path string) (QuiesceState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var cf ControlFile
	if err := json.Unmarshal(data, &cf); err != nil {
		return "", err
	}
	return cf.State, nil
}
