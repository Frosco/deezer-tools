// Package gateway is a client for Deezer's unofficial gw-light.php endpoint.
//
// The protocol is undocumented; this package treats it as a best-effort
// adapter informed by open-source references (deezer-py, deemix, d-fi-core).
// Exact method names and response shapes are documented in
// docs/superpowers/research/2026-04-27-deezer-gateway-protocol.md.
package gateway

import (
	"encoding/json"
	"errors"
	"fmt"
)

// ErrorKind classifies a gateway failure for retry / surface decisions.
type ErrorKind int

const (
	ErrUnknown ErrorKind = iota
	ErrCSRFExpired
	ErrAuthFailed
	ErrRateLimited
	ErrServerError
	ErrNotFound
)

// Sentinel values for errors.Is matching.
var (
	ErrCSRFExpiredSentinel = errors.New("csrf expired")
	ErrAuthFailedSentinel  = errors.New("auth failed")
	ErrRateLimitedSentinel = errors.New("rate limited")
	ErrServerErrorSentinel = errors.New("server error")
	ErrNotFoundSentinel    = errors.New("not found")
	ErrUnknownSentinel     = errors.New("unknown gateway error")
)

// GatewayError describes a failure from a single gateway call.
type GatewayError struct {
	Kind    ErrorKind
	Method  string
	Status  int
	Message string
}

func (e *GatewayError) Error() string {
	return fmt.Sprintf("gateway %s: %s (status=%d)", e.Method, e.Message, e.Status)
}

// Is supports errors.Is(err, ErrXxxSentinel).
func (e *GatewayError) Is(target error) bool {
	switch e.Kind {
	case ErrCSRFExpired:
		return target == ErrCSRFExpiredSentinel
	case ErrAuthFailed:
		return target == ErrAuthFailedSentinel
	case ErrRateLimited:
		return target == ErrRateLimitedSentinel
	case ErrServerError:
		return target == ErrServerErrorSentinel
	case ErrNotFound:
		return target == ErrNotFoundSentinel
	}
	return target == ErrUnknownSentinel
}

// classifyError inspects an HTTP status + JSON body and returns a typed error,
// or nil if the response indicates success.
func classifyError(method string, status int, body []byte) *GatewayError {
	if status == 429 {
		return &GatewayError{Kind: ErrRateLimited, Method: method, Status: status, Message: "rate limited"}
	}
	if status >= 500 && status < 600 {
		return &GatewayError{Kind: ErrServerError, Method: method, Status: status, Message: fmt.Sprintf("server error %d", status)}
	}
	if status >= 400 && status < 500 && status != 429 {
		// continue: 4xx may also carry JSON error info
	}

	if len(body) == 0 {
		if status >= 400 {
			return &GatewayError{Kind: ErrUnknown, Method: method, Status: status, Message: fmt.Sprintf("http %d with empty body", status)}
		}
		return nil
	}

	// Gateway responses use {"error":[]} when there's no error and
	// {"error":{"CODE":"message", ...}} when there is. Decode tolerantly.
	var probe struct {
		Error json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(body, &probe); err != nil {
		return &GatewayError{Kind: ErrUnknown, Method: method, Status: status, Message: "invalid JSON in response"}
	}
	if len(probe.Error) == 0 || string(probe.Error) == "[]" || string(probe.Error) == "null" {
		return nil
	}

	var errMap map[string]string
	if err := json.Unmarshal(probe.Error, &errMap); err != nil {
		return &GatewayError{Kind: ErrUnknown, Method: method, Status: status, Message: "unrecognized error shape"}
	}
	for code, msg := range errMap {
		switch code {
		case "VALID_TOKEN_REQUIRED", "CSRF_TOKEN_INVALID":
			return &GatewayError{Kind: ErrCSRFExpired, Method: method, Status: status, Message: msg}
		case "GATEWAY_ERROR":
			if msg == "NEED_USER_AUTH_REQUIRED" || msg == "USER_AUTH_REQUIRED" {
				return &GatewayError{Kind: ErrAuthFailed, Method: method, Status: status, Message: msg}
			}
		case "DATA_ERROR":
			return &GatewayError{Kind: ErrNotFound, Method: method, Status: status, Message: msg}
		}
	}

	// Fallback: take any one entry as the message.
	for code, msg := range errMap {
		return &GatewayError{Kind: ErrUnknown, Method: method, Status: status, Message: code + ": " + msg}
	}
	return &GatewayError{Kind: ErrUnknown, Method: method, Status: status, Message: "unknown error shape"}
}
