package api

import (
	"net/http"
)

// HealthHandler returns 200 if service is healthy.
func (a *API) HealthHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

// ReadyHandler returns 200 if service is ready to accept requests.
func (a *API) ReadyHandler(w http.ResponseWriter, r *http.Request) {
	// Check DB connectivity
	ctx := r.Context()
	if err := a.pool.Ping(ctx); err != nil {
		WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "db unavailable"})
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}
