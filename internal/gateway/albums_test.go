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
