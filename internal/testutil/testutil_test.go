package testutil_test

import (
	"sync"
	"testing"
	"time"

	"klazomenai/bridge/internal/testutil"
)

// TestVerifyNone_NoLeaks confirms VerifyNone passes when no goroutines
// were spawned by the test body.
func TestVerifyNone_NoLeaks(t *testing.T) {
	defer testutil.VerifyNone(t)
	// no goroutines spawned; expect VerifyNone to succeed
}

// TestVerifyNone_CleanGoroutine confirms VerifyNone passes when a goroutine
// is spawned but joins cleanly before VerifyNone is called.
//
// Uses sync.WaitGroup as the join mechanism (NOT a channel close) because
// `wg.Done()` happens *after* the goroutine's body returns: by the time
// `wg.Wait()` returns, the goroutine has fully exited, with no return-path
// race window for goleak to observe. A channel close inside the goroutine
// would only signal that the close call ran, not that the goroutine has
// finished returning. This is the pattern downstream tests should model.
func TestVerifyNone_CleanGoroutine(t *testing.T) {
	defer testutil.VerifyNone(t)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		// goroutine body — anything that completes
	}()
	wg.Wait()
}

// TestNewFakeClock_AdvancesDeterministically confirms the fake clock advances
// only when explicitly asked, never via real wall-clock progress.
func TestNewFakeClock_AdvancesDeterministically(t *testing.T) {
	defer testutil.VerifyNone(t)
	fc := testutil.NewFakeClock()
	start := fc.Now()

	fc.Advance(2 * time.Hour)

	if got := fc.Now().Sub(start); got != 2*time.Hour {
		t.Errorf("expected 2h elapsed on fake clock, got %v", got)
	}
}
