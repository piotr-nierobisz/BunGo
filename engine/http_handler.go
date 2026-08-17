package engine

import (
	"mime"
	"net/http"
	"path/filepath"
	"sort"
	"strings"

	bungo "github.com/piotr-nierobisz/BunGo"
	"github.com/piotr-nierobisz/BunGo/internal/builder"
	"github.com/piotr-nierobisz/BunGo/internal/wsbridge"
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
			serveStaticFile(w, r, srv, strings.TrimPrefix(r.URL.Path, "/static/"))
		})
	}

	// Serve registered static aliases at their root URL paths (e.g. /robots.txt).
	for urlPath, staticPath := range srv.StaticAliases {
		staticRef := staticPath
		mux.HandleFunc(urlPath, func(w http.ResponseWriter, r *http.Request) {
			serveStaticFile(w, r, srv, staticRef)
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

	// Register Pages. The root path and the bungo.NotFoundPath sentinel need the
	// combined dispatcher below: pattern "/" is a ServeMux subtree root, so it
	// doubles as the fallback for every otherwise-unmatched path.
	rootRoute, hasRoot := srv.Pages["/"]
	notFoundRoute, hasNotFound := srv.Pages[bungo.NotFoundPath]
	for path, pageRoute := range srv.Pages {
		if path == "/" || path == bungo.NotFoundPath {
			continue
		}
		routeRef := pageRoute
		mux.HandleFunc(path, e.createPageHandler(srv, &routeRef, http.StatusOK))
	}
	switch {
	case hasNotFound:
		notFoundHandler := e.createPageHandler(srv, &notFoundRoute, http.StatusNotFound)
		var rootHandler http.HandlerFunc
		if hasRoot {
			rootHandler = e.createPageHandler(srv, &rootRoute, http.StatusOK)
		}
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/" && rootHandler != nil {
				rootHandler(w, r)
				return
			}
			if r.Method != http.MethodGet {
				// A non-GET to an unknown path is a plain 404, not a page render.
				http.NotFound(w, r)
				return
			}
			notFoundHandler(w, r)
		})
	case hasRoot:
		// Without a not-found page, keep the historic ServeMux behavior: the "/"
		// subtree root also answers every unmatched path.
		mux.HandleFunc("/", e.createPageHandler(srv, &rootRoute, http.StatusOK))
	}

	// Register WebSockets. The hub lives in the core Server's unexported registry;
	// resolve it through the internal bridge, since engines are a separate package.
	for path, wsRoute := range srv.WebSockets {
		routeRef := wsRoute
		hub, _ := wsbridge.HubFor(srv, path).(*bungo.WebSocketHub)
		mux.HandleFunc(path, e.createWebSocketHandler(srv, &routeRef, hub))
	}

	// Register APIs, remembering which methods exist per full path so the /api/
	// fallback below can answer 405 with an Allow header instead of a 404.
	apiMethods := make(map[string][]string)
	for _, apiRoute := range srv.APIs {
		routeRef := apiRoute
		routePath := routeRef.Path
		if !strings.HasPrefix(routePath, "/") {
			routePath = "/" + routePath
		}
		fullPath := "/api/" + routeRef.Version + routePath

		method := strings.ToUpper(routeRef.Method)
		pattern := method + " " + fullPath

		mux.HandleFunc(pattern, e.createAPIHandler(srv, &routeRef))
		apiMethods[fullPath] = append(apiMethods[fullPath], method)
	}

	// Sort once at registration: request handlers must only read this map, so
	// concurrent requests never mutate a shared slice.
	for _, methods := range apiMethods {
		sort.Strings(methods)
	}

	// Unmatched /api/ paths must never fall through to the "/" page subtree:
	// answer 405 when the path exists under other methods, else a JSON 404.
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		if methods, ok := apiMethods[r.URL.Path]; ok {
			w.Header().Set("Allow", strings.Join(methods, ", "))
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"Not Found"}`))
	})

	// Snapshot the global response headers once so a running server never races
	// application code mutating the map; SetResponseHeaders is a pre-Serve call.
	globalHeaders := srv.ResponseHeaders()
	if len(globalHeaders) == 0 {
		return mux, nil
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for k, v := range globalHeaders {
			w.Header().Set(k, v)
		}
		mux.ServeHTTP(w, r)
	}), nil
}

// serveStaticFile writes one GET response for a static asset resolved through AssetStorage.
// Inputs:
// - w: response writer receiving the file contents or error status.
// - r: incoming request; non-GET methods are rejected with 405.
// - srv: BunGo server registry providing the asset storage.
// - staticPath: file path relative to webDir/static to resolve and serve.
// Outputs:
// - none
func serveStaticFile(w http.ResponseWriter, r *http.Request, srv *bungo.Server, staticPath string) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	content, err := srv.AssetStorage().ReadStaticFile(staticPath)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	ext := filepath.Ext(strings.ToLower(staticPath))
	contentType := mime.TypeByExtension(ext)
	if contentType == "" {
		contentType = http.DetectContentType(content)
	}

	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(content)
}
