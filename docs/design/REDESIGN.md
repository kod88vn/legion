# Redesign brief (approved by the owner 2026-11-14)

The single brief for every Phase 2 agent. The rules behind it are in `RULES.md` (sections 0, 3b, 4, 5).
Read `STATUS.md` first. **Nothing may be started**: no containers, no servers, no loops, no minds. The owner tests
after restarting the machine.

## 1. The design in one page
- **The world runs without AI.** The Go engine (`legion/`) plays every bot. AI adds direction and words on top.
- **One goal per bot:** its ultimate build. The engine already works it down: next build step -> how to get it ->
  where to farm -> solo or party -> routine. Plans last about 2 hours.
- **The two metrics that matter per bot:** kills per minute and zeny per hour.
  `zeny/hour = kills/minute x value per kill - costs`.
  System agents own kills/minute (engine quality). Puppet masters own value per kill (direction).
- **Two classes of agents.**
  - Puppet masters (in the world; act through data and direction only, never code): guild leaders (guild01-10, and
    Ayla for Starveil), economy/market, build/class, chat. Each runs as its own one-shot agent outside the main
    session (`claude -p`, Haiku unless stated), woken by a dispatcher script on a schedule or an event.
  - System agents (outside the world; never steer the game): sentinel and other scripts, party watcher, class QA,
    fixers, deploy agent, analyst. The main session listens to the owner and to bug reports and assigns fixers.
- **Guilds compete on rank and wealth.** Leaders are frenemies. Guild wars come later.
- **Guild direction, no bounties.** A leader posts a few standing directions. Every member owes the guild about
  2 hours a day on a direction that is safe for it and that it can do; otherwise it is excused. Members who do not
  contribute are warned, then kicked. A kicked bot goes independent and looks for another guild, preferring high
  rank and wealth. Loot of a guild block: the items the guild asked for go to guild stock; the bot keeps the rest.
- **Giving.** Party member: free. Guild mate or friend: free when it is a spare, else a heavy discount (25% of
  market). Stranger: market price. A chat agent helps bots communicate; with it off, template lines still work.
- **Ayla is Neo.** She, her party and her guild Starveil must be at the top of the two metrics against comparable
  bots. Her cheat code is a direct, instant line to the system: improvement requests that jump the bug queue.
- **Deploys every 3 hours** (critical unblocking fixes excepted). One deploy, then about 3 hours of measurement.
- **Token efficiency.** Scripts do the watching. Most agents are Haiku. Sonnet: main session, Ayla's mind,
  build/class master, routine fixers, deploy. Strongest model only on demand (hard fixes, escalations).

## 2. Ground rules for every Phase 2 agent
- Work in your own git worktree and branch (section 4). Commit there. Never push; there is no remote.
- Use Windows git (Git Bash) for all git commands. Use WSL only to compile and test Go.
- Do not edit `RULES.md`, `CLASSES.md`, `AGENTS.md`, `HANDOVER.md`, `CLAUDE.md`, `STATUS.md` unless you are the
  rules agent. Write your notes to `docs/design/<your-name>.md`: what you built, flags, endpoints, how to test live.
- Never start docker, a server, a loop or an agent. Never touch the database or the running-state files in
  `legion/data` that git ignores.
- Preserve each file's line endings. In Bash heredocs a double backslash collapses to one; build Windows paths
  with care.
- End commit messages with: `Co-Authored-By: Claude <noreply@anthropic.com>` using your own model name if known.

### Extra rules for engine (Go) agents
- New behaviour goes in **new files**; hook-ins to shared files (`ai.go`, `bot.go`, `api.go`, `lfg.go`,
  `economy.go`) are one-line calls. Three engine agents work in parallel and their branches get merged.
- Every behaviour change sits behind a `tuning.json` flag, **default off** (section 3). Measuring is always on.
  The first live run is a baseline with the flags off; features are switched on one at a time afterwards.
- Lessons that caused the API wedge of 2026-11-14 (`legion/data/lessons.md`): no file I/O while holding a bot's
  lock; no global lock taken in every bot's tick; an API handler never locks every bot for long (copy what it
  needs under each bot's lock, release, then compute); unit tests replay the server's real packet sequence.
- Both instances run the same binary: the main one and `legion-core` (Ayla's party members, API on :8091, merged
  by the main instance). New endpoints must work for both, following how existing endpoints merge or forward.
- Never Ayla's party for anything destructive (kicks, resets). Humans are treated like every other player in the world
  (gate G25: same tax, duty, kicks, prices, trades, chat); only out-of-world data safety differs (G06, G11).
- Compile and test with `bash /g/RO/legion/tools/dev/gocheck.sh <path-to-engine-dir>` (created by the toolchain
  agent; it prints the known baseline failures in `docs/design/toolchain.md`). Your branch must compile, pass
  `go vet`, and add unit tests for your logic. If the script is not there yet, keep working and check later.

## 3. Work packages

### T. Toolchain (Sonnet) - branch `main`, only adds `tools/dev/` and `docs/design/toolchain.md`
Portable Go 1.22.x in WSL home (no sudo needed: tarball from go.dev into `~/go-toolchain`), no docker.
`tools/dev/gocheck.sh <engine-dir>`: copy the dir to a WSL-native temp folder, run `gofmt -l`, `go vet ./...`,
`go test -count=1 ./...`, print a short summary, exit non-zero on failure. Record the baseline on `main` (which
tests fail or skip without a live server, and why) in `docs/design/toolchain.md`.

### M. Meter (engine) - branch `feat/meter`
Always on, read-only for behaviour.
- Per bot: fair kills (monster level >= bot base level - `meter_fair_lv_gap`, default 15), zeny income (zeny
  gained + market value of loot picked up - consumables and travel fees), deaths, and a **time budget** in
  seconds per bucket: fighting, searching (on the hunting map with no target), travelling, resting, town, dead,
  waiting (regroup, rally, hold), stuck, idle (afk, hangout), plus a flag for "on guild duty".
- Windows: last 10 minutes, last hour, session.
- Cohorts: class line x level band (1-20, 21-40, 41-60, 61-80, 81-99) x solo/party. Per cohort: n, mean, median
  and p75 of kills/minute and zeny/hour, time shares, deaths/hour. **Par = p75 of the cohort.**
  Gap to par x n = the size of the prize.
- History: one row per cohort per hour appended to `legion/data/meter.jsonl` (written off-lock).
- Neo view: Ayla's party members (core instance) and Starveil against their cohorts' par. Ayla herself is played
  by OpenKore; use what `openkore-ayla/logs/status.json` (mounted at `/ayla`) gives.
- API: `GET /api/meter` (server totals + cohorts), `GET /api/meter/bots?cohort=&sort=&limit=`,
  `GET /api/meter/bot/{name}`, `GET /api/meter/neo`. `tools/legionctl.py meter ...` commands.

### G. Guild direction (engine) - branch `feat/guild`
- Directions: up to 3 active per guild: `{id, kind: stock|gear|carry|level|supply|wealth, item, map, min_lv,
  max_lv, classes, note, set_by: leader|autopilot, expires}`. Autopilot (no AI) derives them from what
  `guildstock.go` already computes (gaps, farmers) plus "carry the lowest members"; a leader-set direction lasts
  24 hours by default, then autopilot takes over again.
- Duty: a member owes `guild_duty_min_per_day` (default 120) minutes per played day. At plan time, a member with
  duty outstanding takes one guild block (about 2 hours) on the direction that fits it: safe for its level and
  gear by the existing map-fit checks, and doable for its class. Nothing fits -> excused. Requested items from the
  block go to guild stock through the existing flow; the rest stays with the bot.
- Ledger: minutes per member per day for the last 7 played days, persisted off-lock. Standing: good, behind,
  warned, kicked. Default: behind 3 played days -> warning in guild chat; 2 more -> kicked. A per-guild strictness
  factor follows the guild's style (strict min-maxers kick sooner, the social guilds later).
- Kick and after: expel in game, clear the guild, 24-hour cool-down, then the bot looks for a guild with room,
  preferring higher rank and wealth, and friends. Never kicked: guild leaders, guild alchemists and traders
  (their job is their duty), members of Ayla's party (no human exception, G25).
- Rank and wealth: rank from guild level, members' levels and leaderboard places; wealth from the guild stock's
  market value plus guild funds. History kept. Feeds the join preference.
- Relations between guilds: a table from rival (-1) to ally (+1) that drifts with events (a poached member
  sours it, a fair trade warms it). For now it feeds briefings and chat tone only.
- API: `GET/POST /api/guild-direction[/{guildNN}]`, `GET /api/guild-duty[?guild=]`,
  `POST /api/guild-duty/{guildNN}/pardon|kick`, `GET /api/guild-rank`, `GET /api/guild-relations`.
  `legionctl` commands for each.
- Flag: `guild_duty_on` (default off). Ledger, rank, wealth and relations are computed regardless.

### X. Exchange and chat jobs (engine) - branch `feat/exchange`
- Price by relationship in the existing want/offer matching (`economy.go` matcher, gear pool, mail trades):
  party -> free; guild mate or friend -> free when it is a spare (a duplicate, or not on the giver's build table),
  else `kin_discount_pct` (default 25) percent of the market price; anyone else -> market price.
  Ask order: party chat, then guild chat, then #trade. Counters and a recent list at `GET /api/exchange`.
- Chat jobs: wherever the engine speaks for a social event (ask, offer, thanks, direction announced, duty
  warning, kick, leader banter), it creates a job `{id, t, kind, channel, speaker, hearers, facts,
  template_line, deadline}`. If `chat_jobs_on` is set **and** a chat agent polled within the last 2 minutes, the
  engine waits until the deadline (20-40 s) for lines posted to the job; otherwise, or on timeout, it sends
  `template_line`. `GET /api/chat-jobs`, `POST /api/chat-jobs/{id}/lines` with `[{speaker, channel, text}]`.
- Intents: `POST /api/chat-intent {from, kind: need|offer, item, ...}` lets the chat agent register what a human
  (or a messy conversation) meant, so the engine treats it like a bot's want or offer (`humanwant.go`).
- Flags: `exchange_tiers_on`, `chat_jobs_on` (default off). `legionctl` commands for each.

### P. Puppet-master harness (Sonnet) - branch `feat/puppet`, owns `tools/puppet/`
- `dispatch.py`: the only long-running piece; no AI itself. On a schedule or a trigger it assembles a briefing
  and starts one one-shot agent: `claude -p --model <model> --allowedTools "Bash(python tools/legionctl.py:*)"
  --max-budget-usd <cap> --output-format json`, run from a folder with a tiny `CLAUDE.md` (so the big rules are
  not loaded), with the role brief as the system prompt. It logs every call (role, trigger, tokens, cost, exit)
  to `legion/data/puppet_calls.jsonl`, writes a heartbeat file, and has `--dry-run` and `--once <role>`.
  Schedule: guild council every 20-30 minutes per guild, staggered; market every 30 minutes; chat when a human
  line or queued chat jobs exist (check every 30-60 s); build/class master on demand; Ayla's mind (Sonnet) every
  5 minutes and on events (death, party split, goal met, member need). A per-hour budget cap per role.
- Briefings assembled in Python from the engine's API (not by the engine): short, role-specific, and including
  feedback on that role's previous decisions (what it set, what followed, how its numbers moved).
- Role briefs (short, in-world rules only): `guild.md`, `market.md`, `build.md`, `chat.md`, `ayla.md`. Each mind
  keeps a small notes file between calls. Minds never get direct goal/map commands for bots: they set
  directions, prices, priorities and words. They report engine gaps to the bug queue (`legionctl bug ...`).
- Neo line: `legionctl neo "<request>"` -> top-priority entry in the bug queue with Ayla's recent evidence
  attached automatically (party, map, deaths, meter numbers), and an immediate wake of the main session.
- Guild leaders see rivals in their briefing (rank, wealth, relations) and may deal or compete in words.

### S. System-agent harness (Sonnet) - branch `feat/system`, owns `tools/system/`, `tools/sentinel.py`,
`tools/mindgate.py`, `tools/deploy/`, `.claude/agents/`, `.claude/skills/`
- Bug queue `tools/system/bugq.py`: add, list, dedupe, rank by impact on the two metrics; sources are the
  sentinel, the watcher, the minds, the owner and Neo. A small watcher process exits (waking the main session)
  only for: an outage, a Neo request, or the pre-deploy planning time. Everything else waits in the queue.
- New gate in `tools/mindgate.py` (keep the pause flag): no longer "15 minds alive". It checks that the
  dispatcher and the sentinel are alive (heartbeat files) when the stack is supposed to be up, reports once per
  problem, and never blocks a turn for longer than one message.
- Sentinel: drop the 15-minds check; add dispatcher heartbeat, meter-based checks, and write problems into the
  bug queue instead of only exiting.
- Deploy guard: the deploy scripts refuse a deploy sooner than 3 hours after the last one unless `--critical`.
  One-per-hour limit on rathena restarts. Record each deploy with the fixes in it and the metric each should move.
- Analyst `tools/system/league.py` (no AI): reads `/api/meter`, ranks cohorts by gap to par x n, breaks the gap
  down by time bucket, writes `docs/reports/league-<time>.md`. After a deploy it compares the 3-hour windows
  before and after for the cohorts each fix targeted and marks keep or revert.
- Party rule checks as a script (`tools/system/partycheck.py`): together within 20 cells, idle time, deaths,
  wrong save point, sitting, back-and-forth; it files violations to the bug queue. The AI watcher is woken only
  by violations and for a 30-minute review.
- Agent definitions with their models (`.claude/agents/`): fixer (Sonnet), hard-fixer (strongest, on demand),
  deploy (Sonnet), watcher (Haiku), qa (Haiku or Sonnet), analyst-review (Sonnet). Each gets a short brief.
- New session skill replacing `legion-session`: what to start, in what order, and that the owner decides when.

### R. Rules trim (Sonnet) - branch `feat/rules`, owns the rule and doc files
- Sort every current rule in `RULES.md` and `CLASSES.md` into: hard gate, behaviour spec, process rule, server
  setting, replaced.
- Rewrite each as one short plain line with an ID and a date and **no quote**. The owner's original words go to
  `docs/rules/archive.md` under the same ID (never auto-loaded).
- Gate register `docs/rules/gates.md`: every hard gate, how it is enforced (hook, script, test, text only), and
  how to check it. Build it from the current files **before** trimming. Add `tools/system/gatecheck.py` that
  verifies every gate ID is still in the always-loaded core.
- New layout: an always-loaded core of about one page (goal, hard gates, map of what to read for which task);
  topic files (`docs/rules/parties.md`, `economy.md`, `guilds.md`, `chat.md`, `ayla.md`, `process.md`,
  `settings.md`) and one class file per class line under `docs/classes/`; `HANDOVER.md` cut to a short
  current-state page plus topic references; an index by topic for `legion/data/lessons.md` (do not rewrite the
  lessons). Target: about 2,000 tokens always loaded.
- Coverage table `docs/rules/coverage.md`: every old rule -> kept, merged, moved (where), or archived as replaced.
  Nothing is deleted until the table is complete; an independent auditor checks it afterwards.
- Update `CLAUDE.md` (repo) and tell the main session what `G:\RO\CLAUDE.md` (outside the repo) should say.

## 4. Worktrees
Create yours from the current `main`:
`git -C /g/RO/legion worktree add /g/RO/legion-wt/<name> -b feat/<name>`, work only inside it, commit there.
The integration pass merges the branches into `main`, runs the checks, and writes the test checklist.
