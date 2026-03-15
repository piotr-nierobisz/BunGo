package bungo

// Request represents a framework-agnostic HTTP request
type Request struct {
	Headers  map[string]string
	Params   map[string]string // URL and Query parameters
	Body     []byte
	Internal map[string]any // For passing context between Security Layers and Handlers
}

// APIResponse represents an API response
type APIResponse struct {
	StatusCode int
	Body       any
}
