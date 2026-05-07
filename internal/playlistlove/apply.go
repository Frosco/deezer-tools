package playlistlove

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

	"github.com/niref/deezer-tools/internal/throttle"
)

// ErrUnsupportedRecordVersion means the record's "version" field is missing
// or differs from the version this build supports (currently 1).
var ErrUnsupportedRecordVersion = errors.New("playlistlove: unsupported record version")

// ErrMalformedRecord means the record file failed structural validation:
// invalid JSON, missing required keys, or an entry without an id.
var ErrMalformedRecord = errors.New("playlistlove: malformed record")

const supportedRecordVersion = 1

// LoadRunRecord reads and validates a record JSON file produced by
// playlists love-contents (with or without --dry-run).
//
// Returns ErrUnsupportedRecordVersion (wrapped with the version we saw) or
// ErrMalformedRecord (wrapped with the parse error or location) on failure.
//
// Unknown top-level keys are accepted for forward compatibility. The
// "stats" and "source_playlists" blocks are ignored at load time —
// LoadRunRecord is a pure structural check.
//
// "Both arrays absent" and "both arrays empty" collapse here: json.Unmarshal
// surfaces nil for either case and we don't distinguish them. The design spec
// listed the absent case as ErrMalformedRecord, but the practical effect of
// rejecting it (vs. letting ApplyFromRecord short-circuit "nothing to apply")
// is nil — so we accept it. TestLoadRunRecord_bothArraysEmpty pins that.
func LoadRunRecord(path string) (*RunRecord, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var rec RunRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMalformedRecord, err)
	}
	if rec.Version != supportedRecordVersion {
		return nil, fmt.Errorf("%w: file is version %d, this build supports version %d",
			ErrUnsupportedRecordVersion, rec.Version, supportedRecordVersion)
	}
	for i, a := range rec.AlbumsToAdd {
		if a.ID == "" {
			return nil, fmt.Errorf("%w: albums_to_add[%d]: missing id", ErrMalformedRecord, i)
		}
	}
	for i, a := range rec.ArtistsToAdd {
		if a.ID == "" {
			return nil, fmt.Errorf("%w: artists_to_add[%d]: missing id", ErrMalformedRecord, i)
		}
	}
	return &rec, nil
}

// ApplyOptions configures one ApplyFromRecord call.
//
// Sentinels match Run:
//   - RetryBackoff: nil → throttle.DefaultRetryBackoff, empty → no retries.
//   - MaxConsecutiveFinalFailures: 0 → throttle default, negative → disable.
//
// RecordPath is optional. When empty, stdout uses "(in-memory record)" and the
// skip-log basename uses "deezer-playlist-love-replay".
type ApplyOptions struct {
	Record                      *RunRecord
	RecordPath                  string
	BackupDir                   string
	AssumeYes                   bool
	Stdin                       io.Reader
	Stdout                      io.Writer
	Stderr                      io.Writer
	RetryBackoff                []time.Duration
	MaxConsecutiveFinalFailures int
}

// ApplyFromRecord applies a previously generated RunRecord: it filters already-
// loved items, deduplicates, confirms with the user, then calls applyPlan.
func ApplyFromRecord(ctx context.Context, gw Gateway, opts ApplyOptions) (*Result, error) {
	if opts.Record == nil {
		return nil, errors.New("playlistlove: ApplyOptions.Record must not be nil")
	}
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
	maxConsec := opts.MaxConsecutiveFinalFailures
	if maxConsec == 0 {
		maxConsec = throttle.DefaultMaxConsecutiveFinalFailures
	}

	res := &Result{StartedAt: time.Now().UTC()}

	// Short-circuit: record carries nothing.
	if len(opts.Record.AlbumsToAdd) == 0 && len(opts.Record.ArtistsToAdd) == 0 {
		fmt.Fprintln(opts.Stdout, "Nothing to apply (record is empty).")
		res.Elapsed = time.Since(res.StartedAt)
		return res, nil
	}

	// Fetch the current loved sets.
	lovedAlbums, err := gw.ListFavoriteAlbumIDs(ctx)
	if err != nil {
		return nil, fmt.Errorf("list loved albums: %w", err)
	}
	lovedArtists, err := gw.ListFavoriteArtistIDs(ctx)
	if err != nil {
		return nil, fmt.Errorf("list loved artists: %w", err)
	}

	// Filter already-loved items.
	albums, removedAlbums := filterAlreadyLovedAlbums(opts.Record.AlbumsToAdd, lovedAlbums)
	artists, removedArtists := filterAlreadyLovedArtists(opts.Record.ArtistsToAdd, lovedArtists)
	totalRemoved := removedAlbums + removedArtists
	if totalRemoved > 0 {
		fmt.Fprintf(opts.Stderr, "%d items already loved, skipping\n", totalRemoved)
	}

	// Dedupe duplicate IDs.
	albums, dedupedAlbums := dedupeAlbums(albums)
	artists, dedupedArtists := dedupeArtists(artists)
	totalDeduped := dedupedAlbums + dedupedArtists
	if totalDeduped > 0 {
		fmt.Fprintf(opts.Stderr, "%d duplicate entries collapsed\n", totalDeduped)
	}

	// Short-circuit: nothing left after filtering.
	if len(albums) == 0 && len(artists) == 0 {
		fmt.Fprintln(opts.Stdout, "Nothing to apply (all already loved).")
		res.Elapsed = time.Since(res.StartedAt)
		return res, nil
	}

	// Announce the plan.
	recordLabel := opts.RecordPath
	if recordLabel == "" {
		recordLabel = "(in-memory record)"
	}
	fmt.Fprintf(opts.Stdout, "Will love %d albums and %d artists from %s\n",
		len(albums), len(artists), recordLabel)

	// Confirm.
	if !opts.AssumeYes {
		fmt.Fprint(opts.Stdout, "Type yes to apply: ")
		ans, _ := bufio.NewReader(opts.Stdin).ReadString('\n')
		if !isYes(ans) {
			fmt.Fprintln(opts.Stdout, "Aborted.")
			res.Elapsed = time.Since(res.StartedAt)
			return res, ErrAborted
		}
	}

	// Apply.
	baseName := skipLogBaseName(opts.RecordPath, res.StartedAt)
	applyErr := applyPlan(ctx, gw, res, applyPlanOpts{
		BackupDir:                   opts.BackupDir,
		SkipLogBaseName:             baseName,
		RetryBackoff:                opts.RetryBackoff,
		MaxConsecutiveFinalFailures: maxConsec,
	}, albums, artists)

	fmt.Fprintf(opts.Stdout, "Added %d albums, %d artists, skipped %d", res.AddedAlbums, res.AddedArtists, res.SkippedItems)
	if res.SkippedItems > 0 {
		fmt.Fprintf(opts.Stdout, " (see %s)", res.SkipLogPath)
	}
	fmt.Fprintf(opts.Stdout, ", elapsed %s\n", res.Elapsed.Round(time.Second))

	return res, applyErr
}

// filterAlreadyLovedAlbums returns the subset of in that is not in loved,
// plus the count of removed entries. Order is preserved.
func filterAlreadyLovedAlbums(in []RecordAlbum, loved []string) ([]RecordAlbum, int) {
	set := make(map[string]struct{}, len(loved))
	for _, id := range loved {
		set[id] = struct{}{}
	}
	out := in[:0:0]
	for _, a := range in {
		if _, ok := set[a.ID]; ok {
			continue
		}
		out = append(out, a)
	}
	return out, len(in) - len(out)
}

// filterAlreadyLovedArtists is the artist-symmetric counterpart.
func filterAlreadyLovedArtists(in []RecordArtist, loved []string) ([]RecordArtist, int) {
	set := make(map[string]struct{}, len(loved))
	for _, id := range loved {
		set[id] = struct{}{}
	}
	out := in[:0:0]
	for _, a := range in {
		if _, ok := set[a.ID]; ok {
			continue
		}
		out = append(out, a)
	}
	return out, len(in) - len(out)
}

// dedupeAlbums returns the deduplicated slice (first-occurrence wins) and the
// count of removed duplicates.
func dedupeAlbums(in []RecordAlbum) ([]RecordAlbum, int) {
	seen := make(map[string]struct{}, len(in))
	out := in[:0:0]
	for _, a := range in {
		if _, ok := seen[a.ID]; ok {
			continue
		}
		seen[a.ID] = struct{}{}
		out = append(out, a)
	}
	return out, len(in) - len(out)
}

// dedupeArtists is the artist-symmetric counterpart.
func dedupeArtists(in []RecordArtist) ([]RecordArtist, int) {
	seen := make(map[string]struct{}, len(in))
	out := in[:0:0]
	for _, a := range in {
		if _, ok := seen[a.ID]; ok {
			continue
		}
		seen[a.ID] = struct{}{}
		out = append(out, a)
	}
	return out, len(in) - len(out)
}

// skipLogBaseName returns "<recordBase>.applied-<UTC>" where recordBase is
// derived from recordPath (stripping the .json extension), or
// "deezer-playlist-love-replay" when recordPath is empty.
func skipLogBaseName(recordPath string, started time.Time) string {
	base := "deezer-playlist-love-replay"
	if recordPath != "" {
		base = strings.TrimSuffix(filepath.Base(recordPath), ".json")
	}
	return base + ".applied-" + started.Format("20060102T150405Z")
}
