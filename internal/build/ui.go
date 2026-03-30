package build

import (
	"fmt"
	"io"

	"github.com/charmbracelet/lipgloss"
	"github.com/piotr-nierobisz/BunGo/internal/theme"
)

type progressPrinter struct {
	out         io.Writer
	entry       string
	outputPath  string
	headerStyle lipgloss.Style
	stepStyle   lipgloss.Style
	metaStyle   lipgloss.Style
}

// newProgressPrinter creates a styled progress printer for the bungo build command.
// Inputs:
// - out: destination writer receiving build progress lines.
// - entry: selected build entry target shown in build metadata.
// - outputPath: selected output path flag value shown in build metadata.
// Outputs:
// - *progressPrinter: initialized progress printer with BunGo-themed styles.
func newProgressPrinter(out io.Writer, entry string, outputPath string) *progressPrinter {
	return &progressPrinter{
		out:        out,
		entry:      entry,
		outputPath: outputPath,
		headerStyle: lipgloss.NewStyle().
			Foreground(theme.Primary).
			Bold(true),
		stepStyle: lipgloss.NewStyle().
			Foreground(theme.Secondary),
		metaStyle: lipgloss.NewStyle().
			Foreground(theme.Muted),
	}
}

// start prints the build header and selected build options.
// Inputs:
// - none
// Outputs:
// - none
func (p *progressPrinter) start() {
	fmt.Fprintln(p.out, p.headerStyle.Render(theme.EN.Build.PipelineTitle))
	fmt.Fprintln(p.out, p.metaStyle.Render(fmt.Sprintf(theme.EN.Build.MetaFmt, p.entry, p.outputPathOrDefault())))
}

// step prints one formatted pipeline stage update.
// Inputs:
// - stage: stable build stage key emitted by internal/build progress callbacks.
// - detail: human-readable detail associated with the stage.
// Outputs:
// - none
func (p *progressPrinter) step(stage string, detail string) {
	if stage == "done" {
		return
	}
	fmt.Fprintln(p.out, p.stepStyle.Render(fmt.Sprintf("  • %s", detail)))
}

// success prints the final visual separator after build completion.
// Inputs:
// - binaryPath: final output binary path produced by the build pipeline.
// Outputs:
// - none
func (p *progressPrinter) success(binaryPath string) {
	_ = binaryPath
	fmt.Fprintln(p.out, p.metaStyle.Render(theme.EN.Build.Separator))
}

// outputPathOrDefault returns a display string for the output flag value.
// Inputs:
// - none
// Outputs:
// - string: explicit output path when provided, otherwise `<default>`.
func (p *progressPrinter) outputPathOrDefault() string {
	if p.outputPath == "" {
		return theme.EN.Build.OutputDefault
	}
	return p.outputPath
}
