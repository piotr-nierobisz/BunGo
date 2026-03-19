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

## Static files (`web/static`)

When you use **`engine.NewHTTPEngine()`** with a non-empty `webDir`, BunGo looks for a directory named **`static`** inside that folder. If it exists, the engine registers a standard file server:

| On disk | URL |
|--------|-----|
| `<webDir>/static/styles.css` | `GET /static/styles.css` |
| `<webDir>/static/img/logo.png` | `GET /static/img/logo.png` |

- **Convention:** Put assets you want served as-is under `<webDir>/static/`. Reference them from HTML templates with root-absolute paths, e.g. `<link rel="stylesheet" href="/static/styles.css" />` or `<img src="/static/img/hero.webp" alt="" />`. APIs can return the same paths in JSON for use in React (see `examples/http_hybrid`).
- **Optional:** If `static/` is missing, nothing is mounted—no error. There is **no** static mount when `webDir` is `""` (API-only servers).
- **Security layers:** Static requests are handled by `net/http`’s `FileServer` on `/static/` and **do not** run BunGo **Security** layers. Treat everything under `static/` as **public**. Keep secrets and user-specific files out of this tree; use authenticated API or page handlers instead.
- **What not to put here:** Compiled React view bundles are produced from `web/views/` and injected into HTML by BunGo—you do not copy or hand-link those as separate files under `static/`.
- **Other engines:** The automatic `/static/` mount is implemented in the **HTTP** engine. **AWS Lambda** and **GCP** adapters route pages and `/api/...` only; for serverless or CDN deployments, host long-lived assets on object storage or a CDN and use full URLs or a path your gateway maps to that bucket.


Next: [Templates and Layouts](./templates-and-layouts.md).
