// Package lovedtracks orchestrates wholesale management of a user's loved
// songs. It depends on internal/gateway for transport and is independent of
// any other domains (loved albums, loved artists, playlists are not
// imported and must not be from this package).
package lovedtracks

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/niref/deezer-tools/internal/gateway"
)

// ErrAborted is returned when the user fails to confirm the wipe.
var ErrAborted = errors.New("wipe aborted by user")

// Gateway is the slice of internal/gateway.Client used by Wipe.
type Gateway interface {
	ListFavoriteSongs(ctx context.Context, pageSize int) ([]gateway.FavoriteSong, error)
	RemoveFavoriteSong(ctx context.Context, songID string) error
}

// Options configures a single Wipe run.
type Options struct {
	DryRun    bool
	BackupDir string
	PageSize  int
	Stdin     io.Reader
	Stdout    io.Writer
	Stderr    io.Writer
}

// Result summarizes a completed Wipe run.
type Result struct {
	ListedCount  int
	DeletedCount int
	SkippedCount int
	BackupPath   string
	SkipLogPath  string
	Elapsed      time.Duration
}

// Wipe executes the full list → backup → confirm → delete flow against gw.
//
// On success returns a populated Result and nil error.
// On confirmation failure returns ErrAborted.
// On listing or auth failure returns a wrapped error and (typically) a nil Result.
// On per-track 4xx skips, returns a non-nil error so callers can exit non-zero,
// and Result.SkippedCount/SkipLogPath are populated.
func Wipe(ctx context.Context, gw Gateway, opts Options) (*Result, error) {
	if opts.Stdout == nil {
		opts.Stdout = io.Discard
	}
	if opts.Stderr == nil {
		opts.Stderr = io.Discard
	}
	if opts.Stdin == nil {
		opts.Stdin = strings.NewReader("")
	}
	if opts.PageSize <= 0 {
		opts.PageSize = 100
	}
	if opts.BackupDir == "" {
		opts.BackupDir = "."
	}

	start := time.Now()

	fmt.Fprintln(opts.Stderr, "Listing loved tracks...")
	songs, err := gw.ListFavoriteSongs(ctx, opts.PageSize)
	if err != nil {
		return nil, fmt.Errorf("list loved tracks: %w", err)
	}
	fmt.Fprintf(opts.Stderr, "Listed %d tracks.\n", len(songs))

	res := &Result{ListedCount: len(songs)}

	if len(songs) == 0 {
		fmt.Fprintln(opts.Stdout, "No loved tracks to wipe.")
		res.Elapsed = time.Since(start)
		return res, nil
	}

	backupPath, err := writeBackup(opts.BackupDir, songs)
	if err != nil {
		return nil, fmt.Errorf("write backup: %w", err)
	}
	res.BackupPath = backupPath
	fmt.Fprintf(opts.Stderr, "Backup written to %s\n", backupPath)

	if opts.DryRun {
		fmt.Fprintf(opts.Stdout, "would delete %d tracks, backup at %s\n", len(songs), backupPath)
		res.Elapsed = time.Since(start)
		return res, nil
	}

	if !confirm(opts.Stdin, opts.Stdout, len(songs), backupPath) {
		return res, ErrAborted
	}

	skipLog, skipPath, err := openSkipLog(opts.BackupDir, backupPath)
	if err != nil {
		return res, fmt.Errorf("open skip log: %w", err)
	}
	defer skipLog.Close()

	for i, s := range songs {
		if err := deleteWithRetry(ctx, gw, s.ID); err != nil {
			var gerr *gateway.GatewayError
			if errors.As(err, &gerr) && gerr.Kind == gateway.ErrAuthFailed {
				res.Elapsed = time.Since(start)
				return res, fmt.Errorf("auth failed during delete (refresh your arl in ~/.config/deezer-tools/config.toml): %w", err)
			}
			res.SkippedCount++
			res.SkipLogPath = skipPath
			if werr := writeSkipEntry(skipLog, s, err); werr != nil {
				fmt.Fprintf(opts.Stderr, "warning: failed to record skip for track %s in %s: %v\n", s.ID, skipPath, werr)
			}
			continue
		}
		res.DeletedCount++
		if (i+1)%50 == 0 || i+1 == len(songs) {
			fmt.Fprintf(opts.Stderr, "deleted %d/%d\n", i+1, len(songs))
		}
	}

	res.Elapsed = time.Since(start)
	fmt.Fprintf(opts.Stdout, "Deleted %d, skipped %d", res.DeletedCount, res.SkippedCount)
	if res.SkippedCount > 0 {
		fmt.Fprintf(opts.Stdout, " (see %s)", res.SkipLogPath)
	}
	fmt.Fprintf(opts.Stdout, ", elapsed %s\n", res.Elapsed.Round(time.Second))

	if res.SkippedCount > 0 {
		return res, fmt.Errorf("%d track(s) skipped", res.SkippedCount)
	}
	return res, nil
}

func writeBackup(dir string, songs []gateway.FavoriteSong) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	stamp := time.Now().UTC().Format("20060102T150405Z")
	final := filepath.Join(dir, "deezer-loved-tracks-"+stamp+".json")
	tmp := final + ".tmp"

	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return "", err
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(songs); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return "", err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return "", err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	if err := os.Rename(tmp, final); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	return final, nil
}

func confirm(in io.Reader, out io.Writer, n int, backupPath string) bool {
	fmt.Fprintf(out, "Found %d loved tracks. Backup written to %s.\nType the number %d to confirm wipe: ", n, backupPath, n)
	r := bufio.NewReader(in)
	line, _ := r.ReadString('\n')
	line = strings.TrimSpace(line)
	expected := fmt.Sprintf("%d", n)
	if line != expected {
		fmt.Fprintln(out, "Aborted.")
		return false
	}
	return true
}

func openSkipLog(dir, backupPath string) (io.WriteCloser, string, error) {
	base := strings.TrimSuffix(filepath.Base(backupPath), ".json")
	skipPath := filepath.Join(dir, base+".skip.log")
	f, err := os.OpenFile(skipPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, "", err
	}
	return f, skipPath, nil
}

func writeSkipEntry(w io.Writer, s gateway.FavoriteSong, err error) error {
	entry := map[string]string{
		"id":     s.ID,
		"title":  s.Title,
		"artist": s.Artist,
		"error":  err.Error(),
	}
	b, _ := json.Marshal(entry)
	_, werr := fmt.Fprintln(w, string(b))
	return werr
}

func deleteWithRetry(ctx context.Context, gw Gateway, songID string) error {
	// CSRF refresh is handled inside the gateway client (callWithCSRF), so
	// this layer only retries on transient transport failures (rate limit /
	// 5xx). Auth failures and per-track 4xx errors return immediately and
	// are handled by the caller.
	backoff := []time.Duration{0, time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second}
	var lastErr error
	for _, d := range backoff {
		if d > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(d):
			}
		}
		err := gw.RemoveFavoriteSong(ctx, songID)
		if err == nil {
			return nil
		}
		lastErr = err
		var gerr *gateway.GatewayError
		if errors.As(err, &gerr) {
			switch gerr.Kind {
			case gateway.ErrRateLimited, gateway.ErrServerError:
				continue
			default:
				return err
			}
		}
		return err
	}
	return lastErr
}
