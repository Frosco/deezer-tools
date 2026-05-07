---
title: Error classification belongs at the package boundary, not the CLI
date: 2026-05-07
category: architecture-patterns
module: internal/playlistlove
problem_type: architecture_pattern
component: tooling
severity: medium
applies_when:
  - "A package function calls gateway methods that can return auth-failure errors"
  - "The CLI layer is tempted to errors.As / re-wrap a package-level error to add a user-facing hint"
  - "Adding a new entry point (e.g. ApplyFromRecord beside Run) that shares an applyPlan helper"
  - "Adding a new domain package that depends on internal/gateway"
related_components:
  - tooling
  - authentication
tags:
  - error-wrapping
  - auth-failure
  - cli-package-boundary
  - gateway
  - error-classification
  - double-wrapping
---

# Error classification belongs at the package boundary, not the CLI

## Context

When `playlists apply-record` was added to the deezer-tools Go CLI, the design
spec called for a "refresh your arl in `~/.config/deezer-tools/config.toml`"
hint on every `gateway.ErrAuthFailed` — both during the loved-set re-fetch
and during the apply phase (session history: design spec
`docs/superpowers/specs/2026-05-07-playlists-apply-record-design.md`
collapsed both paths into one row of the error-handling table).

The package layer (`internal/playlistlove`) already classified auth failures
inside `applyPlan`: it wrapped `*gateway.GatewayError{Kind: ErrAuthFailed}`
with the hint at the package boundary. But `ApplyFromRecord` also called
two gateway methods (`ListFavoriteAlbumIDs` / `ListFavoriteArtistIDs`) whose
errors it forwarded with only a plain `fmt.Errorf("list loved albums: %w", err)`
— no hint.

The CLI's `RunE` tried to plug that gap universally by pattern-matching
`*gateway.GatewayError` via `errors.As` and adding the hint there. Because
`applyPlan` had already wrapped with the same hint text, an auth failure on
the apply-loop path passed through both wrappers and the user saw "refresh
your arl" twice. The CLI-level classifier was also redundant and brittle —
trying to own classification that properly belongs to the domain package.

## Guidance

**Classify errors at the package boundary, not at the CLI boundary. Each
package is responsible for making its own errors actionable. The CLI is a
thin pass-through.**

The rule has two parts:

1. Every call path inside a domain package that can produce an actionable
   error (one where the user needs a concrete hint to recover) must classify
   and annotate that error before returning it. Do not leave any call path
   in the package returning a raw `*gateway.GatewayError` that the CLI is
   expected to annotate.

2. The CLI's `RunE` must not classify or re-wrap package errors. Its job is
   to translate the command-line invocation into a function call and return
   whatever the package returns. If it also classifies, you have two
   classifiers in series and any path the package already covers will
   double-wrap.

When several call sites inside one package need the same classification
logic, extract a small unexported helper rather than repeating the
`errors.As` block inline.

**Before — two classifiers in series:**

```go
// internal/playlistlove/apply.go — package boundary leaves fetch path unclassified
lovedAlbums, err := gw.ListFavoriteAlbumIDs(ctx)
if err != nil {
    return nil, fmt.Errorf("list loved albums: %w", err)   // raw, no hint
}

// cmd/deezer-tools/playlistlove_apply_cmd.go — CLI tries to cover the gap
var gerr *gateway.GatewayError
if errors.As(err, &gerr) && gerr.Kind == gateway.ErrAuthFailed {
    return fmt.Errorf("auth failed (refresh your arl in ~/.config/deezer-tools/config.toml): %w", err)
}
return err
// Result on apply-phase auth failure: applyPlan's wrap + CLI's wrap = two
// "refresh your arl" hints in the user's terminal.
```

**After — classify once, at the package boundary:**

```go
// internal/playlistlove/apply.go — package classifies all its own error paths

lovedAlbums, err := gw.ListFavoriteAlbumIDs(ctx)
if err != nil {
    return nil, wrapLovedFetchErr("list loved albums", err)
}
lovedArtists, err := gw.ListFavoriteArtistIDs(ctx)
if err != nil {
    return nil, wrapLovedFetchErr("list loved artists", err)
}

// wrapLovedFetchErr — unexported helper, classification lives here.
func wrapLovedFetchErr(step string, err error) error {
    var gerr *gateway.GatewayError
    if errors.As(err, &gerr) && gerr.Kind == gateway.ErrAuthFailed {
        return fmt.Errorf("auth failed during %s (refresh your arl in ~/.config/deezer-tools/config.toml): %w", step, err)
    }
    return fmt.Errorf("%s: %w", step, err)
}

// cmd/deezer-tools/playlistlove_apply_cmd.go — CLI is a thin pass-through.
return err
```

## Why This Matters

**Correctness.** Two classifiers in series produce duplicate hint text. On
an auth failure in the apply loop the user reads:

```
auth failed during album apply (refresh your arl in ~/.config/deezer-tools/config.toml): auth failed (refresh your arl in ~/.config/deezer-tools/config.toml): USER_AUTH_REQUIRED
```

That's noise that undermines trust in the tool's error messages.

**Incomplete coverage is invisible.** The opposite failure mode is quieter
but just as bad: if you delete the CLI classifier without plugging the gap
at the package level, `list loved albums: USER_AUTH_REQUIRED` reaches the
user with no hint. The user sees a raw gateway error code and doesn't know
what to do. Both failure modes stem from the same root cause: classification
spread across two layers.

**Coupling by message text.** Any attempt to "de-duplicate" at the CLI
level — detecting that the message already contains the hint string and
skipping the second wrap — ties the two layers together through message
text, which is fragile and hard to test. Keeping classification in one
place eliminates that coupling.

**Testability.** When classification lives in the package, it can be tested
with `fakeGateway` stubs at the unit-test level, with precise assertions on
message content. When it lives in the CLI, tests require wiring up a full
Cobra command, which is heavier and tests more than one thing at a time.

## When to Apply

- Any time a domain package has more than one call path that can surface a
  user-actionable error (e.g., a pre-flight fetch and an operational loop
  in the same function).
- Any time you find yourself writing `errors.As` in a CLI `RunE` to detect
  and re-wrap a package-level error. That is a signal the package has an
  unclassified path.
- Any time you're adding a new call site inside a package and the error
  from that call site needs the same annotation as an existing call site.
  Extract a shared helper; don't copy the `errors.As` block.
- Any time a package grows from one entry point to two (e.g. `Run` vs.
  `ApplyFromRecord`): audit every call path in both entry points for
  unclassified errors before wiring up the CLI.

## Examples

**Symptom that triggers this investigation:**

User sees the same actionable hint repeated twice in an error message
(see "Why This Matters" above for the literal output).

**Tests that pin the contract — positive AND negative:**

```go
// Positive: auth failure carries the hint and the step name.
func TestApplyFromRecord_authFailureDuringListLovedAlbums(t *testing.T) {
    gw := &fakeGateway{
        listLovedAlbumsErr: &gateway.GatewayError{Kind: gateway.ErrAuthFailed, ...},
    }
    _, err := ApplyFromRecord(ctx, gw, opts)
    if !strings.Contains(err.Error(), "refresh your arl") {
        t.Errorf("err = %v, want refresh-arl hint", err)
    }
    if !strings.Contains(err.Error(), "list loved albums") {
        t.Errorf("err = %v, want step name", err)
    }
}

// Negative: non-auth failure must NOT carry the auth hint.
func TestApplyFromRecord_nonAuthFailureDuringListLoved(t *testing.T) {
    gw := &fakeGateway{
        listLovedAlbumsErr: &gateway.GatewayError{Kind: gateway.ErrServerError, ...},
    }
    _, err := ApplyFromRecord(ctx, gw, opts)
    if strings.Contains(err.Error(), "refresh your arl") {
        t.Errorf("non-auth failure should NOT carry refresh-arl hint")
    }
}
```

The negative case is as important as the positive case: it prevents a
helper that accidentally plasters the auth hint on every error regardless
of kind.

## Related

- `docs/solutions/integration-issues/quota-error-misclassification-akamai-ip-block-2026-04-29.md`
  — Sibling pattern at a different layer: classification belongs at the
  layer with context. That doc owns the gateway-level case (mapping
  `QUOTA_ERROR` to `ErrRateLimited` so retry machinery engages); this doc
  owns the package→CLI case.
- `docs/solutions/design-patterns/gw-light-go-adapter-quirks-2026-04-28.md`
  — Guidance #6 (flexString/parseFlexInt error propagation) is a narrower
  instance of the same "classify at the deepest layer" principle, applied
  at a decode boundary.
- `docs/superpowers/followups/2026-05-07-playlists-apply-record.md` — Item
  #1 documents the symmetric `Run` loved-set fetch path that still lacks
  the hint; closing it is the direct follow-up for this learning.
- `docs/superpowers/followups/2026-05-07-loved-albums-dedupe.md` — Item #4
  proposes lifting the duplicated `errors.As(&ge) && ge.Kind == ErrAuthFailed`
  pattern (six call sites across `internal/lovedalbums` and
  `internal/playlistlove`) into a shared helper. This learning is the
  rationale for that consolidation: once classification is confirmed to
  live at the package layer, the duplicated inline classify-and-wrap
  pattern is the next thing to consolidate.
- Commits: `8facfce` removed the CLI's double-wrap; `f594f9e` introduced
  `wrapLovedFetchErr` at the package boundary with positive + negative
  tests.
