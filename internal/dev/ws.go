package dev

import (
	"fmt"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
	bungo "github.com/piotr-nierobisz/BunGo"
)

var httpServerClosed = http.ErrServerClosed

type wsHub struct {
	mu    sync.Mutex
	conns map[*websocket.Conn]struct{}
}

func newWSHub() *wsHub {
	return &wsHub{
		conns: make(map[*websocket.Conn]struct{}),
	}
}

func (h *wsHub) add(conn *websocket.Conn) {
	h.mu.Lock()
	h.conns[conn] = struct{}{}
	h.mu.Unlock()
}

func (h *wsHub) remove(conn *websocket.Conn) {
	h.mu.Lock()
	delete(h.conns, conn)
	h.mu.Unlock()
}

func (h *wsHub) DisconnectAll() {
	h.mu.Lock()
	connections := make([]*websocket.Conn, 0, len(h.conns))
	for conn := range h.conns {
		connections = append(connections, conn)
	}
	h.conns = make(map[*websocket.Conn]struct{})
	h.mu.Unlock()

	for _, conn := range connections {
		_ = conn.Close() // triggers reload client reconnect flow
	}
}

func startDevWebSocketServer(hub *wsHub) *http.Server {
	upgrader := websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(r *http.Request) bool {
			// Dev-only server; allow all origins (runs on localhost)
			return true
		},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/__bungo", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}

		hub.add(conn)
		go readAndDrainWS(conn, hub)
	})

	return &http.Server{
		Addr:    fmt.Sprintf(":%d", bungo.DevWebSocketPort),
		Handler: mux,
	}
}

func readAndDrainWS(conn *websocket.Conn, hub *wsHub) {
	defer func() {
		hub.remove(conn)
		_ = conn.Close()
	}()

	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
	}
}
