# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & test

```sh
go build ./...                                     # compile everything
go build -o deezer-tools ./cmd/deezer-tools        # produce the CLI binary
go test ./...                                      # unit tests (no network)
go test ./internal/gateway -run TestIntegration_   # live integration test
go vet ./...
```

The live integration test in `internal/gateway/integration_test.go` is gated by `DEEZER_INTEGRATION=1` and reads the real `arl` from `~/.config/deezer-tools/config.toml`. It is read-only — it only paginates IDs.

After modifying dependencies, run `go mod tidy` and `go build ./...` before committing.

## Architecture

Three internal packages, one CLI. Layering is strict and one-directional:

```
cmd/deezer-tools  → internal/lovedtracks → internal/gateway
                  → internal/config
```

- `internal/config` — loads `~/.config/deezer-tools/config.toml`, refuses to read it unless mode is `0600` (the `arl` cookie is equivalent to a session token). Pure; no network.
- `internal/gateway` — low-level adapter for Deezer's unofficial `gw-light.php` endpoint. Owns auth, CSRF lifecycle, error classification, and the "list favorites + remove favorite" primitives. The package doc references `docs/superpowers/research/2026-04-27-deezer-gateway-protocol.md` (the protocol is undocumented and reverse-engineered).
- `internal/lovedtracks` — orchestrates the list → backup → confirm → delete flow. Depends on `internal/gateway` via the small `Gateway` interface in `wipe.go` so tests fake the transport without spinning up an HTTP server. Must not import sibling domain packages (loved albums, artists, playlists) when those are added.
- `cmd/deezer-tools` — Cobra wiring only. `lovedtracks_cmd.go` translates flags → `lovedtracks.Options` and passes through `cmd.Context()`/IO.

### Gateway client invariants

`gateway.Client` is the only place that knows about the gw-light wire format. Things that look surprising but are load-bearing:

- **Cookie jar is required.** The HTTP client carries a `cookiejar.New(nil)` so the server-set `sid` cookie persists across calls. Without it, the CSRF token from `deezer.getUserData` is bound to a session you've already dropped, and the next call returns `Invalid CSRF token`. Don't replace the client without preserving the jar.
- **CSRF bootstrap uses the literal string `"null"`.** `refreshCSRF` sets `c.apiToken = "null"` for the very first `deezer.getUserData` call. That's the gw-light protocol, not a placeholder.
- **`callWithCSRF` is the public path for authenticated calls.** It handles initial CSRF acquisition and a single refresh-and-retry on `ErrCSRFExpired`. Higher-level helpers should never call `Call` directly for authenticated methods.
- **`SNG_ID` decodes via `flexString`.** The gateway returns it sometimes quoted, sometimes as a bare number, *within the same response*. Don't change it to plain `string`.
- **`USER_ID == 0` means the `arl` is invalid.** Treated as `ErrAuthFailed`, not as a parse error — surface it that way.
- **Errors are classified, not raw.** `classifyError` maps `{VALID_TOKEN_REQUIRED, CSRF_TOKEN_INVALID}` → `ErrCSRFExpired`, `{NEED_USER_AUTH_REQUIRED, USER_AUTH_REQUIRED}` → `ErrAuthFailed`, `DATA_ERROR` → `ErrNotFound`, `QUOTA_ERROR` → `ErrRateLimited`, HTTP 429 → `ErrRateLimited`, 5xx → `ErrServerError`. Callers branch on `gateway.ErrorKind`, not on string matching. **`QUOTA_ERROR` is gw-light's own throttle signal returned at HTTP 200** — the 2026-04-28 incident (Akamai IP block after 5,513 unretried QUOTA_ERROR responses streamed at full rate) is the reason it must be treated as rate-limit and not as a one-shot skip.

### Wipe orchestration invariants

- **List is two-stage.** `ListFavoriteSongs` first paginates `song.getFavoriteIds` (authoritative for "what is loved"), then enriches in 200-ID chunks via `song.getListData`. If enrichment omits an ID we still emit a record — otherwise the wipe would silently skip those tracks.
- **Backup is atomic.** `writeBackup` writes `<dir>/deezer-loved-tracks-<UTC>.json.tmp` at `0600`, fsyncs, then renames. The matching skip log uses the same prefix with `.skip.log`.
- **Confirmation requires typing the count.** `confirm` reads the exact number of tracks as a string; anything else aborts with `ErrAborted`.
- **Retry policy is in `deleteWithRetry`, not in the gateway.** Per-track retry schedule `5s, 15s, 30s, 60s, 120s` (`defaultRetryBackoff` in `wipe.go`) only on `ErrRateLimited` / `ErrServerError`. `ErrAuthFailed` aborts the whole run; other classified 4xx (e.g. `ErrNotFound`) skip the single track and append to the skip log. The schedule is configurable via `Options.RetryBackoff` so tests can pass an empty slice for no-retry.
- **Baseline pacer between every delete.** `pacedSleep` waits `Pace ± PaceJitter` (defaults `1s ± 200ms`) before *every* delete attempt, including the first. This exists so even on a happy path we don't burst hard enough to look like a bot. `Pace < 0` disables it (test-only). Don't remove the pacer to "make it faster" — it's the throttle that keeps us off Akamai's bot list.
- **Streak circuit breaker.** `Wipe` aborts when `MaxConsecutiveFinalFailures` (default 5) tracks in a row exhaust their retry budgets with no success between. The counter resets on any successful delete. Bounds worst-case behavior so a sustained quota or service degradation can't silently rack up thousands of skips. Negative disables (test-only).
- **Context is checked between every successful delete.** Without that explicit `select`, a 10k-track happy path would ignore SIGINT until the next failure (the inner backoff only checks ctx during sleeps).
- **Skipped tracks → non-zero exit.** `Wipe` returns a non-nil error when `SkippedCount > 0`, even though `Result` is fully populated, so the CLI exits non-zero for review.

## Configuration & runtime

Config lives at `~/.config/deezer-tools/config.toml` with one key: `arl = "..."`. The file must be `0600`; `config.Load` rejects anything more permissive on unix. The `arl` is obtained from a logged-in browser session at deezer.com (DevTools → Cookies). It can be revoked at any time and will eventually expire — auth-failure errors should tell the user to refresh it in `~/.config/deezer-tools/config.toml`.

Backups (`deezer-loved-tracks-*.json`) and skip logs (`*.skip.log`) are gitignored — never commit them.

## Where design context lives

- `docs/superpowers/specs/` — design specs that predate the implementation. The wipe-loved-tracks design doc explains why list/backup/confirm/delete are separate phases and the empty-account edge case.
- `docs/superpowers/plans/` — phased implementation plans.
- Reverse-engineered gw-light protocol notes are referenced from the `internal/gateway` package doc.

When adding a new domain (loved albums, playlists, etc.), mirror the `lovedtracks` shape: a small package that depends on `internal/gateway` via a narrow interface, owns its own CLI subcommand under `cmd/deezer-tools`, and does not cross-import other domain packages.
