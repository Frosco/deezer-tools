# Playlists `apply-record` — Follow-Ups

**Date opened:** 2026-05-07
**Source:** Final-review pass on `wip/playlists-apply-record`.
**Status:** Open. Non-blocking.

## 1. `Run` loved-set fetch should carry the refresh-arl hint

**Where:** `internal/playlistlove/run.go:195-202`.

**What's wrong:** `Run`'s loved-album/loved-artist fetches return
`fmt.Errorf("list loved albums: %w", err)` without distinguishing
`gateway.ErrAuthFailed` from other failures. The user gets `list loved
albums: USER_AUTH_REQUIRED` with no actionable hint about refreshing the
arl.

`ApplyFromRecord` was fixed in this branch — it now uses `wrapLovedFetchErr`
in `internal/playlistlove/apply.go:127-134` to detect auth failures and
surface the standard `"refresh your arl in ~/.config/deezer-tools/config.toml"`
hint, mirroring how `applyPlan` handles auth errors during the apply loop.

`Run`'s playlist-fetch path (`run.go:151-160`) and within-playlist dedup
path (`run.go:184-191`) already wrap auth failures with the hint — only the
loved-set fetch path is missing it.

**Fix:** lift `wrapLovedFetchErr` from `apply.go` into a shared private
helper in `run.go` (or move it to `apply.go` and have `run.go` reach for it
within the same package), then use it for both loved-set fetches in `Run`.

```go
// internal/playlistlove/run.go
lovedAlbums, err := gw.ListFavoriteAlbumIDs(ctx)
if err != nil {
    return nil, wrapLovedFetchErr("list loved albums", err)
}
lovedArtists, err := gw.ListFavoriteArtistIDs(ctx)
if err != nil {
    return nil, wrapLovedFetchErr("list loved artists", err)
}
```

**Test gap to close at the same time:** add `TestRun_authFailureDuringListLoved`
mirroring the two `TestApplyFromRecord_authFailureDuringListLoved*` tests in
`apply_test.go`.

**Why deferred:** pre-existing behavior in `Run` from before the
apply-record branch. The spec for `apply-record` required the hint on this
path, and `ApplyFromRecord` now satisfies it; bringing `Run` into the same
shape is a one-call-site symmetry fix that's safe to do separately.

## 2. Sub-type doc comments on `RecordPlaylist` / `RunRecordStats` / `RecordAlbum` / `RecordArtist`

**Where:** `internal/playlistlove/run.go:78-108`.

**What's wrong:** Task 1 of this branch lifted these types from private to
public. `RunRecord` got a doc comment; the four sub-types did not. Now that
they're part of the public surface (used by `apply.go` and reachable via
`go doc`), each deserves a one-line description.

**Fix:** add a `// RecordPlaylist is one source playlist entry in a RunRecord.`-
style line above each. Trivial.

**Why deferred:** flagged in Task 1's quality review as deferrable; the
package is internal-to-the-repo and the names are self-explanatory.
