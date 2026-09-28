package providers

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

	"github.com/bulutaysarac/pastebin-system-design/environments"
)

// StartServer serves HTTP until SIGINT or SIGTERM arrives, then gives in-flight
// requests up to 10 seconds to finish.
func StartServer(e *echo.Echo) {
	e.HideBanner = true
	e.HidePort = true
	e.Server.ReadHeaderTimeout = 5 * time.Second

	app := environments.GetApp()

	go func() {
		slog.Info("server starting", "port", app.Port, "base_url", app.BaseURL)

		if err := e.Start(":" + app.Port); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server stopped unexpectedly", "err", err)
			os.Exit(1)
		}
	}()

	// Wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	<-sigCh

	slog.Info("shutdown signal received")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := e.Shutdown(ctx); err != nil {
		slog.Error("graceful shutdown failed", "err", err)
		return
	}

	slog.Info("server stopped")
}
