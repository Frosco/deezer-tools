package lovedalbums

import (
	"testing"

	"github.com/niref/deezer-tools/internal/gateway"
)

func TestPickWinner_mostTracksFirst(t *testing.T) {
	group := []gateway.AlbumMetadata{
		{ID: "1", TrackCount: 1, FanCount: 999999},
		{ID: "2", TrackCount: 12, FanCount: 100},
	}
	got := PickWinner(group)
	if got[0].ID != "2" {
		t.Errorf("winner = %s, want 2 (more tracks beats more fans)", got[0].ID)
	}
}

func TestPickWinner_fansBreakTrackTie(t *testing.T) {
	group := []gateway.AlbumMetadata{
		{ID: "1", TrackCount: 13, FanCount: 100},
		{ID: "2", TrackCount: 13, FanCount: 999999},
	}
	got := PickWinner(group)
	if got[0].ID != "2" {
		t.Errorf("winner = %s, want 2 (fans break track tie)", got[0].ID)
	}
}

func TestPickWinner_lowestIDBreaksFinalTie(t *testing.T) {
	group := []gateway.AlbumMetadata{
		{ID: "200", TrackCount: 13, FanCount: 100},
		{ID: "100", TrackCount: 13, FanCount: 100},
		{ID: "300", TrackCount: 13, FanCount: 100},
	}
	got := PickWinner(group)
	if got[0].ID != "100" {
		t.Errorf("winner = %s, want 100 (lowest ID)", got[0].ID)
	}
}

func TestPickWinner_idCompareIsNumeric(t *testing.T) {
	// Lexicographic comparison would put "9" after "100"; numeric-aware
	// comparison must put "9" first.
	group := []gateway.AlbumMetadata{
		{ID: "100", TrackCount: 1, FanCount: 1},
		{ID: "9", TrackCount: 1, FanCount: 1},
	}
	got := PickWinner(group)
	if got[0].ID != "9" {
		t.Errorf("winner = %s, want 9", got[0].ID)
	}
}

func TestPickWinner_returnsAllInOrder(t *testing.T) {
	group := []gateway.AlbumMetadata{
		{ID: "B", TrackCount: 1},
		{ID: "A", TrackCount: 5},
		{ID: "C", TrackCount: 3},
	}
	got := PickWinner(group)
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	if got[0].ID != "A" || got[1].ID != "C" || got[2].ID != "B" {
		t.Errorf("order = [%s %s %s], want [A C B]", got[0].ID, got[1].ID, got[2].ID)
	}
}
