// Package handlers serves the web pages: the home page form, paste creation
// and paste viewing. Which URL maps to which handler lives in package routes.
package handlers

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/bulutaysarac/pastebin-system-design/internal/paste"
)

// expiryOption is one choice in the home page's "expires" dropdown.
type expiryOption struct {
	Value string
	Label string
	TTL   time.Duration // 0 = never
}

var expiryOptions = []expiryOption{
	{"never", "Never", 0},
	{"10m", "10 minutes", 10 * time.Minute},
	{"1h", "1 hour", time.Hour},
	{"1d", "1 day", 24 * time.Hour},
	{"1w", "1 week", 7 * 24 * time.Hour},
}

// maxBrowserCache caps the Cache-Control max-age sent with a paste.
const maxBrowserCache = 24 * time.Hour

type Handler struct {
	pastes  *paste.Service
	baseURL string
	log     *slog.Logger
}

func New(pastes *paste.Service, baseURL string, log *slog.Logger) *Handler {
	return &Handler{pastes: pastes, baseURL: baseURL, log: log}
}

// Home renders the form for a new paste.
func (h *Handler) Home(c echo.Context) error {
	return c.Render(http.StatusOK, "index.html", map[string]any{"Expiries": expiryOptions})
}

// CreatePaste saves the submitted form and redirects to the new paste.
func (h *Handler) CreatePaste(c echo.Context) error {
	form, err := c.FormParams()
	if err != nil {
		return c.String(http.StatusBadRequest, "could not read form")
	}

	ttl, ok := lookupExpiry(form.Get("expires"))
	if !ok {
		return c.String(http.StatusBadRequest, "unknown expiry option")
	}

	p, err := h.pastes.Create(c.Request().Context(), form.Get("content"), ttl)
	if err != nil {
		h.log.Error("create paste", "err", err)
		return c.String(http.StatusInternalServerError, "could not save paste")
	}

	// Post/Redirect/Get: refreshing the next page re-reads the paste instead
	// of submitting the form a second time.
	return c.Redirect(http.StatusSeeOther, "/"+p.ID)
}

// ShowPaste renders the paste named by the :id path parameter.
func (h *Handler) ShowPaste(c echo.Context) error {
	id := c.Param("id")

	p, err := h.pastes.Get(c.Request().Context(), id)
	if errors.Is(err, paste.ErrNotFound) {
		c.Response().Header().Set(echo.HeaderCacheControl, "no-store")
		return c.Render(http.StatusNotFound, "notfound.html", nil)
	}
	if err != nil {
		h.log.Error("get paste", "id", id, "err", err)
		return c.String(http.StatusInternalServerError, "could not load paste")
	}

	// private: only the reader's own browser may cache it, not shared proxies.
	maxAge := cacheLifetime(p, h.pastes.Now())
	c.Response().Header().Set(echo.HeaderCacheControl, "private, max-age="+strconv.Itoa(int(maxAge.Seconds())))

	return c.Render(http.StatusOK, "paste.html", map[string]any{
		"Paste":    p,
		"ShareURL": h.baseURL + "/" + p.ID,
	})
}

// cacheLifetime is how long the browser may reuse a paste without asking the
// server. It never runs past the paste's expiry, so an expired paste is not
// served from the browser cache.
func cacheLifetime(p paste.Paste, now time.Time) time.Duration {
	ttl := maxBrowserCache
	if p.ExpiresAt != nil {
		if left := p.ExpiresAt.Sub(now); left < ttl {
			ttl = left
		}
	}
	return max(ttl, 0)
}

func lookupExpiry(value string) (time.Duration, bool) {
	for _, o := range expiryOptions {
		if o.Value == value {
			return o.TTL, true
		}
	}
	return 0, false
}
