package lovedalbums

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/niref/deezer-tools/internal/gateway"
	"github.com/niref/deezer-tools/internal/throttle"
)

// metadataFetcher is the slice of the gateway used by Phase1Fetch.
type metadataFetcher interface {
	GetAlbumMetadata(ctx context.Context, albumID string) (gateway.AlbumMetadata, error)
}

// tracksFetcher is the slice of the gateway used by Phase2Fetch.
type tracksFetcher interface {
	ListAlbumTracks(ctx context.Context, albumID string) ([]gateway.AlbumTrack, error)
}

// Phase1Fetch calls GetAlbumMetadata once per loved-album ID. Each call is
// preceded by throttle.Sleep and wrapped in throttle.RunOne with the gateway
// retryable predicate.
//
// Behaviour on classified errors:
//   - ErrAuthFailed → abort, return the error verbatim (caller surfaces the
//     standard arl-refresh message).
//   - ErrNotFound → drop the album from the candidate set; continue.
//   - Any other classified non-retryable error → drop with a debug log via
//     notify (if non-nil); continue.
//
// retry is the per-call retry schedule; nil → throttle.DefaultRetryBackoff,
// empty → first attempt only.
//
// notify is called once per dropped album so the orchestrator can log it to
// the run record. It may be nil.
func Phase1Fetch(
	ctx context.Context,
	gw metadataFetcher,
	ids []string,
	retry []time.Duration,
	notify func(albumID string, err error),
) ([]gateway.AlbumMetadata, error) {
	out := make([]gateway.AlbumMetadata, 0, len(ids))
	for _, id := range ids {
		select {
		case <-ctx.Done():
			return out, ctx.Err()
		default:
		}
		if err := throttle.Sleep(ctx); err != nil {
			return out, err
		}
		var meta gateway.AlbumMetadata
		callErr := throttle.RunOne(ctx, func(ctx context.Context) error {
			var err error
			meta, err = gw.GetAlbumMetadata(ctx, id)
			return err
		}, gateway.IsRetryable, retry)
		if callErr == nil {
			out = append(out, meta)
			continue
		}
		if errors.Is(callErr, context.Canceled) || errors.Is(callErr, context.DeadlineExceeded) {
			return out, callErr
		}
		var ge *gateway.GatewayError
		if errors.As(callErr, &ge) && ge.Kind == gateway.ErrAuthFailed {
			return out, callErr
		}
		if notify != nil {
			notify(id, callErr)
		}
	}
	return out, nil
}

// TracksLookup is the callback returned by Phase2Fetch. It returns the tracks
// fetched for the given album, or false if no tracks were fetched (album
// wasn't eligible, or the fetch failed).
type TracksLookup func(albumID string) ([]gateway.AlbumTrack, bool)

// Phase2Fetch calls ListAlbumTracks once per long album in every
// phase-2-eligible artist's loved set. An artist is eligible iff post
// contains both at least one short album (TrackCount ≤ threshold) and at
// least one long album (TrackCount > threshold) for that artist.
//
// Same throttle / retry / classification semantics as Phase1Fetch. Failed
// fetches are logged via notify (if non-nil) and dropped from the returned
// lookup; detection in DetectCase2 will simply not match against dropped
// albums.
func Phase2Fetch(
	ctx context.Context,
	gw tracksFetcher,
	post []gateway.AlbumMetadata,
	threshold int,
	retry []time.Duration,
	notify func(albumID string, err error),
) (TracksLookup, int, error) {
	if threshold <= 0 {
		threshold = 3
	}

	type bucket struct{ shorts, longs []gateway.AlbumMetadata }
	byArtist := make(map[string]*bucket)
	for _, a := range post {
		b, ok := byArtist[a.ArtistID]
		if !ok {
			b = &bucket{}
			byArtist[a.ArtistID] = b
		}
		if a.TrackCount <= threshold {
			b.shorts = append(b.shorts, a)
		} else {
			b.longs = append(b.longs, a)
		}
	}

	// Iterate artists in deterministic order so attempts/calls ordering is
	// reproducible across runs (and across test invocations).
	artistIDs := make([]string, 0, len(byArtist))
	for k := range byArtist {
		artistIDs = append(artistIDs, k)
	}
	sort.Strings(artistIDs)

	tracksByID := make(map[string][]gateway.AlbumTrack)
	attempts := 0
	for _, aid := range artistIDs {
		b := byArtist[aid]
		if len(b.shorts) == 0 || len(b.longs) == 0 {
			continue
		}
		for _, l := range b.longs {
			select {
			case <-ctx.Done():
				return nil, attempts, ctx.Err()
			default:
			}
			if err := throttle.Sleep(ctx); err != nil {
				return nil, attempts, err
			}
			id := l.ID
			attempts++
			var tracks []gateway.AlbumTrack
			callErr := throttle.RunOne(ctx, func(ctx context.Context) error {
				var err error
				tracks, err = gw.ListAlbumTracks(ctx, id)
				return err
			}, gateway.IsRetryable, retry)
			if callErr == nil {
				tracksByID[id] = tracks
				continue
			}
			if errors.Is(callErr, context.Canceled) || errors.Is(callErr, context.DeadlineExceeded) {
				return nil, attempts, callErr
			}
			var ge *gateway.GatewayError
			if errors.As(callErr, &ge) && ge.Kind == gateway.ErrAuthFailed {
				return nil, attempts, callErr
			}
			if notify != nil {
				notify(id, callErr)
			}
		}
	}

	return func(id string) ([]gateway.AlbumTrack, bool) {
		t, ok := tracksByID[id]
		return t, ok
	}, attempts, nil
}
