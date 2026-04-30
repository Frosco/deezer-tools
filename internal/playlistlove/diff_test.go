package playlistlove

import (
	"reflect"
	"sort"
	"testing"

	"github.com/niref/deezer-tools/internal/gateway"
)

func TestAggregate_dedupesByID(t *testing.T) {
	songs := []gateway.PlaylistSong{
		{SongID: "1", AlbumID: "100", AlbumTitle: "A1", ArtistID: "10", ArtistName: "X"},
		{SongID: "2", AlbumID: "100", AlbumTitle: "A1", ArtistID: "10", ArtistName: "X"},
		{SongID: "3", AlbumID: "101", AlbumTitle: "A2", ArtistID: "11", ArtistName: "Y"},
	}
	got := Aggregate(songs, "5080")
	if len(got.Albums) != 2 {
		t.Errorf("albums = %d, want 2 (got %+v)", len(got.Albums), got.Albums)
	}
	if len(got.Artists) != 2 {
		t.Errorf("artists = %d, want 2", len(got.Artists))
	}
	if got.UnparseableSongs != 0 {
		t.Errorf("unparseable = %d, want 0", got.UnparseableSongs)
	}
	if got.VariousArtistsSkipped != 0 {
		t.Errorf("VA skipped = %d, want 0", got.VariousArtistsSkipped)
	}
}

func TestAggregate_dropsVariousArtists(t *testing.T) {
	songs := []gateway.PlaylistSong{
		{SongID: "1", AlbumID: "100", AlbumTitle: "Comp", ArtistID: "5080", ArtistName: "Various Artists"},
		{SongID: "2", AlbumID: "100", AlbumTitle: "Comp", ArtistID: "5080", ArtistName: "Various Artists"},
		{SongID: "3", AlbumID: "101", AlbumTitle: "Real", ArtistID: "11", ArtistName: "Y"},
	}
	got := Aggregate(songs, "5080")
	if len(got.Albums) != 2 {
		t.Errorf("albums = %d, want 2 (compilations are still loved)", len(got.Albums))
	}
	if len(got.Artists) != 1 || got.Artists[0].ID != "11" {
		t.Errorf("artists = %+v, want [{11, Y}]", got.Artists)
	}
	if got.VariousArtistsSkipped != 2 {
		t.Errorf("VA skipped = %d, want 2 (per-song count)", got.VariousArtistsSkipped)
	}
}

func TestAggregate_countsUnparseableAndDoesNotEmit(t *testing.T) {
	songs := []gateway.PlaylistSong{
		{SongID: "1", AlbumID: "", ArtistID: "10", ArtistName: "X"},
		{SongID: "2", AlbumID: "100", ArtistID: "", ArtistName: ""},
		{SongID: "3", AlbumID: "0", ArtistID: "0", ArtistName: ""},
		{SongID: "4", AlbumID: "100", AlbumTitle: "A", ArtistID: "10", ArtistName: "X"},
	}
	got := Aggregate(songs, "5080")
	if len(got.Albums) != 1 || got.Albums[0].ID != "100" {
		t.Errorf("albums = %+v", got.Albums)
	}
	if len(got.Artists) != 1 || got.Artists[0].ID != "10" {
		t.Errorf("artists = %+v", got.Artists)
	}
	if got.UnparseableSongs != 3 {
		t.Errorf("unparseable = %d, want 3", got.UnparseableSongs)
	}
}

func TestDiff_subtractsLovedSets(t *testing.T) {
	set := AggregatedSet{
		Albums:  []Album{{ID: "100", Title: "A1"}, {ID: "101", Title: "A2"}, {ID: "102", Title: "A3"}},
		Artists: []Artist{{ID: "10", Name: "X"}, {ID: "11", Name: "Y"}},
	}
	loved := DiffInputs{
		LovedAlbumIDs:  []string{"101"},
		LovedArtistIDs: []string{"10"},
	}
	got := Diff(set, loved)
	sortAlbums := func(a []Album) { sort.Slice(a, func(i, j int) bool { return a[i].ID < a[j].ID }) }
	sortArtists := func(a []Artist) { sort.Slice(a, func(i, j int) bool { return a[i].ID < a[j].ID }) }
	sortAlbums(got.AlbumsToAdd)
	sortArtists(got.ArtistsToAdd)
	wantAlbums := []Album{{ID: "100", Title: "A1"}, {ID: "102", Title: "A3"}}
	wantArtists := []Artist{{ID: "11", Name: "Y"}}
	if !reflect.DeepEqual(got.AlbumsToAdd, wantAlbums) {
		t.Errorf("albumsToAdd = %+v, want %+v", got.AlbumsToAdd, wantAlbums)
	}
	if !reflect.DeepEqual(got.ArtistsToAdd, wantArtists) {
		t.Errorf("artistsToAdd = %+v, want %+v", got.ArtistsToAdd, wantArtists)
	}
	if got.AlbumsAlreadyLoved != 1 {
		t.Errorf("AlbumsAlreadyLoved = %d, want 1", got.AlbumsAlreadyLoved)
	}
	if got.ArtistsAlreadyLoved != 1 {
		t.Errorf("ArtistsAlreadyLoved = %d, want 1", got.ArtistsAlreadyLoved)
	}
}

func TestDiff_emptyLovedSetsMeansAllToAdd(t *testing.T) {
	set := AggregatedSet{
		Albums:  []Album{{ID: "100"}, {ID: "101"}},
		Artists: []Artist{{ID: "10"}},
	}
	got := Diff(set, DiffInputs{})
	if len(got.AlbumsToAdd) != 2 {
		t.Errorf("albumsToAdd = %d, want 2", len(got.AlbumsToAdd))
	}
	if len(got.ArtistsToAdd) != 1 {
		t.Errorf("artistsToAdd = %d, want 1", len(got.ArtistsToAdd))
	}
}
