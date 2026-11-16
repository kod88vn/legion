# Review of dd1ea2f2aafefbbf20fdc2febbe6ce2a46baf39f (fix/bugq-id-guard, base 91b54a2)

Files: tools/system/bugq.py, tests/system/test_bugq_ids.py.

## Checked
- Callers of update/close/assign across the repo: tools/system/aylalog.py:422 (id from `i, new = bugq.add(...)`),
  tools/system/watchdog.py:362 (id stored from `i, _ = bugq.add(...)`), bugq.py internals (ids from load(), which skips
  falsy ids and uses file ids), the CLI (`a.id`, a string), tests/system/test_system.py (string ids). Sentinel, partycheck,
  dashserve and tools/puppet only add/load/read; none call update/close/assign. No caller passes a tuple, so the guard
  cannot break an existing caller.
- CLI add/assign/close/show on a temp LEGION_DATA copy works; the live queue was not touched (no data file read or written).
- Tests: test_bugq_ids.py 5/5 pass; test_system.py OK; tools/puppet/tests/test_puppet.py 63/63 OK. With bugq.py reverted
  to 91b54a2 the new tests fail (3 failures, 1 error), so they prove the claim.
- The guard raises before append_jsonl, so nothing is written on refusal. No new default-on behaviour; no rule or
  constitution conflict. No effect on Ayla's party.

## Findings
- minor, tools/system/bugq.py:137-149 (check_id) and the CLI: `bugq.py assign "" X` ends in a raw traceback instead of a
  clean message. Harmless (nothing written).
- minor, bugq.py:77 load(): a legacy non-string id already in a file would now be refused by update() (e.g. dedupe/mark_woken).
  Unlikely; the live file was not inspected.
- minor: docstring says "a new item needs its own unique key" but add() itself is unchanged, so the underlying key-sharing
  hazard is documented, not prevented.

No blocker, no major.

verdict: merge
