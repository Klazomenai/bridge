package testutil

import (
	"github.com/jonboulle/clockwork"
)

// NewFakeClock returns a *clockwork.FakeClock for use in tests.
//
// Use FakeClock for any test that asserts time-dependent behaviour —
// TTL eviction, deadlines, retry backoff. Advance the clock manually
// with fc.Advance(d); never use real time.Sleep in tests, which is a
// primary source of flake on slow CI runners.
//
// Production code that takes a clock should accept clockwork.Clock
// directly (the interface lives in github.com/jonboulle/clockwork, not
// here — internal/testutil is test-only). Production wiring passes
// clockwork.NewRealClock() from cmd/bridge/main.go; tests pass the
// *clockwork.FakeClock returned by this constructor.
//
//	fc := testutil.NewFakeClock()
//	mgr := NewWithClock(fc) // production constructor takes clockwork.Clock
//	fc.Advance(2 * time.Hour)
//	// assert eviction happened
func NewFakeClock() *clockwork.FakeClock {
	return clockwork.NewFakeClock()
}
