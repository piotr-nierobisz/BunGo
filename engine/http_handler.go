package engine

import (
	"mime"
	"net/http"
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
	compiledMap, optimizedMap, err := builder.CompilePagesFromStorage(srv.Pages, srv.AssetStorage())
	if err != nil {
		return nil, err
	}
	e.compiledViews = compiledMap
	e.optimizedAssets = optimizedMap

	mux := http.NewServeMux()

	// Serve static assets from memory-first storage when static directory exists.
	if srv.AssetStorage().Exists("static") {
		mux.HandleFunc("/static/", func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet {
				http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
				return
			}

			requestPath := strings.TrimPrefix(r.URL.Path, "/static/")
			content, err := srv.AssetStorage().ReadStaticFile(requestPath)
			if err != nil {
				http.NotFound(w, r)
				return
			}

			ext := filepath.Ext(strings.ToLower(requestPath))
			contentType := mime.TypeByExtension(ext)
			if contentType == "" {
				contentType = http.DetectContentType(content)
			}

			w.Header().Set("Content-Type", contentType)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(content)
		})
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

		pattern := strings.ToUpper(routeRef.Method) + " " + fullPath

		mux.HandleFunc(pattern, e.createAPIHandler(srv, &routeRef))
	}

	return mux, nil
}
