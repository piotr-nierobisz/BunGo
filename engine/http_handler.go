package engine

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	bungo "github.com/piotr-nierobisz/BunGo"
	"github.com/piotr-nierobisz/BunGo/internal/builder"
)

// CreateHandler builds an HTTP handler mux with static, page, and API routes.
// Inputs:
// - srv: BunGo server registry containing pages, APIs, security layers, and web directory settings.
// Outputs:
// - http.Handler: configured mux that dispatches all registered BunGo HTTP routes.
// - error: non-nil when view compilation fails before route registration.
func (e *HTTPEngine) CreateHandler(srv *bungo.Server) (http.Handler, error) {
	// Compile JSX views
	compiledMap, optimizedMap, err := builder.CompilePages(srv.Pages, srv.WebDir)
	if err != nil {
		return nil, err
	}
	e.compiledViews = compiledMap
	e.optimizedAssets = optimizedMap

	mux := http.NewServeMux()

	// Serve static assets if "static" directory exists
	if srv.WebDir != "" {
		staticDir := filepath.Join(srv.WebDir, "static")
		if info, err := os.Stat(staticDir); err == nil && info.IsDir() {
			mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir(staticDir))))
		}
	}

	if srv.AssetOptimizationEnabled() {
		mux.HandleFunc("/_bungo/", func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet {
				http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
				return
			}
			if js, ok := e.optimizedAssets[r.URL.Path]; ok {
				w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(js))
				return
			}
			http.NotFound(w, r)
		})
	}

	// Register Pages
	for path, pageRoute := range srv.Pages {
		routeRef := pageRoute
		mux.HandleFunc(path, e.createPageHandler(srv, &routeRef))
	}

	// Register APIs
	for _, apiRoute := range srv.APIs {
		routeRef := apiRoute
		routePath := routeRef.Path
		if !strings.HasPrefix(routePath, "/") {
			routePath = "/" + routePath
		}
		fullPath := "/api/" + routeRef.Version + routePath
		mux.HandleFunc(fullPath, e.createAPIHandler(srv, &routeRef))
	}

	return mux, nil
}
