package gateway

import (
	"context"
	"encoding/json"
	"fmt"
)

// FavoriteSong is one entry in a user's loved-tracks library, after merging
// IDs from song.getFavoriteIds with metadata from song.getListData.
type FavoriteSong struct {
	ID      string
	Title   string
	Artist  string
	Album   string
	TimeAdd int64
}

const (
	listFavoriteIdsMethod = "song.getFavoriteIds"
	getSongListDataMethod = "song.getListData"
	enrichmentChunkSize   = 200
)

// favoriteIDRecord is the per-song record returned by song.getFavoriteIds.
// Only SNG_ID and DATE_ADD are reliably present; the rest of the metadata
// requires a follow-up song.getListData call.
type favoriteIDRecord struct {
	ID      string      `json:"SNG_ID"`
	TimeAdd json.Number `json:"DATE_ADD"`
}

// songListDataRecord is the richer record returned by song.getListData.
type songListDataRecord struct {
	ID     string `json:"SNG_ID"`
	Title  string `json:"SNG_TITLE"`
	Artist string `json:"ART_NAME"`
	Album  string `json:"ALB_TITLE"`
}

// ListFavoriteSongs returns every loved song in the authenticated user's
// library. It paginates song.getFavoriteIds by pageSize, then enriches the
// results in chunks via song.getListData. CSRF acquisition and refresh are
// handled by callWithCSRF.
//
// pageSize is the number of records requested per song.getFavoriteIds call.
// Reasonable values are 100–10000 (deezer-py defaults to 10000, deemix to
// 25; the gateway accepts up to at least 10000 in observed usage).
func (c *Client) ListFavoriteSongs(ctx context.Context, pageSize int) ([]FavoriteSong, error) {
	if pageSize <= 0 {
		pageSize = 1000
	}

	if err := c.ensureCSRF(ctx); err != nil {
		return nil, err
	}

	// Stage 1: paginate song.getFavoriteIds to collect IDs + DATE_ADD.
	var ids []favoriteIDRecord
	start := 0
	for {
		body := map[string]any{
			"start":    start,
			"nb":       pageSize,
			"checksum": nil,
		}
		raw, err := c.callWithCSRF(ctx, listFavoriteIdsMethod, body)
		if err != nil {
			return nil, fmt.Errorf("getFavoriteIds start=%d: %w", start, err)
		}
		var page struct {
			Data  []favoriteIDRecord `json:"data"`
			Total int                `json:"total"`
		}
		if err := json.Unmarshal(raw, &page); err != nil {
			return nil, fmt.Errorf("decode getFavoriteIds start=%d: %w", start, err)
		}
		if len(page.Data) == 0 {
			break
		}
		ids = append(ids, page.Data...)
		start += len(page.Data)
		if page.Total > 0 && start >= page.Total {
			break
		}
	}

	if len(ids) == 0 {
		return nil, nil
	}

	// Stage 2: enrich in chunks via song.getListData. The IDs from Stage 1
	// are authoritative for "what is loved"; Stage 2 only adds metadata.
	// If getListData omits an ID (deleted track, regional restriction, etc.)
	// we still emit a FavoriteSong for it — otherwise the wipe would silently
	// skip those tracks.
	dateByID := make(map[string]int64, len(ids))
	for _, r := range ids {
		if t, err := r.TimeAdd.Int64(); err == nil {
			dateByID[r.ID] = t
		}
	}

	metaByID := make(map[string]songListDataRecord, len(ids))
	for i := 0; i < len(ids); i += enrichmentChunkSize {
		end := i + enrichmentChunkSize
		if end > len(ids) {
			end = len(ids)
		}
		chunk := make([]string, 0, end-i)
		for _, r := range ids[i:end] {
			chunk = append(chunk, r.ID)
		}
		raw, err := c.callWithCSRF(ctx, getSongListDataMethod, map[string]any{"SNG_IDS": chunk})
		if err != nil {
			return nil, fmt.Errorf("getListData chunk %d-%d: %w", i, end, err)
		}
		var page struct {
			Data []songListDataRecord `json:"data"`
		}
		if err := json.Unmarshal(raw, &page); err != nil {
			return nil, fmt.Errorf("decode getListData chunk %d-%d: %w", i, end, err)
		}
		for _, rec := range page.Data {
			metaByID[rec.ID] = rec
		}
	}

	all := make([]FavoriteSong, 0, len(ids))
	for _, r := range ids {
		meta := metaByID[r.ID]
		all = append(all, FavoriteSong{
			ID:      r.ID,
			Title:   meta.Title,
			Artist:  meta.Artist,
			Album:   meta.Album,
			TimeAdd: dateByID[r.ID],
		})
	}
	return all, nil
}

const removeFavoriteSongMethod = "favorite_song.remove"

// RemoveFavoriteSong unloves the song with the given Deezer SNG_ID.
// CSRF acquisition and refresh-on-expiry are handled by callWithCSRF.
// Returns *GatewayError on classified failure, error otherwise.
func (c *Client) RemoveFavoriteSong(ctx context.Context, songID string) error {
	body := map[string]any{"SNG_ID": songID}
	if _, err := c.callWithCSRF(ctx, removeFavoriteSongMethod, body); err != nil {
		return err
	}
	return nil
}

// listFavoriteSongsOnePage fetches a single page of loved-track IDs (no
// metadata enrichment) via song.getFavoriteIds. Used by the live integration
// test so it doesn't have to walk the whole library or do follow-up calls.
// The returned FavoriteSong records have ID and TimeAdd populated; Title,
// Artist, Album are empty (full enrichment is the caller's job in
// ListFavoriteSongs).
func (c *Client) listFavoriteSongsOnePage(ctx context.Context, start, nb int) ([]FavoriteSong, error) {
	body := map[string]any{
		"start":    start,
		"nb":       nb,
		"checksum": nil,
	}
	raw, err := c.callWithCSRF(ctx, listFavoriteIdsMethod, body)
	if err != nil {
		return nil, err
	}
	var page struct {
		Data []favoriteIDRecord `json:"data"`
	}
	if err := json.Unmarshal(raw, &page); err != nil {
		return nil, err
	}
	out := make([]FavoriteSong, 0, len(page.Data))
	for _, r := range page.Data {
		t, _ := r.TimeAdd.Int64()
		out = append(out, FavoriteSong{ID: r.ID, TimeAdd: t})
	}
	return out, nil
}
