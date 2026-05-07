---
title: parseFlexInt swallowed errors silently zero-coerced sort keys in album dedupe
date: 2026-05-07
category: logic-errors
module: internal/gateway
problem_type: logic_error
component: tooling
severity: high
symptoms:
  - "GetAlbumMetadata's call site discarded parseFlexInt errors with `fans, _ := parseFlexInt(rec.FanCount)`"
  - "NB_FAN or NUMBER_TRACK arriving as JSON null or a non-integer string would coerce silently to 0"
  - "Zero-coerced TrackCount or FanCount would cause PickWinner to un-love the wrong canonical edition during loved-albums dedupe"
root_cause: logic_error
resolution_type: code_fix
related_components:
  - internal/lovedalbums
tags:
  - gw-light
  - flexstring
  - albums
  - dedupe
  - error-propagation
  - silent-data-corruption
  - parseflexint
---

# parseFlexInt swallowed errors silently zero-coerced sort keys in album dedupe

## Problem

`parseFlexInt` in `internal/gateway/albums.go` was wired into `GetAlbumMetadata`
with `_` for the error return on both `NB_FAN` and `NUMBER_TRACK`, silently
coercing any unparseable upstream value to `0`. Because `PickWinner` in
`internal/lovedalbums/plan.go` sorts dedupe candidates by `TrackCount`
(primary) and `FanCount` (tiebreaker), a silent zero on either field would
have caused the wrong canonical edition to be chosen as the winner — and the
orchestrator un-loves the *correct* edition instead.

## Symptoms

- The dedupe orchestrator un-loves a user's preferred (canonical) album
  edition while keeping a degraded duplicate (e.g. a single-track edition
  over the full LP, or a fan-count-zero clone over the real release).
- No error surfaces at the gateway layer, no warning is logged, and the run
  reports success.
- The corruption is silent and only visible by inspecting which IDs got
  removed against the backup-record file after the fact.
- Any future malformed gw-light response — e.g. `NB_FAN: "412k"`, an
  unexpected suffix, a stringified float, a sentinel — quietly becomes
  `FanCount: 0` and skews ordering for the entire group containing that
  record.
- Reproduction is non-deterministic from the user's side because gw-light's
  wire format is non-uniform within a single response (the `flexString`
  design pattern exists exactly because of this).

## What Didn't Work

The implementation plan literally prescribed:

```go
fans, _ := parseFlexInt(rec.FanCount)
tracks, _ := parseFlexInt(rec.TrackCount)
```

This shape was tempting for three reasons:

1. **It matched the plan verbatim.** Following the plan is the default safe
   move, and review pressure leans toward "do what was specified."
2. **It looked defensive.** `flexString` already absorbs heterogeneous JSON
   shapes (quoted, bare, null, absent), so swallowing a residual parse error
   on top read as "belt and braces" rather than as data loss.
3. **The zero default is plausibly meaningful.** `NB_FAN = 0` is a real value
   for unpopular albums; `NUMBER_TRACK = 0` is at least conceivable for
   empty/unreleased records. So a zero looked like a legitimate reading, not
   a parse failure.

The problem: `parseFlexInt` returns `(0, nil)` for the inputs where zero is
the correct default (`""`, `"null"`) and `(0, err)` for inputs where zero is
**not** correct (anything non-empty that fails `Atoi`). Discarding the error
collapses those two cases into one, and the downstream consumer (`PickWinner`)
cannot distinguish "this album genuinely has zero fans" from "we failed to
read this album's fan count." Sort-keying on a silently-zeroed value is
silent data corruption with destructive downstream effect (un-loving the
wrong edition is not recoverable from inside the run — only via the backup
file).

## Solution

Propagate the error and let `parseFlexInt`'s contract explicitly enumerate
which inputs map to the zero default vs. which inputs are real failures.

Before (in `internal/gateway/albums.go`):

```go
fans, _ := parseFlexInt(rec.FanCount)
tracks, _ := parseFlexInt(rec.TrackCount)
return AlbumMetadata{
    ID:         string(rec.ID),
    Title:      rec.Title,
    ArtistID:   string(rec.ArtistID),
    ArtistName: rec.ArtistName,
    FanCount:   fans,
    TrackCount: tracks,
}, nil

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

After:

```go
fans, err := parseFlexInt(rec.FanCount)
if err != nil {
    return AlbumMetadata{}, fmt.Errorf("decode %s NB_FAN %q: %w", getAlbumMetadataMethod, string(rec.FanCount), err)
}
tracks, err := parseFlexInt(rec.TrackCount)
if err != nil {
    return AlbumMetadata{}, fmt.Errorf("decode %s NUMBER_TRACK %q: %w", getAlbumMetadataMethod, string(rec.TrackCount), err)
}
return AlbumMetadata{
    ID:         string(rec.ID),
    Title:      rec.Title,
    ArtistID:   string(rec.ArtistID),
    ArtistName: rec.ArtistName,
    FanCount:   fans,
    TrackCount: tracks,
}, nil

// parseFlexInt parses a flexString that might arrive as a quoted string, a
// bare JSON number, JSON null, or be absent. Returns 0, nil for empty/null
// input (treated as "field missing or unset"). Returns 0, err if the content
// is non-empty but not a valid integer — propagating lets PickWinner skip and
// log the album rather than silently scoring it 0 on a tiebreaker dimension.
func parseFlexInt(s flexString) (int, error) {
    if s == "" || string(s) == "null" {
        return 0, nil
    }
    return strconv.Atoi(string(s))
}
```

Tests added in `internal/gateway/albums_test.go`:

- `TestGetAlbumMetadata_missingOrNullNumericFieldsDecodeAsZero` — payload with
  `NB_FAN` absent and `NUMBER_TRACK` as JSON null; asserts `FanCount=0`,
  `TrackCount=0`, `err == nil`.
- `TestGetAlbumMetadata_malformedNumericFieldPropagatesError` — payload with
  `NB_FAN: "412k"`; asserts the returned error wraps a parse failure and the
  field name `NB_FAN` appears in the message.

Wet-run verified by Nils against his real Deezer account on 2026-05-07
(commit `dfe737e`).

## Why This Works

`flexString` is intentionally permissive — it accepts quoted strings, bare
numbers, JSON null, and absent fields, because gw-light returns the same
field in different shapes within a single response (see
`docs/solutions/design-patterns/gw-light-go-adapter-quirks-2026-04-28.md`).
That permissiveness is correct at the *decoding* layer: it gets bytes off
the wire without crashing.

But "we got bytes off the wire" is not the same as "we got a meaningful
integer." The `parseFlexInt` helper is the seam where bytes become semantic
values, and it's the only place that can distinguish:

- **Field missing or explicitly null** — the gateway is telling us "no value
  here." Mapping this to `0` with no error is correct: the caller's zero
  default is meaningful (`NB_FAN = 0` plausibly means "unmeasured" or "no
  fans tracked").
- **Field present but unparseable** — the gateway returned bytes we didn't
  expect (`"412k"`, `"N/A"`, a stringified float, a future format change).
  `0` here is **not** a meaningful default; it's a fabricated reading.

The fix encodes that distinction in the contract and forces the call site to
confront the second case. `PickWinner` then either gets a real integer or
the whole album gets surfaced as an error and skipped — which is recoverable.
A silently-zeroed sort key is not.

The destructive-action amplifier is what makes this severity high rather
than medium: the value flows into a sort that drives a *destructive* gateway
call (un-love). Silent corruption of a sort key in a read-only context would
be a bug; here it's data loss against the user's library.

## Prevention

- **Never `_ :=` a parser error when the parsed value drives downstream
  ordering or destructive action.** Treat `_` on a parse return as a code
  smell during review and ask: "is the zero/empty default sentinel meaningful
  in the caller's context, or am I collapsing 'missing' and 'malformed' into
  the same value?"
- **When introducing a wire-format helper that handles nulls/absent inputs,
  explicitly enumerate inputs that map to defaults vs inputs that should
  error, and document the contract in the godoc.** The fixed `parseFlexInt`
  does this — `""` and `"null"` are defaults, anything else either parses or
  errors. No third bucket.
- **Test all three branches:** valid input, valid empty/null input, malformed
  input. The malformed-input test is the one that catches silent-zero
  regressions; without it the change is invisible to CI.
- **For wire-format shims like `flexString`, write synthetic gateway
  responses that exercise each shape independently** — `null`, `""`, missing
  field, malformed string — rather than asserting only on the happy path.
- **Consider relocating `parseFlexInt` next to `flexString` in
  `internal/gateway/tracks.go`** if more callers emerge; the same hazard
  applies anywhere a `flexString` is converted to a numeric type, and
  centralizing the helper means the contract documentation lives next to the
  type that motivates it.

## Related Issues

- `docs/solutions/design-patterns/gw-light-go-adapter-quirks-2026-04-28.md`
  — sibling gw-light wire-format doc; `parseFlexInt` is the integer-typed
  cousin of `flexString`. That doc covers why `flexString` exists; this one
  extends the guidance to integer conversion and the empty-vs-malformed
  distinction.
- `docs/solutions/integration-issues/quota-error-misclassification-akamai-ip-block-2026-04-29.md`
  — same silent-failure family at a different layer (error classifier vs.
  parser): a permissive default silently degrading bulk operation behavior.
  Different mechanism, identical lesson — surface unknowns, don't silently
  coerce or skip.
- `docs/solutions/design-patterns/gw-light-favorites-naming-asymmetry-2026-04-30.md`
  — adjacent gw-light wire-format learning; produces the album records that
  `PickWinner` consumes.
