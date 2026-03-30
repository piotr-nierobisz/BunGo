![BunGo logo](./docs/assets/header.png)

# BunGo (Bundler 4 Go)
**BunGo** is a high-performance, fullstack, and idiomatic Go web framework designed to seamlessly bridge backend Go logic with frontend React (JSX) views. It eliminates the need for Node.js, `npm`, `package.json`, and complex external build pipelines by embedding everything you need right into your Go binary.

If you love the simplicity of Go backends but want the rich interactivity of React without the headache of managing a separate frontend ecosystem, BunGo is the framework for you.

---

## 📚 Documentation

For complete guides, usage patterns, and best practices, **please refer to our new [docs directory](./docs/)**:

1. [Introduction to BunGo](./docs/introduction.md)
2. [Installation & Setup](./docs/installation.md)
3. [Routing and Pages](./docs/routing-and-pages.md)
4. [Templates and Layouts](./docs/templates-and-layouts.md)
5. [React Integration](./docs/react-integration.md)
6. [Security Layers](./docs/security-layers.md)
7. [CLI and Dev Tools](./docs/cli-tools.md)
8. [Deployment Options](./docs/deployment.md)
9. [AI-Assisted Development Guide](./docs/ai-guide.md)

---

## 📦 Quick Start & Installation

You can instantly scaffold a fresh BunGo application using our dedicated CLI tool.

> **Important Note**: To use the `bungo` CLI, your Go `bin` directory must be in your system's `PATH`. If you encounter a "command not found" error after installation, add `$HOME/go/bin` to your `PATH` (e.g., `export PATH=$PATH:$HOME/go/bin` in your `.zshrc` or `.bashrc`). For specific OS instructions, see the [Installation Guide](./docs/installation.md).
1. Install the CLI once:
   ```bash
   go install github.com/piotr-nierobisz/BunGo/cmd/bungo@latest
   ```

2. Scaffold a project and run the live-reload dev server:
   ```bash
   bungo init my-project
   cd my-project
   go mod tidy
   
   # Starts your server and continuously hot-reloads on changes!
   bungo dev

   # Builds a portable production binary in ./bin/
   bungo build
   ```
*(For a native TypeScript integration, add `--typescript` to the init command).*

For manual project setups without the CLI, [refer to the Installation Guide](./docs/installation.md).

---

## TODOs
- [x] Improved documentation
- [ ] Add unit tests
- [x] Cache resolved dependencies for faster startup
- [x] TypeScript support (`.tsx`/`.ts` views + CLI `--typescript` scaffold)
- [x] Allow for in browser optimisation by serving minified JS separate to the template 
- [x] Live reload for development (`bungo dev`)
- [x] More comments!
- [x] Add support for net/http ssl
- [ ] Add a template function to auto-inject jsx/tsx files e.g {{ bungoView "showcase.tsx" . }}
- [x] Add `bungo build` single-binary production packaging
- [ ] Add warnings and logging to the library