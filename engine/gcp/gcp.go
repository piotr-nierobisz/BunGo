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

// NewGCPEngine creates a new GCP adapter. The functionName must match
// the entry point you define in the GCP Cloud Functions console.
func NewGCPEngine(functionName string) *GCPEngine {
	return &GCPEngine{
		FunctionName: functionName,
	}
}

// Start registers the framework with GCP and starts the local framework server.
func (e *GCPEngine) Start(address string, srv *bungo.Server) error {
	// 1. Initialize the HTTP routing setup from BunGo
	httpEngine := engine.NewHTTPEngine()
	handler, err := httpEngine.CreateHandler(srv)
	if err != nil {
		panic(fmt.Sprintf("GCPEngine Initialization Error: %v", err))
	}

	// 2. Register the BunGo multiplexer as the single GCP Function HTTP entry point.
	functions.HTTP(e.FunctionName, func(w http.ResponseWriter, r *http.Request) {
		handler.ServeHTTP(w, r)
	})

	// Parse out port from address if present (e.g., ":3303" -> "3303")
	port := strings.TrimPrefix(address, ":")

	log.Printf("Starting GCP Functions Framework on port %s", port)
	return funcframework.Start(port)
}
