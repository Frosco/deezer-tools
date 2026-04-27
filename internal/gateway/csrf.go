package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
)

// ensureCSRF populates c.apiToken and c.userID if they aren't set yet.
func (c *Client) ensureCSRF(ctx context.Context) error {
	if c.apiToken != "" && c.userID != "" {
		return nil
	}
	return c.refreshCSRF(ctx)
}

// refreshCSRF unconditionally re-fetches the CSRF token and user id by
// calling deezer.getUserData. The first call uses the literal-string
// api_token "null" — per the gw-light protocol, that's what the gateway
// expects for the bootstrap call to this specific method.
func (c *Client) refreshCSRF(ctx context.Context) error {
	prev := c.apiToken
	c.apiToken = "null"
	raw, err := c.Call(ctx, "deezer.getUserData", nil)
	if err != nil {
		c.apiToken = prev
		return fmt.Errorf("getUserData: %w", err)
	}

	var data struct {
		CheckForm string `json:"checkForm"`
		User      struct {
			UserID json.Number `json:"USER_ID"`
		} `json:"USER"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		return fmt.Errorf("decode getUserData: %w", err)
	}
	if data.CheckForm == "" {
		return fmt.Errorf("getUserData returned empty checkForm")
	}

	uidStr := data.User.UserID.String()
	if uidStr == "" || uidStr == "0" {
		return &GatewayError{Kind: ErrAuthFailed, Method: "deezer.getUserData", Message: "USER_ID is 0 (arl likely invalid)"}
	}
	if _, err := strconv.ParseInt(uidStr, 10, 64); err != nil {
		return fmt.Errorf("parse USER_ID %q: %w", uidStr, err)
	}

	c.apiToken = data.CheckForm
	c.userID = uidStr
	return nil
}

// callWithCSRF wraps Call with automatic CSRF acquisition and a single
// refresh-and-retry on CSRF-expiry errors. Higher-level helpers
// (ListFavoriteSongs, RemoveFavoriteSong, etc.) call through this so neither
// callers nor the orchestration layer need to know about CSRF lifecycle.
func (c *Client) callWithCSRF(ctx context.Context, method string, body any) (json.RawMessage, error) {
	if err := c.ensureCSRF(ctx); err != nil {
		return nil, err
	}
	raw, err := c.Call(ctx, method, body)
	if err == nil {
		return raw, nil
	}
	var gerr *GatewayError
	if errors.As(err, &gerr) && gerr.Kind == ErrCSRFExpired {
		if rerr := c.refreshCSRF(ctx); rerr != nil {
			return nil, fmt.Errorf("refresh after CSRF expiry: %w", rerr)
		}
		return c.Call(ctx, method, body)
	}
	return nil, err
}
