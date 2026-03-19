package main

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"syscall"

	"github.com/charmbracelet/lipgloss"
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

			goModPath := filepath.Join(root, "go.mod")
			if _, err := os.Stat(goModPath); err != nil {
				if os.IsNotExist(err) {
					return fmt.Errorf(theme.EN.CLI.ErrGoModNotFoundFmt, root)
				}
				return err
			}

			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)

			return dev.RunUI(ctx, stop, root, entry, getVersion())
		},
	}

	cmd.Flags().StringVar(&entry, "entry", ".", theme.EN.CLI.FlagEntry)
	return cmd
}

func getVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok || info.Main.Version == "" {
		return theme.CLIVersionUnknown
	}
	return info.Main.Version
}
