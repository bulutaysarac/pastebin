// Package routes maps URLs to handlers.
package routes

import (
	"github.com/labstack/echo/v4"

	"github.com/bulutaysarac/pastebin/handlers"
	"github.com/bulutaysarac/pastebin/internal/metrics"
)

// RegisterAPIRoutes wires the handlers. limitCreates guards POST /paste.
func RegisterAPIRoutes(e *echo.Echo, h *handlers.Handler, limitCreates echo.MiddlewareFunc) {
	e.GET("/", h.Home)                            // home page with the form
	e.POST("/paste", h.CreatePaste, limitCreates) // form submit → redirect to /:id
	e.GET("/:id", h.ShowPaste)                    // read a paste
	e.GET("/:id/stats", h.PasteStats)             // how many times it was read

	e.GET("/health", h.Health)                             // 200 while MySQL answers, else 503
	e.GET("/metrics", echo.WrapHandler(metrics.Handler())) // Prometheus scrape target

	assets := e.Group("/assets", handlers.CacheAssets)
	assets.StaticFS("/", handlers.Assets()) // CSS, JS, vendored highlight.js
}
