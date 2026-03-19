# Routing and Pages

In BunGo, handling web traffic is straightforward. You instantiate a server with an `Engine` (like standard HTTP) and a target `webDir` that holds your frontend files.

## Server Setup
```go
package main

import (
    "log"
    "github.com/piotr-nierobisz/BunGo"
    "github.com/piotr-nierobisz/BunGo/engine"
)

func main() {
    // We use the basic HTTP engine for standard requests.
    engineInstance := engine.NewHTTPEngine()
    
    // Pass our engine and our web directory 
    srv := bungo.NewServer(engineInstance, "./web")
    
    // Note: Calling Serve starts listening on a port!
    if err := srv.Serve(3303); err != nil {
        log.Fatal(err)
    }
}
```

> **Tip: Pure API Servers**
> If you are building a backend application that strictly serves JSON APIs and does not require HTML layouts or React views, you can securely pass an empty string `""` as the `webDir` parameter during initialization:
> `srv := bungo.NewServer(engineInstance, "")`
> BunGo will automatically bypass all fail-fast folder verifications for `layouts/` and `views/`, keeping your project footprint perfectly minimal!

## Creating Page Routes
A **Page Route** combines server-side data fetching with a frontend presentation. It ties a Go backend handler, a base HTML Template, and a React View together in one logical request.

The `Handler` is where your backend logic lives. It executes *before* your template or view is rendered. Here, you talk to your database, perform business logic, and construct a `map[string]any`. BunGo takes this map and automatically serializes it as JSON, making it available to your template and React components instantly!

```go
srv.Page(bungo.PageRoute{
    Path:     "/",
    Template: "index.gohtml", // Required: Maps to web/layouts/index.gohtml
    View:     "loader.jsx",   // Optional: Maps to web/views/loader.jsx
    
    // The Go backend logic executes before the page returns.
    Handler: func(req *bungo.Request) (map[string]any, error) {
        return map[string]any{
            "PageTitle": "BunGo App",
            "InitialData": []string{"React", "Without", "NPM"},
        }, nil
    },
})
```

The data you return from this handler is automatically shipped seamlessly as JSON directly into your React components.

## Creating API Routes
For explicit JSON endpoints, use **API Routes**. These are bound to specific HTTP methods (`GET`, `POST`, etc.) and enforce RESTful responses.

Similar to Pages, API routes have a `Handler`, but instead of returning a map for a view, they return an explicit `bungo.APIResponse` struct. This gives you exact control over the HTTP status code and the JSON body that gets sent back to the client.

```go
srv.Api(bungo.ApiRoute{
    Path:    "/users",
    Version: "v1",     // Prepended automatically. The actual path is /api/v1/users
    Method:  "GET",
    Handler: func(req *bungo.Request) (bungo.APIResponse, error) {
        return bungo.APIResponse{
            StatusCode: 200,
            Body: map[string]any{
                "success": true,
                "users":   []string{"Alice", "Bob"},
            },
        }, nil
    },
})
```

Next: [Templates and Layouts](./templates-and-layouts.md).
