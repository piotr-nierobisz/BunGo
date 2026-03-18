package builder

import (
	_ "embed"

	"github.com/evanw/esbuild/pkg/api"
)

//go:embed vendor/react.production.min.js
var reactJS string

//go:embed vendor/react-dom.production.min.js
var reactDOMJS string

// ReactPlugin intercepts imports for react and react-dom and feeds them the embedded JS
var ReactPlugin = api.Plugin{
	Name: "react-embed",
	Setup: func(build api.PluginBuild) {
		// Intercept "react"
		build.OnResolve(api.OnResolveOptions{Filter: `^react$`},
			func(args api.OnResolveArgs) (api.OnResolveResult, error) {
				return api.OnResolveResult{
					Path:      "react",
					Namespace: "react-ns",
				}, nil
			})

		build.OnLoad(api.OnLoadOptions{Filter: `^react$`, Namespace: "react-ns"},
			func(args api.OnLoadArgs) (api.OnLoadResult, error) {
				return api.OnLoadResult{
					Contents: &reactJS,
					Loader:   api.LoaderJS,
				}, nil
			})

		// Intercept "react-dom" and "react-dom/client"
		build.OnResolve(api.OnResolveOptions{Filter: `^react-dom(/client)?$`},
			func(args api.OnResolveArgs) (api.OnResolveResult, error) {
				return api.OnResolveResult{
					Path:      "react-dom",
					Namespace: "react-dom-ns",
				}, nil
			})

		build.OnLoad(api.OnLoadOptions{Filter: `^react-dom$`, Namespace: "react-dom-ns"},
			func(args api.OnLoadArgs) (api.OnLoadResult, error) {
				return api.OnLoadResult{
					Contents: &reactDOMJS,
					Loader:   api.LoaderJS,
				}, nil
			})

		// Support for JSXAutomatic injecting react/jsx-runtime
		build.OnResolve(api.OnResolveOptions{Filter: `^react/jsx-runtime$`},
			func(args api.OnResolveArgs) (api.OnResolveResult, error) {
				return api.OnResolveResult{
					Path:      "react/jsx-runtime",
					Namespace: "react-jsx-ns",
				}, nil
			})

		build.OnLoad(api.OnLoadOptions{Filter: `^react/jsx-runtime$`, Namespace: "react-jsx-ns"},
			func(args api.OnLoadArgs) (api.OnLoadResult, error) {
				contents := `import React from "react";

export const Fragment = React.Fragment;

// JSX automatic runtime passes key as a third argument. We need a shim
// instead of aliasing createElement directly, otherwise key is treated as
// a child and can render as text nodes (e.g. "0", "1", "2").
export function jsx(type, props, key) {
  if (key !== undefined) {
    const nextProps = props == null ? {} : Object.assign({}, props);
    nextProps.key = key;
    return React.createElement(type, nextProps);
  }
  return React.createElement(type, props);
}

export const jsxs = jsx;
`
				return api.OnLoadResult{
					Contents: &contents,
					Loader:   api.LoaderJS,
				}, nil
			})

		// bungo/render: provides _bungoRender(Component, elementId?) so views don't need createRoot boilerplate.
		// elementId defaults to "root" (React convention) when omitted.
		build.OnResolve(api.OnResolveOptions{Filter: `^bungo/render$`},
			func(args api.OnResolveArgs) (api.OnResolveResult, error) {
				return api.OnResolveResult{
					Path:      "bungo/render",
					Namespace: "bungo-render-ns",
				}, nil
			})

		bungoRenderJS := `import React from "react";
import { createRoot } from "react-dom";

export function useBungoData() {
  return window.__BUNGO_DATA__ || {};
}

export function _bungoRender(Component, elementId) {
  const id = elementId == null ? "root" : elementId;
  const el = document.getElementById(id);
  if (el) {
    const root = createRoot(el);
    root.render(React.createElement(Component));
  }
}
`
		build.OnLoad(api.OnLoadOptions{Filter: `^bungo/render$`, Namespace: "bungo-render-ns"},
			func(args api.OnLoadArgs) (api.OnLoadResult, error) {
				return api.OnLoadResult{
					Contents: &bungoRenderJS,
					Loader:   api.LoaderJS,
				}, nil
			})
	},
}
