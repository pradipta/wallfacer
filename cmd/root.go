package cmd

import (
	"fmt"
	"os"

	"github.com/mattn/go-isatty"
	"github.com/pradipta-s/wallfacer/internal/agent"
	"github.com/pradipta-s/wallfacer/internal/launcher"
	"github.com/pradipta-s/wallfacer/internal/tui"
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
		if !isatty.IsTerminal(os.Stdout.Fd()) {
			return cmd.Help()
		}
		return browseLoop()
	},
}

// browseLoop alternates between the TUI and the agent: the browser picks an
// action, hands the terminal to the agent, and reopens when the agent exits.
func browseLoop() error {
	s, err := openStore()
	if err != nil {
		return err
	}
	defer s.Close()

	for {
		if _, err := s.Sync(); err != nil {
			return err
		}
		action, err := tui.Run(s)
		if err != nil {
			return err
		}
		switch action.Type {
		case tui.ActionQuit:
			return nil
		case tui.ActionResume:
			if _, err := launcher.Resume(s, action.Session); err != nil {
				fmt.Fprintln(os.Stderr, "wallfacer:", err)
				pause()
			}
		case tui.ActionNew:
			a, _ := agent.Get("claude-code")
			res, err := launcher.New(s, a, action.Dir, launcher.Overlay{})
			if err != nil {
				fmt.Fprintln(os.Stderr, "wallfacer:", err)
				pause()
			} else if res.SessionID == "" {
				fmt.Fprintln(os.Stderr, "wallfacer: no session was recorded")
				pause()
			}
		}
	}
}

// pause lets the user read an error before the alt-screen TUI redraws over it.
func pause() {
	fmt.Fprint(os.Stderr, "press enter to continue...")
	fmt.Fscanln(os.Stdin)
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "wallfacer:", err)
		os.Exit(1)
	}
}
