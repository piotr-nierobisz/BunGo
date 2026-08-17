package bungo

import (
	"encoding/json"
	"errors"
	"sync"
)

// webSocketSendBuffer is the per-connection outbound frame buffer. A client too
// slow to drain this many pending frames is closed rather than allowed to block
// every publisher queued behind it.
const webSocketSendBuffer = 256

// ErrWebSocketClosed is returned by a connection's Send methods once it is closed,
// including after it was closed for failing to keep up with its outbound buffer.
var ErrWebSocketClosed = errors.New("BunGo WebSocket Error: connection is closed")

// WebSocketFrame is one queued outbound frame. The transport's writer goroutine
// receives these from a connection's Outbound channel; application code never
// constructs one and uses WebSocketConn.Send* instead.
type WebSocketFrame struct {
	Binary bool // false sends a text frame, true sends a binary frame
	Data   []byte
}

// WebSocketRoute configures a single WebSocket route.
//
// Path is required and is matched exactly, like PageRoute.Path. The route accepts
// only GET requests carrying a WebSocket upgrade handshake; security layers run
// BEFORE the connection is upgraded, so a rejected client receives a plain
// HTTP 401 and never opens a socket.
//
// All lifecycle callbacks are optional. OnConnect runs after the connection has
// been registered in the route's hub, OnMessage runs once per client frame (text
// or binary), and OnDisconnect runs after the connection has been unregistered — a
// message published from OnDisconnect never reaches the departing client.
//
// CheckOrigin is optional: when nil, engines apply a same-host origin policy
// (browser cross-origin connections are refused). MaxMessageSize is optional:
// when 0, engines apply their default inbound frame limit.
type WebSocketRoute struct {
	Path           string
	SecurityLayer  []string
	MaxMessageSize int64
	CheckOrigin    func(req *Request) bool
	OnConnect      func(conn *WebSocketConn)
	OnMessage      func(conn *WebSocketConn, message []byte)
	OnDisconnect   func(conn *WebSocketConn)
}

// WebSocketConn is one connected client on a WebSocket route.
//
// A connection is a concurrency-safe mailbox: Send* enqueue onto a buffered
// outbound channel drained by the transport's single writer goroutine, so
// handlers, hub broadcasts, and background goroutines can all send without
// coordination. Send* never block; a connection whose buffer stays full is closed
// as a slow consumer, and its Send* calls return ErrWebSocketClosed from then on.
//
// Request returns the upgrade-time request, including the Internal map populated by
// the route's security layers — the idiomatic place to read the authenticated
// identity a layer stored during the handshake.
//
// Engines create connections with NewWebSocketConn and drive their I/O through the
// Outbound and Done channels; application code only calls Request, Send*, and Close.
type WebSocketConn struct {
	request   *Request
	outbound  chan WebSocketFrame
	closed    chan struct{}
	closeOnce sync.Once
}

// NewWebSocketConn creates a connection around an upgrade-time request; engines call
// it after a successful upgrade. It is part of the engine-facing seam, not the
// application API.
// Inputs:
// - request: request translated from the WebSocket handshake, carrying any Internal values set by security layers.
// Outputs:
// - *WebSocketConn: connection with an empty outbound buffer, ready to register in a hub.
func NewWebSocketConn(request *Request) *WebSocketConn {
	return &WebSocketConn{
		request:  request,
		outbound: make(chan WebSocketFrame, webSocketSendBuffer),
		closed:   make(chan struct{}),
	}
}

// Request returns the upgrade-time request, including Internal values set by security layers.
// Inputs:
// - none
// Outputs:
// - *Request: request translated from the WebSocket handshake.
func (c *WebSocketConn) Request() *Request {
	return c.request
}

// Send enqueues a binary frame for delivery to the client.
// Inputs:
// - message: payload written to the client as one binary frame.
// Outputs:
// - error: ErrWebSocketClosed when the connection is closed or was closed as a slow consumer.
func (c *WebSocketConn) Send(message []byte) error {
	return c.enqueue(WebSocketFrame{Binary: true, Data: message})
}

// SendText enqueues a text frame for delivery to the client.
// Inputs:
// - message: payload written to the client as one text frame.
// Outputs:
// - error: ErrWebSocketClosed when the connection is closed or was closed as a slow consumer.
func (c *WebSocketConn) SendText(message string) error {
	return c.enqueue(WebSocketFrame{Data: []byte(message)})
}

// SendJSON marshals a value and enqueues it as one JSON text frame.
// Inputs:
// - v: value marshalled with encoding/json.
// Outputs:
// - error: the marshal error when encoding fails, otherwise ErrWebSocketClosed when the connection is closed.
func (c *WebSocketConn) SendJSON(v any) error {
	payload, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return c.enqueue(WebSocketFrame{Data: payload})
}

// Close marks the connection closed, prompting a close frame and teardown; it is idempotent.
// Inputs:
// - none
// Outputs:
// - error: always nil; kept for symmetry with io closers.
func (c *WebSocketConn) Close() error {
	c.closeOnce.Do(func() {
		close(c.closed)
	})
	return nil
}

// Outbound is the transport's receive end for queued frames; the writer goroutine
// ranges over it. It is part of the engine-facing seam, not the application API.
// Inputs:
// - none
// Outputs:
// - <-chan WebSocketFrame: frames enqueued by Send*, in send order.
func (c *WebSocketConn) Outbound() <-chan WebSocketFrame {
	return c.outbound
}

// Done is closed when the connection closes; the transport selects on it to stop
// writing. It is part of the engine-facing seam, not the application API.
// Inputs:
// - none
// Outputs:
// - <-chan struct{}: channel closed once Close has run.
func (c *WebSocketConn) Done() <-chan struct{} {
	return c.closed
}

// enqueue pushes one frame onto the outbound buffer without ever blocking the caller.
// Inputs:
// - frame: frame to queue for the writer goroutine.
// Outputs:
// - error: ErrWebSocketClosed when the connection is closed, or when a full buffer forced it closed.
func (c *WebSocketConn) enqueue(frame WebSocketFrame) error {
	select {
	case c.outbound <- frame:
		return nil
	case <-c.closed:
		return ErrWebSocketClosed
	default:
		// Slow consumer: closing this connection beats blocking every publisher behind it.
		_ = c.Close()
		return ErrWebSocketClosed
	}
}

// WebSocketHub fans messages out to the connections of one WebSocket route.
//
// Every route registered with Server.WebSocket owns exactly one hub, returned at
// registration time and retrievable later via Server.WebSocketHub. Broadcast*
// methods reach every connection; Publish* methods reach only connections
// subscribed to a topic — an arbitrary string such as "user:42" or "room:lobby".
// Register and Unregister are the engine-facing seam that ties connection lifecycle
// to the hub; application code does not call them.
type WebSocketHub struct {
	mu         sync.Mutex
	conns      map[*WebSocketConn]struct{}
	topics     map[string]map[*WebSocketConn]struct{}
	connTopics map[*WebSocketConn]map[string]struct{}
}

// newWebSocketHub creates an empty hub for one WebSocket route.
// Inputs:
// - none
// Outputs:
// - *WebSocketHub: initialized hub with no connections or topic subscriptions.
func newWebSocketHub() *WebSocketHub {
	return &WebSocketHub{
		conns:      make(map[*WebSocketConn]struct{}),
		topics:     make(map[string]map[*WebSocketConn]struct{}),
		connTopics: make(map[*WebSocketConn]map[string]struct{}),
	}
}

// Register adds a connection to the hub; engines call it after a successful upgrade.
// Inputs:
// - conn: newly connected client to include in broadcasts.
// Outputs:
// - none
func (h *WebSocketHub) Register(conn *WebSocketConn) {
	h.mu.Lock()
	h.conns[conn] = struct{}{}
	h.mu.Unlock()
}

// Unregister removes a connection and all its topic subscriptions; engines call it on disconnect.
// Inputs:
// - conn: departing client to exclude from further broadcasts and publishes.
// Outputs:
// - none
func (h *WebSocketHub) Unregister(conn *WebSocketConn) {
	h.mu.Lock()
	delete(h.conns, conn)
	for topic := range h.connTopics[conn] {
		delete(h.topics[topic], conn)
		if len(h.topics[topic]) == 0 {
			delete(h.topics, topic)
		}
	}
	delete(h.connTopics, conn)
	h.mu.Unlock()
}

// Subscribe adds a registered connection to a topic so Publish* calls for that topic reach it.
// Inputs:
// - conn: connection to subscribe; ignored when it is not registered in the hub.
// - topic: arbitrary topic string, for example "user:42".
// Outputs:
// - none
func (h *WebSocketHub) Subscribe(conn *WebSocketConn, topic string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, registered := h.conns[conn]; !registered {
		return
	}
	if h.topics[topic] == nil {
		h.topics[topic] = make(map[*WebSocketConn]struct{})
	}
	h.topics[topic][conn] = struct{}{}
	if h.connTopics[conn] == nil {
		h.connTopics[conn] = make(map[string]struct{})
	}
	h.connTopics[conn][topic] = struct{}{}
}

// Unsubscribe removes a connection from a topic.
// Inputs:
// - conn: connection to remove from the topic's subscriber set.
// - topic: topic string the connection no longer wants.
// Outputs:
// - none
func (h *WebSocketHub) Unsubscribe(conn *WebSocketConn, topic string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.topics[topic], conn)
	if len(h.topics[topic]) == 0 {
		delete(h.topics, topic)
	}
	delete(h.connTopics[conn], topic)
}

// Broadcast sends a binary message to every connection in the hub.
// Inputs:
// - message: payload delivered to each connection as a binary frame.
// Outputs:
// - none
func (h *WebSocketHub) Broadcast(message []byte) {
	for _, conn := range h.snapshot("") {
		_ = conn.Send(message)
	}
}

// BroadcastText sends a text message to every connection in the hub.
// Inputs:
// - message: payload delivered to each connection as a text frame.
// Outputs:
// - none
func (h *WebSocketHub) BroadcastText(message string) {
	for _, conn := range h.snapshot("") {
		_ = conn.SendText(message)
	}
}

// BroadcastJSON marshals a value once and sends it to every connection as a JSON text frame.
// Inputs:
// - v: value marshalled with encoding/json.
// Outputs:
// - error: non-nil when marshalling fails; nothing is sent in that case.
func (h *WebSocketHub) BroadcastJSON(v any) error {
	payload, err := json.Marshal(v)
	if err != nil {
		return err
	}
	h.BroadcastText(string(payload))
	return nil
}

// Publish sends a binary message to every connection subscribed to a topic.
// Inputs:
// - topic: topic string whose subscribers receive the message.
// - message: payload delivered to each subscriber as a binary frame.
// Outputs:
// - none
func (h *WebSocketHub) Publish(topic string, message []byte) {
	for _, conn := range h.snapshot(topic) {
		_ = conn.Send(message)
	}
}

// PublishText sends a text message to every connection subscribed to a topic.
// Inputs:
// - topic: topic string whose subscribers receive the message.
// - message: payload delivered to each subscriber as a text frame.
// Outputs:
// - none
func (h *WebSocketHub) PublishText(topic string, message string) {
	for _, conn := range h.snapshot(topic) {
		_ = conn.SendText(message)
	}
}

// PublishJSON marshals a value once and sends it to a topic's subscribers as a JSON text frame.
// Inputs:
// - topic: topic string whose subscribers receive the message.
// - v: value marshalled with encoding/json.
// Outputs:
// - error: non-nil when marshalling fails; nothing is sent in that case.
func (h *WebSocketHub) PublishJSON(topic string, v any) error {
	payload, err := json.Marshal(v)
	if err != nil {
		return err
	}
	h.PublishText(topic, string(payload))
	return nil
}

// ConnectionCount reports how many connections are currently registered in the hub.
// Inputs:
// - none
// Outputs:
// - int: number of connected clients.
func (h *WebSocketHub) ConnectionCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.conns)
}

// snapshot copies the current recipients for a topic (or all connections when topic is empty) under lock.
// Inputs:
// - topic: topic to snapshot, or empty string for every registered connection.
// Outputs:
// - []*WebSocketConn: recipient list safe to iterate without holding the hub lock.
func (h *WebSocketHub) snapshot(topic string) []*WebSocketConn {
	h.mu.Lock()
	defer h.mu.Unlock()

	source := h.conns
	if topic != "" {
		source = h.topics[topic]
	}
	conns := make([]*WebSocketConn, 0, len(source))
	for conn := range source {
		conns = append(conns, conn)
	}
	return conns
}
