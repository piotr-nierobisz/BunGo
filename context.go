package bungo

import (
	"context"
	"time"
)

// Request represents a framework-agnostic HTTP request
type Request struct {
	Context  context.Context
	Headers  map[string]string
	Params   map[string]string // URL and Query parameters
	Body     []byte
	Internal map[string]any // For passing context between Security Layers and Handlers
}

// APIResponse represents an API response
type APIResponse struct {
	StatusCode int
	Body       any
	Cookies    []Cookie // Optional Set-Cookie headers emitted with the response
}

// SameSiteMode controls the SameSite attribute on a Cookie.
// An empty value omits the attribute and lets the user agent apply its default.
type SameSiteMode string

const (
	SameSiteDefault SameSiteMode = ""
	SameSiteLax     SameSiteMode = "Lax"
	SameSiteStrict  SameSiteMode = "Strict"
	SameSiteNone    SameSiteMode = "None"
)

// Cookie describes a Set-Cookie header in an engine-agnostic way.
//
// Name is required. To delete a cookie on the client, set MaxAge to a negative
// value — engines translate this to Max-Age=0 plus an expired Expires date.
//
// MaxAge semantics match net/http.Cookie:
//   - 0  — no Max-Age attribute is emitted
//   - >0 — Max-Age in seconds
//   - <0 — instructs the user agent to discard the cookie immediately
//
// This struct intentionally has no transport-specific methods. Each engine
// supplies its own cookie converter (see Engine implementations) so the core
// package remains free of net/http or vendor SDK dependencies.
type Cookie struct {
	Name     string
	Value    string
	Path     string
	Domain   string
	Expires  time.Time
	MaxAge   int
	Secure   bool
	HttpOnly bool
	SameSite SameSiteMode
}
