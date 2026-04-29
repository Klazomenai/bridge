package testutil

import (
	"github.com/jonboulle/clockwork"
)

// Clock is the time interface tests inject in place of real wall time.
// Production code that takes a Clock should accept the interface, not a
// concrete clockwork type, so production wiring can pass clockwork.NewRealClock()
// while tests pass NewFakeClock().
type Clock = clockwork.Clock

// NewFakeClock returns a *clockwork.FakeClock for use in tests.
//
// Use FakeClock for any test that asserts time-dependent behaviour —
// TTL eviction, deadlines, retry backoff. Advance the clock manually
// with fc.Advance(d); never use real time.Sleep in tests, which is a
// primary source of flake on slow CI runners.
//
//	fc := testutil.NewFakeClock()
//	mgr := NewWithClock(fc) // production constructor takes testutil.Clock
//	fc.Advance(2 * time.Hour)
//	// assert eviction happened
func NewFakeClock() *clockwork.FakeClock {
	return clockwork.NewFakeClock()
}
