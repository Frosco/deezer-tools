package main

import (
	"fmt"

	"github.com/niref/deezer-tools/internal/config"
	"github.com/niref/deezer-tools/internal/gateway"
	"github.com/niref/deezer-tools/internal/lovedalbums"
	"github.com/spf13/cobra"
)

func newLovedAlbumsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "loved-albums",
		Short: "Tools that operate on the user's loved-albums collection",
	}
	cmd.AddCommand(newDedupeCmd())
	return cmd
}

func newDedupeCmd() *cobra.Command {
	var dryRun bool
	var backupDir string
	var threshold int

	cmd := &cobra.Command{
		Use:   "dedupe",
		Short: "Find and (after confirm) un-love duplicate entries in the loved-albums list",
		Long: `Find duplicate loved albums in two cases:

  1. Same artist, same normalised title, different ALB_IDs → keep the album
     with most tracks (then most fans, then lowest ID); un-love the rest.
  2. A short loved album (default ≤3 tracks) whose title equals a track on a
     longer same-artist album that's also loved → un-love the short one.

Writes a JSON run record before doing anything destructive. After a single
batched confirmation, un-loves the losers in sequence with the same paced
throttle / retry / circuit-breaker discipline as the wipe and love-contents
commands.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfgPath := defaultConfigPath()
			cfg, err := config.Load(cfgPath)
			if err != nil {
				return fmt.Errorf("config: %w", err)
			}
			client := gateway.New(cfg.ARL)

			_, err = lovedalbums.Run(cmd.Context(), client, lovedalbums.Options{
				DryRun:              dryRun,
				BackupDir:           backupDir,
				Case2TrackThreshold: threshold,
				Stdin:               cmd.InOrStdin(),
				Stdout:              cmd.OutOrStdout(),
				Stderr:              cmd.ErrOrStderr(),
			})
			return err
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "detect, write run-record, do not unlove")
	cmd.Flags().StringVar(&backupDir, "backup-dir", ".", "directory for the run-record JSON and skip log")
	cmd.Flags().IntVar(&threshold, "case2-track-threshold", 3, "albums with at most this many tracks count as 'short' for Case 2")
	return cmd
}
