package engine

import (
	"encoding/json"
	"log"
	"net/http"

	bungo "github.com/piotr-nierobisz/BunGo"
)

// createAPIHandler creates a net/http handler for one configured API route.
// Inputs:
// - srv: BunGo server registry containing security layers and API handler dependencies.
// - route: API route configuration applied by this generated handler closure.
// Outputs:
// - http.HandlerFunc: request handler that enforces method/security and writes JSON responses.
func (e *HTTPEngine) createAPIHandler(srv *bungo.Server, route *bungo.ApiRoute) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != route.Method {
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
			} else {
				log.Printf("BunGo Security Error: Security Layer '%s' was requested but is not registered.", layerName)
				http.Error(w, "Internal Server Error: Application Misconfigured", http.StatusInternalServerError)
				return
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

// createPageHandler creates a net/http handler for one configured page route.
// Inputs:
// - srv: BunGo server registry containing templates, layouts, security, and rendering settings.
// - route: page route configuration applied by this generated handler closure.
// Outputs:
// - http.HandlerFunc: request handler that enforces security and renders HTML responses.
func (e *HTTPEngine) createPageHandler(srv *bungo.Server, route *bungo.PageRoute) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
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
			} else {
				log.Printf("BunGo Security Error: Security Layer '%s' was requested but is not registered.", layerName)
				http.Error(w, "Internal Server Error: Application Misconfigured", http.StatusInternalServerError)
				return
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

		templatePath, layoutPath := srv.ResolvePageTemplatePaths(route)
		inlineJS, moduleSrc := srv.ResolvePageScriptAssets(route, e.compiledViews)

		htmlOutput, err := bungo.RenderTemplate(srv.AssetStorage(), templatePath, layoutPath, inlineJS, moduleSrc, pageData)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(htmlOutput))
	}
}
