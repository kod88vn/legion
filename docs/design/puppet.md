# Puppet-master harness (package P)

## What it is
`tools/puppet/dispatch.py` wakes one-shot Claude Code agents in print mode. Each agent gets the role brief as system
prompt and a briefing (built in Python from the engine API) on stdin, acts only through `python tools/legionctl.py`,
and exits. The world needs none of it: with every AI off the engine keeps playing the bots (autopilot directions,
template chat lines, current prices); nothing else changes.

## The exact command (per call, cwd = tools/puppet/workdir)
```
claude -p --model <haiku|sonnet> --safe-mode --tools Bash
  --allowedTools "Bash(python <repo>/tools/legionctl.py:*)" [ayla only: "Bash(bash <repo>/tools/rogui/ayla.sh:*)"]
  --disallowedTools "Bash(python <ctl> goal:*)" ... (goal, guild-goal, party, leader-party, boss-hunt, boss-cancel)
  --permission-mode dontAsk --max-budget-usd <cap> --output-format json --no-session-persistence
  --append-system-prompt "<_common.md + role brief>"        # briefing on stdin
```
Flags verified against `claude -p --help` (installed version). `--safe-mode` disables CLAUDE.md, skills, hooks, MCP, so
the repo's big rules and the mindgate hook are not loaded; the briefs carry the in-world rules. Direct goal/map
commands for bots are denied at the tool level, not only in the text.

## Schedule (defaults, `--list`)
| role | model | wake | per call | per hour |
|---|---|---|---|---|
| guild01-10 | Haiku | every 25 min, staggered 2.5 min apart | $0.06 | $0.15 each |
| market | Haiku | every 30 min | $0.08 | $0.15 |
| chat | Haiku | triggers only: new human line or queued chat job (checked every tick, 30 s; min gap 30 s) | $0.06 | $0.45 |
| build | Sonnet | on demand: owner order, `build.wake`, `--once build` (min gap 30 min) | $0.35 | $0.60 |
| ayla | Sonnet | every 5 min, plus events: death, split, goal met, member need (min gap 2 min) | $0.25 | $1.50 |

Every role also wakes on an unread owner order on the minds bus (at most every 2 min, 3 min for guilds). A role never
runs twice at once; at most 3 calls run in parallel; a role at its rolling-hour cap is skipped until old calls leave the
window (the window is read from the log, so a restart keeps it). After a restart the schedule resumes from the log
(nobody wakes at once). API down: the loop backs off 10 s up to 120 s, writes `api-down` heartbeats and never crashes.
A pending trigger (e.g. a human line while chat is busy) waits and runs as soon as the role is free.

## Expected volume (rough; measure from puppet_calls.jsonl)
Per hour: guilds 24 calls, market 2, ayla about 12 + events, chat 5-30, build 0-2. A call is about 5-6 turns, 10-15k
input tokens (mostly cache reads: the CLI's own system prompt dominates) and 1-2k output. Haiku about $0.02-0.04 per
call, Sonnet about $0.08-0.20. Rough total about $2-4 per hour with everything on; the caps above bound it at about
$5.2 per hour worst case. These are estimates: read the first live hour off the log and retune the caps.

## Briefings and feedback
Role-specific and short (max about 6,000 chars). Guild: roster, own rank and wealth, rival guilds' rank/wealth,
relations, active directions, member duty standing, the guild's two metrics, biggest cohort gaps, the build master's
latest note. Market: economy, shops, exchange counters, potions, zeny/hour by cohort. Build: cohorts furthest below par.
Chat: queued jobs and humans online. Ayla: Neo view vs par, party, Starveil directions and rank.
Feedback: the dispatcher stores a numeric snapshot per role in `notes/<role>.snap.json` at each call; the next briefing
shows "since your last call: rank 6 -> 4, wealth +100000, directions still active ..." plus the role's own last
thoughts from the minds bus and its notes file. Missing endpoints are listed ("not available right now") and skipped.

## How the owner talks to a mind
Dashboard Minds panel or `POST /api/minds/<mind>/message` (mind names: guild01-10, market, chat, build, ayla). The
dispatcher sees the unread count and wakes that mind within a tick; its first step is `legionctl orders <mind>`, it
acknowledges with a thought. A paused dispatcher (`legion/data/puppet.pause`) wakes nobody.

## Bug queue and the Neo line
`legionctl bug "<text>" [--from role]` appends `{id, t, from, priority, text, evidence, status:"open"}` (one JSON per
line) to `legion/data/bugqueue.jsonl`. `legionctl neo "<request>"` does the same with priority `neo`, attaches Ayla's
recent evidence (meter neo view, her party, her last events, her state) from the API, and writes `legion/data/neo.wake`.
Package S reads both.

## What each mind can do
guild: directions (set/get), duty pardon, rank/relations reading, words. market: shop titles and price levels, ads.
chat: chat-job answers, replies, chat intents. build: bug reports and guidance thoughts (no direct lever on builds).
ayla: her own interface `ayla.sh`, legionctl, guild-direction for Starveil, `neo`.

## Depends on engine endpoints not merged yet
meter (`/api/meter`, `/bots`, `/bot/{n}`, `/neo`), guild (`/api/guild-direction`, `guild-duty`, `guild-rank`,
`guild-relations`), exchange (`/api/exchange`, `/api/chat-jobs`, `/api/chat-jobs/{id}/lines`, `/api/chat-intent`).
Response shapes are read defensively (field guesses: `rank`, `wealth`, `standing`, `cohorts[].gap_x_n`, `totals`,
`online`, `unread`); the first live `--dry-run` should be compared with the real JSON and `briefing.py` adjusted.
Assumed, not in the brief: `guild-direction set` body fields (kind,item,map,min_lv,max_lv,classes,note,replace,hours),
`guild-duty pardon` body `{name}`, `GET /api/minds/<m>/thoughts?limit=4`, `GET /api/chat-jobs?peek=1` (must not count
as the chat agent's poll in the engine's 2-minute check) and Starveil's guild id `starveil`.

## Resolved at the integration pass (2026-11-14)
- The guessed legionctl commands were removed; the minds use the engine authors' commands: `meter ...`, `exchange`,
  `chat-jobs [list|answer|pass]`, `chat-intent <from> <need|offer> <item>`, `guild-direction [g]`,
  `guild-direction-set <g> <kind> ...`, `guild-direction-clear`, `guild-duty`, `guild-pardon`, `guild-rank`,
  `guild-relations`. Puppet keeps `bug`, `neo`, `notes`. `guild-kick` is denied to the minds (the ledger kicks).
- `briefing.py` reads the real JSON: `/api/guild-rank` `rank[]` (place, score, wealth, name), `/api/guild-relations`
  `relations[]` (a, b, value, label), `/api/guild-direction/<g>` `active[]`, `/api/guild-duty` `members[]` and
  `standings{}`, `/api/meter` `server{kpm,zph,deaths_h}` and `cohorts[].{kpm,zph}.prize`, `/api/meter/bots` (guild
  metrics by the in-game guild name), `/api/meter/neo` `party_summary`/`starveil_summary`, `/api/chat-jobs` `jobs[]`,
  `/api/exchange` `counts{}`, `/api/minds/<m>/thoughts` `thoughts[]`, `/api/guild-stock` (replaces `/api/guilds/<g>`,
  which has no GET). The guessed shapes are still read (test `test_guessed_shapes_still_read`).
- Starveil's engine id is `human-guild` (`starveil` is accepted as an alias by the guild endpoints).
- Heartbeat: the dispatcher also writes `legion/data/dispatch.heartbeat` (epoch) for the gate and the sentinel; it is
  removed on a clean stop. `stack.hold` pauses the dispatcher like `puppet.pause`.
- Chat: while the chat role can run (in the loop, under its hourly cap, not paused) the dispatcher's chat-jobs GET is a
  real poll, so the engine holds social lines for the agent; otherwise it peeks and every line goes out as its template.
  The job deadline is 20-40 s and the tick 30 s plus CLI start-up: for live chat use `--tick 10` and raise
  `chat_job_wait_min_sec`/`chat_job_wait_max_sec` (e.g. 45/75) in tuning.json, or most jobs fall back to templates.
- Ayla's interface `tools/rogui/ayla.sh` needs WSL (it runs `docker logs` in WSL and writes `/mnt/g/...` files), while
  the dispatcher runs on Windows Python and the CLI's Bash tool is Git Bash. Her allowed tool and her brief therefore use
  `wsl -d Ubuntu-24.04 -- bash /mnt/g/RO/legion/tools/rogui/ayla.sh <action>` (distro: env `LEGION_WSL_DISTRO`); a
  dispatcher started inside WSL uses plain `bash /mnt/g/...`. Needs: `wsl.exe` on PATH (it is: `C:\Windows\system32`),
  the distro's docker able to reach `ro-openkore-ayla`. The minds' `python` resolves to Python 3.12 in Git Bash.
