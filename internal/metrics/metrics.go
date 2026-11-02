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

var expiredDeleted = promauto.NewCounter(prometheus.CounterOpts{
	Name: "pastebin_expired_pastes_deleted_total",
	Help: "Expired pastes the cleanup job deleted.",
})

var cleanupFailures = promauto.NewCounter(prometheus.CounterOpts{
	Name: "pastebin_cleanup_failures_total",
	Help: "Cleanup runs that stopped on an error.",
})

// ObserveCleanup records one cleanup run: the pastes it deleted, including
// those before an error, and whether it failed.
func ObserveCleanup(deleted int64, failed bool) {
	expiredDeleted.Add(float64(deleted))
	if failed {
		cleanupFailures.Inc()
	}
}

// What a call to the paste cache in Redis came to.
const (
	CacheHit   = "hit"
	CacheMiss  = "miss"
	CacheError = "error" // Redis failed or is being skipped; the database answered
)

var cacheRequests = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "pastebin_cache_requests_total",
	Help: "Calls to the paste cache in Redis, by result. Reads waiting on the same load count once.",
}, []string{"result"})

func init() {
	// Created at 0, for the same reason as InitPasteCreated.
	for _, result := range []string{CacheHit, CacheMiss, CacheError} {
		cacheRequests.WithLabelValues(result)
	}
}

// ObserveCacheRequest records one call to the paste cache.
func ObserveCacheRequest(result string) {
	cacheRequests.WithLabelValues(result).Inc()
}

var viewsFlushed = promauto.NewCounter(prometheus.CounterOpts{
	Name: "pastebin_views_flushed_total",
	Help: "Paste reads written to the database.",
})

var viewsFlushFailures = promauto.NewCounter(prometheus.CounterOpts{
	Name: "pastebin_views_flush_failures_total",
	Help: "Writes of read counts that stopped on an error. The counts are kept for the next one.",
})

var viewsDropped = promauto.NewCounter(prometheus.CounterOpts{
	Name: "pastebin_views_dropped_total",
	Help: "Paste reads not counted, because too many pastes were waiting to be written.",
})

// ObserveViewsFlush records one write of read counts: the reads it wrote,
// including those before an error, and whether it failed.
func ObserveViewsFlush(written int64, failed bool) {
	viewsFlushed.Add(float64(written))
	if failed {
		viewsFlushFailures.Inc()
	}
}

// ObserveViewsDropped records reads that could not be counted.
func ObserveViewsDropped(reads int64) {
	viewsDropped.Add(float64(reads))
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
