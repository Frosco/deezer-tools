# Love Playlist Contents — Design

**Date:** 2026-04-29
**Status:** Approved (brainstorming → writing-plans handoff)
**Tool:** `deezer-tools playlists love-contents` (second command in the `deezer-tools` toolbox)

## Goal

Read N Deezer playlists (typical case 1–10 playlists, ~5,000 songs each, often
dumps of complete albums), compute which albums and artists referenced in
those playlists are not yet in Nils's loved-albums and loved-artists
collections, and — after confirmation — love them. The tool only ever **adds**;
it never removes loved items.

## Why this exists

Nils's library was reset by `loved-tracks wipe` on 2026-04-28: ~10k loved
tracks removed, 5,513 of them after Akamai briefly IP-blocked the user
(documented in `docs/solutions/integration-issues/`). Loved albums and loved
artists were intentionally untouched in that wipe. He now wants to expand
those collections to reflect the curation already encoded in his
album-dump playlists, without re-loving individual tracks. This tool is the
expand-side complement to the wipe.

It is the first command in the toolbox that performs *write* traffic against
the gateway since the Akamai incident, so it inherits the wipe's full throttle
discipline (paced sleeps, classified-error retry, consecutive-failure circuit
breaker) and triggers an extraction of that discipline into a shared package.

## Scope

**In scope (v1):**

- One Cobra subcommand: `deezer-tools playlists love-contents <inputs>...`.
- Three accepted input forms, normalized to a numeric playlist ID:
  - bare numeric ID (`15018766163`)
  - long URL containing `/playlist/<digits>` (`https://www.deezer.com/en/playlist/15018766163`)
  - short share link (`https://link.deezer.com/s/337D7rZEQd0wiR1D0ivjS`),
    resolved by following its HTTP redirect to the canonical URL.
- Positional args, with stdin fallback when *no* positional args are given
  (one input per line, blank lines and `#` comments ignored). If positional
  args are given, stdin is ignored — no merging. Confirm prompt reads from
  `/dev/tty` when the playlist list was sourced from stdin and stdin is not
  a tty.
- Adds *missing* loved albums and *missing* loved artists. Already-loved
  items are no-ops.
- Various-Artists is filtered at the artist level by `ART_ID`, with an
  observable count in the run summary. Falls back to `ART_NAME` match if the
  ID assumption turns out not to hold (verified at impl time).
- Dedupe by `ALB_ID` and `ART_ID` happens *before* any diff or any add call —
  this is the only "smart" step, and the load-bearing one for ~5k-song input.
- Same throttle discipline as wipe: 1s ± 200ms baseline pacer between every
  attempt, retry classifier with `5s/15s/30s/60s/120s` schedule on
  `ErrRateLimited` / `ErrServerError`, consecutive-final-failure circuit
  breaker default 5.
- Throttle logic is extracted from `internal/lovedtracks` into a new
  `internal/throttle` package; `internal/lovedtracks` is refactored to use
  it as part of this PR.
- One JSON run-record file per invocation (atomic write, `0600`). One
  per-item skip log mirroring wipe.
- `--dry-run`: resolve, fetch, dedupe, diff, write the run-record, skip
  confirm + apply.

**Out of scope (v1):**

- Standalone `loved-albums` / `loved-artists` subcommands. The gateway
  primitives exist after this PR; thin orchestrators can be added later.
- A "sync" mode that *removes* loved items not present in the playlists.
  Expand-only.
- Resume / replay-from-skip-log.
- Concurrency tuning. Sequential paced writes — the Akamai incident is the
  reason.
- A non-interactive `--yes` flag.
- Per-playlist breakdown in the diff summary (a single union diff is enough).

YAGNI: anything not strictly required to expand loved-albums / loved-artists
from the playlists is deferred.

## Architecture

### Repo layout (after this PR)

```
deezer-tools/
├── cmd/deezer-tools/
│   ├── main.go
│   ├── lovedtracks_cmd.go
│   └── playlistlove_cmd.go               # NEW: cobra wiring for the new tool
├── internal/
│   ├── config/config.go                  # unchanged
│   ├── gateway/
│   │   ├── client.go                     # unchanged
│   │   ├── csrf.go                       # unchanged
│   │   ├── errors.go                     # extended: IsRetryable helper +
│   │   │                                 #   any new classified kinds we discover
│   │   ├── tracks.go                     # unchanged
│   │   ├── playlists.go                  # NEW: read playlist contents
│   │   ├── albums.go                     # NEW: list / add favorite album
│   │   └── artists.go                    # NEW: list / add favorite artist
│   ├── throttle/                         # NEW: extracted from lovedtracks
│   │   └── throttle.go                   # Pace / Jitter vars, Sleep, RunOne,
│   │                                     #   DefaultRetryBackoff, DefaultMax...
│   ├── lovedtracks/wipe.go               # REFACTORED: uses internal/throttle.
│   │                                     #   Public API unchanged. Tests pass
│   │                                     #   unchanged — that's the gate.
│   └── playlistlove/                     # NEW: orchestration for this tool
│       ├── input.go                      #   parse + normalize playlist inputs
│       ├── diff.go                       #   dedupe + diff
│       └── run.go                        #   list → confirm → apply orchestration
└── docs/superpowers/specs/...
```

### Layering (strict, one-directional)

```
cmd/deezer-tools  → internal/lovedtracks     → internal/throttle
                                              → internal/gateway
                  → internal/playlistlove    → internal/throttle
                                              → internal/gateway
                  → internal/config
```

`internal/playlistlove` does **not** import `internal/lovedtracks`.
`internal/throttle` does **not** import `internal/gateway` — it stays
transport-agnostic by taking a `func(error) bool` retryable predicate from
its caller. The gateway provides that predicate (`gateway.IsRetryable`)
because it owns error-kind classification.

### `internal/throttle` public surface

```go
package throttle

// Pace and Jitter are package vars (not consts, not Options fields) so the
// test binary can zero them in init() the same way lovedtracks currently
// does. Production is intentionally untunable.
var (
    Pace   = time.Second
    Jitter = 200 * time.Millisecond
)

// DefaultRetryBackoff is the per-item retry schedule for retryable errors.
// 5s/15s/30s/60s/120s = ~232s of waiting before a single item is given up on.
var DefaultRetryBackoff = []time.Duration{
    5 * time.Second, 15 * time.Second, 30 * time.Second,
    60 * time.Second, 120 * time.Second,
}

const DefaultMaxConsecutiveFinalFailures = 5

// Sleep waits Pace ± Jitter, returning ctx.Err() on cancellation.
// Called before EVERY attempt, including the first.
func Sleep(ctx context.Context) error

// RunOne executes attempt with the retry schedule and the retryable
// predicate. Returns nil on success, the final error after all retries,
// or ctx.Err() on cancellation.
//
// schedule == nil uses DefaultRetryBackoff. schedule == empty slice means
// "no retries, single attempt". Matches lovedtracks Options sentinel.
func RunOne(
    ctx context.Context,
    attempt func(ctx context.Context) error,
    isRetryable func(error) bool,
    schedule []time.Duration,
) error
```

The **circuit breaker stays in the orchestrator's loop**, not in `throttle`.
Reason: the breaker semantics ("consecutive item-level failures across the
run") need the orchestrator's notion of an item, and crosses item boundaries.
Each domain keeps a small counter and aborts when the streak hits the
threshold:

```go
streak := 0
maxStreak := opts.MaxConsecutiveFinalFailures // 0 → 5, negative → disable
for _, item := range items {
    err := throttle.RunOne(ctx, func(ctx context.Context) error {
        return gw.AddFavoriteAlbum(ctx, item.ID)
    }, gateway.IsRetryable, opts.RetryBackoff)
    if err == nil { streak = 0; continue }
    if isAuth(err) { return err }       // bubble up, abort run
    skipLog.Append(item, err)
    streak++
    if maxStreak >= 0 && streak >= maxStreak { return ErrCircuitTripped }
}
```

### Refactor of `internal/lovedtracks`

Mechanical, behavior-preserving:

- `defaultPace`, `defaultPaceJitter`, `defaultRetryBackoff`,
  `defaultMaxConsecutiveFailure`, `pacedSleep`, `deleteWithRetry`
  → migrate to `internal/throttle` (or to `internal/gateway` for
  `IsRetryable`).
- The `Wipe` orchestration loop shrinks slightly to call
  `throttle.RunOne` + the breaker pattern above.
- `Options.RetryBackoff` (nil = default, empty = no-retry) and
  `Options.MaxConsecutiveFinalFailures` (0 = default, negative = disable)
  keep their current contracts.
- Test binary's pacer-zeroing `init()` moves to wherever the throttle vars
  live.
- **Public API of `internal/lovedtracks` is unchanged.** Existing
  `wipe_test.go` passes unchanged → that is the verification gate for the
  refactor.

### `internal/playlistlove` boundaries

Mirrors the `lovedtracks` shape:

- Depends on `internal/gateway` via a narrow `Gateway` interface defined
  inside `playlistlove` (not exported from gateway). Tests fake the
  transport without spinning up an HTTP server.
- Owns its own subcommand under `cmd/deezer-tools`.
- Does not cross-import sibling domain packages.
- The `Gateway` interface needs: read-playlist-songs, list-favorite-albums,
  add-favorite-album, list-favorite-artists, add-favorite-artist. Five
  methods, all concretely backed by `gateway.Client`.

## Inputs and resolution

Three accepted forms per input string, all normalized to a numeric
playlist ID before any gateway call:

| Form                                                  | Resolution                                                |
| ----------------------------------------------------- | --------------------------------------------------------- |
| Bare digits, e.g. `15018766163`                       | as-is                                                     |
| Long URL, e.g. `https://www.deezer.com/en/playlist/…` | regex extract `/playlist/(\d+)` (host on `*.deezer.com`)  |
| Short share link, `https://link.deezer.com/s/<token>` | HTTP `GET` with `CheckRedirect = http.ErrUseLastResponse`, read `Location` header, recurse |

Resolution lives in `internal/playlistlove/input.go`. The short-link
resolver makes one outbound HTTP call per share link; that's an explicit
network dependency, but the rest of the tool also requires network.

Inputs are deduped *by playlist ID* immediately after normalization, so the
same playlist passed twice (via different URL forms or via stdin + arg)
costs one playlist read.

Failures during input parsing or short-link resolution are collected; if any
inputs succeeded, the partial-input prompt below decides whether to proceed.
If *no* inputs succeeded, the tool exits non-zero before any gateway call.

## The `love-contents` flow

```
deezer-tools playlists love-contents [--dry-run] [--backup-dir <dir>] <inputs>...
```

When no positional args are given, inputs are read from stdin (one per line,
blank lines and `#` comments ignored). `--backup-dir` defaults to the
current working directory.

1. **Load config.** Verify `arl` is present and file perms are `0600`.
2. **Normalize inputs.** Parse each arg / stdin line into a numeric playlist
   ID (handling the three forms above). Dedupe by ID. Collect parse failures.
3. **Load each playlist.** For each unique ID, paginate `playlist.getSongs`,
   collecting `{SNG_ID, ALB_ID, ALB_TITLE, ART_ID, ART_NAME}` for each song.
   Per-playlist read goes through `throttle.RunOne` so a transient 5xx
   doesn't kill a playlist load. `ErrAuthFailed` aborts the whole run with
   the standard `arl`-refresh message.
4. **Partial-input prompt.** If any inputs failed (parse or load):
   - Print which inputs failed and why.
   - If at least one input succeeded, prompt: `Proceed with X of Y playlists? (yes/no)`.
   - "yes" continues with the partial set; anything else aborts non-zero.
   - If `--dry-run`, skip the prompt and continue silently with the partial
     set (the diff is read-only anyway).
5. **Aggregate + dedupe.**
   - Build `unique_albums` keyed by `ALB_ID` (carry first-seen
     `ALB_TITLE` and primary `ART_NAME` for display).
   - Build `unique_artists` keyed by `ART_ID` (carry first-seen
     `ART_NAME`).
   - Drop the Various-Artists pseudo-`ART_ID` from `unique_artists`.
   - Songs with empty / zero `ALB_ID` or `ART_ID` (regional removal,
     deleted track, missing metadata) drop out of dedupe and are counted
     under `unparseable_songs` — they don't fail the run.
6. **Read current loved sets.** Paginate `album.getFavoriteIds` and
   `artist.getFavoriteIds`. Both are read-only.
7. **Diff.**
   - `albums_to_add  = unique_albums  − loved_albums`
   - `artists_to_add = unique_artists − loved_artists`
8. **Write run-record.** Write
   `<backup-dir>/deezer-playlist-love-<RFC3339>.json` atomically
   (`.tmp` → `fsync` → `rename`, `0600`):

   ```json
   {
     "version": 1,
     "started_at": "2026-04-29T18:23:00Z",
     "source_playlists": [
       {"input": "https://link.deezer.com/s/...", "playlist_id": "15018766163",
        "title": "...", "song_count": 4892}
     ],
     "stats": {
       "songs_scanned": 4892,
       "playlists_loaded": 1,
       "playlists_failed": 0,
       "unique_albums": 487,
       "unique_artists": 152,
       "various_artists_skipped": 3,
       "unparseable_songs": 0,
       "albums_already_loved": 0,
       "artists_already_loved": 12,
       "albums_to_add": 487,
       "artists_to_add": 140
     },
     "albums_to_add":  [{"id": "...", "title": "...", "artist": "..."}],
     "artists_to_add": [{"id": "...", "name": "..."}]
   }
   ```

   `song_count` is `len(songs)` from the load step. `title` is best-effort:
   include it if `playlist.getSongs` returns playlist metadata inline (some
   gw-light variants do); otherwise omit it rather than make a separate
   metadata call. Plan must check at impl time.

9. **Empty-diff short-circuit.** If both `albums_to_add` and
   `artists_to_add` are empty, print `Nothing to add, your loved albums
   and artists already cover these playlists.` and exit `0`.
10. **Dry-run short-circuit.** If `--dry-run`, print
    `would add A albums and B artists, run-record at <path>` and exit `0`.
    Steps 11–13 are skipped.
11. **Confirmation gate** (interactive). Print summary + run-record path,
    prompt `Type yes to apply:`. Anything other than `yes` (case-insensitive,
    trimmed) aborts with `ErrAborted`. Read from `/dev/tty` if stdin was
    consumed by the playlist list (B1 path).
12. **Apply phase A — albums.** Sequential, one album at a time:
    `throttle.Sleep` then `favorite_album.add`, going through `throttle.RunOne`
    with `gateway.IsRetryable` and the configured backoff schedule.
    Per-item failure → append `{id, title, artist, error}` JSON line to
    `<backup-dir>/deezer-playlist-love-<RFC3339>.skip.log`. Auth failure →
    abort whole run. Circuit breaker as above. `ctx` is checked with an
    explicit `select` between every successful add (mirroring the wipe's
    invariant — without that check, a long happy-path apply would ignore
    SIGINT until the next failure).
13. **Apply phase B — artists.** Same shape with `favorite_artist.add`.
    Album phase A and artist phase B share one breaker counter, not two —
    a sustained backend problem doesn't get a second N-failure budget just
    because we crossed phases.
14. **Final summary.** `Added A albums + B artists, skipped S (see <skip-log>),
    elapsed T`. Exit `0` iff `S == 0`; non-zero otherwise.

## The "smart but simple" piece

The single load-bearing observation, given Nils's typical input shape (~5k
songs of complete albums, 1–10 playlists per run):

**Dedupe by `ALB_ID` and `ART_ID` happens before any diff or any add call.**

Order-of-magnitude napkin (not a promise): 5 playlists × 5k songs = 25k
songs scanned (cheap, single-digit gateway calls per playlist). After
dedupe maybe 2,500 unique albums + 400 unique artists. After diff against
existing loved sets, perhaps 700 albums + 100 artists actually need adding.
At 1.2s/call pacer, ~15 minutes of wall-clock writes.

No caching, no parallelism, no clever batching. The gateway does not support
batch-add for favorites (no OSS client uses one), and the wipe established
that single-item paced writes are the safe path through Akamai. Dedupe +
the wipe's pacer is the entire win.

## Error handling and exit codes

Most of the error story already exists in the project — the Akamai incident
wrote it down. The new tool inherits it.

**Inherited (no change):**

- All gateway calls go through `callWithCSRF`. `ErrCSRFExpired` is handled
  transparently with refresh-and-retry.
- Classified kinds: `ErrAuthFailed`, `ErrCSRFExpired`, `ErrRateLimited`,
  `ErrServerError`, `ErrNotFound`. `QUOTA_ERROR` (HTTP 200) and HTTP 429 →
  `ErrRateLimited`. `IsRetryable` returns true for `ErrRateLimited` /
  `ErrServerError`, false for everything else.
- `ErrAuthFailed` aborts the whole run with the
  `refresh arl in ~/.config/deezer-tools/config.toml` message.
- Per-item failures append to `*.skip.log` → non-zero exit.

**New things specific to this tool:**

- **Input-parsing failures** are reported per input and contribute to the
  partial-input prompt. They never silently skip.
- **Short-link redirect failures** (network error, no `Location`, redirect
  to an unexpected host) are treated as input failures for that one input.
- **Already-loved on add** (idempotency race or dedupe mismatch): if
  `favorite_album.add` / `favorite_artist.add` returns a recognizable
  "already in favorites" code at HTTP 200, classify as success. The exact
  code is unknown to this spec — implementation plan must verify against
  live behavior.
- **Loved-albums / loved-artists ceiling.** Whether a cap exists, and
  what error code it produces, is unknown. Plan must include a
  "discover ceiling behavior" task. Until classified:
  - If the ceiling errors as `ErrRateLimited`-shaped, the existing retry
    path burns its budget and skips — graceful, if noisy.
  - If it errors with an unclassified code, `IsRetryable` returns false,
    the item lands in skip.log with a meaningful message, and the run
    completes whatever it can before tripping the breaker.
  - If a stable ceiling-error shape is observed, add `ErrLimitReached`
    in `gateway/errors.go`, treat it as run-aborting in the orchestrator,
    and surface `Added X before hitting the limit` in the summary.

**Exit codes:**

- `0` — clean run (every item added, or empty diff, or successful dry-run).
- non-zero — anything skipped, partial-input refused, or run aborted by
  auth / circuit-breaker / ceiling. Specific exit-code values are not part
  of v1.

## Honest disclosures

- **Exact gw-light method names + parameter shapes are not asserted by this
  spec.** Likely candidates from prior reading are `playlist.getSongs`,
  `album.getFavoriteIds`, `favorite_album.add`, `artist.getFavoriteIds`,
  `favorite_artist.add` — parallel to the established `song.getFavoriteIds`
  and `favorite_song.remove`. The implementation plan must include a
  "verify exact gateway methods + sample wire shapes" task before any
  new gateway code is written, by reading deemix / deezer-py / d-fi-core
  source the same way the wipe was verified.
- **Various-Artists `ART_ID` is not asserted.** Deemix and friends treat
  it as a stable ID (commonly `5080`), but the plan must verify against
  live data and fall back to `ART_NAME` matching if the ID assumption
  fails.
- **Loved-albums / loved-artists ceiling behavior is not asserted.**
  Discovered and classified at impl time.
- **Idempotency response shape on `favorite_album.add` /
  `favorite_artist.add` is not asserted.** Discovered at impl time.

## Testing

CLAUDE.md constraints: no mocked-behavior tests, no e2e mocks, real data
and real APIs for end-to-end testing.

- **Unit tests for `internal/throttle`** — `Sleep` cancellation, `RunOne`
  covering success-first-attempt / retry-then-success / retry-budget-exhausted
  / non-retryable-immediate-return / ctx-cancel-mid-sleep. Table-driven.
- **Unit tests for the new `internal/gateway` methods** via `httptest.Server`,
  same shape as `tracks_test.go` today: request shape, pagination,
  `flexString` round-trips on any ID-shaped field gw-light flexes on,
  error-classification, idempotency-response shape (whichever we observe).
- **Unit tests for `internal/playlistlove`** with a fake gateway:
  - Input normalization (numeric, multiple long-URL shapes including
    `/en/playlist/`, trailing slash, query string; short share link via an
    injected redirect resolver).
  - Dedupe behavior on a 5k-songs-into-300-albums scenario, including
    overlap across multiple playlists.
  - Various-Artists `ART_ID` dropped from the artist set; surfaced count.
  - Songs with empty / zero `ALB_ID` / `ART_ID` counted, not added.
  - Diff with every combination of (some-already-loved, none-already-loved,
    all-already-loved) for albums and artists.
  - Confirm: `yes` continues; anything else aborts. `/dev/tty` fallback
    via a small interface so the path is testable without a real tty.
  - Apply: success / per-item retry-then-success / retry-budget-exhausted
    appends to skip log / auth-failure mid-apply aborts run / breaker
    trips after N consecutive final failures / ctx cancellation between
    items.
  - Partial-playlist-load: confirm-yes proceeds; confirm-no aborts.
  - Run-record: atomic write, `0600`, valid JSON, expected stats.
- **Existing `internal/lovedtracks` tests pass unchanged** — that is the
  refactor's verification gate.
- **Integration tests gated behind `DEEZER_INTEGRATION=1`**: read-only.
  Cover `playlist.getSongs` against a known-public playlist (ID chosen at
  impl time, documented), and `album.getFavoriteIds` /
  `artist.getFavoriteIds` against the live account. **No write methods
  are called by integration tests.**
- **No automated test of the destructive-by-mistake path.** Verified
  manually by `--dry-run` first against the real account, then a small
  one-playlist run, then the full run.

## Risks and known unknowns

- **Exact gateway method names & parameter shapes.** Verified against OSS
  libraries at implementation time. Plan must call out this verification
  step explicitly.
- **Loved-albums / loved-artists ceiling.** Existence and error shape both
  unknown.
- **Idempotency response on add.** Shape unknown.
- **Various-Artists `ART_ID`.** Stable in practice across OSS clients,
  unverified against this account.
- **`arl` cookie TTL.** Months in practice, no SLA. Auth-failure detection
  with the standard refresh message covers eventual expiry.
- **Deezer changing the gateway.** Inherent to the unofficial path.
  Accepted.

## Decisions log

For traceability of choices made during brainstorming:

- **Mode: report → confirm → add.** (Vs report-only, vs apply-by-default
  with `--dry-run` for report.) Matches the wipe's list → backup → confirm
  → delete shape; "expand my library" goal calls for actually applying.
- **Inputs: accept all three forms** (numeric, long URL, short share link).
  (Vs numeric-only, vs short-link-only.) Parsing cost is trivial; UX win
  of "paste whatever you have" is real.
- **Multi-playlist mechanic: positional args + stdin fallback, with
  `/dev/tty` for confirm.** (Vs positional-only, vs `--from-file` flag,
  vs requiring `--yes` when piping.) Stdin is ~5 lines of code; `/dev/tty`
  fallback avoids introducing a non-interactive flag.
- **Various-Artists: filter at the artist level by `ART_ID`.** (Vs name
  match, vs no special-casing, vs configurable skip list.) Right behavior
  in 99% of cases without a config knob; falls back to name-match if the
  ID assumption is wrong.
- **Confirm: type `yes`.** (Vs type-the-count, vs two confirms, vs no
  confirm.) Additive blast radius is lower than wipe's; matching the
  wipe's friction wasn't worth it. Run-record file gives a written record
  of intent regardless.
- **Architecture: single new domain package + extract throttle.**
  (Vs two domain packages, vs duplicate-don't-extract.) Two real callers
  of the throttle logic = enough to extract; the Akamai incident is the
  exact bug class that doesn't want to live in two places that can drift.
- **Circuit breaker stays in the orchestrator's loop.** (Vs hide it inside
  throttle.) Breaker spans item boundaries and needs the orchestrator's
  notion of "item"; hiding it would force a stateful object whose only
  consumer is the orchestrator.
- **Albums and artists share one breaker counter across both phases.**
  (Vs reset between phases.) A sustained backend problem doesn't get a
  fresh N-failure budget per phase.
- **Sequential paced writes; no concurrency, no batching.** Forced by the
  Akamai incident: gw-light's rate-limit window is short and shared, and
  no batch-add primitive exists.
