# Design: Easee CommandDispatcher Refactoring

**Date:** 2026-03-08
**Branch:** (to be created)

---

## Problem

`charger/easee.go` has grown to ~937 lines. The async command correlation
subsystem — the mechanism that bridges REST API calls to their SignalR
`CommandResponse` acknowledgements — is the hardest part to follow and the
most fragile to change:

- Six helper methods (`registerPendingTick`, `unregisterPendingTick`,
  `registerPendingByID`, `unregisterPendingByID`, `registerExpectedOrphan`,
  `consumeExpectedOrphan`) are spread across `easee.go` alongside unrelated
  charger logic.
- `postJSONAndWait` manually creates a per-call channel, registers it in two
  maps, defers two cleanup calls, and then waits — callers must understand
  all of this to reason about what happens when something goes wrong.
- The `CommandResponse` SignalR handler contains ~25 lines of map-lookup
  routing that is only understandable in context of the above helpers.
- The four correlation fields (`cmdMu`, `pendingTicks`, `pendingByID`,
  `expectedOrphans`) are peers of unrelated state fields in the `Easee` struct.

The result: tracing a single command from REST POST to SignalR confirmation
requires reading across six methods and four struct fields, with no clear
ownership boundary.

---

## Goals

1. Extract all async command correlation into a single, self-contained type.
2. Make `easee.go` call sites trivially readable — one method call per command.
3. Enable isolated unit testing of the correlation logic without a full `Easee`
   struct.
4. Preserve functional equivalence with the current implementation, with one
   intentional bug fix in `MaxCurrent` (see Intentional Behaviour Changes).

## Non-Goals

- Changing the public `api.Charger` interface exposed by `Easee`.
- Splitting `easee.go` into multiple files beyond the dispatcher extraction.
- Changing `waitForChargerEnabledState` or `waitForDynamicChargerCurrent`
  — these listen on `obsC` (SignalR `ProductUpdate` observations) and are a
  separate confirmation layer from `CommandResponse` correlation.
- Introducing new project-wide dependencies.

---

## Design

### Approach: Extract `CommandDispatcher` into the `easee` sub-package

A new type `CommandDispatcher` is added to `charger/easee/dispatcher.go`. It
takes ownership of:

- The HTTP POST (currently in `postJSONAndWait`)
- Response body parsing (Easee-specific: array for `/settings/`, object for
  `/commands/`)
- Per-call channel creation and lifecycle
- Both correlation maps and their mutex
- The expected-orphan counter
- `CommandResponse` routing and rogue detection

The `Easee` struct loses the four correlation fields and six helper methods,
and gains a single `dispatcher *easee.CommandDispatcher` field.
`postJSONAndWait` is deleted from `easee.go` entirely.

---

## File Structure

```
charger/
  easee.go                — Easee struct, NewEasee, Enable, MaxCurrent,
                            Phases1p3p, Status, smart charging, RFID, etc.
  easee_test.go           — existing integration-level tests (simplified)
  easee/
    dispatcher.go         — CommandDispatcher type  (new)
    dispatcher_test.go    — isolated dispatcher unit tests  (new)
    identity.go           — auth / token source  (unchanged)
    signalr.go            — SignalR types  (unchanged)
    types.go              — API types, constants, observation IDs  (unchanged)
    log.go                — SignalR logger adapter  (unchanged)
    observationid_enumer.go — generated stringer  (unchanged)
```

This is consistent with how other complex charger sub-packages are organised:
`charger/ocpp/` contains charge-point and connector logic; `charger/keba/`
contains listener and sender protocol logic; `charger/zaptec/` contains auth
logic. The `easee` sub-package already has `identity.go` making HTTP calls,
so adding HTTP-aware logic to the sub-package is not a new pattern.

---

## `CommandDispatcher` Interface

```go
package easee

// CommandDispatcher owns the full lifecycle of an Easee command:
// HTTP POST → response parsing → SignalR CommandResponse correlation.
type CommandDispatcher struct {
    helper          *request.Helper
    mu              sync.Mutex
    pendingTicks    map[int64]chan SignalRCommandResponse
    pendingByID     map[ObservationID]chan SignalRCommandResponse
    expectedOrphans map[ObservationID]int
    log             *util.Logger
    timeout         time.Duration
}

// NewCommandDispatcher creates a dispatcher. helper must be the authenticated
// HTTP client used for all Easee API calls.
func NewCommandDispatcher(
    helper *request.Helper,
    log *util.Logger,
    timeout time.Duration,
) *CommandDispatcher

// Send posts to uri with data, parses the Easee-specific response body, and
// if the response is asynchronous (HTTP 202), waits for the matching SignalR
// CommandResponse.
//
// Returns nil on success (both synchronous HTTP 200 and confirmed async HTTP 202,
// including noops where Ticks == 0). Returns an error on HTTP failure, decode
// failure, command rejection, or timeout.
func (d *CommandDispatcher) Send(uri string, data any) error

// Dispatch routes an incoming CommandResponse to the appropriate waiter.
// Must be called from the Easee.CommandResponse SignalR handler.
// Logs a WARN if no pending registration or expected orphan matches.
func (d *CommandDispatcher) Dispatch(res SignalRCommandResponse)

// ExpectOrphan pre-registers one expected CommandResponse per id for a
// sync (HTTP 200) endpoint that still produces a CommandResponse on the wire.
// Must be called before Send to avoid a race with the arriving CommandResponse.
func (d *CommandDispatcher) ExpectOrphan(ids ...ObservationID)

// CancelOrphan decrements the expected-orphan counter for id.
// Returns true if a counter entry was consumed, false if none existed.
// Used by call sites to undo an ExpectOrphan registration when the POST fails.
func (d *CommandDispatcher) CancelOrphan(id ObservationID) bool
```

---

## Internal Behaviour

### `Send`

1. POST to `uri` with `data` using the authenticated helper.
2. HTTP 200 → return `nil` immediately (synchronous, no wait).
3. HTTP other → return error immediately.
4. HTTP 202 → parse body:
   - URI contains `/commands/` → decode single `RestCommandResponse`
   - otherwise → decode `[]RestCommandResponse`, take index 0 if present
5. `cmd.Ticks == 0` → return `nil` (noop, no wait).
6. Create `ch := make(chan SignalRCommandResponse, 1)`.
7. Under `mu`: register `pendingTicks[cmd.Ticks] = ch` and
   `pendingByID[ObservationID(cmd.CommandId)] = ch`.
8. Defer cleanup: under `mu`, delete both entries on return.
9. `select { case res := <-ch: ... | case <-time.After(d.timeout): return api.ErrTimeout }`.
10. If `res.WasAccepted == false` → return `fmt.Errorf("command rejected: %d", res.Ticks)`.
11. Return `nil`.

The buffered channel (capacity 1) ensures `Dispatch` never blocks even if
`Send` has already returned due to timeout.

### `Dispatch`

1. Compute `obsID := ObservationID(res.ID)`.
2. Under `mu`: look up `ch, ok := pendingTicks[res.Ticks]`.
3. If found → send `res` to `ch`, return.
4. Under `mu`: look up `ch, ok := pendingByID[obsID]`.
5. If found → send `res` to `ch`, return.
6. Call `CancelOrphan(obsID)` → if it returns true, return silently.
7. Log WARN: rogue CommandResponse (serial, ObservationID name, Ticks,
   WasAccepted, ResultCode).

Note: `mu` is released before sending to `ch` to avoid holding the lock
during a channel send (consistent with current implementation).

### Correlation Priority

| Priority | Map | Match condition |
|----------|-----|-----------------|
| 1 (primary) | `pendingTicks` | `res.Ticks` matches registered tick |
| 2 (fallback) | `pendingByID` | `ObservationID(res.ID)` matches registered ID |
| 3 (orphan) | `expectedOrphans` | counter > 0 for this ObservationID |
| — (rogue) | — | none of the above → WARN |

The `pendingByID` fallback exists to handle backend clock drift or load
balancer scenarios where the Easee cloud delivers a `CommandResponse` with
a different `Ticks` value than the one returned in the HTTP 202 body. Without
it, a Ticks mismatch would cause a command timeout even though the charger
executed the command successfully.

---

## Changes to `easee.go`

### Struct

```go
// Remove:
cmdMu           sync.Mutex
pendingTicks    map[int64]chan easee.SignalRCommandResponse
pendingByID     map[easee.ObservationID]chan easee.SignalRCommandResponse
expectedOrphans map[easee.ObservationID]int

// Add:
dispatcher *easee.CommandDispatcher
```

### `NewEasee`

```go
// Remove:
cmdMu:           sync.Mutex{},   // (zero value, was implicit)
pendingTicks:    make(map[int64]chan easee.SignalRCommandResponse),
pendingByID:     make(map[easee.ObservationID]chan easee.SignalRCommandResponse),
expectedOrphans: make(map[easee.ObservationID]int),

// Add:
dispatcher: easee.NewCommandDispatcher(c.Helper, log, timeout),
```

### Deleted from `easee.go`

- `func (c *Easee) registerPendingTick(...)`
- `func (c *Easee) unregisterPendingTick(...)`
- `func (c *Easee) registerPendingByID(...)`
- `func (c *Easee) unregisterPendingByID(...)`
- `func (c *Easee) registerExpectedOrphan(...)`
- `func (c *Easee) consumeExpectedOrphan(...) bool`
- `func (c *Easee) waitForTickResponse(...) error`
- `func (c *Easee) postJSONAndWait(...) (bool, error)`

### Call site changes

**`Enable`, `updateSmartCharging`** — every `c.postJSONAndWait(uri, data)` becomes `c.dispatcher.Send(uri, data)`.

**`MaxCurrent`** — `c.postJSONAndWait(uri, data)` becomes `c.dispatcher.Send(uri, data)`, and the subsequent `waitForDynamicChargerCurrent` call is corrected to pass the capped value (see Intentional Behaviour Changes):

```go
// Before:
_, noop, err := c.postJSONAndWait(uri, data)
if err == nil && !noop {
    err = c.waitForDynamicChargerCurrent(float64(current)) // BUG: uncapped value
}

// After:
if err := c.dispatcher.Send(uri, data); err == nil {
    err = c.waitForDynamicChargerCurrent(cur) // cur = min(float64(current), maxChargerCurrent)
}
```

**`CommandResponse` handler** (before: ~25 lines):

```go
func (c *Easee) CommandResponse(i json.RawMessage) {
    var res easee.SignalRCommandResponse
    if err := json.Unmarshal(i, &res); err != nil {
        c.log.ERROR.Printf("invalid message: %s %v", i, err)
        return
    }
    c.log.TRACE.Printf("CommandResponse %s: %+v", res.SerialNumber, res)
    c.dispatcher.Dispatch(res)
}
```

**`Phases1p3p`** (circuit branch):

```go
c.dispatcher.ExpectOrphan(easee.CIRCUIT_MAX_CURRENT_P1)
err = c.dispatcher.Send(uri, data)
if err != nil {
    c.dispatcher.CancelOrphan(easee.CIRCUIT_MAX_CURRENT_P1)
}
```

---

## Error Handling

| Scenario | Behaviour |
|---|---|
| HTTP error (non-200/202) | Return error immediately; no channel created |
| Response body decode failure | Return error immediately; no channel created |
| Noop (`Ticks == 0`) | Return `nil` immediately; no channel created |
| `WasAccepted: false` | Return `fmt.Errorf("command rejected: %d", res.Ticks)`; deferred cleanup runs |
| Timeout | Return `api.ErrTimeout`; deferred cleanup removes both map entries. If `CommandResponse` arrives later (e.g. post-reconnect), `Dispatch` finds no entry and logs a WARN — an accepted false-positive |
| `ExpectOrphan` + POST error | Call site calls `CancelOrphan`; if orphan already consumed, `CancelOrphan` returns false and is a no-op |

---

## Testing

### New: `charger/easee/dispatcher_test.go`

The dispatcher is tested in isolation using `httpmock` on `request.Helper`.
No `Easee` struct required.

| Test | Verifies |
|---|---|
| `Send` — HTTP 200 sync | Returns `nil` immediately |
| `Send` — noop (Ticks=0) | Returns `nil` immediately |
| `Send` — HTTP error | Returns error immediately |
| `Send` — normal 202, Ticks match | Goroutine calls `Dispatch`; `Send` returns nil |
| `Send` — 202, ID fallback | `Dispatch` with wrong Ticks, matching ObservationID; `Send` returns nil |
| `Send` — timeout | No `Dispatch`; returns `api.ErrTimeout` |
| `Send` — rejected | `Dispatch` with `WasAccepted: false`; returns error |
| `Dispatch` — rogue | No entry, no orphan; WARN logged |
| `Dispatch` — expected orphan | `ExpectOrphan` first; silent consumption |
| `CancelOrphan` — rollback | `ExpectOrphan` then `CancelOrphan`; counter returns to 0 |
| `CancelOrphan` — double consume | Second call returns false |

### Simplified: `charger/easee_test.go`

- `TestEasee_postJsonAndWait` — retired (coverage moves to dispatcher tests)
- `TestEasee_waitForTickResponse` — retired (now internal to `Send`)
- `TestEasee_CommandResponse_*` — retired (coverage moves to dispatcher tests)
- `TestEasee_registerAndConsumeExpectedOrphan` — retired
- `TestEasee_Phases1p3p_registersExpectedOrphan` — remains, adapted to use
  dispatcher

All other tests (`waitForChargerEnabledState`, `waitForDynamicChargerCurrent`,
`Enable` flows, `StatusReason`, `MaxCurrent`, `InExpectedOpMode`) remain and
are unaffected.

---

## Intentional Behaviour Changes

### `MaxCurrent`: capped current passed to `waitForDynamicChargerCurrent`

**Bug:** In the current implementation, `MaxCurrent(current int64)` computes a
capped value `cur = min(float64(current), c.maxChargerCurrent)` and posts `cur`
to the API, but then calls `waitForDynamicChargerCurrent(float64(current))` with
the *uncapped* value. If `current > maxChargerCurrent`, the charger never reaches
`float64(current)`, so `waitForDynamicChargerCurrent` always times out even
though the command succeeded.

**Fix:** Pass `cur` (the capped value, which is what was actually sent) to
`waitForDynamicChargerCurrent`. The wait then short-circuits immediately if the
charger is already at `cur`, or correctly waits for the charger to reach `cur`.

This fix is a direct consequence of removing the noop boolean: the old code used
`!noop` to gate the `waitForDynamicChargerCurrent` call, but with `Send` returning
only `error`, the short-circuit for the noop case is handled inside
`waitForDynamicChargerCurrent` itself (which returns immediately if
`c.dynamicChargerCurrent == expected`). For this short-circuit to work correctly
in the capping scenario, the expected value must be `cur`, not `float64(current)`.

---

## Functional Equivalence Notes

- `waitForChargerEnabledState` and `waitForDynamicChargerCurrent` are **not**
  part of the dispatcher. They listen on `obsC` (the `ProductUpdate`
  observation channel) and confirm state changes, which is a separate
  confirmation layer from `CommandResponse` correlation.
- The pre-existing edge case where two concurrent `Send` calls share the same
  `ObservationID` would cause the second to overwrite the first's entry in
  `pendingByID` is preserved as-is. In practice this cannot occur because the
  loadpoint serializes `Enable`/`MaxCurrent` and `updateSmartCharging` writes
  a distinct ObservationID. It is worth a comment in the code.
- The `timeout` parameter passed to `NewCommandDispatcher` should be
  `c.Client.Timeout`, which is set once in `NewEasee` and never changes.
