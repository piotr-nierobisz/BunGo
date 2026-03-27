package main

import (
	"log"
	"time"

	bungo "github.com/piotr-nierobisz/BunGo"
	"github.com/piotr-nierobisz/BunGo/engine"
)

func main() {
	engineInstance := engine.NewHTTPEngine()
	srv := bungo.NewServer(engineInstance, "./web")

	srv.SetDefaultLayout("base.gohtml")
	srv.SetAssetOptimization(false)

	srv.Security(bungo.SecurityLayer{
		Name: "check_jwt_token",
		Handler: func(req *bungo.Request) bool {
			log.Println("Executing security layer: check_jwt_token")
			return true
		},
	})

	srv.Page(bungo.PageRoute{
		Path:     "/",
		Template: "landing.gohtml",
		View:     "landing.jsx",
		Handler: func(req *bungo.Request) (map[string]any, error) {
			// Vendor-agnostic business logic goes here.
			return map[string]any{
				"HeroTitle":    "Welcome to BunGo",
				"HeroSubtitle": "The blazing fast embedded React framework for Go.",
			}, nil
		},
	})

	srv.Page(bungo.PageRoute{
		Path:          "/dashboard",
		Template:      "dashboard.gohtml",
		View:          "dashboard.jsx",
		SecurityLayer: []string{"check_jwt_token"},
		Handler: func(req *bungo.Request) (map[string]any, error) {
			// Vendor-agnostic business logic goes here.
			return map[string]any{
				"Title":       "BunGo Dashboard",
				"UserMessage": "Hello from the Go backend! You are authenticated.",
				"ServerTime":  time.Now().Format(time.RFC1123),
			}, nil
		},
	})

	srv.Page(bungo.PageRoute{
		Path:          "/profile",
		Template:      "profile.gohtml",
		SecurityLayer: []string{"check_jwt_token"},
		Handler: func(req *bungo.Request) (map[string]any, error) {
			// Vendor-agnostic business logic goes here.
			return map[string]any{
				"Username": "jdoe_secure",
				"Role":     "Administrator",
			}, nil
		},
	})

	if err := srv.Serve(3303); err != nil {
		panic(err)
	}
}
