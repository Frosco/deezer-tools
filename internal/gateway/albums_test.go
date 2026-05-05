package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestListFavoriteAlbumIDs_singleCall(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("method") {
		case "deezer.getUserData":
			w.Write([]byte(`{"results":{"checkForm":"tok","USER":{"USER_ID":42}}}`))
		case "deezer.pageProfile":
			calls++
			body, _ := readBody(r)
			s := string(body)
			if !strings.Contains(s, `"tab":"albums"`) {
				t.Errorf("expected tab=albums in body: %s", s)
			}
			w.Write([]byte(`{"results":{"TAB":{"albums":{"data":[{"ALB_ID":"1"},{"ALB_ID":"2"},{"ALB_ID":"3"}],"total":3}}}}`))
		}
	}))
	defer srv.Close()

	c := New("test-arl")
	c.baseURL = srv.URL

	got, err := c.ListFavoriteAlbumIDs(context.Background())
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(got) != 3 || got[0] != "1" || got[2] != "3" {
		t.Errorf("got = %v, want [1 2 3]", got)
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1", calls)
	}
}

func TestListFavoriteAlbumIDs_acceptsNumericALBID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("method") {
		case "deezer.getUserData":
			w.Write([]byte(`{"results":{"checkForm":"tok","USER":{"USER_ID":42}}}`))
		case "deezer.pageProfile":
			w.Write([]byte(`{"results":{"TAB":{"albums":{"data":[{"ALB_ID":42},{"ALB_ID":"43"}],"total":2}}}}`))
		}
	}))
	defer srv.Close()

	c := New("test-arl")
	c.baseURL = srv.URL

	got, err := c.ListFavoriteAlbumIDs(context.Background())
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(got) != 2 || got[0] != "42" || got[1] != "43" {
		t.Errorf("got = %v", got)
	}
}

func TestAddFavoriteAlbum_success(t *testing.T) {
	var seenALB string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("method") {
		case "deezer.getUserData":
			w.Write([]byte(`{"results":{"checkForm":"tok","USER":{"USER_ID":42}}}`))
		case "album.addFavorite":
			body, _ := readBody(r)
			s := string(body)
			switch {
			case strings.Contains(s, `"ALB_ID":"123"`):
				seenALB = "123"
			}
			w.Write([]byte(`{"results":true}`))
		}
	}))
	defer srv.Close()

	c := New("test-arl")
	c.baseURL = srv.URL

	if err := c.AddFavoriteAlbum(context.Background(), "123"); err != nil {
		t.Fatalf("err = %v", err)
	}
	if seenALB != "123" {
		t.Errorf("server did not see ALB_ID=123 (seen=%q)", seenALB)
	}
}

func TestAddFavoriteAlbum_classifiedError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("method") {
		case "deezer.getUserData":
			w.Write([]byte(`{"results":{"checkForm":"tok","USER":{"USER_ID":42}}}`))
		case "album.addFavorite":
			w.Write([]byte(`{"error":{"QUOTA_ERROR":"Quota exceeded"}}`))
		}
	}))
	defer srv.Close()

	c := New("test-arl")
	c.baseURL = srv.URL

	err := c.AddFavoriteAlbum(context.Background(), "123")
	if err == nil {
		t.Fatal("err = nil, want classified error")
	}
	var ge *GatewayError
	if !asGatewayError(err, &ge) || ge.Kind != ErrRateLimited {
		t.Errorf("err = %v, want ErrRateLimited via QUOTA_ERROR", err)
	}
}

func TestGetAlbumMetadata_success_mixedFormIDs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("method") {
		case "deezer.getUserData":
			w.Write([]byte(`{"results":{"checkForm":"tok","USER":{"USER_ID":42}}}`))
		case "album.getData":
			body, _ := readBody(r)
			s := string(body)
			if !strings.Contains(s, `"ALB_ID":"123"`) {
				t.Errorf("expected ALB_ID=123 in body: %s", s)
			}
			// Mixed-form IDs in the SAME response — the gw-light-quirks
			// learning says non-determinism shows up deep in pagination,
			// so synthetic responses must mix forms within one payload.
			w.Write([]byte(`{"results":{"ALB_ID":123,"ALB_TITLE":"Random Access Memories","ART_ID":"8537","ART_NAME":"Daft Punk","NB_FAN":"412000","NUMBER_TRACK":13}}`))
		}
	}))
	defer srv.Close()

	c := New("test-arl")
	c.baseURL = srv.URL

	got, err := c.GetAlbumMetadata(context.Background(), "123")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	want := AlbumMetadata{
		ID: "123", Title: "Random Access Memories",
		ArtistID: "8537", ArtistName: "Daft Punk",
		FanCount: 412000, TrackCount: 13,
	}
	if got != want {
		t.Errorf("got = %+v, want %+v", got, want)
	}
}

func TestGetAlbumMetadata_missingOrNullNumericFieldsDecodeAsZero(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("method") {
		case "deezer.getUserData":
			w.Write([]byte(`{"results":{"checkForm":"tok","USER":{"USER_ID":42}}}`))
		case "album.getData":
			// NB_FAN absent, NUMBER_TRACK literal JSON null. Both should
			// decode as 0 with no error — the orchestrator's PickWinner
			// will still pick a winner via the lowest-ALB_ID tiebreaker.
			w.Write([]byte(`{"results":{"ALB_ID":"123","ALB_TITLE":"X","ART_ID":"7","ART_NAME":"A","NUMBER_TRACK":null}}`))
		}
	}))
	defer srv.Close()

	c := New("test-arl")
	c.baseURL = srv.URL

	got, err := c.GetAlbumMetadata(context.Background(), "123")
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if got.FanCount != 0 || got.TrackCount != 0 {
		t.Errorf("got FanCount=%d TrackCount=%d, want both 0", got.FanCount, got.TrackCount)
	}
}

func TestGetAlbumMetadata_malformedNumericFieldPropagatesError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("method") {
		case "deezer.getUserData":
			w.Write([]byte(`{"results":{"checkForm":"tok","USER":{"USER_ID":42}}}`))
		case "album.getData":
			w.Write([]byte(`{"results":{"ALB_ID":"123","ALB_TITLE":"X","ART_ID":"7","ART_NAME":"A","NB_FAN":"412k","NUMBER_TRACK":13}}`))
		}
	}))
	defer srv.Close()

	c := New("test-arl")
	c.baseURL = srv.URL

	_, err := c.GetAlbumMetadata(context.Background(), "123")
	if err == nil {
		t.Fatal("err = nil, want parse error for NB_FAN=\"412k\"")
	}
	if !strings.Contains(err.Error(), "NB_FAN") {
		t.Errorf("err = %v, want it to mention NB_FAN", err)
	}
}

func TestListAlbumTracks_success_mixedFormIDs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("method") {
		case "deezer.getUserData":
			w.Write([]byte(`{"results":{"checkForm":"tok","USER":{"USER_ID":42}}}`))
		case "song.getListByAlbum":
			body, _ := readBody(r)
			s := string(body)
			if !strings.Contains(s, `"ALB_ID":"123"`) {
				t.Errorf("expected ALB_ID=123 in body: %s", s)
			}
			// Mixed quoted/unquoted SNG_ID in the same response chunk.
			w.Write([]byte(`{"results":{"data":[{"SNG_ID":"1","SNG_TITLE":"Get Lucky"},{"SNG_ID":2,"SNG_TITLE":"Instant Crush"}]}}`))
		}
	}))
	defer srv.Close()

	c := New("test-arl")
	c.baseURL = srv.URL

	got, err := c.ListAlbumTracks(context.Background(), "123")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2", len(got))
	}
	if got[0] != (AlbumTrack{ID: "1", Title: "Get Lucky"}) ||
		got[1] != (AlbumTrack{ID: "2", Title: "Instant Crush"}) {
		t.Errorf("got = %+v", got)
	}
}

func TestListAlbumTracks_dataErrorMapsToNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("method") {
		case "deezer.getUserData":
			w.Write([]byte(`{"results":{"checkForm":"tok","USER":{"USER_ID":42}}}`))
		case "song.getListByAlbum":
			w.Write([]byte(`{"error":{"DATA_ERROR":"album not found"}}`))
		}
	}))
	defer srv.Close()

	c := New("test-arl")
	c.baseURL = srv.URL

	_, err := c.ListAlbumTracks(context.Background(), "999999")
	var ge *GatewayError
	if !asGatewayError(err, &ge) || ge.Kind != ErrNotFound {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestGetAlbumMetadata_dataErrorMapsToNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("method") {
		case "deezer.getUserData":
			w.Write([]byte(`{"results":{"checkForm":"tok","USER":{"USER_ID":42}}}`))
		case "album.getData":
			w.Write([]byte(`{"error":{"DATA_ERROR":"album not found"}}`))
		}
	}))
	defer srv.Close()

	c := New("test-arl")
	c.baseURL = srv.URL

	_, err := c.GetAlbumMetadata(context.Background(), "999999")
	var ge *GatewayError
	if !asGatewayError(err, &ge) || ge.Kind != ErrNotFound {
		t.Errorf("err = %v, want ErrNotFound via DATA_ERROR", err)
	}
}

func TestGetAlbumMetadata_quotaErrorMapsToRateLimited(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("method") {
		case "deezer.getUserData":
			w.Write([]byte(`{"results":{"checkForm":"tok","USER":{"USER_ID":42}}}`))
		case "album.getData":
			w.Write([]byte(`{"error":{"QUOTA_ERROR":"Quota exceeded"}}`))
		}
	}))
	defer srv.Close()

	c := New("test-arl")
	c.baseURL = srv.URL

	_, err := c.GetAlbumMetadata(context.Background(), "123")
	var ge *GatewayError
	if !asGatewayError(err, &ge) || ge.Kind != ErrRateLimited {
		t.Errorf("err = %v, want ErrRateLimited via QUOTA_ERROR", err)
	}
}

// asGatewayError is a tiny helper bridging errors.As for terse tests.
func asGatewayError(err error, target **GatewayError) bool {
	for e := err; e != nil; {
		if ge, ok := e.(*GatewayError); ok {
			*target = ge
			return true
		}
		u, ok := e.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		e = u.Unwrap()
	}
	return false
}
