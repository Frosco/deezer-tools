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
