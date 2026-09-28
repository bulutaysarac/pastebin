package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"

	"github.com/bulutaysarac/pastebin-system-design/handlers"
	"github.com/bulutaysarac/pastebin-system-design/internal/mysqlstore"
	"github.com/bulutaysarac/pastebin-system-design/internal/paste"
	"github.com/bulutaysarac/pastebin-system-design/routes"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	if err := run(log); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	addr := env("PASTEBIN_ADDR", ":8080")
	baseURL := env("PASTEBIN_BASE_URL", "http://localhost:8080")
	dsn := os.Getenv("PASTEBIN_MYSQL_DSN")
	if dsn == "" {
		return errors.New("PASTEBIN_MYSQL_DSN is required")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	openCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	store, err := mysqlstore.Open(openCtx, dsn)
	cancel()
	if err != nil {
		return err
	}
	defer store.Close()

	e := echo.New()
	e.HideBanner = true
	e.HidePort = true
	e.Server.ReadHeaderTimeout = 5 * time.Second
	e.Renderer = handlers.NewRenderer()
	e.Use(middleware.Recover())

	routes.RegisterAPIRoutes(e, handlers.New(paste.NewService(store), baseURL, log))

	errCh := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", addr, "base_url", baseURL)
		errCh <- e.Start(addr)
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := e.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
