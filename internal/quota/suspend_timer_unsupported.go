//go:build !linux

package quota

import (
	"time"
)

func newSuspendAwareTimer(delay time.Duration) (<-chan time.Time, func(), error) {
	timer := time.NewTimer(delay)
	return timer.C, func() { timer.Stop() }, nil
}
