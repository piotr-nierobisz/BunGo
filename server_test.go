package bungo

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewServer_missingLayoutsPanics(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "views"), 0755); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for missing layouts/")
		}
	}()
	NewServer(nil, dir)
}

func TestOptimizedAssetPath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		view string
		want string
	}{
		{"dash.jsx", "/_bungo/dash.js"},
		{`sub\page.tsx`, "/_bungo/sub/page.js"},
		{"/abs.jsx", "/_bungo/abs.js"},
	}
	for _, tt := range tests {
		if got := OptimizedAssetPath(tt.view); got != tt.want {
			t.Errorf("OptimizedAssetPath(%q) = %q; want %q", tt.view, got, tt.want)
		}
	}
}

func TestNewServer_emptyWebDir(t *testing.T) {
	t.Parallel()
	s := NewServer(nil, "")
	if s.WebDir != "" {
		t.Fatal("expected empty WebDir")
	}
	if s.Pages == nil || s.APIs == nil {
		t.Fatal("maps not initialized")
	}
}

func TestServer_resolvePaths(t *testing.T) {
	t.Parallel()
	s := &Server{DefaultLayout: "base.gohtml"}
	route := &PageRoute{Template: "page.gohtml", Layout: "wrap.gohtml"}
	tp, lp := s.ResolvePageTemplatePaths(route)
	if tp != "layouts/page.gohtml" || lp != "layouts/wrap.gohtml" {
		t.Fatalf("explicit layout: %s %s", tp, lp)
	}
	route2 := &PageRoute{Template: "p.gohtml"}
	tp, lp = s.ResolvePageTemplatePaths(route2)
	if tp != "layouts/p.gohtml" || lp != "layouts/base.gohtml" {
		t.Fatalf("default layout: %s %s", tp, lp)
	}
}

func TestServer_ResolvePageScriptAssets(t *testing.T) {
	t.Parallel()
	s := &Server{}
	s.SetAssetOptimization(false)
	inline, mod := s.ResolvePageScriptAssets(&PageRoute{View: "a.jsx"}, map[string]string{"a.jsx": "CODE"})
	if mod != "" || inline != "CODE" {
		t.Fatalf("inline: %q %q", inline, mod)
	}
	s.SetAssetOptimization(true)
	inline, mod = s.ResolvePageScriptAssets(&PageRoute{View: "a.jsx"}, nil)
	if inline != "" || mod != OptimizedAssetPath("a.jsx") {
		t.Fatalf("optimized: %q %q", inline, mod)
	}
	inline, mod = s.ResolvePageScriptAssets(&PageRoute{}, nil)
	if inline != "" || mod != "" {
		t.Fatal("empty view should return empty scripts")
	}
}

func TestServer_Page_Api_Security_integration(t *testing.T) {
	dir := newTestWebTree(t)
	eng := &noopEngine{}
	s := NewServer(eng, dir)

	s.Security(SecurityLayer{Name: "ok", Handler: func(req *Request) bool { return true }})

	s.Page(PageRoute{
		Path:     "/",
		Template: "home.gohtml",
		Handler: func(req *Request) (map[string]any, error) {
			return map[string]any{"Title": "Hi"}, nil
		},
	})

	s.Api(ApiRoute{
		Path:    "hello",
		Version: "v1",
		Method:  "GET",
		Handler: func(req *Request) (APIResponse, error) {
			return APIResponse{StatusCode: 200, Body: map[string]string{"ok": "1"}}, nil
		},
	})

	if len(s.Pages) != 1 || len(s.APIs) != 1 {
		t.Fatalf("routes: pages=%d apis=%d", len(s.Pages), len(s.APIs))
	}
}

func TestServer_SetDefaultLayout(t *testing.T) {
	dir := newTestWebTree(t)
	s := NewServer(&noopEngine{}, dir)
	s.SetDefaultLayout("base.gohtml")
	if s.DefaultLayout != "base.gohtml" {
		t.Fatal(s.DefaultLayout)
	}
	s.SetDefaultLayout("")
	if s.DefaultLayout != "" {
		t.Fatal("clear default")
	}
}

func TestPanics(t *testing.T) {
	dir := newTestWebTree(t)
	s := NewServer(&noopEngine{}, dir)

	t.Run("Page missing template", func(t *testing.T) {
		defer expectPanic(t)
		s.Page(PageRoute{Path: "/"})
	})

	t.Run("Api missing method", func(t *testing.T) {
		defer expectPanic(t)
		s.Api(ApiRoute{Path: "/x", Handler: func(*Request) (APIResponse, error) {
			return APIResponse{}, nil
		}})
	})

	t.Run("Api nil handler", func(t *testing.T) {
		defer expectPanic(t)
		s.Api(ApiRoute{Path: "/x", Method: "GET"})
	})

	t.Run("Api invalid HTTP method", func(t *testing.T) {
		defer expectPanic(t)
		s.Api(ApiRoute{
			Path:    "/x",
			Method:  "FAKE",
			Handler: func(*Request) (APIResponse, error) { return APIResponse{}, nil },
		})
	})
}

func TestServer_Api_methodNormalizedToUppercase(t *testing.T) {
	dir := newTestWebTree(t)
	s := NewServer(&noopEngine{}, dir)
	s.Api(ApiRoute{
		Path:    "r",
		Version: "v1",
		Method:  " post ",
		Handler: func(*Request) (APIResponse, error) {
			return APIResponse{StatusCode: 200, Body: map[string]bool{"ok": true}}, nil
		},
	})
	got, ok := s.APIs["v1:POST:r"]
	if !ok || got.Method != "POST" {
		t.Fatalf("APIs[%q] = %#v, ok=%v", "v1:POST:r", got, ok)
	}
}

func expectPanic(t *testing.T) {
	t.Helper()
	if r := recover(); r == nil {
		t.Fatal("expected panic")
	}
}

// noopEngine satisfies Engine without listening.
type noopEngine struct{}

func (noopEngine) Start(address string, srv *Server) error { return nil }

func newTestWebTree(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, sub := range []string{"layouts", "views"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0755); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []struct {
		path string
		data string
	}{
		{"layouts/home.gohtml", `<!DOCTYPE html><html><head></head><body>{{.Title}}</body></html>`},
		{"layouts/base.gohtml", `{{block "content" .}}{{end}}`},
	} {
		if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(f.path)), []byte(f.data), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}
