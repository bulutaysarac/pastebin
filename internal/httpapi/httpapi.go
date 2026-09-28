// Package httpapi serves the web pages: the home page form, paste creation
// and paste viewing.
package httpapi

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

type Server struct {
	pastes  *paste.Service
	baseURL string
	log     *slog.Logger
}

func New(pastes *paste.Service, baseURL string, log *slog.Logger) http.Handler {
	s := &Server{pastes: pastes, baseURL: baseURL, log: log}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.home)
	mux.HandleFunc("POST /paste", s.createPaste)
	mux.HandleFunc("GET /{id}", s.showPaste)
	return mux
}

func (s *Server) home(w http.ResponseWriter, r *http.Request) {
	s.render(w, http.StatusOK, "index.html", map[string]any{"Expiries": expiryOptions})
}

func (s *Server) createPaste(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "could not read form", http.StatusBadRequest)
		return
	}

	ttl, ok := lookupExpiry(r.PostForm.Get("expires"))
	if !ok {
		http.Error(w, "unknown expiry option", http.StatusBadRequest)
		return
	}

	p, err := s.pastes.Create(r.Context(), r.PostForm.Get("content"), ttl)
	if err != nil {
		s.log.Error("create paste", "err", err)
		http.Error(w, "could not save paste", http.StatusInternalServerError)
		return
	}

	// Post/Redirect/Get: refreshing the next page re-reads the paste instead
	// of submitting the form a second time.
	http.Redirect(w, r, "/"+p.ID, http.StatusSeeOther)
}

func (s *Server) showPaste(w http.ResponseWriter, r *http.Request) {
	p, err := s.pastes.Get(r.Context(), r.PathValue("id"))
	if errors.Is(err, paste.ErrNotFound) {
		w.Header().Set("Cache-Control", "no-store")
		s.render(w, http.StatusNotFound, "notfound.html", nil)
		return
	}
	if err != nil {
		s.log.Error("get paste", "id", r.PathValue("id"), "err", err)
		http.Error(w, "could not load paste", http.StatusInternalServerError)
		return
	}

	// private: only the reader's own browser may cache it, not shared proxies.
	maxAge := cacheLifetime(p, s.pastes.Now())
	w.Header().Set("Cache-Control", "private, max-age="+strconv.Itoa(int(maxAge.Seconds())))

	s.render(w, http.StatusOK, "paste.html", map[string]any{
		"Paste":    p,
		"ShareURL": s.baseURL + "/" + p.ID,
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

func (s *Server) render(w http.ResponseWriter, status int, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := templates.ExecuteTemplate(w, name, data); err != nil {
		s.log.Error("render template", "template", name, "err", err)
	}
}
