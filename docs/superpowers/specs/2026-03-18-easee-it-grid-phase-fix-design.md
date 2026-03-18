# Easee IT Grid Phase Control Fix

**Date:** 2026-03-18
**Status:** Approved

## Problem

For Easee chargers that are the sole occupant of a circuit (`c.circuit != 0`), evcc uses
circuit-level dynamic current settings (P1/P2/P3) for phase detection and switching. On IT
grids this breaks in two ways:

1. `GetPhases()` reads `dynamicCircuitCurrent` and returns 3, because on an IT grid all three
   circuit current limits are non-zero by default — even when the charger is running
   single-phase IT.
2. `Phases1p3p(1)` zeroes P2 and P3 in the circuit settings, which on an IT single-phase
   installation cuts the return conductor (e.g. L3 for `P1_T2_T4_IT`), briefly interrupting
   charging.

**Root cause:** The circuit-level phase control assumes TN grid semantics, where zeroing P2/P3
means "disable those phases". On IT grids, the charger uses two live conductors (e.g. L1+L3)
with no neutral — P3 is an active conductor, not an unused phase.

**Confirmed case:** Belgian installation, `DETECTED_POWER_GRID_TYPE=4` (IT3Phase),
`OUTPUT_PHASE=13` (P1_T2_T4_IT, single-phase L1+L3), `PHASE_MODE=1` (locked to 1-phase).
Discussion: https://github.com/evcc-io/evcc/discussions/27911

## Scope

Limited to the circuit-assignment logic currently inlined in `NewEasee` — the code that sets
`c.site` and `c.circuit`. The change also introduces two new items that support this logic:
`chargerConfig()` (re-added from a previous commit) and `isTNGrid()` (new). No changes to
`GetPhases()`, `Phases1p3p()`, their callers, or the multi-charger path.

## Design

### Principle

Circuit-level phase control is only engaged when we have positively confirmed the grid type is
TN. Any other outcome — API error, unknown grid type, IT grid — defaults to charger-level phase
control (`c.circuit = 0`, the Go zero value for `int`). This is the safe path: `GetPhases()`
reads `phaseMode` (correct for IT), and `Phases1p3p()` controls the charger's `PhaseMode`
setting instead of circuit currents.

### New method: `determineCircuit`

The existing inline circuit-assignment loop in `NewEasee` is extracted into a new method,
`determineCircuit(site easee.Site)`. `NewEasee` calls it after `chargerSite()`. The method is
responsible for fetching the grid type and, only when TN is confirmed, setting `c.site` and
`c.circuit`.

Extracting into its own method makes it independently testable without touching `NewEasee`'s
OAuth, SignalR, or sponsor-check machinery — consistent with how all existing tests use
`newEasee()`.

```go
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
                return  // first matching circuit wins
            }
        }
    }
}
```

**Refactor note:** The original inline code used `break` to exit the inner loop, after which the
outer loop continued iterating (without taking further action). The extracted version uses
`return` for the same net effect — a charger ID can only appear in one circuit, so the outer
loop would never produce a second match. `return` makes this "first match wins" intent explicit.

`c.circuit` and `c.site` are `int` fields, zero-valued at struct construction. `determineCircuit`
only ever sets them to non-zero values; it never resets them. The safe default is established by
Go's zero-initialisation.

### `chargerConfig` helper

Re-introduce the method that existed in `easee.go` until commit `56d13c2b4`. The
`easee.ChargerConfig` struct already exists in `charger/easee/types.go` and requires no changes
— only the method body is re-added to `easee.go`:

```go
func (c *Easee) chargerConfig(charger string) (res easee.ChargerConfig, err error) {
    uri := fmt.Sprintf("%s/chargers/%s/config", easee.API, charger)
    err = c.GetJSON(uri, &res)
    return res, err
}
```

The method takes `charger string` as a parameter rather than using `c.charger` directly,
following the same convention as `chargerSite`. `easee.API` is `"https://api.easee.com/api"`,
so the full URL is `https://api.easee.com/api/chargers/{id}/config`.

### Error handling

`chargerConfig()` is explicitly **non-fatal**. If it fails (network error, non-2xx response —
`GetJSON` returns an error on non-2xx status), `determineCircuit` returns early with
`c.circuit = 0` and logs a WARN. The charger remains fully functional on the charger-level
phase control path.

This is asymmetric with `chargerSite()` in `NewEasee`, which is fatal. `chargerConfig()` is
treated differently because its result only gates an optimisation; the charger is not broken
without it.

### Guard logic

`isTNGrid` is a package-level function in `charger/easee.go`:

```go
func isTNGrid(gridType int) bool {
    switch gridType {
    case PowerGridTN3Phase, PowerGridTN2PhasePin234, PowerGridTN1Phase:
        return true
    }
    return false
}
```

| Value | Constant               |
|-------|------------------------|
| 1     | PowerGridTN3Phase      |
| 2     | PowerGridTN2PhasePin234|
| 3     | PowerGridTN1Phase      |

Source: Easee API enumeration `detectedPowerGridType` (observation ID 21),
https://developer.easee.com/docs/enumerations

All other values — IT types, warning states, and `0` (field absent or API returns unknown) —
return `false` and fall through to the safe charger-level default. No special log entry is
emitted for non-TN values in the normal path; the WARN is reserved for when the
`chargerConfig()` call itself fails.

Named constants are added to `charger/easee/types.go` in a new, separate const block:
```go
// DetectedPowerGridType values
const (
    PowerGridTN3Phase       = 1
    PowerGridTN2PhasePin234 = 2
    PowerGridTN1Phase       = 3
)
```

## Testing

New tests in `charger/easee_test.go`, all using `newEasee()` directly — consistent with the
entire existing test suite. Tests use `httpmock` for HTTP mocking.

**Test setup requirements for `determineCircuit` tests:**
- Set `e.charger = "<someID>"` — `newEasee()` leaves `c.charger` as `""`. The charger ID must
  match the ID used in the `site` argument and the `httpmock` URL pattern, otherwise the inner
  loop never matches and `c.circuit` stays 0 regardless of grid type (silently wrong tests).
- Mock `GET https://api.easee.com/api/chargers/{id}/config`. The mock response only needs to
  populate `DetectedPowerGridType`; all other `ChargerConfig` fields can be zero. `GetJSON`
  returns an error on non-2xx status, so an HTTP 500 mock correctly triggers the error path.
- No `chargerSite()` mock is needed — `determineCircuit` receives `site` as a parameter.

**`determineCircuit` test cases (four):**

| # | Scenario | `chargerConfig` mock | `site.Circuits[0].Chargers` | Expected `c.circuit` |
|---|---|---|---|---|
| 1 | TN grid, sole charger | HTTP 200, `DetectedPowerGridType=1` | 1 charger (matching ID) | non-zero |
| 2 | IT grid, sole charger | HTTP 200, `DetectedPowerGridType=4` | 1 charger (matching ID) | 0 |
| 3 | Config fetch fails, sole charger | HTTP 500 | 1 charger (matching ID) | 0 |
| 4 | TN grid, multi-charger circuit | HTTP 200, `DetectedPowerGridType=1` | 2 chargers | 0 |

Test 3 must not emit the WARN to stdout. Prior art: PR #28036 (`dispatcher_test.go`,
`TestDispatcher_Dispatch_Rogue`) uses `util.LogLevel` to temporarily raise the global stdout
threshold, suppressing expected warnings for the duration of a test:

```go
util.LogLevel("error", nil)
t.Cleanup(func() { util.LogLevel("info", nil) })
```

`util.LogLevel` sets `OutThreshold` and calls `SetStdoutThreshold` on all cached loggers.
Restoring via `t.Cleanup` ensures the threshold is reset even if the test fails. Use the same
pattern in test 3.

Test 4: `chargerConfig` is called before the charger list is inspected, so the mock is required
even in the multi-charger case. With TN grid confirmed, the loop runs but `len > 1` causes
`continue` on every circuit — `c.circuit` stays 0.

Coverage note: IT grid + multi-charger is not tested (returns 0 via `isTNGrid` early return
rather than the `len > 1` guard — different path, same result). Left as an acknowledged gap.

**`isTNGrid` direct unit test (one):**

Since `isTNGrid` is the core of the fix, add one direct test covering the allowlist:
representative TN values (1, 2, 3) return `true`; IT values (4, 5), zero, and an arbitrary
unknown value return `false`. No HTTP mocking needed.

## Files changed

- `charger/easee.go` — extract circuit-assignment loop from `NewEasee` into
  `determineCircuit(site easee.Site)`; re-add `chargerConfig(charger string)` helper; add
  `isTNGrid(gridType int) bool` package-level function; call `c.determineCircuit(site)` from
  `NewEasee`
- `charger/easee/types.go` — add `PowerGridTN3Phase`, `PowerGridTN2PhasePin234`,
  `PowerGridTN1Phase` constants in a new `// DetectedPowerGridType values` const block
- `charger/easee_test.go` — four `determineCircuit` test cases; one `isTNGrid` unit test
