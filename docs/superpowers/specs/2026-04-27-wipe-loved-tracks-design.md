# Wipe Loved Tracks — Design

**Date:** 2026-04-27
**Status:** Approved (brainstorming → writing-plans handoff)
**Tool:** `deezer-tools loved-tracks wipe` (first command in the `deezer-tools` toolbox)

## Goal

Delete every loved *track* from Nils's Deezer account in bulk, leaving loved
*albums* and loved *artists* untouched. Write a JSON backup of every track
before any deletion happens.

## Why this exists

When the library was migrated from Spotify, the import pushed Nils past
Deezer's 10,000 loved-tracks ceiling. The library is now full of tracks he
didn't curate. He wants to start fresh: keep loved albums and loved artists,
clear out loved tracks, then re-love selectively from scratch over time.

This is the first tool in `deezer-tools`, a personal Go toolbox for Deezer
account automation. Auth scaffolding and the gateway client built here are
intended to be reused by future tools (playlist management, etc.).

## Scope

**In scope (v1):**

- One subcommand: `deezer-tools loved-tracks wipe`
- Reusable infrastructure: config loader, gateway client, CSRF handling.

**Out of scope (v1):**

- Playlist tools.
- Loved-album / loved-artist tools.
- Re-loving from a backup.
- A `--resume` flag.
- Concurrency tuning.
- A non-interactive `--yes` flag.

YAGNI: anything not strictly required to wipe loved tracks safely is deferred.

## Auth approach

Deezer's official developer portal stopped accepting new app registrations
years ago. The OAuth path is closed to new users. We therefore use Deezer's
**unofficial internal gateway** (`gw-light.php`) with `arl` cookie auth — the
same approach used by the open-source Deezer ecosystem (deemix, deezer-py,
d-fi-core).

**Trade-off accepted:** the gateway is unofficial, can change without notice,
and sits in a TOS gray zone (Nils is acting on his own account, but from a
non-browser client). The risk is acknowledged and accepted.

**Honest disclosure:** the broad shape of the gateway protocol is known
(POST to `https://www.deezer.com/ajax/gw-light.php` with method, api_token,
input, api_version query params and `Cookie: arl=<value>`). The **exact method
names and parameter shapes** for listing and removing favorite songs are NOT
asserted in this spec. They will be verified at implementation time by reading
existing OSS libraries (deezer-py, deemix, d-fi-core), not invented. The
implementation plan must include a "verify exact gateway methods" task before
any Go code is written.

## Architecture

### Repo layout

```
deezer-tools/
├── cmd/deezer-tools/main.go          # cobra root + subcommand registration
├── internal/
│   ├── config/config.go              # load ~/.config/deezer-tools/config.toml
│   ├── gateway/
│   │   ├── client.go                 # gw-light.php client + arl cookie auth
│   │   ├── csrf.go                   # api_token acquisition + refresh
│   │   ├── tracks.go                 # list / remove favorite songs
│   │   └── errors.go                 # gateway error classification
│   └── lovedtracks/wipe.go           # orchestration: list → backup → confirm → delete
├── docs/superpowers/specs/           # design docs (this file lives here)
├── go.mod / go.sum
└── README.md / LICENSE
```

Single binary, future tools become siblings in `cmd/deezer-tools/main.go`.
`internal/gateway` is the shared substrate every future tool reuses.

### Package boundaries (design invariant)

`internal/lovedtracks` only imports the track-related helpers from
`internal/gateway/tracks.go`. It must not call album or artist helpers. This
makes "we never touch loved albums/artists" a structural property, not just a
runtime promise.

### Gateway client

- `POST https://www.deezer.com/ajax/gw-light.php` with query params
  `method=<name>&api_token=<csrf>&input=3&api_version=1.0` and
  `Cookie: arl=<value>`.
- CSRF `api_token` is obtained at session start via `deezer.getUserData` and
  refreshed once on token-expiry errors before retrying the failed call.
- Two methods needed (exact names verified at implementation time):
  1. List the user's favorite songs, paginated.
  2. Remove a favorite song by track id.

### Config

Path: `~/.config/deezer-tools/config.toml`
Permissions: enforced at `0600`. The tool refuses to start if the file is
world-readable.

```toml
arl = "..."
```

If the file is missing, the tool prints clear instructions: "open deezer.com,
log in, open DevTools → Application → Cookies, copy the `arl` cookie value,
write it to `~/.config/deezer-tools/config.toml` as shown above, `chmod 600`."

## The `wipe` flow

```
deezer-tools loved-tracks wipe [--dry-run] [--backup-dir <dir>]
```

Default `--backup-dir`: current working directory.

1. **Load config.** Verify `arl` is present; verify file perms are `0600`.
2. **Obtain CSRF token** by calling `deezer.getUserData`.
3. **List phase.** Paginate all loved tracks, accumulating in memory
   (~10k tracks × ~200 bytes ≈ 2 MB; trivial). Each entry contains
   `{id, title, artist, album, time_add}`. Progress to stderr:
   `listed N…`. If listing fails partway, abort — no backup file is written,
   no deletions have run.
4. **Backup phase.** After the list completes, write the full JSON array
   once to `<backup-dir>/deezer-loved-tracks-<RFC3339>.json`. Atomic write
   (write to `.tmp`, `fsync`, rename). On any error here, abort before any
   deletion runs.
5. **Empty-account short-circuit.** If `N == 0`, print
   `No loved tracks to wipe.` and exit 0. No backup file is written for an
   empty account.
6. **Dry-run short-circuit.** If `--dry-run` is set, print
   `would delete N tracks, backup at <path>` and exit 0. Steps 7 and 8 are
   skipped.
7. **Confirmation gate** (interactive). Print summary + backup path, prompt:
   `Found <N> loved tracks. Backup written to <path>. Type the number <N>
   to confirm wipe:`. Typing anything other than the exact count aborts.
   The count-as-shibboleth is harder to fat-finger than "yes". No `--yes`
   flag in v1.
8. **Delete phase.** Sequential, one track at a time. Per-track error policy:
   - **CSRF expired** → refresh token, retry the same delete once.
   - **Rate-limited / 5xx** → exponential backoff 1s/2s/4s/8s/16s, max 5
     retries.
   - **Auth failure (`arl` invalid)** → abort immediately with refresh
     instructions.
   - **Other 4xx on a specific track** → log `{id, error}` to a skip-log file
     at `<backup-dir>/deezer-loved-tracks-<RFC3339>.skip.log`, continue with
     the next track.
9. **Final summary.** `Deleted X, skipped Y (see <skip-log>), elapsed T`.
   Exit 0 iff Y == 0; exit 1 otherwise.

### Resumability

Out of scope for v1. If interrupted, re-run the command from scratch — the
next list phase reflects current state, a new backup is written, and any
already-deleted tracks are simply absent from the new list. A `--resume`
mode is conceivable for v1.1 but YAGNI for now.

## Testing

CLAUDE.md constraints: no mocked-behavior tests, no e2e mocks, real data and
real APIs for end-to-end testing.

- **Unit tests** for `internal/gateway` via `httptest.Server`: request shape,
  pagination, CSRF refresh-on-error, retry/backoff classification, error
  mapping. Real HTTP round-trips against a fake server with hand-crafted
  responses derived from real captured traffic.
- **Integration tests gated behind `DEEZER_INTEGRATION=1`**: read-only paths
  (list, paginate) against the live gateway with a real `arl`. Skipped by
  default in regular runs.
- **No automated test of the destructive path.** Verified manually by running
  `--dry-run` first on the real account, then running for real.

## Risks and known unknowns

- **Exact gateway method names & parameter shapes.** Verified against OSS
  libraries at implementation time. The plan must call out this verification
  step explicitly.
- **Rate-limit thresholds.** Unknown. Sequential deletes plus conservative
  exponential backoff is the starting point; tune empirically only if the
  first run reveals a problem.
- **`arl` cookie TTL.** Months in practice, no SLA. Auth-failure detection
  with a clear refresh message covers the eventual expiry.
- **Deezer changing the gateway.** Inherent to choosing the unofficial path.
  Accepted.

## Decisions log

For traceability of choices made during brainstorming:

- **Language: Go.** (Vs Python / TypeScript.) Chosen for skill-building
  alignment with Nils's work environment, and because a static binary fits a
  toolbox repo.
- **Backup mode: in-tool, automatic.** (Vs separate `export` command, vs no
  backup.) Backup is written automatically before any deletion; cheap insurance
  against bugs in the wipe path or against tracks the user actually wanted.
- **Auth: unofficial gateway + `arl` cookie.** (Vs OAuth, vs reusing a
  community `app_id`, vs using Soundiiz/TuneMyMusic.) Forced by Deezer
  closing developer registrations; matches what the OSS ecosystem does;
  enables future tools in this repo without per-tool auth ceremony.
- **Config: TOML file at `~/.config/deezer-tools/config.toml`, `0600`.**
  (Vs env vars, vs all-in-one.) Friendlier on a personal machine; gives a
  natural home for future per-tool state.
- **Confirmation: type-the-count.** (Vs type "yes", vs `--yes` flag.)
  Harder to fat-finger; no non-interactive flag in v1 because no automation
  consumer exists yet.
