package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestListFavoriteArtistIDs_singleCall(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("method") {
		case "deezer.getUserData":
			w.Write([]byte(`{"results":{"checkForm":"tok","USER":{"USER_ID":42}}}`))
		case "deezer.pageProfile":
			calls++
			body, _ := readBody(r)
			s := string(body)
			if !strings.Contains(s, `"tab":"artists"`) {
				t.Errorf("expected tab=artists in body: %s", s)
			}
			w.Write([]byte(`{"results":{"TAB":{"artists":{"data":[{"ART_ID":"10"},{"ART_ID":"20"},{"ART_ID":"30"}],"total":3}}}}`))
		}
	}))
	defer srv.Close()

	c := New("test-arl")
	c.baseURL = srv.URL

	got, err := c.ListFavoriteArtistIDs(context.Background())
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(got) != 3 || got[0] != "10" || got[2] != "30" {
		t.Errorf("got = %v", got)
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1", calls)
	}
}

func TestListFavoriteArtistIDs_acceptsNumericARTID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("method") {
		case "deezer.getUserData":
			w.Write([]byte(`{"results":{"checkForm":"tok","USER":{"USER_ID":42}}}`))
		case "deezer.pageProfile":
			w.Write([]byte(`{"results":{"TAB":{"artists":{"data":[{"ART_ID":99},{"ART_ID":"100"}],"total":2}}}}`))
		}
	}))
	defer srv.Close()

	c := New("test-arl")
	c.baseURL = srv.URL

	got, err := c.ListFavoriteArtistIDs(context.Background())
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(got) != 2 || got[0] != "99" || got[1] != "100" {
		t.Errorf("got = %v", got)
	}
}

func TestAddFavoriteArtist_success(t *testing.T) {
	var seenART string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("method") {
		case "deezer.getUserData":
			w.Write([]byte(`{"results":{"checkForm":"tok","USER":{"USER_ID":42}}}`))
		case "artist.addFavorite":
			body, _ := readBody(r)
			if strings.Contains(string(body), `"ART_ID":"500"`) {
				seenART = "500"
			}
			w.Write([]byte(`{"results":true}`))
		}
	}))
	defer srv.Close()

	c := New("test-arl")
	c.baseURL = srv.URL

	if err := c.AddFavoriteArtist(context.Background(), "500"); err != nil {
		t.Fatalf("err = %v", err)
	}
	if seenART != "500" {
		t.Errorf("server did not see ART_ID=500")
	}
}
