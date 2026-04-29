package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"time"
)

const defaultBaseURL = "https://www.deezer.com/ajax/gw-light.php"

// Client is a low-level adapter for Deezer's unofficial gw-light gateway.
// Higher-level helpers (track listing, favorites mutation) live alongside in
// this package and call into Client.Call.
type Client struct {
	httpClient *http.Client
	arl        string
	apiToken   string
	userID     string
	baseURL    string
}

// New constructs a Client authenticated with the given arl cookie value.
// CSRF acquisition is the caller's responsibility (see ensureCSRF).
//
// The client owns a cookie jar so server-set session cookies (notably "sid",
// which the gw-light gateway uses to bind CSRF tokens to a session) persist
// across calls. Without this, deezer.getUserData hands out a CSRF token tied
// to a session we'd immediately drop, and the next call fails with
// "Invalid CSRF token".
func New(arl string) *Client {
	jar, _ := cookiejar.New(nil)
	return &Client{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
			Jar:     jar,
		},
		arl:     arl,
		baseURL: defaultBaseURL,
	}
}

// Call performs a single gateway POST. Returns the raw "results" JSON on
// success, or a *GatewayError on failure.
//
// body may be nil; if non-nil it is JSON-encoded into the request body.
func (c *Client) Call(ctx context.Context, method string, body any) (json.RawMessage, error) {
	u, err := url.Parse(c.baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse baseURL: %w", err)
	}
	q := u.Query()
	q.Set("method", method)
	q.Set("input", "3")
	q.Set("api_version", "1.0")
	q.Set("api_token", c.apiToken)
	u.RawQuery = q.Encode()

	var bodyReader io.Reader
	if body == nil {
		bodyReader = bytes.NewReader([]byte("{}"))
	} else {
		buf, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal body for %s: %w", method, err)
		}
		bodyReader = bytes.NewReader(buf)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bodyReader)
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}
	req.AddCookie(&http.Cookie{Name: "arl", Value: c.arl})
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "*/*")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http %s: %w", method, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response %s: %w", method, err)
	}

	if gerr := classifyError(method, resp.StatusCode, respBody); gerr != nil {
		return nil, gerr
	}

	var envelope struct {
		Results json.RawMessage `json:"results"`
	}
	if err := json.Unmarshal(respBody, &envelope); err != nil {
		return nil, fmt.Errorf("decode envelope %s: %w", method, err)
	}
	return envelope.Results, nil
}
