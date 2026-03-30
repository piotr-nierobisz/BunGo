# CLI and Dev Tools

The BunGo CLI creates seamless development pipelines, letting you iterate on backend and frontend logic in absolute real time without manually refreshing!

## Project Scaffolding
Create boilerplates filled with example handlers, React components, and template documents!

```bash
# General Setup
bungo init my-project

# Typescript First Setup
bungo init my-project --typescript
```

## Running the Dev Server (`bungo dev`)
Inside your project directory, execute the development environment!

```bash
bungo dev
```

### What does it do?
- Runs `go run <entry>` from the project root with a default entry of `.` (same as `go run .`). Override the package or file with `bungo dev --entry ./cmd/server` or similar.
- Starts a WebSocket server on port **35729** at path **`/__bungo`** (fixed; see `bungo.DevWebSocketPort`). The dev runner notifies browsers on that endpoint so they reconnect and reload after restarts.
- Recursively watches the project tree (skipping dirs such as `.git`, `vendor`, `bin`, `dist`, and other ignored paths in `internal/dev/watcher.go`) and restarts when relevant files change: **`.go`**, **`.mod`**, **`.sum`**, **`.gohtml`**, **`.html`**, **`.css`**, **`.js`**, **`.jsx`**, **`.ts`**, and **`.tsx`**.
- Debounces bursty saves (~200ms), restarts the Go process, disconnects WebSocket clients, and relies on the injected reconnect script so the browser refreshes once the app is back up.

It results in a flawless edit-save-refresh development loop that feels like magic.

## Building Portable Binaries (`bungo build`)
`bungo build` packages your BunGo app for production and auto-embeds your registered web assets into the binary:

- templates from `<webDir>/layouts`
- views from `<webDir>/views`
- static files from `<webDir>/static`

No app code changes are required. BunGo uses memory-first asset reads at runtime, with disk fallback for normal local development workflows.

```bash
# Build using default entry (.) and output to ./bin/<project-name>
bungo build

# Build a specific entry package
bungo build --entry ./cmd/server

# Build from a specific entry file (BunGo normalizes this to the containing package)
bungo build --entry ./cmd/server/main.go

# Build to an explicit output path
bungo build --output ./dist/my-app

# Manually choose a web root to embed (disables auto-discovery)
bungo build --web-dir ./web
```

By default, `bungo build` auto-discovers web roots by scanning your entry package for `NewServer(..., "webDir")` string literals. Relative literals (for example `./web`) are resolved from the project root (the directory where you run `bungo build`), then BunGo falls back to `./web` when no match is found.  
When `--entry` points to a `.go` file, BunGo builds the containing package so generated embed imports are linked correctly.
If discovery resolves a path that does not exist, the build now fails with a clear error instead of producing a binary with empty embedded assets.
When `--web-dir` is provided, this discovery step is skipped and BunGo embeds only that manually supplied root.

Next: [Deployment](./deployment.md).
