package builder

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/evanw/esbuild/pkg/api"
	bungo "github.com/piotr-nierobisz/BunGo"
)

// CompilePages extracts all View paths, calls esbuild api.Build with ReactPlugin, and returns mapped compiled strings
func CompilePages(pages map[string]bungo.PageRoute, webDir string) (map[string]string, error) {
	var entryPoints []string

	for _, page := range pages {
		if page.View != "" {
			entryPoints = append(entryPoints, filepath.Join(webDir, "views", page.View))
		}
	}

	if len(entryPoints) == 0 {
		return map[string]string{}, nil
	}

	result := api.Build(api.BuildOptions{
		EntryPoints:      entryPoints,
		Bundle:           true,
		Write:            false,
		MinifyWhitespace: true,
		MinifySyntax:     true,
		JSX:              api.JSXAutomatic,
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
		return nil, fmt.Errorf("esbuild errors: %v", result.Errors)
	}

	compiledMap := make(map[string]string)

	// Map the original JSX entry point to its corresponding compiled JavaScript string
	for _, page := range pages {
		if page.View != "" {
			viewBase := strings.TrimSuffix(page.View, filepath.Ext(page.View))
			for _, file := range result.OutputFiles {
				if strings.HasPrefix(filepath.Base(file.Path), filepath.Base(viewBase)) {
					compiledMap[page.View] = string(file.Contents)
					break
				}
			}
		}
	}

	return compiledMap, nil
}
