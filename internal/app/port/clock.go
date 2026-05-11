package port

import "time"

// Clock abstracts "now" so use cases can be tested deterministically. The
// production wiring uses RealClock; tests pass a fixed-time fake.
type Clock interface {
	Now() time.Time
}

// RealClock is the standard wall-clock implementation.
type RealClock struct{}

func (RealClock) Now() time.Time { return time.Now() }
