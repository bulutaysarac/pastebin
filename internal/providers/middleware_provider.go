package providers

import (
	"github.com/labstack/echo/v4"
	eMid "github.com/labstack/echo/v4/middleware"
)

func RegisterMiddlewares(e *echo.Echo) {
	e.Use(eMid.Recover())
}
