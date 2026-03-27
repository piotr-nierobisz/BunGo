package engine

import (
	"net/http"

	bungo "github.com/piotr-nierobisz/BunGo"
)

// HTTPEngine implements Invoker for net/http server
type HTTPEngine struct {
	compiledViews   map[string]string
	optimizedAssets map[string]string
}

// NewHTTPEngine creates an HTTP engine with initialized compiled-view caches.
// Inputs:
// - none
// Outputs:
// - *HTTPEngine: engine instance ready to compile routes and serve requests.
func NewHTTPEngine() *HTTPEngine {
	return &HTTPEngine{
		compiledViews:   make(map[string]string),
		optimizedAssets: make(map[string]string),
	}
}

// Start creates the HTTP handler and starts listening on the provided address.
// Inputs:
// - address: network listen address passed to net/http, for example ":3303".
// - srv: BunGo server registry used to create handlers and route dispatchers.
// Outputs:
// - error: non-nil when handler creation fails or the HTTP server exits with an error.
func (e *HTTPEngine) Start(address string, srv *bungo.Server) error {
	handler, err := e.CreateHandler(srv)
	if err != nil {
		return err
	}

	return http.ListenAndServe(address, handler)
}
