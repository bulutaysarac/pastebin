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
)

// newTestEcho builds a server with the real middlewares and three routes:
// one that succeeds, one that fails and one that panics.
func newTestEcho(t *testing.T) *echo.Echo {
	t.Helper()
	e := echo.New()
	e.Logger.SetOutput(io.Discard) // Recover prints the panic's stack trace here
	RegisterMiddlewares(e)
	e.GET("/test/ok", func(c echo.Context) error { return c.String(http.StatusOK, "ok") })
	e.GET("/test/fail", func(echo.Context) error { return echo.NewHTTPError(http.StatusTeapot) })
	e.GET("/test/panic", func(echo.Context) error { panic("boom") })
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

	e := newTestEcho(t)
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
