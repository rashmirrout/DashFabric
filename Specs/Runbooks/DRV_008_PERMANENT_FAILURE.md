# DRV_008 — Driver Permanent Failure (Poison Goal)

**Severity:** CRITICAL
**Subsystem:** Southbound driver (`pkg/fm/driver`)
**SLO Impact:** A specific NicGoalState cannot be programmed anywhere; the affected ENI stops converging.

---

## 1. Symptoms

- Alert `fm.driver.permanent_failure_total > 0` (any value is critical).
- Logs: `driver: permanent failure; goal_hash=<h> eni=<id> code=<vendor_code>` repeated for the same hash.
- The NicActor is parked in `DRIVER_REJECT` state.
- Retries have exhausted (backoff capped at 10 s for 10 attempts per `error-handling-design.md`).

## 2. Likely Causes (ordered)

1. NicGoalState contains a vendor-illegal combination (e.g., overlapping ACL priorities, encap on unsupported port).
2. Driver-side software bug — vendor SDK rejects a previously-accepted combination after firmware upgrade.
3. Resource exhaustion on the device (TCAM full) classified as permanent by the driver.
4. Plugin (CB-Plugin) misclassified a transient as permanent (driver bug — see DRV_008 grading).

## 3. Diagnostics (read-only)

```bash
# Pull the offending goal state and its hash
fmctl nic inspect --eni=<eni_id> --include-goal-state > /tmp/poison-<eni>.pb.txt

# Last 10 driver attempts with vendor responses
fmctl driver attempts --eni=<eni_id> --tail=10

# Device-side resource counters (TCAM, route table)
fmctl driver call <eni's device_id> read.resource_stats
```

```promql
# Are multiple ENIs hitting the same vendor code?
sum by (vendor_code) (fm_driver_permanent_failure_total)
```

## 4. Remediation

**Goal:** Stop pretending. Tell the operator the input is poison; preserve the rest of the device's programmed state.

1. **Confirm classification.** Open `/tmp/poison-<eni>.pb.txt` and identify the likely offender (ACL conflict, malformed action, etc.). If unclear, page driver team.

2. **Quarantine the ENI** (NOT the device):
   `fmctl nic quarantine --eni=<eni_id> --reason="DRV_008: <vendor_code>"`
   This withdraws all programmed state for the ENI cleanly and marks the NicActor `OPERATOR_REVIEW`.
   *Rollback:* `fmctl nic unquarantine --eni=<eni_id>` (only after CB writes a fixed NicSpec).

3. **Notify the operator / CB owner** with the goal-state dump and vendor code. The CB-side fix is to write a new NicSpec.spec_revision with the offending fields corrected. FM will re-compose and re-attempt automatically.

4. **If the device-side resource is the cause (TCAM full)** the fix is capacity-driven, not config-driven — open a capacity-planning ticket and consider migrating some ENIs to another device.

5. **If misclassification is suspected** (Cause #4), file a driver bug. Workaround: `fmctl driver override --eni=<eni_id> --treat-as=transient` (audited; expires in 24 h).

## 5. Rollback

- Unquarantine without a fixed NicSpec WILL re-trigger DRV_008 — don't.
- `fmctl driver override` is the only "make it retry" knob; it auto-expires. Do not script around it.

## 6. Escalate When

- > 5 ENIs hit DRV_008 with the same vendor_code in 1 hour → either firmware regression or systemic goal-state composition bug. Page driver-team + composition owner.
- The vendor code is unknown to the driver mapping table (look in `driver-codes.yaml`) — driver team must classify before we proceed.
- DRV_008 appears immediately after a firmware upgrade — coordinate with firmware-rollback runbook (not in this set).

## 7. References

- `southbound-driver-interface-redesign.md` §Permanent vs transient classification
- `adapter-protocol-design.md` §Failure grading
- `nicgoalstate-schema-design.md` — schema; cross-check field validity
- `error-handling-design.md` DRV_008 entry
