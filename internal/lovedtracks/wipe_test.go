package lovedtracks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/niref/deezer-tools/internal/gateway"
)

type fakeGateway struct {
	songs            []gateway.FavoriteSong
	listErr          error
	removeErrByID    map[string]error
	removed          []string
	removeCallCount  int
}

func (f *fakeGateway) ListFavoriteSongs(_ context.Context, _ int) ([]gateway.FavoriteSong, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.songs, nil
}

func (f *fakeGateway) RemoveFavoriteSong(_ context.Context, id string) error {
	f.removeCallCount++
	if err, ok := f.removeErrByID[id]; ok {
		return err
	}
	f.removed = append(f.removed, id)
	return nil
}

func makeSongs(n int) []gateway.FavoriteSong {
	s := make([]gateway.FavoriteSong, n)
	for i := 0; i < n; i++ {
		s[i] = gateway.FavoriteSong{
			ID: itoa(i + 1), Title: "T" + itoa(i+1), Artist: "A", Album: "Alb", TimeAdd: int64(1700000000 + i),
		}
	}
	return s
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var buf [20]byte
	n := len(buf)
	for i > 0 {
		n--
		buf[n] = byte('0' + i%10)
		i /= 10
	}
	return string(buf[n:])
}

func TestWipe_EmptyAccount(t *testing.T) {
	dir := t.TempDir()
	fg := &fakeGateway{songs: nil}
	out := &bytes.Buffer{}

	res, err := Wipe(context.Background(), fg, Options{
		BackupDir: dir, Stdout: out, Stderr: io.Discard, Stdin: strings.NewReader(""),
	})
	if err != nil {
		t.Fatalf("Wipe: %v", err)
	}
	if res.ListedCount != 0 || res.DeletedCount != 0 {
		t.Errorf("counts wrong: %+v", res)
	}
	if !strings.Contains(out.String(), "No loved tracks to wipe") {
		t.Errorf("stdout = %q", out.String())
	}
	if fg.removeCallCount != 0 {
		t.Errorf("removeCallCount = %d, want 0", fg.removeCallCount)
	}
	matches, _ := filepath.Glob(filepath.Join(dir, "deezer-loved-tracks-*.json"))
	if len(matches) != 0 {
		t.Errorf("backup file should not be created for empty account, found: %v", matches)
	}
}

func TestWipe_DryRun(t *testing.T) {
	dir := t.TempDir()
	fg := &fakeGateway{songs: makeSongs(5)}
	out := &bytes.Buffer{}

	res, err := Wipe(context.Background(), fg, Options{
		DryRun: true, BackupDir: dir, Stdout: out, Stderr: io.Discard, Stdin: strings.NewReader(""),
	})
	if err != nil {
		t.Fatalf("Wipe: %v", err)
	}
	if res.ListedCount != 5 || res.DeletedCount != 0 {
		t.Errorf("counts: %+v", res)
	}
	if fg.removeCallCount != 0 {
		t.Errorf("dry-run should not delete; removeCallCount = %d", fg.removeCallCount)
	}
	if !strings.Contains(out.String(), "would delete 5") {
		t.Errorf("stdout = %q", out.String())
	}
	matches, _ := filepath.Glob(filepath.Join(dir, "deezer-loved-tracks-*.json"))
	if len(matches) != 1 {
		t.Fatalf("expected 1 backup file, found: %v", matches)
	}
	verifyBackupShape(t, matches[0], 5)
}

func TestWipe_ConfirmationWrongCountAborts(t *testing.T) {
	dir := t.TempDir()
	fg := &fakeGateway{songs: makeSongs(3)}
	out := &bytes.Buffer{}

	_, err := Wipe(context.Background(), fg, Options{
		BackupDir: dir, Stdout: out, Stderr: io.Discard, Stdin: strings.NewReader("yes\n"),
	})
	if err == nil || !errors.Is(err, ErrAborted) {
		t.Errorf("err = %v, want ErrAborted", err)
	}
	if fg.removeCallCount != 0 {
		t.Errorf("removeCallCount = %d, want 0", fg.removeCallCount)
	}
}

func TestWipe_HappyPath(t *testing.T) {
	dir := t.TempDir()
	fg := &fakeGateway{songs: makeSongs(3)}
	out := &bytes.Buffer{}

	res, err := Wipe(context.Background(), fg, Options{
		BackupDir: dir, Stdout: out, Stderr: io.Discard, Stdin: strings.NewReader("3\n"),
	})
	if err != nil {
		t.Fatalf("Wipe: %v", err)
	}
	if res.DeletedCount != 3 || res.SkippedCount != 0 {
		t.Errorf("res = %+v", res)
	}
	if got := fg.removed; len(got) != 3 || got[0] != "1" || got[2] != "3" {
		t.Errorf("removed = %v", got)
	}
}

func TestWipe_SkipOn4xxContinues(t *testing.T) {
	dir := t.TempDir()
	fg := &fakeGateway{
		songs: makeSongs(3),
		removeErrByID: map[string]error{
			"2": &gateway.GatewayError{Kind: gateway.ErrNotFound, Method: "favorite_song.remove", Status: 404, Message: "missing"},
		},
	}
	out := &bytes.Buffer{}

	res, err := Wipe(context.Background(), fg, Options{
		BackupDir: dir, Stdout: out, Stderr: io.Discard, Stdin: strings.NewReader("3\n"),
	})
	if err == nil {
		t.Fatal("expected non-zero exit indication")
	}
	if res.DeletedCount != 2 || res.SkippedCount != 1 {
		t.Errorf("res = %+v", res)
	}
	if res.SkipLogPath == "" {
		t.Fatal("SkipLogPath should be set when skips occur")
	}
	content, _ := os.ReadFile(res.SkipLogPath)
	if !strings.Contains(string(content), "\"id\":\"2\"") {
		t.Errorf("skip log missing track 2: %s", content)
	}
}

func TestWipe_AuthFailureAbortsImmediately(t *testing.T) {
	dir := t.TempDir()
	fg := &fakeGateway{
		songs: makeSongs(5),
		removeErrByID: map[string]error{
			"1": &gateway.GatewayError{Kind: gateway.ErrAuthFailed, Method: "favorite_song.remove", Message: "arl invalid"},
		},
	}
	out := &bytes.Buffer{}

	res, err := Wipe(context.Background(), fg, Options{
		BackupDir: dir, Stdout: out, Stderr: io.Discard, Stdin: strings.NewReader("5\n"),
	})
	if err == nil || !errors.Is(err, gateway.ErrAuthFailedSentinel) {
		t.Errorf("err = %v, want auth failure", err)
	}
	if res != nil && res.DeletedCount > 0 {
		t.Errorf("DeletedCount = %d; auth failure should abort before any delete succeeds", res.DeletedCount)
	}
}

func TestWipe_ListFailureMakesNoBackup(t *testing.T) {
	dir := t.TempDir()
	fg := &fakeGateway{listErr: errors.New("network")}

	_, err := Wipe(context.Background(), fg, Options{
		BackupDir: dir, Stdout: io.Discard, Stderr: io.Discard, Stdin: strings.NewReader(""),
	})
	if err == nil {
		t.Fatal("expected list error")
	}
	matches, _ := filepath.Glob(filepath.Join(dir, "deezer-loved-tracks-*.json"))
	if len(matches) != 0 {
		t.Errorf("no backup should be written on list failure; found: %v", matches)
	}
}

func verifyBackupShape(t *testing.T, path string, expectedCount int) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	var entries []gateway.FavoriteSong
	if err := json.Unmarshal(raw, &entries); err != nil {
		t.Fatalf("unmarshal backup: %v", err)
	}
	if len(entries) != expectedCount {
		t.Errorf("backup has %d entries, want %d", len(entries), expectedCount)
	}
}
