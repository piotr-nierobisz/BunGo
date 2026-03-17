package main

import (
	"fmt"
	"os"

	"github.com/piotr-nierobisz/BunGo/internal/scaffold"
	"github.com/spf13/cobra"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "bungo",
		Short: "BunGo project helper CLI",
	}

	rootCmd.AddCommand(newInitCommand())

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
			fmt.Fprintln(cmd.OutOrStdout(), "  go run .")
			return nil
		},
	}

	cmd.Flags().BoolVar(&useTypescript, "typescript", false, "Scaffold TypeScript views and add tsconfig.json")
	return cmd
}
