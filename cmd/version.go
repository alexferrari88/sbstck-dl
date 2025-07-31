package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// versionCmd represents the version command
var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version number of sbstck-dl",
	Long:  `Display the current version of the app.`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("sbstck-dl v0.6.5")
	},
}

func init() {
}
