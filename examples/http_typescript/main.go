package main

import (
	"time"

	bungo "github.com/piotr-nierobisz/BunGo"
	"github.com/piotr-nierobisz/BunGo/engine"
)

func main() {
	engineInstance := engine.NewHTTPEngine()
	srv := bungo.NewServer(engineInstance, "./web")

	srv.Page(bungo.PageRoute{
		Path:     "/",
		Template: "index.gohtml",
		View:     "showcase.tsx",
		Handler: func(req *bungo.Request) (map[string]any, error) {
			return map[string]any{
				"PageTitle":    "BunGo TypeScript Demo",
				"CounterStart": 5,
				"GeneratedAt":  time.Now().Format(time.RFC3339),
			}, nil
		},
	})

	if err := srv.Serve(3303); err != nil {
		panic(err)
	}
}
