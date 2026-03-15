package engine

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	bungo "github.com/piotr-nierobisz/BunGo"
	"github.com/piotr-nierobisz/BunGo/internal/builder"
)

// HTTPEngine implements Invoker for net/http server
type HTTPEngine struct {
	compiledViews map[string]string
}

// NewHTTPEngine returns a new HTTPEngine instance
func NewHTTPEngine() *HTTPEngine {
	return &HTTPEngine{
		compiledViews: make(map[string]string),
	}
}

// Start creates an http.ServeMux, registers routes, and starts the server
func (e *HTTPEngine) Start(address string, srv *bungo.Server) error {
	// Compile JSX views
	compiledMap, err := builder.CompilePages(srv.Pages, srv.WebDir)
	if err != nil {
		return err
	}
	e.compiledViews = compiledMap

	mux := http.NewServeMux()

	// Serve static assets if "static" directory exists
	staticDir := filepath.Join(srv.WebDir, "static")
	if info, err := os.Stat(staticDir); err == nil && info.IsDir() {
		mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir(staticDir))))
	}

	// Register Pages
	for path, pageRoute := range srv.Pages {
		mux.HandleFunc(path, e.createPageHandler(srv, pageRoute))
	}

	// Register APIs
	for _, apiRoute := range srv.APIs {
		method := apiRoute.Method
		version := apiRoute.Version
		routePath := apiRoute.Path
		if !strings.HasPrefix(routePath, "/") {
			routePath = "/" + routePath
		}
		fullPath := "/api/" + version + routePath
		mux.HandleFunc(fullPath, e.createAPIHandler(srv, apiRoute, method))
	}

	return http.ListenAndServe(address, mux)
}

func (e *HTTPEngine) createAPIHandler(srv *bungo.Server, route bungo.ApiRoute, method string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != method {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		breq, err := e.translateRequest(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Daisy-chained Security Layers
		for _, layerName := range route.SecurityLayer {
			if layer, ok := srv.SecurityLayers[layerName]; ok {
				if !layer.Handler(breq) {
					http.Error(w, "Unauthorized", http.StatusUnauthorized)
					return
				}
			}
		}

		// Execute Handler
		resp, err := route.Handler(breq)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		json.NewEncoder(w).Encode(resp.Body)
	}
}

func (e *HTTPEngine) createPageHandler(srv *bungo.Server, route bungo.PageRoute) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		breq, err := e.translateRequest(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Daisy-chained Security Layers
		for _, layerName := range route.SecurityLayer {
			if layer, ok := srv.SecurityLayers[layerName]; ok {
				if !layer.Handler(breq) {
					http.Error(w, "Unauthorized", http.StatusUnauthorized)
					return
				}
			}
		}

		var pageData map[string]any
		if route.Handler != nil {
			data, err := route.Handler(breq)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			pageData = data
		}

		templatePath := filepath.Join(srv.WebDir, "layouts", route.Template)
		// Resolve layout: per-route Layout overrides DefaultLayout; empty means standalone template.
		layoutPath := ""
		if route.Layout != "" {
			layoutPath = filepath.Join(srv.WebDir, "layouts", route.Layout)
		} else if srv.DefaultLayout != "" {
			layoutPath = filepath.Join(srv.WebDir, "layouts", srv.DefaultLayout)
		}
		var inlineJS string
		if route.View != "" {
			inlineJS = e.compiledViews[route.View]
		}

		htmlOutput, err := bungo.RenderTemplate(templatePath, layoutPath, inlineJS, pageData)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(htmlOutput))
	}
}

func (e *HTTPEngine) translateRequest(r *http.Request) (*bungo.Request, error) {
	breq := &bungo.Request{
		Headers:  make(map[string]string),
		Params:   make(map[string]string),
		Internal: make(map[string]any),
	}

	for k, v := range r.Header {
		if len(v) > 0 {
			breq.Headers[k] = v[0]
		}
	}

	for k, v := range r.URL.Query() {
		if len(v) > 0 {
			breq.Params[k] = v[0]
		}
	}

	if r.Body != nil {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			return nil, err
		}
		breq.Body = body
		r.Body.Close()
	}

	return breq, nil
}
