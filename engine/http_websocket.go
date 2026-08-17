package engine

import (
	"net/http"
	"time"

	"github.com/gorilla/websocket"
	bungo "github.com/piotr-nierobisz/BunGo"
)

const (
	// webSocketWriteWait bounds each outbound frame write, ping and close included.
	webSocketWriteWait = 10 * time.Second
	// webSocketPongWait is the inbound read deadline, extended on every pong.
	webSocketPongWait = 60 * time.Second
	// webSocketPingPeriod is the keepalive ping cadence; it must stay below webSocketPongWait.
	webSocketPingPeriod = 54 * time.Second
	// webSocketDefaultMaxMessageSize caps inbound frames when the route sets no MaxMessageSize.
	webSocketDefaultMaxMessageSize = 1 << 20
)

// writePump owns every write to the socket — queued frames, keepalive pings, and the
// final close frame — so the single-writer contract gorilla requires is never broken.
// It returns when the connection closes; the deferred conn.Close unblocks readLoop.
// Inputs:
// - conn: BunGo connection whose outbound frames and Done signal drive the loop.
// - ws: underlying gorilla connection this pump writes to.
// Outputs:
// - none
func writePump(conn *bungo.WebSocketConn, ws *websocket.Conn) {
	ticker := time.NewTicker(webSocketPingPeriod)
	defer func() {
		ticker.Stop()
		_ = ws.Close() // unblocks the read loop so disconnect cleanup runs
	}()

	write := func(messageType int, data []byte) error {
		_ = ws.SetWriteDeadline(time.Now().Add(webSocketWriteWait))
		return ws.WriteMessage(messageType, data)
	}

	for {
		select {
		case frame := <-conn.Outbound():
			messageType := websocket.TextMessage
			if frame.Binary {
				messageType = websocket.BinaryMessage
			}
			if err := write(messageType, frame.Data); err != nil {
				_ = conn.Close()
				return
			}
		case <-ticker.C:
			if err := write(websocket.PingMessage, nil); err != nil {
				_ = conn.Close()
				return
			}
		case <-conn.Done():
			_ = write(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			return
		}
	}
}

// readLoop consumes client frames until disconnect, dispatching each to the route's OnMessage.
// Inputs:
// - conn: BunGo connection closed when the client goes away or a read fails.
// - ws: underlying gorilla connection this loop reads from.
// - maxMessageSize: inbound frame size limit in bytes.
// - onMessage: optional route callback invoked once per received frame.
// Outputs:
// - none
func readLoop(conn *bungo.WebSocketConn, ws *websocket.Conn, maxMessageSize int64, onMessage func(*bungo.WebSocketConn, []byte)) {
	ws.SetReadLimit(maxMessageSize)
	_ = ws.SetReadDeadline(time.Now().Add(webSocketPongWait))
	ws.SetPongHandler(func(string) error {
		return ws.SetReadDeadline(time.Now().Add(webSocketPongWait))
	})

	for {
		_, message, err := ws.ReadMessage()
		if err != nil {
			_ = conn.Close()
			return
		}
		if onMessage != nil {
			onMessage(conn, message)
		}
	}
}

// createWebSocketHandler creates a net/http handler for one configured WebSocket route.
// Inputs:
// - srv: BunGo server registry containing named security layers.
// - route: WebSocket route configuration applied by this generated handler closure.
// - hub: hub bound to the route that tracks the lifecycle of every connection.
// Outputs:
// - http.HandlerFunc: request handler that enforces security, upgrades, and runs the connection pumps.
func (e *HTTPEngine) createWebSocketHandler(srv *bungo.Server, route *bungo.WebSocketRoute, hub *bungo.WebSocketHub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		breq, err := e.translateRequest(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Daisy-chained Security Layers, before the upgrade so rejections stay plain HTTP.
		if !e.runSecurityLayers(w, srv, route.SecurityLayer, breq) {
			return
		}

		upgrader := websocket.Upgrader{ReadBufferSize: 1024, WriteBufferSize: 1024}
		if route.CheckOrigin != nil {
			upgrader.CheckOrigin = func(*http.Request) bool {
				return route.CheckOrigin(breq)
			}
		}

		ws, upgradeErr := upgrader.Upgrade(w, r, nil)
		if upgradeErr != nil {
			// Upgrade already wrote the HTTP error response.
			return
		}

		conn := bungo.NewWebSocketConn(breq)

		maxMessageSize := route.MaxMessageSize
		if maxMessageSize <= 0 {
			maxMessageSize = webSocketDefaultMaxMessageSize
		}

		hub.Register(conn)
		if route.OnConnect != nil {
			route.OnConnect(conn)
		}

		go writePump(conn, ws)
		readLoop(conn, ws, maxMessageSize, route.OnMessage)

		hub.Unregister(conn)
		if route.OnDisconnect != nil {
			route.OnDisconnect(conn)
		}
	}
}
