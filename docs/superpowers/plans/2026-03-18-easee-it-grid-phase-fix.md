# Easee IT Grid Phase Control Fix — Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix Easee charger phase detection and switching on IT grids by gating circuit-level phase control on a confirmed TN grid type.

**Architecture:** Extract the inline circuit-assignment loop from `NewEasee` into a new `determineCircuit` method; gate it with a new `isTNGrid` allowlist function that reads `DetectedPowerGridType` from the charger config API. Any non-TN outcome (IT grid, API error, unknown) leaves `c.circuit = 0`, which is Go's zero value and the pre-existing safe default for charger-level phase control.

**Tech Stack:** Go, `httpmock` (HTTP mocking in tests), `util.LogLevel` (log suppression in tests).

---

## Chunk 1: File Map + Constants

### File Map

| File | Change |
|------|--------|
| `charger/easee/types.go` | Add `PowerGridTN3Phase`, `PowerGridTN2PhasePin234`, `PowerGridTN1Phase` constants |
| `charger/easee.go` | Add `isTNGrid`, `chargerConfig`, `determineCircuit`; replace inline loop in `NewEasee` with `c.determineCircuit(site)` |
| `charger/easee_test.go` | Add `TestIsTNGrid` and `TestDetermineCircuit` |

### Task 1: Add PowerGridTN constants to `charger/easee/types.go`

**Files:**
- Modify: `charger/easee/types.go:11` (insert after the `ChargeStart/Stop/Pause/Resume` const block)

- [ ] **Step 1: Open `charger/easee/types.go` and locate the existing const blocks**

  Lines 6–11 currently hold the charge-command constants. The new block goes immediately after line 11:

  ```go
  // DetectedPowerGridType values
  const (
  	PowerGridTN3Phase       = 1
  	PowerGridTN2PhasePin234 = 2
  	PowerGridTN1Phase       = 3
  )
  ```

- [ ] **Step 2: Run `go build ./charger/easee/...` to confirm the file compiles**

  ```bash
  cd /Users/mhess/development/grimmimeloni/evcc && go build ./charger/easee/...
  ```

  Expected: no output (clean build).

- [ ] **Step 3: Commit**

  ```bash
  cd /Users/mhess/development/grimmimeloni/evcc/.worktrees/easee-it-grid-spec
  gofmt -w charger/easee/types.go
  golangci-lint run ./charger/easee/...
  git add charger/easee/types.go
  git commit -m "easee: add PowerGridTN* constants for DetectedPowerGridType"
  ```

---

## Chunk 2: `isTNGrid` — Test Then Implement

### Task 2: Write and implement `isTNGrid`

**Files:**
- Modify: `charger/easee_test.go` (append at end of file)
- Modify: `charger/easee.go` (insert after `chargerSite` at line 282)

- [ ] **Step 1: Write the failing test in `charger/easee_test.go`**

  Append at the end of the file:

  ```go
  func TestIsTNGrid(t *testing.T) {
  	// TN grid types must return true
  	assert.True(t, isTNGrid(easee.PowerGridTN3Phase))
  	assert.True(t, isTNGrid(easee.PowerGridTN2PhasePin234))
  	assert.True(t, isTNGrid(easee.PowerGridTN1Phase))

  	// IT grid types, zero, and unknown values must return false
  	assert.False(t, isTNGrid(4))  // IT3Phase
  	assert.False(t, isTNGrid(5))  // IT1Phase
  	assert.False(t, isTNGrid(0))  // absent / unknown
  	assert.False(t, isTNGrid(99)) // arbitrary unknown
  }
  ```

- [ ] **Step 2: Run the test — expect compile failure (function not yet defined)**

  ```bash
  cd /Users/mhess/development/grimmimeloni/evcc
  go test ./charger/ -run TestIsTNGrid -v 2>&1 | head -20
  ```

  Expected: compile error `undefined: isTNGrid`.

- [ ] **Step 3: Add `isTNGrid` to `charger/easee.go` after `chargerSite` (after line 282)**

  ```go
  func isTNGrid(gridType int) bool {
  	switch gridType {
  	case easee.PowerGridTN3Phase, easee.PowerGridTN2PhasePin234, easee.PowerGridTN1Phase:
  		return true
  	}
  	return false
  }
  ```

- [ ] **Step 4: Run the test — expect PASS**

  ```bash
  cd /Users/mhess/development/grimmimeloni/evcc
  go test ./charger/ -run TestIsTNGrid -v
  ```

  Expected: `PASS`.

- [ ] **Step 5: Commit**

  ```bash
  cd /Users/mhess/development/grimmimeloni/evcc/.worktrees/easee-it-grid-spec
  gofmt -w charger/easee.go charger/easee_test.go
  golangci-lint run ./charger/...
  git add charger/easee.go charger/easee_test.go
  git commit -m "easee: add isTNGrid guard function"
  ```

---

## Chunk 3: `chargerConfig` + `determineCircuit` — Test Then Implement

### Task 3: Write `determineCircuit` tests, then implement

**Files:**
- Modify: `charger/easee_test.go` (append after `TestIsTNGrid`)
- Modify: `charger/easee.go` (add `chargerConfig` and `determineCircuit` after `isTNGrid`; replace inline loop in `NewEasee`)

- [ ] **Step 1: Add `makeTestSite` helper and `TestDetermineCircuit` to `charger/easee_test.go`**

  Append immediately after `TestIsTNGrid`:

  ```go
  // makeTestSite returns a Site with a single Circuit containing the given charger IDs.
  // Site.ID = 111, Circuit.ID = 222.
  func makeTestSite(chargerIDs ...string) easee.Site {
  	chargers := make([]easee.Charger, len(chargerIDs))
  	for i, id := range chargerIDs {
  		chargers[i] = easee.Charger{ID: id}
  	}
  	return easee.Site{
  		ID: 111,
  		Circuits: []easee.Circuit{
  			{ID: 222, Chargers: chargers},
  		},
  	}
  }

  func TestDetermineCircuit(t *testing.T) {
  	const chargerID = "TESTTEST"
  	configURI := fmt.Sprintf("%s/chargers/%s/config", easee.API, chargerID)

  	tests := []struct {
  		name         string
  		httpStatus   int
  		gridType     int
  		chargerIDs   []string
  		wantCircuit  int
  		suppressWarn bool
  	}{
  		{
  			name:        "TN grid, sole charger — circuit assigned",
  			httpStatus:  200,
  			gridType:    easee.PowerGridTN3Phase,
  			chargerIDs:  []string{chargerID},
  			wantCircuit: 222,
  		},
  		{
  			name:        "IT grid, sole charger — circuit not assigned",
  			httpStatus:  200,
  			gridType:    4, // IT3Phase
  			chargerIDs:  []string{chargerID},
  			wantCircuit: 0,
  		},
  		{
  			name:         "config fetch fails — circuit not assigned",
  			httpStatus:   500,
  			chargerIDs:   []string{chargerID},
  			wantCircuit:  0,
  			suppressWarn: true,
  		},
  		{
  			name:        "TN grid, multi-charger circuit — circuit not assigned",
  			httpStatus:  200,
  			gridType:    easee.PowerGridTN3Phase,
  			chargerIDs:  []string{chargerID, "OTHER"},
  			wantCircuit: 0,
  		},
  	}

  	for _, tc := range tests {
  		t.Run(tc.name, func(t *testing.T) {
  			e := newEasee()
  			e.charger = chargerID

  			httpmock.ActivateNonDefault(e.Client)
  			defer httpmock.DeactivateAndReset()

  			if tc.httpStatus == 200 {
  				body, _ := json.Marshal(easee.ChargerConfig{DetectedPowerGridType: tc.gridType})
  				httpmock.RegisterResponder(http.MethodGet, configURI,
  					httpmock.NewBytesResponder(200, body))
  			} else {
  				httpmock.RegisterResponder(http.MethodGet, configURI,
  					httpmock.NewStringResponder(tc.httpStatus, ""))
  			}

  			if tc.suppressWarn {
  				util.LogLevel("error", nil)
  				t.Cleanup(func() { util.LogLevel("info", nil) })
  			}

  			e.determineCircuit(makeTestSite(tc.chargerIDs...))

  			assert.Equal(t, tc.wantCircuit, e.circuit)
  		})
  	}
  }
  ```

- [ ] **Step 2: Run `TestDetermineCircuit` — expect compile failure**

  ```bash
  cd /Users/mhess/development/grimmimeloni/evcc
  go test ./charger/ -run TestDetermineCircuit -v 2>&1 | head -20
  ```

  Expected: compile error `undefined: (*Easee).determineCircuit`.

- [ ] **Step 3: Add `chargerConfig` and `determineCircuit` to `charger/easee.go` (after `isTNGrid`)**

  ```go
  func (c *Easee) chargerConfig(charger string) (res easee.ChargerConfig, err error) {
  	uri := fmt.Sprintf("%s/chargers/%s/config", easee.API, charger)
  	err = c.GetJSON(uri, &res)
  	return res, err
  }

  func (c *Easee) determineCircuit(site easee.Site) {
  	config, err := c.chargerConfig(c.charger)
  	if err != nil {
  		c.log.WARN.Printf("charger config unavailable, using charger-level phase control: %v", err)
  		return
  	}
  	if !isTNGrid(config.DetectedPowerGridType) {
  		return
  	}
  	for _, circuit := range site.Circuits {
  		if len(circuit.Chargers) > 1 {
  			continue
  		}
  		for _, charger := range circuit.Chargers {
  			if charger.ID == c.charger {
  				c.site = site.ID
  				c.circuit = circuit.ID
  				return
  			}
  		}
  	}
  }
  ```

- [ ] **Step 4: Run `TestDetermineCircuit` — expect PASS**

  ```bash
  cd /Users/mhess/development/grimmimeloni/evcc
  go test ./charger/ -run TestDetermineCircuit -v
  ```

  Expected: all 4 sub-tests `PASS`.

- [ ] **Step 5: Replace the inline circuit loop in `NewEasee` with `c.determineCircuit(site)`**

  In `charger/easee.go`, replace lines 167–180 (the `// find single charger per circuit` block):

  ```go
  	// find single charger per circuit
  	for _, circuit := range site.Circuits {
  		if len(circuit.Chargers) > 1 {
  			continue
  		}

  		for _, charger := range circuit.Chargers {
  			if charger.ID == c.charger {
  				c.site = site.ID
  				c.circuit = circuit.ID
  				break
  			}
  		}
  	}
  ```

  with:

  ```go
  	c.determineCircuit(site)
  ```

- [ ] **Step 6: Run the full charger test suite — all tests must pass**

  ```bash
  cd /Users/mhess/development/grimmimeloni/evcc
  go test ./charger/ -v 2>&1 | tail -30
  ```

  Expected: all tests `PASS`, no `FAIL` lines.

- [ ] **Step 7: Run linter**

  ```bash
  cd /Users/mhess/development/grimmimeloni/evcc
  golangci-lint run ./charger/...
  ```

  Expected: no issues.

- [ ] **Step 8: Commit**

  ```bash
  cd /Users/mhess/development/grimmimeloni/evcc/.worktrees/easee-it-grid-spec
  gofmt -w charger/easee.go charger/easee_test.go
  git add charger/easee.go charger/easee_test.go
  git commit -m "easee: fix IT grid phase control via determineCircuit + isTNGrid"
  ```

---

## Summary

Three commits produce a working, tested fix:

1. `easee: add PowerGridTN* constants for DetectedPowerGridType`
2. `easee: add isTNGrid guard function`
3. `easee: fix IT grid phase control via determineCircuit + isTNGrid`

After these commits, IT grid chargers (`DetectedPowerGridType` 4 or 5) and chargers whose config endpoint is unreachable fall through to charger-level phase control (`c.circuit = 0`). Only confirmed TN grids engage circuit-level phase control, preserving the existing behavior for TN installations.
