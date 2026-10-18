package providers

import (
	"log/slog"

	"github.com/labstack/echo/v4"
	eMid "github.com/labstack/echo/v4/middleware"
)

// RegisterMiddlewares wraps every request. The request logger sits outside
// Recover, so a request that panicked is still logged, with its 500.
func RegisterMiddlewares(e *echo.Echo) {
	e.Use(requestLogger())
	e.Use(eMid.Recover())
}

func requestLogger() echo.MiddlewareFunc {
	return eMid.RequestLoggerWithConfig(eMid.RequestLoggerConfig{
		// Let the error handler write the response first, so the logged status
		// is the one the client got.
		HandleError: true,
		LogMethod:   true,
		LogURIPath:  true,
		LogStatus:   true,
		LogLatency:  true,
		LogValuesFunc: func(_ echo.Context, v eMid.RequestLoggerValues) error {
			slog.Info("request", "method", v.Method, "path", v.URIPath, "status", v.Status, "latency", v.Latency)
			return nil
		},
	})
}
