# Playlists apply-record Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `deezer-tools playlists apply-record <FILE>` subcommand that applies a previously-written run record without re-running the load/dedup/diff phases, using the run-record file as the source of truth (and the editable exclusion list).

**Architecture:** Lift the existing `runRecord` types in `internal/playlistlove` to public, extract the apply loop in `Run` into a private `applyPlan` helper, then add a new `apply.go` with `LoadRunRecord` (validates a record file) and `ApplyFromRecord` (re-fetches loved sets, filters already-loved + duplicate IDs, prompts, calls `applyPlan`). Wire a new Cobra subcommand under `playlists`.

**Tech Stack:** Go 1.22+, Cobra, existing `internal/gateway` + `internal/throttle` packages. No new dependencies.

**Spec:** `docs/superpowers/specs/2026-05-07-playlists-apply-record-design.md`.

**Branch:** `wip/playlists-apply-record` (already created).

---

## File map

| File | Action | Responsibility |
|---|---|---|
| `internal/playlistlove/run.go` | Modify | Public types (`RunRecord`, `RecordAlbum`, ...); private `applyPlan` helper; `Run` now calls `applyPlan` |
| `internal/playlistlove/apply.go` | Create | `LoadRunRecord`, `ErrUnsupportedRecordVersion`, `ErrMalformedRecord`, `ApplyOptions`, `ApplyFromRecord` |
| `internal/playlistlove/apply_test.go` | Create | Tests for load validation, filter/dedupe/empty-after-filter, confirm/`AssumeYes`, auth/streak/cancel, skip-log naming, no-second-record |
| `cmd/deezer-tools/playlistlove_apply_cmd.go` | Create | `newPlaylistsApplyRecordCmd()` Cobra wiring |
| `cmd/deezer-tools/playlistlove_cmd.go` | Modify | Wire new subcommand into `newPlaylistsCmd()` |
| `README.md` | Modify | Document the `playlists apply-record` subcommand |

---

## Task 1: Lift run-record types to public

Pure refactor. No behavior change. Existing `run_test.go` is the regression net (in particular `TestRun_runRecordContainsExpectedShape` covers JSON tag fidelity).

**Files:**
- Modify: `internal/playlistlove/run.go`

- [ ] **Step 1.1: Verify existing tests pass before any change**

Run: `go test ./internal/playlistlove/...`
Expected: PASS, all existing tests green.

- [ ] **Step 1.2: Rename private record types and helpers to public in `run.go`**

Apply these renames in `internal/playlistlove/run.go`:

| Old (private) | New (public) |
|---|---|
| `type runRecord struct` | `type RunRecord struct` |
| `type recordPlaylist struct` | `type RecordPlaylist struct` |
| `type runRecordStats struct` | `type RunRecordStats struct` |
| `type recordAlbum struct` | `type RecordAlbum struct` |
| `type recordArtist struct` | `type RecordArtist struct` |

Also update the field types where they reference the renamed names:

```go
// inside RunRecord:
SourcePlaylists []RecordPlaylist `json:"source_playlists"`
Stats           RunRecordStats   `json:"stats"`
AlbumsToAdd     []RecordAlbum    `json:"albums_to_add"`
ArtistsToAdd    []RecordArtist   `json:"artists_to_add"`
```

And inside `Run`, where the local `rec` is built:

```go
rec := RunRecord{
    Version:         1,
    StartedAt:       res.StartedAt.Format(time.RFC3339),
    SourcePlaylists: sourcePlaylists,
    Stats: RunRecordStats{
        // ... unchanged field assignments
    },
}
for _, a := range plan.AlbumsToAdd {
    rec.AlbumsToAdd = append(rec.AlbumsToAdd, RecordAlbum{ID: a.ID, Title: a.Title, Artist: a.Artist})
}
for _, a := range plan.ArtistsToAdd {
    rec.ArtistsToAdd = append(rec.ArtistsToAdd, RecordArtist{ID: a.ID, Name: a.Name})
}
```

Also update the `sourcePlaylists` local-variable type:

```go
var sourcePlaylists []RecordPlaylist
```

And the `writeRunRecord` signature:

```go
func writeRunRecord(dir string, started time.Time, rec RunRecord) (string, error)
```

JSON tags are unchanged. The file format on disk is identical.

- [ ] **Step 1.3: Verify `go build` passes**

Run: `go build ./...`
Expected: no output, exit 0.

- [ ] **Step 1.4: Verify all existing playlistlove tests still pass**

Run: `go test ./internal/playlistlove/... -count=1`
Expected: PASS — including `TestRun_runRecordContainsExpectedShape` (proves the JSON shape is unchanged).

- [ ] **Step 1.5: Verify whole-tree tests still pass**

Run: `go test ./... -count=1`
Expected: PASS.

- [ ] **Step 1.6: Commit**

```bash
git add internal/playlistlove/run.go
git commit -m "refactor(playlistlove): lift runRecord types to public"
```

---

## Task 2: Extract `applyPlan` helper

Pure refactor. Move the apply loop (existing steps 11–13 in `Run`) into a private helper that both `Run` and the upcoming `ApplyFromRecord` call. No behavior change. Existing tests are the regression net.

**Files:**
- Modify: `internal/playlistlove/run.go`

- [ ] **Step 2.1: Add the `applyPlan` helper near the bottom of `run.go`**

Add this function at the bottom of `run.go`, just before `isYes`:

```go
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
```

- [ ] **Step 2.2: Update `openSkipLog` signature to accept a basename instead of a record path**

Find:

```go
func openSkipLog(dir, recordPath string) (io.WriteCloser, string, error) {
	base := strings.TrimSuffix(filepath.Base(recordPath), ".json")
	skipPath := filepath.Join(dir, base+".skip.log")
```

Replace with:

```go
func openSkipLog(dir, baseName string) (io.WriteCloser, string, error) {
	skipPath := filepath.Join(dir, baseName+".skip.log")
```

The `.json` stripping is now caller responsibility — it lets `ApplyFromRecord` add a different suffix (`.applied-<UTC>`) without relying on the input filename ending in `.json`.

- [ ] **Step 2.3: Replace the apply loop in `Run` with a call to `applyPlan`**

Find the current section in `Run` that runs from "11. Open skip log." through "14. Final summary." (the comment markers) and the trailing `if res.SkippedItems > 0` return. Replace it with:

```go
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
```

Note: `Run` keeps printing its own "Added/skipped/elapsed" summary line (so the existing UX is unchanged); `applyPlan` is silent on stdout.

- [ ] **Step 2.4: Verify `go build` passes**

Run: `go build ./...`
Expected: no output, exit 0.

- [ ] **Step 2.5: Verify all existing playlistlove tests still pass**

Run: `go test ./internal/playlistlove/... -count=1 -v`
Expected: PASS — `TestRun_appliesAlbumsThenArtistsOnYes`, `TestRun_perItemFailureGoesToSkipLog`, `TestRun_authFailureDuringApplyAborts`, `TestRun_breakerTripsAfterNConsecutiveFailures` all green.

- [ ] **Step 2.6: Verify whole-tree tests still pass**

Run: `go test ./... -count=1`
Expected: PASS.

- [ ] **Step 2.7: Commit**

```bash
git add internal/playlistlove/run.go
git commit -m "refactor(playlistlove): extract applyPlan helper from Run"
```

---

## Task 3: `LoadRunRecord` with validation

TDD. Build the loader and validation table from the spec, one error case at a time.

**Files:**
- Create: `internal/playlistlove/apply.go`
- Create: `internal/playlistlove/apply_test.go`

- [ ] **Step 3.1: Write the failing happy-path test**

Create `internal/playlistlove/apply_test.go` with this content:

```go
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
```

- [ ] **Step 3.2: Run the test and verify it fails to compile**

Run: `go test ./internal/playlistlove/... -run TestLoadRunRecord_happyPath -v`
Expected: BUILD FAIL — `undefined: LoadRunRecord`.

- [ ] **Step 3.3: Create `apply.go` with `LoadRunRecord` skeleton**

Create `internal/playlistlove/apply.go` with this content:

```go
package playlistlove

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
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
	// Both arrays absent (not just empty) is a structural error.
	// json.Unmarshal sets nil for missing arrays, but we can't distinguish
	// missing-key from empty-array here. We treat "both nil/empty" as
	// load-time success and let ApplyFromRecord handle "nothing to apply".
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
```

- [ ] **Step 3.4: Run the happy-path test and verify it passes**

Run: `go test ./internal/playlistlove/... -run TestLoadRunRecord_happyPath -v`
Expected: PASS.

- [ ] **Step 3.5: Add the unsupported-version test**

Append to `apply_test.go`:

```go
func TestLoadRunRecord_unsupportedVersion(t *testing.T) {
	dir := t.TempDir()
	path := writeRecordFile(t, dir, `{
		"version": 2,
		"albums_to_add": [],
		"artists_to_add": []
	}`)

	_, err := LoadRunRecord(path)
	if !errors.Is(err, ErrUnsupportedRecordVersion) {
		t.Fatalf("err = %v, want ErrUnsupportedRecordVersion", err)
	}
	if !strings.Contains(err.Error(), "version 2") {
		t.Errorf("err message lacks version: %v", err)
	}
}
```

- [ ] **Step 3.6: Run the test and verify it passes**

Run: `go test ./internal/playlistlove/... -run TestLoadRunRecord_unsupportedVersion -v`
Expected: PASS.

- [ ] **Step 3.7: Add the malformed-JSON test**

Append to `apply_test.go`:

```go
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
		t.Errorf("err message lacks index: %v", err)
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
		t.Errorf("err message lacks index: %v", err)
	}
}

func TestLoadRunRecord_unknownTopLevelKeysIgnored(t *testing.T) {
	dir := t.TempDir()
	path := writeRecordFile(t, dir, `{
		"version": 1,
		"future_field": {"anything": [1, 2, 3]},
		"albums_to_add": [{"id": "100"}],
		"artists_to_add": []
	}`)

	rec, err := LoadRunRecord(path)
	if err != nil {
		t.Fatalf("LoadRunRecord: %v", err)
	}
	if len(rec.AlbumsToAdd) != 1 {
		t.Errorf("AlbumsToAdd = %+v", rec.AlbumsToAdd)
	}
}

func TestLoadRunRecord_missingFile(t *testing.T) {
	_, err := LoadRunRecord("/nonexistent/path/record.json")
	if err == nil {
		t.Fatal("err = nil, want non-nil")
	}
	if !os.IsNotExist(err) {
		t.Errorf("err = %v, want os.IsNotExist", err)
	}
}
```

- [ ] **Step 3.8: Run all LoadRunRecord tests and verify they pass**

Run: `go test ./internal/playlistlove/... -run TestLoadRunRecord -v`
Expected: PASS — all five additional tests green.

- [ ] **Step 3.9: Commit**

```bash
git add internal/playlistlove/apply.go internal/playlistlove/apply_test.go
git commit -m "feat(playlistlove): add LoadRunRecord with validation"
```

---

## Task 4: `ApplyFromRecord` — core flow

TDD. Build the apply-from-record entry point: filter already-loved, dedupe duplicate IDs, empty-after-filter short-circuit, confirm prompt, `AssumeYes`. Reuses `applyPlan` from Task 2.

**Files:**
- Modify: `internal/playlistlove/apply.go`
- Modify: `internal/playlistlove/apply_test.go`

- [ ] **Step 4.1: Write the failing happy-path apply test**

Append to `apply_test.go`:

```go
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
	if got := append([]string{}, gw.addedAlbums...); len(got) != 2 || got[0] != "100" || got[1] != "101" {
		t.Errorf("addedAlbums = %v", gw.addedAlbums)
	}
	if got := gw.addedArtists; len(got) != 1 || got[0] != "10" {
		t.Errorf("addedArtists = %v", gw.addedArtists)
	}
}
```

- [ ] **Step 4.2: Run the test and verify it fails to compile**

Run: `go test ./internal/playlistlove/... -run TestApplyFromRecord_happyPath -v`
Expected: BUILD FAIL — `undefined: ApplyFromRecord`, `undefined: ApplyOptions`.

- [ ] **Step 4.3: Replace the import block in `apply.go`**

The file currently has only the imports needed by `LoadRunRecord`. Expand the import block to:

```go
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
```

Auth-failure classification on the loved-set fetches is done by the CLI layer, exactly as it is for `Run` today (callers `errors.As` against `*gateway.GatewayError` on the returned error). `apply.go` itself does not import `internal/gateway` — that keeps this file's surface narrow.

- [ ] **Step 4.4: Append `ApplyOptions`, `ApplyFromRecord`, and helpers to `apply.go`**

Append the following block at the end of `internal/playlistlove/apply.go`:

```go
// ApplyOptions configures one ApplyFromRecord run.
//
// Sentinel handling matches Run:
//   - RetryBackoff: nil → throttle.DefaultRetryBackoff, empty → no retries.
//   - MaxConsecutiveFinalFailures: 0 → throttle default, negative → disable.
//
// Record is required. RecordPath is optional and used only to derive the
// skip-log basename and the "Will love ... from <path>" plan line.
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

// ApplyFromRecord applies a previously-computed plan from a run record.
//
// It re-fetches loved albums + loved artists right before apply, silently
// filters items that are already loved, collapses duplicate IDs introduced
// by hand edits, and (unless AssumeYes) prompts for "yes" before applying.
//
// The skip log is written to <BackupDir>/<base>.applied-<UTC>.skip.log
// where <base> is RecordPath's basename without its ".json" suffix. No
// new run-record file is written; the input file is the record.
//
// Result.RunRecordPath is left empty (no second record). PlaylistsLoaded /
// PlaylistsFailed are 0 (no playlists were loaded in this mode).
func ApplyFromRecord(ctx context.Context, gw Gateway, opts ApplyOptions) (*Result, error) {
	if opts.Record == nil {
		return nil, fmt.Errorf("ApplyFromRecord: Record is required")
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

	// 2. Both arrays empty → nothing to apply.
	if len(opts.Record.AlbumsToAdd) == 0 && len(opts.Record.ArtistsToAdd) == 0 {
		fmt.Fprintln(opts.Stdout, "Nothing to apply (record is empty).")
		res.Elapsed = time.Since(res.StartedAt)
		return res, nil
	}

	// 3. Re-fetch loved sets.
	lovedAlbums, err := gw.ListFavoriteAlbumIDs(ctx)
	if err != nil {
		return nil, fmt.Errorf("list loved albums: %w", err)
	}
	lovedArtists, err := gw.ListFavoriteArtistIDs(ctx)
	if err != nil {
		return nil, fmt.Errorf("list loved artists: %w", err)
	}

	// 4. Filter already-loved.
	albums, alreadyAlb := filterAlreadyLovedAlbums(opts.Record.AlbumsToAdd, lovedAlbums)
	artists, alreadyArt := filterAlreadyLovedArtists(opts.Record.ArtistsToAdd, lovedArtists)
	if alreadyAlb+alreadyArt > 0 {
		fmt.Fprintf(opts.Stderr, "%d items already loved, skipping\n", alreadyAlb+alreadyArt)
	}

	// 5. Dedupe duplicate IDs (hand-edit safety net).
	albums, dupAlb := dedupeAlbums(albums)
	artists, dupArt := dedupeArtists(artists)
	if dupAlb+dupArt > 0 {
		fmt.Fprintf(opts.Stderr, "%d duplicate entries collapsed\n", dupAlb+dupArt)
	}

	// 6. Empty after filter.
	if len(albums) == 0 && len(artists) == 0 {
		fmt.Fprintln(opts.Stdout, "Nothing to apply (all already loved).")
		res.Elapsed = time.Since(res.StartedAt)
		return res, nil
	}

	// 7. Print plan.
	src := opts.RecordPath
	if src == "" {
		src = "(in-memory record)"
	}
	fmt.Fprintf(opts.Stdout, "Will love %d albums and %d artists from %s\n", len(albums), len(artists), src)

	// 8. Confirm.
	if !opts.AssumeYes {
		fmt.Fprint(opts.Stdout, "Type yes to apply: ")
		ans, _ := bufio.NewReader(opts.Stdin).ReadString('\n')
		if !isYes(ans) {
			fmt.Fprintln(opts.Stdout, "Aborted.")
			res.Elapsed = time.Since(res.StartedAt)
			return res, ErrAborted
		}
	}

	// 9–12. Apply phase.
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

// filterAlreadyLovedAlbums returns (notLoved, removedCount).
func filterAlreadyLovedAlbums(in []RecordAlbum, loved []string) ([]RecordAlbum, int) {
	if len(in) == 0 {
		return in, 0
	}
	set := make(map[string]bool, len(loved))
	for _, id := range loved {
		set[id] = true
	}
	out := make([]RecordAlbum, 0, len(in))
	removed := 0
	for _, a := range in {
		if set[a.ID] {
			removed++
			continue
		}
		out = append(out, a)
	}
	return out, removed
}

// filterAlreadyLovedArtists returns (notLoved, removedCount).
func filterAlreadyLovedArtists(in []RecordArtist, loved []string) ([]RecordArtist, int) {
	if len(in) == 0 {
		return in, 0
	}
	set := make(map[string]bool, len(loved))
	for _, id := range loved {
		set[id] = true
	}
	out := make([]RecordArtist, 0, len(in))
	removed := 0
	for _, a := range in {
		if set[a.ID] {
			removed++
			continue
		}
		out = append(out, a)
	}
	return out, removed
}

// dedupeAlbums collapses duplicate IDs. Returns (deduped, droppedCount).
// First occurrence wins (preserves metadata from the order the user wrote
// in the file).
func dedupeAlbums(in []RecordAlbum) ([]RecordAlbum, int) {
	seen := make(map[string]bool, len(in))
	out := make([]RecordAlbum, 0, len(in))
	dropped := 0
	for _, a := range in {
		if seen[a.ID] {
			dropped++
			continue
		}
		seen[a.ID] = true
		out = append(out, a)
	}
	return out, dropped
}

// dedupeArtists is the artist-side mirror of dedupeAlbums.
func dedupeArtists(in []RecordArtist) ([]RecordArtist, int) {
	seen := make(map[string]bool, len(in))
	out := make([]RecordArtist, 0, len(in))
	dropped := 0
	for _, a := range in {
		if seen[a.ID] {
			dropped++
			continue
		}
		seen[a.ID] = true
		out = append(out, a)
	}
	return out, dropped
}

// skipLogBaseName returns the basename for the apply skip log:
// "<recordBase>.applied-<UTC>" where recordBase is the input path's
// basename without its ".json" suffix, or a fallback when no path is
// provided.
func skipLogBaseName(recordPath string, started time.Time) string {
	stamp := started.Format("20060102T150405Z")
	if recordPath == "" {
		return "deezer-playlist-love-replay.applied-" + stamp
	}
	base := strings.TrimSuffix(filepath.Base(recordPath), ".json")
	return base + ".applied-" + stamp
}
```

All listed imports are used by the combined file (`errors` by `LoadRunRecord`'s sentinels; the rest by `ApplyFromRecord` and helpers).

- [ ] **Step 4.5: Run the happy-path test and verify it passes**

Run: `go test ./internal/playlistlove/... -run TestApplyFromRecord_happyPath -v`
Expected: PASS.

- [ ] **Step 4.6: Add the filter-already-loved test**

Append to `apply_test.go`:

```go
func TestApplyFromRecord_filtersAlreadyLoved(t *testing.T) {
	dir := t.TempDir()
	rec := &RunRecord{
		Version: 1,
		AlbumsToAdd: []RecordAlbum{
			{ID: "100"}, {ID: "101"}, {ID: "102"},
		},
		ArtistsToAdd: []RecordArtist{
			{ID: "10"}, {ID: "11"},
		},
	}
	gw := &fakeGateway{
		lovedAlbumIDs:  []string{"101", "999"},
		lovedArtistIDs: []string{"11"},
	}
	stderr := &bytes.Buffer{}
	opts := defaultApplyOpts("yes\n", dir, rec)
	opts.Stderr = stderr

	res, err := ApplyFromRecord(context.Background(), gw, opts)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if res.AddedAlbums != 2 || res.AddedArtists != 1 {
		t.Errorf("added = %d/%d, want 2/1", res.AddedAlbums, res.AddedArtists)
	}
	if !strings.Contains(stderr.String(), "2 items already loved") {
		t.Errorf("stderr lacks already-loved notice: %q", stderr.String())
	}
	if got := gw.addedAlbums; len(got) != 2 || got[0] != "100" || got[1] != "102" {
		t.Errorf("addedAlbums = %v", got)
	}
}
```

- [ ] **Step 4.7: Run and verify**

Run: `go test ./internal/playlistlove/... -run TestApplyFromRecord_filtersAlreadyLoved -v`
Expected: PASS.

- [ ] **Step 4.8: Add the dedupe-duplicate-IDs test**

Append to `apply_test.go`:

```go
func TestApplyFromRecord_dedupesDuplicateIDs(t *testing.T) {
	dir := t.TempDir()
	rec := &RunRecord{
		Version: 1,
		AlbumsToAdd: []RecordAlbum{
			{ID: "100"}, {ID: "100"}, {ID: "101"},
		},
		ArtistsToAdd: []RecordArtist{
			{ID: "10"}, {ID: "10"},
		},
	}
	gw := &fakeGateway{}
	stderr := &bytes.Buffer{}
	opts := defaultApplyOpts("yes\n", dir, rec)
	opts.Stderr = stderr

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
	if !strings.Contains(stderr.String(), "2 duplicate entries collapsed") {
		t.Errorf("stderr lacks dedupe notice: %q", stderr.String())
	}
}
```

- [ ] **Step 4.9: Run and verify**

Run: `go test ./internal/playlistlove/... -run TestApplyFromRecord_dedupesDuplicateIDs -v`
Expected: PASS.

- [ ] **Step 4.10: Add the empty-after-filter and empty-record tests**

Append to `apply_test.go`:

```go
func TestApplyFromRecord_emptyAfterFilter(t *testing.T) {
	dir := t.TempDir()
	rec := &RunRecord{
		Version:      1,
		AlbumsToAdd:  []RecordAlbum{{ID: "100"}},
		ArtistsToAdd: []RecordArtist{{ID: "10"}},
	}
	gw := &fakeGateway{
		lovedAlbumIDs:  []string{"100"},
		lovedArtistIDs: []string{"10"},
	}
	stdout := &bytes.Buffer{}
	opts := defaultApplyOpts("", dir, rec)
	opts.Stdout = stdout

	res, err := ApplyFromRecord(context.Background(), gw, opts)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if res.AddedAlbums != 0 || res.AddedArtists != 0 {
		t.Errorf("added = %d/%d, want 0/0", res.AddedAlbums, res.AddedArtists)
	}
	if !strings.Contains(stdout.String(), "all already loved") {
		t.Errorf("stdout lacks short-circuit notice: %q", stdout.String())
	}
	// No skip log should be opened in the short-circuit path.
	if res.SkipLogPath != "" {
		t.Errorf("SkipLogPath = %q, want empty", res.SkipLogPath)
	}
}

func TestApplyFromRecord_emptyRecord(t *testing.T) {
	dir := t.TempDir()
	rec := &RunRecord{Version: 1}
	gw := &fakeGateway{}
	stdout := &bytes.Buffer{}
	opts := defaultApplyOpts("", dir, rec)
	opts.Stdout = stdout

	res, err := ApplyFromRecord(context.Background(), gw, opts)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(stdout.String(), "record is empty") {
		t.Errorf("stdout lacks empty-record notice: %q", stdout.String())
	}
	// Loved-set fetch should NOT happen for an empty record.
	if len(gw.addedAlbums) != 0 || len(gw.addedArtists) != 0 {
		t.Error("apply happened on empty record")
	}
	_ = res
}
```

- [ ] **Step 4.11: Run and verify**

Run: `go test ./internal/playlistlove/... -run "TestApplyFromRecord_empty" -v`
Expected: PASS — both tests.

- [ ] **Step 4.12: Add the confirm-prompt + AssumeYes tests**

Append to `apply_test.go`:

```go
func TestApplyFromRecord_confirmPromptAbortsOnNonYes(t *testing.T) {
	dir := t.TempDir()
	rec := &RunRecord{
		Version:     1,
		AlbumsToAdd: []RecordAlbum{{ID: "100"}},
	}
	gw := &fakeGateway{}
	res, err := ApplyFromRecord(context.Background(), gw, defaultApplyOpts("no\n", dir, rec))
	if !errors.Is(err, ErrAborted) {
		t.Fatalf("err = %v, want ErrAborted", err)
	}
	if res.AddedAlbums != 0 {
		t.Errorf("AddedAlbums = %d, want 0", res.AddedAlbums)
	}
	if len(gw.addedAlbums) != 0 {
		t.Error("gateway add happened despite abort")
	}
}

func TestApplyFromRecord_assumeYesSkipsPrompt(t *testing.T) {
	dir := t.TempDir()
	rec := &RunRecord{
		Version:     1,
		AlbumsToAdd: []RecordAlbum{{ID: "100"}},
	}
	gw := &fakeGateway{}
	opts := defaultApplyOpts("", dir, rec) // empty stdin
	opts.AssumeYes = true

	res, err := ApplyFromRecord(context.Background(), gw, opts)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if res.AddedAlbums != 1 {
		t.Errorf("AddedAlbums = %d, want 1", res.AddedAlbums)
	}
}
```

- [ ] **Step 4.13: Run and verify**

Run: `go test ./internal/playlistlove/... -run "TestApplyFromRecord_(confirm|assumeYes)" -v`
Expected: PASS — both tests.

- [ ] **Step 4.14: Verify whole-package tests still pass**

Run: `go test ./internal/playlistlove/... -count=1 -v`
Expected: PASS — all old `TestRun_*` tests AND all new `TestApplyFromRecord_*` and `TestLoadRunRecord_*` tests green.

- [ ] **Step 4.15: Commit**

```bash
git add internal/playlistlove/apply.go internal/playlistlove/apply_test.go
git commit -m "feat(playlistlove): add ApplyFromRecord with filter/dedupe/confirm"
```

---

## Task 5: `ApplyFromRecord` — failure modes and skip-log naming

TDD. Verify `ApplyFromRecord` inherits all the production-grade behavior from `applyPlan` (auth-failure abort, streak breaker, ctx cancel, skip-log writes), and that the skip log is named `<base>.applied-<UTC>.skip.log` and lives next to no second run-record file.

**Files:**
- Modify: `internal/playlistlove/apply_test.go`

- [ ] **Step 5.1: Add the auth-failure abort test**

Append to `apply_test.go`:

```go
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
```

- [ ] **Step 5.2: Run and verify**

Run: `go test ./internal/playlistlove/... -run TestApplyFromRecord_authFailureDuringApplyAborts -v`
Expected: PASS — `applyPlan` provides this behavior.

- [ ] **Step 5.3: Add the streak-breaker test**

Append to `apply_test.go`:

```go
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
}
```

- [ ] **Step 5.4: Run and verify**

Run: `go test ./internal/playlistlove/... -run TestApplyFromRecord_streakBreakerTrips -v`
Expected: PASS.

- [ ] **Step 5.5: Add the context-cancellation test**

Append to `apply_test.go`:

```go
func TestApplyFromRecord_contextCancellation(t *testing.T) {
	dir := t.TempDir()
	rec := &RunRecord{
		Version: 1,
		AlbumsToAdd: []RecordAlbum{
			{ID: "100"}, {ID: "101"},
		},
	}
	// Wrap AddFavoriteAlbum so the first add cancels the context, so the
	// second add observes the cancellation in the apply loop's select.
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
```

- [ ] **Step 5.6: Run and verify**

Run: `go test ./internal/playlistlove/... -run TestApplyFromRecord_contextCancellation -v`
Expected: PASS.

- [ ] **Step 5.7: Add the skip-log-path test**

Append to `apply_test.go`:

```go
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
```

- [ ] **Step 5.8: Run and verify**

Run: `go test ./internal/playlistlove/... -run TestApplyFromRecord_skipLogPath -v`
Expected: PASS.

- [ ] **Step 5.9: Add the no-second-run-record test**

Append to `apply_test.go`:

```go
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
	// Confirm: no *.json file appeared in the backup dir.
	matches, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	if len(matches) != 0 {
		t.Errorf("backup dir contains json files after apply: %v", matches)
	}
}
```

- [ ] **Step 5.10: Run and verify**

Run: `go test ./internal/playlistlove/... -run TestApplyFromRecord_noSecondRunRecord -v`
Expected: PASS.

- [ ] **Step 5.11: Verify whole-package tests pass**

Run: `go test ./internal/playlistlove/... -count=1 -v`
Expected: PASS — every test in the package.

- [ ] **Step 5.12: Commit**

```bash
git add internal/playlistlove/apply_test.go
git commit -m "test(playlistlove): cover apply-record failure modes and skip log"
```

---

## Task 6: CLI subcommand wiring

Add the `playlists apply-record` Cobra command and wire it under `playlists`.

**Files:**
- Create: `cmd/deezer-tools/playlistlove_apply_cmd.go`
- Modify: `cmd/deezer-tools/playlistlove_cmd.go`

- [ ] **Step 6.1: Create the new subcommand file**

Create `cmd/deezer-tools/playlistlove_apply_cmd.go`:

```go
package main

import (
	"errors"
	"fmt"

	"github.com/niref/deezer-tools/internal/config"
	"github.com/niref/deezer-tools/internal/gateway"
	"github.com/niref/deezer-tools/internal/playlistlove"
	"github.com/spf13/cobra"
)

func newPlaylistsApplyRecordCmd() *cobra.Command {
	var assumeYes bool
	var backupDir string

	cmd := &cobra.Command{
		Use:   "apply-record FILE",
		Short: "Apply a previously-written love-contents run record",
		Long: `Read a deezer-playlist-love-<UTC>.json record produced by
'playlists love-contents' (typically with --dry-run), re-fetch your loved
albums and loved artists, silently skip anything already loved, and love the
remainder.

The record file is the source of truth for what gets loved. Edit it
between the dry-run and this command to exclude items you don't want — the
exclusion list is just rows you remove from the file.

A skip log is written to <backup-dir>/<record-base>.applied-<UTC>.skip.log
when individual adds fail. No new run-record file is written.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			recordPath := args[0]
			rec, err := playlistlove.LoadRunRecord(recordPath)
			if err != nil {
				return fmt.Errorf("load record: %w", err)
			}

			cfgPath := defaultConfigPath()
			cfg, err := config.Load(cfgPath)
			if err != nil {
				return fmt.Errorf("config: %w", err)
			}
			client := gateway.New(cfg.ARL)

			_, err = playlistlove.ApplyFromRecord(cmd.Context(), client, playlistlove.ApplyOptions{
				Record:     rec,
				RecordPath: recordPath,
				BackupDir:  backupDir,
				AssumeYes:  assumeYes,
				Stdin:      cmd.InOrStdin(),
				Stdout:     cmd.OutOrStdout(),
				Stderr:     cmd.ErrOrStderr(),
			})
			if errors.Is(err, playlistlove.ErrAborted) {
				return err
			}
			// Auth-failure on the loved-set fetch arrives unwrapped from
			// ApplyFromRecord; surface it with the standard refresh-arl hint.
			var gerr *gateway.GatewayError
			if errors.As(err, &gerr) && gerr.Kind == gateway.ErrAuthFailed {
				return fmt.Errorf("auth failed (refresh your arl in ~/.config/deezer-tools/config.toml): %w", err)
			}
			return err
		},
	}

	cmd.Flags().BoolVar(&assumeYes, "yes", false, "skip the confirm prompt")
	cmd.Flags().StringVar(&backupDir, "backup-dir", ".", "directory for the apply skip log")
	return cmd
}
```

- [ ] **Step 6.2: Wire the new subcommand into `newPlaylistsCmd`**

In `cmd/deezer-tools/playlistlove_cmd.go`, find:

```go
func newPlaylistsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "playlists",
		Short: "Tools that take Deezer playlists as a source",
	}
	cmd.AddCommand(newLoveContentsCmd())
	return cmd
}
```

Replace with:

```go
func newPlaylistsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "playlists",
		Short: "Tools that take Deezer playlists as a source",
	}
	cmd.AddCommand(newLoveContentsCmd())
	cmd.AddCommand(newPlaylistsApplyRecordCmd())
	return cmd
}
```

- [ ] **Step 6.3: Verify `go build` passes**

Run: `go build ./...`
Expected: no output, exit 0.

- [ ] **Step 6.4: Verify the binary builds and the help text shows the new subcommand**

Run: `go build -o /tmp/deezer-tools ./cmd/deezer-tools && /tmp/deezer-tools playlists --help`
Expected output includes:

```
Available Commands:
  apply-record  Apply a previously-written love-contents run record
  love-contents For the given playlists, love every album and artist whose songs appear in them
```

- [ ] **Step 6.5: Verify the apply-record help text**

Run: `/tmp/deezer-tools playlists apply-record --help`
Expected output includes the long description and the `--yes` and `--backup-dir` flags.

- [ ] **Step 6.6: Verify whole-tree tests pass**

Run: `go test ./... -count=1`
Expected: PASS.

- [ ] **Step 6.7: Verify go vet**

Run: `go vet ./...`
Expected: no output, exit 0.

- [ ] **Step 6.8: Commit**

```bash
git add cmd/deezer-tools/playlistlove_apply_cmd.go cmd/deezer-tools/playlistlove_cmd.go
git commit -m "feat(cmd): wire playlists apply-record subcommand"
```

---

## Task 7: README and final verification

Document the new subcommand and the dry-run + edit + apply workflow, then run the full verification sweep.

**Files:**
- Modify: `README.md`

- [ ] **Step 7.1: Read the existing README around the `love-contents` section**

Run: `sed -n '40,90p' README.md`
Expected: shows the `playlists love-contents` section.

- [ ] **Step 7.2: Add an `apply-record` subsection after `love-contents`**

Insert after the `love-contents` section (right before the next top-level section, or at the end of the `Commands` chapter — whichever comes first) the following block:

```markdown
### `playlists apply-record`

Apply a previously-written love-contents run record. Use this together with
`love-contents --dry-run` to preview what would be loved, edit the record to
exclude items you don't want, and then apply the edited record without
re-running the load and dedup phases.

```sh
# 1. Dry-run writes the record file but does not apply.
deezer-tools playlists love-contents --dry-run --backup-dir ./backups <inputs>...

# 2. Edit the record to remove rows you don't want loved.
$EDITOR ./backups/deezer-playlist-love-20260507T120000Z.json

# 3. Apply the edited record. Re-fetches your loved sets and silently skips
#    anything already loved.
deezer-tools playlists apply-record ./backups/deezer-playlist-love-20260507T120000Z.json
```

Flags:

- `--yes` — skip the confirm prompt (useful for scripted runs).
- `--backup-dir <dir>` — directory for the apply skip log (defaults to `.`).

The skip log is written to `<backup-dir>/<record-base>.applied-<UTC>.skip.log`
when individual adds fail. No new run-record file is written.

If the record's `version` field doesn't match what this build supports,
the command refuses to run — this prevents silently applying a record from
a future format that may have different semantics.
```

- [ ] **Step 7.3: Verify the README renders sensibly**

Run: `sed -n '40,140p' README.md`
Expected: the `love-contents` and new `apply-record` sections both visible and well-formed.

- [ ] **Step 7.4: Final verification sweep**

Run all four checks in sequence:

```bash
go mod tidy
go build ./...
go vet ./...
go test ./... -count=1
```

Expected: all four exit 0, no test failures, no `go mod tidy` diff (run a `git status` after to confirm `go.mod`/`go.sum` are untouched).

- [ ] **Step 7.5: Sanity-check the binary end-to-end with an empty record**

Build and dry-test against a hand-crafted empty record, no network needed:

```bash
go build -o /tmp/deezer-tools ./cmd/deezer-tools
mkdir -p /tmp/dt-test
cat > /tmp/dt-test/empty.json <<'EOF'
{
  "version": 1,
  "started_at": "2026-05-07T12:00:00Z",
  "albums_to_add": [],
  "artists_to_add": []
}
EOF
/tmp/deezer-tools playlists apply-record /tmp/dt-test/empty.json --backup-dir /tmp/dt-test
```

Expected stdout includes "Nothing to apply (record is empty)." and exit 0.
(No real arl is needed for the empty path because the loved-set fetch is
short-circuited.)

If your config has a real `arl`, this also exercises that the binary loads
config without crashing on an empty record.

- [ ] **Step 7.6: Commit the README and ship**

```bash
git add README.md
git commit -m "docs(playlists): document apply-record subcommand"
```

- [ ] **Step 7.7: Verify the branch is clean and ready for review**

Run: `git status && git log --oneline wip/playlists-apply-record ^main`
Expected: working tree clean, log shows the spec commit (`docs(playlistlove): apply-record subcommand design`) and the seven implementation commits in order.

---

## Self-review summary

This plan has been internally checked against the spec:

- **Spec coverage:**
  - CLI shape (`apply-record FILE [--yes] [--backup-dir DIR]`) → Task 6
  - Package layout (apply.go new, run.go refactor, cmd file) → Tasks 1, 2, 3, 6
  - Data flow steps 1–12 → Task 4 (steps 1–8) + Task 5 (apply phase via `applyPlan`)
  - Skip-log naming (`<base>.applied-<UTC>.skip.log`) → Task 5.7
  - No second run record → Task 5.9
  - Auth handling reused from `Run` → Task 5.1 + CLI wrapper at Task 6.1
  - Validation table (every row) → Task 3.5–3.8
  - Error/exit-code table → covered transitively by tests in Tasks 4–5 and the CLI's `RunE` return paths
  - Test list (all 12 named tests in spec) → Tasks 3 and 4 and 5 cover all of them
  - "No live integration test" non-goal → respected (no integration_test.go change)

- **Type consistency:** `RunRecord`, `RecordAlbum`, `RecordArtist`, `RecordPlaylist`, `RunRecordStats` are introduced in Task 1 and used unchanged in Tasks 3–6; `applyPlan` and `applyPlanOpts` introduced in Task 2 are used by `ApplyFromRecord` in Task 4; `ApplyOptions` field names match between Task 4 implementation and Task 6 caller.

- **Placeholders:** none. The "remove the `gatewayErrAdapter` placeholder" instruction in Step 4.3 is intentional — it documents a dead-end that an inattentive engineer might leave in. Final state of `apply.go` per the corrected block in Step 4.3 has no placeholder.
