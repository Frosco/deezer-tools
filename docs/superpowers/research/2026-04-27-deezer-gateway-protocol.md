# Deezer gw-light Protocol Reference

**Date:** 2026-04-27
**Status:** Reference for `internal/gateway` implementation in deezer-tools.

## Endpoint

`POST https://www.deezer.com/ajax/gw-light.php`

Common query params (set on the URL, not in the body):
- `method` — gateway method name (e.g. `deezer.getUserData`)
- `input` — `3` (constant in every observed source)
- `api_version` — `1.0`
- `api_token` — CSRF token from `deezer.getUserData`. **For the very first call to `deezer.getUserData`, the literal four-character string `"null"` is sent**, not an empty string. After that call returns, `results.checkForm` becomes the api_token for every subsequent method.

Auth: `Cookie: arl=<value>` (HttpOnly cookie from a logged-in browser session).

Request body: JSON. Empty `{}` is acceptable when no params.

Response envelope:
```json
{ "error": ..., "results": ..., "payload": ... }
```
- `error: []` (empty array) → success.
- `error: { "<CODE>": "<message>" }` → failure (an object, not an array).
- `results` → method-specific payload.
- `payload` → optional. When present with `payload.FALLBACK`, the canonical libraries merge those keys into the args and re-call. We don't need that for loved-tracks operations.

## Sources consulted

- **deezer-py** (most authoritative Python reference for `gw.py`):
  `https://github.com/OhMyMndy/deezer-py` @ commit `5cc29f89c332ee6a5843cd13e20e7ad530943c70` (default branch `main`).
  Files: `deezer/gw.py`, `deezer/utils.py` (`map_user_track`), `deezer/__init__.py`, `deezer/errors.py`.
  Note: the original `RemixDev/deezer-py` repo (referenced in some literature) is **not reachable on github.com** as of this date — `gh api repos/RemixDev/deezer-py` returns 404 and `gh api users/RemixDev/repos` returns `[]`. RemixDev's canonical home is on GitLab. `OhMyMndy/deezer-py` carries a verbatim copy of the `deezer/` package (same module path, same class layout), package version `1.3.7` per `__init__.py`. `dracarys69/deezer-py` (last push 2024-02-02) carries the same code. The two independent forks agreeing is a useful cross-check.

- **deemix** (TypeScript port, actively maintained, cross-reference for the same gw-light protocol):
  `https://github.com/bambanah/deemix` @ commit `26f76240b4d16cf472b51cd35fe305801a2fea27` (default branch `main`, 955 stars, last push 2026-04-14).
  File: `packages/deezer-sdk/src/gw.ts`. The class `GW` mirrors deezer-py's `gw.py` method-by-method and is the closest currently-active TypeScript port of RemixDev's original.

- **d-fi-core** (TypeScript, separate lineage):
  `https://github.com/d-fi/d-fi-core` @ commit `9e7f26007f2ee41bc17da6886d9f56358277b05a` (default branch `master`).
  Files: `src/api/api.ts`, `src/api/request.ts`, `src/types/tracks.ts`, `src/types/profile.ts`.
  d-fi-core uses `mobile.pageUser` instead of `deezer.pageProfile` for listing loved tracks; otherwise consistent on field names and envelope shape.

## Methods used by deezer-tools

### deezer.getUserData

- **Source:** `deezer-py/deezer/gw.py` `GW.get_user_data` and `GW.api_call` (the special-cased method); `deemix/packages/deezer-sdk/src/gw.ts` lines for `get_user_data` and `api_call` (`api_token: method === "deezer.getUserData" ? "null" : this.api_token`); login flow in `deezer-py/deezer/__init__.py` `login_via_arl`.
- **Body:** `{}` (empty JSON object).
- **`api_token` query param on the first call:** the literal string `"null"`. Subsequent calls reuse `results.checkForm`.
- **Response shape under `results`:**
  - `checkForm` (string) — the CSRF token to use as `api_token` for subsequent calls.
  - `USER.USER_ID` (number-or-string in JSON; deezer-py treats it as a number for the `== 0` comparison; d-fi-core's `profileType` types `USER_ID` as `string` for other endpoints — gw-light routinely returns IDs as either number or string, so callers should accept both. `json.Number` decoding is the safe play).
  - `USER.BLOG_NAME` (string), `USER.LOVEDTRACKS_ID` (string playlist id) — present but not strictly needed.
  - `USER.OPTIONS.{license_token, web_hq, mobile_hq, web_lossless, mobile_lossless, license_country}` — used by deemix for streaming, not needed here.
  - `USER.MULTI_ACCOUNT.{ENABLED, IS_SUB_ACCOUNT}` — family-account flags.
  - `checkFormLogin` — separate CSRF for `action.php` login (not relevant when authenticating with `arl`).
- **Auth-failure signal:** `USER.USER_ID == 0` indicates the `arl` is invalid or expired. Both deezer-py and deemix detect login failure this way (see `Deezer.login_via_arl` in `__init__.py`). There is no thrown gateway-error in this case; the call returns `200 OK` with `error: []` and a `USER` object whose `USER_ID` is zero.

### Listing loved songs

The plan currently assumes a method named `favorite_song.getList` with body `{user_id, start, nb, tab: "loved"}`. **No such method exists in any of the OSS libraries consulted.** Searching `deezer-py/deezer/gw.py` and `deemix/packages/deezer-sdk/src/gw.ts` for `favorite_song.getList` returns zero hits. Both libraries use one of two paths:

#### Path A: `song.getFavoriteIds` (+ `song.getListData` for metadata) — preferred

- **Source:** `deezer-py/deezer/gw.py` `GW.get_user_favorite_ids` and `GW.get_my_favorite_tracks`; `deemix/packages/deezer-sdk/src/gw.ts` `get_user_favorite_ids` and `get_my_favorite_tracks`.
- **Method name:** `song.getFavoriteIds`
- **Body:** `{ "nb": <int>, "start": <int>, "checksum": <string|null> }`. No `user_id` (the gateway scopes by cookie). No `tab`. Pass `checksum: null` on the first call; subsequent pages may pass back the `checksum` returned by the gateway to detect concurrent changes (both libraries pass `null` and ignore the returned checksum, so we can too).
- **Pagination:** increment `start` by `nb`. The library defaults to `nb=10000` in deezer-py and `nb=25` (from `options.limit`) in deemix; the gateway accepts large values up to at least 10000 in observed code.
- **Response under `results`:**
  - `data` — array of minimal song-id records. Per `get_my_favorite_tracks`, each entry has `SNG_ID`. `DATE_ADD` is *not* guaranteed in this specific call's output — deezer-py's `map_user_track` writes `time_add = DATE_ADD or DATE_FAVORITE`, and the field that comes back from `song.getFavoriteIds` is typically `DATE_ADD` (per OSS-library handling). For metadata (title/artist/album), the library does a follow-up `song.getListData` call.
  - `total` — total count (integer). Used to bound pagination.
  - `checksum` — string, opaque, may be passed back to next call.
- **Method `song.getListData`** for enrichment:
  - Body: `{ "SNG_IDS": [<id>, <id>, ...] }`
  - Returns under `results`: `data` — array of full song records with `SNG_ID, SNG_TITLE, ART_ID, ART_NAME, ALB_ID, ALB_TITLE, ALB_PICTURE, DURATION, RANK_SNG, ...`. Note `ALB_TITLE` and `ART_NAME` are reliably present here (per d-fi-core's `songType` interface and deezer-py's `EMPTY_TRACK_OBJ`).

#### Path B: `deezer.pageProfile` with `tab="loved"`

- **Source:** `deezer-py/deezer/gw.py` `GW.get_user_profile_page` and `GW.get_user_tracks`; `deemix/packages/deezer-sdk/src/gw.ts` same. `d-fi-core` uses `mobile.pageUser` for the same purpose with body `{user_id, tab: 'loved', nb: -1}` — different method name, same shape.
- **Method name:** `deezer.pageProfile`
- **Body:** `{ "USER_ID": <user_id>, "tab": "loved", "nb": <limit> }`. Note `USER_ID` (uppercase), not `user_id` — that matches the rest of the `_ID`-suffixed param naming on this method family. The plan as drafted uses `user_id` lowercase, which is wrong for this method. (For `song.getFavoriteIds` no user_id is needed at all.)
- **Pagination:** This method is single-page in observed library usage. Pass a large `nb` (deemix uses 25 by default; passing `-1` is the d-fi-core convention for "all"). There is no observed `start` parameter for the loved tab.
- **Response under `results`:** `TAB.loved.data` — array of song records with the standard `SNG_ID, SNG_TITLE, ART_NAME, ALB_TITLE`, plus `time_add` source `DATE_ADD` or `DATE_FAVORITE`. Also `TAB.loved.total` and `DATA.USER` block.
- **Note:** the assumed field path `results.data` (top-level) in the plan is wrong for this method — it's `results.TAB.loved.data`.

#### Recommendation for deezer-tools

Use **Path A (`song.getFavoriteIds` + `song.getListData`)** because:
1. It paginates cleanly via `start`/`nb`.
2. It does not depend on `USER_ID` (one less thing to thread through).
3. The two-call pattern is what both deezer-py and deemix actually use in `get_my_favorite_tracks` for the authenticated user's own loved tracks.

If we want the *minimum* number of round-trips and accept single-page risk, Path B with a large `nb` works in one call but returns the same fields under a deeper path.

### favorite_song.remove

- **Source:** `deezer-py/deezer/gw.py` `GW.remove_song_from_favorites`; `deemix/packages/deezer-sdk/src/gw.ts` `remove_song_from_favorites`.
- **Method name:** `favorite_song.remove`
- **Body:** `{ "SNG_ID": "<id>" }`. The capitalized `SNG_ID` is correct (matches every `_ID` field in the gw-light protocol). The id is a string in deezer-py's wrapper but is also accepted as a number — gw-light is lenient. Sending it as a string is safer.
- **Response:** `results` is typically `true` on success (matching the test fixture in the plan). On classified failure, `error` is set as described above.
- **Notes:** complementary methods `favorite_song.add`, `album.addFavorite`/`album.deleteFavorite`, `artist.addFavorite`/`artist.deleteFavorite`, `playlist.addFavorite`/`playlist.deleteFavorite` exist and follow the same body-shape pattern with `ALB_ID`/`ART_ID`/`PLAYLIST_ID` respectively. Not needed for the loved-tracks wipe but worth knowing for future tools.

## Known error codes

Codes I directly observed in OSS source:

| Code (key in `error` object) | Message string seen | Meaning | Our classification |
|---|---|---|---|
| `GATEWAY_ERROR` | `invalid api token` | CSRF token is invalid/expired | `ErrCSRFExpired` |
| `VALID_TOKEN_REQUIRED` | `Invalid CSRF token` | CSRF token is invalid/expired | `ErrCSRFExpired` |
| `GATEWAY_ERROR` | `NEED_USER_AUTH_REQUIRED` | `arl` cookie missing/invalid (must re-authenticate) | `ErrAuthFailed` |

Codes commonly cited in OSS issue trackers but not directly observed in this round of source-reading (treat as plausible-but-unconfirmed; classify on a best-effort basis if encountered):

| Code | Likely meaning | Our classification |
|---|---|---|
| `DATA_ERROR` | resource not found / no data | `ErrNotFound` |
| `ITEMS_LIMIT_EXCEEDED` | too many items in request | `ErrRateLimited` (or specific) |
| `PARAMETER_REQUIRED` | missing required body param | classify as bug, not retried |

The deezer-py public-API client (`deezer/api.py`, separate from gw-light) classifies errors on `api.deezer.com` like `Quota`, `ItemsLimitExceeded`, `Permission`, `InvalidToken`, `ParameterException`, `MissingParameter`, `InvalidQuery`, `DataException`, `IndividualAccountChangedNotAllowed`. Those names are not directly applicable to gw-light error codes but suggest the family naming.

**Detection rule for CSRF expiry** (matched verbatim in deezer-py and deemix):
```
err == {"GATEWAY_ERROR": "invalid api token"}
  OR
err == {"VALID_TOKEN_REQUIRED": "Invalid CSRF token"}
```
We should treat the *key* match (`GATEWAY_ERROR` with a message that contains `invalid api token`, OR any `VALID_TOKEN_REQUIRED`) as the CSRF-expiry signal, since Deezer occasionally tweaks the message string.

## Plan adjustments

The plan's assumed names are partly wrong. Specifically:

1. **Task 7 (list favorite songs):** the assumed method `favorite_song.getList` does **not** exist. The plan and test fixtures need to be updated to use `song.getFavoriteIds` (Path A) for the paginated listing, with body `{nb, start, checksum: null}` (no `user_id`, no `tab`). The response shape stays `results.data` and `results.total` as the plan assumed — that part holds. The per-record fields visible in the response are at minimum `SNG_ID` and `DATE_ADD`/`DATE_FAVORITE`; to get `SNG_TITLE`, `ART_NAME`, `ALB_TITLE` we either (a) follow up with `song.getListData` for chunks of 100–500 IDs, or (b) accept that the backup writes only IDs and dates from this listing (and we look up titles only if the user wants a richer backup).

2. **Task 11 (live integration test):** the test currently calls `listFavoriteSongsOnePage` with a body containing `user_id`, `start`, `nb`, `tab: "loved"`. That body is for the (unverified, possibly nonexistent) `favorite_song.getList`. Update to the `song.getFavoriteIds` shape `{nb, start, checksum: null}`.

3. **Task 6 (CSRF acquisition):** the plan currently sets `c.apiToken = ""` (empty string) before the first `deezer.getUserData` call. The gw-light gateway expects the literal string `"null"` for that first call (per both deezer-py and deemix). The Task 4 `Call` method (which Task 5 doesn't show) needs to special-case `deezer.getUserData` so that when `c.apiToken == ""`, it sends `api_token=null` on the URL. This is a small fix in the gateway client, not a shape change to `csrf.go`.

4. **Task 8 (remove favorite song):** matches the OSS sources. No change needed.

I have applied surgical edits to Tasks 6, 7, 8, and 11 of `docs/superpowers/plans/2026-04-27-wipe-loved-tracks.md` reflecting the above. Other tasks (1–5, 9, 10, 12) are untouched.

## Honest gaps

- I could not directly verify that `song.getFavoriteIds` returns `DATE_ADD` (vs only `DATE_FAVORITE` or only `SNG_ID`) without making a live call. deezer-py's `get_my_favorite_tracks` merges the id-list response with a separate full-song lookup before mapping `time_add`, so the date field could come from either response. The integration test in Task 11 will surface this empirically.
- The exact integer-vs-string typing of `SNG_ID`, `USER_ID`, `total` in the JSON response is not consistent across the OSS libraries — both have appeared in observed payloads. Decode using `json.Number` (Go) and stringify on use is the safe approach. The Task 6 plan already does this for `USER_ID`.
- I could not enumerate the full list of error codes the gateway returns. The three CSRF/auth ones above are the only ones I saw matched verbatim in source. Other codes (`DATA_ERROR`, `ITEMS_LIMIT_EXCEEDED`, etc.) are plausible by analogy with the public API but unconfirmed. Task 3's design (additive switch on the error key) handles unknowns gracefully — they fall through to `ErrUnknown`.
- The original `RemixDev/deezer-py` repo on github.com is gone (404). I used `OhMyMndy/deezer-py` and `dracarys69/deezer-py` as the surviving copies. The deemix (`bambanah/deemix`) cross-reference is what gives me confidence the protocol description is accurate — two independent ports of RemixDev's code agreeing on the wire format.
