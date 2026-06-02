# Easee: Remove Overshoot Protection Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix delayed charge start at 6A by removing overshoot protection and unifying circuit-level phase switching to use disable/enable flow.

**Architecture:** Two deletions in `charger/easee.go`: (1) remove the post-enable DCC wait and MaxCurrent reset from `Enable()`, (2) replace the DCC:7 workaround in `Phases1p3p` circuit-level with `Enable(false)` before sending circuit settings. No changes to `core/loadpoint.go` — existing 60s grace periods already suppress charger logic errors.

**Tech Stack:** Go, httpmock for tests

**Spec:** `docs/superpowers/specs/2026-06-02-easee-remove-overshoot-protection-design.md`

---

### Task 1: Update `Enable()` tests — remove DCC wait and MaxCurrent expectations

The existing tests may assert behavior that we're about to remove. Update them first so they define the new expected behavior, then make them pass.

**Files:**
- Modify: `charger/easee_test.go`

- [ ] **Step 1: Check which existing Enable tests assert DCC wait or MaxCurrent behavior**

Run:
```bash
grep -n "MaxCurrent\|DynamicChargerCurrent\|waitForDynamic\|Enable" charger/easee_test.go
```

Review the output. The tests `TestEasee_MaxCurrent` (line 246) and `TestEasee_waitForDynamicChargerCurrent` (line 179) test those functions in isolation and should remain unchanged — they still exist, just aren't called from `Enable()` anymore. No Enable-specific integration test exists that asserts the DCC wait + MaxCurrent sequence, so no test changes are needed for Task 1.

- [ ] **Step 2: Run existing tests to confirm green baseline**

Run:
```bash
go test ./charger/ -run "TestEasee" -v -count=1 2>&1 | tail -40
```

Expected: All `TestEasee*` tests pass.

- [ ] **Step 3: Commit baseline confirmation**

No code changes — this is a checkpoint to confirm the baseline is green before modifying code.

---

### Task 2: Remove overshoot protection from `Enable()`

**Files:**
- Modify: `charger/easee.go:548-559`

- [ ] **Step 1: Remove the post-`waitForChargerEnabledState` block**

In `charger/easee.go`, inside `Enable()`, replace lines 548-559:

```go
	if action == easee.ChargeStart { // ChargeStart does not mingle with DCC, no need for below operations
		return nil
	}

	if err := c.waitForDynamicChargerCurrent(targetCurrent); err != nil {
		return err
	}

	if enable {
		// reset currents after enable, as easee automatically resets to maxA
		return c.MaxCurrent(int64(c.current))
	}

	return nil
```

with:

```go
	return nil
```

This also removes the `targetCurrent` variable usage. Since `targetCurrent` is no longer read anywhere, also remove its declaration. In the `if enable` block (around line 531-537), change:

```go
	action := easee.ChargePause
	var targetCurrent float64
	if enable {
		action = easee.ChargeResume
		if opMode == easee.ModeAwaitingAuthentication && c.authorize {
			action = easee.ChargeStart
		}
		targetCurrent = 32
	}
```

to:

```go
	action := easee.ChargePause
	if enable {
		action = easee.ChargeResume
		if opMode == easee.ModeAwaitingAuthentication && c.authorize {
			action = easee.ChargeStart
		}
	}
```

- [ ] **Step 2: Run tests**

Run:
```bash
go test ./charger/ -run "TestEasee" -v -count=1 2>&1 | tail -40
```

Expected: All tests pass. No test should have been asserting the removed behavior.

- [ ] **Step 3: Run linter**

Run:
```bash
golangci-lint run ./charger/
```

Expected: Clean (no unused variable warnings for `targetCurrent`).

- [ ] **Step 4: Commit**

```bash
git add charger/easee.go
git commit -m "easee: remove overshoot protection from Enable()

Let the charger start at its default current (32A) after resume.
The loadpoint's syncCharger corrects offeredCurrent on the next
control interval. The 60s chargerSwitchDuration grace period
suppresses charger logic error warnings during the transition.

Also eliminates a latent timeout on Enable(false) where
waitForDynamicChargerCurrent(0) always timed out.

fix #30417"
```

---

### Task 3: Update `Phases1p3p` tests for new circuit-level flow

Update the two circuit-level phase switch tests to expect `Enable(false)` (ChargePause command) before circuit settings, and remove DCC:7 assertions.

**Files:**
- Modify: `charger/easee_test.go` — `TestEasee_Phases1p3p_registersExpectedOrphan` (line 428) and `TestEasee_Phases1p3p_scaleDown_resetsDCC` (line 473)

- [ ] **Step 1: Update `TestEasee_Phases1p3p_registersExpectedOrphan`**

This test verifies that an expected orphan is registered before the circuit settings POST. It currently also mocks a charger settings POST for the DCC:7 override. Update it to:
- Set `e.opMode = easee.ModeAwaitingStart` so `Enable(false)` → `waitForChargerEnabledState(false)` passes immediately
- Add an HTTP mock for the `ChargePause` command POST
- Remove the charger settings POST mock (no longer needed — DCC:7 is gone)

Replace the entire `TestEasee_Phases1p3p_registersExpectedOrphan` function with:

```go
func TestEasee_Phases1p3p_registersExpectedOrphan(t *testing.T) {
	const siteID = 12345
	const circuitID = 67890
	const chargerID = "TESTTEST"

	e := newEasee()
	e.charger = chargerID
	e.site = siteID
	e.circuit = circuitID
	e.opMode = easee.ModeAwaitingStart

	httpmock.ActivateNonDefault(e.Client)
	defer httpmock.DeactivateAndReset()

	// Mock POST pause command
	pauseURI := fmt.Sprintf("%s/chargers/%s/commands/%s", easee.API, chargerID, easee.ChargePause)
	httpmock.RegisterResponder(http.MethodPost, pauseURI,
		httpmock.NewStringResponder(200, ""))

	// Mock GET circuit settings
	getURI := fmt.Sprintf("%s/sites/%d/circuits/%d/settings", easee.API, siteID, circuitID)
	maxP1, maxP2, maxP3 := 32.0, 32.0, 32.0
	getResp := easee.CircuitSettings{
		MaxCircuitCurrentP1: &maxP1,
		MaxCircuitCurrentP2: &maxP2,
		MaxCircuitCurrentP3: &maxP3,
	}
	body, err := json.Marshal(getResp)
	require.NoError(t, err)
	httpmock.RegisterResponder(http.MethodGet, getURI,
		httpmock.NewBytesResponder(200, body))

	// Mock POST circuit settings — return 200 (sync)
	httpmock.RegisterResponder(http.MethodPost, getURI,
		httpmock.NewStringResponder(200, ""))

	err = e.Phases1p3p(1)
	assert.NoError(t, err)

	// The orphan counter should have been registered before the POST.
	// Since no CommandResponse arrived in this test, the counter stays at 1.
	// CancelOrphan returns true iff a counter entry was consumed.
	assert.True(t, e.dispatcher.CancelOrphan(easee.CIRCUIT_MAX_CURRENT_P1),
		"expected orphan should be registered before the POST")
}
```

- [ ] **Step 2: Rename and update `TestEasee_Phases1p3p_scaleDown_resetsDCC`**

This test verified that DCC:7 was sent on scale-down. Replace it with a test that verifies:
- `Enable(false)` (ChargePause) is called before circuit settings
- DCC:7 is NOT sent
- The test covers both scale-down and scale-up (both should now behave identically)

Replace the entire `TestEasee_Phases1p3p_scaleDown_resetsDCC` function with:

```go
func TestEasee_Phases1p3p_disablesBeforeCircuitSettings(t *testing.T) {
	const siteID = 12345
	const circuitID = 67890
	const chargerID = "TESTTEST"

	for _, phases := range []int{1, 3} {
		t.Run(fmt.Sprintf("%dp", phases), func(t *testing.T) {
			e := newEasee()
			e.charger = chargerID
			e.site = siteID
			e.circuit = circuitID
			e.current = 6
			e.opMode = easee.ModeAwaitingStart

			httpmock.ActivateNonDefault(e.Client)
			defer httpmock.DeactivateAndReset()

			var callOrder []string

			// Mock POST pause command — track call order
			pauseURI := fmt.Sprintf("%s/chargers/%s/commands/%s", easee.API, chargerID, easee.ChargePause)
			httpmock.RegisterResponder(http.MethodPost, pauseURI, func(req *http.Request) (*http.Response, error) {
				callOrder = append(callOrder, "pause")
				return httpmock.NewStringResponse(200, ""), nil
			})

			// Mock GET circuit settings
			getURI := fmt.Sprintf("%s/sites/%d/circuits/%d/settings", easee.API, siteID, circuitID)
			maxP1, maxP2, maxP3 := 32.0, 32.0, 32.0
			getResp := easee.CircuitSettings{
				MaxCircuitCurrentP1: &maxP1,
				MaxCircuitCurrentP2: &maxP2,
				MaxCircuitCurrentP3: &maxP3,
			}
			body, err := json.Marshal(getResp)
			require.NoError(t, err)
			httpmock.RegisterResponder(http.MethodGet, getURI,
				httpmock.NewBytesResponder(200, body))

			// Mock POST circuit settings — track call order
			httpmock.RegisterResponder(http.MethodPost, getURI, func(req *http.Request) (*http.Response, error) {
				callOrder = append(callOrder, "circuit_settings")
				return httpmock.NewStringResponse(200, ""), nil
			})

			err = e.Phases1p3p(phases)
			assert.NoError(t, err)

			// Verify pause was called before circuit settings
			assert.Equal(t, []string{"pause", "circuit_settings"}, callOrder,
				"charger must be paused before circuit settings are sent")

			// Verify no charger settings POST (DCC:7 is gone)
			chargerURI := fmt.Sprintf("%s/chargers/%s/settings", easee.API, chargerID)
			info := httpmock.GetCallCountInfo()
			assert.Equal(t, 0, info["POST "+chargerURI], "no DCC override should be sent")
		})
	}
}
```

- [ ] **Step 3: Run the updated tests**

Run:
```bash
go test ./charger/ -run "TestEasee_Phases1p3p" -v -count=1 2>&1
```

Expected: Tests fail because the implementation hasn't changed yet. The `registersExpectedOrphan` test will fail because `Phases1p3p` doesn't call `Enable(false)` yet (no mock for the pause URI will be hit, but the DCC:7 charger settings POST mock is missing). The `disablesBeforeCircuitSettings` test will fail because the call order won't match.

- [ ] **Step 4: Commit the test changes**

```bash
git add charger/easee_test.go
git commit -m "easee: update Phases1p3p tests for disable-first flow

Tests now expect Enable(false) before circuit settings and verify
no DCC:7 override is sent. Tests will fail until implementation
is updated in the next commit."
```

---

### Task 4: Update `Phases1p3p` circuit-level to disable first

**Files:**
- Modify: `charger/easee.go:736-794` (the `c.circuit != 0` branch)

- [ ] **Step 1: Add `Enable(false)` before circuit settings and remove DCC:7 block**

In `charger/easee.go`, inside `Phases1p3p()`, in the `if c.circuit != 0` branch, the current code is:

```go
	if c.circuit != 0 {
		// circuit level
		uri := fmt.Sprintf("%s/sites/%d/circuits/%d/settings", easee.API, c.site, c.circuit)

		var res easee.CircuitSettings
		if err := c.GetJSON(uri, &res); err != nil {
			return err
		}

		if res.MaxCircuitCurrentP1 == nil || res.MaxCircuitCurrentP2 == nil || res.MaxCircuitCurrentP3 == nil {
			return errors.New("MaxCircuitCurrent must not be nil")
		}

		var zero float64
		max1 := *res.MaxCircuitCurrentP1
		max2 := *res.MaxCircuitCurrentP2
		max3 := *res.MaxCircuitCurrentP3

		data := easee.CircuitSettings{
			DynamicCircuitCurrentP1: &max1,
			DynamicCircuitCurrentP2: &zero,
			DynamicCircuitCurrentP3: &zero,
		}

		if phases > 1 {
			data.DynamicCircuitCurrentP2 = &max2
			data.DynamicCircuitCurrentP3 = &max3
		}

		// Register before POST so the SignalR CommandResponse that the Easee
		// cloud sends on HTTP 200 (sync) responses is silently consumed rather
		// than logged as rogue. On error we undo the registration.
		c.dispatcher.ExpectOrphan(easee.CIRCUIT_MAX_CURRENT_P1)
		_, err = c.dispatcher.Send(uri, data)
		if err != nil {
			c.dispatcher.CancelOrphan(easee.CIRCUIT_MAX_CURRENT_P1)
		}

		// Sending DCC:7 to skip charge pause after scaling down to 1p.
		// The loadpoint's next control interval will send the real target current.
		if err == nil && phases == 1 {
			override := 7.0
			chargerData := easee.ChargerSettings{
				DynamicChargerCurrent: &override,
			}
			chargerURI := fmt.Sprintf("%s/chargers/%s/settings", easee.API, c.charger)
			noop, sendErr := c.dispatcher.Send(chargerURI, chargerData)
			if sendErr != nil {
				c.log.WARN.Printf("phase switch: failed to set charger current override: %v", sendErr)
			} else if !noop {
				if waitErr := c.waitForDynamicChargerCurrent(override); waitErr != nil {
					c.log.WARN.Printf("phase switch: charger current override confirmation timeout: %v", waitErr)
				}
			}

			c.mux.Lock()
			c.current = override
			c.mux.Unlock()
		}
```

Replace with:

```go
	if c.circuit != 0 {
		// circuit level — disable charger first, then reconfigure circuit limits.
		// The loadpoint detects the disabled state within the phaseSwitchDuration
		// window and re-enables the charger automatically.
		if err := c.Enable(false); err != nil {
			return err
		}

		uri := fmt.Sprintf("%s/sites/%d/circuits/%d/settings", easee.API, c.site, c.circuit)

		var res easee.CircuitSettings
		if err := c.GetJSON(uri, &res); err != nil {
			return err
		}

		if res.MaxCircuitCurrentP1 == nil || res.MaxCircuitCurrentP2 == nil || res.MaxCircuitCurrentP3 == nil {
			return errors.New("MaxCircuitCurrent must not be nil")
		}

		var zero float64
		max1 := *res.MaxCircuitCurrentP1
		max2 := *res.MaxCircuitCurrentP2
		max3 := *res.MaxCircuitCurrentP3

		data := easee.CircuitSettings{
			DynamicCircuitCurrentP1: &max1,
			DynamicCircuitCurrentP2: &zero,
			DynamicCircuitCurrentP3: &zero,
		}

		if phases > 1 {
			data.DynamicCircuitCurrentP2 = &max2
			data.DynamicCircuitCurrentP3 = &max3
		}

		// Register before POST so the SignalR CommandResponse that the Easee
		// cloud sends on HTTP 200 (sync) responses is silently consumed rather
		// than logged as rogue. On error we undo the registration.
		c.dispatcher.ExpectOrphan(easee.CIRCUIT_MAX_CURRENT_P1)
		_, err = c.dispatcher.Send(uri, data)
		if err != nil {
			c.dispatcher.CancelOrphan(easee.CIRCUIT_MAX_CURRENT_P1)
		}
```

- [ ] **Step 2: Run tests**

Run:
```bash
go test ./charger/ -run "TestEasee" -v -count=1 2>&1 | tail -40
```

Expected: All `TestEasee*` tests pass, including the updated `Phases1p3p` tests.

- [ ] **Step 3: Run linter**

Run:
```bash
golangci-lint run ./charger/
```

Expected: Clean.

- [ ] **Step 4: Run full charger test suite**

Run:
```bash
go test ./charger/ -count=1 2>&1 | tail -10
```

Expected: All tests pass with no regressions.

- [ ] **Step 5: Commit**

```bash
git add charger/easee.go
git commit -m "easee: disable charger before circuit-level phase switch

Replace the DCC:7 workaround with Enable(false) before sending
circuit settings, mirroring the charger-level phase switch path.
The loadpoint re-enables the charger via syncCharger when it
detects the disabled state within the phaseSwitchDuration window."
```

---

### Task 5: Format, lint, and verify

Final verification that everything is clean.

**Files:**
- Verify: `charger/easee.go`, `charger/easee_test.go`

- [ ] **Step 1: Run gofmt**

Run:
```bash
gofmt -w -l charger/easee.go charger/easee_test.go
```

Expected: No output (files already formatted) or files are reformatted.

- [ ] **Step 2: Run linter on full project**

Run:
```bash
golangci-lint run ./charger/
```

Expected: Clean.

- [ ] **Step 3: Run full test suite**

Run:
```bash
go test ./charger/ -v -count=1 2>&1 | tail -50
```

Expected: All tests pass.

- [ ] **Step 4: Commit any formatting fixes (if needed)**

```bash
git add charger/easee.go charger/easee_test.go
git commit -m "easee: format fixes"
```

Only commit if gofmt made changes in Step 1.
