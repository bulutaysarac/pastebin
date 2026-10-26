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
	providers.RegisterMetrics(db)

	providers.RegisterRenderer(e)
	providers.RegisterMiddlewares(e)

	app := environments.GetApp()
	pastes := paste.NewService(db)
	routes.RegisterAPIRoutes(e, handlers.New(pastes, db, app.BaseURL, slog.Default()), app.CreatesPerMinute)

	providers.StartServer(e)
}
