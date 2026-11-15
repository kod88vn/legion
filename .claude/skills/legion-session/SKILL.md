---
name: legion-session
description: Main session of the Legion world under the Constitution - where its job is defined, what to read each turn (status, queue, dashboard, the 30-minute agent report), and the startup and stop order that an assigned agent carries out on the owner's word. Use at the start of every main session, or when the owner asks to start or stop the stack.
---

# Legion session

Read `STATUS.md` first. The old setup is in `legion-session-old` (history only).

## The main session's job
Constitution Article 1; it starts and stops nothing itself, and the harness blocks it (`tools/system/mainguard.py`).
- Assign: queue items to `fixer` (`hard-fixer` for locking, networking, deploy scripts, or a bug that failed twice); one
  fixer per item, resumed with SendMessage, never a duplicate. Before each 3-hour deploy: `analyst-review`, then the
  `deploy` agent with the batch (G17).
- Watch the state through: `python tools/system/bugq.py rank`, `python tools/system/league.py`, the dashboard
  `http://127.0.0.1:8090/dashboard` (always on, DB06), and the 30-minute report of every system agent (Constitution 3.8).
- Wake `watcher` on `partycheck` violations and for its 30-minute review; `qa` for class sampling.

## Startup order (reference for the agent the main session assigns, on the owner's word; stop in reverse)
The owner decides what starts, part by part (G21). Check what runs first: exactly one copy of each loop. Exit code 2 from a loop is its wake (restart it); exit code 3 means another copy already runs (do not restart it). The codes are set in `tools/system/common.py`.
1. **Stack**: `python tools/system/stackctl.py up` (removes `stack.hold`, writes `stack.up`), then
   `wsl -d Ubuntu-24.04 -- bash -c "cd /mnt/g/RO/legion && docker compose up -d"`; wait about 2 minutes
   (`python tools/legionctl.py status`). The first start after a folder move recreates every container; that is expected.
2. **Sentinel** (WSL, background): `wsl -d Ubuntu-24.04 -- python3 /mnt/g/RO/legion/tools/sentinel.py`. It exits only for
   an outage; restart it after an exit (`--mute KEY --mute-until HH:MM` for a key already handed to a fixer).
3. **Dispatcher** (puppet masters): `python tools/puppet/dispatch.py` in the background; its heartbeat is what the session
   gate checks.
4. **Bug-queue watcher** (background): `python tools/system/bugq.py watch`. Its exit wakes the main session, which reads
   what it printed ("what to assign"), assigns, and has it started again.
5. **Party check** (background): `python tools/system/partycheck.py`.
6. **Unwall loop** (background): `python tools/unwall.py --loop` (moves walled-in bots every 30 s).
7. After any restart or recreate, verify live state: `bash tools/system/fieldcheck.sh` (OpenKore fields), Ayla online and
   farming in her log, `python tools/system/bugq.py summary` triaged.
8. Ayla's keeper and the watch client (`tools/rogui/keeper.sh`, `ai-display.sh`, `ai-client.sh`) only when the owner wants
   her online. `keeper.sh` does nothing while `stack.hold` exists.

**Stopping:** `python tools/system/stackctl.py down` first (writes `stack.hold`, removes `stack.up`, so the keeper and the
gate stay quiet), then the loops, then `docker compose stop`.

## Gates you will see
- `mainguard.py` (PreToolUse): blocks the main session's system-changing tools; denials in `logs/constitution-denied.log`.
- `mindgate.py` (hooks): while `stack.up` exists it checks the dispatcher and sentinel heartbeats, once per problem.
  Pause: file `tools/mindgate.paused`.
- Deploy scripts: 3 hours between deploys unless `--critical "<reason>"`; `--fix "<what>|<metric>|<cohort>"` per fix;
  rathena at most once an hour (G15-G17).
- Reports: `python tools/system/league.py`, `--compare-deploy` (keep or revert the last deploy's fixes).
