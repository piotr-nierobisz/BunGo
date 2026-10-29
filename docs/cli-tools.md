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

The project name becomes both the new directory and the `module` path in the generated `go.mod`, so it must be a single name that `go mod init` would accept:

- only ASCII letters, digits, `-`, `_`, `.`, `~` and `+` (no spaces or other punctuation)
- no leading `-`, no trailing `.`, and not just dots (`.` or `..`)
- not a reserved Windows device name such as `con`, `nul`, `com1` or `lpt1` (even with an extension), and not ending in a Windows short-name suffix like `~1`

`bungo init` validates the name before creating anything, and refuses to run if a file, directory or symlink with that name already exists.

## Running the Dev Server (`bungo dev`)
Inside your project directory, execute the development environment!

```bash
bungo dev
```

### What does it do?
- Runs `go run <entry>` from the project root with a default entry of `.` (same as `go run .`). Override the package or file with `bungo dev --entry ./cmd/server` or similar.
- Starts the app with `BUNGO_DEV_ENABLED=1`, which makes the engine mount a live-reload WebSocket at **`/__bungo`** on the app's own server and inject a small client into every page. Because the endpoint shares the page's origin, live reload works over HTTPS (`wss://` with your app's certificate), from other devices on your LAN, and through proxies or tunnels; no extra port is opened.
- Recursively watches the project tree (skipping dirs such as `.git`, `vendor`, `bin`, `dist`, and other ignored paths in `internal/dev/watcher.go`) and restarts when relevant files change: **`.go`**, **`.mod`**, **`.sum`**, **`.gohtml`**, **`.html`**, **`.css`**, **`.js`**, **`.jsx`**, **`.ts`**, and **`.tsx`**.
- Debounces bursty saves (~200ms) and restarts the Go process. The old process exiting closes each page's live-reload socket, and the injected script reconnects and refreshes the browser once the new process is serving.

It results in a flawless edit-save-refresh development loop that feels like magic.

## Building Portable Binaries (`bungo build`)
`bungo build` packages your BunGo app for production and auto-embeds your registered web assets into the binary:

- templates from `<webDir>/layouts`
- views from `<webDir>/views`
- static files from `<webDir>/static`

Files and folders whose names start with `_` (for example `static/_app.css` or `layouts/_partials/`) are embedded like any other asset. Anything whose name starts with `.` (`.DS_Store`, `.env`, `.git`, editor swap files) is always left out of the binary. Web directory names may contain spaces.

No app code changes are required. BunGo uses memory-first asset reads at runtime, with disk fallback for normal local development workflows.

```bash
# Build using default entry (.) and output to ./bin/<project-name> (.exe is appended when GOOS=windows)
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

By default, `bungo build` auto-discovers web roots by scanning your entry package for `bungo.NewServer(..., webDir)` calls. Only calls to BunGo's own `NewServer` count, whether the package is imported under its default name, an alias, or as a dot-import, so `NewServer` functions from other packages are ignored, as are `_test.go` files. The web dir can be a string literal or a package-level string constant (constants built with `+` included, even when declared in another file of the same package). Relative paths (for example `./web`) are resolved from the project root (the directory where you run `bungo build`), then BunGo falls back to `./web` when no match is found, for example when the web dir comes from a flag or environment variable.  
When `--entry` points to a `.go` file, BunGo builds the containing package so generated embed imports are linked correctly.
If discovery resolves a path that does not exist, the build now fails with a clear error instead of producing a binary with empty embedded assets.
When `--web-dir` is provided, this discovery step is skipped and BunGo embeds only that manually supplied root.

Next: [Deployment](./deployment.md).
