# Security Layers

BunGo implements middleware under the concept of **Security Layers**. You globally define authentication, validation, and authorization scripts natively on the server, which can be elegantly composed onto specific Page or API routes.

## Creating a Security Layer

A security layer receives a generic `bungo.Request` and resolves to a boolean determining whether traffic can progress. If any layer returns `false`, BunGo responds with **HTTP 401 Unauthorized** and does not run the route handler (for both Page and API routes).

## Chaining Layers and Passing Data

Because security layers execute sequentially, you can chain them to build powerful authorization flows. 

A common pattern is having one layer verify a user's identity (e.g., verifying a JWT token) and extracting their Account ID into `req.Internal`. The next layer can then check if that specific Account ID has permission to modify the requested resource!

```go
// 1. Authentication Layer: Who is this?
srv.Security(bungo.SecurityLayer{
    Name: "require_auth",
    Handler: func(req *bungo.Request) bool {
        token := req.Headers["Authorization"]
        if token != "Bearer secret" {
            return false // Stop here, return HTTP 401 Unauthorized
        }
        
        // Pass data down the chain!
        req.Internal["UserID"] = 42 
        
        return true
    },
})

// 2. Authorization Layer: Can they edit this resource?
srv.Security(bungo.SecurityLayer{
    Name: "require_ownership",
    Handler: func(req *bungo.Request) bool {
        // Extract the UserID safely placed here by the previous layer
        userID, ok := req.Internal["UserID"].(int)
        if !ok {
            return false
        }
        
        // Example: Check if the User making the request actually owns the document
        // documentOwnerID := fetchOwnerFromDatabase(req.Params["documentId"])
        documentOwnerID := 42 // Simplified for example
        
        if userID != documentOwnerID {
            // They are authenticated, but don't own the resource!
            return false // Return HTTP 401 Unauthorized
        }
        
        return true
    },
})
```

## Attaching Layers to Routes
To protect APIs or pages, pass the layer names on `ApiRoute.SecurityLayer` or `PageRoute.SecurityLayer`. They are executed in order.

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

Next: [CLI and Tools](./cli-tools.md).
