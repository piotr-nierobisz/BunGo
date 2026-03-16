# BunGo (Bundler 4 Go) 🐇

**BunGo** is a high-performance, lightweight, and idiomatic Go web framework designed to seamlessly bridge backend Go logic with frontend React (JSX) views. It eliminates the need for Node.js, `npm`, `package.json`, and complex external build pipelines by embedding everything you need right into your Go binary.

If you love the simplicity of Go backends but want the rich interactivity of React without the headache of managing a separate frontend ecosystem, BunGo is the framework for you.

---

## 🌟 Philosophy & Core Features

* **Zero External Dependencies (No Node.js or NPM):** BunGo natively embeds `esbuild` via its Go API, alongside a bundled version of React and React-DOM. This means no `.node_modules`, no messy JavaScript build scripts, and no external Javascript tools to install. JSX compiles on the fly right inside your Go application.
* **Fail-Fast by Design:** Typos in template names or missing directories shouldn't result in silent runtime errors when an end-user hits a route. BunGo strictly checks for the existence of your views, layouts, and web directories immediately at startup and will instantly `panic()` if something is misconfigured.
* **Infrastructure Driven, Business Agnostic:** BunGo strictly handles HTTP routing, security enforcement, frontend bundling, and template rendering. It stays entirely out of your database adapters and business logic.
* **Smart Template Composition:** Use zero-boilerplate standalone HTML templates, or define powerful Go `html/template` layouts to wrap common `<html><head><body>` structures. BunGo handles injecting your React bundles automatically.
* **Agnostic Execution Environments:** Rather than locking you into `net/http`, BunGo uses an `Engine` interface separating core logic from HTTP transport. Out of the box, you can use the standard `HTTPEngine`, but writing adapters for AWS Lambda, Google Cloud Functions, or custom transports takes minutes.

---

## 🏗 Directory Structure

BunGo enforces a strict, conventional directory structure for your web assets to ensure predictability.

```text
your-app/
├── main.go
└── web/                <-- The base WebDir
    ├── layouts/        <-- Contains Go HTML templates (.gohtml)
    │   ├── base.gohtml
    │   └── index.gohtml
    ├── views/          <-- Contains React JSX components (.jsx)
    │   └── loader.jsx
    └── static/         <-- (Optional) Standard static assets served directly
        └── logo.png
```

- **`layouts/`**: Where standard `html/template` files live. A `Template` is your page content, while an optional `Layout` acts as a wrapper.
- **`views/`**: Where your React modules live. BunGo compiles these on the fly as entry points and injects them into the respective templates.

---

## 🚀 Quick Start Guide

Here's an example of setting up a BunGo Hybrid application.

### 1. Initialize the Server

First, create a transport engine, initialize the server, and tell it where your web assets live:

```go
package main

import (
    "log"
    bungo "github.com/piotr-nierobisz/BunGo"
    "github.com/piotr-nierobisz/BunGo/engine"
)

func main() {
    // 1. Create your transport engine (e.g., standard HTTP)
    engineInstance := engine.NewHTTPEngine()
    
    // 2. Initialize the Server Registry and pass your Web Directory
    srv := bungo.NewServer(engineInstance, "./web")
    
    // Server setup goes here...
    
    // Start listening!
    if err := srv.Serve(3303); err != nil {
        log.Fatal(err)
    }
}
```

---

### 2. Registering Page Routes

Pages return an HTML document to the client. BunGo allows you to compose these intuitively using Go's built-in templating features combined with automated React injection.

```go
srv.Page(bungo.PageRoute{
    Path:     "/",
    Template: "index.gohtml", // Required: Maps to web/layouts/index.gohtml
    View:     "loader.jsx",   // Optional: Maps to web/views/loader.jsx
    
    // The handler executes the backend logic before rendering.
    // The returned map[string]any is injected as `window.__BUNGO_DATA__`.
    Handler: func(req *bungo.Request) (map[string]any, error) {
        return map[string]any{
            "PageTitle": "Welcome to BunGo",
            "InitialData": []int{1, 2, 3},
        }, nil
    },
})
```

#### Understanding Rendering Composition

- **Templates:** The page-specific HTML file (e.g., `index.gohtml`). Standalone formats execute directly.
- **Layouts (Optional):** Define a base structure, like `base.gohtml`. By wrapping your page with a Layout or defining a global one via `srv.SetDefaultLayout("base.gohtml")`, your Template only needs to define `{{define "content"}}...{{end}}`. BunGo places the content into `{{block "content" .}}` in the Layout file.
- **Automated Script Injection:** You **never** define `<script>` tags for your React bundles manually. BunGo automatically catches the end of the `<head>` or `<body>` tag during template rendering, compiles the JSX defined in the `View` field, embeds it as an inline script, and injects `window.__BUNGO_DATA__` for state hydration. 

---

### 3. Registering API Routes

BunGo provides a specialized route configuration for JSON APIs. APIs enforce strict REST standards by binding to HTTP methods, and instantly serialize your response bodies.

```go
srv.Api(bungo.ApiRoute{
    Path:    "/users",
    Version: "v1",     // Prepended to create /api/v1/users
    Method:  "GET",
    Handler: func(req *bungo.Request) (bungo.APIResponse, error) {
        return bungo.APIResponse{
            StatusCode: 200,
            Body: map[string]any{
                "message": "User list fetched",
                "users":   []string{"Alice", "Bob"},
            },
        }, nil
    },
})
```

---

### 4. Security Layers

BunGo provides a powerful middleware system named **Security Layers**. You define your rules globally on the server and chain them by name to specific APIs or Pages. This promotes reusable authentication authorization logic.

```go
// Register a Security Layer
srv.Security(bungo.SecurityLayer{
    Name: "require_auth",
    Handler: func(req *bungo.Request) bool {
        token := req.Headers["Authorization"]
        return token == "super-secret-key"
    },
})

// Attach it to any Route
srv.Api(bungo.ApiRoute{
    Path:          "/secure-data",
    Version:       "v1",
    Method:        "GET",
    SecurityLayer: []string{"require_auth"}, // Blocks request instantly if it returns false
    Handler:       MySecureHandler,
})
```

Between layers and the final handler, you can safely pass data (like user profiles derived from JWTs) by modifying `req.Internal`!

---

## 🛠 Internal API: `bungo.Request`

To ensure BunGo can adapt anywhere (e.g., local HTTP server or Cloud Event invocations), endpoints receive a generic `*bungo.Request` instead of tightly bound standard library mechanisms like `*http.Request`.

```go
type Request struct {
    Headers  map[string]string // Normalized headers
    Params   map[string]string // Merged query string and URL parameters
    Body     []byte            // Raw body payload
    Internal map[string]any    // Sandbox to pass data across Security Layers
}
```

---

## 🎨 Writing React Views

Since BunGo replaces the traditional NPM frontend lifecycle, your JSX files run completely independent of external packagers. BunGo resolves `import "react"` to the server's embedded React and **auto-injects** the render helper.

Use **`_bungoRender(Component, elementId?)`** to mount your root component—no import needed. You do not need `react-dom/client` or `createRoot`. If `elementId` is omitted, it defaults to `"root"` (React convention).

```jsx
// web/views/loader.jsx
import React from "react";

function App() {
    // Access the data passed from the Go handler!
    const serverData = window.__BUNGO_DATA__;
    
    return (
        <div style={{ padding: "2rem", textAlign: "center" }}>
            <h1>{serverData.PageTitle}</h1>
            <p>Go combined with React, seamlessly.</p>
        </div>
    );
}

_bungoRender(App);  // mounts into #root by default; use _bungoRender(App, "my-el") for a different id
```

The matching `web/layouts/index.gohtml` simply needs a root div:
```html
<body>
    <div id="root"></div>
</body>
```

That's it! Save your files, boot your Go binary, and experience high-speed isomorphic development.
