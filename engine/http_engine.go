package engine

import (
	"net/http"

	bungo "github.com/piotr-nierobisz/BunGo"
)

// HTTPCookieConverter maps a bungo.Cookie into a net/http cookie value.
// Engines call the converter once per APIResponse.Cookies entry before writing
// the response, so applications can override it to customize attribute mapping
// (for example, forcing Secure=true behind a proxy or rewriting Domain).
type HTTPCookieConverter func(bungo.Cookie) *http.Cookie

// HTTPEngine implements Invoker for net/http server
type HTTPEngine struct {
	compiledViews   map[string]string
	optimizedAssets map[string]string
	cookieConverter HTTPCookieConverter
}

// NewHTTPEngine creates an HTTP engine with initialized compiled-view caches.
// The constructor injects the engine's default cookie converter; callers can
// replace it with SetCookieConverter to customize Set-Cookie emission.
// Inputs:
// - none
// Outputs:
// - *HTTPEngine: engine instance ready to compile routes and serve requests.
func NewHTTPEngine() *HTTPEngine {
	return &HTTPEngine{
		compiledViews:   make(map[string]string),
		optimizedAssets: make(map[string]string),
		cookieConverter: DefaultHTTPCookieConverter,
	}
}

// SetCookieConverter overrides the engine's bungo.Cookie → *http.Cookie mapper.
// A nil converter restores the default implementation.
// Inputs:
// - converter: replacement converter callback, or nil to restore defaults.
// Outputs:
// - none
func (e *HTTPEngine) SetCookieConverter(converter HTTPCookieConverter) {
	if converter == nil {
		e.cookieConverter = DefaultHTTPCookieConverter
		return
	}
	e.cookieConverter = converter
}

// DefaultHTTPCookieConverter mirrors a bungo.Cookie onto net/http.Cookie fields.
// Inputs:
// - c: source cookie populated by an API handler.
// Outputs:
// - *http.Cookie: standard-library cookie ready for http.SetCookie.
func DefaultHTTPCookieConverter(c bungo.Cookie) *http.Cookie {
	hc := &http.Cookie{
		Name:     c.Name,
		Value:    c.Value,
		Path:     c.Path,
		Domain:   c.Domain,
		Expires:  c.Expires,
		MaxAge:   c.MaxAge,
		Secure:   c.Secure,
		HttpOnly: c.HttpOnly,
	}
	switch c.SameSite {
	case bungo.SameSiteLax:
		hc.SameSite = http.SameSiteLaxMode
	case bungo.SameSiteStrict:
		hc.SameSite = http.SameSiteStrictMode
	case bungo.SameSiteNone:
		hc.SameSite = http.SameSiteNoneMode
	default:
		hc.SameSite = http.SameSiteDefaultMode
	}
	return hc
}

// Start creates the HTTP handler and starts listening on the provided address.
// Inputs:
// - address: network listen address passed to net/http, for example ":3303".
// - srv: BunGo server registry used to create handlers and route dispatchers.
// Outputs:
// - error: non-nil when handler creation fails or the HTTP server exits with an error.
func (e *HTTPEngine) Start(address string, srv *bungo.Server) error {
	handler, err := e.CreateHandler(srv)
	if err != nil {
		return err
	}

	return http.ListenAndServe(address, handler)
}
