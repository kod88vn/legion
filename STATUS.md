# STATUS - read this first

Updated 2026-11-15 (main session, evening). The Constitution (`CONSTITUTION.md`, changed 2026-11-15 with the owner's
approval: market engine, farmers, price chain) is the highest law; then `RULES.md`. Details by topic: `docs/rules/`.
GitHub Copilot or any AI can cold-start from `.github/copilot-instructions.md` (pointers to this file, the
constitution, rules, HANDOVER and AGENTS).

## What runs
- **Only the game server:** `ro-db` and `ro-rathena` (rathena restarted at 15:18 with chat logging, ST07, and at
  16:24 with no rebirth, ST01/G10). The dashboard (`tools/dashboard/dashserve.py` under `keep.cmd`, port 8099) is always
  on (DB06), reads ro-db as the SELECT-only user `dash_ro`, and has the new Market tab (DB11).
- **Stopped on purpose:** the Legion engine (`legion`, `legion-core`), Ayla's OpenKore, the agency, every agent, mind and
  loop. `stack.hold` is set. Nothing starts until the owner says so, part by part (G21).
- **Do not start by yourself:** `tools/rogui/keeper.sh` and the client launcher `Play-AILab.cmd` (both start containers).
- A stray `tools/system/aylawatch.sh` loop (18 orphan processes, 00:16-08:48) was stopped; its launcher was not
  identified (bug `aylawatch:stray-loops`). Its plain-code replacement is `tools/system/aylawatch.py` (merged; not
  started; heartbeat `aylawatch.heartbeat`; reads OpenKore or engine by `engine_parties`).

## Merged into main, not yet deployed (nothing below is in a running image)
Every merge has a Sonnet review file `docs/reports/reviews/<second-parent-sha>.md` (24 older merges were reviewed
retroactively; their defects are being fixed forward). The deploy is run by `tools/deploy/deploy.py` (PR47, a script
with tests) through an assigned agent: dry run first, read by eye; the main session never runs it. A deploy gate that
refuses unreviewed merges since the last deploy is in review (`fix/deploy-gate`).
- Constitution wiring and `mainguard`; map guard; no rebirth; party, town and rally fixes; guild ledger; metrics on disk;
  watchdog; the Ayla control panel with pending-order Confirm/Decline; dashboard tabs (Players, Builds, Market).
- Ayla on the engine (packages A-E, G): override store/API, hook-in, switch `engine_parties` (OFF), Starveil on the
  engine, parity tests, watcher. **The cut-over waits for the owner's word "start Ayla on the engine"** and for the
  tooling fix in review (`fix/ayla-tooling`: partycheck and sentinel must not go blind on engine parties). Steps: stop
  `ro-openkore-ayla`, migration dry run then `--apply`, add Ayla to `LEGION_CORE`, set `engine_parties`, recreate
  `legion-core --no-deps`, verify restore and party lead hand-over; rollback = flip the switch and restore
  `human-parties.json.pre-ayla-engine`. **Ayla's party is four in total (AY11): Ayla, Crusader, Hunter, Priest.**
- Builds: gear alternatives, card and refine plan (EC40, EC41), plan realism (skill lock, 88 points), wizard plan
  (Meteor Storm 10), numbers tests (EC42).
- Market (EC43-EC48), all flags OFF: `marketengine.go` (`market_engine_on`, `guild_market_feed_on`), market metrics,
  farmer roster tool and farmer orders (`farmer_roster_on`, `farmer_orders_on`; phase 1 = 85 guild-less hunter-main
  accounts), the leader feed, dynamic pricing (`dynamic_price_on`), the crier (`market_broadcast_on`; go-live: load
  `rathena/sql/legion_notice.sql`, `docker restart ro-rathena` in a deploy window, never a compose recreate, dry run
  first). The old market direction channel is retired (410). Design: `docs/design/market-engine.md`, facts:
  `docs/design/market-engine-facts.md`.
- Tooling: test runner `tools/system/runsuites.py` (the gate), live-data audit tripwire, concurrent-safe `gocheck.sh`.

## Open branches (built; under review or being fixed)
Conservative party map rule (`fix/partycons`, PT36), market stall (`fix/market-stall-2`, reviewed: merge queued),
travel share (`fix/travel-share-2`, reviewed: merge queued), helper last-run decoding, Ayla tooling, rotation default
and sentinel, deploy gate, Ayla party of four. Old branches triaged in `docs/reports/branch-triage-20261115.md`.
The bug queue (`legion/data/bugqueue.jsonl`, `tools/system/bugq.py`): every item assigned (backlog group
`followups-batch-2`) or has a written reason.

## How to start (only on the owner's word)
- Stack: `python tools/system/stackctl.py up`, then
  `wsl -d Ubuntu-24.04 -- bash -c "cd /mnt/g/RO/legion && docker compose up -d"`; wait about 2 minutes. Recreating
  rathena also recreates Ayla's OpenKore and both Legion containers (G15).
- Stopping: `python tools/system/stackctl.py down` first (writes `stack.hold`), then loops, then `docker compose stop`.
- Tests: `python tools/system/runsuites.py` (all suites, tripwire on), `tools/dev/gocheck.sh /g/RO/legion/legion`
  (WSL; use the `/g/...` path form).

## Old documents
`HANDOVER.md` has the current wiring; `docs/handover/*` marked reference describe the OpenKore and 15-mind era (the
Ayla ones stay current until the cut-over). `RESTART-NOTES/` is history.

## Next
The owner decides what starts and when. Open owner decisions: none right now.
