package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// Version is set at build time via -ldflags "-X github.com/pradipta-s/wallfacer/cmd.Version=...".
var Version = "dev"

var rootCmd = &cobra.Command{
	Use:   "wallfacer",
	Short: "Session manager and launcher for Claude Code (and other coding agents)",
	Long: `wallfacer indexes coding-agent sessions scattered across your filesystem,
lets you name, tag, group, search, and delete them, and launches or resumes
sessions in any directory.

Run with no arguments to open the interactive session browser.`,
	Version: Version,
	RunE: func(cmd *cobra.Command, args []string) error {
		// TODO: open the TUI browser once implemented; show help until then.
		return cmd.Help()
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "wallfacer:", err)
		os.Exit(1)
	}
}
