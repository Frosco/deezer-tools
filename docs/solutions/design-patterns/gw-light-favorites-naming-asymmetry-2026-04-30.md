---
title: gw-light favorites use asymmetric method names across entity types
date: 2026-04-30
category: design-patterns
module: deezer-tools
problem_type: design_pattern
component: tooling
severity: high
applies_when:
  - Adding loved-X primitives to internal/gateway for any non-song entity (albums, artists, playlists, and likely podcasts/chapters/radios)
  - "Listing user favorites: there is no <entity>.getFavoriteIds for albums or artists"
  - Cross-referencing OSS clients (deemix, deezer-py, d-fi-core) to verify gw-light method names before writing code
  - Reviewing plans that name favorites methods before implementation
tags:
  - deezer
  - gw-light
  - protocol
  - favorites
  - naming-convention
  - reverse-engineered
  - api-asymmetry
---

# gw-light favorites use asymmetric method names across entity types

## Context

While porting the loved-tracks wipe shape to a new `playlists love-contents` tool that touches loved albums and loved artists, the obvious move was to copy `song.getFavoriteIds` / `favorite_song.remove` and rename: `album.getFavoriteIds` / `favorite_album.add`. That is wrong, and gw-light gives almost no feedback when you guess wrong — it just returns its standard not-found error envelope at HTTP 200, which our `classifyError` maps to `ErrUnknown` (non-retryable, non-fatal in a paced loop).

The asymmetry is real and durable: songs use one naming convention, every other entity (albums, artists, playlists) uses a different one for both the write methods *and* the listing call shape. The full source-by-source comparison is in `docs/superpowers/research/2026-04-30-deezer-favorites-protocol.md`.

The discovery only happened because the implementation plan deliberately scoped a pre-implementation research task (Task 2) to verify all new method names against canonical OSS sources before any gateway code was written. The original plan baked in `favorite_album.add` / `album.getFavoriteIds` as unverified assumptions; the research task replaced them with the verified names before Tasks 5–7 dispatched their TDD subagents. (session history)

## Guidance

**Two naming conventions, not one.** Songs sit in their own namespace; everything else uses verb-suffix on the entity namespace:

| Entity   | List                                     | Add                       | Remove                       |
| -------- | ---------------------------------------- | ------------------------- | ---------------------------- |
| song     | `song.getFavoriteIds`                    | `favorite_song.add`       | `favorite_song.remove`       |
| album    | `deezer.pageProfile` (`tab="albums"`)    | `album.addFavorite`       | `album.deleteFavorite`       |
| artist   | `deezer.pageProfile` (`tab="artists"`)   | `artist.addFavorite`      | `artist.deleteFavorite`      |
| playlist | `deezer.pageProfile` (`tab="playlists"`) | `playlist.addFavorite`    | `playlist.deleteFavorite`    |

**Listing is structurally different, not just renamed.** `<entity>.getFavoriteIds` does not exist for non-songs. Listing favorites for albums/artists/playlists goes through `deezer.pageProfile` with a `tab` selector, and the response is nested under `results.TAB.<entity>s.data[]` carrying the entity's own ID field (`ALB_ID`, `ART_ID`, `PLAYLIST_ID`). `nb=2000` is the observed "give me everything" sentinel — the gateway returns a single page rather than paginating, in contrast to song favorites which require start/nb pagination.

**`USER_ID` is required in the `pageProfile` body.** Unlike `song.getFavoriteIds` which scopes to the authenticated user implicitly, `deezer.pageProfile` requires an explicit `USER_ID` field (uppercase). The gateway client already has it after `ensureCSRF` — `c.userID` is populated from `deezer.getUserData`'s `USER.USER_ID`. (session history)

**Various-Artists `ART_ID = 5080`** is the well-known sentinel for compilation albums. Asserted independently by deemix TS (`bambanah/deemix@26f7624`, `packages/deemix/src/types/index.ts`) and the deemix-py port (`snejus/deemix-py@5600852`, `deemix/types/__init__.py`). Safe to treat as a constant; keep an `ART_NAME` case-insensitive fallback in case a regional variant ever appears.

**Don't follow `Tatayoyoh/deezer-tui`.** That single Rust client uses `favorite_album.add` / `favorite_album.remove`, mirroring the song convention. No other OSS gw-light client does, including the same project's neighboring `artist.addFavorite` line — the asymmetry is internal to that project, not a real gateway alias. The convention is verified against deemix (`bambanah/deemix@26f7624`) and deezer-py (`OhMyMndy/deezer-py@5cc29f8`); d-fi-core (`d-fi/d-fi-core@9e7f260`) is read-only and doesn't expose write methods.

## Why This Matters

Guessing `album.getFavoriteIds` doesn't blow up loudly — the gateway returns its standard error envelope at HTTP 200, our `classifyError` doesn't recognize it as any of the known kinds, and it surfaces as `ErrUnknown`. `ErrUnknown` is not in the retry set (`ErrRateLimited` / `ErrServerError`) and not in the abort set (`ErrAuthFailed`), so a paced wipe/love loop would skip every item silently while burning the full pacer budget on each call. That is the same shape of failure that produced the 2026-04-28 Akamai incident: a long, paced run quietly accumulating skips with no human-visible signal until afterwards. Worse, with a wrong method name the failure rate would be 100%, not 55% — every single item silently skipped.

The listing asymmetry is worse than a renamed method, because a copy-paste port of the songs pagination loop wouldn't apply at all — `deezer.pageProfile` is a single-call multi-tab profile fetch, not a paginated favorites endpoint. Reasoning by analogy from `tracks.go` gets you 0% of the way to a working `albums.go`.

The pattern likely extends to other things gw-light exposes through the user profile — podcasts, chapters, radios — though we haven't verified those. Future tools should default to "check `deezer.pageProfile` first, look for `<entity>.addFavorite` / `<entity>.deleteFavorite` second, only assume song-style namespacing if there's a real source confirming it."

## When to Apply

- Adding a new "loved <entity>" primitive to `internal/gateway/` for any entity other than songs.
- Reading a user's favorites of any non-song entity.
- Reviewing OSS gw-light clients and trying to predict whether a method name they reference is real — cross-check at least deemix and deezer-py before trusting a single source.
- Diagnosing a gw-light call that returns `ErrUnknown` on first contact: a wrong method name in this exact convention is high on the suspect list.

## Examples

**Use this shape** — the album listing in `internal/gateway/albums.go`:

```go
const (
    pageProfileMethod      = "deezer.pageProfile"
    addFavoriteAlbumMethod = "album.addFavorite"
    pageProfileNb          = 2000
)

func (c *Client) ListFavoriteAlbumIDs(ctx context.Context) ([]string, error) {
    if err := c.ensureCSRF(ctx); err != nil {
        return nil, err
    }
    body := map[string]any{
        "USER_ID": c.userID,
        "tab":     "albums",
        "nb":      pageProfileNb,
    }
    raw, err := c.callWithCSRF(ctx, pageProfileMethod, body)
    if err != nil {
        return nil, fmt.Errorf("%s tab=albums: %w", pageProfileMethod, err)
    }
    var resp struct {
        TAB struct {
            Albums struct {
                Data  []favoriteAlbumIDRecord `json:"data"`
                Total int                     `json:"total"`
            } `json:"albums"`
        } `json:"TAB"`
    }
    // ... unmarshal + collect r.ID into []string
}
```

`internal/gateway/artists.go` is the same shape with `tab="artists"` and `ART_ID`. Constants live in `albums.go` and are reused; only the per-entity record type and the response struct's tab key change.

**Wrong assumption** (analogy from songs):

```go
// album.getFavoriteIds does not exist; this returns ErrUnknown silently.
const listFavoriteAlbumIdsMethod = "album.getFavoriteIds"

start := 0
for {
    body := map[string]any{"start": start, "nb": pageSize, "checksum": nil}
    raw, err := c.callWithCSRF(ctx, listFavoriteAlbumIdsMethod, body)
    // ... pagination loop modeled on tracks.go ...
}
```

**Right shape** (single call, profile-tab nesting):

```go
body := map[string]any{
    "USER_ID": c.userID,
    "tab":     "albums",
    "nb":      pageProfileNb, // 2000 - "give me everything"
}
raw, err := c.callWithCSRF(ctx, "deezer.pageProfile", body)
// results.TAB.albums.data[].ALB_ID
```

For writes, the corresponding pair is `album.addFavorite` / `album.deleteFavorite` with body `{"ALB_ID": id}` — **not** `favorite_album.add` / `favorite_album.remove`. Substitute `ART_ID` / `artist.*` and `PLAYLIST_ID` / `playlist.*` for the other two entities.

## Related

- Sibling pattern doc: [docs/solutions/design-patterns/gw-light-go-adapter-quirks-2026-04-28.md](./gw-light-go-adapter-quirks-2026-04-28.md) — Go-port pitfalls of gw-light (cookie jar, flexString). Same module, different facet of "gw-light isn't uniform."
- Adjacent failure class: [docs/solutions/integration-issues/quota-error-misclassification-akamai-ip-block-2026-04-29.md](../integration-issues/quota-error-misclassification-akamai-ip-block-2026-04-29.md) — the same "silent-skip in a paced loop" mode this guidance protects against.
- Source of truth: `docs/superpowers/research/2026-04-30-deezer-favorites-protocol.md` (on `main`) — full per-method comparison with line-pinned commit hashes for deemix, deezer-py, d-fi-core, deemix-py.
- Companion protocol doc: `docs/superpowers/research/2026-04-27-deezer-gateway-protocol.md` (on `main`) — the original gw-light envelope and shared error codes.
- Implementation: commits `b382085` (`internal/gateway/albums.go` — `album.addFavorite` + `pageProfile` listing), `82a7cc2` (`internal/gateway/artists.go`), `0950693` (merge to main).
