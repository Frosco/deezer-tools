package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestListPlaylistSongs_singlePage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("method") {
		case "deezer.getUserData":
			w.Write([]byte(`{"results":{"checkForm":"tok","USER":{"USER_ID":42}}}`))
		case "playlist.getSongs":
			w.Write([]byte(`{"results":{"data":[
				{"SNG_ID":"1","SNG_TITLE":"Song One","ALB_ID":"100","ALB_TITLE":"Album One","ART_ID":"200","ART_NAME":"Artist One"},
				{"SNG_ID":"2","SNG_TITLE":"Song Two","ALB_ID":"100","ALB_TITLE":"Album One","ART_ID":"201","ART_NAME":"Artist Two"}
			],"total":2}}`))
		default:
			t.Errorf("unexpected method=%s", r.URL.Query().Get("method"))
		}
	}))
	defer srv.Close()

	c := New("test-arl")
	c.baseURL = srv.URL

	got, err := c.ListPlaylistSongs(context.Background(), "9999", 100)
	if err != nil {
		t.Fatalf("ListPlaylistSongs err = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].SongID != "1" || got[0].AlbumID != "100" || got[0].ArtistID != "200" {
		t.Errorf("song[0] = %+v", got[0])
	}
	if got[1].ArtistName != "Artist Two" {
		t.Errorf("song[1].ArtistName = %q", got[1].ArtistName)
	}
}

func TestListPlaylistSongs_paginates(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("method") {
		case "deezer.getUserData":
			w.Write([]byte(`{"results":{"checkForm":"tok","USER":{"USER_ID":42}}}`))
		case "playlist.getSongs":
			calls++
			body, _ := readBody(r)
			var req struct {
				Start int `json:"start"`
				Nb    int `json:"nb"`
			}
			_ = json.Unmarshal(body, &req)
			switch req.Start {
			case 0:
				w.Write([]byte(`{"results":{"data":[
					{"SNG_ID":"1","ALB_ID":"100","ALB_TITLE":"A","ART_ID":"200","ART_NAME":"X"},
					{"SNG_ID":"2","ALB_ID":"100","ALB_TITLE":"A","ART_ID":"200","ART_NAME":"X"}
				],"total":3}}`))
			case 2:
				w.Write([]byte(`{"results":{"data":[
					{"SNG_ID":"3","ALB_ID":"101","ALB_TITLE":"B","ART_ID":"201","ART_NAME":"Y"}
				],"total":3}}`))
			default:
				t.Errorf("unexpected start=%d", req.Start)
			}
		}
	}))
	defer srv.Close()

	c := New("test-arl")
	c.baseURL = srv.URL

	got, err := c.ListPlaylistSongs(context.Background(), "9999", 2)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(got) != 3 {
		t.Errorf("len = %d, want 3", len(got))
	}
	if calls != 2 {
		t.Errorf("calls = %d, want 2", calls)
	}
}

func TestListPlaylistSongs_acceptsNumericIDs(t *testing.T) {
	// gw-light occasionally returns IDs as bare numbers within the same response.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("method") {
		case "deezer.getUserData":
			w.Write([]byte(`{"results":{"checkForm":"tok","USER":{"USER_ID":42}}}`))
		case "playlist.getSongs":
			w.Write([]byte(`{"results":{"data":[
				{"SNG_ID":1,"ALB_ID":100,"ALB_TITLE":"A","ART_ID":200,"ART_NAME":"X"}
			],"total":1}}`))
		}
	}))
	defer srv.Close()

	c := New("test-arl")
	c.baseURL = srv.URL

	got, err := c.ListPlaylistSongs(context.Background(), "9999", 100)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got[0].SongID != "1" || got[0].AlbumID != "100" || got[0].ArtistID != "200" {
		t.Errorf("got = %+v", got[0])
	}
}

// readBody is a small helper used only by tests in this file.
func readBody(r *http.Request) ([]byte, error) {
	defer r.Body.Close()
	var sb strings.Builder
	buf := make([]byte, 1024)
	for {
		n, err := r.Body.Read(buf)
		if n > 0 {
			sb.Write(buf[:n])
		}
		if err != nil {
			break
		}
	}
	return []byte(sb.String()), nil
}
