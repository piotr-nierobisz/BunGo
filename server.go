package bungo

import (
	"fmt"
	"os"
	"path/filepath"
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
}

// NewServer creates a Server and validates required web directory structure at startup.
// Inputs:
// - engine: runtime engine implementation responsible for serving incoming requests.
// - webDir: base web directory containing required layouts/ and views/ subdirectories.
// Outputs:
// - *Server: initialized server registry with empty route and security maps.
func NewServer(engine Engine, webDir string) *Server {
	if webDir != "" {
		// Fail-fast architecture check
		if _, err := os.Stat(webDir); os.IsNotExist(err) {
			panic(fmt.Sprintf("BunGo Startup Error: Base web directory '%s' does not exist.", webDir))
		}

		if _, err := os.Stat(filepath.Join(webDir, "layouts")); os.IsNotExist(err) {
			panic(fmt.Sprintf("BunGo Startup Error: 'layouts' subdirectory must exist inside '%s'.", webDir))
		}

		if _, err := os.Stat(filepath.Join(webDir, "views")); os.IsNotExist(err) {
			panic(fmt.Sprintf("BunGo Startup Error: 'views' subdirectory must exist inside '%s'.", webDir))
		}
	}

	return &Server{
		Pages:          make(map[string]PageRoute),
		APIs:           make(map[string]ApiRoute),
		SecurityLayers: make(map[string]SecurityLayer),
		Engine:         engine,
		WebDir:         webDir,
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
	if _, err := os.Stat(filepath.Join(s.WebDir, "layouts", route.Template)); os.IsNotExist(err) {
		panic(fmt.Sprintf("BunGo Routing Error: Template file '%s' does not exist in the defined layouts directory.", route.Template))
	}

	if route.Layout != "" {
		if _, err := os.Stat(filepath.Join(s.WebDir, "layouts", route.Layout)); os.IsNotExist(err) {
			panic(fmt.Sprintf("BunGo Routing Error: Layout file '%s' does not exist in the defined layouts directory.", route.Layout))
		}
	}

	if route.View != "" {
		if _, err := os.Stat(filepath.Join(s.WebDir, "views", route.View)); os.IsNotExist(err) {
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
		if _, err := os.Stat(filepath.Join(s.WebDir, "layouts", path)); os.IsNotExist(err) {
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
