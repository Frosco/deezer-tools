package playlistlove

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/niref/deezer-tools/internal/gateway"
	"github.com/niref/deezer-tools/internal/throttle"
)

// fakeGateway implements Gateway for tests.
type fakeGateway struct {
	playlistSongs       map[string][]gateway.PlaylistSong
	playlistErrs        map[string]error
	lovedAlbumIDs       []string
	lovedArtistIDs      []string
	addAlbumErrs        map[string]error
	addArtistErrs       map[string]error
	addedAlbums         []string
	addedArtists        []string
	listLovedAlbumsErr  error
	listLovedArtistsErr error
}

func (f *fakeGateway) ListPlaylistSongs(ctx context.Context, id string, _ int) ([]gateway.PlaylistSong, error) {
	if err := f.playlistErrs[id]; err != nil {
		return nil, err
	}
	return f.playlistSongs[id], nil
}

func (f *fakeGateway) ListFavoriteAlbumIDs(ctx context.Context) ([]string, error) {
	if f.listLovedAlbumsErr != nil {
		return nil, f.listLovedAlbumsErr
	}
	return f.lovedAlbumIDs, nil
}

func (f *fakeGateway) ListFavoriteArtistIDs(ctx context.Context) ([]string, error) {
	if f.listLovedArtistsErr != nil {
		return nil, f.listLovedArtistsErr
	}
	return f.lovedArtistIDs, nil
}

func (f *fakeGateway) GetAlbumMetadata(ctx context.Context, id string) (gateway.AlbumMetadata, error) {
	// Default no-op for tests that don't exercise within-playlist dedup; if a
	// test creates conflict groups it must use a richer fake (see diff_test.go).
	return gateway.AlbumMetadata{}, nil
}

func (f *fakeGateway) AddFavoriteAlbum(ctx context.Context, id string) error {
	if err := f.addAlbumErrs[id]; err != nil {
		return err
	}
	f.addedAlbums = append(f.addedAlbums, id)
	return nil
}

func (f *fakeGateway) AddFavoriteArtist(ctx context.Context, id string) error {
	if err := f.addArtistErrs[id]; err != nil {
		return err
	}
	f.addedArtists = append(f.addedArtists, id)
	return nil
}

// helpers

func tmpBackupDir(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}

func defaultOpts(stdin string, dir string, inputs ...string) Options {
	return Options{
		Inputs:    inputs,
		BackupDir: dir,
		Stdin:     strings.NewReader(stdin),
		Stdout:    &bytes.Buffer{},
		Stderr:    &bytes.Buffer{},
		// Disable retries and pacing for fast tests; pacer vars are zeroed
		// in init() above (they live in throttle).
		RetryBackoff: []time.Duration{},
	}
}

func init() {
	// Zero the throttle pacer so apply tests don't sleep.
	throttle.Pace = 0
	throttle.Jitter = 0
}

func TestRun_emptyDiffExits0(t *testing.T) {
	dir := tmpBackupDir(t)
	gw := &fakeGateway{
		playlistSongs: map[string][]gateway.PlaylistSong{
			"1": {{SongID: "s1", AlbumID: "100", AlbumTitle: "A", ArtistID: "10", ArtistName: "X"}},
		},
		lovedAlbumIDs:  []string{"100"},
		lovedArtistIDs: []string{"10"},
	}
	res, err := Run(context.Background(), gw, defaultOpts("yes\n", dir, "1"))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if res.AddedAlbums != 0 || res.AddedArtists != 0 {
		t.Errorf("added = %d/%d, want 0/0", res.AddedAlbums, res.AddedArtists)
	}
	if len(gw.addedAlbums) != 0 || len(gw.addedArtists) != 0 {
		t.Errorf("gateway calls happened: %v / %v", gw.addedAlbums, gw.addedArtists)
	}
}

func TestRun_dryRunWritesRecordButDoesNotApply(t *testing.T) {
	dir := tmpBackupDir(t)
	gw := &fakeGateway{
		playlistSongs: map[string][]gateway.PlaylistSong{
			"1": {{SongID: "s1", AlbumID: "100", AlbumTitle: "A", ArtistID: "10", ArtistName: "X"}},
		},
	}
	opts := defaultOpts("", dir, "1")
	opts.DryRun = true
	res, err := Run(context.Background(), gw, opts)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(gw.addedAlbums) != 0 || len(gw.addedArtists) != 0 {
		t.Errorf("dry-run made gateway add calls")
	}
	if res.RunRecordPath == "" {
		t.Error("RunRecordPath empty in dry-run")
	}
	if _, err := os.Stat(res.RunRecordPath); err != nil {
		t.Errorf("run record not written: %v", err)
	}
}

func TestRun_appliesAlbumsThenArtistsOnYes(t *testing.T) {
	dir := tmpBackupDir(t)
	gw := &fakeGateway{
		playlistSongs: map[string][]gateway.PlaylistSong{
			"1": {
				{SongID: "s1", AlbumID: "100", AlbumTitle: "A1", ArtistID: "10", ArtistName: "X"},
				{SongID: "s2", AlbumID: "101", AlbumTitle: "A2", ArtistID: "11", ArtistName: "Y"},
			},
		},
	}
	res, err := Run(context.Background(), gw, defaultOpts("yes\n", dir, "1"))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(gw.addedAlbums) != 2 {
		t.Errorf("addedAlbums = %v, want 2", gw.addedAlbums)
	}
	if len(gw.addedArtists) != 2 {
		t.Errorf("addedArtists = %v, want 2", gw.addedArtists)
	}
	if res.AddedAlbums != 2 || res.AddedArtists != 2 {
		t.Errorf("res counts = %d/%d", res.AddedAlbums, res.AddedArtists)
	}
}

func TestRun_abortsOnNonYes(t *testing.T) {
	dir := tmpBackupDir(t)
	gw := &fakeGateway{
		playlistSongs: map[string][]gateway.PlaylistSong{
			"1": {{SongID: "s1", AlbumID: "100", AlbumTitle: "A", ArtistID: "10", ArtistName: "X"}},
		},
	}
	_, err := Run(context.Background(), gw, defaultOpts("no\n", dir, "1"))
	if !errors.Is(err, ErrAborted) {
		t.Fatalf("err = %v, want ErrAborted", err)
	}
	if len(gw.addedAlbums) != 0 || len(gw.addedArtists) != 0 {
		t.Errorf("gateway add calls happened despite abort")
	}
}

func TestRun_perItemFailureGoesToSkipLog(t *testing.T) {
	dir := tmpBackupDir(t)
	gw := &fakeGateway{
		playlistSongs: map[string][]gateway.PlaylistSong{
			"1": {
				{SongID: "s1", AlbumID: "100", AlbumTitle: "A1", ArtistID: "10", ArtistName: "X"},
				{SongID: "s2", AlbumID: "101", AlbumTitle: "A2", ArtistID: "11", ArtistName: "Y"},
			},
		},
		addAlbumErrs: map[string]error{
			"101": &gateway.GatewayError{Kind: gateway.ErrNotFound, Method: "album.addFavorite", Message: "DATA_ERROR"},
		},
	}
	res, err := Run(context.Background(), gw, defaultOpts("yes\n", dir, "1"))
	if err == nil {
		t.Fatal("err = nil, want non-nil due to skip")
	}
	if res.AddedAlbums != 1 {
		t.Errorf("addedAlbums = %d, want 1", res.AddedAlbums)
	}
	if res.SkippedItems != 1 {
		t.Errorf("skipped = %d, want 1", res.SkippedItems)
	}
	if _, statErr := os.Stat(res.SkipLogPath); statErr != nil {
		t.Errorf("skip log not written: %v", statErr)
	}
}

func TestRun_authFailureDuringApplyAborts(t *testing.T) {
	dir := tmpBackupDir(t)
	gw := &fakeGateway{
		playlistSongs: map[string][]gateway.PlaylistSong{
			"1": {{SongID: "s1", AlbumID: "100", AlbumTitle: "A", ArtistID: "10", ArtistName: "X"}},
		},
		addAlbumErrs: map[string]error{
			"100": &gateway.GatewayError{Kind: gateway.ErrAuthFailed, Method: "album.addFavorite", Message: "USER_AUTH_REQUIRED"},
		},
	}
	_, err := Run(context.Background(), gw, defaultOpts("yes\n", dir, "1"))
	if err == nil || !strings.Contains(err.Error(), "auth failed") {
		t.Fatalf("err = %v, want auth-failed wrapped", err)
	}
}

func TestRun_breakerTripsAfterNConsecutiveFailures(t *testing.T) {
	dir := tmpBackupDir(t)
	transient := &gateway.GatewayError{Kind: gateway.ErrServerError, Method: "album.addFavorite", Message: "500"}
	gw := &fakeGateway{
		playlistSongs: map[string][]gateway.PlaylistSong{
			"1": {
				{SongID: "s1", AlbumID: "100", ArtistID: "10", ArtistName: "X"},
				{SongID: "s2", AlbumID: "101", ArtistID: "11", ArtistName: "Y"},
				{SongID: "s3", AlbumID: "102", ArtistID: "12", ArtistName: "Z"},
				{SongID: "s4", AlbumID: "103", ArtistID: "13", ArtistName: "W"},
			},
		},
		addAlbumErrs: map[string]error{
			"100": transient, "101": transient, "102": transient, "103": transient,
		},
	}
	opts := defaultOpts("yes\n", dir, "1")
	opts.MaxConsecutiveFinalFailures = 2
	_, err := Run(context.Background(), gw, opts)
	if err == nil || !strings.Contains(err.Error(), "consecutive") {
		t.Fatalf("err = %v, want breaker abort", err)
	}
	if len(gw.addedAlbums) != 0 {
		t.Errorf("addedAlbums = %v, want 0", gw.addedAlbums)
	}
}

func TestRun_partialPlaylistLoadPromptsAndProceedsOnYes(t *testing.T) {
	dir := tmpBackupDir(t)
	gw := &fakeGateway{
		playlistSongs: map[string][]gateway.PlaylistSong{
			"1": {{SongID: "s1", AlbumID: "100", AlbumTitle: "A", ArtistID: "10", ArtistName: "X"}},
		},
		playlistErrs: map[string]error{
			"2": &gateway.GatewayError{Kind: gateway.ErrNotFound, Method: "playlist.getSongs", Message: "DATA_ERROR"},
		},
	}
	// First "yes" answers the partial-input prompt; second "yes" answers the apply confirm.
	res, err := Run(context.Background(), gw, defaultOpts("yes\nyes\n", dir, "1", "2"))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if res.AddedAlbums != 1 || res.AddedArtists != 1 {
		t.Errorf("added = %d/%d", res.AddedAlbums, res.AddedArtists)
	}
}

func TestRun_partialPlaylistLoadAbortsOnNo(t *testing.T) {
	dir := tmpBackupDir(t)
	gw := &fakeGateway{
		playlistSongs: map[string][]gateway.PlaylistSong{
			"1": {{SongID: "s1", AlbumID: "100", AlbumTitle: "A", ArtistID: "10", ArtistName: "X"}},
		},
		playlistErrs: map[string]error{
			"2": &gateway.GatewayError{Kind: gateway.ErrNotFound, Method: "playlist.getSongs", Message: "DATA_ERROR"},
		},
	}
	_, err := Run(context.Background(), gw, defaultOpts("no\n", dir, "1", "2"))
	if !errors.Is(err, ErrAborted) {
		t.Fatalf("err = %v, want ErrAborted", err)
	}
}

func TestRun_runRecordContainsExpectedShape(t *testing.T) {
	dir := tmpBackupDir(t)
	gw := &fakeGateway{
		playlistSongs: map[string][]gateway.PlaylistSong{
			"1": {
				{SongID: "s1", AlbumID: "100", AlbumTitle: "A", ArtistID: "10", ArtistName: "X"},
				{SongID: "s2", AlbumID: "100", AlbumTitle: "A", ArtistID: "5080", ArtistName: "Various Artists"},
			},
		},
	}
	res, err := Run(context.Background(), gw, defaultOpts("yes\n", dir, "1"))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	body, err := os.ReadFile(res.RunRecordPath)
	if err != nil {
		t.Fatalf("read run record: %v", err)
	}
	s := string(body)
	for _, want := range []string{`"version": 1`, `"albums_to_add"`, `"artists_to_add"`, `"various_artists_skipped": 1`} {
		if !strings.Contains(s, want) {
			t.Errorf("run record missing %q", want)
		}
	}
	// Permission check: 0600
	info, _ := os.Stat(res.RunRecordPath)
	if info.Mode().Perm() != 0o600 {
		t.Errorf("perm = %v, want 0600", info.Mode().Perm())
	}
	// Lives under dir
	if filepath.Dir(res.RunRecordPath) != dir {
		t.Errorf("record at %s, expected under %s", res.RunRecordPath, dir)
	}
}
