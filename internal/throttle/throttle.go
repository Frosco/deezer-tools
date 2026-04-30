// Package throttle is the shared pacer + retry helper used by domain
// packages (lovedtracks, playlistlove, …) that issue paced writes against
// the gw-light gateway.
//
// The 1s ± 200ms baseline pace and 5s/15s/30s/60s/120s retry schedule were
// established by the loved-tracks wipe and tuned in response to the
// 2026-04-28 Akamai IP-block incident (see docs/solutions/integration-issues/).
//
// Pace and Jitter are package vars, not consts and not Options fields, so
// the test binary of consumers can zero them in init() without exposing
// pacing as production-tunable.
package throttle

import (
	"context"
	"math/rand/v2"
	"time"
)

var (
	// Pace is the baseline sleep before every gateway attempt.
	Pace = time.Second
	// Jitter is the random additional delay added to Pace, in [0, Jitter).
	Jitter = 200 * time.Millisecond
)

// DefaultRetryBackoff is the per-item retry schedule for retryable errors.
// 5s/15s/30s/60s/120s = ~232s of waiting before a single item is given up on.
var DefaultRetryBackoff = []time.Duration{
	5 * time.Second,
	15 * time.Second,
	30 * time.Second,
	60 * time.Second,
	120 * time.Second,
}

// DefaultMaxConsecutiveFinalFailures is the orchestrator-side circuit-breaker
// threshold: after this many items in a row exhaust their retry budget with
// no successful item between, the run aborts. Counter resets on any success.
const DefaultMaxConsecutiveFinalFailures = 5

// Sleep waits Pace + rand[0, Jitter) before returning, honoring ctx.
// Pace <= 0 returns immediately. Jitter <= 0 sleeps exactly Pace.
//
// Called before EVERY gateway attempt, including the first — that's the
// throttle that keeps us off Akamai's bot list on long happy-path runs.
func Sleep(ctx context.Context) error {
	pace := Pace
	if pace <= 0 {
		return nil
	}
	d := pace
	if Jitter > 0 {
		d += time.Duration(rand.Int64N(int64(Jitter)))
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}

// RunOne executes attempt with the configured retry schedule. Returns nil on
// success, the final error after retries on failure, or ctx.Err() on
// cancellation. The first attempt is always made; retries follow only if
// isRetryable returns true for the error.
//
//   - schedule == nil   → DefaultRetryBackoff is used.
//   - schedule == empty → no retries; first attempt only.
//
// CSRF refresh is the gateway client's job (callWithCSRF), so RunOne never
// has to know about CSRF. Auth failures, not-found, and other non-retryable
// classified errors return immediately so the caller can branch on them.
func RunOne(
	ctx context.Context,
	attempt func(ctx context.Context) error,
	isRetryable func(error) bool,
	schedule []time.Duration,
) error {
	if schedule == nil {
		schedule = DefaultRetryBackoff
	}
	err := attempt(ctx)
	if err == nil {
		return nil
	}
	for _, d := range schedule {
		if !isRetryable(err) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(d):
		}
		err = attempt(ctx)
		if err == nil {
			return nil
		}
	}
	return err
}
