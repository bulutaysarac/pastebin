package main

import (
	"log/slog"

	"github.com/labstack/echo/v4"

	"github.com/bulutaysarac/pastebin/environments"
	"github.com/bulutaysarac/pastebin/handlers"
	"github.com/bulutaysarac/pastebin/internal/cache"
	"github.com/bulutaysarac/pastebin/internal/paste"
	"github.com/bulutaysarac/pastebin/internal/providers"
	"github.com/bulutaysarac/pastebin/routes"
)

func main() {
	e := echo.New()

	db := providers.ConnectDatabase()
	defer db.Close()
	rdb := providers.ConnectRedis()
	defer rdb.Close()
	providers.RegisterMetrics(db)
	stopCleanup := providers.StartCleanup(db)
	defer stopCleanup() // runs before db.Close
	counts, stopViews := providers.StartViews(db)
	defer stopViews() // runs after the server has stopped, so no read is left out

	providers.RegisterRenderer(e)
	providers.RegisterMiddlewares(e)

	app := environments.GetApp()
	pastes := paste.NewService(cache.New(db, rdb, slog.Default()))
	routes.RegisterAPIRoutes(e, handlers.New(pastes, db, counts, app.BaseURL, slog.Default()), app.CreatesPerMinute)

	providers.StartServer(e)
}
