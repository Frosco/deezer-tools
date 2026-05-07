# Playlists `apply-record` subcommand — design

## Problem

`deezer-tools playlists love-contents` runs as a single pipeline:
load N playlists → within-playlist Case-1 dedup (throttled metadata fetches) →
list loved albums + artists → diff → write run record → confirm → apply.

Two limitations follow:

1. **No way to exclude items.** If the diff includes an artist or album the user
   does not want loved (e.g. a "Various Artists"-style compilation that slipped
   past the filter, or an artist they actively dislike), the only options are
   abort the run, manually edit the source playlists, and re-run.
2. **Re-running is expensive.** A second run repeats every step: re-paginating
   playlists, re-running the throttled within-playlist Case-1 metadata fetches,
   re-fetching loved sets. For large playlists this is minutes of wall time and
   hundreds of gateway calls.

Both problems collapse into one fix: let the user run `--dry-run`, edit the
resulting run-record JSON to remove items they don't want, and apply the
edited record directly. Editing the file *is* the exclusion mechanism.

## Goals

- Add a new subcommand that applies a previously-written run record without
  re-running the load/dedup/diff phases.
- Re-fetch loved sets immediately before apply, silently filtering items that
  are already loved (defends against between-runs drift and the unverified
  gateway idempotency on `AddFavoriteAlbum`).
- Reuse the existing apply-phase machinery (throttle, streak breaker, skip log,
  auth handling) — no drift from `love-contents`.

## Non-goals

- A separate `--exclude-artist` / `--exclude-album` / `--exclude-file` flag.
  The unified mechanism (edit the record file) replaces it.
- Changes to the run-record format. Today's `version: 1` already carries enough
  information.
- Symmetric `apply-record` for `lovedalbums dedupe`. The pattern is identical
  but the record shape differs; we leave the door open by keeping
  apply-from-record inside `internal/playlistlove`, and only add it for
  `lovedalbums` in a follow-up if it turns out to be useful.
- Live integration test against a real account. Apply paths require write
  access; the gateway-level integration test already covers auth + list-loved.

## CLI

```
deezer-tools playlists apply-record <FILE> [--yes] [--backup-dir DIR]
```

- `<FILE>` — path to a `deezer-playlist-love-<UTC>.json` produced by
  `love-contents` (or any file with the same shape, version 1).
- `--yes` — skip the confirm prompt (for scripted use).
- `--backup-dir` — directory for the skip log, defaults to `.`. Same convention
  as `love-contents`.

End-to-end workflow:

```
deezer-tools playlists love-contents --dry-run <inputs...>
# → writes deezer-playlist-love-<UTC>.json
$EDITOR deezer-playlist-love-<UTC>.json   # remove rows you don't want
deezer-tools playlists apply-record deezer-playlist-love-<UTC>.json
```

## Package layout

`internal/playlistlove` already owns the run-record format and the apply phase.
Reuse, don't fork.

### New file: `internal/playlistlove/apply.go`

```go
// LoadRunRecord reads and validates a record JSON file.
// Returns ErrUnsupportedRecordVersion or ErrMalformedRecord on failure.
func LoadRunRecord(path string) (*RunRecord, error)

// ApplyOptions configures one ApplyFromRecord run.
type ApplyOptions struct {
    Record                      *RunRecord
    BackupDir                   string
    AssumeYes                   bool
    Stdin                       io.Reader
    Stdout                      io.Writer
    Stderr                      io.Writer
    RetryBackoff                []time.Duration
    MaxConsecutiveFinalFailures int
}

// ApplyFromRecord applies a previously-computed plan, after re-fetching
// loved sets and silently filtering already-loved items.
func ApplyFromRecord(ctx context.Context, gw Gateway, opts ApplyOptions) (*Result, error)
```

`RunRecord` is the existing `runRecord` struct lifted to public, along with its
nested `RecordAlbum`, `RecordArtist`, `RecordPlaylist`, `RunRecordStats` types.
`Result` is the existing `Result` type, unchanged.

### Refactor of `Run` in `run.go`

The apply loop (current steps 11–13: open skip log, phase A albums, phase B
artists, final summary, skipped-items error) is extracted into a private
`applyPlan(...)` helper. Both `Run` and `ApplyFromRecord` call it. This is the
only refactor — no other behavior change to `Run`. The existing `run_test.go`
keeps passing without modification.

### Gateway interface

The `Gateway` interface in `run.go` is reused as-is. `ApplyFromRecord` only
needs `ListFavoriteAlbumIDs`, `ListFavoriteArtistIDs`, `AddFavoriteAlbum`,
`AddFavoriteArtist`, but narrowing the interface for one caller would be
premature. YAGNI.

### New CLI file: `cmd/deezer-tools/playlistlove_apply_cmd.go`

Wired into `newPlaylistsCmd()` next to `newLoveContentsCmd()`. Translates flags
into `ApplyOptions` and calls `ApplyFromRecord`.

## Data flow inside `ApplyFromRecord`

```
1. (Caller has already invoked LoadRunRecord; record is structurally valid.)
2. If record.AlbumsToAdd and record.ArtistsToAdd both empty
     → print "nothing to apply", success
3. List loved albums + loved artists (~2x deezer.pageProfile)
4. Filter:
     albums  := record.AlbumsToAdd  minus IDs in loved-albums set
     artists := record.ArtistsToAdd minus IDs in loved-artists set
   stderr: "X items already loved, skipping" (only when X > 0)
5. Dedupe within each filtered list (collapse duplicate IDs from hand edits).
   stderr: "Y duplicate entries collapsed" (only when Y > 0)
6. If both filtered lists empty
     → "nothing to apply (all already loved)", success
7. stdout: "Will love N albums and M artists from <record path>"
8. If !AssumeYes: confirm prompt; read "yes" from Stdin (matches isYes());
   anything else returns ErrAborted.
9. Open skip log at <BackupDir>/<record-basename>.applied-<UTC>.skip.log.
10. Apply phase A — albums (existing throttle.Sleep + RunOne + streak breaker)
11. Apply phase B — artists (same)
12. Final summary: added/skipped/elapsed; non-zero error if SkippedItems > 0.
```

### Skip log naming

`<record-basename>.applied-<UTC>.skip.log` in `--backup-dir`, where
`<record-basename>` is the input file's basename without the `.json` suffix and
`<UTC>` is the apply timestamp. The `.applied-<timestamp>` suffix:
- distinguishes from the original `love-contents` skip log (which is empty for
  a `--dry-run` record);
- lets the user re-apply the same record more than once without overwriting an
  earlier skip log;
- keeps the audit chain visible — record file, apply skip log, related by name.

### No second run record

The input file already *is* the record. `ApplyFromRecord` does not write a new
run-record file; doing so would duplicate the audit trail and introduce a
second source of truth.

### Auth and error classification

Reused exactly from `Run`. `errors.As(&gerr)` + `gerr.Kind == gateway.ErrAuthFailed`
on either the loved-set fetch or any apply call → fail fast with the existing
"refresh your arl in ~/.config/deezer-tools/config.toml" wrapper.

## Validation in `LoadRunRecord`

```go
type RunRecord struct {
    Version         int              `json:"version"`
    StartedAt       string           `json:"started_at"`
    SourcePlaylists []RecordPlaylist `json:"source_playlists"` // ignored on load
    Stats           RunRecordStats   `json:"stats"`            // ignored on load
    AlbumsToAdd     []RecordAlbum    `json:"albums_to_add"`
    ArtistsToAdd    []RecordArtist   `json:"artists_to_add"`
}
```

Decoder uses `json.Decoder` *without* `DisallowUnknownFields` — unknown
top-level keys are forward-compatible.

| Situation | Action |
|---|---|
| Schema `version` missing or != 1 | `ErrUnsupportedRecordVersion`, with the version we saw and the version we support |
| Top-level JSON malformed | `ErrMalformedRecord`, wrapping the parse error |
| Both `albums_to_add` and `artists_to_add` keys absent | `ErrMalformedRecord` |
| Both arrays present but empty | success — handled in `ApplyFromRecord` step 2 as "nothing to apply" |
| Album/artist entry missing `id` | `ErrMalformedRecord` with array name + index (`albums_to_add[3]: missing id`) |
| `id` not a non-empty string | `ErrMalformedRecord` |
| Duplicate IDs within an array | accept; deduped silently in `ApplyFromRecord` step 5 |
| Unknown top-level keys (`stats`, `source_playlists`, future fields) | ignore — forward-compat |
| `title` / `artist` / `name` missing or empty | accept — IDs are what matter, names are cosmetic |

`LoadRunRecord` is a pure structural check. Duplicate-ID dedupe is deferred to
the apply step so loading stays predictable and stateless.

## Error handling and exit codes

| Situation | CLI behavior |
|---|---|
| File doesn't exist / unreadable | exit non-zero, error to stderr |
| `ErrUnsupportedRecordVersion` | exit non-zero, "this build supports record version 1, file is version N" |
| `ErrMalformedRecord` | exit non-zero, location of the problem |
| `ErrAuthFailed` (during loved-set re-fetch or apply) | exit non-zero, "refresh your arl in ~/.config/deezer-tools/config.toml" |
| Streak circuit breaker trips | exit non-zero, points at skip log |
| Confirm prompt declined | exit non-zero with `ErrAborted` |
| `SkippedItems > 0` after happy-path | exit non-zero (matches `Run`'s convention — skip log present) |
| All adds succeeded, nothing skipped | exit 0 |

Context cancellation (Ctrl-C) propagates as in `Run`: checked between every
successful apply, also during throttle sleeps. Matches `Run` exactly because
both call the same `applyPlan` helper.

## Testing

In `internal/playlistlove/apply_test.go`, using the existing fake-`Gateway`
pattern (no real HTTP):

- `TestLoadRunRecord_happyPath` — round-trip a record produced by `Run --dry-run`, verify it parses
- `TestLoadRunRecord_unsupportedVersion` — `version: 2` returns `ErrUnsupportedRecordVersion`
- `TestLoadRunRecord_malformed` — bad JSON, missing `id`, non-string `id` each return `ErrMalformedRecord` with a useful location
- `TestApplyFromRecord_filtersAlreadyLoved` — record contains 5 albums; fake gateway reports 2 of those as already loved; expect 3 add calls, 0 skip-log entries, stderr mentions "2 items already loved"
- `TestApplyFromRecord_dedupesDuplicateIDs` — record has the same album ID twice (hand edit); expect a single add call
- `TestApplyFromRecord_emptyAfterFilter` — every item in record is already loved; expect "nothing to apply (all already loved)", exit 0, no skip log
- `TestApplyFromRecord_authFailureAborts` — fake gateway returns `ErrAuthFailed` on first add; expect immediate abort with the "refresh your arl" wrapper
- `TestApplyFromRecord_streakBreakerTrips` — N consecutive non-auth failures abort with the streak-breaker error; counter resets on success between
- `TestApplyFromRecord_confirmPromptHonored` — non-`yes` input returns `ErrAborted`, no add calls made
- `TestApplyFromRecord_assumeYesSkipsPrompt` — `AssumeYes: true`, empty stdin, applies normally
- `TestApplyFromRecord_contextCancellation` — ctx cancelled mid-apply, returns `ctx.Err()` between the next pair of adds
- `TestApplyFromRecord_skipLogPath` — skip log written to `<backup-dir>/<record-basename>.applied-<UTC>.skip.log`; populated only when items are actually skipped
- `TestApplyFromRecord_noNewRunRecord` — assert no second run-record file is written

The refactor of `Run`'s apply loop into `applyPlan` is pure code motion; the
existing `run_test.go` keeps passing without modification and serves as the
regression net for that change.

No live integration test. The existing `gateway` integration test already
covers the auth + list-loved API surface; adding an apply path would require
write access against a real account and is not worth the blast radius for a
CI-gated test.

## Open questions

None at design time. Implementation may surface edge cases in
`LoadRunRecord` error wrapping (e.g., desired exact wording for `ErrMalformedRecord`
location strings); those are decided during the plan.
