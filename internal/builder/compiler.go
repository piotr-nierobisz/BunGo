package builder

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/evanw/esbuild/pkg/api"
	bungo "github.com/piotr-nierobisz/BunGo"
	"github.com/piotr-nierobisz/BunGo/internal/fileutil"
)

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
			outputBase := strings.TrimSuffix(fileutil.NormalizeSlashPath(page.View), filepath.Ext(page.View))
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
		outputPath := strings.TrimPrefix(fileutil.NormalizeSlashPath(file.Path), "/")
		outputByPath[outputPath] = string(file.Contents)
	}

	// Map each page view path to its corresponding compiled JavaScript string.
	for _, page := range pages {
		if page.View != "" {
			outputPath := strings.TrimSuffix(fileutil.NormalizeSlashPath(page.View), filepath.Ext(page.View)) + ".js"
			if js, ok := outputByPath[outputPath]; ok {
				compiledMap[page.View] = js
				optimizedMap[bungo.OptimizedAssetPath(page.View)] = js
			}
		}
	}

	return compiledMap, optimizedMap, nil
}

// CompilePagesFromStorage bundles page view entries using server storage and temporary extraction when needed.
// Inputs:
// - pages: registered page routes keyed by path, used to discover unique view entry files.
// - storage: server asset storage that can provide a buildable disk web directory.
// Outputs:
// - map[string]string: bundled JavaScript keyed by original view entry path.
// - map[string]string: bundled JavaScript keyed by optimized `/_bungo/...js` asset path.
// - error: non-nil when preparing build assets or running esbuild fails.
func CompilePagesFromStorage(pages map[string]bungo.PageRoute, storage *bungo.AssetStorage) (map[string]string, map[string]string, error) {
	if storage == nil {
		return map[string]string{}, map[string]string{}, nil
	}

	webDir, cleanup, err := storage.PrepareWebDirForBuild()
	if err != nil {
		return nil, nil, err
	}
	defer cleanup()

	return CompilePages(pages, webDir)
}
