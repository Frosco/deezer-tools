# Loved-Albums Dedupe — Follow-Ups

**Date opened:** 2026-05-07
**Source:** Final-review pass on `wip/loved-albums-dedupe` (commits ce8c93f → b16677d). Wet run against Nils's real account on 2026-05-07 succeeded.
**Status:** Open. None blocking; revisit when adjacent work justifies the cost.

The shipped feature is good. These are the items the review caught that were
deliberately deferred — captured here so they don't get lost.

## 1. `Phase2Fetch` and `DetectCase2` partition the same artist eligibility twice

**Where:**
- `internal/lovedalbums/fetch.go` — `Phase2Fetch` builds shorts/longs per artist, defaults `threshold` if ≤0, iterates eligible artists.
- `internal/lovedalbums/match.go` — `DetectCase2` builds shorts/longs per artist again, defaults `threshold` if ≤0, iterates eligible artists.
- `internal/lovedalbums/dedupe.go` — `errSkippedTracks` sentinel exists only to bridge `Phase2Fetch`'s pre-fetched lookup into `DetectCase2`'s lazy-fetch error contract.

**What's wrong:** the two functions were originally designed for a "pure
detection / paced fetch" split where `DetectCase2`'s `fetchTracks` callback
would lazy-fetch on demand. The orchestrator pre-fetches up front, so the
sentinel exists only to satisfy a contract that nothing actually needs.

**Cleanup options (pick one when revisiting):**
- (a) Collapse both into a single `DetectCase2WithGateway(ctx, gw, post, threshold)` that does the partition once, fetches paced, and returns groups.
- (b) Keep the split but have `Phase2Fetch` return both the lookup AND a pre-binned `[]artistEligibility` so `DetectCase2` doesn't repeat the work.

Either kills `errSkippedTracks` and the duplicate threshold defaulting.

**Why deferred:** behavior-neutral refactor. The current shape is correct
and the duplication is bounded.

## 2. `Phase1Fetch` / `Phase2Fetch` silently drop on retry exhaustion

**Where:** `internal/lovedalbums/fetch.go:73-75` (Phase1) and `:165-167` (Phase2).

**What happens:** when `throttle.RunOne` exhausts the retry schedule on a
retryable error (e.g. sustained QUOTA_ERROR or 5xx), the final error is
non-retryable. Phase1/Phase2 then route it through `notify(id, err)` and
`continue` — the album drops from the candidate pool. The user only sees
stderr `phase1 dropped <id>: <err>` lines. There's no `Result.Phase1Failed`
or `Phase2Failed` counter to surface the silent drop in the run-record
stats or the final summary.

**Symptom shape:** Case-2 detection reports fewer groups than truth without
flagging exhaustion specifically. Case-1 detection misses albums whose
metadata fetch exhausted retries.

**Fix:** add `Phase1Failed` / `Phase2Failed` to `lovedalbums.Result`,
increment them in the notify path, and surface in the run-record JSON +
final summary line.

**Test gap to close at the same time:**
- `Phase1Fetch` + `Phase2Fetch` retry-exhaustion paths have no direct unit
  coverage. Add `fakeGW` that returns `&GatewayError{Kind: ErrRateLimited}`
  for a specific ID with empty `RetryBackoff`; assert the album is omitted,
  `notify` is invoked, and the new counter increments.

**Why deferred:** observability concern, not a correctness bug. The current
behavior matches the documented "drop and continue" contract.

## 3. `lovedalbums` exports more than its consumers consume

**Where:** `internal/lovedalbums/{match,plan,fetch,dedupe}.go`.

**Public surface today:** `Run`, `Options`, `Result`, `ErrAborted`, `Gateway`,
`Normalise`, `PickWinner`, `Phase1Fetch`, `Phase2Fetch`, `TracksLookup`,
`DetectCase1`, `DetectCase2`, `Case1Group`, `Case2Group`, `BuildPlan`,
`CaseKind` (+ `Case1`/`Case2` constants), `UnloveEntry`, `DedupePlan`.

**Actually used by external consumers** (`cmd/deezer-tools/lovedalbums_cmd.go`
and `internal/playlistlove/diff.go`): `Run`, `Options`, `Result`,
`ErrAborted`, `Gateway`, `Normalise`, `PickWinner`. Everything else is
internal-only.

**Cleanup:** lowercase `Phase1Fetch`, `Phase2Fetch`, `TracksLookup`,
`DetectCase1`, `DetectCase2`, `Case1Group`, `Case2Group`, `BuildPlan`,
`CaseKind`, `UnloveEntry`, `DedupePlan` (and their constructors / consts).
Tests in the same package keep package-private access.

**Why deferred:** wide diff with no behavioral change. The narrower contract
the package doc-comment promises ("callers own their own gateway IO; the
package owns matching rules") matches what's *used*, not what's exported.
Worth doing the next time someone touches this code substantively.

## 4. Cross-package duplication of "classify error and decide"

**Where:** six call sites across `internal/lovedalbums/{dedupe,fetch}.go` and
`internal/playlistlove/{run,diff}.go` repeat:

```go
if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
    return ...
}
var ge *gateway.GatewayError
if errors.As(err, &ge) && ge.Kind == gateway.ErrAuthFailed {
    return wrap(err)
}
```

`classifyAuth` in `dedupe.go:329` covers half of it (the auth wrap) but not
the ctx-cancel passthrough.

**Cleanup:** lift to a shared helper. Two reasonable homes:
- `internal/throttle.ClassifyTerminal(err) (terminal bool, kind ErrorKind)` returning the two terminal-error decisions.
- `internal/gateway` package-level helper since both decisions depend on `gateway.GatewayError` shape.

**Why deferred:** the duplication is correctness-preserving today, and
adding a fourth domain package (loved artists?) is what would tip this from
"two copies" to "rule of three" justification.

## 5. Run-record / skip-log filename collision in same UTC second

**Where:**
- `internal/lovedalbums/dedupe.go:391-394` — record path stamp `20060102T150405Z`
- `internal/lovedalbums/dedupe.go:294-301` — `.skip.log` derived from record path
- `internal/playlistlove/run.go` — same pattern

**Risk:** two `Run`s in the same UTC second clobber each other's record AND
skip log via `O_TRUNC`. Realistic-rare for human-driven CLI use; surfaces
in scripted invocations or parallel-test scenarios.

**Cleanup:** millisecond resolution in the timestamp, OR a PID suffix.

**Why deferred:** human-driven CLI doesn't trip this; tests use
`t.TempDir()` so they're isolated.

## 6. Within-playlist Case-1: 2-member group with one fetch failure silently drops

**Where:** `internal/playlistlove/diff.go` — pre-Case-1 collapse.

**Behavior:** if a 2-member pre-filter group has one fetch succeed and one
fail (NotFound, retry-exhausted, etc.), the failed-fetch member's index
goes into `drop`, then `len(members) < 2` short-circuits and `PickWinner`
is never called. The failed-fetch album is dropped from `AlbumsToAdd`
without ever being proven a Case-1 duplicate. We just couldn't verify it.

**Tested as intentional** in `TestCollapseCase1WithinPlaylist_metadataNotFound_dropsMember`,
but the doc-comment understates the consequence ("drops the affected
member from the conflict group" — also drops it from the love-add plan).

**Decision:**
- For `ErrNotFound` (album doesn't exist anymore): the current behavior is correct.
- For retry-exhausted rate-limit: silently skipping a love-add we couldn't introspect is suboptimal; ideally we'd love it anyway and let the next `loved-albums dedupe` run resolve it.

**Cleanup:** distinguish `ErrNotFound` from "couldn't fetch but probably
exists" in the drop path; on the latter, keep the album in `AlbumsToAdd`
with a stderr warning.

**Why deferred:** edge case, no real-world wet-run signal yet.

## 7. Test coverage gaps captured by the review

These are smaller than items 1–6 but worth pinning when revisiting:

- `DetectCase2` deterministic ordering across multiple artists — the
  `sort.Strings(artistIDs)` in `match.go:139` is easy to break by accident.
- `BuildPlan` ordering invariant: "Case-1 entries first, then Case-2
  entries when groups are interleaved" — not pinned by a test.
- Run-record JSON schema — `TestRun_dryRun_writesRecord_doesNotUnlove`
  unmarshals to `map[string]any` and only asserts `version==1`. Add field-
  level assertions so refactors that drop `Parent` or rename fields fail.
- `CollapseCase1WithinPlaylist`: ctx cancel during the inner
  `GetAlbumMetadata` loop is uncovered.
- `writeRunRecord` / `openSkipLog` file mode — no test stat()s the file to
  confirm `0600`. CLAUDE.md flags this as the same invariant as the wipe's
  backup files.

## Process notes from this round

- **Spec/plan placeholders are load-bearing.** Two of the more-significant
  bugs in the plan were the `<METHOD>` placeholders that the implementer
  was meant to substitute from the research doc — `album.getSongs` (wrong)
  vs `song.getListByAlbum` (correct, namespace-flipped). The
  favorites-naming-asymmetry pattern is now confirmed across two artifacts.
- **Plan tests can contradict their own algorithm.** `Walk-On → walk on`
  expected output didn't match the design-spec's "drop non-alphanumeric"
  rule. The spec was right; the test was wrong.
- **Plan tests can use field names that don't exist.** `RawMessage` was
  used in test `&gateway.GatewayError{...}` literals; the actual field is
  `Message`. Caught by the compiler.
