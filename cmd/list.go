package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/pradipta/wallfacer/internal/format"
	"github.com/pradipta/wallfacer/internal/store"
	"github.com/spf13/cobra"
)

var listFlags struct {
	project string
	tag     string
	dir     string
	agent   string
	all     bool
	asJSON  bool
}

var listCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List indexed sessions (most recently active first)",
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := openStore()
		if err != nil {
			return err
		}
		defer s.Close()
		if _, err := s.Sync(); err != nil {
			return err
		}
		sessions, err := s.List(store.Filter{
			Project:       listFlags.project,
			Tag:           listFlags.tag,
			Dir:           listFlags.dir,
			AgentType:     listFlags.agent,
			IncludeHidden: listFlags.all,
		})
		if err != nil {
			return err
		}
		return printSessions(sessions, listFlags.asJSON)
	},
}

func printSessions(sessions []store.Session, asJSON bool) error {
	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(sessions)
	}
	if len(sessions) == 0 {
		fmt.Println("no sessions found (try `wallfacer sync`)")
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tTITLE\tPROJECT\tTAGS\tDIR\tLAST ACTIVE")
	for _, x := range sessions {
		title := x.DisplayTitle()
		if x.Status != store.StatusActive {
			title += " [" + x.Status + "]"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
			store.ShortID(x.ID), format.Clip(title, 48), x.Project,
			strings.Join(x.Tags, ","), format.CollapseHome(x.Dir), format.RelTime(x.LastActiveAt))
	}
	return w.Flush()
}

func init() {
	listCmd.Flags().StringVar(&listFlags.project, "project", "", "filter by project")
	listCmd.Flags().StringVar(&listFlags.tag, "tag", "", "filter by tag")
	listCmd.Flags().StringVar(&listFlags.dir, "dir", "", "filter by exact working directory")
	listCmd.Flags().StringVar(&listFlags.agent, "agent", "", "filter by agent type (e.g. claude-code)")
	listCmd.Flags().BoolVarP(&listFlags.all, "all", "a", false, "include missing/trashed sessions")
	listCmd.Flags().BoolVar(&listFlags.asJSON, "json", false, "output JSON")
	rootCmd.AddCommand(listCmd)
}
