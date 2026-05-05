# Deezer gw-light album-side protocol

**Date:** 2026-05-05
**Purpose:** Wire-format reference for the three new gateway methods used by
`deezer-tools loved-albums dedupe`.

## Background

The unofficial gw-light gateway is shared with `playlists love-contents` and
`loved-tracks wipe` — same envelope, same CSRF lifecycle, same error
classification. This doc only documents the **new** methods.

The favorites-naming-asymmetry incident
(`docs/solutions/design-patterns/gw-light-favorites-naming-asymmetry-2026-04-30.md`)
is the reason this doc exists at all. The dedupe spec was drafted around three
album-side method-name guesses; one of them (`album.getSongs`) is wrong in
exactly the way `album.getFavoriteIds` was wrong on 2026-04-30. The actual
gw-light method to list an album's tracks is in the **`song.*` namespace**, not
the `album.*` namespace. The other two (`album.getData`, `album.deleteFavorite`)
verify clean.

## Method consensus

| Method                | OSS consensus name      | Body                                  | Response key                                |
| --------------------- | ----------------------- | ------------------------------------- | ------------------------------------------- |
| Get album metadata    | `album.getData`         | `{ "ALB_ID": "<id>" }`                | top-level: `ALB_TITLE`, `ART_ID`, `ART_NAME`, `NB_FAN`, `NUMBER_TRACK` |
| List album tracks     | `song.getListByAlbum`   | `{ "ALB_ID": "<id>", "nb": -1 }`      | `data` (array of track records), `total`, `count` |
| Remove favorite album | `album.deleteFavorite`  | `{ "ALB_ID": "<id>" }`                | response shape unverified in OSS; idempotency unverified |

## Sources consulted

I read the following files at pinned commits.

- **deemix** (TypeScript port): `https://github.com/bambanah/deemix` @ `26f76240b4d16cf472b51cd35fe305801a2fea27`.
  Files: `packages/deezer-sdk/src/gw.ts` (lines 119-121, 123-131, 176-192).

- **deezer-py** (Python — most authoritative gw-light reference): `https://github.com/OhMyMndy/deezer-py` @ `5cc29f89c332ee6a5843cd13e20e7ad530943c70`.
  Files: `deezer/gw.py` (`get_album` at the `album.getData` call site, `get_album_tracks` lines 57-63 for `song.getListByAlbum`, `remove_album_from_favorites` for `album.deleteFavorite`), `deezer/utils.py` (`map_album` lines 273-391, `map_user_album` lines 235-271 — these are where the album-record field names are confirmed).

- **d-fi-core** (TypeScript, separate lineage — independent confirmation of read-side methods and the per-record TS types): `https://github.com/d-fi/d-fi-core` @ `9e7f26007f2ee41bc17da6886d9f56358277b05a`.
  Files: `src/api/api.ts` lines 59-67 (`getAlbumInfo`, `getAlbumTracks`), `src/types/album.ts` (`albumType`, `albumTypeMinimal`, `albumTracksType`).

- **deezspot** (Python — supplementary, read-side only): `https://github.com/jakiepari/deezspot` (HEAD at read time).
  Files: `deezspot/deezloader/deegw_api.py` line 22 (`song.getListByAlbum` constant) and lines 180-186 (call site).

- **RedSea** (Python — third independent confirmation of read-side methods): `https://github.com/Dniel97/RedSea`.
  Files: `deezer/deezer.py` lines 54 (`album.getData`) and 59 (`song.getListByAlbum`).

- **deezer-tui** (Rust — outlier): `https://github.com/Tatayoyoh/deezer-tui` `crates/deezer-core/src/api/gateway.rs` (~lines 520-527). Uses `favorite_album.add` / `favorite_album.remove` — same outlier pattern flagged in the 2026-04-30 favorites doc. Treated as non-canonical; we follow deemix/deezer-py consensus.

GitHub code search for the literal strings was not available (auth required), so cross-source consensus across the four read-side OSS clients (deemix, deezer-py, d-fi-core, deezspot, RedSea) is the verification. For the write-side `album.deleteFavorite`, only deemix and deezer-py reference it directly; that's the same evidence base on which `album.addFavorite` is currently in production via `internal/gateway/albums.go`, so the precedent stands.

## Per-method detail

### `album.getData`

- **Sources (consensus across three independent ports):**
  - deemix `packages/deezer-sdk/src/gw.ts:119-121` (`get_album`)
  - deezer-py `deezer/gw.py` (`get_album` — `return self.api_call("album.getData", {"ALB_ID": alb_id})`)
  - d-fi-core `src/api/api.ts:59-61` (`getAlbumInfo` — `request({alb_id}, 'album.getData')`)
  - RedSea `deezer/deezer.py:54` (`get_album_gw` — `self.gw_api_call('album.getData', {'alb_id': alb_id})`)

- **Method-name string:** `album.getData` (verbatim, case-sensitive). All four sources agree.

- **Body:** `{ "ALB_ID": "<album-id>" }`.
  Casing inconsistency observed: deemix and deezer-py use uppercase `ALB_ID`; d-fi-core and RedSea use lowercase `alb_id`. Same situation as `playlist.getSongs` in the 2026-04-30 favorites doc — the gateway evidently accepts both. **Use uppercase `ALB_ID`** to match `internal/gateway/albums.go`'s existing convention (`AddFavoriteAlbum` already passes `ALB_ID` uppercase) and the deezer-py/deemix consensus.

- **Response under `results` (envelope per the 2026-04-27 protocol doc):** the album record itself, with all top-level fields per d-fi-core's `albumType` interface in `src/types/album.ts`. Fields the dedupe orchestrator needs:

  | Field           | d-fi-core type | Notes                                                         |
  | --------------- | -------------- | ------------------------------------------------------------- |
  | `ALB_ID`        | `string`       | Echoed back. Use `flexString` defensively.                    |
  | `ALB_TITLE`     | `string`       | Album title for display in the diff report.                   |
  | `ART_ID`        | `string`       | Artist ID. Use `flexString`.                                  |
  | `ART_NAME`      | `string`       | Artist display name.                                          |
  | `NB_FAN`        | `number`       | Fan count — used as a tiebreaker for "which dup to keep".     |
  | `NUMBER_TRACK`  | `string` (full `albumType`) / `number` (`albumTypeMinimal`) | Track count. **flexString candidate** — d-fi-core types it as `string` in one interface and `number` in another, mirroring the `SNG_ID` situation we already handle. |
  | `__TYPE__`      | `'album'`      | Discriminator literal; not load-bearing for our use.          |

  Other fields present but unused by dedupe (kept here so the gateway author doesn't have to re-derive them): `ALB_PICTURE`, `LABEL_NAME`, `EXPLICIT_LYRICS`, `EXPLICIT_ALBUM_CONTENT`, `PHYSICAL_RELEASE_DATE`, `DIGITAL_RELEASE_DATE`, `ORIGINAL_RELEASE_DATE`, `VERSION`, `UPC`, `GENRE_ID`, `RANK`, `RANK_ART`, `AVAILABLE`, `NUMBER_DISK`, `COPYRIGHT`, `ARTISTS` (array of contributor records), `TYPE`, `STATUS`, `ROLE_ID`, `ALB_CONTRIBUTORS`. Source: deezer-py `utils.py:273-391` (`map_album`) reads all of these.

- **`flexString` candidates:** `ALB_ID`, `ART_ID` (d-fi-core types both as `string`, deezer-py defensively `str()`-coerces them in `map_album`), and `NUMBER_TRACK` (d-fi-core types it inconsistently across the two album interfaces, exactly the signal that triggered `flexString` adoption for `SNG_ID`). `NB_FAN` is uniformly `number` in d-fi-core and unwrapped in deezer-py — plausibly safe as a plain `int64`, but if a fixture in the integration smoke comes back quoted, promote it.

- **Idempotency / errors:** standard envelope errors only. The most likely failure modes:
  - `DATA_ERROR` — album id doesn't exist on Deezer or has been removed. Map to `ErrNotFound`, skip the album.
  - `VALID_TOKEN_REQUIRED` / `CSRF_TOKEN_INVALID` — handled by the gateway's existing CSRF refresh-and-retry.
  - `QUOTA_ERROR` — gw-light's throttle, already classified `ErrRateLimited`.

### `song.getListByAlbum` (NOT `album.getSongs`)

- **Sources (consensus across four independent ports):**
  - deemix `packages/deezer-sdk/src/gw.ts:123-131` (`get_album_tracks`)
  - deezer-py `deezer/gw.py:57-63` (`get_album_tracks`)
  - d-fi-core `src/api/api.ts:64-67` (`getAlbumTracks` — return type `Promise<albumTracksType>`)
  - deezspot `deezspot/deezloader/deegw_api.py:22,180-186` (`get_album_data`, constant assigned the literal `"song.getListByAlbum"` string)
  - RedSea `deezer/deezer.py:59` (`get_album_tracks_gw`)

- **Method-name string:** `song.getListByAlbum` (verbatim, case-sensitive). **The naming asymmetry is the headline:** it's in the `song.*` namespace, not `album.*`, despite listing tracks for a specific album. This is the same kind of namespace-flip that bit us with `album.getFavoriteIds` (which doesn't exist; you use `deezer.pageProfile` instead). All four read-side OSS clients agree byte-for-byte. **Do not use `album.getSongs`** — no OSS client uses that name.

- **Body:** `{ "ALB_ID": "<album-id>", "nb": -1 }`.
  Same casing inconsistency as `album.getData` — deemix/deezer-py use uppercase `ALB_ID`, d-fi-core/deezspot/RedSea use lowercase `alb_id`. **Use uppercase `ALB_ID`** for the same reasons. d-fi-core additionally passes `lang: 'us'` but it's not required by the gateway (deemix and deezer-py omit it and the call works).

- **Pagination:** `nb: -1` is the convention across all sources — "give me everything, single response". No `start` parameter is observed in any OSS path. For an album track listing this is fine (albums are bounded; the largest commercial release is in the low hundreds of tracks). If the gateway truncates we'll see it during the integration smoke.

- **Response under `results`:** the wrapper shape from d-fi-core's `albumTracksType` (`src/types/album.ts`):

  | Field             | Type                    | Notes                                              |
  | ----------------- | ----------------------- | -------------------------------------------------- |
  | `data`            | `trackType[]`           | Per-track records, see below.                      |
  | `count`           | `number`                | Returned in this response.                         |
  | `total`           | `number`                | Total tracks on the album.                         |
  | `filtered_count`  | `number`                | After server-side filtering — usually `== total`.  |
  | `filtered_items?` | `number[]` (optional)   | Indexes filtered out — usually absent.             |
  | `next?`           | `number` (optional)     | Pagination cursor — only present if server paged.  |

  Same wrapper shape as `playlist.getSongs` from the 2026-04-30 doc. The orchestrator only needs `data[]`.

- **Per-track record fields** (per d-fi-core's `songType` interface in `src/types/tracks.ts`; same shape as the `playlist.getSongs` records documented in the 2026-04-30 doc):
  - `SNG_ID` (string-encoded id) — **flexString**, mirroring `internal/gateway/tracks.go`.
  - `SNG_TITLE` (string)
  - `ALB_ID` (string-encoded id) — flexString.
  - `ART_ID` (string-encoded id) — flexString.
  - `ART_NAME` (string)
  - `DURATION` (string seconds)
  - `TRACK_NUMBER` (number) — d-fi-core types as `number`. Possibly flexString-needed; verify on integration smoke.
  - `__TYPE__: "song"` literal.

  Dedupe only needs `SNG_ID` + `SNG_TITLE` to compute the per-album track-set fingerprint. Everything else is read-through.

- **`flexString` candidates:** all ID-shaped fields (`SNG_ID`, `ALB_ID`, `ART_ID`) — same precedent as `internal/gateway/tracks.go`'s existing decode. `TRACK_NUMBER` is plausibly flexString-needed but the orchestrator doesn't read it for dedupe, so we can defer that decision.

- **Errors:** standard envelope only. Most likely:
  - `DATA_ERROR` — album removed / unavailable in user's region. Skip the album, append to skip log.
  - `QUOTA_ERROR` — `ErrRateLimited`.

### `album.deleteFavorite`

- **Sources:**
  - deemix `packages/deezer-sdk/src/gw.ts:179-181` (`remove_album_from_favorites` — `gw_api_call("album.deleteFavorite", { ALB_ID: alb_id })`)
  - deezer-py `deezer/gw.py` (`remove_album_from_favorites` — `return self.api_call("album.deleteFavorite", {"ALB_ID": alb_id})`)

- **Method-name string:** `album.deleteFavorite` (verbatim, case-sensitive). deemix and deezer-py agree byte-for-byte.

  **Same naming-asymmetry caveat as `album.addFavorite`:** the analogous song-side method is `favorite_song.remove` (in the `favorite_song.*` namespace), but for albums it's `album.deleteFavorite` (in the `album.*` namespace, with the `delete` verb rather than `remove`). The 2026-04-30 favorites doc documents the same flip for `album.addFavorite` vs `favorite_song.add`. The Rust `Tatayoyoh/deezer-tui` client at `crates/deezer-core/src/api/gateway.rs:~527` uses `favorite_album.remove` — same outlier as on the add side. **Don't follow the Rust client.** Two ports of the canonical RemixDev codebase (deemix TS, deezer-py) agree on `album.deleteFavorite`; that's the consensus.

- **Body:** `{ "ALB_ID": "<album-id>" }`. Pass as a string for safety, matching `AddFavoriteAlbum`'s existing convention in `internal/gateway/albums.go:67-72`.

- **Response shape:** **Unverified in OSS.** Both deemix and deezer-py are fire-and-forget — they return whatever `api_call` produced without inspecting it, and there's no docstring or test fixture in either codebase that pins the response. By analogy with `favorite_song.remove` (which the wipe code already exercises in production and which returns a non-error envelope on success), the most-likely shape is one of:
  - `results: true` (boolean) — matches what we observe from `favorite_song.remove` in the wipe.
  - `results: 1` (integer 1).
  - `results: {}` (empty object).

  **Discovery happens at impl time** during the dry-run-then-wet-run flow. If the dedupe orchestrator reads anything off the response, that's the moment to nail this down; if it only checks for absence-of-error (the recommended path, since dedupe is fire-and-forget), the exact success shape doesn't matter.

- **Idempotency on already-unloved album:** **Unknown — not documented in OSS sources.** Same situation as `album.addFavorite` on the add side. Neither library has any code path that distinguishes "removed" from "wasn't loved to begin with"; both treat any non-error response as success. Two plausible behaviors:
  1. The gateway returns the same success shape (silent idempotency). My guess given that no library special-cases it and this matches the song-side behavior we observe.
  2. The gateway returns an `error` envelope with an unknown key. If so, we'd need a classifier branch in `internal/gateway/errors.go` mapping it to a non-error outcome (or a new `ErrAlreadyAbsent` kind treated as success).

  **Discovery plan:** the dedupe spec calls for confirmation-gated unloves with a per-album skip log. If a wet run shows an already-unloved album returning an unclassified error, the skip log captures it and the next iteration adds the classifier branch. This mirrors how the 2026-04-28 Akamai/QUOTA_ERROR incident was discovered and handled.

- **Errors specific to this method:** standard envelope only:
  - `DATA_ERROR` — album id doesn't exist. Map to `ErrNotFound`, skip the album, append to skip log.
  - `VALID_TOKEN_REQUIRED` / `CSRF_TOKEN_INVALID` — CSRF refresh-and-retry.
  - `QUOTA_ERROR` — `ErrRateLimited`. Per the 2026-04-28 incident, this MUST be retried with backoff, not skipped — same `deleteWithRetry` shape as `internal/lovedtracks/wipe.go`.
  - `NEED_USER_AUTH_REQUIRED` / `USER_AUTH_REQUIRED` — `ErrAuthFailed`, abort the run.

## Open unknowns

- **Idempotency response shape on `album.deleteFavorite` for already-unloved albums.** Most likely silent success; possibly an unclassified error envelope. Pin against a real response captured during the first wet run.
- **Exact field name for fan count and track count.** OSS evidence strongly indicates `NB_FAN` (uniform across deezer-py, d-fi-core) and `NUMBER_TRACK` (uniform across the same — there is no `NB_TRACK` observed anywhere). Pin against a real response captured during the first wet run; if the gateway returns a different name in production, this doc is the place to update.
- **`flexString` field set.** Confirmed needed: `ALB_ID`, `ART_ID`, `SNG_ID` (consistent with existing tracks.go precedent and deezer-py's defensive `str()` coercions). Plausibly needed: `NUMBER_TRACK` (d-fi-core types it as both `string` and `number` across two interfaces). Possibly needed: `NB_FAN`, `TRACK_NUMBER` (uniformly typed as `number` in d-fi-core but the gateway has surprised us before — see the SNG_ID precedent in `internal/gateway/tracks.go`).
- **Pagination ceiling on `song.getListByAlbum`.** Every OSS caller passes `nb: -1` and trusts the response. If a real album hits truncation we'll see it; albums are bounded so the risk is low.
- **`album.getData` response shape on a non-existent / removed album.** Most likely `error: { DATA_ERROR: "..." }` per the standard envelope, but unverified.

## Discovery plan

The first wet run captures real response envelopes. If any field name or
error envelope contradicts what's in this doc, update this doc and the
gateway error classifier in the same commit.
