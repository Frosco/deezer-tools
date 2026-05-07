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
	"sort"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"

	"github.com/niref/deezer-tools/internal/gateway"
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
// fetchTracks is called once per long album of every phase-2-eligible artist
// (artists with both at least one short and at least one long album in
// post). On error, the album is dropped from the matching pool but detection
// continues with the remaining albums.
//
// Threshold semantics: TrackCount ≤ threshold is "short"; > threshold is
// "long". Threshold ≤ 0 falls back to the spec default of 3.
func DetectCase2(
	post []gateway.AlbumMetadata,
	fetchTracks func(albumID string) ([]gateway.AlbumTrack, error),
	threshold int,
) ([]Case2Group, error) {
	if threshold <= 0 {
		threshold = 3
	}

	byArtist := make(map[string][]gateway.AlbumMetadata)
	for _, a := range post {
		byArtist[a.ArtistID] = append(byArtist[a.ArtistID], a)
	}

	artistIDs := make([]string, 0, len(byArtist))
	for k := range byArtist {
		artistIDs = append(artistIDs, k)
	}
	sort.Strings(artistIDs)

	var groups []Case2Group
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

		type longWithTracks struct {
			meta   gateway.AlbumMetadata
			titles map[string]bool
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

		parentIdx := make(map[string]int)
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
					ArtistID:   aid,
					ArtistName: artistName,
					Parent:     picked.meta,
					Shorts:     []gateway.AlbumMetadata{s},
				})
			}
		}
	}

	for i := range groups {
		sort.Slice(groups[i].Shorts, func(a, b int) bool {
			return idLess(groups[i].Shorts[a].ID, groups[i].Shorts[b].ID)
		})
	}
	return groups, nil
}
