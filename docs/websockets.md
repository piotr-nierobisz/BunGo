# WebSockets

BunGo ships a first-class WebSocket abstraction: you register a **WebSocket Route** exactly like a Page or API route, and BunGo owns the entire connection lifecycle—the upgrade handshake, keepalive pings, buffered writes, slow-consumer protection, and teardown. Your application only writes three callbacks and talks to a **hub**.

WebSocket routes are served by the **HTTP** and **HTTPS** engines (`engine.NewHTTPEngine()` / `engine.NewHTTPSEngine(...)`). The serverless adapters (AWS Lambda, GCP) cannot hold long-lived connections and do not serve them.

## Registering a route

```go
var hub *bungo.WebSocketHub
hub = srv.WebSocket(bungo.WebSocketRoute{
    Path:          "/ws/feed",
    SecurityLayer: []string{"require_auth"},

    OnConnect: func(conn *bungo.WebSocketConn) {
        userID := conn.Request().Internal["UserID"].(string)
        hub.Subscribe(conn, "user:"+userID)
        _ = conn.SendText("connected")
    },

    OnMessage: func(conn *bungo.WebSocketConn, message []byte) {
        // One call per client frame, text or binary, in arrival order.
        _ = conn.SendText("echo: " + string(message))
    },

    OnDisconnect: func(conn *bungo.WebSocketConn) {
        // The connection is already unregistered from the hub at this point.
    },
})
```

`WebSocket` returns the route's `*bungo.WebSocketHub`, and that return value is the **only** handle to it—there is no look-up-a-hub-by-path accessor to mistype. Capture it wherever you broadcast or publish from: a closure like the `OnConnect` above (declare `hub` with `var` first so the callback can close over it), or a field on your app struct that an API handler reads to push a change signal. All three callbacks are optional—a server-push-only feed needs no `OnMessage` at all.

## The connection

Every connected client is a `*bungo.WebSocketConn`:

| Method | Behavior |
|--------|----------|
| `Request()` | The upgrade-time `*bungo.Request`, **including `Internal` values written by your Security layers**—the place to read the authenticated identity. |
| `Send([]byte)` | Enqueue a binary frame. |
| `SendText(string)` | Enqueue a text frame. |
| `SendJSON(any)` | Marshal and enqueue a JSON text frame. |
| `Close()` | Politely close the connection (idempotent). |

Connections are safe for concurrent use: `Send*` methods enqueue onto a per-connection buffer drained by a single writer goroutine, so handlers, hub broadcasts, and background jobs can all send without coordination. A connection whose buffer stays full (a consumer too slow to drain 256 pending frames) is **closed rather than allowed to block every publisher behind it**; its `Send*` calls return `bungo.ErrWebSocketClosed` from then on.

## The hub: broadcast and topics

Each route owns one hub. `Broadcast*` reaches every connected client; `Publish*` reaches only connections subscribed to a **topic**—an arbitrary string convention such as `"user:42"` or `"room:lobby"`:

```go
// From any handler, goroutine, or scheduler job:
hub.BroadcastJSON(map[string]any{"kind": "announcement", "text": "hello all"})
hub.PublishJSON("user:42", map[string]any{"kind": "change", "revision": 7})
```

| Method | Audience |
|--------|----------|
| `Broadcast` / `BroadcastText` / `BroadcastJSON` | every connection |
| `Publish` / `PublishText` / `PublishJSON` | connections subscribed to the topic |
| `Subscribe(conn, topic)` / `Unsubscribe(conn, topic)` | manage topic membership |
| `ConnectionCount()` | connected clients |

Subscriptions are cleaned up automatically when a connection disconnects. A typical cross-device sync feed is just: `Subscribe` each connection to `"user:<id>"` in `OnConnect`, then `PublishJSON` after each committed mutation.

## Security and origin policy

- **Security layers run before the upgrade.** A rejected client receives a plain HTTP `401 Unauthorized` and never opens a socket; a missing layer name yields `500`, exactly like Page and API routes. Values a layer writes into `req.Internal` travel with the connection for its whole lifetime.
- **Same-host origin by default.** Browsers send an `Origin` header; when it does not match the request host, the handshake is refused with `403`. Override per route when you need cross-origin clients:

```go
srv.WebSocket(bungo.WebSocketRoute{
    Path: "/ws/public",
    CheckOrigin: func(req *bungo.Request) bool {
        return req.Headers["Origin"] == "https://app.example.com"
    },
})
```

- **Inbound frame size** is capped at 1 MiB by default; set `MaxMessageSize` (bytes) on the route to change it.

## Connection lifecycle details

BunGo applies the standard keepalive discipline so you never have to:

- a ping is sent every 54 seconds, and a connection whose pongs stop arriving for 60 seconds is dropped;
- every write carries a 10-second deadline;
- `conn.Close()` (or the client going away) sends a proper close frame, tears the connection down, unregisters the connection from the hub, and then fires `OnDisconnect`—so a message published from `OnDisconnect` never reaches the departing client.

`OnMessage` runs on the connection's reader goroutine in frame order; different connections dispatch concurrently, so guard shared application state as you would in any HTTP handler.

## Browser client

No client library is needed—the native `WebSocket` API pairs directly with a route:

```js
const ws = new WebSocket(`wss://${location.host}/ws/feed`);
ws.onmessage = (event) => {
    const signal = JSON.parse(event.data);
    // refresh, patch state, etc.
};
```

Cookies are sent with the handshake automatically, which is why cookie-based Security layers work unchanged.

Next: [CLI and Tools](./cli-tools.md).