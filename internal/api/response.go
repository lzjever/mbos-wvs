package api

import (
	"encoding/json"
	"net/http"

	"github.com/lzjever/mbos-wvs/internal/core"
)

// ErrorResponse represents a WVS error response.
type ErrorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// WriteError writes a WVS error response.
func WriteError(w http.ResponseWriter, err *core.AppError) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(err.Code.HTTPStatus())
	json.NewEncoder(w).Encode(ErrorResponse{
		Code:    string(err.Code),
		Message: err.Message,
	})
}

// WriteJSON writes a JSON response.
func WriteJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// WriteAccepted writes a 202 Accepted response with a task reference.
func WriteAccepted(w http.ResponseWriter, taskID string, path string) {
	WriteJSON(w, http.StatusAccepted, map[string]interface{}{
		"task_id":     taskID,
		"status":      "PENDING",
		"status_href": path + taskID,
	})
}
