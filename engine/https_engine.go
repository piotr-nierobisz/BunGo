package engine

import (
	"fmt"
	"net/http"

	bungo "github.com/piotr-nierobisz/BunGo"
)

// HTTPSEngine implements bungo.Engine for TLS-enabled net/http serving.
type HTTPSEngine struct {
	httpEngine *HTTPEngine
	certFile   string
	keyFile    string
}

// NewHTTPSEngine creates an HTTPS engine configured with certificate and private key files.
// Inputs:
// - certFile: filesystem path to the PEM-encoded TLS certificate file.
// - keyFile: filesystem path to the PEM-encoded TLS private key file.
// Outputs:
// - *HTTPSEngine: engine instance ready to serve BunGo routes over HTTPS.
func NewHTTPSEngine(certFile, keyFile string) *HTTPSEngine {
	return &HTTPSEngine{
		httpEngine: NewHTTPEngine(),
		certFile:   certFile,
		keyFile:    keyFile,
	}
}

// Start creates the HTTP handler and starts a TLS listener on the provided address.
// Inputs:
// - address: network listen address passed to net/http, for example ":443".
// - srv: BunGo server registry used to create handlers and route dispatchers.
// Outputs:
// - error: non-nil when handler creation fails, TLS files are missing, or the server exits with an error.
func (e *HTTPSEngine) Start(address string, srv *bungo.Server) error {
	if e.certFile == "" {
		return fmt.Errorf("BunGo HTTPS Error: certFile is required")
	}
	if e.keyFile == "" {
		return fmt.Errorf("BunGo HTTPS Error: keyFile is required")
	}

	handler, err := e.httpEngine.CreateHandler(srv)
	if err != nil {
		return err
	}

	return http.ListenAndServeTLS(address, e.certFile, e.keyFile, handler)
}
