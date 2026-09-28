package providers

import (
	"github.com/labstack/echo/v4"

	"github.com/bulutaysarac/pastebin/handlers"
)

// RegisterRenderer lets handlers render the HTML templates with c.Render.
func RegisterRenderer(e *echo.Echo) {
	e.Renderer = handlers.NewRenderer()
}
