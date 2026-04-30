package throttle

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSleep_returnsImmediatelyWhenPaceZero(t *testing.T) {
	prevPace, prevJitter := Pace, Jitter
	t.Cleanup(func() { Pace, Jitter = prevPace, prevJitter })
	Pace, Jitter = 0, 0
	start := time.Now()
	if err := Sleep(context.Background()); err != nil {
		t.Fatalf("Sleep() err = %v", err)
	}
	if d := time.Since(start); d > 50*time.Millisecond {
		t.Errorf("Sleep with Pace=0 took %v, want ~0", d)
	}
}

func TestSleep_respectsContextCancel(t *testing.T) {
	prevPace, prevJitter := Pace, Jitter
	t.Cleanup(func() { Pace, Jitter = prevPace, prevJitter })
	Pace, Jitter = time.Second, 0
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(10 * time.Millisecond); cancel() }()
	start := time.Now()
	if err := Sleep(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Sleep() err = %v, want context.Canceled", err)
	}
	if d := time.Since(start); d > 200*time.Millisecond {
		t.Errorf("Sleep waited %v after cancel, want fast return", d)
	}
}

func TestRunOne_successFirstAttempt(t *testing.T) {
	calls := 0
	err := RunOne(context.Background(),
		func(ctx context.Context) error { calls++; return nil },
		func(error) bool { return true },
		nil,
	)
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1", calls)
	}
}

func TestRunOne_retryThenSuccess(t *testing.T) {
	transient := errors.New("transient")
	calls := 0
	err := RunOne(context.Background(),
		func(ctx context.Context) error {
			calls++
			if calls < 3 {
				return transient
			}
			return nil
		},
		func(e error) bool { return errors.Is(e, transient) },
		[]time.Duration{1 * time.Millisecond, 1 * time.Millisecond, 1 * time.Millisecond, 1 * time.Millisecond, 1 * time.Millisecond},
	)
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if calls != 3 {
		t.Errorf("calls = %d, want 3 (initial + 2 retries)", calls)
	}
}

func TestRunOne_budgetExhausted(t *testing.T) {
	transient := errors.New("transient")
	calls := 0
	err := RunOne(context.Background(),
		func(ctx context.Context) error { calls++; return transient },
		func(e error) bool { return errors.Is(e, transient) },
		[]time.Duration{1 * time.Millisecond, 1 * time.Millisecond},
	)
	if !errors.Is(err, transient) {
		t.Fatalf("err = %v, want transient", err)
	}
	if calls != 3 {
		t.Errorf("calls = %d, want 3 (initial + 2 retries)", calls)
	}
}

func TestRunOne_nonRetryableReturnsImmediately(t *testing.T) {
	fatal := errors.New("fatal")
	calls := 0
	err := RunOne(context.Background(),
		func(ctx context.Context) error { calls++; return fatal },
		func(error) bool { return false },
		[]time.Duration{1 * time.Millisecond, 1 * time.Millisecond},
	)
	if !errors.Is(err, fatal) {
		t.Fatalf("err = %v, want fatal", err)
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1", calls)
	}
}

func TestRunOne_emptyScheduleNoRetry(t *testing.T) {
	transient := errors.New("transient")
	calls := 0
	err := RunOne(context.Background(),
		func(ctx context.Context) error { calls++; return transient },
		func(e error) bool { return errors.Is(e, transient) },
		[]time.Duration{},
	)
	if !errors.Is(err, transient) {
		t.Fatalf("err = %v, want transient", err)
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1 (no retries)", calls)
	}
}

func TestRunOne_ctxCancelMidSleep(t *testing.T) {
	transient := errors.New("transient")
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	go func() { time.Sleep(20 * time.Millisecond); cancel() }()
	err := RunOne(ctx,
		func(ctx context.Context) error { calls++; return transient },
		func(e error) bool { return errors.Is(e, transient) },
		[]time.Duration{500 * time.Millisecond, 500 * time.Millisecond},
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1 (cancel during first retry sleep)", calls)
	}
}
