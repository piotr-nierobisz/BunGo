package build

import (
	"os"
	"os/exec"
)

// runGoBuild executes `go build` for the requested package target and output path from project root.
// Inputs:
// - projectRoot: working directory used for the go build command.
// - entry: normalized package build target.
// - outputPath: absolute output binary path.
// Outputs:
// - error: non-nil when go build exits with an error.
func runGoBuild(projectRoot, entry, outputPath string) error {
	cmd := exec.Command("go", "build", "-o", outputPath, entry)
	cmd.Dir = projectRoot
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
