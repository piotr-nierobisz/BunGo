// Package engine_aws provides an AWS Lambda adapter for the BunGo framework.
// It supports API Gateway HTTP API (v2) and Lambda Function URL payloads.
package engine_aws

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	bungo "github.com/piotr-nierobisz/BunGo"
	"github.com/piotr-nierobisz/BunGo/internal/builder"
)

// LambdaEngine implements bungo.Engine for AWS Lambda (API Gateway HTTP API v2 / Function URL).
type LambdaEngine struct {
	compiledViews   map[string]string
	optimizedAssets map[string]string
}

// NewLambdaEngine creates a Lambda engine with initialized compiled-view caches.
// Inputs:
// - none
// Outputs:
// - *LambdaEngine: engine instance ready to dispatch API Gateway v2 events.
func NewLambdaEngine() *LambdaEngine {
	return &LambdaEngine{
		compiledViews:   make(map[string]string),
		optimizedAssets: make(map[string]string),
	}
}

// Start initializes dispatch state and starts the AWS Lambda runtime loop.
// Inputs:
// - address: unused parameter kept to satisfy the shared engine interface.
// - srv: BunGo server registry used for route lookup, security, and rendering.
// Outputs:
// - error: non-nil when initialization fails before starting the runtime.
func (e *LambdaEngine) Start(address string, srv *bungo.Server) error {
	invoke, err := e.initHandler(srv)
	if err != nil {
		return err
	}
	lambda.Start(invoke)
	return nil
}

// initHandler compiles views and returns the Lambda invocation dispatcher closure.
// Inputs:
// - srv: BunGo server registry containing pages, APIs, and web directory configuration.
// Outputs:
// - func(context.Context, events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error): invocation handler closure.
// - error: non-nil when view compilation fails.
func (e *LambdaEngine) initHandler(srv *bungo.Server) (func(context.Context, events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error), error) {
	if srv.WebDir != "" {
		compiledMap, optimizedMap, err := builder.CompilePages(srv.Pages, srv.WebDir)
		if err != nil {
			return nil, err
		}
		e.compiledViews = compiledMap
		e.optimizedAssets = optimizedMap
	}
	return func(ctx context.Context, req events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
		return e.dispatch(req, srv)
	}, nil
}

// dispatch routes one API Gateway v2 request to BunGo API/page handling paths.
// Inputs:
// - req: API Gateway v2 request event received by Lambda.
// - srv: BunGo server registry containing route maps and rendering configuration.
// Outputs:
// - events.APIGatewayV2HTTPResponse: serialized HTTP response payload for API Gateway.
// - error: non-nil when unexpected runtime dispatch errors occur.
func (e *LambdaEngine) dispatch(req events.APIGatewayV2HTTPRequest, srv *bungo.Server) (events.APIGatewayV2HTTPResponse, error) {
	breq := e.translateRequest(req)
	path := normalizePath(req.RawPath, req.RequestContext.HTTP.Path)
	method := req.RequestContext.HTTP.Method

	// API routes: /api/{version}{path}
	if strings.HasPrefix(path, "/api/") {
		rest := strings.TrimPrefix(path, "/api/")
		parts := strings.SplitN(rest, "/", 2)
		if len(parts) >= 1 && parts[0] != "" {
			version := parts[0]
			var subPath string
			if len(parts) == 2 {
				subPath = "/" + parts[1]
			} else {
				subPath = "/"
			}
			pathPart := strings.TrimPrefix(subPath, "/")
			for _, key := range []string{version + ":" + method + ":" + pathPart, version + ":" + method + ":" + subPath} {
				if route, ok := srv.APIs[key]; ok {
					routeRef := route
					return e.handleAPI(breq, &routeRef, srv)
				}
			}
		}
	}

	// Page routes: GET only, exact path
	if method == http.MethodGet {
		if srv.AssetOptimizationEnabled() && strings.HasPrefix(path, "/_bungo/") {
			if js, ok := e.optimizedAssets[path]; ok {
				return events.APIGatewayV2HTTPResponse{
					StatusCode: http.StatusOK,
					Headers: map[string]string{
						"Content-Type":  "application/javascript; charset=utf-8",
						"Cache-Control": "public, max-age=31536000, immutable",
					},
					Body: js,
				}, nil
			}
			return e.response(http.StatusNotFound, "text/plain", "Not Found"), nil
		}
		if route, ok := srv.Pages[path]; ok {
			routeRef := route
			return e.handlePage(breq, &routeRef, srv)
		}
	}

	return e.response(http.StatusNotFound, "text/plain", "Not Found"), nil
}

// handleAPI executes security checks and handler logic for one API route.
// Inputs:
// - breq: translated BunGo request shared across security and handler execution.
// - route: API route configuration providing security layers and handler callback.
// - srv: BunGo server registry providing named security layers.
// Outputs:
// - events.APIGatewayV2HTTPResponse: JSON response envelope for API Gateway.
// - error: non-nil when low-level execution fails unexpectedly.
func (e *LambdaEngine) handleAPI(breq *bungo.Request, route *bungo.ApiRoute, srv *bungo.Server) (events.APIGatewayV2HTTPResponse, error) {
	for _, layerName := range route.SecurityLayer {
		if layer, ok := srv.SecurityLayers[layerName]; ok {
			if !layer.Handler(breq) {
				return e.response(http.StatusUnauthorized, "application/json", `{"error":"Unauthorized"}`), nil
			}
		}
	}
	resp, err := route.Handler(breq)
	if err != nil {
		return e.response(http.StatusInternalServerError, "application/json", fmt.Sprintf(`{"error":%q}`, err.Error())), nil
	}
	body, _ := json.Marshal(resp.Body)
	return e.response(resp.StatusCode, "application/json", string(body)), nil
}

// handlePage executes security checks and template rendering for one page route.
// Inputs:
// - breq: translated BunGo request shared across security and page handler execution.
// - route: page route configuration defining template/layout/view/handler behavior.
// - srv: BunGo server registry containing layout defaults and rendering configuration.
// Outputs:
// - events.APIGatewayV2HTTPResponse: HTML response envelope for API Gateway.
// - error: non-nil when low-level execution fails unexpectedly.
func (e *LambdaEngine) handlePage(breq *bungo.Request, route *bungo.PageRoute, srv *bungo.Server) (events.APIGatewayV2HTTPResponse, error) {
	for _, layerName := range route.SecurityLayer {
		if layer, ok := srv.SecurityLayers[layerName]; ok {
			if !layer.Handler(breq) {
				return e.response(http.StatusUnauthorized, "text/html; charset=utf-8", "<html><body>Unauthorized</body></html>"), nil
			}
		}
	}
	var pageData map[string]any
	if route.Handler != nil {
		data, err := route.Handler(breq)
		if err != nil {
			return e.response(http.StatusInternalServerError, "text/plain", err.Error()), nil
		}
		pageData = data
	}
	templatePath := filepath.Join(srv.WebDir, "layouts", route.Template)
	layoutPath := ""
	if route.Layout != "" {
		layoutPath = filepath.Join(srv.WebDir, "layouts", route.Layout)
	} else if srv.DefaultLayout != "" {
		layoutPath = filepath.Join(srv.WebDir, "layouts", srv.DefaultLayout)
	}
	var inlineJS string
	var moduleSrc string
	if route.View != "" {
		if srv.AssetOptimizationEnabled() {
			moduleSrc = builder.OptimizedAssetPath(route.View)
		} else {
			inlineJS = e.compiledViews[route.View]
		}
	}
	htmlOutput, err := bungo.RenderTemplate(templatePath, layoutPath, inlineJS, moduleSrc, pageData)
	if err != nil {
		return e.response(http.StatusInternalServerError, "text/plain", err.Error()), nil
	}
	return e.response(http.StatusOK, "text/html; charset=utf-8", htmlOutput), nil
}

// translateRequest converts an API Gateway v2 event into a BunGo request value.
// Inputs:
// - req: incoming API Gateway v2 request event.
// Outputs:
// - *bungo.Request: translated request including headers, query params, body, and internal map.
func (e *LambdaEngine) translateRequest(req events.APIGatewayV2HTTPRequest) *bungo.Request {
	breq := &bungo.Request{
		Headers:  make(map[string]string),
		Params:   make(map[string]string),
		Internal: make(map[string]any),
	}
	for k, v := range req.Headers {
		breq.Headers[strings.ToLower(k)] = v
	}
	for k, v := range req.QueryStringParameters {
		breq.Params[k] = v
	}
	if req.Body != "" {
		breq.Body = []byte(req.Body)
	}
	return breq
}

// response builds a minimal API Gateway v2 response with status, content type, and body.
// Inputs:
// - statusCode: HTTP status code value to return.
// - contentType: Content-Type response header value.
// - body: response payload string body.
// Outputs:
// - events.APIGatewayV2HTTPResponse: formatted response object for Lambda return.
func (e *LambdaEngine) response(statusCode int, contentType, body string) events.APIGatewayV2HTTPResponse {
	return events.APIGatewayV2HTTPResponse{
		StatusCode: statusCode,
		Headers:    map[string]string{"Content-Type": contentType},
		Body:       body,
	}
}

// normalizePath returns a routing path preferring RawPath and enforcing leading slash.
// Inputs:
// - rawPath: request raw path from API Gateway event payload.
// - fallbackPath: request context path used when rawPath is empty.
// Outputs:
// - string: normalized route path beginning with "/" and defaulting to "/".
func normalizePath(rawPath, fallbackPath string) string {
	if rawPath != "" {
		if !strings.HasPrefix(rawPath, "/") {
			return "/" + rawPath
		}
		return rawPath
	}
	if fallbackPath != "" && !strings.HasPrefix(fallbackPath, "/") {
		return "/" + fallbackPath
	}
	if fallbackPath == "" {
		return "/"
	}
	return fallbackPath
}
