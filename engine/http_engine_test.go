package engine

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	bungo "github.com/piotr-nierobisz/BunGo"
)

// startResult runs an engine Start call and returns its error, failing the test
// instead of hanging if Start unexpectedly begins serving.
func startResult(t *testing.T, start func() error) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- start() }()
	select {
	case err := <-done:
		return err
	case <-time.After(10 * time.Second):
		t.Fatal("Start did not return; it appears to be serving")
		return nil
	}
}

// brokenViewServer returns a server whose only page references a view that esbuild cannot compile.
func brokenViewServer(t *testing.T, eng bungo.Engine) *bungo.Server {
	t.Helper()
	dir := mustWebDir(t)
	mustWrite(t, filepath.Join(dir, "views", "broken.jsx"), "export default function Broken( { return <div> }")
	srv := bungo.NewServer(eng, dir)
	srv.Page(bungo.PageRoute{Path: "/", Template: "home.gohtml", View: "broken.jsx"})
	return srv
}

func TestDefaultHTTPCookieConverter(t *testing.T) {
	t.Parallel()
	expires := time.Date(2030, time.March, 4, 5, 6, 7, 0, time.UTC)
	base := bungo.Cookie{
		Name: "sid", Value: "v", Path: "/app", Domain: "example.com",
		Expires: expires, MaxAge: 60, Secure: true, HttpOnly: true,
	}
	tests := []struct {
		in       bungo.SameSiteMode
		want     http.SameSite
		wantAttr string
	}{
		{bungo.SameSiteDefault, http.SameSiteDefaultMode, ""},
		{bungo.SameSiteLax, http.SameSiteLaxMode, "SameSite=Lax"},
		{bungo.SameSiteStrict, http.SameSiteStrictMode, "SameSite=Strict"},
		{bungo.SameSiteNone, http.SameSiteNoneMode, "SameSite=None"},
		{bungo.SameSiteMode("bogus"), http.SameSiteDefaultMode, ""},
	}
	for _, tt := range tests {
		c := base
		c.SameSite = tt.in
		got := DefaultHTTPCookieConverter(c)
		if got.SameSite != tt.want {
			t.Errorf("SameSite %q -> %v, want %v", tt.in, got.SameSite, tt.want)
		}
		if got.Name != "sid" || got.Value != "v" || got.Path != "/app" || got.Domain != "example.com" ||
			!got.Expires.Equal(expires) || got.MaxAge != 60 || !got.Secure || !got.HttpOnly {
			t.Errorf("fields not mirrored for SameSite %q: %#v", tt.in, got)
		}
		header := got.String()
		if tt.wantAttr != "" && !strings.Contains(header, tt.wantAttr) {
			t.Errorf("Set-Cookie %q missing %q", header, tt.wantAttr)
		}
		if tt.wantAttr == "" && strings.Contains(header, "SameSite") {
			t.Errorf("Set-Cookie %q should omit SameSite for mode %q", header, tt.in)
		}
	}
}

func TestHTTPEngine_SetCookieConverter_nilRestoresDefault(t *testing.T) {
	dir := mustWebDir(t)
	eng := NewHTTPEngine()
	eng.SetCookieConverter(func(c bungo.Cookie) *http.Cookie {
		return &http.Cookie{Name: "custom-" + c.Name, Value: c.Value}
	})
	eng.SetCookieConverter(nil)

	srv := bungo.NewServer(eng, dir)
	srv.Api(bungo.ApiRoute{
		Path: "set", Version: "v1", Method: http.MethodGet,
		Handler: func(req *bungo.Request) (bungo.APIResponse, error) {
			return bungo.APIResponse{
				Body:    "ok",
				Cookies: []bungo.Cookie{{Name: "sid", Value: "v", SameSite: bungo.SameSiteStrict}},
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
	if len(cookies) != 1 || cookies[0].Name != "sid" || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatalf("nil converter should restore DefaultHTTPCookieConverter: %#v", cookies)
	}
}

func TestHTTPEngine_Start_listenErrorReturned(t *testing.T) {
	t.Parallel()
	eng := NewHTTPEngine()
	srv := bungo.NewServer(eng, mustWebDir(t))
	// Serve(-1) asks the engine to listen on ":-1", which net.Listen rejects before binding.
	err := startResult(t, func() error { return srv.Serve(-1) })
	if err == nil || !strings.Contains(err.Error(), "invalid port") {
		t.Fatalf("expected listen error, got %v", err)
	}
}

func TestHTTPEngine_compileErrorStopsBeforeListening(t *testing.T) {
	eng := NewHTTPEngine()
	srv := brokenViewServer(t, eng)

	h, err := eng.CreateHandler(srv)
	if err == nil || h != nil {
		t.Fatalf("CreateHandler should fail on an uncompilable view: handler=%v err=%v", h, err)
	}

	// The invalid port is a safety net: if compilation were skipped, Start would fail
	// with a listen error instead of blocking, and the assertion below would catch it.
	err = startResult(t, func() error { return eng.Start("127.0.0.1:-1", srv) })
	if err == nil || !strings.Contains(err.Error(), "esbuild") {
		t.Fatalf("Start should surface the compile error before listening, got %v", err)
	}
}
