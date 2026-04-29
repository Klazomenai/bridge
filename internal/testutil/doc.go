// Package testutil provides shared test helpers for the bridge codebase.
//
// Helpers landed here are reusable across packages so contributors do not
// fork their own copies into per-package test files. Current scope:
//
//   - VerifyNone: wraps go.uber.org/goleak's goroutine-leak verification
//     so tests can defer testutil.VerifyNone(t) without each adding the
//     goleak import directly.
//   - NewFakeClock: re-exports github.com/jonboulle/clockwork's fake clock
//     for tests that exercise time-dependent code paths (TTL eviction,
//     timeout handling, retry backoff). Avoids real time.Sleep waits which
//     are a primary source of flake on slow CI runners.
//
// Add to this package when a helper would otherwise be duplicated across
// two or more packages. Single-use helpers belong with the test that
// uses them.
package testutil
