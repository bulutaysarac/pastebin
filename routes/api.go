// Package routes maps URLs to handlers.
package routes

import (
	"github.com/labstack/echo/v4"

	"github.com/bulutaysarac/pastebin/handlers"
	"github.com/bulutaysarac/pastebin/internal/metrics"
)

// RegisterAPIRoutes wires the handlers. createsPerMinute limits POST /paste
// per client IP; 0 turns the limit off.
func RegisterAPIRoutes(e *echo.Echo, h *handlers.Handler, createsPerMinute int) {
	e.GET("/", h.Home)                                                       // home page with the form
	e.POST("/paste", h.CreatePaste, handlers.LimitCreates(createsPerMinute)) // form submit → redirect to /:id
	e.GET("/:id", h.ShowPaste)                                               // read a paste

	e.GET("/health", h.Health)                             // 200 while MySQL answers, else 503
	e.GET("/metrics", echo.WrapHandler(metrics.Handler())) // Prometheus scrape target

	assets := e.Group("/assets", handlers.CacheAssets)
	assets.StaticFS("/", handlers.Assets()) // CSS, JS, vendored highlight.js
}
