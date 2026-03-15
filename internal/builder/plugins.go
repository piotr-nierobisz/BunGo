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
				contents := `export { createElement as jsx, createElement as jsxs, Fragment } from "react";`
				return api.OnLoadResult{
					Contents: &contents,
					Loader:   api.LoaderJS,
				}, nil
			})
	},
}
