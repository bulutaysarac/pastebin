package providers

import (
	"log/slog"

	"github.com/labstack/echo/v4"
	eMid "github.com/labstack/echo/v4/middleware"

	"github.com/bulutaysarac/pastebin/internal/metrics"
)

// RegisterMiddlewares wraps every request. The request logger, which also
// feeds the latency histogram, sits outside Recover, so a request that
// panicked is still logged and counted, with its 500.
func RegisterMiddlewares(e *echo.Echo) {
	e.Use(requestLogger())
	e.Use(eMid.Recover())
}

func requestLogger() echo.MiddlewareFunc {
	return eMid.RequestLoggerWithConfig(eMid.RequestLoggerConfig{
		// Let the error handler write the response first, so the logged status
		// is the one the client got.
		HandleError:  true,
		LogMethod:    true,
		LogURIPath:   true,
		LogRoutePath: true,
		LogStatus:    true,
		LogLatency:   true,
		LogValuesFunc: func(_ echo.Context, v eMid.RequestLoggerValues) error {
			slog.Info("request", "method", v.Method, "path", v.URIPath, "status", v.Status, "latency", v.Latency)
			metrics.ObserveRequest(v.Method, v.RoutePath, v.Status, v.Latency)
			return nil
		},
	})
}
