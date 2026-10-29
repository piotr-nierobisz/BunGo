package engine

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	bungo "github.com/piotr-nierobisz/BunGo"
)

// traversalMarker tags files outside static/ that a path traversal would leak.
const traversalMarker = "TEMPLATE-SOURCE-MARKER"

// traversalWebFiles is the web tree served in both storage modes: public static
// assets plus web-root files outside static/ that must never be reachable via /static/.
var traversalWebFiles = map[string]string{
	"layouts/home.gohtml":   `<!DOCTYPE html><html><head></head><body>{{.Title}}</body></html>`,
	"layouts/secret.gohtml": traversalMarker + " {{.Title}}",
	"views/app.jsx":         "// " + traversalMarker,
	"static/app.css":        "body{}",
	"static/sub/nested.css": "nested{}",
}

// rawGET sends one GET whose request target is written verbatim, bypassing any
// client-side URL normalization, and returns the response status and body.
func rawGET(t *testing.T, ts *httptest.Server, target string) (int, string) {
	t.Helper()
	addr := ts.Listener.Addr().String()
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := fmt.Fprintf(conn, "GET %s HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n\r\n", target, addr); err != nil {
		t.Fatal(err)
	}
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatalf("GET %s: %v", target, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, string(body)
}

func TestStatic_traversalOverHTTP(t *testing.T) {
	modes := []struct {
		name  string
		setup func(t *testing.T) string // returns the webDir to pass to NewServer
	}{
		{"disk", func(t *testing.T) string {
			dir := t.TempDir()
			for rel, data := range traversalWebFiles {
				mustWrite(t, filepath.Join(dir, filepath.FromSlash(rel)), data)
			}
			return dir
		}},
		{"embedded", func(t *testing.T) string {
			// A web dir absent from disk: every byte served must come from the embedded FS.
			const webDir = "bungo-embedded-traversal-web"
			if _, err := os.Stat(webDir); !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("precondition: %q must not exist on disk (stat err: %v)", webDir, err)
			}
			mem := fstest.MapFS{}
			for rel, data := range traversalWebFiles {
				mem[webDir+"/"+rel] = &fstest.MapFile{Data: []byte(data)}
			}
			bungo.RegisterEmbeddedAssetsFS(mem)
			t.Cleanup(func() { bungo.RegisterEmbeddedAssetsFS(nil) })
			// CreateHandler materializes embedded assets for esbuild; keep that inside the test's temp dir.
			tmp := t.TempDir()
			for _, key := range []string{"TMPDIR", "TMP", "TEMP"} {
				t.Setenv(key, tmp)
			}
			return webDir
		}},
	}

	cases := []struct {
		name   string
		target string
		status int
		body   string // expected body for successful reads
	}{
		{"encoded slash", "/static/..%2flayouts%2fsecret.gohtml", http.StatusNotFound, ""},
		{"encoded slash uppercase", "/static/..%2Flayouts%2Fsecret.gohtml", http.StatusNotFound, ""},
		{"encoded dots and slash", "/static/%2e%2e%2flayouts%2fsecret.gohtml", http.StatusNotFound, ""},
		{"encoded backslash", "/static/..%5clayouts%5csecret.gohtml", http.StatusNotFound, ""},
		{"raw backslash", `/static/..\layouts\secret.gohtml`, http.StatusNotFound, ""},
		{"nested encoded escape", "/static/sub%2f..%2f..%2flayouts%2fsecret.gohtml", http.StatusNotFound, ""},
		{"view source", "/static/..%2fviews%2fapp.jsx", http.StatusNotFound, ""},
		{"bundle route", "/_bungo/..%2flayouts%2fsecret.gohtml", http.StatusNotFound, ""},
		{"plain dot-dot is redirected by ServeMux", "/static/../layouts/secret.gohtml", http.StatusMovedPermanently, ""},
		{"static file", "/static/app.css", http.StatusOK, "body{}"},
		{"nested static file", "/static/sub/nested.css", http.StatusOK, "nested{}"},
		{"encoded slash inside static", "/static/sub%2fnested.css", http.StatusOK, "nested{}"},
		{"dot-dot that stays inside static", "/static/sub/..%2fapp.css", http.StatusOK, "body{}"},
	}

	for _, mode := range modes {
		t.Run(mode.name, func(t *testing.T) {
			webDir := mode.setup(t)
			eng := NewHTTPEngine()
			srv := bungo.NewServer(eng, webDir)
			srv.SetAssetOptimization(true) // mounts /_bungo/ so its lookup is exercised too

			// The files a traversal would leak really are readable from the web root in this mode.
			if b, err := srv.AssetStorage().ReadFile("layouts/secret.gohtml"); err != nil || !strings.Contains(string(b), traversalMarker) {
				t.Fatalf("precondition: secret template unreadable: %q, %v", b, err)
			}

			h, err := eng.CreateHandler(srv)
			if err != nil {
				t.Fatal(err)
			}
			ts := httptest.NewServer(h)
			defer ts.Close()

			for _, tc := range cases {
				status, body := rawGET(t, ts, tc.target)
				if status != tc.status {
					t.Errorf("%s: GET %s status %d, want %d", tc.name, tc.target, status, tc.status)
				}
				if strings.Contains(body, traversalMarker) {
					t.Errorf("%s: GET %s leaked a file outside static/: %q", tc.name, tc.target, body)
				}
				if tc.body != "" && body != tc.body {
					t.Errorf("%s: GET %s body %q, want %q", tc.name, tc.target, body, tc.body)
				}
			}
		})
	}
}
