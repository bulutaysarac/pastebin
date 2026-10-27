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

	"github.com/bulutaysarac/pastebin/environments"
)

// StartServer serves HTTP until SIGINT or SIGTERM arrives, then gives in-flight
// requests up to 10 seconds to finish.
func StartServer(e *echo.Echo) {
	e.HideBanner = true
	e.HidePort = true
	e.Server.ReadHeaderTimeout = 5 * time.Second
	// The whole request, body included: the largest accepted body (about
	// 1.5 MiB) has to arrive at 150 KB/s or faster.
	e.Server.ReadTimeout = 10 * time.Second
	// From the end of the headers to the last byte of the response: the
	// handler's work plus sending a 512 KiB paste to a slow reader.
	e.Server.WriteTimeout = 15 * time.Second
	// Set on its own: at 0 it would fall back to ReadTimeout.
	e.Server.IdleTimeout = 60 * time.Second

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
