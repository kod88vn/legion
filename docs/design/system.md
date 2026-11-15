# System-agent harness (package S)

Branch `feat/system`. Standard library only; Windows Python 3.12 and WSL python3. Nothing here starts anything.
Tests: `python -m unittest discover -s tests/system -v` from the repo root (fake clock, temp data dirs, no server).

## Files and markers (all runtime files in `legion/data/`, git-ignored ones are never committed)
| file | writer | meaning |
|---|---|---|
| `stack.up` | `tools/system/stackctl.py up` | the owner said the stack should be up; the gate and sentinel then expect heartbeats |
| `stack.hold` | `stackctl.py down` | the owner stopped the stack; `keeper.sh` does nothing while it exists |
| `sentinel.heartbeat`, `partycheck.heartbeat`, `bugq-watch.heartbeat` | the scripts | content = epoch, written each pass |
| `dispatch.heartbeat` (aliases `dispatcher.heartbeat`, `puppet.heartbeat`) | package P's dispatcher | **name is an assumption; P must write one of these** |
| `bugqueue.jsonl` | `bugq.py`, P's `legionctl bug/neo`, sentinel, partycheck | the bug queue |
| `neo.wake` | P's `legionctl neo` | mtime = a Neo request arrived |
| `deploys.jsonl`, `rathena_recreates.jsonl` | deploy scripts (via `deploylog.py`) | records |
| `mindgate.state.json`, `bugq.watch.json` | gate / watcher | "already reported" state |
| `meter.jsonl` | engine (package M) | hourly cohort rows, read by `league.py --compare-deploy` |

## Bug queue (`tools/system/bugq.py`)
Append-only JSONL: `id, t, from, priority, text, evidence, status` plus optional `key, count, bots, metric, impact, assigned_to,
note, woke`. A later line with an existing id is an update that merges over earlier lines (safe for several writers).
Commands: `add`, `list`, `rank`, `show`, `close`, `assign`, `dedupe`, `summary`, `watch`.
Rank: outage, then Neo (`from neo`, priority `neo`, or text starting NEO), then the owner's items, then
`weight x bots x (1 + 0.25 log2(count))` (weight from `impact` or priority: high 3, normal 2, low 1), oldest first.
Dedupe key: explicit `key`, else `from|normalized text` (digits collapsed). `add` on an open entry with the same key bumps
its count (throttle: `bump_min_secs`); `dedupe` merges existing duplicates (e.g. from P's writers).
`watch` exits (exit 0, which wakes the main session) only for: an open outage entry not yet woken, a Neo entry not yet woken
or a newer `neo.wake`, or planning time (last deploy + 3h - 20 min, once per deploy). It prints the reasons and
"WHAT TO ASSIGN" (top 8 open items). Woken entries get `woke: true` so a restart does not wake again.

## Session gate (`tools/mindgate.py`)
Silent when `tools/mindgate.paused` exists, when `stack.up` is missing or younger than 3 min, or when both heartbeats are
fresh (dispatcher 5 min, sentinel 7 min). Otherwise one message per distinct problem set (re-sent after 60 min):
`prompt` injects context; `stop` blocks once (never when `stop_hook_active`). Any exception: exit 0, no output.

## Sentinel (`tools/sentinel.py`)
15-minds check removed. Added: dispatcher heartbeat (only while `stack.up`), `/api/overview` latency, meter checks from
`/api/meter` (fighting+searching share >= 85%, stuck share <= 5%, deaths per bot-hour <= 0.5; the old state-sample farming
check is used only when the meter has no data), `sentinel.heartbeat` each cycle. Every problem past its grace time is filed
into the queue (deduped, key `sentinel:<key>`); only `OUTAGE_KEYS` (`legion_api`, `population`, `ayla`) print and exit 2.
Mute still works. All other checks unchanged.

## Deploy guard (`tools/deploy/guard.sh` + `tools/system/deploylog.py`)
- `dep3.sh`, `depcore.sh`, `depcore_nobuild.sh` take `--critical "<reason>"` and repeatable `--fix "<what>|<metric>|<cohort>"`.
  A regular deploy within 3 hours of the last record, or without any `--fix`, exits 5 with nothing built or deployed.
  `--critical` needs a reason and skips both checks. The core step with the same image tag as the last record passes.
  `DRY_RUN=1` reports "would be refused" and continues. Each successful deploy appends `{t, epoch, tag, services, critical,
  reason, fixes[{text, metric, cohort}]}` to `deploys.jsonl` (the core record copies the fixes of its tag).
- `rathena_recreate.sh`: a real run (`--yes`) is refused with exit 5 if a recreate was recorded in the last hour; plan-only
  runs only report. Owner/emergency override: `RATHENA_HOUR_OVERRIDE="<reason>"` (logged). The record is written just before
  the recreate. All old guards (guard_core, `--no-deps`, image pinning, Ayla wait) are untouched.
- `keeper.sh`: does nothing (no docker call) while `stack.hold` exists; logs once.
- `LEGION_FAKE_NOW` pins the clock for tests.

## Analyst (`tools/system/league.py`, `meterlib.py`)
`league.py` reads `/api/meter` and `/api/meter/neo` (or `--input/--neo-input` files), ranks cohorts by
`n x mean relative gap to par` (par = p75), shows prize = (par - mean) x n per metric, and for the top cohorts the time
buckets above their target share with an estimated kpm gain (`kpm x excess / fighting share`). Neo section: every row
flagged below par. Output `docs/reports/league-<time>.md` (`--stdout` to print).
`--compare-deploy` takes the last deploy (first record of the latest tag) and for each fix compares the n-weighted mean of its
metric over the 3 h before vs after in `meter.jsonl` for cohorts whose key contains the fix's cohort: KEEP (>= +2% good),
REVERT (<= -2%), INCONCLUSIVE (kept, remeasured), NO DATA. `meterlib.py` accepts several spellings because
`docs/design/meter.md` was not merged yet; adjust its `cohort()`/`stat()`/`shares()` when the real shape is known.

## Party check (`tools/system/partycheck.py`)
Reads `/api/parties/detail` (Ayla's party) and `/api/events`. Rules: apart > 20 cells for 4 min (excused during errands or town
runs), idle states 3 min, 2 deaths in 20 min, wrong save town for the hunting map (`HUNT_TOWN` table, extend as needed), party
sitting 5 min, 4+ map flips between two maps in 15 min. `--once` treats every duration as met; loop mode keeps timers.
Violations are filed to the queue (source `partycheck`, key `partycheck:<kind>:<name>`, count bumps at most every 15 min).

## Agents and skill
`.claude/agents/`: fixer (Sonnet), hard-fixer (opus), deploy (Sonnet), watcher (Haiku), qa (Haiku), analyst-review (Sonnet).
The four old mind agent files (`legion-chat`, `-guild`, `-life`, `-market`) and their skills stay untouched: package P's role
briefs reuse their in-world realism text; they are no longer started by the session. `legion-session` is rewritten for
the new startup order; the old text is `legion-session-old`. The `.github/` mirrors were not updated (they still describe
the old setup).
