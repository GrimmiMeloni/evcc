# Easee CommandDispatcher Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Extract the async command correlation subsystem from `charger/easee.go` into a self-contained `CommandDispatcher` type in the `charger/easee` sub-package.

**Architecture:** A new `CommandDispatcher` type in `charger/easee/dispatcher.go` takes ownership of the HTTP POST, response parsing, per-call channel lifecycle, both correlation maps, the expected-orphan counter, and `CommandResponse` routing. The `Easee` struct loses four correlation fields and eight methods, gaining a single `dispatcher *easee.CommandDispatcher` field. Call sites in `easee.go` become one-liners.

**Tech Stack:** Go stdlib (`encoding/json`, `fmt`, `strings`, `sync`, `time`), `github.com/evcc-io/evcc/api`, `github.com/evcc-io/evcc/util`, `github.com/evcc-io/evcc/util/request`, `github.com/jarcoal/httpmock` (tests only), `github.com/stretchr/testify` (tests only).

**Design doc:** `docs/plans/2026-03-08-easee-command-dispatcher-design.md`

---

### Task 1: Write failing tests for Dispatch, ExpectOrphan, CancelOrphan

**Files:**
- Create: `charger/easee/dispatcher_test.go`

**Step 1: Create the test file**

```go
package easee

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/evcc-io/evcc/api"
	"github.com/evcc-io/evcc/util"
	"github.com/evcc-io/evcc/util/request"
	"github.com/jarcoal/httpmock"
	"github.com/stretchr/testify/assert"
)

func newTestDispatcher(t *testing.T) *CommandDispatcher {
	t.Helper()
	log := util.NewLogger("test")
	h := request.NewHelper(log)
	h.Client.Timeout = 500 * time.Millisecond
	return NewCommandDispatcher(h, log, 500*time.Millisecond)
}

func TestDispatcher_Dispatch_Rogue(t *testing.T) {
	d := newTestDispatcher(t)
	assert.NotPanics(t, func() {
		d.Dispatch(SignalRCommandResponse{
			SerialNumber: "EH123456",
			Ticks:        999999999,
			WasAccepted:  true,
		})
	})
}

func TestDispatcher_Dispatch_ExpectedOrphan(t *testing.T) {
	d := newTestDispatcher(t)
	d.ExpectOrphan(CIRCUIT_MAX_CURRENT_P1)

	assert.NotPanics(t, func() {
		d.Dispatch(SignalRCommandResponse{
			ID:          int(CIRCUIT_MAX_CURRENT_P1),
			Ticks:       111111111,
			WasAccepted: true,
		})
	})

	// Counter consumed — a second call to CancelOrphan returns false
	assert.False(t, d.CancelOrphan(CIRCUIT_MAX_CURRENT_P1))
}

func TestDispatcher_CancelOrphan_Rollback(t *testing.T) {
	d := newTestDispatcher(t)
	d.ExpectOrphan(CIRCUIT_MAX_CURRENT_P1)
	assert.True(t, d.CancelOrphan(CIRCUIT_MAX_CURRENT_P1))
	assert.False(t, d.CancelOrphan(CIRCUIT_MAX_CURRENT_P1))
}

func TestDispatcher_CancelOrphan_DoubleConsume(t *testing.T) {
	d := newTestDispatcher(t)
	d.ExpectOrphan(CIRCUIT_MAX_CURRENT_P1)
	// Dispatch consumes the orphan counter
	d.Dispatch(SignalRCommandResponse{ID: int(CIRCUIT_MAX_CURRENT_P1), Ticks: 111})
	// CancelOrphan now finds nothing
	assert.False(t, d.CancelOrphan(CIRCUIT_MAX_CURRENT_P1))
}
```

**Step 2: Run tests to verify they fail**

```
go test ./charger/easee/ -run TestDispatcher -v
```
Expected: compilation error — `CommandDispatcher` undefined.

---

### Task 2: Implement dispatcher.go with struct, New, Dispatch, ExpectOrphan, CancelOrphan

**Files:**
- Create: `charger/easee/dispatcher.go`

**Step 1: Create the file**

```go
package easee

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/evcc-io/evcc/api"
	"github.com/evcc-io/evcc/util"
	"github.com/evcc-io/evcc/util/request"
)

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
func NewCommandDispatcher(helper *request.Helper, log *util.Logger, timeout time.Duration) *CommandDispatcher {
	return &CommandDispatcher{
		helper:          helper,
		log:             log,
		timeout:         timeout,
		pendingTicks:    make(map[int64]chan SignalRCommandResponse),
		pendingByID:     make(map[ObservationID]chan SignalRCommandResponse),
		expectedOrphans: make(map[ObservationID]int),
	}
}

// Dispatch routes an incoming CommandResponse to the appropriate waiter.
// Must be called from the Easee.CommandResponse SignalR handler.
// Logs a WARN if no pending registration or expected orphan matches.
func (d *CommandDispatcher) Dispatch(res SignalRCommandResponse) {
	obsID := ObservationID(res.ID)

	d.mu.Lock()
	chTick, tickOk := d.pendingTicks[res.Ticks]
	chID, idOk := d.pendingByID[obsID]
	d.mu.Unlock()

	if tickOk {
		chTick <- res
		return
	}

	if idOk {
		chID <- res
		return
	}

	if d.CancelOrphan(obsID) {
		return
	}

	d.log.WARN.Printf("rogue CommandResponse: charger %s ObservationID=%s Ticks=%d "+
		"(accepted=%v, resultCode=%d) which was not triggered by evcc — "+
		"another system may be controlling this charger",
		res.SerialNumber, obsID, res.Ticks, res.WasAccepted, res.ResultCode)
}

// ExpectOrphan pre-registers one expected CommandResponse per id for a
// sync (HTTP 200) endpoint that still produces a CommandResponse on the wire.
// Must be called before Send to avoid a race with the arriving CommandResponse.
func (d *CommandDispatcher) ExpectOrphan(ids ...ObservationID) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, id := range ids {
		d.expectedOrphans[id]++
	}
}

// CancelOrphan decrements the expected-orphan counter for id.
// Returns true if a counter entry was consumed, false if none existed.
// Used by call sites to undo an ExpectOrphan registration when the POST fails.
func (d *CommandDispatcher) CancelOrphan(id ObservationID) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.expectedOrphans[id] > 0 {
		d.expectedOrphans[id]--
		return true
	}
	return false
}

// Send is a placeholder — implemented in Task 4.
func (d *CommandDispatcher) Send(uri string, data any) error {
	panic("not implemented")
}

// ensure unused imports don't break compilation during incremental implementation
var _ = fmt.Sprintf
var _ = json.NewDecoder
var _ = strings.Contains
var _ = api.ErrTimeout
```

**Step 2: Run the Task 1 tests — they must pass now**

```
go test ./charger/easee/ -run TestDispatcher -v
```
Expected: all four tests PASS (Send tests don't exist yet).

**Step 3: Commit**

```bash
git add charger/easee/dispatcher.go charger/easee/dispatcher_test.go
git commit -m "feat(easee): add CommandDispatcher with Dispatch/ExpectOrphan/CancelOrphan"
```

---

### Task 3: Add failing Send tests to dispatcher_test.go

**Files:**
- Modify: `charger/easee/dispatcher_test.go`

**Step 1: Append Send tests after the existing ones**

```go
// --- Send tests ---

func TestDispatcher_Send_Sync200(t *testing.T) {
	d := newTestDispatcher(t)
	httpmock.ActivateNonDefault(d.helper.Client)
	defer httpmock.DeactivateAndReset()

	uri := API + "/chargers/TEST/settings"
	httpmock.RegisterResponder(http.MethodPost, uri,
		httpmock.NewStringResponder(200, ""))

	assert.NoError(t, d.Send(uri, nil))
}

func TestDispatcher_Send_Noop(t *testing.T) {
	d := newTestDispatcher(t)
	httpmock.ActivateNonDefault(d.helper.Client)
	defer httpmock.DeactivateAndReset()

	// Empty array body → Ticks == 0 → noop
	uri := API + "/chargers/TEST/settings"
	httpmock.RegisterResponder(http.MethodPost, uri,
		httpmock.NewStringResponder(202, "[]"))

	assert.NoError(t, d.Send(uri, nil))
}

func TestDispatcher_Send_HTTPError(t *testing.T) {
	d := newTestDispatcher(t)
	httpmock.ActivateNonDefault(d.helper.Client)
	defer httpmock.DeactivateAndReset()

	uri := API + "/chargers/TEST/settings"
	httpmock.RegisterResponder(http.MethodPost, uri,
		httpmock.NewStringResponder(400, ""))

	assert.EqualError(t, d.Send(uri, nil), "invalid status: 400")
}

func TestDispatcher_Send_AsyncTicksMatch(t *testing.T) {
	const ticks int64 = 638798974487432600

	d := newTestDispatcher(t)
	httpmock.ActivateNonDefault(d.helper.Client)
	defer httpmock.DeactivateAndReset()

	uri := API + "/chargers/TEST/settings"
	body := fmt.Sprintf(`[{"ticks":%d,"commandId":48}]`, ticks)
	httpmock.RegisterResponder(http.MethodPost, uri,
		httpmock.NewStringResponder(202, body))

	go func() {
		for {
			d.mu.Lock()
			_, ok := d.pendingTicks[ticks]
			d.mu.Unlock()
			if ok {
				break
			}
			time.Sleep(time.Millisecond)
		}
		d.Dispatch(SignalRCommandResponse{Ticks: ticks, WasAccepted: true})
	}()

	assert.NoError(t, d.Send(uri, nil))
}

func TestDispatcher_Send_AsyncIDFallback(t *testing.T) {
	const ticks int64 = 638798974487432600
	const wrongTicks int64 = 111111111
	const cmdID = 48

	d := newTestDispatcher(t)
	httpmock.ActivateNonDefault(d.helper.Client)
	defer httpmock.DeactivateAndReset()

	uri := API + "/chargers/TEST/settings"
	body := fmt.Sprintf(`[{"ticks":%d,"commandId":%d}]`, ticks, cmdID)
	httpmock.RegisterResponder(http.MethodPost, uri,
		httpmock.NewStringResponder(202, body))

	go func() {
		for {
			d.mu.Lock()
			_, ok := d.pendingByID[ObservationID(cmdID)]
			d.mu.Unlock()
			if ok {
				break
			}
			time.Sleep(time.Millisecond)
		}
		// Wrong ticks, matching ObservationID — triggers the ID fallback path
		d.Dispatch(SignalRCommandResponse{Ticks: wrongTicks, ID: cmdID, WasAccepted: true})
	}()

	assert.NoError(t, d.Send(uri, nil))
}

func TestDispatcher_Send_CommandEndpoint_AsyncTicksMatch(t *testing.T) {
	const ticks int64 = 638798974487432600

	d := newTestDispatcher(t)
	httpmock.ActivateNonDefault(d.helper.Client)
	defer httpmock.DeactivateAndReset()

	// /commands/ endpoint → body is a JSON object, not an array
	uri := API + "/chargers/TEST/commands/resume_charging"
	body := fmt.Sprintf(`{"device":"TEST","commandId":48,"ticks":%d}`, ticks)
	httpmock.RegisterResponder(http.MethodPost, uri,
		httpmock.NewStringResponder(202, body))

	go func() {
		for {
			d.mu.Lock()
			_, ok := d.pendingTicks[ticks]
			d.mu.Unlock()
			if ok {
				break
			}
			time.Sleep(time.Millisecond)
		}
		d.Dispatch(SignalRCommandResponse{Ticks: ticks, WasAccepted: true})
	}()

	assert.NoError(t, d.Send(uri, nil))
}

func TestDispatcher_Send_Timeout(t *testing.T) {
	const ticks int64 = 789

	d := newTestDispatcher(t)
	httpmock.ActivateNonDefault(d.helper.Client)
	defer httpmock.DeactivateAndReset()

	uri := API + "/chargers/TEST/settings"
	body := fmt.Sprintf(`[{"ticks":%d}]`, ticks)
	httpmock.RegisterResponder(http.MethodPost, uri,
		httpmock.NewStringResponder(202, body))

	// No Dispatch call → Send times out
	assert.ErrorIs(t, d.Send(uri, nil), api.ErrTimeout)
}

func TestDispatcher_Send_Rejected(t *testing.T) {
	const ticks int64 = 456

	d := newTestDispatcher(t)
	httpmock.ActivateNonDefault(d.helper.Client)
	defer httpmock.DeactivateAndReset()

	uri := API + "/chargers/TEST/settings"
	body := fmt.Sprintf(`[{"ticks":%d}]`, ticks)
	httpmock.RegisterResponder(http.MethodPost, uri,
		httpmock.NewStringResponder(202, body))

	go func() {
		for {
			d.mu.Lock()
			_, ok := d.pendingTicks[ticks]
			d.mu.Unlock()
			if ok {
				break
			}
			time.Sleep(time.Millisecond)
		}
		d.Dispatch(SignalRCommandResponse{Ticks: ticks, WasAccepted: false})
	}()

	assert.EqualError(t, d.Send(uri, nil), fmt.Sprintf("command rejected: %d", ticks))
}
```

**Step 2: Run Send tests to confirm they fail (panic)**

```
go test ./charger/easee/ -run TestDispatcher_Send -v
```
Expected: FAIL — panics from the Send placeholder.

---

### Task 4: Implement Send in dispatcher.go

**Files:**
- Modify: `charger/easee/dispatcher.go`

**Step 1: Replace the Send placeholder with the real implementation**

Find and replace the `Send` placeholder and unused-import shims with:

```go
// Send posts to uri with data, parses the Easee-specific response body, and
// if the response is asynchronous (HTTP 202), waits for the matching SignalR
// CommandResponse.
//
// Returns nil on success (both synchronous HTTP 200 and confirmed async HTTP 202,
// including noops where Ticks == 0). Returns an error on HTTP failure, decode
// failure, command rejection, or timeout.
func (d *CommandDispatcher) Send(uri string, data any) error {
	resp, err := d.helper.Post(uri, request.JSONContent, request.MarshalJSON(data))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 200 {
		return nil
	}

	if resp.StatusCode == 202 {
		var cmd RestCommandResponse

		if strings.Contains(uri, "/commands/") {
			if err := json.NewDecoder(resp.Body).Decode(&cmd); err != nil {
				return err
			}
		} else {
			var cmdArr []RestCommandResponse
			if err := json.NewDecoder(resp.Body).Decode(&cmdArr); err != nil {
				return err
			}
			if len(cmdArr) != 0 {
				cmd = cmdArr[0]
			}
		}

		if cmd.Ticks == 0 {
			return nil // noop: API considers value already set
		}

		ch := make(chan SignalRCommandResponse, 1)
		obsID := ObservationID(cmd.CommandId)

		d.mu.Lock()
		d.pendingTicks[cmd.Ticks] = ch
		d.pendingByID[obsID] = ch
		d.mu.Unlock()

		defer func() {
			d.mu.Lock()
			delete(d.pendingTicks, cmd.Ticks)
			delete(d.pendingByID, obsID)
			d.mu.Unlock()
		}()

		select {
		case res := <-ch:
			if !res.WasAccepted {
				return fmt.Errorf("command rejected: %d", res.Ticks)
			}
			return nil
		case <-time.After(d.timeout):
			return api.ErrTimeout
		}
	}

	return fmt.Errorf("invalid status: %d", resp.StatusCode)
}
```

Also remove the unused-import shim lines (`var _ = fmt.Sprintf` etc.) — they are no longer needed.

**Step 2: Run all dispatcher tests**

```
go test ./charger/easee/ -run TestDispatcher -v
```
Expected: all tests PASS.

**Step 3: Verify the whole package still compiles**

```
go build ./charger/...
```
Expected: no errors.

**Step 4: Commit**

```bash
git add charger/easee/dispatcher.go charger/easee/dispatcher_test.go
git commit -m "feat(easee): implement CommandDispatcher.Send"
```

---

### Task 5: Add dispatcher field to Easee struct and NewEasee

**Files:**
- Modify: `charger/easee.go`

At this point the old correlation fields stay — we add the dispatcher alongside them. They co-exist until Task 11.

**Step 1: Add `dispatcher` field to the `Easee` struct** (after `expectedOrphans`, before `obsC`)

```go
// Add this line after expectedOrphans:
dispatcher *easee.CommandDispatcher
```

**Step 2: Initialise `dispatcher` in `NewEasee`** — add after `c.Client.Transport = ...`

```go
c.dispatcher = easee.NewCommandDispatcher(c.Helper, log, timeout)
```

**Step 3: Verify compilation**

```
go build ./charger/...
```
Expected: no errors.

**Step 4: Commit**

```bash
git add charger/easee.go
git commit -m "feat(easee): add CommandDispatcher field to Easee struct"
```

---

### Task 6: Migrate CommandResponse handler to use dispatcher.Dispatch

**Files:**
- Modify: `charger/easee.go`

**Step 1: Replace the body of `CommandResponse`**

Find the `CommandResponse` method (currently ~25 lines, lines 426–461) and replace its body with:

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

The old routing logic (map lookups, `consumeExpectedOrphan`, WARN log) is now inside `dispatcher.Dispatch`. The old code can remain (it won't be called because `Dispatch` handles everything), but removing the dead body here avoids confusion.

**Step 2: Verify build and tests**

```
go build ./charger/...
go test ./charger/ -run TestEasee -count=1 -v
```
Expected: build passes; existing tests pass.

**Step 3: Commit**

```bash
git add charger/easee.go
git commit -m "refactor(easee): delegate CommandResponse to dispatcher.Dispatch"
```

---

### Task 7: Migrate Enable to use dispatcher.Send

**Files:**
- Modify: `charger/easee.go`

**Step 1: Replace both `postJSONAndWait` calls in `Enable`**

First call (enabling the charger, around line 521):
```go
// Before:
if _, err := c.postJSONAndWait(uri, data); err != nil {

// After:
if err := c.dispatcher.Send(uri, data); err != nil {
```

Second call (pause/resume command, around line 543):
```go
// Before:
if _, err := c.postJSONAndWait(uri, nil); err != nil {

// After:
if err := c.dispatcher.Send(uri, nil); err != nil {
```

**Step 2: Verify build and tests**

```
go build ./charger/...
go test ./charger/ -run TestEasee -count=1 -v
```
Expected: build passes; all tests pass.

**Step 3: Commit**

```bash
git add charger/easee.go
git commit -m "refactor(easee): migrate Enable to use dispatcher.Send"
```

---

### Task 8: Migrate updateSmartCharging to use dispatcher.Send

**Files:**
- Modify: `charger/easee.go`

**Step 1: Replace the `postJSONAndWait` call in `updateSmartCharging`** (around line 921)

```go
// Before:
if _, err := c.postJSONAndWait(uri, data); err != nil {

// After:
if err := c.dispatcher.Send(uri, data); err != nil {
```

**Step 2: Verify build and tests**

```
go build ./charger/...
go test ./charger/ -run TestEasee -count=1 -v
```
Expected: build passes; all tests pass.

**Step 3: Commit**

```bash
git add charger/easee.go
git commit -m "refactor(easee): migrate updateSmartCharging to use dispatcher.Send"
```

---

### Task 9: Migrate Phases1p3p to use dispatcher.Send/ExpectOrphan/CancelOrphan

**Files:**
- Modify: `charger/easee.go`

**Step 1: Replace the circuit branch of `Phases1p3p`** (around lines 823–826)

```go
// Before:
c.registerExpectedOrphan(easee.CIRCUIT_MAX_CURRENT_P1)
if _, err = c.postJSONAndWait(uri, data); err != nil {
    c.consumeExpectedOrphan(easee.CIRCUIT_MAX_CURRENT_P1)
}

// After:
c.dispatcher.ExpectOrphan(easee.CIRCUIT_MAX_CURRENT_P1)
if err = c.dispatcher.Send(uri, data); err != nil {
    c.dispatcher.CancelOrphan(easee.CIRCUIT_MAX_CURRENT_P1)
}
```

**Step 2: Replace the charger branch of `Phases1p3p`** (around line 841)

```go
// Before:
if _, err = c.postJSONAndWait(uri, data); err != nil {

// After:
if err = c.dispatcher.Send(uri, data); err != nil {
```

**Step 3: Verify build and tests**

```
go build ./charger/...
go test ./charger/ -run TestEasee -count=1 -v
```
Expected: build passes; all tests pass.

**Step 4: Commit**

```bash
git add charger/easee.go
git commit -m "refactor(easee): migrate Phases1p3p to use dispatcher"
```

---

### Task 10: Migrate MaxCurrent to dispatcher.Send + fix bug

**Files:**
- Modify: `charger/easee.go`

**Step 1: Replace the `postJSONAndWait` call and noop guard in `MaxCurrent`** (lines 714–723)

```go
// Before:
noop, err := c.postJSONAndWait(uri, data)
if err != nil {
    return err
}

if !noop {
    if err := c.waitForDynamicChargerCurrent(float64(current)); err != nil {
        return err
    }
}

// After:
if err := c.dispatcher.Send(uri, data); err != nil {
    return err
}

// Pass `cur` (the capped value that was actually posted) not `float64(current)`.
// If the API returned noop (Ticks==0, value already set), dynamicChargerCurrent
// already equals cur and waitForDynamicChargerCurrent returns immediately.
// Passing float64(current) instead would cause a spurious timeout when capping applies.
if err := c.waitForDynamicChargerCurrent(cur); err != nil {
    return err
}
```

**Step 2: Verify build and tests**

```
go build ./charger/...
go test ./charger/ -run TestEasee_MaxCurrent -count=1 -v
go test ./charger/ -run TestEasee -count=1 -v
```
Expected: all tests pass.

**Step 3: Commit**

```bash
git add charger/easee.go
git commit -m "fix(easee): pass capped current to waitForDynamicChargerCurrent in MaxCurrent"
```

---

### Task 11: Delete dead code from easee.go and clean up imports

**Files:**
- Modify: `charger/easee.go`

**Step 1: Remove the four correlation fields from the `Easee` struct**

Delete these lines from the struct body:
```go
cmdMu           sync.Mutex
pendingTicks    map[int64]chan easee.SignalRCommandResponse
pendingByID     map[easee.ObservationID]chan easee.SignalRCommandResponse
expectedOrphans map[easee.ObservationID]int
```

**Step 2: Remove the three map initializations from `NewEasee`**

Delete these lines from the struct literal in `NewEasee`:
```go
pendingTicks:    make(map[int64]chan easee.SignalRCommandResponse),
pendingByID:     make(map[easee.ObservationID]chan easee.SignalRCommandResponse),
expectedOrphans: make(map[easee.ObservationID]int),
```

**Step 3: Delete the eight dead methods**

Delete the entire bodies of:
- `func (c *Easee) registerPendingTick(...)`
- `func (c *Easee) unregisterPendingTick(...)`
- `func (c *Easee) registerPendingByID(...)`
- `func (c *Easee) unregisterPendingByID(...)`
- `func (c *Easee) registerExpectedOrphan(...)`
- `func (c *Easee) consumeExpectedOrphan(...) bool`
- `func (c *Easee) waitForTickResponse(...) error`
- `func (c *Easee) postJSONAndWait(...) (bool, error)`

**Step 4: Remove unused imports from easee.go**

`strings` was only used by `postJSONAndWait`. Remove it from the import block.
`sync` is still used by `sync.RWMutex` and `sync.OnceFunc` — keep it.

**Step 5: Verify build and run LSP diagnostics**

```
go build ./charger/...
go vet ./charger/...
```
Expected: no errors, no warnings.

**Step 6: Run all tests**

```
go test ./charger/... -count=1 -v 2>&1 | grep -E "^(=== RUN|--- PASS|--- FAIL|FAIL|ok)"
```
Expected: all tests pass. (Several tests will fail until Task 12 — that's expected at this intermediate step.)

**Step 7: Commit**

```bash
git add charger/easee.go
git commit -m "refactor(easee): delete dead correlation code from easee.go"
```

---

### Task 12: Update easee_test.go — retire obsolete tests, fix remaining compilation

**Files:**
- Modify: `charger/easee_test.go`

**Step 1: Update `newEasee()` helper**

Remove `pendingTicks`, `pendingByID`, `expectedOrphans` from the struct literal and add `dispatcher`:

```go
func newEasee() *Easee {
	log := util.NewLogger("easee")
	helper := request.NewHelper(log)
	e := Easee{
		Helper:     helper,
		obsTime:    make(map[easee.ObservationID]time.Time),
		log:        log,
		startDone:  func() {},
		obsC:       make(chan easee.Observation),
		dispatcher: easee.NewCommandDispatcher(helper, log, 500*time.Millisecond),
	}
	helper.Client.Timeout = 500 * time.Millisecond
	return &e
}
```

**Step 2: Delete these entire test functions** (coverage moves to `dispatcher_test.go`)

- `TestEasee_waitForTickResponse`
- `TestEasee_postJsonAndWait`
- `TestEasee_CommandResponse_rogue`
- `TestEasee_CommandResponse_legitimate`
- `TestEasee_CommandResponse_expectedOrphan`
- `TestEasee_CommandResponse_rogueAfterOrphanConsumed`
- `TestEasee_CommandResponse_matchedByID`
- `TestEasee_registerAndConsumeExpectedOrphan`
- `TestEasee_registerExpectedOrphan_multipleRegistrations`

**Step 3: Adapt `TestEasee_Phases1p3p_registersExpectedOrphan`**

Replace the final assertion that accessed `e.cmdMu` / `e.expectedOrphans` directly:

```go
// Before:
e.cmdMu.Lock()
count := e.expectedOrphans[easee.CIRCUIT_MAX_CURRENT_P1]
e.cmdMu.Unlock()
assert.Equal(t, 1, count, "expected orphan should be registered before the POST")

// After:
// The orphan counter is inside the dispatcher. We verify it was registered
// by canceling it — CancelOrphan returns true only if the counter was ≥ 1.
assert.True(t, e.dispatcher.CancelOrphan(easee.CIRCUIT_MAX_CURRENT_P1),
    "expected orphan should be registered before the POST")
```

**Step 4: Remove unused imports from easee_test.go**

After deleting the retired tests, check whether any imports are now unused (`fmt`, `require`, etc.) and remove them. Run `go build ./charger/` to surface unused imports.

**Step 5: Run all tests**

```
go test ./charger/... -count=1 -v 2>&1 | grep -E "^(=== RUN|--- PASS|--- FAIL|FAIL|ok)"
```
Expected: all tests pass.

**Step 6: Run the full dispatcher test suite one more time for confidence**

```
go test ./charger/easee/ -count=1 -v
go test ./charger/ -count=1 -v
```
Expected: all tests pass.

**Step 7: Commit**

```bash
git add charger/easee_test.go
git commit -m "test(easee): retire obsolete tests, adapt Phases1p3p test to use dispatcher"
```

---

## Completion Checklist

- [ ] `charger/easee/dispatcher.go` exists and compiles
- [ ] `charger/easee/dispatcher_test.go` passes all 12 dispatcher tests
- [ ] `charger/easee.go` has no `cmdMu`, `pendingTicks`, `pendingByID`, `expectedOrphans` fields
- [ ] `charger/easee.go` has no `registerPendingTick`, `unregisterPendingTick`, `registerPendingByID`, `unregisterPendingByID`, `registerExpectedOrphan`, `consumeExpectedOrphan`, `waitForTickResponse`, `postJSONAndWait` methods
- [ ] `MaxCurrent` passes `cur` (not `float64(current)`) to `waitForDynamicChargerCurrent`
- [ ] `CommandResponse` handler is ≤ 10 lines, delegates to `c.dispatcher.Dispatch`
- [ ] `charger/easee_test.go` compiles, all remaining tests pass
- [ ] `go test ./charger/... -count=1` passes with zero failures
