package playlistlove

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

func TestLoadRunRecord_missingFile(t *testing.T) {
	_, err := LoadRunRecord("/nonexistent/path/record.json")
	if err == nil || !os.IsNotExist(err) {
		t.Fatalf("err = %v, want os.IsNotExist", err)
	}
}
