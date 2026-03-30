package main

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"syscall"

	"github.com/charmbracelet/lipgloss"
	internalbuild "github.com/piotr-nierobisz/BunGo/internal/build"
	"github.com/piotr-nierobisz/BunGo/internal/dev"
	"github.com/piotr-nierobisz/BunGo/internal/scaffold"
	"github.com/piotr-nierobisz/BunGo/internal/theme"
	"github.com/spf13/cobra"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "bungo",
		Short: theme.EN.CLI.RootShort,
		Long: lipgloss.NewStyle().MarginTop(1).MarginBottom(1).Render(fmt.Sprintf(
			theme.EN.CLI.RootHelpBodyFmt,
			lipgloss.NewStyle().Bold(true).Foreground(theme.Primary).Render(theme.EN.CLI.RootBannerTitle),
			lipgloss.NewStyle().Foreground(theme.Secondary).Render(fmt.Sprintf(
				theme.EN.CLI.RootVersionReactFmt,
				getVersion(),
				theme.EmbeddedReactVersion,
			)),
			theme.EN.CLI.RootHelpParagraph1,
			theme.EN.CLI.RootHelpParagraph2,
		)),
		SilenceUsage: true,
	}
	rootCmd.CompletionOptions.DisableDefaultCmd = true

	rootCmd.AddCommand(newInitCommand())
	rootCmd.AddCommand(newDevCommand())
	rootCmd.AddCommand(newBuildCommand())

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newInitCommand() *cobra.Command {
	var useTypescript bool

	cmd := &cobra.Command{
		Use:   "init [project-name]",
		Short: theme.EN.CLI.InitShort,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			projectPath, err := scaffold.InitProject(args[0], useTypescript)
			if err != nil {
				return err
			}

			viewType := "JSX"
			if useTypescript {
				viewType = "TypeScript"
			}

			successMsg := lipgloss.NewStyle().Foreground(theme.Success).Bold(true).Render(fmt.Sprintf(theme.EN.CLI.InitSuccessFmt, projectPath))
			modeMsg := lipgloss.NewStyle().Foreground(theme.Secondary).Render(fmt.Sprintf(theme.EN.CLI.InitModeFmt, viewType))
			nextStepsHeader := lipgloss.NewStyle().Bold(true).Render(theme.EN.CLI.InitNextSteps)
			cmds := lipgloss.NewStyle().Foreground(theme.Muted).Render(fmt.Sprintf(theme.EN.CLI.InitCommandsFmt, args[0]))

			fmt.Fprintln(cmd.OutOrStdout(), successMsg)
			fmt.Fprintln(cmd.OutOrStdout(), modeMsg+"\n")
			fmt.Fprintln(cmd.OutOrStdout(), nextStepsHeader)
			fmt.Fprintln(cmd.OutOrStdout(), cmds)
			return nil
		},
	}

	cmd.Flags().BoolVar(&useTypescript, "typescript", false, theme.EN.CLI.FlagTypescript)
	return cmd
}

func newDevCommand() *cobra.Command {
	var entry string

	cmd := &cobra.Command{
		Use:   "dev",
		Short: theme.EN.CLI.DevShort,
		Long:  theme.EN.CLI.DevLong,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := os.Getwd()
			if err != nil {
				return err
			}
			if err := ensureGoModExists(root); err != nil {
				return err
			}

			runRoot, runTarget, err := dev.ResolveRunContext(root, entry)
			if err != nil {
				return err
			}

			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)

			return dev.RunUI(ctx, stop, runRoot, runTarget, getVersion())
		},
	}

	cmd.Flags().StringVar(&entry, "entry", ".", theme.EN.CLI.FlagEntry)
	return cmd
}

// newBuildCommand creates the `bungo build` command for portable production binaries.
// Inputs:
// - none
// Outputs:
// - *cobra.Command: configured build command with entry/output flags and run behavior.
func newBuildCommand() *cobra.Command {
	var entry string
	var outputPath string
	var webDir string

	cmd := &cobra.Command{
		Use:   "build",
		Short: theme.EN.CLI.BuildShort,
		Long:  theme.EN.CLI.BuildLong,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := os.Getwd()
			if err != nil {
				return err
			}
			if err := ensureGoModExists(root); err != nil {
				return err
			}

			binaryPath, err := internalbuild.Run(cmd.OutOrStdout(), root, entry, outputPath, webDir)
			if err != nil {
				return err
			}
			_ = binaryPath
			return nil
		},
	}

	cmd.Flags().StringVar(&entry, "entry", ".", theme.EN.CLI.FlagEntry)
	cmd.Flags().StringVar(&outputPath, "output", "", theme.EN.CLI.FlagOutput)
	cmd.Flags().StringVar(&webDir, "web-dir", "", theme.EN.CLI.FlagWebDir)
	return cmd
}

// ensureGoModExists validates that the current working directory contains a go.mod file.
// Inputs:
// - root: absolute directory expected to contain go.mod for BunGo CLI commands.
// Outputs:
// - error: non-nil when go.mod is missing or stat fails for unexpected reasons.
func ensureGoModExists(root string) error {
	goModPath := filepath.Join(root, "go.mod")
	if _, err := os.Stat(goModPath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf(theme.EN.CLI.ErrGoModNotFoundFmt, root)
		}
		return err
	}
	return nil
}

func getVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok || info.Main.Version == "" {
		return theme.CLIVersionUnknown
	}
	return info.Main.Version
}
