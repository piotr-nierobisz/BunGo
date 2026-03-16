package main

import (
	"log"

	bungo "github.com/piotr-nierobisz/BunGo"
	"github.com/piotr-nierobisz/BunGo/engine"
)

func main() {
	engineInstance := engine.NewHTTPEngine()
	srv := bungo.NewServer(engineInstance, "./web")

	srv.Page(bungo.PageRoute{
		Path:     "/",
		Template: "index.gohtml",
		View:     "loader.jsx",
		Handler: func(req *bungo.Request) (map[string]any, error) {
			log.Println("Handling page route: /")
			// Vendor-agnostic business logic goes here.
			return map[string]any{
				"AssetUrl": "/api/v1/constants",
			}, nil
		},
	})

	srv.Security(bungo.SecurityLayer{
		Name: "security_check",
		Handler: func(req *bungo.Request) bool {
			log.Println("Executing security layer: security_check")
			authHeader := req.Headers["Authorization"]
			return authHeader == "pork-up"
		},
	})

	srv.Api(bungo.ApiRoute{
		Path:          "/constants",
		Version:       "v1",
		Method:        "GET",
		SecurityLayer: []string{"security_check"},
		Handler: func(req *bungo.Request) (bungo.APIResponse, error) {
			log.Println("Handling API route: /api/v1/constants")
			return bungo.APIResponse{
				StatusCode: 200,
				Body: map[string]any{
					"url": "/static/john.webp",
				},
			}, nil
		},
	})

	if err := srv.Serve(3303); err != nil {
		panic(err)
	}
}
