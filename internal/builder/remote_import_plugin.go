package builder

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/evanw/esbuild/pkg/api"
)

const remoteHTTPNamespace = "bungo-remote-http"

var (
	remoteModuleCache sync.Map
	remoteHTTPClient  = &http.Client{Timeout: 15 * time.Second}
)

type remoteModule struct {
	contents string
	loader   api.Loader
}

// RemoteImportPlugin enables URL imports inside JSX/JS modules.
// Example: import { format } from "https://esm.sh/date-fns@3.6.0";
var RemoteImportPlugin = api.Plugin{
	Name: "remote-http-imports",
	Setup: func(build api.PluginBuild) {
		// Entry point for absolute URL imports from local files.
		build.OnResolve(api.OnResolveOptions{Filter: `^https?://`},
			func(args api.OnResolveArgs) (api.OnResolveResult, error) {
				_, err := url.ParseRequestURI(args.Path)
				if err != nil {
					return api.OnResolveResult{}, fmt.Errorf("invalid remote import %q: %w", args.Path, err)
				}

				if alias, ok := resolveEmbeddedReactFromRemoteURL(args.Path); ok {
					return alias, nil
				}

				return api.OnResolveResult{
					Path:      args.Path,
					Namespace: remoteHTTPNamespace,
				}, nil
			})

		// Resolve relative and absolute path imports that originate from a remote module.
		build.OnResolve(api.OnResolveOptions{Filter: `^(\.{1,2}/|/)`, Namespace: remoteHTTPNamespace},
			func(args api.OnResolveArgs) (api.OnResolveResult, error) {
				baseURL, err := url.Parse(args.Importer)
				if err != nil {
					return api.OnResolveResult{}, fmt.Errorf("invalid importer URL %q: %w", args.Importer, err)
				}

				refURL, err := url.Parse(args.Path)
				if err != nil {
					return api.OnResolveResult{}, fmt.Errorf("invalid remote module path %q: %w", args.Path, err)
				}

				resolved := baseURL.ResolveReference(refURL)
				if alias, ok := resolveEmbeddedReactFromRemoteURL(resolved.String()); ok {
					return alias, nil
				}

				return api.OnResolveResult{
					Path:      resolved.String(),
					Namespace: remoteHTTPNamespace,
				}, nil
			})

		build.OnLoad(api.OnLoadOptions{Filter: `^https?://`, Namespace: remoteHTTPNamespace},
			func(args api.OnLoadArgs) (api.OnLoadResult, error) {
				module, err := loadRemoteModule(args.Path)
				if err != nil {
					return api.OnLoadResult{}, err
				}

				return api.OnLoadResult{
					Contents: &module.contents,
					Loader:   module.loader,
				}, nil
			})
	},
}

type remoteModuleMeta struct {
	Loader api.Loader `json:"loader"`
}

// getCacheDir returns the remote module cache directory path and ensures it exists.
// Inputs:
// - none
// Outputs:
// - string: absolute path to the BunGo remote module cache directory.
// - error: non-nil when cache directory creation fails.
func getCacheDir() (string, error) {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		cacheDir = os.TempDir()
	}
	dir := filepath.Join(cacheDir, "bungo", "remote_modules")
	return dir, os.MkdirAll(dir, 0755)
}

// getCacheFilePaths returns body and metadata cache file paths for a remote module URL.
// Inputs:
// - moduleURL: absolute remote module URL used as stable cache key material.
// Outputs:
// - string: cache file path for the raw module body.
// - string: cache file path for module metadata JSON.
// - error: non-nil when cache directory resolution fails.
func getCacheFilePaths(moduleURL string) (string, string, error) {
	dir, err := getCacheDir()
	if err != nil {
		return "", "", err
	}
	hash := sha256.Sum256([]byte(moduleURL))
	hashStr := hex.EncodeToString(hash[:])
	return filepath.Join(dir, hashStr+".body"), filepath.Join(dir, hashStr+".meta.json"), nil
}

// loadRemoteModule loads a remote module from memory cache, disk cache, or HTTP fetch.
// Inputs:
// - moduleURL: absolute remote module URL to retrieve and classify.
// Outputs:
// - remoteModule: module payload containing source contents and inferred esbuild loader.
// - error: non-nil when URL fetch, cache decode, or response read fails.
func loadRemoteModule(moduleURL string) (remoteModule, error) {
	if cached, ok := remoteModuleCache.Load(moduleURL); ok {
		return cached.(remoteModule), nil
	}

	bodyPath, metaPath, cacheErr := getCacheFilePaths(moduleURL)
	if cacheErr == nil {
		if bodyData, bErr := os.ReadFile(bodyPath); bErr == nil {
			if metaData, mErr := os.ReadFile(metaPath); mErr == nil {
				var meta remoteModuleMeta
				if err := json.Unmarshal(metaData, &meta); err == nil {
					module := remoteModule{
						contents: string(bodyData),
						loader:   meta.Loader,
					}
					remoteModuleCache.Store(moduleURL, module)
					return module, nil
				}
			}
		}
	}

	req, err := http.NewRequest(http.MethodGet, moduleURL, nil)
	if err != nil {
		return remoteModule{}, fmt.Errorf("creating request for %q failed: %w", moduleURL, err)
	}
	req.Header.Set("User-Agent", "BunGo/esbuild-remote-import")

	resp, err := remoteHTTPClient.Do(req)
	if err != nil {
		return remoteModule{}, fmt.Errorf("fetching %q failed: %w", moduleURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return remoteModule{}, fmt.Errorf("fetching %q failed: unexpected status %s", moduleURL, resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return remoteModule{}, fmt.Errorf("reading %q failed: %w", moduleURL, err)
	}

	inferredLoader := inferRemoteLoader(moduleURL, resp.Header.Get("Content-Type"))

	if cacheErr == nil {
		meta := remoteModuleMeta{Loader: inferredLoader}
		if metaData, err := json.Marshal(meta); err == nil {
			os.WriteFile(bodyPath, body, 0644)
			os.WriteFile(metaPath, metaData, 0644)
		}
	}

	module := remoteModule{
		contents: string(body),
		loader:   inferredLoader,
	}
	remoteModuleCache.Store(moduleURL, module)
	return module, nil
}

// inferRemoteLoader infers the esbuild loader from HTTP content type and URL extension.
// Inputs:
// - moduleURL: remote module URL whose path extension helps infer module type.
// - contentType: HTTP Content-Type header value returned by the remote server.
// Outputs:
// - api.Loader: selected loader used by esbuild to parse the fetched module.
func inferRemoteLoader(moduleURL string, contentType string) api.Loader {
	trimmedType := strings.TrimSpace(strings.ToLower(strings.Split(contentType, ";")[0]))
	switch trimmedType {
	case "application/json", "text/json":
		return api.LoaderJSON
	case "text/css":
		return api.LoaderCSS
	}

	parsed, err := url.Parse(moduleURL)
	if err != nil {
		return api.LoaderJS
	}

	switch strings.ToLower(path.Ext(parsed.Path)) {
	case ".ts":
		return api.LoaderTS
	case ".tsx":
		return api.LoaderTSX
	case ".jsx":
		return api.LoaderJSX
	case ".css":
		return api.LoaderCSS
	case ".json":
		return api.LoaderJSON
	default:
		return api.LoaderJS
	}
}

// resolveEmbeddedReactFromRemoteURL maps remote React-family URLs to embedded runtime modules.
// Inputs:
// - moduleURL: remote URL candidate that may represent react, react-dom, or jsx-runtime.
// Outputs:
// - api.OnResolveResult: resolve result that points to BunGo embedded React namespaces.
// - bool: true when moduleURL maps to an embedded React alias; otherwise false.
func resolveEmbeddedReactFromRemoteURL(moduleURL string) (api.OnResolveResult, bool) {
	module := classifyRemoteReactModule(moduleURL)
	switch module {
	case "react":
		return api.OnResolveResult{
			Path:      "react",
			Namespace: "react-ns",
		}, true
	case "react/jsx-runtime":
		return api.OnResolveResult{
			Path:      "react/jsx-runtime",
			Namespace: "react-jsx-ns",
		}, true
	case "react-dom":
		return api.OnResolveResult{
			Path:      "react-dom",
			Namespace: "react-dom-ns",
		}, true
	default:
		return api.OnResolveResult{}, false
	}
}

// classifyRemoteReactModule classifies a remote URL as react, react-dom, jsx-runtime, or non-react.
// Inputs:
// - moduleURL: remote URL inspected for React package path markers.
// Outputs:
// - string: module classification key (`react`, `react-dom`, `react/jsx-runtime`, or empty).
func classifyRemoteReactModule(moduleURL string) string {
	parsed, err := url.Parse(moduleURL)
	if err != nil {
		return ""
	}

	pathLower := strings.Trim(strings.ToLower(parsed.Path), "/")
	if pathLower == "" {
		return ""
	}

	segments := strings.Split(pathLower, "/")
	for _, segment := range segments {
		if segment == "react-dom" || strings.HasPrefix(segment, "react-dom@") {
			return "react-dom"
		}
	}

	for _, segment := range segments {
		if segment == "react" || strings.HasPrefix(segment, "react@") {
			if strings.Contains(pathLower, "jsx-runtime") {
				return "react/jsx-runtime"
			}
			return "react"
		}
	}

	return ""
}
