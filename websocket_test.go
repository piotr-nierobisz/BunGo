package bungo

import (
	"testing"
)

// drain returns and removes every frame currently queued on a connection. The hub
// enqueues synchronously with no writer goroutine draining in these tests, so a
// drain right after a Broadcast/Publish observes exactly what that call delivered.
func drain(c *WebSocketConn) []WebSocketFrame {
	var out []WebSocketFrame
	for {
		select {
		case f := <-c.Outbound():
			out = append(out, f)
		default:
			return out
		}
	}
}

func TestWebSocketHub_broadcast(t *testing.T) {
	t.Parallel()
	hub := newWebSocketHub()
	a, b := NewWebSocketConn(nil), NewWebSocketConn(nil)
	hub.Register(a)
	hub.Register(b)

	hub.BroadcastText("hi")
	if got := drain(a); len(got) != 1 || got[0].Binary || string(got[0].Data) != "hi" {
		t.Fatalf("broadcast text to a: %+v", got)
	}
	if got := drain(b); len(got) != 1 {
		t.Fatalf("broadcast text to b: %+v", got)
	}

	hub.Broadcast([]byte{1, 2})
	if got := drain(a); len(got) != 1 || !got[0].Binary || string(got[0].Data) != "\x01\x02" {
		t.Fatalf("binary broadcast: %+v", got)
	}
	_ = drain(b)

	hub.Unregister(a)
	hub.BroadcastText("again")
	if got := drain(a); len(got) != 0 {
		t.Fatalf("unregistered connection must not receive broadcasts: %+v", got)
	}
	if got := drain(b); len(got) != 1 {
		t.Fatalf("remaining connection should receive: %+v", got)
	}
	if hub.ConnectionCount() != 1 {
		t.Fatalf("count: %d", hub.ConnectionCount())
	}
}

func TestWebSocketHub_topics(t *testing.T) {
	t.Parallel()
	hub := newWebSocketHub()
	a, b := NewWebSocketConn(nil), NewWebSocketConn(nil)
	hub.Register(a)
	hub.Register(b)
	hub.Subscribe(a, "user:1")
	hub.Subscribe(b, "user:2")

	hub.PublishText("user:1", "ping")
	if got := drain(a); len(got) != 1 || string(got[0].Data) != "ping" {
		t.Fatalf("publish must reach subscriber a: %+v", got)
	}
	if got := drain(b); len(got) != 0 {
		t.Fatalf("publish must not reach non-subscriber b: %+v", got)
	}

	hub.Unsubscribe(a, "user:1")
	hub.PublishText("user:1", "ping2")
	if got := drain(a); len(got) != 0 {
		t.Fatalf("unsubscribed connection must not receive publishes: %+v", got)
	}

	// Unregister must clear subscriptions so re-publishing reaches nobody.
	hub.Subscribe(b, "user:1")
	hub.Unregister(b)
	hub.PublishText("user:1", "ping3")
	if got := drain(b); len(got) != 0 {
		t.Fatalf("unregistered connection received publish: %+v", got)
	}
}

func TestWebSocketHub_subscribeUnregisteredIgnored(t *testing.T) {
	t.Parallel()
	hub := newWebSocketHub()
	ghost := NewWebSocketConn(nil)
	hub.Subscribe(ghost, "user:1")
	hub.PublishText("user:1", "ping")
	if got := drain(ghost); len(got) != 0 {
		t.Fatalf("subscribe of an unregistered connection must be ignored: %+v", got)
	}
}

func TestWebSocketHub_json(t *testing.T) {
	t.Parallel()
	hub := newWebSocketHub()
	a := NewWebSocketConn(nil)
	hub.Register(a)
	hub.Subscribe(a, "t")

	if err := hub.BroadcastJSON(map[string]int{"n": 1}); err != nil {
		t.Fatal(err)
	}
	if err := hub.PublishJSON("t", map[string]int{"n": 2}); err != nil {
		t.Fatal(err)
	}
	got := drain(a)
	if len(got) != 2 || string(got[0].Data) != `{"n":1}` || string(got[1].Data) != `{"n":2}` {
		t.Fatalf("json frames: %+v", got)
	}

	if err := hub.BroadcastJSON(make(chan int)); err == nil {
		t.Fatal("expected marshal error")
	}
}

func TestWebSocketConn_slowConsumerCloses(t *testing.T) {
	t.Parallel()
	conn := NewWebSocketConn(nil)

	// Fill the outbound buffer, then the next send must close the connection.
	for i := 0; i < webSocketSendBuffer; i++ {
		if err := conn.SendText("x"); err != nil {
			t.Fatalf("send %d before buffer full: %v", i, err)
		}
	}
	if err := conn.SendText("overflow"); err != ErrWebSocketClosed {
		t.Fatalf("overflowing send: got %v, want ErrWebSocketClosed", err)
	}
	if err := conn.SendText("after"); err != ErrWebSocketClosed {
		t.Fatalf("send after slow-consumer close: got %v, want ErrWebSocketClosed", err)
	}
	select {
	case <-conn.Done():
	default:
		t.Fatal("slow consumer must be closed")
	}
}
