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
