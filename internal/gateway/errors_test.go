package gateway

import (
	"errors"
	"fmt"
	"testing"
)

func TestClassifyError_NoErrorOK(t *testing.T) {
	err := classifyError("favorite_song.getList", 200, []byte(`{"error":[],"results":{"data":[]}}`))
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestClassifyError_CSRFExpired(t *testing.T) {
	body := []byte(`{"error":{"VALID_TOKEN_REQUIRED":"Invalid CSRF token"}}`)
	err := classifyError("favorite_song.remove", 200, body)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err.Kind != ErrCSRFExpired {
		t.Errorf("Kind = %v, want ErrCSRFExpired", err.Kind)
	}
}

func TestClassifyError_CSRFExpiredViaGatewayError(t *testing.T) {
	body := []byte(`{"error":{"GATEWAY_ERROR":"invalid api token"}}`)
	err := classifyError("favorite_song.remove", 200, body)
	if err == nil || err.Kind != ErrCSRFExpired {
		t.Errorf("got %+v, want ErrCSRFExpired", err)
	}
}

func TestClassifyError_AuthFailed(t *testing.T) {
	body := []byte(`{"error":{"GATEWAY_ERROR":"NEED_USER_AUTH_REQUIRED"}}`)
	err := classifyError("deezer.getUserData", 200, body)
	if err == nil || err.Kind != ErrAuthFailed {
		t.Errorf("got %+v, want ErrAuthFailed", err)
	}
}

func TestClassifyError_RateLimit429(t *testing.T) {
	err := classifyError("favorite_song.remove", 429, nil)
	if err == nil || err.Kind != ErrRateLimited {
		t.Errorf("got %+v, want ErrRateLimited", err)
	}
}

// QUOTA_ERROR is the gw-light protocol's own throttle signal — it arrives at
// HTTP 200 with a JSON body. Treating it as ErrUnknown caused the run on
// 2026-04-28 to skip 5,513 tracks at full rate and trigger an Akamai IP block.
func TestClassifyError_QuotaErrorIsRateLimited(t *testing.T) {
	body := []byte(`{"error":{"QUOTA_ERROR":"Quota exceeded on playlist delete songs"}}`)
	err := classifyError("favorite_song.remove", 200, body)
	if err == nil || err.Kind != ErrRateLimited {
		t.Errorf("got %+v, want ErrRateLimited", err)
	}
}

func TestClassifyError_ServerError5xx(t *testing.T) {
	for _, status := range []int{500, 502, 503, 504} {
		err := classifyError("favorite_song.remove", status, nil)
		if err == nil || err.Kind != ErrServerError {
			t.Errorf("status %d: got %+v, want ErrServerError", status, err)
		}
	}
}

func TestClassifyError_UnknownIsRetryableFalse(t *testing.T) {
	err := classifyError("favorite_song.remove", 418, []byte(`{"error":{"WAT":"teapot"}}`))
	if err == nil || err.Kind != ErrUnknown {
		t.Errorf("got %+v, want ErrUnknown", err)
	}
}

func TestGatewayError_ErrorsIs(t *testing.T) {
	gerr := &GatewayError{Kind: ErrAuthFailed, Method: "x", Message: "y"}
	if !errors.Is(gerr, ErrAuthFailedSentinel) {
		t.Errorf("errors.Is should match sentinel for kind ErrAuthFailed")
	}
}

func TestIsRetryable(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"non-gateway error", errors.New("plain"), false},
		{"rate limited", &GatewayError{Kind: ErrRateLimited}, true},
		{"server error", &GatewayError{Kind: ErrServerError}, true},
		{"auth failed", &GatewayError{Kind: ErrAuthFailed}, false},
		{"csrf expired", &GatewayError{Kind: ErrCSRFExpired}, false},
		{"not found", &GatewayError{Kind: ErrNotFound}, false},
		{"unknown", &GatewayError{Kind: ErrUnknown}, false},
		{"wrapped rate limited",
			fmt.Errorf("removeFavoriteSong: %w", &GatewayError{Kind: ErrRateLimited}),
			true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsRetryable(tc.err); got != tc.want {
				t.Errorf("IsRetryable(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
