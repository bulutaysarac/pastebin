package main

import (
	"log/slog"

	"github.com/labstack/echo/v4"

	"github.com/bulutaysarac/pastebin/environments"
	"github.com/bulutaysarac/pastebin/handlers"
	"github.com/bulutaysarac/pastebin/internal/paste"
	"github.com/bulutaysarac/pastebin/internal/providers"
	"github.com/bulutaysarac/pastebin/routes"
)

func main() {
	e := echo.New()

	db := providers.ConnectDatabase()
	defer db.Close()

	providers.RegisterRenderer(e)
	providers.RegisterMiddlewares(e)

	pastes := paste.NewService(db)
	routes.RegisterAPIRoutes(e, handlers.New(pastes, environments.GetApp().BaseURL, slog.Default()))

	providers.StartServer(e)
}
