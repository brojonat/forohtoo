package metrics

import (
	"net/http"
	"time"
)

// HTTPMetricsMiddleware creates middleware that records HTTP request metrics.
// The handlerName parameter should be a constant identifier for the endpoint (e.g., "/api/v1/wallets").
// Following the project's pattern, this returns a function that wraps an http.Handler.
func HTTPMetricsMiddleware(m *Metrics, handlerName string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			// Wrap ResponseWriter to capture status code
			wrapped := &responseWriter{
				ResponseWriter: w,
				statusCode:     200, // Default status code
			}

			// Call the next handler
			next.ServeHTTP(wrapped, r)

			// Record metrics
			duration := time.Since(start).Seconds()
			if m != nil {
				m.RecordHTTPRequest(handlerName, r.Method, wrapped.statusCode, duration)
			}
		})
	}
}

// responseWriter wraps http.ResponseWriter to capture the status code.
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

// WriteHeader captures the status code and calls the underlying WriteHeader.
func (w *responseWriter) WriteHeader(statusCode int) {
	w.statusCode = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

// Timer is a helper for timing operations.
// Usage:
//
//	defer Timer(start, func(duration float64) {
//	    metrics.RecordSomething(duration)
//	})()
//
// Or simpler pattern:
//
//	start := time.Now()
//	defer func() {
//	    metrics.RecordSomething(time.Since(start).Seconds())
//	}()
func Timer(start time.Time, recordFunc func(float64)) func() {
	return func() {
		recordFunc(time.Since(start).Seconds())
	}
}
