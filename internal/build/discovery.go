package build

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/piotr-nierobisz/BunGo/internal/fileutil"
	"github.com/piotr-nierobisz/BunGo/internal/theme"
)

type discoveredWebDir struct {
	embedPath string
	sourceDir string
}

// bunGoPackageName is the package clause of the BunGo root package: the identifier an
// unaliased import of theme.BunGoModuleImportPath binds in user code.
const bunGoPackageName = "bungo"

// newServerFuncName is the BunGo constructor whose web-dir argument is discovered.
const newServerFuncName = "NewServer"

// maxConstantResolutionDepth bounds constant chains so cyclic (invalid) declarations cannot hang discovery.
const maxConstantResolutionDepth = 32

// discoverServerWebDirs scans entry package source for BunGo NewServer calls and collects discovered web directories.
// A call counts only when it targets the BunGo import (default name, alias, or dot-import) and its web-dir
// argument is a string literal or a package-level string constant (concatenations with `+` included).
// _test.go files are skipped because they are not part of the built binary.
// Inputs:
// - projectRoot: repository root directory used as the embedding safety boundary.
// - entryDir: entry package directory recursively scanned for .go files.
// Outputs:
// - []discoveredWebDir: sorted unique web directories with embed alias and source disk path.
// - error: non-nil when filesystem walking, reading, or parsing a NewServer candidate file fails.
func discoverServerWebDirs(projectRoot string, entryDir string) ([]discoveredWebDir, error) {
	discovered := make(map[string]string)
	fileSet := token.NewFileSet()

	// Package-level constants are shared by every file of a package, so sources are grouped by directory.
	var packageDirs []string
	packageFiles := make(map[string][]string)
	walkErr := filepath.WalkDir(entryDir, func(currentPath string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if currentPath != entryDir && strings.HasPrefix(entry.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(currentPath) != ".go" || strings.HasSuffix(currentPath, "_test.go") {
			return nil
		}

		dir := filepath.Dir(currentPath)
		if _, seen := packageFiles[dir]; !seen {
			packageDirs = append(packageDirs, dir)
		}
		packageFiles[dir] = append(packageFiles[dir], currentPath)
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}

	for _, dir := range packageDirs {
		if err := discoverPackageWebDirs(fileSet, projectRoot, packageFiles[dir], discovered); err != nil {
			return nil, err
		}
	}

	if len(discovered) == 0 {
		entryFallback := filepath.Join(entryDir, "web")
		if info, err := os.Stat(entryFallback); err == nil && info.IsDir() {
			discovered["web"] = entryFallback
		} else {
			projectFallback := filepath.Join(projectRoot, "web")
			if info, err := os.Stat(projectFallback); err == nil && info.IsDir() {
				discovered["web"] = projectFallback
			}
		}
	}

	embedPaths := make([]string, 0, len(discovered))
	for embedPath := range discovered {
		embedPaths = append(embedPaths, embedPath)
	}
	sort.Strings(embedPaths)

	result := make([]discoveredWebDir, 0, len(embedPaths))
	for _, embedPath := range embedPaths {
		result = append(result, discoveredWebDir{
			embedPath: embedPath,
			sourceDir: discovered[embedPath],
		})
	}
	return result, nil
}

// discoverPackageWebDirs records the web dirs passed to BunGo NewServer calls within one package.
// Inputs:
// - fileSet: shared token file set used for parsing.
// - projectRoot: root directory used as the allowed embedding boundary.
// - files: non-test .go files of one package directory.
// - discovered: embed path to source directory map updated in place.
// Outputs:
// - error: non-nil when a file cannot be read or a NewServer candidate file does not parse.
func discoverPackageWebDirs(fileSet *token.FileSet, projectRoot string, files []string, discovered map[string]string) error {
	sources := make([][]byte, len(files))
	hasCandidate := false
	for i, path := range files {
		source, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		sources[i] = source
		hasCandidate = hasCandidate || isServerCandidate(source)
	}
	if !hasCandidate {
		return nil
	}

	var parsed, candidates []*ast.File
	for i, path := range files {
		candidate := isServerCandidate(sources[i])
		file, err := parser.ParseFile(fileSet, path, sources[i], parser.SkipObjectResolution)
		if err != nil {
			if candidate {
				return err
			}
			// Other files only contribute constants; one that does not parse has none we can trust.
			continue
		}
		parsed = append(parsed, file)
		if candidate {
			candidates = append(candidates, file)
		}
	}

	constants := collectStringConstants(parsed)
	for _, file := range candidates {
		collectServerWebDirs(file, constants, projectRoot, discovered)
	}
	return nil
}

// isServerCandidate cheaply pre-filters sources that may call BunGo's NewServer.
// Inputs:
// - source: raw Go source file contents.
// Outputs:
// - bool: true when the source mentions both the BunGo module path and NewServer.
func isServerCandidate(source []byte) bool {
	return bytes.Contains(source, []byte(theme.BunGoModuleImportPath)) && bytes.Contains(source, []byte(newServerFuncName))
}

// collectServerWebDirs records the web-dir argument of every BunGo NewServer call in one file.
// Inputs:
// - file: parsed candidate file.
// - constants: package-level constant expressions keyed by name.
// - projectRoot: root directory used as the allowed embedding boundary.
// - discovered: embed path to source directory map updated in place.
// Outputs:
// - none
func collectServerWebDirs(file *ast.File, constants map[string]ast.Expr, projectRoot string, discovered map[string]string) {
	qualifiers, dotImported := bunGoImportNames(file)
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || len(call.Args) < 2 || !isBunGoNewServerCall(call.Fun, qualifiers, dotImported) {
			return true
		}
		if rawValue, ok := resolveStringExpr(call.Args[1], constants, 0); ok {
			if webDir, keep := normalizeWebDirForEmbedding(rawValue, projectRoot); keep {
				discovered[webDir.embedPath] = webDir.sourceDir
			}
		}
		return true
	})
}

// bunGoImportNames returns the identifiers under which a file imports the BunGo root package.
// Inputs:
// - file: parsed Go source file.
// Outputs:
// - map[string]bool: package qualifiers bound to BunGo (default name or aliases).
// - bool: true when BunGo is dot-imported, so NewServer is called unqualified.
func bunGoImportNames(file *ast.File) (map[string]bool, bool) {
	qualifiers := make(map[string]bool)
	dotImported := false
	for _, spec := range file.Imports {
		if importPath, err := strconv.Unquote(spec.Path.Value); err != nil || importPath != theme.BunGoModuleImportPath {
			continue
		}
		switch {
		case spec.Name == nil:
			qualifiers[bunGoPackageName] = true
		case spec.Name.Name == ".":
			dotImported = true
		case spec.Name.Name != "_":
			qualifiers[spec.Name.Name] = true
		}
	}
	return qualifiers, dotImported
}

// isBunGoNewServerCall reports whether a call target is BunGo's NewServer.
// Inputs:
// - fun: call expression target.
// - qualifiers: identifiers bound to the BunGo import in the file.
// - dotImported: whether the file dot-imports BunGo.
// Outputs:
// - bool: true for `<qualifier>.NewServer`, or unqualified `NewServer` under a dot-import.
func isBunGoNewServerCall(fun ast.Expr, qualifiers map[string]bool, dotImported bool) bool {
	switch target := fun.(type) {
	case *ast.SelectorExpr:
		pkg, ok := target.X.(*ast.Ident)
		return ok && qualifiers[pkg.Name] && target.Sel.Name == newServerFuncName
	case *ast.Ident:
		return dotImported && target.Name == newServerFuncName
	}
	return false
}

// collectStringConstants indexes the value expression of every package-level constant.
// Inputs:
// - files: parsed files of one package.
// Outputs:
// - map[string]ast.Expr: constant name to its (possibly implicitly repeated) value expression.
func collectStringConstants(files []*ast.File) map[string]ast.Expr {
	constants := make(map[string]ast.Expr)
	for _, file := range files {
		for _, decl := range file.Decls {
			genDecl, ok := decl.(*ast.GenDecl)
			if !ok || genDecl.Tok != token.CONST {
				continue
			}
			// In a const block, a spec without values repeats the previous spec's expressions.
			var previous []ast.Expr
			for _, spec := range genDecl.Specs {
				valueSpec := spec.(*ast.ValueSpec)
				values := valueSpec.Values
				if len(values) == 0 {
					values = previous
				}
				previous = values
				for i, name := range valueSpec.Names {
					if i < len(values) {
						constants[name.Name] = values[i]
					}
				}
			}
		}
	}
	return constants
}

// resolveStringExpr evaluates a constant string expression built from literals and package-level constants.
// Inputs:
// - expr: expression passed as the NewServer web-dir argument (or part of it).
// - constants: package-level constant expressions keyed by name.
// - depth: current resolution depth, bounded by maxConstantResolutionDepth.
// Outputs:
// - string: resolved string value.
// - bool: false when the expression is not a statically known string.
func resolveStringExpr(expr ast.Expr, constants map[string]ast.Expr, depth int) (string, bool) {
	if depth > maxConstantResolutionDepth {
		return "", false
	}
	switch e := expr.(type) {
	case *ast.BasicLit:
		if e.Kind != token.STRING {
			return "", false
		}
		value, err := strconv.Unquote(e.Value)
		return value, err == nil
	case *ast.ParenExpr:
		return resolveStringExpr(e.X, constants, depth+1)
	case *ast.Ident:
		value, ok := constants[e.Name]
		if !ok {
			return "", false
		}
		return resolveStringExpr(value, constants, depth+1)
	case *ast.BinaryExpr:
		if e.Op != token.ADD {
			return "", false
		}
		left, ok := resolveStringExpr(e.X, constants, depth+1)
		if !ok {
			return "", false
		}
		right, ok := resolveStringExpr(e.Y, constants, depth+1)
		return left + right, ok
	}
	return "", false
}

// resolveManualWebDir resolves a CLI-provided web root into a validated embed alias and source directory.
// Inputs:
// - projectRoot: repository root directory used as the embedding safety boundary.
// - manualWebDir: raw `--web-dir` flag value supplied by the CLI.
// Outputs:
// - []discoveredWebDir: a single-item slice containing the resolved manual web directory.
// - error: non-nil when the web dir is empty, outside project root, missing, or not a directory.
func resolveManualWebDir(projectRoot string, manualWebDir string) ([]discoveredWebDir, error) {
	rawWebDir := manualWebDir
	trimmed := fileutil.NormalizeSlashPath(rawWebDir)
	if trimmed == "" {
		return nil, fmt.Errorf("manual web dir cannot be empty")
	}

	var embedPath string
	var sourceDir string

	if filepath.IsAbs(trimmed) {
		sourceDir = filepath.Clean(filepath.FromSlash(trimmed))
		relativeToRoot, err := filepath.Rel(projectRoot, sourceDir)
		if err != nil {
			return nil, fmt.Errorf("resolving manual web dir %q failed: %w", rawWebDir, err)
		}
		cleanedRelative, ok := fileutil.CleanProjectRelativePath(relativeToRoot)
		if !ok {
			return nil, fmt.Errorf("manual web dir %q must stay inside project root", rawWebDir)
		}
		embedPath = cleanedRelative
	} else {
		cleanedRelative, ok := fileutil.CleanProjectRelativePath(trimmed)
		if !ok {
			return nil, fmt.Errorf("manual web dir %q must be a valid project-relative path", rawWebDir)
		}
		embedPath = cleanedRelative
		sourceDir = fileutil.JoinRootAndSlashPath(projectRoot, cleanedRelative)
	}

	info, err := os.Stat(sourceDir)
	if err != nil {
		return nil, fmt.Errorf("manual web dir %q is not accessible: %w", rawWebDir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("manual web dir %q is not a directory", rawWebDir)
	}

	return []discoveredWebDir{
		{
			embedPath: embedPath,
			sourceDir: sourceDir,
		},
	}, nil
}

// normalizeWebDirForEmbedding resolves, validates, and normalizes a webDir literal for embedding.
// Inputs:
// - webDir: web directory literal value passed to NewServer in user code.
// - projectRoot: root directory used as the allowed embedding boundary.
// Outputs:
// - discoveredWebDir: embed alias path plus absolute source directory on disk.
// - bool: true when webDir can be embedded; false when it is empty, absolute, or escaping root.
func normalizeWebDirForEmbedding(webDir, projectRoot string) (discoveredWebDir, bool) {
	trimmed := fileutil.NormalizeSlashPath(webDir)
	if trimmed == "" || strings.HasPrefix(trimmed, "/") {
		return discoveredWebDir{}, false
	}

	cleanedEmbed, ok := fileutil.CleanProjectRelativePath(trimmed)
	if !ok {
		return discoveredWebDir{}, false
	}

	// Match dev/runtime behavior by resolving relative web roots from project root.
	candidateAbs := fileutil.JoinRootAndSlashPath(projectRoot, cleanedEmbed)

	relativeToRoot, err := filepath.Rel(projectRoot, candidateAbs)
	if err != nil {
		return discoveredWebDir{}, false
	}
	if _, ok := fileutil.CleanProjectRelativePath(relativeToRoot); !ok {
		return discoveredWebDir{}, false
	}

	return discoveredWebDir{
		embedPath: cleanedEmbed,
		sourceDir: candidateAbs,
	}, true
}
