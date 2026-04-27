package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "deezer-tools",
	Short: "Personal toolbox for Deezer account automation",
	Long:  `deezer-tools is a CLI for managing a personal Deezer account: loved content, playlists, and more.`,
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
