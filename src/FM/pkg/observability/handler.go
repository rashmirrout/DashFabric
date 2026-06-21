package observability

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// MetricsHandler returns an HTTP handler that serves Prometheus metrics
func MetricsHandler() http.HandlerFunc {
	return promhttp.Handler().ServeHTTP
}
