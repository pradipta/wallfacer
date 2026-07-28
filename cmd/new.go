package cmd

import (
	"fmt"

	"github.com/pradipta/wallfacer/internal/agent"
	"github.com/pradipta/wallfacer/internal/launcher"
	"github.com/pradipta/wallfacer/internal/store"
	"github.com/spf13/cobra"
)

var newFlags struct {
	agent   string
	title   string
	project string
	tags    []string
}

var newCmd = &cobra.Command{
	Use:   "new [dir]",
	Short: "Start a new agent session in a directory (default: current)",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		dir := "."
		if len(args) == 1 {
			dir = args[0]
		}
		a, ok := agent.Get(newFlags.agent)
		if !ok {
			return fmt.Errorf("unknown agent type %q", newFlags.agent)
		}
		s, err := openStore()
		if err != nil {
			return err
		}
		defer s.Close()

		res, err := launcher.New(s, a, dir, launcher.Overlay{
			Title:   newFlags.title,
			Project: newFlags.project,
			Tags:    newFlags.tags,
		})
		if err != nil {
			return err
		}
		if res.SessionID == "" {
			fmt.Println("wallfacer: no session was recorded (agent exited before creating one)")
			return res.ExitErr
		}
		sess, err := s.Get(res.SessionID)
		if err != nil {
			return err
		}
		fmt.Printf("wallfacer: saved session %s (%s)\n",
			store.ShortID(sess.ID), sess.DisplayTitle())
		return res.ExitErr
	},
}

func init() {
	newCmd.Flags().StringVar(&newFlags.agent, "agent", "claude-code", "agent type to launch")
	newCmd.Flags().StringVar(&newFlags.title, "title", "", "name the session up front")
	newCmd.Flags().StringVar(&newFlags.project, "project", "", "assign the session to a project")
	newCmd.Flags().StringSliceVar(&newFlags.tags, "tag", nil, "tag(s) for the session (repeatable)")
	rootCmd.AddCommand(newCmd)
}
