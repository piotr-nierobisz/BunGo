package bungo

import (
	"fmt"
	"net/http"
	"path"
	"path/filepath"
	"strings"

	"github.com/piotr-nierobisz/BunGo/internal/wsbridge"
)

// init wires the engine-facing WebSocket hub lookup. Engines live in separate
// packages and cannot read the Server's unexported hub registry directly, so they
// resolve a route's hub through wsbridge; application code has no access to that
// internal package and reaches a hub only via Server.WebSocket's return value.
func init() {
	wsbridge.HubFor = func(srv any, path string) any {
		if hub, ok := srv.(*Server).webSocketHubs[path]; ok {
			return hub
		}
		return nil // untyped nil so an unknown path is a nil interface, not a typed-nil pointer
	}
}

// Invoker represents the execution environment starting the server.
// We define it locally to avoid import cycle, since engine pkg depends on bungo
type Engine interface {
	Start(address string, srv *Server) error
}

// NotFoundPath is the sentinel PageRoute.Path that registers the custom
// not-found page. Register it like any other page — Template, Layout, View,
// Handler, and SecurityLayer all work:
//
//	srv.Page(bungo.PageRoute{Path: bungo.NotFoundPath, Template: "not_found.gohtml"})
//
// Engines render this page with HTTP 404 for any unmatched non-API GET path
// instead of letting the request fall through to the "/" subtree root. The
// value is deliberately not a valid URL path, so it can never collide with a
// real route.
const NotFoundPath = "bungo:404"

// Server is the central registry for the BunGo application.
type Server struct {
	Pages           map[string]PageRoute
	APIs            map[string]ApiRoute
	WebSockets      map[string]WebSocketRoute
	SecurityLayers  map[string]SecurityLayer
	StaticAliases   map[string]string // root URL path -> file path relative to webDir/static
	Engine          Engine
	WebDir          string
	DefaultLayout   string
	optimizeAssets  bool
	assetStorage    *AssetStorage
	webSocketHubs   map[string]*WebSocketHub
	responseHeaders map[string]string
}

// NewServer creates a Server and validates required web directory structure at startup.
// Inputs:
// - engine: runtime engine implementation responsible for serving incoming requests.
// - webDir: base web directory containing required layouts/ and views/ subdirectories.
// Outputs:
// - *Server: initialized server registry with empty route and security maps.
func NewServer(engine Engine, webDir string) *Server {
	storage := newAssetStorage(webDir, getEmbeddedAssetsFS())

	if webDir != "" {
		// Fail-fast architecture check
		if !storage.Exists("") {
			panic(fmt.Sprintf("BunGo Startup Error: Base web directory '%s' does not exist.", webDir))
		}
		if !storage.Exists("layouts") {
			panic(fmt.Sprintf("BunGo Startup Error: 'layouts' subdirectory must exist inside '%s'.", webDir))
		}
		if !storage.Exists("views") {
			panic(fmt.Sprintf("BunGo Startup Error: 'views' subdirectory must exist inside '%s'.", webDir))
		}
	}

	return &Server{
		Pages:          make(map[string]PageRoute),
		APIs:           make(map[string]ApiRoute),
		WebSockets:     make(map[string]WebSocketRoute),
		SecurityLayers: make(map[string]SecurityLayer),
		StaticAliases:  make(map[string]string),
		Engine:         engine,
		WebDir:         webDir,
		assetStorage:   storage,
		webSocketHubs:  make(map[string]*WebSocketHub),
	}
}

// Page registers a page route and validates referenced template, layout, and view files.
// Inputs:
// - route: page route configuration to store in the server route registry.
// Outputs:
// - none
func (s *Server) Page(route PageRoute) {
	if route.Template == "" {
		panic("BunGo Routing Error: PageRoute.Template is required and cannot be empty.")
	}
	if !s.assetStorage.Exists(s.pageTemplatePath(&route)) {
		panic(fmt.Sprintf("BunGo Routing Error: Template file '%s' does not exist in the defined layouts directory.", route.Template))
	}

	if route.Layout != "" {
		if !s.assetStorage.Exists(s.pageLayoutPath(&route)) {
			panic(fmt.Sprintf("BunGo Routing Error: Layout file '%s' does not exist in the defined layouts directory.", route.Layout))
		}
	}

	if route.View != "" {
		if !s.assetStorage.Exists(s.pageViewPath(&route)) {
			panic(fmt.Sprintf("BunGo Routing Error: View file '%s' does not exist in the defined views directory.", route.View))
		}
	}

	s.Pages[route.Path] = route
}

// SetDefaultLayout sets the default layout file used when a page route omits Layout.
// Inputs:
// - path: layout filename in webDir/layouts, or empty string to clear the default.
// Outputs:
// - none
func (s *Server) SetDefaultLayout(path string) {
	if path == "" {
		s.DefaultLayout = ""
		return
	}
	if s.WebDir != "" {
		if !s.assetStorage.Exists("layouts/" + path) {
			panic(fmt.Sprintf("BunGo Routing Error: DefaultLayout file '%s' does not exist in the defined layouts directory.", path))
		}
	}
	s.DefaultLayout = path
}

// SetResponseHeaders sets headers emitted on every response — pages, APIs, static
// files, and pre-upgrade WebSocket responses alike — the place for global security
// headers such as Content-Security-Policy or Strict-Transport-Security. A
// per-response APIResponse.Headers entry overrides a same-named global header.
// The map is copied, and engines snapshot it at startup: call this before Serve.
// Inputs:
// - headers: header name → value map applied to every response; nil or empty clears the global set.
// Outputs:
// - none
func (s *Server) SetResponseHeaders(headers map[string]string) {
	copied := make(map[string]string, len(headers))
	for k, v := range headers {
		copied[k] = v
	}
	s.responseHeaders = copied
}

// ResponseHeaders returns the global response header set for engines to apply.
// Inputs:
// - none
// Outputs:
// - map[string]string: headers configured via SetResponseHeaders; treat as read-only.
func (s *Server) ResponseHeaders() map[string]string {
	return s.responseHeaders
}

// SetAssetOptimization toggles static module delivery for compiled page view bundles.
// Inputs:
// - enabled: true to serve view bundles via /_bungo/*.js, false to inline module code.
// Outputs:
// - none
func (s *Server) SetAssetOptimization(enabled bool) {
	s.optimizeAssets = enabled
}

// AssetOptimizationEnabled reports whether static module bundle delivery is enabled.
// Inputs:
// - none
// Outputs:
// - bool: true when SetAssetOptimization enabled external /_bungo bundle serving.
func (s *Server) AssetOptimizationEnabled() bool {
	return s.optimizeAssets
}

// AssetStorage returns the server storage abstraction for memory-first and disk-fallback web asset access.
// Inputs:
// - none
// Outputs:
// - *AssetStorage: server asset storage used by engines, template rendering, and builders.
func (s *Server) AssetStorage() *AssetStorage {
	return s.assetStorage
}

// Api registers an API route in the server route registry.
// Inputs:
// - route: API route configuration keyed by version, method, and path.
// Outputs:
// - none
func (s *Server) Api(route ApiRoute) {
	method := validateHTTPMethod(route.Method)
	if route.Handler == nil {
		panic(fmt.Sprintf("BunGo Routing Error: ApiRoute.Handler cannot be nil (Method: '%s', Path: '%s').", method, route.Path))
	}
	route.Method = method
	s.APIs[route.Version+":"+route.Method+":"+route.Path] = route
}

// StaticAlias publishes one file from webDir/static at an additional root URL path.
// The alias URL must carry the same file extension as the target static file, so a
// URL never misrepresents the content type it serves (e.g. "/robots.txt" may only
// map onto a ".txt" file). Registration is fail-fast: the target file must already
// exist in the static directory.
// Inputs:
// - urlPath: absolute URL path to publish, for example "/robots.txt" or "/sitemap.xml".
// - staticPath: target file path relative to webDir/static, for example "robots.txt" or "seo/sitemap.xml".
// Outputs:
// - none
func (s *Server) StaticAlias(urlPath string, staticPath string) {
	if !strings.HasPrefix(urlPath, "/") {
		panic(fmt.Sprintf("BunGo Routing Error: StaticAlias urlPath '%s' must start with '/'.", urlPath))
	}
	for _, reserved := range []string{"/static/", "/api/", "/_bungo/"} {
		if strings.HasPrefix(urlPath, reserved) {
			panic(fmt.Sprintf("BunGo Routing Error: StaticAlias urlPath '%s' must not use the reserved '%s' prefix.", urlPath, reserved))
		}
	}

	cleanStatic := strings.TrimPrefix(strings.TrimSpace(staticPath), "/")
	urlExt := path.Ext(urlPath)
	fileExt := path.Ext(cleanStatic)
	if urlExt == "" {
		panic(fmt.Sprintf("BunGo Routing Error: StaticAlias urlPath '%s' must end with a file extension.", urlPath))
	}
	if !strings.EqualFold(urlExt, fileExt) {
		panic(fmt.Sprintf("BunGo Routing Error: StaticAlias urlPath '%s' extension does not match static file '%s'.", urlPath, staticPath))
	}

	if !s.assetStorage.Exists(path.Join("static", cleanStatic)) {
		panic(fmt.Sprintf("BunGo Routing Error: StaticAlias file '%s' does not exist in the static directory.", staticPath))
	}

	s.StaticAliases[urlPath] = cleanStatic
}

// validateHTTPMethod trims whitespace, uppercases the method, and panics when it is empty or not a supported standard HTTP verb.
// Inputs:
// - method: raw ApiRoute.Method value (for example "GET", "get", or " Post ").
// Outputs:
// - string: canonical method string matching net/http.Request.Method (uppercase).
func validateHTTPMethod(method string) string {
	m := strings.TrimSpace(method)
	if m == "" {
		panic("BunGo Routing Error: ApiRoute.Method is required and cannot be empty.")
	}
	u := strings.ToUpper(m)
	switch u {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch,
		http.MethodDelete, http.MethodConnect, http.MethodOptions, http.MethodTrace:
		return u
	default:
		panic(fmt.Sprintf("BunGo Routing Error: ApiRoute.Method %q is not a valid HTTP method (use a standard verb such as GET, POST, PUT, PATCH, DELETE).", method))
	}
}

// WebSocket registers a WebSocket route and returns the hub that fans messages out
// to its connections. The returned hub is the only handle to the route's hub — hold
// onto it (capture it in a closure, or store it) wherever you need to broadcast or
// publish; there is no lookup-a-hub-by-path accessor to fat-finger.
// Inputs:
// - route: WebSocket route configuration with path, security layers, and lifecycle callbacks.
// Outputs:
// - *WebSocketHub: hub bound to the route, usable from any handler or goroutine to broadcast or publish.
func (s *Server) WebSocket(route WebSocketRoute) *WebSocketHub {
	if !strings.HasPrefix(route.Path, "/") {
		panic(fmt.Sprintf("BunGo Routing Error: WebSocketRoute.Path '%s' must start with '/'.", route.Path))
	}
	if _, exists := s.WebSockets[route.Path]; exists {
		panic(fmt.Sprintf("BunGo Routing Error: WebSocketRoute.Path '%s' is already registered.", route.Path))
	}

	hub := newWebSocketHub()
	s.WebSockets[route.Path] = route
	s.webSocketHubs[route.Path] = hub
	return hub
}

// Security registers a named security layer in the server security registry.
// Inputs:
// - layer: reusable security layer definition with name and handler function.
// Outputs:
// - none
func (s *Server) Security(layer SecurityLayer) {
	s.SecurityLayers[layer.Name] = layer
}

// Serve starts server execution on the provided port using the configured engine.
// Inputs:
// - port: TCP port number used to build the engine listen address.
// Outputs:
// - error: non-nil when the engine fails to start or returns a runtime error.
func (s *Server) Serve(port int) error {
	address := fmt.Sprintf(":%d", port)
	return s.Engine.Start(address, s)
}

// ResolvePageTemplatePaths returns template and layout asset paths relative to the configured web root.
// Inputs:
// - route: page route whose template and optional layout should be resolved.
// Outputs:
// - string: required template asset path relative to web root.
// - string: optional layout asset path relative to web root, or empty when no layout applies.
func (s *Server) ResolvePageTemplatePaths(route *PageRoute) (string, string) {
	templatePath := s.pageTemplatePath(route)
	layoutPath := ""
	if route.Layout != "" {
		layoutPath = s.pageLayoutPath(route)
	} else if s.DefaultLayout != "" {
		layoutPath = "layouts/" + s.DefaultLayout
	}
	return templatePath, layoutPath
}

// ResolvePageScriptAssets resolves inline/module script values for one page route render.
// Inputs:
// - route: page route whose optional view determines script asset injection values.
// - compiledViews: map of compiled view source keyed by original route View value.
// Outputs:
// - string: inline JavaScript payload when asset optimization is disabled.
// - string: module source URL when asset optimization is enabled.
func (s *Server) ResolvePageScriptAssets(route *PageRoute, compiledViews map[string]string) (string, string) {
	if route.View == "" {
		return "", ""
	}
	if s.AssetOptimizationEnabled() {
		return "", OptimizedAssetPath(route.View)
	}
	return compiledViews[route.View], ""
}

// pageTemplatePath converts a page route template name into a web-root relative path.
// Inputs:
// - route: page route providing the template filename.
// Outputs:
// - string: `layouts/...` relative asset path for the page template.
func (s *Server) pageTemplatePath(route *PageRoute) string {
	return "layouts/" + route.Template
}

// pageLayoutPath converts a page route layout name into a web-root relative path.
// Inputs:
// - route: page route providing the optional layout filename.
// Outputs:
// - string: `layouts/...` relative asset path for the page layout.
func (s *Server) pageLayoutPath(route *PageRoute) string {
	return "layouts/" + route.Layout
}

// pageViewPath converts a page route view name into a web-root relative path.
// Inputs:
// - route: page route providing the optional view filename.
// Outputs:
// - string: `views/...` relative asset path for the page view entry.
func (s *Server) pageViewPath(route *PageRoute) string {
	return "views/" + route.View
}

// OptimizedAssetPath converts a route view path into the optimized `/_bungo/*.js` route.
// Inputs:
// - view: page route view path relative to `views/`.
// Outputs:
// - string: optimized JavaScript asset route path.
func OptimizedAssetPath(view string) string {
	withoutExt := strings.TrimSuffix(view, filepath.Ext(view))
	normalized := strings.ReplaceAll(withoutExt, "\\", "/")
	normalized = strings.TrimPrefix(normalized, "/")
	return "/_bungo/" + normalized + ".js"
}
