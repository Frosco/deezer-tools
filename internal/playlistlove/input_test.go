package playlistlove

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestNormalizeInputs_bareNumeric(t *testing.T) {
	got, errs := NormalizeInputs(context.Background(), []string{"15018766163"}, nil)
	if len(errs) != 0 {
		t.Fatalf("errs = %v", errs)
	}
	if len(got) != 1 || got[0].PlaylistID != "15018766163" {
		t.Errorf("got = %+v", got)
	}
}

func TestNormalizeInputs_longURL(t *testing.T) {
	urls := []string{
		"https://www.deezer.com/en/playlist/15018766163",
		"https://www.deezer.com/playlist/15018766163",
		"https://www.deezer.com/en/playlist/15018766163/",
		"https://www.deezer.com/en/playlist/15018766163?utm_source=foo",
	}
	for _, u := range urls {
		t.Run(u, func(t *testing.T) {
			got, errs := NormalizeInputs(context.Background(), []string{u}, nil)
			if len(errs) != 0 {
				t.Fatalf("errs = %v", errs)
			}
			if len(got) != 1 || got[0].PlaylistID != "15018766163" {
				t.Errorf("got = %+v", got)
			}
		})
	}
}

func TestNormalizeInputs_shareLink(t *testing.T) {
	resolver := func(ctx context.Context, link string) (string, error) {
		if link != "https://link.deezer.com/s/abc123" {
			t.Errorf("resolver called with %q", link)
		}
		return "https://www.deezer.com/playlist/15018766163", nil
	}
	got, errs := NormalizeInputs(context.Background(), []string{"https://link.deezer.com/s/abc123"}, resolver)
	if len(errs) != 0 {
		t.Fatalf("errs = %v", errs)
	}
	if len(got) != 1 || got[0].PlaylistID != "15018766163" {
		t.Errorf("got = %+v", got)
	}
}

func TestNormalizeInputs_dedupeByPlaylistID(t *testing.T) {
	// same playlist via numeric, long URL, short link
	resolver := func(ctx context.Context, _ string) (string, error) {
		return "https://www.deezer.com/playlist/123", nil
	}
	inputs := []string{
		"123",
		"https://www.deezer.com/playlist/123",
		"https://link.deezer.com/s/anything",
	}
	got, errs := NormalizeInputs(context.Background(), inputs, resolver)
	if len(errs) != 0 {
		t.Fatalf("errs = %v", errs)
	}
	if len(got) != 1 {
		t.Errorf("len = %d, want 1 (deduped)", len(got))
	}
}

func TestNormalizeInputs_invalidStringReturnsError(t *testing.T) {
	got, errs := NormalizeInputs(context.Background(), []string{"not-a-playlist"}, nil)
	if len(got) != 0 {
		t.Errorf("got = %+v, want none", got)
	}
	if len(errs) != 1 {
		t.Fatalf("errs len = %d, want 1", len(errs))
	}
	if !strings.Contains(errs[0].Reason, "unrecognized") {
		t.Errorf("reason = %q", errs[0].Reason)
	}
}

func TestNormalizeInputs_resolverErrorBecomesInputError(t *testing.T) {
	boom := errors.New("network down")
	resolver := func(ctx context.Context, _ string) (string, error) { return "", boom }
	got, errs := NormalizeInputs(context.Background(), []string{"https://link.deezer.com/s/x"}, resolver)
	if len(got) != 0 {
		t.Errorf("got = %+v", got)
	}
	if len(errs) != 1 || !strings.Contains(errs[0].Reason, "network down") {
		t.Errorf("errs = %+v", errs)
	}
}

func TestNormalizeInputs_shortLinkWithNilResolverFails(t *testing.T) {
	got, errs := NormalizeInputs(context.Background(), []string{"https://link.deezer.com/s/abc"}, nil)
	if len(got) != 0 || len(errs) != 1 {
		t.Errorf("got=%v errs=%v", got, errs)
	}
}

func TestReadStdinInputs(t *testing.T) {
	in := strings.NewReader(strings.Join([]string{
		"123",
		"# a comment",
		"",
		"https://www.deezer.com/playlist/456  ",
		"  ",
		"https://link.deezer.com/s/abc",
	}, "\n"))
	lines, err := ReadStdinInputs(in)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	want := []string{"123", "https://www.deezer.com/playlist/456", "https://link.deezer.com/s/abc"}
	if len(lines) != len(want) {
		t.Fatalf("len = %d, want %d (got %v)", len(lines), len(want), lines)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Errorf("line[%d] = %q, want %q", i, lines[i], want[i])
		}
	}
}
