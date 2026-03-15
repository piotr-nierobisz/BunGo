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
}

// NewServer initializes a new Server with the given engine and web directory
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

// Page registers a new page route. Template is required; Layout and View are optional.
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

// SetDefaultLayout sets the optional wrapper template used for all pages that do not
// set Layout on their PageRoute. The layout file must exist in webDir/layouts/ when
// webDir is non-empty.
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

// Api registers a new API route
func (s *Server) Api(route ApiRoute) {
	s.APIs[route.Version+":"+route.Method+":"+route.Path] = route
}

// Security registers a new security layer
func (s *Server) Security(layer SecurityLayer) {
	s.SecurityLayers[layer.Name] = layer
}

// Serve starts the server on the specified port by delegating to the Engine
func (s *Server) Serve(port int) error {
	address := fmt.Sprintf(":%d", port)
	return s.Engine.Start(address, s)
}
