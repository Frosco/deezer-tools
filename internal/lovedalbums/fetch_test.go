package lovedalbums

import (
	"context"
	"errors"
	"testing"

	"github.com/niref/deezer-tools/internal/gateway"
	"github.com/niref/deezer-tools/internal/throttle"
)

func init() {
	throttle.Pace = 0
	throttle.Jitter = 0
}

type fakeGW struct {
	metaByID    map[string]gateway.AlbumMetadata
	metaErrByID map[string]error
	tracksByID  map[string][]gateway.AlbumTrack
	tracksErr   map[string]error
	metaCalls   int
	tracksCalls int
}

func (f *fakeGW) GetAlbumMetadata(ctx context.Context, id string) (gateway.AlbumMetadata, error) {
	f.metaCalls++
	if err, ok := f.metaErrByID[id]; ok {
		return gateway.AlbumMetadata{}, err
	}
	return f.metaByID[id], nil
}

func (f *fakeGW) ListAlbumTracks(ctx context.Context, id string) ([]gateway.AlbumTrack, error) {
	f.tracksCalls++
	if err, ok := f.tracksErr[id]; ok {
		return nil, err
	}
	return f.tracksByID[id], nil
}

func TestPhase1Fetch_happyPath(t *testing.T) {
	gw := &fakeGW{
		metaByID: map[string]gateway.AlbumMetadata{
			"1": {ID: "1", Title: "A", ArtistID: "x", TrackCount: 1},
			"2": {ID: "2", Title: "B", ArtistID: "x", TrackCount: 2},
		},
	}
	got, err := Phase1Fetch(context.Background(), gw, []string{"1", "2"}, nil, nil)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(got) != 2 || gw.metaCalls != 2 {
		t.Errorf("got=%+v calls=%d", got, gw.metaCalls)
	}
}

func TestPhase1Fetch_dropsNotFound(t *testing.T) {
	gw := &fakeGW{
		metaByID: map[string]gateway.AlbumMetadata{
			"1": {ID: "1"},
		},
		metaErrByID: map[string]error{
			"missing": &gateway.GatewayError{Kind: gateway.ErrNotFound, Message: "x"},
		},
	}
	got, err := Phase1Fetch(context.Background(), gw, []string{"1", "missing"}, nil, nil)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(got) != 1 || got[0].ID != "1" {
		t.Errorf("got = %+v", got)
	}
}

func TestPhase1Fetch_authFailureAborts(t *testing.T) {
	gw := &fakeGW{
		metaErrByID: map[string]error{
			"1": &gateway.GatewayError{Kind: gateway.ErrAuthFailed, Message: "x"},
		},
	}
	_, err := Phase1Fetch(context.Background(), gw, []string{"1", "2"}, nil, nil)
	if err == nil {
		t.Fatal("err = nil, want auth")
	}
	var ge *gateway.GatewayError
	if !errors.As(err, &ge) || ge.Kind != gateway.ErrAuthFailed {
		t.Errorf("err = %v, want ErrAuthFailed", err)
	}
}

func TestPhase2Fetch_onlyEligibleArtists(t *testing.T) {
	post := []gateway.AlbumMetadata{
		{ID: "1s", ArtistID: "1", TrackCount: 1},
		{ID: "1l", ArtistID: "1", TrackCount: 12},
		{ID: "2s", ArtistID: "2", TrackCount: 1},
		{ID: "3l", ArtistID: "3", TrackCount: 12},
	}
	gw := &fakeGW{
		tracksByID: map[string][]gateway.AlbumTrack{
			"1l": {{Title: "Foo"}},
		},
	}
	tracks, attempts, err := Phase2Fetch(context.Background(), gw, post, 3, nil, nil)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if gw.tracksCalls != 1 || attempts != 1 {
		t.Errorf("tracksCalls = %d, attempts = %d, want 1 / 1 (only 1l)", gw.tracksCalls, attempts)
	}
	if _, ok := tracks("1l"); !ok {
		t.Errorf("expected tracks for 1l")
	}
	if _, ok := tracks("3l"); ok {
		t.Errorf("did not expect tracks for 3l")
	}
}
