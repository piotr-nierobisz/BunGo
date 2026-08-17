// Package wsbridge is the internal seam BunGo's engine packages use to resolve a
// registered WebSocket route's hub, without the core package exposing that lookup
// on its public API.
//
// Because it lives under internal/, application code cannot import it: the only
// developer-facing handle to a hub is the value returned by Server.WebSocket.
// Engines, being separate packages, cannot read the core Server's unexported hub
// registry directly, so package bungo installs HubFor here for them to call.
package wsbridge

// HubFor returns the *bungo.WebSocketHub registered for path on srv (a *bungo.Server),
// or nil when no WebSocket route is registered at that path. Package bungo installs
// the implementation from its init; callers type-assert the result back to
// *bungo.WebSocketHub.
//
// The parameters and result are typed as any so this package imports nothing from
// bungo — a bungo import here would form a cycle, since bungo imports this package
// to install HubFor.
var HubFor func(srv any, path string) any
