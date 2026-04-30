# Deezer gw-light: Playlist + Favorites (Albums, Artists) Reference

**Date:** 2026-04-30
**Status:** Reference for new methods in `internal/gateway/{playlists,albums,artists}.go` for the `playlists love-contents` subcommand.
**Companion:** `2026-04-27-deezer-gateway-protocol.md` (endpoint, envelope, shared error codes — applies here unchanged).

## Headline findings

The plan (`docs/superpowers/plans/2026-04-30-playlists-love-contents.md`) was drafted around five method-name assumptions. Three of them are **wrong** in the canonical OSS sources:

| Plan assumed                 | Actual gw-light method            | Verdict                              |
|------------------------------|-----------------------------------|--------------------------------------|
| `playlist.getSongs`          | `playlist.getSongs`               | OK                                   |
| `album.getFavoriteIds`       | (does not exist; use `deezer.pageProfile` with `tab="albums"`) | **WRONG**                            |
| `favorite_album.add`         | `album.addFavorite`               | **WRONG**                            |
| `artist.getFavoriteIds`      | (does not exist; use `deezer.pageProfile` with `tab="artists"`) | **WRONG**                            |
| `favorite_artist.add`        | `artist.addFavorite`               | **WRONG**                            |

The plan's instinct was reasonable — `favorite_song.{add,remove}` is in fact the loved-tracks pattern (and the wipe code uses `favorite_song.remove`). But that namespace prefix exists **only** for songs. For albums/artists/playlists the gw-light convention flips: `<entity>.addFavorite` / `<entity>.deleteFavorite`. And there's no `<entity>.getFavoriteIds` for albums or artists at all — the way both reference libraries list a user's favorite albums/artists is `deezer.pageProfile` with the appropriate `tab` value.

I have updated Tasks 5/6/7 of the plan accordingly. See "Plan adjustments" below.

## Sources consulted

I read (and saved locally for cross-check) the following files. Commit hashes are pinned so the citations stay reproducible.

- **deemix** (TypeScript port, actively maintained — closest currently-active port of RemixDev's original):
  `https://github.com/bambanah/deemix` @ commit `26f76240b4d16cf472b51cd35fe305801a2fea27` (default branch `main`, 957 stars, last push 2026-04-14).
  Files: `packages/deezer-sdk/src/gw.ts`, `packages/deemix/src/types/index.ts`, `packages/deemix/src/plugins/spotify.ts`, `packages/deemix/src/download-objects/generatePlaylistItem.ts`.

- **deezer-py** (Python port — most authoritative gw-light reference; surviving copy after RemixDev's repo was taken down):
  `https://github.com/OhMyMndy/deezer-py` @ commit `5cc29f89c332ee6a5843cd13e20e7ad530943c70` (default branch `main`, package version 1.3.7).
  Files: `deezer/gw.py`, `deezer/utils.py`, `deezer/__init__.py`, `deezer/api.py`, `deezer/errors.py`.
  Cross-fork sanity check: `dracarys69/deezer-py` @ commit `82c00ff80f0f5a3566671f34c826f9e41595f061` (last push 2024-02-02) carries the same module byte-for-byte.

- **deemix-py** (Python re-implementation of deemix proper — used to cross-check the Various-Artists constant):
  `https://github.com/snejus/deemix-py` @ commit `5600852d203bcc19862afbe7b4a6b17a7dcb2bfc` (default branch `main`).
  Files: `deemix/types/__init__.py`, `deemix/itemgen.py`.

- **d-fi-core** (TypeScript, separate lineage — independent confirmation of `playlist.getSongs` shape):
  `https://github.com/d-fi/d-fi-core` @ commit `9e7f26007f2ee41bc17da6886d9f56358277b05a` (default branch `master`).
  Files: `src/api/api.ts`, `src/types/playlist.ts`, `src/types/tracks.ts`, `src/types/profile.ts`. d-fi-core is read-only (no add-favorite calls) so it doesn't help with the write-side methods.

- **deezspot** (Python — supplementary cross-reference for `playlist.getSongs` body shape):
  `https://github.com/jakiepari/deezspot` (HEAD at read time) — `deezspot/deezloader/deegw_api.py`.

- **deezer-tui** (Rust — the **only** OSS code I found anywhere that uses the literal name `favorite_album.add`):
  `https://github.com/Tatayoyoh/deezer-tui` — `crates/deezer-core/src/api/gateway.rs`. Treated as a one-off; see the "favorite_album.add" note under `album.addFavorite` below.

GitHub repo URLs from the plan template (`RemixDev/deemix`, `freyr-music/d-fi-core`, `browser-fingerprinting/deezer-py`) all 404 as of this date. RemixDev's canonical home is on GitLab, not GitHub. I used the surviving forks above; the same situation as the 2026-04-27 protocol doc.

## Methods

### `playlist.getSongs`

- **Source:**
  - deemix `packages/deezer-sdk/src/gw.ts:288-299` (`get_playlist_tracks`)
    [`gw.ts@26f7624`](https://github.com/bambanah/deemix/blob/26f76240b4d16cf472b51cd35fe305801a2fea27/packages/deezer-sdk/src/gw.ts#L288-L299)
  - deezer-py `deezer/gw.py:179-187` (`get_playlist_tracks`)
    [`gw.py@5cc29f8`](https://github.com/OhMyMndy/deezer-py/blob/5cc29f89c332ee6a5843cd13e20e7ad530943c70/deezer/gw.py#L179-L187)
  - d-fi-core `src/api/api.ts:63-73` (`getPlaylistTracks`)
    [`api.ts@9e7f260`](https://github.com/d-fi/d-fi-core/blob/9e7f26007f2ee41bc17da6886d9f56358277b05a/src/api/api.ts#L63-L73)

- **Method-name string:** `playlist.getSongs` (verbatim).

- **Body:** sources disagree on parameter casing. Both shapes work in practice (the gateway is lenient about `_ID` casing on this method):
  - deemix + deezer-py: `{ "PLAYLIST_ID": "<id>", "nb": -1 }` — uppercase, only two keys.
  - d-fi-core: `{ "playlist_id": "<id>", "lang": "en", "nb": -1, "start": 0, "tab": 0, "tags": true, "header": true }` — lowercase id, with extras.
  - deezspot: `{ "playlist_id": "<id>", "nb": -1 }` — lowercase id, two keys.

  **Recommendation:** match the deemix/deezer-py pair — `{ "PLAYLIST_ID": "<id>", "nb": -1 }`. Two reasons: (1) all `_ID` body params elsewhere in deezer-tools' gateway are uppercase, and the matching d-fi-core inconsistency suggests the wire-format-canonical form is uppercase; (2) two ports of RemixDev's code agreeing trumps the lone TypeScript fork. The other knobs (`lang`, `tab`, `tags`, `header`, `start`) are defaulted server-side and we don't need them.

  **Pagination:** none observed in any OSS path. Every caller passes `nb: -1` ("all songs at once") and trusts the server to return the full playlist. If the server enforces a per-call cap, we'll discover it in Task 12 (live integration). For our tool, `nb: -1` is fine — playlists are bounded; loved-tracks already pages with `song.getFavoriteIds` and that's a different method.

- **Response under `results` (envelope per the 2026-04-27 doc):**
  - `data` — array of song records.
  - `total` — total count (integer).
  - `count` — count returned in this response (integer; matches `data.length` when fetching everything).
  - `filtered_count` — integer, count after server-side filtering (shouldn't matter for us).
  - `filtered_items?` — optional array of indexes filtered out.
  - `next?` — optional integer offset for pagination (only present when results were paged server-side; absent for `nb: -1` calls).

  Top-level shape per `d-fi-core/src/types/playlist.ts:45-52` ([`playlist.ts@9e7f260`](https://github.com/d-fi/d-fi-core/blob/9e7f26007f2ee41bc17da6886d9f56358277b05a/src/types/playlist.ts#L45-L52)).

- **Per-song record fields** (all present on every record per `d-fi-core/src/types/tracks.ts:23-90` for `songType`, also seen in deezer-py's mapping in `utils.py:51-`):
  - `SNG_ID` (string-encoded number, e.g. `"3135556"`)
  - `SNG_TITLE` (string)
  - `ALB_ID` (string-encoded number, e.g. `"302127"`)
  - `ALB_TITLE` (string)
  - `ALB_PICTURE` (string md5)
  - `ART_ID` (string-encoded number, e.g. `"27"`)
  - `ART_NAME` (string)
  - `ARTISTS` — array of contributor records (each with its own `ART_ID`/`ART_NAME` and a `ROLES` array). For Various-Artists detection the **track-level** `ART_ID` is what matters; we don't need to walk `ARTISTS`.
  - `DURATION` (string seconds)
  - `RANK` / `RANK_SNG` (string)
  - `__TYPE__: "song"` literal.

- **ID quoting:** d-fi-core's TypeScript types call them `string`. deezer-py's `map_user_track` does `str(track["SNG_ID"])` defensively, which is the same precaution `internal/gateway/tracks.go` already takes via `flexString`. Treat all four IDs (`SNG_ID`/`ALB_ID`/`ART_ID`/`PLAYLIST_ID`) as "could be quoted string or bare number on the wire" — `flexString` is the safe decode.

- **Errors specific to this method:** none observed beyond the shared envelope errors documented in `2026-04-27-deezer-gateway-protocol.md`. Most likely failure modes for our use:
  - `DATA_ERROR` — playlist deleted, or visibility mismatch (private playlist that isn't ours). Map to `ErrNotFound`, skip the playlist, continue.
  - `VALID_TOKEN_REQUIRED` / `GATEWAY_ERROR: "invalid api token"` — CSRF expiry, handled by the gateway's existing refresh-and-retry.

### `album.addFavorite` (NOT `favorite_album.add`)

- **Source:**
  - deemix `packages/deezer-sdk/src/gw.ts:380-382` (`add_album_to_favorites`)
    [`gw.ts@26f7624`](https://github.com/bambanah/deemix/blob/26f76240b4d16cf472b51cd35fe305801a2fea27/packages/deezer-sdk/src/gw.ts#L380-L382)
  - deezer-py `deezer/gw.py:254-255` (`add_album_to_favorites`)
    [`gw.py@5cc29f8`](https://github.com/OhMyMndy/deezer-py/blob/5cc29f89c332ee6a5843cd13e20e7ad530943c70/deezer/gw.py#L254-L255)

- **Method-name string:** `album.addFavorite` (verbatim, case-sensitive — deezer-py and deemix agree byte-for-byte).

- **Body:** `{ "ALB_ID": "<album-id>" }`. Both libraries pass the id straight through (no quoting), but downstream the JSON serializer encodes it however the caller's variable was typed; in deemix it's `string | number` and in deezer-py it's whatever the caller passes. Send as a string for safety, matching the wipe's `favorite_song.remove` convention.

- **`favorite_album.add` is also referenced in one third-party Rust client** (`Tatayoyoh/deezer-tui`'s `crates/deezer-core/src/api/gateway.rs:586`, alongside `favorite_album.remove`). Same project uses `artist.addFavorite` for artists, so the asymmetry within that one repo looks like either a bug or an undocumented gw-light alias. **Don't follow it** — every other OSS reference (and the analogous wipe path for `favorite_song.{add,remove}`) uses the `<entity>.addFavorite` / `<entity>.deleteFavorite` form. If the integration test in Task 12 surfaces that the `<entity>.addFavorite` name 404s, that's the signal to swap; until then, go with the consensus.

- **Response shape:** **Unverified.** Both libraries fire-and-forget — they return whatever `api_call` returned without inspecting it, and there's no docstring or test fixture in the OSS code that pins the response. Plausible candidates from the gw-light pattern:
  - `results: true` (boolean), matching `favorite_song.remove`'s observed shape from the wipe research.
  - `results: 1` (integer 1).
  - `results: {}` (empty object).

  **Discovery happens at impl time** (Task 12 integration smoke). Note the live test does NOT call write methods (per the plan), so this will only be confirmed when the actual `playlists love-contents` flow runs end-to-end.

- **Idempotency on already-loved:** **Unknown — not documented in the OSS sources.** Neither library has any code path that distinguishes "added" from "already loved". They call `album.addFavorite`, treat any non-error response as success, and return the raw body to the caller, which is also fire-and-forget. The two plausible behaviors:
  1. The gateway returns the same success shape as a fresh add (silent idempotency). This is the behavior I'd guess given that no library special-cases it.
  2. The gateway returns an `error: { <SOMETHING>: "<message>" }` envelope. If so, the `<SOMETHING>` key is unknown and we'd need to add a classifier branch in `internal/gateway/errors.go` mapping it to a non-error outcome (e.g. introducing an `ErrAlreadyExists` kind, or treating it as success with a flag).

  **Discovery plan:** Task 12 (or earlier, the first wet run on a real account) will surface this. The plan's existing language already accommodates this — see Task 6 step 3: "if `favorite_album.add` for an already-loved album returns an error envelope, add a classifier branch in `internal/gateway/errors.go`." That language survives my T2 edits unchanged, just with the corrected method name.

- **Errors specific to this method:** none observed in OSS. By analogy with the public-API `Quota`/`Permission`/`DataException` family in `deezer-py/deezer/errors.py`, the gw-light variants we're likely to see:
  - `DATA_ERROR` — album id doesn't exist. Map to `ErrNotFound`, skip the album.
  - `QUOTA_ERROR` — gw-light's throttle (already classified `ErrRateLimited` in `internal/gateway/errors.go`; the wipe's documented Akamai incident applies equally here).
  - A potential **loved-albums ceiling** error code. See dedicated section below.

### `artist.addFavorite` (NOT `favorite_artist.add`)

- **Source:**
  - deemix `packages/deezer-sdk/src/gw.ts:388-390` (`add_artist_to_favorites`)
    [`gw.ts@26f7624`](https://github.com/bambanah/deemix/blob/26f76240b4d16cf472b51cd35fe305801a2fea27/packages/deezer-sdk/src/gw.ts#L388-L390)
  - deezer-py `deezer/gw.py:260-261` (`add_artist_to_favorites`)
    [`gw.py@5cc29f8`](https://github.com/OhMyMndy/deezer-py/blob/5cc29f89c332ee6a5843cd13e20e7ad530943c70/deezer/gw.py#L260-L261)

- **Method-name string:** `artist.addFavorite` (verbatim).

- **Body:** `{ "ART_ID": "<artist-id>" }`. Same notes as `album.addFavorite` — pass as a string.

- **Response shape, idempotency, errors:** same situation as `album.addFavorite` (unverified, fire-and-forget in OSS). The same Task 12 / first-wet-run discovery applies.

### Listing favorite albums and artists — there is no `<entity>.getFavoriteIds`

The plan template proposed `album.getFavoriteIds` / `artist.getFavoriteIds` as paginated id-only listings, by analogy with `song.getFavoriteIds`. **Neither method exists in any OSS library I read**, and a GitHub code search across all public repos for the literal strings `"album.getFavoriteIds"` and `"artist.getFavoriteIds"` returned **zero hits**.

What both deezer-py and deemix actually do:

- **deezer-py** `deezer/gw.py:395-411` (`get_user_albums`, `get_user_artists`) — calls `deezer.pageProfile` with `{USER_ID, tab: "albums"|"artists", nb: limit}` and reads `results.TAB.<tab>.data`.
  [`gw.py@5cc29f8`](https://github.com/OhMyMndy/deezer-py/blob/5cc29f89c332ee6a5843cd13e20e7ad530943c70/deezer/gw.py#L395-L411)
- **deemix** `packages/deezer-sdk/src/gw.ts:534-554` (same pattern).
  [`gw.ts@26f7624`](https://github.com/bambanah/deemix/blob/26f76240b4d16cf472b51cd35fe305801a2fea27/packages/deezer-sdk/src/gw.ts#L534-L554)

Confirmed via line `92` of `deezer-py/deezer/gw.py`: the underlying call is `self.api_call("deezer.pageProfile", {"USER_ID": user_id, "tab": tab, "nb": limit})`. The `pageProfile` method shape is the same one used by Path B of the wipe-research doc for `tab="loved"`, and is described there.

- **Method-name string:** `deezer.pageProfile`.
- **Body:** `{ "USER_ID": "<user-id>", "tab": "albums" | "artists", "nb": <limit> }`. Note `USER_ID` is uppercase. The `<user-id>` comes from `deezer.getUserData`'s `USER.USER_ID` — the gateway already exposes this in the existing `internal/gateway` client.
- **Pagination:** single-page in observed library usage; pass a large `nb` (deemix uses 25 by default; passing a higher number like 2000 or `-1` is the convention). Both libraries call `deezer.pageProfile` once and trust the response to contain everything; no `start` param is observed for these tabs. **Risk:** if a user has more loved albums than `nb` allows, we'd silently truncate — Task 12 should bound-test this (or we accept the risk and document it as a known limitation, because the diff against `playlist.getSongs` will only flag what we missed if the user re-runs).
- **Response under `results`:**
  - `TAB.albums.data` (when `tab="albums"`) — array of album records, each with at least:
    - `ALB_ID` (string-encoded id)
    - `ALB_TITLE`, `ALB_PICTURE`, `ART_ID`, `ART_NAME` (per deezer-py's `map_user_album` at `deezer/utils.py:169-205`)
    - `DATE_ADD` and/or `DATE_FAVORITE` (one or the other; deezer-py reads `DATE_ADD or DATE_FAVORITE`)
  - `TAB.albums.total` — integer.
  - Analogous `TAB.artists.data` / `TAB.artists.total` for `tab="artists"`. Per-record fields: `ART_ID`, `ART_NAME`, `ART_PICTURE`, `NB_FAN`, `LOCATION`, `IS_FOLLOWED` (deezer-py `map_user_artist` at `deezer/utils.py:141-167`).
  - `DATA.USER` — block with `USER_ID`, `BLOG_NAME` etc. (not needed for our diff).

- **What the orchestrator needs:** for the playlists love-contents diff, we only need the **set of currently-loved IDs** — `ALB_ID` for albums, `ART_ID` for artists. Everything else in the response is throwaway. The Task 6/7 implementation in the plan is named `ListFavoriteAlbumIDs` / `ListFavoriteArtistIDs` — that contract still holds, the underlying call just changes from a (nonexistent) `<entity>.getFavoriteIds` to a single `deezer.pageProfile` call.

- **Errors specific to this method:** the standard envelope errors. No method-specific code observed. If `nb` is too large the gateway might 4xx; we'd discover that at impl time and clamp.

## Various-Artists `ART_ID`

**Asserted by:**
- deemix TypeScript: `packages/deemix/src/types/index.ts:1` — `export const VARIOUS_ARTISTS = 5080;`
  [`index.ts@26f7624`](https://github.com/bambanah/deemix/blob/26f76240b4d16cf472b51cd35fe305801a2fea27/packages/deemix/src/types/index.ts#L1)
- deemix-py: `deemix/types/__init__.py:1` — `VARIOUS_ARTISTS = "5080"` (note: quoted string here, bare number in TS).
  [`__init__.py@5600852`](https://github.com/snejus/deemix-py/blob/5600852d203bcc19862afbe7b4a6b17a7dcb2bfc/deemix/types/__init__.py#L1)
- Used at: deemix `packages/deemix/src/download-objects/generatePlaylistItem.ts:60` (`dz.api.get_artist(5080)` — comment: "Useful for save as compilation"), deemix-py `deemix/itemgen.py:153` (same call with the same comment), deemix `packages/deemix/src/plugins/spotify.ts:198,279,365` (three call sites, all `dz.api.get_artist(5080)`).

**Stable ID:** `5080` (string `"5080"` on the wire, since gw-light returns IDs as strings). Two fully independent ports (deemix TS, deemix-py) of the same upstream codebase declare it as a named module-level constant rather than inlining it; that's the strongest "this is a well-known canonical value" signal an OSS dependency can give us short of an official API doc.

**Important caveat I want recorded:** all observed uses of `5080` in deemix are for **fetching the Various-Artists profile from the public API** (`https://api.deezer.com/artist/5080`), not for matching artist IDs returned by gw-light. I did not find any OSS code that explicitly *filters* tracks by `ART_ID == "5080"`. Our use of the constant is for filtering the inputs to "should I love the album of this track?" — that filtering logic is novel to deezer-tools, not borrowed.

There's no observed regional variant or A/B-test variant of the Various-Artists ID, but I also couldn't directly disprove the existence of one — neither library special-cases by region for this id.

**Implementation note (already in the plan):** if T12 surfaces tracks whose `ART_NAME` is "Various Artists" but whose `ART_ID` is something other than `5080`, the fallback is the `ART_NAME` case-insensitive match. Don't preemptively code the fallback — start with the `5080` constant as the plan says, and add the name-fallback only if the live data demands it.

## Loved-albums / loved-artists ceiling

**OSS sources:** none. I re-read `deezer/api.py` (deezer-py's public-API wrapper, where `ItemsLimitExceededException` is the public-API error code 100), `deezer/errors.py`, `deezer/gw.py` (no related branch), and the equivalent files in deemix and d-fi-core. No code anywhere references a "loved albums limit" or "loved artists limit" gw-light error code, and Deezer's official UI doesn't surface a numeric cap for loved albums or artists.

**Inference from public-API errors:** deezer-py classifies `error.code == 100` from `api.deezer.com` (NOT gw-light) as `ItemsLimitExceededException` (`deezer/api.py:60-63`). The gw-light analog is **not directly observed**. If the gateway enforces a ceiling, the most-likely `error` envelope key by the public-API naming pattern is `ITEMS_LIMIT_EXCEEDED`, but I'm not asserting that — it's a guess.

**Discovery plan:** at impl time during Task 12 (live integration smoke), if a wet run on a large account yields a sustained error pattern from `album.addFavorite` or `artist.addFavorite` that we can't classify as `ErrRateLimited` or `ErrServerError`, capture the exact `error: { <KEY>: "<MSG>" }` payload, add a new `ErrLimitReached` (or similar) classified kind in `internal/gateway/errors.go`, and update this doc with the verified key. Until then, treat unknown error codes as `ErrUnknown` (the existing fall-through), which will surface them in the skip log without retrying — which is the right behavior for a ceiling: we don't want to retry-storm against a hard limit.

## Plan adjustments

I edited `docs/superpowers/plans/2026-04-30-playlists-love-contents.md` on `main` to reflect the corrected method names and listing path. The edits are surgical — only the affected lines in Tasks 5/6/7 (and the related call-pattern blocks in Tasks 8/9, plus the summary at the bottom) change; the task-by-task structure and verification gates are unchanged.

Specifically:
1. **Task 5** (`playlist.getSongs`): no method-name change; no edit needed beyond minor comment-string sync. The existing body shape `{PLAYLIST_ID, nb: -1}` is consistent with what I read.
2. **Task 6** (`album.getFavoriteIds` + `favorite_album.add`):
   - The listing primitive changes from `album.getFavoriteIds` to `deezer.pageProfile` with `tab="albums"`. Body becomes `{USER_ID, tab: "albums", nb: <limit>}`. Response under `results.TAB.albums.data[]` carrying `ALB_ID` (and other fields we ignore for diffing). The `getFavoriteAlbumIDsMethod` constant is renamed to reflect this.
   - The add primitive changes from `favorite_album.add` to `album.addFavorite`. Body shape `{ALB_ID}` is unchanged.
3. **Task 7** (`artist.getFavoriteIds` + `favorite_artist.add`): symmetric to Task 6 — listing via `deezer.pageProfile` with `tab="artists"`, add via `artist.addFavorite`.
4. **Task 8/9** (orchestration and tests): the assertions on method-name string constants are propagated through; logical structure is unchanged.
5. **Risks summary at bottom of plan**: the bullet listing the assumed names is updated to the verified names.

The pagination contract for `ListFavoriteAlbumIDs` / `ListFavoriteArtistIDs` is now "single call with a high `nb`" rather than "paginated by `start`/`nb`". The orchestrator code path doesn't care (it consumes a `[]string` of ids regardless) — only the gateway implementation differs. If we discover at impl time that the single-call response truncates, we can re-add pagination via `deezer.pageProfile`'s `start` param if it exists, or by switching to the wipe-style two-call `song.getFavoriteIds + song.getListData` path (which doesn't apply here because `<entity>.getFavoriteIds` doesn't exist for albums/artists).

## Honest gaps

- **No live verification** of any of the new methods. Everything in this doc is "what the OSS libraries demonstrate the wire format to be" — the live integration test in Task 12 (and the first wet run) is what closes the loop for response shape on `album.addFavorite` / `artist.addFavorite` and the idempotency behavior. The plan already has scaffolding for this discovery path.
- **`playlist.getSongs` parameter casing:** sources disagree (deemix/deezer-py uppercase `PLAYLIST_ID`, d-fi-core/deezspot lowercase `playlist_id`). The gateway evidently accepts both. We're going with uppercase to match the rest of the wire format.
- **Pagination ceiling on `playlist.getSongs` and `deezer.pageProfile`:** observed callers all pass `nb: -1` or large `nb` and don't paginate. If the server caps the response, we'll see it. None of the OSS libraries handle a paginated `playlist.getSongs` — they all assume "all or nothing" works.
- **Idempotency response shapes:** unknown for `album.addFavorite` and `artist.addFavorite`. Most likely silent success (mirroring how the libraries fire-and-forget), but I can't prove it from source alone. T12 / first wet run is the discovery point.
- **Loved-albums / loved-artists ceiling:** undocumented in OSS. Same — discovered at impl time.
- **Various-Artists `ART_ID = 5080`:** strongly cross-referenced (two independent ports declare it as a named constant), but never used in OSS for the same purpose we'll use it (filtering tracks by `ART_ID`). Plausible-but-unverified that the ID matches the value gw-light returns on a track's `ART_ID` field for compilation tracks; the existing implementation note in the plan (fall back to `ART_NAME` match if needed) is the right contingency.
- **The `Tatayoyoh/deezer-tui` Rust client uses `favorite_album.add`** (the plan's original assumed name). It's the only OSS code I found anywhere that does so. Same project uses `artist.addFavorite` for artists — within-project asymmetry. Treated as an outlier; we follow the deemix/deezer-py consensus. If the live `album.addFavorite` call 404s, that single Rust file is the lead to chase.
