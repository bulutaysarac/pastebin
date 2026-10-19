// Package metrics defines what the app reports at /metrics in the Prometheus
// text format: request latencies, DB pool stats and, through the default
// registry, Go runtime and process metrics.
package metrics

import (
	"database/sql"
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var requestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
	Name: "pastebin_http_request_duration_seconds",
	Help: "Time spent serving HTTP requests, by method, route and status.",
	// Finer than the default buckets at the low end, where most reads land.
	Buckets: []float64{.0005, .001, .0025, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10},
}, []string{"method", "route", "status"})

// ObserveRequest records one served request. route must be the route template
// (e.g. "/:id"), not the raw path, or every paste id becomes its own series.
func ObserveRequest(method, route string, status int, latency time.Duration) {
	if route == "" {
		route = "unmatched"
	}
	requestDuration.WithLabelValues(knownMethod(method), route, strconv.Itoa(status)).Observe(latency.Seconds())
}

// knownMethod folds anything but the standard methods into "OTHER": clients
// choose the method string, so it must not be able to create new series.
func knownMethod(m string) string {
	switch m {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch,
		http.MethodDelete, http.MethodOptions, http.MethodConnect, http.MethodTrace:
		return m
	}
	return "OTHER"
}

// RegisterDBStats reports the connection pool's state: open, in use and idle
// connections, how often requests waited for one, and how many were closed.
func RegisterDBStats(db *sql.DB) {
	prometheus.MustRegister(collectors.NewDBStatsCollector(db, "pastebin"))
}

func Handler() http.Handler { return promhttp.Handler() }
