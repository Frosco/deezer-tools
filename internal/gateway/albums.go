package gateway

import (
	"context"
	"encoding/json"
	"fmt"
)

const (
	pageProfileMethod      = "deezer.pageProfile"
	addFavoriteAlbumMethod = "album.addFavorite"
	// pageProfileNb is the per-tab "give me everything" limit. The gateway is
	// observed to honor large nb values and return a single page; if a real
	// account hits truncation in the wild, switch to a smaller nb plus the
	// nonexistent-but-likely `start` paging knob (discovered at impl time).
	pageProfileNb = 2000
)

// favoriteAlbumIDRecord is the per-album record shape returned in
// results.TAB.albums.data[] from deezer.pageProfile (tab="albums").
type favoriteAlbumIDRecord struct {
	ID flexString `json:"ALB_ID"`
}

// ListFavoriteAlbumIDs returns every loved album ID for the authenticated
// user via a single deezer.pageProfile call (tab="albums").
//
// CSRF acquisition and refresh are handled by callWithCSRF.
func (c *Client) ListFavoriteAlbumIDs(ctx context.Context) ([]string, error) {
	if err := c.ensureCSRF(ctx); err != nil {
		return nil, err
	}
	body := map[string]any{
		"USER_ID": c.userID,
		"tab":     "albums",
		"nb":      pageProfileNb,
	}
	raw, err := c.callWithCSRF(ctx, pageProfileMethod, body)
	if err != nil {
		return nil, fmt.Errorf("%s tab=albums: %w", pageProfileMethod, err)
	}
	var resp struct {
		TAB struct {
			Albums struct {
				Data  []favoriteAlbumIDRecord `json:"data"`
				Total int                     `json:"total"`
			} `json:"albums"`
		} `json:"TAB"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("decode %s tab=albums: %w", pageProfileMethod, err)
	}
	out := make([]string, 0, len(resp.TAB.Albums.Data))
	for _, r := range resp.TAB.Albums.Data {
		out = append(out, string(r.ID))
	}
	return out, nil
}

// AddFavoriteAlbum loves the album with the given Deezer ALB_ID.
// Idempotency on the gateway side is unverified in OSS sources — see
// docs/superpowers/research/2026-04-30-deezer-favorites-protocol.md. If a wet
// run shows an error envelope on already-loved, add a classifier branch in
// internal/gateway/errors.go to map it to success.
//
// Returns a *GatewayError on classified failure.
func (c *Client) AddFavoriteAlbum(ctx context.Context, albumID string) error {
	body := map[string]any{"ALB_ID": albumID}
	if _, err := c.callWithCSRF(ctx, addFavoriteAlbumMethod, body); err != nil {
		return err
	}
	return nil
}
