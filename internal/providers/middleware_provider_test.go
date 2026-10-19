package providers

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"

	"github.com/bulutaysarac/pastebin/internal/metrics"
)

// newTestEcho builds a server with the real middlewares and three routes under
// prefix: one that succeeds, one that fails and one that panics. Metrics live
// in a process-wide registry, so each test uses its own prefix.
func newTestEcho(t *testing.T, prefix string) *echo.Echo {
	t.Helper()
	e := echo.New()
	e.Logger.SetOutput(io.Discard) // Recover prints the panic's stack trace here
	RegisterMiddlewares(e)
	e.GET(prefix+"/ok", func(c echo.Context) error { return c.String(http.StatusOK, "ok") })
	e.GET(prefix+"/fail", func(echo.Context) error { return echo.NewHTTPError(http.StatusTeapot) })
	e.GET(prefix+"/panic", func(echo.Context) error { panic("boom") })
	return e
}

func serve(e *echo.Echo, path string) {
	e.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil))
}

func TestEveryRequestIsLoggedWithTheStatusTheClientGot(t *testing.T) {
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	e := newTestEcho(t, "/test")
	for _, path := range []string{"/test/ok", "/test/fail", "/test/panic", "/test/missing/route"} {
		serve(e, path)
	}

	for _, want := range []string{
		"method=GET path=/test/ok status=200 latency=",
		"path=/test/fail status=418",
		"path=/test/panic status=500",
		"path=/test/missing/route status=404",
	} {
		if !strings.Contains(logs.String(), want) {
			t.Errorf("logs are missing %q\n%s", want, logs.String())
		}
	}
}

func TestEveryRequestFeedsTheLatencyHistogram(t *testing.T) {
	e := newTestEcho(t, "/hist")
	for _, path := range []string{"/hist/ok", "/hist/ok", "/hist/panic", "/hist/missing/route"} {
		serve(e, path)
	}

	rec := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	for _, want := range []string{
		`pastebin_http_request_duration_seconds_count{method="GET",route="/hist/ok",status="200"} 2`,
		`pastebin_http_request_duration_seconds_count{method="GET",route="/hist/panic",status="500"} 1`,
		// Unmatched paths share one series, whichever test sent them.
		`pastebin_http_request_duration_seconds_count{method="GET",route="unmatched",status="404"} `,
	} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("/metrics is missing %s\n%s", want, grepLines(rec.Body.String(), "pastebin_http_request_duration_seconds_count"))
		}
	}
}

func grepLines(s, substr string) string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if strings.Contains(line, substr) {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}
