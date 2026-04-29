package gateway

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestEnsureCSRF_FetchesAndCaches(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Query().Get("method") != "deezer.getUserData" {
			t.Errorf("unexpected method: %s", r.URL.Query().Get("method"))
		}
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"error":[],"results":{"checkForm":"csrf-token-abc","USER":{"USER_ID":42}}}`))
	}))
	defer srv.Close()

	c := New("arl")
	c.baseURL = srv.URL

	if err := c.ensureCSRF(context.Background()); err != nil {
		t.Fatalf("ensureCSRF: %v", err)
	}
	if c.apiToken != "csrf-token-abc" {
		t.Errorf("apiToken = %q", c.apiToken)
	}
	if c.userID != "42" {
		t.Errorf("userID = %q", c.userID)
	}

	if err := c.ensureCSRF(context.Background()); err != nil {
		t.Fatalf("ensureCSRF (cached): %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("expected 1 call (second cached), got %d", got)
	}
}

func TestRefreshCSRF_AlwaysFetches(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"error":[],"results":{"checkForm":"csrf-fresh","USER":{"USER_ID":7}}}`))
	}))
	defer srv.Close()

	c := New("arl")
	c.baseURL = srv.URL
	c.apiToken = "stale"

	if err := c.refreshCSRF(context.Background()); err != nil {
		t.Fatalf("refreshCSRF: %v", err)
	}
	if c.apiToken != "csrf-fresh" {
		t.Errorf("apiToken = %q", c.apiToken)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("calls = %d, want 1", got)
	}
}

func TestEnsureCSRF_AuthFailedSurfaces(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"error":{"GATEWAY_ERROR":"NEED_USER_AUTH_REQUIRED"}}`))
	}))
	defer srv.Close()

	c := New("bad-arl")
	c.baseURL = srv.URL

	err := c.ensureCSRF(context.Background())
	if err == nil {
		t.Fatal("expected auth error")
	}
}

func TestCallWithCSRF_RefreshesOnExpiryAndRetriesOnce(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		method := r.URL.Query().Get("method")
		token := r.URL.Query().Get("api_token")
		switch {
		case method == "deezer.getUserData":
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`{"error":[],"results":{"checkForm":"fresh-csrf","USER":{"USER_ID":42}}}`))
		case method == "test.method" && token == "stale":
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`{"error":{"VALID_TOKEN_REQUIRED":"expired"}}`))
		case method == "test.method" && token == "fresh-csrf":
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`{"error":[],"results":{"ok":true}}`))
		default:
			t.Errorf("unexpected request: method=%s api_token=%s", method, token)
			w.WriteHeader(500)
		}
	}))
	defer srv.Close()

	c := New("arl")
	c.baseURL = srv.URL
	c.apiToken = "stale"
	c.userID = "42"

	raw, err := c.callWithCSRF(context.Background(), "test.method", nil)
	if err != nil {
		t.Fatalf("callWithCSRF: %v", err)
	}
	if !contains(string(raw), `"ok":true`) {
		t.Errorf("results = %s", raw)
	}
	if got := calls.Load(); got != 3 {
		t.Errorf("expected 3 server calls (stale, refresh, retry), got %d", got)
	}
	if c.apiToken != "fresh-csrf" {
		t.Errorf("apiToken = %q, want fresh-csrf", c.apiToken)
	}
}

func TestCallWithCSRF_NonCSRFErrorsBubble(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("method") == "deezer.getUserData" {
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`{"error":[],"results":{"checkForm":"x","USER":{"USER_ID":1}}}`))
			return
		}
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"error":{"GATEWAY_ERROR":"NEED_USER_AUTH_REQUIRED"}}`))
	}))
	defer srv.Close()

	c := New("arl")
	c.baseURL = srv.URL
	c.apiToken = "x"
	c.userID = "1"

	_, err := c.callWithCSRF(context.Background(), "favorite_song.remove", map[string]any{"SNG_ID": "1"})
	if err == nil {
		t.Fatal("expected auth error")
	}
	var gerr *GatewayError
	if !errors.As(err, &gerr) || gerr.Kind != ErrAuthFailed {
		t.Errorf("err = %+v, want ErrAuthFailed", err)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && indexOf(s, sub) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
