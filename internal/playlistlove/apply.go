package playlistlove

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

// ErrUnsupportedRecordVersion means the record's "version" field is missing
// or differs from the version this build supports (currently 1).
var ErrUnsupportedRecordVersion = errors.New("playlistlove: unsupported record version")

// ErrMalformedRecord means the record file failed structural validation:
// invalid JSON, missing required keys, or an entry without an id.
var ErrMalformedRecord = errors.New("playlistlove: malformed record")

const supportedRecordVersion = 1

// LoadRunRecord reads and validates a record JSON file produced by
// playlists love-contents (with or without --dry-run).
//
// Returns ErrUnsupportedRecordVersion (wrapped with the version we saw) or
// ErrMalformedRecord (wrapped with the parse error or location) on failure.
//
// Unknown top-level keys are accepted for forward compatibility. The
// "stats" and "source_playlists" blocks are ignored at load time —
// LoadRunRecord is a pure structural check.
//
// Both arrays absent (not just empty) is a structural error. json.Unmarshal
// sets nil for missing arrays, but we can't distinguish missing-key from
// empty-array here. We treat both as load-time success and let
// ApplyFromRecord handle "nothing to apply".
func LoadRunRecord(path string) (*RunRecord, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var rec RunRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMalformedRecord, err)
	}
	if rec.Version != supportedRecordVersion {
		return nil, fmt.Errorf("%w: file is version %d, this build supports version %d",
			ErrUnsupportedRecordVersion, rec.Version, supportedRecordVersion)
	}
	for i, a := range rec.AlbumsToAdd {
		if a.ID == "" {
			return nil, fmt.Errorf("%w: albums_to_add[%d]: missing id", ErrMalformedRecord, i)
		}
	}
	for i, a := range rec.ArtistsToAdd {
		if a.ID == "" {
			return nil, fmt.Errorf("%w: artists_to_add[%d]: missing id", ErrMalformedRecord, i)
		}
	}
	return &rec, nil
}
