package logging

import (
	"log/slog"
	"net/http"
	"os"
	"time"
	"wasmcat/internal/shared"
)

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func Configure(component string) *slog.Logger {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil)).With("component", component)
	slog.SetDefault(logger)
	return logger
}

func Middleware(component string, next http.Handler) http.Handler {
	return MiddlewareWithMetrics(component, next, nil)
}

func MiddlewareWithMetrics(component string, next http.Handler, metrics *shared.Metrics) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{
			ResponseWriter: w,
			status:         http.StatusOK,
		}

		next.ServeHTTP(rec, r)
		metrics.ObserveRequest(r.URL.Path, rec.status)

		slog.Info("http request",
			"component", component,
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	})
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}
