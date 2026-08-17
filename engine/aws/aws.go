// Package engine_aws provides an AWS Lambda adapter for the BunGo framework.
// It supports API Gateway HTTP API (v2) and Lambda Function URL payloads.
package engine_aws

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"mime"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	bungo "github.com/piotr-nierobisz/BunGo"
	"github.com/piotr-nierobisz/BunGo/internal/builder"
)

// LambdaCookieConverter serializes a bungo.Cookie into the Set-Cookie header
// string that API Gateway expects on APIGatewayV2HTTPResponse.Cookies.
// Override via SetCookieConverter when custom attribute handling is required.
type LambdaCookieConverter func(bungo.Cookie) string

// LambdaEngine implements bungo.Engine for AWS Lambda (API Gateway HTTP API v2 / Function URL).
type LambdaEngine struct {
	compiledViews   map[string]string
	optimizedAssets map[string]string
	cookieConverter LambdaCookieConverter
}

// NewLambdaEngine creates a Lambda engine with initialized compiled-view caches.
// The constructor injects the engine's default cookie converter; callers can
// replace it with SetCookieConverter to customize Set-Cookie emission.
// Inputs:
// - none
// Outputs:
// - *LambdaEngine: engine instance ready to dispatch API Gateway v2 events.
func NewLambdaEngine() *LambdaEngine {
	return &LambdaEngine{
		compiledViews:   make(map[string]string),
		optimizedAssets: make(map[string]string),
		cookieConverter: DefaultLambdaCookieConverter,
	}
}

// SetCookieConverter overrides the engine's bungo.Cookie → Set-Cookie string mapper.
// A nil converter restores the default implementation.
// Inputs:
// - converter: replacement converter callback, or nil to restore defaults.
// Outputs:
// - none
func (e *LambdaEngine) SetCookieConverter(converter LambdaCookieConverter) {
	if converter == nil {
		e.cookieConverter = DefaultLambdaCookieConverter
		return
	}
	e.cookieConverter = converter
}

// DefaultLambdaCookieConverter renders a bungo.Cookie as a Set-Cookie header
// value via the standard library's net/http.Cookie.String formatting rules.
// Inputs:
// - c: source cookie populated by an API handler.
// Outputs:
// - string: Set-Cookie header value, suitable for APIGatewayV2HTTPResponse.Cookies.
func DefaultLambdaCookieConverter(c bungo.Cookie) string {
	hc := &http.Cookie{
		Name:     c.Name,
		Value:    c.Value,
		Path:     c.Path,
		Domain:   c.Domain,
		Expires:  c.Expires,
		MaxAge:   c.MaxAge,
		Secure:   c.Secure,
		HttpOnly: c.HttpOnly,
	}
	switch c.SameSite {
	case bungo.SameSiteLax:
		hc.SameSite = http.SameSiteLaxMode
	case bungo.SameSiteStrict:
		hc.SameSite = http.SameSiteStrictMode
	case bungo.SameSiteNone:
		hc.SameSite = http.SameSiteNoneMode
	default:
		hc.SameSite = http.SameSiteDefaultMode
	}
	return hc.String()
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
		compiledMap, optimizedMap, err := builder.CompilePagesFromStorage(srv.Pages, srv.AssetStorage())
		if err != nil {
			return nil, err
		}
		e.compiledViews = compiledMap
		e.optimizedAssets = optimizedMap
	}
	return func(ctx context.Context, req events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
		return e.dispatch(ctx, req, srv)
	}, nil
}

// dispatch routes one API Gateway v2 request to BunGo API/page handling paths.
// Inputs:
// - req: API Gateway v2 request event received by Lambda.
// - srv: BunGo server registry containing route maps and rendering configuration.
// Outputs:
// - events.APIGatewayV2HTTPResponse: serialized HTTP response payload for API Gateway.
// - error: non-nil when unexpected runtime dispatch errors occur.
func (e *LambdaEngine) dispatch(ctx context.Context, req events.APIGatewayV2HTTPRequest, srv *bungo.Server) (events.APIGatewayV2HTTPResponse, error) {
	breq := e.translateRequest(ctx, req)
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
		if alias, ok := srv.StaticAliases[path]; ok {
			return e.staticFileResponse(srv, alias), nil
		}

		if strings.HasPrefix(path, "/static/") && srv.AssetStorage().Exists("static") {
			return e.staticFileResponse(srv, strings.TrimPrefix(path, "/static/")), nil
		}

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
		} else {
			log.Printf("BunGo Security Error: Security Layer '%s' was requested but is not registered.", layerName)
			return e.response(http.StatusInternalServerError, "application/json", `{"error":"Internal Server Error: Application Misconfigured"}`), nil
		}
	}
	resp, err := route.Handler(breq)
	if err != nil {
		return e.response(http.StatusInternalServerError, "application/json", fmt.Sprintf(`{"error":%q}`, err.Error())), nil
	}
	body, _ := json.Marshal(resp.Body)
	out := e.response(resp.StatusCode, "application/json", string(body))
	for _, c := range resp.Cookies {
		if c.Name == "" {
			continue
		}
		out.Cookies = append(out.Cookies, e.cookieConverter(c))
	}
	return out, nil
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
		} else {
			log.Printf("BunGo Security Error: Security Layer '%s' was requested but is not registered.", layerName)
			return e.response(http.StatusInternalServerError, "text/html; charset=utf-8", "<html><body>Internal Server Error: Application Misconfigured</body></html>"), nil
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
	templatePath, layoutPath := srv.ResolvePageTemplatePaths(route)
	inlineJS, moduleSrc := srv.ResolvePageScriptAssets(route, e.compiledViews)
	htmlOutput, err := bungo.RenderTemplate(srv.AssetStorage(), templatePath, layoutPath, inlineJS, moduleSrc, pageData)
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
func (e *LambdaEngine) translateRequest(ctx context.Context, req events.APIGatewayV2HTTPRequest) *bungo.Request {
	breq := &bungo.Request{
		Context:  ctx,
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

// staticFileResponse builds a base64-encoded API Gateway v2 response for one static asset.
// Inputs:
// - srv: BunGo server registry providing the asset storage.
// - staticPath: file path relative to webDir/static to resolve and serve.
// Outputs:
// - events.APIGatewayV2HTTPResponse: file response, or a plain 404 when the asset is missing.
func (e *LambdaEngine) staticFileResponse(srv *bungo.Server, staticPath string) events.APIGatewayV2HTTPResponse {
	content, err := srv.AssetStorage().ReadStaticFile(staticPath)
	if err != nil {
		return e.response(http.StatusNotFound, "text/plain", "Not Found")
	}

	contentType := mime.TypeByExtension(filepath.Ext(strings.ToLower(staticPath)))
	if contentType == "" {
		contentType = http.DetectContentType(content)
	}

	return events.APIGatewayV2HTTPResponse{
		StatusCode:      http.StatusOK,
		IsBase64Encoded: true,
		Headers:         map[string]string{"Content-Type": contentType},
		Body:            base64.StdEncoding.EncodeToString(content),
	}
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
