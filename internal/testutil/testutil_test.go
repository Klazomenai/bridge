package testutil_test

import (
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
// is spawned but cleanly shuts down before VerifyNone is called.
func TestVerifyNone_CleanGoroutine(t *testing.T) {
	defer testutil.VerifyNone(t)
	done := make(chan struct{})
	go func() {
		close(done)
	}()
	<-done
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
