package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/niref/deezer-tools/internal/config"
	"github.com/niref/deezer-tools/internal/gateway"
	"github.com/niref/deezer-tools/internal/lovedtracks"
	"github.com/spf13/cobra"
)

func newLovedTracksCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "loved-tracks",
		Short: "Manage loved tracks (songs you liked)",
	}
	cmd.AddCommand(newWipeCmd())
	return cmd
}

func newWipeCmd() *cobra.Command {
	var dryRun bool
	var backupDir string

	cmd := &cobra.Command{
		Use:   "wipe",
		Short: "Delete every loved track. Loved albums and artists are not touched.",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfgPath := defaultConfigPath()
			cfg, err := config.Load(cfgPath)
			if err != nil {
				return fmt.Errorf("config: %w", err)
			}

			client := gateway.New(cfg.ARL)

			res, err := lovedtracks.Wipe(cmd.Context(), client, lovedtracks.Options{
				DryRun:    dryRun,
				BackupDir: backupDir,
				Stdin:     cmd.InOrStdin(),
				Stdout:    cmd.OutOrStdout(),
				Stderr:    cmd.ErrOrStderr(),
			})
			if err != nil {
				if errors.Is(err, lovedtracks.ErrAborted) {
					return err
				}
				if res != nil && res.SkippedCount > 0 {
					// non-zero exit but already-printed summary is fine
					return err
				}
				return err
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "list and back up tracks but do not delete")
	cmd.Flags().StringVar(&backupDir, "backup-dir", ".", "directory to write the JSON backup into")

	return cmd
}

func defaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "config.toml"
	}
	return filepath.Join(home, ".config", "deezer-tools", "config.toml")
}
