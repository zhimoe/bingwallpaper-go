package app

import "time"

const (
	minRetryDelay = time.Minute
	maxRetryDelay = 30 * time.Minute
)

// nextRetryDelay doubles the current delay up to maxRetryDelay.
func nextRetryDelay(current time.Duration) time.Duration {
	if current < minRetryDelay {
		return minRetryDelay
	}
	next := current * 2
	if next > maxRetryDelay || next < current {
		return maxRetryDelay
	}
	return next
}
