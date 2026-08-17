package engine

import (
	"encoding/json"
	"log"
	"net/http"

	bungo "github.com/piotr-nierobisz/BunGo"
)

// runSecurityLayers executes a route's security layers in order and writes the
// rejection response when one refuses the request.
// Inputs:
// - w: response writer receiving the rejection or misconfiguration response.
// - srv: BunGo server registry containing named security layers.
// - layerNames: layer names attached to the route, executed in order.
// - breq: translated request shared by every layer.
// Outputs:
// - bool: true when every layer passed and the route handler may run.
func (e *HTTPEngine) runSecurityLayers(w http.ResponseWriter, srv *bungo.Server, layerNames []string, breq *bungo.Request) bool {
	for _, layerName := range layerNames {
		layer, ok := srv.SecurityLayers[layerName]
		if !ok {
			log.Printf("BunGo Security Error: Security Layer '%s' was requested but is not registered.", layerName)
			http.Error(w, "Internal Server Error: Application Misconfigured", http.StatusInternalServerError)
			return false
		}
		passed, resp := layer.Handler(breq)
		if passed {
			continue
		}
		if resp == nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return false
		}
		e.writeSecurityRejection(w, resp)
		return false
	}
	return true
}

// applyResponseMeta sets a response's headers and cookies onto w; it must run
// before the status code is written. Headers are applied first so a response
// header can never clobber a Set-Cookie entry.
// Inputs:
// - w: response writer whose header map is populated.
// - resp: response carrying optional Headers and Cookies.
// Outputs:
// - none
func (e *HTTPEngine) applyResponseMeta(w http.ResponseWriter, resp *bungo.APIResponse) {
	for k, v := range resp.Headers {
		w.Header().Set(k, v)
	}
	for _, c := range resp.Cookies {
		if c.Name == "" {
			continue
		}
		http.SetCookie(w, e.cookieConverter(c))
	}
}

// writeSecurityRejection writes a security layer's custom rejection response.
// A zero StatusCode falls back to 401; a nil Body writes headers and status
// only (the redirect case), any other Body is JSON-encoded.
// Inputs:
// - w: response writer receiving the rejection.
// - resp: rejection response returned by the refusing security layer.
// Outputs:
// - none
func (e *HTTPEngine) writeSecurityRejection(w http.ResponseWriter, resp *bungo.APIResponse) {
	e.applyResponseMeta(w, resp)
	status := resp.StatusCode
	if status == 0 {
		status = http.StatusUnauthorized
	}
	if resp.Body == nil {
		w.WriteHeader(status)
		return
	}
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "application/json")
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(resp.Body)
}

// createAPIHandler creates a net/http handler for one configured API route.
// Inputs:
// - srv: BunGo server registry containing security layers and API handler dependencies.
// - route: API route configuration applied by this generated handler closure.
// Outputs:
// - http.HandlerFunc: request handler that enforces method/origin/security and writes JSON responses.
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

		if route.CheckOrigin != nil && !route.CheckOrigin(breq) {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}

		if !e.runSecurityLayers(w, srv, route.SecurityLayer, breq) {
			return
		}

		// Execute Handler
		resp, err := route.Handler(breq)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		e.applyResponseMeta(w, &resp)
		if w.Header().Get("Content-Type") == "" {
			w.Header().Set("Content-Type", "application/json")
		}
		status := resp.StatusCode
		if status == 0 {
			status = http.StatusOK
		}
		w.WriteHeader(status)
		json.NewEncoder(w).Encode(resp.Body)
	}
}

// createPageHandler creates a net/http handler for one configured page route.
// Inputs:
// - srv: BunGo server registry containing templates, layouts, security, and rendering settings.
// - route: page route configuration applied by this generated handler closure.
// - statusCode: HTTP status written with a successful render — 200 for regular pages, 404 for the NotFoundPath page.
// Outputs:
// - http.HandlerFunc: request handler that enforces security and renders HTML responses.
func (e *HTTPEngine) createPageHandler(srv *bungo.Server, route *bungo.PageRoute, statusCode int) http.HandlerFunc {
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

		if !e.runSecurityLayers(w, srv, route.SecurityLayer, breq) {
			return
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
		w.WriteHeader(statusCode)
		w.Write([]byte(htmlOutput))
	}
}
