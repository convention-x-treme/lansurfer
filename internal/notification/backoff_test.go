package notification

import (
	"testing"
	"time"
)

func TestComputeBackoffFollowsScheduleAndCapsAtFiveMinutes(t *testing.T) {
	noJitter := func(d time.Duration) time.Duration { return d }
	want := []time.Duration{5 * time.Second, 30 * time.Second, 2 * time.Minute, 5 * time.Minute, 5 * time.Minute}
	for attemptsMade := 1; attemptsMade <= len(want); attemptsMade++ {
		got := computeBackoff(attemptsMade, nil, noJitter)
		if got != want[attemptsMade-1] {
			t.Fatalf("computeBackoff(%d) = %v, want %v", attemptsMade, got, want[attemptsMade-1])
		}
	}
}

func TestComputeBackoffClampsAttemptsBeyondScheduleLength(t *testing.T) {
	noJitter := func(d time.Duration) time.Duration { return d }
	if got := computeBackoff(9, nil, noJitter); got != 5*time.Minute {
		t.Fatalf("computeBackoff(9) = %v, want 5m (clamped to the last schedule entry)", got)
	}
}

func TestComputeBackoffHonorsRetryAfterWithoutCap(t *testing.T) {
	noJitter := func(d time.Duration) time.Duration { return d }
	hint := 45 * time.Second
	if got := computeBackoff(1, &hint, noJitter); got != hint {
		t.Fatalf("computeBackoff with Retry-After = %v, want %v", got, hint)
	}
	longHint := 20 * time.Minute
	if got := computeBackoff(1, &longHint, noJitter); got != longHint {
		t.Fatalf("computeBackoff with a long Retry-After = %v, want %v", got, longHint)
	}
	negativeHint := -time.Second
	if got := computeBackoff(1, &negativeHint, noJitter); got != 0 {
		t.Fatalf("computeBackoff with negative Retry-After = %v, want 0", got)
	}
}

func TestComputeBackoffAppliesJitterOnlyToScheduledDelay(t *testing.T) {
	called := false
	jitter := func(d time.Duration) time.Duration { called = true; return d + time.Second }
	if got := computeBackoff(1, nil, jitter); !called || got != 6*time.Second {
		t.Fatalf("computeBackoff(1, nil, jitter) = %v, called = %v, want 6s and called", got, called)
	}

	called = false
	if got := computeBackoff(1, nil, func(time.Duration) time.Duration {
		called = true
		return 20 * time.Minute
	}); !called || got != 5*time.Minute {
		t.Fatalf("computeBackoff with an over-cap jittered delay = %v, called = %v, want 5m and called", got, called)
	}

	called = false
	hint := 10 * time.Second
	if got := computeBackoff(1, &hint, jitter); called || got != hint {
		t.Fatalf("computeBackoff with Retry-After must not be jittered: got %v, called = %v", got, called)
	}
}

func TestApplyJitterStaysWithinHalfToOneAndHalfTimesBase(t *testing.T) {
	base := 10 * time.Second
	lower, upper := base/2, base/2+base // upper is exclusive
	for i := 0; i < 200; i++ {
		got := applyJitter(base)
		if got < lower || got >= upper {
			t.Fatalf("applyJitter(%v) = %v, want within [%v, %v)", base, got, lower, upper)
		}
	}
}
