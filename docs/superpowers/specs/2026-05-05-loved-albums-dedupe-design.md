# Loved-Albums Dedupe — Design

**Date:** 2026-05-05
**Status:** Approved (brainstorming → writing-plans handoff)
**Tool:** `deezer-tools loved-albums dedupe` (third command in the toolbox)

## Goal

Find and remove duplicate entries in the loved-albums list, where "duplicate"
means one of two things:

1. **Case 1 — same album, different IDs.** Two or more loved albums by the
   same artist whose normalised titles match (different `ALB_ID`s). Pick a
   winner, un-love the rest.
2. **Case 2 — single masquerading as album.** A short loved album (≤3 tracks)
   whose title equals a track on a longer same-artist album that is **also
   loved**. Un-love the short one; the long one stays loved.

Run as a standalone command for cleaning up an existing messy loved-albums
state. Additionally, perform Case-1 dedup at ingest time inside `playlists
love-contents` so newly-loved albums don't reintroduce within-playlist
duplicates.

## Why this exists

The loved-albums list got noisy after running `playlists love-contents` on
large playlists imported from Spotify by a third-party transfer service. The
service produces two recurring forms of duplication:

- It sometimes picks different `ALB_ID`s for the same album across songs (or
  across runs), giving multiple loved entries for one album with slightly
  different IDs.
- It sometimes loves the *single* version of a track instead of the album it
  belongs to. Different title, different `ALB_ID`, but functionally redundant
  with an album that's also been loved from elsewhere in the playlist.

Both forms surface as visible clutter in the loved-albums list. The fix is a
small dedup pass that reuses the gateway primitives and orchestration patterns
already established by `lovedtracks` and `playlistlove`.

This is the first command in the toolbox that **removes** loved-albums entries,
so `album.deleteFavorite` enters the gateway. It also adds the first metadata
fetcher (`album.getData` / equivalent) — useful surface for any future tool
that wants album info beyond just the favorite-set IDs.

## Scope

**In scope (v1):**

- Standalone Cobra subcommand: `deezer-tools loved-albums dedupe [flags]`.
- Two duplicate cases as defined above; both detected in one run.
- Selection rule (Case 1): **most tracks > most fans > lowest ALB_ID** —
  strict, deterministic, no ties.
- Title normalisation: NFKD, strip combining marks, lowercase, collapse
  whitespace, drop non-alphanumeric (no edition-suffix stripping; deluxes,
  remasters, etc. stay distinct from the base title and are user-curated).
- Pure dedup: Case 2 only collapses when the longer same-artist album is
  *already loved*. The command never adds albums and never reduces library
  coverage.
- Single batch confirmation gate before any unlove (no per-album confirm).
- `--dry-run`: detect, write run-record, skip confirm + unlove.
- Atomic JSON run-record + per-album skip log, both at `0600`. New filename
  prefix: `deezer-loved-albums-dedupe-<UTC>.json` and `.skip.log`. The repo
  `.gitignore` is extended to cover both, mirroring the entries already
  present for the wipe and love-contents prefixes.
- Throttle / retry / circuit-breaker discipline inherited from
  `internal/throttle`, identical to `playlistlove` and refactored
  `lovedtracks`.
- Within-playlist Case-1 dedup added to `playlists love-contents`,
  using metadata calls scoped to actual conflict groups (typical run hits
  zero or a handful).

**Out of scope (v1):**

- Against-loved-albums Case-1 dedup *inside* `love-contents`. The standalone
  command, run after love-contents, picks those up across the union.
- Edition-suffix stripping (`(Deluxe Edition)`, `(Remastered)`,
  `[Bonus Tracks]`, etc.). Different normalised titles, treated as different
  albums.
- Replace-the-single-with-the-real-album when the real album isn't loved.
  Out of scope by design — pure dedup never expands coverage.
- Loved-tracks / loved-artists / loved-playlists dedup. Albums only.
- A `--prefer-search` flag that tiebreaks via Deezer's own search ranking.
  Adds API cost without clear win; revisit only if a wet run shows the
  fan-count-based picks are wrong.
- Manual triage UI for picking winners interactively. The selection rule is
  strict and deterministic; the run-record is the audit trail.
- A non-interactive `--yes` flag.
- Resume / replay-from-skip-log.

YAGNI: anything not strictly required to remove duplicate loved-album
entries given the two defined cases is deferred.

## Architecture

### Repo layout (after this PR)

```
deezer-tools/
├── cmd/deezer-tools/
│   ├── main.go
│   ├── lovedtracks_cmd.go
│   ├── playlistlove_cmd.go
│   └── lovedalbums_cmd.go                 # NEW: cobra wiring for dedupe
├── internal/
│   ├── config/                            # unchanged
│   ├── gateway/
│   │   ├── albums.go                      # EXTENDED: GetAlbumMetadata,
│   │   │                                  #   ListAlbumTracks,
│   │   │                                  #   RemoveFavoriteAlbum
│   │   ├── albums_test.go                 # extended for the three new methods
│   │   ├── errors.go                      # unchanged unless wet run surfaces
│   │   │                                  #   a new classifiable kind
│   │   └── ... (other files unchanged)
│   ├── throttle/                          # unchanged
│   ├── lovedtracks/                       # unchanged
│   ├── playlistlove/
│   │   ├── diff.go                        # EXTENDED: within-playlist Case-1
│   │   │                                  #   pass after ALB_ID dedupe
│   │   ├── run.go                         # EXTENDED: surfaces new stats
│   │   └── ...
│   └── lovedalbums/                       # NEW
│       ├── match.go                       #   normalise, group, detect Case 1/2
│       ├── match_test.go
│       ├── plan.go                        #   PickWinner, build DedupePlan
│       ├── plan_test.go
│       ├── fetch.go                       #   phase-1 metadata, phase-2
│       │                                  #     tracklist, paced + classified
│       ├── fetch_test.go
│       ├── dedupe.go                      #   Run() orchestrator
│       └── dedupe_test.go
└── docs/superpowers/specs/...
```

### Layering (strict, one-directional)

```
cmd/deezer-tools  → internal/lovedtracks   → internal/throttle
                                            → internal/gateway
                  → internal/playlistlove  → internal/throttle
                                            → internal/gateway
                                            → internal/lovedalbums (Normalise + PickWinner only)
                  → internal/lovedalbums   → internal/throttle
                                            → internal/gateway
                  → internal/config
```

`internal/lovedalbums` exposes a small API to `playlistlove` consisting only
of normalisation + matching helpers (no orchestrator, no gateway IO).
`playlistlove` does its own metadata calls and its own diff. The package
boundary is "lovedalbums owns the rules for what counts as a duplicate;
callers do their own IO".

### `internal/gateway` extensions

Three new methods on `gateway.Client`, all going through `callWithCSRF` and
inheriting CSRF refresh + error classification:

```go
// GetAlbumMetadata returns lightweight metadata for one album.
// Backed by album.getData (verified against deemix/deezer-py at impl time).
//
// All ID-shaped fields use flexString — the gw-light protocol returns IDs
// in mixed quoted/numeric forms within a single response payload, see
// docs/solutions/design-patterns/gw-light-go-adapter-quirks-2026-04-28.md.
func (c *Client) GetAlbumMetadata(ctx context.Context, albumID string) (AlbumMetadata, error)

type AlbumMetadata struct {
    ID         string
    Title      string
    ArtistID   string
    ArtistName string
    FanCount   int
    TrackCount int
}

// ListAlbumTracks returns one album's track list.
// Backed by album.getSongs (verified at impl time; alternative names
// observed in OSS clients include song.getListByAlbum).
func (c *Client) ListAlbumTracks(ctx context.Context, albumID string) ([]AlbumTrack, error)

type AlbumTrack struct {
    ID    string
    Title string
}

// RemoveFavoriteAlbum un-loves the album. Backed by album.deleteFavorite,
// symmetric with the existing AddFavoriteAlbum (album.addFavorite).
func (c *Client) RemoveFavoriteAlbum(ctx context.Context, albumID string) error
```

`flexString` is used for `ID`, `ArtistID`, `FanCount`, `TrackCount` (any
field that may be returned as either a quoted string or a bare number;
exact set verified at impl time against real responses).

### `internal/lovedalbums` public surface

```go
package lovedalbums

// Run executes the full dedupe flow against gw.
func Run(ctx context.Context, gw Gateway, opts Options) (*Result, error)

// Gateway is the slice of internal/gateway.Client used by Run. Defined here
// (not in internal/gateway) to keep the dependency narrow and let tests fake
// the transport without spinning up an HTTP server.
type Gateway interface {
    ListFavoriteAlbumIDs(ctx context.Context) ([]string, error)
    GetAlbumMetadata(ctx context.Context, albumID string) (gateway.AlbumMetadata, error)
    ListAlbumTracks(ctx context.Context, albumID string) ([]gateway.AlbumTrack, error)
    RemoveFavoriteAlbum(ctx context.Context, albumID string) error
}

type Options struct {
    DryRun                      bool
    BackupDir                   string
    Stdin                       io.Reader
    Stdout                      io.Writer
    Stderr                      io.Writer
    Case2TrackThreshold         int           // 0 → default 3
    RetryBackoff                []time.Duration
    MaxConsecutiveFinalFailures int
    OpenTTY                     func() (io.ReadCloser, error)
}

type Result struct {
    StartedAt        time.Time
    RunRecordPath    string
    SkipLogPath      string
    Case1Groups      int
    Case2Groups      int
    AlbumsToUnlove   int
    AlbumsUnloved    int
    AlbumsSkipped    int
    Phase1Calls      int
    Phase2Calls      int
    Elapsed          time.Duration
}

// Helpers exported for use by playlistlove's within-playlist Case-1 pass:
//
// Normalise applies the spec's title-normalisation rules: NFKD, strip
// combining marks, lowercase, collapse whitespace, drop non-alphanumeric.
func Normalise(title string) string

// PickWinner chooses the canonical album from a group of Case-1 candidates
// using the strict ordering: most tracks → most fans → lowest ALB_ID.
// The first element of the returned slice is the winner; the rest are losers.
func PickWinner(group []gateway.AlbumMetadata) []gateway.AlbumMetadata
```

The orchestrator (`Run`) is **not** exposed to `playlistlove`. Only `Normalise`
and `PickWinner` cross the package boundary.

## The dedupe flow

```
deezer-tools loved-albums dedupe [--dry-run] [--backup-dir <dir>]
                                 [--case2-track-threshold N] (default 3)
```

1. **Load config.** Verify `arl` is present and file perms are `0600`.
2. **List loved album IDs.** Single `deezer.pageProfile` call via
   `ListFavoriteAlbumIDs`. Read-only. `ErrAuthFailed` aborts with the
   standard `arl`-refresh message.
3. **Phase 1 — fetch metadata for every loved album.** One
   `GetAlbumMetadata` call per ID, each wrapped with `throttle.Sleep` +
   `throttle.RunOne`. `ErrNotFound` on a metadata call drops that album from
   the candidate set without aborting (TOCTOU: the user un-loved it via the
   web UI mid-run). Any other classified error follows the standard apply-loop
   semantics: classified 4xx → skip-and-continue, `ErrAuthFailed` → abort,
   streak counter contributes to the circuit breaker.
4. **Detect Case 1 groups.** Build a map keyed by
   `(ArtistID, Normalise(Title))`. Any entry with ≥2 members is a Case-1
   group. Members within a group are sorted by `PickWinner`'s ordering;
   index 0 = winner, indices 1+ = losers.
5. **Build the post-Case-1 candidate set.** Case 2 is detected on the set
   of loved albums *minus the Case-1 losers*. This avoids the edge case
   where a Case-1 loser is also chosen as a Case-2 parent, which would
   leave the run-record pointing at a parent that the same run is about
   to un-love. Concretely: from this point onward, "loved set" for the
   Case-2 logic means `loved \ case1_losers`. Case-1 losers can never
   become Case-2 parents *or* Case-2 shorts.
6. **Identify artists needing phase 2.** Operating on the post-Case-1 set
   (step 5), an artist needs phase 2 iff their remaining loved albums
   include both at least one short album (track count ≤
   `Case2TrackThreshold`) and at least one long album (track count >
   threshold). Artists with no short albums, or with no long albums, skip
   phase 2 entirely.
7. **Phase 2 — fetch tracklists for long albums of phase-2-eligible
   artists.** For each long album in those artists' (post-Case-1) loved
   sets, call `ListAlbumTracks` (paced, retry-classified, same as phase 1).
   `ErrNotFound` on a tracklist fetch drops that long album from the
   matching pool but doesn't abort.
8. **Detect Case 2 groups.** For each phase-2-eligible artist, for each
   short album of that artist (post-Case-1), look for any track on any
   long album of the same artist (post-Case-1) whose `Normalise(Title)`
   matches `Normalise(short.Title)`. If found, the short album is a
   Case-2 loser paired with the long album as parent. If multiple long
   albums match, pair with the long album whose normalised title is
   lexicographically smallest (deterministic tiebreaker, log-only).
9. **Build the dedupe plan.** Flatten Case-1 losers + Case-2 shorts into
   `albums_to_unlove`, deduped by `ALB_ID`. The Case-1 winner of a group
   is *not* in `albums_to_unlove` (it stays loved). The Case-2 parent is
   *not* in `albums_to_unlove` (it was already loved, no action). Because
   Case 2 is detected on the post-Case-1 set, an album cannot be both a
   Case-1 loser and a Case-2 short — they're disjoint by construction.
10. **Write the run record.** Atomic write to
    `<backup-dir>/deezer-loved-albums-dedupe-<UTC>.json`
    (`.tmp` → `fsync` → `rename`, `0600`):

    ```json
    {
      "version": 1,
      "started_at": "2026-05-05T20:00:00Z",
      "stats": {
        "loved_albums": 1834,
        "phase1_calls": 1834,
        "phase2_calls": 47,
        "case1_groups": 12,
        "case2_groups": 39,
        "albums_to_unlove": 51
      },
      "case1_groups": [
        {
          "artist_id": "8537", "artist_name": "Daft Punk",
          "normalised_key": "random access memories",
          "winner": {"id": "...", "title": "Random Access Memories",
                     "fan_count": 412000, "track_count": 13},
          "losers": [
            {"id": "...", "title": "RANDOM ACCESS MEMORIES",
             "fan_count": 1200, "track_count": 13}
          ]
        }
      ],
      "case2_groups": [
        {
          "artist_id": "...", "artist_name": "...",
          "parent": {"id": "...", "title": "Some LP", "track_count": 12},
          "shorts": [
            {"id": "...", "title": "Some Track", "track_count": 1,
             "matched_track_id": "..."}
          ]
        }
      ],
      "albums_to_unlove": [
        {"id": "...", "title": "...", "artist": "...",
         "case": "case1" | "case2", "reason": "same normalised title" |
                                              "single masquerading as <parent>"}
      ]
    }
    ```

11. **Empty-plan short-circuit.** If `albums_to_unlove` is empty, print
    `Nothing to dedupe; loved-albums list is clean.` and exit `0`.
12. **Dry-run short-circuit.** If `--dry-run`, print
    `would unlove N albums (X case-1, Y case-2), run-record at <path>` and
    exit `0`. Steps 13–15 are skipped.
13. **Confirmation gate** (single, batched). Print summary + run-record path:

    ```
    Will unlove N albums (X case-1 dups, Y case-2 singles).
    Run record: <path>
    Type yes to apply:
    ```

    Anything other than `yes` (case-insensitive, trimmed) aborts with
    `ErrAborted`. Read from `/dev/tty` if stdin was consumed.
14. **Apply phase — un-love each loser.** Sequential, one album at a time,
    via `RemoveFavoriteAlbum`. Per-album retry through `throttle.RunOne` with
    `gateway.IsRetryable` and the configured backoff. Per-item failure →
    append `{id, title, artist, case, error}` JSON line to
    `<backup-dir>/deezer-loved-albums-dedupe-<UTC>.skip.log`.
    `ErrAuthFailed` → abort whole run with refresh message. Streak
    circuit breaker default 5; counter resets on each successful unlove.
    `ctx` is checked with an explicit `select` between every successful
    un-love (mirrors `lovedtracks.Wipe`'s invariant — without it a long
    happy-path apply ignores SIGINT until the next failure).
15. **Final summary.**
    `Unloved A albums (X case-1, Y case-2), skipped S (see <skip-log>),
    elapsed T`. Exit `0` iff `S == 0`; non-zero otherwise.

## Title normalisation

```
Normalise(s):
  s = NFKD(s)
  s = strip combining marks            // "Café" → "Cafe"
  s = lowercase
  s = remove all non-alphanumeric/non-space runes
  s = collapse whitespace runs to single space
  s = trim leading/trailing whitespace
```

Used for both album titles (Case 1 grouping) and album-vs-track equality
(Case 2 matching). Same function, same rules — symmetry matters.

Deliberately **not** stripped: edition suffixes like `(Deluxe Edition)`,
`(Remastered)`, `(2011 Remaster)`, `[Bonus Track Version]`,
`- Anniversary Edition`. Different editions are treated as distinct albums.

## Winner selection (Case 1)

Strict, deterministic ordering:

1. **Highest `TrackCount`** wins (full album beats single/EP/promo even if
   the single has more fans).
2. Tie → **highest `FanCount`** wins (canonical edition over alt-region).
3. Still tied → **lowest `ALB_ID`** as a string-numeric comparison
   (Deezer IDs are roughly chronological; this picks the earliest catalogue
   entry as a stable proxy).

No fan-count threshold. No interactive override. The run-record JSON gives
the user the data to verify each pick before confirming the batch.

## Case 2 detection

Conditions for an album `S` to be a Case-2 loser:

- `S` is in the post-Case-1 loved set (i.e. is loved AND is not a Case-1
  loser).
- `S.TrackCount` ≤ `Case2TrackThreshold` (default 3).
- There exists a long album `L` such that:
  - `L` is in the post-Case-1 loved set.
  - `L.ArtistID == S.ArtistID`
  - `L.TrackCount > Case2TrackThreshold`
  - some track `T` on `L` satisfies `Normalise(T.Title) == Normalise(S.Title)`

If multiple `L` candidates qualify, the chosen parent is the `L` whose
`Normalise(L.Title)` is lexicographically smallest. (Determinism only —
the choice doesn't affect what gets un-loved.)

The parent album `L` stays loved. Only `S` is un-loved.

## `playlists love-contents` integration

A within-playlist Case-1 collapse is added to `playlistlove.Aggregate` (or
its caller — implementation choice between the brainstorming and the plan).

After the existing `ALB_ID` dedupe step:

1. Group the unique-album set by `(ART_ID, Normalise(ALB_TITLE))`.
2. For each group with ≥2 members, fetch `GetAlbumMetadata` for each member
   (paced via `throttle.Sleep`, retry-classified via `throttle.RunOne`),
   then call `lovedalbums.PickWinner` on the group's metadata.
3. Drop the losers from the candidate set.
4. Surface counts in `AggregatedSet` (new field
   `Case1WithinPlaylistSuppressed int`) and in the run-record stats.

The `playlistlove.Gateway` interface gains one method:

```go
GetAlbumMetadata(ctx context.Context, albumID string) (gateway.AlbumMetadata, error)
```

Metadata calls are bounded by the number of conflict groups, **not** by
playlist size. Typical run hits zero or a handful. If a `GetAlbumMetadata`
call returns `ErrNotFound` (TOCTOU), drop that member from its group and
pick the winner from the rest; don't abort the love-contents run.

Against-loved-albums Case-1 dedup is **not** done in love-contents. The
standalone `loved-albums dedupe` command, run after love-contents, picks
those up across the union. This is a deliberate cost / complexity trade —
doing it inside love-contents would require fetching metadata for every
existing loved album per love-contents invocation, which dwarfs the rest
of the run's API budget.

## Error handling and exit codes

Mostly inherited from `lovedtracks` and `playlistlove`. Calling out the
deltas only.

**Inherited (no change):**

- All gateway calls go through `callWithCSRF`. `ErrCSRFExpired` is handled
  transparently with refresh-and-retry.
- Classified kinds: `ErrAuthFailed`, `ErrCSRFExpired`, `ErrRateLimited`,
  `ErrServerError`, `ErrNotFound`. `QUOTA_ERROR` (HTTP 200) and HTTP 429 →
  `ErrRateLimited`. `IsRetryable` returns true for `ErrRateLimited` /
  `ErrServerError`.
- `ErrAuthFailed` aborts the whole run with the
  `refresh arl in ~/.config/deezer-tools/config.toml` message.
- Per-album failures append to `*.skip.log` → non-zero exit.
- `throttle.Sleep` fires before every gateway call (phase 1, phase 2,
  unlove). The pacer is the bot-detection mitigation; do not skip it on
  the happy path.
- Streak circuit breaker: `MaxConsecutiveFinalFailures` consecutive
  per-album final failures aborts the run. Counter resets on any successful
  un-love. Negative disables.

**New things specific to this tool:**

- **`RemoveFavoriteAlbum` on an already-unloved album** (TOCTOU race; the
  user un-loved it via the web UI between phase 1 and apply): expected
  classification is `DATA_ERROR` → `ErrNotFound`. Treat as a one-shot skip
  with a meaningful skip-log entry; do **not** retry. Exact response shape
  is unverified — implementation plan must capture and confirm against the
  first wet run.
- **`GetAlbumMetadata` / `ListAlbumTracks` on a non-existent album**: same
  classification, same handling. Drop from the candidate set, continue.
- **Unknown error envelopes from any of the three new methods** →
  `ErrUnknown`, fall through, surface in skip log without retrying. Capture
  the literal JSON envelope for a future classifier branch (per the
  favorites-protocol research doc's discovery plan, mirrored here for
  consistency).
- **Idempotency on re-run.** A second invocation after a clean run finds
  nothing to dedupe (losers are gone from the loved set, so no Case-1
  groups reform). A re-run after a partial run finds the still-loved
  losers as candidates and re-attempts them. No state file needed; the
  loved-album list is the source of truth.

**Exit codes:**

- `0` — clean run (every album un-loved, or empty plan, or successful
  dry-run).
- non-zero — anything skipped, run aborted by auth / circuit-breaker /
  user. Specific exit-code values are not part of v1.

## The "smart but simple" piece

For Nils's typical loved-albums size (low thousands; the running figure on
recent runs is <2k):

- **Phase 1: one `GetAlbumMetadata` per album.** ~2k calls at ~1.2s/call =
  ~40 min wall clock.
- **Phase 2: tracklists for long albums in phase-2-eligible artists only.**
  Most artists won't qualify. Order-of-magnitude napkin: 50 phase-2 calls,
  ~1 min.
- **Apply: one `RemoveFavoriteAlbum` per loser.** Typical run probably
  ≤100 losers, ~2 min.

Total: a fraction of an hour for a full sweep, dominated by phase 1.

The simplification that makes phase 2 cheap is the "artist needs both short
*and* long loved albums" filter. Without that filter we'd be fetching
tracklists for every loved album. With it, we only fetch where Case 2 can
actually trigger.

There is no caching, no parallelism, no batching. The gateway has no batch
album-metadata primitive that any OSS client uses; sequential paced calls
are the safe path through Akamai.

## Testing

CLAUDE.md constraints: no mocked-behavior tests, no e2e mocks, real data
and real APIs for end-to-end testing.

- **Unit tests for `internal/lovedalbums/match.go`** — pure, table-driven:
  - Normalisation: NFKD/lowercase/punctuation/whitespace cases, including
    `"Café"`/`"Cafe"`, `"It's"`/`"Its"`, double-spaces, leading/trailing
    whitespace, mixed-case combinations.
  - Case-1 grouping: 2-member, 3-member, separate artists (must not group),
    same title across artists (must not group).
  - Case-2 detection: short matches a track on a long; short matches
    multiple longs (deterministic tiebreak on lex-smallest normalised
    parent title); short with no matching long (no group); track-count
    boundary at 3 vs 4.
  - Case 2 detection on the post-Case-1 set: a Case-1 loser is never
    revisited as a Case-2 short or as a Case-2 parent (verified by
    constructing a fixture where the naive algorithm would do so).
- **Unit tests for `internal/lovedalbums/plan.go`** — winner-picking edge
  cases:
  - Tracks differ → most tracks wins regardless of fans.
  - Tracks equal, fans differ → highest fans wins.
  - All equal → lowest ALB_ID wins.
  - Case-2 plan: parent always winner; multiple shorts collapse onto one
    parent.
- **Unit tests for `internal/lovedalbums/dedupe.go`** — orchestration with
  a fake Gateway returning canned data:
  - Empty loved set → empty plan → no calls beyond list.
  - Phase 2 only triggered for artists with mixed-length loved albums.
  - Streak circuit breaker trips at `MaxConsecutiveFinalFailures`
    consecutive un-love failures.
  - Context cancellation between un-loves — verified via cancellable
    ctx + count of completed un-loves.
  - `ErrNotFound` from `GetAlbumMetadata` drops album from candidate set,
    no abort.
  - `ErrNotFound` from `RemoveFavoriteAlbum` skip-and-continue, not retry.
  - Skipped albums → non-zero error return + populated `Result`.
  - Atomic backup write at `0600`; skip log path derived from record path.
- **Unit tests for `internal/gateway`** via `httptest.Server`, same shape
  as existing tests:
  - `GetAlbumMetadata`: happy path, CSRF refresh on first call,
    `DATA_ERROR` → `ErrNotFound`.
  - `flexString` mixed-form regression: response payloads that quote
    `ALB_ID` as `"123"` AND as bare `123` *within the same chunk of
    synthetic responses*. Same for `ART_ID`, and any of `NB_FAN` /
    `NB_TRACK` that the wet run shows as flexed.
  - `ListAlbumTracks`: happy path + classified errors + flexString IDs.
  - `RemoveFavoriteAlbum`: happy path + `DATA_ERROR` → `ErrNotFound` +
    `ErrCSRFExpired` refresh-and-retry.
- **Unit tests for `internal/playlistlove` Case-1 within-playlist dedup**:
  - No conflict groups → no metadata calls (verified via fake Gateway
    counting calls).
  - Two same-(artist, normalised-title) candidates → one `GetAlbumMetadata`
    call per member, winner picked, loser dropped, count surfaced.
  - Three+ members in a conflict group.
  - Conflict group where one member returns `ErrNotFound` from
    `GetAlbumMetadata` → drop that member from the group, pick winner from
    the rest (don't abort the whole love-contents run).
- **Existing `internal/lovedtracks` and `internal/playlistlove` tests pass
  unchanged** wherever they're not directly extended.
- **Live integration tests gated by `DEEZER_INTEGRATION=1`** in
  `internal/gateway/integration_test.go`, **read-only**:
  - `GetAlbumMetadata`: pull metadata for ~5 known ALB_IDs from the user's
    loved set. Verify all required fields decode without error.
  - `ListAlbumTracks`: fetch tracklist for one known long loved album;
    verify track titles decode and count matches metadata's `TrackCount`.
  - `RemoveFavoriteAlbum` is **not** in the live integration test (it's a
    write). Verified manually at first wet run via `--dry-run` first, then
    a real run.

## Honest disclosures

- **Exact gw-light method names + parameter shapes for `album.getData`,
  `album.getSongs`, `album.deleteFavorite` are not asserted by this spec.**
  Names listed are the consensus from prior research and the symmetry with
  `album.addFavorite`. The implementation plan must include a "verify
  exact gateway methods + sample wire shapes against deemix / deezer-py /
  d-fi-core" task before any new gateway code is written. Same lesson as
  the favorites-naming-asymmetry doc.
- **Exact JSON field names for fans (`NB_FAN`) and track count (`NB_TRACK`)
  are not asserted.** Verified at impl time against real responses.
- **`flexString` field set is not asserted.** Default to `flexString` for
  every ID-shaped field; verify the rest (counts, etc.) at impl time. The
  gw-light-quirks doc warns that the bug surfaces deep in pagination —
  unit tests must mix forms within a single response.
- **Idempotency response shape on `RemoveFavoriteAlbum` for already-unloved
  is not asserted.** Likely `DATA_ERROR` → `ErrNotFound`, mirroring the
  add-side discovery; confirmed at first wet run.
- **Loved-albums ceiling behavior** (whether un-loving down past some
  threshold has any side-effect) is not relevant — pure removal can't
  trip a ceiling.
- **The Case-1 selection rule (tracks > fans > ALB_ID) is a heuristic, not
  a guarantee.** If the wet run shows fans-based picks are wrong, the
  follow-up is to add a tiebreaker layer (e.g. Deezer search) — not part
  of v1.

## Risks and known unknowns

- **Exact gateway method names & wire shapes.** Verified against OSS
  libraries at implementation time.
- **Case-2 false positives.** A short album may legitimately share a title
  with a track on a longer same-artist album without being a single from
  it (independent EP, coincidental track name). The threshold (≤3) and
  the requirement that the long album also be loved limit this, and the
  run-record gives the user a chance to spot suspicious entries before
  confirming. Accepted risk.
- **TOCTOU between phase-1 list and apply phase.** Mitigated by treating
  `ErrNotFound` as a one-shot skip on un-love.
- **`arl` cookie TTL.** Months in practice, no SLA. Auth-failure detection
  with the standard refresh message covers eventual expiry.
- **Deezer changing the gateway.** Inherent to the unofficial path.
  Accepted.

## Decisions log

For traceability of choices made during brainstorming:

- **Mode: detect → batch confirm → unlove.** Single confirmation for the
  whole list (vs per-album confirm, vs two-phase report-then-apply, vs
  detect-only). Matches the wipe / love-contents shape.
- **Selection signal: tracks > fans > ALB_ID.** (Vs fans-first, vs Deezer
  search default, vs completeness scoring.) Tracks-first reflects the
  Spotify-import scenario where the loser is often a 1-track promo with
  high fan counts; fans-first would pick the promo over the album.
  Deezer-search adds an extra API call per group with diminishing returns
  — search rank is largely fan-driven anyway.
- **Title normalisation: NFKD + casefold + punctuation strip; no edition
  stripping.** (Vs strict case-only; vs aggressive `(Deluxe)` /
  `(Remastered)` stripping.) Catches the casing/punctuation cases observed
  in the wild without quietly merging editions the user may have
  intentionally kept distinct.
- **Case 2 scope: pure dedup, only collapse when long album is already
  loved.** (Vs replace-the-single-with-the-real-album.) Pure dedup never
  reduces or expands library coverage. Singles-loved-on-purpose stay
  loved.
- **Case-2 track threshold: ≤3.** (Vs ≤1; vs ≤5; vs `< parent.TrackCount`.)
  Catches `single + remix(es)` patterns common on imports without flagging
  legit short EPs more than necessary.
- **Case 2 runs on the post-Case-1 set.** (Vs detecting both in parallel
  and resolving overlap afterward.) Detecting in parallel allows a Case-1
  loser to be picked as a Case-2 parent, which would leave the run-record
  pointing at a parent the same run is about to un-love. Sequencing
  Case 1 first eliminates the wrinkle by construction; loser sets are
  always disjoint.
- **love-contents integration: Case 1 only, both within-playlist and
  *not* against-loved-albums.** (Vs no integration; vs full Case 1 + Case 2;
  vs against-loved-albums included.) Within-playlist dedup is cheap
  (metadata calls bounded by conflict groups, not playlist size).
  Against-loved-albums dedup would balloon the API budget and the
  standalone command catches it on the next sweep anyway. Case 2
  benefits from operating on the full loved set.
- **love-contents picker: full metadata calls, not lowest-ALB_ID
  fallback.** (Vs lowest-ALB_ID-wins to avoid metadata calls.) Within
  cost bounds since calls are scoped to actual conflict groups; the
  picking quality matches the standalone command's, so picks are
  consistent across the two paths.
- **Architecture: new `internal/lovedalbums` package, mirrors
  `lovedtracks` / `playlistlove`.** (Vs flag on existing command; vs
  feature in `playlistlove`.) Standalone use case is explicit, and the
  layering rule in CLAUDE.md is "don't cross-import sibling domain
  packages". A new domain package is the right fit.
- **Two-pass metadata fetching.** (Vs single-pass metadata + tracklist
  for everything.) ~2-3× the API budget difference for the full-fetch
  approach; the artist-eligibility filter for phase 2 trims it cheaply.
- **`Normalise` and `PickWinner` exposed; `Run` not.** (Vs full
  cross-package surface; vs zero cross-package surface.) `playlistlove`
  needs the rules but does its own IO. Exposing the orchestrator would
  invite cross-domain coupling.
