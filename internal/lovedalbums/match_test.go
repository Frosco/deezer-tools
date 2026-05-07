package lovedalbums

import (
	"sort"
	"testing"

	"github.com/niref/deezer-tools/internal/gateway"
)

func TestNormalise(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"identity", "random access memories", "random access memories"},
		{"casefold", "Random Access Memories", "random access memories"},
		{"shouty", "RANDOM ACCESS MEMORIES", "random access memories"},
		{"accent_fold", "Café", "cafe"},
		{"accent_fold_compound", "École", "ecole"},
		{"apostrophe_strip", "It's", "its"},
		// Hyphen is dropped (no space substituted), per the design spec's
		// "remove all non-alphanumeric/non-space runes" rule. Same rule
		// gives "It's" → "its" above. A side effect is "Walk-On" and
		// "Walk On" don't match, but covering that would require treating
		// `-` differently from `'`, which the design spec rejects in
		// favor of a single uniform rule.
		{"hyphen_strip", "Walk-On", "walkon"},
		{"parens_strip", "Title (Live)", "title live"},
		{"brackets_strip", "Title [Bonus]", "title bonus"},
		{"colon_strip", "Vol: 1", "vol 1"},
		{"double_space_collapse", "A  B   C", "a b c"},
		{"leading_trailing_space", "  A B  ", "a b"},
		{"unicode_full_width_digit", "Vol １", "vol 1"},
		{"empty", "", ""},
		{"only_punctuation", "***", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Normalise(tc.in)
			if got != tc.want {
				t.Errorf("Normalise(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestDetectCase1_groupsSameArtistSameTitle(t *testing.T) {
	loved := []gateway.AlbumMetadata{
		{ID: "1", Title: "Random Access Memories", ArtistID: "8537", TrackCount: 13, FanCount: 1000},
		{ID: "2", Title: "RANDOM ACCESS MEMORIES", ArtistID: "8537", TrackCount: 13, FanCount: 5},
		{ID: "3", Title: "Discovery", ArtistID: "8537", TrackCount: 14, FanCount: 100},
	}
	groups := DetectCase1(loved)
	if len(groups) != 1 {
		t.Fatalf("len(groups) = %d, want 1", len(groups))
	}
	g := groups[0]
	if g.ArtistID != "8537" {
		t.Errorf("ArtistID = %s", g.ArtistID)
	}
	if g.NormalisedKey != "random access memories" {
		t.Errorf("NormalisedKey = %s", g.NormalisedKey)
	}
	if len(g.Members) != 2 {
		t.Errorf("Members = %d, want 2", len(g.Members))
	}
	if g.Members[0].ID != "1" {
		t.Errorf("winner = %s, want 1", g.Members[0].ID)
	}
}

func TestDetectCase1_doesNotGroupAcrossArtists(t *testing.T) {
	loved := []gateway.AlbumMetadata{
		{ID: "1", Title: "Greatest Hits", ArtistID: "1"},
		{ID: "2", Title: "Greatest Hits", ArtistID: "2"},
	}
	groups := DetectCase1(loved)
	if len(groups) != 0 {
		t.Errorf("len(groups) = %d, want 0", len(groups))
	}
}

func TestDetectCase1_singletonsAreNotGroups(t *testing.T) {
	loved := []gateway.AlbumMetadata{
		{ID: "1", Title: "A", ArtistID: "1"},
		{ID: "2", Title: "B", ArtistID: "1"},
	}
	groups := DetectCase1(loved)
	if len(groups) != 0 {
		t.Errorf("len(groups) = %d, want 0", len(groups))
	}
}

func TestDetectCase1_threeMemberGroup(t *testing.T) {
	loved := []gateway.AlbumMetadata{
		{ID: "1", Title: "X", ArtistID: "1", TrackCount: 1},
		{ID: "2", Title: "x", ArtistID: "1", TrackCount: 5},
		{ID: "3", Title: "X ", ArtistID: "1", TrackCount: 3},
	}
	groups := DetectCase1(loved)
	if len(groups) != 1 || len(groups[0].Members) != 3 {
		t.Fatalf("groups = %+v", groups)
	}
	if groups[0].Members[0].ID != "2" {
		t.Errorf("winner = %s, want 2", groups[0].Members[0].ID)
	}
}

func TestDetectCase1_deterministicOrder(t *testing.T) {
	loved := []gateway.AlbumMetadata{
		{ID: "a1", Title: "B", ArtistID: "2"},
		{ID: "a2", Title: "B", ArtistID: "2"},
		{ID: "b1", Title: "A", ArtistID: "1"},
		{ID: "b2", Title: "A", ArtistID: "1"},
	}
	groups := DetectCase1(loved)
	if len(groups) != 2 {
		t.Fatalf("len = %d, want 2", len(groups))
	}
	if groups[0].ArtistID != "1" || groups[1].ArtistID != "2" {
		t.Errorf("artist order = [%s %s]", groups[0].ArtistID, groups[1].ArtistID)
	}
}

func ids(group []gateway.AlbumMetadata) []string {
	out := make([]string, len(group))
	for i, m := range group {
		out[i] = m.ID
	}
	sort.Strings(out)
	return out
}

func TestDetectCase2_shortMatchesTrackOnLong(t *testing.T) {
	post := []gateway.AlbumMetadata{
		{ID: "S", Title: "Foo", ArtistID: "1", TrackCount: 1},
		{ID: "L", Title: "Bar", ArtistID: "1", TrackCount: 12},
	}
	tracks := func(albumID string) ([]gateway.AlbumTrack, error) {
		if albumID == "L" {
			return []gateway.AlbumTrack{{ID: "t1", Title: "Foo"}, {ID: "t2", Title: "Other"}}, nil
		}
		t.Fatalf("unexpected ListTracks for %s", albumID)
		return nil, nil
	}
	groups, err := DetectCase2(post, tracks, 3)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(groups) != 1 || groups[0].Parent.ID != "L" || len(groups[0].Shorts) != 1 || groups[0].Shorts[0].ID != "S" {
		t.Errorf("groups = %+v", groups)
	}
}

func TestDetectCase2_shortMustBeShorterThanThreshold(t *testing.T) {
	post := []gateway.AlbumMetadata{
		{ID: "S3", Title: "Foo", ArtistID: "1", TrackCount: 3},
		{ID: "S4", Title: "Foo", ArtistID: "2", TrackCount: 4},
		{ID: "L1", Title: "Bar", ArtistID: "1", TrackCount: 12},
		{ID: "L2", Title: "Bar", ArtistID: "2", TrackCount: 12},
	}
	tracks := func(id string) ([]gateway.AlbumTrack, error) {
		switch id {
		case "L1":
			return []gateway.AlbumTrack{{ID: "t", Title: "Foo"}}, nil
		case "L2":
			return []gateway.AlbumTrack{{ID: "t", Title: "Foo"}}, nil
		}
		t.Fatalf("unexpected: %s", id)
		return nil, nil
	}
	groups, err := DetectCase2(post, tracks, 3)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(groups) != 1 || groups[0].Parent.ID != "L1" {
		t.Errorf("expected one group on artist 1; got %+v", groups)
	}
}

func TestDetectCase2_noMatchingTrack_noGroup(t *testing.T) {
	post := []gateway.AlbumMetadata{
		{ID: "S", Title: "Foo", ArtistID: "1", TrackCount: 1},
		{ID: "L", Title: "Bar", ArtistID: "1", TrackCount: 12},
	}
	tracks := func(id string) ([]gateway.AlbumTrack, error) {
		return []gateway.AlbumTrack{{ID: "t", Title: "Other"}}, nil
	}
	groups, err := DetectCase2(post, tracks, 3)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(groups) != 0 {
		t.Errorf("groups = %+v, want empty", groups)
	}
}

func TestDetectCase2_artistWithoutLong_noFetch(t *testing.T) {
	post := []gateway.AlbumMetadata{
		{ID: "S1", Title: "Foo", ArtistID: "1", TrackCount: 1},
		{ID: "S2", Title: "Foo", ArtistID: "1", TrackCount: 2},
	}
	tracks := func(id string) ([]gateway.AlbumTrack, error) {
		t.Fatalf("unexpected ListTracks for %s", id)
		return nil, nil
	}
	groups, err := DetectCase2(post, tracks, 3)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(groups) != 0 {
		t.Errorf("groups = %+v, want empty", groups)
	}
}

func TestDetectCase2_multipleShortsCollapseOntoOneParent(t *testing.T) {
	post := []gateway.AlbumMetadata{
		{ID: "S1", Title: "Foo", ArtistID: "1", TrackCount: 1},
		{ID: "S2", Title: "Bar", ArtistID: "1", TrackCount: 1},
		{ID: "L", Title: "LP", ArtistID: "1", TrackCount: 12},
	}
	tracks := func(id string) ([]gateway.AlbumTrack, error) {
		return []gateway.AlbumTrack{{Title: "Foo"}, {Title: "Bar"}}, nil
	}
	groups, err := DetectCase2(post, tracks, 3)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("len = %d", len(groups))
	}
	got := ids(groups[0].Shorts)
	if len(got) != 2 || got[0] != "S1" || got[1] != "S2" {
		t.Errorf("shorts = %v", got)
	}
}

func TestDetectCase2_multipleParents_lexSmallestNormalisedWins(t *testing.T) {
	post := []gateway.AlbumMetadata{
		{ID: "S", Title: "Foo", ArtistID: "1", TrackCount: 1},
		{ID: "Lz", Title: "Zeta", ArtistID: "1", TrackCount: 12},
		{ID: "La", Title: "Alpha", ArtistID: "1", TrackCount: 12},
	}
	tracks := func(id string) ([]gateway.AlbumTrack, error) {
		return []gateway.AlbumTrack{{Title: "Foo"}}, nil
	}
	groups, err := DetectCase2(post, tracks, 3)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(groups) != 1 || groups[0].Parent.ID != "La" {
		t.Errorf("parent = %+v, want La (lex-smallest normalised title)", groups[0].Parent)
	}
}

func TestDetectCase2_tracklistError_dropsParentFromPool(t *testing.T) {
	post := []gateway.AlbumMetadata{
		{ID: "S", Title: "Foo", ArtistID: "1", TrackCount: 1},
		{ID: "Lbad", Title: "Alpha", ArtistID: "1", TrackCount: 12},
		{ID: "Lgood", Title: "Beta", ArtistID: "1", TrackCount: 12},
	}
	tracks := func(id string) ([]gateway.AlbumTrack, error) {
		if id == "Lbad" {
			return nil, errFakeNotFound
		}
		return []gateway.AlbumTrack{{Title: "Foo"}}, nil
	}
	groups, err := DetectCase2(post, tracks, 3)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(groups) != 1 || groups[0].Parent.ID != "Lgood" {
		t.Errorf("parent = %+v, want Lgood (Lbad dropped due to fetch error)", groups[0].Parent)
	}
}

var errFakeNotFound = &gateway.GatewayError{Kind: gateway.ErrNotFound, Message: "fake"}
