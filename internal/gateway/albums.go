package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
)

const (
	pageProfileMethod      = "deezer.pageProfile"
	addFavoriteAlbumMethod = "album.addFavorite"
	getAlbumMetadataMethod = "album.getData"
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

// AlbumMetadata is the lightweight album record used by lovedalbums dedup
// and by playlistlove's within-playlist Case-1 pass.
type AlbumMetadata struct {
	ID         string
	Title      string
	ArtistID   string
	ArtistName string
	FanCount   int
	TrackCount int
}

// albumMetadataRecord is the on-the-wire shape of one album record returned
// by album.getData. All ID-shaped fields use flexString — gw-light returns
// IDs in mixed quoted/numeric forms within a single response payload, see
// docs/solutions/design-patterns/gw-light-go-adapter-quirks-2026-04-28.md.
// FanCount and TrackCount are also flexString — d-fi-core types NUMBER_TRACK
// inconsistently across two interfaces, the same precedent that triggered
// flexString adoption for SNG_ID.
type albumMetadataRecord struct {
	ID         flexString `json:"ALB_ID"`
	Title      string     `json:"ALB_TITLE"`
	ArtistID   flexString `json:"ART_ID"`
	ArtistName string     `json:"ART_NAME"`
	FanCount   flexString `json:"NB_FAN"`
	TrackCount flexString `json:"NUMBER_TRACK"`
}

// GetAlbumMetadata fetches one album's metadata via gw-light album.getData.
// CSRF acquisition and refresh-on-expiry are handled by callWithCSRF.
func (c *Client) GetAlbumMetadata(ctx context.Context, albumID string) (AlbumMetadata, error) {
	body := map[string]any{"ALB_ID": albumID}
	raw, err := c.callWithCSRF(ctx, getAlbumMetadataMethod, body)
	if err != nil {
		return AlbumMetadata{}, err
	}
	var rec albumMetadataRecord
	if err := json.Unmarshal(raw, &rec); err != nil {
		return AlbumMetadata{}, fmt.Errorf("decode %s: %w", getAlbumMetadataMethod, err)
	}
	fans, err := parseFlexInt(rec.FanCount)
	if err != nil {
		return AlbumMetadata{}, fmt.Errorf("decode %s NB_FAN %q: %w", getAlbumMetadataMethod, string(rec.FanCount), err)
	}
	tracks, err := parseFlexInt(rec.TrackCount)
	if err != nil {
		return AlbumMetadata{}, fmt.Errorf("decode %s NUMBER_TRACK %q: %w", getAlbumMetadataMethod, string(rec.TrackCount), err)
	}
	return AlbumMetadata{
		ID:         string(rec.ID),
		Title:      rec.Title,
		ArtistID:   string(rec.ArtistID),
		ArtistName: rec.ArtistName,
		FanCount:   fans,
		TrackCount: tracks,
	}, nil
}

// parseFlexInt parses a flexString that might arrive as a quoted string, a
// bare JSON number, JSON null, or be absent. Returns 0, nil for empty/null
// input (treated as "field missing or unset"). Returns 0, err if the content
// is non-empty but not a valid integer — propagating lets PickWinner skip and
// log the album rather than silently scoring it 0 on a tiebreaker dimension.
func parseFlexInt(s flexString) (int, error) {
	if s == "" || string(s) == "null" {
		return 0, nil
	}
	return strconv.Atoi(string(s))
}
