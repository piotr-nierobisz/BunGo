package bungo

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestInjectScripts(t *testing.T) {
	t.Parallel()
	html := `<html><head><title>x</title></head><body></body></html>`
	inj := `<script>test</script>`
	out := injectScripts(html, inj)
	if !strings.Contains(out, inj) || !strings.Contains(out, "</head>") {
		t.Fatalf("bad inject: %s", out)
	}
	html2 := `<html><body></body></html>`
	out2 := injectScripts(html2, inj)
	if !strings.Contains(out2, "</body>") {
		t.Fatal(out2)
	}
	if injectScripts("plain", inj) != "plain"+inj {
		t.Fatal("append fallback")
	}
	if injectScripts("x", "") != "x" {
		t.Fatal("empty injection")
	}
}

func TestBuildScriptInjection(t *testing.T) {
	t.Parallel()
	inj, payload := buildScriptInjection(map[string]any{"a": 1}, "console.log(1)", "")
	if !strings.Contains(inj, "BunGoInlineJS") || payload["a"] == nil {
		t.Fatalf("inline: %s %#v", inj, payload)
	}
	inj2, _ := buildScriptInjection(nil, "", "/_bungo/x.js")
	if !strings.Contains(inj2, "BunGoModuleSrc") {
		t.Fatal(inj2)
	}
}

func TestRenderTemplate_standalone(t *testing.T) {
	dir := newTestWebTree(t)
	s := newAssetStorage(dir, nil)
	html, err := RenderTemplate(s, "layouts/home.gohtml", "", "", "", map[string]any{"Title": "T"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html, "T") {
		t.Fatalf("missing data: %s", html)
	}
}

func TestRenderTemplate_withLayout(t *testing.T) {
	dir := newTestWebTree(t)
	// layout defines block content; page defines content
	layout := `<!DOCTYPE html><html><head></head><body>{{block "content" .}}{{end}}</body></html>`
	if err := os.WriteFile(filepath.Join(dir, "layouts", "shell.gohtml"), []byte(layout), 0644); err != nil {
		t.Fatal(err)
	}
	page := `{{define "content"}}<p>{{.Title}}</p>{{end}}`
	if err := os.WriteFile(filepath.Join(dir, "layouts", "inner.gohtml"), []byte(page), 0644); err != nil {
		t.Fatal(err)
	}
	s := newAssetStorage(dir, nil)
	out, err := RenderTemplate(s, "layouts/inner.gohtml", "layouts/shell.gohtml", "", "", map[string]any{"Title": "L"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "L") {
		t.Fatalf("%s", out)
	}
}

func TestRenderTemplate_devModeInjectsLiveReloadClient(t *testing.T) {
	s := newAssetStorage(newTestWebTree(t), nil)
	// The client must dial the page's own origin, where engines mount the
	// reload endpoint, so HTTPS pages get wss:// with the app's certificate.
	endpoint := `"://" + window.location.host + "/__bungo"`

	t.Setenv("BUNGO_DEV_ENABLED", "1")
	html, err := RenderTemplate(s, "layouts/home.gohtml", "", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	idx := strings.Index(html, endpoint)
	if idx < 0 {
		t.Fatalf("dev client targeting %s not injected: %s", endpoint, html)
	}
	if idx > strings.Index(html, "</head>") {
		t.Fatal("dev client should be injected before </head>")
	}
	if strings.Contains(html, strconv.Itoa(DevWebSocketPort)) || strings.Contains(html, "location.hostname") {
		t.Fatalf("dev client still targets the old separate dev port: %s", html)
	}
	// template.JS must emit the script verbatim, not as an escaped string literal.
	if !strings.Contains(html, "new WebSocket(endpoint())") {
		t.Fatalf("dev client script was escaped or mangled: %s", html)
	}

	t.Setenv("BUNGO_DEV_ENABLED", "0")
	html, err = RenderTemplate(s, "layouts/home.gohtml", "", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(html, "/__bungo") {
		t.Fatalf("dev client injected outside dev mode: %s", html)
	}
}

func TestRenderTemplate_layoutReceivesEscapedScripts(t *testing.T) {
	t.Parallel()
	dir := newTestWebTree(t)
	mustWrite(t, filepath.Join(dir, "layouts", "shell.gohtml"), []byte(`<html><head></head><body>{{block "content" .}}{{end}}</body></html>`))
	mustWrite(t, filepath.Join(dir, "layouts", "page.gohtml"), []byte(`{{define "content"}}<p>{{.Title}}</p>{{end}}`))
	s := newAssetStorage(dir, nil)

	hostile := "</script><b>x"
	out, err := RenderTemplate(s, "layouts/page.gohtml", "layouts/shell.gohtml", "console.log(1)", "", map[string]any{"Title": hostile})
	if err != nil {
		t.Fatal(err)
	}
	// Page data reaches the browser twice (initial-data JSON and the template body);
	// neither may let a value break out of its script or element.
	if strings.Contains(out, hostile) {
		t.Fatalf("unescaped page data in output: %s", out)
	}
	head := out[:strings.Index(out, "</head>")]
	if !strings.Contains(head, "window.__BUNGO_DATA__ = ") || !strings.Contains(head, "console.log(1)") {
		t.Fatalf("scripts must be injected into the layout's head: %s", out)
	}
}

func TestRenderTemplate_errors(t *testing.T) {
	t.Parallel()
	dir := newTestWebTree(t)
	for name, src := range map[string]string{
		"unclosed.gohtml":    `<html>{{.Title</html>`,
		"badindex.gohtml":    `<html><body>{{index .Items 5}}</body></html>`,
		"page.gohtml":        `{{define "content"}}<p>{{.Title}}</p>{{end}}`,
		"badpage.gohtml":     `{{define "content"}}{{.Title{{end}}`,
		"badexecpage.gohtml": `{{define "content"}}{{index .Items 5}}{{end}}`,
		"shell.gohtml":       `<html><head></head><body>{{block "content" .}}{{end}}</body></html>`,
		"badshell.gohtml":    `<html>{{if}}</html>`,
	} {
		mustWrite(t, filepath.Join(dir, "layouts", name), []byte(src))
	}
	s := newAssetStorage(dir, nil)
	data := map[string]any{"Title": "t", "Items": []string{"only"}}

	tests := []struct {
		name     string
		template string
		layout   string
		notExist bool
	}{
		{"standalone template missing", "layouts/absent.gohtml", "", true},
		{"standalone parse error", "layouts/unclosed.gohtml", "", false},
		{"standalone execute error", "layouts/badindex.gohtml", "", false},
		{"layout missing", "layouts/page.gohtml", "layouts/absent.gohtml", true},
		{"template missing under layout", "layouts/absent.gohtml", "layouts/shell.gohtml", true},
		{"layout parse error", "layouts/page.gohtml", "layouts/badshell.gohtml", false},
		{"template parse error under layout", "layouts/badpage.gohtml", "layouts/shell.gohtml", false},
		{"execute error under layout", "layouts/badexecpage.gohtml", "layouts/shell.gohtml", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			html, err := RenderTemplate(s, tt.template, tt.layout, "", "", data)
			if err == nil {
				t.Fatalf("expected error, rendered %q", html)
			}
			if html != "" {
				t.Fatalf("partial output returned alongside error: %q", html)
			}
			if got := errors.Is(err, fs.ErrNotExist); got != tt.notExist {
				t.Fatalf("errors.Is(err, fs.ErrNotExist) = %v, want %v (err: %v)", got, tt.notExist, err)
			}
		})
	}
}
