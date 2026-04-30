package gateway

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/BurntSushi/toml"
)

// loadIntegrationARL reads the arl from ~/.config/deezer-tools/config.toml.
// Used by every TestIntegration_* test in this file. Skips/fatals via t.
func loadIntegrationARL(t *testing.T) string {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	cfgPath := filepath.Join(home, ".config", "deezer-tools", "config.toml")
	var cfg struct {
		ARL string `toml:"arl"`
	}
	if _, err := toml.DecodeFile(cfgPath, &cfg); err != nil {
		t.Fatalf("read config: %v", err)
	}
	if cfg.ARL == "" {
		t.Fatal("arl missing in config")
	}
	return cfg.ARL
}

// TestIntegration_ListFavoriteSongs_Live hits the real Deezer gw-light
// gateway. Skipped unless DEEZER_INTEGRATION=1 is set. Reads arl from
// ~/.config/deezer-tools/config.toml.
func TestIntegration_ListFavoriteSongs_Live(t *testing.T) {
	if os.Getenv("DEEZER_INTEGRATION") != "1" {
		t.Skip("set DEEZER_INTEGRATION=1 to run live read-only tests")
	}
	c := New(loadIntegrationARL(t))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// First, force CSRF acquisition so we get a clean error if the arl is
	// invalid before paginating.
	if err := c.ensureCSRF(ctx); err != nil {
		t.Fatalf("ensureCSRF: %v", err)
	}
	if c.userID == "" {
		t.Fatal("userID still empty after ensureCSRF")
	}

	// Just pull the first 50 entries so this stays fast and bounded.
	songs, err := c.listFavoriteSongsOnePage(ctx, 0, 50)
	if err != nil {
		t.Fatalf("list one page: %v", err)
	}

	t.Logf("fetched %d song ids (first page, max 50)", len(songs))
	for i, s := range songs {
		if s.ID == "" {
			t.Errorf("song %d has empty ID: %+v", i, s)
		}
	}
}

func TestIntegration_ListPlaylistSongs_publicPlaylist(t *testing.T) {
	if os.Getenv("DEEZER_INTEGRATION") != "1" {
		t.Skip("set DEEZER_INTEGRATION=1 to run")
	}
	c := New(loadIntegrationARL(t))
	// Deezer editorial playlist. The exact ID needs to be a stable, public
	// playlist; verify in a browser before relying on this test in CI.
	// "100% Hits 80s" placeholder — replace if it 404s.
	const knownPublicPlaylistID = "1313621735"
	songs, err := c.ListPlaylistSongs(context.Background(), knownPublicPlaylistID, 200)
	if err != nil {
		t.Fatalf("ListPlaylistSongs err = %v", err)
	}
	if len(songs) == 0 {
		t.Fatal("expected at least one song")
	}
	first := songs[0]
	if first.SongID == "" || first.AlbumID == "" || first.ArtistID == "" {
		t.Errorf("first song missing IDs: %+v", first)
	}
}

func TestIntegration_ListFavoriteAlbumIDs(t *testing.T) {
	if os.Getenv("DEEZER_INTEGRATION") != "1" {
		t.Skip("set DEEZER_INTEGRATION=1 to run")
	}
	c := New(loadIntegrationARL(t))
	ids, err := c.ListFavoriteAlbumIDs(context.Background())
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	t.Logf("loved albums: %d", len(ids))
}

func TestIntegration_ListFavoriteArtistIDs(t *testing.T) {
	if os.Getenv("DEEZER_INTEGRATION") != "1" {
		t.Skip("set DEEZER_INTEGRATION=1 to run")
	}
	c := New(loadIntegrationARL(t))
	ids, err := c.ListFavoriteArtistIDs(context.Background())
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	t.Logf("loved artists: %d", len(ids))
}
