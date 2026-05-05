package lovedalbums

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/niref/deezer-tools/internal/gateway"
)

type fullFakeGW struct {
	*fakeGW
	listIDs     []string
	listErr     error
	removed     []string
	removeErr   map[string]error
	removeCalls int
}

func (f *fullFakeGW) ListFavoriteAlbumIDs(ctx context.Context) ([]string, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.listIDs, nil
}

func (f *fullFakeGW) RemoveFavoriteAlbum(ctx context.Context, id string) error {
	f.removeCalls++
	if err, ok := f.removeErr[id]; ok {
		return err
	}
	f.removed = append(f.removed, id)
	return nil
}

func newGW(meta map[string]gateway.AlbumMetadata, tracks map[string][]gateway.AlbumTrack, ids []string) *fullFakeGW {
	return &fullFakeGW{
		fakeGW:  &fakeGW{metaByID: meta, tracksByID: tracks},
		listIDs: ids,
	}
}

func TestRun_emptyLovedSet(t *testing.T) {
	tmp := t.TempDir()
	gw := newGW(nil, nil, nil)
	res, err := Run(context.Background(), gw, Options{
		BackupDir: tmp,
		Stdout:    &bytes.Buffer{},
		Stderr:    &bytes.Buffer{},
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if res.AlbumsToUnlove != 0 || res.AlbumsUnloved != 0 {
		t.Errorf("res = %+v", res)
	}
	if gw.metaCalls != 0 || gw.tracksCalls != 0 || gw.removeCalls != 0 {
		t.Errorf("calls: meta=%d tracks=%d remove=%d (want all 0)",
			gw.metaCalls, gw.tracksCalls, gw.removeCalls)
	}
}

func TestRun_dryRun_writesRecord_doesNotUnlove(t *testing.T) {
	tmp := t.TempDir()
	meta := map[string]gateway.AlbumMetadata{
		"1": {ID: "1", Title: "X", ArtistID: "a", TrackCount: 13, FanCount: 1000},
		"2": {ID: "2", Title: "x", ArtistID: "a", TrackCount: 13, FanCount: 5},
	}
	gw := newGW(meta, nil, []string{"1", "2"})
	out := &bytes.Buffer{}
	res, err := Run(context.Background(), gw, Options{
		DryRun: true, BackupDir: tmp, Stdout: out, Stderr: &bytes.Buffer{},
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if gw.removeCalls != 0 {
		t.Errorf("removeCalls = %d, want 0 (dry-run)", gw.removeCalls)
	}
	if res.AlbumsToUnlove != 1 || res.AlbumsUnloved != 0 {
		t.Errorf("res = %+v", res)
	}
	if !strings.Contains(out.String(), "would unlove") {
		t.Errorf("stdout = %q", out.String())
	}
	matches, _ := filepath.Glob(filepath.Join(tmp, "deezer-loved-albums-dedupe-*.json"))
	if len(matches) != 1 {
		t.Fatalf("matches = %v", matches)
	}
	b, _ := os.ReadFile(matches[0])
	var rec map[string]any
	if err := json.Unmarshal(b, &rec); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if rec["version"].(float64) != 1 {
		t.Errorf("version = %v", rec["version"])
	}
}

func TestRun_authFailureOnList_aborts(t *testing.T) {
	gw := newGW(nil, nil, nil)
	gw.listErr = &gateway.GatewayError{Kind: gateway.ErrAuthFailed, Message: "x"}
	_, err := Run(context.Background(), gw, Options{
		BackupDir: t.TempDir(), Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{},
	})
	if err == nil {
		t.Fatal("err = nil, want auth")
	}
	if !strings.Contains(err.Error(), "config.toml") {
		t.Errorf("err = %v, want refresh-arl message", err)
	}
}
