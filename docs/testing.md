# Testing patterns

This document collects test-writing patterns shared across the codebase. New patterns land here when they're used by two or more packages.

## Goroutine-leak verification (`goleak`)

Tests that spawn goroutines should assert no goroutines escape the test boundary. Use `testutil.VerifyNone`:

```go
import (
    "testing"

    "klazomenai/bridge/internal/testutil"
)

func TestFoo(t *testing.T) {
    defer testutil.VerifyNone(t)
    // ... test body that spawns goroutines ...
}
```

`VerifyNone` runs via `defer` when the test function returns, before any `t.Cleanup` callbacks. If any goroutine started during the test is still running at that point, the test fails with a goroutine dump pointing to the leak. (See "Common false positives" below for why this ordering matters when servers and listeners are involved.)

### When to use

- Any test that calls a function which `go func() { ... }()` is started inside.
- Any test that uses a `*http.Server`, `net.Listener`, or other resource that spins its own goroutines.
- Any test that uses `context.WithCancel` and expects cancellation to clean up downstream goroutines.

### When to ignore current goroutines

If a test runs in a binary where a prior test or `TestMain` started a long-lived goroutine (e.g. a package-level singleton), use `goleak.IgnoreCurrent()` to baseline:

```go
import (
    "testing"

    "go.uber.org/goleak"

    "klazomenai/bridge/internal/testutil"
)

func TestFoo(t *testing.T) {
    defer testutil.VerifyNone(t, goleak.IgnoreCurrent())
    // ... test body ...
}
```

Prefer per-test `IgnoreCurrent` over a `TestMain`-level `goleak.VerifyTestMain` — the per-test scope keeps the leak surface tightest. Reach for `VerifyTestMain` only when an entire package's tests share the same baseline.

### Common false positives

- The Go runtime's `sync.Pool` cleaner goroutine: handled automatically by `goleak`.
- `httptest.Server` not closed: call `defer srv.Close()` **after** the `defer testutil.VerifyNone(t)` line. Go runs deferred calls in LIFO order, so the server closes first and `VerifyNone` runs last — no false-positive listener-goroutine leak. (`t.Cleanup` callbacks run AFTER deferred calls and would arrive too late for the leak check.)

  ```go
  func TestFoo(t *testing.T) {
      defer testutil.VerifyNone(t) // first defer statement; runs LAST
      srv := httptest.NewServer(handler)
      defer srv.Close()            // second defer statement; runs FIRST
      // ... test body ...
  }
  ```
- Timer goroutines from `time.AfterFunc`: cancel the timer before test exit (`timer.Stop()`).

## Fake clocks (`clockwork`)

Tests that assert time-dependent behaviour — TTL eviction, deadlines, retry backoff, periodic sweeps — must inject a clock interface and use a fake clock in tests. Real `time.Sleep` waits are a primary source of CI flake on slow runners.

```go
import (
    "testing"
    "time"

    "klazomenai/bridge/internal/testutil"
)

func TestTTLEviction(t *testing.T) {
    defer testutil.VerifyNone(t)
    fc := testutil.NewFakeClock()

    mgr := NewManagerWithClock(fc) // production constructor accepts clockwork.Clock

    // ... add some entries ...
    fc.Advance(2 * time.Hour) // simulate two hours passing
    mgr.Sweep()               // trigger TTL eviction explicitly

    // ... assert eviction happened ...
}
```

### Production wiring

Production constructors that need a clock should accept the `clockwork.Clock` interface (which lives in `github.com/jonboulle/clockwork`, NOT in `internal/testutil` — production code must not import a test-only package). Pass `clockwork.NewRealClock()` from `cmd/bridge/main.go`; tests pass `testutil.NewFakeClock()`.

```go
// internal/foo/foo.go
package foo

import (
    "github.com/jonboulle/clockwork"
)

type Foo struct {
    clk clockwork.Clock
    // ...
}

func NewFoo(clk clockwork.Clock) *Foo {
    return &Foo{clk: clk}
}
```

```go
// cmd/bridge/main.go
package main

import (
    "github.com/jonboulle/clockwork"

    "klazomenai/bridge/internal/foo"
)

func main() {
    f := foo.NewFoo(clockwork.NewRealClock())
    _ = f
}
```

```go
// internal/foo/foo_test.go
package foo_test

import (
    "testing"
    "time"

    "klazomenai/bridge/internal/foo"
    "klazomenai/bridge/internal/testutil"
)

func TestFooTTL(t *testing.T) {
    defer testutil.VerifyNone(t)
    fc := testutil.NewFakeClock()
    f := foo.NewFoo(fc)
    fc.Advance(2 * time.Hour)
    // ... assert TTL behaviour ...
    _ = f
}
```

### When NOT to use a fake clock

- Tests that exercise real I/O timeouts where the system call's own timeout is the thing being tested. In those cases use `context.WithTimeout` and a tight bound (e.g. 100 ms).
- Tests where wall-clock progression isn't load-bearing on the assertion. Prefer to remove the time dependency entirely.

## Test budgets

- Unit tests should complete in **under 1 second** per package.
- Integration tests (mock servers, lifecycle drivers) should complete in **under 5 seconds** per test.
- Full suite (`go test ./...`) target: **under 90 seconds** locally; CI is currently faster because it doesn't run `-race` (see below).

If you find yourself adding a `time.Sleep` to make a test pass, you almost certainly want a fake clock instead.

## Race detection

CI today runs the suite **without** `-race` (see `.github/workflows/ci.yml`) — coverage is collected with `-covermode=atomic` instead. Adopting `-race` in CI is tracked under bridge#131 (test-runtime budget); the rough cost is ~2-3× wall-clock per package, which is the reason the work hasn't already landed.

Until that lands, run `-race` locally before pushing any change that touches concurrent code:

```sh
go test -tags goolm -race -count=1 ./...
```

The bridge has a small enough test surface that `-race` runs in a few seconds locally; there's no excuse to skip it on a concurrency-touching PR. CI failures from `-race`-only flakes will surface on the developer's laptop, not in production triage.
