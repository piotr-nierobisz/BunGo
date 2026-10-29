package bungo

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/piotr-nierobisz/BunGo/internal/wsbridge"
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
		view       string
		wantPrefix string
	}{
		{"dash.jsx", "/_bungo/dash."},
		{`sub\page.tsx`, "/_bungo/sub/page."},
		{"/abs.jsx", "/_bungo/abs."},
	}
	hashRe := regexp.MustCompile(`\.[0-9a-f]{8}\.js$`)
	for _, tt := range tests {
		got := OptimizedAssetPath(tt.view, "console.log(1)")
		if !strings.HasPrefix(got, tt.wantPrefix) || !hashRe.MatchString(got) {
			t.Errorf("OptimizedAssetPath(%q) = %q; want prefix %q and an 8-hex hash segment", tt.view, got, tt.wantPrefix)
		}
	}

	same := OptimizedAssetPath("dash.jsx", "console.log(1)")
	if same != OptimizedAssetPath("dash.jsx", "console.log(1)") {
		t.Fatal("hash must be deterministic for identical content")
	}
	if same == OptimizedAssetPath("dash.jsx", "console.log(2)") {
		t.Fatal("hash must change when the compiled content changes")
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
	inline, mod = s.ResolvePageScriptAssets(&PageRoute{View: "a.jsx"}, map[string]string{"a.jsx": "CODE"})
	if inline != "" || mod != OptimizedAssetPath("a.jsx", "CODE") {
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

	s.Security(SecurityLayer{Name: "ok", Handler: func(req *Request) (bool, *APIResponse) { return true, nil }})

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

func TestServer_StaticAlias(t *testing.T) {
	dir := newTestWebTree(t)
	if err := os.MkdirAll(filepath.Join(dir, "static", "seo"), 0755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"static/robots.txt", "static/seo/sitemap.xml"} {
		if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(f)), []byte("data"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	s := NewServer(&noopEngine{}, dir)

	s.StaticAlias("/robots.txt", "robots.txt")
	s.StaticAlias("/sitemap.xml", "/seo/sitemap.xml")

	if s.StaticAliases["/robots.txt"] != "robots.txt" {
		t.Fatalf("robots alias: %q", s.StaticAliases["/robots.txt"])
	}
	if s.StaticAliases["/sitemap.xml"] != "seo/sitemap.xml" {
		t.Fatalf("sitemap alias not normalized: %q", s.StaticAliases["/sitemap.xml"])
	}

	t.Run("urlPath missing leading slash", func(t *testing.T) {
		defer expectPanic(t)
		s.StaticAlias("robots.txt", "robots.txt")
	})

	t.Run("urlPath missing extension", func(t *testing.T) {
		defer expectPanic(t)
		s.StaticAlias("/robots", "robots.txt")
	})

	t.Run("extension mismatch", func(t *testing.T) {
		defer expectPanic(t)
		s.StaticAlias("/robots.xml", "robots.txt")
	})

	t.Run("missing static file", func(t *testing.T) {
		defer expectPanic(t)
		s.StaticAlias("/humans.txt", "humans.txt")
	})

	t.Run("reserved prefix", func(t *testing.T) {
		defer expectPanic(t)
		s.StaticAlias("/static/robots.txt", "robots.txt")
	})

	// layouts/home.gohtml exists in the web root and the extensions match, so only the
	// static-dir containment check stands between these aliases and the template source.
	for _, target := range []string{"../layouts/home.gohtml", `..\layouts\home.gohtml`, "seo/../../layouts/home.gohtml"} {
		t.Run("target escaping static dir "+target, func(t *testing.T) {
			msg := panicMessage(t, func() { s.StaticAlias("/home.gohtml", target) })
			if !strings.Contains(msg, "inside the static directory") {
				t.Fatalf("unexpected panic: %q", msg)
			}
			if _, registered := s.StaticAliases["/home.gohtml"]; registered {
				t.Fatal("escaping alias was registered")
			}
		})
	}

	t.Run("backslash target normalized", func(t *testing.T) {
		s.StaticAlias("/sitemap-win.xml", `seo\sitemap.xml`)
		if got := s.StaticAliases["/sitemap-win.xml"]; got != "seo/sitemap.xml" {
			t.Fatalf("alias target = %q, want slash-normalized seo/sitemap.xml", got)
		}
	})
}

func TestServer_WebSocket_registration(t *testing.T) {
	dir := newTestWebTree(t)
	s := NewServer(&noopEngine{}, dir)

	hub := s.WebSocket(WebSocketRoute{Path: "/ws"})
	if hub == nil {
		t.Fatal("expected hub")
	}
	// The hub is developer-facing only through this return value; the engine reaches
	// the same instance through the internal bridge, which apps cannot import.
	if got, _ := wsbridge.HubFor(s, "/ws").(*WebSocketHub); got != hub {
		t.Fatal("bridge should resolve the registration hub")
	}
	if wsbridge.HubFor(s, "/other") != nil {
		t.Fatal("unknown path should resolve to a nil hub")
	}

	t.Run("path missing leading slash", func(t *testing.T) {
		defer expectPanic(t)
		s.WebSocket(WebSocketRoute{Path: "ws"})
	})

	t.Run("duplicate path", func(t *testing.T) {
		defer expectPanic(t)
		s.WebSocket(WebSocketRoute{Path: "/ws"})
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

// panicMessage runs fn and returns its recovered panic message, failing the test when fn returns normally.
func panicMessage(t *testing.T, fn func()) (msg string) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic")
		}
		msg = fmt.Sprint(r)
	}()
	fn()
	return ""
}

// recordingEngine captures the arguments Serve hands to the engine and returns a canned error.
type recordingEngine struct {
	address string
	srv     *Server
	err     error
}

func (e *recordingEngine) Start(address string, srv *Server) error {
	e.address = address
	e.srv = srv
	return e.err
}

func TestNewServer_startupValidationPanics(t *testing.T) {
	t.Parallel()
	missingRoot := filepath.Join(t.TempDir(), "absent")
	noViews := t.TempDir()
	mustMk(t, filepath.Join(noViews, "layouts"))

	tests := []struct {
		name string
		dir  string
		want string
	}{
		{"missing web dir", missingRoot, "Base web directory"},
		{"missing views dir", noViews, "'views' subdirectory"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := panicMessage(t, func() { NewServer(nil, tt.dir) })
			if !strings.Contains(msg, tt.want) || !strings.Contains(msg, tt.dir) {
				t.Fatalf("panic %q should mention %q and the web dir %q", msg, tt.want, tt.dir)
			}
		})
	}
}

func TestServer_Page_validatesReferencedFiles(t *testing.T) {
	t.Parallel()
	dir := newTestWebTree(t)
	mustWrite(t, filepath.Join(dir, "views", "home.jsx"), []byte("export default function Home(){return null}"))
	s := NewServer(&noopEngine{}, dir)

	tests := []struct {
		name  string
		route PageRoute
		want  string
	}{
		{"missing template file", PageRoute{Path: "/a", Template: "absent.gohtml"}, "Template file 'absent.gohtml'"},
		{"missing layout file", PageRoute{Path: "/b", Template: "home.gohtml", Layout: "absent.gohtml"}, "Layout file 'absent.gohtml'"},
		{"missing view file", PageRoute{Path: "/c", Template: "home.gohtml", View: "absent.jsx"}, "View file 'absent.jsx'"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := panicMessage(t, func() { s.Page(tt.route) })
			if !strings.Contains(msg, tt.want) {
				t.Fatalf("panic %q should name the missing file (%q)", msg, tt.want)
			}
			if _, registered := s.Pages[tt.route.Path]; registered {
				t.Fatalf("route %q registered despite failed validation", tt.route.Path)
			}
		})
	}

	s.Page(PageRoute{Path: "/ok", Template: "home.gohtml", Layout: "base.gohtml", View: "home.jsx"})
	if got, ok := s.Pages["/ok"]; !ok || got.Layout != "base.gohtml" || got.View != "home.jsx" {
		t.Fatalf("valid route with layout and view not registered: %#v ok=%v", got, ok)
	}
}

func TestServer_SetDefaultLayout_validation(t *testing.T) {
	t.Parallel()
	s := NewServer(&noopEngine{}, newTestWebTree(t))
	s.SetDefaultLayout("base.gohtml")

	msg := panicMessage(t, func() { s.SetDefaultLayout("absent.gohtml") })
	if !strings.Contains(msg, "absent.gohtml") {
		t.Fatalf("panic should name the missing layout: %q", msg)
	}
	if s.DefaultLayout != "base.gohtml" {
		t.Fatalf("failed SetDefaultLayout must keep the previous default, got %q", s.DefaultLayout)
	}

	// Without a web directory there is nothing to validate against, so any name is accepted.
	bare := NewServer(nil, "")
	bare.SetDefaultLayout("shell.gohtml")
	if bare.DefaultLayout != "shell.gohtml" {
		t.Fatalf("DefaultLayout = %q", bare.DefaultLayout)
	}
}

func TestServer_SetResponseHeaders(t *testing.T) {
	t.Parallel()
	s := NewServer(nil, "")
	if len(s.ResponseHeaders()) != 0 {
		t.Fatalf("new server should have no global headers: %#v", s.ResponseHeaders())
	}

	in := map[string]string{"X-Frame-Options": "DENY"}
	s.SetResponseHeaders(in)
	// The server keeps its own copy: later caller mutations must not leak into responses.
	in["X-Frame-Options"] = "SAMEORIGIN"
	in["X-Late"] = "1"
	got := s.ResponseHeaders()
	if len(got) != 1 || got["X-Frame-Options"] != "DENY" {
		t.Fatalf("ResponseHeaders = %#v, want only the snapshot taken at SetResponseHeaders", got)
	}

	s.SetResponseHeaders(nil)
	if len(s.ResponseHeaders()) != 0 {
		t.Fatalf("nil should clear global headers: %#v", s.ResponseHeaders())
	}
}

func TestServer_AssetStorage_readsWebDir(t *testing.T) {
	t.Parallel()
	s := NewServer(&noopEngine{}, newTestWebTree(t))
	b, err := s.AssetStorage().ReadFile("layouts/home.gohtml")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "{{.Title}}") {
		t.Fatalf("unexpected template contents: %q", b)
	}
}

func TestServer_Serve_delegatesToEngine(t *testing.T) {
	t.Parallel()
	errListen := errors.New("listen failed")
	eng := &recordingEngine{err: errListen}
	s := NewServer(eng, "")

	if err := s.Serve(8080); !errors.Is(err, errListen) {
		t.Fatalf("Serve must return the engine error, got %v", err)
	}
	if eng.address != ":8080" {
		t.Fatalf("engine address = %q, want \":8080\"", eng.address)
	}
	if eng.srv != s {
		t.Fatal("engine must receive the serving Server")
	}
}
