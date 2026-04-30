package playlistlove

import (
	"github.com/niref/deezer-tools/internal/gateway"
)

// Album is a deduped (ALB_ID, ALB_TITLE, primary ART_NAME) record for the
// diff plan. Artist is a deduped (ART_ID, ART_NAME) record.
type Album struct {
	ID     string
	Title  string
	Artist string
}

type Artist struct {
	ID   string
	Name string
}

// AggregatedSet is the dedupe output: unique albums and artists across all
// songs, with per-cohort counts surfaced for the run summary.
type AggregatedSet struct {
	Albums                []Album
	Artists               []Artist
	UnparseableSongs      int
	VariousArtistsSkipped int
}

// DefaultVariousArtistsID is the ART_ID that gw-light emits for compilation
// "Various Artists" entries. Verified against deemix/deezer-py in the
// research doc dated 2026-04-30. Configurable on Options for override.
const DefaultVariousArtistsID = "5080"

// Aggregate dedupes the songs by ALB_ID and ART_ID, dropping the
// Various-Artists pseudo-ART_ID at the artist level. Songs with empty/zero
// ALB_ID or ART_ID are counted under UnparseableSongs and don't appear in
// the output sets.
func Aggregate(songs []gateway.PlaylistSong, variousArtistsID string) AggregatedSet {
	if variousArtistsID == "" {
		variousArtistsID = DefaultVariousArtistsID
	}
	albums := make(map[string]Album)
	artists := make(map[string]Artist)
	var set AggregatedSet
	for _, s := range songs {
		albID := s.AlbumID
		artID := s.ArtistID
		if albID == "" || albID == "0" || artID == "" || artID == "0" {
			set.UnparseableSongs++
			continue
		}
		if _, ok := albums[albID]; !ok {
			albums[albID] = Album{ID: albID, Title: s.AlbumTitle, Artist: s.ArtistName}
		}
		if artID == variousArtistsID {
			set.VariousArtistsSkipped++
			continue
		}
		if _, ok := artists[artID]; !ok {
			artists[artID] = Artist{ID: artID, Name: s.ArtistName}
		}
	}
	set.Albums = make([]Album, 0, len(albums))
	for _, a := range albums {
		set.Albums = append(set.Albums, a)
	}
	set.Artists = make([]Artist, 0, len(artists))
	for _, a := range artists {
		set.Artists = append(set.Artists, a)
	}
	return set
}

// DiffInputs carries the user's current loved-album and loved-artist IDs.
type DiffInputs struct {
	LovedAlbumIDs  []string
	LovedArtistIDs []string
}

// DiffPlan is the result of subtracting the loved sets from AggregatedSet.
type DiffPlan struct {
	AlbumsToAdd         []Album
	ArtistsToAdd        []Artist
	AlbumsAlreadyLoved  int
	ArtistsAlreadyLoved int
}

// Diff returns AlbumsToAdd / ArtistsToAdd: items in set whose IDs are not
// already in the corresponding loved-ID slice.
func Diff(set AggregatedSet, loved DiffInputs) DiffPlan {
	lovedAlb := make(map[string]bool, len(loved.LovedAlbumIDs))
	for _, id := range loved.LovedAlbumIDs {
		lovedAlb[id] = true
	}
	lovedArt := make(map[string]bool, len(loved.LovedArtistIDs))
	for _, id := range loved.LovedArtistIDs {
		lovedArt[id] = true
	}
	var plan DiffPlan
	for _, a := range set.Albums {
		if lovedAlb[a.ID] {
			plan.AlbumsAlreadyLoved++
			continue
		}
		plan.AlbumsToAdd = append(plan.AlbumsToAdd, a)
	}
	for _, a := range set.Artists {
		if lovedArt[a.ID] {
			plan.ArtistsAlreadyLoved++
			continue
		}
		plan.ArtistsToAdd = append(plan.ArtistsToAdd, a)
	}
	return plan
}
