// Package playlistlove orchestrates the "love-contents" run: read N playlists,
// dedupe to unique albums and artists, diff against the user's loved sets,
// confirm, and apply paced add-to-favorites calls. It depends on
// internal/gateway via a narrow Gateway interface and on internal/throttle
// for the shared pacer / retry discipline.
package playlistlove

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
)

// Input is a normalized playlist input: the original raw form plus the
// extracted numeric playlist ID.
type Input struct {
	Raw        string
	PlaylistID string
}

// InputError describes a single input that failed to normalize.
type InputError struct {
	Raw    string
	Reason string
}

func (e InputError) Error() string { return fmt.Sprintf("%s: %s", e.Raw, e.Reason) }

// ResolveShareLink follows a Deezer short-link redirect (link.deezer.com/s/<token>)
// to the canonical URL. Decoupled as a function value so tests don't need a
// real HTTP server.
type ResolveShareLink func(ctx context.Context, link string) (string, error)

var (
	bareNumericRE = regexp.MustCompile(`^\d+$`)
	longURLRE     = regexp.MustCompile(`(?i)^https?://(?:www\.)?deezer\.com/(?:[a-z]{2}/)?playlist/(\d+)`)
	shortLinkRE   = regexp.MustCompile(`(?i)^https?://link\.deezer\.com/s/[A-Za-z0-9]+`)
)

// NormalizeInputs parses each raw input into Input{Raw, PlaylistID}, deduping
// the result by PlaylistID. Inputs that fail (bad format, network error on a
// short-link resolve) appear in the second return; they don't poison
// successful inputs.
//
// resolver is consulted only for short share links. If a short link is given
// and resolver is nil, that input fails with "no resolver".
func NormalizeInputs(ctx context.Context, raws []string, resolver ResolveShareLink) ([]Input, []InputError) {
	var ok []Input
	var bad []InputError
	seen := make(map[string]bool)

	for _, raw := range raws {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		id, err := extractPlaylistID(ctx, raw, resolver)
		if err != nil {
			bad = append(bad, InputError{Raw: raw, Reason: err.Error()})
			continue
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		ok = append(ok, Input{Raw: raw, PlaylistID: id})
	}
	return ok, bad
}

func extractPlaylistID(ctx context.Context, raw string, resolver ResolveShareLink) (string, error) {
	switch {
	case bareNumericRE.MatchString(raw):
		return raw, nil
	case longURLRE.MatchString(raw):
		m := longURLRE.FindStringSubmatch(raw)
		return m[1], nil
	case shortLinkRE.MatchString(raw):
		if resolver == nil {
			return "", fmt.Errorf("short link given but no resolver: %s", raw)
		}
		canonical, err := resolver(ctx, raw)
		if err != nil {
			return "", err
		}
		m := longURLRE.FindStringSubmatch(canonical)
		if m == nil {
			return "", fmt.Errorf("short link resolved to unrecognized URL: %s", canonical)
		}
		return m[1], nil
	default:
		return "", fmt.Errorf("unrecognized playlist input")
	}
}

// DefaultShareLinkResolver returns a ResolveShareLink backed by the given
// HTTP client. It issues a GET with CheckRedirect = http.ErrUseLastResponse,
// reads the Location header from the resulting 30x, and returns it.
//
// client may be nil; nil uses http.DefaultClient with redirects suppressed.
func DefaultShareLinkResolver(client *http.Client) ResolveShareLink {
	return func(ctx context.Context, link string) (string, error) {
		c := client
		if c == nil {
			c = &http.Client{}
		}
		// Copy the client to avoid mutating the caller's CheckRedirect.
		cc := *c
		cc.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, link, nil)
		if err != nil {
			return "", err
		}
		resp, err := cc.Do(req)
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		loc := resp.Header.Get("Location")
		if loc == "" {
			return "", fmt.Errorf("no Location header from %s (status=%d)", link, resp.StatusCode)
		}
		return loc, nil
	}
}

// ReadStdinInputs reads one playlist input per line from r. Blank lines and
// lines beginning with `#` (after trimming whitespace) are ignored.
func ReadStdinInputs(r io.Reader) ([]string, error) {
	var out []string
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
