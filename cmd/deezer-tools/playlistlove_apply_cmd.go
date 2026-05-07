package main

import (
	"errors"
	"fmt"

	"github.com/niref/deezer-tools/internal/config"
	"github.com/niref/deezer-tools/internal/gateway"
	"github.com/niref/deezer-tools/internal/playlistlove"
	"github.com/spf13/cobra"
)

func newPlaylistsApplyRecordCmd() *cobra.Command {
	var assumeYes bool
	var backupDir string

	cmd := &cobra.Command{
		Use:   "apply-record FILE",
		Short: "Apply a previously-written love-contents run record",
		Long: `Read a deezer-playlist-love-<UTC>.json record produced by
'playlists love-contents' (typically with --dry-run), re-fetch your loved
albums and loved artists, silently skip anything already loved, and love the
remainder.

The record file is the source of truth for what gets loved. Edit it
between the dry-run and this command to exclude items you don't want — the
exclusion list is just rows you remove from the file.

A skip log is written to <backup-dir>/<record-base>.applied-<UTC>.skip.log
when individual adds fail. No new run-record file is written.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			recordPath := args[0]
			rec, err := playlistlove.LoadRunRecord(recordPath)
			if err != nil {
				return fmt.Errorf("load record: %w", err)
			}

			cfgPath := defaultConfigPath()
			cfg, err := config.Load(cfgPath)
			if err != nil {
				return fmt.Errorf("config: %w", err)
			}
			client := gateway.New(cfg.ARL)

			_, err = playlistlove.ApplyFromRecord(cmd.Context(), client, playlistlove.ApplyOptions{
				Record:     rec,
				RecordPath: recordPath,
				BackupDir:  backupDir,
				AssumeYes:  assumeYes,
				Stdin:      cmd.InOrStdin(),
				Stdout:     cmd.OutOrStdout(),
				Stderr:     cmd.ErrOrStderr(),
			})
			if errors.Is(err, playlistlove.ErrAborted) {
				return err
			}
			// Auth-failure on the loved-set fetch arrives unwrapped from
			// ApplyFromRecord; surface it with the standard refresh-arl hint.
			var gerr *gateway.GatewayError
			if errors.As(err, &gerr) && gerr.Kind == gateway.ErrAuthFailed {
				return fmt.Errorf("auth failed (refresh your arl in ~/.config/deezer-tools/config.toml): %w", err)
			}
			return err
		},
	}

	cmd.Flags().BoolVar(&assumeYes, "yes", false, "skip the confirm prompt")
	cmd.Flags().StringVar(&backupDir, "backup-dir", ".", "directory for the apply skip log")
	return cmd
}
