package main

import (
	"fmt"
	"log"
	"time"

	bungo "github.com/piotr-nierobisz/BunGo"
	"github.com/piotr-nierobisz/BunGo/engine"
)

func main() {
	engineInstance := engine.NewHTTPEngine()
	srv := bungo.NewServer(engineInstance, "./web")

	// Headers emitted on every response, the WebSocket handshake rejections included.
	srv.SetResponseHeaders(map[string]string{
		"X-Content-Type-Options": "nosniff",
	})

	// The chat page: a plain server-rendered template with a vanilla JS client,
	// no React view required.
	srv.Page(bungo.PageRoute{
		Path:     "/",
		Template: "chat.gohtml",
		Handler: func(req *bungo.Request) (map[string]any, error) {
			return map[string]any{"PageTitle": "BunGo WebSocket Chat"}, nil
		},
	})

	// The returned hub is the only handle to this route's connections — capture
	// it wherever you broadcast or publish.
	var hub *bungo.WebSocketHub
	hub = srv.WebSocket(bungo.WebSocketRoute{
		Path: "/ws/chat",
		OnConnect: func(conn *bungo.WebSocketConn) {
			log.Printf("client connected (%d online)", hub.ConnectionCount())
			_ = conn.SendText("welcome — everything you type is broadcast to everyone")
		},
		OnMessage: func(conn *bungo.WebSocketConn, message []byte) {
			hub.BroadcastText(string(message)) // fan each frame out to every client
		},
		OnDisconnect: func(conn *bungo.WebSocketConn) {
			log.Printf("client left (%d online)", hub.ConnectionCount())
		},
	})

	// Server-side pushes work from any goroutine holding the hub.
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for t := range ticker.C {
			hub.BroadcastText(fmt.Sprintf("server time: %s", t.Format(time.RFC3339)))
		}
	}()

	log.Println("BunGo WebSocket chat running at http://localhost:3303")
	if err := srv.Serve(3303); err != nil {
		log.Fatal(err)
	}
}
