// Package handlers serves the web pages: the home page form, paste creation
// and paste viewing. Which URL maps to which handler lives in package routes.
package handlers

import (
	"embed"
	"errors"
	"html/template"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/bulutaysarac/pastebin-system-design/internal/paste"
)

//go:embed templates/*.html
var templateFS embed.FS

var templates = template.Must(template.ParseFS(templateFS, "templates/*.html"))

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
func (h *Handler) Home(w http.ResponseWriter, r *http.Request) {
	h.render(w, http.StatusOK, "index.html", map[string]any{"Expiries": expiryOptions})
}

// CreatePaste saves the submitted form and redirects to the new paste.
func (h *Handler) CreatePaste(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "could not read form", http.StatusBadRequest)
		return
	}

	ttl, ok := lookupExpiry(r.PostForm.Get("expires"))
	if !ok {
		http.Error(w, "unknown expiry option", http.StatusBadRequest)
		return
	}

	p, err := h.pastes.Create(r.Context(), r.PostForm.Get("content"), ttl)
	if err != nil {
		h.log.Error("create paste", "err", err)
		http.Error(w, "could not save paste", http.StatusInternalServerError)
		return
	}

	// Post/Redirect/Get: refreshing the next page re-reads the paste instead
	// of submitting the form a second time.
	http.Redirect(w, r, "/"+p.ID, http.StatusSeeOther)
}

// ShowPaste renders the paste named by the {id} path parameter.
func (h *Handler) ShowPaste(w http.ResponseWriter, r *http.Request) {
	p, err := h.pastes.Get(r.Context(), r.PathValue("id"))
	if errors.Is(err, paste.ErrNotFound) {
		w.Header().Set("Cache-Control", "no-store")
		h.render(w, http.StatusNotFound, "notfound.html", nil)
		return
	}
	if err != nil {
		h.log.Error("get paste", "id", r.PathValue("id"), "err", err)
		http.Error(w, "could not load paste", http.StatusInternalServerError)
		return
	}

	// private: only the reader's own browser may cache it, not shared proxies.
	maxAge := cacheLifetime(p, h.pastes.Now())
	w.Header().Set("Cache-Control", "private, max-age="+strconv.Itoa(int(maxAge.Seconds())))

	h.render(w, http.StatusOK, "paste.html", map[string]any{
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

func (h *Handler) render(w http.ResponseWriter, status int, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := templates.ExecuteTemplate(w, name, data); err != nil {
		h.log.Error("render template", "template", name, "err", err)
	}
}
