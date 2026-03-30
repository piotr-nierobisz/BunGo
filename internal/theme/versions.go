package theme

// Central shared constants (keep in sync with vendored assets and scaffold templates).

const (
	// EmbeddedReactVersion matches the vendored React in internal/builder/vendor/.
	EmbeddedReactVersion = "18.2.0"

	// ScaffoldGoVersion is the `go` directive written into scaffolded go.mod files.
	ScaffoldGoVersion = "1.25.0"

	// CLIVersionUnknown is shown when build info does not expose a module version.
	CLIVersionUnknown = "unknown"

	// BunGoModuleImportPath is the canonical module import path used across generated build artifacts.
	BunGoModuleImportPath = "github.com/piotr-nierobisz/BunGo"

	// BunGoNewServerSelector is the method selector used to discover BunGo server bootstrap calls.
	BunGoNewServerSelector = ".NewServer"
)
