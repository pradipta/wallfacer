package cmd

import (
	"fmt"
	"os"
	"runtime/debug"

	"github.com/mattn/go-isatty"
	"github.com/pradipta/wallfacer/internal/agent"
	"github.com/pradipta/wallfacer/internal/banner"
	"github.com/pradipta/wallfacer/internal/launcher"
	"github.com/pradipta/wallfacer/internal/tui"
	"github.com/spf13/cobra"
)

// Version is set at build time via -ldflags "-X github.com/pradipta/wallfacer/cmd.Version=...".
// When built without that flag (e.g. `go install github.com/pradipta/wallfacer@v1.0.0`),
// it falls back to the module version Go embeds in the binary's build info.
var Version = "dev"

// resolveVersion returns the ldflags-injected version when present, otherwise
// the module version from build info (set for `go install module@version`).
func resolveVersion() string {
	if Version != "dev" {
		return Version
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" {
		return bi.Main.Version
	}
	return Version
}

var rootCmd = &cobra.Command{
	Use:   "wallfacer",
	Short: "Session manager and launcher for Claude Code (and other coding agents)",
	Long: banner.Art + `
wallfacer indexes coding-agent sessions scattered across your filesystem,
lets you name, tag, group, search, and delete them, and launches or resumes
sessions in any directory.

Run with no arguments to open the interactive session browser.`,
	Version: resolveVersion(),
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
			a, ok := resolveAgent(action.Agent)
			if !ok {
				fmt.Fprintf(os.Stderr, "wallfacer: no adapter for agent %q\n", action.Agent)
				pause()
				continue
			}
			res, err := launcher.New(s, a, action.Dir, launcher.Overlay{
				Title:   action.Title,
				Project: action.Project,
				Tags:    action.Tags,
			})
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

// resolveAgent looks up the agent the browser asked for, falling back to the
// default so an empty choice can never strand the new-session flow.
func resolveAgent(agentType string) (agent.Adapter, bool) {
	if a, ok := agent.Get(agentType); ok {
		return a, true
	}
	return agent.Get(defaultAgentType)
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
