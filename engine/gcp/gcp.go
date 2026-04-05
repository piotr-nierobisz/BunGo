package engine_gcp

import (
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/GoogleCloudPlatform/functions-framework-go/funcframework"
	"github.com/GoogleCloudPlatform/functions-framework-go/functions"
	bungo "github.com/piotr-nierobisz/BunGo"
	"github.com/piotr-nierobisz/BunGo/engine"
)

// GCPEngine implements Invoker for Google Cloud Functions
type GCPEngine struct {
	FunctionName string
}

// NewGCPEngine creates a GCP engine bound to a Cloud Functions HTTP entrypoint name.
// Inputs:
// - functionName: Cloud Function entrypoint identifier registered in deployment settings.
// Outputs:
// - *GCPEngine: engine instance that proxies BunGo HTTP handling into Functions Framework.
func NewGCPEngine(functionName string) *GCPEngine {
	return &GCPEngine{
		FunctionName: functionName,
	}
}

// Start registers BunGo routing as the GCP function handler and starts the framework server.
// Inputs:
// - address: listen address where the Functions Framework starts locally.
// - srv: BunGo server registry used to create the underlying HTTP handler.
// Outputs:
// - error: non-nil when handler initialization or framework startup fails.
func (e *GCPEngine) Start(address string, srv *bungo.Server) error {
	// 1. Initialize the HTTP routing setup from BunGo
	httpEngine := engine.NewHTTPEngine()
	handler, err := httpEngine.CreateHandler(srv)
	if err != nil {
		panic(fmt.Sprintf("BunGo GCPEngine Error: Initialization failed: %v", err))
	}

	// 2. Register the BunGo multiplexer as the single GCP Function HTTP entry point.
	functions.HTTP(e.FunctionName, func(w http.ResponseWriter, r *http.Request) {
		handler.ServeHTTP(w, r)
	})

	// Parse out port from address if present (e.g., ":3303" -> "3303")
	port := strings.TrimPrefix(address, ":")

	log.Printf("BunGo GCPEngine Info: Starting GCP Functions Framework on port %s", port)
	return funcframework.Start(port)
}
