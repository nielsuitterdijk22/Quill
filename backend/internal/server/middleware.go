package server

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/go-chi/chi/v5/middleware"
)

// sentryCapture is a middleware that catches panics, reports them to Sentry,
// and re-panics so that chi's Recoverer middleware can write the 500 response.
// It is a no-op when no Sentry client is configured.
func sentryCapture(next http.Handler) http.Handler {
	// Skip installing the hook when Sentry has not been initialised.
	if sentry.CurrentHub().Client() == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				sentry.CurrentHub().WithScope(func(scope *sentry.Scope) {
					scope.SetRequest(r)
					sentry.CurrentHub().RecoverWithContext(r.Context(), err)
				})
				// Re-panic so chi's Recoverer can write the 500 response.
				panic(err)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// securityHeaders sets defensive HTTP response headers on every response. The
// API serves JSON only (no HTML), so a full Content-Security-Policy is omitted
// here — it belongs on the frontend. Clickjacking and sniffing headers apply
// regardless of content type.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		next.ServeHTTP(w, r)
	})
}

// requestLogger logs one structured line per request with status, size, and latency.
func requestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			start := time.Now()

			defer func() {
				logger.Info("http request",
					"method", r.Method,
					"path", r.URL.Path,
					"status", ww.Status(),
					"bytes", ww.BytesWritten(),
					"duration_ms", time.Since(start).Milliseconds(),
					"request_id", middleware.GetReqID(r.Context()),
					"remote", r.RemoteAddr,
				)
			}()

			next.ServeHTTP(ww, r)
		})
	}
}
