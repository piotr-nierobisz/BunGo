package scaffold

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"html/template"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/piotr-nierobisz/BunGo/internal/theme"
)

// listTree returns every entry below root as sorted, slash-separated relative
// paths; directories carry a trailing slash.
func listTree(t *testing.T, root string) []string {
	t.Helper()
	var entries []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			rel += "/"
		}
		entries = append(entries, rel)
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	slices.Sort(entries)
	return entries
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

func assertExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
}

// goSourceLiterals parses Go source and returns the string literals assigned to
// composite-literal keys (e.g. View, Template) and passed to method calls
// (e.g. SetDefaultLayout), keyed by field or method name in source order.
func goSourceLiterals(t *testing.T, src string) (fields, calls map[string][]string) {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "main.go", src, 0)
	if err != nil {
		t.Fatalf("generated main.go does not parse: %v\n%s", err, src)
	}
	if file.Name.Name != "main" {
		t.Fatalf("package = %q; want main", file.Name.Name)
	}
	hasMain := slices.ContainsFunc(file.Decls, func(d ast.Decl) bool {
		fn, ok := d.(*ast.FuncDecl)
		return ok && fn.Recv == nil && fn.Name.Name == "main"
	})
	if !hasMain {
		t.Fatal("generated main.go has no func main")
	}

	unquote := func(e ast.Expr) (string, bool) {
		lit, ok := e.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return "", false
		}
		s, err := strconv.Unquote(lit.Value)
		return s, err == nil
	}

	fields = map[string][]string{}
	calls = map[string][]string{}
	ast.Inspect(file, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.KeyValueExpr:
			key, ok := n.Key.(*ast.Ident)
			if !ok {
				return true
			}
			if s, ok := unquote(n.Value); ok {
				fields[key.Name] = append(fields[key.Name], s)
			}
		case *ast.CallExpr:
			sel, ok := n.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			for _, arg := range n.Args {
				if s, ok := unquote(arg); ok {
					calls[sel.Sel.Name] = append(calls[sel.Sel.Name], s)
				}
			}
		}
		return true
	})
	return fields, calls
}

func TestInitProject_writesProjectSkeleton(t *testing.T) {
	tests := []struct {
		name          string
		useTypescript bool
		wantTree      []string
	}{
		{
			name:          "jsx",
			useTypescript: false,
			wantTree: []string{
				"go.mod",
				"main.go",
				"web/",
				"web/layouts/",
				"web/layouts/base.gohtml",
				"web/layouts/home.gohtml",
				"web/static/",
				"web/static/styles.css",
				"web/views/",
				"web/views/showcase.jsx",
			},
		},
		{
			name:          "typescript",
			useTypescript: true,
			wantTree: []string{
				"bungo-env.d.ts",
				"go.mod",
				"main.go",
				"tsconfig.json",
				"web/",
				"web/layouts/",
				"web/layouts/base.gohtml",
				"web/layouts/home.gohtml",
				"web/static/",
				"web/static/styles.css",
				"web/views/",
				"web/views/showcase.tsx",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Chdir(dir)

			got, err := InitProject("demo", tt.useTypescript)
			if err != nil {
				t.Fatalf("InitProject: %v", err)
			}

			project := filepath.Join(dir, "demo")
			if !filepath.IsAbs(got) {
				t.Fatalf("returned path %q is not absolute", got)
			}
			gotInfo, err := os.Stat(got)
			if err != nil {
				t.Fatalf("stat returned path: %v", err)
			}
			wantInfo, err := os.Stat(project)
			if err != nil {
				t.Fatal(err)
			}
			if !os.SameFile(gotInfo, wantInfo) {
				t.Fatalf("returned path %q is not %q", got, project)
			}

			if tree := listTree(t, project); !slices.Equal(tree, tt.wantTree) {
				t.Fatalf("tree = %v\nwant %v", tree, tt.wantTree)
			}

			wantGoMod := fmt.Sprintf("module demo\n\ngo %s\n", theme.ScaffoldGoVersion)
			if goMod := readFile(t, filepath.Join(project, "go.mod")); goMod != wantGoMod {
				t.Fatalf("go.mod = %q; want %q", goMod, wantGoMod)
			}
		})
	}
}

// TestInitProject_mainGoReferencesScaffoldedFiles checks that the generated main.go
// compiles syntactically and that every file or route it references was actually
// produced by the scaffold, so the project runs without manual edits.
func TestInitProject_mainGoReferencesScaffoldedFiles(t *testing.T) {
	for _, tc := range []struct {
		useTypescript bool
		viewFile      string
	}{
		{false, "showcase.jsx"},
		{true, "showcase.tsx"},
	} {
		t.Run(tc.viewFile, func(t *testing.T) {
			t.Chdir(t.TempDir())
			project, err := InitProject("demo", tc.useTypescript)
			if err != nil {
				t.Fatalf("InitProject: %v", err)
			}

			src := readFile(t, filepath.Join(project, "main.go"))
			if strings.Contains(src, "%!") {
				t.Fatalf("main.go contains a fmt formatting error:\n%s", src)
			}
			fields, calls := goSourceLiterals(t, src)

			if got := calls["NewServer"]; !slices.Equal(got, []string{"./web"}) {
				t.Fatalf("NewServer web dir = %v; want [./web]", got)
			}
			webDir := filepath.Join(project, "web")

			if got := fields["View"]; !slices.Equal(got, []string{tc.viewFile}) {
				t.Fatalf("View = %v; want [%s]", got, tc.viewFile)
			}
			assertExists(t, filepath.Join(webDir, "views", tc.viewFile))

			if got := calls["SetDefaultLayout"]; len(got) != 1 {
				t.Fatalf("SetDefaultLayout args = %v", got)
			}
			assertExists(t, filepath.Join(webDir, "layouts", calls["SetDefaultLayout"][0]))

			if got := fields["Template"]; len(got) != 1 {
				t.Fatalf("Template = %v", got)
			}
			assertExists(t, filepath.Join(webDir, "layouts", fields["Template"][0]))

			// The view fetches the API route registered in main.go (BunGo serves
			// APIs at /api/<version><path>) using the token the security layer accepts.
			view := readFile(t, filepath.Join(webDir, "views", tc.viewFile))
			if v := fields["Version"]; len(v) != 1 || !slices.Contains(fields["Path"], "/showcase") {
				t.Fatalf("unexpected API route: Version=%v Path=%v", v, fields["Path"])
			}
			apiURL := "/api/" + fields["Version"][0] + "/showcase"
			if !strings.Contains(view, `fetch("`+apiURL+`"`) {
				t.Fatalf("view does not fetch %s", apiURL)
			}
			if !strings.Contains(view, `"Bearer demo-token"`) || !strings.Contains(src, `"Bearer demo-token"`) {
				t.Fatal("view and security layer disagree on the demo token")
			}
			if !strings.Contains(view, "_bungoRender(ShowcaseApp)") {
				t.Fatal("view does not mount its component")
			}
		})
	}
}

// TestInitProject_layoutsRenderTogether composes the scaffolded layout and page
// template the same way bungo.RenderTemplate does and executes them.
func TestInitProject_layoutsRenderTogether(t *testing.T) {
	t.Chdir(t.TempDir())
	project, err := InitProject("demo", false)
	if err != nil {
		t.Fatalf("InitProject: %v", err)
	}
	layouts := filepath.Join(project, "web", "layouts")

	tmpl, err := template.New("base.gohtml").Parse(readFile(t, filepath.Join(layouts, "base.gohtml")))
	if err != nil {
		t.Fatalf("parse base.gohtml: %v", err)
	}
	if tmpl, err = tmpl.Parse(readFile(t, filepath.Join(layouts, "home.gohtml"))); err != nil {
		t.Fatalf("parse home.gohtml: %v", err)
	}

	render := func(data map[string]any) string {
		t.Helper()
		var buf bytes.Buffer
		if err := tmpl.ExecuteTemplate(&buf, "base.gohtml", data); err != nil {
			t.Fatalf("execute: %v", err)
		}
		return buf.String()
	}

	out := render(map[string]any{
		"PageTitle": "Hello <World>",
		"Subtitle":  "Sub",
		"Features":  []string{"Feature A", "Feature B"},
	})
	for _, want := range []string{
		"<title>Hello &lt;World&gt;</title>",
		"<h1>Hello &lt;World&gt;</h1>",
		"<p>Sub</p>",
		"<li>Feature A</li>",
		"<li>Feature B</li>",
		`<div id="root"></div>`,
		`href="/static/styles.css"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered page missing %q", want)
		}
	}
	// The stylesheet linked from the layout must be one the scaffold wrote.
	assertExists(t, filepath.Join(project, "web", "static", "styles.css"))

	fallback := render(map[string]any{})
	if !strings.Contains(fallback, "<title>BunGo Showcase</title>") {
		t.Errorf("default title missing from:\n%s", fallback)
	}
}

func TestInitProject_typescriptConfig(t *testing.T) {
	t.Chdir(t.TempDir())
	project, err := InitProject("demo", true)
	if err != nil {
		t.Fatalf("InitProject: %v", err)
	}

	var cfg struct {
		CompilerOptions map[string]any `json:"compilerOptions"`
		Include         []string       `json:"include"`
	}
	if err := json.Unmarshal([]byte(readFile(t, filepath.Join(project, "tsconfig.json"))), &cfg); err != nil {
		t.Fatalf("tsconfig.json is not valid JSON: %v", err)
	}
	if cfg.CompilerOptions["jsx"] != "react-jsx" {
		t.Errorf("compilerOptions.jsx = %v; want react-jsx", cfg.CompilerOptions["jsx"])
	}
	if !slices.Contains(cfg.Include, "bungo-env.d.ts") || !slices.Contains(cfg.Include, "web/views/**/*") {
		t.Errorf("include = %v; want the views and bungo-env.d.ts", cfg.Include)
	}

	env := readFile(t, filepath.Join(project, "bungo-env.d.ts"))
	for _, decl := range []string{"declare function useBungoData()", "declare function _bungoRender("} {
		if !strings.Contains(env, decl) {
			t.Errorf("bungo-env.d.ts missing %q", decl)
		}
	}
}

func TestInitProject_rejectsInvalidTargets(t *testing.T) {
	tests := []struct {
		name          string
		project       string
		setup         func(t *testing.T)
		wantErr       string
		wantIs        error
		skipOnWindows bool
	}{
		{
			name:    "empty name",
			project: "",
			wantErr: theme.EN.Scaffold.ErrProjectNameRequired,
		},
		{
			name:    "blank name",
			project: "   ",
			wantErr: theme.EN.Scaffold.ErrProjectNameRequired,
		},
		{
			name:    "nested path",
			project: "apps/demo",
			wantErr: fmt.Sprintf(theme.EN.Scaffold.ErrProjectNameSingleDirFmt, "apps/demo"),
		},
		{
			name:    "existing directory",
			project: "demo",
			setup: func(t *testing.T) {
				if err := os.Mkdir("demo", 0o755); err != nil {
					t.Fatal(err)
				}
			},
			wantErr: fmt.Sprintf(theme.EN.Scaffold.ErrTargetExistsFmt, "demo"),
		},
		{
			name:    "existing file",
			project: "demo",
			setup: func(t *testing.T) {
				if err := os.WriteFile("demo", []byte("x"), 0o644); err != nil {
					t.Fatal(err)
				}
			},
			wantErr: fmt.Sprintf(theme.EN.Scaffold.ErrTargetExistsFmt, "demo"),
		},
		{
			// A valid module name longer than NAME_MAX (255) makes lstat fail
			// with ENAMETOOLONG rather than "not exist".
			name:          "inaccessible target",
			project:       strings.Repeat("a", 300),
			wantErr:       fmt.Sprintf("unable to access target directory %q: ", strings.Repeat("a", 300)),
			wantIs:        syscall.ENAMETOOLONG,
			skipOnWindows: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.skipOnWindows && runtime.GOOS == "windows" {
				t.Skip("relies on POSIX NAME_MAX behaviour")
			}
			dir := t.TempDir()
			t.Chdir(dir)
			if tt.setup != nil {
				tt.setup(t)
			}
			before := listTree(t, dir)

			got, err := InitProject(tt.project, false)
			if err == nil {
				t.Fatalf("expected error, got path %q", got)
			}
			if got != "" {
				t.Errorf("path = %q; want empty on error", got)
			}
			if !strings.HasPrefix(err.Error(), tt.wantErr) {
				t.Errorf("err = %q; want prefix %q", err, tt.wantErr)
			}
			if tt.wantIs != nil && !errors.Is(err, tt.wantIs) {
				t.Errorf("err = %v; want errors.Is %v", err, tt.wantIs)
			}
			if after := listTree(t, dir); !slices.Equal(before, after) {
				t.Errorf("working dir changed on error: before %v, after %v", before, after)
			}
		})
	}
}

// The project name becomes the go.mod module path, so InitProject must accept
// exactly the single-element paths `go mod init` accepts.
func TestInitProject_projectNameMustBeModulePath(t *testing.T) {
	for _, name := range []string{"my-app", "my_app", "App123", "app.v2", "c++", "a~", "abc~x", ".hidden", "v2"} {
		t.Run("valid/"+name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			project, err := InitProject(name, false)
			if err != nil {
				t.Fatalf("InitProject(%q): %v", name, err)
			}
			want := fmt.Sprintf(theme.EN.Scaffold.GoModBodyFmt, name, theme.ScaffoldGoVersion)
			if got := readFile(t, filepath.Join(project, "go.mod")); got != want {
				t.Fatalf("go.mod = %q; want %q", got, want)
			}
		})
	}

	msgs := theme.EN.Scaffold
	invalidChar := func(r rune) string { return fmt.Sprintf(msgs.ProjectNameInvalidCharFmt, r) }
	reserved := func(s string) string { return fmt.Sprintf(msgs.ProjectNameWindowsReservedFmt, s) }
	for _, tt := range []struct {
		project string
		reason  string
	}{
		{"my app", invalidChar(' ')},
		{" app", invalidChar(' ')},
		{"app!", invalidChar('!')},
		{"a@b", invalidChar('@')},
		{"a:b", invalidChar(':')},
		{"café", invalidChar('é')},
		{"bad\x00name", invalidChar(0)},
		{"\xffapp", msgs.ProjectNameInvalidUTF8},
		{"-app", msgs.ProjectNameLeadingDash},
		{".", msgs.ProjectNameOnlyDots},
		{"..", msgs.ProjectNameOnlyDots},
		{"...", msgs.ProjectNameOnlyDots},
		{"app.", msgs.ProjectNameTrailingDot},
		{"con", reserved("con")},
		{"COM1.foo", reserved("COM1")},
		{"Lpt9", reserved("Lpt9")},
		{"abc~1", msgs.ProjectNameWindowsShortName},
		{"abc~12.txt", msgs.ProjectNameWindowsShortName},
	} {
		t.Run(fmt.Sprintf("invalid/%q", tt.project), func(t *testing.T) {
			dir := t.TempDir()
			t.Chdir(dir)

			_, err := InitProject(tt.project, false)
			want := fmt.Sprintf(msgs.ErrProjectNameInvalidFmt, tt.project, tt.reason)
			if err == nil || err.Error() != want {
				t.Fatalf("err = %v; want %q", err, want)
			}
			if tree := listTree(t, dir); len(tree) != 0 {
				t.Fatalf("nothing should be created for an invalid name, found %v", tree)
			}
		})
	}
}

// A dangling symlink is an existing entry: InitProject must report it as such
// and must not scaffold through the link.
func TestInitProject_danglingSymlinkTarget(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	elsewhere := filepath.Join(dir, "elsewhere")
	if err := os.Symlink(elsewhere, "demo"); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	_, err := InitProject("demo", false)
	want := fmt.Sprintf(theme.EN.Scaffold.ErrTargetExistsFmt, "demo")
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v; want %q", err, want)
	}
	if _, err := os.Lstat(elsewhere); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("scaffold wrote through the symlink to %s (lstat err: %v)", elsewhere, err)
	}
}
