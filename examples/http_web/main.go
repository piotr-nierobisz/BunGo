package main

import (
	"log"
	"time"

	bungo "github.com/piotr-nierobisz/BunGo"
	"github.com/piotr-nierobisz/BunGo/engine"
)

func main() {
	// Engine: chooses how requests are served (e.g. net/http, Lambda, Cloud Run).
	// HTTPEngine uses standard library net/http and compiles JSX from views/ at startup.
	engineInstance := engine.NewHTTPEngine()

	// Server: central registry for routes, security layers, and web dir (layouts/, views/, optional static/).
	srv := bungo.NewServer(engineInstance, "./web")

	// Optional: shared base layout. Page templates define {{define "content"}} and are rendered
	// inside base.gohtml's {{block "content" .}}, so you don't repeat <html><head><body> per page.
	srv.SetDefaultLayout("base.gohtml")

	// Security layers: reusable middleware. Chain them by name on routes (Pages or APIs).
	srv.Security(bungo.SecurityLayer{
		Name: "check_jwt_token",
		Handler: func(req *bungo.Request) bool {
			log.Println("Executing security layer: check_jwt_token")
			return true
		},
	})

	// ——— Page routes ———
	// Each page has a required Template (layouts/*.gohtml) and optional View (views/*.jsx, compiled by BunGo).

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

	if err := srv.Serve(3348); err != nil {
		panic(err)
	}
}
