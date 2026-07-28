package cmd

import (
	"fmt"

	"github.com/pradipta-s/wallfacer/internal/launcher"
	"github.com/spf13/cobra"
)

var resumeCmd = &cobra.Command{
	Use:     "resume <id-prefix|title>",
	Aliases: []string{"r"},
	Short:   "Resume a session in its original directory",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := openStore()
		if err != nil {
			return err
		}
		defer s.Close()
		if _, err := s.Sync(); err != nil {
			return err
		}
		sess, err := s.Resolve(args[0])
		if err != nil {
			return err
		}
		fmt.Printf("wallfacer: resuming %s in %s\n", sess.DisplayTitle(), sess.Dir)
		res, err := launcher.Resume(s, *sess)
		if err != nil {
			return err
		}
		return res.ExitErr
	},
}

func init() {
	rootCmd.AddCommand(resumeCmd)
}
