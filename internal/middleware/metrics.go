package middleware

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Prometheus metrics for HTTP traffic. Names follow the promhttp convention.
var (
	httpRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "pulsar",
		Name:      "http_requests_total",
		Help:      "Total number of HTTP requests processed.",
	}, []string{"method", "path", "status"})

	httpRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "pulsar",
		Name:      "http_request_duration_seconds",
		Help:      "Latency of HTTP requests in seconds.",
		Buckets:   prometheus.DefBuckets,
	}, []string{"method", "path"})

	httpResponseSize = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "pulsar",
		Name:      "http_response_size_bytes",
		Help:      "Size of HTTP responses in bytes.",
		Buckets:   prometheus.ExponentialBuckets(64, 4, 10),
	}, []string{"method", "path"})

	httpInFlight = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "pulsar",
		Name:      "http_in_flight",
		Help:      "Number of HTTP requests currently being served.",
	})
)

// Metrics instruments every request with counters and histograms. It relies
// on chi's WrapResponseWriter to capture status and bytes written.
func Metrics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		httpInFlight.Inc()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		httpInFlight.Dec()

		path := normalizePath(r)
		method := r.Method
		status := strconv.Itoa(ww.Status())
		httpRequestsTotal.WithLabelValues(method, path, status).Inc()
		httpRequestDuration.WithLabelValues(method, path).
			Observe(time.Since(start).Seconds())
		httpResponseSize.WithLabelValues(method, path).
			Observe(float64(ww.BytesWritten()))
	})
}

// normalizePath strips IDs and query strings so label cardinality stays bounded.
// /app/buckets/550e8400-... becomes /app/buckets/:id.
func normalizePath(r *http.Request) string {
	p := r.URL.Path
	// Collapse UUID-like segments.
	out := make([]byte, 0, len(p))
	seg := []byte{}
	flush := func() {
		if isUUIDish(seg) {
			out = append(out, ":id"...)
		} else {
			out = append(out, seg...)
		}
		seg = seg[:0]
	}
	for i := 0; i < len(p); i++ {
		c := p[i]
		if c == '/' {
			flush()
			out = append(out, '/')
		} else {
			seg = append(seg, c)
		}
	}
	flush()
	return string(out)
}

// isUUIDish returns true for hex-or-dash runs >= 20 chars (UUID-ish).
func isUUIDish(b []byte) bool {
	if len(b) < 20 {
		return false
	}
	for _, c := range b {
		switch {
		case c >= '0' && c <= '9':
		case c >= 'a' && c <= 'f':
		case c >= 'A' && c <= 'F':
		case c == '-':
		default:
			return false
		}
	}
	return true
}
