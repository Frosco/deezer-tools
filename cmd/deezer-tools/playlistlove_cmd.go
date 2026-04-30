package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/niref/deezer-tools/internal/config"
	"github.com/niref/deezer-tools/internal/gateway"
	"github.com/niref/deezer-tools/internal/playlistlove"
	"github.com/spf13/cobra"
)

func newPlaylistsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "playlists",
		Short: "Tools that take Deezer playlists as a source",
	}
	cmd.AddCommand(newLoveContentsCmd())
	return cmd
}

func newLoveContentsCmd() *cobra.Command {
	var dryRun bool
	var backupDir string

	cmd := &cobra.Command{
		Use:   "love-contents [PLAYLIST_INPUT...]",
		Short: "For the given playlists, love every album and artist whose songs appear in them",
		Long: `Read N Deezer playlists (numeric ID, full URL, or short link.deezer.com share link),
dedupe to unique albums and artists, diff against your loved-albums and loved-artists
collections, and (after confirmation) love the missing items.

If no positional args are given, reads one input per line from stdin (blank lines and
'#' comments ignored). When piping inputs, the confirm prompt reads from /dev/tty.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfgPath := defaultConfigPath()
			cfg, err := config.Load(cfgPath)
			if err != nil {
				return fmt.Errorf("config: %w", err)
			}
			client := gateway.New(cfg.ARL)

			inputs := args
			stdinUsedForInputs := false
			if len(inputs) == 0 {
				lines, err := playlistlove.ReadStdinInputs(cmd.InOrStdin())
				if err != nil {
					return fmt.Errorf("read stdin: %w", err)
				}
				inputs = lines
				stdinUsedForInputs = true
			}
			if len(inputs) == 0 {
				return fmt.Errorf("no playlist inputs given (positional or stdin)")
			}

			confirmReader := cmd.InOrStdin()
			if stdinUsedForInputs {
				if r, err := openTTY(); err == nil {
					confirmReader = r
					defer r.Close()
				} else {
					return fmt.Errorf("piping playlists requires a tty for confirm: %w", err)
				}
			}

			_, err = playlistlove.Run(cmd.Context(), client, playlistlove.Options{
				DryRun:    dryRun,
				BackupDir: backupDir,
				Inputs:    inputs,
				Stdin:     confirmReader,
				Stdout:    cmd.OutOrStdout(),
				Stderr:    cmd.ErrOrStderr(),
				ShareLinkResolver: playlistlove.DefaultShareLinkResolver(&http.Client{Timeout: 10 * time.Second}),
			})
			return err
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "compute diff and write run-record but do not love anything")
	cmd.Flags().StringVar(&backupDir, "backup-dir", ".", "directory for the run-record JSON and skip log")
	return cmd
}

// openTTY opens /dev/tty for reading. Used when stdin was consumed by the
// playlist input list and we still need a place to read the confirm answer.
func openTTY() (io.ReadCloser, error) {
	return os.OpenFile("/dev/tty", os.O_RDONLY, 0)
}
