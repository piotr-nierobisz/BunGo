package builder

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/evanw/esbuild/pkg/api"
	bungo "github.com/piotr-nierobisz/BunGo"
)

// OptimizedAssetPath converts a view entry path into the cacheable optimized asset route.
// Inputs:
// - view: view entry path relative to the views directory, including extension.
// Outputs:
// - string: normalized `/_bungo/...js` route used for optimized asset serving.
func OptimizedAssetPath(view string) string {
	withoutExt := strings.TrimSuffix(view, filepath.Ext(view))
	normalized := strings.ReplaceAll(withoutExt, "\\", "/")
	normalized = strings.TrimPrefix(normalized, "/")
	return "/_bungo/" + normalized + ".js"
}

// CompilePages bundles page view entries and returns lookup maps for inline and optimized delivery.
// Inputs:
// - pages: registered page routes keyed by path, used to discover unique view entry files.
// - webDir: project web directory containing the `views` subdirectory for entry resolution.
// Outputs:
// - map[string]string: bundled JavaScript keyed by original view entry path.
// - map[string]string: bundled JavaScript keyed by optimized `/_bungo/...js` asset path.
// - error: non-nil when esbuild compilation reports one or more errors.
func CompilePages(pages map[string]bungo.PageRoute, webDir string) (map[string]string, map[string]string, error) {
	var entryPoints []api.EntryPoint
	seen := make(map[string]struct{})
	for _, page := range pages {
		if page.View != "" {
			outputBase := strings.TrimSuffix(filepath.ToSlash(page.View), filepath.Ext(page.View))
			if _, ok := seen[outputBase]; ok {
				continue
			}
			seen[outputBase] = struct{}{}
			entryPoints = append(entryPoints, api.EntryPoint{
				InputPath:  filepath.Join(webDir, "views", page.View),
				OutputPath: outputBase,
			})
		}
	}

	if len(entryPoints) == 0 {
		return map[string]string{}, map[string]string{}, nil
	}

	result := api.Build(api.BuildOptions{
		EntryPointsAdvanced: entryPoints,
		Bundle:              true,
		Write:               false,
		MinifyWhitespace:    true,
		MinifySyntax:        true,
		JSX:                 api.JSXAutomatic,
		Loader: map[string]api.Loader{
			".js":  api.LoaderJS,
			".jsx": api.LoaderJSX,
			".ts":  api.LoaderTS,
			".tsx": api.LoaderTSX,
		},
		Plugins: []api.Plugin{ReactPlugin, RemoteImportPlugin},
		Inject:  []string{"bungo/render"},
		Outdir:  "/", // Used so multiple in-memory outputs can map individually
	})

	if len(result.Errors) > 0 {
		return nil, nil, fmt.Errorf("esbuild errors: %v", result.Errors)
	}

	compiledMap := make(map[string]string)
	optimizedMap := make(map[string]string)
	outputByPath := make(map[string]string)

	for _, file := range result.OutputFiles {
		outputPath := strings.TrimPrefix(filepath.ToSlash(file.Path), "/")
		outputByPath[outputPath] = string(file.Contents)
	}

	// Map each page view path to its corresponding compiled JavaScript string.
	for _, page := range pages {
		if page.View != "" {
			outputPath := strings.TrimSuffix(filepath.ToSlash(page.View), filepath.Ext(page.View)) + ".js"
			if js, ok := outputByPath[outputPath]; ok {
				compiledMap[page.View] = js
				optimizedMap[OptimizedAssetPath(page.View)] = js
			}
		}
	}

	return compiledMap, optimizedMap, nil
}
