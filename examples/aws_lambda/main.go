package main

import (
	"log"

	bungo "github.com/piotr-nierobisz/BunGo"
	engine_aws "github.com/piotr-nierobisz/BunGo/engine/aws"
)

func main() {
	// 1. Initialize the Lambda engine.
	awsEngine := engine_aws.NewLambdaEngine()

	// 2. Initialize the BunGo server with the engine.
	srv := bungo.NewServer(awsEngine, "")

	// 3. Register standard BunGo APIs or Pages.
	srv.Api(bungo.ApiRoute{
		Path:    "/hello",
		Version: "v1",
		Method:  "GET",
		Handler: func(req *bungo.Request) (bungo.APIResponse, error) {
			return bungo.APIResponse{
				StatusCode: 200,
				Body: map[string]any{
					"message": "Hello from BunGo running on AWS Lambda!",
				},
			}, nil
		},
	})

	// 4. "Serve" starts the Lambda runtime (use API Gateway HTTP API v2 or Function URL).
	if err := srv.Serve(3303); err != nil {
		log.Fatal(err)
	}
}
