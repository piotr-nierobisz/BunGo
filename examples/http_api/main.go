package main

import (
	"log"

	bungo "github.com/piotr-nierobisz/BunGo"
	"github.com/piotr-nierobisz/BunGo/engine"
)

func main() {
	// Engine: chooses how requests are served. HTTPEngine uses net/http.
	engineInstance := engine.NewHTTPEngine()

	// Server with empty web dir: no layouts/views, so this app is API-only.
	// BunGo will not validate or compile any page assets.
	srv := bungo.NewServer(engineInstance, "")

	// Security layers apply to both Page and API routes. Register them by name, then reference in routes.
	srv.Security(bungo.SecurityLayer{
		Name: "require_api_key",
		Handler: func(req *bungo.Request) bool {
			log.Println("Executing security layer: require_api_key")
			// Validate API key from headers, query, or body. Use req.Headers, req.Params, req.Body.
			return true
		},
	})

	srv.Security(bungo.SecurityLayer{
		Name: "ensure_json_body",
		Handler: func(req *bungo.Request) bool {
			log.Println("Executing security layer: ensure_json_body")
			// Check Content-Type: application/json, parse body, store in req.Internal["json_body"] for handlers.
			return true
		},
	})

	// ——— API routes ———
	// Path + Version + Method form the full path: /api/{Version}{Path}, e.g. /api/v1/users.

	srv.Api(bungo.ApiRoute{
		Path:          "/users",
		Version:       "v1",
		Method:        "PUT",
		SecurityLayer: []string{"require_api_key", "ensure_json_body"},
		Handler: func(req *bungo.Request) (bungo.APIResponse, error) {
			return bungo.APIResponse{
				StatusCode: 201,
				Body: map[string]any{
					"message": "User created successfully",
					"user_id": 123,
				},
			}, nil
		},
	})

	srv.Api(bungo.ApiRoute{
		Path:    "/health",
		Version: "v1",
		Method:  "GET",
		Handler: func(req *bungo.Request) (bungo.APIResponse, error) {
			return bungo.APIResponse{
				StatusCode: 200,
				Body:       map[string]any{"status": "ok"},
			}, nil
		},
	})

	srv.Serve(4489)
}
