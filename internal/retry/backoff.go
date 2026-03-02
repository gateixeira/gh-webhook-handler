package retry

import (
	"math"
	"time"
)

const (
	maxBackoff   = 1 * time.Hour
	baseInterval = 10 * time.Second
	fixedDelay   = 30 * time.Second
)

// CalculateBackoff returns the delay before the next retry based on the
// strategy name and the current attempt number (1-based).
func CalculateBackoff(strategy string, attempt int) time.Duration {
	switch strategy {
	case "linear":
		d := baseInterval * time.Duration(attempt)
		if d > maxBackoff {
			return maxBackoff
		}
		return d
	case "fixed":
		return fixedDelay
	default: // "exponential" or unrecognised
		d := baseInterval * time.Duration(math.Pow(2, float64(attempt-1)))
		if d > maxBackoff {
			return maxBackoff
		}
		return d
	}
}
