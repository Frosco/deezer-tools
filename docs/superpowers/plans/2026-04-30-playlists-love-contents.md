# Playlists Love-Contents Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the second subcommand `deezer-tools playlists love-contents <inputs>...` which reads N Deezer playlists, dedupes the songs to unique albums and artists, diffs against the user's loved-albums and loved-artists collections, and (after confirmation) loves the missing items via the unofficial `gw-light.php` gateway.

**Architecture:** Reuses the existing `internal/gateway` substrate (cookie-jar HTTP client, CSRF lifecycle, error classification). New gateway primitives live in three new files: `playlists.go` (read playlist songs), `albums.go` (list + add favorite album), `artists.go` (list + add favorite artist). The wipe's pacer / retry / circuit-breaker discipline is **extracted** to a new shared package `internal/throttle` and `internal/lovedtracks` is refactored to use it; the wipe's public API and tests stay unchanged. A new domain package `internal/playlistlove` orchestrates input normalization (numeric, long URL, short share link), dedupe, diff, atomic JSON run-record, confirm, and paced apply. A new Cobra subcommand wires it up.

**Tech Stack:** Go 1.22+, `github.com/spf13/cobra` (already), stdlib `net/http` and `net/http/cookiejar` (already), `net/http/httptest` for unit tests, stdlib `testing`. No new third-party dependencies.

**Spec:** `docs/superpowers/specs/2026-04-29-playlists-love-contents-design.md`

---

## Pre-Implementation Setup

The spec lives on `main`. Per Nils's CLAUDE.md, design docs/plans/research must not appear in the MR diff. Implementation lands on a WIP branch off `main`.

```bash
git checkout main
git pull --ff-only origin main 2>/dev/null || true
git checkout -b wip/playlists-love-contents
```

All implementation commits land on `wip/playlists-love-contents`. The research document produced by Task 2 lives on `main` (separate commit, separate branch context, mirroring the wipe's research doc on `2026-04-27-deezer-gateway-protocol.md`).

**Module path note:** This plan assumes `github.com/niref/deezer-tools` (matches existing `go.mod`). No `go.mod` changes expected.

---

## File Structure

```
deezer-tools/
├── cmd/deezer-tools/
│   ├── main.go                       # MODIFY: register newPlaylistsCmd
│   ├── lovedtracks_cmd.go            # unchanged
│   └── playlistlove_cmd.go           # NEW: playlists / love-contents wiring
├── internal/
│   ├── config/                       # unchanged
│   ├── gateway/
│   │   ├── client.go                 # unchanged
│   │   ├── csrf.go                   # unchanged
│   │   ├── errors.go                 # MODIFY: add IsRetryable
│   │   ├── errors_test.go            # MODIFY: add IsRetryable test
│   │   ├── tracks.go                 # unchanged (flexString reused by new files)
│   │   ├── tracks_test.go            # unchanged
│   │   ├── playlists.go              # NEW: PlaylistSong, ListPlaylistSongs
│   │   ├── playlists_test.go         # NEW
│   │   ├── albums.go                 # NEW: ListFavoriteAlbumIDs, AddFavoriteAlbum
│   │   ├── albums_test.go            # NEW
│   │   ├── artists.go                # NEW: ListFavoriteArtistIDs, AddFavoriteArtist
│   │   ├── artists_test.go           # NEW
│   │   └── integration_test.go       # MODIFY: add playlist + favorite-id reads
│   ├── throttle/                     # NEW package, extracted from lovedtracks
│   │   ├── throttle.go               # Pace, Jitter, Sleep, RunOne, defaults
│   │   └── throttle_test.go
│   ├── lovedtracks/
│   │   ├── wipe.go                   # REFACTOR: use throttle, gateway.IsRetryable
│   │   └── wipe_test.go              # MODIFY: init() zeroes throttle.Pace/Jitter
│   └── playlistlove/                 # NEW package
│       ├── input.go                  # Input, NormalizeInputs, ResolveShareLink
│       ├── input_test.go
│       ├── diff.go                   # Album, Artist, Aggregate, Diff
│       ├── diff_test.go
│       ├── run.go                    # Options, Result, Run
│       └── run_test.go
└── docs/superpowers/
    ├── plans/2026-04-30-playlists-love-contents.md     # this file (on main)
    └── research/2026-04-30-deezer-favorites-protocol.md # Task 2 (on main)
```

---

## Task 1: WIP branch

**Files:** none

- [ ] **Step 1: Verify clean working tree on main**

```bash
git status
git rev-parse --abbrev-ref HEAD
```

Expected: working tree clean (or only untracked dotfiles unrelated to the project — leave them alone). Branch: `main`.

- [ ] **Step 2: Update main**

```bash
git pull --ff-only origin main 2>/dev/null || true
```

- [ ] **Step 3: Create the WIP branch**

```bash
git checkout -b wip/playlists-love-contents
```

- [ ] **Step 4: Verify**

```bash
git rev-parse --abbrev-ref HEAD
```

Expected: `wip/playlists-love-contents`.

---

## Task 2: Research and document new gw-light methods

**Files:**
- Create: `docs/superpowers/research/2026-04-30-deezer-favorites-protocol.md` (committed to `main`, NOT to the WIP branch)

This task produces no code. The spec explicitly requires verification of method names, parameter shapes, the Various-Artists `ART_ID`, and the idempotency response shape on add-favorite calls before any new gateway code is written.

Mirrors the structure of the wipe's `2026-04-27-deezer-gateway-protocol.md`. That file already documents the endpoint, envelope, and shared error codes — this new file extends it for the new methods.

- [ ] **Step 1: Switch to main**

```bash
git stash --include-untracked 2>/dev/null || true   # in case anything got staged
git checkout main
```

- [ ] **Step 2: Read the canonical OSS references**

Browse on GitHub. Record commit hashes inline in the research doc.

- `https://github.com/RemixDev/deemix` — `deezer/__init__.py`, `deezer/gw.py`. Look for `playlist.getSongs`, `album.getFavoriteIds`, `favorite_album.add`, `artist.getFavoriteIds`, `favorite_artist.add`.
- `https://github.com/browser-fingerprinting/deezer-py` — `deezer/api.py`, `deezer/gw.py`. Cross-reference field names.
- `https://github.com/freyr-music/d-fi-core` — same methods; cross-reference field names if available.

For each method, record:
1. Exact method name string.
2. Required and optional parameters (and whether IDs are quoted strings or numbers).
3. Response shape (top-level keys under `results`, inner field names).
4. Any known error codes specific to that method.
5. Idempotency behavior on add-favorite calls (what does the gateway return when the album/artist is already loved?).

For the Various-Artists `ART_ID`: confirm it's a stable single ID (commonly `5080`) by grepping deemix and deezer-py for "Various Artists" / "5080" handling, and any explicit special-case logic.

For loved-albums / loved-artists ceilings: search the OSS code for any explicit error-code mapping. If none, note that the ceiling behavior is unknown and will be discovered at impl time during Task 12 (integration smoke).

- [ ] **Step 3: Write the research doc**

Create `docs/superpowers/research/2026-04-30-deezer-favorites-protocol.md`:

```markdown
# Deezer gw-light: Playlist + Favorites (Albums, Artists) Reference

**Date:** 2026-04-30
**Sources:** deemix, deezer-py, d-fi-core (URLs + commit hashes inline below).
**Status:** Reference for new methods in `internal/gateway/{playlists,albums,artists}.go`.
**Companion:** `2026-04-27-deezer-gateway-protocol.md` (endpoint, envelope, shared error codes).

## Methods

### playlist.getSongs
- Source: <URL@hash>
- Body: `{ "playlist_id": "<id>", "nb": <pageSize>, "start": <offset>, "tab": "songs" }`
  (verify each field against sources).
- Pagination: `nb` per call; increment `start` by returned count.
- Returns under `results`:
  - `data` — array of song records, each with: `SNG_ID`, `SNG_TITLE`, `ALB_ID`, `ALB_TITLE`, `ART_ID`, `ART_NAME` (verify exact names).
  - `total` — total count.
- **ID flexing:** confirm whether `SNG_ID` / `ALB_ID` / `ART_ID` come as quoted strings or bare numbers. (Existing `flexString` in `tracks.go` handles either.)

### album.getFavoriteIds
- Source: <URL@hash>
- Body: `{ "user_id": "<USER_ID>", "nb": <pageSize>, "start": <offset>, "checksum": null }`
- Returns under `results`:
  - `data` — array of records, each with at least `ALB_ID`.
  - `total` — total count.
- Note: the spec only needs IDs; ignore other fields.

### favorite_album.add
- Source: <URL@hash>
- Body: `{ "ALB_ID": "<id>" }` (verify field name and whether quoted).
- Returns: typically a boolean or empty payload. **Capture exact shape on success and on already-loved.**

### artist.getFavoriteIds
- Source: <URL@hash>
- Body: `{ "user_id": "<USER_ID>", "nb": <pageSize>, "start": <offset>, "checksum": null }`
- Returns under `results`:
  - `data` — array of records, each with at least `ART_ID`.
  - `total` — total count.

### favorite_artist.add
- Source: <URL@hash>
- Body: `{ "ART_ID": "<id>" }` (verify field name).
- Returns: as above.

## Various-Artists ART_ID

- Asserted by: <source link + line numbers>
- Stable ID: `<value, expected "5080">`
- Implementation note: if the assertion turns out wrong on live data, fall back to `ART_NAME == "Various Artists"` (case-insensitive). Surface the discrepancy in this doc.

## Idempotency on add

- `favorite_album.add` for an already-loved album returns: <verified shape>
- `favorite_artist.add` for an already-loved artist returns: <verified shape>
- Mapping decision: if the response is success-shaped (no error envelope), no special-case is needed. If it's an error envelope with a stable code, classify it as success in `internal/gateway/errors.go` (add a new code branch in `classifyError`).

## Loved-albums / loved-artists ceiling

- OSS sources: <none documented> / <link if found>
- Discovery plan: small probe at impl time (Task 12). If hit, classified error code goes here.

## Field-name decisions

If sources disagree, prefer the most-recent commit on the most-active repo. Record the disagreement here so future tools don't re-research.
```

Fill in concrete URLs, commit hashes, and exact method/field names from the sources. **Do not invent.** If a source has changed, update accordingly.

- [ ] **Step 4: Update plan tasks 5/6/7 if research surfaces different names**

If research shows e.g. the listing method is `album.getList` rather than `album.getFavoriteIds`, edit Tasks 5/6/7 in this plan file (which lives on `main`) to use the discovered names. Commit those edits with the research doc.

- [ ] **Step 5: Commit on main**

```bash
git add docs/superpowers/research/2026-04-30-deezer-favorites-protocol.md docs/superpowers/plans/2026-04-30-playlists-love-contents.md
git commit -m "docs: research Deezer gw-light playlist + favorites methods"
```

- [ ] **Step 6: Return to the WIP branch**

```bash
git checkout wip/playlists-love-contents
```

The research doc is now available on `main` for reference but does not appear in the eventual MR diff.

---

## Task 3: Add `gateway.IsRetryable`

**Files:**
- Modify: `internal/gateway/errors.go`
- Modify: `internal/gateway/errors_test.go`

`gateway.IsRetryable` is the predicate the throttle package needs. It currently lives as a private `shouldRetry` in `internal/lovedtracks/wipe.go`. Promote it to the gateway package so both `internal/lovedtracks` and `internal/playlistlove` can use it without duplication.

- [ ] **Step 1: Add the failing test in `internal/gateway/errors_test.go`**

Append to the existing test file:

```go
func TestIsRetryable(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"non-gateway error", errors.New("plain"), false},
		{"rate limited", &GatewayError{Kind: ErrRateLimited}, true},
		{"server error", &GatewayError{Kind: ErrServerError}, true},
		{"auth failed", &GatewayError{Kind: ErrAuthFailed}, false},
		{"csrf expired", &GatewayError{Kind: ErrCSRFExpired}, false},
		{"not found", &GatewayError{Kind: ErrNotFound}, false},
		{"unknown", &GatewayError{Kind: ErrUnknown}, false},
		{"wrapped rate limited",
			fmt.Errorf("removeFavoriteSong: %w", &GatewayError{Kind: ErrRateLimited}),
			true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsRetryable(tc.err); got != tc.want {
				t.Errorf("IsRetryable(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
```

If `errors_test.go` doesn't already import `errors` and `fmt`, add them.

- [ ] **Step 2: Run, expect FAIL**

```bash
go test ./internal/gateway/ -run TestIsRetryable
```

Expected: undefined: `IsRetryable`.

- [ ] **Step 3: Implement in `internal/gateway/errors.go`**

Append at the end of `errors.go`:

```go
// IsRetryable reports whether err is a transient gateway failure that the
// caller should retry. True for ErrRateLimited (incl. QUOTA_ERROR mapped at
// HTTP 200) and ErrServerError; false for everything else, including auth
// and not-found.
func IsRetryable(err error) bool {
	var gerr *GatewayError
	if !errors.As(err, &gerr) {
		return false
	}
	return gerr.Kind == ErrRateLimited || gerr.Kind == ErrServerError
}
```

- [ ] **Step 4: Run, expect PASS**

```bash
go test ./internal/gateway/ -run TestIsRetryable -v
```

- [ ] **Step 5: Run full gateway test suite to confirm no regressions**

```bash
go test ./internal/gateway/ -v
```

Expected: all PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/gateway/errors.go internal/gateway/errors_test.go
git commit -m "feat(gateway): add IsRetryable predicate"
```

---

## Task 4: Extract `internal/throttle` and refactor `lovedtracks`

**Files:**
- Create: `internal/throttle/throttle.go`
- Create: `internal/throttle/throttle_test.go`
- Modify: `internal/lovedtracks/wipe.go`
- Modify: `internal/lovedtracks/wipe_test.go`

Behavior-preserving refactor. The wipe's pacer (`pacedSleep`), retry helper (`deleteWithRetry`), retry classifier (`shouldRetry` → already promoted to `gateway.IsRetryable` in Task 3), package-level pacing vars, and breaker constant migrate to `internal/throttle`. The wipe orchestration loop becomes a `throttle.RunOne` call site.

**Verification gate:** the existing wipe tests must pass unchanged after the refactor (apart from the one-line init() that targets the new variable names). If they don't, the refactor went wrong.

- [ ] **Step 1: Create `internal/throttle/throttle.go`**

```go
// Package throttle is the shared pacer + retry helper used by domain
// packages (lovedtracks, playlistlove, …) that issue paced writes against
// the gw-light gateway.
//
// The 1s ± 200ms baseline pace and 5s/15s/30s/60s/120s retry schedule were
// established by the loved-tracks wipe and tuned in response to the
// 2026-04-28 Akamai IP-block incident (see docs/solutions/integration-issues/).
//
// Pace and Jitter are package vars, not consts and not Options fields, so
// the test binary of consumers can zero them in init() without exposing
// pacing as production-tunable.
package throttle

import (
	"context"
	"math/rand/v2"
	"time"
)

var (
	// Pace is the baseline sleep before every gateway attempt.
	Pace = time.Second
	// Jitter is the random additional delay added to Pace, in [0, Jitter).
	Jitter = 200 * time.Millisecond
)

// DefaultRetryBackoff is the per-item retry schedule for retryable errors.
// 5s/15s/30s/60s/120s = ~232s of waiting before a single item is given up on.
var DefaultRetryBackoff = []time.Duration{
	5 * time.Second,
	15 * time.Second,
	30 * time.Second,
	60 * time.Second,
	120 * time.Second,
}

// DefaultMaxConsecutiveFinalFailures is the orchestrator-side circuit-breaker
// threshold: after this many items in a row exhaust their retry budget with
// no successful item between, the run aborts. Counter resets on any success.
const DefaultMaxConsecutiveFinalFailures = 5

// Sleep waits Pace + rand[0, Jitter) before returning, honoring ctx.
// Pace <= 0 returns immediately. Jitter <= 0 sleeps exactly Pace.
//
// Called before EVERY gateway attempt, including the first — that's the
// throttle that keeps us off Akamai's bot list on long happy-path runs.
func Sleep(ctx context.Context) error {
	pace := Pace
	if pace <= 0 {
		return nil
	}
	d := pace
	if Jitter > 0 {
		d += time.Duration(rand.Int64N(int64(Jitter)))
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}

// RunOne executes attempt with the configured retry schedule. Returns nil on
// success, the final error after retries on failure, or ctx.Err() on
// cancellation. The first attempt is always made; retries follow only if
// isRetryable returns true for the error.
//
//   - schedule == nil   → DefaultRetryBackoff is used.
//   - schedule == empty → no retries; first attempt only.
//
// CSRF refresh is the gateway client's job (callWithCSRF), so RunOne never
// has to know about CSRF. Auth failures, not-found, and other non-retryable
// classified errors return immediately so the caller can branch on them.
func RunOne(
	ctx context.Context,
	attempt func(ctx context.Context) error,
	isRetryable func(error) bool,
	schedule []time.Duration,
) error {
	if schedule == nil {
		schedule = DefaultRetryBackoff
	}
	err := attempt(ctx)
	if err == nil {
		return nil
	}
	for _, d := range schedule {
		if !isRetryable(err) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(d):
		}
		err = attempt(ctx)
		if err == nil {
			return nil
		}
	}
	return err
}
```

- [ ] **Step 2: Create `internal/throttle/throttle_test.go`**

```go
package throttle

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSleep_returnsImmediatelyWhenPaceZero(t *testing.T) {
	prevPace, prevJitter := Pace, Jitter
	t.Cleanup(func() { Pace, Jitter = prevPace, prevJitter })
	Pace, Jitter = 0, 0
	start := time.Now()
	if err := Sleep(context.Background()); err != nil {
		t.Fatalf("Sleep() err = %v", err)
	}
	if d := time.Since(start); d > 50*time.Millisecond {
		t.Errorf("Sleep with Pace=0 took %v, want ~0", d)
	}
}

func TestSleep_respectsContextCancel(t *testing.T) {
	prevPace, prevJitter := Pace, Jitter
	t.Cleanup(func() { Pace, Jitter = prevPace, prevJitter })
	Pace, Jitter = time.Second, 0
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(10 * time.Millisecond); cancel() }()
	start := time.Now()
	if err := Sleep(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Sleep() err = %v, want context.Canceled", err)
	}
	if d := time.Since(start); d > 200*time.Millisecond {
		t.Errorf("Sleep waited %v after cancel, want fast return", d)
	}
}

func TestRunOne_successFirstAttempt(t *testing.T) {
	calls := 0
	err := RunOne(context.Background(),
		func(ctx context.Context) error { calls++; return nil },
		func(error) bool { return true },
		nil,
	)
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1", calls)
	}
}

func TestRunOne_retryThenSuccess(t *testing.T) {
	transient := errors.New("transient")
	calls := 0
	err := RunOne(context.Background(),
		func(ctx context.Context) error {
			calls++
			if calls < 3 {
				return transient
			}
			return nil
		},
		func(e error) bool { return errors.Is(e, transient) },
		[]time.Duration{1 * time.Millisecond, 1 * time.Millisecond, 1 * time.Millisecond, 1 * time.Millisecond, 1 * time.Millisecond},
	)
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if calls != 3 {
		t.Errorf("calls = %d, want 3 (initial + 2 retries)", calls)
	}
}

func TestRunOne_budgetExhausted(t *testing.T) {
	transient := errors.New("transient")
	calls := 0
	err := RunOne(context.Background(),
		func(ctx context.Context) error { calls++; return transient },
		func(e error) bool { return errors.Is(e, transient) },
		[]time.Duration{1 * time.Millisecond, 1 * time.Millisecond},
	)
	if !errors.Is(err, transient) {
		t.Fatalf("err = %v, want transient", err)
	}
	if calls != 3 {
		t.Errorf("calls = %d, want 3 (initial + 2 retries)", calls)
	}
}

func TestRunOne_nonRetryableReturnsImmediately(t *testing.T) {
	fatal := errors.New("fatal")
	calls := 0
	err := RunOne(context.Background(),
		func(ctx context.Context) error { calls++; return fatal },
		func(error) bool { return false },
		[]time.Duration{1 * time.Millisecond, 1 * time.Millisecond},
	)
	if !errors.Is(err, fatal) {
		t.Fatalf("err = %v, want fatal", err)
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1", calls)
	}
}

func TestRunOne_emptyScheduleNoRetry(t *testing.T) {
	transient := errors.New("transient")
	calls := 0
	err := RunOne(context.Background(),
		func(ctx context.Context) error { calls++; return transient },
		func(e error) bool { return errors.Is(e, transient) },
		[]time.Duration{},
	)
	if !errors.Is(err, transient) {
		t.Fatalf("err = %v, want transient", err)
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1 (no retries)", calls)
	}
}

func TestRunOne_ctxCancelMidSleep(t *testing.T) {
	transient := errors.New("transient")
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	go func() { time.Sleep(20 * time.Millisecond); cancel() }()
	err := RunOne(ctx,
		func(ctx context.Context) error { calls++; return transient },
		func(e error) bool { return errors.Is(e, transient) },
		[]time.Duration{500 * time.Millisecond, 500 * time.Millisecond},
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1 (cancel during first retry sleep)", calls)
	}
}
```

- [ ] **Step 3: Run new throttle tests, expect PASS**

```bash
go test ./internal/throttle/ -v
```

- [ ] **Step 4: Refactor `internal/lovedtracks/wipe.go`**

Open `internal/lovedtracks/wipe.go` and apply these changes:

1. Add `"github.com/niref/deezer-tools/internal/throttle"` to the import block. Remove `"math/rand/v2"` (no longer used directly).

2. Delete the package-level vars and constant that are now in throttle:

   ```go
   // DELETE:
   var defaultRetryBackoff = []time.Duration{ ... }
   var (
       defaultPace       = time.Second
       defaultPaceJitter = 200 * time.Millisecond
   )
   const defaultMaxConsecutiveFailure = 5
   ```

3. In `Wipe`, replace the defaulting block:

   ```go
   // BEFORE:
   retryBackoff := opts.RetryBackoff
   if retryBackoff == nil {
       retryBackoff = defaultRetryBackoff
   }
   maxConsec := opts.MaxConsecutiveFinalFailures
   if maxConsec == 0 {
       maxConsec = defaultMaxConsecutiveFailure
   }

   // AFTER:
   retryBackoff := opts.RetryBackoff   // throttle.RunOne handles nil
   maxConsec := opts.MaxConsecutiveFinalFailures
   if maxConsec == 0 {
       maxConsec = throttle.DefaultMaxConsecutiveFinalFailures
   }
   ```

4. In the per-track loop, replace the pacedSleep + deleteWithRetry call site:

   ```go
   // BEFORE:
   if err := pacedSleep(ctx, defaultPace, defaultPaceJitter); err != nil {
       res.Elapsed = time.Since(start)
       return res, err
   }
   if err := deleteWithRetry(ctx, gw, s.ID, retryBackoff); err != nil {
       ...
   }

   // AFTER:
   if err := throttle.Sleep(ctx); err != nil {
       res.Elapsed = time.Since(start)
       return res, err
   }
   id := s.ID
   if err := throttle.RunOne(ctx, func(ctx context.Context) error {
       return gw.RemoveFavoriteSong(ctx, id)
   }, gateway.IsRetryable, retryBackoff); err != nil {
       ...
   }
   ```

   Keep the existing branches for ctx-cancel, auth-failure, skip-log append, and breaker increment — those are orchestrator-side and stay in `wipe.go`.

5. Delete the now-orphaned helpers at the bottom of `wipe.go`:

   ```go
   // DELETE: pacedSleep, deleteWithRetry, shouldRetry
   ```

- [ ] **Step 5: Update `internal/lovedtracks/wipe_test.go` init()**

Find the existing `init()` near the top of `wipe_test.go`:

```go
// BEFORE:
func init() {
	defaultPace = 0
	defaultPaceJitter = 0
}

// AFTER:
func init() {
	throttle.Pace = 0
	throttle.Jitter = 0
}
```

Add `"github.com/niref/deezer-tools/internal/throttle"` to the test file's import block.

- [ ] **Step 6: Build**

```bash
go build ./...
```

Expected: clean build. If anything still references `defaultPace`, `pacedSleep`, etc., delete it.

- [ ] **Step 7: Run lovedtracks tests — verification gate**

```bash
go test ./internal/lovedtracks/ -v
```

Expected: all PASS, identical pass count to before the refactor. If any test fails, the refactor went wrong.

- [ ] **Step 8: Run full test suite**

```bash
go test ./...
go vet ./...
```

Expected: all PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/throttle/ internal/lovedtracks/wipe.go internal/lovedtracks/wipe_test.go
git commit -m "refactor(lovedtracks): extract throttle package shared with future tools"
```

---

## Task 5: `internal/gateway/playlists.go` — read playlist songs

**Files:**
- Create: `internal/gateway/playlists.go`
- Create: `internal/gateway/playlists_test.go`

The `playlist.getSongs` method is the only read primitive needed for ingesting the user's playlists. Each song record carries `ALB_ID`, `ALB_TITLE`, `ART_ID`, `ART_NAME` inline — no per-song enrichment call is needed for this tool (unlike loved tracks, which had to enrich after a thin `getFavoriteIds` listing).

Use `callWithCSRF` for transport. Reuse the existing `flexString` type from `tracks.go` (same package; free re-use).

**Method-name caveat:** if Task 2's research surfaces a different method name or parameter shape, update this task's request body and method-name constant accordingly.

- [ ] **Step 1: Write the failing tests in `internal/gateway/playlists_test.go`**

```go
package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestListPlaylistSongs_singlePage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("method") {
		case "deezer.getUserData":
			w.Write([]byte(`{"results":{"checkForm":"tok","USER":{"USER_ID":42}}}`))
		case "playlist.getSongs":
			w.Write([]byte(`{"results":{"data":[
				{"SNG_ID":"1","SNG_TITLE":"Song One","ALB_ID":"100","ALB_TITLE":"Album One","ART_ID":"200","ART_NAME":"Artist One"},
				{"SNG_ID":"2","SNG_TITLE":"Song Two","ALB_ID":"100","ALB_TITLE":"Album One","ART_ID":"201","ART_NAME":"Artist Two"}
			],"total":2}}`))
		default:
			t.Errorf("unexpected method=%s", r.URL.Query().Get("method"))
		}
	}))
	defer srv.Close()

	c := New("test-arl")
	c.baseURL = srv.URL

	got, err := c.ListPlaylistSongs(context.Background(), "9999", 100)
	if err != nil {
		t.Fatalf("ListPlaylistSongs err = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].SongID != "1" || got[0].AlbumID != "100" || got[0].ArtistID != "200" {
		t.Errorf("song[0] = %+v", got[0])
	}
	if got[1].ArtistName != "Artist Two" {
		t.Errorf("song[1].ArtistName = %q", got[1].ArtistName)
	}
}

func TestListPlaylistSongs_paginates(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("method") {
		case "deezer.getUserData":
			w.Write([]byte(`{"results":{"checkForm":"tok","USER":{"USER_ID":42}}}`))
		case "playlist.getSongs":
			calls++
			body, _ := readBody(r)
			var req struct {
				Start int `json:"start"`
				Nb    int `json:"nb"`
			}
			_ = json.Unmarshal(body, &req)
			switch req.Start {
			case 0:
				w.Write([]byte(`{"results":{"data":[
					{"SNG_ID":"1","ALB_ID":"100","ALB_TITLE":"A","ART_ID":"200","ART_NAME":"X"},
					{"SNG_ID":"2","ALB_ID":"100","ALB_TITLE":"A","ART_ID":"200","ART_NAME":"X"}
				],"total":3}}`))
			case 2:
				w.Write([]byte(`{"results":{"data":[
					{"SNG_ID":"3","ALB_ID":"101","ALB_TITLE":"B","ART_ID":"201","ART_NAME":"Y"}
				],"total":3}}`))
			default:
				t.Errorf("unexpected start=%d", req.Start)
			}
		}
	}))
	defer srv.Close()

	c := New("test-arl")
	c.baseURL = srv.URL

	got, err := c.ListPlaylistSongs(context.Background(), "9999", 2)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(got) != 3 {
		t.Errorf("len = %d, want 3", len(got))
	}
	if calls != 2 {
		t.Errorf("calls = %d, want 2", calls)
	}
}

func TestListPlaylistSongs_acceptsNumericIDs(t *testing.T) {
	// gw-light occasionally returns IDs as bare numbers within the same response.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("method") {
		case "deezer.getUserData":
			w.Write([]byte(`{"results":{"checkForm":"tok","USER":{"USER_ID":42}}}`))
		case "playlist.getSongs":
			w.Write([]byte(`{"results":{"data":[
				{"SNG_ID":1,"ALB_ID":100,"ALB_TITLE":"A","ART_ID":200,"ART_NAME":"X"}
			],"total":1}}`))
		}
	}))
	defer srv.Close()

	c := New("test-arl")
	c.baseURL = srv.URL

	got, err := c.ListPlaylistSongs(context.Background(), "9999", 100)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got[0].SongID != "1" || got[0].AlbumID != "100" || got[0].ArtistID != "200" {
		t.Errorf("got = %+v", got[0])
	}
}

// readBody is a small helper used only by tests in this file.
func readBody(r *http.Request) ([]byte, error) {
	defer r.Body.Close()
	var sb strings.Builder
	buf := make([]byte, 1024)
	for {
		n, err := r.Body.Read(buf)
		if n > 0 {
			sb.Write(buf[:n])
		}
		if err != nil {
			break
		}
	}
	return []byte(sb.String()), nil
}
```

If `readBody` already exists elsewhere in the gateway test files (e.g., `tracks_test.go`), reuse the existing one and remove the duplicate from this file.

- [ ] **Step 2: Run, expect FAIL**

```bash
go test ./internal/gateway/ -run TestListPlaylistSongs
```

Expected: undefined `ListPlaylistSongs`.

- [ ] **Step 3: Implement `internal/gateway/playlists.go`**

```go
package gateway

import (
	"context"
	"encoding/json"
	"fmt"
)

// PlaylistSong is one song record from playlist.getSongs, carrying enough
// metadata for the playlistlove tool to dedupe by album and artist without
// a follow-up enrichment call.
type PlaylistSong struct {
	SongID     string
	SongTitle  string
	AlbumID    string
	AlbumTitle string
	ArtistID   string
	ArtistName string
}

const (
	getPlaylistSongsMethod   = "playlist.getSongs"
	getPlaylistSongsPageSize = 200
)

// playlistSongRecord matches the per-song JSON shape returned by playlist.getSongs.
// flexString covers the gw-light habit of returning IDs as either quoted strings
// or bare numbers within the same response.
type playlistSongRecord struct {
	SongID     flexString `json:"SNG_ID"`
	SongTitle  string     `json:"SNG_TITLE"`
	AlbumID    flexString `json:"ALB_ID"`
	AlbumTitle string     `json:"ALB_TITLE"`
	ArtistID   flexString `json:"ART_ID"`
	ArtistName string     `json:"ART_NAME"`
}

// ListPlaylistSongs paginates playlist.getSongs and returns every song in
// the playlist with album- and artist-level metadata sufficient for dedupe.
//
// pageSize <= 0 uses the default (200). Reasonable values are 100–1000.
//
// CSRF acquisition and refresh are handled by callWithCSRF.
func (c *Client) ListPlaylistSongs(ctx context.Context, playlistID string, pageSize int) ([]PlaylistSong, error) {
	if pageSize <= 0 {
		pageSize = getPlaylistSongsPageSize
	}
	if err := c.ensureCSRF(ctx); err != nil {
		return nil, err
	}

	var out []PlaylistSong
	start := 0
	for {
		body := map[string]any{
			"playlist_id": playlistID,
			"nb":          pageSize,
			"start":       start,
			"tab":         "songs",
		}
		raw, err := c.callWithCSRF(ctx, getPlaylistSongsMethod, body)
		if err != nil {
			return nil, fmt.Errorf("%s playlist=%s start=%d: %w", getPlaylistSongsMethod, playlistID, start, err)
		}
		var page struct {
			Data  []playlistSongRecord `json:"data"`
			Total int                  `json:"total"`
		}
		if err := json.Unmarshal(raw, &page); err != nil {
			return nil, fmt.Errorf("decode %s playlist=%s start=%d: %w", getPlaylistSongsMethod, playlistID, start, err)
		}
		if len(page.Data) == 0 {
			break
		}
		for _, r := range page.Data {
			out = append(out, PlaylistSong{
				SongID:     string(r.SongID),
				SongTitle:  r.SongTitle,
				AlbumID:    string(r.AlbumID),
				AlbumTitle: r.AlbumTitle,
				ArtistID:   string(r.ArtistID),
				ArtistName: r.ArtistName,
			})
		}
		start += len(page.Data)
		if page.Total > 0 && start >= page.Total {
			break
		}
	}
	return out, nil
}
```

- [ ] **Step 4: Run, expect PASS**

```bash
go test ./internal/gateway/ -run TestListPlaylistSongs -v
```

- [ ] **Step 5: Run all gateway tests**

```bash
go test ./internal/gateway/
go vet ./internal/gateway/
```

Expected: all PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/gateway/playlists.go internal/gateway/playlists_test.go
git commit -m "feat(gateway): paginate playlist.getSongs"
```

---

## Task 6: `internal/gateway/albums.go` — list and add favorite albums

**Files:**
- Create: `internal/gateway/albums.go`
- Create: `internal/gateway/albums_test.go`

Two methods:
1. `ListFavoriteAlbumIDs(ctx, pageSize int) ([]string, error)` — paginate `album.getFavoriteIds`. The orchestrator only needs IDs for the diff.
2. `AddFavoriteAlbum(ctx, albumID string) error` — `favorite_album.add`.

Same conventions as `tracks.go`: `callWithCSRF`, `flexString` for ID-shaped fields, classified errors.

- [ ] **Step 1: Write failing tests in `internal/gateway/albums_test.go`**

```go
package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestListFavoriteAlbumIDs_paginates(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("method") {
		case "deezer.getUserData":
			w.Write([]byte(`{"results":{"checkForm":"tok","USER":{"USER_ID":42}}}`))
		case "album.getFavoriteIds":
			calls++
			body, _ := readBody(r)
			s := string(body)
			switch {
			case strings.Contains(s, `"start":0`):
				w.Write([]byte(`{"results":{"data":[{"ALB_ID":"1"},{"ALB_ID":"2"}],"total":3}}`))
			case strings.Contains(s, `"start":2`):
				w.Write([]byte(`{"results":{"data":[{"ALB_ID":"3"}],"total":3}}`))
			default:
				t.Errorf("unexpected start in body: %s", s)
			}
		}
	}))
	defer srv.Close()

	c := New("test-arl")
	c.baseURL = srv.URL

	got, err := c.ListFavoriteAlbumIDs(context.Background(), 2)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(got) != 3 || got[0] != "1" || got[2] != "3" {
		t.Errorf("got = %v, want [1 2 3]", got)
	}
	if calls != 2 {
		t.Errorf("calls = %d, want 2", calls)
	}
}

func TestListFavoriteAlbumIDs_acceptsNumericALBID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("method") {
		case "deezer.getUserData":
			w.Write([]byte(`{"results":{"checkForm":"tok","USER":{"USER_ID":42}}}`))
		case "album.getFavoriteIds":
			w.Write([]byte(`{"results":{"data":[{"ALB_ID":42},{"ALB_ID":"43"}],"total":2}}`))
		}
	}))
	defer srv.Close()

	c := New("test-arl")
	c.baseURL = srv.URL

	got, err := c.ListFavoriteAlbumIDs(context.Background(), 100)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(got) != 2 || got[0] != "42" || got[1] != "43" {
		t.Errorf("got = %v", got)
	}
}

func TestAddFavoriteAlbum_success(t *testing.T) {
	var seenALB string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("method") {
		case "deezer.getUserData":
			w.Write([]byte(`{"results":{"checkForm":"tok","USER":{"USER_ID":42}}}`))
		case "favorite_album.add":
			body, _ := readBody(r)
			s := string(body)
			switch {
			case strings.Contains(s, `"ALB_ID":"123"`):
				seenALB = "123"
			}
			w.Write([]byte(`{"results":true}`))
		}
	}))
	defer srv.Close()

	c := New("test-arl")
	c.baseURL = srv.URL

	if err := c.AddFavoriteAlbum(context.Background(), "123"); err != nil {
		t.Fatalf("err = %v", err)
	}
	if seenALB != "123" {
		t.Errorf("server did not see ALB_ID=123 (seen=%q)", seenALB)
	}
}

func TestAddFavoriteAlbum_classifiedError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("method") {
		case "deezer.getUserData":
			w.Write([]byte(`{"results":{"checkForm":"tok","USER":{"USER_ID":42}}}`))
		case "favorite_album.add":
			w.Write([]byte(`{"error":{"QUOTA_ERROR":"Quota exceeded"}}`))
		}
	}))
	defer srv.Close()

	c := New("test-arl")
	c.baseURL = srv.URL

	err := c.AddFavoriteAlbum(context.Background(), "123")
	if err == nil {
		t.Fatal("err = nil, want classified error")
	}
	var ge *GatewayError
	if !asGatewayError(err, &ge) || ge.Kind != ErrRateLimited {
		t.Errorf("err = %v, want ErrRateLimited via QUOTA_ERROR", err)
	}
}

// asGatewayError is a tiny helper bridging errors.As for terse tests.
func asGatewayError(err error, target **GatewayError) bool {
	for e := err; e != nil; {
		if ge, ok := e.(*GatewayError); ok {
			*target = ge
			return true
		}
		u, ok := e.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		e = u.Unwrap()
	}
	return false
}
```

If `asGatewayError` already exists elsewhere in the gateway test files, remove the duplicate from this file.

- [ ] **Step 2: Run, expect FAIL**

```bash
go test ./internal/gateway/ -run "TestListFavoriteAlbumIDs|TestAddFavoriteAlbum"
```

- [ ] **Step 3: Implement `internal/gateway/albums.go`**

```go
package gateway

import (
	"context"
	"encoding/json"
	"fmt"
)

const (
	getFavoriteAlbumIDsMethod = "album.getFavoriteIds"
	addFavoriteAlbumMethod    = "favorite_album.add"
	favoriteAlbumPageSize     = 1000
)

// favoriteAlbumIDRecord is the per-album record shape from album.getFavoriteIds.
type favoriteAlbumIDRecord struct {
	ID flexString `json:"ALB_ID"`
}

// ListFavoriteAlbumIDs paginates album.getFavoriteIds and returns every loved
// album ID for the authenticated user. pageSize <= 0 uses the default.
//
// CSRF acquisition and refresh are handled by callWithCSRF.
func (c *Client) ListFavoriteAlbumIDs(ctx context.Context, pageSize int) ([]string, error) {
	if pageSize <= 0 {
		pageSize = favoriteAlbumPageSize
	}
	if err := c.ensureCSRF(ctx); err != nil {
		return nil, err
	}
	var out []string
	start := 0
	for {
		body := map[string]any{
			"user_id":  c.userID,
			"start":    start,
			"nb":       pageSize,
			"checksum": nil,
		}
		raw, err := c.callWithCSRF(ctx, getFavoriteAlbumIDsMethod, body)
		if err != nil {
			return nil, fmt.Errorf("%s start=%d: %w", getFavoriteAlbumIDsMethod, start, err)
		}
		var page struct {
			Data  []favoriteAlbumIDRecord `json:"data"`
			Total int                     `json:"total"`
		}
		if err := json.Unmarshal(raw, &page); err != nil {
			return nil, fmt.Errorf("decode %s start=%d: %w", getFavoriteAlbumIDsMethod, start, err)
		}
		if len(page.Data) == 0 {
			break
		}
		for _, r := range page.Data {
			out = append(out, string(r.ID))
		}
		start += len(page.Data)
		if page.Total > 0 && start >= page.Total {
			break
		}
	}
	return out, nil
}

// AddFavoriteAlbum loves the album with the given Deezer ALB_ID.
// Idempotent on the gateway side: re-adding an already-loved album is a no-op
// (verified shape recorded in docs/superpowers/research/2026-04-30-deezer-favorites-protocol.md).
//
// Returns a *GatewayError on classified failure.
func (c *Client) AddFavoriteAlbum(ctx context.Context, albumID string) error {
	body := map[string]any{"ALB_ID": albumID}
	if _, err := c.callWithCSRF(ctx, addFavoriteAlbumMethod, body); err != nil {
		return err
	}
	return nil
}
```

- [ ] **Step 4: Run, expect PASS**

```bash
go test ./internal/gateway/ -run "TestListFavoriteAlbumIDs|TestAddFavoriteAlbum" -v
```

- [ ] **Step 5: Full gateway test suite**

```bash
go test ./internal/gateway/
```

- [ ] **Step 6: Commit**

```bash
git add internal/gateway/albums.go internal/gateway/albums_test.go
git commit -m "feat(gateway): list and add favorite albums"
```

---

## Task 7: `internal/gateway/artists.go` — list and add favorite artists

**Files:**
- Create: `internal/gateway/artists.go`
- Create: `internal/gateway/artists_test.go`

Identical structure to Task 6, with `ART_ID` instead of `ALB_ID` and `artist.getFavoriteIds` / `favorite_artist.add` as the methods.

- [ ] **Step 1: Write failing tests in `internal/gateway/artists_test.go`**

```go
package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestListFavoriteArtistIDs_paginates(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("method") {
		case "deezer.getUserData":
			w.Write([]byte(`{"results":{"checkForm":"tok","USER":{"USER_ID":42}}}`))
		case "artist.getFavoriteIds":
			calls++
			body, _ := readBody(r)
			s := string(body)
			switch {
			case strings.Contains(s, `"start":0`):
				w.Write([]byte(`{"results":{"data":[{"ART_ID":"10"},{"ART_ID":"20"}],"total":3}}`))
			case strings.Contains(s, `"start":2`):
				w.Write([]byte(`{"results":{"data":[{"ART_ID":"30"}],"total":3}}`))
			}
		}
	}))
	defer srv.Close()

	c := New("test-arl")
	c.baseURL = srv.URL

	got, err := c.ListFavoriteArtistIDs(context.Background(), 2)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(got) != 3 || got[0] != "10" || got[2] != "30" {
		t.Errorf("got = %v", got)
	}
	if calls != 2 {
		t.Errorf("calls = %d, want 2", calls)
	}
}

func TestListFavoriteArtistIDs_acceptsNumericARTID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("method") {
		case "deezer.getUserData":
			w.Write([]byte(`{"results":{"checkForm":"tok","USER":{"USER_ID":42}}}`))
		case "artist.getFavoriteIds":
			w.Write([]byte(`{"results":{"data":[{"ART_ID":99},{"ART_ID":"100"}],"total":2}}`))
		}
	}))
	defer srv.Close()

	c := New("test-arl")
	c.baseURL = srv.URL

	got, err := c.ListFavoriteArtistIDs(context.Background(), 100)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(got) != 2 || got[0] != "99" || got[1] != "100" {
		t.Errorf("got = %v", got)
	}
}

func TestAddFavoriteArtist_success(t *testing.T) {
	var seenART string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("method") {
		case "deezer.getUserData":
			w.Write([]byte(`{"results":{"checkForm":"tok","USER":{"USER_ID":42}}}`))
		case "favorite_artist.add":
			body, _ := readBody(r)
			if strings.Contains(string(body), `"ART_ID":"500"`) {
				seenART = "500"
			}
			w.Write([]byte(`{"results":true}`))
		}
	}))
	defer srv.Close()

	c := New("test-arl")
	c.baseURL = srv.URL

	if err := c.AddFavoriteArtist(context.Background(), "500"); err != nil {
		t.Fatalf("err = %v", err)
	}
	if seenART != "500" {
		t.Errorf("server did not see ART_ID=500")
	}
}
```

- [ ] **Step 2: Run, expect FAIL**

```bash
go test ./internal/gateway/ -run "TestListFavoriteArtistIDs|TestAddFavoriteArtist"
```

- [ ] **Step 3: Implement `internal/gateway/artists.go`**

```go
package gateway

import (
	"context"
	"encoding/json"
	"fmt"
)

const (
	getFavoriteArtistIDsMethod = "artist.getFavoriteIds"
	addFavoriteArtistMethod    = "favorite_artist.add"
	favoriteArtistPageSize     = 1000
)

type favoriteArtistIDRecord struct {
	ID flexString `json:"ART_ID"`
}

// ListFavoriteArtistIDs paginates artist.getFavoriteIds and returns every
// loved artist ID for the authenticated user. pageSize <= 0 uses the default.
func (c *Client) ListFavoriteArtistIDs(ctx context.Context, pageSize int) ([]string, error) {
	if pageSize <= 0 {
		pageSize = favoriteArtistPageSize
	}
	if err := c.ensureCSRF(ctx); err != nil {
		return nil, err
	}
	var out []string
	start := 0
	for {
		body := map[string]any{
			"user_id":  c.userID,
			"start":    start,
			"nb":       pageSize,
			"checksum": nil,
		}
		raw, err := c.callWithCSRF(ctx, getFavoriteArtistIDsMethod, body)
		if err != nil {
			return nil, fmt.Errorf("%s start=%d: %w", getFavoriteArtistIDsMethod, start, err)
		}
		var page struct {
			Data  []favoriteArtistIDRecord `json:"data"`
			Total int                      `json:"total"`
		}
		if err := json.Unmarshal(raw, &page); err != nil {
			return nil, fmt.Errorf("decode %s start=%d: %w", getFavoriteArtistIDsMethod, start, err)
		}
		if len(page.Data) == 0 {
			break
		}
		for _, r := range page.Data {
			out = append(out, string(r.ID))
		}
		start += len(page.Data)
		if page.Total > 0 && start >= page.Total {
			break
		}
	}
	return out, nil
}

// AddFavoriteArtist loves the artist with the given Deezer ART_ID.
// Idempotent on the gateway side. Returns *GatewayError on classified failure.
func (c *Client) AddFavoriteArtist(ctx context.Context, artistID string) error {
	body := map[string]any{"ART_ID": artistID}
	if _, err := c.callWithCSRF(ctx, addFavoriteArtistMethod, body); err != nil {
		return err
	}
	return nil
}
```

- [ ] **Step 4: Run, expect PASS**

```bash
go test ./internal/gateway/ -run "TestListFavoriteArtistIDs|TestAddFavoriteArtist" -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/gateway/artists.go internal/gateway/artists_test.go
git commit -m "feat(gateway): list and add favorite artists"
```

---

## Task 8: `internal/playlistlove/input.go` — parse and normalize playlist inputs

**Files:**
- Create: `internal/playlistlove/input.go`
- Create: `internal/playlistlove/input_test.go`

Three accepted forms per input string, all normalized to a numeric playlist ID. Numeric and long-URL forms are pure parsing. Short share links (`https://link.deezer.com/s/<token>`) require an HTTP `GET` with `CheckRedirect = http.ErrUseLastResponse` so we read the `Location` header from the 30x without fetching the page.

The resolver is a function value (`ResolveShareLink`), making tests trivially mockable.

A small `ReadStdinInputs` helper reads one playlist per line, ignoring blanks and `#` comments.

- [ ] **Step 1: Write failing tests in `internal/playlistlove/input_test.go`**

```go
package playlistlove

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestNormalizeInputs_bareNumeric(t *testing.T) {
	got, errs := NormalizeInputs(context.Background(), []string{"15018766163"}, nil)
	if len(errs) != 0 {
		t.Fatalf("errs = %v", errs)
	}
	if len(got) != 1 || got[0].PlaylistID != "15018766163" {
		t.Errorf("got = %+v", got)
	}
}

func TestNormalizeInputs_longURL(t *testing.T) {
	urls := []string{
		"https://www.deezer.com/en/playlist/15018766163",
		"https://www.deezer.com/playlist/15018766163",
		"https://www.deezer.com/en/playlist/15018766163/",
		"https://www.deezer.com/en/playlist/15018766163?utm_source=foo",
	}
	for _, u := range urls {
		t.Run(u, func(t *testing.T) {
			got, errs := NormalizeInputs(context.Background(), []string{u}, nil)
			if len(errs) != 0 {
				t.Fatalf("errs = %v", errs)
			}
			if len(got) != 1 || got[0].PlaylistID != "15018766163" {
				t.Errorf("got = %+v", got)
			}
		})
	}
}

func TestNormalizeInputs_shareLink(t *testing.T) {
	resolver := func(ctx context.Context, link string) (string, error) {
		if link != "https://link.deezer.com/s/abc123" {
			t.Errorf("resolver called with %q", link)
		}
		return "https://www.deezer.com/playlist/15018766163", nil
	}
	got, errs := NormalizeInputs(context.Background(), []string{"https://link.deezer.com/s/abc123"}, resolver)
	if len(errs) != 0 {
		t.Fatalf("errs = %v", errs)
	}
	if len(got) != 1 || got[0].PlaylistID != "15018766163" {
		t.Errorf("got = %+v", got)
	}
}

func TestNormalizeInputs_dedupeByPlaylistID(t *testing.T) {
	// same playlist via numeric, long URL, short link
	resolver := func(ctx context.Context, _ string) (string, error) {
		return "https://www.deezer.com/playlist/123", nil
	}
	inputs := []string{
		"123",
		"https://www.deezer.com/playlist/123",
		"https://link.deezer.com/s/anything",
	}
	got, errs := NormalizeInputs(context.Background(), inputs, resolver)
	if len(errs) != 0 {
		t.Fatalf("errs = %v", errs)
	}
	if len(got) != 1 {
		t.Errorf("len = %d, want 1 (deduped)", len(got))
	}
}

func TestNormalizeInputs_invalidStringReturnsError(t *testing.T) {
	got, errs := NormalizeInputs(context.Background(), []string{"not-a-playlist"}, nil)
	if len(got) != 0 {
		t.Errorf("got = %+v, want none", got)
	}
	if len(errs) != 1 {
		t.Fatalf("errs len = %d, want 1", len(errs))
	}
	if !strings.Contains(errs[0].Reason, "unrecognized") {
		t.Errorf("reason = %q", errs[0].Reason)
	}
}

func TestNormalizeInputs_resolverErrorBecomesInputError(t *testing.T) {
	boom := errors.New("network down")
	resolver := func(ctx context.Context, _ string) (string, error) { return "", boom }
	got, errs := NormalizeInputs(context.Background(), []string{"https://link.deezer.com/s/x"}, resolver)
	if len(got) != 0 {
		t.Errorf("got = %+v", got)
	}
	if len(errs) != 1 || !strings.Contains(errs[0].Reason, "network down") {
		t.Errorf("errs = %+v", errs)
	}
}

func TestNormalizeInputs_shortLinkWithNilResolverFails(t *testing.T) {
	got, errs := NormalizeInputs(context.Background(), []string{"https://link.deezer.com/s/abc"}, nil)
	if len(got) != 0 || len(errs) != 1 {
		t.Errorf("got=%v errs=%v", got, errs)
	}
}

func TestReadStdinInputs(t *testing.T) {
	in := strings.NewReader(strings.Join([]string{
		"123",
		"# a comment",
		"",
		"https://www.deezer.com/playlist/456  ",
		"  ",
		"https://link.deezer.com/s/abc",
	}, "\n"))
	lines, err := ReadStdinInputs(in)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	want := []string{"123", "https://www.deezer.com/playlist/456", "https://link.deezer.com/s/abc"}
	if len(lines) != len(want) {
		t.Fatalf("len = %d, want %d (got %v)", len(lines), len(want), lines)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Errorf("line[%d] = %q, want %q", i, lines[i], want[i])
		}
	}
}
```

- [ ] **Step 2: Run, expect FAIL**

```bash
go test ./internal/playlistlove/ -run "TestNormalizeInputs|TestReadStdinInputs"
```

Expected: package not found / undefined.

- [ ] **Step 3: Implement `internal/playlistlove/input.go`**

```go
// Package playlistlove orchestrates the "love-contents" run: read N playlists,
// dedupe to unique albums and artists, diff against the user's loved sets,
// confirm, and apply paced add-to-favorites calls. It depends on
// internal/gateway via a narrow Gateway interface and on internal/throttle
// for the shared pacer / retry discipline.
package playlistlove

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
)

// Input is a normalized playlist input: the original raw form plus the
// extracted numeric playlist ID.
type Input struct {
	Raw        string
	PlaylistID string
}

// InputError describes a single input that failed to normalize.
type InputError struct {
	Raw    string
	Reason string
}

func (e InputError) Error() string { return fmt.Sprintf("%s: %s", e.Raw, e.Reason) }

// ResolveShareLink follows a Deezer short-link redirect (link.deezer.com/s/<token>)
// to the canonical URL. Decoupled as a function value so tests don't need a
// real HTTP server.
type ResolveShareLink func(ctx context.Context, link string) (string, error)

var (
	bareNumericRE  = regexp.MustCompile(`^\d+$`)
	longURLRE      = regexp.MustCompile(`(?i)^https?://(?:www\.)?deezer\.com/(?:[a-z]{2}/)?playlist/(\d+)`)
	shortLinkRE    = regexp.MustCompile(`(?i)^https?://link\.deezer\.com/s/[A-Za-z0-9]+`)
)

// NormalizeInputs parses each raw input into Input{Raw, PlaylistID}, deduping
// the result by PlaylistID. Inputs that fail (bad format, network error on a
// short-link resolve) appear in the second return; they don't poison
// successful inputs.
//
// resolver is consulted only for short share links. If a short link is given
// and resolver is nil, that input fails with "no resolver".
func NormalizeInputs(ctx context.Context, raws []string, resolver ResolveShareLink) ([]Input, []InputError) {
	var ok []Input
	var bad []InputError
	seen := make(map[string]bool)

	for _, raw := range raws {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		id, err := extractPlaylistID(ctx, raw, resolver)
		if err != nil {
			bad = append(bad, InputError{Raw: raw, Reason: err.Error()})
			continue
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		ok = append(ok, Input{Raw: raw, PlaylistID: id})
	}
	return ok, bad
}

func extractPlaylistID(ctx context.Context, raw string, resolver ResolveShareLink) (string, error) {
	switch {
	case bareNumericRE.MatchString(raw):
		return raw, nil
	case longURLRE.MatchString(raw):
		m := longURLRE.FindStringSubmatch(raw)
		return m[1], nil
	case shortLinkRE.MatchString(raw):
		if resolver == nil {
			return "", fmt.Errorf("short link given but no resolver: %s", raw)
		}
		canonical, err := resolver(ctx, raw)
		if err != nil {
			return "", err
		}
		m := longURLRE.FindStringSubmatch(canonical)
		if m == nil {
			return "", fmt.Errorf("short link resolved to unrecognized URL: %s", canonical)
		}
		return m[1], nil
	default:
		return "", fmt.Errorf("unrecognized playlist input")
	}
}

// DefaultShareLinkResolver returns a ResolveShareLink backed by the given
// HTTP client. It issues a GET with CheckRedirect = http.ErrUseLastResponse,
// reads the Location header from the resulting 30x, and returns it.
//
// client may be nil; nil uses http.DefaultClient with redirects suppressed.
func DefaultShareLinkResolver(client *http.Client) ResolveShareLink {
	return func(ctx context.Context, link string) (string, error) {
		c := client
		if c == nil {
			c = &http.Client{}
		}
		// Copy the client to avoid mutating the caller's CheckRedirect.
		cc := *c
		cc.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, link, nil)
		if err != nil {
			return "", err
		}
		resp, err := cc.Do(req)
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		loc := resp.Header.Get("Location")
		if loc == "" {
			return "", fmt.Errorf("no Location header from %s (status=%d)", link, resp.StatusCode)
		}
		return loc, nil
	}
}

// ReadStdinInputs reads one playlist input per line from r. Blank lines and
// lines beginning with `#` (after trimming whitespace) are ignored.
func ReadStdinInputs(r io.Reader) ([]string, error) {
	var out []string
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
```

- [ ] **Step 4: Run, expect PASS**

```bash
go test ./internal/playlistlove/ -run "TestNormalizeInputs|TestReadStdinInputs" -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/playlistlove/input.go internal/playlistlove/input_test.go
git commit -m "feat(playlistlove): parse and normalize playlist inputs"
```

---

## Task 9: `internal/playlistlove/diff.go` — aggregate, dedupe, and diff

**Files:**
- Create: `internal/playlistlove/diff.go`
- Create: `internal/playlistlove/diff_test.go`

Pure functions, no I/O. Take `[]gateway.PlaylistSong` plus the loved-album / loved-artist ID slices, produce the dedupe sets and the diff plan.

The Various-Artists `ART_ID` filter happens here. Default value is `"5080"` (research-confirmed in Task 2). Configurable so a future `Options.VariousArtistsID` (or test) can override.

**Contingency from T2 research:** if Task 2 finds that the Various-Artists `ART_ID` is unstable across compilations (e.g., per-region IDs or multiple values), replace the `variousArtistsID string` parameter with a predicate `func(gateway.PlaylistSong) bool` that returns true on either a matching `ART_ID` *or* `ART_NAME == "Various Artists"` (case-insensitive). The spec calls out this name-match fallback explicitly. Update the test cases to drive the predicate path, and surface the `VariousArtistsSkipped` count exactly as before.

Songs with empty / zero `ALB_ID` or `ART_ID` are counted under `UnparseableSongs` and dropped. They don't fail the run.

- [ ] **Step 1: Write failing tests in `internal/playlistlove/diff_test.go`**

```go
package playlistlove

import (
	"reflect"
	"sort"
	"testing"

	"github.com/niref/deezer-tools/internal/gateway"
)

func TestAggregate_dedupesByID(t *testing.T) {
	songs := []gateway.PlaylistSong{
		{SongID: "1", AlbumID: "100", AlbumTitle: "A1", ArtistID: "10", ArtistName: "X"},
		{SongID: "2", AlbumID: "100", AlbumTitle: "A1", ArtistID: "10", ArtistName: "X"},
		{SongID: "3", AlbumID: "101", AlbumTitle: "A2", ArtistID: "11", ArtistName: "Y"},
	}
	got := Aggregate(songs, "5080")
	if len(got.Albums) != 2 {
		t.Errorf("albums = %d, want 2 (got %+v)", len(got.Albums), got.Albums)
	}
	if len(got.Artists) != 2 {
		t.Errorf("artists = %d, want 2", len(got.Artists))
	}
	if got.UnparseableSongs != 0 {
		t.Errorf("unparseable = %d, want 0", got.UnparseableSongs)
	}
	if got.VariousArtistsSkipped != 0 {
		t.Errorf("VA skipped = %d, want 0", got.VariousArtistsSkipped)
	}
}

func TestAggregate_dropsVariousArtists(t *testing.T) {
	songs := []gateway.PlaylistSong{
		{SongID: "1", AlbumID: "100", AlbumTitle: "Comp", ArtistID: "5080", ArtistName: "Various Artists"},
		{SongID: "2", AlbumID: "100", AlbumTitle: "Comp", ArtistID: "5080", ArtistName: "Various Artists"},
		{SongID: "3", AlbumID: "101", AlbumTitle: "Real", ArtistID: "11", ArtistName: "Y"},
	}
	got := Aggregate(songs, "5080")
	if len(got.Albums) != 2 {
		t.Errorf("albums = %d, want 2 (compilations are still loved)", len(got.Albums))
	}
	if len(got.Artists) != 1 || got.Artists[0].ID != "11" {
		t.Errorf("artists = %+v, want [{11, Y}]", got.Artists)
	}
	if got.VariousArtistsSkipped != 2 {
		t.Errorf("VA skipped = %d, want 2 (per-song count)", got.VariousArtistsSkipped)
	}
}

func TestAggregate_countsUnparseableAndDoesNotEmit(t *testing.T) {
	songs := []gateway.PlaylistSong{
		{SongID: "1", AlbumID: "", ArtistID: "10", ArtistName: "X"},
		{SongID: "2", AlbumID: "100", ArtistID: "", ArtistName: ""},
		{SongID: "3", AlbumID: "0", ArtistID: "0", ArtistName: ""},
		{SongID: "4", AlbumID: "100", AlbumTitle: "A", ArtistID: "10", ArtistName: "X"},
	}
	got := Aggregate(songs, "5080")
	if len(got.Albums) != 1 || got.Albums[0].ID != "100" {
		t.Errorf("albums = %+v", got.Albums)
	}
	if len(got.Artists) != 1 || got.Artists[0].ID != "10" {
		t.Errorf("artists = %+v", got.Artists)
	}
	if got.UnparseableSongs != 3 {
		t.Errorf("unparseable = %d, want 3", got.UnparseableSongs)
	}
}

func TestDiff_subtractsLovedSets(t *testing.T) {
	set := AggregatedSet{
		Albums:  []Album{{ID: "100", Title: "A1"}, {ID: "101", Title: "A2"}, {ID: "102", Title: "A3"}},
		Artists: []Artist{{ID: "10", Name: "X"}, {ID: "11", Name: "Y"}},
	}
	loved := DiffInputs{
		LovedAlbumIDs:  []string{"101"},
		LovedArtistIDs: []string{"10"},
	}
	got := Diff(set, loved)
	sortAlbums := func(a []Album) { sort.Slice(a, func(i, j int) bool { return a[i].ID < a[j].ID }) }
	sortArtists := func(a []Artist) { sort.Slice(a, func(i, j int) bool { return a[i].ID < a[j].ID }) }
	sortAlbums(got.AlbumsToAdd)
	sortArtists(got.ArtistsToAdd)
	wantAlbums := []Album{{ID: "100", Title: "A1"}, {ID: "102", Title: "A3"}}
	wantArtists := []Artist{{ID: "11", Name: "Y"}}
	if !reflect.DeepEqual(got.AlbumsToAdd, wantAlbums) {
		t.Errorf("albumsToAdd = %+v, want %+v", got.AlbumsToAdd, wantAlbums)
	}
	if !reflect.DeepEqual(got.ArtistsToAdd, wantArtists) {
		t.Errorf("artistsToAdd = %+v, want %+v", got.ArtistsToAdd, wantArtists)
	}
	if got.AlbumsAlreadyLoved != 1 {
		t.Errorf("AlbumsAlreadyLoved = %d, want 1", got.AlbumsAlreadyLoved)
	}
	if got.ArtistsAlreadyLoved != 1 {
		t.Errorf("ArtistsAlreadyLoved = %d, want 1", got.ArtistsAlreadyLoved)
	}
}

func TestDiff_emptyLovedSetsMeansAllToAdd(t *testing.T) {
	set := AggregatedSet{
		Albums:  []Album{{ID: "100"}, {ID: "101"}},
		Artists: []Artist{{ID: "10"}},
	}
	got := Diff(set, DiffInputs{})
	if len(got.AlbumsToAdd) != 2 {
		t.Errorf("albumsToAdd = %d, want 2", len(got.AlbumsToAdd))
	}
	if len(got.ArtistsToAdd) != 1 {
		t.Errorf("artistsToAdd = %d, want 1", len(got.ArtistsToAdd))
	}
}
```

- [ ] **Step 2: Run, expect FAIL**

```bash
go test ./internal/playlistlove/ -run "TestAggregate|TestDiff"
```

- [ ] **Step 3: Implement `internal/playlistlove/diff.go`**

```go
package playlistlove

import (
	"github.com/niref/deezer-tools/internal/gateway"
)

// Album is a deduped (ALB_ID, ALB_TITLE, primary ART_NAME) record for the
// diff plan. Artist is a deduped (ART_ID, ART_NAME) record.
type Album struct {
	ID     string
	Title  string
	Artist string
}

type Artist struct {
	ID   string
	Name string
}

// AggregatedSet is the dedupe output: unique albums and artists across all
// songs, with per-cohort counts surfaced for the run summary.
type AggregatedSet struct {
	Albums                []Album
	Artists               []Artist
	UnparseableSongs      int
	VariousArtistsSkipped int
}

// DefaultVariousArtistsID is the ART_ID that gw-light emits for compilation
// "Various Artists" entries. Verified against deemix/deezer-py in the
// research doc dated 2026-04-30. Configurable on Options for override.
const DefaultVariousArtistsID = "5080"

// Aggregate dedupes the songs by ALB_ID and ART_ID, dropping the
// Various-Artists pseudo-ART_ID at the artist level. Songs with empty/zero
// ALB_ID or ART_ID are counted under UnparseableSongs and don't appear in
// the output sets.
func Aggregate(songs []gateway.PlaylistSong, variousArtistsID string) AggregatedSet {
	if variousArtistsID == "" {
		variousArtistsID = DefaultVariousArtistsID
	}
	albums := make(map[string]Album)
	artists := make(map[string]Artist)
	var set AggregatedSet
	for _, s := range songs {
		albID := s.AlbumID
		artID := s.ArtistID
		if albID == "" || albID == "0" || artID == "" || artID == "0" {
			set.UnparseableSongs++
			continue
		}
		if _, ok := albums[albID]; !ok {
			albums[albID] = Album{ID: albID, Title: s.AlbumTitle, Artist: s.ArtistName}
		}
		if artID == variousArtistsID {
			set.VariousArtistsSkipped++
			continue
		}
		if _, ok := artists[artID]; !ok {
			artists[artID] = Artist{ID: artID, Name: s.ArtistName}
		}
	}
	set.Albums = make([]Album, 0, len(albums))
	for _, a := range albums {
		set.Albums = append(set.Albums, a)
	}
	set.Artists = make([]Artist, 0, len(artists))
	for _, a := range artists {
		set.Artists = append(set.Artists, a)
	}
	return set
}

// DiffInputs carries the user's current loved-album and loved-artist IDs.
type DiffInputs struct {
	LovedAlbumIDs  []string
	LovedArtistIDs []string
}

// DiffPlan is the result of subtracting the loved sets from AggregatedSet.
type DiffPlan struct {
	AlbumsToAdd         []Album
	ArtistsToAdd        []Artist
	AlbumsAlreadyLoved  int
	ArtistsAlreadyLoved int
}

// Diff returns AlbumsToAdd / ArtistsToAdd: items in set whose IDs are not
// already in the corresponding loved-ID slice.
func Diff(set AggregatedSet, loved DiffInputs) DiffPlan {
	lovedAlb := make(map[string]bool, len(loved.LovedAlbumIDs))
	for _, id := range loved.LovedAlbumIDs {
		lovedAlb[id] = true
	}
	lovedArt := make(map[string]bool, len(loved.LovedArtistIDs))
	for _, id := range loved.LovedArtistIDs {
		lovedArt[id] = true
	}
	var plan DiffPlan
	for _, a := range set.Albums {
		if lovedAlb[a.ID] {
			plan.AlbumsAlreadyLoved++
			continue
		}
		plan.AlbumsToAdd = append(plan.AlbumsToAdd, a)
	}
	for _, a := range set.Artists {
		if lovedArt[a.ID] {
			plan.ArtistsAlreadyLoved++
			continue
		}
		plan.ArtistsToAdd = append(plan.ArtistsToAdd, a)
	}
	return plan
}
```

- [ ] **Step 4: Run, expect PASS**

```bash
go test ./internal/playlistlove/ -run "TestAggregate|TestDiff" -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/playlistlove/diff.go internal/playlistlove/diff_test.go
git commit -m "feat(playlistlove): aggregate, dedupe, and diff"
```

---

## Task 10: `internal/playlistlove/run.go` — orchestration

**Files:**
- Create: `internal/playlistlove/run.go`
- Create: `internal/playlistlove/run_test.go`

The orchestration layer ties everything together. Follows the spec's `love-contents` flow: normalize inputs → load each playlist → partial-input prompt → aggregate + dedupe → load loved sets → diff → write run-record → empty-diff short-circuit → dry-run short-circuit → confirm → apply phase A (albums) → apply phase B (artists) → final summary.

`Gateway` is a narrow interface that the runtime gets via `gateway.Client` and tests fake.

The `/dev/tty` fallback is hidden behind an `OpenTTY` field (nil → `os.Open("/dev/tty")`) so the path is testable.

The breaker counter is shared across phase A and phase B (per spec).

This is the largest task. Steps are grouped: (1) write the runtime types and a fake gateway in the test file, (2) write tests covering the major branches, (3) write `run.go`, (4) iterate until tests pass.

- [ ] **Step 1: Write `internal/playlistlove/run_test.go` with the fake gateway and test scaffolding**

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
	"github.com/niref/deezer-tools/internal/throttle"
)

// fakeGateway implements Gateway for tests.
type fakeGateway struct {
	playlistSongs       map[string][]gateway.PlaylistSong
	playlistErrs        map[string]error
	lovedAlbumIDs       []string
	lovedArtistIDs      []string
	addAlbumErrs        map[string]error
	addArtistErrs       map[string]error
	addedAlbums         []string
	addedArtists        []string
	listLovedAlbumsErr  error
	listLovedArtistsErr error
}

func (f *fakeGateway) ListPlaylistSongs(ctx context.Context, id string, _ int) ([]gateway.PlaylistSong, error) {
	if err := f.playlistErrs[id]; err != nil {
		return nil, err
	}
	return f.playlistSongs[id], nil
}

func (f *fakeGateway) ListFavoriteAlbumIDs(ctx context.Context, _ int) ([]string, error) {
	if f.listLovedAlbumsErr != nil {
		return nil, f.listLovedAlbumsErr
	}
	return f.lovedAlbumIDs, nil
}

func (f *fakeGateway) ListFavoriteArtistIDs(ctx context.Context, _ int) ([]string, error) {
	if f.listLovedArtistsErr != nil {
		return nil, f.listLovedArtistsErr
	}
	return f.lovedArtistIDs, nil
}

func (f *fakeGateway) AddFavoriteAlbum(ctx context.Context, id string) error {
	if err := f.addAlbumErrs[id]; err != nil {
		return err
	}
	f.addedAlbums = append(f.addedAlbums, id)
	return nil
}

func (f *fakeGateway) AddFavoriteArtist(ctx context.Context, id string) error {
	if err := f.addArtistErrs[id]; err != nil {
		return err
	}
	f.addedArtists = append(f.addedArtists, id)
	return nil
}

// helpers

func tmpBackupDir(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}

func defaultOpts(stdin string, dir string, inputs ...string) Options {
	return Options{
		Inputs:    inputs,
		BackupDir: dir,
		Stdin:     strings.NewReader(stdin),
		Stdout:    &bytes.Buffer{},
		Stderr:    &bytes.Buffer{},
		// Disable retries and pacing for fast tests; pacer vars are zeroed
		// in init() above (they live in throttle).
		RetryBackoff: []time.Duration{},
	}
}

func init() {
	// Zero the throttle pacer so apply tests don't sleep.
	throttle.Pace = 0
	throttle.Jitter = 0
}
```

> **Note:** the `defaultOpts` helper above is a starting point — extend per-test as needed (different inputs, different stdin, OpenTTY override, etc.).

- [ ] **Step 2: Add the apply-path test cases to `run_test.go`**

Append to `run_test.go`:

```go
func TestRun_emptyDiffExits0(t *testing.T) {
	dir := tmpBackupDir(t)
	gw := &fakeGateway{
		playlistSongs: map[string][]gateway.PlaylistSong{
			"1": {{SongID: "s1", AlbumID: "100", AlbumTitle: "A", ArtistID: "10", ArtistName: "X"}},
		},
		lovedAlbumIDs:  []string{"100"},
		lovedArtistIDs: []string{"10"},
	}
	res, err := Run(context.Background(), gw, defaultOpts("yes\n", dir, "1"))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if res.AddedAlbums != 0 || res.AddedArtists != 0 {
		t.Errorf("added = %d/%d, want 0/0", res.AddedAlbums, res.AddedArtists)
	}
	if len(gw.addedAlbums) != 0 || len(gw.addedArtists) != 0 {
		t.Errorf("gateway calls happened: %v / %v", gw.addedAlbums, gw.addedArtists)
	}
}

func TestRun_dryRunWritesRecordButDoesNotApply(t *testing.T) {
	dir := tmpBackupDir(t)
	gw := &fakeGateway{
		playlistSongs: map[string][]gateway.PlaylistSong{
			"1": {{SongID: "s1", AlbumID: "100", AlbumTitle: "A", ArtistID: "10", ArtistName: "X"}},
		},
	}
	opts := defaultOpts("", dir, "1")
	opts.DryRun = true
	res, err := Run(context.Background(), gw, opts)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(gw.addedAlbums) != 0 || len(gw.addedArtists) != 0 {
		t.Errorf("dry-run made gateway add calls")
	}
	if res.RunRecordPath == "" {
		t.Error("RunRecordPath empty in dry-run")
	}
	if _, err := os.Stat(res.RunRecordPath); err != nil {
		t.Errorf("run record not written: %v", err)
	}
}

func TestRun_appliesAlbumsThenArtistsOnYes(t *testing.T) {
	dir := tmpBackupDir(t)
	gw := &fakeGateway{
		playlistSongs: map[string][]gateway.PlaylistSong{
			"1": {
				{SongID: "s1", AlbumID: "100", AlbumTitle: "A1", ArtistID: "10", ArtistName: "X"},
				{SongID: "s2", AlbumID: "101", AlbumTitle: "A2", ArtistID: "11", ArtistName: "Y"},
			},
		},
	}
	res, err := Run(context.Background(), gw, defaultOpts("yes\n", dir, "1"))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(gw.addedAlbums) != 2 {
		t.Errorf("addedAlbums = %v, want 2", gw.addedAlbums)
	}
	if len(gw.addedArtists) != 2 {
		t.Errorf("addedArtists = %v, want 2", gw.addedArtists)
	}
	if res.AddedAlbums != 2 || res.AddedArtists != 2 {
		t.Errorf("res counts = %d/%d", res.AddedAlbums, res.AddedArtists)
	}
}

func TestRun_abortsOnNonYes(t *testing.T) {
	dir := tmpBackupDir(t)
	gw := &fakeGateway{
		playlistSongs: map[string][]gateway.PlaylistSong{
			"1": {{SongID: "s1", AlbumID: "100", AlbumTitle: "A", ArtistID: "10", ArtistName: "X"}},
		},
	}
	_, err := Run(context.Background(), gw, defaultOpts("no\n", dir, "1"))
	if !errors.Is(err, ErrAborted) {
		t.Fatalf("err = %v, want ErrAborted", err)
	}
	if len(gw.addedAlbums) != 0 || len(gw.addedArtists) != 0 {
		t.Errorf("gateway add calls happened despite abort")
	}
}

func TestRun_perItemFailureGoesToSkipLog(t *testing.T) {
	dir := tmpBackupDir(t)
	gw := &fakeGateway{
		playlistSongs: map[string][]gateway.PlaylistSong{
			"1": {
				{SongID: "s1", AlbumID: "100", AlbumTitle: "A1", ArtistID: "10", ArtistName: "X"},
				{SongID: "s2", AlbumID: "101", AlbumTitle: "A2", ArtistID: "11", ArtistName: "Y"},
			},
		},
		addAlbumErrs: map[string]error{
			"101": &gateway.GatewayError{Kind: gateway.ErrNotFound, Method: "favorite_album.add", Message: "DATA_ERROR"},
		},
	}
	res, err := Run(context.Background(), gw, defaultOpts("yes\n", dir, "1"))
	if err == nil {
		t.Fatal("err = nil, want non-nil due to skip")
	}
	if res.AddedAlbums != 1 {
		t.Errorf("addedAlbums = %d, want 1", res.AddedAlbums)
	}
	if res.SkippedItems != 1 {
		t.Errorf("skipped = %d, want 1", res.SkippedItems)
	}
	if _, statErr := os.Stat(res.SkipLogPath); statErr != nil {
		t.Errorf("skip log not written: %v", statErr)
	}
}

func TestRun_authFailureDuringApplyAborts(t *testing.T) {
	dir := tmpBackupDir(t)
	gw := &fakeGateway{
		playlistSongs: map[string][]gateway.PlaylistSong{
			"1": {{SongID: "s1", AlbumID: "100", AlbumTitle: "A", ArtistID: "10", ArtistName: "X"}},
		},
		addAlbumErrs: map[string]error{
			"100": &gateway.GatewayError{Kind: gateway.ErrAuthFailed, Method: "favorite_album.add", Message: "USER_AUTH_REQUIRED"},
		},
	}
	_, err := Run(context.Background(), gw, defaultOpts("yes\n", dir, "1"))
	if err == nil || !strings.Contains(err.Error(), "auth failed") {
		t.Fatalf("err = %v, want auth-failed wrapped", err)
	}
}

func TestRun_breakerTripsAfterNConsecutiveFailures(t *testing.T) {
	dir := tmpBackupDir(t)
	transient := &gateway.GatewayError{Kind: gateway.ErrServerError, Method: "favorite_album.add", Message: "500"}
	gw := &fakeGateway{
		playlistSongs: map[string][]gateway.PlaylistSong{
			"1": {
				{SongID: "s1", AlbumID: "100", ArtistID: "10", ArtistName: "X"},
				{SongID: "s2", AlbumID: "101", ArtistID: "11", ArtistName: "Y"},
				{SongID: "s3", AlbumID: "102", ArtistID: "12", ArtistName: "Z"},
				{SongID: "s4", AlbumID: "103", ArtistID: "13", ArtistName: "W"},
			},
		},
		addAlbumErrs: map[string]error{
			"100": transient, "101": transient, "102": transient, "103": transient,
		},
	}
	opts := defaultOpts("yes\n", dir, "1")
	opts.MaxConsecutiveFinalFailures = 2
	_, err := Run(context.Background(), gw, opts)
	if err == nil || !strings.Contains(err.Error(), "consecutive") {
		t.Fatalf("err = %v, want breaker abort", err)
	}
	if len(gw.addedAlbums) != 0 {
		t.Errorf("addedAlbums = %v, want 0", gw.addedAlbums)
	}
}

func TestRun_partialPlaylistLoadPromptsAndProceedsOnYes(t *testing.T) {
	dir := tmpBackupDir(t)
	gw := &fakeGateway{
		playlistSongs: map[string][]gateway.PlaylistSong{
			"1": {{SongID: "s1", AlbumID: "100", AlbumTitle: "A", ArtistID: "10", ArtistName: "X"}},
		},
		playlistErrs: map[string]error{
			"2": &gateway.GatewayError{Kind: gateway.ErrNotFound, Method: "playlist.getSongs", Message: "DATA_ERROR"},
		},
	}
	// First "yes" answers the partial-input prompt; second "yes" answers the apply confirm.
	res, err := Run(context.Background(), gw, defaultOpts("yes\nyes\n", dir, "1", "2"))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if res.AddedAlbums != 1 || res.AddedArtists != 1 {
		t.Errorf("added = %d/%d", res.AddedAlbums, res.AddedArtists)
	}
}

func TestRun_partialPlaylistLoadAbortsOnNo(t *testing.T) {
	dir := tmpBackupDir(t)
	gw := &fakeGateway{
		playlistSongs: map[string][]gateway.PlaylistSong{
			"1": {{SongID: "s1", AlbumID: "100", AlbumTitle: "A", ArtistID: "10", ArtistName: "X"}},
		},
		playlistErrs: map[string]error{
			"2": &gateway.GatewayError{Kind: gateway.ErrNotFound, Method: "playlist.getSongs", Message: "DATA_ERROR"},
		},
	}
	_, err := Run(context.Background(), gw, defaultOpts("no\n", dir, "1", "2"))
	if !errors.Is(err, ErrAborted) {
		t.Fatalf("err = %v, want ErrAborted", err)
	}
}

func TestRun_runRecordContainsExpectedShape(t *testing.T) {
	dir := tmpBackupDir(t)
	gw := &fakeGateway{
		playlistSongs: map[string][]gateway.PlaylistSong{
			"1": {
				{SongID: "s1", AlbumID: "100", AlbumTitle: "A", ArtistID: "10", ArtistName: "X"},
				{SongID: "s2", AlbumID: "100", AlbumTitle: "A", ArtistID: "5080", ArtistName: "Various Artists"},
			},
		},
	}
	res, err := Run(context.Background(), gw, defaultOpts("yes\n", dir, "1"))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	body, err := os.ReadFile(res.RunRecordPath)
	if err != nil {
		t.Fatalf("read run record: %v", err)
	}
	s := string(body)
	for _, want := range []string{`"version": 1`, `"albums_to_add"`, `"artists_to_add"`, `"various_artists_skipped": 1`} {
		if !strings.Contains(s, want) {
			t.Errorf("run record missing %q", want)
		}
	}
	// Permission check: 0600
	info, _ := os.Stat(res.RunRecordPath)
	if info.Mode().Perm() != 0o600 {
		t.Errorf("perm = %v, want 0600", info.Mode().Perm())
	}
	// Lives under dir
	if filepath.Dir(res.RunRecordPath) != dir {
		t.Errorf("record at %s, expected under %s", res.RunRecordPath, dir)
	}
}
```

> Some of these tests assume helper / type names (`Run`, `Options`, `Result`, `Gateway`, `ErrAborted`, `RunRecordPath`, `SkipLogPath`, `AddedAlbums`, `AddedArtists`, `SkippedItems`) defined in `run.go` below. They will compile once Step 4 lands.

- [ ] **Step 3: Run, expect FAIL**

```bash
go test ./internal/playlistlove/ -run TestRun
```

Expected: undefined `Run` / `Options` / etc.

- [ ] **Step 4: Implement `internal/playlistlove/run.go`**

```go
package playlistlove

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/niref/deezer-tools/internal/gateway"
	"github.com/niref/deezer-tools/internal/throttle"
)

// ErrAborted is returned when the user declines a confirm prompt.
var ErrAborted = errors.New("playlistlove: aborted by user")

// Gateway is the slice of internal/gateway.Client used by Run. Defining it
// here (rather than in internal/gateway) keeps the dependency narrow and
// lets tests fake transport without spinning up an HTTP server.
type Gateway interface {
	ListPlaylistSongs(ctx context.Context, playlistID string, pageSize int) ([]gateway.PlaylistSong, error)
	ListFavoriteAlbumIDs(ctx context.Context, pageSize int) ([]string, error)
	ListFavoriteArtistIDs(ctx context.Context, pageSize int) ([]string, error)
	AddFavoriteAlbum(ctx context.Context, albumID string) error
	AddFavoriteArtist(ctx context.Context, artistID string) error
}

// Options configures one Run.
//
// Sentinels match the lovedtracks pattern:
//   - RetryBackoff: nil → throttle.DefaultRetryBackoff, empty → no retries.
//   - MaxConsecutiveFinalFailures: 0 → throttle default, negative → disable.
type Options struct {
	DryRun                      bool
	BackupDir                   string
	PageSize                    int
	Inputs                      []string
	Stdin                       io.Reader
	Stdout                      io.Writer
	Stderr                      io.Writer
	RetryBackoff                []time.Duration
	MaxConsecutiveFinalFailures int
	VariousArtistsID            string
	ShareLinkResolver           ResolveShareLink
	OpenTTY                     func() (io.ReadCloser, error)
}

// Result summarizes a completed Run.
type Result struct {
	StartedAt       time.Time
	RunRecordPath   string
	SkipLogPath     string
	AddedAlbums     int
	AddedArtists    int
	SkippedItems    int
	PlaylistsLoaded int
	PlaylistsFailed int
	Elapsed         time.Duration
}

// runRecord is the JSON payload written to <BackupDir>/deezer-playlist-love-<UTC>.json.
type runRecord struct {
	Version         int               `json:"version"`
	StartedAt       string            `json:"started_at"`
	SourcePlaylists []recordPlaylist  `json:"source_playlists"`
	Stats           runRecordStats    `json:"stats"`
	AlbumsToAdd     []recordAlbum     `json:"albums_to_add"`
	ArtistsToAdd    []recordArtist    `json:"artists_to_add"`
}

type recordPlaylist struct {
	Input      string `json:"input"`
	PlaylistID string `json:"playlist_id"`
	SongCount  int    `json:"song_count"`
}

type runRecordStats struct {
	SongsScanned          int `json:"songs_scanned"`
	PlaylistsLoaded       int `json:"playlists_loaded"`
	PlaylistsFailed       int `json:"playlists_failed"`
	UniqueAlbums          int `json:"unique_albums"`
	UniqueArtists         int `json:"unique_artists"`
	VariousArtistsSkipped int `json:"various_artists_skipped"`
	UnparseableSongs      int `json:"unparseable_songs"`
	AlbumsAlreadyLoved    int `json:"albums_already_loved"`
	ArtistsAlreadyLoved   int `json:"artists_already_loved"`
	AlbumsToAdd           int `json:"albums_to_add"`
	ArtistsToAdd          int `json:"artists_to_add"`
}

type recordAlbum struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Artist string `json:"artist"`
}

type recordArtist struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Run executes the full love-contents flow against gw.
func Run(ctx context.Context, gw Gateway, opts Options) (*Result, error) {
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
	if opts.VariousArtistsID == "" {
		opts.VariousArtistsID = DefaultVariousArtistsID
	}
	if opts.ShareLinkResolver == nil {
		opts.ShareLinkResolver = DefaultShareLinkResolver(&http.Client{Timeout: 10 * time.Second})
	}
	maxConsec := opts.MaxConsecutiveFinalFailures
	if maxConsec == 0 {
		maxConsec = throttle.DefaultMaxConsecutiveFinalFailures
	}

	res := &Result{StartedAt: time.Now().UTC()}
	confirmReader := bufio.NewReader(opts.Stdin)

	// 1. Normalize inputs.
	good, badInputs := NormalizeInputs(ctx, opts.Inputs, opts.ShareLinkResolver)
	for _, b := range badInputs {
		fmt.Fprintf(opts.Stderr, "input %q failed: %s\n", b.Raw, b.Reason)
	}
	if len(good) == 0 {
		return nil, fmt.Errorf("no valid playlist inputs (%d failed)", len(badInputs))
	}

	// 2. Load each playlist.
	var allSongs []gateway.PlaylistSong
	var sourcePlaylists []recordPlaylist
	var loadFailures []InputError
	for _, in := range good {
		songs, err := gw.ListPlaylistSongs(ctx, in.PlaylistID, opts.PageSize)
		if err != nil {
			var gerr *gateway.GatewayError
			if errors.As(err, &gerr) && gerr.Kind == gateway.ErrAuthFailed {
				return nil, fmt.Errorf("auth failed loading playlist %s (refresh your arl in ~/.config/deezer-tools/config.toml): %w", in.PlaylistID, err)
			}
			fmt.Fprintf(opts.Stderr, "playlist %s failed to load: %v\n", in.PlaylistID, err)
			loadFailures = append(loadFailures, InputError{Raw: in.Raw, Reason: err.Error()})
			continue
		}
		allSongs = append(allSongs, songs...)
		sourcePlaylists = append(sourcePlaylists, recordPlaylist{
			Input: in.Raw, PlaylistID: in.PlaylistID, SongCount: len(songs),
		})
		res.PlaylistsLoaded++
	}
	res.PlaylistsFailed = len(loadFailures) + len(badInputs)

	// 3. Partial-input prompt.
	if (len(loadFailures) > 0 || len(badInputs) > 0) && !opts.DryRun {
		if res.PlaylistsLoaded == 0 {
			return nil, fmt.Errorf("no playlists loaded successfully (%d failed)", res.PlaylistsFailed)
		}
		fmt.Fprintf(opts.Stdout, "Proceed with %d of %d playlists? Type yes to continue: ", res.PlaylistsLoaded, res.PlaylistsLoaded+res.PlaylistsFailed)
		ans, _ := confirmReader.ReadString('\n')
		if !isYes(ans) {
			return nil, ErrAborted
		}
	}

	// 4. Aggregate + dedupe.
	set := Aggregate(allSongs, opts.VariousArtistsID)

	// 5. Read loved sets.
	lovedAlbums, err := gw.ListFavoriteAlbumIDs(ctx, opts.PageSize)
	if err != nil {
		return nil, fmt.Errorf("list loved albums: %w", err)
	}
	lovedArtists, err := gw.ListFavoriteArtistIDs(ctx, opts.PageSize)
	if err != nil {
		return nil, fmt.Errorf("list loved artists: %w", err)
	}

	// 6. Diff.
	plan := Diff(set, DiffInputs{LovedAlbumIDs: lovedAlbums, LovedArtistIDs: lovedArtists})

	// 7. Run-record.
	rec := runRecord{
		Version:         1,
		StartedAt:       res.StartedAt.Format(time.RFC3339),
		SourcePlaylists: sourcePlaylists,
		Stats: runRecordStats{
			SongsScanned:          len(allSongs),
			PlaylistsLoaded:       res.PlaylistsLoaded,
			PlaylistsFailed:       res.PlaylistsFailed,
			UniqueAlbums:          len(set.Albums),
			UniqueArtists:         len(set.Artists),
			VariousArtistsSkipped: set.VariousArtistsSkipped,
			UnparseableSongs:      set.UnparseableSongs,
			AlbumsAlreadyLoved:    plan.AlbumsAlreadyLoved,
			ArtistsAlreadyLoved:   plan.ArtistsAlreadyLoved,
			AlbumsToAdd:           len(plan.AlbumsToAdd),
			ArtistsToAdd:          len(plan.ArtistsToAdd),
		},
	}
	for _, a := range plan.AlbumsToAdd {
		rec.AlbumsToAdd = append(rec.AlbumsToAdd, recordAlbum{ID: a.ID, Title: a.Title, Artist: a.Artist})
	}
	for _, a := range plan.ArtistsToAdd {
		rec.ArtistsToAdd = append(rec.ArtistsToAdd, recordArtist{ID: a.ID, Name: a.Name})
	}
	recordPath, err := writeRunRecord(opts.BackupDir, res.StartedAt, rec)
	if err != nil {
		return nil, fmt.Errorf("write run record: %w", err)
	}
	res.RunRecordPath = recordPath
	fmt.Fprintf(opts.Stderr, "Run record written to %s\n", recordPath)

	// 8. Empty-diff short-circuit.
	if len(plan.AlbumsToAdd) == 0 && len(plan.ArtistsToAdd) == 0 {
		fmt.Fprintln(opts.Stdout, "Nothing to add, your loved albums and artists already cover these playlists.")
		res.Elapsed = time.Since(res.StartedAt)
		return res, nil
	}

	// 9. Dry-run short-circuit.
	if opts.DryRun {
		fmt.Fprintf(opts.Stdout, "would add %d albums and %d artists, run-record at %s\n",
			len(plan.AlbumsToAdd), len(plan.ArtistsToAdd), recordPath)
		res.Elapsed = time.Since(res.StartedAt)
		return res, nil
	}

	// 10. Confirm.
	fmt.Fprintf(opts.Stdout, "Plan: love %d albums and %d artists missing from your library\n",
		len(plan.AlbumsToAdd), len(plan.ArtistsToAdd))
	fmt.Fprintf(opts.Stdout, "  (sourced from %d playlists, %d unique songs scanned)\n",
		res.PlaylistsLoaded, len(allSongs))
	fmt.Fprintf(opts.Stdout, "Run record: %s\n", recordPath)
	fmt.Fprint(opts.Stdout, "Type yes to apply: ")
	ans, _ := confirmReader.ReadString('\n')
	if !isYes(ans) {
		fmt.Fprintln(opts.Stdout, "Aborted.")
		return res, ErrAborted
	}

	// 11. Open skip log.
	skipLog, skipPath, err := openSkipLog(opts.BackupDir, recordPath)
	if err != nil {
		return res, fmt.Errorf("open skip log: %w", err)
	}
	defer skipLog.Close()
	res.SkipLogPath = skipPath

	// 12. Apply phase A — albums.
	streak := 0
	for _, a := range plan.AlbumsToAdd {
		select {
		case <-ctx.Done():
			res.Elapsed = time.Since(res.StartedAt)
			return res, ctx.Err()
		default:
		}
		if err := throttle.Sleep(ctx); err != nil {
			res.Elapsed = time.Since(res.StartedAt)
			return res, err
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
			return res, err
		}
		var gerr *gateway.GatewayError
		if errors.As(err, &gerr) && gerr.Kind == gateway.ErrAuthFailed {
			res.Elapsed = time.Since(res.StartedAt)
			return res, fmt.Errorf("auth failed during album apply (refresh your arl in ~/.config/deezer-tools/config.toml): %w", err)
		}
		res.SkippedItems++
		_ = writeSkipEntry(skipLog, "album", a.ID, a.Title, a.Artist, err)
		streak++
		if maxConsec > 0 && streak >= maxConsec {
			res.Elapsed = time.Since(res.StartedAt)
			return res, fmt.Errorf("aborting: %d consecutive add failures (quota likely tripped or service degraded). Skipped items recorded in %s", streak, skipPath)
		}
	}

	// 13. Apply phase B — artists.
	for _, a := range plan.ArtistsToAdd {
		select {
		case <-ctx.Done():
			res.Elapsed = time.Since(res.StartedAt)
			return res, ctx.Err()
		default:
		}
		if err := throttle.Sleep(ctx); err != nil {
			res.Elapsed = time.Since(res.StartedAt)
			return res, err
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
			return res, err
		}
		var gerr *gateway.GatewayError
		if errors.As(err, &gerr) && gerr.Kind == gateway.ErrAuthFailed {
			res.Elapsed = time.Since(res.StartedAt)
			return res, fmt.Errorf("auth failed during artist apply (refresh your arl in ~/.config/deezer-tools/config.toml): %w", err)
		}
		res.SkippedItems++
		_ = writeSkipEntry(skipLog, "artist", a.ID, a.Name, "", err)
		streak++
		if maxConsec > 0 && streak >= maxConsec {
			res.Elapsed = time.Since(res.StartedAt)
			return res, fmt.Errorf("aborting: %d consecutive add failures (quota likely tripped or service degraded). Skipped items recorded in %s", streak, skipPath)
		}
	}

	// 14. Final summary.
	res.Elapsed = time.Since(res.StartedAt)
	fmt.Fprintf(opts.Stdout, "Added %d albums, %d artists, skipped %d", res.AddedAlbums, res.AddedArtists, res.SkippedItems)
	if res.SkippedItems > 0 {
		fmt.Fprintf(opts.Stdout, " (see %s)", skipPath)
	}
	fmt.Fprintf(opts.Stdout, ", elapsed %s\n", res.Elapsed.Round(time.Second))

	if res.SkippedItems > 0 {
		return res, fmt.Errorf("%d item(s) skipped", res.SkippedItems)
	}
	return res, nil
}

func isYes(s string) bool {
	return strings.EqualFold(strings.TrimSpace(s), "yes")
}

func writeRunRecord(dir string, started time.Time, rec runRecord) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	stamp := started.Format("20060102T150405Z")
	final := filepath.Join(dir, "deezer-playlist-love-"+stamp+".json")
	tmp := final + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return "", err
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(rec); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return "", err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return "", err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	if err := os.Rename(tmp, final); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	return final, nil
}

func openSkipLog(dir, recordPath string) (io.WriteCloser, string, error) {
	base := strings.TrimSuffix(filepath.Base(recordPath), ".json")
	skipPath := filepath.Join(dir, base+".skip.log")
	f, err := os.OpenFile(skipPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, "", err
	}
	return f, skipPath, nil
}

type skipEntry struct {
	Kind   string `json:"kind"`
	ID     string `json:"id"`
	Title  string `json:"title,omitempty"`
	Artist string `json:"artist,omitempty"`
	Error  string `json:"error"`
}

func writeSkipEntry(w io.Writer, kind, id, title, artist string, err error) error {
	b, _ := json.Marshal(skipEntry{Kind: kind, ID: id, Title: title, Artist: artist, Error: err.Error()})
	_, werr := fmt.Fprintln(w, string(b))
	return werr
}
```

- [ ] **Step 5: Run, expect PASS**

```bash
go build ./...
go test ./internal/playlistlove/ -v
```

If a test fails, read the failure, fix the run.go logic (do NOT change tests to make them pass — that defeats the purpose). Common gotchas:
- The fake gateway's `ListFavoriteAlbumIDs` / `ListFavoriteArtistIDs` returning `nil` slices: that's fine, the diff treats it as empty loved sets.
- The partial-input prompt and the apply confirm both read from `Stdin` via the same `bufio.Reader`. Tests must include enough lines to satisfy both prompts.

- [ ] **Step 6: Run vet + full suite**

```bash
go vet ./internal/playlistlove/
go test ./...
```

- [ ] **Step 7: Commit**

```bash
git add internal/playlistlove/run.go internal/playlistlove/run_test.go
git commit -m "feat(playlistlove): orchestrate list, diff, confirm, paced apply"
```

---

## Task 11: Cobra wiring — `playlists love-contents`

**Files:**
- Create: `cmd/deezer-tools/playlistlove_cmd.go`
- Modify: `cmd/deezer-tools/main.go`

Translates flags to `playlistlove.Options`, wires stdin reading when no positional args, opens `/dev/tty` for confirm if stdin was the playlist source.

- [ ] **Step 1: Locate where `newLovedTracksCmd` is registered**

```bash
grep -n "newLovedTracksCmd\|AddCommand" cmd/deezer-tools/main.go
```

- [ ] **Step 2: Create `cmd/deezer-tools/playlistlove_cmd.go`**

```go
package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/niref/deezer-tools/internal/config"
	"github.com/niref/deezer-tools/internal/gateway"
	"github.com/niref/deezer-tools/internal/playlistlove"
	"github.com/spf13/cobra"
)

func newPlaylistsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "playlists",
		Short: "Tools that take Deezer playlists as a source",
	}
	cmd.AddCommand(newLoveContentsCmd())
	return cmd
}

func newLoveContentsCmd() *cobra.Command {
	var dryRun bool
	var backupDir string

	cmd := &cobra.Command{
		Use:   "love-contents [PLAYLIST_INPUT...]",
		Short: "For the given playlists, love every album and artist whose songs appear in them",
		Long: `Read N Deezer playlists (numeric ID, full URL, or short link.deezer.com share link),
dedupe to unique albums and artists, diff against your loved-albums and loved-artists
collections, and (after confirmation) love the missing items.

If no positional args are given, reads one input per line from stdin (blank lines and
'#' comments ignored). When piping inputs, the confirm prompt reads from /dev/tty.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfgPath := defaultConfigPath()
			cfg, err := config.Load(cfgPath)
			if err != nil {
				return fmt.Errorf("config: %w", err)
			}
			client := gateway.New(cfg.ARL)

			inputs := args
			stdinUsedForInputs := false
			if len(inputs) == 0 {
				lines, err := playlistlove.ReadStdinInputs(cmd.InOrStdin())
				if err != nil {
					return fmt.Errorf("read stdin: %w", err)
				}
				inputs = lines
				stdinUsedForInputs = true
			}
			if len(inputs) == 0 {
				return fmt.Errorf("no playlist inputs given (positional or stdin)")
			}

			confirmReader := cmd.InOrStdin()
			if stdinUsedForInputs {
				if r, err := openTTY(); err == nil {
					confirmReader = r
					defer r.Close()
				} else {
					return fmt.Errorf("piping playlists requires a tty for confirm: %w", err)
				}
			}

			_, err = playlistlove.Run(cmd.Context(), client, playlistlove.Options{
				DryRun:    dryRun,
				BackupDir: backupDir,
				Inputs:    inputs,
				Stdin:     confirmReader,
				Stdout:    cmd.OutOrStdout(),
				Stderr:    cmd.ErrOrStderr(),
				ShareLinkResolver: playlistlove.DefaultShareLinkResolver(&http.Client{Timeout: 10 * time.Second}),
			})
			return err
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "compute diff and write run-record but do not love anything")
	cmd.Flags().StringVar(&backupDir, "backup-dir", ".", "directory for the run-record JSON and skip log")
	return cmd
}

// openTTY opens /dev/tty for reading. Used when stdin was consumed by the
// playlist input list and we still need a place to read the confirm answer.
func openTTY() (io.ReadCloser, error) {
	return os.OpenFile("/dev/tty", os.O_RDONLY, 0)
}
```

- [ ] **Step 3: Register in `cmd/deezer-tools/main.go`**

Find the existing `rootCmd.AddCommand(newLovedTracksCmd())` line. Add a sibling:

```go
rootCmd.AddCommand(newLovedTracksCmd())
rootCmd.AddCommand(newPlaylistsCmd())
```

- [ ] **Step 4: Build**

```bash
go build -o deezer-tools ./cmd/deezer-tools
./deezer-tools playlists love-contents --help
```

Expected: cobra prints the help text. No actual run yet.

- [ ] **Step 5: Smoke-check the help output**

```bash
./deezer-tools --help
./deezer-tools playlists --help
```

Expected: `playlists` listed under root, `love-contents` listed under playlists.

- [ ] **Step 6: Commit**

```bash
git add cmd/deezer-tools/playlistlove_cmd.go cmd/deezer-tools/main.go
git commit -m "feat(cmd): wire playlists love-contents subcommand"
```

---

## Task 12: Live integration test for new read-only methods

**Files:**
- Modify: `internal/gateway/integration_test.go`

Mirror the existing `DEEZER_INTEGRATION=1` pattern. Add three read-only checks: `playlist.getSongs` against a known-public playlist (pick a stable Deezer editorial playlist ID at run time and document it), `album.getFavoriteIds` against the live account, and `artist.getFavoriteIds` against the live account. **No write methods are called** by integration tests.

This task also produces the implementation-side discoveries for the spec's known-unknowns: Various-Artists `ART_ID` actually emitted on a real playlist, idempotency response shape, and any ceiling-error code (only checked manually if the user has > some threshold of loved albums).

- [ ] **Step 1: Open `internal/gateway/integration_test.go` and read the existing pattern**

```bash
sed -n '1,80p' internal/gateway/integration_test.go
```

- [ ] **Step 2: Append the new test cases**

```go
func TestIntegration_ListPlaylistSongs_publicPlaylist(t *testing.T) {
	if os.Getenv("DEEZER_INTEGRATION") != "1" {
		t.Skip("set DEEZER_INTEGRATION=1 to run")
	}
	arl := loadIntegrationARL(t)
	c := New(arl)
	// Deezer editorial playlist "Best of <year>" or similar. Pick one at run
	// time and pin its ID here once verified stable. Replace the literal below
	// with the chosen ID before merging this test.
	const knownPublicPlaylistID = "1313621735" // Deezer "100% Hits 80s" — verify before merging
	songs, err := c.ListPlaylistSongs(context.Background(), knownPublicPlaylistID, 200)
	if err != nil {
		t.Fatalf("ListPlaylistSongs err = %v", err)
	}
	if len(songs) == 0 {
		t.Fatal("expected at least one song")
	}
	// Sanity-check field shapes.
	first := songs[0]
	if first.SongID == "" || first.AlbumID == "" || first.ArtistID == "" {
		t.Errorf("first song missing IDs: %+v", first)
	}
}

func TestIntegration_ListFavoriteAlbumIDs(t *testing.T) {
	if os.Getenv("DEEZER_INTEGRATION") != "1" {
		t.Skip("set DEEZER_INTEGRATION=1 to run")
	}
	arl := loadIntegrationARL(t)
	c := New(arl)
	ids, err := c.ListFavoriteAlbumIDs(context.Background(), 100)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	t.Logf("loved albums: %d", len(ids))
}

func TestIntegration_ListFavoriteArtistIDs(t *testing.T) {
	if os.Getenv("DEEZER_INTEGRATION") != "1" {
		t.Skip("set DEEZER_INTEGRATION=1 to run")
	}
	arl := loadIntegrationARL(t)
	c := New(arl)
	ids, err := c.ListFavoriteArtistIDs(context.Background(), 100)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	t.Logf("loved artists: %d", len(ids))
}
```

If `loadIntegrationARL` doesn't yet exist as a helper, the existing integration test will already define it (it's the same pattern the wipe used). Reuse it; don't duplicate.

- [ ] **Step 3: Verify the `knownPublicPlaylistID` is actually public and stable**

Open `https://www.deezer.com/playlist/1313621735` in a browser. If it 404s or is now private, search for another stable editorial playlist and update the constant. Commit the chosen ID with a comment naming the playlist.

- [ ] **Step 4: Run with the integration env**

```bash
DEEZER_INTEGRATION=1 go test ./internal/gateway/ -run TestIntegration_ -v
```

Expected: all three tests pass. The third logs the loved-album / loved-artist counts so we have a real data point.

- [ ] **Step 5: Validate the Various-Artists ART_ID against live data**

If any song from the integration playlist returns `ART_ID == "5080"` (or whatever the research doc asserts), record it. If a different ID shows up, update `playlistlove.DefaultVariousArtistsID` and the research doc.

```bash
DEEZER_INTEGRATION=1 go test ./internal/gateway/ -run TestIntegration_ListPlaylistSongs -v 2>&1 | head -50
```

If "Various Artists" appears in the artist names but with a different ART_ID, that's the research feedback loop the spec called for. Update accordingly.

- [ ] **Step 6: Commit**

```bash
git add internal/gateway/integration_test.go
git commit -m "test(gateway): live read-only checks for playlist + favorite-ids methods"
```

---

## Task 13: README + manual verification

**Files:**
- Modify: `README.md`

Document the new subcommand and the supported input forms. Then run a real, small dry-run to verify end-to-end.

- [ ] **Step 1: Read the existing README**

```bash
sed -n '1,80p' README.md
```

- [ ] **Step 2: Add a `playlists love-contents` section**

Append (or insert under the existing usage section):

```markdown
### `playlists love-contents`

For one or more Deezer playlists, love every album and artist whose songs appear in them. Already-loved items are no-ops. Use this to expand your loved-albums and loved-artists collections from playlists that contain complete albums.

```sh
deezer-tools playlists love-contents [--dry-run] [--backup-dir <dir>] <input>...
```

Each `<input>` may be:

- A bare numeric playlist ID: `15018766163`
- A full Deezer playlist URL: `https://www.deezer.com/en/playlist/15018766163`
- A short share link: `https://link.deezer.com/s/337D7rZEQd0wiR1D0ivjS`

If no positional args are given, inputs are read from stdin (one per line, blank lines and `#` comments ignored). Confirm prompt then reads from `/dev/tty`.

A JSON run record is written to `<backup-dir>/deezer-playlist-love-<UTC>.json` before the apply phase. Per-item failures append to `<backup-dir>/deezer-playlist-love-<UTC>.skip.log`.

Sequential paced writes (1s ± 200ms between attempts, 5s/15s/30s/60s/120s retry on rate-limit/5xx) protect against the gw-light quota that historically tripped Akamai's WAF on the wipe — see `docs/solutions/integration-issues/`.
```

- [ ] **Step 3: Commit README**

```bash
git add README.md
git commit -m "docs: README usage for playlists love-contents"
```

- [ ] **Step 4: Manual dry-run on a small playlist**

Pick one of your real playlists with maybe 50 songs to start. Run:

```bash
./deezer-tools playlists love-contents --dry-run <playlist-id-or-url>
```

Expected: the tool prints the diff stats, writes a run record under `./deezer-playlist-love-*.json`, and exits 0. Inspect the run record:

```bash
ls -la deezer-playlist-love-*.json
cat deezer-playlist-love-*.json | head -60
```

Verify:
- `stats.unique_albums` and `stats.unique_artists` look reasonable for a 50-song playlist (~5–15 albums, ~5–30 artists).
- `albums_to_add` and `artists_to_add` lists are populated.
- Permissions: `0600`.

If anything looks wrong, debug before doing the actual apply.

- [ ] **Step 5: Manual real run on the same small playlist**

```bash
./deezer-tools playlists love-contents <playlist-id-or-url>
```

Expected: prints the plan, prompts `Type yes to apply:`, applies sequentially with the pacer, prints final summary, exits 0. Verify on deezer.com that the new albums and artists appear in your loved sets.

If you observe a ceiling-style error mid-run (e.g., "loved albums limit reached"), capture the exact error text. That's the signal to (a) record it in the research doc, (b) add a new `ErrLimitReached` classified kind in `internal/gateway/errors.go`, and (c) handle it as run-aborting in `playlistlove/run.go`. Do that as a follow-up commit.

- [ ] **Step 6: Open the MR**

```bash
git push -u origin wip/playlists-love-contents
```

Then create the MR (via `gh pr create` or the equivalent for your remote). The MR should include only commits from this branch (Task 1 onward); the spec, plan, and research doc are on `main` and should not appear in the MR diff.

---

## Spec coverage matrix

| Spec section / requirement                                        | Plan task   |
|-------------------------------------------------------------------|-------------|
| Subcommand `deezer-tools playlists love-contents <inputs>...`     | T11         |
| Three input forms: numeric, long URL, short share link            | T8          |
| Stdin fallback when no positional args; `/dev/tty` confirm        | T11, T10    |
| Adds missing albums and missing artists; idempotent re-runs       | T6, T7, T10 |
| Various-Artists filter at artist level (default `5080`)           | T9, T12     |
| Dedupe by `ALB_ID` / `ART_ID` before any diff or add              | T9          |
| Same throttle discipline as wipe (1s ± 200ms, 5s/15s/30s/60s/120s, breaker 5) | T4, T10 |
| `internal/throttle` extracted, lovedtracks refactored             | T4          |
| Atomic JSON run-record (`0600`); skip log mirroring wipe          | T10         |
| `--dry-run` short-circuit                                         | T10         |
| `gateway.IsRetryable` predicate                                   | T3          |
| New gateway primitives in `playlists.go`/`albums.go`/`artists.go` | T5, T6, T7  |
| Verify exact gw-light methods + Various-Artists ID before code    | T2          |
| Live integration test (read-only) for new methods                 | T12         |
| Existing wipe tests pass unchanged (refactor verification gate)   | T4 step 7   |
| README documents subcommand                                       | T13         |

## Risks & known unknowns recorded against tasks

- **Method names** for `playlist.getSongs` / `album.getFavoriteIds` / `favorite_album.add` / `artist.getFavoriteIds` / `favorite_artist.add` — Task 2 is the verification gate. If any name differs from the assumption in this plan, edit the relevant Task 5/6/7 string constants before implementing.
- **Various-Artists `ART_ID`** — Task 2 asserts in the research doc. Task 12 validates against live data. If different, update `playlistlove.DefaultVariousArtistsID` and the research doc, no code-shape change required.
- **Idempotency response on add** — Task 2 captures the shape. If `favorite_album.add` for an already-loved album returns an error envelope, add a classifier branch in `internal/gateway/errors.go` to map it to success.
- **Loved-album / loved-artist ceiling** — unverified. Task 13 step 5 catches it on a manual real run; if hit, add `ErrLimitReached` kind and abort behaviour as a follow-up commit on the same WIP branch.
