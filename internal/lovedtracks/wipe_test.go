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
	"time"

	"github.com/niref/deezer-tools/internal/gateway"
	"github.com/niref/deezer-tools/internal/throttle"
)

type fakeGateway struct {
	songs           []gateway.FavoriteSong
	listErr         error
	removeErrByID   map[string]error
	removeFn        func(ctx context.Context, id string) error
	removed         []string
	removeCallCount int
}

func (f *fakeGateway) ListFavoriteSongs(_ context.Context, _ int) ([]gateway.FavoriteSong, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.songs, nil
}

func (f *fakeGateway) RemoveFavoriteSong(ctx context.Context, id string) error {
	f.removeCallCount++
	if f.removeFn != nil {
		err := f.removeFn(ctx, id)
		if err == nil {
			f.removed = append(f.removed, id)
		}
		return err
	}
	if err, ok := f.removeErrByID[id]; ok {
		return err
	}
	f.removed = append(f.removed, id)
	return nil
}

// init zeroes the package-level pacer defaults so the test binary doesn't
// sleep ~1s before every delete. This file is only compiled into test
// binaries; production keeps the real defaults.
func init() {
	throttle.Pace = 0
	throttle.Jitter = 0
}

// fastTune disables retry and the circuit breaker so unit tests don't have
// to wait through backoffs or trip the streak counter accidentally. Pacing
// is already zero via init() above.
func fastTune(o Options) Options {
	o.RetryBackoff = []time.Duration{}
	o.MaxConsecutiveFinalFailures = -1
	return o
}

func quotaErr() error {
	return &gateway.GatewayError{
		Kind:    gateway.ErrRateLimited,
		Method:  "favorite_song.remove",
		Status:  200,
		Message: "QUOTA_ERROR: Quota exceeded on playlist delete songs",
	}
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

	res, err := Wipe(context.Background(), fg, fastTune(Options{
		BackupDir: dir, Stdout: out, Stderr: io.Discard, Stdin: strings.NewReader(""),
	}))
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

	res, err := Wipe(context.Background(), fg, fastTune(Options{
		DryRun: true, BackupDir: dir, Stdout: out, Stderr: io.Discard, Stdin: strings.NewReader(""),
	}))
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

	_, err := Wipe(context.Background(), fg, fastTune(Options{
		BackupDir: dir, Stdout: out, Stderr: io.Discard, Stdin: strings.NewReader("yes\n"),
	}))
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

	res, err := Wipe(context.Background(), fg, fastTune(Options{
		BackupDir: dir, Stdout: out, Stderr: io.Discard, Stdin: strings.NewReader("3\n"),
	}))
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

	res, err := Wipe(context.Background(), fg, fastTune(Options{
		BackupDir: dir, Stdout: out, Stderr: io.Discard, Stdin: strings.NewReader("3\n"),
	}))
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

	res, err := Wipe(context.Background(), fg, fastTune(Options{
		BackupDir: dir, Stdout: out, Stderr: io.Discard, Stdin: strings.NewReader("5\n"),
	}))
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

	_, err := Wipe(context.Background(), fg, fastTune(Options{
		BackupDir: dir, Stdout: io.Discard, Stderr: io.Discard, Stdin: strings.NewReader(""),
	}))
	if err == nil {
		t.Fatal("expected list error")
	}
	matches, _ := filepath.Glob(filepath.Join(dir, "deezer-loved-tracks-*.json"))
	if len(matches) != 0 {
		t.Errorf("no backup should be written on list failure; found: %v", matches)
	}
}

// QUOTA_ERROR is the gw-light protocol's own throttle signal. The 2026-04-28
// run treated it as a one-shot skip; this test pins the new behavior:
// transient quota → retry after backoff → success → no skip.
func TestWipe_QuotaErrorRetriesThenSucceeds(t *testing.T) {
	dir := t.TempDir()
	calls := map[string]int{}
	fg := &fakeGateway{
		songs: makeSongs(2),
		removeFn: func(_ context.Context, id string) error {
			calls[id]++
			if calls[id] == 1 {
				return quotaErr()
			}
			return nil
		},
	}
	out := &bytes.Buffer{}

	opts := fastTune(Options{
		BackupDir: dir, Stdout: out, Stderr: io.Discard, Stdin: strings.NewReader("2\n"),
	})
	opts.RetryBackoff = []time.Duration{time.Microsecond} // one retry, ~instant

	res, err := Wipe(context.Background(), fg, opts)
	if err != nil {
		t.Fatalf("Wipe: %v", err)
	}
	if res.DeletedCount != 2 || res.SkippedCount != 0 {
		t.Errorf("res = %+v", res)
	}
	if calls["1"] != 2 || calls["2"] != 2 {
		t.Errorf("expected each track to be tried twice, got %v", calls)
	}
}

func TestWipe_QuotaErrorRetriesExhaustedSkips(t *testing.T) {
	dir := t.TempDir()
	fg := &fakeGateway{
		songs:    makeSongs(1),
		removeFn: func(_ context.Context, _ string) error { return quotaErr() },
	}
	out := &bytes.Buffer{}

	opts := fastTune(Options{
		BackupDir: dir, Stdout: out, Stderr: io.Discard, Stdin: strings.NewReader("1\n"),
	})
	opts.RetryBackoff = []time.Duration{time.Microsecond, time.Microsecond}

	res, err := Wipe(context.Background(), fg, opts)
	if err == nil {
		t.Fatal("expected non-zero exit indication for skip")
	}
	if res.DeletedCount != 0 || res.SkippedCount != 1 {
		t.Errorf("res = %+v", res)
	}
	// initial attempt + 2 retries = 3 total
	if fg.removeCallCount != 3 {
		t.Errorf("removeCallCount = %d, want 3 (initial + 2 retries)", fg.removeCallCount)
	}
}

func TestWipe_CircuitBreakerAbortsOnConsecutiveFailures(t *testing.T) {
	dir := t.TempDir()
	fg := &fakeGateway{
		songs:    makeSongs(10),
		removeFn: func(_ context.Context, _ string) error { return quotaErr() },
	}
	out := &bytes.Buffer{}

	opts := fastTune(Options{
		BackupDir: dir, Stdout: out, Stderr: io.Discard, Stdin: strings.NewReader("10\n"),
	})
	opts.MaxConsecutiveFinalFailures = 3 // breaker after 3 failed-with-no-success-between

	res, err := Wipe(context.Background(), fg, opts)
	if err == nil {
		t.Fatal("expected breaker abort error")
	}
	if !strings.Contains(err.Error(), "consecutive") {
		t.Errorf("error message should mention consecutive failures, got %q", err.Error())
	}
	if res.SkippedCount != 3 {
		t.Errorf("SkippedCount = %d, want 3 (breaker stops at 3rd)", res.SkippedCount)
	}
	// once breaker tripped, no further deletes should be attempted
	// initial-attempt-only on 3 songs = 3 calls (RetryBackoff is empty in fastTune)
	if fg.removeCallCount != 3 {
		t.Errorf("removeCallCount = %d, want 3 (no calls past breaker trip)", fg.removeCallCount)
	}
}

func TestWipe_CircuitBreakerResetsOnSuccess(t *testing.T) {
	dir := t.TempDir()
	// fail, fail, succeed, fail, fail — streak should reset at the success,
	// so it never reaches 3 consecutive failures.
	fg := &fakeGateway{
		songs: makeSongs(5),
		removeFn: func(_ context.Context, id string) error {
			if id == "3" {
				return nil
			}
			return quotaErr()
		},
	}
	out := &bytes.Buffer{}

	opts := fastTune(Options{
		BackupDir: dir, Stdout: out, Stderr: io.Discard, Stdin: strings.NewReader("5\n"),
	})
	opts.MaxConsecutiveFinalFailures = 3

	res, err := Wipe(context.Background(), fg, opts)
	// Run completes (no breaker trip), but skips > 0 → non-nil error from Wipe.
	if err == nil {
		t.Fatal("expected non-zero exit for skips")
	}
	if strings.Contains(err.Error(), "consecutive") {
		t.Errorf("breaker should NOT have tripped: %v", err)
	}
	if res.DeletedCount != 1 || res.SkippedCount != 4 {
		t.Errorf("res = %+v, want Deleted=1 Skipped=4", res)
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
