package main

import (
	bungo "github.com/piotr-nierobisz/BunGo"
	"github.com/piotr-nierobisz/BunGo/engine"
)

func main() {
	engineInstance := engine.NewHTTPEngine()
	srv := bungo.NewServer(engineInstance, "./web")

	srv.Page(bungo.PageRoute{
		Path:     "/",
		Template: "index.gohtml",
		View:     "chart.jsx",
		Handler: func(req *bungo.Request) (map[string]any, error) {
			return map[string]any{
				"Points": []map[string]any{
					{"date": "2026-03-10", "users": 42},
					{"date": "2026-03-11", "users": 56},
					{"date": "2026-03-12", "users": 61},
					{"date": "2026-03-13", "users": 48},
					{"date": "2026-03-14", "users": 73},
					{"date": "2026-03-15", "users": 69},
				},
			}, nil
		},
	})

	if err := srv.Serve(3303); err != nil {
		panic(err)
	}
}
