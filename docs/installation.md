# Installation & Setup

## Automatic setup (Recommended)
You can instantly scaffold a fresh BunGo application using our dedicated CLI tool.

**Important Note**: The `bungo` CLI is installed into your Go environment's `bin` directory. If you type `bungo` and get a "command not found" error after installation, you need to add the Go binary path to your system's `PATH`.

- **macOS / Linux (Zsh)**: Run `echo 'export PATH=$PATH:$HOME/go/bin' >> ~/.zshrc` and restart your terminal.
- **macOS / Linux (Bash)**: Run `echo 'export PATH=$PATH:$HOME/go/bin' >> ~/.bashrc` and restart your terminal.
- **Windows**: Search for "Environment Variables" in the Start Menu, edit your user `PATH` variable, and add `%USERPROFILE%\go\bin`.

1. Install the CLI tool globally:
   ```bash
   go install github.com/piotr-nierobisz/BunGo/cmd/bungo@latest
   ```

2. Scaffold a generic React/JavaScript application:
   ```bash
   bungo init my-project
   cd my-project
   go mod tidy
   bungo dev
   ```

3. (Optional) Scaffold a React/TypeScript application:
   ```bash
   bungo init my-project --typescript
   ```

## Manual Setup
If you're migrating an existing Go project or prefer absolute control:

1. Initialize Go module:
   ```bash
   mkdir myapp && cd myapp
   go mod init myapp
   ```

2. Add the framework:
   ```bash
   go get github.com/piotr-nierobisz/BunGo
   ```

3. Create the mandatory directory structure:
   ```text
   myapp/
   ├── main.go
   └── web/
       ├── layouts/   # required for HTML templates
       ├── views/     # required for React components
       └── static/    # optional: served at /static/
   ```

> **Warning:** BunGo strictly expects the `web/layouts` and `web/views` folders to exist on server startup; if missing, your application will panic at boot to fail-fast.

Next, read about [Routing and Pages](./routing-and-pages.md).
