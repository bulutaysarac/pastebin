// External test package: it drives the handlers through the real router,
// and package routes already imports package handlers.
package handlers_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/bulutaysarac/pastebin/handlers"
	"github.com/bulutaysarac/pastebin/internal/paste"
	"github.com/bulutaysarac/pastebin/routes"
)

type memStore struct {
	mu      sync.Mutex
	pastes  map[string]paste.Paste
	pingErr error // what Ping returns, for the /health tests
	hang    bool  // Get and Create wait until the request's context ends, like on a locked table
	err     error // what Get and Create return instead of working
}

func (m *memStore) Ping(context.Context) error { return m.pingErr }

func (m *memStore) fail(ctx context.Context) error {
	if m.hang {
		<-ctx.Done()
		return ctx.Err()
	}
	return m.err
}

func (m *memStore) Create(ctx context.Context, p paste.Paste) error {
	if err := m.fail(ctx); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.pastes[p.ID]; ok {
		return paste.ErrIDTaken
	}
	m.pastes[p.ID] = p
	return nil
}

func (m *memStore) Get(ctx context.Context, id string) (paste.Paste, error) {
	if err := m.fail(ctx); err != nil {
		return paste.Paste{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.pastes[id]
	if !ok {
		return paste.Paste{}, paste.ErrNotFound
	}
	return p, nil
}

// newTestApp builds the app over an in-memory store. createsPerMinute is the
// create rate limit; 0 turns it off.
func newTestApp(t *testing.T, createsPerMinute int) (*echo.Echo, *memStore) {
	t.Helper()
	return newLoggingTestApp(t, createsPerMinute, io.Discard)
}

// newLoggingTestApp is newTestApp with the handlers' log written to logs.
func newLoggingTestApp(t *testing.T, createsPerMinute int, logs io.Writer) (*echo.Echo, *memStore) {
	t.Helper()
	store := &memStore{pastes: map[string]paste.Paste{}}
	h := handlers.New(paste.NewService(store), store, "http://paste.test", slog.New(slog.NewTextHandler(logs, nil)))

	e := echo.New()
	e.Renderer = handlers.NewRenderer()
	routes.RegisterAPIRoutes(e, h, createsPerMinute)
	return e, store
}

func newTestServer(t *testing.T) (*httptest.Server, *memStore) {
	t.Helper()
	e, store := newTestApp(t, 0)
	srv := httptest.NewServer(e)
	t.Cleanup(srv.Close)
	return srv, store
}

// noRedirect makes the client return 3xx responses instead of following them.
var noRedirect = &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

// postPaste submits the form without a language field, like curl or an old client would.
func postPaste(t *testing.T, srv *httptest.Server, content, expires string) *http.Response {
	t.Helper()
	return postForm(t, srv, url.Values{"content": {content}, "expires": {expires}})
}

func postForm(t *testing.T, srv *httptest.Server, form url.Values) *http.Response {
	t.Helper()
	resp, err := noRedirect.PostForm(srv.URL+"/paste", form)
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
	for _, want := range []string{`action="/paste"`, `name="content"`, `name="expires"`, `value="1h"`, `name="language"`, `value="sql"`, `id="theme-toggle"`, fmt.Sprintf("Up to %d KiB", paste.MaxContentBytes>>10)} {
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

func TestEmptyPasteIsRejected(t *testing.T) {
	srv, store := newTestServer(t)
	for _, content := range []string{"", " \r\n\t "} {
		resp := postPaste(t, srv, content, "never")
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusBadRequest || string(body) != "paste is empty" {
			t.Errorf("content %q: status %d, body %q; want 400, paste is empty", content, resp.StatusCode, body)
		}
	}
	if len(store.pastes) != 0 {
		t.Fatal("an empty paste was stored")
	}
}

func TestPasteOverTheCapIsRejected(t *testing.T) {
	srv, store := newTestServer(t)
	resp := postPaste(t, srv, strings.Repeat("x", paste.MaxContentBytes+1), "never")
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusRequestEntityTooLarge || string(body) != paste.ErrTooLarge.Error() {
		t.Fatalf("status %d, body %q; want 413, %q", resp.StatusCode, body, paste.ErrTooLarge)
	}
	if len(store.pastes) != 0 {
		t.Fatal("a paste over the cap was stored")
	}
}

func TestPasteAtTheCapFitsTheBodyLimitWhenEncoded(t *testing.T) {
	srv, store := newTestServer(t)
	// Every "%" is sent as "%25", three bytes on the wire for one stored.
	resp := postPaste(t, srv, strings.Repeat("%", paste.MaxContentBytes), "never")
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("status %d, want 303", resp.StatusCode)
	}
	if got := len(store.pastes[resp.Header.Get("Location")[1:]].Content); got != paste.MaxContentBytes {
		t.Fatalf("stored %d bytes, want %d", got, paste.MaxContentBytes)
	}
}

func TestOversizedBodyIsRejectedBeforeItIsParsed(t *testing.T) {
	// Straight to the router, not over a connection: the server answers
	// before the client has sent the whole body.
	e, store := newTestApp(t, 0)
	// The content itself is fine; only the body limit can refuse this.
	body := url.Values{"content": {"hello"}, "expires": {"never"}, "padding": {strings.Repeat("x", 2<<20)}}.Encode()
	for name, r := range map[string]io.Reader{
		"with Content-Length": strings.NewReader(body),
		"chunked":             struct{ io.Reader }{strings.NewReader(body)},
	} {
		req := httptest.NewRequest(http.MethodPost, "/paste", r)
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if rec.Code != http.StatusRequestEntityTooLarge || rec.Body.String() != paste.ErrTooLarge.Error() {
			t.Errorf("%s: status %d, body %q; want 413, %q", name, rec.Code, rec.Body, paste.ErrTooLarge)
		}
	}
	if len(store.pastes) != 0 {
		t.Fatal("an oversized body was stored")
	}
}

// createFrom sends one create straight to the router as if from remoteAddr.
func createFrom(e *echo.Echo, remoteAddr string, header http.Header) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/paste", strings.NewReader("content=hi&expires=never"))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
	maps.Copy(req.Header, header)
	req.RemoteAddr = remoteAddr
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

// useBurst spends remoteAddr's burst of 10 creates.
func useBurst(t *testing.T, e *echo.Echo, remoteAddr string) {
	t.Helper()
	for i := range 10 {
		if rec := createFrom(e, remoteAddr, nil); rec.Code != http.StatusSeeOther {
			t.Fatalf("create %d of the burst: status %d, want 303", i+1, rec.Code)
		}
	}
}

func TestCreatesPastTheBurstGet429WithRetryAfter(t *testing.T) {
	// One a minute, so no create comes back while the test runs.
	e, store := newTestApp(t, 1)
	useBurst(t, e, "192.0.2.1:40000")

	rec := createFrom(e, "192.0.2.1:40001", nil)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status %d, want 429", rec.Code)
	}
	if got := rec.Header().Get("Retry-After"); got != "60" {
		t.Errorf("Retry-After = %q, want 60", got)
	}
	if !strings.Contains(rec.Body.String(), "try again in 60s") {
		t.Errorf("body %q does not say when to try again", rec.Body)
	}
	if len(store.pastes) != 10 {
		t.Fatalf("store has %d pastes, want the 10 of the burst", len(store.pastes))
	}
}

func TestEachClientIPHasItsOwnLimit(t *testing.T) {
	e, _ := newTestApp(t, 1)
	useBurst(t, e, "192.0.2.1:40000")

	if rec := createFrom(e, "192.0.2.2:40000", nil); rec.Code != http.StatusSeeOther {
		t.Fatalf("another IP: status %d, want 303", rec.Code)
	}
}

func TestForwardedHeadersDontResetTheLimit(t *testing.T) {
	e, _ := newTestApp(t, 1)
	useBurst(t, e, "192.0.2.1:40000")

	forged := http.Header{"X-Forwarded-For": {"203.0.113.7"}, "X-Real-Ip": {"203.0.113.8"}}
	if rec := createFrom(e, "192.0.2.1:40000", forged); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("forged X-Forwarded-For: status %d, want 429", rec.Code)
	}
}

func TestZeroCreatesPerMinuteTurnsTheLimitOff(t *testing.T) {
	e, _ := newTestApp(t, 0)
	for i := range 20 {
		if rec := createFrom(e, "192.0.2.1:40000", nil); rec.Code != http.StatusSeeOther {
			t.Fatalf("create %d: status %d, want 303", i+1, rec.Code)
		}
	}
}

// storeRequests is one read and one create, each sent with ctx.
func storeRequests(ctx context.Context) map[string]*http.Request {
	create := httptest.NewRequest(http.MethodPost, "/paste", strings.NewReader("content=hi&expires=never"))
	create.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
	return map[string]*http.Request{
		"read":   httptest.NewRequest(http.MethodGet, "/AAAAAAAA", nil).WithContext(ctx),
		"create": create.WithContext(ctx),
	}
}

func TestStoreOutlastingTheDeadlineGets503(t *testing.T) {
	var logs bytes.Buffer
	e, store := newLoggingTestApp(t, 0, &logs)
	store.hang = true

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	for name, req := range storeRequests(ctx) {
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if rec.Code != http.StatusServiceUnavailable || !strings.HasSuffix(rec.Body.String(), "in time, try again") {
			t.Errorf("%s: status %d, body %q; want 503 ... in time, try again", name, rec.Code, rec.Body)
		}
	}
	if !strings.Contains(logs.String(), "level=WARN") || strings.Contains(logs.String(), "level=ERROR") {
		t.Errorf("want WARN and no ERROR in the log:\n%s", logs.String())
	}
}

func TestDatabaseTimeoutGets503(t *testing.T) {
	var logs bytes.Buffer
	e, store := newLoggingTestApp(t, 0, &logs)
	store.err = fmt.Errorf("%w: lock wait timeout exceeded", paste.ErrStoreTimeout)

	for name, req := range storeRequests(context.Background()) {
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("%s: status %d, want 503", name, rec.Code)
		}
	}
	if !strings.Contains(logs.String(), "level=WARN") || strings.Contains(logs.String(), "level=ERROR") {
		t.Errorf("want WARN and no ERROR in the log:\n%s", logs.String())
	}
}

func TestClientThatLeftIsNotAServerError(t *testing.T) {
	var logs bytes.Buffer
	e, store := newLoggingTestApp(t, 0, &logs)
	store.hang = true

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for name, req := range storeRequests(ctx) {
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if rec.Code != 499 {
			t.Errorf("%s: status %d, want 499", name, rec.Code)
		}
	}
	if !strings.Contains(logs.String(), "client went away") || strings.Contains(logs.String(), "level=ERROR") {
		t.Errorf("want \"client went away\" and no ERROR in the log:\n%s", logs.String())
	}
}

func TestOtherStoreErrorsStay500(t *testing.T) {
	var logs bytes.Buffer
	e, store := newLoggingTestApp(t, 0, &logs)
	store.err = errors.New("disk full")

	for name, req := range storeRequests(context.Background()) {
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("%s: status %d, want 500", name, rec.Code)
		}
	}
	if !strings.Contains(logs.String(), "level=ERROR") {
		t.Errorf("want ERROR in the log:\n%s", logs.String())
	}
}

func TestLanguageIsStoredAndRendered(t *testing.T) {
	srv, store := newTestServer(t)
	resp := postForm(t, srv, url.Values{"content": {"SELECT 1;"}, "expires": {"never"}, "language": {"sql"}})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("POST status = %d, want 303", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if got := store.pastes[loc[1:]].Language; got != "sql" {
		t.Fatalf("stored language = %q, want sql", got)
	}

	_, body := get(t, srv.URL+loc)
	for _, want := range []string{`class="language-sql"`, `>SQL<`, `data-copy="#paste-code"`, `data-copy="#share-url"`} {
		if !strings.Contains(body, want) {
			t.Errorf("paste page is missing %s", want)
		}
	}
}

func TestMissingLanguageDefaultsToPlainText(t *testing.T) {
	srv, store := newTestServer(t)
	loc := postPaste(t, srv, "no language field", "never").Header.Get("Location")
	if got := store.pastes[loc[1:]].Language; got != "plaintext" {
		t.Fatalf("stored language = %q, want plaintext", got)
	}
}

func TestUnknownLanguageIsRejected(t *testing.T) {
	srv, store := newTestServer(t)
	resp := postForm(t, srv, url.Values{"content": {"x"}, "expires": {"never"}, "language": {"brainfuck"}})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	if len(store.pastes) != 0 {
		t.Fatal("a paste was stored despite the unknown language")
	}
}

func TestHealthFollowsTheDatabase(t *testing.T) {
	srv, store := newTestServer(t)
	if resp, body := get(t, srv.URL+"/health"); resp.StatusCode != http.StatusOK || body != "ok" {
		t.Fatalf("healthy: status %d, body %q", resp.StatusCode, body)
	}

	store.pingErr = errors.New("connection refused")
	resp, _ := get(t, srv.URL+"/health")
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("database down: status %d, want 503", resp.StatusCode)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", cc)
	}
}

func TestMetricsAreServed(t *testing.T) {
	srv, _ := newTestServer(t)
	resp, body := get(t, srv.URL+"/metrics")
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, "go_goroutines") {
		t.Fatalf("/metrics: status %d, body without go_goroutines", resp.StatusCode)
	}
}

func TestOnlyStoredPastesAreCounted(t *testing.T) {
	srv, _ := newTestServer(t)
	// No other test uses this language and expiry pair, so the count is this test's alone.
	postForm(t, srv, url.Values{"content": {"fn main() {}"}, "expires": {"1w"}, "language": {"rust"}})
	postForm(t, srv, url.Values{"content": {"fn main() {}"}, "expires": {"forever"}, "language": {"rust"}})

	_, body := get(t, srv.URL+"/metrics")
	if want := `pastebin_pastes_created_total{expires="1w",language="rust"} 1`; !strings.Contains(body, want) {
		t.Errorf("/metrics is missing %s", want)
	}
	if strings.Contains(body, `expires="forever"`) {
		t.Error("a rejected expiry value became a label")
	}
}

func TestEveryOptionPairIsCountedFromZero(t *testing.T) {
	srv, _ := newTestServer(t)
	_, body := get(t, srv.URL+"/metrics")
	// No test creates a PHP paste that expires in a day.
	if want := `pastebin_pastes_created_total{expires="1d",language="php"} 0`; !strings.Contains(body, want) {
		t.Errorf("/metrics is missing %s", want)
	}
}

func TestAssetsAreServedWithCacheHeader(t *testing.T) {
	srv, _ := newTestServer(t)
	for _, path := range []string{"/assets/app.css", "/assets/app.js", "/assets/vendor/highlight.min.js", "/assets/vendor/github-dark.min.css"} {
		resp, body := get(t, srv.URL+path)
		if resp.StatusCode != http.StatusOK || len(body) == 0 {
			t.Errorf("%s: status %d, %d bytes", path, resp.StatusCode, len(body))
			continue
		}
		if cc := resp.Header.Get("Cache-Control"); cc != "public, max-age=3600" {
			t.Errorf("%s: Cache-Control = %q", path, cc)
		}
	}
}
