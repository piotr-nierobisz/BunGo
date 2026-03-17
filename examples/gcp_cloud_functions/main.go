package main

import (
	"log"

	bungo "github.com/piotr-nierobisz/BunGo"
	engine_gcp "github.com/piotr-nierobisz/BunGo/engine/gcp"
)

func main() {
	// 1. Initialize the GCP Engine.
	// The function name must exactly match the entry point passed to gcloud or the Go package function.
	gcpEngine := engine_gcp.NewGCPEngine("BunGoCloudFunction")

	// 2. Initialize the BunGo server with the engine
	srv := bungo.NewServer(gcpEngine, "")

	// 3. Register standard BunGo APIs or Pages.
	srv.Api(bungo.ApiRoute{
		Path:    "/cloud-function",
		Version: "v1",
		Method:  "GET",
		Handler: func(req *bungo.Request) (bungo.APIResponse, error) {
			return bungo.APIResponse{
				StatusCode: 200,
				Body: map[string]any{
					"message": "Hello from BunGo running on GCP Cloud Functions!",
				},
			}, nil
		},
	})

	// 4. "Serve" starts the local GCP functions framework server on the specified port.
	if err := srv.Serve(3303); err != nil {
		log.Fatal(err)
	}
}
