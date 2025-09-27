//go:build !prom

package metrics

import "net/http"

// PrometheusHandler returns a no-op handler when Prometheus support is not compiled in
func PrometheusHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("Prometheus metrics not available - build with -tags=prom to enable"))
	})
}