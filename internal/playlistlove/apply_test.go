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
)

func writeRecordFile(t *testing.T, dir string, body string) string {
	t.Helper()
	path := filepath.Join(dir, "deezer-playlist-love-20260507T120000Z.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write record: %v", err)
	}
	return path
}

func TestLoadRunRecord_happyPath(t *testing.T) {
	dir := t.TempDir()
	path := writeRecordFile(t, dir, `{
		"version": 1,
		"started_at": "2026-05-07T12:00:00Z",
		"source_playlists": [{"input": "1", "playlist_id": "1", "song_count": 2}],
		"stats": {"unique_albums": 2, "unique_artists": 2},
		"albums_to_add": [{"id": "100", "title": "A", "artist": "X"}],
		"artists_to_add": [{"id": "10", "name": "X"}]
	}`)

	rec, err := LoadRunRecord(path)
	if err != nil {
		t.Fatalf("LoadRunRecord: %v", err)
	}
	if rec.Version != 1 {
		t.Errorf("Version = %d, want 1", rec.Version)
	}
	if len(rec.AlbumsToAdd) != 1 || rec.AlbumsToAdd[0].ID != "100" {
		t.Errorf("AlbumsToAdd = %+v", rec.AlbumsToAdd)
	}
	if len(rec.ArtistsToAdd) != 1 || rec.ArtistsToAdd[0].ID != "10" {
		t.Errorf("ArtistsToAdd = %+v", rec.ArtistsToAdd)
	}
}

func TestLoadRunRecord_unsupportedVersion(t *testing.T) {
	dir := t.TempDir()
	path := writeRecordFile(t, dir, `{"version": 2, "albums_to_add": [], "artists_to_add": []}`)

	_, err := LoadRunRecord(path)
	if !errors.Is(err, ErrUnsupportedRecordVersion) {
		t.Fatalf("err = %v, want ErrUnsupportedRecordVersion", err)
	}
	if !strings.Contains(err.Error(), "version 2") {
		t.Errorf("error %q does not mention version 2", err.Error())
	}
}

func TestLoadRunRecord_malformedJSON(t *testing.T) {
	dir := t.TempDir()
	path := writeRecordFile(t, dir, `{ this is not json `)

	_, err := LoadRunRecord(path)
	if !errors.Is(err, ErrMalformedRecord) {
		t.Fatalf("err = %v, want ErrMalformedRecord", err)
	}
}

func TestLoadRunRecord_missingAlbumID(t *testing.T) {
	dir := t.TempDir()
	path := writeRecordFile(t, dir, `{
		"version": 1,
		"albums_to_add": [{"id": "100"}, {"id": ""}],
		"artists_to_add": []
	}`)

	_, err := LoadRunRecord(path)
	if !errors.Is(err, ErrMalformedRecord) {
		t.Fatalf("err = %v, want ErrMalformedRecord", err)
	}
	if !strings.Contains(err.Error(), "albums_to_add[1]") {
		t.Errorf("error %q does not mention albums_to_add[1]", err.Error())
	}
}

func TestLoadRunRecord_missingArtistID(t *testing.T) {
	dir := t.TempDir()
	path := writeRecordFile(t, dir, `{
		"version": 1,
		"albums_to_add": [],
		"artists_to_add": [{"id": "10"}, {"id": ""}]
	}`)

	_, err := LoadRunRecord(path)
	if !errors.Is(err, ErrMalformedRecord) {
		t.Fatalf("err = %v, want ErrMalformedRecord", err)
	}
	if !strings.Contains(err.Error(), "artists_to_add[1]") {
		t.Errorf("error %q does not mention artists_to_add[1]", err.Error())
	}
}

func TestLoadRunRecord_unknownTopLevelKeysIgnored(t *testing.T) {
	dir := t.TempDir()
	path := writeRecordFile(t, dir, `{
		"version": 1,
		"future_field": {"something": "new"},
		"albums_to_add": [{"id": "100", "title": "A", "artist": "X"}],
		"artists_to_add": [{"id": "10", "name": "X"}]
	}`)

	rec, err := LoadRunRecord(path)
	if err != nil {
		t.Fatalf("LoadRunRecord: %v", err)
	}
	if len(rec.AlbumsToAdd) != 1 {
		t.Errorf("AlbumsToAdd = %+v, want 1 entry", rec.AlbumsToAdd)
	}
}

func TestLoadRunRecord_bothArraysEmpty(t *testing.T) {
	dir := t.TempDir()
	path := writeRecordFile(t, dir, `{"version": 1, "albums_to_add": [], "artists_to_add": []}`)

	rec, err := LoadRunRecord(path)
	if err != nil {
		t.Fatalf("LoadRunRecord: %v", err)
	}
	if len(rec.AlbumsToAdd) != 0 || len(rec.ArtistsToAdd) != 0 {
		t.Errorf("expected empty slices, got %+v / %+v", rec.AlbumsToAdd, rec.ArtistsToAdd)
	}
}

func TestLoadRunRecord_bothArraysAbsent(t *testing.T) {
	dir := t.TempDir()
	path := writeRecordFile(t, dir, `{"version": 1}`)

	rec, err := LoadRunRecord(path)
	if err != nil {
		t.Fatalf("LoadRunRecord: %v", err)
	}
	if rec.AlbumsToAdd != nil || rec.ArtistsToAdd != nil {
		t.Errorf("expected nil slices, got %+v / %+v", rec.AlbumsToAdd, rec.ArtistsToAdd)
	}
}

func TestLoadRunRecord_missingFile(t *testing.T) {
	_, err := LoadRunRecord("/nonexistent/path/record.json")
	if err == nil || !os.IsNotExist(err) {
		t.Fatalf("err = %v, want os.IsNotExist", err)
	}
}

func defaultApplyOpts(stdin string, dir string, rec *RunRecord) ApplyOptions {
	return ApplyOptions{
		Record:       rec,
		BackupDir:    dir,
		Stdin:        strings.NewReader(stdin),
		Stdout:       &bytes.Buffer{},
		Stderr:       &bytes.Buffer{},
		RetryBackoff: []time.Duration{},
	}
}

func TestApplyFromRecord_nilRecord(t *testing.T) {
	opts := ApplyOptions{
		Stdin:  strings.NewReader(""),
		Stdout: &bytes.Buffer{},
		Stderr: &bytes.Buffer{},
	}
	_, err := ApplyFromRecord(context.Background(), &fakeGateway{}, opts)
	if err == nil {
		t.Fatal("err = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "Record must not be nil") {
		t.Errorf("err = %v, want message about nil Record", err)
	}
}

func TestApplyFromRecord_happyPath(t *testing.T) {
	dir := t.TempDir()
	rec := &RunRecord{
		Version:      1,
		AlbumsToAdd:  []RecordAlbum{{ID: "100", Title: "A"}, {ID: "101", Title: "B"}},
		ArtistsToAdd: []RecordArtist{{ID: "10", Name: "X"}},
	}
	gw := &fakeGateway{
		lovedAlbumIDs:  []string{},
		lovedArtistIDs: []string{},
	}
	res, err := ApplyFromRecord(context.Background(), gw, defaultApplyOpts("yes\n", dir, rec))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if res.AddedAlbums != 2 {
		t.Errorf("AddedAlbums = %d, want 2", res.AddedAlbums)
	}
	if res.AddedArtists != 1 {
		t.Errorf("AddedArtists = %d, want 1", res.AddedArtists)
	}
	if got := gw.addedAlbums; len(got) != 2 || got[0] != "100" || got[1] != "101" {
		t.Errorf("addedAlbums = %v", gw.addedAlbums)
	}
	if got := gw.addedArtists; len(got) != 1 || got[0] != "10" {
		t.Errorf("addedArtists = %v", gw.addedArtists)
	}
}

func TestApplyFromRecord_filtersAlreadyLoved(t *testing.T) {
	dir := t.TempDir()
	rec := &RunRecord{
		Version: 1,
		AlbumsToAdd: []RecordAlbum{
			{ID: "100", Title: "A"},
			{ID: "101", Title: "B"},
			{ID: "102", Title: "C"},
		},
		ArtistsToAdd: []RecordArtist{
			{ID: "10", Name: "X"},
			{ID: "11", Name: "Y"},
		},
	}
	gw := &fakeGateway{
		lovedAlbumIDs:  []string{"101"},
		lovedArtistIDs: []string{"11"},
	}
	opts := defaultApplyOpts("yes\n", dir, rec)
	res, err := ApplyFromRecord(context.Background(), gw, opts)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if res.AddedAlbums != 2 {
		t.Errorf("AddedAlbums = %d, want 2", res.AddedAlbums)
	}
	if res.AddedArtists != 1 {
		t.Errorf("AddedArtists = %d, want 1", res.AddedArtists)
	}
	if got := gw.addedAlbums; len(got) != 2 || got[0] != "100" || got[1] != "102" {
		t.Errorf("addedAlbums = %v, want [100 102]", gw.addedAlbums)
	}
	stderr := opts.Stderr.(*bytes.Buffer).String()
	if !strings.Contains(stderr, "2 items already loved") {
		t.Errorf("stderr %q does not contain '2 items already loved'", stderr)
	}
}

func TestApplyFromRecord_dedupesDuplicateIDs(t *testing.T) {
	dir := t.TempDir()
	rec := &RunRecord{
		Version: 1,
		AlbumsToAdd: []RecordAlbum{
			{ID: "100", Title: "A"},
			{ID: "100", Title: "A-dup"},
		},
		ArtistsToAdd: []RecordArtist{
			{ID: "10", Name: "X"},
			{ID: "10", Name: "X-dup"},
		},
	}
	gw := &fakeGateway{
		lovedAlbumIDs:  []string{},
		lovedArtistIDs: []string{},
	}
	opts := defaultApplyOpts("yes\n", dir, rec)
	res, err := ApplyFromRecord(context.Background(), gw, opts)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if res.AddedAlbums != 1 {
		t.Errorf("AddedAlbums = %d, want 1", res.AddedAlbums)
	}
	if res.AddedArtists != 1 {
		t.Errorf("AddedArtists = %d, want 1", res.AddedArtists)
	}
	stderr := opts.Stderr.(*bytes.Buffer).String()
	if !strings.Contains(stderr, "2 duplicate entries collapsed") {
		t.Errorf("stderr %q does not contain '2 duplicate entries collapsed'", stderr)
	}
}

func TestApplyFromRecord_emptyAfterFilter(t *testing.T) {
	dir := t.TempDir()
	rec := &RunRecord{
		Version:      1,
		AlbumsToAdd:  []RecordAlbum{{ID: "100", Title: "A"}},
		ArtistsToAdd: []RecordArtist{{ID: "10", Name: "X"}},
	}
	gw := &fakeGateway{
		lovedAlbumIDs:  []string{"100"},
		lovedArtistIDs: []string{"10"},
	}
	opts := defaultApplyOpts("", dir, rec)
	res, err := ApplyFromRecord(context.Background(), gw, opts)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	stdout := opts.Stdout.(*bytes.Buffer).String()
	if !strings.Contains(stdout, "all already loved") {
		t.Errorf("stdout %q does not contain 'all already loved'", stdout)
	}
	if res.SkipLogPath != "" {
		t.Errorf("SkipLogPath = %q, want empty", res.SkipLogPath)
	}
}

func TestApplyFromRecord_emptyRecord(t *testing.T) {
	dir := t.TempDir()
	rec := &RunRecord{
		Version:      1,
		AlbumsToAdd:  []RecordAlbum{},
		ArtistsToAdd: []RecordArtist{},
	}
	gw := &fakeGateway{}
	opts := defaultApplyOpts("", dir, rec)
	_, err := ApplyFromRecord(context.Background(), gw, opts)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	stdout := opts.Stdout.(*bytes.Buffer).String()
	if !strings.Contains(stdout, "record is empty") {
		t.Errorf("stdout %q does not contain 'record is empty'", stdout)
	}
	if len(gw.addedAlbums) != 0 || len(gw.addedArtists) != 0 {
		t.Errorf("gateway add calls happened despite empty record")
	}
}

func TestApplyFromRecord_confirmPromptAbortsOnNonYes(t *testing.T) {
	dir := t.TempDir()
	rec := &RunRecord{
		Version:      1,
		AlbumsToAdd:  []RecordAlbum{{ID: "100", Title: "A"}},
		ArtistsToAdd: []RecordArtist{},
	}
	gw := &fakeGateway{
		lovedAlbumIDs:  []string{},
		lovedArtistIDs: []string{},
	}
	_, err := ApplyFromRecord(context.Background(), gw, defaultApplyOpts("no\n", dir, rec))
	if !errors.Is(err, ErrAborted) {
		t.Fatalf("err = %v, want ErrAborted", err)
	}
	if len(gw.addedAlbums) != 0 {
		t.Errorf("addedAlbums = %v, want none", gw.addedAlbums)
	}
}

func TestApplyFromRecord_assumeYesSkipsPrompt(t *testing.T) {
	dir := t.TempDir()
	rec := &RunRecord{
		Version:      1,
		AlbumsToAdd:  []RecordAlbum{{ID: "100", Title: "A"}},
		ArtistsToAdd: []RecordArtist{},
	}
	gw := &fakeGateway{
		lovedAlbumIDs:  []string{},
		lovedArtistIDs: []string{},
	}
	opts := defaultApplyOpts("", dir, rec)
	opts.AssumeYes = true
	res, err := ApplyFromRecord(context.Background(), gw, opts)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if res.AddedAlbums != 1 {
		t.Errorf("AddedAlbums = %d, want 1", res.AddedAlbums)
	}
}

func TestApplyFromRecord_authFailureDuringApplyAborts(t *testing.T) {
	dir := t.TempDir()
	rec := &RunRecord{
		Version:     1,
		AlbumsToAdd: []RecordAlbum{{ID: "100"}},
	}
	gw := &fakeGateway{
		addAlbumErrs: map[string]error{
			"100": &gateway.GatewayError{Kind: gateway.ErrAuthFailed, Method: "album.addFavorite", Message: "USER_AUTH_REQUIRED"},
		},
	}
	_, err := ApplyFromRecord(context.Background(), gw, defaultApplyOpts("yes\n", dir, rec))
	if err == nil || !strings.Contains(err.Error(), "auth failed") {
		t.Fatalf("err = %v, want auth-failed wrapped", err)
	}
}

func TestApplyFromRecord_streakBreakerTrips(t *testing.T) {
	dir := t.TempDir()
	transient := &gateway.GatewayError{Kind: gateway.ErrServerError, Method: "album.addFavorite", Message: "500"}
	rec := &RunRecord{
		Version: 1,
		AlbumsToAdd: []RecordAlbum{
			{ID: "100"}, {ID: "101"}, {ID: "102"}, {ID: "103"},
		},
	}
	gw := &fakeGateway{
		addAlbumErrs: map[string]error{
			"100": transient, "101": transient, "102": transient, "103": transient,
		},
	}
	opts := defaultApplyOpts("yes\n", dir, rec)
	opts.MaxConsecutiveFinalFailures = 2

	_, err := ApplyFromRecord(context.Background(), gw, opts)
	if err == nil || !strings.Contains(err.Error(), "consecutive") {
		t.Fatalf("err = %v, want breaker abort", err)
	}
	if len(gw.addedAlbums) != 0 {
		t.Errorf("addedAlbums = %v, want 0", gw.addedAlbums)
	}
	if gw.addAlbumCalls != 2 {
		t.Errorf("addAlbumCalls = %d, want exactly 2 (breaker should abort after MaxConsecutiveFinalFailures)", gw.addAlbumCalls)
	}
}

func TestApplyFromRecord_contextCancellation(t *testing.T) {
	dir := t.TempDir()
	rec := &RunRecord{
		Version: 1,
		AlbumsToAdd: []RecordAlbum{
			{ID: "100"}, {ID: "101"},
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	gw := &cancellingGateway{
		fakeGateway: &fakeGateway{},
		cancel:      cancel,
	}
	_, err := ApplyFromRecord(ctx, gw, defaultApplyOpts("yes\n", dir, rec))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if len(gw.addedAlbums) != 1 {
		t.Errorf("addedAlbums = %v, want exactly 1 (the first one)", gw.addedAlbums)
	}
}

// cancellingGateway is a fakeGateway that cancels its supplied context on
// the first successful AddFavoriteAlbum call, so the next loop iteration
// observes ctx.Err().
type cancellingGateway struct {
	*fakeGateway
	cancel context.CancelFunc
	hit    bool
}

func (c *cancellingGateway) AddFavoriteAlbum(ctx context.Context, id string) error {
	if err := c.fakeGateway.AddFavoriteAlbum(ctx, id); err != nil {
		return err
	}
	if !c.hit {
		c.hit = true
		c.cancel()
	}
	return nil
}

func TestApplyFromRecord_skipLogPath(t *testing.T) {
	dir := t.TempDir()
	rec := &RunRecord{
		Version:     1,
		AlbumsToAdd: []RecordAlbum{{ID: "100"}, {ID: "101"}},
	}
	gw := &fakeGateway{
		addAlbumErrs: map[string]error{
			"101": &gateway.GatewayError{Kind: gateway.ErrNotFound, Method: "album.addFavorite", Message: "DATA_ERROR"},
		},
	}
	opts := defaultApplyOpts("yes\n", dir, rec)
	opts.RecordPath = filepath.Join(dir, "deezer-playlist-love-20260507T120000Z.json")

	res, err := ApplyFromRecord(context.Background(), gw, opts)
	if err == nil {
		t.Fatal("err = nil, want non-nil due to skip")
	}
	if res.SkipLogPath == "" {
		t.Fatal("SkipLogPath empty")
	}
	wantPrefix := filepath.Join(dir, "deezer-playlist-love-20260507T120000Z.applied-")
	if !strings.HasPrefix(res.SkipLogPath, wantPrefix) {
		t.Errorf("SkipLogPath = %q, want prefix %q", res.SkipLogPath, wantPrefix)
	}
	if !strings.HasSuffix(res.SkipLogPath, ".skip.log") {
		t.Errorf("SkipLogPath = %q, want .skip.log suffix", res.SkipLogPath)
	}
	if _, statErr := os.Stat(res.SkipLogPath); statErr != nil {
		t.Errorf("skip log not written: %v", statErr)
	}
}

func TestApplyFromRecord_noSecondRunRecord(t *testing.T) {
	dir := t.TempDir()
	rec := &RunRecord{
		Version:     1,
		AlbumsToAdd: []RecordAlbum{{ID: "100"}},
	}
	gw := &fakeGateway{}
	res, err := ApplyFromRecord(context.Background(), gw, defaultApplyOpts("yes\n", dir, rec))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if res.RunRecordPath != "" {
		t.Errorf("RunRecordPath = %q, want empty", res.RunRecordPath)
	}
	matches, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	if len(matches) != 0 {
		t.Errorf("backup dir contains json files after apply: %v", matches)
	}
}
