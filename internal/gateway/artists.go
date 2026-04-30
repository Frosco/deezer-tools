package gateway

import (
	"context"
	"encoding/json"
	"fmt"
)

const (
	addFavoriteArtistMethod = "artist.addFavorite"
)

// favoriteArtistIDRecord is the per-artist record shape returned in
// results.TAB.artists.data[] from deezer.pageProfile (tab="artists").
type favoriteArtistIDRecord struct {
	ID flexString `json:"ART_ID"`
}

// ListFavoriteArtistIDs returns every loved artist ID for the authenticated
// user via a single deezer.pageProfile call (tab="artists"). pageProfileMethod
// and pageProfileNb are defined alongside the album listing in albums.go.
func (c *Client) ListFavoriteArtistIDs(ctx context.Context) ([]string, error) {
	if err := c.ensureCSRF(ctx); err != nil {
		return nil, err
	}
	body := map[string]any{
		"USER_ID": c.userID,
		"tab":     "artists",
		"nb":      pageProfileNb,
	}
	raw, err := c.callWithCSRF(ctx, pageProfileMethod, body)
	if err != nil {
		return nil, fmt.Errorf("%s tab=artists: %w", pageProfileMethod, err)
	}
	var resp struct {
		TAB struct {
			Artists struct {
				Data  []favoriteArtistIDRecord `json:"data"`
				Total int                      `json:"total"`
			} `json:"artists"`
		} `json:"TAB"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("decode %s tab=artists: %w", pageProfileMethod, err)
	}
	out := make([]string, 0, len(resp.TAB.Artists.Data))
	for _, r := range resp.TAB.Artists.Data {
		out = append(out, string(r.ID))
	}
	return out, nil
}

// AddFavoriteArtist loves the artist with the given Deezer ART_ID.
// Idempotency on the gateway side is unverified in OSS sources — see the
// research doc dated 2026-04-30. Returns *GatewayError on classified failure.
func (c *Client) AddFavoriteArtist(ctx context.Context, artistID string) error {
	body := map[string]any{"ART_ID": artistID}
	if _, err := c.callWithCSRF(ctx, addFavoriteArtistMethod, body); err != nil {
		return err
	}
	return nil
}
