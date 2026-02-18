package middleware

import (
	"context"
	"net/http"

	"github.com/google/uuid"
)

const RequestIDHeader = "X-Request-ID"

type ctxKeyRequestID struct{}

// RequestID injects a request ID into the context.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := r.Header.Get(RequestIDHeader)
		if requestID == "" {
			requestID = uuid.New().String()
		}
		ctx := context.WithValue(r.Context(), ctxKeyRequestID{}, requestID)
		w.Header().Set(RequestIDHeader, requestID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func GetRequestID(r *http.Request) string {
	if id, ok := r.Context().Value(ctxKeyRequestID{}).(string); ok {
		return id
	}
	return r.Header.Get(RequestIDHeader)
}
