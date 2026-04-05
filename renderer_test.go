package bungo

import (
	"os"
	"path/filepath"
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

func TestIsDevModeEnabled(t *testing.T) {
	t.Setenv("BUNGO_DEV_ENABLED", "1")
	if !isDevModeEnabled() {
		t.Fatal("expected dev")
	}
	t.Setenv("BUNGO_DEV_ENABLED", "")
	if isDevModeEnabled() {
		t.Fatal("expected off")
	}
}
