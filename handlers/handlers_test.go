// External test package: it drives the handlers through the real router,
// and package routes already imports package handlers.
package handlers_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bulutaysarac/pastebin-system-design/handlers"
	"github.com/bulutaysarac/pastebin-system-design/internal/paste"
	"github.com/bulutaysarac/pastebin-system-design/routes"
)

type memStore struct {
	mu     sync.Mutex
	pastes map[string]paste.Paste
}

func (m *memStore) Create(_ context.Context, p paste.Paste) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.pastes[p.ID]; ok {
		return paste.ErrIDTaken
	}
	m.pastes[p.ID] = p
	return nil
}

func (m *memStore) Get(_ context.Context, id string) (paste.Paste, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.pastes[id]
	if !ok {
		return paste.Paste{}, paste.ErrNotFound
	}
	return p, nil
}

func newTestServer(t *testing.T) (*httptest.Server, *memStore) {
	t.Helper()
	store := &memStore{pastes: map[string]paste.Paste{}}
	h := handlers.New(paste.NewService(store), "http://paste.test", slog.New(slog.NewTextHandler(io.Discard, nil)))
	srv := httptest.NewServer(routes.New(h))
	t.Cleanup(srv.Close)
	return srv, store
}

// noRedirect makes the client return 3xx responses instead of following them.
var noRedirect = &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

func postPaste(t *testing.T, srv *httptest.Server, content, expires string) *http.Response {
	t.Helper()
	resp, err := noRedirect.PostForm(srv.URL+"/paste", url.Values{"content": {content}, "expires": {expires}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func get(t *testing.T, url string) (*http.Response, string) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp, string(body)
}

func TestHomePageShowsForm(t *testing.T) {
	srv, _ := newTestServer(t)
	resp, body := get(t, srv.URL+"/")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	for _, want := range []string{`action="/paste"`, `name="content"`, `name="expires"`, `value="1h"`} {
		if !strings.Contains(body, want) {
			t.Errorf("home page is missing %s", want)
		}
	}
}

func TestCreateThenRead(t *testing.T) {
	srv, _ := newTestServer(t)

	resp := postPaste(t, srv, "hello <script>alert(1)</script>", "never")
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("POST status = %d, want 303", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if len(loc) != 9 || loc[0] != '/' {
		t.Fatalf("Location = %q, want /<8-char id>", loc)
	}

	resp, body := get(t, srv.URL+loc)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("Cache-Control"); got != "private, max-age=86400" {
		t.Errorf("Cache-Control = %q, want private, max-age=86400", got)
	}
	if !strings.Contains(body, "http://paste.test"+loc) {
		t.Error("page does not show the share link")
	}
	if strings.Contains(body, "<script>alert(1)</script>") {
		t.Error("paste content was rendered as raw HTML")
	}
	if !strings.Contains(body, "&lt;script&gt;alert(1)&lt;/script&gt;") {
		t.Error("escaped paste content not found on the page")
	}
}

func TestCacheLifetimeStopsAtExpiry(t *testing.T) {
	srv, _ := newTestServer(t)
	loc := postPaste(t, srv, "short-lived", "10m").Header.Get("Location")

	resp, _ := get(t, srv.URL+loc)
	cc := resp.Header.Get("Cache-Control")
	var secs int
	if _, err := fmt.Sscanf(cc, "private, max-age=%d", &secs); err != nil {
		t.Fatalf("Cache-Control = %q: %v", cc, err)
	}
	if secs <= 0 || secs > 600 {
		t.Fatalf("max-age = %d, want within (0, 600] for a 10 minute paste", secs)
	}
}

func TestExpiredPasteIsNotFound(t *testing.T) {
	srv, store := newTestServer(t)
	past := time.Now().UTC().Add(-time.Minute)
	store.pastes["OLDPASTE"] = paste.Paste{ID: "OLDPASTE", Content: "gone", ExpiresAt: &past}

	resp, body := get(t, srv.URL+"/OLDPASTE")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	if resp.Header.Get("Cache-Control") != "no-store" {
		t.Errorf("404 should not be cached, got Cache-Control %q", resp.Header.Get("Cache-Control"))
	}
	if strings.Contains(body, "gone") {
		t.Error("expired content leaked into the 404 page")
	}
}

func TestUnknownPasteIsNotFound(t *testing.T) {
	srv, _ := newTestServer(t)
	if resp, _ := get(t, srv.URL+"/NOPE1234"); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestUnknownExpiryIsRejected(t *testing.T) {
	srv, store := newTestServer(t)
	if resp := postPaste(t, srv, "hello", "forever-and-ever"); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	if len(store.pastes) != 0 {
		t.Fatal("a paste was stored despite the bad expiry value")
	}
}
