package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
)

// Pinger is a dependency the app cannot serve without. /health asks it.
type Pinger interface {
	Ping(ctx context.Context) error
}

// healthTimeout bounds the MySQL ping, so a hung database fails the check
// instead of hanging it.
const healthTimeout = time.Second

// Health answers 200 while MySQL responds within healthTimeout and 503
// otherwise. It is a readiness check: restarting the app would not fix a
// database that is down.
func (h *Handler) Health(c echo.Context) error {
	ctx, cancel := context.WithTimeout(c.Request().Context(), healthTimeout)
	defer cancel()

	c.Response().Header().Set(echo.HeaderCacheControl, "no-store")
	if err := h.db.Ping(ctx); err != nil {
		h.log.Warn("health check failed", "err", err)
		return c.String(http.StatusServiceUnavailable, "database unavailable")
	}
	return c.String(http.StatusOK, "ok")
}
