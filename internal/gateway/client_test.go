package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type capturedRequest struct {
	method   string
	query    map[string]string
	cookies  map[string]string
	body     []byte
}

func newFakeServer(t *testing.T, handler func(*capturedRequest, http.ResponseWriter)) (*httptest.Server, *capturedRequest) {
	t.Helper()
	captured := &capturedRequest{
		query:   map[string]string{},
		cookies: map[string]string{},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured.method = r.Method
		for k, v := range r.URL.Query() {
			captured.query[k] = v[0]
		}
		for _, c := range r.Cookies() {
			captured.cookies[c.Name] = c.Value
		}
		body, _ := io.ReadAll(r.Body)
		captured.body = body
		handler(captured, w)
	}))
	t.Cleanup(srv.Close)
	return srv, captured
}

func TestCall_SendsCorrectShape(t *testing.T) {
	srv, captured := newFakeServer(t, func(_ *capturedRequest, w http.ResponseWriter) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"error":[],"results":{"ok":true}}`))
	})

	c := New("test-arl")
	c.baseURL = srv.URL
	c.apiToken = "csrf-xyz"

	raw, err := c.Call(context.Background(), "favorite_song.getList", map[string]any{"user_id": "42"})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}

	if captured.method != http.MethodPost {
		t.Errorf("method = %s, want POST", captured.method)
	}
	if captured.query["method"] != "favorite_song.getList" {
		t.Errorf("method query = %q", captured.query["method"])
	}
	if captured.query["api_token"] != "csrf-xyz" {
		t.Errorf("api_token query = %q", captured.query["api_token"])
	}
	if captured.query["input"] != "3" {
		t.Errorf("input query = %q, want 3", captured.query["input"])
	}
	if captured.query["api_version"] != "1.0" {
		t.Errorf("api_version query = %q, want 1.0", captured.query["api_version"])
	}
	if captured.cookies["arl"] != "test-arl" {
		t.Errorf("arl cookie = %q", captured.cookies["arl"])
	}
	if !strings.Contains(string(captured.body), `"user_id":"42"`) {
		t.Errorf("body = %q (missing user_id)", string(captured.body))
	}

	var got map[string]bool
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal results: %v", err)
	}
	if !got["ok"] {
		t.Errorf("results.ok = %v", got["ok"])
	}
}

func TestCall_ReturnsTypedErrorOnGatewayError(t *testing.T) {
	srv, _ := newFakeServer(t, func(_ *capturedRequest, w http.ResponseWriter) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"error":{"VALID_TOKEN_REQUIRED":"bad csrf"}}`))
	})

	c := New("arl")
	c.baseURL = srv.URL
	c.apiToken = "stale"

	_, err := c.Call(context.Background(), "x.y", nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrCSRFExpiredSentinel) {
		t.Errorf("err = %v, want CSRF expired", err)
	}
}

func TestCall_ReturnsServerErrorOn500(t *testing.T) {
	srv, _ := newFakeServer(t, func(_ *capturedRequest, w http.ResponseWriter) {
		w.WriteHeader(500)
	})

	c := New("arl")
	c.baseURL = srv.URL
	c.apiToken = "x"

	_, err := c.Call(context.Background(), "x", nil)
	if !errors.Is(err, ErrServerErrorSentinel) {
		t.Errorf("err = %v, want server error", err)
	}
}

func TestCall_ContextCanceled(t *testing.T) {
	srv, _ := newFakeServer(t, func(_ *capturedRequest, w http.ResponseWriter) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"error":[],"results":{}}`))
	})

	c := New("arl")
	c.baseURL = srv.URL
	c.apiToken = "x"

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := c.Call(ctx, "x", nil)
	if err == nil {
		t.Fatal("expected error from canceled context")
	}
}
