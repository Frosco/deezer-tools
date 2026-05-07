package lovedalbums

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

func TestRun_apply_happyPath(t *testing.T) {
	tmp := t.TempDir()
	meta := map[string]gateway.AlbumMetadata{
		"1": {ID: "1", Title: "X", ArtistID: "a", TrackCount: 13, FanCount: 1000},
		"2": {ID: "2", Title: "x", ArtistID: "a", TrackCount: 13, FanCount: 5},
	}
	gw := newGW(meta, nil, []string{"1", "2"})
	out := &bytes.Buffer{}
	res, err := Run(context.Background(), gw, Options{
		BackupDir: tmp,
		Stdin:     strings.NewReader("yes\n"),
		Stdout:    out, Stderr: &bytes.Buffer{},
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if gw.removeCalls != 1 || gw.removed[0] != "2" {
		t.Errorf("removed = %v (calls=%d)", gw.removed, gw.removeCalls)
	}
	if res.AlbumsUnloved != 1 || res.AlbumsSkipped != 0 {
		t.Errorf("res = %+v", res)
	}
	if !strings.Contains(out.String(), "Type yes to apply") {
		t.Errorf("missing prompt in stdout: %q", out.String())
	}
}

func TestRun_apply_userDeclines_aborts(t *testing.T) {
	tmp := t.TempDir()
	meta := map[string]gateway.AlbumMetadata{
		"1": {ID: "1", Title: "X", ArtistID: "a", TrackCount: 13, FanCount: 1000},
		"2": {ID: "2", Title: "x", ArtistID: "a", TrackCount: 13, FanCount: 5},
	}
	gw := newGW(meta, nil, []string{"1", "2"})
	res, err := Run(context.Background(), gw, Options{
		BackupDir: tmp,
		Stdin:     strings.NewReader("no\n"),
		Stdout:    &bytes.Buffer{}, Stderr: &bytes.Buffer{},
	})
	if !errors.Is(err, ErrAborted) {
		t.Errorf("err = %v, want ErrAborted", err)
	}
	if gw.removeCalls != 0 {
		t.Errorf("removeCalls = %d, want 0", gw.removeCalls)
	}
	if res.AlbumsUnloved != 0 {
		t.Errorf("res.AlbumsUnloved = %d", res.AlbumsUnloved)
	}
}

func TestRun_apply_classifiedSkipAndContinue(t *testing.T) {
	tmp := t.TempDir()
	meta := map[string]gateway.AlbumMetadata{
		"w1": {ID: "w1", Title: "X", ArtistID: "a", TrackCount: 13, FanCount: 1000},
		"l1": {ID: "l1", Title: "x", ArtistID: "a", TrackCount: 13, FanCount: 5},
		"w2": {ID: "w2", Title: "Y", ArtistID: "a", TrackCount: 13, FanCount: 1000},
		"l2": {ID: "l2", Title: "y", ArtistID: "a", TrackCount: 13, FanCount: 5},
	}
	gw := newGW(meta, nil, []string{"w1", "l1", "w2", "l2"})
	gw.removeErr = map[string]error{
		"l1": &gateway.GatewayError{Kind: gateway.ErrNotFound, Message: "x"},
	}
	res, err := Run(context.Background(), gw, Options{
		BackupDir:    tmp,
		Stdin:        strings.NewReader("yes\n"),
		Stdout:       &bytes.Buffer{}, Stderr: &bytes.Buffer{},
		RetryBackoff: []time.Duration{},
	})
	if err == nil || !strings.Contains(err.Error(), "skipped") {
		t.Fatalf("err = %v, want non-nil with 'skipped'", err)
	}
	if res.AlbumsUnloved != 1 || res.AlbumsSkipped != 1 {
		t.Errorf("res = %+v", res)
	}
	b, _ := os.ReadFile(res.SkipLogPath)
	if !strings.Contains(string(b), `"id":"l1"`) {
		t.Errorf("skip log missing l1: %q", string(b))
	}
}

func TestRun_apply_authFailureAborts(t *testing.T) {
	tmp := t.TempDir()
	meta := map[string]gateway.AlbumMetadata{
		"w": {ID: "w", Title: "X", ArtistID: "a", TrackCount: 13, FanCount: 1000},
		"l": {ID: "l", Title: "x", ArtistID: "a", TrackCount: 13, FanCount: 5},
	}
	gw := newGW(meta, nil, []string{"w", "l"})
	gw.removeErr = map[string]error{
		"l": &gateway.GatewayError{Kind: gateway.ErrAuthFailed, Message: "x"},
	}
	_, err := Run(context.Background(), gw, Options{
		BackupDir:    tmp,
		Stdin:        strings.NewReader("yes\n"),
		Stdout:       &bytes.Buffer{}, Stderr: &bytes.Buffer{},
		RetryBackoff: []time.Duration{},
	})
	if err == nil || !strings.Contains(err.Error(), "config.toml") {
		t.Errorf("err = %v, want refresh-arl message", err)
	}
}

func TestRun_apply_circuitBreakerTripsOnStreak(t *testing.T) {
	tmp := t.TempDir()
	meta := map[string]gateway.AlbumMetadata{}
	ids := []string{}
	for i := 0; i < 6; i++ {
		w := fmt.Sprintf("w%d", i)
		l := fmt.Sprintf("l%d", i)
		title := fmt.Sprintf("T%d", i)
		meta[w] = gateway.AlbumMetadata{ID: w, Title: title, ArtistID: fmt.Sprintf("a%d", i), TrackCount: 13, FanCount: 1000}
		meta[l] = gateway.AlbumMetadata{ID: l, Title: title, ArtistID: fmt.Sprintf("a%d", i), TrackCount: 13, FanCount: 1}
		ids = append(ids, w, l)
	}
	gw := newGW(meta, nil, ids)
	gw.removeErr = map[string]error{}
	for i := 0; i < 6; i++ {
		gw.removeErr[fmt.Sprintf("l%d", i)] = &gateway.GatewayError{Kind: gateway.ErrNotFound, Message: "x"}
	}
	_, err := Run(context.Background(), gw, Options{
		BackupDir:                   tmp,
		Stdin:                       strings.NewReader("yes\n"),
		Stdout:                      &bytes.Buffer{}, Stderr: &bytes.Buffer{},
		RetryBackoff:                []time.Duration{},
		MaxConsecutiveFinalFailures: 5,
	})
	if err == nil || !strings.Contains(err.Error(), "consecutive") {
		t.Errorf("err = %v, want circuit-breaker message", err)
	}
	if gw.removeCalls != 5 {
		t.Errorf("removeCalls = %d, want 5 (breaker trips)", gw.removeCalls)
	}
}

type cancellingGW struct {
	*fullFakeGW
	after  int
	cancel context.CancelFunc
}

func (g *cancellingGW) RemoveFavoriteAlbum(ctx context.Context, id string) error {
	if err := g.fullFakeGW.RemoveFavoriteAlbum(ctx, id); err != nil {
		return err
	}
	if len(g.fullFakeGW.removed) >= g.after {
		g.cancel()
	}
	return nil
}

func TestRun_apply_ctxCancelBetweenUnloves(t *testing.T) {
	tmp := t.TempDir()
	meta := map[string]gateway.AlbumMetadata{}
	ids := []string{}
	for i := 0; i < 6; i++ {
		w := fmt.Sprintf("w%d", i)
		l := fmt.Sprintf("l%d", i)
		title := fmt.Sprintf("T%d", i)
		meta[w] = gateway.AlbumMetadata{ID: w, Title: title, ArtistID: fmt.Sprintf("a%d", i), TrackCount: 13, FanCount: 1000}
		meta[l] = gateway.AlbumMetadata{ID: l, Title: title, ArtistID: fmt.Sprintf("a%d", i), TrackCount: 13, FanCount: 1}
		ids = append(ids, w, l)
	}
	gw := newGW(meta, nil, ids)

	ctx, cancel := context.WithCancel(context.Background())
	gw2 := &cancellingGW{fullFakeGW: gw, after: 2, cancel: cancel}

	_, err := Run(ctx, gw2, Options{
		BackupDir: tmp,
		Stdin:     strings.NewReader("yes\n"),
		Stdout:    &bytes.Buffer{}, Stderr: &bytes.Buffer{},
		RetryBackoff: []time.Duration{},
	})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
	if gw.removed == nil || len(gw.removed) > 3 {
		t.Errorf("unloved = %v, want at most 3", gw.removed)
	}
}
