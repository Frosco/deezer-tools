---
title: gw-light Go adapter quirks - cookie jar and flexString
date: 2026-04-28
last_refreshed: 2026-05-07
category: design-patterns
module: deezer-tools
problem_type: design_pattern
component: tooling
severity: high
applies_when:
  - "Implementing a Go client against Deezer's unofficial gw-light.php gateway"
  - "Porting a session-stateful Python HTTP client (deezer-py, deemix) to Go"
  - "Authenticating to gw-light with an arl cookie and calling CSRF-protected methods"
  - "Decoding gw-light JSON payloads with strict struct typing"
symptoms:
  - "gateway song.getFavoriteIds: Invalid CSRF token (status=200) immediately after a successful deezer.getUserData call"
  - "json: cannot unmarshal number into Go struct field of type string on SNG_ID mid-pagination"
  - "Unit tests against httptest pass but live integration runs fail"
  - "Behavior differs between identical-looking gw-light responses across pagination chunks"
tags: [deezer, gw-light, http-client, json-decoding, cookie-jar, integration]
---

# gw-light Go adapter quirks - cookie jar and flexString

## Context

You are writing a Go HTTP adapter against Deezer's unofficial `gw-light.php` gateway, with the only references being Python clients (`deezer-py`, `deemix`). Unit tests with `httptest.Server` pass; integration against a real account fails in ways the unit tests didn't predict.

This guidance applies whenever you are porting a session-stateful, dynamically-typed wire protocol from Python to Go. Two real bugs encountered while building `loved-tracks wipe` (commits `41be10d` and `1db64eb` on `wip/loved-tracks-wipe`) shared a single root cause: **trusting OSS Python reference libraries too literally when porting to Go**. Python's `requests.Session` makes cookie persistence implicit; Python's dynamic typing makes JSON shape variance invisible. A statically-typed Go port turns both implicit behaviors into explicit failures.

## Guidance

### 1. Always attach a cookie jar

Python's `requests.Session` persists server-set cookies implicitly. Go's `http.Client` does not - you must supply a `Jar`. For any session-stateful gateway, this is a default, not an optimization:

```go
import "net/http/cookiejar"

jar, _ := cookiejar.New(nil)
client := &http.Client{
    Timeout: 30 * time.Second,
    Jar:     jar,
}
```

Even if you attach known cookies (e.g. `arl`) explicitly via `req.AddCookie`, the jar is still required to retain server-issued session cookies (e.g. `sid`) that bind CSRF tokens, anti-bot tokens, and similar.

### 2. Use a flex string type for ID-shaped fields

PHP and Python serialize the same logical field as either a quoted string or a bare number across responses - sometimes within a single response. `json.Number` handles only bare numbers, not quoted strings. For ID-like fields, define a small permissive type:

```go
// flexString unmarshals from either a JSON string or a JSON number.
type flexString string

func (s *flexString) UnmarshalJSON(b []byte) error {
    if len(b) > 0 && b[0] == '"' {
        var str string
        if err := json.Unmarshal(b, &str); err != nil {
            return err
        }
        *s = flexString(str)
        return nil
    }
    *s = flexString(b)
    return nil
}
```

Apply it to every ID-like field by default. Reserve `json.Number` for fields that only ever appear as bare numbers (timestamps, totals).

### 3. Integration tests gated on real credentials

Unit tests with hand-crafted `httptest.Server` responses cannot simulate behaviors the implementer doesn't already know about - the mocks encode the implementer's assumptions, not the gateway's actual behavior. Add a gated live test:

```go
if os.Getenv("DEEZER_INTEGRATION") != "1" {
    t.Skip("set DEEZER_INTEGRATION=1 to run live")
}
```

At minimum, exercise: (a) two sequential authenticated calls that share a session, (b) a paginated list large enough to cross multiple chunks.

### 4. Regression tests must mix types within a single payload

When a heterogeneous-typing bug is fixed, the regression test must include both wire shapes in **one response** - not as separate test cases - because the bug only manifests when a single decode pass sees both:

```go
`{"error":[],"results":{"data":[`+
    `{"SNG_ID":1,...},`+
    `{"SNG_ID":"42",...}`+
`]}}`
```

Single-type fixtures will mask heterogeneity bugs.

### 5. Don't trust analogy when extending a fix

When a typing fix is applied to one field, audit all sibling fields in the same response for the same wire characteristics. The protocol research doc on `main` correctly listed `SNG_ID/USER_ID/total` as varying types, but the `json.Number` fix only worked for the bare-number cases - `SNG_ID` arrived as both forms and needed a different mechanism. Every "JSON typing varies" remediation must enumerate all affected fields in one pass.

### 6. When converting flexString to a non-string type, distinguish missing from malformed

`flexString` is correct as a permissive *decoder* — it gets bytes off the wire without crashing. But "we got bytes off the wire" is not the same as "we got a meaningful value." Any helper that converts `flexString` to a numeric type (or any other domain type) is the seam where bytes become semantic values, and it must explicitly distinguish:

- **Field missing or explicitly null** — gateway is telling us "no value here." Map to the type's zero default with no error: this is the legitimate "unset" case.
- **Field present but unparseable** — gateway returned bytes we did not expect (`"412k"`, a stringified float, an unknown sentinel). The zero default here is **not** meaningful; it is a fabricated reading. Return an error.

If the helper collapses both cases into the same zero-with-no-error, the call site cannot distinguish them — and any downstream consumer that uses the value as a sort key, a comparison key, or a destructive-action input will silently corrupt user data.

The risk is not theoretical: see [docs/solutions/logic-errors/parseflexint-swallowed-error-album-dedupe-2026-05-07.md](../logic-errors/parseflexint-swallowed-error-album-dedupe-2026-05-07.md) for a real case where `parseFlexInt` (the integer-typed cousin of `flexString`) was wired into the album metadata struct with `_` for the error return on `NB_FAN` and `NUMBER_TRACK`. A null or malformed value would have silently scored albums as 0-fans/0-tracks, causing the dedup orchestrator to un-love the wrong canonical edition.

The contract that worked:

```go
// parseFlexInt parses a flexString that might arrive as a quoted string, a
// bare JSON number, JSON null, or be absent. Returns 0, nil for empty/null
// input (treated as "field missing or unset"). Returns 0, err if the content
// is non-empty but not a valid integer — propagating lets the caller skip
// and log the record rather than silently scoring it 0 on a sort key.
func parseFlexInt(s flexString) (int, error) {
    if s == "" || string(s) == "null" {
        return 0, nil
    }
    return strconv.Atoi(string(s))
}
```

Two tests pin the contract: one that exercises the empty/null path (asserts zero with no error) and one that exercises the malformed path (asserts a wrapped error mentioning the field name). Reviewing a `_ :=` discard on a parse return is the cheapest way to catch this class of bug.

## Why This Matters

Both bugs broke the loved-tracks wipe end-to-end against a real account, and either would silently re-appear in every future gw-light adapter (playlists, loved-albums, etc.) if the patterns above aren't established as defaults. Unit tests with mocked responses can't catch either class of bug. The cost of getting this wrong is correctness bugs that surface only against a real account, often after substantial work has already been done in a single run - in this case, the SNG_ID variance only showed up on chunk 1800-2000, eight successful chunks deep.

## When to Apply

- Writing any new HTTP adapter against `gw-light.php` or a similar undocumented PHP/Python-fronted gateway.
- Porting a Python (especially `requests.Session`-based) client to Go.
- Adding a new endpoint to an existing adapter where the response shape has not been observed across many real responses.
- Reviewing a Go HTTP client whose only tests use hand-crafted `httptest.Server` payloads.

## Examples

### Cookie jar - before and after

Before (broken):

```go
func New(arl string) *Client {
    return &Client{
        httpClient: &http.Client{Timeout: 30 * time.Second},
        arl:        arl,
        baseURL:    defaultBaseURL,
    }
}
```

After (fixed):

```go
func New(arl string) *Client {
    jar, _ := cookiejar.New(nil)
    return &Client{
        httpClient: &http.Client{
            Timeout: 30 * time.Second,
            Jar:     jar,
        },
        arl:     arl,
        baseURL: defaultBaseURL,
    }
}
```

Diagnostic: `gateway song.getFavoriteIds: Invalid CSRF token (status=200)` on the second authenticated call. The CSRF `checkForm` was being issued bound to a `sid` session cookie that the client immediately dropped. The gateway returns HTTP 200 with the error in the JSON body, so naive HTTP-status checks won't surface this.

`Set-Cookie` headers from a real `deezer.getUserData` response include `sid` (HttpOnly, SameSite=None, secure), `account_id`, `dzr_uniq_id`, `_abck`, and `bm_sz`. The `sid` is the load-bearing one for CSRF binding.

### flexString - before and after

Before (broken):

```go
type favoriteIDRecord struct {
    ID      string      `json:"SNG_ID"`
    TimeAdd json.Number `json:"DATE_ADD"`
}

type songListDataRecord struct {
    ID     string `json:"SNG_ID"`
    Title  string `json:"SNG_TITLE"`
    Artist string `json:"ART_NAME"`
    Album  string `json:"ALB_TITLE"`
}
```

After (fixed):

```go
type favoriteIDRecord struct {
    ID      flexString  `json:"SNG_ID"`
    TimeAdd json.Number `json:"DATE_ADD"`
}

type songListDataRecord struct {
    ID     flexString `json:"SNG_ID"`
    Title  string     `json:"SNG_TITLE"`
    Artist string     `json:"ART_NAME"`
    Album  string     `json:"ALB_TITLE"`
}
```

Use sites cast to `string(r.ID)` for map keys, body params, and downstream-typed struct literals.

Diagnostic: `json: cannot unmarshal number into Go struct field songListDataRecord.data.SNG_ID of type string` on the 9th chunk (records 1800-2000) of a live listing. Earlier chunks happened to all use the quoted form. The mismatch is non-deterministic per song; both forms appear in real responses.

### Mixed-type regression test

```go
func TestListFavoriteSongs_AcceptsNumericSNG_ID(t *testing.T) {
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        method := r.URL.Query().Get("method")
        w.WriteHeader(200)
        switch method {
        case "song.getFavoriteIds":
            _, _ = fmt.Fprint(w, `{"error":[],"results":{"data":[`+
                `{"SNG_ID":"1","DATE_ADD":1700000000},`+
                `{"SNG_ID":42,"DATE_ADD":1700000001}`+
                `],"total":2}}`)
        case "song.getListData":
            _, _ = fmt.Fprint(w, `{"error":[],"results":{"data":[`+
                `{"SNG_ID":1,"SNG_TITLE":"A","ART_NAME":"X","ALB_TITLE":"Alb"},`+
                `{"SNG_ID":"42","SNG_TITLE":"B","ART_NAME":"Y","ALB_TITLE":"Alb"}`+
                `]}}`)
        }
    }))
    // ... assertions verify both decode and produce string IDs "1" and "42"
}
```

Mixing both shapes in a single response is what catches the bug. Two separate single-shape tests would have continued to pass.

## Related

- Sibling pattern doc: [docs/solutions/design-patterns/gw-light-favorites-naming-asymmetry-2026-04-30.md](./gw-light-favorites-naming-asymmetry-2026-04-30.md) — gw-light's asymmetric method names across favorites entity types (songs vs albums/artists/playlists). Same module, different facet of the same "gw-light isn't uniform" theme.
- Concrete case for guidance #6: [docs/solutions/logic-errors/parseflexint-swallowed-error-album-dedupe-2026-05-07.md](../logic-errors/parseflexint-swallowed-error-album-dedupe-2026-05-07.md) — a real silent-zero-coercion bug class on `NB_FAN` / `NUMBER_TRACK` that motivated the flex-decoder semantics rule.
- Spec: `docs/superpowers/specs/2026-04-27-wipe-loved-tracks-design.md` (on `main`)
- Plan: `docs/superpowers/plans/2026-04-27-wipe-loved-tracks.md` (on `main`)
- Protocol research: `docs/superpowers/research/2026-04-27-deezer-gateway-protocol.md` (on `main`)
- Cookie jar fix commit: `41be10d`
- flexString fix commit: `1db64eb`
- Live integration test: `internal/gateway/integration_test.go` (gated on `DEEZER_INTEGRATION=1`)
