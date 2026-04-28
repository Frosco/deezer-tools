package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestListFavoriteSongs_Pagination(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		method := r.URL.Query().Get("method")
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.WriteHeader(200)

		switch method {
		case "song.getFavoriteIds":
			start, _ := body["start"].(float64)
			var data []map[string]any
			switch int(start) {
			case 0:
				data = []map[string]any{
					{"SNG_ID": "1", "DATE_ADD": 1700000000},
					{"SNG_ID": "2", "DATE_ADD": 1700000001},
				}
			case 2:
				data = []map[string]any{
					{"SNG_ID": "3", "DATE_ADD": 1700000002},
				}
			default:
				data = []map[string]any{}
			}
			resp := map[string]any{
				"error":   []any{},
				"results": map[string]any{"data": data, "total": 3},
			}
			_ = json.NewEncoder(w).Encode(resp)
		case "song.getListData":
			ids, _ := body["SNG_IDS"].([]any)
			data := make([]map[string]any, 0, len(ids))
			titles := map[string]string{"1": "A", "2": "B", "3": "C"}
			artists := map[string]string{"1": "X", "2": "Y", "3": "Z"}
			for _, raw := range ids {
				id, _ := raw.(string)
				data = append(data, map[string]any{
					"SNG_ID":    id,
					"SNG_TITLE": titles[id],
					"ART_NAME":  artists[id],
					"ALB_TITLE": "Alb",
				})
			}
			resp := map[string]any{
				"error":   []any{},
				"results": map[string]any{"data": data},
			}
			_ = json.NewEncoder(w).Encode(resp)
		default:
			t.Errorf("unexpected method: %s", method)
		}
	}))
	defer srv.Close()

	c := New("arl")
	c.baseURL = srv.URL
	c.apiToken = "csrf"
	c.userID = "42"

	songs, err := c.ListFavoriteSongs(context.Background(), 2)
	if err != nil {
		t.Fatalf("ListFavoriteSongs: %v", err)
	}
	if len(songs) != 3 {
		t.Errorf("got %d songs, want 3", len(songs))
	}
	if songs[0].ID != "1" || songs[0].Title != "A" || songs[0].Artist != "X" {
		t.Errorf("first song wrong: %+v", songs[0])
	}
	if songs[2].ID != "3" {
		t.Errorf("third song id = %q", songs[2].ID)
	}
	if songs[0].TimeAdd != 1700000000 {
		t.Errorf("first song TimeAdd = %d, want 1700000000", songs[0].TimeAdd)
	}
	if got := calls.Load(); got < 3 {
		// expect at least: 2 getFavoriteIds pages + 1 getListData enrichment
		t.Errorf("expected at least 3 server calls, got %d", got)
	}
}

func TestListFavoriteSongs_EmptyAccount(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only song.getFavoriteIds is called when the account is empty;
		// song.getListData should be skipped.
		if r.URL.Query().Get("method") != "song.getFavoriteIds" {
			t.Errorf("unexpected method on empty account: %s", r.URL.Query().Get("method"))
		}
		w.WriteHeader(200)
		_, _ = fmt.Fprint(w, `{"error":[],"results":{"data":[],"total":0}}`)
	}))
	defer srv.Close()

	c := New("arl")
	c.baseURL = srv.URL
	c.apiToken = "csrf"
	c.userID = "42"

	songs, err := c.ListFavoriteSongs(context.Background(), 100)
	if err != nil {
		t.Fatalf("ListFavoriteSongs: %v", err)
	}
	if len(songs) != 0 {
		t.Errorf("got %d, want 0", len(songs))
	}
}

func TestListFavoriteSongs_PreservesIDsMissingFromEnrichment(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method := r.URL.Query().Get("method")
		w.WriteHeader(200)
		switch method {
		case "song.getFavoriteIds":
			_, _ = fmt.Fprint(w, `{"error":[],"results":{"data":[`+
				`{"SNG_ID":"1","DATE_ADD":1700000000},`+
				`{"SNG_ID":"2","DATE_ADD":1700000001},`+
				`{"SNG_ID":"3","DATE_ADD":1700000002}`+
				`],"total":3}}`)
		case "song.getListData":
			// Server only returns metadata for IDs 1 and 3 — ID 2 is dropped
			// (e.g. removed track). The orchestration must still see ID 2
			// in the result so the wipe can delete it.
			_, _ = fmt.Fprint(w, `{"error":[],"results":{"data":[`+
				`{"SNG_ID":"1","SNG_TITLE":"A","ART_NAME":"X","ALB_TITLE":"Alb"},`+
				`{"SNG_ID":"3","SNG_TITLE":"C","ART_NAME":"Z","ALB_TITLE":"Alb"}`+
				`]}}`)
		}
	}))
	defer srv.Close()

	c := New("arl")
	c.baseURL = srv.URL
	c.apiToken = "csrf"
	c.userID = "42"

	songs, err := c.ListFavoriteSongs(context.Background(), 100)
	if err != nil {
		t.Fatalf("ListFavoriteSongs: %v", err)
	}
	if len(songs) != 3 {
		t.Fatalf("got %d songs, want 3 (all IDs preserved even when enrichment drops some)", len(songs))
	}
	if songs[0].ID != "1" || songs[1].ID != "2" || songs[2].ID != "3" {
		t.Errorf("ID order = [%s, %s, %s], want [1, 2, 3]", songs[0].ID, songs[1].ID, songs[2].ID)
	}
	if songs[1].Title != "" || songs[1].Artist != "" {
		t.Errorf("expected empty metadata for un-enriched ID 2, got %+v", songs[1])
	}
	if songs[1].TimeAdd != 1700000001 {
		t.Errorf("ID 2 TimeAdd = %d, want 1700000001 (must come from getFavoriteIds)", songs[1].TimeAdd)
	}
	if songs[0].Title != "A" || songs[2].Title != "C" {
		t.Errorf("enriched titles wrong: [%q, %q]", songs[0].Title, songs[2].Title)
	}
}

func TestRemoveFavoriteSong_SendsCorrectBody(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		if r.URL.Query().Get("method") != "favorite_song.remove" {
			t.Errorf("method = %q", r.URL.Query().Get("method"))
		}
		w.WriteHeader(200)
		_, _ = fmt.Fprint(w, `{"error":[],"results":true}`)
	}))
	defer srv.Close()

	c := New("arl")
	c.baseURL = srv.URL
	c.apiToken = "csrf"
	c.userID = "42"

	if err := c.RemoveFavoriteSong(context.Background(), "12345"); err != nil {
		t.Fatalf("RemoveFavoriteSong: %v", err)
	}
	if got["SNG_ID"] != "12345" {
		t.Errorf("body SNG_ID = %v, want 12345", got["SNG_ID"])
	}
}

func TestRemoveFavoriteSong_PropagatesAuthError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = fmt.Fprint(w, `{"error":{"GATEWAY_ERROR":"NEED_USER_AUTH_REQUIRED"}}`)
	}))
	defer srv.Close()

	c := New("arl")
	c.baseURL = srv.URL
	c.apiToken = "csrf"
	c.userID = "42"

	err := c.RemoveFavoriteSong(context.Background(), "1")
	if err == nil {
		t.Fatal("expected error")
	}
	var gerr *GatewayError
	if !errorsAs(err, &gerr) || gerr.Kind != ErrAuthFailed {
		t.Errorf("err = %+v, want ErrAuthFailed", err)
	}
}

func errorsAs(err error, target **GatewayError) bool {
	for err != nil {
		if g, ok := err.(*GatewayError); ok {
			*target = g
			return true
		}
		type unwrapper interface{ Unwrap() error }
		u, ok := err.(unwrapper)
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}
