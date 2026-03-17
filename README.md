![BunGo logo](./docs/header.png)



# BunGo (Bundler 4 Go) 🐇
**BunGo** is a high-performance, lightweight, and idiomatic Go web framework designed to seamlessly bridge backend Go logic with frontend React (JSX) views. It eliminates the need for Node.js, `npm`, `package.json`, and complex external build pipelines by embedding everything you need right into your Go binary.

If you love the simplicity of Go backends but want the rich interactivity of React without the headache of managing a separate frontend ecosystem, BunGo is the framework for you.

---

## 📦 Installation & project setup

### Install the library

From your Go module:

```bash
go get github.com/piotr-nierobisz/BunGo
```

For a new project, create a module first:

```bash
mkdir myapp && cd myapp
go mod init myapp
go get github.com/piotr-nierobisz/BunGo
```

Optional engines (add only if you use them):

```bash
go get github.com/piotr-nierobisz/BunGo/engine/gcp   # Google Cloud Functions
go get github.com/piotr-nierobisz/BunGo/engine/aws   # AWS Lambda
```

### Set up the project

1. **Create the web directory** and required subdirectories next to your `main.go` (or wherever you pass as `webDir`):

   ```text
   your-app/
   ├── main.go
   └── web/
       ├── layouts/   # required
       └── views/     # required
   ```

2. **Add at least one layout file** in `web/layouts/` (e.g. `index.gohtml`). This is the page template that BunGo will render for the routes you register.

3. **In your code**, create an engine, pass the path to `web` (e.g. `"./web"`), and call `srv.Serve(port)` as shown in the Quick Start below.

The server will **panic at startup** if `webDir`, `web/layouts/`, or `web/views/` are missing, so you get immediate feedback if the layout is wrong.

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

### Google Cloud Functions Adapter

Add the engine: `go get github.com/piotr-nierobisz/BunGo/engine/gcp`

Compared to the standard HTTP setup, only the import and engine creation change:

```go
import engine_gcp "github.com/piotr-nierobisz/BunGo/engine/gcp"

// In main():
gcpEngine := engine_gcp.NewGCPEngine("MyCloudFunction")  // name must match gcloud entry point
srv := bungo.NewServer(gcpEngine, "")
// ... register routes, then srv.Serve(8080) as usual
```

### AWS Lambda Adapter

Add the engine: `go get github.com/piotr-nierobisz/BunGo/engine/aws`

Use API Gateway HTTP API (v2) or Lambda Function URL. Only the import and engine change:

```go
import engine_aws "github.com/piotr-nierobisz/BunGo/engine/aws"

// In main():
awsEngine := engine_aws.NewLambdaEngine()
srv := bungo.NewServer(awsEngine, "")
// ... register routes, then srv.Serve(port) as usual
```

For local Lambda testing use AWS SAM CLI or Lambda RIE.

---

### 2. Registering Page Routes

Pages return an HTML document to the client. BunGo allows you to compose these intuitively using Go's built-in templating features combined with automated React injection.

```go
srv.Page(bungo.PageRoute{
    Path:     "/",
    Template: "index.gohtml", // Required: Maps to web/layouts/index.gohtml
    View:     "loader.jsx",   // Optional: Maps to web/views/loader.jsx
    
    // The handler executes the backend logic before rendering.
    // The returned map[string]any is injected and accessible in JSX via useBungoData().
    Handler: func(req *bungo.Request) (map[string]any, error) {
        return map[string]any{
            "PageTitle": "Welcome to BunGo",
            "InitialData": []int{1, 2, 3},
        }, nil
    },
})
```

#### How templates work

BunGo uses Go’s standard `html/template` for server-side HTML. Each page route specifies a **Template** (a `.gohtml` file in `web/layouts/`) and optionally a **Layout** that wraps it.

- **Template:** The page-specific file (e.g. `index.gohtml`) that holds the content for that route. It is always required.
- **Layout (optional):** A wrapper file (e.g. `base.gohtml`) that defines the common shell (`<html>`, `<head>`, `<body>`) and a `{{block "content" .}}`. Your template then defines `{{define "content"}}...{{end}}` so BunGo fills that block. Use `Layout` on the route or `srv.SetDefaultLayout("base.gohtml")` so you don’t repeat boilerplate in every template.
- **Standalone vs layout:** If you don’t set a Layout, the template is executed on its own. If you set one, the layout is executed and the template only provides the `"content"` block.

**Handler data and template binding:** The `map[string]any` returned from the page **Handler** is used in two places:

1. **Server-side (Go templates):** The same map is passed as the template data when rendering the `.gohtml` file. You can use any key in your template with `{{.KeyName}}` or `{{range .Items}}...{{end}}`, etc. So handler data drives both structure and content of the HTML.

2. **Client-side (React):** The same map is serialized to JSON and injected into the page, and your JSX can access it through the auto-injected `useBungoData()` helper.

Example: if your handler returns `map[string]any{"PageTitle": "Welcome", "Items": []string{"a","b"}}`, then in `index.gohtml` you can write `{{.PageTitle}}` and `{{range .Items}}<li>{{.}}</li>{{end}}`, and in JSX you can use `useBungoData().PageTitle` and `useBungoData().Items`. One handler, one data source, used in both template and view.

- **Automated script injection:** You do **not** add `<script>` tags for the React bundle or for `__BUNGO_DATA__` yourself. BunGo injects them (before `</head>` or `</body>`) when it renders the template.

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

BunGo also supports Deno-style remote ESM imports directly in your JSX files. You can import modules from `https://...` URLs (for example from `esm.sh`) without adding `package.json` or `node_modules`:

```jsx
import { LineChart, Line, XAxis, YAxis, CartesianGrid, Tooltip } from "https://esm.sh/recharts@2.12.0";
import { format } from "https://esm.sh/date-fns@3.6.0";
```

When remote dependencies reference `react`, `react/jsx-runtime`, or `react-dom/client`, BunGo automatically aliases them to the embedded runtime so hooks and context use a single React instance.

For a runnable demo, see `examples/http_remote_imports`:

```bash
cd examples/http_remote_imports
go run .
```

Use **`_bungoRender(Component, elementId?)`** to mount your root component and **`useBungoData()`** to read server data—no imports needed. You do not need `react-dom/client` or `createRoot`. If `elementId` is omitted, it defaults to `"root"` (React convention).

```jsx
// web/views/loader.jsx
import React from "react";

function App() {
    // Access the data passed from the Go handler!
    const serverData = useBungoData();
    
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


# TODOs
- [ ] Add more detailed documentation and examples
- [ ] Add testing utilities and examples
- [ ] Cache resolved dependencies for faster startup
- [ ] Typescript support
