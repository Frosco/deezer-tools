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
