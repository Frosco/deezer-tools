# Loved-Albums Dedupe Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the third subcommand `deezer-tools loved-albums dedupe` which finds and (after one batch confirmation) un-loves duplicate entries in the user's loved-albums list — Case 1 (same artist + same normalised title, different `ALB_ID`s) and Case 2 (a short same-artist album whose title matches a track on a longer same-artist album that's also loved). Additionally adds a within-playlist Case-1 dedup pass to `playlists love-contents`.

**Architecture:** Reuses the existing `internal/gateway` substrate (cookie-jar HTTP client, CSRF lifecycle, error classification) and the existing `internal/throttle` pacer/retry/circuit-breaker discipline. Three new gateway methods live in the existing `internal/gateway/albums.go` (`GetAlbumMetadata`, `ListAlbumTracks`, `RemoveFavoriteAlbum`). A new domain package `internal/lovedalbums` orchestrates list → phase-1 metadata → Case-1 detection → phase-2 tracklists → Case-2 detection → plan → confirm → un-love. `internal/playlistlove` consumes only `lovedalbums.Normalise` and `lovedalbums.PickWinner` for its own within-playlist Case-1 pass; against-loved-albums dedup is left to the standalone command.

**Tech Stack:** Go 1.22+, `github.com/spf13/cobra` (already), stdlib `golang.org/x/text/unicode/norm` for NFKD normalisation (new dependency — single import), `net/http/httptest` for unit tests, stdlib `testing`. No other new third-party dependencies.

**Spec:** `docs/superpowers/specs/2026-05-05-loved-albums-dedupe-design.md`

---

## Pre-Implementation Setup

The spec lives on `main`. Per Nils's CLAUDE.md, design docs / plans / research must not appear in the MR diff. Implementation lands on a WIP branch off `main`. The Task 2 research doc lives on `main` (separate commit).

```bash
git checkout main
git pull --ff-only origin main 2>/dev/null || true
git checkout -b wip/loved-albums-dedupe
```

All implementation commits land on `wip/loved-albums-dedupe`.

**Module path note:** This plan assumes `github.com/niref/deezer-tools` (matches existing `go.mod`). Adding `golang.org/x/text` requires a single `go get` + `go mod tidy` invocation, run as part of Task 6.

---

## File Structure

```
deezer-tools/
├── cmd/deezer-tools/
│   ├── main.go                          # MODIFY: register newLovedAlbumsCmd
│   ├── lovedtracks_cmd.go               # unchanged
│   ├── playlistlove_cmd.go              # unchanged
│   └── lovedalbums_cmd.go               # NEW: cobra wiring
├── internal/
│   ├── config/                          # unchanged
│   ├── gateway/
│   │   ├── albums.go                    # EXTEND: AlbumMetadata, AlbumTrack,
│   │   │                                #   GetAlbumMetadata, ListAlbumTracks,
│   │   │                                #   RemoveFavoriteAlbum
│   │   ├── albums_test.go               # EXTEND: tests for the three new methods
│   │   ├── integration_test.go          # MODIFY: read-only checks for the two
│   │   │                                #   new metadata methods
│   │   └── ... (other files unchanged)
│   ├── throttle/                        # unchanged
│   ├── lovedtracks/                     # unchanged
│   ├── playlistlove/
│   │   ├── diff.go                      # MODIFY: Aggregate now does Case-1
│   │   │                                #   within-playlist dedup with metadata
│   │   ├── diff_test.go                 # EXTEND: tests for the new pass
│   │   ├── run.go                       # MODIFY: Gateway gains GetAlbumMetadata,
│   │   │                                #   stats surface Case1WithinPlaylistSuppressed
│   │   └── run_test.go                  # MODIFY where stats are asserted
│   └── lovedalbums/                     # NEW package
│       ├── match.go                     #   Normalise, DetectCase1, DetectCase2
│       ├── match_test.go
│       ├── plan.go                      #   PickWinner, BuildPlan
│       ├── plan_test.go
│       ├── fetch.go                     #   Phase1Fetch, Phase2Fetch
│       ├── fetch_test.go
│       ├── dedupe.go                    #   Options, Result, Run
│       └── dedupe_test.go
├── .gitignore                           # MODIFY: add deezer-loved-albums-dedupe-*
├── go.mod                               # MODIFY: golang.org/x/text dep
├── go.sum                               # MODIFY: lockfile
└── docs/superpowers/
    ├── plans/2026-05-05-loved-albums-dedupe.md            # this file (on main)
    └── research/2026-05-05-deezer-album-protocol.md       # Task 2 (on main)
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
git checkout -b wip/loved-albums-dedupe
```

- [ ] **Step 4: Verify**

```bash
git rev-parse --abbrev-ref HEAD
```

Expected: `wip/loved-albums-dedupe`.

---

## Task 2: Research and document the new gw-light methods

**Files:**
- Create: `docs/superpowers/research/2026-05-05-deezer-album-protocol.md` (committed to `main`, NOT to the WIP branch)

This task produces no code. The spec explicitly requires verification of method names, parameter shapes, and response field names for `album.getData`, `album.getSongs`, and `album.deleteFavorite` before any new gateway code is written. The favorites-naming-asymmetry incident (`docs/solutions/design-patterns/gw-light-favorites-naming-asymmetry-2026-04-30.md`) is the reason: assumed-by-analogy method names have already cost us once.

Mirrors the structure of `docs/superpowers/research/2026-04-30-deezer-favorites-protocol.md`.

- [ ] **Step 1: Switch to main**

```bash
git stash --include-untracked   # if you have local-only edits; usually no-op
git checkout main
```

- [ ] **Step 2: Read the prior research doc to match its shape**

Open `docs/superpowers/research/2026-04-30-deezer-favorites-protocol.md`. Note the structure: per-method section with method-name string, body shape, response shape, idempotency notes, source citations from deemix / deezer-py / d-fi-core.

- [ ] **Step 3: Verify each new method against OSS sources**

For each of `album.getData`, `album.getSongs`, `album.deleteFavorite`, find the reference in:

- `https://github.com/freyacodes/deemix` — `deemix/api/`
- `https://github.com/uhwot/deezer-py` — `deezer/`
- `https://github.com/d-fi/d-fi-core` — `lib/api/`
- GitHub code search for the literal method-name string across public repos.

For each method capture:
- The exact method-name string (verbatim, case-sensitive).
- The request body shape (which fields are required, types, gw-light quirks).
- The response shape: which fields hold the data we need (album title, artist id+name, fan count, track count for `album.getData`; track id + title for `album.getSongs`).
- Whether IDs / counts come back quoted or unquoted — `flexString` candidates.
- Idempotency / error envelope notes for `album.deleteFavorite` on already-unloved.

If any method's name is contradicted across sources, treat the deemix + deezer-py consensus as authoritative; cite outliers in the doc but plan to use the consensus.

- [ ] **Step 4: Write the research doc**

Create `docs/superpowers/research/2026-05-05-deezer-album-protocol.md` with the same skeleton as the favorites doc:

```markdown
# Deezer gw-light album-side protocol

**Date:** 2026-05-05
**Purpose:** Wire-format reference for the three new gateway methods used by
`deezer-tools loved-albums dedupe`.

## Background

The unofficial gw-light gateway is shared with `playlists love-contents` and
`loved-tracks wipe` — same envelope, same CSRF lifecycle, same error
classification. This doc only documents the **new** methods.

## Method consensus

| Method                | OSS consensus name      | Body                          | Response key        |
| --------------------- | ----------------------- | ----------------------------- | ------------------- |
| Get album metadata    | <verified name>         | `{"ALB_ID":"<id>"}`           | `<verified key>`    |
| List album tracks     | <verified name>         | `{"ALB_ID":"<id>","NB":...}`  | `<verified key>`    |
| Remove favorite album | <verified name>         | `{"ALB_ID":"<id>"}`           | `<verified key>`    |

## Per-method detail

### <verified name for album.getData>

[…include full body/response shapes, sample envelopes, flexString fields,
error code observations, source citations…]

### <verified name for album.getSongs>

[…]

### <verified name for album.deleteFavorite>

[…include the OSS code paths that call it; flag if NO OSS client uses it
under that name — that's the same risk class as `album.getFavoriteIds`.]

## Open unknowns

- Idempotency response shape on `<delete name>` for already-unloved.
- Exact field name for fan count (`NB_FAN`?) and track count (`NB_TRACK`?
  `NUMBER_TRACK`?). Pin against a real response captured during the first
  wet run.
- `flexString` field set: ID-shaped fields default to `flexString`; counts
  may need it too — verified during integration tests with mixed-form
  fixtures.

## Discovery plan

The first wet run captures real response envelopes. If any field name or
error envelope contradicts what's in this doc, update this doc and the
gateway error classifier in the same commit.
```

Replace `<verified name>` and bracketed sections with the actual findings from Step 3. Do NOT leave any `<…>` placeholders in the committed doc — every method must have a verified name.

- [ ] **Step 5: Commit on main**

```bash
git add docs/superpowers/research/2026-05-05-deezer-album-protocol.md
git commit -m "docs: research Deezer gw-light album-side methods"
```

- [ ] **Step 6: Switch back to the WIP branch**

```bash
git checkout wip/loved-albums-dedupe
```

The WIP branch will reference these verified names in Tasks 3–5 but won't include the doc in the diff.

---

## Task 3: Add `GetAlbumMetadata` gateway method

**Files:**
- Modify: `internal/gateway/albums.go`
- Modify: `internal/gateway/albums_test.go`

This task adds the phase-1 metadata fetcher. Wire-format details (method name, response field names, flexString set) come from the Task 2 research doc — substitute the verified strings for the placeholders below.

- [ ] **Step 1: Write the failing happy-path test**

Append to `internal/gateway/albums_test.go`. Replace `<METHOD>`, `<TITLE_KEY>`, `<ART_ID_KEY>`, `<ART_NAME_KEY>`, `<NB_FAN_KEY>`, `<NB_TRACK_KEY>` with verified strings from Task 2:

```go
func TestGetAlbumMetadata_success_mixedFormIDs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("method") {
		case "deezer.getUserData":
			w.Write([]byte(`{"results":{"checkForm":"tok","USER":{"USER_ID":42}}}`))
		case "<METHOD>":
			body, _ := readBody(r)
			s := string(body)
			if !strings.Contains(s, `"ALB_ID":"123"`) {
				t.Errorf("expected ALB_ID=123 in body: %s", s)
			}
			// Mixed-form IDs in the SAME response — the gw-light-quirks
			// learning says non-determinism shows up deep in pagination,
			// so synthetic responses must mix forms within one payload.
			w.Write([]byte(`{"results":{"ALB_ID":123,"<TITLE_KEY>":"Random Access Memories","<ART_ID_KEY>":"8537","<ART_NAME_KEY>":"Daft Punk","<NB_FAN_KEY>":"412000","<NB_TRACK_KEY>":13}}`))
		}
	}))
	defer srv.Close()

	c := New("test-arl")
	c.baseURL = srv.URL

	got, err := c.GetAlbumMetadata(context.Background(), "123")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	want := AlbumMetadata{
		ID: "123", Title: "Random Access Memories",
		ArtistID: "8537", ArtistName: "Daft Punk",
		FanCount: 412000, TrackCount: 13,
	}
	if got != want {
		t.Errorf("got = %+v, want %+v", got, want)
	}
}
```

- [ ] **Step 2: Run, expect FAIL**

```bash
cd /home/niref/dev/frosco/deezer-tools
go test ./internal/gateway -run TestGetAlbumMetadata_success_mixedFormIDs
```

Expected: compile error (`undefined: AlbumMetadata`, `undefined: GetAlbumMetadata`).

- [ ] **Step 3: Implement the method**

Append to `internal/gateway/albums.go`:

```go
const getAlbumMetadataMethod = "<METHOD>" // verified in Task 2 research doc

// AlbumMetadata is the lightweight album record used by lovedalbums dedup
// and by playlistlove's within-playlist Case-1 pass.
type AlbumMetadata struct {
	ID         string
	Title      string
	ArtistID   string
	ArtistName string
	FanCount   int
	TrackCount int
}

// albumMetadataRecord is the on-the-wire shape of one album record returned
// by <METHOD>. All ID-shaped fields use flexString — gw-light returns IDs
// in mixed quoted/numeric forms within a single response payload, see
// docs/solutions/design-patterns/gw-light-go-adapter-quirks-2026-04-28.md.
type albumMetadataRecord struct {
	ID         flexString  `json:"ALB_ID"`
	Title      string      `json:"<TITLE_KEY>"`
	ArtistID   flexString  `json:"<ART_ID_KEY>"`
	ArtistName string      `json:"<ART_NAME_KEY>"`
	FanCount   flexString  `json:"<NB_FAN_KEY>"`
	TrackCount flexString  `json:"<NB_TRACK_KEY>"`
}

// GetAlbumMetadata fetches one album's metadata via gw-light <METHOD>.
// CSRF acquisition and refresh-on-expiry are handled by callWithCSRF.
func (c *Client) GetAlbumMetadata(ctx context.Context, albumID string) (AlbumMetadata, error) {
	body := map[string]any{"ALB_ID": albumID}
	raw, err := c.callWithCSRF(ctx, getAlbumMetadataMethod, body)
	if err != nil {
		return AlbumMetadata{}, err
	}
	var rec albumMetadataRecord
	if err := json.Unmarshal(raw, &rec); err != nil {
		return AlbumMetadata{}, fmt.Errorf("decode %s: %w", getAlbumMetadataMethod, err)
	}
	fans, _ := parseFlexInt(rec.FanCount)
	tracks, _ := parseFlexInt(rec.TrackCount)
	return AlbumMetadata{
		ID: string(rec.ID), Title: rec.Title,
		ArtistID: string(rec.ArtistID), ArtistName: rec.ArtistName,
		FanCount: fans, TrackCount: tracks,
	}, nil
}

// parseFlexInt parses a flexString that might be quoted or unquoted, and
// might be empty. Returns 0, nil for empty input. Returns 0, err if the
// content isn't a valid integer.
func parseFlexInt(s flexString) (int, error) {
	if s == "" {
		return 0, nil
	}
	return strconv.Atoi(string(s))
}
```

Add `"strconv"` to the import block at the top of `albums.go` if it isn't already imported.

- [ ] **Step 4: Run, expect PASS**

```bash
go test ./internal/gateway -run TestGetAlbumMetadata_success_mixedFormIDs
```

Expected: `--- PASS`.

- [ ] **Step 5: Add the error-classification test**

Append to `albums_test.go`:

```go
func TestGetAlbumMetadata_dataErrorMapsToNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("method") {
		case "deezer.getUserData":
			w.Write([]byte(`{"results":{"checkForm":"tok","USER":{"USER_ID":42}}}`))
		case "<METHOD>":
			w.Write([]byte(`{"error":{"DATA_ERROR":"album not found"}}`))
		}
	}))
	defer srv.Close()

	c := New("test-arl")
	c.baseURL = srv.URL

	_, err := c.GetAlbumMetadata(context.Background(), "999999")
	var ge *GatewayError
	if !asGatewayError(err, &ge) || ge.Kind != ErrNotFound {
		t.Errorf("err = %v, want ErrNotFound via DATA_ERROR", err)
	}
}

func TestGetAlbumMetadata_quotaErrorMapsToRateLimited(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("method") {
		case "deezer.getUserData":
			w.Write([]byte(`{"results":{"checkForm":"tok","USER":{"USER_ID":42}}}`))
		case "<METHOD>":
			w.Write([]byte(`{"error":{"QUOTA_ERROR":"Quota exceeded"}}`))
		}
	}))
	defer srv.Close()

	c := New("test-arl")
	c.baseURL = srv.URL

	_, err := c.GetAlbumMetadata(context.Background(), "123")
	var ge *GatewayError
	if !asGatewayError(err, &ge) || ge.Kind != ErrRateLimited {
		t.Errorf("err = %v, want ErrRateLimited via QUOTA_ERROR", err)
	}
}
```

- [ ] **Step 6: Run, expect PASS**

```bash
go test ./internal/gateway -run TestGetAlbumMetadata
```

Expected: 3 tests pass.

- [ ] **Step 7: Run the whole gateway test suite to confirm no regressions**

```bash
go test ./internal/gateway
```

Expected: all green.

- [ ] **Step 8: Commit**

```bash
git add internal/gateway/albums.go internal/gateway/albums_test.go
git commit -m "feat(gateway): GetAlbumMetadata"
```

---

## Task 4: Add `ListAlbumTracks` gateway method

**Files:**
- Modify: `internal/gateway/albums.go`
- Modify: `internal/gateway/albums_test.go`

Verify the method name and response shape against the Task 2 research doc before starting.

- [ ] **Step 1: Write the failing happy-path test**

Replace `<TRACKS_METHOD>`, `<DATA_KEY>`, `<TITLE_KEY>` with verified strings:

```go
func TestListAlbumTracks_success_mixedFormIDs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("method") {
		case "deezer.getUserData":
			w.Write([]byte(`{"results":{"checkForm":"tok","USER":{"USER_ID":42}}}`))
		case "<TRACKS_METHOD>":
			body, _ := readBody(r)
			s := string(body)
			if !strings.Contains(s, `"ALB_ID":"123"`) {
				t.Errorf("expected ALB_ID=123 in body: %s", s)
			}
			// Mixed quoted/unquoted SNG_ID in the same response chunk.
			w.Write([]byte(`{"results":{"<DATA_KEY>":[{"SNG_ID":"1","<TITLE_KEY>":"Get Lucky"},{"SNG_ID":2,"<TITLE_KEY>":"Instant Crush"}]}}`))
		}
	}))
	defer srv.Close()

	c := New("test-arl")
	c.baseURL = srv.URL

	got, err := c.ListAlbumTracks(context.Background(), "123")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2", len(got))
	}
	if got[0] != (AlbumTrack{ID: "1", Title: "Get Lucky"}) ||
		got[1] != (AlbumTrack{ID: "2", Title: "Instant Crush"}) {
		t.Errorf("got = %+v", got)
	}
}
```

- [ ] **Step 2: Run, expect FAIL**

```bash
go test ./internal/gateway -run TestListAlbumTracks_success_mixedFormIDs
```

Expected: compile error (`undefined: AlbumTrack`, `undefined: ListAlbumTracks`).

- [ ] **Step 3: Implement**

Append to `internal/gateway/albums.go`:

```go
const listAlbumTracksMethod = "<TRACKS_METHOD>" // verified in Task 2 research doc

// AlbumTrack is one track on an album, used for Case-2 detection.
type AlbumTrack struct {
	ID    string
	Title string
}

type albumTrackRecord struct {
	ID    flexString `json:"SNG_ID"`
	Title string     `json:"<TITLE_KEY>"`
}

// ListAlbumTracks returns the tracks on one album. Used only in phase 2 of
// loved-albums dedup.
//
// gw-light's <TRACKS_METHOD> returns all tracks in a single call for any
// reasonable album size. If a wet run surfaces pagination, switch to a
// start/nb loop following ListFavoriteSongs's stage-1 shape.
func (c *Client) ListAlbumTracks(ctx context.Context, albumID string) ([]AlbumTrack, error) {
	body := map[string]any{"ALB_ID": albumID, "NB": -1}
	raw, err := c.callWithCSRF(ctx, listAlbumTracksMethod, body)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Data []albumTrackRecord `json:"<DATA_KEY>"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("decode %s: %w", listAlbumTracksMethod, err)
	}
	out := make([]AlbumTrack, 0, len(resp.Data))
	for _, r := range resp.Data {
		out = append(out, AlbumTrack{ID: string(r.ID), Title: r.Title})
	}
	return out, nil
}
```

- [ ] **Step 4: Run, expect PASS**

```bash
go test ./internal/gateway -run TestListAlbumTracks_success_mixedFormIDs
```

- [ ] **Step 5: Add the error-classification test**

```go
func TestListAlbumTracks_dataErrorMapsToNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("method") {
		case "deezer.getUserData":
			w.Write([]byte(`{"results":{"checkForm":"tok","USER":{"USER_ID":42}}}`))
		case "<TRACKS_METHOD>":
			w.Write([]byte(`{"error":{"DATA_ERROR":"album not found"}}`))
		}
	}))
	defer srv.Close()

	c := New("test-arl")
	c.baseURL = srv.URL

	_, err := c.ListAlbumTracks(context.Background(), "999999")
	var ge *GatewayError
	if !asGatewayError(err, &ge) || ge.Kind != ErrNotFound {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}
```

- [ ] **Step 6: Run, expect PASS**

```bash
go test ./internal/gateway -run TestListAlbumTracks
```

- [ ] **Step 7: Run full gateway suite**

```bash
go test ./internal/gateway
```

- [ ] **Step 8: Commit**

```bash
git add internal/gateway/albums.go internal/gateway/albums_test.go
git commit -m "feat(gateway): ListAlbumTracks"
```

---

## Task 5: Add `RemoveFavoriteAlbum` gateway method

**Files:**
- Modify: `internal/gateway/albums.go`
- Modify: `internal/gateway/albums_test.go`

Symmetric with `AddFavoriteAlbum`. Verify the method-name string against the Task 2 research doc.

- [ ] **Step 1: Write the failing happy-path test**

```go
func TestRemoveFavoriteAlbum_success(t *testing.T) {
	var seenALB string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("method") {
		case "deezer.getUserData":
			w.Write([]byte(`{"results":{"checkForm":"tok","USER":{"USER_ID":42}}}`))
		case "<DELETE_METHOD>":
			body, _ := readBody(r)
			s := string(body)
			if strings.Contains(s, `"ALB_ID":"123"`) {
				seenALB = "123"
			}
			w.Write([]byte(`{"results":true}`))
		}
	}))
	defer srv.Close()

	c := New("test-arl")
	c.baseURL = srv.URL

	if err := c.RemoveFavoriteAlbum(context.Background(), "123"); err != nil {
		t.Fatalf("err = %v", err)
	}
	if seenALB != "123" {
		t.Errorf("server did not see ALB_ID=123 (seen=%q)", seenALB)
	}
}

func TestRemoveFavoriteAlbum_dataErrorMapsToNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("method") {
		case "deezer.getUserData":
			w.Write([]byte(`{"results":{"checkForm":"tok","USER":{"USER_ID":42}}}`))
		case "<DELETE_METHOD>":
			w.Write([]byte(`{"error":{"DATA_ERROR":"not in favorites"}}`))
		}
	}))
	defer srv.Close()

	c := New("test-arl")
	c.baseURL = srv.URL

	err := c.RemoveFavoriteAlbum(context.Background(), "999999")
	var ge *GatewayError
	if !asGatewayError(err, &ge) || ge.Kind != ErrNotFound {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}
```

- [ ] **Step 2: Run, expect FAIL**

```bash
go test ./internal/gateway -run TestRemoveFavoriteAlbum
```

Expected: compile error (`undefined: RemoveFavoriteAlbum`).

- [ ] **Step 3: Implement**

Append to `internal/gateway/albums.go`:

```go
const removeFavoriteAlbumMethod = "<DELETE_METHOD>" // verified in Task 2 research doc

// RemoveFavoriteAlbum un-loves the album with the given Deezer ALB_ID.
// Symmetric with AddFavoriteAlbum (album.addFavorite). On already-unloved,
// the wet-run-discovered classification is DATA_ERROR → ErrNotFound; callers
// treat that as a one-shot skip (do NOT retry).
//
// CSRF acquisition and refresh are handled by callWithCSRF.
// Returns *GatewayError on classified failure.
func (c *Client) RemoveFavoriteAlbum(ctx context.Context, albumID string) error {
	body := map[string]any{"ALB_ID": albumID}
	if _, err := c.callWithCSRF(ctx, removeFavoriteAlbumMethod, body); err != nil {
		return err
	}
	return nil
}
```

- [ ] **Step 4: Run, expect PASS**

```bash
go test ./internal/gateway -run TestRemoveFavoriteAlbum
```

- [ ] **Step 5: Run full gateway suite + vet + build**

```bash
go vet ./internal/gateway
go test ./internal/gateway
go build ./...
```

- [ ] **Step 6: Commit**

```bash
git add internal/gateway/albums.go internal/gateway/albums_test.go
git commit -m "feat(gateway): RemoveFavoriteAlbum"
```

---

## Task 6: `lovedalbums.Normalise`

**Files:**
- Create: `internal/lovedalbums/match.go`
- Create: `internal/lovedalbums/match_test.go`
- Modify: `go.mod`, `go.sum`

This is the first file in the new package. Adds the `golang.org/x/text` dependency for NFKD normalisation.

- [ ] **Step 1: Add the dependency**

```bash
cd /home/niref/dev/frosco/deezer-tools
go get golang.org/x/text@latest
go mod tidy
```

- [ ] **Step 2: Write the failing test**

Create `internal/lovedalbums/match_test.go`:

```go
package lovedalbums

import "testing"

func TestNormalise(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"identity", "random access memories", "random access memories"},
		{"casefold", "Random Access Memories", "random access memories"},
		{"shouty", "RANDOM ACCESS MEMORIES", "random access memories"},
		{"accent_fold", "Café", "cafe"},
		{"accent_fold_compound", "École", "ecole"},
		{"apostrophe_strip", "It's", "its"},
		{"hyphen_strip", "Walk-On", "walk on"},
		{"parens_strip", "Title (Live)", "title live"},
		{"brackets_strip", "Title [Bonus]", "title bonus"},
		{"colon_strip", "Vol: 1", "vol 1"},
		{"double_space_collapse", "A  B   C", "a b c"},
		{"leading_trailing_space", "  A B  ", "a b"},
		{"unicode_full_width_digit", "Vol １", "vol 1"}, // NFKD folds ﹙１﹚-style fullwidth
		{"empty", "", ""},
		{"only_punctuation", "***", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Normalise(tc.in)
			if got != tc.want {
				t.Errorf("Normalise(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
```

- [ ] **Step 3: Run, expect FAIL**

```bash
go test ./internal/lovedalbums -run TestNormalise
```

Expected: compile error (`undefined: Normalise`).

- [ ] **Step 4: Implement**

Create `internal/lovedalbums/match.go`:

```go
// Package lovedalbums detects and removes duplicate entries in a Deezer
// account's loved-albums list. Two duplicate cases are detected:
//
//   - Case 1: same artist, same normalised album title, different ALB_IDs.
//   - Case 2: a short loved album (≤ Case2TrackThreshold tracks) whose title
//     matches a track on a longer same-artist album that is also loved.
//
// The package owns the matching rules; callers (the dedupe orchestrator and
// playlistlove's within-playlist Case-1 pass) own their own gateway IO.
package lovedalbums

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// Normalise applies the title-normalisation rules used for both Case-1
// grouping and Case-2 album-vs-track equality:
//
//  1. NFKD decompose
//  2. drop combining marks (so "Café" → "Cafe")
//  3. lowercase
//  4. drop runes that are not letters / digits / spaces
//  5. collapse whitespace runs to a single space
//  6. trim leading and trailing whitespace
//
// Edition suffixes like "(Deluxe)" survive only their punctuation: the
// normalised title still contains "deluxe", which keeps deluxes distinct
// from the base title — that's deliberate (see the design spec).
func Normalise(s string) string {
	decomposed := norm.NFKD.String(s)
	var b strings.Builder
	b.Grow(len(decomposed))
	for _, r := range decomposed {
		if unicode.Is(unicode.Mn, r) { // combining marks
			continue
		}
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r):
			b.WriteRune(unicode.ToLower(r))
		case unicode.IsSpace(r):
			b.WriteRune(' ')
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}
```

- [ ] **Step 5: Run, expect PASS**

```bash
go test ./internal/lovedalbums -run TestNormalise
```

Expected: all sub-tests green.

- [ ] **Step 6: Build everything**

```bash
go build ./...
```

- [ ] **Step 7: Commit**

```bash
git add go.mod go.sum internal/lovedalbums/match.go internal/lovedalbums/match_test.go
git commit -m "feat(lovedalbums): title normalisation"
```

---

## Task 7: `lovedalbums.PickWinner`

**Files:**
- Create: `internal/lovedalbums/plan.go`
- Create: `internal/lovedalbums/plan_test.go`

Pure function. Strict ordering: most tracks → highest fans → lowest ALB_ID.

- [ ] **Step 1: Write the failing tests**

Create `internal/lovedalbums/plan_test.go`:

```go
package lovedalbums

import (
	"testing"

	"github.com/niref/deezer-tools/internal/gateway"
)

func TestPickWinner_mostTracksFirst(t *testing.T) {
	group := []gateway.AlbumMetadata{
		{ID: "1", TrackCount: 1, FanCount: 999999}, // single, popular
		{ID: "2", TrackCount: 12, FanCount: 100},   // album, niche
	}
	got := PickWinner(group)
	if got[0].ID != "2" {
		t.Errorf("winner = %s, want 2 (more tracks beats more fans)", got[0].ID)
	}
}

func TestPickWinner_fansBreakTrackTie(t *testing.T) {
	group := []gateway.AlbumMetadata{
		{ID: "1", TrackCount: 13, FanCount: 100},
		{ID: "2", TrackCount: 13, FanCount: 999999},
	}
	got := PickWinner(group)
	if got[0].ID != "2" {
		t.Errorf("winner = %s, want 2 (fans break track tie)", got[0].ID)
	}
}

func TestPickWinner_lowestIDBreaksFinalTie(t *testing.T) {
	group := []gateway.AlbumMetadata{
		{ID: "200", TrackCount: 13, FanCount: 100},
		{ID: "100", TrackCount: 13, FanCount: 100},
		{ID: "300", TrackCount: 13, FanCount: 100},
	}
	got := PickWinner(group)
	if got[0].ID != "100" {
		t.Errorf("winner = %s, want 100 (lowest ID)", got[0].ID)
	}
}

func TestPickWinner_idCompareIsNumeric(t *testing.T) {
	// Lexicographic comparison would put "9" after "100"; numeric-aware
	// comparison must put "9" first.
	group := []gateway.AlbumMetadata{
		{ID: "100", TrackCount: 1, FanCount: 1},
		{ID: "9", TrackCount: 1, FanCount: 1},
	}
	got := PickWinner(group)
	if got[0].ID != "9" {
		t.Errorf("winner = %s, want 9", got[0].ID)
	}
}

func TestPickWinner_returnsAllInOrder(t *testing.T) {
	group := []gateway.AlbumMetadata{
		{ID: "B", TrackCount: 1},
		{ID: "A", TrackCount: 5},
		{ID: "C", TrackCount: 3},
	}
	got := PickWinner(group)
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	if got[0].ID != "A" || got[1].ID != "C" || got[2].ID != "B" {
		t.Errorf("order = [%s %s %s], want [A C B]", got[0].ID, got[1].ID, got[2].ID)
	}
}
```

- [ ] **Step 2: Run, expect FAIL**

```bash
go test ./internal/lovedalbums -run TestPickWinner
```

Expected: compile error (`undefined: PickWinner`).

- [ ] **Step 3: Implement**

Create `internal/lovedalbums/plan.go`:

```go
package lovedalbums

import (
	"sort"
	"strconv"

	"github.com/niref/deezer-tools/internal/gateway"
)

// PickWinner sorts a group of Case-1 candidates so the canonical album is at
// index 0 and the losers (to be un-loved) follow. The strict ordering is:
//
//  1. most tracks first
//  2. ties → highest fans first
//  3. ties → lowest ALB_ID first (numeric comparison; "9" < "100")
//
// If two members compare equal under all three keys, the input order is
// preserved (stable sort).
//
// The returned slice is a new slice; the caller's slice is not mutated.
func PickWinner(group []gateway.AlbumMetadata) []gateway.AlbumMetadata {
	out := make([]gateway.AlbumMetadata, len(group))
	copy(out, group)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].TrackCount != out[j].TrackCount {
			return out[i].TrackCount > out[j].TrackCount
		}
		if out[i].FanCount != out[j].FanCount {
			return out[i].FanCount > out[j].FanCount
		}
		return idLess(out[i].ID, out[j].ID)
	})
	return out
}

// idLess compares two ALB_IDs numerically when both parse as integers, and
// falls back to lexicographic comparison otherwise. The numeric path is the
// expected one — Deezer IDs are integers — but the fallback prevents a
// panic on any unexpected non-numeric ID.
func idLess(a, b string) bool {
	ai, aerr := strconv.Atoi(a)
	bi, berr := strconv.Atoi(b)
	if aerr == nil && berr == nil {
		return ai < bi
	}
	return a < b
}
```

- [ ] **Step 4: Run, expect PASS**

```bash
go test ./internal/lovedalbums -run TestPickWinner
```

Expected: all 5 tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/lovedalbums/plan.go internal/lovedalbums/plan_test.go
git commit -m "feat(lovedalbums): PickWinner"
```

---

## Task 8: `lovedalbums.DetectCase1`

**Files:**
- Modify: `internal/lovedalbums/match.go`
- Modify: `internal/lovedalbums/match_test.go`

Detect groups of same-artist same-normalised-title albums with ≥2 members.

- [ ] **Step 1: Write the failing tests**

Append to `internal/lovedalbums/match_test.go`:

```go
import (
	"sort"
	"testing"

	"github.com/niref/deezer-tools/internal/gateway"
)

func TestDetectCase1_groupsSameArtistSameTitle(t *testing.T) {
	loved := []gateway.AlbumMetadata{
		{ID: "1", Title: "Random Access Memories", ArtistID: "8537", TrackCount: 13, FanCount: 1000},
		{ID: "2", Title: "RANDOM ACCESS MEMORIES", ArtistID: "8537", TrackCount: 13, FanCount: 5},
		{ID: "3", Title: "Discovery", ArtistID: "8537", TrackCount: 14, FanCount: 100},
	}
	groups := DetectCase1(loved)
	if len(groups) != 1 {
		t.Fatalf("len(groups) = %d, want 1", len(groups))
	}
	g := groups[0]
	if g.ArtistID != "8537" {
		t.Errorf("ArtistID = %s", g.ArtistID)
	}
	if g.NormalisedKey != "random access memories" {
		t.Errorf("NormalisedKey = %s", g.NormalisedKey)
	}
	if len(g.Members) != 2 {
		t.Errorf("Members = %d, want 2", len(g.Members))
	}
	if g.Members[0].ID != "1" {
		t.Errorf("winner = %s, want 1", g.Members[0].ID)
	}
}

func TestDetectCase1_doesNotGroupAcrossArtists(t *testing.T) {
	// Same title, different artists → not a Case-1 group.
	loved := []gateway.AlbumMetadata{
		{ID: "1", Title: "Greatest Hits", ArtistID: "1"},
		{ID: "2", Title: "Greatest Hits", ArtistID: "2"},
	}
	groups := DetectCase1(loved)
	if len(groups) != 0 {
		t.Errorf("len(groups) = %d, want 0", len(groups))
	}
}

func TestDetectCase1_singletonsAreNotGroups(t *testing.T) {
	loved := []gateway.AlbumMetadata{
		{ID: "1", Title: "A", ArtistID: "1"},
		{ID: "2", Title: "B", ArtistID: "1"},
	}
	groups := DetectCase1(loved)
	if len(groups) != 0 {
		t.Errorf("len(groups) = %d, want 0", len(groups))
	}
}

func TestDetectCase1_threeMemberGroup(t *testing.T) {
	loved := []gateway.AlbumMetadata{
		{ID: "1", Title: "X", ArtistID: "1", TrackCount: 1},
		{ID: "2", Title: "x", ArtistID: "1", TrackCount: 5},
		{ID: "3", Title: "X ", ArtistID: "1", TrackCount: 3},
	}
	groups := DetectCase1(loved)
	if len(groups) != 1 || len(groups[0].Members) != 3 {
		t.Fatalf("groups = %+v", groups)
	}
	// PickWinner ordering: most tracks first.
	if groups[0].Members[0].ID != "2" {
		t.Errorf("winner = %s, want 2", groups[0].Members[0].ID)
	}
}

func TestDetectCase1_deterministicOrder(t *testing.T) {
	// Two groups → must come back in stable order across runs.
	// Order is sorted by ArtistID asc, then NormalisedKey asc.
	loved := []gateway.AlbumMetadata{
		{ID: "a1", Title: "B", ArtistID: "2"},
		{ID: "a2", Title: "B", ArtistID: "2"},
		{ID: "b1", Title: "A", ArtistID: "1"},
		{ID: "b2", Title: "A", ArtistID: "1"},
	}
	groups := DetectCase1(loved)
	if len(groups) != 2 {
		t.Fatalf("len = %d, want 2", len(groups))
	}
	if groups[0].ArtistID != "1" || groups[1].ArtistID != "2" {
		t.Errorf("artist order = [%s %s]", groups[0].ArtistID, groups[1].ArtistID)
	}
}

// helper used by later tests
func ids(group []gateway.AlbumMetadata) []string {
	out := make([]string, len(group))
	for i, m := range group {
		out[i] = m.ID
	}
	sort.Strings(out)
	return out
}
```

The `import` block at the top of `match_test.go` may already have a `testing` line; merge the new imports rather than duplicating.

- [ ] **Step 2: Run, expect FAIL**

```bash
go test ./internal/lovedalbums -run TestDetectCase1
```

Expected: compile error (`undefined: DetectCase1`, `undefined: Case1Group`).

- [ ] **Step 3: Implement**

Append to `internal/lovedalbums/match.go`:

```go
import (
	// existing imports (strings, unicode, golang.org/x/text/unicode/norm) plus:
	"sort"

	"github.com/niref/deezer-tools/internal/gateway"
)

// Case1Group is a set of loved albums that share the same artist and the
// same normalised title. Members[0] is the winner (PickWinner ordering);
// the remaining members are losers to be un-loved.
type Case1Group struct {
	ArtistID      string
	ArtistName    string
	NormalisedKey string
	Members       []gateway.AlbumMetadata
}

// DetectCase1 returns one Case1Group per duplicate cluster found in the
// loved-album set. Singletons (no duplicate) are not returned. The result is
// sorted deterministically by (ArtistID, NormalisedKey) so two runs over the
// same input produce identical output.
func DetectCase1(loved []gateway.AlbumMetadata) []Case1Group {
	type key struct{ artist, title string }
	bucket := make(map[key][]gateway.AlbumMetadata)
	artistName := make(map[string]string)
	for _, a := range loved {
		k := key{a.ArtistID, Normalise(a.Title)}
		bucket[k] = append(bucket[k], a)
		if _, ok := artistName[a.ArtistID]; !ok {
			artistName[a.ArtistID] = a.ArtistName
		}
	}
	out := make([]Case1Group, 0)
	for k, members := range bucket {
		if len(members) < 2 {
			continue
		}
		out = append(out, Case1Group{
			ArtistID:      k.artist,
			ArtistName:    artistName[k.artist],
			NormalisedKey: k.title,
			Members:       PickWinner(members),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ArtistID != out[j].ArtistID {
			return out[i].ArtistID < out[j].ArtistID
		}
		return out[i].NormalisedKey < out[j].NormalisedKey
	})
	return out
}
```

Move the `import` block at the top of `match.go` to include `sort` and the gateway package alongside the existing imports.

- [ ] **Step 4: Run, expect PASS**

```bash
go test ./internal/lovedalbums -run TestDetectCase1
```

- [ ] **Step 5: Run the full lovedalbums suite**

```bash
go test ./internal/lovedalbums
```

Expected: all green.

- [ ] **Step 6: Commit**

```bash
git add internal/lovedalbums/match.go internal/lovedalbums/match_test.go
git commit -m "feat(lovedalbums): DetectCase1"
```

---

## Task 9: `lovedalbums.DetectCase2`

**Files:**
- Modify: `internal/lovedalbums/match.go`
- Modify: `internal/lovedalbums/match_test.go`

Detect short albums whose title equals a track on a longer same-artist loved album. Operates on the post-Case-1 set (the caller is responsible for removing Case-1 losers before calling).

- [ ] **Step 1: Write the failing tests**

Append to `internal/lovedalbums/match_test.go`:

```go
func TestDetectCase2_shortMatchesTrackOnLong(t *testing.T) {
	post := []gateway.AlbumMetadata{
		{ID: "S", Title: "Foo", ArtistID: "1", TrackCount: 1},
		{ID: "L", Title: "Bar", ArtistID: "1", TrackCount: 12},
	}
	tracks := func(albumID string) ([]gateway.AlbumTrack, error) {
		if albumID == "L" {
			return []gateway.AlbumTrack{{ID: "t1", Title: "Foo"}, {ID: "t2", Title: "Other"}}, nil
		}
		t.Fatalf("unexpected ListTracks for %s", albumID)
		return nil, nil
	}
	groups, err := DetectCase2(post, tracks, 3)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(groups) != 1 || groups[0].Parent.ID != "L" || len(groups[0].Shorts) != 1 || groups[0].Shorts[0].ID != "S" {
		t.Errorf("groups = %+v", groups)
	}
}

func TestDetectCase2_shortMustBeShorterThanThreshold(t *testing.T) {
	// Boundary: TrackCount == threshold is short; TrackCount > threshold is
	// not short. Threshold default is 3.
	post := []gateway.AlbumMetadata{
		{ID: "S3", Title: "Foo", ArtistID: "1", TrackCount: 3},
		{ID: "S4", Title: "Foo", ArtistID: "2", TrackCount: 4},
		{ID: "L1", Title: "Bar", ArtistID: "1", TrackCount: 12},
		{ID: "L2", Title: "Bar", ArtistID: "2", TrackCount: 12},
	}
	tracks := func(id string) ([]gateway.AlbumTrack, error) {
		switch id {
		case "L1":
			return []gateway.AlbumTrack{{ID: "t", Title: "Foo"}}, nil
		case "L2":
			return []gateway.AlbumTrack{{ID: "t", Title: "Foo"}}, nil
		}
		t.Fatalf("unexpected: %s", id)
		return nil, nil
	}
	groups, err := DetectCase2(post, tracks, 3)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(groups) != 1 || groups[0].Parent.ID != "L1" {
		t.Errorf("expected one group on artist 1; got %+v", groups)
	}
}

func TestDetectCase2_noMatchingTrack_noGroup(t *testing.T) {
	post := []gateway.AlbumMetadata{
		{ID: "S", Title: "Foo", ArtistID: "1", TrackCount: 1},
		{ID: "L", Title: "Bar", ArtistID: "1", TrackCount: 12},
	}
	tracks := func(id string) ([]gateway.AlbumTrack, error) {
		return []gateway.AlbumTrack{{ID: "t", Title: "Other"}}, nil
	}
	groups, err := DetectCase2(post, tracks, 3)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(groups) != 0 {
		t.Errorf("groups = %+v, want empty", groups)
	}
}

func TestDetectCase2_artistWithoutLong_noFetch(t *testing.T) {
	// Artist has only short albums → no phase-2 fetch should happen.
	post := []gateway.AlbumMetadata{
		{ID: "S1", Title: "Foo", ArtistID: "1", TrackCount: 1},
		{ID: "S2", Title: "Foo", ArtistID: "1", TrackCount: 2},
	}
	tracks := func(id string) ([]gateway.AlbumTrack, error) {
		t.Fatalf("unexpected ListTracks for %s", id)
		return nil, nil
	}
	groups, err := DetectCase2(post, tracks, 3)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(groups) != 0 {
		t.Errorf("groups = %+v, want empty", groups)
	}
}

func TestDetectCase2_multipleShortsCollapseOntoOneParent(t *testing.T) {
	post := []gateway.AlbumMetadata{
		{ID: "S1", Title: "Foo", ArtistID: "1", TrackCount: 1},
		{ID: "S2", Title: "Bar", ArtistID: "1", TrackCount: 1},
		{ID: "L", Title: "LP", ArtistID: "1", TrackCount: 12},
	}
	tracks := func(id string) ([]gateway.AlbumTrack, error) {
		return []gateway.AlbumTrack{{Title: "Foo"}, {Title: "Bar"}}, nil
	}
	groups, err := DetectCase2(post, tracks, 3)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("len = %d", len(groups))
	}
	got := ids(groups[0].Shorts)
	if len(got) != 2 || got[0] != "S1" || got[1] != "S2" {
		t.Errorf("shorts = %v", got)
	}
}

func TestDetectCase2_multipleParents_lexSmallestNormalisedWins(t *testing.T) {
	post := []gateway.AlbumMetadata{
		{ID: "S", Title: "Foo", ArtistID: "1", TrackCount: 1},
		{ID: "Lz", Title: "Zeta", ArtistID: "1", TrackCount: 12},
		{ID: "La", Title: "Alpha", ArtistID: "1", TrackCount: 12},
	}
	tracks := func(id string) ([]gateway.AlbumTrack, error) {
		// Both Alpha and Zeta contain a "Foo" track.
		return []gateway.AlbumTrack{{Title: "Foo"}}, nil
	}
	groups, err := DetectCase2(post, tracks, 3)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(groups) != 1 || groups[0].Parent.ID != "La" {
		t.Errorf("parent = %+v, want La (lex-smallest normalised title)", groups[0].Parent)
	}
}

func TestDetectCase2_tracklistError_dropsParentFromPool(t *testing.T) {
	// One of two parents fails ListTracks. Detection continues with the other.
	post := []gateway.AlbumMetadata{
		{ID: "S", Title: "Foo", ArtistID: "1", TrackCount: 1},
		{ID: "Lbad", Title: "Alpha", ArtistID: "1", TrackCount: 12},
		{ID: "Lgood", Title: "Beta", ArtistID: "1", TrackCount: 12},
	}
	tracks := func(id string) ([]gateway.AlbumTrack, error) {
		if id == "Lbad" {
			return nil, errFakeNotFound
		}
		return []gateway.AlbumTrack{{Title: "Foo"}}, nil
	}
	groups, err := DetectCase2(post, tracks, 3)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(groups) != 1 || groups[0].Parent.ID != "Lgood" {
		t.Errorf("parent = %+v, want Lgood (Lbad dropped due to fetch error)", groups[0].Parent)
	}
}

// errFakeNotFound is a sentinel used by tests to simulate ListTracks
// returning a classified error that should drop the album from the pool.
var errFakeNotFound = &gateway.GatewayError{Kind: gateway.ErrNotFound, RawMessage: "fake"}
```

The reference to `gateway.GatewayError` requires verifying the existing struct field layout. If `RawMessage` isn't the field name in `internal/gateway/errors.go`, substitute the actual field name (or use a no-arg constructor if one exists).

- [ ] **Step 2: Run, expect FAIL**

```bash
go test ./internal/lovedalbums -run TestDetectCase2
```

Expected: compile error (`undefined: DetectCase2`, `undefined: Case2Group`).

- [ ] **Step 3: Implement**

Append to `internal/lovedalbums/match.go`:

```go
// Case2Group is one short album (or several) by the same artist whose title
// equals a track on a longer same-artist album that is also loved. Parent
// stays loved; Shorts are losers to be un-loved.
type Case2Group struct {
	ArtistID   string
	ArtistName string
	Parent     gateway.AlbumMetadata
	Shorts     []gateway.AlbumMetadata
}

// DetectCase2 returns Case-2 groups detected in the post-Case-1 loved set.
//
// "Post-Case-1" means the caller has already removed Case-1 losers from the
// input slice. DetectCase2 does not re-run Case-1.
//
// `fetchTracks` is called once per long album of every phase-2-eligible
// artist (artists with both at least one short and at least one long album
// in `post`). On error, the album is dropped from the matching pool but
// detection continues with the remaining albums.
//
// Threshold semantics: TrackCount ≤ threshold is "short"; > threshold is
// "long".
func DetectCase2(
	post []gateway.AlbumMetadata,
	fetchTracks func(albumID string) ([]gateway.AlbumTrack, error),
	threshold int,
) ([]Case2Group, error) {
	if threshold <= 0 {
		threshold = 3
	}

	// Group by artist.
	byArtist := make(map[string][]gateway.AlbumMetadata)
	for _, a := range post {
		byArtist[a.ArtistID] = append(byArtist[a.ArtistID], a)
	}

	var groups []Case2Group
	// Iterate in deterministic ArtistID order.
	artistIDs := make([]string, 0, len(byArtist))
	for k := range byArtist {
		artistIDs = append(artistIDs, k)
	}
	sort.Strings(artistIDs)

	for _, aid := range artistIDs {
		albums := byArtist[aid]
		var shorts, longs []gateway.AlbumMetadata
		for _, a := range albums {
			if a.TrackCount <= threshold {
				shorts = append(shorts, a)
			} else {
				longs = append(longs, a)
			}
		}
		if len(shorts) == 0 || len(longs) == 0 {
			continue
		}

		// Phase-2 fetch: tracklists for every long album of this artist.
		// On error, drop the album from the pool.
		type longWithTracks struct {
			meta   gateway.AlbumMetadata
			titles map[string]bool // normalised track titles
		}
		pool := make([]longWithTracks, 0, len(longs))
		for _, l := range longs {
			tracks, err := fetchTracks(l.ID)
			if err != nil {
				continue
			}
			titles := make(map[string]bool, len(tracks))
			for _, t := range tracks {
				titles[Normalise(t.Title)] = true
			}
			pool = append(pool, longWithTracks{meta: l, titles: titles})
		}
		if len(pool) == 0 {
			continue
		}

		// Match each short to the lex-smallest-normalised-title parent
		// whose tracklist contains it.
		parentIdx := make(map[string]int) // parent ID → index into groups
		artistName := albums[0].ArtistName
		for _, s := range shorts {
			n := Normalise(s.Title)
			var picked *longWithTracks
			pickedNormTitle := ""
			for i := range pool {
				if !pool[i].titles[n] {
					continue
				}
				normTitle := Normalise(pool[i].meta.Title)
				if picked == nil || normTitle < pickedNormTitle {
					picked = &pool[i]
					pickedNormTitle = normTitle
				}
			}
			if picked == nil {
				continue
			}
			if idx, ok := parentIdx[picked.meta.ID]; ok {
				groups[idx].Shorts = append(groups[idx].Shorts, s)
			} else {
				parentIdx[picked.meta.ID] = len(groups)
				groups = append(groups, Case2Group{
					ArtistID: aid, ArtistName: artistName,
					Parent: picked.meta, Shorts: []gateway.AlbumMetadata{s},
				})
			}
		}
	}

	// Determinism: sort each group's shorts by ALB_ID.
	for i := range groups {
		sort.Slice(groups[i].Shorts, func(a, b int) bool {
			return idLess(groups[i].Shorts[a].ID, groups[i].Shorts[b].ID)
		})
	}
	return groups, nil
}
```

- [ ] **Step 4: Run, expect PASS**

```bash
go test ./internal/lovedalbums -run TestDetectCase2
```

- [ ] **Step 5: Run the full lovedalbums suite**

```bash
go test ./internal/lovedalbums
```

- [ ] **Step 6: Commit**

```bash
git add internal/lovedalbums/match.go internal/lovedalbums/match_test.go
git commit -m "feat(lovedalbums): DetectCase2"
```

---

## Task 10: `BuildPlan` and `DedupePlan` data type

**Files:**
- Modify: `internal/lovedalbums/plan.go`
- Modify: `internal/lovedalbums/plan_test.go`

Combine Case-1 and Case-2 groups into a single plan with a flat `AlbumsToUnlove` list. Records per-loser metadata (case kind, reason, parent if Case 2).

- [ ] **Step 1: Write the failing tests**

Append to `internal/lovedalbums/plan_test.go`:

```go
func TestBuildPlan_combinesCase1AndCase2_disjoint(t *testing.T) {
	c1 := []Case1Group{
		{
			ArtistID: "1", ArtistName: "A", NormalisedKey: "x",
			Members: []gateway.AlbumMetadata{
				{ID: "winner1", Title: "X"}, {ID: "loser1", Title: "X"},
			},
		},
	}
	c2 := []Case2Group{
		{
			ArtistID: "1", ArtistName: "A",
			Parent: gateway.AlbumMetadata{ID: "parent2", Title: "LP"},
			Shorts: []gateway.AlbumMetadata{{ID: "short2", Title: "Foo"}},
		},
	}
	plan := BuildPlan(c1, c2)
	if len(plan.AlbumsToUnlove) != 2 {
		t.Fatalf("AlbumsToUnlove = %d, want 2", len(plan.AlbumsToUnlove))
	}
	got := plan.AlbumsToUnlove
	// Order must be deterministic: case1 losers first (by group order),
	// then case2 shorts.
	if got[0].Album.ID != "loser1" || got[0].Case != Case1 {
		t.Errorf("got[0] = %+v", got[0])
	}
	if got[1].Album.ID != "short2" || got[1].Case != Case2 {
		t.Errorf("got[1] = %+v", got[1])
	}
	if got[1].Parent == nil || got[1].Parent.ID != "parent2" {
		t.Errorf("got[1].Parent = %+v, want parent2", got[1].Parent)
	}
}

func TestBuildPlan_winnersAndParentsNotUnloved(t *testing.T) {
	c1 := []Case1Group{
		{
			Members: []gateway.AlbumMetadata{
				{ID: "winner"}, {ID: "loser"},
			},
		},
	}
	c2 := []Case2Group{
		{
			Parent: gateway.AlbumMetadata{ID: "parent"},
			Shorts: []gateway.AlbumMetadata{{ID: "short"}},
		},
	}
	plan := BuildPlan(c1, c2)
	for _, e := range plan.AlbumsToUnlove {
		if e.Album.ID == "winner" || e.Album.ID == "parent" {
			t.Errorf("unexpected unlove: %s", e.Album.ID)
		}
	}
}

func TestBuildPlan_dedupesByALBID(t *testing.T) {
	// Same ALB_ID listed in two groups → appears once.
	c1 := []Case1Group{
		{Members: []gateway.AlbumMetadata{{ID: "w1"}, {ID: "dup"}}},
		{Members: []gateway.AlbumMetadata{{ID: "w2"}, {ID: "dup"}}},
	}
	plan := BuildPlan(c1, nil)
	count := 0
	for _, e := range plan.AlbumsToUnlove {
		if e.Album.ID == "dup" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("dup count = %d, want 1", count)
	}
}

func TestBuildPlan_emptyInputs_emptyPlan(t *testing.T) {
	plan := BuildPlan(nil, nil)
	if len(plan.AlbumsToUnlove) != 0 || plan.Case1Groups != 0 || plan.Case2Groups != 0 {
		t.Errorf("plan = %+v", plan)
	}
}
```

- [ ] **Step 2: Run, expect FAIL**

```bash
go test ./internal/lovedalbums -run TestBuildPlan
```

Expected: compile error (`undefined: BuildPlan`, `undefined: DedupePlan`, etc.).

- [ ] **Step 3: Implement**

Append to `internal/lovedalbums/plan.go`:

```go
// CaseKind identifies which detection rule produced an unlove entry.
type CaseKind int

const (
	Case1 CaseKind = iota + 1
	Case2
)

func (c CaseKind) String() string {
	switch c {
	case Case1:
		return "case1"
	case Case2:
		return "case2"
	}
	return "unknown"
}

// UnloveEntry is one album scheduled to be un-loved, plus the rationale.
type UnloveEntry struct {
	Album  gateway.AlbumMetadata
	Case   CaseKind
	Reason string
	// Parent is non-nil only for Case 2: the longer same-artist album
	// whose tracklist contains a track named like Album.Title.
	Parent *gateway.AlbumMetadata
}

// DedupePlan is the input to the apply phase. Case1Groups and Case2Groups
// are kept around as-is for run-record reporting; AlbumsToUnlove is the
// flattened, ALB_ID-deduped list the apply loop iterates over.
type DedupePlan struct {
	Case1Groups    []Case1Group
	Case2Groups    []Case2Group
	AlbumsToUnlove []UnloveEntry
}

// BuildPlan flattens Case-1 losers + Case-2 shorts into a single
// AlbumsToUnlove slice, deduped by ALB_ID. Order is deterministic:
// Case-1 entries first (in group order), then Case-2 entries.
//
// Caller invariant: c2 was computed on the post-Case-1 set, so an album
// cannot be both a Case-1 loser and a Case-2 short (see the design spec).
// The dedup-by-ALB_ID step here is a defence-in-depth, not a workaround.
func BuildPlan(c1 []Case1Group, c2 []Case2Group) DedupePlan {
	plan := DedupePlan{Case1Groups: c1, Case2Groups: c2}
	seen := make(map[string]bool)
	add := func(e UnloveEntry) {
		if seen[e.Album.ID] {
			return
		}
		seen[e.Album.ID] = true
		plan.AlbumsToUnlove = append(plan.AlbumsToUnlove, e)
	}
	for _, g := range c1 {
		for _, m := range g.Members[1:] {
			add(UnloveEntry{
				Album:  m,
				Case:   Case1,
				Reason: "same normalised title as " + g.Members[0].Title,
			})
		}
	}
	for _, g := range c2 {
		parent := g.Parent
		for _, s := range g.Shorts {
			add(UnloveEntry{
				Album:  s,
				Case:   Case2,
				Reason: "single matches a track on " + parent.Title,
				Parent: &parent,
			})
		}
	}
	return plan
}
```

- [ ] **Step 4: Run, expect PASS**

```bash
go test ./internal/lovedalbums -run TestBuildPlan
```

- [ ] **Step 5: Commit**

```bash
git add internal/lovedalbums/plan.go internal/lovedalbums/plan_test.go
git commit -m "feat(lovedalbums): BuildPlan"
```

---

## Task 11: `Phase1Fetch` and `Phase2Fetch`

**Files:**
- Create: `internal/lovedalbums/fetch.go`
- Create: `internal/lovedalbums/fetch_test.go`

Wraps the gateway in a paced, classified-error-aware loop. Two fetchers — phase 1 over IDs, phase 2 over a callback.

- [ ] **Step 1: Write the failing tests**

Create `internal/lovedalbums/fetch_test.go`:

```go
package lovedalbums

import (
	"context"
	"errors"
	"testing"

	"github.com/niref/deezer-tools/internal/gateway"
	"github.com/niref/deezer-tools/internal/throttle"
)

func init() {
	// Test binary: zero the pacer so tests don't sleep.
	throttle.Pace = 0
	throttle.Jitter = 0
}

type fakeGW struct {
	metaByID    map[string]gateway.AlbumMetadata
	metaErrByID map[string]error
	tracksByID  map[string][]gateway.AlbumTrack
	tracksErr   map[string]error
	metaCalls   int
	tracksCalls int
}

func (f *fakeGW) GetAlbumMetadata(ctx context.Context, id string) (gateway.AlbumMetadata, error) {
	f.metaCalls++
	if err, ok := f.metaErrByID[id]; ok {
		return gateway.AlbumMetadata{}, err
	}
	return f.metaByID[id], nil
}
func (f *fakeGW) ListAlbumTracks(ctx context.Context, id string) ([]gateway.AlbumTrack, error) {
	f.tracksCalls++
	if err, ok := f.tracksErr[id]; ok {
		return nil, err
	}
	return f.tracksByID[id], nil
}

func TestPhase1Fetch_happyPath(t *testing.T) {
	gw := &fakeGW{
		metaByID: map[string]gateway.AlbumMetadata{
			"1": {ID: "1", Title: "A", ArtistID: "x", TrackCount: 1},
			"2": {ID: "2", Title: "B", ArtistID: "x", TrackCount: 2},
		},
	}
	got, err := Phase1Fetch(context.Background(), gw, []string{"1", "2"}, nil, nil)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(got) != 2 || gw.metaCalls != 2 {
		t.Errorf("got=%+v calls=%d", got, gw.metaCalls)
	}
}

func TestPhase1Fetch_dropsNotFound(t *testing.T) {
	gw := &fakeGW{
		metaByID: map[string]gateway.AlbumMetadata{
			"1": {ID: "1"},
		},
		metaErrByID: map[string]error{
			"missing": &gateway.GatewayError{Kind: gateway.ErrNotFound, RawMessage: "x"},
		},
	}
	got, err := Phase1Fetch(context.Background(), gw, []string{"1", "missing"}, nil, nil)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(got) != 1 || got[0].ID != "1" {
		t.Errorf("got = %+v", got)
	}
}

func TestPhase1Fetch_authFailureAborts(t *testing.T) {
	gw := &fakeGW{
		metaErrByID: map[string]error{
			"1": &gateway.GatewayError{Kind: gateway.ErrAuthFailed, RawMessage: "x"},
		},
	}
	_, err := Phase1Fetch(context.Background(), gw, []string{"1", "2"}, nil, nil)
	if err == nil {
		t.Fatal("err = nil, want auth")
	}
	var ge *gateway.GatewayError
	if !errors.As(err, &ge) || ge.Kind != gateway.ErrAuthFailed {
		t.Errorf("err = %v, want ErrAuthFailed", err)
	}
}

func TestPhase2Fetch_onlyEligibleArtists(t *testing.T) {
	post := []gateway.AlbumMetadata{
		// artist 1: short + long → eligible
		{ID: "1s", ArtistID: "1", TrackCount: 1},
		{ID: "1l", ArtistID: "1", TrackCount: 12},
		// artist 2: only short → not eligible
		{ID: "2s", ArtistID: "2", TrackCount: 1},
		// artist 3: only long → not eligible
		{ID: "3l", ArtistID: "3", TrackCount: 12},
	}
	gw := &fakeGW{
		tracksByID: map[string][]gateway.AlbumTrack{
			"1l": {{Title: "Foo"}},
		},
	}
	tracks, attempts, err := Phase2Fetch(context.Background(), gw, post, 3, nil, nil)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if gw.tracksCalls != 1 || attempts != 1 {
		t.Errorf("tracksCalls = %d, attempts = %d, want 1 / 1 (only 1l)", gw.tracksCalls, attempts)
	}
	if _, ok := tracks("1l"); !ok {
		t.Errorf("expected tracks for 1l")
	}
	if _, ok := tracks("3l"); ok {
		t.Errorf("did not expect tracks for 3l")
	}
}
```

- [ ] **Step 2: Run, expect FAIL**

```bash
go test ./internal/lovedalbums -run TestPhase
```

Expected: compile errors.

- [ ] **Step 3: Implement**

Create `internal/lovedalbums/fetch.go`:

```go
package lovedalbums

import (
	"context"
	"errors"
	"time"

	"github.com/niref/deezer-tools/internal/gateway"
	"github.com/niref/deezer-tools/internal/throttle"
)

// metadataFetcher is the slice of the gateway used by Phase1Fetch.
type metadataFetcher interface {
	GetAlbumMetadata(ctx context.Context, albumID string) (gateway.AlbumMetadata, error)
}

// tracksFetcher is the slice of the gateway used by Phase2Fetch.
type tracksFetcher interface {
	ListAlbumTracks(ctx context.Context, albumID string) ([]gateway.AlbumTrack, error)
}

// Phase1Fetch calls GetAlbumMetadata once per loved-album ID. Each call is
// preceded by throttle.Sleep and wrapped in throttle.RunOne with the gateway
// retryable predicate.
//
// Behaviour on classified errors:
//   - ErrAuthFailed → abort, return the error verbatim (caller surfaces the
//     standard arl-refresh message).
//   - ErrNotFound → drop the album from the candidate set; continue.
//   - Any other classified non-retryable error → drop with a debug log via
//     `notify` (if non-nil); continue.
//
// `retry` is the per-call retry schedule; nil → throttle.DefaultRetryBackoff,
// empty → first attempt only.
//
// `notify` is called once per dropped album so the orchestrator can log it
// to the run record. It may be nil.
func Phase1Fetch(
	ctx context.Context,
	gw metadataFetcher,
	ids []string,
	retry []time.Duration,
	notify func(albumID string, err error),
) ([]gateway.AlbumMetadata, error) {
	out := make([]gateway.AlbumMetadata, 0, len(ids))
	for _, id := range ids {
		select {
		case <-ctx.Done():
			return out, ctx.Err()
		default:
		}
		if err := throttle.Sleep(ctx); err != nil {
			return out, err
		}
		var meta gateway.AlbumMetadata
		callErr := throttle.RunOne(ctx, func(ctx context.Context) error {
			var err error
			meta, err = gw.GetAlbumMetadata(ctx, id)
			return err
		}, gateway.IsRetryable, retry)
		if callErr == nil {
			out = append(out, meta)
			continue
		}
		if errors.Is(callErr, context.Canceled) || errors.Is(callErr, context.DeadlineExceeded) {
			return out, callErr
		}
		var ge *gateway.GatewayError
		if errors.As(callErr, &ge) && ge.Kind == gateway.ErrAuthFailed {
			return out, callErr
		}
		if notify != nil {
			notify(id, callErr)
		}
	}
	return out, nil
}

// TracksLookup is the callback returned by Phase2Fetch. It returns the
// tracks fetched for the given album, or false if no tracks were fetched
// (album wasn't eligible, or the fetch failed).
type TracksLookup func(albumID string) ([]gateway.AlbumTrack, bool)

// Phase2Fetch calls ListAlbumTracks once per long album in every
// phase-2-eligible artist's loved set. An artist is eligible iff `post`
// contains both at least one short album (TrackCount ≤ threshold) and at
// least one long album (TrackCount > threshold) for that artist.
//
// Same throttle / retry / classification semantics as Phase1Fetch.
// Failed fetches are logged via `notify` (if non-nil) and dropped from the
// returned lookup; detection in DetectCase2 will simply not match against
// dropped albums.
func Phase2Fetch(
	ctx context.Context,
	gw tracksFetcher,
	post []gateway.AlbumMetadata,
	threshold int,
	retry []time.Duration,
	notify func(albumID string, err error),
) (TracksLookup, int, error) {
	if threshold <= 0 {
		threshold = 3
	}

	type bucket struct{ shorts, longs []gateway.AlbumMetadata }
	byArtist := make(map[string]*bucket)
	for _, a := range post {
		b, ok := byArtist[a.ArtistID]
		if !ok {
			b = &bucket{}
			byArtist[a.ArtistID] = b
		}
		if a.TrackCount <= threshold {
			b.shorts = append(b.shorts, a)
		} else {
			b.longs = append(b.longs, a)
		}
	}

	tracksByID := make(map[string][]gateway.AlbumTrack)
	attempts := 0
	for _, b := range byArtist {
		if len(b.shorts) == 0 || len(b.longs) == 0 {
			continue
		}
		for _, l := range b.longs {
			select {
			case <-ctx.Done():
				return nil, attempts, ctx.Err()
			default:
			}
			if err := throttle.Sleep(ctx); err != nil {
				return nil, attempts, err
			}
			id := l.ID
			attempts++
			var tracks []gateway.AlbumTrack
			callErr := throttle.RunOne(ctx, func(ctx context.Context) error {
				var err error
				tracks, err = gw.ListAlbumTracks(ctx, id)
				return err
			}, gateway.IsRetryable, retry)
			if callErr == nil {
				tracksByID[id] = tracks
				continue
			}
			if errors.Is(callErr, context.Canceled) || errors.Is(callErr, context.DeadlineExceeded) {
				return nil, attempts, callErr
			}
			var ge *gateway.GatewayError
			if errors.As(callErr, &ge) && ge.Kind == gateway.ErrAuthFailed {
				return nil, attempts, callErr
			}
			if notify != nil {
				notify(id, callErr)
			}
		}
	}

	return func(id string) ([]gateway.AlbumTrack, bool) {
		t, ok := tracksByID[id]
		return t, ok
	}, attempts, nil
}
```

- [ ] **Step 4: Run, expect PASS**

```bash
go test ./internal/lovedalbums -run TestPhase
```

- [ ] **Step 5: Run the full lovedalbums suite**

```bash
go test ./internal/lovedalbums
```

- [ ] **Step 6: Commit**

```bash
git add internal/lovedalbums/fetch.go internal/lovedalbums/fetch_test.go
git commit -m "feat(lovedalbums): paced phase-1 + phase-2 fetchers"
```

---

## Task 12: `Run` orchestrator — list, plan, run-record, dry-run, empty short-circuit

**Files:**
- Create: `internal/lovedalbums/dedupe.go`
- Create: `internal/lovedalbums/dedupe_test.go`

This task covers everything up to the confirmation gate. The apply phase comes in Task 13.

- [ ] **Step 1: Write the failing tests for the read-only path**

Create `internal/lovedalbums/dedupe_test.go`:

```go
package lovedalbums

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/niref/deezer-tools/internal/gateway"
)

type fullFakeGW struct {
	*fakeGW
	listIDs       []string
	listErr       error
	removed       []string
	removeErr     map[string]error
	removeCalls   int
}

func (f *fullFakeGW) ListFavoriteAlbumIDs(ctx context.Context) ([]string, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.listIDs, nil
}
func (f *fullFakeGW) RemoveFavoriteAlbum(ctx context.Context, id string) error {
	f.removeCalls++
	if err, ok := f.removeErr[id]; ok {
		return err
	}
	f.removed = append(f.removed, id)
	return nil
}

func newGW(meta map[string]gateway.AlbumMetadata, tracks map[string][]gateway.AlbumTrack, ids []string) *fullFakeGW {
	return &fullFakeGW{
		fakeGW:  &fakeGW{metaByID: meta, tracksByID: tracks},
		listIDs: ids,
	}
}

func TestRun_emptyLovedSet(t *testing.T) {
	tmp := t.TempDir()
	gw := newGW(nil, nil, nil)
	res, err := Run(context.Background(), gw, Options{
		BackupDir: tmp,
		Stdout:    &bytes.Buffer{},
		Stderr:    &bytes.Buffer{},
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if res.AlbumsToUnlove != 0 || res.AlbumsUnloved != 0 {
		t.Errorf("res = %+v", res)
	}
	if gw.metaCalls != 0 || gw.tracksCalls != 0 || gw.removeCalls != 0 {
		t.Errorf("calls: meta=%d tracks=%d remove=%d (want all 0)",
			gw.metaCalls, gw.tracksCalls, gw.removeCalls)
	}
}

func TestRun_dryRun_writesRecord_doesNotUnlove(t *testing.T) {
	tmp := t.TempDir()
	meta := map[string]gateway.AlbumMetadata{
		"1": {ID: "1", Title: "X", ArtistID: "a", TrackCount: 13, FanCount: 1000},
		"2": {ID: "2", Title: "x", ArtistID: "a", TrackCount: 13, FanCount: 5},
	}
	gw := newGW(meta, nil, []string{"1", "2"})
	out := &bytes.Buffer{}
	res, err := Run(context.Background(), gw, Options{
		DryRun: true, BackupDir: tmp, Stdout: out, Stderr: &bytes.Buffer{},
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if gw.removeCalls != 0 {
		t.Errorf("removeCalls = %d, want 0 (dry-run)", gw.removeCalls)
	}
	if res.AlbumsToUnlove != 1 || res.AlbumsUnloved != 0 {
		t.Errorf("res = %+v", res)
	}
	if !strings.Contains(out.String(), "would unlove") {
		t.Errorf("stdout = %q", out.String())
	}
	// Run record exists, valid JSON, contains the case-1 group.
	matches, _ := filepath.Glob(filepath.Join(tmp, "deezer-loved-albums-dedupe-*.json"))
	if len(matches) != 1 {
		t.Fatalf("matches = %v", matches)
	}
	b, _ := os.ReadFile(matches[0])
	var rec map[string]any
	if err := json.Unmarshal(b, &rec); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if rec["version"].(float64) != 1 {
		t.Errorf("version = %v", rec["version"])
	}
}

func TestRun_authFailureOnList_aborts(t *testing.T) {
	gw := newGW(nil, nil, nil)
	gw.listErr = &gateway.GatewayError{Kind: gateway.ErrAuthFailed, RawMessage: "x"}
	_, err := Run(context.Background(), gw, Options{
		BackupDir: t.TempDir(), Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{},
	})
	if err == nil {
		t.Fatal("err = nil, want auth")
	}
	if !strings.Contains(err.Error(), "config.toml") {
		t.Errorf("err = %v, want refresh-arl message", err)
	}
}
```

- [ ] **Step 2: Run, expect FAIL**

```bash
go test ./internal/lovedalbums -run TestRun
```

Expected: compile error (`undefined: Run`, `undefined: Options`, `undefined: Result`).

- [ ] **Step 3: Implement the orchestrator skeleton**

Create `internal/lovedalbums/dedupe.go`:

```go
package lovedalbums

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/niref/deezer-tools/internal/gateway"
	"github.com/niref/deezer-tools/internal/throttle"
)

// ErrAborted is returned when the user declines the confirmation prompt.
var ErrAborted = errors.New("lovedalbums: aborted by user")

// Gateway is the slice of internal/gateway.Client used by Run. Defined here
// (not in internal/gateway) to keep the dependency narrow and let tests fake
// the transport without spinning up an HTTP server.
type Gateway interface {
	ListFavoriteAlbumIDs(ctx context.Context) ([]string, error)
	GetAlbumMetadata(ctx context.Context, albumID string) (gateway.AlbumMetadata, error)
	ListAlbumTracks(ctx context.Context, albumID string) ([]gateway.AlbumTrack, error)
	RemoveFavoriteAlbum(ctx context.Context, albumID string) error
}

// Options configures one Run.
//
// Sentinels match the lovedtracks / playlistlove patterns:
//   - RetryBackoff: nil → throttle.DefaultRetryBackoff; empty → no retries.
//   - MaxConsecutiveFinalFailures: 0 → throttle default; negative → disable.
//   - Case2TrackThreshold: 0 → 3.
type Options struct {
	DryRun                      bool
	BackupDir                   string
	Stdin                       io.Reader
	Stdout                      io.Writer
	Stderr                      io.Writer
	Case2TrackThreshold         int
	RetryBackoff                []time.Duration
	MaxConsecutiveFinalFailures int
	OpenTTY                     func() (io.ReadCloser, error)
}

// Result summarizes a completed Run.
type Result struct {
	StartedAt      time.Time
	RunRecordPath  string
	SkipLogPath    string
	Case1Groups    int
	Case2Groups    int
	AlbumsToUnlove int
	AlbumsUnloved  int
	AlbumsSkipped  int
	Phase1Calls    int
	Phase2Calls    int
	Elapsed        time.Duration
}

// runRecord is the JSON payload written to <BackupDir>/deezer-loved-albums-dedupe-<UTC>.json.
type runRecord struct {
	Version     int             `json:"version"`
	StartedAt   string          `json:"started_at"`
	Stats       runRecordStats  `json:"stats"`
	Case1Groups []recordCase1   `json:"case1_groups"`
	Case2Groups []recordCase2   `json:"case2_groups"`
	Unloves     []recordUnlove  `json:"albums_to_unlove"`
}

type runRecordStats struct {
	LovedAlbums    int `json:"loved_albums"`
	Phase1Calls    int `json:"phase1_calls"`
	Phase2Calls    int `json:"phase2_calls"`
	Case1Groups    int `json:"case1_groups"`
	Case2Groups    int `json:"case2_groups"`
	AlbumsToUnlove int `json:"albums_to_unlove"`
}

type recordAlbumLite struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	FanCount   int    `json:"fan_count,omitempty"`
	TrackCount int    `json:"track_count,omitempty"`
}

type recordCase1 struct {
	ArtistID      string            `json:"artist_id"`
	ArtistName    string            `json:"artist_name"`
	NormalisedKey string            `json:"normalised_key"`
	Winner        recordAlbumLite   `json:"winner"`
	Losers        []recordAlbumLite `json:"losers"`
}

type recordCase2 struct {
	ArtistID   string            `json:"artist_id"`
	ArtistName string            `json:"artist_name"`
	Parent     recordAlbumLite   `json:"parent"`
	Shorts     []recordAlbumLite `json:"shorts"`
}

type recordUnlove struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Artist string `json:"artist"`
	Case   string `json:"case"`
	Reason string `json:"reason"`
}

// Run executes the full dedupe flow against gw.
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
	if opts.Case2TrackThreshold <= 0 {
		opts.Case2TrackThreshold = 3
	}

	res := &Result{StartedAt: time.Now().UTC()}

	// 1. List loved-album IDs.
	ids, err := gw.ListFavoriteAlbumIDs(ctx)
	if err != nil {
		var ge *gateway.GatewayError
		if errors.As(err, &ge) && ge.Kind == gateway.ErrAuthFailed {
			return nil, fmt.Errorf("auth failed listing loved albums (refresh your arl in ~/.config/deezer-tools/config.toml): %w", err)
		}
		return nil, fmt.Errorf("list loved albums: %w", err)
	}

	// 2. Phase 1.
	notify1 := func(id string, e error) {
		fmt.Fprintf(opts.Stderr, "phase1 dropped %s: %v\n", id, e)
	}
	loved, err := Phase1Fetch(ctx, gw, ids, opts.RetryBackoff, notify1)
	res.Phase1Calls = len(ids)
	if err != nil {
		return res, classifyAuth(err, "phase1 metadata fetch")
	}

	// 3. Detect Case 1.
	c1 := DetectCase1(loved)
	res.Case1Groups = len(c1)

	// 4. Build post-Case-1 set: drop Case-1 losers from `loved`.
	loserIDs := make(map[string]bool)
	for _, g := range c1 {
		for _, m := range g.Members[1:] {
			loserIDs[m.ID] = true
		}
	}
	post := loved[:0]
	for _, a := range loved {
		if !loserIDs[a.ID] {
			post = append(post, a)
		}
	}
	// reslice to a fresh backing array to avoid aliasing the truncated tail
	post = append([]gateway.AlbumMetadata(nil), post...)

	// 5. Phase 2.
	notify2 := func(id string, e error) {
		fmt.Fprintf(opts.Stderr, "phase2 dropped %s: %v\n", id, e)
	}
	tracksLookup, phase2Attempts, err := Phase2Fetch(ctx, gw, post, opts.Case2TrackThreshold, opts.RetryBackoff, notify2)
	res.Phase2Calls = phase2Attempts
	if err != nil {
		return res, classifyAuth(err, "phase2 tracklist fetch")
	}

	// 6. Detect Case 2.
	c2, err := DetectCase2(post, func(id string) ([]gateway.AlbumTrack, error) {
		t, ok := tracksLookup(id)
		if !ok {
			return nil, errSkippedTracks
		}
		return t, nil
	}, opts.Case2TrackThreshold)
	if err != nil {
		return res, fmt.Errorf("detect case 2: %w", err)
	}
	res.Case2Groups = len(c2)

	// 7. Build plan.
	plan := BuildPlan(c1, c2)
	res.AlbumsToUnlove = len(plan.AlbumsToUnlove)

	// 8. Write run record.
	rec := buildRunRecord(res, len(ids), plan)
	recPath, err := writeRunRecord(opts.BackupDir, res.StartedAt, rec)
	if err != nil {
		return res, fmt.Errorf("write run record: %w", err)
	}
	res.RunRecordPath = recPath
	fmt.Fprintf(opts.Stderr, "Run record written to %s\n", recPath)

	// 9. Empty-plan short-circuit.
	if len(plan.AlbumsToUnlove) == 0 {
		fmt.Fprintln(opts.Stdout, "Nothing to dedupe; loved-albums list is clean.")
		res.Elapsed = time.Since(res.StartedAt)
		return res, nil
	}

	// 10. Dry-run short-circuit.
	if opts.DryRun {
		fmt.Fprintf(opts.Stdout, "would unlove %d albums (%d case-1, %d case-2), run-record at %s\n",
			res.AlbumsToUnlove, res.Case1Groups, res.Case2Groups, recPath)
		res.Elapsed = time.Since(res.StartedAt)
		return res, nil
	}

	// 11–14: confirmation + apply phase — implemented in Task 13.
	return res, errApplyNotImplemented
}

// errSkippedTracks is the sentinel that bridges Phase2Fetch's "no entry"
// signal into DetectCase2's fetchTracks error contract. DetectCase2 treats
// any error as "drop this parent from the pool", which is the desired
// behaviour for skipped fetches.
var errSkippedTracks = errors.New("phase2: tracks unavailable")

// errApplyNotImplemented is a placeholder used while the apply phase is
// being implemented in Task 13. Removed in Task 13.
var errApplyNotImplemented = errors.New("apply phase not yet implemented")

func classifyAuth(err error, prefix string) error {
	var ge *gateway.GatewayError
	if errors.As(err, &ge) && ge.Kind == gateway.ErrAuthFailed {
		return fmt.Errorf("auth failed during %s (refresh your arl in ~/.config/deezer-tools/config.toml): %w", prefix, err)
	}
	return fmt.Errorf("%s: %w", prefix, err)
}

func buildRunRecord(res *Result, lovedCount int, plan DedupePlan) runRecord {
	rec := runRecord{
		Version:   1,
		StartedAt: res.StartedAt.Format(time.RFC3339),
		Stats: runRecordStats{
			LovedAlbums:    lovedCount,
			Phase1Calls:    res.Phase1Calls,
			Phase2Calls:    res.Phase2Calls,
			Case1Groups:    res.Case1Groups,
			Case2Groups:    res.Case2Groups,
			AlbumsToUnlove: res.AlbumsToUnlove,
		},
	}
	for _, g := range plan.Case1Groups {
		var entry recordCase1
		entry.ArtistID = g.ArtistID
		entry.ArtistName = g.ArtistName
		entry.NormalisedKey = g.NormalisedKey
		entry.Winner = liteOf(g.Members[0])
		for _, m := range g.Members[1:] {
			entry.Losers = append(entry.Losers, liteOf(m))
		}
		rec.Case1Groups = append(rec.Case1Groups, entry)
	}
	for _, g := range plan.Case2Groups {
		var entry recordCase2
		entry.ArtistID = g.ArtistID
		entry.ArtistName = g.ArtistName
		entry.Parent = liteOf(g.Parent)
		for _, s := range g.Shorts {
			entry.Shorts = append(entry.Shorts, liteOf(s))
		}
		rec.Case2Groups = append(rec.Case2Groups, entry)
	}
	for _, e := range plan.AlbumsToUnlove {
		rec.Unloves = append(rec.Unloves, recordUnlove{
			ID: e.Album.ID, Title: e.Album.Title, Artist: e.Album.ArtistName,
			Case: e.Case.String(), Reason: e.Reason,
		})
	}
	return rec
}

func liteOf(a gateway.AlbumMetadata) recordAlbumLite {
	return recordAlbumLite{
		ID: a.ID, Title: a.Title,
		FanCount: a.FanCount, TrackCount: a.TrackCount,
	}
}

func writeRunRecord(dir string, started time.Time, rec runRecord) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	stamp := started.Format("20060102T150405Z")
	final := filepath.Join(dir, "deezer-loved-albums-dedupe-"+stamp+".json")
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

// confirmReader is wired up in Task 13.
var _ = bufio.NewReader
```

The `_ = bufio.NewReader` is intentional to keep the `bufio` import used; Task 13 turns it into real code. The same goes for `errApplyNotImplemented`. Don't be precious about removing them now — they'll be replaced wholesale.

- [ ] **Step 4: Update tests to reflect that the apply phase isn't done yet**

In `TestRun_emptyLovedSet` and `TestRun_dryRun_writesRecord_doesNotUnlove`, the apply phase is not exercised, so `errApplyNotImplemented` should not be returned. Both tests already short-circuit at the empty-plan or dry-run check. Verify by running.

```bash
go test ./internal/lovedalbums -run TestRun
```

Expected: 3 tests pass.

- [ ] **Step 5: Run full lovedalbums + build everything**

```bash
go test ./internal/lovedalbums
go build ./...
```

- [ ] **Step 6: Commit**

```bash
git add internal/lovedalbums/dedupe.go internal/lovedalbums/dedupe_test.go
git commit -m "feat(lovedalbums): orchestrator (list, plan, run-record, dry-run)"
```

---

## Task 13: `Run` orchestrator — confirmation + apply phase

**Files:**
- Modify: `internal/lovedalbums/dedupe.go`
- Modify: `internal/lovedalbums/dedupe_test.go`

Adds the confirm prompt, the un-love loop with throttle / skip-log / circuit-breaker, and ctx-cancellation between un-loves. Removes the `errApplyNotImplemented` placeholder.

- [ ] **Step 1: Write failing tests for the apply path**

Append to `internal/lovedalbums/dedupe_test.go`:

```go
func TestRun_apply_happyPath(t *testing.T) {
	tmp := t.TempDir()
	meta := map[string]gateway.AlbumMetadata{
		"1": {ID: "1", Title: "X", ArtistID: "a", TrackCount: 13, FanCount: 1000},
		"2": {ID: "2", Title: "x", ArtistID: "a", TrackCount: 13, FanCount: 5},
	}
	gw := newGW(meta, nil, []string{"1", "2"})
	out := &bytes.Buffer{}
	res, err := Run(context.Background(), gw, Options{
		BackupDir: tmp,
		Stdin:     strings.NewReader("yes\n"),
		Stdout:    out, Stderr: &bytes.Buffer{},
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if gw.removeCalls != 1 || gw.removed[0] != "2" {
		t.Errorf("removed = %v (calls=%d)", gw.removed, gw.removeCalls)
	}
	if res.AlbumsUnloved != 1 || res.AlbumsSkipped != 0 {
		t.Errorf("res = %+v", res)
	}
	if !strings.Contains(out.String(), "Type yes to apply") {
		t.Errorf("missing prompt in stdout: %q", out.String())
	}
}

func TestRun_apply_userDeclines_aborts(t *testing.T) {
	tmp := t.TempDir()
	meta := map[string]gateway.AlbumMetadata{
		"1": {ID: "1", Title: "X", ArtistID: "a", TrackCount: 13, FanCount: 1000},
		"2": {ID: "2", Title: "x", ArtistID: "a", TrackCount: 13, FanCount: 5},
	}
	gw := newGW(meta, nil, []string{"1", "2"})
	res, err := Run(context.Background(), gw, Options{
		BackupDir: tmp,
		Stdin:     strings.NewReader("no\n"),
		Stdout:    &bytes.Buffer{}, Stderr: &bytes.Buffer{},
	})
	if !errors.Is(err, ErrAborted) {
		t.Errorf("err = %v, want ErrAborted", err)
	}
	if gw.removeCalls != 0 {
		t.Errorf("removeCalls = %d, want 0", gw.removeCalls)
	}
	if res.AlbumsUnloved != 0 {
		t.Errorf("res.AlbumsUnloved = %d", res.AlbumsUnloved)
	}
}

func TestRun_apply_classifiedSkipAndContinue(t *testing.T) {
	tmp := t.TempDir()
	meta := map[string]gateway.AlbumMetadata{
		"w1": {ID: "w1", Title: "X", ArtistID: "a", TrackCount: 13, FanCount: 1000},
		"l1": {ID: "l1", Title: "x", ArtistID: "a", TrackCount: 13, FanCount: 5},
		"w2": {ID: "w2", Title: "Y", ArtistID: "a", TrackCount: 13, FanCount: 1000},
		"l2": {ID: "l2", Title: "y", ArtistID: "a", TrackCount: 13, FanCount: 5},
	}
	gw := newGW(meta, nil, []string{"w1", "l1", "w2", "l2"})
	gw.removeErr = map[string]error{
		"l1": &gateway.GatewayError{Kind: gateway.ErrNotFound, RawMessage: "x"},
	}
	res, err := Run(context.Background(), gw, Options{
		BackupDir: tmp,
		Stdin:     strings.NewReader("yes\n"),
		Stdout:    &bytes.Buffer{}, Stderr: &bytes.Buffer{},
		RetryBackoff: []time.Duration{}, // no retries
	})
	if err == nil || !strings.Contains(err.Error(), "skipped") {
		t.Fatalf("err = %v, want non-nil with 'skipped'", err)
	}
	if res.AlbumsUnloved != 1 || res.AlbumsSkipped != 1 {
		t.Errorf("res = %+v", res)
	}
	// Skip log should exist and contain l1's error.
	b, _ := os.ReadFile(res.SkipLogPath)
	if !strings.Contains(string(b), `"id":"l1"`) {
		t.Errorf("skip log missing l1: %q", string(b))
	}
}

func TestRun_apply_authFailureAborts(t *testing.T) {
	tmp := t.TempDir()
	meta := map[string]gateway.AlbumMetadata{
		"w": {ID: "w", Title: "X", ArtistID: "a", TrackCount: 13, FanCount: 1000},
		"l": {ID: "l", Title: "x", ArtistID: "a", TrackCount: 13, FanCount: 5},
	}
	gw := newGW(meta, nil, []string{"w", "l"})
	gw.removeErr = map[string]error{
		"l": &gateway.GatewayError{Kind: gateway.ErrAuthFailed, RawMessage: "x"},
	}
	_, err := Run(context.Background(), gw, Options{
		BackupDir: tmp,
		Stdin:     strings.NewReader("yes\n"),
		Stdout:    &bytes.Buffer{}, Stderr: &bytes.Buffer{},
		RetryBackoff: []time.Duration{},
	})
	if err == nil || !strings.Contains(err.Error(), "config.toml") {
		t.Errorf("err = %v, want refresh-arl message", err)
	}
}

func TestRun_apply_circuitBreakerTripsOnStreak(t *testing.T) {
	tmp := t.TempDir()
	// Five Case-1 groups, each with one loser that always fails un-love.
	meta := map[string]gateway.AlbumMetadata{}
	ids := []string{}
	for i := 0; i < 6; i++ {
		w := fmt.Sprintf("w%d", i)
		l := fmt.Sprintf("l%d", i)
		title := fmt.Sprintf("T%d", i)
		meta[w] = gateway.AlbumMetadata{ID: w, Title: title, ArtistID: fmt.Sprintf("a%d", i), TrackCount: 13, FanCount: 1000}
		meta[l] = gateway.AlbumMetadata{ID: l, Title: title, ArtistID: fmt.Sprintf("a%d", i), TrackCount: 13, FanCount: 1}
		ids = append(ids, w, l)
	}
	gw := newGW(meta, nil, ids)
	gw.removeErr = map[string]error{}
	for i := 0; i < 6; i++ {
		gw.removeErr[fmt.Sprintf("l%d", i)] = &gateway.GatewayError{Kind: gateway.ErrNotFound, RawMessage: "x"}
	}
	_, err := Run(context.Background(), gw, Options{
		BackupDir:                   tmp,
		Stdin:                       strings.NewReader("yes\n"),
		Stdout:                      &bytes.Buffer{}, Stderr: &bytes.Buffer{},
		RetryBackoff:                []time.Duration{},
		MaxConsecutiveFinalFailures: 5,
	})
	if err == nil || !strings.Contains(err.Error(), "consecutive") {
		t.Errorf("err = %v, want circuit-breaker message", err)
	}
	// Should have stopped after the 5th failure, not all 6.
	if gw.removeCalls != 5 {
		t.Errorf("removeCalls = %d, want 5 (breaker trips)", gw.removeCalls)
	}
}

func TestRun_apply_ctxCancelBetweenUnloves(t *testing.T) {
	tmp := t.TempDir()
	meta := map[string]gateway.AlbumMetadata{}
	ids := []string{}
	for i := 0; i < 6; i++ {
		w := fmt.Sprintf("w%d", i)
		l := fmt.Sprintf("l%d", i)
		title := fmt.Sprintf("T%d", i)
		meta[w] = gateway.AlbumMetadata{ID: w, Title: title, ArtistID: fmt.Sprintf("a%d", i), TrackCount: 13, FanCount: 1000}
		meta[l] = gateway.AlbumMetadata{ID: l, Title: title, ArtistID: fmt.Sprintf("a%d", i), TrackCount: 13, FanCount: 1}
		ids = append(ids, w, l)
	}
	gw := newGW(meta, nil, ids)

	// Cancel after the 2nd successful unlove via removeErr's bookkeeping.
	ctx, cancel := context.WithCancel(context.Background())
	gw2 := &cancellingGW{fullFakeGW: gw, after: 2, cancel: cancel}

	_, err := Run(ctx, gw2, Options{
		BackupDir: tmp,
		Stdin:     strings.NewReader("yes\n"),
		Stdout:    &bytes.Buffer{}, Stderr: &bytes.Buffer{},
		RetryBackoff: []time.Duration{},
	})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
	if gw.removed == nil || len(gw.removed) > 3 {
		t.Errorf("unloved = %v, want at most 3", gw.removed)
	}
}

type cancellingGW struct {
	*fullFakeGW
	after  int
	cancel context.CancelFunc
}

func (g *cancellingGW) RemoveFavoriteAlbum(ctx context.Context, id string) error {
	if err := g.fullFakeGW.RemoveFavoriteAlbum(ctx, id); err != nil {
		return err
	}
	if len(g.fullFakeGW.removed) >= g.after {
		g.cancel()
	}
	return nil
}
```

You may need to add `"fmt"` to the test file's imports.

- [ ] **Step 2: Run, expect FAIL**

```bash
go test ./internal/lovedalbums -run TestRun_apply
```

Expected: tests fail (`errApplyNotImplemented` returned, or compile errors against new test fields).

- [ ] **Step 3: Replace the placeholder with the apply phase**

In `internal/lovedalbums/dedupe.go`, replace the `// 11–14: confirmation + apply phase — implemented in Task 13.` block (and `errApplyNotImplemented`) with:

```go
	// 11. Confirmation gate.
	confirmReader := bufio.NewReader(opts.Stdin)
	fmt.Fprintf(opts.Stdout, "Will unlove %d albums (%d case-1 dups, %d case-2 singles).\n",
		res.AlbumsToUnlove, res.Case1Groups, res.Case2Groups)
	fmt.Fprintf(opts.Stdout, "Run record: %s\n", recPath)
	fmt.Fprint(opts.Stdout, "Type yes to apply: ")
	ans, _ := confirmReader.ReadString('\n')
	if !isYes(ans) {
		fmt.Fprintln(opts.Stdout, "Aborted.")
		res.Elapsed = time.Since(res.StartedAt)
		return res, ErrAborted
	}

	// 12. Open skip log.
	skipLog, skipPath, err := openSkipLog(opts.BackupDir, recPath)
	if err != nil {
		return res, fmt.Errorf("open skip log: %w", err)
	}
	defer skipLog.Close()
	res.SkipLogPath = skipPath

	// 13. Apply phase: un-love each loser.
	maxConsec := opts.MaxConsecutiveFinalFailures
	if maxConsec == 0 {
		maxConsec = throttle.DefaultMaxConsecutiveFinalFailures
	}
	streak := 0
	for _, e := range plan.AlbumsToUnlove {
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
		albumID := e.Album.ID
		callErr := throttle.RunOne(ctx, func(ctx context.Context) error {
			return gw.RemoveFavoriteAlbum(ctx, albumID)
		}, gateway.IsRetryable, opts.RetryBackoff)
		if callErr == nil {
			res.AlbumsUnloved++
			streak = 0
			continue
		}
		if errors.Is(callErr, context.Canceled) || errors.Is(callErr, context.DeadlineExceeded) {
			res.Elapsed = time.Since(res.StartedAt)
			return res, callErr
		}
		var ge *gateway.GatewayError
		if errors.As(callErr, &ge) && ge.Kind == gateway.ErrAuthFailed {
			res.Elapsed = time.Since(res.StartedAt)
			return res, fmt.Errorf("auth failed during unlove (refresh your arl in ~/.config/deezer-tools/config.toml): %w", callErr)
		}
		res.AlbumsSkipped++
		_ = writeSkipEntry(skipLog, e, callErr)
		streak++
		if maxConsec > 0 && streak >= maxConsec {
			res.Elapsed = time.Since(res.StartedAt)
			return res, fmt.Errorf("aborting: %d consecutive unlove failures (quota likely tripped or service degraded). Skipped items recorded in %s", streak, skipPath)
		}
	}

	// 14. Final summary.
	res.Elapsed = time.Since(res.StartedAt)
	fmt.Fprintf(opts.Stdout, "Unloved %d albums (%d case-1, %d case-2), skipped %d",
		res.AlbumsUnloved, res.Case1Groups, res.Case2Groups, res.AlbumsSkipped)
	if res.AlbumsSkipped > 0 {
		fmt.Fprintf(opts.Stdout, " (see %s)", skipPath)
	}
	fmt.Fprintf(opts.Stdout, ", elapsed %s\n", res.Elapsed.Round(time.Second))

	if res.AlbumsSkipped > 0 {
		return res, fmt.Errorf("%d album(s) skipped", res.AlbumsSkipped)
	}
	return res, nil
}

func isYes(s string) bool {
	return strings.EqualFold(strings.TrimSpace(s), "yes")
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
	ID     string `json:"id"`
	Title  string `json:"title,omitempty"`
	Artist string `json:"artist,omitempty"`
	Case   string `json:"case"`
	Reason string `json:"reason,omitempty"`
	Error  string `json:"error"`
}

func writeSkipEntry(w io.Writer, e UnloveEntry, err error) error {
	rec := skipEntry{
		ID: e.Album.ID, Title: e.Album.Title, Artist: e.Album.ArtistName,
		Case: e.Case.String(), Reason: e.Reason, Error: err.Error(),
	}
	b, _ := json.Marshal(rec)
	_, werr := fmt.Fprintln(w, string(b))
	return werr
}
```

Remove the `errApplyNotImplemented` declaration and the trailing `var _ = bufio.NewReader` placeholder line.

- [ ] **Step 4: Run, expect PASS for all apply tests**

```bash
go test ./internal/lovedalbums -run TestRun
```

Expected: all `TestRun*` tests pass.

- [ ] **Step 5: Run full lovedalbums + build**

```bash
go test ./internal/lovedalbums
go vet ./internal/lovedalbums
go build ./...
```

- [ ] **Step 6: Commit**

```bash
git add internal/lovedalbums/dedupe.go internal/lovedalbums/dedupe_test.go
git commit -m "feat(lovedalbums): confirm + apply with throttle, skip log, breaker"
```

---

## Task 14: CLI wiring and `.gitignore`

**Files:**
- Create: `cmd/deezer-tools/lovedalbums_cmd.go`
- Modify: `cmd/deezer-tools/main.go`
- Modify: `.gitignore`

- [ ] **Step 1: Extend `.gitignore`**

Open `/home/niref/dev/frosco/deezer-tools/.gitignore` and add the new prefix lines under the existing `# Backups and run records generated by the tool` block:

```
deezer-loved-albums-dedupe-*.json
deezer-loved-albums-dedupe-*.skip.log
```

- [ ] **Step 2: Create the Cobra wiring**

Create `cmd/deezer-tools/lovedalbums_cmd.go`:

```go
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/niref/deezer-tools/internal/config"
	"github.com/niref/deezer-tools/internal/gateway"
	"github.com/niref/deezer-tools/internal/lovedalbums"
	"github.com/spf13/cobra"
)

func newLovedAlbumsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "loved-albums",
		Short: "Tools that operate on the user's loved-albums collection",
	}
	cmd.AddCommand(newDedupeCmd())
	return cmd
}

func newDedupeCmd() *cobra.Command {
	var dryRun bool
	var backupDir string
	var threshold int

	cmd := &cobra.Command{
		Use:   "dedupe",
		Short: "Find and (after confirm) un-love duplicate entries in the loved-albums list",
		Long: `Find duplicate loved albums in two cases:

  1. Same artist, same normalised title, different ALB_IDs → keep the album
     with most tracks (then most fans, then lowest ID); un-love the rest.
  2. A short loved album (default ≤3 tracks) whose title equals a track on a
     longer same-artist album that's also loved → un-love the short one.

Writes a JSON run record before doing anything destructive. After a single
batched confirmation, un-loves the losers in sequence with the same paced
throttle / retry / circuit-breaker discipline as the wipe and love-contents
commands.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfgPath := defaultConfigPath()
			cfg, err := config.Load(cfgPath)
			if err != nil {
				return fmt.Errorf("config: %w", err)
			}
			client := gateway.New(cfg.ARL)

			_, err = lovedalbums.Run(cmd.Context(), client, lovedalbums.Options{
				DryRun:              dryRun,
				BackupDir:           backupDir,
				Case2TrackThreshold: threshold,
				Stdin:               cmd.InOrStdin(),
				Stdout:              cmd.OutOrStdout(),
				Stderr:              cmd.ErrOrStderr(),
				OpenTTY:             openLovedAlbumsTTY,
			})
			return err
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "detect, write run-record, do not unlove")
	cmd.Flags().StringVar(&backupDir, "backup-dir", ".", "directory for the run-record JSON and skip log")
	cmd.Flags().IntVar(&threshold, "case2-track-threshold", 3, "albums with at most this many tracks count as 'short' for Case 2")
	return cmd
}

// openLovedAlbumsTTY is unused by the current Options surface but kept in the
// signature for parity with playlistlove and for future stdin-fed inputs.
func openLovedAlbumsTTY() (io.ReadCloser, error) {
	return os.OpenFile("/dev/tty", os.O_RDONLY, 0)
}
```

- [ ] **Step 3: Register the command in `main.go`**

Modify `cmd/deezer-tools/main.go`:

```go
func init() {
	rootCmd.AddCommand(newLovedTracksCmd())
	rootCmd.AddCommand(newPlaylistsCmd())
	rootCmd.AddCommand(newLovedAlbumsCmd())
}
```

- [ ] **Step 4: Build + smoke-test the CLI**

```bash
go build -o deezer-tools ./cmd/deezer-tools
./deezer-tools loved-albums dedupe --help
```

Expected: help output describing the dedupe command, including the `--dry-run`, `--backup-dir`, `--case2-track-threshold` flags.

- [ ] **Step 5: Run full test suite + vet**

```bash
go test ./...
go vet ./...
```

Expected: all green.

- [ ] **Step 6: Commit**

```bash
git add cmd/deezer-tools/main.go cmd/deezer-tools/lovedalbums_cmd.go .gitignore
git commit -m "feat(cmd): wire loved-albums dedupe subcommand"
```

---

## Task 15: Within-playlist Case-1 dedup in `playlistlove`

**Files:**
- Modify: `internal/playlistlove/diff.go`
- Modify: `internal/playlistlove/diff_test.go`
- Modify: `internal/playlistlove/run.go`

Add a Case-1 collapse pass to `Aggregate` (or its caller). Bounded API surface — metadata calls are scoped to actual conflict groups.

- [ ] **Step 1: Write the failing tests**

Append to `internal/playlistlove/diff_test.go`:

```go
import (
	// existing imports plus:
	"context"

	"github.com/niref/deezer-tools/internal/gateway"
)

type fakeMeta struct {
	byID map[string]gateway.AlbumMetadata
	calls int
}

func (f *fakeMeta) GetAlbumMetadata(ctx context.Context, id string) (gateway.AlbumMetadata, error) {
	f.calls++
	return f.byID[id], nil
}

func TestCollapseCase1WithinPlaylist_noConflict_noCalls(t *testing.T) {
	set := AggregatedSet{
		Albums: []Album{
			{ID: "1", Title: "A", Artist: "X"},
			{ID: "2", Title: "B", Artist: "X"},
		},
	}
	gw := &fakeMeta{byID: map[string]gateway.AlbumMetadata{}}
	got, err := CollapseCase1WithinPlaylist(context.Background(), gw, set, nil)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if gw.calls != 0 {
		t.Errorf("metadata calls = %d, want 0", gw.calls)
	}
	if got.Case1WithinPlaylistSuppressed != 0 {
		t.Errorf("suppressed = %d", got.Case1WithinPlaylistSuppressed)
	}
}

func TestCollapseCase1WithinPlaylist_collapsesGroup(t *testing.T) {
	set := AggregatedSet{
		Albums: []Album{
			{ID: "1", Title: "Random Access Memories", Artist: "Daft Punk"},
			{ID: "2", Title: "RANDOM ACCESS MEMORIES", Artist: "Daft Punk"},
			{ID: "3", Title: "Discovery", Artist: "Daft Punk"},
		},
	}
	// Both candidates need ART_ID set on metadata — match.go groups by
	// ArtistID. The Album records carry just artist names; the matching
	// pass uses the metadata's ArtistID.
	gw := &fakeMeta{
		byID: map[string]gateway.AlbumMetadata{
			"1": {ID: "1", Title: "Random Access Memories", ArtistID: "8537", TrackCount: 13, FanCount: 999999},
			"2": {ID: "2", Title: "RANDOM ACCESS MEMORIES", ArtistID: "8537", TrackCount: 13, FanCount: 1},
		},
	}
	got, err := CollapseCase1WithinPlaylist(context.Background(), gw, set, nil)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if gw.calls != 2 {
		t.Errorf("metadata calls = %d, want 2", gw.calls)
	}
	if got.Case1WithinPlaylistSuppressed != 1 {
		t.Errorf("suppressed = %d, want 1", got.Case1WithinPlaylistSuppressed)
	}
	if len(got.Albums) != 2 {
		t.Fatalf("len(Albums) = %d, want 2", len(got.Albums))
	}
	// Winner is "1" (more fans, same tracks).
	for _, a := range got.Albums {
		if a.ID == "2" {
			t.Errorf("loser still in Albums: %v", a)
		}
	}
}

func TestCollapseCase1WithinPlaylist_metadataNotFound_dropsMember(t *testing.T) {
	set := AggregatedSet{
		Albums: []Album{
			{ID: "1", Title: "X", Artist: "A"},
			{ID: "2", Title: "X", Artist: "A"},
			{ID: "3", Title: "X", Artist: "A"},
		},
	}
	gw := &errfulMeta{
		byID: map[string]gateway.AlbumMetadata{
			"1": {ID: "1", Title: "X", ArtistID: "1", TrackCount: 13, FanCount: 100},
			// "2" returns ErrNotFound; "3" returns metadata.
			"3": {ID: "3", Title: "X", ArtistID: "1", TrackCount: 13, FanCount: 50},
		},
		errByID: map[string]error{
			"2": &gateway.GatewayError{Kind: gateway.ErrNotFound, RawMessage: "x"},
		},
	}
	got, err := CollapseCase1WithinPlaylist(context.Background(), gw, set, nil)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	// The conflict group resolves on { "1", "3" } only. Suppressed = 1
	// (the loser from {1,3}); "2" is also dropped because it has no
	// metadata, but counts under suppressed since it can't be selected.
	if got.Case1WithinPlaylistSuppressed != 2 {
		t.Errorf("suppressed = %d, want 2 (1 loser + 1 not-found)", got.Case1WithinPlaylistSuppressed)
	}
}

type errfulMeta struct {
	byID    map[string]gateway.AlbumMetadata
	errByID map[string]error
}

func (e *errfulMeta) GetAlbumMetadata(ctx context.Context, id string) (gateway.AlbumMetadata, error) {
	if err, ok := e.errByID[id]; ok {
		return gateway.AlbumMetadata{}, err
	}
	return e.byID[id], nil
}
```

- [ ] **Step 2: Run, expect FAIL**

```bash
go test ./internal/playlistlove -run TestCollapseCase1WithinPlaylist
```

Expected: compile error (`undefined: CollapseCase1WithinPlaylist`, `AggregatedSet.Case1WithinPlaylistSuppressed not a field`).

- [ ] **Step 3: Add the field and the function**

In `internal/playlistlove/diff.go`, append the new field to `AggregatedSet`:

```go
type AggregatedSet struct {
	Albums                       []Album
	Artists                      []Artist
	UnparseableSongs             int
	VariousArtistsSkipped        int
	Case1WithinPlaylistSuppressed int
}
```

Append the function (and the metadata interface) at the bottom of `diff.go`:

```go
import (
	// existing imports plus:
	"context"
	"errors"
	"time"

	"github.com/niref/deezer-tools/internal/lovedalbums"
	"github.com/niref/deezer-tools/internal/throttle"
)

// MetadataFetcher is the slice of internal/gateway.Client used by
// CollapseCase1WithinPlaylist. Defined here to keep the playlistlove
// dependency narrow.
type MetadataFetcher interface {
	GetAlbumMetadata(ctx context.Context, albumID string) (gateway.AlbumMetadata, error)
}

// CollapseCase1WithinPlaylist applies the lovedalbums Case-1 dedup rule to
// the set's albums in-place. For each conflict group (≥2 candidates sharing
// `(artist, normalised title)`), it fetches metadata for every member, picks
// the winner via lovedalbums.PickWinner, and drops the losers from
// set.Albums. The number of dropped candidates is recorded in
// Case1WithinPlaylistSuppressed.
//
// API cost is bounded by conflict-group membership, NOT by playlist size.
// A typical playlist run hits zero or a handful of conflict groups.
//
// Metadata-fetch failures (e.g. ErrNotFound) drop the affected member from
// the conflict group; the run continues. Auth failures bubble up.
//
// retry: nil → throttle.DefaultRetryBackoff; empty → first attempt only.
func CollapseCase1WithinPlaylist(
	ctx context.Context,
	gw MetadataFetcher,
	set AggregatedSet,
	retry []time.Duration,
) (AggregatedSet, error) {
	type key struct{ artist, title string }
	type idx struct{ pos int }
	groups := make(map[key][]int)
	for i, a := range set.Albums {
		k := key{a.Artist, lovedalbums.Normalise(a.Title)}
		groups[k] = append(groups[k], i)
	}
	drop := make(map[int]bool)
	for _, indices := range groups {
		if len(indices) < 2 {
			continue
		}
		var members []gateway.AlbumMetadata
		var memberIdx []int
		for _, i := range indices {
			if err := throttle.Sleep(ctx); err != nil {
				return set, err
			}
			id := set.Albums[i].ID
			var meta gateway.AlbumMetadata
			callErr := throttle.RunOne(ctx, func(ctx context.Context) error {
				var err error
				meta, err = gw.GetAlbumMetadata(ctx, id)
				return err
			}, gateway.IsRetryable, retry)
			if callErr == nil {
				members = append(members, meta)
				memberIdx = append(memberIdx, i)
				continue
			}
			if errors.Is(callErr, context.Canceled) || errors.Is(callErr, context.DeadlineExceeded) {
				return set, callErr
			}
			var ge *gateway.GatewayError
			if errors.As(callErr, &ge) && ge.Kind == gateway.ErrAuthFailed {
				return set, callErr
			}
			// Drop unresolvable members from the candidate set.
			drop[i] = true
		}
		if len(members) < 2 {
			continue
		}
		ranked := lovedalbums.PickWinner(members)
		winnerID := ranked[0].ID
		for j, m := range members {
			if m.ID != winnerID {
				drop[memberIdx[j]] = true
			}
		}
	}
	if len(drop) == 0 {
		return set, nil
	}
	out := AggregatedSet{
		UnparseableSongs:              set.UnparseableSongs,
		VariousArtistsSkipped:         set.VariousArtistsSkipped,
		Case1WithinPlaylistSuppressed: len(drop),
		Artists:                       set.Artists,
	}
	for i, a := range set.Albums {
		if drop[i] {
			continue
		}
		out.Albums = append(out.Albums, a)
	}
	return out, nil
}
```

- [ ] **Step 4: Wire `Run` to call the new pass**

In `internal/playlistlove/run.go`, add `GetAlbumMetadata` to the `Gateway` interface:

```go
type Gateway interface {
	ListPlaylistSongs(ctx context.Context, playlistID string, pageSize int) ([]gateway.PlaylistSong, error)
	ListFavoriteAlbumIDs(ctx context.Context) ([]string, error)
	ListFavoriteArtistIDs(ctx context.Context) ([]string, error)
	GetAlbumMetadata(ctx context.Context, albumID string) (gateway.AlbumMetadata, error)
	AddFavoriteAlbum(ctx context.Context, albumID string) error
	AddFavoriteArtist(ctx context.Context, artistID string) error
}
```

Find the `// 4. Aggregate + dedupe.` block and replace with:

```go
	// 4. Aggregate + dedupe.
	set := Aggregate(allSongs, opts.VariousArtistsID)
	set, err = CollapseCase1WithinPlaylist(ctx, gw, set, opts.RetryBackoff)
	if err != nil {
		var gerr *gateway.GatewayError
		if errors.As(err, &gerr) && gerr.Kind == gateway.ErrAuthFailed {
			return nil, fmt.Errorf("auth failed during within-playlist dedup (refresh your arl in ~/.config/deezer-tools/config.toml): %w", err)
		}
		return nil, fmt.Errorf("within-playlist dedup: %w", err)
	}
```

Add a stat to `runRecordStats`:

```go
type runRecordStats struct {
	// ... existing fields ...
	Case1WithinPlaylistSuppressed int `json:"case1_within_playlist_suppressed"`
}
```

And in the run-record construction, include it:

```go
	Stats: runRecordStats{
		// ... existing fields ...
		Case1WithinPlaylistSuppressed: set.Case1WithinPlaylistSuppressed,
	},
```

- [ ] **Step 5: Run, expect PASS**

```bash
go test ./internal/playlistlove -run TestCollapseCase1WithinPlaylist
go test ./internal/playlistlove
```

Expected: all green. Existing playlistlove tests must still pass — if a test relies on a fake `Gateway` that didn't have `GetAlbumMetadata`, add a no-op method to the fake.

- [ ] **Step 6: Build + vet**

```bash
go build ./...
go vet ./...
```

- [ ] **Step 7: Commit**

```bash
git add internal/playlistlove/diff.go internal/playlistlove/diff_test.go internal/playlistlove/run.go
git commit -m "feat(playlistlove): within-playlist Case-1 dedup using lovedalbums helpers"
```

---

## Task 16: Live integration test — read-only

**Files:**
- Modify: `internal/gateway/integration_test.go`

Read-only verification of the two new metadata methods against the user's real account. `RemoveFavoriteAlbum` is **not** in the integration test — it's a write, exercised manually at first wet run.

- [ ] **Step 1: Inspect the existing integration test**

```bash
sed -n '1,80p' /home/niref/dev/frosco/deezer-tools/internal/gateway/integration_test.go
```

Note the gating (`DEEZER_INTEGRATION=1`), the config-loading helper, and the existing live calls. Mirror them.

- [ ] **Step 2: Add the new live cases**

Append to `internal/gateway/integration_test.go` (within the existing `TestIntegration_*` block or as a new function — match local convention):

```go
func TestIntegration_GetAlbumMetadata_andTracks(t *testing.T) {
	if os.Getenv("DEEZER_INTEGRATION") != "1" {
		t.Skip("DEEZER_INTEGRATION=1 not set")
	}
	c := liveClient(t) // existing helper; substitute the actual name from the file
	ctx := context.Background()

	ids, err := c.ListFavoriteAlbumIDs(ctx)
	if err != nil {
		t.Fatalf("list loved albums: %v", err)
	}
	if len(ids) == 0 {
		t.Skip("account has no loved albums; cannot run")
	}

	// Take the first ALB_ID and fetch metadata.
	meta, err := c.GetAlbumMetadata(ctx, ids[0])
	if err != nil {
		t.Fatalf("GetAlbumMetadata(%s): %v", ids[0], err)
	}
	if meta.ID != ids[0] {
		t.Errorf("ID round-trip: got %s, want %s", meta.ID, ids[0])
	}
	if meta.Title == "" || meta.ArtistID == "" || meta.ArtistName == "" {
		t.Errorf("missing metadata: %+v", meta)
	}
	if meta.TrackCount <= 0 {
		t.Errorf("TrackCount = %d", meta.TrackCount)
	}
	t.Logf("first loved album: %+v", meta)

	// Find a long album for the tracklist test (or fall back to the first
	// one if all loved albums are short).
	probe := meta
	for _, id := range ids {
		if probe.TrackCount > 1 {
			break
		}
		m, err := c.GetAlbumMetadata(ctx, id)
		if err != nil {
			continue
		}
		probe = m
	}

	tracks, err := c.ListAlbumTracks(ctx, probe.ID)
	if err != nil {
		t.Fatalf("ListAlbumTracks(%s): %v", probe.ID, err)
	}
	if len(tracks) == 0 {
		t.Errorf("no tracks for %s", probe.ID)
	}
	if probe.TrackCount > 0 && len(tracks) != probe.TrackCount {
		t.Errorf("tracks=%d, TrackCount=%d (mismatch may indicate flexString miss)",
			len(tracks), probe.TrackCount)
	}
	for i, tr := range tracks {
		if tr.ID == "" || tr.Title == "" {
			t.Errorf("tracks[%d] missing fields: %+v", i, tr)
		}
	}
	t.Logf("first 3 tracks of %s: %+v", probe.Title, tracks[:min(3, len(tracks))])
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
```

If `liveClient(t)` has a different name in the existing file, substitute. If `min` is already defined in the file, drop the local declaration.

- [ ] **Step 3: Run the test (gated)**

```bash
DEEZER_INTEGRATION=1 go test ./internal/gateway -run TestIntegration_GetAlbumMetadata_andTracks -v
```

Expected: PASS, with logs showing real metadata + first 3 tracks. If TrackCount mismatches `len(tracks)`, that's a flexString-on-counts hint — fix the metadata struct (add `flexString` to whichever count field is flexed) and update the Task 2 research doc.

- [ ] **Step 4: Run unit suite to confirm no regressions**

```bash
go test ./...
```

- [ ] **Step 5: Commit**

```bash
git add internal/gateway/integration_test.go
git commit -m "test(gateway): live read-only checks for album metadata + tracklist"
```

---

## Task 17: First wet-run smoke + documentation polish

**Files:**
- Modify: `README.md` (usage section)

This task is partly hand-on-keyboard verification (a real wet run) and partly README polish. No new tests.

- [ ] **Step 1: Smoke test the CLI in `--dry-run`**

```bash
./deezer-tools loved-albums dedupe --dry-run --backup-dir /tmp/dedupe-smoke
```

Expected: exits `0`. Stdout contains `would unlove N albums (X case-1, Y case-2), run-record at /tmp/dedupe-smoke/...`. Run-record JSON is well-formed and contains plausible Case-1 / Case-2 entries (you can `cat` and inspect).

If the dry-run blows up, capture the failure mode (wire shape mismatch, classifier miss) and update either the gateway code or the Task 2 research doc accordingly. This is the spec's discovery-at-impl-time work.

- [ ] **Step 2: Verify the run-record looks sensible**

```bash
ls /tmp/dedupe-smoke/
cat /tmp/dedupe-smoke/deezer-loved-albums-dedupe-*.json | jq '.stats, .case1_groups[0], .case2_groups[0]'
```

Inspect a few `case1_groups` and `case2_groups` entries by hand. If picks look off (e.g. fan-count-based picks consistently choose a less canonical version), capture the pattern as a spec follow-up — don't fix in this PR.

- [ ] **Step 3: Wet run — small subset first**

There is no `--limit` flag (out of scope for v1). The only way to do a small wet run is to ensure the loved set is small or back up + truncate beforehand. **Skip the full wet run if you're not confident in the dry-run output.** If you proceed, do it on the user's own account and watch stderr for skip-log entries.

```bash
./deezer-tools loved-albums dedupe --backup-dir /tmp/dedupe-smoke
# answer "yes" at the prompt
```

Verify:
- Exit code is `0` if no skips, non-zero if any.
- The number un-loved matches `albums_to_unlove` from the run record.
- Re-running the command reports `Nothing to dedupe; loved-albums list is clean.` (idempotency).

- [ ] **Step 4: README**

Open `README.md`. Add a `loved-albums dedupe` section under the existing usage table-of-contents, mirroring the style of the `playlists love-contents` section. Cover:
- Brief description of the two cases.
- The `--dry-run`, `--backup-dir`, `--case2-track-threshold` flags.
- Note that the run-record + skip log are gitignored.

```markdown
### `loved-albums dedupe`

Find and remove duplicate entries in your loved-albums list:

- **Case 1** — same artist, same normalised title, different ALB_IDs. Picks the
  album with most tracks → most fans → lowest ID; un-loves the rest.
- **Case 2** — a short loved album (default ≤3 tracks) whose title equals a
  track on a longer same-artist album that's also loved. Un-loves the short one.

```sh
deezer-tools loved-albums dedupe --dry-run            # detect, write report, do not unlove
deezer-tools loved-albums dedupe --backup-dir ./out   # write run record + skip log to ./out
deezer-tools loved-albums dedupe --case2-track-threshold 5
```

After detection a run record is written to
`<backup-dir>/deezer-loved-albums-dedupe-<UTC>.json`. After confirmation, losers
are un-loved with the same paced-throttle / retry / circuit-breaker discipline
as `loved-tracks wipe` and `playlists love-contents`. Run record and skip log are
gitignored.
```

- [ ] **Step 5: Final test sweep**

```bash
go test ./...
go vet ./...
go build ./...
```

- [ ] **Step 6: Commit**

```bash
git add README.md
git commit -m "docs: README usage for loved-albums dedupe"
```

---

## Self-review checklist (run by the implementing engineer at end of plan)

- [ ] All tasks committed; `git log --oneline wip/loved-albums-dedupe` shows a clean history of the feature commits.
- [ ] `go test ./...` is green.
- [ ] `go vet ./...` is green.
- [ ] `./deezer-tools loved-albums dedupe --help` shows the documented flags.
- [ ] `--dry-run` produces a well-formed run-record JSON.
- [ ] No `<…>` placeholders remain in the code (search: `grep -rn '<[A-Z_]*>' internal/gateway/albums.go` — should be empty).
- [ ] The Task 2 research doc has been committed to `main` (separate branch).
- [ ] The MR diff (against `main`) does NOT include any spec / plan / research files.
