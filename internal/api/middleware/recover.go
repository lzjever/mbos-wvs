package middleware

import (
	"encoding/json"
	"net/http"
	"runtime/debug"

	"go.uber.org/zap"
)

// Recoverer recovers from panics and logs the error.
func Recoverer(log *zap.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rvr := recover(); rvr != nil {
					log.Error("panic recovered",
						zap.Any("panic", rvr),
						zap.String("stack", string(debug.Stack())),
						zap.String("request_id", GetRequestID(r)),
					)
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusInternalServerError)
					json.NewEncoder(w).Encode(map[string]string{
						"code":    "WVS_INTERNAL",
						"message": "internal server error",
					})
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}
