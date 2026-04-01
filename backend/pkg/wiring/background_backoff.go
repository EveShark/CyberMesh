package wiring

import (
	"context"
	"errors"
	"time"
)

const maxBackgroundProbeCooldown = 60 * time.Second

func isBackgroundTimeoutErr(err error) bool {
	return errors.Is(err, context.DeadlineExceeded)
}

func nextBackgroundProbeCooldown(streak int, base time.Duration) time.Duration {
	if streak <= 0 {
		return 0
	}
	if base <= 0 {
		base = 2 * time.Second
	}
	cooldown := base
	for i := 1; i < streak; i++ {
		if cooldown >= maxBackgroundProbeCooldown/2 {
			return maxBackgroundProbeCooldown
		}
		cooldown *= 2
	}
	if cooldown > maxBackgroundProbeCooldown {
		return maxBackgroundProbeCooldown
	}
	return cooldown
}
