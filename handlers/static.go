package handlers

import (
	"embed"
	"io/fs"

	"github.com/labstack/echo/v4"
)

//go:embed static
var staticFS embed.FS

// Assets holds the CSS, JS and the vendored highlight.js served under /assets.
func Assets() fs.FS {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic(err) // the embed directive guarantees "static" exists
	}
	return sub
}

// CacheAssets lets browsers reuse static files for an hour. Embedded files
// carry no modification time, so without this every page load would download
// them again.
func CacheAssets(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		c.Response().Header().Set(echo.HeaderCacheControl, "public, max-age=3600")
		return next(c)
	}
}
