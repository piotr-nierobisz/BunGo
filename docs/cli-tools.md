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
- Recursively watches the project tree (skipping dirs such as `.git`, `vendor`, `bin`, `dist`, and other ignored paths in `internal/dev/watcher.go`) and restarts when relevant files change: **`.go`**, **.sum`**, **`.gohtml`**, **`.html`**, **`.css`**, **`.js`**, **`.jsx`**, **`.ts`**, **`.tsx`**.`.mod`**, **`
- Debounces bursty saves (~200ms), restarts the Go process, disconnects WebSocket clients, and relies on the injected reconnect script so the browser refreshes once the app is back up.

It results in a flawless edit-save-refresh development loop that feels like magic.

Next: [Deployment](./deployment.md).
