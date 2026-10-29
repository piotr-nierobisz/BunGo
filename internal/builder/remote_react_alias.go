package builder

import (
	"net/url"
	"regexp"
	"strings"

	"github.com/evanw/esbuild/pkg/api"
)

// embeddedReactModules maps a React package and the entry point named by a CDN subpath to the
// embedded module serving it. Entry points missing here (react-dom/server, react/package.json, a
// file that merely lives under a "react" directory, ...) are not embedded and are fetched normally.
var embeddedReactModules = map[string]map[string]string{
	"react": {
		"":                      "react",
		"index":                 "react",
		"react":                 "react",
		"jsx-runtime":           "react/jsx-runtime",
		"react-jsx-runtime":     "react/jsx-runtime",
		"jsx-dev-runtime":       "react/jsx-dev-runtime",
		"react-jsx-dev-runtime": "react/jsx-dev-runtime",
	},
	"react-dom": {
		"":          "react-dom",
		"index":     "react-dom",
		"react-dom": "react-dom",
		"client":    "react-dom",
	},
}

// esmBuildVersionSegment matches esm.sh build-version prefixes such as "v135".
var esmBuildVersionSegment = regexp.MustCompile(`^v[0-9]+$`)

// esmTargetSegment matches esm.sh build target directories such as "es2022" or "denonext".
var esmTargetSegment = regexp.MustCompile(`^(es[0-9]+|esnext|deno|denonext|node)$`)

// resolveEmbeddedReactFromRemoteURL maps remote React-family URLs to embedded runtime modules.
// Inputs:
// - moduleURL: remote URL candidate that may represent react, react-dom, or a JSX runtime.
// Outputs:
// - api.OnResolveResult: resolve result that points to BunGo embedded React namespaces.
// - bool: true when moduleURL maps to an embedded React alias; otherwise false.
func resolveEmbeddedReactFromRemoteURL(moduleURL string) (api.OnResolveResult, bool) {
	switch classifyRemoteReactModule(moduleURL) {
	case "react":
		return api.OnResolveResult{Path: "react", Namespace: "react-ns"}, true
	case "react/jsx-runtime":
		return api.OnResolveResult{Path: "react/jsx-runtime", Namespace: "react-jsx-ns"}, true
	case "react/jsx-dev-runtime":
		return api.OnResolveResult{Path: "react/jsx-dev-runtime", Namespace: "react-jsx-dev-ns"}, true
	case "react-dom":
		return api.OnResolveResult{Path: "react-dom", Namespace: "react-dom-ns"}, true
	default:
		return api.OnResolveResult{}, false
	}
}

// classifyRemoteReactModule reports which embedded React module a CDN URL refers to.
// Only the package position of the supported CDN URL layouts is inspected:
//   - /<pkg>[@version][/subpath]            esm.sh, unpkg, esm.run, Skypack
//   - /{stable,vNNN}/<pkg>@version/subpath  esm.sh build paths
//   - /npm/<pkg>[@version][/subpath]        jsDelivr
//   - /npm:<pkg>@version/subpath            JSPM
//   - /{-,pin}/<pkg>@version/subpath        Skypack internal and pinned paths
//
// The package must be exactly react or react-dom, and the subpath (ignoring CDN build
// directories) must name an embedded entry point.
// Inputs:
// - moduleURL: remote URL inspected for a React package in package position.
// Outputs:
// - string: `react`, `react/jsx-runtime`, `react/jsx-dev-runtime`, `react-dom`, or empty.
func classifyRemoteReactModule(moduleURL string) string {
	parsed, err := url.Parse(moduleURL)
	if err != nil {
		return ""
	}

	segments := strings.Split(strings.Trim(strings.ToLower(parsed.Path), "/"), "/")
	if len(segments) > 1 && isCDNPrefixSegment(segments[0]) {
		segments = segments[1:]
	}

	entries, ok := embeddedReactModules[remotePackageName(segments[0])]
	if !ok {
		return ""
	}
	entry, ok := reactEntryPoint(segments[1:])
	if !ok {
		return ""
	}
	return entries[entry]
}

// isCDNPrefixSegment reports whether a leading path segment is CDN routing rather than a package.
// Inputs:
// - segment: first lowercase URL path segment.
// Outputs:
// - bool: true for jsDelivr `npm`, esm.sh `stable`/`vNNN`, and Skypack `-`/`pin` prefixes.
func isCDNPrefixSegment(segment string) bool {
	switch segment {
	case "npm", "stable", "-", "pin":
		return true
	}
	return esmBuildVersionSegment.MatchString(segment)
}

// remotePackageName extracts the bare package name from a package-position path segment.
// Inputs:
// - segment: lowercase path segment such as `react@18.2.0`, `npm:react@18.2.0`, or `*react-dom@18`.
// Outputs:
// - string: package name without registry prefix, external marker, or version (scoped names yield "").
func remotePackageName(segment string) string {
	segment = strings.TrimPrefix(segment, "npm:")
	segment = strings.TrimPrefix(segment, "*")
	name, _, _ := strings.Cut(segment, "@")
	return name
}

// reactEntryPoint reduces a package subpath to the entry point it names.
// Inputs:
// - subpath: lowercase path segments after the package segment.
// Outputs:
// - string: entry name with CDN build directories and file extensions removed (empty for the main entry).
// - bool: false when the subpath points deeper than a single entry file.
func reactEntryPoint(subpath []string) (string, bool) {
	var rest []string
	for _, segment := range subpath {
		if !isCDNBuildSegment(segment) {
			rest = append(rest, segment)
		}
	}
	switch len(rest) {
	case 0:
		return "", true
	case 1:
		// Entry names never contain dots, so this drops ".js", ".mjs", ".production.min.js", ...
		entry, _, _ := strings.Cut(rest[0], ".")
		return entry, true
	default:
		return "", false
	}
}

// isCDNBuildSegment reports whether a subpath segment is a CDN build directory rather than an entry.
// Inputs:
// - segment: lowercase path segment inside a package.
// Outputs:
// - bool: true for build output directories (umd, cjs, +esm, es2022, ...) and encoded build options.
func isCDNBuildSegment(segment string) bool {
	switch segment {
	case "", "umd", "cjs", "esm", "dist", "+esm", "optimized":
		return true
	}
	// Skypack encodes build options as key=value segments; esm.sh encodes build args as "X-...".
	return strings.Contains(segment, "=") || strings.HasPrefix(segment, "x-") || esmTargetSegment.MatchString(segment)
}
