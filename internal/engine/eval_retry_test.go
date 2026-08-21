package engine

import (
	"testing"
	"time"

	"reviewparty/internal/model"
)

func TestRetryDelayHonorsProviderDelayWithinPolicyMaximum(t *testing.T) {
	policy := model.RetryPolicy{MaxAttempts: 3, InitialBackoff: "100ms", MaxBackoff: "1s"}
	if delay := retryDelay(policy, 1, 800*time.Millisecond); delay < 800*time.Millisecond || delay > time.Second {
		t.Fatalf("provider delay = %s", delay)
	}
	if delay := retryDelay(policy, 1, 2*time.Second); delay != time.Second {
		t.Fatalf("capped provider delay = %s", delay)
	}
}
