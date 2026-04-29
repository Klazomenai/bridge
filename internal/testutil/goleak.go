package testutil

import (
	"testing"

	"go.uber.org/goleak"
)

// VerifyNone fails the test if any goroutines have leaked beyond the
// runtime baseline at the time VerifyNone is called.
//
// Typical usage at the top of a test that spawns goroutines:
//
//	func TestFoo(t *testing.T) {
//	    defer testutil.VerifyNone(t)
//	    // ... test body that spins up goroutines ...
//	}
//
// Pass goleak.IgnoreCurrent() (or any other goleak.Option) when the test
// needs to ignore goroutines that already exist before the test starts —
// for example a package-level singleton spawned by a prior test in the
// same binary. Ignoring per-test is preferred over ignoring per-package
// (TestMain-level) because it keeps the leak surface smallest.
func VerifyNone(t testing.TB, opts ...goleak.Option) {
	t.Helper()
	goleak.VerifyNone(t, opts...)
}
