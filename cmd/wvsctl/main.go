package main

import (
	"os"

	"github.com/spf13/cobra"
)

var (
	apiURL string
	output string
)

var rootCmd = &cobra.Command{
	Use:   "wvsctl",
	Short: "WVS CLI - Workspace Versioning System command line tool",
	Long:  `wvsctl is a command line interface for the Workspace Versioning System (WVS).`,
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&apiURL, "api-url", "a", "http://localhost:8080", "WVS API URL")
	rootCmd.PersistentFlags().StringVarP(&output, "output", "o", "table", "Output format (table, json)")
}
