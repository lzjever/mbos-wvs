package core

import "github.com/google/uuid"

// NewID generates a UUID v7 (time-ordered).
func NewID() string {
	id, err := uuid.NewV7()
	if err != nil {
		// Fallback to v4 if v7 fails (should not happen).
		return uuid.New().String()
	}
	return id.String()
}
