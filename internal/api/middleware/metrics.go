package middleware

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/lzjever/mbos-wvs/internal/observability"
)

// Metrics records HTTP metrics for each request.
func Metrics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Wrap response writer to capture status code
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

		observability.ActiveRequests.Inc()
		defer observability.ActiveRequests.Dec()

		next.ServeHTTP(ww, r)

		// Record metrics
		duration := time.Since(start).Seconds()
		route := getRoutePattern(r)
		method := r.Method
		status := strconv.Itoa(ww.Status())

		observability.HTTPRequestsTotal.WithLabelValues(route, method, status).Inc()
		observability.HTTPRequestDuration.WithLabelValues(route, method).Observe(duration)
	})
}

func getRoutePattern(r *http.Request) string {
	rctx := chi.RouteContext(r.Context())
	if rctx == nil {
		return r.URL.Path
	}
	pattern := rctx.RoutePattern()
	if pattern == "" {
		return r.URL.Path
	}
	return pattern
}
