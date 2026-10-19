package metrics

import (
	"database/sql"
	"database/sql/driver"
	"io"
	"net/http/httptest"
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

// nopDriver lets the test build a *sql.DB without a database behind it; the
// stats collector only reads db.Stats().
type nopDriver struct{}

func (nopDriver) Open(string) (driver.Conn, error) { return nil, driver.ErrSkip }

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
