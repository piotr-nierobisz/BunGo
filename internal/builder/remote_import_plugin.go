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

// remoteCacheFormatVersion identifies the on-disk cache entry layout. Entries written with any
// other version (including the unversioned layout that stored esbuild's numeric loader) are misses.
const remoteCacheFormatVersion = 2

var (
	remoteModuleCache sync.Map
	remoteHTTPClient  = &http.Client{Timeout: 15 * time.Second}
)

type remoteModule struct {
	contents string
	loader   api.Loader
	// url is the final URL the module was served from after redirects. Relative imports inside
	// the module resolve against it, exactly as a browser would.
	url string
}

// remoteModuleData is attached to loaded remote modules; esbuild hands it to the OnResolve
// callbacks of that module's imports.
type remoteModuleData struct {
	baseURL string
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
				// Prefer the URL the importer was actually served from: after a redirect
				// (e.g. https://unpkg.com/pkg -> /pkg@1.2.3/index.js) the requested URL is the wrong base.
				base := args.Importer
				if data, ok := args.PluginData.(remoteModuleData); ok {
					base = data.baseURL
				}
				baseURL, err := url.Parse(base)
				if err != nil {
					return api.OnResolveResult{}, fmt.Errorf("invalid importer URL %q: %w", base, err)
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
					Contents:   &module.contents,
					Loader:     module.loader,
					PluginData: remoteModuleData{baseURL: module.url},
				}, nil
			})
	},
}

// remoteModuleMeta is the JSON metadata stored next to each cached module body.
type remoteModuleMeta struct {
	Version int `json:"version"`
	// Loader is stored by name: esbuild's api.Loader values are positional and shift between releases.
	Loader string `json:"loader"`
	// URL is the final (post-redirect) URL, used as the base for the module's relative imports.
	URL string `json:"url"`
	// BodySHA256 ties the metadata to one exact body, so a body and metadata written by
	// different fetches are never combined into a cache hit.
	BodySHA256 string `json:"bodySha256"`
}

// remoteLoaderNames lists every loader inferRemoteLoader can produce, keyed by its cache name.
var remoteLoaderNames = map[string]api.Loader{
	"js":   api.LoaderJS,
	"jsx":  api.LoaderJSX,
	"ts":   api.LoaderTS,
	"tsx":  api.LoaderTSX,
	"json": api.LoaderJSON,
	"css":  api.LoaderCSS,
}

// remoteLoaderName returns the stable cache name of a loader, or "" when it has none.
// Inputs:
// - loader: esbuild loader selected for a remote module.
// Outputs:
// - string: name stored in cache metadata; unknown loaders yield "" and therefore never hit.
func remoteLoaderName(loader api.Loader) string {
	for name, candidate := range remoteLoaderNames {
		if candidate == loader {
			return name
		}
	}
	return ""
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

// readCachedModule loads one disk cache entry, rejecting anything not written by this format.
// Inputs:
// - bodyPath: cache file path of the module body.
// - metaPath: cache file path of the module metadata JSON.
// Outputs:
// - remoteModule: cached module contents, loader, and final URL.
// - bool: true only for a complete, current-format entry whose body matches its metadata.
func readCachedModule(bodyPath string, metaPath string) (remoteModule, bool) {
	metaData, err := os.ReadFile(metaPath)
	if err != nil {
		return remoteModule{}, false
	}
	var meta remoteModuleMeta
	if json.Unmarshal(metaData, &meta) != nil || meta.Version != remoteCacheFormatVersion || meta.URL == "" {
		return remoteModule{}, false
	}
	loader, ok := remoteLoaderNames[meta.Loader]
	if !ok {
		return remoteModule{}, false
	}
	body, err := os.ReadFile(bodyPath)
	if err != nil {
		return remoteModule{}, false
	}
	if sum := sha256.Sum256(body); hex.EncodeToString(sum[:]) != meta.BodySHA256 {
		return remoteModule{}, false
	}
	return remoteModule{contents: string(body), loader: loader, url: meta.URL}, true
}

// writeCachedModule stores one module in the disk cache.
// Inputs:
// - bodyPath: cache file path of the module body.
// - metaPath: cache file path of the module metadata JSON.
// - module: fetched module to persist.
// Outputs:
// - error: non-nil when either file could not be written.
func writeCachedModule(bodyPath string, metaPath string, module remoteModule) error {
	sum := sha256.Sum256([]byte(module.contents))
	// Marshal cannot fail: the metadata holds only string and int fields.
	metaData, _ := json.Marshal(remoteModuleMeta{
		Version:    remoteCacheFormatVersion,
		Loader:     remoteLoaderName(module.loader),
		URL:        module.url,
		BodySHA256: hex.EncodeToString(sum[:]),
	})
	// The body goes first: until the matching metadata lands, readers treat the entry as a miss.
	if err := writeFileAtomic(bodyPath, []byte(module.contents)); err != nil {
		return err
	}
	return writeFileAtomic(metaPath, metaData)
}

// writeFileAtomic replaces path with data via a temporary file and rename, so concurrent
// readers (including other bungo processes) see either the old or the new file, never a partial one.
// Inputs:
// - path: destination file path.
// - data: complete file contents.
// Outputs:
// - error: non-nil when the temporary file cannot be written or renamed into place.
func writeFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	_, err = tmp.Write(data)
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Chmod(tmp.Name(), 0644)
	}
	if err == nil {
		err = os.Rename(tmp.Name(), path)
	}
	if err != nil {
		_ = os.Remove(tmp.Name())
	}
	return err
}

// loadRemoteModule loads a remote module from memory cache, disk cache, or HTTP fetch.
// Inputs:
// - moduleURL: absolute remote module URL to retrieve and classify.
// Outputs:
// - remoteModule: module payload containing source contents, inferred esbuild loader, and final URL.
// - error: non-nil when URL fetch or response read fails.
func loadRemoteModule(moduleURL string) (remoteModule, error) {
	if cached, ok := remoteModuleCache.Load(moduleURL); ok {
		return cached.(remoteModule), nil
	}

	bodyPath, metaPath, cacheErr := getCacheFilePaths(moduleURL)
	if cacheErr == nil {
		if module, ok := readCachedModule(bodyPath, metaPath); ok {
			remoteModuleCache.Store(moduleURL, module)
			return module, nil
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

	// http.Transport records the last request of a redirect chain on the response; custom
	// transports may not, in which case the requested URL is the best available base.
	finalURL := moduleURL
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL.String()
	}

	module := remoteModule{
		contents: string(body),
		loader:   inferRemoteLoader(finalURL, resp.Header.Get("Content-Type")),
		url:      finalURL,
	}
	if cacheErr == nil {
		// A failed cache write only costs a refetch next time; the module itself is fine.
		_ = writeCachedModule(bodyPath, metaPath, module)
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
