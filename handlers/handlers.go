// Package handlers serves the web pages: the home page form, paste creation
// and paste viewing. Which URL maps to which handler lives in package routes.
package handlers

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/bulutaysarac/pastebin/internal/metrics"
	"github.com/bulutaysarac/pastebin/internal/paste"
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

// languageOption is one choice in the home page's "syntax" dropdown. Value is
// the highlight.js language name; highlighting happens in the reader's browser,
// so the server only stores and echoes this string.
type languageOption struct {
	Value string
	Label string
}

var languageOptions = []languageOption{
	{"plaintext", "Plain text"},
	{"sql", "SQL"},
	{"go", "Go"},
	{"json", "JSON"},
	{"yaml", "YAML"},
	{"javascript", "JavaScript"},
	{"typescript", "TypeScript"},
	{"python", "Python"},
	{"bash", "Bash"},
	{"html", "HTML"},
	{"css", "CSS"},
	{"java", "Java"},
	{"php", "PHP"},
	{"rust", "Rust"},
	{"markdown", "Markdown"},
	{"diff", "Diff"},
}

// maxBrowserCache caps the Cache-Control max-age sent with a paste.
const maxBrowserCache = 24 * time.Hour

// maxFormBytes bounds the POST /paste body before it is parsed. URL encoding
// turns a byte into up to three ("%E2"), so a paste at the size cap still
// fits, with room left for the other fields.
const maxFormBytes = 3*paste.MaxContentBytes + 4<<10

type Handler struct {
	pastes  *paste.Service
	db      Pinger
	baseURL string
	log     *slog.Logger
}

func New(pastes *paste.Service, db Pinger, baseURL string, log *slog.Logger) *Handler {
	for _, l := range languageOptions {
		for _, e := range expiryOptions {
			metrics.InitPasteCreated(l.Value, e.Value)
		}
	}
	return &Handler{pastes: pastes, db: db, baseURL: baseURL, log: log}
}

// Home renders the form for a new paste.
func (h *Handler) Home(c echo.Context) error {
	return c.Render(http.StatusOK, "index.html", map[string]any{
		"Expiries":  expiryOptions,
		"Languages": languageOptions,
		"MaxKiB":    paste.MaxContentBytes >> 10,
	})
}

// CreatePaste saves the submitted form and redirects to the new paste.
func (h *Handler) CreatePaste(c echo.Context) error {
	req := c.Request()
	// The underlying writer, not c.Response(): only it lets net/http close the
	// connection instead of reading the rest of an oversized body.
	req.Body = http.MaxBytesReader(c.Response().Writer, req.Body, maxFormBytes)

	form, err := c.FormParams()
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return c.String(http.StatusRequestEntityTooLarge, paste.ErrTooLarge.Error())
	}
	if err != nil {
		return c.String(http.StatusBadRequest, "could not read form")
	}

	expires := form.Get("expires")
	ttl, ok := lookupExpiry(expires)
	if !ok {
		return c.String(http.StatusBadRequest, "unknown expiry option")
	}

	language, ok := lookupLanguage(form.Get("language"))
	if !ok {
		return c.String(http.StatusBadRequest, "unknown language")
	}

	content := form.Get("content")
	p, err := h.pastes.Create(c.Request().Context(), content, language.Value, ttl)
	if errors.Is(err, paste.ErrEmpty) {
		return c.String(http.StatusBadRequest, err.Error())
	}
	if errors.Is(err, paste.ErrTooLarge) {
		return c.String(http.StatusRequestEntityTooLarge, err.Error())
	}
	if err != nil {
		return h.storeFailed(c, err, "could not save paste", "create paste")
	}
	metrics.ObservePasteCreated(language.Value, expires, len(content))

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
		return h.storeFailed(c, err, "could not load paste", "get paste", "id", id)
	}

	// private: only the reader's own browser may cache it, not shared proxies.
	maxAge := cacheLifetime(p, h.pastes.Now())
	c.Response().Header().Set(echo.HeaderCacheControl, "private, max-age="+strconv.Itoa(int(maxAge.Seconds())))

	language, _ := lookupLanguage(p.Language)

	return c.Render(http.StatusOK, "paste.html", map[string]any{
		"Paste":    p,
		"Language": language,
		"ShareURL": h.baseURL + "/" + p.ID,
	})
}

// statusClientClosedRequest is nginx's code for a client that left before the
// answer; net/http has none. It keeps those requests out of the 5xx count.
const statusClientClosedRequest = 499

// storeFailed answers a request whose store call returned err. msg is the
// text the client gets; op and attrs go to the log.
func (h *Handler) storeFailed(c echo.Context, err error, msg, op string, attrs ...any) error {
	ctx := c.Request().Context()
	attrs = append(attrs, "err", err)
	switch {
	case errors.Is(err, context.Canceled) && errors.Is(ctx.Err(), context.Canceled):
		// The client went away. Nothing is wrong on this side.
		h.log.Info(op+": client went away", attrs...)
		return c.NoContent(statusClientClosedRequest)
	case errors.Is(err, paste.ErrStoreTimeout),
		errors.Is(err, context.DeadlineExceeded) && errors.Is(ctx.Err(), context.DeadlineExceeded):
		// MySQL gave up on the statement, or the wait for a free connection
		// took the whole deadline.
		h.log.Warn(op+": timed out", attrs...)
		return c.String(http.StatusServiceUnavailable, msg+" in time, try again")
	default:
		h.log.Error(op, attrs...)
		return c.String(http.StatusInternalServerError, msg)
	}
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

// lookupLanguage treats a missing value as plain text, so clients that do not
// send a language (curl, older forms) keep working. Unknown values fail, and
// an unknown value read back from the store renders as plain text.
func lookupLanguage(value string) (languageOption, bool) {
	if value == "" {
		return languageOptions[0], true
	}
	for _, o := range languageOptions {
		if o.Value == value {
			return o, true
		}
	}
	return languageOptions[0], false
}

func lookupExpiry(value string) (time.Duration, bool) {
	for _, o := range expiryOptions {
		if o.Value == value {
			return o.TTL, true
		}
	}
	return 0, false
}
