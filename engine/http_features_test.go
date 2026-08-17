package engine

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	bungo "github.com/piotr-nierobisz/BunGo"
)

func TestCreateHandler_globalResponseHeaders(t *testing.T) {
	dir := mustWebDir(t)
	mustWrite(t, filepath.Join(dir, "static", "a.txt"), "hello")

	eng := NewHTTPEngine()
	srv := bungo.NewServer(eng, dir)
	srv.SetResponseHeaders(map[string]string{
		"X-Frame-Options": "DENY",
		"Cache-Control":   "no-cache",
	})
	srv.Page(bungo.PageRoute{Path: "/", Template: "home.gohtml"})
	srv.Api(bungo.ApiRoute{
		Path:    "x",
		Version: "v1",
		Method:  http.MethodGet,
		Handler: func(req *bungo.Request) (bungo.APIResponse, error) {
			return bungo.APIResponse{
				StatusCode: 200,
				Body:       map[string]any{"ok": true},
				Headers:    map[string]string{"Cache-Control": "no-store"},
			}, nil
		},
	})

	h, err := eng.CreateHandler(srv)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("page gets global headers", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		if rec.Code != http.StatusOK || rec.Header().Get("X-Frame-Options") != "DENY" {
			t.Fatalf("%d %#v", rec.Code, rec.Header())
		}
	})

	t.Run("static gets global headers", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/static/a.txt", nil))
		if rec.Code != http.StatusOK || rec.Header().Get("X-Frame-Options") != "DENY" {
			t.Fatalf("%d %#v", rec.Code, rec.Header())
		}
	})

	t.Run("per-response header overrides global", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/x", nil))
		if rec.Code != http.StatusOK {
			t.Fatal(rec.Code, rec.Body.String())
		}
		if got := rec.Header().Get("Cache-Control"); got != "no-store" {
			t.Fatalf("Cache-Control = %q, want per-response no-store", got)
		}
		if rec.Header().Get("X-Frame-Options") != "DENY" {
			t.Fatal("global header missing on API response")
		}
	})
}

func TestCreateHandler_securityRichRejection(t *testing.T) {
	dir := mustWebDir(t)
	eng := NewHTTPEngine()
	srv := bungo.NewServer(eng, dir)

	srv.Security(bungo.SecurityLayer{
		Name: "throttle",
		Handler: func(req *bungo.Request) (bool, *bungo.APIResponse) {
			return false, &bungo.APIResponse{
				StatusCode: http.StatusTooManyRequests,
				Body:       map[string]any{"error": "rate limited"},
				Headers:    map[string]string{"Retry-After": "60"},
				Cookies:    []bungo.Cookie{{Name: "throttled", Value: "1"}},
			}
		},
	})
	srv.Security(bungo.SecurityLayer{
		Name: "to_login",
		Handler: func(req *bungo.Request) (bool, *bungo.APIResponse) {
			return false, &bungo.APIResponse{
				StatusCode: http.StatusFound,
				Headers:    map[string]string{"Location": "/login"},
			}
		},
	})
	srv.Security(bungo.SecurityLayer{
		Name: "zero_status",
		Handler: func(req *bungo.Request) (bool, *bungo.APIResponse) {
			return false, &bungo.APIResponse{Body: map[string]any{"error": "nope"}}
		},
	})

	srv.Api(bungo.ApiRoute{
		Path: "limited", Version: "v1", Method: http.MethodGet,
		SecurityLayer: []string{"throttle"},
		Handler: func(req *bungo.Request) (bungo.APIResponse, error) {
			return bungo.APIResponse{StatusCode: 200, Body: "unreachable"}, nil
		},
	})
	srv.Api(bungo.ApiRoute{
		Path: "defaulted", Version: "v1", Method: http.MethodGet,
		SecurityLayer: []string{"zero_status"},
		Handler: func(req *bungo.Request) (bungo.APIResponse, error) {
			return bungo.APIResponse{StatusCode: 200, Body: "unreachable"}, nil
		},
	})
	srv.Page(bungo.PageRoute{
		Path: "/private", Template: "home.gohtml",
		SecurityLayer: []string{"to_login"},
	})

	h, err := eng.CreateHandler(srv)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("429 with body, header, and cookie", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/limited", nil))
		if rec.Code != http.StatusTooManyRequests {
			t.Fatal(rec.Code, rec.Body.String())
		}
		if rec.Header().Get("Retry-After") != "60" {
			t.Fatalf("Retry-After missing: %#v", rec.Header())
		}
		if !strings.Contains(rec.Body.String(), "rate limited") {
			t.Fatal(rec.Body.String())
		}
		if cookies := rec.Result().Cookies(); len(cookies) != 1 || cookies[0].Name != "throttled" {
			t.Fatalf("rejection cookie missing: %#v", cookies)
		}
	})

	t.Run("page route redirects to login", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/private", nil))
		if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/login" {
			t.Fatalf("%d %#v", rec.Code, rec.Header())
		}
		if rec.Body.Len() != 0 {
			t.Fatalf("redirect should have no body, got %q", rec.Body.String())
		}
	})

	t.Run("zero status defaults to 401", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/defaulted", nil))
		if rec.Code != http.StatusUnauthorized {
			t.Fatal(rec.Code, rec.Body.String())
		}
	})
}

func TestCreateHandler_notFoundPage(t *testing.T) {
	dir := mustWebDir(t)
	mustWrite(t, filepath.Join(dir, "layouts", "not_found.gohtml"), `<!DOCTYPE html><html><body>missing: {{.Requested}}</body></html>`)

	eng := NewHTTPEngine()
	srv := bungo.NewServer(eng, dir)
	srv.Page(bungo.PageRoute{
		Path:     "/",
		Template: "home.gohtml",
		Handler: func(req *bungo.Request) (map[string]any, error) {
			return map[string]any{"Title": "root"}, nil
		},
	})
	srv.Page(bungo.PageRoute{
		Path:     bungo.NotFoundPath,
		Template: "not_found.gohtml",
		Handler: func(req *bungo.Request) (map[string]any, error) {
			return map[string]any{"Requested": "somewhere"}, nil
		},
	})

	h, err := eng.CreateHandler(srv)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("unmatched path renders 404 page", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/nope", nil))
		if rec.Code != http.StatusNotFound {
			t.Fatal(rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "missing: somewhere") {
			t.Fatal(rec.Body.String())
		}
	})

	t.Run("root still renders 200", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "root") {
			t.Fatal(rec.Code, rec.Body.String())
		}
	})

	t.Run("non-GET unknown path is a plain 404", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/nope", nil))
		if rec.Code != http.StatusNotFound {
			t.Fatal(rec.Code)
		}
	})
}

func TestCreateHandler_notFoundPageWithoutRoot(t *testing.T) {
	dir := mustWebDir(t)
	eng := NewHTTPEngine()
	srv := bungo.NewServer(eng, dir)
	srv.Page(bungo.PageRoute{Path: bungo.NotFoundPath, Template: "home.gohtml"})

	h, err := eng.CreateHandler(srv)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatal(rec.Code, rec.Body.String())
	}
}

func TestCreateHandler_legacyRootFallthroughWithoutNotFound(t *testing.T) {
	dir := mustWebDir(t)
	eng := NewHTTPEngine()
	srv := bungo.NewServer(eng, dir)
	srv.Page(bungo.PageRoute{
		Path:     "/",
		Template: "home.gohtml",
		Handler: func(req *bungo.Request) (map[string]any, error) {
			return map[string]any{"Title": "root"}, nil
		},
	})

	h, err := eng.CreateHandler(srv)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/anything", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "root") {
		t.Fatalf("legacy subtree fallthrough broken: %d %s", rec.Code, rec.Body.String())
	}
}

func TestCreateHandler_apiFallback(t *testing.T) {
	dir := mustWebDir(t)
	eng := NewHTTPEngine()
	srv := bungo.NewServer(eng, dir)
	srv.Page(bungo.PageRoute{Path: "/", Template: "home.gohtml"})
	srv.Api(bungo.ApiRoute{
		Path: "x", Version: "v1", Method: http.MethodGet,
		Handler: func(req *bungo.Request) (bungo.APIResponse, error) {
			return bungo.APIResponse{StatusCode: 200, Body: "ok"}, nil
		},
	})
	srv.Api(bungo.ApiRoute{
		Path: "x", Version: "v1", Method: http.MethodDelete,
		Handler: func(req *bungo.Request) (bungo.APIResponse, error) {
			return bungo.APIResponse{StatusCode: 200, Body: "ok"}, nil
		},
	})

	h, err := eng.CreateHandler(srv)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("unknown api path is JSON 404, not the landing page", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/unknown", nil))
		if rec.Code != http.StatusNotFound {
			t.Fatal(rec.Code, rec.Body.String())
		}
		if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
			t.Fatalf("content type %q", ct)
		}
	})

	t.Run("wrong method on known api path is 405 with Allow", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/x", nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatal(rec.Code, rec.Body.String())
		}
		if allow := rec.Header().Get("Allow"); allow != "DELETE, GET" {
			t.Fatalf("Allow = %q", allow)
		}
	})
}

func TestCreateHandler_apiCheckOrigin(t *testing.T) {
	dir := mustWebDir(t)
	eng := NewHTTPEngine()
	srv := bungo.NewServer(eng, dir)
	srv.Api(bungo.ApiRoute{
		Path: "guarded", Version: "v1", Method: http.MethodGet,
		CheckOrigin: func(req *bungo.Request) bool {
			return req.Headers["Origin"] == "https://good.example"
		},
		Handler: func(req *bungo.Request) (bungo.APIResponse, error) {
			return bungo.APIResponse{StatusCode: 200, Body: "ok"}, nil
		},
	})
	srv.Api(bungo.ApiRoute{
		Path: "open", Version: "v1", Method: http.MethodGet,
		Handler: func(req *bungo.Request) (bungo.APIResponse, error) {
			return bungo.APIResponse{StatusCode: 200, Body: "ok"}, nil
		},
	})

	h, err := eng.CreateHandler(srv)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("refused origin gets 403", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/guarded", nil)
		req.Header.Set("Origin", "https://evil.example")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatal(rec.Code, rec.Body.String())
		}
	})

	t.Run("allowed origin passes", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/guarded", nil)
		req.Header.Set("Origin", "https://good.example")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatal(rec.Code, rec.Body.String())
		}
	})

	t.Run("nil CheckOrigin means no check", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/open", nil)
		req.Header.Set("Origin", "https://evil.example")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatal(rec.Code, rec.Body.String())
		}
	})
}
