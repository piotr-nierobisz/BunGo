package bungo

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Invoker represents the execution environment starting the server.
// We define it locally to avoid import cycle, since engine pkg depends on bungo
type Engine interface {
	Start(address string, srv *Server) error
}

// Server is the central registry for the BunGo application.
type Server struct {
	Pages          map[string]PageRoute
	APIs           map[string]ApiRoute
	SecurityLayers map[string]SecurityLayer
	Engine         Engine
	WebDir         string
	DefaultLayout  string
	optimizeAssets bool
	assetStorage   *AssetStorage
}

// NewServer creates a Server and validates required web directory structure at startup.
// Inputs:
// - engine: runtime engine implementation responsible for serving incoming requests.
// - webDir: base web directory containing required layouts/ and views/ subdirectories.
// Outputs:
// - *Server: initialized server registry with empty route and security maps.
func NewServer(engine Engine, webDir string) *Server {
	if webDir != "" {
		storage := newAssetStorage(webDir, getEmbeddedAssetsFS())

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

		return &Server{
			Pages:          make(map[string]PageRoute),
			APIs:           make(map[string]ApiRoute),
			SecurityLayers: make(map[string]SecurityLayer),
			Engine:         engine,
			WebDir:         webDir,
			assetStorage:   storage,
		}
	}

	return &Server{
		Pages:          make(map[string]PageRoute),
		APIs:           make(map[string]ApiRoute),
		SecurityLayers: make(map[string]SecurityLayer),
		Engine:         engine,
		WebDir:         webDir,
		assetStorage:   newAssetStorage(webDir, getEmbeddedAssetsFS()),
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
	s.APIs[route.Version+":"+route.Method+":"+route.Path] = route
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
