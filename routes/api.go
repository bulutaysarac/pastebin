// Package routes maps URLs to handlers.
package routes

import (
	"github.com/labstack/echo/v4"

	"github.com/bulutaysarac/pastebin/handlers"
)

func RegisterAPIRoutes(e *echo.Echo, h *handlers.Handler) {
	e.GET("/", h.Home)              // home page with the form
	e.POST("/paste", h.CreatePaste) // form submit → redirect to /:id
	e.GET("/:id", h.ShowPaste)      // read a paste

	assets := e.Group("/assets", handlers.CacheAssets)
	assets.StaticFS("/", handlers.Assets()) // CSS, JS, vendored highlight.js
}
