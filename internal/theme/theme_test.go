package theme

import "testing"

func TestVersionConstants(t *testing.T) {
	t.Parallel()
	if EmbeddedReactVersion == "" || BunGoModuleImportPath == "" || BunGoNewServerSelector == "" {
		t.Fatal("constants must be non-empty for build/discovery tooling")
	}
}
