package gateway

import (
	"context"
	"encoding/json"
	"fmt"
)

// PlaylistSong is one song record from playlist.getSongs, carrying enough
// metadata for the playlistlove tool to dedupe by album and artist without
// a follow-up enrichment call.
type PlaylistSong struct {
	SongID     string
	SongTitle  string
	AlbumID    string
	AlbumTitle string
	ArtistID   string
	ArtistName string
}

const (
	getPlaylistSongsMethod   = "playlist.getSongs"
	getPlaylistSongsPageSize = 200
)

// playlistSongRecord matches the per-song JSON shape returned by playlist.getSongs.
// flexString covers the gw-light habit of returning IDs as either quoted strings
// or bare numbers within the same response.
type playlistSongRecord struct {
	SongID     flexString `json:"SNG_ID"`
	SongTitle  string     `json:"SNG_TITLE"`
	AlbumID    flexString `json:"ALB_ID"`
	AlbumTitle string     `json:"ALB_TITLE"`
	ArtistID   flexString `json:"ART_ID"`
	ArtistName string     `json:"ART_NAME"`
}

// ListPlaylistSongs paginates playlist.getSongs and returns every song in
// the playlist with album- and artist-level metadata sufficient for dedupe.
//
// pageSize <= 0 uses the default (200). Reasonable values are 100–1000.
//
// CSRF acquisition and refresh are handled by callWithCSRF.
func (c *Client) ListPlaylistSongs(ctx context.Context, playlistID string, pageSize int) ([]PlaylistSong, error) {
	if pageSize <= 0 {
		pageSize = getPlaylistSongsPageSize
	}
	if err := c.ensureCSRF(ctx); err != nil {
		return nil, err
	}

	var out []PlaylistSong
	start := 0
	for {
		body := map[string]any{
			"playlist_id": playlistID,
			"nb":          pageSize,
			"start":       start,
			"tab":         "songs",
		}
		raw, err := c.callWithCSRF(ctx, getPlaylistSongsMethod, body)
		if err != nil {
			return nil, fmt.Errorf("%s playlist=%s start=%d: %w", getPlaylistSongsMethod, playlistID, start, err)
		}
		var page struct {
			Data  []playlistSongRecord `json:"data"`
			Total int                  `json:"total"`
		}
		if err := json.Unmarshal(raw, &page); err != nil {
			return nil, fmt.Errorf("decode %s playlist=%s start=%d: %w", getPlaylistSongsMethod, playlistID, start, err)
		}
		if len(page.Data) == 0 {
			break
		}
		for _, r := range page.Data {
			out = append(out, PlaylistSong{
				SongID:     string(r.SongID),
				SongTitle:  r.SongTitle,
				AlbumID:    string(r.AlbumID),
				AlbumTitle: r.AlbumTitle,
				ArtistID:   string(r.ArtistID),
				ArtistName: r.ArtistName,
			})
		}
		start += len(page.Data)
		if page.Total > 0 && start >= page.Total {
			break
		}
	}
	return out, nil
}
