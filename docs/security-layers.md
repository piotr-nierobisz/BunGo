# Security Layers

BunGo implements middleware under the concept of **Security Layers**. You globally define authentication, validation, and authorization scripts natively on the server, which can be elegantly composed onto specific Page, API, or WebSocket routes.

## Creating a Security Layer

A security layer receives a generic `bungo.Request` and returns two values: a boolean determining whether traffic can progress, and an optional `*bungo.APIResponse` shaping the rejection.

- `return true, nil` — the request continues to the next layer or the route handler.
- `return false, nil` — BunGo responds with the default **HTTP 401 Unauthorized** and does not run the route handler (for Page, API, and WebSocket routes alike — on a WebSocket route the layers run before the upgrade, so a rejected client never opens a socket).
- `return false, &bungo.APIResponse{...}` — BunGo writes *your* response instead: `StatusCode` (`0` falls back to 401), `Headers`, `Cookies`, and `Body` (`nil` writes no body; anything else is JSON-encoded) are all honored.

## Shaping Rejections

The rejection response covers the cases a bare 401 cannot:

```go
// Rate limiting: reject with 429 and a Retry-After header.
srv.Security(bungo.SecurityLayer{
    Name: "throttle_auth",
    Handler: func(req *bungo.Request) (bool, *bungo.APIResponse) {
        if tooManyAttempts(req) {
            return false, &bungo.APIResponse{
                StatusCode: 429,
                Headers:    map[string]string{"Retry-After": "60"},
                Body:       map[string]any{"error": "rate limited"},
            }
        }
        return true, nil
    },
})

// Browser page routes: bounce an expired session to /login instead of a bare 401.
srv.Security(bungo.SecurityLayer{
    Name: "require_session_page",
    Handler: func(req *bungo.Request) (bool, *bungo.APIResponse) {
        if !hasValidSession(req) {
            return false, &bungo.APIResponse{
                StatusCode: 302,
                Headers:    map[string]string{"Location": "/login"},
            }
        }
        return true, nil
    },
})
```

Authenticated-but-not-authorized flows distinguish 403 from 401 the same way: `return false, &bungo.APIResponse{StatusCode: 403}`.

## Chaining Layers and Passing Data

Because security layers execute sequentially, you can chain them to build powerful authorization flows.

A common pattern is having one layer verify a user's identity (e.g., verifying a JWT token) and extracting their Account ID into `req.Internal`. The next layer can then check if that specific Account ID has permission to modify the requested resource!

```go
// 1. Authentication Layer: Who is this?
srv.Security(bungo.SecurityLayer{
    Name: "require_auth",
    Handler: func(req *bungo.Request) (bool, *bungo.APIResponse) {
        token := req.Headers["Authorization"]
        if token != "Bearer secret" {
            return false, nil // Stop here, return HTTP 401 Unauthorized
        }

        // Pass data down the chain!
        req.Internal["UserID"] = 42

        return true, nil
    },
})

// 2. Authorization Layer: Can they edit this resource?
srv.Security(bungo.SecurityLayer{
    Name: "require_ownership",
    Handler: func(req *bungo.Request) (bool, *bungo.APIResponse) {
        // Extract the UserID safely placed here by the previous layer
        userID, ok := req.Internal["UserID"].(int)
        if !ok {
            return false, nil
        }

        // Example: Check if the User making the request actually owns the document
        // documentOwnerID := fetchOwnerFromDatabase(req.Params["documentId"])
        documentOwnerID := 42 // Simplified for example

        if userID != documentOwnerID {
            // They are authenticated, but don't own the resource!
            return false, &bungo.APIResponse{StatusCode: 403}
        }

        return true, nil
    },
})
```

## Attaching Layers to Routes
To protect APIs, pages, or WebSocket routes, pass the layer names on `ApiRoute.SecurityLayer`, `PageRoute.SecurityLayer`, or `WebSocketRoute.SecurityLayer`. They are executed in order.

```go
srv.Api(bungo.ApiRoute{
    Path:          "/document", // Parameters are passed via Query String: /api/v1/document?documentId=123
    Version:       "v1",
    Method:        "PUT",
    // Chain them in order!
    SecurityLayer: []string{"require_auth", "require_ownership"},
    Handler: func(req *bungo.Request) (bungo.APIResponse, error) {

        // 3. Extraction in the final handler!
        userID := req.Internal["UserID"].(int)

        return bungo.APIResponse{
            StatusCode: 200,
            Body: map[string]any{"updated": true, "editorId": userID},
        }, nil
    },
})
```

## Origin Checks on API Routes

Independent of security layers, an API route can validate the caller's `Origin` header before any layer runs — the same shape as `WebSocketRoute.CheckOrigin`:

```go
srv.Api(bungo.ApiRoute{
    Path:    "/transfer",
    Version: "v1",
    Method:  "POST",
    CheckOrigin: func(req *bungo.Request) bool {
        return req.Headers["Origin"] == "https://app.example.com"
    },
    Handler: transferHandler,
})
```

A refused origin receives **HTTP 403 Forbidden**. When `CheckOrigin` is nil, no origin check runs — unlike WebSocket routes, API routes apply no default origin policy, so existing routes are unaffected.

Next: [WebSockets](./websockets.md).
