package engine

import (
	"io"
	"net/http"

	bungo "github.com/piotr-nierobisz/BunGo"
)

// translateRequest converts a net/http request into BunGo's framework-agnostic request.
// Inputs:
// - r: incoming net/http request to translate into BunGo request fields.
// Outputs:
// - *bungo.Request: translated request including headers, query params, body, and internal map.
// - error: non-nil when request body reading fails.
func (e *HTTPEngine) translateRequest(r *http.Request) (*bungo.Request, error) {
	breq := &bungo.Request{
		Context:  r.Context(),
		Headers:  make(map[string]string),
		Params:   make(map[string]string),
		Internal: make(map[string]any),
	}

	for k, v := range r.Header {
		if len(v) > 0 {
			breq.Headers[k] = v[0]
		}
	}

	for k, v := range r.URL.Query() {
		if len(v) > 0 {
			breq.Params[k] = v[0]
		}
	}

	if r.Body != nil {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			return nil, err
		}
		breq.Body = body
	}

	return breq, nil
}
