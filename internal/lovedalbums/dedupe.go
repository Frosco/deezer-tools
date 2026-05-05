package lovedalbums

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
	"github.com/niref/deezer-tools/internal/throttle"
)

// ErrAborted is returned when the user declines the confirmation prompt.
var ErrAborted = errors.New("lovedalbums: aborted by user")

// Gateway is the slice of internal/gateway.Client used by Run. Defined here
// (not in internal/gateway) to keep the dependency narrow and let tests fake
// the transport without spinning up an HTTP server.
type Gateway interface {
	ListFavoriteAlbumIDs(ctx context.Context) ([]string, error)
	GetAlbumMetadata(ctx context.Context, albumID string) (gateway.AlbumMetadata, error)
	ListAlbumTracks(ctx context.Context, albumID string) ([]gateway.AlbumTrack, error)
	RemoveFavoriteAlbum(ctx context.Context, albumID string) error
}

// Options configures one Run.
//
// Sentinels match the lovedtracks / playlistlove patterns:
//   - RetryBackoff: nil → throttle.DefaultRetryBackoff; empty → no retries.
//   - MaxConsecutiveFinalFailures: 0 → throttle default; negative → disable.
//   - Case2TrackThreshold: 0 → 3.
type Options struct {
	DryRun                      bool
	BackupDir                   string
	Stdin                       io.Reader
	Stdout                      io.Writer
	Stderr                      io.Writer
	Case2TrackThreshold         int
	RetryBackoff                []time.Duration
	MaxConsecutiveFinalFailures int
	OpenTTY                     func() (io.ReadCloser, error)
}

// Result summarizes a completed Run.
type Result struct {
	StartedAt      time.Time
	RunRecordPath  string
	SkipLogPath    string
	Case1Groups    int
	Case2Groups    int
	AlbumsToUnlove int
	AlbumsUnloved  int
	AlbumsSkipped  int
	Phase1Calls    int
	Phase2Calls    int
	Elapsed        time.Duration
}

// runRecord is the JSON payload written to
// <BackupDir>/deezer-loved-albums-dedupe-<UTC>.json.
type runRecord struct {
	Version     int            `json:"version"`
	StartedAt   string         `json:"started_at"`
	Stats       runRecordStats `json:"stats"`
	Case1Groups []recordCase1  `json:"case1_groups"`
	Case2Groups []recordCase2  `json:"case2_groups"`
	Unloves     []recordUnlove `json:"albums_to_unlove"`
}

type runRecordStats struct {
	LovedAlbums    int `json:"loved_albums"`
	Phase1Calls    int `json:"phase1_calls"`
	Phase2Calls    int `json:"phase2_calls"`
	Case1Groups    int `json:"case1_groups"`
	Case2Groups    int `json:"case2_groups"`
	AlbumsToUnlove int `json:"albums_to_unlove"`
}

type recordAlbumLite struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	FanCount   int    `json:"fan_count,omitempty"`
	TrackCount int    `json:"track_count,omitempty"`
}

type recordCase1 struct {
	ArtistID      string            `json:"artist_id"`
	ArtistName    string            `json:"artist_name"`
	NormalisedKey string            `json:"normalised_key"`
	Winner        recordAlbumLite   `json:"winner"`
	Losers        []recordAlbumLite `json:"losers"`
}

type recordCase2 struct {
	ArtistID   string            `json:"artist_id"`
	ArtistName string            `json:"artist_name"`
	Parent     recordAlbumLite   `json:"parent"`
	Shorts     []recordAlbumLite `json:"shorts"`
}

type recordUnlove struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Artist string `json:"artist"`
	Case   string `json:"case"`
	Reason string `json:"reason"`
}

// Run executes the full dedupe flow against gw.
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
	if opts.Case2TrackThreshold <= 0 {
		opts.Case2TrackThreshold = 3
	}

	res := &Result{StartedAt: time.Now().UTC()}

	ids, err := gw.ListFavoriteAlbumIDs(ctx)
	if err != nil {
		var ge *gateway.GatewayError
		if errors.As(err, &ge) && ge.Kind == gateway.ErrAuthFailed {
			return nil, fmt.Errorf("auth failed listing loved albums (refresh your arl in ~/.config/deezer-tools/config.toml): %w", err)
		}
		return nil, fmt.Errorf("list loved albums: %w", err)
	}

	notify1 := func(id string, e error) {
		fmt.Fprintf(opts.Stderr, "phase1 dropped %s: %v\n", id, e)
	}
	loved, err := Phase1Fetch(ctx, gw, ids, opts.RetryBackoff, notify1)
	res.Phase1Calls = len(ids)
	if err != nil {
		return res, classifyAuth(err, "phase1 metadata fetch")
	}

	c1 := DetectCase1(loved)
	res.Case1Groups = len(c1)

	loserIDs := make(map[string]bool)
	for _, g := range c1 {
		for _, m := range g.Members[1:] {
			loserIDs[m.ID] = true
		}
	}
	post := make([]gateway.AlbumMetadata, 0, len(loved))
	for _, a := range loved {
		if !loserIDs[a.ID] {
			post = append(post, a)
		}
	}

	notify2 := func(id string, e error) {
		fmt.Fprintf(opts.Stderr, "phase2 dropped %s: %v\n", id, e)
	}
	tracksLookup, phase2Attempts, err := Phase2Fetch(ctx, gw, post, opts.Case2TrackThreshold, opts.RetryBackoff, notify2)
	res.Phase2Calls = phase2Attempts
	if err != nil {
		return res, classifyAuth(err, "phase2 tracklist fetch")
	}

	c2, err := DetectCase2(post, func(id string) ([]gateway.AlbumTrack, error) {
		t, ok := tracksLookup(id)
		if !ok {
			return nil, errSkippedTracks
		}
		return t, nil
	}, opts.Case2TrackThreshold)
	if err != nil {
		return res, fmt.Errorf("detect case 2: %w", err)
	}
	res.Case2Groups = len(c2)

	plan := BuildPlan(c1, c2)
	res.AlbumsToUnlove = len(plan.AlbumsToUnlove)

	rec := buildRunRecord(res, len(ids), plan)
	recPath, err := writeRunRecord(opts.BackupDir, res.StartedAt, rec)
	if err != nil {
		return res, fmt.Errorf("write run record: %w", err)
	}
	res.RunRecordPath = recPath
	fmt.Fprintf(opts.Stderr, "Run record written to %s\n", recPath)

	if len(plan.AlbumsToUnlove) == 0 {
		fmt.Fprintln(opts.Stdout, "Nothing to dedupe; loved-albums list is clean.")
		res.Elapsed = time.Since(res.StartedAt)
		return res, nil
	}

	if opts.DryRun {
		fmt.Fprintf(opts.Stdout, "would unlove %d albums (%d case-1, %d case-2), run-record at %s\n",
			res.AlbumsToUnlove, res.Case1Groups, res.Case2Groups, recPath)
		res.Elapsed = time.Since(res.StartedAt)
		return res, nil
	}

	confirmReader := bufio.NewReader(opts.Stdin)
	fmt.Fprintf(opts.Stdout, "Will unlove %d albums (%d case-1 dups, %d case-2 singles).\n",
		res.AlbumsToUnlove, res.Case1Groups, res.Case2Groups)
	fmt.Fprintf(opts.Stdout, "Run record: %s\n", recPath)
	fmt.Fprint(opts.Stdout, "Type yes to apply: ")
	ans, _ := confirmReader.ReadString('\n')
	if !isYes(ans) {
		fmt.Fprintln(opts.Stdout, "Aborted.")
		res.Elapsed = time.Since(res.StartedAt)
		return res, ErrAborted
	}

	skipLog, skipPath, err := openSkipLog(opts.BackupDir, recPath)
	if err != nil {
		return res, fmt.Errorf("open skip log: %w", err)
	}
	defer skipLog.Close()
	res.SkipLogPath = skipPath

	maxConsec := opts.MaxConsecutiveFinalFailures
	if maxConsec == 0 {
		maxConsec = throttle.DefaultMaxConsecutiveFinalFailures
	}
	streak := 0
	for _, e := range plan.AlbumsToUnlove {
		select {
		case <-ctx.Done():
			res.Elapsed = time.Since(res.StartedAt)
			return res, ctx.Err()
		default:
		}
		if err := throttle.Sleep(ctx); err != nil {
			res.Elapsed = time.Since(res.StartedAt)
			return res, err
		}
		albumID := e.Album.ID
		callErr := throttle.RunOne(ctx, func(ctx context.Context) error {
			return gw.RemoveFavoriteAlbum(ctx, albumID)
		}, gateway.IsRetryable, opts.RetryBackoff)
		if callErr == nil {
			res.AlbumsUnloved++
			streak = 0
			continue
		}
		if errors.Is(callErr, context.Canceled) || errors.Is(callErr, context.DeadlineExceeded) {
			res.Elapsed = time.Since(res.StartedAt)
			return res, callErr
		}
		var ge *gateway.GatewayError
		if errors.As(callErr, &ge) && ge.Kind == gateway.ErrAuthFailed {
			res.Elapsed = time.Since(res.StartedAt)
			return res, fmt.Errorf("auth failed during unlove (refresh your arl in ~/.config/deezer-tools/config.toml): %w", callErr)
		}
		res.AlbumsSkipped++
		_ = writeSkipEntry(skipLog, e, callErr)
		streak++
		if maxConsec > 0 && streak >= maxConsec {
			res.Elapsed = time.Since(res.StartedAt)
			return res, fmt.Errorf("aborting: %d consecutive unlove failures (quota likely tripped or service degraded). Skipped items recorded in %s", streak, skipPath)
		}
	}

	res.Elapsed = time.Since(res.StartedAt)
	fmt.Fprintf(opts.Stdout, "Unloved %d albums (%d case-1, %d case-2), skipped %d",
		res.AlbumsUnloved, res.Case1Groups, res.Case2Groups, res.AlbumsSkipped)
	if res.AlbumsSkipped > 0 {
		fmt.Fprintf(opts.Stdout, " (see %s)", skipPath)
	}
	fmt.Fprintf(opts.Stdout, ", elapsed %s\n", res.Elapsed.Round(time.Second))

	if res.AlbumsSkipped > 0 {
		return res, fmt.Errorf("%d album(s) skipped", res.AlbumsSkipped)
	}
	return res, nil
}

func isYes(s string) bool {
	return strings.EqualFold(strings.TrimSpace(s), "yes")
}

func openSkipLog(dir, recordPath string) (io.WriteCloser, string, error) {
	base := strings.TrimSuffix(filepath.Base(recordPath), ".json")
	skipPath := filepath.Join(dir, base+".skip.log")
	f, err := os.OpenFile(skipPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, "", err
	}
	return f, skipPath, nil
}

type skipEntry struct {
	ID     string `json:"id"`
	Title  string `json:"title,omitempty"`
	Artist string `json:"artist,omitempty"`
	Case   string `json:"case"`
	Reason string `json:"reason,omitempty"`
	Error  string `json:"error"`
}

func writeSkipEntry(w io.Writer, e UnloveEntry, err error) error {
	rec := skipEntry{
		ID: e.Album.ID, Title: e.Album.Title, Artist: e.Album.ArtistName,
		Case: e.Case.String(), Reason: e.Reason, Error: err.Error(),
	}
	b, _ := json.Marshal(rec)
	_, werr := fmt.Fprintln(w, string(b))
	return werr
}

// errSkippedTracks is the sentinel that bridges Phase2Fetch's "no entry"
// signal into DetectCase2's fetchTracks error contract. DetectCase2 treats
// any error as "drop this parent from the pool", which is the desired
// behaviour for skipped fetches.
var errSkippedTracks = errors.New("phase2: tracks unavailable")

func classifyAuth(err error, prefix string) error {
	var ge *gateway.GatewayError
	if errors.As(err, &ge) && ge.Kind == gateway.ErrAuthFailed {
		return fmt.Errorf("auth failed during %s (refresh your arl in ~/.config/deezer-tools/config.toml): %w", prefix, err)
	}
	return fmt.Errorf("%s: %w", prefix, err)
}

func buildRunRecord(res *Result, lovedCount int, plan DedupePlan) runRecord {
	rec := runRecord{
		Version:   1,
		StartedAt: res.StartedAt.Format(time.RFC3339),
		Stats: runRecordStats{
			LovedAlbums:    lovedCount,
			Phase1Calls:    res.Phase1Calls,
			Phase2Calls:    res.Phase2Calls,
			Case1Groups:    res.Case1Groups,
			Case2Groups:    res.Case2Groups,
			AlbumsToUnlove: res.AlbumsToUnlove,
		},
	}
	for _, g := range plan.Case1Groups {
		var entry recordCase1
		entry.ArtistID = g.ArtistID
		entry.ArtistName = g.ArtistName
		entry.NormalisedKey = g.NormalisedKey
		entry.Winner = liteOf(g.Members[0])
		for _, m := range g.Members[1:] {
			entry.Losers = append(entry.Losers, liteOf(m))
		}
		rec.Case1Groups = append(rec.Case1Groups, entry)
	}
	for _, g := range plan.Case2Groups {
		var entry recordCase2
		entry.ArtistID = g.ArtistID
		entry.ArtistName = g.ArtistName
		entry.Parent = liteOf(g.Parent)
		for _, s := range g.Shorts {
			entry.Shorts = append(entry.Shorts, liteOf(s))
		}
		rec.Case2Groups = append(rec.Case2Groups, entry)
	}
	for _, e := range plan.AlbumsToUnlove {
		rec.Unloves = append(rec.Unloves, recordUnlove{
			ID: e.Album.ID, Title: e.Album.Title, Artist: e.Album.ArtistName,
			Case: e.Case.String(), Reason: e.Reason,
		})
	}
	return rec
}

func liteOf(a gateway.AlbumMetadata) recordAlbumLite {
	return recordAlbumLite{
		ID: a.ID, Title: a.Title,
		FanCount: a.FanCount, TrackCount: a.TrackCount,
	}
}

func writeRunRecord(dir string, started time.Time, rec runRecord) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	stamp := started.Format("20060102T150405Z")
	final := filepath.Join(dir, "deezer-loved-albums-dedupe-"+stamp+".json")
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

