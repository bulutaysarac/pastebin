package providers

import (
	"github.com/bulutaysarac/pastebin/internal/metrics"
	"github.com/bulutaysarac/pastebin/internal/mysqlstore"
)

// RegisterMetrics adds the DB pool stats to /metrics, next to the request
// latencies and the Go runtime metrics that are always there.
func RegisterMetrics(db *mysqlstore.Store) {
	metrics.RegisterDBStats(db.DB())
}
