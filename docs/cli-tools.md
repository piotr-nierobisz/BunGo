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
- Automatically runs `go run .`  to start your internal systems. (Can be overridden via `bungo dev --entry ./path/to/main.go`)
- Initiates an internal WebSocket at Port `35729` tracking live reload events.
- Hot-watches modifying `.go`, `.gohtml`, `.jsx`, `.tsx` files inside your environment. 
- Debounces save bursts, restarts the Go instance, and injects reconnect scripts that auto-refresh your browser view instantly on reconnect. 

It results in a flawless edit-save-refresh development loop that feels like magic.

Next: [Deployment](./deployment.md).
