package bungo

// SecurityLayer represents a reusable security layer that can be applied to routes.
//
// Handler decides whether traffic may continue to the next layer and, on
// rejection, optionally shapes the response:
//
//   - return (true, _) to pass; a returned response is ignored on pass
//   - return (false, nil) to reject with the default 401 Unauthorized
//   - return (false, resp) to reject with resp — engines honor StatusCode
//     (0 defaults to 401), Headers, Cookies, and Body (nil writes no body,
//     anything else is JSON-encoded), so a layer can emit a 429 rate limit,
//     a 403, or a redirect (StatusCode 302 plus a "Location" header)
type SecurityLayer struct {
	Name    string
	Handler func(req *Request) (bool, *APIResponse)
}

// PageRoute configures a single page route.
//
// Template is required: the page-specific .gohtml file in layouts/ that holds the
// page content (and optionally {{define "content"}} when using a Layout).
//
// Layout is optional: a wrapper .gohtml in layouts/ that defines {{block "content" .}}.
// When set, the Template is rendered inside that block, so you avoid repeating
// <html>, <head>, and <body> in every template. If empty, the Template is rendered
// as a standalone page.
//
// View is optional: the corresponding .jsx/.tsx (or .js/.ts) entry in views/
// to be compiled and injected into the page as a module script.
type PageRoute struct {
	Path          string
	Template      string
	Layout        string
	View          string
	SecurityLayer []string
	Handler       func(req *Request) (map[string]any, error)
}

// ApiRoute represents a configuration for an API route.
//
// Method must be a standard HTTP verb (GET, HEAD, POST, PUT, PATCH, DELETE, CONNECT, OPTIONS, TRACE).
// Registration normalizes it to uppercase; invalid or empty values panic at Api registration time.
//
// CheckOrigin is optional: when set, engines call it before the security layers
// (read the caller's origin via req.Headers["Origin"]) and reject the request
// with 403 Forbidden when it returns false. When nil, no origin check runs —
// unlike WebSocket routes, API routes apply no default origin policy.
type ApiRoute struct {
	Path          string
	Version       string
	Method        string
	SecurityLayer []string
	CheckOrigin   func(req *Request) bool
	Handler       func(req *Request) (APIResponse, error)
}
