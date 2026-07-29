package evaluation

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

type countingCompleter struct {
	calls atomic.Int64
	fail  int64
}

func (c *countingCompleter) Complete(_ context.Context, _, _ string) (string, error) {
	call := c.calls.Add(1)
	if call <= c.fail {
		return "", errors.New("temporary failure")
	}
	return `{"ok":true}`, nil
}

func TestRetryCompleter_RetriesThenSucceeds(t *testing.T) {
	inner := &countingCompleter{fail: 2}
	retry := NewRetryCompleter(inner, time.Second, 2)
	retry.backoff = time.Millisecond

	if _, err := retry.Complete(context.Background(), "system", "user"); err != nil {
		t.Fatal(err)
	}
	if got := inner.calls.Load(); got != 3 {
		t.Fatalf("calls=%d want 3", got)
	}
}

type timeoutCompleter struct{}

func (timeoutCompleter) Complete(ctx context.Context, _, _ string) (string, error) {
	<-ctx.Done()
	return "", ctx.Err()
}

func TestRetryCompleter_PerAttemptTimeout(t *testing.T) {
	retry := NewRetryCompleter(timeoutCompleter{}, 10*time.Millisecond, 0)
	start := time.Now()
	if _, err := retry.Complete(context.Background(), "system", "user"); err == nil {
		t.Fatal("want timeout error")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("timeout took too long: %v", elapsed)
	}
}
