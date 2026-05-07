package playlistlove

import (
	"context"
	"errors"
	"time"

	"github.com/niref/deezer-tools/internal/gateway"
	"github.com/niref/deezer-tools/internal/lovedalbums"
	"github.com/niref/deezer-tools/internal/throttle"
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
	Albums                        []Album
	Artists                       []Artist
	UnparseableSongs              int
	VariousArtistsSkipped         int
	Case1WithinPlaylistSuppressed int
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

// MetadataFetcher is the slice of internal/gateway.Client used by
// CollapseCase1WithinPlaylist. Defined here to keep the playlistlove
// dependency narrow.
type MetadataFetcher interface {
	GetAlbumMetadata(ctx context.Context, albumID string) (gateway.AlbumMetadata, error)
}

// CollapseCase1WithinPlaylist applies the lovedalbums Case-1 dedup rule to
// the set's albums. For each conflict group (≥2 candidates sharing
// (artist display name, normalised title)), it fetches metadata for every
// member, then re-groups by (ArtistID, normalised title) — the same key
// lovedalbums.DetectCase1 uses on the loved set — and runs PickWinner per
// re-grouped subgroup. Losers are dropped from set.Albums.
//
// The two-pass keying matters: Album.Artist is the playlist's display name,
// which can collide across distinct ArtistIDs (homonym artists) or vary
// across rows for the same ArtistID. The first pass over display names is a
// cheap pre-filter to avoid fetching metadata for every album; the second
// pass uses the authoritative ArtistID so we never falsely group two
// distinct artists.
//
// API cost is bounded by pre-filter membership, NOT by playlist size.
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
	type nameKey struct{ artistName, normTitle string }
	prefilter := make(map[nameKey][]int)
	for i, a := range set.Albums {
		k := nameKey{a.Artist, lovedalbums.Normalise(a.Title)}
		prefilter[k] = append(prefilter[k], i)
	}
	drop := make(map[int]bool)
	type idKey struct{ artistID, normTitle string }
	for _, indices := range prefilter {
		if len(indices) < 2 {
			continue
		}
		// Fetch metadata for every pre-filter candidate, then re-group by
		// the authoritative ArtistID before deciding losers.
		type fetched struct {
			meta gateway.AlbumMetadata
			pos  int
		}
		var fetchedAll []fetched
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
				fetchedAll = append(fetchedAll, fetched{meta: meta, pos: i})
				continue
			}
			if errors.Is(callErr, context.Canceled) || errors.Is(callErr, context.DeadlineExceeded) {
				return set, callErr
			}
			var ge *gateway.GatewayError
			if errors.As(callErr, &ge) && ge.Kind == gateway.ErrAuthFailed {
				return set, callErr
			}
			drop[i] = true
		}

		subgroups := make(map[idKey][]fetched)
		for _, f := range fetchedAll {
			k := idKey{f.meta.ArtistID, lovedalbums.Normalise(f.meta.Title)}
			subgroups[k] = append(subgroups[k], f)
		}
		for _, sub := range subgroups {
			if len(sub) < 2 {
				continue
			}
			members := make([]gateway.AlbumMetadata, len(sub))
			for j, f := range sub {
				members[j] = f.meta
			}
			ranked := lovedalbums.PickWinner(members)
			winnerID := ranked[0].ID
			for _, f := range sub {
				if f.meta.ID != winnerID {
					drop[f.pos] = true
				}
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
