package bungo

// SecurityLayer represents a reusable security layer that can be applied to routes.
type SecurityLayer struct {
	Name    string
	Handler func(req *Request) bool
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
// View is optional: the corresponding .jsx entry in views/ to be compiled and
// injected into the page as a module script.
type PageRoute struct {
	Path          string
	Template      string
	Layout        string
	View          string
	SecurityLayer []string
	Handler       func(req *Request) (map[string]any, error)
}

// ApiRoute represents a configuration for an API route
type ApiRoute struct {
	Path          string
	Version       string
	Method        string
	SecurityLayer []string
	Handler       func(req *Request) (APIResponse, error)
}
