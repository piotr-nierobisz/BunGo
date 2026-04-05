package engine

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

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
		Handler: func(req *bungo.Request) bool {
			return false
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
	path := bungo.OptimizedAssetPath("v.jsx")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct == "" {
		t.Fatal("missing content type")
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
