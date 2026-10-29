package builder

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"testing/iotest"

	"github.com/evanw/esbuild/pkg/api"
	bungo "github.com/piotr-nierobisz/BunGo"
)

// isolateRemoteCache points os.UserCacheDir at a fresh temp directory and clears the
// process-wide in-memory module cache, so tests never touch the developer's real cache.
// It returns the cache directory loadRemoteModule will use.
func isolateRemoteCache(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("LocalAppData", filepath.Join(home, "AppData", "Local"))
	resetRemoteModuleCache()
	t.Cleanup(resetRemoteModuleCache)

	dir, err := getCacheDir()
	if err != nil {
		t.Fatalf("getCacheDir: %v", err)
	}
	if !strings.HasPrefix(dir, home) {
		t.Fatalf("cache dir %q escaped isolated home %q", dir, home)
	}
	return dir
}

func resetRemoteModuleCache() {
	remoteModuleCache.Range(func(key, _ any) bool {
		remoteModuleCache.Delete(key)
		return true
	})
}

// useRemoteHTTPClient swaps the package HTTP client for the duration of the test.
func useRemoteHTTPClient(t *testing.T, client *http.Client) {
	t.Helper()
	previous := remoteHTTPClient
	remoteHTTPClient = client
	t.Cleanup(func() { remoteHTTPClient = previous })
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type remoteRoute struct {
	contentType string
	body        string
	status      int    // zero means 200
	redirect    string // when set, answer 302 to this path (like unpkg pinning a bare package URL)
}

// remoteModuleServer is a local stand-in for a CDN such as esm.sh that records every request.
type remoteModuleServer struct {
	*httptest.Server
	mu         sync.Mutex
	routes     map[string]remoteRoute
	hits       map[string]int
	userAgents []string
}

func newRemoteModuleServer(t *testing.T, routes map[string]remoteRoute) *remoteModuleServer {
	t.Helper()
	s := &remoteModuleServer{routes: routes, hits: make(map[string]int)}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.hits[r.URL.Path]++
		s.userAgents = append(s.userAgents, r.UserAgent())
		route, ok := s.routes[r.URL.Path]
		s.mu.Unlock()

		if !ok {
			http.NotFound(w, r)
			return
		}
		if route.redirect != "" {
			http.Redirect(w, r, route.redirect, http.StatusFound)
			return
		}
		if route.contentType != "" {
			w.Header().Set("Content-Type", route.contentType)
		}
		if route.status != 0 {
			w.WriteHeader(route.status)
		}
		_, _ = io.WriteString(w, route.body)
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *remoteModuleServer) hitCount(path string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hits[path]
}

func (s *remoteModuleServer) requestUserAgents() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.userAgents...)
}

func (s *remoteModuleServer) setRoute(path string, route remoteRoute) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.routes[path] = route
}

// capturedPlugin records the callbacks a plugin registers so they can be invoked directly.
type capturedPlugin struct {
	resolvers map[string]func(api.OnResolveArgs) (api.OnResolveResult, error)
	loaders   map[string]func(api.OnLoadArgs) (api.OnLoadResult, error)
}

func captureCallbacks(plugin api.Plugin) *capturedPlugin {
	c := &capturedPlugin{
		resolvers: make(map[string]func(api.OnResolveArgs) (api.OnResolveResult, error)),
		loaders:   make(map[string]func(api.OnLoadArgs) (api.OnLoadResult, error)),
	}
	plugin.Setup(api.PluginBuild{
		OnResolve: func(options api.OnResolveOptions, callback func(api.OnResolveArgs) (api.OnResolveResult, error)) {
			c.resolvers[options.Namespace+"|"+options.Filter] = callback
		},
		OnLoad: func(options api.OnLoadOptions, callback func(api.OnLoadArgs) (api.OnLoadResult, error)) {
			c.loaders[options.Namespace+"|"+options.Filter] = callback
		},
	})
	return c
}

const (
	absoluteURLResolverKey = "|^https?://"
	remoteRelativeKey      = remoteHTTPNamespace + `|^(\.{1,2}/|/)`
	remoteLoaderKey        = remoteHTTPNamespace + "|^https?://"
)

func (c *capturedPlugin) resolver(t *testing.T, key string) func(api.OnResolveArgs) (api.OnResolveResult, error) {
	t.Helper()
	cb, ok := c.resolvers[key]
	if !ok {
		t.Fatalf("no OnResolve registered for %q; registered: %v", key, keysOf(c.resolvers))
	}
	return cb
}

func (c *capturedPlugin) loader(t *testing.T, key string) func(api.OnLoadArgs) (api.OnLoadResult, error) {
	t.Helper()
	cb, ok := c.loaders[key]
	if !ok {
		t.Fatalf("no OnLoad registered for %q; registered: %v", key, keysOf(c.loaders))
	}
	return cb
}

func keysOf[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func TestInferRemoteLoader(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		url         string
		contentType string
		want        api.Loader
	}{
		{"json content type with params", "https://cdn.test/data", "application/json; charset=utf-8", api.LoaderJSON},
		{"text/json content type", "https://cdn.test/data", "text/json", api.LoaderJSON},
		{"content type wins over extension", "https://cdn.test/mod.ts", " Application/JSON ", api.LoaderJSON},
		{"css content type", "https://cdn.test/style", "text/css", api.LoaderCSS},
		{"ts extension with generic type", "https://cdn.test/mod.ts", "application/javascript", api.LoaderTS},
		{"tsx extension", "https://cdn.test/App.tsx", "", api.LoaderTSX},
		{"jsx extension", "https://cdn.test/App.jsx", "", api.LoaderJSX},
		{"uppercase extension", "https://cdn.test/App.JSX", "", api.LoaderJSX},
		{"css extension", "https://cdn.test/theme.css", "text/plain", api.LoaderCSS},
		{"json extension", "https://cdn.test/config.json", "", api.LoaderJSON},
		{"query string ignored", "https://cdn.test/mod.ts?target=es2022", "", api.LoaderTS},
		{"only last segment counts", "https://cdn.test/pkg.ts/index", "", api.LoaderJS},
		{"mjs defaults to js", "https://cdn.test/mod.mjs", "text/javascript", api.LoaderJS},
		{"no extension defaults to js", "https://esm.sh/date-fns@3.6.0", "", api.LoaderJS},
		{"unparseable url defaults to js", "http://[::1", "", api.LoaderJS},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := inferRemoteLoader(tt.url, tt.contentType); got != tt.want {
				t.Fatalf("inferRemoteLoader(%q, %q) = %v; want %v", tt.url, tt.contentType, got, tt.want)
			}
		})
	}
}

func TestClassifyRemoteReactModule(t *testing.T) {
	t.Parallel()
	tests := []struct {
		url  string
		want string
	}{
		{"https://esm.sh/react", "react"},
		{"https://esm.sh/react@18.2.0", "react"},
		{"https://esm.sh/stable/react@18.2.0/es2022/react.mjs", "react"},
		{"https://esm.sh/react@18.2.0/jsx-runtime", "react/jsx-runtime"},
		{"https://ESM.SH/REACT@18.2.0/JSX-RUNTIME", "react/jsx-runtime"},
		{"https://esm.sh/react-dom@18.2.0", "react-dom"},
		{"https://esm.sh/react-dom@18.2.0/client", "react-dom"},
		{"https://unpkg.com/react-dom/umd/react-dom.production.min.js", "react-dom"},
		{"https://esm.sh/react-router@6.22.0", ""},
		{"https://esm.sh/react-is@18.2.0", ""},
		{"https://esm.sh/preact@10.19.0", ""},
		{"https://esm.sh/date-fns@3.6.0?deps=react@18.2.0", ""},
		{"https://esm.sh/", ""},
		{"https://esm.sh", ""},
		{"http://[::1", ""},

		// Supported CDN layouts: the package is only recognized in package position.
		{"https://unpkg.com/react@18.2.0/umd/react.production.min.js", "react"},
		{"https://cdn.jsdelivr.net/npm/react@18.2.0/+esm", "react"},
		{"https://cdn.jsdelivr.net/npm/react-dom@18.2.0/client/+esm", "react-dom"},
		{"https://esm.run/react@18.2.0", "react"},
		{"https://ga.jspm.io/npm:react@18.2.0/jsx-runtime.js", "react/jsx-runtime"},
		{"https://cdn.skypack.dev/-/react@v17.0.1-yQtfP/dist=es2019,mode=imports/optimized/react.js", "react"},
		{"https://cdn.skypack.dev/pin/react-dom@v17.0.1-oZ1BXZ5O/mode=imports/optimized/react-dom.js", "react-dom"},
		{"https://esm.sh/v135/react-dom@18.2.0/X-ZS9yZWFjdA/es2022/client.js", "react-dom"},
		{"https://esm.sh/react@19.0.0/es2022/jsx-runtime.mjs", "react/jsx-runtime"},
		{"https://esm.sh/*react-dom@18.2.0", "react-dom"},

		// Subpaths map to the matching embedded entry point.
		{"https://esm.sh/react@18.2.0/jsx-dev-runtime", "react/jsx-dev-runtime"},
		{"https://esm.sh/react@18.2.0/es2022/jsx-dev-runtime.development.mjs", "react/jsx-dev-runtime"},
		{"https://unpkg.com/react@18.2.0/cjs/react-jsx-runtime.production.min.js", "react/jsx-runtime"},

		// Everything else is fetched normally.
		{"https://cdn.example.com/libs/react/widget.js", ""},
		{"https://cdn.example.com/react/widget.js", ""},
		{"https://esm.sh/react-dom@18.2.0/server", ""},
		{"https://esm.sh/react@18.2.0/package.json", ""},
		{"https://esm.sh/react@18.2.0/lib/nested/thing.js", ""},
		{"https://esm.sh/@types/react@18.2.0", ""},
		{"https://cdn.jsdelivr.net/gh/someone/react@main/index.js", ""},
	}
	for _, tt := range tests {
		if got := classifyRemoteReactModule(tt.url); got != tt.want {
			t.Errorf("classifyRemoteReactModule(%q) = %q; want %q", tt.url, got, tt.want)
		}
	}
}

func TestResolveEmbeddedReactFromRemoteURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		url       string
		wantPath  string
		wantNS    string
		wantAlias bool
	}{
		{"https://esm.sh/react@18.2.0", "react", "react-ns", true},
		{"https://esm.sh/react@18.2.0/jsx-runtime", "react/jsx-runtime", "react-jsx-ns", true},
		{"https://esm.sh/react@18.2.0/jsx-dev-runtime", "react/jsx-dev-runtime", "react-jsx-dev-ns", true},
		{"https://esm.sh/react-dom@18.2.0/client", "react-dom", "react-dom-ns", true},
		{"https://esm.sh/recharts@2.12.0", "", "", false},
	}
	for _, tt := range tests {
		got, ok := resolveEmbeddedReactFromRemoteURL(tt.url)
		if ok != tt.wantAlias || got.Path != tt.wantPath || got.Namespace != tt.wantNS {
			t.Errorf("resolveEmbeddedReactFromRemoteURL(%q) = (%q, %q, %v); want (%q, %q, %v)",
				tt.url, got.Path, got.Namespace, ok, tt.wantPath, tt.wantNS, tt.wantAlias)
		}
	}
}

// The embedded React namespaces used by the remote plugin must be the ones ReactPlugin serves,
// otherwise aliased CDN React imports would fail to load.
func TestRemoteReactAliasesMatchReactPluginNamespaces(t *testing.T) {
	t.Parallel()
	served := make(map[string]bool)
	for key := range captureCallbacks(ReactPlugin).loaders {
		namespace, _, _ := strings.Cut(key, "|")
		served[namespace] = true
	}
	for _, url := range []string{
		"https://esm.sh/react@18.2.0",
		"https://esm.sh/react@18.2.0/jsx-runtime",
		"https://esm.sh/react@18.2.0/jsx-dev-runtime",
		"https://esm.sh/react-dom@18.2.0",
	} {
		alias, ok := resolveEmbeddedReactFromRemoteURL(url)
		if !ok {
			t.Fatalf("expected %q to alias embedded React", url)
		}
		if !served[alias.Namespace] {
			t.Errorf("%q aliases to namespace %q, which ReactPlugin does not load (has %v)", url, alias.Namespace, served)
		}
	}
}

func TestRemoteImportPlugin_registersCallbacks(t *testing.T) {
	t.Parallel()
	plugin := captureCallbacks(RemoteImportPlugin)
	if len(plugin.resolvers) != 2 || len(plugin.loaders) != 1 {
		t.Fatalf("unexpected registrations: resolvers=%v loaders=%v", keysOf(plugin.resolvers), keysOf(plugin.loaders))
	}
	plugin.resolver(t, absoluteURLResolverKey)
	plugin.resolver(t, remoteRelativeKey)
	plugin.loader(t, remoteLoaderKey)
}

func TestRemoteImportPlugin_resolveAbsoluteURL(t *testing.T) {
	t.Parallel()
	resolve := captureCallbacks(RemoteImportPlugin).resolver(t, absoluteURLResolverKey)

	tests := []struct {
		path     string
		wantPath string
		wantNS   string
	}{
		{"https://cdn.test/lib@1.0.0/index.js", "https://cdn.test/lib@1.0.0/index.js", remoteHTTPNamespace},
		{"http://cdn.test/plain.js", "http://cdn.test/plain.js", remoteHTTPNamespace},
		{"https://esm.sh/react@18.2.0", "react", "react-ns"},
		{"https://esm.sh/react@18.2.0/jsx-runtime", "react/jsx-runtime", "react-jsx-ns"},
		{"https://esm.sh/react-dom@18.2.0/client", "react-dom", "react-dom-ns"},
	}
	for _, tt := range tests {
		got, err := resolve(api.OnResolveArgs{Path: tt.path, Importer: "/project/web/views/app.jsx", Namespace: "file"})
		if err != nil {
			t.Errorf("resolve(%q): %v", tt.path, err)
			continue
		}
		if got.Path != tt.wantPath || got.Namespace != tt.wantNS {
			t.Errorf("resolve(%q) = (%q, %q); want (%q, %q)", tt.path, got.Path, got.Namespace, tt.wantPath, tt.wantNS)
		}
	}

	if _, err := resolve(api.OnResolveArgs{Path: "http://bad host/x.js"}); err == nil || !strings.Contains(err.Error(), "invalid remote import") {
		t.Fatalf("expected invalid remote import error, got %v", err)
	}
}

func TestRemoteImportPlugin_resolveRelativeToRemoteImporter(t *testing.T) {
	t.Parallel()
	resolve := captureCallbacks(RemoteImportPlugin).resolver(t, remoteRelativeKey)
	const importer = "https://cdn.test/pkg@1.0.0/dist/index.js"

	tests := []struct {
		path     string
		wantPath string
		wantNS   string
	}{
		{"./util.js", "https://cdn.test/pkg@1.0.0/dist/util.js", remoteHTTPNamespace},
		{"../package.json", "https://cdn.test/pkg@1.0.0/package.json", remoteHTTPNamespace},
		{"/other@2.0.0/mod.js", "https://cdn.test/other@2.0.0/mod.js", remoteHTTPNamespace},
		// esm.sh modules re-export React through absolute paths; those must hit the embedded copy.
		{"/react@18.2.0/es2022/react.mjs", "react", "react-ns"},
		{"/react-dom@18.2.0/es2022/client.mjs", "react-dom", "react-dom-ns"},
	}
	for _, tt := range tests {
		got, err := resolve(api.OnResolveArgs{Path: tt.path, Importer: importer, Namespace: remoteHTTPNamespace})
		if err != nil {
			t.Errorf("resolve(%q): %v", tt.path, err)
			continue
		}
		if got.Path != tt.wantPath || got.Namespace != tt.wantNS {
			t.Errorf("resolve(%q) = (%q, %q); want (%q, %q)", tt.path, got.Path, got.Namespace, tt.wantPath, tt.wantNS)
		}
	}

	// After a redirect the importer was served from a different URL than it was requested as;
	// relative imports must resolve against the served URL (passed along as plugin data).
	got, err := resolve(api.OnResolveArgs{
		Path:       "./dep.js",
		Importer:   "https://unpkg.test/pkg",
		Namespace:  remoteHTTPNamespace,
		PluginData: remoteModuleData{baseURL: "https://unpkg.test/pkg@1.0.0/dist/index.js"},
	})
	if err != nil || got.Path != "https://unpkg.test/pkg@1.0.0/dist/dep.js" {
		t.Fatalf("resolve against served URL = %q, %v", got.Path, err)
	}

	if _, err := resolve(api.OnResolveArgs{Path: "./x.js", Importer: "http://[::1"}); err == nil || !strings.Contains(err.Error(), "invalid importer URL") {
		t.Fatalf("expected invalid importer error, got %v", err)
	}
	if _, err := resolve(api.OnResolveArgs{Path: "./%zz.js", Importer: importer}); err == nil || !strings.Contains(err.Error(), "invalid remote module path") {
		t.Fatalf("expected invalid module path error, got %v", err)
	}
}

func TestRemoteImportPlugin_load(t *testing.T) {
	isolateRemoteCache(t)
	srv := newRemoteModuleServer(t, map[string]remoteRoute{
		"/widget.tsx": {contentType: "application/javascript", body: "export const Widget = () => null;"},
	})
	load := captureCallbacks(RemoteImportPlugin).loader(t, remoteLoaderKey)

	got, err := load(api.OnLoadArgs{Path: srv.URL + "/widget.tsx", Namespace: remoteHTTPNamespace})
	if err != nil {
		t.Fatal(err)
	}
	if got.Contents == nil || *got.Contents != "export const Widget = () => null;" {
		t.Fatalf("unexpected contents: %v", got.Contents)
	}
	if got.Loader != api.LoaderTSX {
		t.Fatalf("loader = %v; want TSX", got.Loader)
	}
	if data, ok := got.PluginData.(remoteModuleData); !ok || data.baseURL != srv.URL+"/widget.tsx" {
		t.Fatalf("plugin data = %#v; want the served URL as base for relative imports", got.PluginData)
	}

	if _, err := load(api.OnLoadArgs{Path: srv.URL + "/missing.js", Namespace: remoteHTTPNamespace}); err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("expected 404 error, got %v", err)
	}
}

func TestGetCacheFilePaths(t *testing.T) {
	dir := isolateRemoteCache(t)
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Fatalf("cache dir not created: %v", err)
	}
	if filepath.Base(dir) != "remote_modules" || filepath.Base(filepath.Dir(dir)) != "bungo" {
		t.Fatalf("unexpected cache dir layout: %s", dir)
	}

	bodyA, metaA, err := getCacheFilePaths("https://esm.sh/a@1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	bodyAgain, metaAgain, _ := getCacheFilePaths("https://esm.sh/a@1.0.0")
	bodyB, _, _ := getCacheFilePaths("https://esm.sh/a@1.0.1")

	if bodyA != bodyAgain || metaA != metaAgain {
		t.Fatal("cache paths must be stable for the same URL")
	}
	if bodyA == bodyB {
		t.Fatal("different URLs must not share a cache entry")
	}
	if filepath.Dir(bodyA) != dir || filepath.Dir(metaA) != dir {
		t.Fatalf("cache files outside cache dir: %s %s", bodyA, metaA)
	}
	if strings.TrimSuffix(bodyA, ".body") != strings.TrimSuffix(metaA, ".meta.json") {
		t.Fatalf("body and meta should share a key: %s %s", bodyA, metaA)
	}
}

func TestGetCacheDir_fallsBackToTempDirWithoutHome(t *testing.T) {
	if runtime.GOOS == "windows" || runtime.GOOS == "plan9" {
		t.Skip("user cache dir is not derived from HOME on this platform")
	}
	tmp := t.TempDir()
	t.Setenv("HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("TMPDIR", tmp)

	dir, err := getCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(tmp, "bungo", "remote_modules"); dir != want {
		t.Fatalf("cache dir = %q; want %q", dir, want)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Fatalf("fallback cache dir not created: %v", err)
	}
}

func readCacheMeta(t *testing.T, metaPath string) remoteModuleMeta {
	t.Helper()
	raw, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatalf("reading cache meta: %v", err)
	}
	var meta remoteModuleMeta
	if err := json.Unmarshal(raw, &meta); err != nil {
		t.Fatalf("decoding cache meta %q: %v", raw, err)
	}
	return meta
}

// currentFormatMeta encodes cache metadata exactly as writeCachedModule would for body.
func currentFormatMeta(t *testing.T, loader string, moduleURL string, body string) string {
	t.Helper()
	sum := sha256.Sum256([]byte(body))
	raw, err := json.Marshal(remoteModuleMeta{
		Version:    remoteCacheFormatVersion,
		Loader:     loader,
		URL:        moduleURL,
		BodySHA256: hex.EncodeToString(sum[:]),
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestLoadRemoteModule_fetchesAndCaches(t *testing.T) {
	cacheDir := isolateRemoteCache(t)
	const body = "export const n: number = 1;"
	srv := newRemoteModuleServer(t, map[string]remoteRoute{
		"/lib/mod.ts": {contentType: "application/javascript; charset=utf-8", body: body},
	})
	moduleURL := srv.URL + "/lib/mod.ts"

	got, err := loadRemoteModule(moduleURL)
	if err != nil {
		t.Fatal(err)
	}
	if got.contents != body || got.loader != api.LoaderTS || got.url != moduleURL {
		t.Fatalf("got %#v", got)
	}
	if ua := srv.requestUserAgents(); len(ua) != 1 || ua[0] != "BunGo/esbuild-remote-import" {
		t.Fatalf("User-Agent = %q", ua)
	}

	bodyPath, metaPath, err := getCacheFilePaths(moduleURL)
	if err != nil {
		t.Fatal(err)
	}
	if cached, err := os.ReadFile(bodyPath); err != nil || string(cached) != body {
		t.Fatalf("disk body = %q, %v", cached, err)
	}
	if raw, _ := os.ReadFile(metaPath); string(raw) != currentFormatMeta(t, "ts", moduleURL, body) {
		t.Fatalf("disk meta = %s", raw)
	}
	// The loader is persisted by name: esbuild's numeric api.Loader values shift between releases.
	if meta := readCacheMeta(t, metaPath); meta.Loader != "ts" {
		t.Fatalf("loader stored as %q; want its name", meta.Loader)
	}
	// Atomic writes go through temporary files; none may be left behind.
	entries, err := os.ReadDir(cacheDir)
	if err != nil || len(entries) != 2 {
		t.Fatalf("cache dir should hold exactly the body and meta files, got %v (%v)", entries, err)
	}

	again, err := loadRemoteModule(moduleURL)
	if err != nil || again != got {
		t.Fatalf("second load = %#v, %v", again, err)
	}
	if n := srv.hitCount("/lib/mod.ts"); n != 1 {
		t.Fatalf("expected in-memory cache hit, server saw %d requests", n)
	}
}

func TestLoadRemoteModule_diskCacheSurvivesRestart(t *testing.T) {
	isolateRemoteCache(t)
	srv := newRemoteModuleServer(t, map[string]remoteRoute{
		"/config": {contentType: "application/json", body: `{"label":"cached"}`},
	})
	moduleURL := srv.URL + "/config"

	if _, err := loadRemoteModule(moduleURL); err != nil {
		t.Fatal(err)
	}

	// Simulate a new process with the CDN unreachable: memory cache gone, server down.
	resetRemoteModuleCache()
	srv.Close()

	got, err := loadRemoteModule(moduleURL)
	if err != nil {
		t.Fatalf("expected disk cache hit, got %v", err)
	}
	if got.contents != `{"label":"cached"}` || got.loader != api.LoaderJSON || got.url != moduleURL {
		t.Fatalf("got %#v", got)
	}
}

func TestLoadRemoteModule_invalidDiskEntriesAreMisses(t *testing.T) {
	const stale = "export default 'stale';"
	tests := []struct {
		name   string
		noBody bool
		meta   func(moduleURL string) string // nil: meta file absent
	}{
		// The layout written before cache entries were versioned: esbuild's numeric loader (10 was
		// LoaderJS in esbuild 0.27) and no URL or checksum. It must never be trusted.
		{name: "legacy entry with numeric loader", meta: func(string) string { return `{"loader":10}` }},
		{name: "body without meta"},
		{name: "corrupt meta", meta: func(string) string { return "{not json" }},
		{name: "unknown loader name", meta: func(u string) string { return currentFormatMeta(t, "wasm", u, stale) }},
		{name: "different format version", meta: func(u string) string {
			var meta remoteModuleMeta
			if err := json.Unmarshal([]byte(currentFormatMeta(t, "js", u, stale)), &meta); err != nil {
				t.Fatal(err)
			}
			meta.Version++
			raw, _ := json.Marshal(meta)
			return string(raw)
		}},
		{name: "meta without url", meta: func(string) string { return currentFormatMeta(t, "js", "", stale) }},
		{name: "meta describes a different body", meta: func(u string) string { return currentFormatMeta(t, "js", u, "other body") }},
		{name: "meta without body", noBody: true, meta: func(u string) string { return currentFormatMeta(t, "js", u, stale) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolateRemoteCache(t)
			srv := newRemoteModuleServer(t, map[string]remoteRoute{
				"/mod.js": {body: "export default 'fresh';"},
			})
			moduleURL := srv.URL + "/mod.js"
			bodyPath, metaPath, err := getCacheFilePaths(moduleURL)
			if err != nil {
				t.Fatal(err)
			}
			if !tt.noBody {
				if err := os.WriteFile(bodyPath, []byte(stale), 0644); err != nil {
					t.Fatal(err)
				}
			}
			if tt.meta != nil {
				if err := os.WriteFile(metaPath, []byte(tt.meta(moduleURL)), 0644); err != nil {
					t.Fatal(err)
				}
			}

			got, err := loadRemoteModule(moduleURL)
			if err != nil {
				t.Fatal(err)
			}
			if got.contents != "export default 'fresh';" || srv.hitCount("/mod.js") != 1 {
				t.Fatalf("expected a cache miss and refetch, got %#v (hits=%d)", got, srv.hitCount("/mod.js"))
			}
			if cached, _ := os.ReadFile(bodyPath); string(cached) != got.contents {
				t.Fatalf("disk body not refreshed: %q", cached)
			}
			if raw, _ := os.ReadFile(metaPath); string(raw) != currentFormatMeta(t, "js", moduleURL, got.contents) {
				t.Fatalf("disk meta not rewritten in the current format: %s", raw)
			}
		})
	}
}

func TestLoadRemoteModule_validDiskEntryIsHit(t *testing.T) {
	isolateRemoteCache(t)
	useRemoteHTTPClient(t, &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		t.Errorf("unexpected request to %s: entry should come from disk", r.URL)
		return nil, errors.New("unexpected request")
	})})
	const moduleURL = "https://cdn.test/pkg"
	const servedURL = "https://cdn.test/pkg@1.0.0/index.tsx"
	bodyPath, metaPath, err := getCacheFilePaths(moduleURL)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bodyPath, []byte("export const x = 1;"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(metaPath, []byte(currentFormatMeta(t, "tsx", servedURL, "export const x = 1;")), 0644); err != nil {
		t.Fatal(err)
	}

	got, err := loadRemoteModule(moduleURL)
	if err != nil {
		t.Fatal(err)
	}
	if got.contents != "export const x = 1;" || got.loader != api.LoaderTSX || got.url != servedURL {
		t.Fatalf("got %#v", got)
	}
}

func TestLoadRemoteModule_recordsServedURLAfterRedirect(t *testing.T) {
	isolateRemoteCache(t)
	srv := newRemoteModuleServer(t, map[string]remoteRoute{
		"/pkg":                {redirect: "/pkg@1.0.0/index.ts"},
		"/pkg@1.0.0/index.ts": {contentType: "application/javascript", body: "export const v: number = 1;"},
	})
	moduleURL := srv.URL + "/pkg"

	got, err := loadRemoteModule(moduleURL)
	if err != nil {
		t.Fatal(err)
	}
	served := srv.URL + "/pkg@1.0.0/index.ts"
	// The loader follows the served file (.ts), not the extensionless request URL.
	if got.url != served || got.loader != api.LoaderTS {
		t.Fatalf("got url=%q loader=%v; want %q and TS", got.url, got.loader, served)
	}
	_, metaPath, _ := getCacheFilePaths(moduleURL)
	if meta := readCacheMeta(t, metaPath); meta.URL != served {
		t.Fatalf("disk meta url = %q; want %q", meta.URL, served)
	}
}

func TestLoadRemoteModule_transportWithoutResponseRequest(t *testing.T) {
	isolateRemoteCache(t)
	useRemoteHTTPClient(t, &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     http.Header{},
			Body:       io.NopCloser(strings.NewReader("export {};")),
		}, nil
	})})

	got, err := loadRemoteModule("https://cdn.test/mod.js")
	if err != nil {
		t.Fatal(err)
	}
	if got.url != "https://cdn.test/mod.js" {
		t.Fatalf("url = %q; want the requested URL when the transport omits Response.Request", got.url)
	}
}

func TestLoadRemoteModule_replacesCacheEntryAtomically(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows refuses to rename over a file that is open for reading")
	}
	isolateRemoteCache(t)
	srv := newRemoteModuleServer(t, map[string]remoteRoute{
		"/mod.js": {body: "export default 'fresh';"},
	})
	moduleURL := srv.URL + "/mod.js"
	bodyPath, _, err := getCacheFilePaths(moduleURL)
	if err != nil {
		t.Fatal(err)
	}
	const stale = "export default 'stale, but complete';"
	if err := os.WriteFile(bodyPath, []byte(stale), 0644); err != nil {
		t.Fatal(err)
	}

	// Another process is in the middle of reading the old entry while this one refreshes it.
	reader, err := os.Open(bodyPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()

	if _, err := loadRemoteModule(moduleURL); err != nil {
		t.Fatal(err)
	}
	old, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if string(old) != stale {
		t.Fatalf("concurrent reader saw %q; an in-place rewrite must not leak into an open entry", old)
	}
	if fresh, _ := os.ReadFile(bodyPath); string(fresh) != "export default 'fresh';" {
		t.Fatalf("cache entry not replaced: %q", fresh)
	}
}

func TestRemoteLoaderNames(t *testing.T) {
	t.Parallel()
	for name, loader := range remoteLoaderNames {
		if got := remoteLoaderName(loader); got != name {
			t.Errorf("remoteLoaderName(%v) = %q; want %q", loader, got, name)
		}
	}
	// Every loader inferRemoteLoader can choose must survive a disk cache round trip.
	for _, loader := range []api.Loader{api.LoaderJS, api.LoaderJSX, api.LoaderTS, api.LoaderTSX, api.LoaderJSON, api.LoaderCSS} {
		if remoteLoaderName(loader) == "" {
			t.Errorf("loader %v has no cache name", loader)
		}
	}
	if got := remoteLoaderName(api.LoaderBase64); got != "" {
		t.Errorf("loaders never inferred for remote modules have no cache name, got %q", got)
	}
}

func TestWriteCachedModule_bodyFailureWritesNoMeta(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	metaPath := filepath.Join(dir, "entry.meta.json")
	err := writeCachedModule(filepath.Join(dir, "missing", "entry.body"), metaPath, remoteModule{
		contents: "export {};", loader: api.LoaderJS, url: "https://cdn.test/x.js",
	})
	if err == nil {
		t.Fatal("expected body write failure")
	}
	if _, statErr := os.Stat(metaPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("metadata must not be written when the body could not be: %v", statErr)
	}
}

func TestWriteFileAtomic(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	target := filepath.Join(dir, "entry.body")

	for _, content := range []string{"first", "second"} {
		if err := writeFileAtomic(target, []byte(content)); err != nil {
			t.Fatal(err)
		}
		if got, _ := os.ReadFile(target); string(got) != content {
			t.Fatalf("content = %q; want %q", got, content)
		}
	}

	occupied := filepath.Join(dir, "occupied")
	if err := os.MkdirAll(filepath.Join(occupied, "child"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := writeFileAtomic(occupied, []byte("x")); err == nil {
		t.Fatal("expected rename onto a non-empty directory to fail")
	}
	if err := writeFileAtomic(filepath.Join(dir, "missing", "entry.body"), []byte("x")); err == nil {
		t.Fatal("expected failure when the cache directory is missing")
	}

	entries, _ := os.ReadDir(dir)
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	if strings.Join(names, ",") != "entry.body,occupied" {
		t.Fatalf("temporary files left behind: %v", names)
	}
}

func TestLoadRemoteModule_httpErrorIsNotCached(t *testing.T) {
	isolateRemoteCache(t)
	srv := newRemoteModuleServer(t, map[string]remoteRoute{
		"/flaky.js": {status: http.StatusServiceUnavailable, body: "try later"},
	})
	moduleURL := srv.URL + "/flaky.js"

	_, err := loadRemoteModule(moduleURL)
	if err == nil || !strings.Contains(err.Error(), "unexpected status 503") {
		t.Fatalf("expected 503 error, got %v", err)
	}
	bodyPath, _, _ := getCacheFilePaths(moduleURL)
	if _, statErr := os.Stat(bodyPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("failed response must not be written to disk cache: %v", statErr)
	}

	srv.setRoute("/flaky.js", remoteRoute{body: "export const ok = true;"})
	got, err := loadRemoteModule(moduleURL)
	if err != nil || got.contents != "export const ok = true;" {
		t.Fatalf("retry after recovery = %#v, %v", got, err)
	}
	if n := srv.hitCount("/flaky.js"); n != 2 {
		t.Fatalf("expected 2 requests, got %d", n)
	}
}

func TestLoadRemoteModule_transportError(t *testing.T) {
	isolateRemoteCache(t)
	errDial := errors.New("dial refused by test transport")
	useRemoteHTTPClient(t, &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errDial
	})})

	_, err := loadRemoteModule("https://cdn.test/unreachable.js")
	if !errors.Is(err, errDial) || !strings.Contains(err.Error(), "fetching") {
		t.Fatalf("expected wrapped transport error, got %v", err)
	}
}

func TestLoadRemoteModule_bodyReadError(t *testing.T) {
	isolateRemoteCache(t)
	errRead := errors.New("connection reset mid-body")
	useRemoteHTTPClient(t, &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     http.Header{},
			Body:       io.NopCloser(iotest.ErrReader(errRead)),
			Request:    r,
		}, nil
	})})

	moduleURL := "https://cdn.test/truncated.js"
	_, err := loadRemoteModule(moduleURL)
	if !errors.Is(err, errRead) || !strings.Contains(err.Error(), "reading") {
		t.Fatalf("expected wrapped read error, got %v", err)
	}
	if _, cached := remoteModuleCache.Load(moduleURL); cached {
		t.Fatal("failed read must not populate memory cache")
	}
}

func TestLoadRemoteModule_invalidURL(t *testing.T) {
	isolateRemoteCache(t)
	useRemoteHTTPClient(t, &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		t.Errorf("unexpected request to %s", r.URL)
		return nil, errors.New("unexpected request")
	})})

	_, err := loadRemoteModule("http://bad\x7fhost/mod.js")
	if err == nil || !strings.Contains(err.Error(), "creating request") {
		t.Fatalf("expected request construction error, got %v", err)
	}
}

func TestLoadRemoteModule_worksWhenCacheDirUnavailable(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(blocker, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", blocker)
	t.Setenv("XDG_CACHE_HOME", blocker)
	t.Setenv("LocalAppData", blocker)
	resetRemoteModuleCache()
	t.Cleanup(resetRemoteModuleCache)

	if _, err := getCacheDir(); err == nil {
		t.Fatal("expected cache dir creation to fail beneath a regular file")
	}
	if _, _, err := getCacheFilePaths("https://cdn.test/x.js"); err == nil {
		t.Fatal("expected getCacheFilePaths to propagate cache dir error")
	}

	srv := newRemoteModuleServer(t, map[string]remoteRoute{
		"/mod.js": {body: "export const n = 1;"},
	})
	for i := 0; i < 2; i++ {
		got, err := loadRemoteModule(srv.URL + "/mod.js")
		if err != nil || got.contents != "export const n = 1;" {
			t.Fatalf("load %d = %#v, %v", i, got, err)
		}
	}
	if n := srv.hitCount("/mod.js"); n != 1 {
		t.Fatalf("expected memory cache to serve second load, server saw %d requests", n)
	}
}

func writeView(t *testing.T, webDir, name, source string) {
	t.Helper()
	path := filepath.Join(webDir, "views", filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestCompilePages_bundlesRemoteImports(t *testing.T) {
	isolateRemoteCache(t)
	srv := newRemoteModuleServer(t, map[string]remoteRoute{
		"/greet@1.0.0/index.js": {contentType: "application/javascript", body: `import { prefix } from "./lib/prefix.js";
import React from "/react@18.2.0";
export function greet(name) { return React.createElement("p", null, prefix + name); }
`},
		"/greet@1.0.0/lib/prefix.js": {contentType: "application/javascript", body: `export const prefix = "remote-prefix-marker:";`},
		"/config":                    {contentType: "application/json", body: `{"label": "remote-json-marker"}`},
	})

	webDir := t.TempDir()
	writeView(t, webDir, "app.jsx", `import { greet } from "`+srv.URL+`/greet@1.0.0/index.js";
import config from "`+srv.URL+`/config";

function App() {
    return greet(config.label);
}

_bungoRender(App);
`)

	compiled, optimized, err := CompilePages(map[string]bungo.PageRoute{"/": {Path: "/", Template: "app.gohtml", View: "app.jsx"}}, webDir)
	if err != nil {
		t.Fatal(err)
	}
	js := compiled["app.jsx"]
	for _, marker := range []string{"remote-prefix-marker:", "remote-json-marker"} {
		if !strings.Contains(js, marker) {
			t.Errorf("bundle missing %q from remote module", marker)
		}
	}
	if len(optimized) != 1 {
		t.Fatalf("expected one optimized bundle, got %d", len(optimized))
	}
	for _, path := range []string{"/greet@1.0.0/index.js", "/greet@1.0.0/lib/prefix.js", "/config"} {
		if n := srv.hitCount(path); n != 1 {
			t.Errorf("expected exactly one fetch of %s, got %d", path, n)
		}
	}
	if n := srv.hitCount("/react@18.2.0"); n != 0 {
		t.Errorf("remote React import should alias the embedded copy, but it was fetched %d times", n)
	}
}

func TestCompilePages_relativeImportsAfterRedirect(t *testing.T) {
	isolateRemoteCache(t)
	srv := newRemoteModuleServer(t, map[string]remoteRoute{
		// unpkg-style: the bare package URL redirects to the pinned entry file.
		"/pkg":                     {redirect: "/pkg@1.0.0/dist/index.js"},
		"/pkg@1.0.0/dist/index.js": {contentType: "application/javascript", body: `export { label } from "./label.js";`},
		"/pkg@1.0.0/dist/label.js": {contentType: "application/javascript", body: `export const label = "redirect-dep-marker";`},
	})
	webDir := t.TempDir()
	writeView(t, webDir, "app.jsx", `import { label } from "`+srv.URL+`/pkg";
_bungoRender(() => label);
`)
	pages := map[string]bungo.PageRoute{"/": {Path: "/", Template: "app.gohtml", View: "app.jsx"}}

	compile := func(stage string) {
		t.Helper()
		compiled, _, err := CompilePages(pages, webDir)
		if err != nil {
			t.Fatalf("%s: %v", stage, err)
		}
		if !strings.Contains(compiled["app.jsx"], "redirect-dep-marker") {
			t.Fatalf("%s: bundle is missing the module imported relative to the redirected URL", stage)
		}
	}

	compile("network fetch")
	if n := srv.hitCount("/pkg@1.0.0/dist/label.js"); n != 1 {
		t.Fatalf("expected label.js to be fetched relative to the served URL once, got %d", n)
	}

	// From here on the CDN is gone, so the served URL must come back from each cache layer.
	srv.Close()
	compile("memory cache")
	resetRemoteModuleCache()
	compile("disk cache")
}

func TestCompilePages_remoteReactAliasing(t *testing.T) {
	isolateRemoteCache(t)
	srv := newRemoteModuleServer(t, map[string]remoteRoute{
		// A development build of a component library, as esm.sh serves with ?dev.
		"/ui@1.0.0/index.js": {contentType: "application/javascript", body: `import { jsxDEV } from "/react@18.2.0/jsx-dev-runtime";
import { Fragment } from "/react@18.2.0/jsx-runtime";
// esm.sh builds with ?external=react keep bare React specifiers.
import { jsxDEV as externalJSX } from "react/jsx-dev-runtime";
import { badge } from "/libs/react/badge.js";
export const Panel = () => jsxDEV(Fragment, { children: externalJSX("b", { children: badge }) }, "panel");
`},
		// The site's own file that merely lives under a "react" directory: never React itself.
		"/libs/react/badge.js": {contentType: "application/javascript", body: `export const badge = "own-react-dir-marker";`},
	})
	webDir := t.TempDir()
	writeView(t, webDir, "app.jsx", `import { Panel } from "`+srv.URL+`/ui@1.0.0/index.js";
_bungoRender(Panel);
`)

	compiled, _, err := CompilePages(map[string]bungo.PageRoute{"/": {Path: "/", Template: "app.gohtml", View: "app.jsx"}}, webDir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(compiled["app.jsx"], "own-react-dir-marker") || srv.hitCount("/libs/react/badge.js") != 1 {
		t.Fatal("a file under a /react/ directory must be fetched, not replaced by embedded React")
	}
	for _, path := range []string{"/react@18.2.0/jsx-dev-runtime", "/react@18.2.0/jsx-runtime"} {
		if n := srv.hitCount(path); n != 0 {
			t.Errorf("%s should alias the embedded runtime, but was fetched %d times", path, n)
		}
	}
}

func TestCompilePages_remoteImportFailures(t *testing.T) {
	isolateRemoteCache(t)
	srv := newRemoteModuleServer(t, map[string]remoteRoute{})

	tests := []struct {
		name      string
		importURL string
		wantErr   string
	}{
		{"missing remote module", srv.URL + "/does-not-exist@1.0.0", "404"},
		{"malformed remote URL", "http://bad host/mod.js", "invalid remote import"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			webDir := t.TempDir()
			writeView(t, webDir, "broken.jsx", `import thing from "`+tt.importURL+`";
_bungoRender(() => thing);
`)
			_, _, err := CompilePages(map[string]bungo.PageRoute{"/": {Path: "/", Template: "x.gohtml", View: "broken.jsx"}}, webDir)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
			}
		})
	}
}
