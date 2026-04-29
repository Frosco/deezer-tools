package gateway

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/BurntSushi/toml"
)

// TestIntegration_ListFavoriteSongs_Live hits the real Deezer gw-light
// gateway. Skipped unless DEEZER_INTEGRATION=1 is set. Reads arl from
// ~/.config/deezer-tools/config.toml.
func TestIntegration_ListFavoriteSongs_Live(t *testing.T) {
	if os.Getenv("DEEZER_INTEGRATION") != "1" {
		t.Skip("set DEEZER_INTEGRATION=1 to run live read-only tests")
	}

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

	c := New(cfg.ARL)

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
