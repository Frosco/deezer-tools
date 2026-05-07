package playlistlove

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/niref/deezer-tools/internal/gateway"
	"github.com/niref/deezer-tools/internal/throttle"
)

// ErrAborted is returned when the user declines a confirm prompt.
var ErrAborted = errors.New("playlistlove: aborted by user")

// Gateway is the slice of internal/gateway.Client used by Run. Defining it
// here (rather than in internal/gateway) keeps the dependency narrow and
// lets tests fake transport without spinning up an HTTP server.
type Gateway interface {
	ListPlaylistSongs(ctx context.Context, playlistID string, pageSize int) ([]gateway.PlaylistSong, error)
	ListFavoriteAlbumIDs(ctx context.Context) ([]string, error)
	ListFavoriteArtistIDs(ctx context.Context) ([]string, error)
	GetAlbumMetadata(ctx context.Context, albumID string) (gateway.AlbumMetadata, error)
	AddFavoriteAlbum(ctx context.Context, albumID string) error
	AddFavoriteArtist(ctx context.Context, artistID string) error
}

// Options configures one Run.
//
// Sentinels match the lovedtracks pattern:
//   - RetryBackoff: nil → throttle.DefaultRetryBackoff, empty → no retries.
//   - MaxConsecutiveFinalFailures: 0 → throttle default, negative → disable.
type Options struct {
	DryRun                      bool
	BackupDir                   string
	PageSize                    int
	Inputs                      []string
	Stdin                       io.Reader
	Stdout                      io.Writer
	Stderr                      io.Writer
	RetryBackoff                []time.Duration
	MaxConsecutiveFinalFailures int
	VariousArtistsID            string
	ShareLinkResolver           ResolveShareLink
	OpenTTY                     func() (io.ReadCloser, error)
}

// Result summarizes a completed Run.
type Result struct {
	StartedAt       time.Time
	RunRecordPath   string
	SkipLogPath     string
	AddedAlbums     int
	AddedArtists    int
	SkippedItems    int
	PlaylistsLoaded int
	PlaylistsFailed int
	Elapsed         time.Duration
}

// RunRecord is the JSON payload written to <BackupDir>/deezer-playlist-love-<UTC>.json.
type RunRecord struct {
	Version         int              `json:"version"`
	StartedAt       string           `json:"started_at"`
	SourcePlaylists []RecordPlaylist `json:"source_playlists"`
	Stats           RunRecordStats   `json:"stats"`
	AlbumsToAdd     []RecordAlbum    `json:"albums_to_add"`
	ArtistsToAdd    []RecordArtist   `json:"artists_to_add"`
}

type RecordPlaylist struct {
	Input      string `json:"input"`
	PlaylistID string `json:"playlist_id"`
	SongCount  int    `json:"song_count"`
}

type RunRecordStats struct {
	SongsScanned                  int `json:"songs_scanned"`
	PlaylistsLoaded               int `json:"playlists_loaded"`
	PlaylistsFailed               int `json:"playlists_failed"`
	UniqueAlbums                  int `json:"unique_albums"`
	UniqueArtists                 int `json:"unique_artists"`
	VariousArtistsSkipped         int `json:"various_artists_skipped"`
	UnparseableSongs              int `json:"unparseable_songs"`
	Case1WithinPlaylistSuppressed int `json:"case1_within_playlist_suppressed"`
	AlbumsAlreadyLoved            int `json:"albums_already_loved"`
	ArtistsAlreadyLoved           int `json:"artists_already_loved"`
	AlbumsToAdd                   int `json:"albums_to_add"`
	ArtistsToAdd                  int `json:"artists_to_add"`
}

type RecordAlbum struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Artist string `json:"artist"`
}

type RecordArtist struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Run executes the full love-contents flow against gw.
func Run(ctx context.Context, gw Gateway, opts Options) (*Result, error) {
	if opts.Stdout == nil {
		opts.Stdout = io.Discard
	}
	if opts.Stderr == nil {
		opts.Stderr = io.Discard
	}
	if opts.Stdin == nil {
		opts.Stdin = strings.NewReader("")
	}
	if opts.BackupDir == "" {
		opts.BackupDir = "."
	}
	if opts.VariousArtistsID == "" {
		opts.VariousArtistsID = DefaultVariousArtistsID
	}
	if opts.ShareLinkResolver == nil {
		opts.ShareLinkResolver = DefaultShareLinkResolver(&http.Client{Timeout: 10 * time.Second})
	}
	maxConsec := opts.MaxConsecutiveFinalFailures
	if maxConsec == 0 {
		maxConsec = throttle.DefaultMaxConsecutiveFinalFailures
	}

	res := &Result{StartedAt: time.Now().UTC()}
	confirmReader := bufio.NewReader(opts.Stdin)

	// 1. Normalize inputs.
	good, badInputs := NormalizeInputs(ctx, opts.Inputs, opts.ShareLinkResolver)
	for _, b := range badInputs {
		fmt.Fprintf(opts.Stderr, "input %q failed: %s\n", b.Raw, b.Reason)
	}
	if len(good) == 0 {
		return nil, fmt.Errorf("no valid playlist inputs (%d failed)", len(badInputs))
	}

	// 2. Load each playlist.
	var allSongs []gateway.PlaylistSong
	var sourcePlaylists []RecordPlaylist
	var loadFailures []InputError
	for _, in := range good {
		songs, err := gw.ListPlaylistSongs(ctx, in.PlaylistID, opts.PageSize)
		if err != nil {
			var gerr *gateway.GatewayError
			if errors.As(err, &gerr) && gerr.Kind == gateway.ErrAuthFailed {
				return nil, fmt.Errorf("auth failed loading playlist %s (refresh your arl in ~/.config/deezer-tools/config.toml): %w", in.PlaylistID, err)
			}
			fmt.Fprintf(opts.Stderr, "playlist %s failed to load: %v\n", in.PlaylistID, err)
			loadFailures = append(loadFailures, InputError{Raw: in.Raw, Reason: err.Error()})
			continue
		}
		allSongs = append(allSongs, songs...)
		sourcePlaylists = append(sourcePlaylists, RecordPlaylist{
			Input: in.Raw, PlaylistID: in.PlaylistID, SongCount: len(songs),
		})
		res.PlaylistsLoaded++
	}
	res.PlaylistsFailed = len(loadFailures) + len(badInputs)

	// 3. Partial-input prompt.
	if (len(loadFailures) > 0 || len(badInputs) > 0) && !opts.DryRun {
		if res.PlaylistsLoaded == 0 {
			return nil, fmt.Errorf("no playlists loaded successfully (%d failed)", res.PlaylistsFailed)
		}
		fmt.Fprintf(opts.Stdout, "Proceed with %d of %d playlists? Type yes to continue: ", res.PlaylistsLoaded, res.PlaylistsLoaded+res.PlaylistsFailed)
		ans, _ := confirmReader.ReadString('\n')
		if !isYes(ans) {
			return nil, ErrAborted
		}
	}

	// 4. Aggregate + dedupe.
	set := Aggregate(allSongs, opts.VariousArtistsID)
	set, err := CollapseCase1WithinPlaylist(ctx, gw, set, opts.RetryBackoff)
	if err != nil {
		var gerr *gateway.GatewayError
		if errors.As(err, &gerr) && gerr.Kind == gateway.ErrAuthFailed {
			return nil, fmt.Errorf("auth failed during within-playlist dedup (refresh your arl in ~/.config/deezer-tools/config.toml): %w", err)
		}
		return nil, fmt.Errorf("within-playlist dedup: %w", err)
	}

	// 5. Read loved sets. Both methods are single-call (deezer.pageProfile);
	// no pagination knob needed — see the 2026-04-30 research doc.
	lovedAlbums, err := gw.ListFavoriteAlbumIDs(ctx)
	if err != nil {
		return nil, fmt.Errorf("list loved albums: %w", err)
	}
	lovedArtists, err := gw.ListFavoriteArtistIDs(ctx)
	if err != nil {
		return nil, fmt.Errorf("list loved artists: %w", err)
	}

	// 6. Diff.
	plan := Diff(set, DiffInputs{LovedAlbumIDs: lovedAlbums, LovedArtistIDs: lovedArtists})

	// 7. Run-record.
	rec := RunRecord{
		Version:         1,
		StartedAt:       res.StartedAt.Format(time.RFC3339),
		SourcePlaylists: sourcePlaylists,
		Stats: RunRecordStats{
			SongsScanned:                  len(allSongs),
			PlaylistsLoaded:               res.PlaylistsLoaded,
			PlaylistsFailed:               res.PlaylistsFailed,
			UniqueAlbums:                  len(set.Albums),
			UniqueArtists:                 len(set.Artists),
			VariousArtistsSkipped:         set.VariousArtistsSkipped,
			UnparseableSongs:              set.UnparseableSongs,
			Case1WithinPlaylistSuppressed: set.Case1WithinPlaylistSuppressed,
			AlbumsAlreadyLoved:            plan.AlbumsAlreadyLoved,
			ArtistsAlreadyLoved:           plan.ArtistsAlreadyLoved,
			AlbumsToAdd:                   len(plan.AlbumsToAdd),
			ArtistsToAdd:                  len(plan.ArtistsToAdd),
		},
	}
	for _, a := range plan.AlbumsToAdd {
		rec.AlbumsToAdd = append(rec.AlbumsToAdd, RecordAlbum{ID: a.ID, Title: a.Title, Artist: a.Artist})
	}
	for _, a := range plan.ArtistsToAdd {
		rec.ArtistsToAdd = append(rec.ArtistsToAdd, RecordArtist{ID: a.ID, Name: a.Name})
	}
	recordPath, err := writeRunRecord(opts.BackupDir, res.StartedAt, rec)
	if err != nil {
		return nil, fmt.Errorf("write run record: %w", err)
	}
	res.RunRecordPath = recordPath
	fmt.Fprintf(opts.Stderr, "Run record written to %s\n", recordPath)

	// 8. Empty-diff short-circuit.
	if len(plan.AlbumsToAdd) == 0 && len(plan.ArtistsToAdd) == 0 {
		fmt.Fprintln(opts.Stdout, "Nothing to add, your loved albums and artists already cover these playlists.")
		res.Elapsed = time.Since(res.StartedAt)
		return res, nil
	}

	// 9. Dry-run short-circuit.
	if opts.DryRun {
		fmt.Fprintf(opts.Stdout, "would add %d albums and %d artists, run-record at %s\n",
			len(plan.AlbumsToAdd), len(plan.ArtistsToAdd), recordPath)
		res.Elapsed = time.Since(res.StartedAt)
		return res, nil
	}

	// 10. Confirm.
	fmt.Fprintf(opts.Stdout, "Plan: love %d albums and %d artists missing from your library\n",
		len(plan.AlbumsToAdd), len(plan.ArtistsToAdd))
	fmt.Fprintf(opts.Stdout, "  (sourced from %d playlists, %d unique songs scanned)\n",
		res.PlaylistsLoaded, len(allSongs))
	fmt.Fprintf(opts.Stdout, "Run record: %s\n", recordPath)
	fmt.Fprint(opts.Stdout, "Type yes to apply: ")
	ans, _ := confirmReader.ReadString('\n')
	if !isYes(ans) {
		fmt.Fprintln(opts.Stdout, "Aborted.")
		return res, ErrAborted
	}

	// 11–14. Apply phase, shared with ApplyFromRecord.
	baseName := strings.TrimSuffix(filepath.Base(recordPath), ".json")
	applyErr := applyPlan(ctx, gw, res, applyPlanOpts{
		BackupDir:                   opts.BackupDir,
		SkipLogBaseName:             baseName,
		RetryBackoff:                opts.RetryBackoff,
		MaxConsecutiveFinalFailures: maxConsec,
	}, rec.AlbumsToAdd, rec.ArtistsToAdd)

	fmt.Fprintf(opts.Stdout, "Added %d albums, %d artists, skipped %d", res.AddedAlbums, res.AddedArtists, res.SkippedItems)
	if res.SkippedItems > 0 {
		fmt.Fprintf(opts.Stdout, " (see %s)", res.SkipLogPath)
	}
	fmt.Fprintf(opts.Stdout, ", elapsed %s\n", res.Elapsed.Round(time.Second))

	return res, applyErr
}

// applyPlan executes the apply phase: open the skip log, throttle/retry every
// album add, then every artist add, with the streak circuit breaker. It is
// shared between Run (full pipeline) and ApplyFromRecord (replay from an
// edited record file).
//
// recordPath is used only to derive the skip-log path. albums and artists are
// the already-filtered, already-deduped slices to apply. res is mutated in
// place — AddedAlbums, AddedArtists, SkippedItems, SkipLogPath, Elapsed.
//
// Returns the same errors as before: ErrAuthFailed wrapped with the refresh-
// arl message, the streak-breaker error, ctx.Err() on cancel, or a
// "%d item(s) skipped" error when SkippedItems > 0.
func applyPlan(
	ctx context.Context,
	gw Gateway,
	res *Result,
	opts applyPlanOpts,
	albums []RecordAlbum,
	artists []RecordArtist,
) error {
	skipLog, skipPath, err := openSkipLog(opts.BackupDir, opts.SkipLogBaseName)
	if err != nil {
		return fmt.Errorf("open skip log: %w", err)
	}
	defer skipLog.Close()
	res.SkipLogPath = skipPath

	streak := 0
	for _, a := range albums {
		select {
		case <-ctx.Done():
			res.Elapsed = time.Since(res.StartedAt)
			return ctx.Err()
		default:
		}
		if err := throttle.Sleep(ctx); err != nil {
			res.Elapsed = time.Since(res.StartedAt)
			return err
		}
		albumID := a.ID
		err := throttle.RunOne(ctx, func(ctx context.Context) error {
			return gw.AddFavoriteAlbum(ctx, albumID)
		}, gateway.IsRetryable, opts.RetryBackoff)
		if err == nil {
			res.AddedAlbums++
			streak = 0
			continue
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			res.Elapsed = time.Since(res.StartedAt)
			return err
		}
		var gerr *gateway.GatewayError
		if errors.As(err, &gerr) && gerr.Kind == gateway.ErrAuthFailed {
			res.Elapsed = time.Since(res.StartedAt)
			return fmt.Errorf("auth failed during album apply (refresh your arl in ~/.config/deezer-tools/config.toml): %w", err)
		}
		res.SkippedItems++
		_ = writeSkipEntry(skipLog, "album", a.ID, a.Title, a.Artist, err)
		streak++
		if opts.MaxConsecutiveFinalFailures > 0 && streak >= opts.MaxConsecutiveFinalFailures {
			res.Elapsed = time.Since(res.StartedAt)
			return fmt.Errorf("aborting: %d consecutive add failures (quota likely tripped or service degraded). Skipped items recorded in %s", streak, skipPath)
		}
	}

	for _, a := range artists {
		select {
		case <-ctx.Done():
			res.Elapsed = time.Since(res.StartedAt)
			return ctx.Err()
		default:
		}
		if err := throttle.Sleep(ctx); err != nil {
			res.Elapsed = time.Since(res.StartedAt)
			return err
		}
		artistID := a.ID
		err := throttle.RunOne(ctx, func(ctx context.Context) error {
			return gw.AddFavoriteArtist(ctx, artistID)
		}, gateway.IsRetryable, opts.RetryBackoff)
		if err == nil {
			res.AddedArtists++
			streak = 0
			continue
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			res.Elapsed = time.Since(res.StartedAt)
			return err
		}
		var gerr *gateway.GatewayError
		if errors.As(err, &gerr) && gerr.Kind == gateway.ErrAuthFailed {
			res.Elapsed = time.Since(res.StartedAt)
			return fmt.Errorf("auth failed during artist apply (refresh your arl in ~/.config/deezer-tools/config.toml): %w", err)
		}
		res.SkippedItems++
		_ = writeSkipEntry(skipLog, "artist", a.ID, a.Name, "", err)
		streak++
		if opts.MaxConsecutiveFinalFailures > 0 && streak >= opts.MaxConsecutiveFinalFailures {
			res.Elapsed = time.Since(res.StartedAt)
			return fmt.Errorf("aborting: %d consecutive add failures (quota likely tripped or service degraded). Skipped items recorded in %s", streak, skipPath)
		}
	}

	res.Elapsed = time.Since(res.StartedAt)
	if res.SkippedItems > 0 {
		return fmt.Errorf("%d item(s) skipped", res.SkippedItems)
	}
	return nil
}

// applyPlanOpts is the tunables the apply loop needs from its caller.
// SkipLogBaseName is the basename (without ".json") whose ".skip.log"
// sibling will be created in BackupDir.
type applyPlanOpts struct {
	BackupDir                   string
	SkipLogBaseName             string
	RetryBackoff                []time.Duration
	MaxConsecutiveFinalFailures int
}

func isYes(s string) bool {
	return strings.EqualFold(strings.TrimSpace(s), "yes")
}

func writeRunRecord(dir string, started time.Time, rec RunRecord) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	stamp := started.Format("20060102T150405Z")
	final := filepath.Join(dir, "deezer-playlist-love-"+stamp+".json")
	tmp := final + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return "", err
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(rec); err != nil {
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

func openSkipLog(dir, baseName string) (io.WriteCloser, string, error) {
	skipPath := filepath.Join(dir, baseName+".skip.log")
	f, err := os.OpenFile(skipPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, "", err
	}
	return f, skipPath, nil
}

type skipEntry struct {
	Kind   string `json:"kind"`
	ID     string `json:"id"`
	Title  string `json:"title,omitempty"`
	Artist string `json:"artist,omitempty"`
	Error  string `json:"error"`
}

func writeSkipEntry(w io.Writer, kind, id, title, artist string, err error) error {
	b, _ := json.Marshal(skipEntry{Kind: kind, ID: id, Title: title, Artist: artist, Error: err.Error()})
	_, werr := fmt.Fprintln(w, string(b))
	return werr
}
