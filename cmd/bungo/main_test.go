package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"syscall"
	"testing"

	"github.com/piotr-nierobisz/BunGo/internal/theme"
	"github.com/spf13/cobra"
)

// runCLI executes args against the real root command main uses and returns
// everything the commands wrote to stdout and stderr.
func runCLI(t *testing.T, args ...string) (string, error) {
	t.Helper()
	root := newRootCommand()

	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	// A nil slice would make cobra fall back to os.Args (the test binary's flags).
	root.SetArgs(append([]string{}, args...))
	err := root.Execute()
	return out.String(), err
}

// chdirTemp switches the working directory to a fresh temp dir and returns
// the directory as reported by os.Getwd, which is what the commands see.
func chdirTemp(t *testing.T) string {
	t.Helper()
	t.Chdir(t.TempDir())
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return wd
}

func dirEntries(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// TestRun_printsErrorsOnce guards against cobra and run both reporting the same
// error: stderr must hold the message exactly once, with no "Error:" prefix.
func TestRun_printsErrorsOnce(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		setup   func(t *testing.T)
		wantErr func(wd string) string
	}{
		{
			name: "command error",
			args: []string{"build"},
			wantErr: func(wd string) string {
				return fmt.Sprintf(theme.EN.CLI.ErrGoModNotFoundFmt, wd, "bungo build")
			},
		},
		{
			name: "scaffold error",
			args: []string{"init", "demo"},
			setup: func(t *testing.T) {
				if err := os.Mkdir("demo", 0o755); err != nil {
					t.Fatal(err)
				}
			},
			wantErr: func(string) string { return fmt.Sprintf(theme.EN.Scaffold.ErrTargetExistsFmt, "demo") },
		},
		{
			name:    "unknown flag",
			args:    []string{"init", "--bogus", "demo"},
			wantErr: func(string) string { return "unknown flag: --bogus" },
		},
		{
			name:    "unknown command",
			args:    []string{"bogus"},
			wantErr: func(string) string { return `unknown command "bogus" for "bungo"` },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wd := chdirTemp(t)
			if tt.setup != nil {
				tt.setup(t)
			}

			var stdout, stderr bytes.Buffer
			if code := run(tt.args, &stdout, &stderr); code != 1 {
				t.Fatalf("exit code = %d; want 1", code)
			}
			if want := tt.wantErr(wd) + "\n"; stderr.String() != want {
				t.Fatalf("stderr = %q; want exactly %q", stderr.String(), want)
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout = %q; want empty on failure", stdout.String())
			}
		})
	}
}

func TestRun_rootCommand(t *testing.T) {
	t.Run("help lists the subcommands", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		if code := run([]string{"--help"}, &stdout, &stderr); code != 0 {
			t.Fatalf("exit code = %d; stderr:\n%s", code, stderr.String())
		}
		help := stdout.String()
		for _, want := range []string{theme.EN.CLI.RootHelpParagraph1, "init", "dev", "build"} {
			if !strings.Contains(help, want) {
				t.Errorf("help missing %q:\n%s", want, help)
			}
		}
		if strings.Contains(help, "completion") {
			t.Errorf("default completion command should be disabled:\n%s", help)
		}
	})

	t.Run("success exits zero", func(t *testing.T) {
		chdirTemp(t)
		var stdout, stderr bytes.Buffer
		if code := run([]string{"init", "demo"}, &stdout, &stderr); code != 0 {
			t.Fatalf("exit code = %d; stderr:\n%s", code, stderr.String())
		}
		if stderr.Len() != 0 || !strings.Contains(stdout.String(), "cd demo") {
			t.Fatalf("stdout = %q, stderr = %q", stdout.String(), stderr.String())
		}
	})
}

func TestInitCommand_scaffoldsProject(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		mode      string
		viewFile  string
		tsProject bool
	}{
		{"jsx by default", []string{"init", "demo"}, "JSX", "showcase.jsx", false},
		{"typescript flag", []string{"init", "--typescript", "demo"}, "TypeScript", "showcase.tsx", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wd := chdirTemp(t)

			out, err := runCLI(t, tt.args...)
			if err != nil {
				t.Fatalf("init: %v\n%s", err, out)
			}

			project := filepath.Join(wd, "demo")
			for _, want := range []string{
				fmt.Sprintf(theme.EN.CLI.InitSuccessFmt, project),
				fmt.Sprintf(theme.EN.CLI.InitModeFmt, tt.mode),
				theme.EN.CLI.InitNextSteps,
				"cd demo",
				"bungo dev",
			} {
				if !strings.Contains(out, want) {
					t.Errorf("output missing %q:\n%s", want, out)
				}
			}

			if _, err := os.Stat(filepath.Join(project, "web", "views", tt.viewFile)); err != nil {
				t.Errorf("view not scaffolded: %v", err)
			}
			_, err = os.Stat(filepath.Join(project, "tsconfig.json"))
			if gotTS := err == nil; gotTS != tt.tsProject {
				t.Errorf("tsconfig.json present = %v; want %v", gotTS, tt.tsProject)
			}
		})
	}
}

func TestInitCommand_requiresExactlyOneArg(t *testing.T) {
	tests := map[string][]string{
		"no project name":   {"init"},
		"two project names": {"init", "one", "two"},
	}
	for name, args := range tests {
		t.Run(name, func(t *testing.T) {
			wd := chdirTemp(t)

			out, err := runCLI(t, args...)
			if err == nil {
				t.Fatalf("expected argument error, got output:\n%s", out)
			}
			if !strings.Contains(err.Error(), "accepts 1 arg(s)") {
				t.Fatalf("err = %v; want cobra ExactArgs(1) error", err)
			}
			if entries := dirEntries(t, wd); len(entries) != 0 {
				t.Fatalf("nothing should be created, found %v", entries)
			}
		})
	}
}

func TestInitCommand_reportsScaffoldError(t *testing.T) {
	chdirTemp(t)
	if err := os.Mkdir("demo", 0o755); err != nil {
		t.Fatal(err)
	}

	out, err := runCLI(t, "init", "demo")
	want := fmt.Sprintf(theme.EN.Scaffold.ErrTargetExistsFmt, "demo")
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v; want %q", err, want)
	}
	if out != "" {
		t.Fatalf("no success output expected on failure, got:\n%s", out)
	}
}

func TestInitCommand_rejectsInvalidProjectName(t *testing.T) {
	wd := chdirTemp(t)

	out, err := runCLI(t, "init", "my app")
	want := fmt.Sprintf(theme.EN.Scaffold.ErrProjectNameInvalidFmt, "my app",
		fmt.Sprintf(theme.EN.Scaffold.ProjectNameInvalidCharFmt, ' '))
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v; want %q", err, want)
	}
	if out != "" {
		t.Fatalf("no success output expected on failure, got:\n%s", out)
	}
	if entries := dirEntries(t, wd); len(entries) != 0 {
		t.Fatalf("nothing should be created, found %v", entries)
	}
}

func TestCommandFlags(t *testing.T) {
	t.Parallel()
	type flagSpec struct{ name, typ, def string }
	tests := []struct {
		cmd   *cobra.Command
		name  string
		flags []flagSpec
	}{
		{newInitCommand(), "init", []flagSpec{{"typescript", "bool", "false"}}},
		{newDevCommand(), "dev", []flagSpec{{"entry", "string", "."}}},
		{newBuildCommand(), "build", []flagSpec{
			{"entry", "string", "."},
			{"output", "string", ""},
			{"web-dir", "string", ""},
		}},
	}
	for _, tt := range tests {
		if tt.cmd.Name() != tt.name {
			t.Errorf("command name = %q; want %q", tt.cmd.Name(), tt.name)
		}
		if tt.cmd.Short == "" || tt.cmd.RunE == nil {
			t.Errorf("%s: missing Short help or RunE", tt.name)
		}
		for _, want := range tt.flags {
			f := tt.cmd.Flags().Lookup(want.name)
			if f == nil {
				t.Errorf("%s: flag --%s not defined", tt.name, want.name)
				continue
			}
			if f.Value.Type() != want.typ || f.DefValue != want.def {
				t.Errorf("%s --%s: type %s default %q; want type %s default %q",
					tt.name, want.name, f.Value.Type(), f.DefValue, want.typ, want.def)
			}
			if f.Usage == "" {
				t.Errorf("%s --%s: empty usage", tt.name, want.name)
			}
		}
	}
}

// `bungo dev` go-runs its entry while `bungo build` compiles it, so their
// --entry help texts must differ accordingly.
func TestEntryFlagHelpMatchesCommand(t *testing.T) {
	t.Parallel()
	devUsage := newDevCommand().Flags().Lookup("entry").Usage
	buildUsage := newBuildCommand().Flags().Lookup("entry").Usage
	if !strings.Contains(devUsage, "go run") {
		t.Errorf("dev --entry usage = %q; want it to mention go run", devUsage)
	}
	if strings.Contains(buildUsage, "go run") {
		t.Errorf("build --entry usage = %q; must not claim the entry is passed to go run", buildUsage)
	}
	if !strings.Contains(buildUsage, "build") {
		t.Errorf("build --entry usage = %q; want it to describe building", buildUsage)
	}
}

// The go.mod hint must name the command the user actually ran.
func TestProjectCommands_requireGoMod(t *testing.T) {
	for _, tt := range []struct {
		args        []string
		wantCommand string
	}{
		{[]string{"dev"}, "bungo dev"},
		{[]string{"dev", "--entry", "./cmd/server"}, "bungo dev"},
		{[]string{"build"}, "bungo build"},
		{[]string{"build", "--output", "bin/app"}, "bungo build"},
	} {
		t.Run(strings.Join(tt.args, " "), func(t *testing.T) {
			wd := chdirTemp(t)

			out, err := runCLI(t, tt.args...)
			want := fmt.Sprintf(theme.EN.CLI.ErrGoModNotFoundFmt, wd, tt.wantCommand)
			if err == nil || err.Error() != want {
				t.Fatalf("err = %v; want %q", err, want)
			}
			if !strings.Contains(err.Error(), "`"+tt.wantCommand+"`") {
				t.Fatalf("hint does not name %q: %v", tt.wantCommand, err)
			}
			if out != "" {
				t.Fatalf("nothing should run without go.mod, got output:\n%s", out)
			}
			if entries := dirEntries(t, wd); len(entries) != 0 {
				t.Fatalf("nothing should be created, found %v", entries)
			}
		})
	}
}

func TestProjectCommands_reportGetwdFailure(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs POSIX permissions enforced for the current user")
	}
	for _, name := range []string{"dev", "build"} {
		t.Run(name, func(t *testing.T) {
			wd := chdirTemp(t)
			// Without search permission on the working directory, stat(".")
			// and therefore os.Getwd fail with EACCES.
			if err := os.Chmod(wd, 0); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod(wd, 0o755) })

			out, err := runCLI(t, name)
			if !errors.Is(err, os.ErrPermission) {
				t.Fatalf("err = %v; want the getwd permission error", err)
			}
			if out != "" {
				t.Fatalf("unexpected output:\n%s", out)
			}
		})
	}
}

// TestBuildCommand_passesFlagsToPipeline uses a go.mod without a module
// directive so the build pipeline fails at its first step, before invoking the
// Go toolchain, while still echoing the entry/output options it received.
func TestBuildCommand_passesFlagsToPipeline(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantMeta string
	}{
		{
			name:     "defaults",
			args:     []string{"build"},
			wantMeta: fmt.Sprintf(theme.EN.Build.MetaFmt, ".", theme.EN.Build.OutputDefault),
		},
		{
			name:     "explicit flags",
			args:     []string{"build", "--entry", "./cmd/server", "--output", "dist/app", "--web-dir", "./web"},
			wantMeta: fmt.Sprintf(theme.EN.Build.MetaFmt, "./cmd/server", "dist/app"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wd := chdirTemp(t)
			if err := os.WriteFile("go.mod", []byte("go 1.25.0\n"), 0o644); err != nil {
				t.Fatal(err)
			}

			out, err := runCLI(t, tt.args...)
			if err == nil || !strings.Contains(err.Error(), "module declaration not found") {
				t.Fatalf("err = %v; want the pipeline's go.mod error", err)
			}
			if !strings.Contains(out, theme.EN.Build.PipelineTitle) || !strings.Contains(out, tt.wantMeta) {
				t.Fatalf("output missing pipeline header or %q:\n%s", tt.wantMeta, out)
			}
			if entries := dirEntries(t, wd); len(entries) != 1 {
				t.Fatalf("failed build must not leave files behind, found %v", entries)
			}
		})
	}
}

func TestEnsureGoModExists(t *testing.T) {
	t.Parallel()

	t.Run("present", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/app\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := ensureGoModExists(root, "bungo build"); err != nil {
			t.Fatalf("ensureGoModExists: %v", err)
		}
	})

	t.Run("missing", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		err := ensureGoModExists(root, "bungo build")
		want := fmt.Sprintf(theme.EN.CLI.ErrGoModNotFoundFmt, root, "bungo build")
		if err == nil || err.Error() != want {
			t.Fatalf("err = %v; want %q", err, want)
		}
	})

	t.Run("unexpected stat error is returned as is", func(t *testing.T) {
		t.Parallel()
		if runtime.GOOS == "windows" {
			t.Skip("stat through a file reports not-exist on Windows")
		}
		// Using a regular file as the root makes stat fail with ENOTDIR.
		root := filepath.Join(t.TempDir(), "not-a-dir")
		if err := os.WriteFile(root, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		err := ensureGoModExists(root, "bungo dev")
		if !errors.Is(err, syscall.ENOTDIR) {
			t.Fatalf("err = %v; want the raw ENOTDIR stat error", err)
		}
	})
}

func TestGetVersion(t *testing.T) {
	t.Parallel()
	got := getVersion()
	if got == "" {
		t.Fatal("getVersion returned an empty string")
	}
	want := theme.CLIVersionUnknown
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" {
		want = info.Main.Version
	}
	if got != want {
		t.Fatalf("getVersion() = %q; want %q", got, want)
	}
}
