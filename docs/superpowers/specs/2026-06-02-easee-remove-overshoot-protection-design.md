# Easee: Remove Overshoot Protection and Unify Phase Switch Flow

**Date:** 2026-06-02
**Issue:** [#30417](https://github.com/evcc-io/evcc/issues/30417) — Easee: delayed charge start at minA
**Supersedes:** [2026-06-01-easee-dcc7-kickstart-design.md](2026-06-01-easee-dcc7-kickstart-design.md) (PR #30419, abandoned)

## Problem

Newer Easee firmware delays ~5 minutes when charging starts at 6A. Some vehicles do not recover from this extended wait and require unplugging. The current code works around Easee's automatic DCC reset to 32A on resume by immediately re-sending the target current (`MaxCurrent(c.current)`) inside `Enable(true)`. This "overshoot protection" forces the charger to start at the low target current, triggering the firmware delay.

A prior approach (PR #30419) added DCC:7 kickstart workarounds in `Enable` and `Phases1p3p`. Feedback from the maintainer: coding around the API is a bad pattern.

## Design

Remove the overshoot protection entirely and let the charger handle its own start current. Unify the circuit-level phase switch path to use the same disable/enable flow as charger-level.

### Change 1: `Enable()` — Remove overshoot protection

**File:** `charger/easee.go`, inside `Enable()`

Delete the following after `waitForChargerEnabledState`:
- `ChargeStart` early return (line 548-550) — nothing below it to skip anymore
- `waitForDynamicChargerCurrent(targetCurrent)` (line 552-554)
- `MaxCurrent(c.current)` overshoot reset (line 556-558)

After `waitForChargerEnabledState`, simply return `nil`.

**Resulting `Enable(true)` flow:**
1. Send `ChargeResume` (or `ChargeStart` for welcome charge)
2. Wait for charger to confirm enabled state
3. Return

The charger starts at 32A. The loadpoint's existing `syncCharger()` detects the current mismatch on the next control interval (via `GetMaxCurrent`), syncs `offeredCurrent`, and sends the real target current. The 60-second `chargerSwitchDuration` grace period (stamped at `loadpoint.go:969`) suppresses charger logic error warnings during this window.

**Side benefit:** Eliminates `waitForDynamicChargerCurrent(0)` on `Enable(false)`, which always timed out because the charger does not reset DCC to 0 on pause.

### Change 2: `Phases1p3p` circuit-level — Disable first, then reconfigure

**File:** `charger/easee.go`, inside `Phases1p3p()`, the `c.circuit != 0` branch

Replace the current flow (send circuit settings → DCC:7 workaround) with:

1. `c.Enable(false)` — pause charger, wait for confirmation. Return error on failure (circuit settings are not sent if disable fails).
2. Send `DynamicCircuitCurrentP1/P2/P3` settings (existing circuit settings POST)
3. Delete the entire DCC:7 block (lines 774-793)
4. Return (the `err` from step 2)

**Resulting circuit-level phase switch flow:**
1. Charger pauses
2. Circuit current limits are reconfigured
3. Loadpoint's `syncCharger()` detects `!enabled && !phaseSwitchCompleted()` (line 867-872) and re-enables the charger
4. `Enable(true)` sends `ChargeResume`, charger starts at 32A
5. Grace periods from both `chargerSwitched` and `phasesSwitched` suppress charger logic errors
6. Next control interval sends the real target current

This mirrors the existing charger-level phase switch path, which already calls `c.Enable(false)` and relies on the loadpoint to re-enable.

### What stays unchanged

- `chargerSwitchDuration` (60s) and `phaseSwitchDuration` (60s) grace periods
- `syncCharger()` logic in `core/loadpoint.go`
- Charger-level phase switching (already uses `Enable(false)`)
- `ChargeStart` / welcome charge path (never had overshoot protection)
- `GetMaxCurrent()` — still returns `dynamicChargerCurrent` from SignalR observations

### Why not unify to charger-level phase switching only?

The Easee developer docs note that the `PhaseMode` charger setting is ["not designed to be changed often"](https://developer.easee.com/reference/charger_setchargersetting), implying flash/NVRAM writes. Circuit-level switching via `DynamicCircuitCurrentP1/P2/P3` is documented as the dynamic approach in the [Load Balancing](https://developer.easee.com/docs/load-balancing) guide. For PV surplus scenarios with frequent phase switches, circuit-level remains the right choice. The codebase already auto-selects based on `circuit != 0`.

## Affected Code Paths

| Scenario | Current behavior | New behavior |
|---|---|---|
| `Enable(true)` → `ChargeResume` | Wait for DCC=32, re-send `MaxCurrent(target)` | Return immediately, loadpoint corrects next interval |
| `Enable(true)` → `ChargeStart` | Early return (no overshoot protection) | Same — no change |
| `Enable(false)` → `ChargePause` | Waits for DCC=0 (always times out) | Returns after pause confirmation |
| `Phases1p3p` circuit-level, scale down | Send circuit settings, then DCC:7 | Disable, send circuit settings, loadpoint re-enables |
| `Phases1p3p` circuit-level, scale up | Send circuit settings (no DCC:7) | Disable, send circuit settings, loadpoint re-enables |
| `Phases1p3p` charger-level | `Enable(false)`, loadpoint re-enables | No change |

## Risks and Mitigations

**Risk:** Brief period where `offeredCurrent` = 32A appears in UI or affects power calculations.
**Mitigation:** The loadpoint syncs `offeredCurrent` on the very next control interval (typically 10-30s). The `shouldBeConsistent() = false` window prevents any corrective action based on the stale value.

**Risk:** Circuit-level phase switch now adds a pause/resume round-trip that didn't exist before.
**Mitigation:** Accepted trade-off. The extra ~1 interval delay is preferable to the DCC workarounds and the current 5-minute firmware delay at 6A.

**Risk:** Hardware race if circuit reconfiguration arrives while charger is still active.
**Mitigation:** Disable-first ordering ensures the charger is paused before circuit limits change.

## Testing

- Existing `TestEasee_Phases1p3p` — update to expect `Enable(false)` before circuit settings
- Existing `TestEasee_Enable` — update to remove expectations for DCC wait and MaxCurrent after resume
- Verify `chargerSwitchDuration` grace period suppresses charger logic errors in loadpoint tests
- Manual verification with Easee hardware: charge start at 6A, phase switch 1p↔3p
