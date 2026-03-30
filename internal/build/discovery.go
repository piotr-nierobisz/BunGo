package build

import (
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

// discoverServerWebDirs scans entry package source for NewServer calls and collects discovered web directories.
// Inputs:
// - projectRoot: repository root directory used as the embedding safety boundary.
// - entryDir: entry package directory recursively scanned for .go files.
// Outputs:
// - []discoveredWebDir: sorted unique web directories with embed alias and source disk path.
// - error: non-nil when filesystem walking or Go parsing fails.
func discoverServerWebDirs(projectRoot string, entryDir string) ([]discoveredWebDir, error) {
	discovered := make(map[string]string)
	fileSet := token.NewFileSet()

	walkErr := filepath.WalkDir(entryDir, func(currentPath string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			name := entry.Name()
			if strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			return nil
		}

		if filepath.Ext(currentPath) != ".go" {
			return nil
		}

		sourceBytes, readErr := os.ReadFile(currentPath)
		if readErr != nil {
			return readErr
		}
		source := string(sourceBytes)
		if !strings.Contains(source, theme.BunGoModuleImportPath) || !strings.Contains(source, theme.BunGoNewServerSelector) {
			return nil
		}

		parsed, parseErr := parser.ParseFile(fileSet, currentPath, nil, 0)
		if parseErr != nil {
			return parseErr
		}

		ast.Inspect(parsed, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || len(call.Args) < 2 {
				return true
			}

			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || selector.Sel == nil || selector.Sel.Name != "NewServer" {
				return true
			}

			literal, ok := call.Args[1].(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				return true
			}

			rawValue, unquoteErr := strconv.Unquote(literal.Value)
			if unquoteErr != nil {
				return true
			}

			webDir, keep := normalizeWebDirForEmbedding(rawValue, projectRoot)
			if keep {
				if existingSource, exists := discovered[webDir.embedPath]; !exists || existingSource == "" {
					discovered[webDir.embedPath] = webDir.sourceDir
				}
			}
			return true
		})

		return nil
	})
	if walkErr != nil {
		return nil, walkErr
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
