package lovedalbums

import "testing"

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
