# Review: tools/system/aylawatch.sh (untracked)

Reviewer: Sonnet. Read-only; the script was not run, committed or changed. Constitution read first.

## What it does
A bash wrapper around an inline Python snippet, looping every 300 s from `/g/RO/legion`. Each round prints one line about
Ayla's party:
- reads `openkore-ayla/logs/status.json` (map, x, y, lock_map, intent, file age);
- runs `wsl ... docker logs --since 5m ro-openkore-ayla` and counts exp gained, kills, deaths, map changes and the last two
  `[watchdog]` lines;
- GETs `http://127.0.0.1:8090/api/parties/detail` and lists the non-human members' distance to the leader.
It writes nothing: no file, no command to a character, no DB. It is a read-only snapshot for the main session.

## Who uses it
- `STATUS.md:130` says the main session watches "via tools/system/aylawatch.sh (5-min snapshot)". That is the only live
  reference.
- `tools/system/leaderboard.py:5` mentions it in a docstring (`--brief` "one line for aylawatch.sh"), but the script does
  not call `leaderboard.py`, so that link is dead.
- Past reviews (`571473b0...md`, `f11031fa...md`) note a branch that added it in 3c2e692 and removed it in ab2c630; main
  never tracked it. No code, test, doc rule or watchdog starts it.

## Superseded by ba4d6fc (tools/puppet/aylaswitch.py)?
No. `aylaswitch.py` only answers "is Ayla on the engine or OpenKore?" from the `engine_parties` tuning key and switches the
Ayla watcher's brief between its OpenKore and engine texts. It observes nothing live. The package-G watcher is an AI
agent that files bugs. `aylawatch.sh` is a different thing: a plain-code snapshot for the main session. Its inputs
(`openkore-ayla` status.json and the `ro-openkore-ayla` container) are OpenKore-era, so it becomes stale or blind once the
switch (`engine_parties: ayla`) is on, which is a real reason to replace it, but nothing has replaced it yet.

## Rules
- G01: no mouse, keyboard or client. Pass.
- G05: never sends `ai` or any command to OpenKore; only reads logs and a JSON file. Pass.
- G13 / Constitution 1.3: the script itself sends no command and changes nothing. Pass. But 1.3 also bars the main
  session from starting anything. A loop that the main session started itself would breach it, and STATUS line 130 reads
  as if the main session runs it. Author and launcher are not recorded; the owner should know who launched it.
- Constitution 1.2d: the main session "does not sit on one character's log; watching is an agent's job". A main-session
  poll of Ayla's log is the thing 1.2d warns against. Better as a watcher agent's or a plain-code watchdog's output
  (3.8), read by the main session as a report.
- G23: read-only, but it reads live running state (`status.json`, container logs, live API) by hardcoded relative paths,
  not via `tools/system/common.py`, the one resolver. As a monitor, not a test, this is allowed; it is not "touching"
  live data. Minor: it should use `common.py`.
- G12, G19, G25: not affected.
- Code notes: no timeouts on the `wsl` call (a hang stalls the loop); bare `except` hides failures as `api?` or `-1`; no
  heartbeat, so a dead loop is silent (3.8). It cannot be tested because it is untracked and has no test.

## Recommendation
Leave it untracked for now; do not commit. It has no review-worthy tracked history and it is a main-session crutch against
1.2d and 3.8. Committing would make a main-session loop official. Deleting now would blind the current watch, and 3.2/3.10
say a blind meter comes first. When a watcher agent or a plain-code watchdog with a heartbeat covers the same numbers (kills,
exp, deaths, map changes, party distance) and works after the engine switch, delete the script and fix `STATUS.md:130` and
the `leaderboard.py` docstring in the same change. Until then, the main session should not run it itself; an agent should.

verdict: do not merge
