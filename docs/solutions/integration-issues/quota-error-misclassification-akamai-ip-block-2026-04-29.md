---
title: QUOTA_ERROR misclassification caused Akamai IP block during loved-tracks wipe
date: 2026-04-29
category: integration-issues
module: "internal/gateway + internal/lovedtracks"
problem_type: integration_issue
component: tooling
symptoms:
  - "All 5,513 skipped tracks logged the same error: `gateway favorite_song.remove: QUOTA_ERROR: Quota exceeded on playlist delete songs (status=200)`"
  - "Run reported 4,487 deleted, 5,513 skipped (~45% completion, then no further successes that night)"
  - "Akamai edge-block on the user's IP after the run: `Access Denied. Reference #18.67e11602...`"
  - "Sustained ~73 failed deletes/min during the second half of the run scored as bot behavior"
  - "Skip-log IDs exactly matched the count remaining as loved in the web UI; deletes had persisted, the failure was retry/classification not write semantics"
root_cause: logic_error
resolution_type: code_fix
severity: critical
tags: [deezer, gw-light, quota-error, rate-limit, akamai, retry, circuit-breaker, integration]
---

# QUOTA_ERROR misclassification caused Akamai IP block during loved-tracks wipe

## Problem

A `loved-tracks wipe` run against a 10,000-track account silently skipped 5,513 tracks — every skip was a `QUOTA_ERROR` from gw-light that our classifier didn't recognize, so the retry path never engaged. The sustained ~73 failed deletes/min that resulted got the user's IP WAF-blocked from `deezer.com` by Akamai overnight.

## Symptoms

- 5,513 / 10,000 tracks logged with the identical error string `QUOTA_ERROR: Quota exceeded on playlist delete songs (status=200)` — every skip in `*.skip.log` was the same.
- Run summary: `4,487 deleted, 5,513 skipped, elapsed 1h15m`.
- Next morning, `deezer.com` from the user's IP returned an Akamai edge page: `Access Denied. Reference #18.67e11602...`. Plain web traffic was blocked, not just API calls.
- Once verified from a different IP, the web UI showed exactly 5,513 favourites — i.e. the deletes that *did* go through were genuinely persistent. The remaining set == the skip-log IDs, exactly.
- Observed delete cadence pre-block: ~2.2 req/s wall-clock, ~73 failures/min sustained for the second half of the run.

## What Didn't Work

- **"Deezer rolled back our deletes as anti-abuse."** Ruled out by arithmetic: 10,000 listed − 4,487 deleted = 5,513 remaining, which exactly matched the post-incident UI count. Deletes were honest.
- **"App-side caching of the favourites list was masking the true count."** Same arithmetic disproof. Both client and server agreed.
- **A `--resume` flag that replays the skip log on the next run.** Dropped as YAGNI: the existing `ListFavoriteSongs` flow already returns just the remaining ~5,513 IDs (the 10k limit is a *cap*, not a fixed length), so a fresh wipe from the current state is a no-op for already-deleted tracks.
- **Multi-minute backoffs on `QUOTA_ERROR` (e.g. `30s, 2m, 5m`).** Dropped after recalling that the original run dipped in and out of quota mid-run — windows are short. Long sleeps would over-pessimize the common case for no extra safety.

## Solution

Three changes on commit `df28a07` of `wip/loved-tracks-wipe`. They engage the retry path, throttle the happy path, and bound worst-case behavior.

### 1. Classify `QUOTA_ERROR` as rate-limited

`gw-light`'s own throttle signal arrives at HTTP 200 with a JSON error body — easy to miss as a status-code-driven retry layer. Map it explicitly.

`internal/gateway/errors.go` — added arm in the `errMap` switch:

```go
case "QUOTA_ERROR":
    // gw-light's own throttle signal, returned at HTTP 200. Treat as
    // rate-limit so deleteWithRetry backs off instead of streaming
    // failures (see 2026-04-28 incident: 5,513 unretried QUOTA_ERROR
    // responses tripped Akamai's WAF and IP-blocked us).
    return &GatewayError{Kind: ErrRateLimited, Method: method, Status: status, Message: "QUOTA_ERROR: " + msg}
```

Previously this code fell through to the unknown-error fallback at the end of the switch; `shouldRetry` returns `false` for `ErrUnknown`, so each `QUOTA_ERROR` was a one-shot skip.

### 2. Stretch retry backoff and add a baseline pacer

`internal/lovedtracks/wipe.go` — pacing lives at package scope (so production is untunable, intentionally; tests zero it via `init()`); the retry schedule and breaker threshold are exposed on `Options`:

```go
var defaultRetryBackoff = []time.Duration{
    5 * time.Second, 15 * time.Second, 30 * time.Second,
    60 * time.Second, 120 * time.Second,
}

var (
    defaultPace       = time.Second
    defaultPaceJitter = 200 * time.Millisecond
)

const defaultMaxConsecutiveFailure = 5
```

`pacedSleep(ctx, defaultPace, defaultPaceJitter)` runs before *every* delete attempt — including the happy path — so the loop's natural cadence is ~1 req/s with jitter rather than network-bound at ~2.2 req/s. The retry schedule (~232s total per stuck track) crosses short quota windows without amplifying load when one is genuinely exhausted.

### 3. Streak circuit breaker

The main `Wipe` loop tracks consecutive *final* failures (i.e. tracks that exhausted their retry budget). A successful delete resets the counter:

```go
consecutiveFailures++
if maxConsec > 0 && consecutiveFailures >= maxConsec {
    return res, fmt.Errorf("aborting wipe: %d consecutive deletes failed (quota likely tripped or service degraded). Try again later. Skipped tracks recorded in %s", consecutiveFailures, skipPath)
}
```

Five failures in a row aborts the whole run. The two operator-facing tunables live on `Options` (`RetryBackoff`, `MaxConsecutiveFinalFailures`) with defaults; tests opt out via a `fastTune` helper that sets disable sentinels (empty slice / `-1`). Pacing is intentionally not exposed on `Options` — the test binary's `init()` simply zeroes the package vars `defaultPace` and `defaultPaceJitter` so the loop runs instantly under test, while production keeps the 1s ± 200ms cadence.

## Why This Works

- **Classification engages the retry machinery already in place.** `deleteWithRetry` already retried `ErrRateLimited`; mapping `QUOTA_ERROR → ErrRateLimited` re-enters that path with zero new control flow.
- **Retry handles transient quota.** The `5,15,30,60,120s` schedule is long enough to ride out short windows the user observed dipping in and out of, short enough to surface a genuinely exhausted window via the circuit breaker.
- **The pacer keeps us under bot-scoring radar even on the happy path.** ~1 req/s with jitter is a fundamentally different traffic shape from the prior ~2.2 req/s network-bound bursting. Akamai didn't flag the failure rate alone — it flagged the *traffic shape* of sustained fast-failed requests, which a pacer breaks regardless of success or failure.
- **The breaker bounds amplification.** Worst case after the breaker trips: 5 failed tracks × (1 initial + 5 retries) over ~232s of backoffs ≈ ~1.5 failures/min, vs. the ~73/min that triggered the WAF — a ~50× reduction in worst-case failure rate.

## Prevention

- **Treat unrecognized error codes from undocumented protocols as retryable until proven otherwise**, or surface them prominently rather than bucketing into `ErrUnknown` and skipping. The 5,513 silent skips were possible only because "unknown" silently meant "skip and continue" — a 5-minute dev-time decision that became a multi-thousand-track production failure.
- **Add a baseline pacer to any long bulk-action loop against an external API**, not only after a rate-limit response. Reactive throttling alone still lets the happy path look automated. Bot scoring sees traffic shape, not just response codes.
- **Add a streak circuit breaker on consecutive final-failures.** Costs ~5 lines of code; converts "thousands of doomed requests" into "5 doomed requests then stop and tell the human."
- **Pin the literal production error body shape in a test.** The QUOTA_ERROR classification regression test (`internal/gateway/errors_test.go::TestClassifyError_QuotaErrorIsRateLimited`) decodes the exact JSON body observed from production:

  ```json
  {"error":{"QUOTA_ERROR":"Quota exceeded on playlist delete songs"}}
  ```

  A future refactor of the classifier can't regress this mapping without breaking the test.

- **The deeper lesson**: the original retry design (commit `7a4ab05`) assumed the gateway would signal rate limits via HTTP 429, the standard REST behavior. gw-light enforces quota entirely in-band via JSON error codes on HTTP 200 — a non-standard pattern consistent with the reverse-engineered, unofficial nature of the API. *When integrating with an undocumented protocol, write a "transient-error" arm that catches anything plausibly throttle-shaped (`QUOTA_ERROR`, `RATE_LIMIT_ERROR`, `THROTTLE`, etc.) before falling through to "unknown skip."* (session history)

## Related Issues

- **Sibling gw-light learning:** [docs/solutions/design-patterns/gw-light-go-adapter-quirks-2026-04-28.md](../design-patterns/gw-light-go-adapter-quirks-2026-04-28.md) (currently on `main`) — the cookie-jar + flexString pitfalls when porting Python session-stateful clients to Go. Same module, different failure class. Together these document the gw-light adapter's two main classes of pitfall: implicit-Python-behavior gaps (sibling doc) and unannounced-error-code gaps (this doc).
- Repo invariants now reference all three changes: `CLAUDE.md` "Gateway client invariants" (QUOTA_ERROR classification) and "Wipe orchestration invariants" (retry schedule, pacer, circuit breaker).
- Pre-incident retry policy lived in `docs/superpowers/specs/2026-04-27-wipe-loved-tracks-design.md` (rate-limited / 5xx → exponential `1/2/4/8/16s, max 5`) and the implementation plan `docs/superpowers/plans/2026-04-27-wipe-loved-tracks.md`. Both are dated artifacts; this incident is the empirical-tuning event the spec anticipated.
- Reverse-engineered protocol notes referenced from the `internal/gateway` package doc: `docs/superpowers/research/2026-04-27-deezer-gateway-protocol.md` (file is referenced but not yet present in the working tree).
- Commits: `df28a07` (the fix) and `22aef86` (track `CLAUDE.md` by overriding global gitignore).
