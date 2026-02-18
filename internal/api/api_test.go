package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/lzjever/mbos-wvs/internal/core"
)

// Mock tests for API handlers without DB dependency

func TestHealthHandler(t *testing.T) {
	api := &API{}
	r := chi.NewRouter()
	r.Get("/healthz", api.HealthHandler)

	req := httptest.NewRequest("GET", "/healthz", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
	if w.Body.String() != "OK" {
		t.Errorf("expected body OK, got %s", w.Body.String())
	}
}

func TestWriteError(t *testing.T) {
	w := httptest.NewRecorder()
	WriteError(w, core.NewAppError(core.ErrBadRequest, "test error"))

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}

	var resp ErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %s", err)
	}
	if resp.Code != "WVS_BAD_REQUEST" {
		t.Errorf("expected code WVS_BAD_REQUEST, got %s", resp.Code)
	}
}

func TestWriteJSON(t *testing.T) {
	w := httptest.NewRecorder()
	data := map[string]string{"key": "value"}
	WriteJSON(w, http.StatusOK, data)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %s", err)
	}
	if resp["key"] != "value" {
		t.Errorf("expected key=value, got %v", resp)
	}
}

func TestWriteAccepted(t *testing.T) {
	w := httptest.NewRecorder()
	WriteAccepted(w, "task-123", "/v1/tasks/")

	if w.Code != http.StatusAccepted {
		t.Errorf("expected status 202, got %d", w.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %s", err)
	}
	if resp["task_id"] != "task-123" {
		t.Errorf("expected task_id task-123, got %v", resp["task_id"])
	}
	if resp["status"] != "PENDING" {
		t.Errorf("expected status PENDING, got %v", resp["status"])
	}
}

func TestIdempotencyKeyRequired(t *testing.T) {
	// This test verifies the error response format for missing Idempotency-Key
	w := httptest.NewRecorder()
	WriteError(w, core.NewAppError(core.ErrBadRequest, "Idempotency-Key header required"))

	var resp ErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %s", err)
	}
	if resp.Code != "WVS_BAD_REQUEST" {
		t.Errorf("expected code WVS_BAD_REQUEST, got %s", resp.Code)
	}
}
