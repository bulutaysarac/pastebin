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

var pastesCreated = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "pastebin_pastes_created_total",
	Help: "Pastes stored, by language and expiry option.",
}, []string{"language", "expires"})

var pasteSize = promauto.NewHistogram(prometheus.HistogramOpts{
	Name: "pastebin_paste_size_bytes",
	Help: "Content size of stored pastes.",
	// 256 B up to 16 MiB, the most a MEDIUMTEXT column holds.
	Buckets: prometheus.ExponentialBuckets(256, 4, 9),
})

// InitPasteCreated creates the series for one language and expiry pair at 0.
// Prometheus' increase() can't see the step from "no series" to 1, so without
// this the first paste of each pair never shows up on the dashboards.
func InitPasteCreated(language, expires string) {
	pastesCreated.WithLabelValues(language, expires)
}

// ObservePasteCreated records one stored paste. language and expires must be
// values from the form's fixed option lists, so they can't create new series.
func ObservePasteCreated(language, expires string, sizeBytes int) {
	pastesCreated.WithLabelValues(language, expires).Inc()
	pasteSize.Observe(float64(sizeBytes))
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
