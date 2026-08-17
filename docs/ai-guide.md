# BunGo Framework — AI Agent Reference

This document is a comprehensive, self-contained reference for AI-assisted development inside a BunGo project. Copy the text below (everything inside the fenced block) into your project's AI rules file so that any coding agent understands the framework without additional exploration.

---

```text
## BunGo Framework Reference (for AI agents)

BunGo (Bundler 4 Go) is a fullstack Go web framework that pairs Go server logic with
React (JSX/TSX) views in a single repository. It embeds esbuild and the React runtime
directly into the Go binary — there is no Node.js, npm, package.json, or node_modules.

Go module: github.com/piotr-nierobisz/BunGo


### Project structure

Every BunGo web application using page routes requires a web directory (commonly
"./web") with the following mandatory sub-directories:

  web/
    layouts/     — Go HTML templates (.gohtml). Required on boot.
    views/       — React entry files (.jsx, .tsx, .js, .ts). Required on boot.
    static/      — Optional. Served as-is at /static/ by the HTTP engine.

When a non-empty webDir is passed to NewServer, the framework panics at startup if
layouts/ or views/ do not exist. If webDir is "" (API-only mode), no directories are
required and no static files are served.


### Core types (root package "bungo")

--- Server ---

  type Server struct {
      Pages          map[string]PageRoute
      APIs           map[string]ApiRoute
      WebSockets     map[string]WebSocketRoute
      SecurityLayers map[string]SecurityLayer
      StaticAliases  map[string]string   // root URL path -> file path relative to webDir/static
      Engine         Engine
      WebDir         string
      DefaultLayout  string
  }

  func NewServer(engine Engine, webDir string) *Server

    Creates a Server. Validates that webDir/layouts and webDir/views exist when webDir
    is non-empty (panics on failure).

  func (s *Server) Page(route PageRoute)

    Registers a page route. Panics if Template is empty, if the template file is
    missing on disk, or if the specified Layout or View file does not exist.

  func (s *Server) SetDefaultLayout(path string)

    Sets an optional default layout file for all page routes that do not specify their
    own Layout. The file must exist in webDir/layouts/ (panics otherwise).

  func (s *Server) SetAssetOptimization(enabled bool)

    Toggles production-oriented view asset delivery (default false). When enabled,
    templates reference content-hashed `/_bungo/<view>.<hash>.js` URLs instead of
    embedding full view bundles inline. The hash is derived from the compiled
    output, so the URL changes on every deploy that changes the bundle — engines
    serve these with `Cache-Control: public, max-age=31536000, immutable` and the
    hash busts stale browser caches. The URL is produced by
    bungo.OptimizedAssetPath(view, compiledJS); never hardcode it.

  func (s *Server) AssetOptimizationEnabled() bool

    Returns whether optimized JS asset delivery is enabled.

  func (s *Server) SetResponseHeaders(headers map[string]string)

    Sets global headers emitted on every response (pages, APIs, static files, and
    pre-upgrade WebSocket responses) — the place for CSP, HSTS, X-Frame-Options.
    A per-response APIResponse.Headers entry overrides a same-named global header.
    The map is copied and engines snapshot it at startup: call before Serve.

  func (s *Server) ResponseHeaders() map[string]string

    Returns the global response header set (engine-facing; treat as read-only).

  func (s *Server) Api(route ApiRoute)

    Registers an API route. The internal key is "Version:Method:Path".

  func (s *Server) StaticAlias(urlPath string, staticPath string)

    Publishes one file from webDir/static at an additional root URL (e.g.
    "/robots.txt" -> "robots.txt"). Panics unless urlPath starts with "/", carries
    the same file extension as staticPath (case-insensitive), avoids the reserved
    /static/, /api/ and /_bungo/ prefixes, and the target file already exists.

  func (s *Server) WebSocket(route WebSocketRoute) *WebSocketHub

    Registers a WebSocket route and returns its hub — the only handle to that hub,
    so capture it (a closure or a field) wherever you broadcast or publish; there is
    no lookup-by-path accessor. Panics unless Path starts with "/" and is not already
    registered as a WebSocket route.

  func (s *Server) Security(layer SecurityLayer)

    Registers a named security layer.

  func (s *Server) Serve(port int) error

    Delegates to Engine.Start with address ":port".

--- Engine interface ---

  type Engine interface {
      Start(address string, srv *Server) error
  }

--- Request ---

  type Request struct {
      Context  context.Context     // Request scoped context lifecycle
      Headers  map[string]string   // HTTP headers (first value per key)
      Params   map[string]string   // URL query parameters
      Body     []byte              // Raw request body
      Internal map[string]any      // Mutable bag for passing data between security
  }                                // layers and handlers

--- APIResponse ---

  type APIResponse struct {
      StatusCode int                 // 0 defaults to 200 on API routes, 401 on security rejections
      Body       any                 // Marshaled to JSON
      Headers    map[string]string   // Optional; each overrides a same-named global SetResponseHeaders entry
      Cookies    []Cookie            // Optional Set-Cookie headers emitted with the response
  }

  Security layers reuse APIResponse to shape custom rejection responses.

--- Cookie ---

  type Cookie struct {
      Name     string
      Value    string
      Path     string
      Domain   string
      Expires  time.Time
      MaxAge   int           // 0=unset, >0=seconds, <0=delete now
      Secure   bool
      HttpOnly bool
      SameSite SameSiteMode  // "", "Lax", "Strict", "None"
  }

  bungo.Cookie is transport-neutral. Each engine registers its own cookie
  converter callback in its constructor:
    - engine.NewHTTPEngine().SetCookieConverter(func(bungo.Cookie) *http.Cookie)
  Passing nil restores the engine's default converter. Custom engines apply the
  same pattern when serializing APIResponse.Cookies for their transport.

--- SecurityLayer ---

  type SecurityLayer struct {
      Name    string
      Handler func(req *Request) (bool, *APIResponse)
  }

  Return (true, nil) to pass, (false, nil) for the default HTTP 401 Unauthorized,
  or (false, &APIResponse{...}) to shape the rejection: StatusCode (0 → 401),
  Headers, Cookies, and Body (nil → no body, otherwise JSON-encoded) are honored,
  so a layer can emit a 429 rate limit, a 403, or a redirect-to-login
  (StatusCode 302 plus a "Location" header) on browser page routes.

--- PageRoute ---

  type PageRoute struct {
      Path          string
      Template      string               // Required: .gohtml file in layouts/
      Layout        string               // Optional: wrapper .gohtml in layouts/
      View          string               // Optional: .jsx/.tsx/.js/.ts in views/
      SecurityLayer []string             // Names of registered SecurityLayers
      Handler       func(req *Request) (map[string]any, error)
  }

  The Handler runs before rendering. The returned map is:
    - Available as Go template fields in the .gohtml (e.g. {{.PageTitle}}).
    - Serialized to JSON as window.__BUNGO_DATA__ and readable via useBungoData()
      in the React view.

--- ApiRoute ---

  type ApiRoute struct {
      Path          string               // Static path segment, e.g. "/users" — NOT :id / {id} patterns
      Version       string               // e.g. "v1"  → full path: /api/v1/users
      Method        string               // Standard verb only; normalized to uppercase at registration
      SecurityLayer []string
      CheckOrigin   func(req *Request) bool   // Optional; runs before security layers, false → 403 Forbidden.
                                              // nil = no origin check (no default policy, unlike WebSockets)
      Handler       func(req *Request) (APIResponse, error)
  }

  BunGo does NOT support path parameters (no /users/:id). Use query params (req.Params)

### Engines

BunGo ships two engine adapters (HTTP and HTTPS). Any other environment is
served by a small user-written engine implementing the Engine interface.

  HTTP engine (standard net/http)

    Import: github.com/piotr-nierobisz/BunGo/engine

      engineInstance := engine.NewHTTPEngine()
      srv := bungo.NewServer(engineInstance, "./web")
      srv.Serve(3303)   // listens on :3303

    Static file serving: If webDir/static/ exists, /static/... is served from
    BunGo AssetStorage (embedded memory first, disk fallback). Registered
    StaticAlias URLs (e.g. /robots.txt) are served the same way. Static requests
    do NOT pass through security layers.

    Global headers registered via srv.SetResponseHeaders are applied to every
    response this engine writes (pages, APIs, static, pre-upgrade WebSocket).

    WebSocket routes: served by this engine (and the HTTPS engine, which reuses
    its handler). Serverless environments cannot hold them.

  HTTPS engine (standard net/http TLS)

    Import: github.com/piotr-nierobisz/BunGo/engine

      httpsEngine := engine.NewHTTPSEngine("./certs/server.crt", "./certs/server.key")
      srv := bungo.NewServer(httpsEngine, "./web")
      srv.Serve(443)   // listens with TLS on :443

    This engine reuses BunGo's HTTP route handling and starts with
    http.ListenAndServeTLS(address, certFile, keyFile, handler).

  Custom engines

    Any type implementing bungo.Engine works as an engine:

      type Engine interface {
          Start(address string, srv *bungo.Server) error
      }

    srv.Serve(port) calls Engine.Start(":port", srv). The recommended pattern
    is to call engine.NewHTTPEngine().CreateHandler(srv) inside Start — it
    compiles all views and returns a standard http.Handler with pages, APIs,
    WebSockets, security layers, and static files fully wired — then host that
    handler however the target environment requires (custom http.Server,
    middleware wrapping, serverless platforms that accept an http.Handler).
    JSX view compilation is internal to the framework, so engines that bypass
    CreateHandler entirely are only suitable for API-only or template-only
    (no View) servers. See docs/deployment.md for full worked examples.

There are no cloud-specific engine modules (the former engine/aws and engine/gcp
were removed in v0.5.0); write a small custom engine (see above) instead.


### Templates and layouts

All .gohtml files live in webDir/layouts/.

  Template: The page-specific file. Always required on a PageRoute.
  Layout:   Optional wrapper. Defines {{block "content" .}}{{end}}.
            The Template fills it with {{define "content"}}...{{end}}.

If a Layout is set (per-route or via SetDefaultLayout), BunGo renders the Template
inside the Layout. Otherwise the Template is rendered standalone.

Handler data is available in templates as Go template fields. Example: if the handler
returns map[string]any{"PageTitle": "Hello"}, use {{.PageTitle}} in the template.

Script injection: BunGo automatically injects <script> tags for:
  - window.__BUNGO_DATA__ (the handler's map, JSON-serialized)
  - The compiled JSX bundle:
      - default: inline <script type="module">...</script>
      - optimized mode (SetAssetOptimization(true)): external
        <script type="module" src="/_bungo/<view>.<hash>.js"></script>

Injection point: before the first </head> tag; if absent, before </body>; if neither
exists, appended to the document. When a Layout is used, injection happens in the
layout file. NEVER manually place BunGo script tags — the framework handles this.


### React views

View files live in webDir/views/ and are compiled at server start via esbuild.
Supported extensions: .jsx, .tsx, .js, .ts.

BunGo uses the automatic JSX runtime (React 18.2.0 embedded). The compiler injects
two globals that view code can use without importing them:

  _bungoRender(Component, elementId?)
    Mounts the React Component via createRoot. elementId defaults to "root".

  useBungoData()
    Returns window.__BUNGO_DATA__ (the handler map) or {}.

Minimal view example (web/views/app.jsx):

  import React from "react";

  function App() {
    const data = useBungoData();
    return <h1>{data.PageTitle}</h1>;
  }

  _bungoRender(App);

TypeScript views: For type checking, add a bungo-env.d.ts to the project root:

  declare function useBungoData(): any;
  declare function _bungoRender(component: any, elementId?: string): void;

And a tsconfig.json including "web/views/**/*" and "bungo-env.d.ts".

Local imports: You can create additional directories like web/components/ and import
from views using relative paths (e.g. import { Button } from "../components/Button.jsx").

Remote URL imports (Deno-style): BunGo resolves https://... imports at build time.
Remote modules that reference React are normalized to the embedded runtime to prevent
duplicate React instances.

  import { LineChart } from "https://esm.sh/recharts@2.12.0";


### Security layers

Security layers are named middleware that run before page, API, or WebSocket handlers.

  srv.Security(bungo.SecurityLayer{
      Name: "require_auth",
      Handler: func(req *bungo.Request) (bool, *bungo.APIResponse) {
          if tooManyAttempts(req) {
              return false, &bungo.APIResponse{      // shaped rejection
                  StatusCode: 429,
                  Body:       map[string]any{"error": "rate limited"},
              }
          }
          if req.Headers["Authorization"] != "Bearer secret" {
              return false, nil   // → default HTTP 401 Unauthorized
          }
          req.Internal["UserID"] = 42   // pass data to later layers/handler
          return true, nil
      },
  })

Attach to routes via SecurityLayer: []string{"require_auth", "another_layer"}.
Layers execute in the order listed. If any returns false the chain stops and the
response is the returned *APIResponse, or HTTP 401 Unauthorized when it is nil.
A redirect-to-login for page routes is StatusCode 302 with Headers:
map[string]string{"Location": "/login"}.

req.Internal is a shared mutable map for passing data between layers and the final
handler (e.g. parsed JWT claims).


### Page routes

Page routes combine a Go handler, an HTML template, and an optional React view into a
single URL. Register them with srv.Page().

  srv.Page(bungo.PageRoute{
      Path:     "/",
      Template: "home.gohtml",
      Layout:   "base.gohtml",
      View:     "home.jsx",
      SecurityLayer: []string{"require_auth"},
      Handler: func(req *bungo.Request) (map[string]any, error) {
          userID := req.Internal["UserID"].(int)
          return map[string]any{
              "PageTitle": "Dashboard",
              "UserID":    userID,
              "Features":  []string{"Embedded React", "Go templates", "Security layers"},
          }, nil
      },
  })

What happens at request time:
  - Security layers run in order. If any rejects → its shaped response, or
    401 Unauthorized by default.
  - Handler executes. The returned map becomes both Go template data and
    window.__BUNGO_DATA__ for the React view.
  - The template (home.gohtml) is rendered. If a Layout is set, the template's
    {{define "content"}}...{{end}} block fills the layout's {{block "content" .}}.
  - The compiled View bundle and __BUNGO_DATA__ script are injected automatically
    (inline by default, external content-hashed /_bungo/<view>.<hash>.js when
    optimization is enabled).

Template is always required. Layout is optional (can be set per-route or globally
via SetDefaultLayout). View is optional — pages can be pure server-rendered HTML.
Handler is optional — if nil, the template renders with no data.

Custom not-found page: register a page at the sentinel path bungo.NotFoundPath
("bungo:404"). Template, Layout, View, Handler, and SecurityLayer all work, and
engines render it with HTTP 404 for any unmatched non-API GET path. Without it,
unmatched paths fall through to the "/" page (ServeMux subtree root semantics).

Example layout (web/layouts/base.gohtml):

  <!DOCTYPE html>
  <html lang="en">
  <head><title>{{.PageTitle}}</title></head>
  <body>
      {{block "content" .}}{{end}}
  </body>
  </html>

Example template (web/layouts/home.gohtml):

  {{define "content"}}
  <h1>{{.PageTitle}}</h1>
  <ul>
      {{range .Features}}<li>{{.}}</li>{{end}}
  </ul>
  <div id="root"></div>
  {{end}}

Example view (web/views/home.jsx):

  import React from "react";

  function Home() {
    const data = useBungoData();
    return (
      <div>
        <p>Welcome, user {data.UserID}</p>
        <ul>
          {data.Features.map((f, i) => <li key={i}>{f}</li>)}
        </ul>
      </div>
    );
  }

  _bungoRender(Home);


### API routes

API routes are registered with srv.Api(). The full HTTP path is /api/{Version}{Path}
(a leading slash is added to Path if missing). Example:

  srv.Api(bungo.ApiRoute{
      Path:    "/users",
      Version: "v1",
      Method:  "GET",
      Handler: func(req *bungo.Request) (bungo.APIResponse, error) {
          return bungo.APIResponse{StatusCode: 200, Body: map[string]any{"ok": true}}, nil
      },
  })

This registers GET /api/v1/users. The response Body is marshaled to JSON.
APIResponse.Headers adds per-response headers; APIResponse.Cookies plants cookies.
Set ApiRoute.CheckOrigin to validate req.Headers["Origin"] before the security
layers run — false yields 403 Forbidden; nil means no origin check.

Unmatched /api/ paths never fall through to page routes: they return a JSON 404,
or a 405 with an Allow header when the path exists under other methods.

Path parameters such as `/api/v1/users/:id` are NOT supported—use a fixed Path like
`/user` or `/users` and pass ids via query string (e.g. `?id=`) or request body.


### WebSocket routes

  hub := srv.WebSocket(bungo.WebSocketRoute{
      Path:          "/ws/feed",
      SecurityLayer: []string{"require_auth"},   // runs BEFORE the upgrade; failure = plain 401
      OnConnect:     func(conn *bungo.WebSocketConn) { /* connection registered in hub */ },
      OnMessage:     func(conn *bungo.WebSocketConn, message []byte) { /* per frame */ },
      OnDisconnect:  func(conn *bungo.WebSocketConn) { /* connection already unregistered */ },
  })

All callbacks are optional. *WebSocketConn: Request() (upgrade-time request with
Internal populated by security layers), Send([]byte), SendText(string),
SendJSON(any), Close(). Sends are buffered per connection and never block; a
connection too slow to drain its buffer is closed. WebSocketHub: Broadcast/
BroadcastText/BroadcastJSON (every connection), Publish/PublishText/
PublishJSON(topic, ...) (topic subscribers only), Subscribe/Unsubscribe(conn,
topic), ConnectionCount().
Keepalive (ping/pong, deadlines) is automatic. Default origin policy is same-host;
override with WebSocketRoute.CheckOrigin. Default inbound frame cap is 1 MiB;
override with WebSocketRoute.MaxMessageSize. HTTP/HTTPS engines only.


### API-only servers

To build a pure JSON API with no HTML pages:

  srv := bungo.NewServer(engineInstance, "")

Passing "" as webDir skips all directory validation and static-file mounting. Only
Api() routes can be registered (Page routes require templates on disk).


### Critical rules for AI agents

  - NEVER generate package.json, node_modules, or npm/yarn/pnpm commands.
    BunGo embeds everything. There is no Node.js toolchain.

  - NEVER manually place <script> tags for React bundles or __BUNGO_DATA__.
    The framework injects these automatically during rendering.

  - NEVER import database drivers, ORM libraries, or user-specific models into the
    BunGo framework itself. BunGo is infrastructure only. App-level dependencies
    belong in the project's go.mod, not in the framework.

  - View files MUST call _bungoRender(Component) to mount. They do NOT need to
    import _bungoRender or useBungoData — these are auto-injected by the compiler.

  - Template (.gohtml) is ALWAYS required on a PageRoute. Layout is optional.
    When using layouts, the Template must {{define "content"}}...{{end}} and the
    Layout must have {{block "content" .}}{{end}}.

  - Security layers rejecting with a nil response produce HTTP 401 Unauthorized.
    Return (false, &bungo.APIResponse{...}) to emit another status, headers
    (e.g. a 302 redirect via "Location"), cookies, or a JSON body instead.

  - Static files under web/static/ are public and bypass security layers.
    Never put secrets or user-specific files in static/.

  - API paths are automatically prefixed with /api/{Version}. Do not hardcode
    /api/ in the Path field.

  - Do NOT use Express-style or mux-style path parameters (:id, {id}, wildcards).
    BunGo matches exact paths only. Use query parameters (req.Params) or JSON body.

  - For TypeScript views, provide bungo-env.d.ts with declarations for
    useBungoData and _bungoRender.

  - When adding third-party frontend libraries, use Deno-style URL imports
    (e.g. https://esm.sh/package@version). Do not create a package.json.
```

---

*This page is part of the [BunGo documentation](./introduction.md).*
