package metrics

import (
	"database/sql"
	"database/sql/driver"
	"io"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func scrape(t *testing.T) string {
	t.Helper()
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	body, _ := io.ReadAll(rec.Body)
	return string(body)
}

func TestObserveRequestLabelsByRouteTemplate(t *testing.T) {
	ObserveRequest("GET", "/metrics-test/:id", 200, 3*time.Millisecond)
	ObserveRequest("GET", "/metrics-test/:id", 200, 4*time.Millisecond)
	ObserveRequest("BREW", "/metrics-test/:id", 405, time.Millisecond)
	ObserveRequest("GET", "", 404, time.Millisecond)

	out := scrape(t)
	for _, want := range []string{
		`pastebin_http_request_duration_seconds_count{method="GET",route="/metrics-test/:id",status="200"} 2`,
		`pastebin_http_request_duration_seconds_count{method="OTHER",route="/metrics-test/:id",status="405"} 1`,
		`pastebin_http_request_duration_seconds_count{method="GET",route="unmatched",status="404"} 1`,
		`go_goroutines `,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("/metrics is missing %s", want)
		}
	}
	if strings.Contains(out, `method="BREW"`) {
		t.Error("a client-chosen method became its own label value")
	}
}

func TestObservePasteCreatedCountsByOptionAndSize(t *testing.T) {
	ObservePasteCreated("metrics-test-lang", "1h", 100)
	ObservePasteCreated("metrics-test-lang", "1h", 1000)

	out := scrape(t)
	for _, want := range []string{
		`pastebin_pastes_created_total{expires="1h",language="metrics-test-lang"} 2`,
		`pastebin_paste_size_bytes_bucket{le="256"} 1`,
		`pastebin_paste_size_bytes_bucket{le="1024"} 2`,
		`pastebin_paste_size_bytes_bucket{le="1.6777216e+07"} 2`,
		`pastebin_paste_size_bytes_sum 1100`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("/metrics is missing %s", want)
		}
	}
}

func TestInitPasteCreatedStartsTheSeriesAtZero(t *testing.T) {
	InitPasteCreated("metrics-test-init", "1w")
	if want := `pastebin_pastes_created_total{expires="1w",language="metrics-test-init"} 0`; !strings.Contains(scrape(t), want) {
		t.Errorf("/metrics is missing %s", want)
	}
}

func TestObserveCleanupCountsDeletedPastesAndFailedRuns(t *testing.T) {
	ObserveCleanup(1500, false)
	ObserveCleanup(0, false)
	ObserveCleanup(200, true)

	out := scrape(t)
	for _, want := range []string{
		`pastebin_expired_pastes_deleted_total 1700`,
		`pastebin_cleanup_failures_total 1`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("/metrics is missing %s", want)
		}
	}
}

// nopDriver lets the test build a *sql.DB without a database behind it; the
// stats collector only reads db.Stats().
type nopDriver struct{}

func (nopDriver) Open(string) (driver.Conn, error) { return nil, driver.ErrSkip }

func TestCacheResultsStartAtZeroAndCount(t *testing.T) {
	body := scrape(t)
	for _, result := range []string{CacheHit, CacheMiss, CacheError} {
		if !strings.Contains(body, `pastebin_cache_requests_total{result="`+result+`"} `) {
			t.Errorf("/metrics has no series for result %q", result)
		}
	}

	before := cacheRequestsFor(t, CacheHit)
	ObserveCacheRequest(CacheHit)
	if after := cacheRequestsFor(t, CacheHit); after != before+1 {
		t.Fatalf("hit counter went from %v to %v", before, after)
	}
}

func cacheRequestsFor(t *testing.T, result string) float64 {
	t.Helper()
	prefix := `pastebin_cache_requests_total{result="` + result + `"} `
	for _, line := range strings.Split(scrape(t), "\n") {
		if value, ok := strings.CutPrefix(line, prefix); ok {
			n, err := strconv.ParseFloat(value, 64)
			if err != nil {
				t.Fatal(err)
			}
			return n
		}
	}
	t.Fatalf("/metrics has no %s", prefix)
	return 0
}

func TestRegisterDBStatsExposesPoolGauges(t *testing.T) {
	sql.Register("metrics-test-nop", nopDriver{})
	db, err := sql.Open("metrics-test-nop", "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	db.SetMaxOpenConns(7)

	RegisterDBStats(db)

	out := scrape(t)
	for _, want := range []string{
		`go_sql_max_open_connections{db_name="pastebin"} 7`,
		`go_sql_open_connections{db_name="pastebin"} 0`,
		`go_sql_wait_count_total{db_name="pastebin"} 0`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("/metrics is missing %s", want)
		}
	}
}
