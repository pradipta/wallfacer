package cmd

import (
	"fmt"
	"io"
	"os"
	"runtime/debug"

	"github.com/mattn/go-isatty"
	"github.com/pradipta/wallfacer/internal/agent"
	"github.com/pradipta/wallfacer/internal/banner"
	"github.com/pradipta/wallfacer/internal/launcher"
	"github.com/pradipta/wallfacer/internal/store"
	"github.com/pradipta/wallfacer/internal/tui"
	"github.com/pradipta/wallfacer/internal/update"
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

	// The browser is the one front end that outlives a GitHub round trip, so it
	// does the looking — for its own footer, and to leave a warm cache behind
	// for one-shot subcommands.
	updateCheck.Refresh()

	for {
		if _, err := s.Sync(); err != nil {
			return err
		}
		action, err := tui.Run(s, awaitUpdateNotice)
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
	// The check runs in the background while the command does its work, and is
	// collected afterwards — either by browseLoop (which shows it in the TUI)
	// or by reportUpdate, on stderr. A failing command reports only its error:
	// an upgrade hint on top of a failure is noise.
	updateCheck = update.Start(update.Config{
		Current:  resolveVersion(),
		CacheDir: cacheDir(),
	})
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "wallfacer:", err)
		os.Exit(1)
	}
	reportUpdate()
}

// updateCheck is the in-flight release check for this process. It is always
// non-nil by the time any command runs, and yields a notice at most once.
var updateCheck *update.Check

// awaitUpdateNotice is handed to the browser, which calls it from a Bubble Tea
// command — so blocking here delays nothing. The notice is yielded once, so if
// the browser shows it, reportUpdate stays quiet, and vice versa. Later browser
// reopens (after an agent session) get an empty string.
func awaitUpdateNotice() string { return updateCheck.Await().Line() }

// reportUpdate prints the upgrade notice to stderr, so it never pollutes
// `--json` output or a piped listing. It stays quiet when stderr is not a
// terminal (scripts, CI), when the TUI already took the notice, and when the
// check has not finished — Result never waits, and an answer that lands too
// late is picked up from the cache by the next command.
func reportUpdate() {
	printUpdateNotice(os.Stderr, isatty.IsTerminal(os.Stderr.Fd()), updateCheck.Result())
}

// printUpdateNotice is the testable half of reportUpdate.
func printUpdateNotice(w io.Writer, isTTY bool, n *update.Notice) {
	if !isTTY || n == nil {
		return
	}
	fmt.Fprintln(w, "\n"+n.Block())
}

// cacheDir is where the update check caches its answer: wallfacer's data dir,
// or "" (meaning "don't cache") if it cannot be determined.
func cacheDir() string {
	dir, err := store.DataDir()
	if err != nil {
		return ""
	}
	return dir
}
