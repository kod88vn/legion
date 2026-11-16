# Retroactive review of ee521d6 (aylalog.py, Ayla console-log reporter, PR39/AY18), merged as c43569c and e1b420c

Reviewed 2026-11-15 by a Sonnet reviewer, after the merge. Scope: merge-base 04e59f6..ee521d6 (`tools/system/aylalog.py`,
`tests/system/test_aylalog.py`); the merge diff against the first parent is mostly unrelated history. Judged against
current main (33c0b6d), where the file has since gained ONCE_KINDS, the resolver (G23) and a lock. Scratch run:
`tests/system/test_aylalog` 27 tests OK (with test_wakelimit).

## Hard gates
Read-only on Ayla's console log; writes only the bug queue and `aylalog.state.json`/heartbeat. No command to any
character, no AI toggle (G05), no zeny (G12). G23: main routes the log through `refuse_live`. OK.

## Findings that are still on main
- major, `tools/system/aylalog.py:600-610` (with `tools/system/watchdog.py:76`): the reporter has no knowledge of the
  engine switch (`engine_parties`, `tools/puppet/aylaswitch.py`). `aylawatch.py` reads it, aylalog does not. After
  Ayla moves to the engine her OpenKore log stops being written, and the watchdog keeps aylalog alive (it is in
  `AGENTS`, not `never_start`). It then files `log-silent` (high, `:144`, `:607`) every 10 minutes for good, a false
  high item that never closes and drowns real ones. Same today whenever OpenKore is down on purpose (the owner
  stopped it). Fix: when `aylaswitch.engine_on()` is true (or the log is absent and the stack is held) keep the
  heartbeat but file nothing, and say so in the heartbeat; add a test.
- major, `tools/system/aylalog.py:615` and `:487-505`: on a restart that resumes from saved state (the `fresh` branch)
  `Tracker.exp_times` is not restored (only map, cell, map_since, last_exp, last_t), so `tracker.kpm()` reads 0 for up
  to an hour. `meter_events` then files `below-par` (high when k < 0.25 par) after every restart, and the watchdog
  restarts it when stale. False "Ayla is far under par" items right when the meter is the first thing judged
  (Constitution 3.2). Fix: persist `exp_times` in the state (bounded to the last hour) or suppress below-par until
  the tracker has an hour of lines; test the resume path.
- minor, `tools/system/aylalog.py` (`backlog()` docstring says it returns four values, returns two): cosmetic.
- minor: the metric ticks (no-exp, town-stall) run on wall clock against UTC log stamps and assume the container
  clock; `--log-utc-offset` defaults to 0. Fine while both are UTC.

## Tests
Parsing, classification, window/flood limits, tail rotation and backlog seeding are tested and would fail without the
code. No test covers the resumed-state path (the below-par defect) or an absent/silent log in the engine era.

verdict: do not merge
