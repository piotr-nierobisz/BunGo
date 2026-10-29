package engine

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	bungo "github.com/piotr-nierobisz/BunGo"
)

// startWebSocketTestServer builds a live test server for one configured BunGo server.
func startWebSocketTestServer(t *testing.T, srv *bungo.Server, eng *HTTPEngine) (*httptest.Server, string) {
	t.Helper()
	h, err := eng.CreateHandler(srv)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)
	return ts, "ws" + strings.TrimPrefix(ts.URL, "http")
}

// readText reads one text frame with a deadline so a broken pump fails fast instead of hanging.
func readText(t *testing.T, conn *websocket.Conn) string {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	return string(msg)
}

func TestWebSocket_echoBroadcastAndLifecycle(t *testing.T) {
	dir := mustWebDir(t)
	eng := NewHTTPEngine()
	srv := bungo.NewServer(eng, dir)

	disconnected := make(chan struct{})
	hub := srv.WebSocket(bungo.WebSocketRoute{
		Path: "/ws/feed",
		OnConnect: func(conn *bungo.WebSocketConn) {
			_ = conn.SendText("welcome")
		},
		OnMessage: func(conn *bungo.WebSocketConn, message []byte) {
			_ = conn.SendText("echo:" + string(message))
		},
		OnDisconnect: func(conn *bungo.WebSocketConn) {
			close(disconnected)
		},
	})

	_, wsURL := startWebSocketTestServer(t, srv, eng)
	conn, _, err := websocket.DefaultDialer.Dial(wsURL+"/ws/feed", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	if got := readText(t, conn); got != "welcome" {
		t.Fatalf("OnConnect frame: %q", got)
	}
	if hub.ConnectionCount() != 1 {
		t.Fatalf("connection count: %d", hub.ConnectionCount())
	}

	if err := conn.WriteMessage(websocket.TextMessage, []byte("ping")); err != nil {
		t.Fatal(err)
	}
	if got := readText(t, conn); got != "echo:ping" {
		t.Fatalf("echo frame: %q", got)
	}

	hub.BroadcastText("news")
	if got := readText(t, conn); got != "news" {
		t.Fatalf("broadcast frame: %q", got)
	}

	if err := hub.BroadcastJSON(map[string]int{"revision": 7}); err != nil {
		t.Fatal(err)
	}
	if got := readText(t, conn); got != `{"revision":7}` {
		t.Fatalf("json frame: %q", got)
	}

	conn.Close()
	select {
	case <-disconnected:
	case <-time.After(5 * time.Second):
		t.Fatal("OnDisconnect never fired")
	}
	// Unregistration races the callback by a hair; poll briefly.
	for i := 0; i < 100 && hub.ConnectionCount() != 0; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	if hub.ConnectionCount() != 0 {
		t.Fatalf("connection count after disconnect: %d", hub.ConnectionCount())
	}
}

func TestWebSocket_binaryRoundTrip(t *testing.T) {
	dir := mustWebDir(t)
	eng := NewHTTPEngine()
	srv := bungo.NewServer(eng, dir)

	received := make(chan []byte, 1)
	srv.WebSocket(bungo.WebSocketRoute{
		Path: "/ws",
		OnMessage: func(conn *bungo.WebSocketConn, message []byte) {
			received <- message
			_ = conn.Send([]byte{0xCA, 0xFE})
		},
	})

	_, wsURL := startWebSocketTestServer(t, srv, eng)
	conn, _, err := websocket.DefaultDialer.Dial(wsURL+"/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	if err := conn.WriteMessage(websocket.BinaryMessage, []byte{0x00, 0x01, 0x02}); err != nil {
		t.Fatal(err)
	}
	select {
	case msg := <-received:
		if string(msg) != "\x00\x01\x02" {
			t.Fatalf("server received %v", msg)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("OnMessage never fired")
	}

	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	frameType, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	if frameType != websocket.BinaryMessage || string(msg) != "\xCA\xFE" {
		t.Fatalf("client received type=%d msg=%v", frameType, msg)
	}
}

func TestWebSocket_publishFromCapturedHub(t *testing.T) {
	dir := mustWebDir(t)
	eng := NewHTTPEngine()
	srv := bungo.NewServer(eng, dir)

	// The canonical usage: the hub is reachable only through WebSocket's return value.
	// Capture it, subscribe each connection in OnConnect, and publish to it from
	// elsewhere (here the test body stands in for an API handler) — there is no
	// hub-by-path lookup to reach for instead.
	subscribed := make(chan struct{})
	var hub *bungo.WebSocketHub
	hub = srv.WebSocket(bungo.WebSocketRoute{
		Path: "/ws/feed",
		OnConnect: func(conn *bungo.WebSocketConn) {
			hub.Subscribe(conn, "user:42")
			close(subscribed)
		},
	})

	_, wsURL := startWebSocketTestServer(t, srv, eng)
	conn, _, err := websocket.DefaultDialer.Dial(wsURL+"/ws/feed", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	select {
	case <-subscribed:
	case <-time.After(5 * time.Second):
		t.Fatal("OnConnect never ran")
	}

	if err := hub.PublishJSON("user:42", map[string]any{"kind": "change"}); err != nil {
		t.Fatal(err)
	}
	if got := readText(t, conn); got != `{"kind":"change"}` {
		t.Fatalf("publish frame: %q", got)
	}
}

func TestWebSocket_topicsReachOnlySubscribers(t *testing.T) {
	dir := mustWebDir(t)
	eng := NewHTTPEngine()
	srv := bungo.NewServer(eng, dir)

	hub := srv.WebSocket(bungo.WebSocketRoute{Path: "/ws"})
	// Subscribe by a client-driven convention: first message names the topic.
	route := srv.WebSockets["/ws"]
	route.OnMessage = func(conn *bungo.WebSocketConn, message []byte) {
		hub.Subscribe(conn, string(message))
		_ = conn.SendText("subscribed:" + string(message))
	}
	srv.WebSockets["/ws"] = route

	_, wsURL := startWebSocketTestServer(t, srv, eng)

	dial := func() *websocket.Conn {
		conn, _, err := websocket.DefaultDialer.Dial(wsURL+"/ws", nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { conn.Close() })
		return conn
	}
	alice, bob := dial(), dial()

	if err := alice.WriteMessage(websocket.TextMessage, []byte("user:alice")); err != nil {
		t.Fatal(err)
	}
	if got := readText(t, alice); got != "subscribed:user:alice" {
		t.Fatalf("subscribe ack: %q", got)
	}
	if err := bob.WriteMessage(websocket.TextMessage, []byte("user:bob")); err != nil {
		t.Fatal(err)
	}
	if got := readText(t, bob); got != "subscribed:user:bob" {
		t.Fatalf("subscribe ack: %q", got)
	}

	hub.PublishText("user:alice", "for-alice")
	if got := readText(t, alice); got != "for-alice" {
		t.Fatalf("publish frame: %q", got)
	}

	// Bob must receive nothing; a broadcast afterwards is the ordering fence.
	hub.BroadcastText("for-everyone")
	if got := readText(t, bob); got != "for-everyone" {
		t.Fatalf("bob leaked a topic frame: %q", got)
	}
}

func TestWebSocket_securityLayerRejectsBeforeUpgrade(t *testing.T) {
	dir := mustWebDir(t)
	eng := NewHTTPEngine()
	srv := bungo.NewServer(eng, dir)
	srv.Security(bungo.SecurityLayer{
		Name: "gate",
		Handler: func(req *bungo.Request) (bool, *bungo.APIResponse) {
			return req.Headers["X-Token"] == "good", nil
		},
	})
	srv.WebSocket(bungo.WebSocketRoute{Path: "/ws", SecurityLayer: []string{"gate"}})

	_, wsURL := startWebSocketTestServer(t, srv, eng)

	_, resp, err := websocket.DefaultDialer.Dial(wsURL+"/ws", nil)
	if err == nil {
		t.Fatal("expected dial rejection")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %#v", resp)
	}

	conn, _, err := websocket.DefaultDialer.Dial(wsURL+"/ws", http.Header{"X-Token": []string{"good"}})
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
}

func TestWebSocket_securityLayerPopulatesSessionRequest(t *testing.T) {
	dir := mustWebDir(t)
	eng := NewHTTPEngine()
	srv := bungo.NewServer(eng, dir)
	srv.Security(bungo.SecurityLayer{
		Name: "auth",
		Handler: func(req *bungo.Request) (bool, *bungo.APIResponse) {
			req.Internal["UserID"] = "u-42"
			return true, nil
		},
	})

	userIDs := make(chan string, 1)
	srv.WebSocket(bungo.WebSocketRoute{
		Path:          "/ws",
		SecurityLayer: []string{"auth"},
		OnConnect: func(conn *bungo.WebSocketConn) {
			userID, _ := conn.Request().Internal["UserID"].(string)
			userIDs <- userID
		},
	})

	_, wsURL := startWebSocketTestServer(t, srv, eng)
	conn, _, err := websocket.DefaultDialer.Dial(wsURL+"/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	select {
	case userID := <-userIDs:
		if userID != "u-42" {
			t.Fatalf("UserID: %q", userID)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("OnConnect never fired")
	}
}

func TestWebSocket_originPolicy(t *testing.T) {
	dir := mustWebDir(t)
	eng := NewHTTPEngine()
	srv := bungo.NewServer(eng, dir)
	srv.WebSocket(bungo.WebSocketRoute{Path: "/strict"})
	srv.WebSocket(bungo.WebSocketRoute{
		Path:        "/open",
		CheckOrigin: func(req *bungo.Request) bool { return true },
	})

	_, wsURL := startWebSocketTestServer(t, srv, eng)
	crossOrigin := http.Header{"Origin": []string{"http://evil.example"}}

	if _, resp, err := websocket.DefaultDialer.Dial(wsURL+"/strict", crossOrigin); err == nil {
		t.Fatal("default policy must refuse cross-origin browsers")
	} else if resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %#v", resp)
	}

	conn, _, err := websocket.DefaultDialer.Dial(wsURL+"/open", crossOrigin)
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
}

func TestWebSocket_plainGETRejected(t *testing.T) {
	dir := mustWebDir(t)
	eng := NewHTTPEngine()
	srv := bungo.NewServer(eng, dir)
	srv.WebSocket(bungo.WebSocketRoute{Path: "/ws"})

	ts, _ := startWebSocketTestServer(t, srv, eng)

	resp, err := http.Get(ts.URL + "/ws")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("plain GET: %d", resp.StatusCode)
	}

	postResp, err := http.Post(ts.URL+"/ws", "text/plain", nil)
	if err != nil {
		t.Fatal(err)
	}
	postResp.Body.Close()
	if postResp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("POST: %d", postResp.StatusCode)
	}
}

func TestWebSocket_maxMessageSizeClosesWithTooBig(t *testing.T) {
	dir := mustWebDir(t)
	eng := NewHTTPEngine()
	srv := bungo.NewServer(eng, dir)

	received := make(chan string, 4)
	disconnected := make(chan struct{})
	srv.WebSocket(bungo.WebSocketRoute{
		Path:           "/ws",
		MaxMessageSize: 16,
		OnMessage: func(conn *bungo.WebSocketConn, message []byte) {
			received <- string(message)
		},
		OnDisconnect: func(conn *bungo.WebSocketConn) { close(disconnected) },
	})

	_, wsURL := startWebSocketTestServer(t, srv, eng)
	conn, _, err := websocket.DefaultDialer.Dial(wsURL+"/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	if err := conn.WriteMessage(websocket.TextMessage, []byte("small")); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-received:
		if got != "small" {
			t.Fatalf("OnMessage got %q", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("frame within the limit was not delivered")
	}

	if err := conn.WriteMessage(websocket.TextMessage, []byte(strings.Repeat("x", 64))); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, _, err := conn.ReadMessage(); !websocket.IsCloseError(err, websocket.CloseMessageTooBig) {
		t.Fatalf("oversized frame: got %v, want close 1009 (message too big)", err)
	}
	select {
	case <-disconnected:
	case <-time.After(5 * time.Second):
		t.Fatal("OnDisconnect never fired after the oversized frame")
	}
	// OnMessage runs synchronously in the read loop, which has exited by now.
	select {
	case got := <-received:
		t.Fatalf("oversized frame reached OnMessage (%d bytes)", len(got))
	default:
	}
}

func TestWebSocket_controlFramesNotDeliveredToOnMessage(t *testing.T) {
	dir := mustWebDir(t)
	eng := NewHTTPEngine()
	srv := bungo.NewServer(eng, dir)

	received := make(chan string, 4)
	srv.WebSocket(bungo.WebSocketRoute{
		Path: "/ws",
		OnMessage: func(conn *bungo.WebSocketConn, message []byte) {
			received <- string(message)
			_ = conn.SendText("echo:" + string(message))
		},
	})

	_, wsURL := startWebSocketTestServer(t, srv, eng)
	conn, _, err := websocket.DefaultDialer.Dial(wsURL+"/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	// An unsolicited pong is a keepalive signal for the server's read deadline, not application data.
	if err := conn.WriteControl(websocket.PongMessage, []byte("keepalive"), time.Now().Add(5*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, []byte("hello")); err != nil {
		t.Fatal(err)
	}
	if got := readText(t, conn); got != "echo:hello" {
		t.Fatalf("echo after pong: %q", got)
	}
	if got := <-received; got != "hello" {
		t.Fatalf("first OnMessage payload %q, want the data frame", got)
	}
	select {
	case got := <-received:
		t.Fatalf("control frame leaked into OnMessage: %q", got)
	default:
	}
}

func TestWebSocket_serverCloseSendsNormalClosure(t *testing.T) {
	dir := mustWebDir(t)
	eng := NewHTTPEngine()
	srv := bungo.NewServer(eng, dir)

	disconnected := make(chan struct{})
	hub := srv.WebSocket(bungo.WebSocketRoute{
		Path: "/ws",
		OnMessage: func(conn *bungo.WebSocketConn, message []byte) {
			if string(message) == "bye" {
				_ = conn.Close()
			}
		},
		OnDisconnect: func(conn *bungo.WebSocketConn) { close(disconnected) },
	})

	_, wsURL := startWebSocketTestServer(t, srv, eng)
	conn, _, err := websocket.DefaultDialer.Dial(wsURL+"/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	if err := conn.WriteMessage(websocket.TextMessage, []byte("bye")); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, _, err := conn.ReadMessage(); !websocket.IsCloseError(err, websocket.CloseNormalClosure) {
		t.Fatalf("server-initiated Close: got %v, want close 1000", err)
	}
	select {
	case <-disconnected:
	case <-time.After(5 * time.Second):
		t.Fatal("OnDisconnect never fired after server-initiated close")
	}
	for i := 0; i < 100 && hub.ConnectionCount() != 0; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	if hub.ConnectionCount() != 0 {
		t.Fatalf("connection count after close: %d", hub.ConnectionCount())
	}
}

func TestWritePump_writeFailureClosesConnection(t *testing.T) {
	serverSide := make(chan *websocket.Conn, 1)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		serverSide <- ws
	}))
	defer ts.Close()

	client, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	var ws *websocket.Conn
	select {
	case ws = <-serverSide:
	case <-time.After(5 * time.Second):
		t.Fatal("server never upgraded")
	}
	_ = ws.Close() // the transport is gone before the queued frame can be written

	conn := bungo.NewWebSocketConn(nil)
	if err := conn.SendText("lost"); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		writePump(conn, ws)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("writePump did not return after a failed write")
	}
	select {
	case <-conn.Done():
	default:
		t.Fatal("a failed write must close the BunGo connection so senders and the read loop stop")
	}
}
