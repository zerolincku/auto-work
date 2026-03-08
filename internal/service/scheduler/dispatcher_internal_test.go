package scheduler

import (
	"testing"
	"time"
)

func TestRetryBackoff_IncreasesAndCaps(t *testing.T) {
	t.Parallel()

	if got := retryBackoff(1); got != 30*time.Second {
		t.Fatalf("attempt1 expected 30s, got %s", got)
	}
	if got := retryBackoff(2); got != 60*time.Second {
		t.Fatalf("attempt2 expected 60s, got %s", got)
	}
	if got := retryBackoff(3); got != 120*time.Second {
		t.Fatalf("attempt3 expected 120s, got %s", got)
	}
	if got := retryBackoff(5); got != 5*time.Minute {
		t.Fatalf("attempt5 expected capped 5m, got %s", got)
	}
	if got := retryBackoff(10); got != 5*time.Minute {
		t.Fatalf("attempt10 expected capped 5m, got %s", got)
	}
}
