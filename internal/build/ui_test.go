package build

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/piotr-nierobisz/BunGo/internal/theme"
)

func TestProgressPrinter(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		outputPath string
		wantOutput string
	}{
		{"default output", "", theme.EN.Build.OutputDefault},
		{"explicit output", "dist/api", "dist/api"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var out bytes.Buffer
			p := newProgressPrinter(&out, "./cmd/api", tt.outputPath)
			p.start()
			p.step("module", theme.EN.Build.StepReadGoMod)
			p.step("compile", theme.EN.Build.StepCompile)
			p.step("done", "api") // internal completion stage: must not be printed
			p.success("/project/bin/api")

			got := strings.Split(strings.TrimRight(stripANSI(out.String()), "\n"), "\n")
			want := []string{
				theme.EN.Build.PipelineTitle,
				fmt.Sprintf(theme.EN.Build.MetaFmt, "./cmd/api", tt.wantOutput),
				"  • " + theme.EN.Build.StepReadGoMod,
				"  • " + theme.EN.Build.StepCompile,
				theme.EN.Build.Separator,
			}
			if strings.Join(got, "\n") != strings.Join(want, "\n") {
				t.Fatalf("printer output mismatch\ngot:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
			}
		})
	}
}
