package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/pradipta/wallfacer/internal/format"
	"github.com/pradipta/wallfacer/internal/store"
	"github.com/spf13/cobra"
)

// resolveSession opens the store, syncs, and resolves ref. Shared by all
// commands that operate on one session. Caller must Close the store.
func resolveSession(ref string, includeHidden bool) (*store.Store, *store.Session, error) {
	s, err := openStore()
	if err != nil {
		return nil, nil, err
	}
	if _, err := s.Sync(); err != nil {
		s.Close()
		return nil, nil, err
	}
	var sess *store.Session
	if includeHidden {
		sess, err = s.ResolveAny(ref)
	} else {
		sess, err = s.Resolve(ref)
	}
	if err != nil {
		s.Close()
		return nil, nil, err
	}
	return s, sess, nil
}

var renameCmd = &cobra.Command{
	Use:   "rename <id-prefix|title> <new-title>",
	Short: "Set a session's title",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		s, sess, err := resolveSession(args[0], true)
		if err != nil {
			return err
		}
		defer s.Close()
		if err := s.SetTitle(sess.ID, args[1]); err != nil {
			return err
		}
		fmt.Printf("renamed %s → %q\n", store.ShortID(sess.ID), args[1])
		return nil
	},
}

var tagCmd = &cobra.Command{
	Use:   "tag",
	Short: "Manage session tags",
}

var tagAddCmd = &cobra.Command{
	Use:   "add <id-prefix|title> <tag>...",
	Short: "Add tag(s) to a session",
	Args:  cobra.MinimumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		s, sess, err := resolveSession(args[0], true)
		if err != nil {
			return err
		}
		defer s.Close()
		for _, tag := range args[1:] {
			if err := s.AddTag(sess.ID, tag); err != nil {
				return err
			}
		}
		fmt.Printf("tagged %s with %s\n", store.ShortID(sess.ID), strings.Join(args[1:], ", "))
		return nil
	},
}

var tagRmCmd = &cobra.Command{
	Use:   "rm <id-prefix|title> <tag>...",
	Short: "Remove tag(s) from a session",
	Args:  cobra.MinimumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		s, sess, err := resolveSession(args[0], true)
		if err != nil {
			return err
		}
		defer s.Close()
		for _, tag := range args[1:] {
			if err := s.RemoveTag(sess.ID, tag); err != nil {
				return err
			}
		}
		fmt.Printf("untagged %s: %s\n", store.ShortID(sess.ID), strings.Join(args[1:], ", "))
		return nil
	},
}

var projectCmd = &cobra.Command{
	Use:   "project",
	Short: "Manage session project assignment",
}

var projectSetCmd = &cobra.Command{
	Use:   "set <id-prefix|title> <project>",
	Short: "Assign a session to a project",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		s, sess, err := resolveSession(args[0], true)
		if err != nil {
			return err
		}
		defer s.Close()
		if err := s.SetProject(sess.ID, args[1]); err != nil {
			return err
		}
		fmt.Printf("%s → project %q\n", store.ShortID(sess.ID), args[1])
		return nil
	},
}

var projectClearCmd = &cobra.Command{
	Use:   "clear <id-prefix|title>",
	Short: "Remove a session's project assignment",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		s, sess, err := resolveSession(args[0], true)
		if err != nil {
			return err
		}
		defer s.Close()
		return s.SetProject(sess.ID, "")
	},
}

var searchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "Search sessions by title, prompt, directory, project, or tag",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := openStore()
		if err != nil {
			return err
		}
		defer s.Close()
		if _, err := s.Sync(); err != nil {
			return err
		}
		sessions, err := s.List(store.Filter{Query: args[0]})
		if err != nil {
			return err
		}
		return printSessions(sessions, false)
	},
}

var showCmd = &cobra.Command{
	Use:   "show <id-prefix|title>",
	Short: "Show full details of one session",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		s, sess, err := resolveSession(args[0], true)
		if err != nil {
			return err
		}
		defer s.Close()
		fmt.Printf("ID:          %s\n", sess.ID)
		fmt.Printf("Title:       %s\n", sess.DisplayTitle())
		fmt.Printf("Agent:       %s\n", sess.AgentType)
		fmt.Printf("Directory:   %s\n", sess.Dir)
		fmt.Printf("Project:     %s\n", sess.Project)
		fmt.Printf("Tags:        %s\n", strings.Join(sess.Tags, ", "))
		fmt.Printf("Branch:      %s\n", sess.GitBranch)
		fmt.Printf("Status:      %s\n", sess.Status)
		fmt.Printf("Created:     %s\n", sess.CreatedAt.Format("2006-01-02 15:04"))
		fmt.Printf("Last active: %s\n", sess.LastActiveAt.Format("2006-01-02 15:04"))
		fmt.Printf("File:        %s (%s)\n", sess.FilePath, format.Size(sess.FileSize))
		fmt.Printf("First prompt:\n  %s\n", strings.ReplaceAll(sess.FirstPrompt, "\n", "\n  "))
		return nil
	},
}

var rmFlags struct {
	purge bool
	force bool
}

var rmCmd = &cobra.Command{
	Use:   "rm <id-prefix|title>",
	Short: "Move a session to wallfacer's trash (--purge to delete permanently)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		s, sess, err := resolveSession(args[0], rmFlags.purge)
		if err != nil {
			return err
		}
		defer s.Close()

		action := "move to trash"
		if rmFlags.purge {
			action = "PERMANENTLY delete"
		}
		if !rmFlags.force {
			fmt.Printf("%s %q (%s, last active %s)? [y/N] ",
				action, sess.DisplayTitle(), store.ShortID(sess.ID), format.RelTime(sess.LastActiveAt))
			line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
			if ans := strings.ToLower(strings.TrimSpace(line)); ans != "y" && ans != "yes" {
				fmt.Println("aborted")
				return nil
			}
		}
		if rmFlags.purge {
			if err := s.Purge(*sess); err != nil {
				return err
			}
			fmt.Printf("purged %s\n", store.ShortID(sess.ID))
			return nil
		}
		dest, err := s.Trash(*sess)
		if err != nil {
			return err
		}
		fmt.Printf("trashed %s → %s\n", store.ShortID(sess.ID), dest)
		return nil
	},
}

func init() {
	tagCmd.AddCommand(tagAddCmd, tagRmCmd)
	projectCmd.AddCommand(projectSetCmd, projectClearCmd)
	rmCmd.Flags().BoolVar(&rmFlags.purge, "purge", false, "delete permanently instead of trashing")
	rmCmd.Flags().BoolVarP(&rmFlags.force, "force", "f", false, "skip confirmation prompt")
	rootCmd.AddCommand(renameCmd, tagCmd, projectCmd, searchCmd, showCmd, rmCmd)
}
