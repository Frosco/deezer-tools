package playlistlove

import (
	"context"
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

type fakeMeta struct {
	byID  map[string]gateway.AlbumMetadata
	calls int
}

func (f *fakeMeta) GetAlbumMetadata(ctx context.Context, id string) (gateway.AlbumMetadata, error) {
	f.calls++
	return f.byID[id], nil
}

func TestCollapseCase1WithinPlaylist_noConflict_noCalls(t *testing.T) {
	set := AggregatedSet{
		Albums: []Album{
			{ID: "1", Title: "A", Artist: "X"},
			{ID: "2", Title: "B", Artist: "X"},
		},
	}
	gw := &fakeMeta{byID: map[string]gateway.AlbumMetadata{}}
	got, err := CollapseCase1WithinPlaylist(context.Background(), gw, set, nil)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if gw.calls != 0 {
		t.Errorf("metadata calls = %d, want 0", gw.calls)
	}
	if got.Case1WithinPlaylistSuppressed != 0 {
		t.Errorf("suppressed = %d", got.Case1WithinPlaylistSuppressed)
	}
}

func TestCollapseCase1WithinPlaylist_collapsesGroup(t *testing.T) {
	set := AggregatedSet{
		Albums: []Album{
			{ID: "1", Title: "Random Access Memories", Artist: "Daft Punk"},
			{ID: "2", Title: "RANDOM ACCESS MEMORIES", Artist: "Daft Punk"},
			{ID: "3", Title: "Discovery", Artist: "Daft Punk"},
		},
	}
	gw := &fakeMeta{
		byID: map[string]gateway.AlbumMetadata{
			"1": {ID: "1", Title: "Random Access Memories", ArtistID: "8537", TrackCount: 13, FanCount: 999999},
			"2": {ID: "2", Title: "RANDOM ACCESS MEMORIES", ArtistID: "8537", TrackCount: 13, FanCount: 1},
		},
	}
	got, err := CollapseCase1WithinPlaylist(context.Background(), gw, set, nil)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if gw.calls != 2 {
		t.Errorf("metadata calls = %d, want 2", gw.calls)
	}
	if got.Case1WithinPlaylistSuppressed != 1 {
		t.Errorf("suppressed = %d, want 1", got.Case1WithinPlaylistSuppressed)
	}
	if len(got.Albums) != 2 {
		t.Fatalf("len(Albums) = %d, want 2", len(got.Albums))
	}
	for _, a := range got.Albums {
		if a.ID == "2" {
			t.Errorf("loser still in Albums: %v", a)
		}
	}
}

type errfulMeta struct {
	byID    map[string]gateway.AlbumMetadata
	errByID map[string]error
}

func (e *errfulMeta) GetAlbumMetadata(ctx context.Context, id string) (gateway.AlbumMetadata, error) {
	if err, ok := e.errByID[id]; ok {
		return gateway.AlbumMetadata{}, err
	}
	return e.byID[id], nil
}

func TestCollapseCase1WithinPlaylist_distinctArtistIDsSameName_notFalselyGrouped(t *testing.T) {
	// Two homonym artists with the same display name but different ArtistIDs.
	// Pre-filter groups them (both share "Headphones" + normalised title), but
	// the second-pass re-grouping by metadata.ArtistID must keep them apart so
	// neither's album is suppressed.
	set := AggregatedSet{
		Albums: []Album{
			{ID: "1", Title: "Self-Titled", Artist: "Headphones"},
			{ID: "2", Title: "Self-Titled", Artist: "Headphones"},
		},
	}
	gw := &fakeMeta{
		byID: map[string]gateway.AlbumMetadata{
			"1": {ID: "1", Title: "Self-Titled", ArtistID: "111", TrackCount: 10, FanCount: 100},
			"2": {ID: "2", Title: "Self-Titled", ArtistID: "222", TrackCount: 10, FanCount: 100},
		},
	}
	got, err := CollapseCase1WithinPlaylist(context.Background(), gw, set, nil)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got.Case1WithinPlaylistSuppressed != 0 {
		t.Errorf("suppressed = %d, want 0 (homonym artists must not be grouped)", got.Case1WithinPlaylistSuppressed)
	}
	if len(got.Albums) != 2 {
		t.Errorf("len(Albums) = %d, want 2 (both albums survive)", len(got.Albums))
	}
}

func TestCollapseCase1WithinPlaylist_metadataNotFound_dropsMember(t *testing.T) {
	set := AggregatedSet{
		Albums: []Album{
			{ID: "1", Title: "X", Artist: "A"},
			{ID: "2", Title: "X", Artist: "A"},
			{ID: "3", Title: "X", Artist: "A"},
		},
	}
	gw := &errfulMeta{
		byID: map[string]gateway.AlbumMetadata{
			"1": {ID: "1", Title: "X", ArtistID: "1", TrackCount: 13, FanCount: 100},
			"3": {ID: "3", Title: "X", ArtistID: "1", TrackCount: 13, FanCount: 50},
		},
		errByID: map[string]error{
			"2": &gateway.GatewayError{Kind: gateway.ErrNotFound, Message: "x"},
		},
	}
	got, err := CollapseCase1WithinPlaylist(context.Background(), gw, set, nil)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got.Case1WithinPlaylistSuppressed != 2 {
		t.Errorf("suppressed = %d, want 2 (1 loser + 1 not-found)", got.Case1WithinPlaylistSuppressed)
	}
}
