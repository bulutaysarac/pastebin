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

	srv := &http.Server{
		Addr:              addr,
		Handler:           routes.New(handlers.New(paste.NewService(store), baseURL, log)),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", addr, "base_url", baseURL)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
