// Package devreload holds the live-reload contract shared by the bungo dev
// runner, the HTML renderer and the engines.
//
// `bungo dev` starts the app with EnvVar set. The renderer then injects a
// client that connects to Path on the page's own origin, and engines mount
// Handler at Path. The socket carries no messages: restarting the app closes
// it, and the client reloads once the new process is serving. Because the
// endpoint shares the page's scheme, host and port, it works over HTTPS, LAN
// addresses, proxies and tunnels without a separate port or certificate.
package devreload

import (
	"net/http"
	"os"

	"github.com/gorilla/websocket"
)

// EnvVar is set to "1" by `bungo dev` in the environment of the app process.
const EnvVar = "BUNGO_DEV_ENABLED"

// Path is the live-reload websocket endpoint mounted on the app's own server.
const Path = "/__bungo"

// upgrader accepts any origin: the socket only ever signals a reload by
// closing, and proxies or tunnels that rewrite Host must keep working.
var upgrader = websocket.Upgrader{
	CheckOrigin: func(*http.Request) bool { return true },
}

// Enabled reports whether the current process was started by `bungo dev`.
// Inputs:
// - none
// Outputs:
// - bool: true when EnvVar is set to "1"; otherwise false.
func Enabled() bool {
	return os.Getenv(EnvVar) == "1"
}

// Handler upgrades a request to the live-reload websocket and holds it open
// until the client disconnects or the app process exits.
// Inputs:
// - w: response writer for the upgrade handshake.
// - r: incoming websocket upgrade request.
// Outputs:
// - none
func Handler(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return // Upgrade has already answered with an HTTP error
	}
	defer conn.Close()

	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
	}
}
