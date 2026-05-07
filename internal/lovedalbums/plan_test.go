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

func TestBuildPlan_combinesCase1AndCase2_disjoint(t *testing.T) {
	c1 := []Case1Group{
		{
			ArtistID: "1", ArtistName: "A", NormalisedKey: "x",
			Members: []gateway.AlbumMetadata{
				{ID: "winner1", Title: "X"}, {ID: "loser1", Title: "X"},
			},
		},
	}
	c2 := []Case2Group{
		{
			ArtistID: "1", ArtistName: "A",
			Parent: gateway.AlbumMetadata{ID: "parent2", Title: "LP"},
			Shorts: []gateway.AlbumMetadata{{ID: "short2", Title: "Foo"}},
		},
	}
	plan := BuildPlan(c1, c2)
	if len(plan.AlbumsToUnlove) != 2 {
		t.Fatalf("AlbumsToUnlove = %d, want 2", len(plan.AlbumsToUnlove))
	}
	got := plan.AlbumsToUnlove
	if got[0].Album.ID != "loser1" || got[0].Case != Case1 {
		t.Errorf("got[0] = %+v", got[0])
	}
	if got[1].Album.ID != "short2" || got[1].Case != Case2 {
		t.Errorf("got[1] = %+v", got[1])
	}
	if got[1].Parent == nil || got[1].Parent.ID != "parent2" {
		t.Errorf("got[1].Parent = %+v, want parent2", got[1].Parent)
	}
}

func TestBuildPlan_winnersAndParentsNotUnloved(t *testing.T) {
	c1 := []Case1Group{
		{
			Members: []gateway.AlbumMetadata{
				{ID: "winner"}, {ID: "loser"},
			},
		},
	}
	c2 := []Case2Group{
		{
			Parent: gateway.AlbumMetadata{ID: "parent"},
			Shorts: []gateway.AlbumMetadata{{ID: "short"}},
		},
	}
	plan := BuildPlan(c1, c2)
	for _, e := range plan.AlbumsToUnlove {
		if e.Album.ID == "winner" || e.Album.ID == "parent" {
			t.Errorf("unexpected unlove: %s", e.Album.ID)
		}
	}
}

func TestBuildPlan_dedupesByALBID(t *testing.T) {
	c1 := []Case1Group{
		{Members: []gateway.AlbumMetadata{{ID: "w1"}, {ID: "dup"}}},
		{Members: []gateway.AlbumMetadata{{ID: "w2"}, {ID: "dup"}}},
	}
	plan := BuildPlan(c1, nil)
	count := 0
	for _, e := range plan.AlbumsToUnlove {
		if e.Album.ID == "dup" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("dup count = %d, want 1", count)
	}
}

func TestBuildPlan_emptyInputs_emptyPlan(t *testing.T) {
	plan := BuildPlan(nil, nil)
	if len(plan.AlbumsToUnlove) != 0 || len(plan.Case1Groups) != 0 || len(plan.Case2Groups) != 0 {
		t.Errorf("plan = %+v", plan)
	}
}
