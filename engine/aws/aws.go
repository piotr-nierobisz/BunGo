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
	compiledViews map[string]string
}

// NewLambdaEngine returns a new Lambda engine. Use with bungo.NewServer(engine, webDir) then srv.Serve(port), like the HTTP and GCP engines.
func NewLambdaEngine() *LambdaEngine {
	return &LambdaEngine{
		compiledViews: make(map[string]string),
	}
}

// Start implements bungo.Engine. It starts the Lambda runtime (lambda.Start); the address parameter is ignored.
// Use API Gateway HTTP API (v2) or Lambda Function URL. For local testing use AWS SAM CLI or Lambda RIE.
func (e *LambdaEngine) Start(address string, srv *bungo.Server) error {
	invoke, err := e.initHandler(srv)
	if err != nil {
		return err
	}
	lambda.Start(invoke)
	return nil
}

// initHandler compiles views and returns the dispatch function used by both Start (via adapter) and Handler.
func (e *LambdaEngine) initHandler(srv *bungo.Server) (func(context.Context, events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error), error) {
	if srv.WebDir != "" {
		compiledMap, err := builder.CompilePages(srv.Pages, srv.WebDir)
		if err != nil {
			return nil, err
		}
		e.compiledViews = compiledMap
	}
	return func(ctx context.Context, req events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
		return e.dispatch(req, srv)
	}, nil
}

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
					return e.handleAPI(breq, route, srv)
				}
			}
		}
	}

	// Page routes: GET only, exact path
	if method == http.MethodGet {
		if route, ok := srv.Pages[path]; ok {
			return e.handlePage(breq, route, srv)
		}
	}

	return e.response(http.StatusNotFound, "text/plain", "Not Found"), nil
}

func (e *LambdaEngine) handleAPI(breq *bungo.Request, route bungo.ApiRoute, srv *bungo.Server) (events.APIGatewayV2HTTPResponse, error) {
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

func (e *LambdaEngine) handlePage(breq *bungo.Request, route bungo.PageRoute, srv *bungo.Server) (events.APIGatewayV2HTTPResponse, error) {
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
	if route.View != "" {
		inlineJS = e.compiledViews[route.View]
	}
	htmlOutput, err := bungo.RenderTemplate(templatePath, layoutPath, inlineJS, pageData)
	if err != nil {
		return e.response(http.StatusInternalServerError, "text/plain", err.Error()), nil
	}
	return e.response(http.StatusOK, "text/html; charset=utf-8", htmlOutput), nil
}

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

func (e *LambdaEngine) response(statusCode int, contentType, body string) events.APIGatewayV2HTTPResponse {
	return events.APIGatewayV2HTTPResponse{
		StatusCode: statusCode,
		Headers:    map[string]string{"Content-Type": contentType},
		Body:       body,
	}
}

// normalizePath returns the path used for routing (RawPath or Path, with leading /).
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
