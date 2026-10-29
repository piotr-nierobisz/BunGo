// Package engine provides the net/http engines that run a bungo.Server.
//
// HTTPEngine serves plain HTTP and HTTPSEngine terminates TLS from a
// certificate and key file. On start, both compile the server's React views
// and then serve its pages, API routes, static files and WebSocket routes,
// applying the configured security layers and response headers.
//
// Other environments, such as a serverless platform or a test harness, can be
// supported by implementing the bungo.Engine interface.
package engine
