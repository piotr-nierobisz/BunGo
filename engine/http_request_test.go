package engine

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	bungo "github.com/piotr-nierobisz/BunGo"
)

func TestHTTPEngine_translateRequest(t *testing.T) {
	t.Parallel()
	e := NewHTTPEngine()
	req := httptest.NewRequest(http.MethodPost, "/p?q=1&z=two", bytes.NewBufferString(`{"a":1}`))
	req.Header.Set("X-Test", "v")
	req.Header.Set("X-Multi", "first")
	req.Header.Add("X-Multi", "second")

	breq, err := e.translateRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	if breq.Headers["X-Test"] != "v" {
		t.Fatal(breq.Headers)
	}
	if breq.Params["q"] != "1" || breq.Params["z"] != "two" {
		t.Fatal(breq.Params)
	}
	if string(breq.Body) != `{"a":1}` {
		t.Fatal(string(breq.Body))
	}
	if breq.Internal == nil {
		t.Fatal("internal map")
	}
}

func TestHTTPEngine_translateRequest_nilBody(t *testing.T) {
	t.Parallel()
	e := NewHTTPEngine()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Body = nil
	breq, err := e.translateRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	if len(breq.Body) != 0 {
		t.Fatal(breq.Body)
	}
}

func TestCreateHandler_pageAndAPI(t *testing.T) {
	dir := mustWebDir(t)
	eng := NewHTTPEngine()
	srv := bungo.NewServer(eng, dir)

	srv.Page(bungo.PageRoute{
		Path:     "/",
		Template: "home.gohtml",
		Handler: func(req *bungo.Request) (map[string]any, error) {
			return map[string]any{"Title": "ok"}, nil
		},
	})

	srv.Api(bungo.ApiRoute{
		Path:    "x",
		Version: "v1",
		Method:  http.MethodGet,
		Handler: func(req *bungo.Request) (bungo.APIResponse, error) {
			return bungo.APIResponse{StatusCode: 200, Body: map[string]int{"n": 42}}, nil
		},
	})

	h, err := eng.CreateHandler(srv)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("GET page", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		if rec.Code != http.StatusOK {
			t.Fatal(rec.Code, rec.Body.String())
		}
		if !bytes.Contains(rec.Body.Bytes(), []byte("ok")) {
			t.Fatal(rec.Body.String())
		}
	})

	t.Run("GET API", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/x", nil))
		if rec.Code != http.StatusOK {
			t.Fatal(rec.Code, rec.Body.String())
		}
	})

	t.Run("wrong method page", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatal(rec.Code)
		}
	})

	t.Run("wrong method API", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/x", nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatal(rec.Code)
		}
	})
}

func TestCreateHandler_securityUnauthorized(t *testing.T) {
	dir := mustWebDir(t)
	eng := NewHTTPEngine()
	srv := bungo.NewServer(eng, dir)
	srv.Security(bungo.SecurityLayer{
		Name: "gate",
		Handler: func(req *bungo.Request) (bool, *bungo.APIResponse) {
			return false, nil
		},
	})
	srv.Page(bungo.PageRoute{
		Path:          "/",
		Template:      "home.gohtml",
		SecurityLayer: []string{"gate"},
		Handler:       func(*bungo.Request) (map[string]any, error) { return nil, nil },
	})

	h, err := eng.CreateHandler(srv)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatal(rec.Code, rec.Body.String())
	}
}

func TestCreateHandler_securityMissingLayer(t *testing.T) {
	dir := mustWebDir(t)
	eng := NewHTTPEngine()
	srv := bungo.NewServer(eng, dir)
	srv.Page(bungo.PageRoute{
		Path:          "/",
		Template:      "home.gohtml",
		SecurityLayer: []string{"nope"},
		Handler:       func(*bungo.Request) (map[string]any, error) { return nil, nil },
	})
	h, err := eng.CreateHandler(srv)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatal(rec.Code)
	}
}

func TestCreateHandler_static(t *testing.T) {
	dir := mustWebDir(t)
	mustWrite(t, filepath.Join(dir, "static", "a.txt"), "hello")

	eng := NewHTTPEngine()
	srv := bungo.NewServer(eng, dir)
	srv.Page(bungo.PageRoute{Path: "/z", Template: "home.gohtml"})

	h, err := eng.CreateHandler(srv)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/static/a.txt", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "hello" {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
}

func TestCreateHandler_staticAlias(t *testing.T) {
	dir := mustWebDir(t)
	mustWrite(t, filepath.Join(dir, "static", "robots.txt"), "User-agent: *")
	mustWrite(t, filepath.Join(dir, "static", "seo", "sitemap.xml"), "<urlset/>")

	eng := NewHTTPEngine()
	srv := bungo.NewServer(eng, dir)
	srv.StaticAlias("/robots.txt", "robots.txt")
	srv.StaticAlias("/sitemap.xml", "seo/sitemap.xml")

	h, err := eng.CreateHandler(srv)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("GET root alias", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/robots.txt", nil))
		if rec.Code != http.StatusOK || rec.Body.String() != "User-agent: *" {
			t.Fatalf("%d %s", rec.Code, rec.Body.String())
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
			t.Fatalf("content type: %q", ct)
		}
	})

	t.Run("GET nested target", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sitemap.xml", nil))
		if rec.Code != http.StatusOK || rec.Body.String() != "<urlset/>" {
			t.Fatalf("%d %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("POST rejected", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/robots.txt", nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatal(rec.Code)
		}
	})

	t.Run("target still served under /static/", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/static/robots.txt", nil))
		if rec.Code != http.StatusOK || rec.Body.String() != "User-agent: *" {
			t.Fatalf("%d %s", rec.Code, rec.Body.String())
		}
	})
}

func TestCreateHandler_optimizedBungo(t *testing.T) {
	dir := mustWebDir(t)
	eng := NewHTTPEngine()
	srv := bungo.NewServer(eng, dir)
	srv.SetAssetOptimization(true)
	mustWrite(t, filepath.Join(dir, "views", "v.jsx"), `export default function V(){return null}
_bungoRender(V);`)

	srv.Page(bungo.PageRoute{
		Path:     "/",
		Template: "home.gohtml",
		View:     "v.jsx",
	})

	h, err := eng.CreateHandler(srv)
	if err != nil {
		t.Fatal(err)
	}

	// The injected src is the source of truth: fetch the page, extract the
	// content-hashed URL, and confirm the engine serves exactly that path.
	page := httptest.NewRecorder()
	h.ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/", nil))
	if page.Code != http.StatusOK {
		t.Fatalf("%d %s", page.Code, page.Body.String())
	}
	match := regexp.MustCompile(`src="(/_bungo/v\.[0-9a-f]{8}\.js)"`).FindStringSubmatch(page.Body.String())
	if match == nil {
		t.Fatalf("no content-hashed module src injected: %s", page.Body.String())
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, match[1], nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct == "" {
		t.Fatal("missing content type")
	}
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Fatalf("expected immutable Cache-Control, got %q", cc)
	}

	// The old hashless URL must not resolve — a stale reference should 404
	// rather than silently pin an outdated bundle.
	stale := httptest.NewRecorder()
	h.ServeHTTP(stale, httptest.NewRequest(http.MethodGet, "/_bungo/v.js", nil))
	if stale.Code != http.StatusNotFound {
		t.Fatalf("hashless path: expected 404, got %d", stale.Code)
	}
}

func TestCreateHandler_apiCookies(t *testing.T) {
	dir := mustWebDir(t)
	eng := NewHTTPEngine()
	srv := bungo.NewServer(eng, dir)

	expiry := time.Date(2030, time.January, 2, 3, 4, 5, 0, time.UTC)
	srv.Api(bungo.ApiRoute{
		Path:    "set",
		Version: "v1",
		Method:  http.MethodGet,
		Handler: func(req *bungo.Request) (bungo.APIResponse, error) {
			return bungo.APIResponse{
				StatusCode: 200,
				Body:       map[string]any{"ok": true},
				Cookies: []bungo.Cookie{
					{
						Name:     "session",
						Value:    "abc123",
						Path:     "/",
						Expires:  expiry,
						HttpOnly: true,
						Secure:   true,
						SameSite: bungo.SameSiteLax,
					},
					{Name: "tracker", Value: "", MaxAge: -1, Path: "/"},
					{Name: "", Value: "ignored"},
				},
			}, nil
		},
	})

	h, err := eng.CreateHandler(srv)
	if err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/set", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}

	cookies := rec.Result().Cookies()
	if len(cookies) != 2 {
		t.Fatalf("expected 2 cookies (anonymous skipped), got %d: %#v", len(cookies), cookies)
	}

	var session, tracker *http.Cookie
	for _, c := range cookies {
		switch c.Name {
		case "session":
			session = c
		case "tracker":
			tracker = c
		}
	}
	if session == nil {
		t.Fatal("session cookie missing")
	}
	if session.Value != "abc123" || !session.HttpOnly || !session.Secure || session.SameSite != http.SameSiteLaxMode {
		t.Fatalf("session attributes wrong: %#v", session)
	}
	if !session.Expires.Equal(expiry) {
		t.Fatalf("session expiry: got %s want %s", session.Expires, expiry)
	}

	if tracker == nil {
		t.Fatal("tracker delete cookie missing")
	}
	if tracker.MaxAge >= 0 {
		t.Fatalf("tracker MaxAge=%d, expected negative for delete", tracker.MaxAge)
	}
}

func TestCreateHandler_apiCookiesCustomConverter(t *testing.T) {
	dir := mustWebDir(t)
	eng := NewHTTPEngine()
	eng.SetCookieConverter(func(c bungo.Cookie) *http.Cookie {
		return &http.Cookie{Name: "x-" + c.Name, Value: c.Value, Path: "/forced"}
	})
	srv := bungo.NewServer(eng, dir)
	srv.Api(bungo.ApiRoute{
		Path:    "set",
		Version: "v1",
		Method:  http.MethodGet,
		Handler: func(req *bungo.Request) (bungo.APIResponse, error) {
			return bungo.APIResponse{
				StatusCode: 200,
				Body:       map[string]any{"ok": true},
				Cookies:    []bungo.Cookie{{Name: "session", Value: "v"}},
			}, nil
		},
	})

	h, err := eng.CreateHandler(srv)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/set", nil))

	cookies := rec.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != "x-session" || cookies[0].Path != "/forced" {
		t.Fatalf("custom converter not applied: %#v", cookies)
	}
}

func mustWebDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, sub := range []string{"layouts", "views"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "layouts", "home.gohtml"), []byte(`<!DOCTYPE html><html><head></head><body>{{.Title}}</body></html>`), 0644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func mustWrite(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
