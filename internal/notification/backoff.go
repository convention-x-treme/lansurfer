package notification

import (
	"math/rand/v2"
	"time"
)

// MaxAutomaticAttempts is the total number of attempts (including the
// first) an automatic retry round makes before a delivery becomes
// exhausted.
const MaxAutomaticAttempts = 6

const maxAutomaticDelay = 5 * time.Minute

// backoffSchedule has one entry per gap between the six total attempts:
// 5s, 30s, 2m, 5m, 5m.
var backoffSchedule = []time.Duration{
	5 * time.Second,
	30 * time.Second,
	2 * time.Minute,
	5 * time.Minute,
	5 * time.Minute,
}

// computeBackoff returns the delay before the next automatic attempt.
// attemptsMade is the number of attempts already made (1-based). When
// retryAfter is set (an HTTP 429 Retry-After or equivalent provider hint),
// it is returned as-is, without jitter or the automatic-delay cap;
// otherwise the schedule entry for attemptsMade is used, jittered, then
// capped. jitter is injected so tests can be deterministic; production code
// passes applyJitter.
func computeBackoff(attemptsMade int, retryAfter *time.Duration, jitter func(time.Duration) time.Duration) time.Duration {
	if retryAfter != nil {
		if *retryAfter < 0 {
			return 0
		}
		return *retryAfter
	}

	index := attemptsMade - 1
	if index < 0 {
		index = 0
	}
	if index >= len(backoffSchedule) {
		index = len(backoffSchedule) - 1
	}
	delay := jitter(backoffSchedule[index])
	if delay > maxAutomaticDelay {
		delay = maxAutomaticDelay
	}
	if delay < 0 {
		delay = 0
	}
	return delay
}

// applyJitter spreads a delay across roughly half to one-and-a-half times
// its base value, so many simultaneously failing deliveries do not all wake
// up on the same worker tick.
func applyJitter(base time.Duration) time.Duration {
	if base <= 0 {
		return 0
	}
	return base/2 + time.Duration(rand.Int64N(int64(base)))
}
