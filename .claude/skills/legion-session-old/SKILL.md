---
name: legion-session-old
description: SUPERSEDED history of the 2026-11-14 setup (15 minds, standing agents, the main session starting loops). Do not use; the main session's job is Constitution Article 1 and the legion-session skill.
---

> **SUPERSEDED (history only).** Nothing below is an instruction. Where it has the main session start, restart,
> observe or run anything, the Constitution (Article 1) and the `legion-session` skill apply instead.

# Start a Legion session

You are the orchestrator. Your job is to make sure the world is running and then hand the players' minds to
sub-agents. Read `AGENTS.md` first if you haven't.

## 0. Full session startup (do this every time a main session starts; 2026-11-14)
The owner wants every new session to come back up exactly like the working setup of 2026-11-14. The authoritative
checklist is **`HANDOVER.md` section 0** (read it now); in short:
1. **Your role** (RULES.md 0): you only observe Ayla's party and listen to the owner. All coding, fixes and deploys go
   to subagents. Relay their reports to the owner in plain words, record every owner rule in `RULES.md` the same turn
   (class/build-specific ones in `CLASSES.md`),
   restart any standing agent that stops, and resume an agent with `SendMessage` instead of spawning a duplicate.
2. **Check the world** (step 1 below, plus `docker ps` and `/api/parties/detail`: Ayla's party must be online).
3. **Background loops**, started as background shells, exactly one copy each (check with
   `wsl -d Ubuntu-24.04 -- pgrep -af "sentinel.py|ai-display|ai-client"` first; Git Bash needs `MSYS_NO_PATHCONV=1`):
   - `wsl -d Ubuntu-24.04 -- python3 /mnt/g/RO/legion/tools/sentinel.py` (exits on a problem; after handing it to a fixer,
     restart with `--mute KEY[,KEY] --mute-until HH:MM`)
   - `python tools/unwall.py --loop` (cwd G:\RO\legion)
   - `MSYS_NO_PATHCONV=1 wsl.exe -d Ubuntu-24.04 -- bash /mnt/g/RO/legion/tools/rogui/ai-display.sh` (VIEW_SCALE 1.39 =
     1780x1000, never fullscreen), then `.../ai-client.sh` the same way.
4. **LLM minds** (step 2 below, with the cycle counts of HANDOVER 0.4): ayla (Sonnet, 60-cycle loops, "cycle N/60"
   status), chat (~300 cycles of 30 s), life (60), market (60, "don't stop until 60 cycles"), guild01-05 and
   guild06-10 (60 each), watchdog (minds 15/15, party together, sentinel, deaths). All Haiku except ayla.
5. **Standing engineering agents** (background, each reports to you with SendMessage; briefs in HANDOVER 0.4):
   Ayla party watcher (Sonnet, read-only, every 60-90 s, 20-min summaries), class-QA agent (read-only),
   build-and-strategy agent (keeps `CLASSES.md` current), economy engineer; on demand: Ayla-plugin engineer, one fixer per bug (build only, report
   "built", never restart legion/legion-core), one deploy agent per batch (every 3 hours, RULES.md "Batch deploys"; gofmt, vet, go test,
   `tools/deploy/dep3.sh` (frozen-snapshot build + test, tag `ro-legion:deploy-<time>`) then `tools/deploy/depcore_nobuild.sh` (same tag), measure at +10/+25 min). The scripts only `up --no-deps`
   legion/legion-core; if their guard prints "rathena config changed — needs an owner-approved rathena recreate; not
   deploying" (exit 2, nothing deployed) the deploy agent stops and reports. Rathena is recreated only on purpose with
   `tools/deploy/rathena_recreate.sh --yes`, which brings Ayla, openkore, legion-core and legion back with it
   (HANDOVER 0.4 item 8).
6. **Pending items**: HANDOVER 0.6 (falcon rental needs a rAthena restart; owner's open questions on drop rate and a
   rAthena restart for depleted spawns).

## 1. Check the world
```
python tools/legionctl.py status
```
- If the API isn't reachable, start the stack and wait ~2 minutes:
  `wsl -d Ubuntu-24.04 -- bash -c "cd /mnt/g/RO/legion && docker compose up -d"`
  then re-run `status`. Use the `legion-ops` skill if it still fails.
- Note how many are online, the busiest maps, and whether any **humans** are online.

## 2. Spawn the minds (sub-agents, in parallel)
Spawn these as sub-agents (Claude Code: the agents in `.claude/agents/`; Copilot: the custom agents in
`.github/agents/`). If your tool can't select a named agent, give a general sub-agent the brief below and tell
it which skill to follow. Use a fast/cheap model (e.g. Haiku) for them if you can choose.

| agent | skill to follow | brief |
|---|---|---|
| legion-chat | legion-chat | "Run the chat-director loop for 60 cycles; answer every HUMAN line." |
| legion-life | legion-life | "Run ambient life for 60 cycles; focus on maps with humans, then towns, then busy fields." |
| legion-guild (xN) | legion-guild | "You lead guild01, guild02, guild03 (and so on). Run 20 cycles." Split guild01..guild10 across 3-5 agents. |
| legion-market | legion-market | "Run the Prontera market for 30 cycles." |

**Current cycle counts (2026-11-14, these override the table above):** Haiku minds stop early, so give each an explicit
count and restart it when it ends: chat ~300 polling cycles of 30 s; life 60 (quote every name/text; lines only from
diary/live state); market 60 (short strict brief: "don't stop until 60 cycles"); guilds in two agents, guild01-05 and
guild06-10, 60 cycles each (`party` lists ALL members - `leader-party --members` replaces the party; "Novice 1" =
offline char; wait 10 min after a restart before re-forming; `boss-check`/`boss-hunt` only when READY, never `--force`;
use mapinfo/drops); `ayla` on Sonnet in 60-cycle loops with "cycle N/60" status; plus a Haiku `watchdog` (minds
15/15, party together, sentinel, deaths; reports problems to the main session).

Each sub-agent must use only `python tools/legionctl.py ...` (plus reading files in this folder), sleep between
cycles with `legionctl wait`, and finish with a 5-line summary of what happened in the world.

**Minds bus (tell every sub-agent this in its brief):** each mind has a name: `chat`, `life` (`life2` for a second
one), `market`, one per guild (`guild01`..`guild10`), and `ayla` for Ayla's mind. Every cycle it must first run
`python tools/legionctl.py orders <mind>` and follow the owner's orders (they override the plan; acknowledge with a
thought), and post 1-2 short plain-English thoughts with `python tools/legionctl.py think <mind> "..."` (what it saw,
what it decided). The owner reads these and replies on the dashboard (`http://127.0.0.1:8090/dashboard`, Minds).
**Game data (tell every mind this, RULES.md 5):** for any question about drops, monsters or maps, and before picking
a map to farm or level on, the mind looks it up with `python tools/legionctl.py drops "<item>" [--for "<leader>"]`
(who drops it, rates, maps ranked by drop rate x spawn count, party fit, danger), `mob "<monster>"` and
`mapinfo <map> [--for "<leader>"]`; never answer "I can't find the data". Covers all items, monsters (MVPs and
mini-bosses too) and maps (fields and dungeons).
You can check the minds yourself with `python tools/legionctl.py minds`; an `idle` mind (not seen for 3+ min) has
probably stopped and should be re-spawned.

**Ayla's mind picks maps from data (tell the `ayla` mind this):** before choosing the party's grinding map, or when
the owner asks to farm an item ("farm SS card"), run `python tools/legionctl.py drops "<item>" --for Ayla` (maps ranked
by drop rate x spawn count, graded against her party's levels, with danger) and `mapinfo <map> --for Ayla` on the
candidates; then send the map with `tools/rogui/ayla.sh map NAME`. Say in a thought what the data showed (rate, kills
per drop, monster levels vs the party). If the item's monsters are far below the party (little exp), tell the owner.

**Ayla's mind and her party-lead plugin (tell the `ayla` mind this):** movement decisions for Ayla's party have one
owner at a time; the mind's explicit order wins. Use `tools/rogui/ayla.sh` (it writes to her OpenKore commands.txt):
`map NAME` (= `partylead goto NAME`: hunt there, cancels any rally/hold), `rally cancel` (drop a Kafra rally at once,
none for 30 min), `rally TOWN` (meet at that town's Kafra), `lead` (what the plugin is doing). Don't announce a plan in
party chat that contradicts the plugin without sending the matching order. Never send `ai`, `ai off` or `ai manual`.
During a party town run (Ayla said "town run ..."; members "brb selling", "done, coming back") a split is normal: people
shop in different places and Kafra back on their own. Post no meeting point while it lasts; step in only if the party
is still split 5 min after the run. A meeting point is always the Kafra of the town nearest the hunting map (Payon for
pay_dun00/pay_arche), never the town where people happen to be shopping, and once named it is never changed:
cmdfile.pl blocks a party line naming a different meeting town within 10 min (QA 2026-11-14: "Kafra in Payon", then
53 s later "change of plan: meet in Prontera" split the party for 6+ min).

## 3. Keep it going
When a sub-agent finishes, read its summary, check `status` and `minds`, and spawn a fresh one for that role (memory lives
in Legion, so nothing is lost). If a human is online, make sure the chat director and an ambient-life agent are
always running.

## Notes
- Never touch the database or Docker unless `status` shows the world is down.
- Don't speak as players yourself while sub-agents run (avoid double replies).
