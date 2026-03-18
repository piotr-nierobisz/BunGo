package main

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"syscall"

	bungo "github.com/piotr-nierobisz/BunGo"
	"github.com/piotr-nierobisz/BunGo/internal/dev"
	"github.com/piotr-nierobisz/BunGo/internal/scaffold"
	"github.com/spf13/cobra"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "bungo",
		Short: "BunGo project helper CLI",
		Long: fmt.Sprintf(
			"=== BunGo CLI ===\n\nBunGo version: %s\nEmbedded React runtime: %s\n\nUse BunGo to scaffold apps and run the development workflow.",
			getVersion(),
			bungo.EmbeddedReactVersion,
		),
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
		Short: "Scaffold a BunGo showcase project",
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

			fmt.Fprintf(cmd.OutOrStdout(), "Created BunGo showcase project at %s\n", projectPath)
			fmt.Fprintf(cmd.OutOrStdout(), "View mode: %s\n", viewType)
			fmt.Fprintln(cmd.OutOrStdout(), "Next steps:")
			fmt.Fprintf(cmd.OutOrStdout(), "  cd %s\n", args[0])
			fmt.Fprintln(cmd.OutOrStdout(), "  go mod tidy")
			fmt.Fprintln(cmd.OutOrStdout(), "  bungo dev")
			return nil
		},
	}

	cmd.Flags().BoolVar(&useTypescript, "typescript", false, "Scaffold TypeScript views and add tsconfig.json")
	return cmd
}

func newDevCommand() *cobra.Command {
	var entry string

	cmd := &cobra.Command{
		Use:   "dev",
		Short: "Run BunGo development server",
		Long:  "Runs `go run <entry>`, watches project files, and reloads browser pages after restart cycles.",
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := os.Getwd()
			if err != nil {
				return err
			}

			goModPath := filepath.Join(root, "go.mod")
			if _, err := os.Stat(goModPath); err != nil {
				if os.IsNotExist(err) {
					return fmt.Errorf("go.mod not found in %s (run `bungo dev` from your project root)", root)
				}
				return err
			}

			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)

			return dev.RunUI(ctx, stop, root, entry, getVersion())
		},
	}

	cmd.Flags().StringVar(&entry, "entry", ".", "Go entry target")
	return cmd
}

func getVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok || info.Main.Version == "" {
		return "unknown"
	}
	return info.Main.Version
}
