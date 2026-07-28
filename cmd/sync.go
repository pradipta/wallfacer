package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Rescan agent session files and refresh the index",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := openStore()
		if err != nil {
			return err
		}
		defer s.Close()
		res, err := s.Sync()
		if err != nil {
			return err
		}
		fmt.Printf("scanned %d, parsed %d, missing %d, sidechain %d\n",
			res.Scanned, res.Parsed, res.Missing, res.Skipped)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(syncCmd)
}
