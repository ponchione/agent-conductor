package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Version is the current version of the conductor CLI.
const Version = "v0.1.0-dev"

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the conductor version",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println(Version)
	},
}
