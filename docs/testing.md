# Testing patterns

This document collects test-writing patterns shared across the codebase. New patterns land here when they're used by two or more packages.

## Goroutine-leak verification (`goleak`)

Tests that spawn goroutines should assert no goroutines escape the test boundary. Use `testutil.VerifyNone`:

```go
import "klazomenai/bridge/internal/testutil"

func TestFoo(t *testing.T) {
    defer testutil.VerifyNone(t)
    // ... test body that spawns goroutines ...
}
```

`VerifyNone` runs at test cleanup time. If any goroutine started during the test is still running when the test exits, the test fails with a goroutine dump pointing to the leak.

### When to use

- Any test that calls a function which `go func() { ... }()` is started inside.
- Any test that uses a `*http.Server`, `net.Listener`, or other resource that spins its own goroutines.
- Any test that uses `context.WithCancel` and expects cancellation to clean up downstream goroutines.

### When to ignore current goroutines

If a test runs in a binary where a prior test or `TestMain` started a long-lived goroutine (e.g. a package-level singleton), use `goleak.IgnoreCurrent()` to baseline:

```go
defer testutil.VerifyNone(t, goleak.IgnoreCurrent())
```

Prefer per-test `IgnoreCurrent` over a `TestMain`-level `goleak.VerifyTestMain` — the per-test scope keeps the leak surface tightest. Reach for `VerifyTestMain` only when an entire package's tests share the same baseline.

### Common false positives

- The Go runtime's `sync.Pool` cleaner goroutine: handled automatically by `goleak`.
- `httptest.Server` not closed: register `t.Cleanup(srv.Close)` immediately after creation.
- Timer goroutines from `time.AfterFunc`: cancel the timer before test exit (`timer.Stop()`).

## Fake clocks (`clockwork`)

Tests that assert time-dependent behaviour — TTL eviction, deadlines, retry backoff, periodic sweeps — must inject a clock interface and use a fake clock in tests. Real `time.Sleep` waits are a primary source of CI flake on slow runners.

```go
import "klazomenai/bridge/internal/testutil"

func TestTTLEviction(t *testing.T) {
    defer testutil.VerifyNone(t)
    fc := testutil.NewFakeClock()

    mgr := NewManagerWithClock(fc) // production constructor accepts testutil.Clock

    // ... add some entries ...
    fc.Advance(2 * time.Hour) // simulate two hours passing
    mgr.Sweep()               // trigger TTL eviction explicitly

    // ... assert eviction happened ...
}
```

### Production wiring

Production constructors that need a clock should accept the `testutil.Clock` interface, not a concrete `*clockwork.FakeClock`. Pass `clockwork.NewRealClock()` from `main` (or wherever the production wiring lives):

```go
// internal/foo/foo.go
import "klazomenai/bridge/internal/testutil"

func NewFoo(clk testutil.Clock) *Foo { ... }

// cmd/bridge/main.go
foo := foo.NewFoo(clockwork.NewRealClock())
```

### When NOT to use a fake clock

- Tests that exercise real I/O timeouts where the system call's own timeout is the thing being tested. In those cases use `context.WithTimeout` and a tight bound (e.g. 100 ms).
- Tests where wall-clock progression isn't load-bearing on the assertion. Prefer to remove the time dependency entirely.

## Test budgets

- Unit tests should complete in **under 1 second** per package.
- Integration tests (mock servers, lifecycle drivers) should complete in **under 5 seconds** per test.
- Full suite (`go test ./...`) target: **under 90 seconds** including `-race`.

If you find yourself adding a `time.Sleep` to make a test pass, you almost certainly want a fake clock instead.

## Race detection

CI runs the suite under `-race`. Locally:

```sh
go test -tags goolm -race -count=1 ./...
```

`-race` roughly triples test runtime; budget accordingly.
