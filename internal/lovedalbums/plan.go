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
	// Parent is non-nil only for Case 2: the longer same-artist album whose
	// tracklist contains a track named like Album.Title.
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
// AlbumsToUnlove slice, deduped by ALB_ID. Order is deterministic: Case-1
// entries first (in group order), then Case-2 entries.
//
// Caller invariant: c2 was computed on the post-Case-1 set, so an album
// cannot be both a Case-1 loser and a Case-2 short (see the design spec).
// The dedup-by-ALB_ID step here is defence-in-depth, not a workaround.
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
