package engine

import (
	"errors"
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

func TestCreateHandler_handlerAndRenderFailuresAre500(t *testing.T) {
	dir := mustWebDir(t)
	mustWrite(t, filepath.Join(dir, "layouts", "broken.gohtml"), `<html><body>{{index .Items 3}}</body></html>`)
	eng := NewHTTPEngine()
	srv := bungo.NewServer(eng, dir)
	srv.Api(bungo.ApiRoute{
		Path: "fail", Version: "v1", Method: http.MethodGet,
		Handler: func(req *bungo.Request) (bungo.APIResponse, error) {
			return bungo.APIResponse{StatusCode: 200, Body: "unreachable"}, errors.New("db unavailable")
		},
	})
	srv.Page(bungo.PageRoute{
		Path: "/fail", Template: "home.gohtml",
		Handler: func(req *bungo.Request) (map[string]any, error) {
			return nil, errors.New("lookup failed")
		},
	})
	srv.Page(bungo.PageRoute{
		Path: "/broken", Template: "broken.gohtml",
		Handler: func(req *bungo.Request) (map[string]any, error) {
			return map[string]any{"Items": []string{"one"}}, nil
		},
	})

	h, err := eng.CreateHandler(srv)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		path string
		want string
	}{
		{"API handler error", "/api/v1/fail", "db unavailable"},
		{"page handler error", "/fail", "lookup failed"},
		{"page render error", "/broken", "index out of range"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tt.path, nil))
			if rec.Code != http.StatusInternalServerError {
				t.Fatalf("status %d, want 500 (body %q)", rec.Code, rec.Body.String())
			}
			body := rec.Body.String()
			if !strings.Contains(body, tt.want) {
				t.Fatalf("body %q should carry the failure %q", body, tt.want)
			}
			// A failed render must not ship half a page or the handler's response body.
			if strings.Contains(body, "<html>") || strings.Contains(body, "unreachable") {
				t.Fatalf("partial response leaked: %q", body)
			}
		})
	}
}

func TestCreateHandler_apiResponseDefaults(t *testing.T) {
	dir := mustWebDir(t)
	eng := NewHTTPEngine()
	srv := bungo.NewServer(eng, dir)
	srv.Api(bungo.ApiRoute{
		Path: "zero", Version: "v1", Method: http.MethodGet,
		Handler: func(req *bungo.Request) (bungo.APIResponse, error) {
			return bungo.APIResponse{Body: map[string]bool{"ok": true}}, nil
		},
	})
	srv.Api(bungo.ApiRoute{
		Path: "problem", Version: "v1", Method: http.MethodGet,
		Handler: func(req *bungo.Request) (bungo.APIResponse, error) {
			return bungo.APIResponse{
				StatusCode: http.StatusUnprocessableEntity,
				Body:       map[string]string{"title": "invalid"},
				Headers:    map[string]string{"Content-Type": "application/problem+json"},
			}, nil
		},
	})
	h, err := eng.CreateHandler(srv)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("zero status defaults to 200 JSON", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/zero", nil))
		if rec.Code != http.StatusOK {
			t.Fatal(rec.Code, rec.Body.String())
		}
		if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
			t.Fatalf("Content-Type = %q", ct)
		}
		if got := strings.TrimSpace(rec.Body.String()); got != `{"ok":true}` {
			t.Fatalf("body = %q", got)
		}
	})

	t.Run("handler Content-Type is preserved", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/problem", nil))
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatal(rec.Code, rec.Body.String())
		}
		if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
			t.Fatalf("Content-Type = %q, want the handler's value", ct)
		}
	})
}

func TestCreateHandler_apiHEADOnGETRouteNeverReachesHandler(t *testing.T) {
	dir := mustWebDir(t)
	eng := NewHTTPEngine()
	srv := bungo.NewServer(eng, dir)
	called := false
	srv.Api(bungo.ApiRoute{
		Path: "x", Version: "v1", Method: http.MethodGet,
		Handler: func(req *bungo.Request) (bungo.APIResponse, error) {
			called = true
			return bungo.APIResponse{Body: "ok"}, nil
		},
	})
	h, err := eng.CreateHandler(srv)
	if err != nil {
		t.Fatal(err)
	}

	// ServeMux routes HEAD to "GET /path" patterns; the handler's own method guard
	// is what keeps a GET-only route from running for HEAD.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodHead, "/api/v1/x", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("HEAD on GET route: status %d, want 405", rec.Code)
	}
	if allow := rec.Header().Get("Allow"); allow != http.MethodGet {
		t.Fatalf("Allow = %q, want %q", allow, http.MethodGet)
	}
	if called {
		t.Fatal("handler ran for a method it was not registered for")
	}
}

func TestCreateHandler_every405CarriesAllow(t *testing.T) {
	dir := mustWebDir(t)
	mustWrite(t, filepath.Join(dir, "static", "robots.txt"), "User-agent: *")
	eng := NewHTTPEngine()
	srv := bungo.NewServer(eng, dir)
	srv.SetAssetOptimization(true)
	srv.StaticAlias("/robots.txt", "robots.txt")
	srv.Page(bungo.PageRoute{Path: "/", Template: "home.gohtml"})
	srv.Page(bungo.PageRoute{Path: bungo.NotFoundPath, Template: "home.gohtml"})
	srv.Page(bungo.PageRoute{Path: "/about", Template: "home.gohtml"})
	srv.WebSocket(bungo.WebSocketRoute{Path: "/ws"})
	for _, method := range []string{http.MethodGet, http.MethodDelete} {
		srv.Api(bungo.ApiRoute{
			Path: "x", Version: "v1", Method: method,
			Handler: func(req *bungo.Request) (bungo.APIResponse, error) {
				return bungo.APIResponse{Body: "ok"}, nil
			},
		})
	}
	h, err := eng.CreateHandler(srv)
	if err != nil {
		t.Fatal(err)
	}

	// RFC 9110 §15.5.6: a 405 response MUST list the target's supported methods in Allow.
	tests := []struct {
		name   string
		method string
		path   string
		allow  string
	}{
		{"page", http.MethodPost, "/about", "GET"},
		{"root page behind the not-found dispatcher", http.MethodPost, "/", "GET"},
		{"API path fallback", http.MethodPost, "/api/v1/x", "DELETE, GET"},
		{"API route method guard", http.MethodHead, "/api/v1/x", "DELETE, GET"},
		{"static file", http.MethodPost, "/static/robots.txt", "GET"},
		{"static alias", http.MethodPut, "/robots.txt", "GET"},
		{"bundle", http.MethodPost, "/_bungo/anything.js", "GET"},
		{"websocket", http.MethodPost, "/ws", "GET"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(tt.method, tt.path, nil))
			if rec.Code != http.StatusMethodNotAllowed {
				t.Fatalf("%s %s: status %d, want 405", tt.method, tt.path, rec.Code)
			}
			if got := rec.Header().Get("Allow"); got != tt.allow {
				t.Fatalf("%s %s: Allow = %q, want %q", tt.method, tt.path, got, tt.allow)
			}
		})
	}
}

func TestCreateHandler_staticEdgeCases(t *testing.T) {
	dir := mustWebDir(t)
	png := "\x89PNG\r\n\x1a\n" + strings.Repeat("\x00", 16)
	mustWrite(t, filepath.Join(dir, "static", "logo"), png)
	mustWrite(t, filepath.Join(dir, "static", "app.CSS"), "body{}")

	eng := NewHTTPEngine()
	srv := bungo.NewServer(eng, dir)
	h, err := eng.CreateHandler(srv)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("missing file is 404", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/static/absent.css", nil))
		if rec.Code != http.StatusNotFound {
			t.Fatal(rec.Code, rec.Body.String())
		}
	})

	t.Run("extensionless file is content-sniffed", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/static/logo", nil))
		if rec.Code != http.StatusOK || rec.Body.String() != png {
			t.Fatalf("%d %q", rec.Code, rec.Body.String())
		}
		if ct := rec.Header().Get("Content-Type"); ct != "image/png" {
			t.Fatalf("Content-Type = %q, want sniffed image/png", ct)
		}
	})

	t.Run("extension matched case-insensitively", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/static/app.CSS", nil))
		if ct := rec.Header().Get("Content-Type"); rec.Code != http.StatusOK || !strings.HasPrefix(ct, "text/css") {
			t.Fatalf("%d Content-Type = %q", rec.Code, ct)
		}
	})
}

func TestCreateHandler_bodyReadErrorIs400(t *testing.T) {
	dir := mustWebDir(t)
	eng := NewHTTPEngine()
	srv := bungo.NewServer(eng, dir)
	reached := map[string]bool{}
	srv.Api(bungo.ApiRoute{
		Path: "upload", Version: "v1", Method: http.MethodPost,
		Handler: func(req *bungo.Request) (bungo.APIResponse, error) {
			reached["api"] = true
			return bungo.APIResponse{Body: "ok"}, nil
		},
	})
	srv.Page(bungo.PageRoute{
		Path: "/form", Template: "home.gohtml",
		Handler: func(req *bungo.Request) (map[string]any, error) {
			reached["page"] = true
			return nil, nil
		},
	})
	srv.WebSocket(bungo.WebSocketRoute{
		Path:      "/ws",
		OnConnect: func(conn *bungo.WebSocketConn) { reached["ws"] = true },
	})
	h, err := eng.CreateHandler(srv)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		kind   string
		method string
		path   string
	}{
		{"api", http.MethodPost, "/api/v1/upload"},
		{"page", http.MethodGet, "/form"},
		{"ws", http.MethodGet, "/ws"},
	}
	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			req.Body = failingBody{}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status %d, want 400 (body %q)", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), errBodyRead.Error()) {
				t.Fatalf("body %q should explain the read failure", rec.Body.String())
			}
			if reached[tt.kind] {
				t.Fatalf("%s handler ran despite an unreadable request", tt.kind)
			}
		})
	}
}
