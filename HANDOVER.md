# Handover: state of the AI-Lab Ragnarok world (2026-11-13, ~19:40 EST)

Read this first if you are an AI taking over. It covers what the owner wants, how everything is wired, how to run
and check it, what was learned the hard way, and what is still open. `AGENTS.md` (same folder) is the standing guide
for the LLM "minds"; this file is the engineering handover.
**Starting a new session? Do section 0 first** (bring every loop and standing agent back up; `/legion-session`).

---------------------------------------------------------------------------------------------------------------------
## 0. Session startup: bring everything back up (2026-11-14)

Owner: "at least the sub agent system that report to you works. I want you to write it down in a way that next time
we start, everything is back up the same way it is right now." A fresh main session restores exactly this setup, in
this order. `/legion-session` (skill `.claude/skills/legion-session/SKILL.md`) walks through the same steps. Rules
behind it: `RULES.md` section 0. Class/build rules and per-class notes: `CLASSES.md` (the reference for builds; the
build-and-strategy agent keeps it current, adding its notes as it reviews each class).

### 0.1 The main thread's role (owner rule, RULES.md 0)
- The main thread **only observes Ayla's party and listens to the owner**. All coding, fixes and deploys go to
  subagents. It spots a problem, writes a clear brief with the evidence, hands it to a subagent, checks the result in
  the party.
- It **relays agent reports to the owner in plain words** (what was wrong, what was done, the measured result).
- It **records every owner rule in `RULES.md` in the same turn** (class- or build-specific ones in `CLASSES.md`).
- It **restarts any standing agent that stops** (0.4), and **resumes an existing agent with `SendMessage`** (same
  name/ID, context kept) instead of spawning a duplicate. Exactly one agent per standing role.

### 0.2 Check the world first
```
wsl -d Ubuntu-24.04 -- docker ps --format '{{.Names}} {{.Status}}'   # ro-db ro-rathena ro-agency ro-legion ro-legion-core ro-openkore-ayla
python tools/legionctl.py status                                     # from G:\RO\legion
python tools/legionctl.py minds                                      # which minds are alive / idle
curl -s http://127.0.0.1:8090/api/parties/detail                     # Ayla's party first
```
Stack down: `wsl -d Ubuntu-24.04 -- bash -c "cd /mnt/g/RO/legion && docker compose up -d"`, wait ~2 min (skill
`legion-ops` if it still fails). Ayla's party (Ayla + the `LEGION_CORE` members) must be online.

### 0.3 Background loops (the main thread starts them as background shells; exactly ONE copy of each)
Check first that a copy isn't already running (`wsl -d Ubuntu-24.04 -- pgrep -af "sentinel.py|ai-display|ai-client"`;
for unwall look at the session's background tasks). From Git Bash always set `MSYS_NO_PATHCONV=1` (else `/mnt/...`
paths get mangled).

| loop | command | notes |
|---|---|---|
| sentinel | `wsl -d Ubuntu-24.04 -- python3 /mnt/g/RO/legion/tools/sentinel.py` | Exits on a problem (that wakes the main thread). Main hands the problem to a fixer, then restarts it with `--mute KEY[,KEY] --mute-until HH:MM` for keys already handed off (they stay logged in `legion/data/health.log`). |
| unwall | `python tools/unwall.py --loop` (cwd G:\RO\legion) | Moves OFFLINE lg* chars off blocked cells every 30 s. |
| watch display | `MSYS_NO_PATHCONV=1 wsl.exe -d Ubuntu-24.04 -- bash /mnt/g/RO/legion/tools/rogui/ai-display.sh` | Xvfb :98 + x11vnc + TigerVNC viewer. `VIEW_SCALE=1.39` (the default) = 1780x1000 window, the owner's preferred size. Normal window, **never fullscreen**. |
| watch client | `MSYS_NO_PATHCONV=1 wsl.exe -d Ubuntu-24.04 -- bash /mnt/g/RO/legion/tools/rogui/ai-client.sh` | Wine client on :98 through OpenKore's X-Kore 2 proxy; freeze detection, re-logs after drops, stops after 3 failed logins (`/tmp/roai-restarts.log`). Start it after ai-display.sh. |

Details of the watch view: section 4. Never drive any client with the real mouse/keyboard.

### 0.4 Standing agents (all background; each reports to main with SendMessage; restart whenever one finishes)
1. **LLM minds** - the server's core; the `tools/mindgate.py` hook blocks ending a turn while one is silent > 5 min.
   All Haiku except `ayla` (Sonnet). Haiku minds stop early: always give an explicit cycle count, and restart them.
   Every mind, every cycle: `legionctl orders <mind>` first, 1-2 `legionctl think <mind> "..."` lines; game data from
   `legionctl drops / mob / mapinfo` (never "can't find the data"). Full briefs: skill `legion-session` section 2.

   | mind | agent / skill | brief essentials |
   |---|---|---|
   | `ayla` | Sonnet; brief in section 4 + legion-session skill | Ayla's mind: picks the party's map from data (`drops --for Ayla`, `mapinfo --for Ayla`), moves with `tools/rogui/ayla.sh map NAME`; 60-cycle loops posting "cycle N/60" status; **never `ai`, `ai off`, `ai manual`**. |
   | `chat` | legion-chat | ~300 polling cycles of 30 s; answers every HUMAN line. |
   | `life` | legion-life | 60 cycles; quote every name/text in commands; lines only from diary/live state (nothing invented). |
   | `market` | legion-market | 60 cycles; short strict brief: "don't stop until 60 cycles". |
   | `guild01`-`guild05` | legion-guild | 60 cycles. `legionctl party` takes ALL members (`leader-party --members` REPLACES the party); "Novice 1" in a roster = offline char; wait 10 min after a restart before re-forming parties; `boss-check`/`boss-hunt` only when READY, never `--force`; use mapinfo/drops. |
   | `guild06`-`guild10` | legion-guild | same as guild01-05. |
   | `watchdog` | general agent (Haiku) | Checks minds 15/15 (`legionctl minds`), Ayla's party together (within 20 cells), sentinel/health.log, deaths; reports problems to main. |

2. **Ayla party watcher** (Sonnet, read-only): every 60-90 s checks Ayla's party against RULES.md sections 3 and 5
   and the class rules in CLASSES.md:
   together within 20 cells, no idling, rest discipline (priest sits when idle), sweep, leader behaviour (no back and
   forth), combat roles (tank pulls, priest heals first), town runs together, deaths/escapes/warp loops, gear. Reports
   each misbehaviour to main with evidence; a summary every 20 min.
3. **Class-QA agent** (read-only): samples one member of every class (and Ayla's party), verifies each deploy with
   numbers, traces stuck bots on request, reports bugs to main (tools: `tools/legion/classprobe.py`, `tools/measure/`).
4. **Build-and-strategy agent**: reviews each class against online Renewal guides (stat/skill builds, skill use incl.
   AoE on crowds, buffs, Peco/falcon, gear path), boss-hunt readiness and tactics, the gear-upgrade push. Plans must be
   prefix-compatible with existing builds; never mass-delete. It keeps `CLASSES.md` current (owner's class rules +
   an "Engine notes" block per class, added as it reviews each class). When its context runs low it hands off with a numbered
   list of what's done/open so a fresh one continues.
5. **Economy engineer**: build-aware market (`legion/data/buildgear.json` table), dynamic prices (server wealth +
   rarity + supply/demand + markdowns), guild pools for boss items, junk rules, alchemist/guide progress. Tuning flags
   in `legion/data/tuning.json`: `dynamic_prices`, `dietrich_enabled`, `parcels_enabled` (parcels stay OFF: 2,500z per
   mail attachment). Details: section 6e.
6. **Ayla-plugin engineer** (as needed): `partylead.pl` / `cmdfile.pl` / `statusout.pl` / `control/config.txt`,
   applied live with safe reloads (`plugin reload <name>`, `eval` for config values). Never `conf` for hold state,
   never any ai toggle.
7. **Fixer subagents** on demand, one per bug, briefed with the evidence. They **build only** (report "built") and
   never restart `legion` / `legion-core`.
8. **Deploy agent**, one per batch (every 3 hours since 2026-11-14, RULES.md "Batch deploys"): waits for its slot, checks no build
   is running, `gofmt` (`-w` if the diff is only formatting), `go vet`, `go test` (re-run once on failure; abort if it
   fails again), deploys `tools/deploy/dep3.sh` (main) then `tools/deploy/depcore_nobuild.sh` (core, on the same
   image), measures at +10 and +25 min and reports.
   **Frozen snapshot (2026-11-14):** other agents edit `legion/*.go` all the time, so `dep3.sh` copies the Dockerfile's
   inputs (Dockerfile, go.mod, *.go, *.html, recvpackets.txt) to `/tmp/legion-snap-*`, runs gofmt -w (snapshot only;
   it lists the files it fixed), go vet and `go test -count=1` on that copy (one retry on a fresh snapshot after 60 s,
   in case a file was half-saved), and `docker build`s the image from the same copy, tagged
   `ro-legion:deploy-<YYYYmmdd-HHMMSS>` and `ro-legion:latest`. The tag goes to `/tmp/legion-deploy.tag` (WSL). legion
   is started pinned to that tag (compose override `/tmp/legion-deploy-override.yml`, `--no-deps --no-build --pull
   never`), so a `:latest` moved by someone else's build is never picked up; `depcore_nobuild.sh [TAG]` deploys the
   core on the tag from the file (or the TAG given) and refuses if there is none. `depcore.sh` = its own snapshot build
   + core. Tested = shipped; the deploy agent no longer runs gofmt/vet/test on the live tree itself.
   **Rathena guard (RULES.md 0, after 10:12 2026-11-14):** the scripts only ever run `docker compose up -d --no-deps
   legion|legion-core` (never rathena/db/agency/openkore*; `guard.sh` refuses any other service). Before building and
   again before `up`, `guard.sh` compares `docker compose config --hash db|rathena` with the running containers'
   `com.docker.compose.config-hash` label and asks compose (`up --dry-run` with deps) whether db/rathena would be
   recreated. If so it prints "rathena config changed — needs an owner-approved rathena recreate; not deploying" and
   exits 2 with nothing built or deployed: the deploy agent then **stops and reports to main** (never works around it,
   never runs a plain `docker compose up`). First run `DRY_RUN=1 bash .../dep3.sh` (checks only) to see the guard
   result. It also prints a NOTE if the rathena/db image tag was rebuilt (no block) and a WARNING if ro-openkore-ayla /
   ro-openkore sit on a dead namespace (= Ayla offline: tell main at once).
   **Owner-approved rathena recreate:** `wsl -d Ubuntu-24.04 -- bash /mnt/g/RO/legion/tools/deploy/rathena_recreate.sh`
   (plan only) then `... rathena_recreate.sh --yes` (`--build` to rebuild the image first). It recreates ro-db if its
   config changed, then `--force-recreate rathena`, waits for "Map-server 0 loading complete", force-recreates every
   `network_mode: service:rathena` service read from the compose file (openkore-ayla first, then legion-core, openkore,
   legion; mastermind only if its container exists) **each pinned to the image it was running** (tagged
   `ro-pin/<svc>:<id12>`, compose override `/tmp/rathena_recreate_pin.yml`, `--no-build --pull never`; never a newer
   `:latest` - at 10:20 legion/legion-core came back on an untested build). If a running image no longer exists it
   stops before touching rathena (exit 4) unless `--use-compose-image`. It checks each sits in the new rathena namespace, and waits until
   Ayla is in game (`openkore-ayla/logs/status.json` online with a fresh ts, or "You are now in the game"; then
   `tools/rogui/ayla.sh status`). It never sends anything to Ayla's OpenKore (no ai/ai off/ai manual). Kicks everyone:
   main picks the moment and tells the owner.

### 0.5 Scripts kept in the repo (copied 2026-11-14 from the session scratchpad, which is per-session)
Run the deploy scripts in WSL: `wsl -d Ubuntu-24.04 -- bash /mnt/g/RO/legion/tools/deploy/dep3.sh`.
- `tools/deploy/dep3.sh`: frozen-snapshot build (gofmt/vet/go test + docker build from one copy of legion/, tag
  `ro-legion:deploy-<time>` + `:latest`), then `up --no-deps legion` pinned to that tag (main). `DRY_RUN=1` = checks only.
- `tools/deploy/depcore.sh`: same snapshot build, then legion-core on it; fixes a stray `<hash>_ro-legion-core`
  container and checks the core runs the deploy image.
- `tools/deploy/depcore_nobuild.sh [TAG]`: core restart on the last deploy tag (`/tmp/legion-deploy.tag`), no rebuild.
- `tools/deploy/guard.sh`: sourced by the three above (rathena/db change check, `--no-deps` only, `DRY_RUN=1`).
- `tools/measure/sktree.py`: `dump <Job>...` prints a job's Renewal skill tree; `check <1st job> <2nd job> "<sk(...) pairs>"` checks a builds.go plan (prerequisites, max levels, points); copies skill_tree.yml / skill_db.yml from ro-rathena into tools/measure/rd on first run.
- `tools/deploy/rathena_recreate.sh [--yes] [--build]`: the only way to recreate rathena; brings Ayla and every
  service:rathena container back with it (0.4 item 8).
- `tools/measure/restshare.py N GAP`: rest/fight share per class over N samples of /api/bots, GAP s apart.
- `tools/measure/collect.py`: leaders in "collecting X" and whether it's needed (target within 20 cells or fighting = BAD).
- `tools/measure/tanksample.py [SECONDS]`: state mix of lead/member tanks, ranged and support in parties; tank kills/distance.
- `tools/measure/kcount.py SINCE [UNTIL]`: Kafra teleports per bot from diaries (run with cwd `legion/data/memory`; ISO times).
- `tools/measure/sk.sh SECONDS bot...`: traces bots, counts skill sends/fails and top states (Git Bash; uses WSL docker logs).
- `tools/measure/ec6_counts.sh`: economy counters (refine, brew, vend, shops, trades) from /api/economy.
- `tools/measure/econ5_pots.py`: HP potions / zeny / spare weapons in a lv30+ sample + out-of-potion run calls.

The 2026-11-14 session's scratchpad (hundreds of one-off scripts and measurements, not copied):
`C:\Users\kod\AppData\Local\Temp\claude\9b0368b215c2c0f3\40046184-90b0-47ad-be6b-88bc200a3437\scratchpad\`.
A new session gets a new scratchpad: use the repo copies above.

### 0.6 Pending items (carry over until done)
- **Butterfly Wing 300z (owner 2026-11-14 "lower price to help bots")**: `rathena/db-import/item_db.yml` (Id 602 Buy 300
  / Sell 150; official Renewal price, the RE db had 1,000z) is READY but NOT LIVE: add to the rathena volumes in
  docker-compose.yml `- ./rathena/db-import/item_db.yml:/rathena/db/import/item_db.yml:ro` immediately before running
  `tools/deploy/rathena_recreate.sh --yes` (a rathena config change: while the line is in the compose file and rathena
  not recreated, guard.sh blocks every Legion deploy with "rathena config changed"). NPC shops (legion_shops.txt) use -1 =
  item db price, so nothing else on the rAthena side. Legion reads only db/re/item_db_*.yml (legion/Dockerfile copies
  them), so `items[602].Buy` stays 1000 in the engine (town.go wingKitCost / gear reserves) until loadItemDB also reads
  the import override; the actual buy uses the shop's listed price (listedPrice) and is right.
- Hunter falcon rental needs a rAthena restart (`docker compose restart rathena`; kicks everyone incl. Ayla - pick a
  quiet moment and tell the owner).
- Owner's open questions still unanswered: the drop rate, and a rAthena restart for depleted spawns.
- **Trans cap (2026-11-14) also waits for the rAthena restart**, and it needs a container RECREATE, not a plain restart
  (a new volume in docker-compose.yml): `tools/deploy/rathena_recreate.sh --yes` (kicks everyone incl. Ayla; never a
  bare `docker compose up -d rathena`, which leaves Ayla on a dead namespace). Note: ro-rathena was recreated at
  10:12 and again at 10:20 (2026-11-14) and now runs the compose config with the scripts_custom.conf volume. Until then the stock
  3rd-job NPCs are still loaded (no bot can reach them: max bot base level is 80). See section 6f4.

---------------------------------------------------------------------------------------------------------------------
## 1. What the owner wants

- A **local Ragnarok Online server** on their Windows 11 PC (`G:\RO`), everything in **Docker**, everything in **English**,
  used for experiments with **AI-driven bots**: up to ~1000 bots online out of ~4000 player profiles, each with a persona.
  The world must "feel real, like interacting with humans" (a Matrix simulation). Bots never reveal they are bots/AI.
- 99% of behaviour is **algorithmic** (Legion, Go). LLM "minds" handle judgement and words (chat, guild strategy).
- **Real progression**: every bot started at level 1 Novice (re-seeded 2026-11-13) and levels up for real, each with
  a build plan, levelling strategy and equipment path to 99. 10 ambitious guild leaders found guilds and recruit.
  Parties use tank/ranged/priest roles, bait-and-carry, even EXP share. Bots prefer farming, open shops for surplus,
  and must **run away from danger**.
- **Ayla's core test party** must **always be online**: Ayla (owner's account) + Zendancer84, Echobunny, Zen.fox, xXRinfangXx (Hunter), Silent rogue (Alchemist), vex_x18 (Crusader, from the next deploy) (6h).
  Lessons from this party get built back into all bots. The owner **watches Ayla live** in a client window.
- A **dashboard** to see the world, talk to the minds, and see guild and party data.

### Owner rules (don't break these)
- **Never drive any client with the real mouse/keyboard**: "it keeps clicking the wrong thing and I cannot use my
  computer". Only the Wine client on the virtual display `:98` (xdotool there is fine) or OpenKore.
  (`tools/rogui/ro.ps1.disabled-uses-real-mouse`, `input.ps1.disabled-uses-real-mouse` are disabled on purpose.)
- **No fullscreen** for the owner's client.
- **No GM accounts for watching** (a hidden GM "camera" was tried and removed on request).
- **Don't remove information from the dashboard**; add, don't replace.
- **If something keeps failing, stop retrying and investigate** (find the root cause, don't loop).
- **Re-seeds must never touch human characters**: delete only by bot account range 2100001-2199999, never by char_id
  range (the owner's character "kitty" was lost once and recreated). Keep the core party: `seed -keep "Zendancer84,Echobunny,Zen.fox,xXRinfangXx,Silent rogue,vex_x18"`.
- Don't bypass anti-cheat / EAC, don't use pirated client downloads, don't unpack protected executables.
- Ask before outward-facing or hard-to-reverse actions (installing external tools, deleting data).

---------------------------------------------------------------------------------------------------------------------
## 2. Layout

| Path | What |
|---|---|
| `G:\RO\legion\docker-compose.yml`, `.env` | the stack: `ro-db` (MariaDB), `ro-rathena` (login/char/map), `ro-agency` (login token stub), `ro-legion` (bot engine + dashboard/API on :8090), `ro-openkore` (spare), `ro-openkore-ayla` (plays Ayla) |
| `G:\RO\legion\rathena\` | rAthena (Renewal, packetver 20210107). `conf/import/*` local config, `npc-custom/legion.txt` job changes / autoskill / starter kit |
| `G:\RO\legion\legion\` | Legion (Go). `README.md` = engine internals + HTTP API. `data/` = personas, memory, minds, guild roster, human parties, bugs.md, lessons.md, backups |
| `G:\RO\legion\tools\legionctl.py` | CLI the minds use (status, inbox, say, party, goal, minds bus...) |
| `G:\RO\legion\.claude\skills\legion-*` | skills for the mind roles (session, chat, life, guild, market, ops) |
| `G:\RO\legion\openkore\` | OpenKore image build (Dockerfile, servers-local.txt, custom serverType) |
| `G:\RO\legion\openkore-ayla\` | Ayla's OpenKore: `control/config.txt`, `control/sys.txt`, `control/commands.txt` (command inbox), `plugins/cmdfile/` (cmdfile.pl, **xkore2fix.pl**) |
| `G:\RO\kRO-210607\client\` | the owner's game client (`RO-AILab.exe`, WARP-patched 2021-01-07 client), launched via `Play-AILab.cmd` / desktop shortcut "RO-English"; login player/player |
| `G:\RO\legion\tools\rogui\` | watch-client and Ayla tooling (WSL scripts), screenshots in `shots\` |
| `G:\RO\tools\sprhd\`, `realesrgan\` | HD sprite pipeline (AI-upscaled sprites packed in `sprites_hd2.grf` for the owner's client) |
| `G:\RO\WARP\` | client patcher (profile `Inputs\ailab-2021-en.yml`: NoHourly, CustomFontHeight 13) |
| `C:\Users\kod\.claude\projects\G--RO\memory\` | Claude Code memory (rules above, core party) |

---------------------------------------------------------------------------------------------------------------------
## 3. How to operate (Windows host + WSL Ubuntu-24.04, Docker runs inside WSL)

- Shell quirks: PowerShell 5.1 and Git Bash are available. In Git Bash use `export MSYS_NO_PATHCONV=1`.
  `wsl.exe -- bash -c '...'` mangles `$`, `$(...)` and awk fields: **write a script file and run
  `wsl -d Ubuntu-24.04 -- bash /mnt/c/.../script.sh`**. Processes started inside one `wsl` call die when it returns,
  so long-lived things (Xvfb, client, keeper) must run as their own long-running/background command.
- **Tune first, deploy second**: many behaviour numbers (party leash, tank pull radius, priest SP thresholds,
  beginner level, crowding, Kafra/gear money rules, chat rates...) live in `legion/data/tuning.json` and are re-read
  every 30 s by both instances - edit the file, no restart (fields: `legion/README.md` "Live tuning", `tuning.go`).
  Check the log line `tuning: <field> old -> new`. Move a constant there when you expect to tune it again.
- **Two Legion instances** (`legion/instances.go`): `legion` (main, ~900 bots, API :8090, dashboard, minds bus) runs
  with `-exclude` the core party; `legion-core` (container `ro-legion-core`, API 127.0.0.1:8091) runs only Ayla's
  friends (`LEGION_CORE`, default `Zendancer84,Echobunny,Zen.fox`) and owns `data/human-parties.json`. :8090 forwards
  `/api/human-party`, `/api/bots/<core member>`, `/api/memory/<core member>` to the core and merges Ayla's party into
  `/api/parties/detail`, so the tools keep using :8090.
- **Bot parties survive restarts** (2026-11-14, `legion/partypersist.go`): the main instance saves every bot-led party
  to `data/parties.json` (on change + every minute) and restores it at startup (members log in on the saved character,
  goals come back once the leader is online, members missing after 10 min are dropped). `botPartyKeeper` keeps the
  in-game party in step (create/invite/leave leftovers). Party names must be letters/digits/spaces only (rAthena
  `char_name_option: 1`): before this fix NO bot party existed in-game ("Name's pt" was refused).
- **Need-driven parties** (2026-11-14, owner rule RULES.md 3 "Parties come from need, not permanence"; `legion/lfg.go`,
  **built, behind `goal_parties_on` in data/tuning.json, default false** = the old permanent grouping keeps running).
  With it on (main instance only; Ayla's human party is never touched):
  - Goals (every bot, re-checked every ~2 min): `gear` (nextBuildPiece dropped on a map safe for the bot: soloMapOK =
    not deadly/too hard/swarm/boss/empty, has room, <= 6 maps away, beginners <= 3 maps from home), `card` (band card
    for a free slot, 60 min max), `zeny` (shop/trade piece: its price + reserve), `level` (+2, +1 at lv70+), `heal`
    (Acolyte-line healers). gear/card are "fixed" (need a drop map), the rest flexible. Solo bots hunt on their fixed
    goal's map (ai.go chooseHuntingGround). Ends: met / expired / changed -> next goal.
  - LFG/LFM (every 20 s, after a 3-min boot pause): free bots post one #global line per 10 min ("LFP payon cave 2,
    farming Ninja Suit, lv52 assassin"), parties without a healer post "LFM priest for ...". Server-wide budget
    `lfg_global_per_min` (default 6). Matching needs one side to have posted: answerer whispers, joins (keeper invites)
    or a new party forms (poster leads; a healer poster joins the best solo farmer). Party wants: healer first (1 per
    party), then Monk, then missing roles; caps tank 2 / ranged 3 / melee 2, 5 non-healers + 1 healer, level +-10,
    partyMapOK with the newcomer (goalMapSafe for an item-goal anchor: safe is enough, exp-poor is fine).
  - Ends: a member whose goal is met and whose next goal is elsewhere leaves ("got my X! gl guys"), a fixed goal not
    served by the party map for 10 min leaves, a healer >12 levels off the party leaves, the leader leaving ends the
    party (endGoalParty), 3 h cap. Restored parties from before (no `need` in parties.json) end 20-70 min after the
    switch-on, staggered. Party.Anchor (item-goal map) is kept by leaderPartyPick / partyFitCheck while safe.
    Every party formed after boot (API/minds too) is a need party.
  - Guild leaders (`leaderGoalParty`, guildlead.go): when not in a party, pick their goal's drop map if safe for the
    group (else bestPartySpot), recruit guild mates whose goals that map serves, announce in guild chat; parties end
    with the leader's goal and re-form a minute later around the next. Leaders never join others' parties via LFG
    but may anchor/post. `leader-party` / `party` (legionctl) still work; those parties also end with their goals.
  - partyManager: no 90-min refresh and no same-map auto grouping while on. chatter.go: no fake LFP lines while on.
  - Measure: `GET /api/lfg[?bot=Name]` (goals now by kind, goals set/met/expired/changed, posts by kind, posts/min,
    joins, parties formed/dissolved by reason, members_left by reason, open posts); `/api/parties/detail` rows have
    `need` + `purpose`. Before (2026-11-14 16:20, old system): 352 bot parties, 865 members, sizes 2:227 3:90 4:34 5:1,
    44% with an Acolyte-line/Monk (37% a healer), median age 67 min (all restored at the last deploy); #global 7.8
    lines/min (all grats/milestones, 0 LFG), #trade 5.4/min. After: TODO once switched on (measure 30-60 min).
  - Switch on: set `"goal_parties_on": true` in data/tuning.json (live-reloaded, no restart). Off again = same file. Switched ON
    2026-11-14 ~11:40 (owner: goal parties for everyone).
  - Live fixes after switch-on: goals were 754 zeny / 0 gear-card (865 of 1,010 next pieces are trade-only) -> card
    goals now come first after a zeny goal and half the time otherwise, and no longer need a free slot (cardGoal);
    the first 19 posts were all "LFM priest" from old healer-less parties -> posts round-robin over priorities.
  - **Incident 2026-11-14 17:00-17:13Z, flag set off 17:13**: a Legion deploy then a rathena recreate ~3 min later
    took every bot offline; partyManager disbanded all ~350 parties (`on < 2`) and the saver wrote the empty set, so
    nothing was restored. The need system then only re-formed 43 parties in 11 min (formation was gated by #global
    posts, 6/min): 80% solo, deaths 5.8/min (41 of 58 solo). Fixes (built 12:30 local): a party with < 2 online is
    kept 10 min (LowSince); parties.json saves that would drop below half of the last count are held 10 min, the old
    file rotates to parties.json.bak1..3 (every 10 min and before a big drop), and the restore falls back to the
    fullest backup < 2 h old when the file holds far fewer parties; restore always logs "restored N of M".
    Matching is now direct (lfgMatch): open parties take fitting free bots, then same-map parties form via local chat,
    then cross-map via whispers (60/tick) - no #global post needed (posts are flavour). Offline cold start
    (TestLfgColdStartSim, 1,000 solo lv20-90 bots, 85% sociable): 29% partied at +20 s, 85% (= every sociable bot) at
    +60 s, 163 parties, 73% with a healer/Monk. Solo bots (flag on) skip party guide spots and maps partyMapTooHard
    for them. Card/gear goals must fit the level band (monster >= lv-20 from lv40) unless valuable (price >= max(20k,
    lv x 400)); goal maps skip crowded and exp-poor maps (goalMapSafeItem / soloMapOKFor), so lv60+ parties no
    longer camp prt_fild08 for a Pupa card. Core goal turns run whatever the flag says.
  - **Ayla's party (core instance, `legion/lfg_human.go`)** - owner: "they should still have need base rule ... they
    function like other parties", "if it's someone else's turn to farm gears, then go". The party stays together; its
    farm map rotates in turns among members' goals (equal time, "party is one mind for gear"). Friends get build goals
    (the priest too: no "heal" goal inside a human party); Ayla's goal is set by her mind. A turn = a member with a
    gear/card drop map safe for the party; it lasts until that goal is met or 45 min, then the member with the fewest
    turn-minutes. The engine does not move Ayla: her mind reads the turn and moves the party (`ayla.sh map NAME`).
    Friends announce turns and met goals in party chat. State in data/party-turns.json (core). Until the mind sets
    a goal, her current lockMap stands for it (kind "hunt").
    Owner rule 2026-11-14 "Farming should target the best map for the party level and equipments": a turn map must
    be safe and not exp-poor for the party (Ayla's level counts); her goal on such a map gives no turn (buy it). With
    no mappable goal the turn map is `best_map` (bestFarmMap: guide spot that suits the weakest member, real exp,
    room, scored by level band + MapZenyHour - distance). goalValuable is now always false: no item justifies an
    exp-poor map for any goal party. Guild leaders only ever lead (leadersOut in formPartyOpt, joinGoalParty refuses,
    formRoleParties / POST /api/party skip them; TestGuildLeadersOnlyLead); a party leader whose goal map isn't the
    party map takes the party there if safe, else levels with it (leaderGoalSync).
    Owner rule 2026-11-14 (RULES.md 5) "they should prioritize getting good gears, either farming or buying ... for
    each member": friends' goals in a human party are gear-first (humanPartyGearGoal: next build piece -> "buy" when
    they can pay, "gear" farm on its drop map (the turn checks the party fits), else "zeny"); Ayla's goal is picked
    from her build's gear table (plan `assassin` by default, `owned` list kept by her mind; "met" adds the item),
    buy when her zeny + the friends' spare zeny cover it. While anyone saves zeny, best_map is the best zeny/h map.
    GET adds `gear` (per member: next, price, source, how buy/farm/zeny/done, map, have, pooled), `gear_pool_turn`
    (gearpool.go), `plan`, `owned`; POST `{"human":"Ayla","owned":[...]}` / `{"plan":"assassin"}`.
    API (main forwards to core): `GET /api/party-turn` -> turn, item, map, why, until, order, served_min,
    human_goal, member_goals, history. `POST /api/party-turn {"human":"Ayla","item":"Soldier Skeleton Card",
    "map":"pay_dun01"}` sets her goal; `{"human":"Ayla","met":true}` when she gets it; `{"human":"Ayla","next":true}`
    ends the current turn. Needs a legion-core deploy (only main batches core deploys).
- **Map server capacity (2026-11-14):** rAthena's map-server is a select() build, MAXCONN = FD_SETSIZE = 1024, so it
  holds ~1013 clients in total (Legion + legion-core + Ayla + humans). Legion keeps `-reserve` (default 40) of
  `-max-online` free for guild leaders/pinned/core/humans, and a guild leader that can't get onto the map bumps one
  ordinary bot (legion/leaderroom.go). Going past ~1000 online needs a rathena rebuild with epoll and a bigger MAXCONN
  (owner decision: image rebuild + rathena restart).
- Deploy main Legion: `cd /mnt/g/RO/legion && docker compose build legion && docker compose up -d legion` (restarts
  ~900 bots for ~1 min; **Ayla's party stays online**). Vet first: `docker run --rm -v /mnt/g/RO/legion/legion:/src -w /src golang:1.22 sh -c 'gofmt -l -w . && go vet ./...'`.
- Deploy core: `docker compose up -d legion-core` (after the build above; same image). **Only when party-relevant code
  changed** (party.go, partyroles.go, humanparty.go, ai.go fighting/healing, ...) and you want Ayla's friends on it -
  it logs the three out for ~1 min. Otherwise the core keeps running the older build, which is fine.
- Rotating a member of Ayla's party: change `LEGION_CORE` in `.env` (both services read it - they must match, or a bot
  is double-logged or orphaned), update `data/human-parties.json`, then `up -d legion-core legion`.
- rAthena config / NPC changes need `docker compose restart rathena` (kicks everyone, including Ayla; she reconnects).
- DB: `docker exec -i ro-db mariadb -uragnarok -pragnarok ragnarok`. Bot accounts `lg0001..` (password `legion`),
  account ids 2100001-2199999. Owner account `player`/`player` (id 2000000, group 99) holds Admin, kitty (200001, Doram)
  and Ayla (150010). Re-seeding: `docker compose run --rm legion seed ...` (see legion/README.md; always `-keep` the party).
- Dashboard: <http://127.0.0.1:8090/dashboard>. Health JSON: `/api/overview`. Parties: `/api/parties/detail`.
  Guilds: `/api/guilds/detail`. Minds bus: `/api/minds...` (see AGENTS.md).

---------------------------------------------------------------------------------------------------------------------
## 4. Ayla and the live view (the fiddly part)

```
 OpenKore (ro-openkore-ayla, X-Kore 2) ----logged in to----> rAthena (account player, char slot 2 = Ayla)
      ^  proxy: account 172.18.0.3:6901, char :6902, map :6903  (XKore_publicIp = rathena container IP; binds AND advertises it)
      |
 Wine client ~/roclient/RO-AILab.exe (login address patched to 127.0.0.1:6901 -> docker publish -> proxy)
      on Xvfb :98 1280x720  --x11vnc (view-only, :5998, -scale 1.39)-->  xtigervncviewer window on the owner's desktop
```
- **Ayla's Haiku mind picks where the party grinds** (owner order 2026-11-13: "let AI make the right grinding
  decision", Prontera/pay_fild01 too slow): it checks party levels (`/api/parties/detail`), `legionctl spots <lv>`,
  picks the best map that is safe for the lowest member, moves with `ayla.sh map NAME`, explains via
  `legionctl think ayla ... --kind decision`, re-evaluates every ~20 min.
  **It looks up game data before choosing** (owner rule 2026-11-14, RULES.md 5): `legionctl drops "<item>" --for Ayla`
  (who drops it at what rate, maps ranked by drop rate x spawn count, graded vs the party's levels, danger),
  `legionctl mob "<monster>"`, `legionctl mapinfo <map> --for Ayla`. It never says it can't find a drop table.
- **Game-data lookup for all minds** (`legion/lookup.go`, 2026-11-14): `GET /api/lookup/item|monster|map?q=<name|id>
  [&lv=69,70,72]` answers from the rAthena tables legion already loads (mob_db Drops/MvpDrops, item_db, npc/re/mobs
  spawns; dungeon/field from the spawn script folder). Fuzzy names (case, `_`, word prefixes "blue pot", initials
  "SS card", small typos); items/monsters that really spawn rank first; `matches` lists the other candidates.
  Unit test `TestLookupMatcher`; `LEGION_RATHENA=/rdata go test -run TestLookupLive` checks real data and, with
  `LEGION_LOOKUP_SERVE=0.0.0.0:18090`, serves the endpoints for a legionctl dry run without a full legion.
- **OpenKore plays Ayla** (fights, loots, levels; `attackAuto 2`, `lockMap` = the map the mind chose). The watch client just shows
  what OpenKore sees and does. The owner's own Windows client is NOT involved.
- **`openkore-ayla/plugins/cmdfile/xkore2fix.pl`** (loaded via `loadPlugins_list` in `control/sys.txt`) is required:
  OpenKore's X-Kore 2 proxy was written for older clients. Found by recording the real server's traffic to this client
  (`tools/rogui/realcap.py`) vs the proxy's (`tools/rogui/pktlog.py`). It makes the proxy send `0AC4` (server list
  with a 16-char web token; was `0069`), non-zero session IDs, a `099D` page for every `09A1` (OpenKore alternated
  with a PIN request), `0AC5` map info on char select (was `0071`), and lets the client close login/char connections
  (the proxy closing first -> "Disconnected from Server"). Base::Ragnarok::* modules compile only when X-Kore 2
  starts, so those overrides are applied lazily in `mainLoop_pre`.
- If the rathena container is recreated and its IP changes from 172.18.0.3, update `XKore_publicIp` in
  `openkore-ayla/control/config.txt` (and the `in_game` check in `ai-client.sh`), then restart `openkore-ayla`.
- **Scripts** (`G:\RO\legion\tools\rogui\`, run in WSL as long-running commands):
  - `ai-display.sh`: Xvfb :98 1280x720 + x11vnc + TigerVNC viewer window. Restart all three if the view freezes.
    **View size** (owner 2026-11-14 "double the UI window", then "too big now, keep 3/4"): the game and Xvfb stay at
    1280x720; x11vnc serves the screen scaled by `VIEW_SCALE` (default 1.39 -> 1780x1000 viewer window; 1.85 =
    2368x1332 was too big; the owner's screen is 1440 physical px high, exact 2x = 2560x1440 would need scrolling).
    Restart trick: kill with bracketed patterns (`pkill -f "[x]11vnc -display :98"`) so pkill doesn't hit its own shell. Normal window, not fullscreen. Changing it needs only a
    restart of `ai-display.sh` (kill the script, x11vnc and xtigervncviewer, NOT Xvfb), never of the client.
    Revert: `VIEW_SCALE=1` (edit the default or start it as `VIEW_SCALE=1 bash ai-display.sh`). From Git Bash start it
    with `MSYS_NO_PATHCONV=1 wsl.exe -d Ubuntu-24.04 -- bash /mnt/g/RO/legion/tools/rogui/ai-display.sh` (else the path is mangled).
  - `ai-client.sh`: waits until Ayla is online, starts the Wine client, logs in through the proxy (agree -> Enter ->
    player/player -> Enter -> Ayla slot (545,170) -> Game Start (1040,530) -> closes tip box), re-logs after drops,
    stops after 3 failed logins in a row (log `/tmp/roai-restarts.log`, client log `/tmp/roai.log`).
  - `keeper.sh`: checks the DB `online` flag of Ayla (char 150010) every 20 s; offline ~5 min -> restarts
    `ro-openkore-ayla` once, then only logs (`legion/data/watchdog.log`). It never touches the client.
  - `ayla.sh`: the Ayla mind's interface: `log [N]`, `chat`, `status`, `cmd TEXT`, `party TEXT`, `say TEXT`,
    `whisper NAME TEXT`, `map NAME` (writes OpenKore commands to `control/commands.txt`).
  - `ro.sh`: xdotool actions on :98 (shot/click/key/type...). `PLAYER.md` is the older brief from when Haiku played
    Ayla by clicking the client; OpenKore plays her now, so `ro.sh` is only for client housekeeping.
- **Wine client facts**: private copy `~/roclient` (WSL home), prefix `~/.wine-ro`, `DATA.INI` without HD sprites,
  exe byte-identical to `G:\RO\kRO-210607\client\RO-AILab.exe` except the login address string (4 occurrences of
  `127.0.0.1:6900` -> `127.0.0.1:6901`).
  - Do NOT set the large-address-aware flag (that copy dies right after server select under Wine).
  - 32-bit Mesa llvmpipe ran out of address space (`GL_OUT_OF_MEMORY in glBufferStorage` -> access violation). Fixed by
    env `MESA_EXTENSION_OVERRIDE=-GL_ARB_buffer_storage LP_NUM_THREADS=1 LP_NATIVE_VECTOR_WIDTH=128` and registry
    `HKCU\Software\Wine\Direct3D` `csmt=0`, `VideoMemorySize=256`.
- **Ayla leads her party** ("a true party leader, protect and guide its party instead of just running ahead"):
  - Plugin `openkore-ayla/plugins/cmdfile/partylead.pl` (in `loadPlugins_list`): while any online partymate is
    on another map or more than 9 cells away, Ayla stops random walking and lockMap routing and waits.
  - If a partymate is more than 16 cells away on the same map, she walks back to them.
  - She still fights anything that attacks her or a partymate (`attackAuto_party 1`). The log shows
    "[partylead] holding for the party: ...".
- **Watch-client crash on unknown NPC sprites**: some NPC IDs have no sprite name in this client. It then tries to
  load `sprite\npc\.spr`, shows "Resource File Loading fail" and crashes. Fixed with `~/roclient/npcfix.grf` (a
  fallback sprite at exactly that path, first in DATA.INI); the same files are also loose in `~/roclient/data/sprite/npc/`.
- **Watch client crashed every ~30 min (OPEN, 2026-11-14)**: the crash logs (`/tmp/roai-crash-*.log`) all showed
  `LLVM ERROR: Unable to allocate section memory!`, then `GL_OUT_OF_MEMORY in Resizing framebuffer` and the Wine
  lock timeout. llvmpipe's LLVM JIT keeps compiling shader variants inside the 32-bit client's 4 GB address space
  (RSS was only ~0.5 GB and flat, so this was not a RAM leak) until a JIT allocation failed. softpipe (no JIT) was tried
  via `ROAI_GALLIUM=softpipe`: the screen stays black and the scripted login fails, so the default stays llvmpipe.
  Next ideas: a 64-bit host GL path (new-WoW64 Wine), or a newer Mesa. Window size and
  VIEW_SCALE are unchanged. A changed export only takes effect when the ai-client.sh loop is restarted.
  **Fix found, waiting for main to switch (2026-11-14 16:05): new-WoW64 Wine.** Measured with `/proc/PID/maps`
  (address space below vs above 4 GB): the live classic client (system Wine 9.0, 32-bit Mesa 25.2.8 / LLVM 20 inside
  the 32-bit process) starts in game at 3,780 MB of its 4,096 MB and grew to 4,047 MB in 20 min (crash zone); RSS is
  only ~0.4 GB. Under WineHQ **wine-stable 11.0 in new-WoW64 mode** the same exe runs in a 64-bit host process: Mesa,
  libLLVM and the JIT live above 4 GB (libLLVM mapped at 0x7236...), the 32-bit window holds only the Windows side and
  stayed flat at 1,778 MB for 28 min on a test display (:97, separate prefix, exe copy renamed ROTEST.exe; login
  screen only - no safe test account, so in-game rendering under WoW64 is verified on the first real switch). Agree and
  login screens render normally, first screen in 20 s. Mesa was already new (25.2.8), so a newer Mesa alone would not
  help; LP_* knobs only slow the growth.
  - Install (done, no system packages touched): `~/wine11-wow64` = `wine-stable`, `wine-stable-amd64`,
    `wine-stable-i386` 11.0.0.0~noble-1 from dl.winehq.org extracted with `dpkg -x`, then `lib/wine/i386-unix`
    deleted (without the 32-bit unix side, Wine 11 runs 32-bit exes through wow64). Debs kept in `~/winetest/deb`.
    Prefix `~/.wine-ro-wow64` = copy of `~/.wine-ro`, upgraded once with `wineboot -u` (csmt=0 and
    VideoMemorySize=256 kept). `WINEDLLOVERRIDES="mscoree=;mshtml="` stops the Mono/Gecko install dialog (it blocked
    the first boot). Keep `~/.wine-ro` untouched: it is the fallback for Wine 9.0 (an 11.0-upgraded prefix must not go
    back to 9.0).
  - Switch: patch `tools/rogui/ai-client-wow64.patch` adds `ROAI_WOW64` (default 0 = unchanged). Apply it by writing
    a new file and `mv` over the old one (never edit a running bash script in place), stop the loop, end the old
    client with the SYSTEM `WINEPREFIX=~/.wine-ro wineserver -k` (the new wineserver only knows the wow64 prefix), then
    start `ROAI_WOW64=1 bash /mnt/g/RO/legion/tools/rogui/ai-client.sh`. Revert: restart without `ROAI_WOW64`. Check after
    the switch: `/tmp/roai-restarts.log` no longer shows drops every ~30 min, and the client's below-4 GB total stays
    flat (awk over `/proc/$(pgrep -f '^RO-AILab.exe')/maps`, addresses < 0x100000000).
  - Test hygiene for the future: never name a test exe RO-AILab.exe (ai-client.sh `pkill -f RO-AILab.exe` would kill
    it and its `pgrep` would take it for the watch client); use another display (:97) and another prefix.
- **Known launcher gap**: `ai-client.sh` checks "Ayla online" from the last 60 s of OpenKore logs, so right after an
  OpenKore restart it can try a login too early (one wasted attempt, then it recovers).
- **OpenKore gotchas**: never send a bare `ai` command (it TOGGLES the AI off; use `ai on`). Never set `lockMap` to a
  town (she just sits). `attackAuto 1` only fights back; use `2`. A restart takes ~1-2 min ("server still recognizes
  your last connection" is normal, it retries).
- X-Kore 3 was tried earlier and is broken with this client (OpenKore never relayed the server list).
- **Ayla's own stats on the dashboard** (2026-11-13): plugin `openkore-ayla/plugins/cmdfile/statusout.pl` (in
  `loadPlugins_list`) writes `logs/status.json` every 3 s (hp/sp/zeny/weight/levels/map/pos, `ts`). `legion` and
  `legion-core` mount `openkore-ayla/logs` read-only at `/ayla`; `legion/humanstats.go` reads it (ignored when > 30 s
  old) and fills her row in `/api/parties/detail`. `plugin load <name>` takes the plugin NAME as a regexp, not a path.
- Party dashboard shows each member's weight vs limit (`w cur/max (pct)`, client units = server/10): amber >= 50%
  (natural regen stops), red >= 90% (can't attack).

---------------------------------------------------------------------------------------------------------------------
## 5. Legion: what exists (G:\RO\legion\legion)

Key files: `bot.go` (protocol/session), `ai.go` (think loop, fighting, potions), `nav.go`, `strategy.go` (117 reachable
hunting spots), `builds.go` (13 build plans: stats/skills/gear to 99; research in `research/proposed_builds.md`),
`party.go` + `partyroles.go` (role parties, bait one-hit tagging, even share), `humanparty.go` (parties led by a
human: members follow the human's map, wait at the checkpoint town when the map is too dangerous; pinned characters
in `data/human-parties.json`), `guilds.go` (10 GuildDefs, leaders chosen by ambition, found at Base 15, recruit by fit;
roster `data/guild_roster.json`), `danger.go` (escape with Fly/Butterfly Wing/run, easier map after 3 escapes),
`sale.go` / `vend.go` (shops, "S>" chat rooms), `minds.go` (minds bus), `dashboard.go/.html` (dashboard),
`partydash.go` (`/api/parties/detail`, dashboard "Parties" card), `memory.go` (per-player memory + diary).

### General bot rules added 2026-11-13 evening (owner's words in quotes)
- **Level, don't march** ("prioritize levelling up rather than walking around"; "Stop the march"):
  - `goodHere()` (ai.go): a bot on a map that fits its level and holds <= 70 bots stays and levels there.
  - Gear trips (builds.go): none before base level 10; only to the nearest shop within 5 map changes; a gear trip
    that times out isn't retried that session. Sell/potion trips go to the nearest town with a tool dealer, not the
    home town. Before this fix 500+ Novices were walking to `yuno_in01` / `alberta_in`, and the average level stayed ~1.
- **Kafra fast travel** ("they have kafra for fast travel"; "make kafra teleport cheaper"; kafra.go):
  - A bot in a Kafra town whose route saves >= 3 map changes by teleporting goes to the Kafra Employee and picks
    "Use Teleport Service" -> destination.
  - Prices are 1/10 of official (60-180z) via `rathena/npc-custom/functions_kafras.txt`, a copy of the stock file
    mounted over `npc/kafras/functions_kafras.txt`. (Defining F_KafSet a second time doesn't work: the stock file
    loads after the custom ones and wins.)
- **Save at the Kafra nearest the grinding ground** ("save the spawn at the closest kafra near grinding ground"):
  a bot heading to a hunting map that passes through that map's nearest town saves there (Kafra menu "Save").
  Ayla's mind does the same with OpenKore `talknpc`.
- **All shops at Prontera's south gate** ("all shops to be in pron", "south gate of pron should be the market place"):
  - `rathena/npc-custom/legion_shops.txt` (generated from every normal NPC shop) puts Weapons/Armor/Tools/Goods
    merchants at prontera 142-168,57-60. Legion parses it (npc-custom is mounted into ro-legion).
  - Player vending pitches (vend.go `initMarket`) are at the south gate too: x 133-172, y 45-78, with the road
    x 152-160 kept clear.
- **Tanks lead** ("the tanks must be a lot more active. It cannot sit back"; party.go): a tank in a party pulls the
  next monster near the group by itself, and rests at half the usual HP threshold.
  2026-11-14 (`legion/tanklead.go`, owner: "attacking the mobs more aggressively instead of sitting behind"): measured
  before, member tanks fought 38% of samples and trailed "with/following/waiting/regrouping" 27%; non-tank leaders
  engaged first while their tanks followed 7-12 cells behind. Now: `tankTakeAggro` (Provoke or hit any monster
  targeting a member) runs BEFORE the tank's own target, for member tanks and tank leaders (hunt); the member tank
  keeps fighting its target up to together-4 from the leader, pulls around the leader or the group's front
  (`tankFront`: 5 cells ahead on the leader's walk, pubState DX/DY; pull radius 12, never beyond 16 from the leader)
  incl. the leader's own target, and with nothing to pull walks at the front ("leading the way for X") instead of
  trailing; a non-tank bot leader assists its tank's target and holds a fresh pull up to 3 s per 8 s
  (`leaderDefer`, "letting X pull Y"); ranged/melee members assist the tank's target when the leader has none; a
  party tank doesn't sit for its own rest while a member fights nearby (`tankHoldsRest`, HP >= 30%). Party rests
  (restGuard) and escape are unchanged. Test: tanklead_test.go. Check after deploy: share of "tanking/pulling/taking/
  provoking/leading the way" in tank states vs "following/with"; leaders "letting X pull" should be brief.
  Crowds (owner: "knight can pull but must use AOE instead of escaping"; Echobunny winged off 8-12 monsters at 46-48%
  on pay_dun01 and left the priest): no pull limit. A party tank with potions or a priest within 14 cells is exempt
  from escape()'s "N monsters on me"/"losing" triggers (`tankStands`/`tankHolds`); it wings out only under 10% HP,
  under 20% with no potions or no priest near, vs a monster 8+ levels above, or a boss. With 3+ monsters on it and the
  priest 6+ cells away it walks the crowd to the priest ("bringing N monsters to X", `tankFallBack`); its crowd skills
  fire from fight() (Bowling Bash/Brandish = weaponskill.go crowd table, Magnum Break ai.go, Grand Cross skillbuff.go);
  fighting a single monster it keeps SP for floor + 2 crowd casts (`tankAoEReserve`, in fight()'s reserve), and with a
  crowd on it drinks an SP potion when the next crowd cast doesn't fit (`tankCrowdSP`). HP potions: survive() (50%+).
- **Melee must walk into reach itself** (2026-11-14, ai.go `closeIn`): rAthena never chases for a player; an attack
  on an out-of-reach target gets 0x0139 and is dropped. fight() closes in first and re-sends after a 0x0139. This
  was the real cause of "swordman standing still, not attacking". Debug any combat problem with the per-bot packet
  trace (`legion/trace.go`: env `LEGION_TRACE=Name` or `GET /api/trace?bot=Name&on=1|0`, kept in `data/trace.txt`;
  lines `TRACE <name>` in the container log).
- **Party stays together** ("party should generally stay close to each other while grinding for safety"):
  - `partyLeash = 8` (party.go): members only start fights within 8 cells of the leader and catch up beyond that.
  - A bot party's leader waits for stragglers before the next pull (ai.go hunt).
- **Ayla's party picks its own grinding spot**: that's the Haiku mind's job (see section 4).
- **Party rallies at the destination's Kafra** (owner rule 2026-11-13, `legion/rally.go`): `Party.Rally` holds the
  rally (town, Kafra x,y, dest, start, all-arrived, cooldown, human's hunting map). `rallyManager` (every 3 s) starts a
  rally when a party is formed with members off the destination (`rallyOnForm` in formParty) or when online members
  are on different maps for >45 s (stragglers <=2 maps from the destination get 3 min to catch up; never during a
  party town run or while someone waits at the checkpoint on purpose). Rally town = nearestTown(dest); spot = the
  town's Kafra nearest the exit warp toward dest (`rallySpot`). Members (and a bot leader) in `rallyThink` (ai.go,
  after tripThink): travel there (fight back), save (`kafraTrip{Save}`), wait within 4 cells sitting. Ends when every
  online member (and the human: in the town, within 12 cells if seen) is within 12 cells and saved (or 3 min after
  all arrived) -> "all here, let's go", 3 min cooldown; 12 min timeout -> abandon, 5 min cooldown.
  Party chat line "meet at the Kafra in <town> (x,y)" is fixed wording: Ayla's partylead.pl parses it, and the bots
  join a rally Ayla calls with the same line (`rallyHeard`, bot.go 0x0109). Ayla: `start_rally`/`rally_tick` in
  partylead.pl route to the Kafra, `talknpc x y c r~/Save/ n`, sit, wait for all within 12 cells, restore lockMap.
  Her old cross-map chasing (ai_route to the straggler's map) is replaced by calling a rally. State shown in
  `/api/human-party` (`rally`) and `/api/parties/detail` (`rally`) + the dashboard Parties card.
  `GET/POST /api/rally?party=<name|human|leader>` shows / starts a rally. Rally radius = together_cells (20).
- **Together = 20 cells** (owner 2026-11-14): `tune().TogetherCells` (`together_cells` in tuning.json) is the one
  threshold for member regroup (party.go), bot-leader collect (ai.go), rally completion and dashboard `with_leader`.
  partyLeash (8) still limits where members start fights. Ayla's plugin: TOGETHER 20 / RESUME 17; waits for followers,
  walks back only for a friend that needs help (asked / HP<50% / not moving 30 s), finishes that commitment.
- **Stuck watchdog** (`stuckWatch`, rally.go, every think): sending walks but same cell 20 s -> close NPC dialog
  (last id from 0x00b4-0x00b7), cancel trade, leave chat, stand, drop route/path, step to a free cell. Logged as
  "stuck at ...: unsticking" events. travel() and the regroup branch stand up when seated.
- **Town runs from the human**: only when the human is inside a town building (shopping), not walking through town.
- **Rally kill switches** (tuning.json): `rally_enabled` (bot parties; default false = OFF since 2026-11-14 00:28 -
  bot-party rallies stalled guild leader parties legion-wide) and `rally_human_disabled` (Ayla's party; false = on).
  Rally max 5 min, 30 min between rallies of the same party, members with no route to the rally town are left out,
  the rally Kafra must be reachable on foot from the town spawn without warps (`kafraReachable`, else spawn point).
- **Rally progress watchdog**: a rally whose "distance left" score doesn't improve for 2 min is abandoned (5 min
  cooldown); a completed one has a 20 min cooldown. Ayla's own rallies: 20 min after success, 10 after a failure.
- **FindPath warp rule** (nav.go): only the goal's warp box is exempt; the box the bot stands next to is exempt only
  for its ring, never its trigger area (bots arriving at prontera 47,228 from prt_in walked straight back through the
  door warp 50,228 - prontera <-> prt_in loop).
- **Collect/regroup hysteresis** (`collect_stop_cells` 8 in tuning.json): a bot leader's collect and a member's
  regroup start beyond together_cells (20) and keep going until within 8; the leader keeps one straggler until done.
  Ayla's plugin: waits when a friend is > 20 away, carries on when all are within 12 (RESUME).
- **Ayla knows where her friends really are**: partylead.pl reads Legion's core API (`127.0.0.1:8091/api/human-party`,
  same network namespace, every 4 s, ~1 ms) and uses those positions when a friend is out of sight (the party list's
  positions go stale or are missing; missing ones used to be skipped, so she ran ahead with holding=0).
- **No lone walk back after a death**: when a friend that was out hunting shows up in a town (respawn or a solo trip),
  Ayla holds the party; in a dungeon she first leads it out to the first map outside the dungeon on the way to the
  friend (e.g. pay_arche) and waits there until the friend is within 20 cells (7 min max) - `back_tick`.
- **Ayla's random walk is short**: partylead.pl hooks `ai_processRandomWalk` and walks to a random cell within 12
  cells (OpenKore's own picks any cell of the map); `route_randomWalk_maxRouteTime 10`. Passing through a town she
  also stops while a friend is > 20 cells behind or still on the previous map (3 min max per stop).
- **Ayla's gear plan advanced (2026-11-14 10:40Z)**: she owns Jur [2] (equipped since 08:04Z). The 10:2xZ Prontera run
  did NOT buy a second one (no 19,500z debit in her log; gear_run_end printed "bought" for "owned" - fixed: it now
  compares the count before/after). Step 3: `buyAuto Muffler` / `buyAuto Shoes` (Payon Armor Dealer payon_in01 7 119,
  no minAmount, maxAmount 1, zeny >= 9000), equipAuto robe/shoes; step 4: `gearPlan_item Cap` (15000, Prontera Armor
  Merchant 146 57, `buyAuto Cap` disabled+gearRun). gear_run_start never enables a gearRun slot for an item she owns;
  after a run the plan advances via `gearPlan_next` ("Item=zeny;..."). In-memory config was set with `eval` (no full
  `reload config`, which resets lockMap); config.txt carries the same blocks for the next start.
- **Mind -> plugin orders** (partylead.pl `Commands::register('partylead')`, via commands.txt or `ayla.sh`):
  `partylead rally cancel` / `partylead rally <town>` / `partylead goto <map>` / `partylead status`. The mind's
  explicit order wins at once (QA: the mind said "no rally, wait at pay_dun00" and the plugin kept walking to the
  Kafra). `ayla.sh map NAME` now sends `partylead goto`. Documented in the legion-session skill for the ayla mind.
- **Ayla's gear plan** (2026-11-14). She is an Assassin (STR 49 / AGI 67 / VIT 1 / INT 1 / DEX 15 / LUK 31,
  Katar Mastery 4, Sonic Blow 7, Double Attack 10) who was wearing only Knife [3] + Cotton Shirt with ~10k zeny; her
  friends have ~1k each, so she funds herself (the Legion gear pool does not include her). Path (rAthena re prices;
  every item is also at the Prontera south-gate market, `npc-custom/legion_shops.txt`): **1** Adventurer's Suit 1,000z
  (DEF 20; Payon Armor Dealer `payon_in01 7 119`) -> **2** Jur [2] 19,500z (katar ATK 125, unlocks Sonic Blow; only
  Prontera `142 57` / Morroc) -> Muffler 5,000 + Shoes 3,500 (Payon) -> Cap 12,000 (Prontera) -> Boots 18,000 ->
  Manteau 32,000 (Payon) -> Katar [1] 41,000 or Jamadhar 37,200 -> Thief Clothes 74,000 (DEF 40, AGI+1, weight 10).
  Wooden Mail / Circlet / Biretta are not wearable by Assassins; Chain Mail (weight 330 of her 387) is too heavy.
  Implemented with OpenKore's own mechanisms in `control/config.txt`: gear `buyAuto` slots have **no minAmount**
  (then `shouldStartAutoBuy` never starts a trip for them; they are bought only inside a real sell/restock run) and
  `maxAmount 1` (equipped items count, so no Main Gauche loop); `equipAuto_0` wears them (`armor Adventurer's Suit`,
  `rightHand Jur [2]`); `attackSkillSlot Sonic Blow` only `whenEquipped Jur [2]`. The Jur slot is `disabled 1` +
  `gearRun 1`: partylead.pl `gear_run_start/gear_run_end` turns a **real-need** town run (sellAuto at
  itemsMaxWeight_sellOrStore 48%, potions <= 10, or a friend's overweight call) into a Prontera south-gate run when
  she lacks `gearPlan_item` and has `gearPlan_zeny` (21000): sellAuto_npc and every buyAuto npc are swapped in
  memory to their `pronteraNpc` twin, the slot enabled, and all put back when the errand ends (max one per 30 min;
  Kafra Payon<->Prontera is 120-240z). Next step: when she owns the Jur, set `gearPlan_item` to the next Prontera-only
  item (or add Payon items as plain no-minAmount slots). items_control: OpenKore's default "worthless gears"
  (Adventurer's Suit, Thief Clothes, Chain Mail, Main Gauche, ...) were set to sell; all switched to keep (owner rule:
  weapons/armor never go to an NPC). `attackEquip_rightHand Main Gauche [3]` was cleared (she'd have swapped to it).
- **Ayla's gear path v2 + her purse: plugin `aylagear.pl` (2026-11-14 ~18:40Z)**. It replaces the gearPlan_item /
  pronteraNpc twin system above (partylead's old gear_run_start stays as a dead fallback: `sellAuto_pronteraNpc` is gone).
  - `ayla.sh give NAME ZENY [why]` (console `give "NAME" ZENY why`): RODEX zeny mail (open -> write NAME -> name check ->
    zeny/title/body -> send -> cancel-write -> close; rAthena fee 2%), one gift at a time, then party chat "sent Echobunny
    22k for the X". Verified 18:32Z (Echobunny's core diary: "got zeny by mail from Ayla"). Legion friends need no change:
    mail.go collects zeny mail from any sender.
  - `gearPlan_list` (config.txt) = ordered "IDS=BUDGET; ..." (first id = the piece, others count as owning it; "CARD>ITEM"
    = compound the card into that item). Sellers come from `:8090/api/lookup/item` (npc_shops). On every real-need town
    run partylead calls `aylagear::run_start(huntMap)`: the first unowned piece she can pay (NPC) or that a Prontera vendor
    lists (player-only, `/api/market`) picks the town = the seller closest to the hunting town (%NEAR, Kafra hops; a
    friend's "can pay" shop town gets a 2.5-rank bonus, at most once per 30 min); `sellAuto_npc`, every buyAuto slot with
    `townShop tool` and `storageAuto_npc` (town Kafra) point to that town, and slot `buyAuto Gear Slot` (`gearSlot 1`)
    gets the item id / NPC / zeny. partylead announces "town run to <that town>" (fixes "said payon, went to geffen").
    `run_end()` restores everything; a vendor piece is then bought in Prontera (walk to the shop, `sendEnteringVender`,
    buy if price <= budget, partylead paused via `aylagear::busy()`), never tested live yet (no vendor had one).
    Player-only pieces nobody vends: one "B> <item>, paying up to Xk" on #trade every 30 min (Legion has no code that
    answers a human's B>, so this is flavour until the economy side adds it).
  - Equip: every 10 s an owned-but-unworn plan piece is worn; first `equipAuto_0_<slot>` is set to it (configModify,
    file + memory) so equipAuto doesn't put the old piece back, and a new weapon replaces the old one in every
    `*_whenEquipped` (Sonic Blow is "whenEquipped Jur [2]"). Card: equipAuto slot cleared, item taken off, `card forceuse`,
    carded item worn.
  - Her list: Boots 18k -> Manteau 32k -> Clip [1] (player-only: no NPC sells 2607 here) -> Creamy Card into it ->
    Katar [1] 41k (Prontera/Morroc) -> Coat 22k -> Helm 44k -> Thief Clothes [1] (player). **No Chain Mail / Padded
    Armor**: weight 330 / 280 of her 420 capacity (status weight 714/4200 is in tenths).
  - `ayla.sh gear` prints the plan; console `gear dry [map]` shows the town/piece a run would pick, `gear applytest [map]`
    applies + restores the switches, `gear set KEY VALUE` / `gear unset KEY` edit %config in memory (no `reload config`).
- **partylead fixes (same time)**: `partylead rally <town>` from the mind replaces a running rally in another town (it was
  silently ignored); while a rally runs, a lockMap set meanwhile is stashed in `$saved` and cleared (OpenKore routed to
  gl_church and the rally back to the Geffen Kafra every 15 s); town_of_map knows gl_* / orcsdun (Geffen), in_sphinx /
  moc_pryd (Morroc). `goto_tick`: a mind's goto stands her up and starts `ai_route` itself (retried every 10 s; also
  after 15 s of an empty ai_seq off her lockMap when not holding/resting) - she sat at the Morroc Kafra with lockMap set.
- **Strong-party respawn + danger priority (2026-11-14 ~19:10Z)**: back_tick - in a strong party (party_strong: all
  lv50+) a friend that reappears in a town (death/respawn) gets "X come back to <map>, kafra to <town>" and comes by
  itself; no rally, no leaving the hunting map (weak parties keep the town rally). `%dangerFrom`: a fight-help call
  ("help! 6 mobs on me", "dying", "heal pls", ...) counts as danger for 90 s, like HP < 50%: she turns back for it even
  in a strong party, back_tick starts nothing while a friend on her map is in danger, a self-started rally (`self`
  flag) pauses (rally_tick returns 0, the collect logic goes to the friend) and travel mode is off so she fights.
- **Verified Kafra save + rally destination (2026-11-14 ~19:20Z, owner rule RULES 2 "save at the Kafra nearest the
  grinding ground", enforced)**: rally_tick saves with `talknpc x y c r~/Save/ n` and counts it only when the Kafra's
  "...has been saved here" / "Saved." line arrives (hook `npc_talk` -> npc_said) in that town, up to 3 tries; then
  `saveMap` is written (configModify) and she says "saved at <town>". Friends' "saved at X" lines are tracked
  (%friendSaved); the rally ends when everyone is within 12 cells and has said it (2 min grace after all arrived, since
  the Legion side may not say it yet) with "everyone saved at X? ok going in, <map>". `partylead goto <map>` whose town
  (town_of_map) is not her saveMap starts with a save-stop rally at that town's Kafra. `partylead rally <town> [map]`
  (ayla.sh rally TOWN [MAP]): a mind-ordered rally drops the old lockMap; with a map the party departs there after
  regrouping, without one everyone waits at the Kafra for a goto (verified live 19:15Z: "Kafra: respawn point saved in
  payon", "rally: respawn point saved at the payon Kafra (verified)").
- **aylagear: #trade deals, popups, gear town (2026-11-14 ~19:05Z)**.
  - Deals (Legion humanwant.go answers her "B> X, paying up to Nk"): `heard_pm` reads offers in whispers (an unowned
    plan piece's name + "12k"/"12,500z"); within the piece's budget (and zeny + 2% + 1k) she answers "deal, mail it" (a
    vendor offer: "ill pass by ur shop"), above it "cant do 90k atm" (verified 19:03Z with a test whisper). `mail_tick`
    opens the mailbox every 3 min (8 s after "sent, check ur mail", 5 s after an unread notice), reads each mail with an
    attachment, takes a wanted piece at the body's "for Nz" / the agreed price (no price = gift), takes zeny, leaves the
    rest (whispers "sorry, cant take"), then pays each seller by RODEX (give queue, `sale => 1`, thank-you whisper,
    3 retries, else an UNPAID line). Every step -> `logs/deals.log` + console "[aylagear] DEAL: ..."; `ayla.sh deals`.
  - Popups (owner rule RULES 5): the watch client never gets the server's answers for actions the plugins drive:
    `Network::Receive/willMangle`+`mangle` drop rodex_* and vender shop packets while a give / mail check / vendor buy
    runs (+5 s), and npc_talk*/npc_store*/npc_sell_list/storage_* while sellAuto/buyAuto/storageAuto run or partylead's
    Kafra save sets `$aylagear::quietNpcUntil`. Shot after two gives (shots/aylagive_after.png): no mail window.
  - Gear town = the town of her current hunting map (lockMap -> town_of_map, else saveMap); a piece is bought only if
    that town sells it (vendor pieces only on a Prontera run); no detour for gear, no friend-steered town. run_end puts
    back only switches still holding the value it set (a mind's `conf` meanwhile wins). Clip [1] -> Belt [1] (2627, NPC
    20k Prontera/Aldebaran; Clip has no NPC seller), Creamy goes into the Belt.
- **Farm, don't wait (2026-11-14 ~19:30Z, owner: "as soon as priest is there, they can already start farming", "farm
  while waiting", "ayla just walking around in circle")**: partylead holds for stragglers only while the healer
  (Priest/Acolyte by `class` from the core API, `healer_near`) is NOT within 15 cells; with it, she farms and the rest
  catch up; pace_ok (sweep steps) also follows only the healer then. She turns back only for a friend hurt / in danger /
  asking for help (no more "not moving / not coming" fetches). `wait_farm`: while she does wait on a field (hold, dungeon
  exit, party rest she doesn't need) she attacks monsters within 12 cells of the wait spot and walks back to it;
  never in town. Sweep: `mob_target` walks toward the nearest monster in sight with no other player within 6 cells, else a
  spot where monsters were seen in the last 2 min, before the fixed waypoint loop. `crowd_check`: 60 s with nothing to
  fight on her hunting map -> "map too crowded: ... pick another map" in the console + `logs/alerts.log` (5 min apart).
- **One hunting map, no shifting (2026-11-14 ~19:45Z, owner rule RULES 3)**: partylead keeps `$homeMap` = the map the
  mind chose last (goto, `rally <town> <map>`, its own `conf lockMap` seen while not holding, a lockMap set during a
  rally). restore() and the post-rally departure go back to homeMap, never to a stale lockMap from an older hold. The
  plugin starts no destination change of its own: back_tick no longer calls a town rally or walks out of the dungeon
  for a respawned member (she says "X come back to <map>, kafra to <town>, we wait here" and waits farming; strong party
  or healer present: no wait at all); a friends' "meet at the Kafra in X" is joined only when she is not on her hunting
  map. Self-started trips left: the real-need town run (returns to homeMap) and the goto save stop.
- **Renewal maps in OpenKore (2026-11-14 ~20:30Z, Ayla stuck in Izlude)**: the OpenKore image's fields/portals for
  izlude were the classic layout (268x268) while the server runs Renewal Izlude (268x300): every getRoute in it failed.
  A check of all 1,296 maps in the server map caches (db/map_cache.dat + db/re/map_cache.dat, re wins) against
  fields/*.fld2.gz found 13 mismatches: izlude, morocc, alberta, iz_ac01, iz_ac02, iz_int, prt_castle, sec_pri,
  int_land(01-04). New fields generated from the map cache (gat 0->1 walk, 1->0, 2/4->1, 3->5 water, 5->10 cliff;
  mapping checked on payon/prontera/gef_fild10/pay_dun03) are in `openkore/fields-re/`, copied into the running
  ro-openkore-ayla (docker cp; .dist/.weight deleted to rebuild). `openkore/fix_re_portals.sh` rewrites the izlude
  lines of tables/portals.txt to rathena npc/re/warps/cities/izlude.txt (Kafra 128,148, exits 20,98 / 110,182 /
  52,172, ship landing 195,212); applied live + `reload portals`. Offline A* test on the new fields: izlude 195,212 ->
  20,98 (235 steps), 128,148 -> 110,182, morocc 156,97 -> 160,260 all route. routeweights.txt: izlude 500 (was 10000
  for an hour). Persisted in `openkore/Dockerfile` (COPY fields-re + the portals patch) - takes effect on the next image
  build; a container recreate before that loses the docker-cp fix. Open: `portals recompile` (LOS for the new izlude
  portals) blocks OpenKore for a while - run `ayla.sh cmd portals recompile` once while she's safe in town.
- **RODEX read is broken on this PACKETVER; help calls really fetch (2026-11-14 ~20:50Z)**: `rodex read` answers with
  0x0B63, unknown to this OpenKore ("Packet Parser: Unknown switch: 0B63"), so aylagear's mail step 3 timed out every
  3 min and lunaqueen's Creamy Card (accepted 19:39 for 8,700z) sat in the mailbox. aylagear no longer reads mails: the
  list gives sender / title / attachment, the price is the whispered deal (%offers, rebuilt from deals.log ACCEPTED
  lines after a restart); `rodex getitems <id>` (1-3 digit ids by list index), pays only after the item count rises,
  logs TAKING / GOT / FAILED / LEFT. Mails with items from a party friend = gifts; anything else without a deal stays.
  partylead: heard() no longer answers a help call with "omw" (that promise came with no action); a help call counts
  in a strong party too (she goes; only a merely-far friend comes by itself); the needy list puts the healer first;
  cross-map help fetches work in a strong party; goto_tick and off-map routes wait while a friend on her map called
  for help / is in danger (help_pending). The "til ~16:50" plan line is the ayla mind's text: the containers run UTC.
- **Gear turn, storage, portal recording, help vs goto (2026-11-14 ~21:00Z)**: aylagear asks the core
  `/api/party-turn?human=Ayla` (cached 60 s): while the `gear_pool_turn` member's piece is unpaid (have < price) Ayla buys
  nothing for herself (next_piece returns nothing, new #trade offers get "not now, saving up for a friend's gear") and
  each town run mails that member the shortfall +5% (keeps 5k; once per member per 30 min; TOPUP line in deals.log).
  items_control.txt: her worn / planned gear (Boots, Manteau, Belt [1], Creamy Card, Katar, Coat, Helm, ...) is
  `0 0 0` - `all 0 1 0` had made storageAuto put the new Boots in Kafra storage; a plan piece found in the storage list
  counts as owned (never re-bought) and the next town run takes it out (getAuto_0) for equip_tick. `portalRecord 0`
  (it rewrote portals.txt on every warp and recompiled LOS ~30 s, freezing her; payon_in01's field equals the server's -
  the getRoute errors came from standing on its 26,115 internal warp cell). partylead: the goto branch no longer
  returns before the hold logic while a friend here called for help (help_pending), "21 on me" / "cant find u" count as
  danger, the fetch uses the friend's live map, and goto_tick does not pull her out of town during the post-run Kafra meeting.
- **ginvite by name (2026-11-14 ~21:20Z)**: guildrecruit.pl `ginvite <name>` resolves the account id from a player in
  sight, else `persona.account_id` of `/api/bots/<name>` (:8090, then :8091); it prints "ginvite SENT to X (account N)"
  and, from ZC_ACK_REQ_JOIN_GUILD 0x0169, "ginvite RESULT for X: ACCEPTED - joined Starveil / declined / already in a
  guild / guild is full" (tested: LilRyuheart -> "already in a guild" - it is in Starveil's member list).
  `ginvite <account_id> [name]` still works. fix_re_portals.sh writes no Kafra lines (fix_kafra_portals.py owns them and
  runs after it in the Dockerfile); `portalUpdatePosition 0` + `portalRecord 0` in Ayla's config.
- **Death handling (2026-11-14 ~21:45Z, Ayla died 20:14 in pay_dun02, dead on screen 70 s, then idle 4 min in Payon)**:
  partylead and aylagear do nothing while `$char->{dead}` (their attack/route/fetch calls displaced OpenKore's `dead`
  task, processDead re-queued it ~40 times and the 4 s respawn timer kept restarting); on death she says once "argh
  died, respawning in <save>, regroup at the kafra"; the town run after a death says "i died and respawned here, regroup
  at the kafra" (not "bag full"). In town a sitAuto no longer blocks storage/sell/buy (she sat 4 min at the respawn
  point). No looting with 3+ aggressives or < 60% HP; protect_tick finishes the current target and adds no new monster
  while she is under 50% HP or has 3+ on her. config.txt: `useSelf_item Yellow Potion, Orange Potion, Red Potion`
  at hp < 50% (timeout 0.5, notInTown 1 - she drank 22 Red Potions standing in town), `buyAuto Orange Potion` (40, 50z),
  `teleportAuto_hp 35`, `teleportAuto_minAggressives 6` / `InLock 7` (was 10% / off: teleport only at 3% HP).
  Not done: waiting for a priest's Resurrection before respawning (no res AI known on the Legion priest).
- **partylead 20:20-20:41 (container clock UTC) fixes**: one fetch target is kept until reached (<= 4 cells), gone, on
  another map or 2 min (its last known cell bridges API gaps; was 8 changes in 2 min); "hurt" = real HP < 60% (party list,
  else the core API hp "x/y"), a "N on me" line counts only under 80% HP; the fetch log names its own reason; status
  prints the target's name. A goto issued in town with the party scattered first rallies at that town's Kafra (the plan's
  "meet at the kafra"), then departs together. A single friend's "overweight / bag full" is its own errand ("butterfly to
  town and sell, we keep farming"); 2+ within 5 min make a party run; the run's line gives the real reason (friend names,
  "i died ...", "restock" under 45% weight). A friend already on her destination map counts as ahead (no hold by the
  portal); back_tick only reacts to a fresh core-API town map (stale party-list maps had sent friends standing on
  pay_fild10 to Payon) and its "come back" names her hunting map. aylagear: one top-up per member until the topped-up
  piece is in that member's inventory (core /api/bots, logs/topups.json), and 10k (or her restock cost) always stays.
- **items_control names, wings, gifts, pkt2021fix (2026-11-14 20:50-21:05 container time)**: OpenKore ignores numeric
  item-id lines in items_control.txt, so `all 0 1 0` stored her Fly Wings (death to Eddga 20:49, no wing to teleport)
  and other kept items: 1,311 id lines rewritten to names (from OpenKore's %items_lut dump, slots as "Name [n]"),
  consumables (Fly / Butterfly Wing, potions, gems, Magnifier, Arrow) `0 0 0`. config: `buyAuto Fly Wing` min 2 / max
  10, `getAuto Fly Wing` min 3 / max 10, `teleportAuto_useSkill 0` (no Teleport skill: Fly Wing directly). aylagear:
  `giftItems "Item=Friend; ..."` - fetched from storage on the next storage visit (getAuto_2) and mailed with the item
  attached (`rodex add`), counted as that friend's turn help (Great Bow [3] -> xXRinfangXx). In transit (travel mode)
  no loot (itemsTakeAuto / itemsGatherAuto 0) and protect only within 5 cells. A mind's goto with the party scattered
  rallies at the current town, or the destination's town when she is on a field. homeMap always follows a non-empty
  lockMap. pkt2021fix.pl (from openkore-qa: RefineUI 0AA2/0AA3, NPC market 09D5/09D6, RODEX read 0B63) is in her
  plugins + loadPlugins_list and in openkore/plugins-extra (Dockerfile COPY to plugins/extra). Mail with zeny from a guild
  mate collected 20:53:32 ("GOT zeny by mail from LilGhostgirl").
- **10k zeny floor (owner rule RULES 5, 2026-11-14 ~21:15 container time)**: aylagear `FLOOR => 10000`, `can_spend($cost)`:
  `give` refuses ("give BLOCKED: Ayla must keep 10k ..."), the give queue drops gifts / top-ups and holds seller
  payments (HELD line, 3 retries, then UNPAID in deals.log) when the mail + fee would leave her under 10k; #trade offers
  are accepted only above it; vendor buys and the gear slot (`buyAuto ... zeny >= price + 10000`) too; top-ups keep
  max(10k, restock cost). cmdfile.pl blocks `rodex setzeny N` and `deal add z N` that would go under 10k ("BLOCKED ...
  blocked: Ayla must keep 10k"). Potion / wing restock stays allowed. Tested live at 5,399z: both guards fired.
- **Gift by trade, item cache, safe reloads (2026-11-14 ~21:20 container time)**: aylagear gives a `giftItems` piece by
  a direct trade (deal: open, add the item, OK, trade; no zeny, no fee) when the friend is within 12 cells - the Legion
  friend accepts its party human's trade, walks next to her and confirms; by mail only above the 10k floor. Deal windows
  are kept off the watch client. Item lookups are cached in `openkore-ayla/logs/itemcache.json` (pre-filled for her plan)
  and at most one uncached lookup per second - after a reload the plan was re-fetched in the main loop (likely cause of
  the 21:14 "Timeout on Map Server" kick). partylead: "hurt" = under 60% HP only (a help line at 79% is "asked").
  **No tests on the live Ayla** (use openkore-qa) and **reload only when she is idle in town**:
  `tools/rogui/ayla_safe_reload.sh PLUGIN...` waits for that (and for no rally/departure before partylead), one plugin
  every 30 s.
- **Escape fix (2026-11-14 ~21:30 container time)**: OpenKore's teleport task only runs while it is the CURRENT AI
  action; attack / route / loot queued on top (partylead protect / farm / fetch, attackAuto) left it waiting ("NPC or
  Teleport in queue", 43 s at 21:22:55, and the Eddga death). partylead now, while a teleport is queued, clears
  attack / route / move / take / sit actions above it and does nothing else until it is done. Escape thresholds are set
  per tick: with the party's healer on her map `teleportAuto_hp 30`, no aggressive-count trigger, plus a plugin escape
  at HP < 45% with 4+ aggressives (AI::ai_useTeleport(1)); alone `teleportAuto_hp 35`, `minAggressives 6` (InLock 7);
  MVPs/bosses keep mon_control's teleport-on-sight.
- **Town-run return + hold fixes (2026-11-14 12:40Z, lessons.md)**: kafra.go - a teleport for a town trip or by a
  human leader's member sets kafraNext +15 s (was 2 min: quick shoppers walked home 6 maps). humanparty.go humanState
  prefers Ayla's status.json position when newer than humanLocs. partylead.pl: a friend > 20 cells for 60 s is
  collected ("is not coming"); %helpFrom expires after 2 min and `goto` clears it; `partylead reload-cmdfile` reloads
  cmdfile.pl safely. cmdfile.pl blocks a party line naming a different meeting town within 10 min of the last.
  items_control: Phracon/Emveretarcon stored, not sold. Core changes BUILT, not deployed.
  Grinding area (12:43Z): partylead.pl anchors her grind (AREA_R 25, slow drift, checkMonsterAutoAttack skips far /
  near-portal unprovoked targets); PORTAL_KEEP 9 covers the pay_dun00 east stairs (184,33, x 182-186 y 26-40).
  REPLACED 14:00Z by a sweep loop (20-cell grid of open cells, nearest-neighbour loop, driven by the random-walk hook)
  and 'rest together' (she sits while a friend rests on its own; no collect for 'not moving/coming' during a rest).
- **Ayla's SP / breather** (2026-11-14): config.txt Sonic Blow only at SP > 40% (2 extra blocks: > 25% with
  `aggressives > 2` or a tough-monster list), sitAuto SP 10/40, HP 50/80, Red Potion at HP < 35%, SP items at < 12%.
  partylead.pl own_rest uses the same numbers; breather_line() says "sitting a sec, keep killing near me" (must keep
  matching together.go humanRestLineRe). Core together.go restFarmFor/restGuard 1b: while the party leader (not a
  healer) rests, non-healer guards fight any non-boss mob within restFarmCells (15) of it.
- **No split rallies for Ayla's party** (server or plugin): friends travel to her map on their own; a friend calling for
  help from a map of the same area (next floor/field) is fetched by her (`$fetchMap`, one trip, 3 min max).
  Rally protocol lines are lowercase and varied; parsers accept "meet|regroup|gather ... kafra in <town> (x,y)".
- **Far members regroup first** (party.go): beyond together_cells a member regroups even while a walk is in progress
  (the old `!moving` test let it fall through to fighting); a bot party's member on another map than the leader
  travels there; "waiting (coming to get me)" ends after 30 s unless the leader got 5+ cells closer; the leader
  finishes a collect before new fights (ai.go hunt), fighting only what hits it.
- **Bot-leader collect** (ai.go): only members beyond together_cells (20); the member being collected
  (`pubState.Collect`) waits instead of walking toward the leader, so only one side moves.

rAthena settings that matter: `conf/import/battle_conf.txt` `base_exp_rate: 1000`, `job_exp_rate: 1000` (10x);
`conf/import/inter_conf.txt` `party_share_level: 99`; battle conf `mob_count_rate 200`,
`guild_emperium_check: no`; `npc-custom/legion.txt` (job changes, auto skills for `legion_autojob` chars incl. Ayla,
one-time starter kit `legion_starter`).

---------------------------------------------------------------------------------------------------------------------
## 6. Current state (2026-11-13 ~19:40)

- ~700 bots online. All bots were reset to Novice Lv1 (re-seed ~17:28; 50 that had already changed job were reset
  again at 18:48 on request, backup `legion/data/backup-20261113-1848-novice-reset`). Many have legitimately reached
  first jobs again since (party EXP share is fast).
- Ayla: Thief Base 42 / Job 32, pay_fild01, played by OpenKore, visible in the watch window.
  Party "Ayla pt": Zendancer84 (Acolyte ~26), Zen.fox (Mage ~28) with her; Echobunny (Novice/Swordman, stuck on
  moc_fild02 earlier, offline at last check).
- Running LLM minds (Haiku, ~90 min runs, they stop by themselves): `ayla` (Ayla's mind via ayla.sh), `chat`
  (chat director). Re-spawn with the legion-session skill or the briefs in AGENTS.md. Watchdog agent finished.
- The owner's account `camera` was deleted (no GM watcher).

---------------------------------------------------------------------------------------------------------------------
## 6b. The minds are the core - hard gate
The owner: "the llm minds are the core of the server, the watch dog is supposed to watch out for them and keep them
alive". Required minds: ayla, chat, life, market, guild01-10, plus a watchdog agent that messages the main session
when one goes silent. `G:\RO\legion\tools\mindgate.py` runs as a UserPromptSubmit and a Stop hook (`G:\RO\.claude\settings.json`,
`G:\RO\legion\.claude\settings.json`). Any required mind silent for more than 5 minutes is injected as a warning and
blocks ending the turn until it's restarted. The minds are background Haiku agents that run ~90 min each and must be
re-spawned (briefs: skills in `.claude/skills`, Ayla's mind and the watchdog as in this file). Chat volume also comes
from Legion's chatter engine (`legion/chatter.go`: #global/#trade autojoin via the mounted `rathena/conf/channels.conf`,
~45 lines/min at 900 online).

## 6c. The self-improvement loop (owner: "I am trying to create a self learning/improving loop")
1. **Observe**: `G:\RO\legion\tools\sentinel.py` (WSL, long-running background command of the engineering session) checks the
   whole system every minute and logs to `legion/data/health.log`: population, kills per bot, share of frozen bots
   (it samples random bots), minds alive, Ayla online, her party together, the watch client and its launcher, chat
   volume, death rate. `tools/legion/classprobe.py [minutes] [per_class]` samples bots of every class on demand;
   `GET /api/bots/<name>` has a `mobs` list saying why each nearby monster is or isn't a target.
2. **Wake**: when a problem outlasts its grace time the sentinel prints the evidence and exits, which wakes the
   engineering session. The Haiku watchdog agent also messages the session (minds down, party split, deaths).
3. **Fix** the cause in the engine (not the symptom), deploy, and verify with the probe.
4. **Record** the lesson in `legion/data/lessons.md` (one line: what was seen -> the rule now in the engine) and
   here, then start the sentinel again.
Ayla's party is the test bench: keep the priest (Zendancer84) and ROTATE the other two members through the other
jobs so each class gets tested (`POST /api/human-party {"human":"Ayla","members":[...]}`). Next rotation planned:
Thornlord (Archer) and Ashwing (Merchant) once the current trio's gear purchase is verified.

## 6d. Guild leaders: always online, levelling first, leaderboard (2026-11-14, `legion/guildlead.go`)
- **One leader per guild, stable** (2026-11-14 fix): `data/guild_leaders.json` (guild id -> persona account) keeps the
  picks across restarts; the in-game guild master (0x016c `isMaster`, `noteGuildMaster`) always wins and swaps with
  the old pick. DB check 2026-11-14: Silver Lanterns master ThePuddinglord (guild02), Obsidian Edge master Sunheart
  (guild07) = engine picks; the other 8 guilds aren't founded yet (leaders found them at Base 15). `/api/bots` gives a
  leader `guild` = the guild it leads + `guild_leader: true`; `legionctl guild` prints a LEADER line; the legion-guild
  skill no longer says "highest-level member is the leader".
- Leaders = `pickGuildLeaders` (guilds.go), for guilds without a saved leader: the 10 most ambitious unpinned, non-`-keep` personas of the main instance,
  one per `guildDefs` entry (`guildLeader(id)`, `b.leaderVision()`). `b.alwaysOnline()` is true for them.
- Always online: bot.go `run` ignores the schedule and online slots for them, zero `sessionEnd` and no hang-out; ai.go
  never starts "calling it a day" and `logoutSignal` never fires; `kickSession` (human-party pins) is a no-op; after
  any disconnect the run loop logs straight back in (counted as `relogs`). They play their `main` character (pickChar).
- Levelling: `leaderPartyManager` (every 60 s) gives an online leader (basic skill >= 7) a tank/priest/ranged party of
  free guild members (in-game roster, then persona guild) within 10 levels, at `bestPartySpot` (strategy.go guide).
  party.go still disbands bot parties after 90 min; the manager re-forms one with a fresh spot a minute later.
  With `goal_parties_on` (lfg.go, see "Need-driven parties"): leaderGoalParty instead - the party forms around the
  leader's goal and ends with it.
- Leaderboard: levels of every Legion char seen online are kept in `data/levels.json` (offline chars keep their place).
  API: `GET /api/leaderboard?limit=N`, `GET /api/guild-leaders[/{guildNN}]`, `POST /api/guild-leaders/{guildNN}/party
  {"map","members"}`; `/api/guilds` and `/api/guilds/detail` gained leader/online/rank fields; dashboard: "Leaderboard"
  card + "[always on · #rank · pt N map]" in the guild table. legionctl: `leader`, `leader-party`, `leaderboard`.
- Before (2026-11-14, by /api/bots level): ranks of 3997 - guild02 #8 (lv28 Mage), guild07 #125, guild01 #656, the
  other 7 leaders lv1 Novices (#746), 6 of them offline by schedule.

## 6e. Economy (2026-11-14, economy engineer; `economy.go`, `vend.go`, `vendbuy.go`, `alchemy.go`)
- Owner: "the economy is the core of the game; bots develop naturally". No items are added to NPC shops (no NPC Blue
  Potion / Grape Juice; Blue Syrup 7,000z is the only NPC SP potion). Potions come from guild alchemists.
- Player trades (economy.go): price standard `priceOf`, B>/S> on #trade, econManager matches every 15 s, trade window
  with verification. Measured: works (2 trades, 0 failed in an hour), volume limited by supply (spare goods are cards
  and starter gear; wants are mostly NPC gear). Additions: `Want.Partial` (any amount), guild mates trade at 70% of
  ask, item gifts (price 0: the guild alchemist gives up to 5 potions per 30 min to its guild leader's party, served
  first), wants nobody has in person are matched against open vending shops.
- Vending (vend.go): vendor characters learn Enlarge Weight 5 -> Pushcart 3 -> Vending 3 first (builds.go
  `vendorFirst`; before, 42 of 43 had Mammonite and no Vending, so 0 shops ever opened). Prices = priceOf x vendor
  margin (0.95-1.3, market mind `--markup` multiplies the standard). Carts list loot, cards, spare gear, brewed potions.
- Buying from shops (vendbuy.go): 0x0130 -> 0x0b3d (64-byte entries for 20210107) -> 0x0801; vendor side 0x09e5.
  `/api/economy` has `vend_sales`, `vend_sales_zeny`, `shops` (open shops with listing and sales).
- Alchemists (alchemy.go): `alchemyManager` picks per guild (joined guild, else persona guild) the most advanced
  character with plan "alchemist" (levels of all chars from the char list); it is played more (pickChar +0.6). As an
  Alchemist it learns Potion Research 5 -> Pharmacy 5 first, buys the Potion Creation Guide (100k) and Medicine Bowls
  at the Guild Dealer (alde_alche 24,188, dialog + 0x0142/0x0143), bottles + Scell at the south gate Goods Merchant
  (trip "alchemy"), asks for Blue/White Herbs (B>), brews via 0x0438(228) -> 0x018d -> 0x018e -> 0x018f. Herbs are
  offered to alchemists and never NPC-sold; brewed potions are never NPC-sold. `GET /api/alchemists` (+ `potions`),
  legionctl `alchemists`, `potions`, `economy`. UNVERIFIED live: no character is an Alchemist yet (best Merchant job ~20).
- Zeny: bots only lose money (13.1M total vs 13.5M seeded; lv 30-44 avg ~800z). EXP 10x but drops 1x. Raising
  `item_rate_*` in rathena battle_conf is an owner decision (needs a rathena restart, kicks Ayla).
- Live after the 00:30 deploy (2026-11-14): 19 shops opened at the south gate within 12 min (48/72 vendors had
  Vending) but listed only NPC junk (Jellopy 3z) -> 0 sales; 0 item trades; party zeny gifts 61 ok / 51 "trade
  request not answered".
- Batch 2 (cards.go, lootvalue.go; built 00:50, deploy pending): card compounding (a card that suits the build goes
  into a free slot of worn gear: 0x00ab unequip -> 0x017c -> 0x017d; card value = item_db script bonuses x build stat
  goals), card wants for free slots, bare wants (no weapon / no body armor -> cheapest usable item on the board),
  item variants (a want accepts Knife 1202 for 1201 etc.; `wantAccepts`), card standard 500 + mob lv x 250 (was
  20k+; 1,000 + lv x 400 in batch 2 still priced cards out of the 1-3k purses), slotted gear priced from its NPC namesake +25%/extra slot (`namesakePrice`), vendors stock cards/materials/
  gear/potions only (canVend: 2+ items worth 1,500z), every drop looted (the Greed roll is gone), broke bots
  (`shortOfZeny`: < 500 + lv x 80) prefer the guide map with the best NPC loot per kill (`mapLootZeny`, mob_db drops),
  market partners' trade requests accepted at once. /api/economy: `gear_gifts`, `cards_inserted`.
- Batch 2 went live at 00:44 (4 cards compounded in the first minutes). Batch 3 (built ~01:05): cardMob picks the
  lowest-level non-boss of a name (a lv99 event "Poring" made Poring Card 40k); vendors move NPC junk from the cart
  back to the bag (0x0127, bot.go handles 0x0125) and openShop lists only `vendable` items; "cheaper than NPC" wants
  only when the item or a variant is on the offers board (`onBoard`; they were 114 of 116 wants, all unfillable).
- Batch 4 (deployed 01:12 as image 18d7406b): never sell unidentified gear; a "no weapon" want is blocked by any usable
  weapon in the bag (acolytes bought Club after Club the server refused to equip: identify flag); slot wants (a card
  holder buys a slotted variant of its worn gear, compounds and wears it); failed inserts mark the slot; market trade
  approach checks `sameWalkRegion` (nav.go), the alchemy dealer walk uses `talkCell`. Live 01:15-01:30: shops list
  cards/materials (Lunatic Card 3k, Chonchon 1.4k, Phracon 300); trades: Hornet Card 1,000z x2, 10 Red Potions 110z.
- Built after (01:30, not deployed): card mob level ignores lv0 copies (Hornet/Chonchon priced 1,000 = lv0 copy);
  card wants skip a card already asked by 2x its sellers (25 bots wanted the same 2 Hornet Cards).
- Limits seen: party members trade on their own map only and cross-map meets need 5,000z+ (Kafra fees), so card
  supply scattered over maps rarely meets demand; vend buying is for solo bots (crossOK) or bots already in Prontera.
- Batch 5 (built ~01:50): guild fund (alchemy.go `guildFund`/`needsGuide`): the guild alchemist (Merchant job 30+ or
  Alchemist, no guide) gets up to 3,000z from a free guild mate on its map with 2,500z+ (once per donor per hour, a
  market Deal with ItemID 0 and Why "guild fund"; /api/economy `guild_fund_gifts`). Open shops are on the offers
  board (wants can target shop goods). A vendor-role alchemist receives while not vending (econSnap.idle).
- Batch 6 (deployed 02:04, dbf67c29): `match_misses` in /api/economy (per want: "no seller", "price too high", "too
  far / party", "buyer short of zeny"; "partner busy / not safe at start"; "deal ended: <reason>"). Cause of 0 trades
  01:33-02:04: partyTradeOK (death fix: a party member trades only within 10 cells) was not applied by the matcher,
  which kept picking far holders that startDeal refused. Now the matcher applies it, and delivery deals
  (`Deal.Deliver`) let a solo seller (crossOK) travel to a party member's map (item >= 500z, <= 3 maps unless
  >= 3,000z); the buyer keeps playing until the seller is within 8 cells. First data (9 min): no seller 373 (mostly
  SP potion wants: nobody brews yet), too far/party 49, price 14; guild fund 6 gifts (1.5-2.3k).
- Batch 7 (built 02:25): guild alchemist picks persist in data/alchemists.json (main instance writes) and change only
  for a whole tier (the 02:04 restart re-picked 5 guilds, scattering the fund); gearReserve keeps a saving alchemist's
  money (zeny > 3k: all but 1k) from gear purchases and party gifts.
- Batch 8 (built 02:42, not deployed): the 02:35 deploy still re-picked 3 alchemists, because after a restart a
  character's levels are only the seed (Novice) until its account logs in, so any logged-in Merchant looked "a tier
  further". The current pick is now kept until its own levels are seen. data/alchemists.json was hand-restored for
  guild05-07 (Lily_dancer holds 1.5k of fund), but the running 02:35 instance may rewrite it: re-check after deploy.
- Batch 9 (built 02:51, economy engineer 3): (a) `browseShops` (vendbuy.go, called from town.go trip "done"): a
  non-vendor bot whose town trip ends in Prontera (solo or on the party run) looks over the open shops (econ.shops,
  refreshed every round) and buys ONE thing at a fair price: its #trade want (<= bid +10%), SP potions, a card for a
  free slot (cardValue >= 2, <= bid +20%), or a gear upgrade (<= standard +20%); 3-min shopping task, "brb buying X"
  in party chat. (b) Stockers: a vendor with Vending, not vending, cart < 3 items, lv15+, 1.5k+ zeny buys spare cards /
  valuable materials / slotted or drop-only gear at 90% of the standard (`stockWant`, "stock for my shop"; resells at
  95-130%). (c) Fetch deals (`Deal.Fetch`): the mirror of delivery - a solo buyer travels to the party member's map
  (follows it if the party moves), the party member keeps playing until the buyer is within 8 cells. (d) partyTradeOK:
  inside a town map party members trade at any distance. (e) /api/economy: `counts` (deal started/done/failed by kind
  same/meet/deliver/fetch/gift/town, stock-*; shop buy started/results; "shop browse: nothing for me"),
  `zeny_by_level`, and match_misses "too far: <which rule>".
- Batch 9 went live 03:35: first vendor sale of the day within 2 min (Lunatic Card 1,450z to a bot on a town run);
  4 shop sales (4,350z) by 03:41, but "shop browse: nothing for me" 25 vs 4 bought.
- Batch 9b (built ~03:40, deploy ~04:35; economy.go matcher): a delivery seller short of Kafra money (not crossOK,
  i.e. < 1,000z) still walks a delivery when it is lv15+ and the buyer's map is <= 3 hops away (median purse lv20-39 is
  ~600z, so most solo sellers could never deliver); and no delivery into a map too strong for the seller
  (`mapTooStrong(buyer.mapN, s.lv)`, same guard as fetch).
- Batch 10 (built ~04:20, economy engineer 4): (a) RODEX mail (`mail.go`): a bot can mail zeny to any character
  (0x0a08 -> 0x0a12 -> 0x0a6e -> 0x09ed, then ALWAYS 0x0a03: while "writing a mail" rAthena refuses item use and
  trades); every bot collects zeny mails on 0x09e7 (0x0ac1 -> 0x0ac2 -> 0x09f1 -> 0x09f2, then deletes 0x09f5).
  Fee 2% (mail_zeny_fee). (b) Guild fund by mail: guildFund prefers a same-map donor (trade, no fee), otherwise a free
  guild mate anywhere with 2.5k+ mails min(half of what it has above 1.5k, 3k) once an hour ("guild fund: mailed" in
  counts; /api/economy `mails_sent`/`mails_collected`/`mails_failed`). Measured before: 0 fund gifts in 70 min, 0
  guilds had a 2.5k+ member on the alchemist's map. (c) Diary fixes: deals say how they meet (fetch/delivery were
  all "same map"); purchases, gifts and the guide were written twice (event "market"/"info" already remembers);
  shop sales record the buyer's name in the trade history (was "(shop customer)"). (d) legion-life skill: trade talk
  must match `legionctl market` and real "sold/bought" diary lines (the life mind wrote invented trades into notes,
  e.g. "buys iron ore from Onistorm"). UNVERIFIED live: mail packets (watch counts "mail zeny: *").
  (e) "shop buy: shop did not open" (10 of 16 shop buys): rAthena refuses the WHOLE shop when one item is unidentified
  (vending.cpp `i != j`), but vend.go set Opened when it asked, so e.g. LilPixelfang Bazaar sat "vending" with
  looted unidentified Swords/Clubs and no shop. Now: ZC_ACK_OPENSTORE2 0x0a28 result 1 -> Opened=false ("shop open
  refused" in counts); cart items carry the identify flag (0x0b45 byte 13; the cart's equipment list 0x0b39 type 1
  is now parsed at login); unidentified items are not vendable; vendors identify drops worth 300z+ with a Magnifier.
  (f) Stockers stock by demand (`stockDemand`: buyer wants + past sales minus units listed in shops); a card with no
  listing counts as 1 demand; materials/gear need a buyer. (g) Party chat events are logged once with `heard_by`
  (eventRing.add folds the same party line within 3 s; inbox merges hearers): ~3,800 -> ~1 per line.
  (h) Spoken trip lines drop the log label (`spokenWhy`, chatter.go): no more "(on the party town run)" in party chat.
  (i) Party.ShareSet comes from the server's party option packet 0x07d8 (dashboard even_share was false for 258/259).
- Batch 11 (built 04:09, same image as 10): vendor couriers (vendbuy.go `courierReady/courierSpare/courierTake/
  courierEnd`, Deal.Courier, kind "courier"): a vendor whose shop has been open 5+ min offers its listing to party
  members' wants as a delivery; on a match it closes the shop (0x012e), moves the item cart -> bag (0x0127), walks /
  Kafras to the buyer's map like a delivery seller, and after the deal puts leftovers back in the cart (0x0126) and
  reopens. One trip per vendor per 20 min. Aimed at "too far: both in parties" (1,227) / "partner can't travel" (643).
- Batch 12 (shopcheck.go, built for 05:35): ghost shops off the market. (1) a buyer's "shop did not open" marks the
  vendor a ghost for everyone (`shopGhost`; shopListing/openShops hide it; vendThink closes 0x012e and sets up
  again; cleared only by 0x0a28 result 0). (2) every 2 min `checkShops`: a shop open 2+ min whose sign (ZC_STORE_ENTRY
  0x0131, AREA broadcast on open + sent on coming into view) no bot saw since it opened, while another bot stands
  within 12 cells, is a ghost. 0x0132 is NOT proof (rAthena sends it on out-of-sight too). Legion has no DB driver, so
  the `vendings` table isn't read. (3) `fixShopTitle`: a saved title naming cards/gear/materials the cart lacks is
  replaced by defaultShopTitle (e.g. LilPixelfang "Lunatic Card - quick sale" over Sword/Club).
- Batches 9b-12 went live 04:36 (772c432b). First 10 min: mails 64 sent / 64 collected / 0 failed; DB `vendings` =
  /api/economy shops (28/28: no ghosts; check with `select ... from vendings` in ro-db); shop buys 5 (5.1k), "shop did
  not open" 0; no courier deals (party wants are Blue Potions, no shop has them). 8 of 10 guild alchemists are
  Alchemists with Pharmacy 5 (Silent rogue, LilTacox, Blaze.mage, lilyrogue, Lily_dancer, Star song, ravenclaw85
  Bazaar; LilPuddingheart Merchant job 39); first fund wave took them to 3-18k.
- Batch 13 (built 04:55, a3c610eb): (a) MAMMONITE ate the guide fund: lilyrogue 11,166 -> 4,066z in 10 min, nothing
  in its diary, -200z per hit (Mammonite costs 100z x level; progress.go cast it at level zeny/1500). Now a guide
  saver never casts it; others need 2,500z+, level = 1 per 2,000z above 2,000. (b) alchemyWant only with the guide
  (savers bought herbs with fund money). (c) the guide errand runs even in a party once zeny >= 103k (the party check
  blocked it forever). (d) a guide saver counts as shortOfZeny (loot-rich maps). (e) tripGear/gearFromShop: on a
  Prontera trip, gear a south-gate shop sells at <= NPC price is skipped at the NPC and bought by browseShops
  (shopGearPick; counts "trip gear: player shop instead of NPC"). (f) /api/economy `match_misses_items`.
- Batch 13 went live 05:06: alchemists' zeny only rises since (Silent rogue 1.4k -> 4.2k, ravenclaw85 Bazaar 20.2k ->
  22.6k in 5 min; 0 drops in 40 polls).
- Batch 14 (built 05:25, f40edaa5): (a) alchemist pick after a restart: a saved pick that isn't a candidate yet (joined
  its guild in game, persona guild "": only counted once logged in) is waited for 20 min; an unseen current pick is
  kept only 20 min after start (Blaze.mage, guild03 Alchemist with 9.2k fund, lost the job to an offline Novice at the
  05:06 restart and could never win it back). (b) NEW refine.go: weapons refined to the safe limit at Hollgrehenn
  (prt_in 63,60) via the refine UI (feature.refineui on): 0x0090 talk, mes/close2 (answerStrayDialog 0x0146), 0x0aa0
  window, 0x0aa1 <idx> -> 0x0aa2 materials {itemId.L chance.B zeny.L}, 0x0aa3 <idx><mat>.L<0> -> 0x0188 <res><idx>
  <refine>, 0x0aa4 close. Weapon lv1 Phracon + 50z to +7, lv2 Emveretarcon + 200z to +6 (refine only at 100%
  chance; less = "safe limit", remembered per char+item). Starts after a Prontera trip ("done", after browseShops)
  with the material in the bag; refineWant (lowest priority, econWantAll last) asks players for the material with
  money above gearReserve+500 (lv12+, not vendors/guide savers). InvItem.Refine from the login equip list (byte 65)
  and 0x0188; Item.WLv/Refineable from item_db. Counts: "refine visit started", "refine: success/failed", "refine
  visit: <why>". Why: 338 Phracon / 115 Emveretarcon on the board, 0 buyers. UNVERIFIED live. (c) "cheaper than NPC"
  wants bid the lowest ask if it is <= 97% of the NPC price (346 "Adventurer's Suit: price too high": bid 900 vs 950).
  (d) vendBuyThink walks to talkCell(vendor) instead of the cell south of it (LilGhostx Shop at 136,60 with 136,61
  blocked: 6 of 8 shop buys after 05:06 timed out on "no path").
  (e) fixShopTitle also drops the stale title from data/shoptitles.json (dropShopTitle; Onistorm Deals' saved "Hornet
  Card, fair price" was reloaded and replaced on every open). (f) Alchemist supplies: needBottles keeps 30 bottles
  even without herbs (south gate Goods Merchant sells 1093 + Scell); a party alchemist with the guide that is OUT of
  bowls or bottles makes one solo restock errand (like the guide errand). (g) potBuyPlan: a guide saver's potion
  top-up above the survival minimum is not paid from the fund (reserve = gearReserve).
- Batch 14 live 06:05: 162 player trades in 8 min (Phracon 240-260z for refining; was 3 per 20 min); 154 same-map
  deals done. Onistorm stale title dropped. Refine visits got stuck in prt_in's wrong room (128,40).
- Batch 15 (built 06:20, 766d8459): refineThink routes inside Prontera with RouteMapsPoint to Hollgrehenn's room
  (wrong door -> out and round, <= 3 entries, "room can't be reached").
  Tested offline (copy of the tree + /rathena from the image, go test): every prt_in entry routes out to Prontera
  and in through prontera 179,184 -> prt_in 60,73 (Hollgrehenn's room). Also: econSpare keeps out refineKeep (the
  material its own weapon still needs) and anything bought within the hour (noteBought/boughtRecently; Phracon went
  back and forth between bots); the 20-min limit on unseen alchemist picks is reverted (it replaced 4 offline
  Alchemists holding fund); data/alchemists.json restored. Built 06:32, 2c6ac700.
- Batch 16 (built 06:38, 755423e4): potions - lv30+ take Red Potions unless they can afford an hour's stock
  (hpPotTarget) in Orange (was: unless they could afford potMin), gearKeep reserves hpPotTarget not potMin (baseline
  06:33: lv30+ median 15 HP pots, p25 9). Loot: spareGearFull (lootvalue.go) - a weapon/armor already held as an
  unequipped spare (vendors: 3) stays on the ground (baseline: spare weapons per lv30+ bot median 3, p90 7, max 15).
  Measure: sample 60 online lv30+ bots, count ItemID 501-504 and unequipped weapons in /api/bots/<name> inventory.
- Batch 17 (in the tree 06:43, parcel.go + mail.go): RODEX item parcels. parcelThink (economy round, <= 1 per 30 min,
  idle bots only): surplus gear (identified, unequipped, +0, no cards, not keepsGear, every copy beyond the first per
  item id, max 5) is mailed to the account's own vendor char, else a guild mate's vendor char (fnv-stable pick).
  Packets: 0x0a08 open -> 0x0a12 -> 0x0a04 <idx>.W <amount>.W per item -> 0x0a05 <result>.B... -> 0x0a6e (zeny 0) ->
  0x09ed -> 0x0a03. Receiving: mailbox 0x0ac2 entries with type&4 -> 0x09f3 <mailid>.Q <0> -> 0x09f4 (result byte
  11) -> delete 0x09f5; only below 60% weight. Counts "mail items: sent/failed (why)/collected". UNVERIFIED live.
  Gated: tuning.json parcels_enabled (default false) or parcel_test_bots "Name1,Name2" (re-read every 30 s).
- Batch 16 live 07:05; refining confirmed live (07:20: "refine: success" 10, visits 8, "out of Phracon" 3, "weapon gone" 2).
- Batch 18 (built 07:30, economy engineer 6): (a) parcels as written in batch 17 could never fire: 07:15 scan of all
  936 online field bots found 0 with 2+ IDENTIFIED copies of a weapon/armor - the spare piles are unidentified drops
  (lv30+: unidentified spare median 2, p90 10, max 19; Club 1501/1502 99+49, Knife 36, Sword 18 in 60 bots). parcelItems
  now takes unidentified copies too (one copy per item id stays, the identified one), only when worth a Magnifier
  (vendIdentifyWorth: weapon/armor with priceOf >= 100 - Club yes, Knife 50z no). (b) vendors identify for the shop:
  59 of 74 online vendors held unidentified gear (161 items), 2 had a Magnifier, the old threshold was 300z. Now a
  vendor (shop closed) with such gear and no Magnifier walks to the south gate Tools Merchant (prontera 164,57; trip Why
  "magnifier", run from vendThink because vendors never reach ai.go's tripThink) and buys one per item (max 15), then
  waits up to 20 s per item for identifyThink before opening. Counts "vendor magnifier trip", "vendor magnifiers
  bought". (c) restock: an open shop (20+ min) checks every 30 min; with 2+ listable items in the bag (or 2+ to
  identify) it closes (0x012e) and loads them into the non-empty cart (VendState.restock), then reopens. Count "vendor
  restock: shop closed". (d) Vending pitch refused: 07:25 13 of 35 vendors in Prontera sat "setting up shop" forever,
  re-casting Vending every 12 s and getting 0x0110 (skill 41) - pitches within 3 cells of the south gate merchants
  (min_npc_vendchat_distance). Now 0x0110 for skill 41 drops that pitch for the session and the vendor takes another
  (count "vend pitch refused: moved").
  (e) npcGearCap (economy.go askPrice + vend.go vendPrice): used weapons/armor an NPC also sells ask at most 93% of the
  NPC price (07:25: 33 Adventurer's Suit wants bid 910 vs 5 sellers at 990+, "price too high" 1,036).
  (f) party.go member loot: batch 16's spareGearFull (and heavyJunk over the regen line) were only in ai.go's solo
  loot; party members (most bots) looted every copy (lv30+ spare weapons median 3 -> 4, max 15 -> 27 at 07:27). Both
  filters now apply in partyThink's loot step too.
- Batch 19 (built 07:36, dietrich.go): refine materials. Stock rAthena Dietrich (prt_in 63,69, in Hollgrehenn's room,
  market shop: Phracon 200z, Emveretarcon 1,000z) - main's decision 07:40: players first, NPC when no player shop sells
  at or below the NPC price. refineAfterTrip starts a visit without the material when dietrichBuyCount > 0 (no open
  shop at <= NPC price via cheaperInShop, money above gearReserve+300); in the room the bot talks to Dietrich (menu 1,
  answered in answerStrayDialog via dietrichMenu), market open 0x0b7a (19-byte entries; 0x09d5 15) -> 0x09d6 -> result
  0x0b4e (result.W 0 = ok) -> close 0x09d8. Counts "refine material: *". GATED: tuning dietrich_enabled /
  dietrich_test_bots. Always on: npcMatCap (asks <= 95% of the NPC price, #trade and shops) and bids <= NPC price.
- Batches 18+19 live 07:44 (early deploy after a crash): first 2 min "vend pitch refused: moved" 11, vendor
  magnifier trips 26. Parcel test 07:48: parcel_test_bots = Raven rogue81, ironrogue, Ashrogue87.
- Batch 20 (built 07:55, refine.go refineTarget): refine only the weapon it keeps - lv20+, a class weapon
  (isClassWeapon, classWeaponMissing false), and not when nextGear()'s next buy is a weapon (archers refined the
  starter Knife before their Bow; lv1-15 Novices refined). Checked: "no path on prt_in" diary lines end 12:05:33Z (0
  after batch 15 went live); Phracon trade flip-flops after 12:06Z: 1 (storm_soul, across the 12:44 restart that clears
  boughtAt) vs Nekoqueen's 11 before.
- Parcel test 07:48-07:56: 3/3 "mail items: failed (no answer)", no disconnects. rAthena (PACKETVER 20211103) answers
  CZ_REQ_ADD_ITEM_TO_MAIL with 0x0b3f (64 bytes, RE >= 20200723), not 0x0a05. Fixed in mail.go/bot.go (built 07:58,
  batch 20); parcel_test_bots emptied until it is live. ALWAYS check a packet header against the server's PACKETVER
  in rathena src/map/packets_struct.hpp (DEFINE_PACKET_HEADER under the #if) - recvpackets.txt lists both.
- Dietrich test from 07:58: dietrich_test_bots = yukimonk, lilstormfire, MintBuffs, Kirabunny, TheSilentchan.
- Batch 20 also (built 08:05): refine visits end "left Prontera" when the bot is out of Prontera (Lilyknight started
  a visit, its party took the Kafra to Geffen, then it walked back from gef_fild00); "weapon gone" (7 of 14 visits
  07:44-08:00): 0x0aa0 takes the index of the weapon in hand, a stale index is re-asked once, and the reason now says
  "(not equipped)" / "(no item at index)" - check those counts after the deploy.
- PARCELS ARE UNECONOMIC: rathena conf/battle/misc.conf mail_attachment_price: 2500 - every attached item costs the
  sender 2,500z on send (mail.cpp mail_removeitem). Mailing Clubs (~120z) is absurd; parcel.go stays off
  (parcels_enabled false). Item mail only makes sense for goods worth well over 2,500z (good cards). Changing the
  conf is an owner decision (rathena restart kicks Ayla). Zeny mails: 2% fee only.
- Batch 20 also: a visitor out of material mid-visit (0x0aa2 "out of Phracon": 13 of 27 visits) closes the refine UI,
  buys from Dietrich (when allowed: no player shop at <= NPC price, dietrich gate) and talks to Hollgrehenn again
  (once per visit, refineTask.dietTried). The live Dietrich test (live code) only covers visits that START without
  material; this mid-visit path arrives with the 09:05 deploy.
- 08:28 Dietrich live test clean (Vexwing33: Katana bought on the party run, 0 Phracon -> 2 from Dietrich 400z ->
  Katana +2, packets 0x0b7a/0x09d6/0x0b4e right, no disconnect). dietrich_enabled = true in tuning.json (08:30).
- Batch 21 (owner rules: build gear table, wealth x rarity prices, boss items = a guild's fortune):
  step 1 buildgear.go (built 08:40): per build (13), level band (1-20 ... 81-99) and slot, the suitable items best
  first: "guide" (data/buildgear.json from the build-and-strategy agent; keys = builds.go plan ids; re-read every
  5 min), "plan" (builds.go routes), "auto" (in-world items the band's job can wear, class weapons only, ranked by the
  build's style); cards per band (guide + buildCardValue >= 4, no boss / far-too-strong monster cards). GET
  /api/buildgear?build=X, /api/buildgear/fit (share of online bots wearing table weapon/armor - the owner's measure).
  step 2 pricing.go (built 08:45): every 15 min priceRound measures the money supply (online zeny by level band and
  guild, estimated total) and prices cards + non-NPC weapons/armor: supply = copies held online (bags+carts) + a day
  of normal-monster drops (rate x spawns x 6 x 24); buyers = online characters whose build table/card value wants it;
  supply >= buyers -> old standard; scarce -> the purse of the (supply+1)-th richest buyer; boss-only (every spawning
  source a boss/MVP) -> the median guild's pooled online zeny. GET /api/economy/pricing (money_supply, history, top
  200). GATED: tuning dynamic_prices (standardPrice uses the table only when on; fixedStandardPrice = old formula and
  floor). Next: step 3 wants/adverts from the build table (guild mates first, cards to the builds that use them), and
  guild pooling for boss items.
  step 2b market.go (built 08:55, ALWAYS ON): supply and demand - every 2 min (marketTick from econRound) per item
  units offered (#trade spares + shop listings) vs units wanted: demand > supply -> factor x1.04 (max 2.0; 1.0 for
  NPC-sold), supply > 2x demand -> x0.97 (min 0.6), else drift to 1; priceOf = reference x factor, floor NPC sell
  price. Vendors undercut the lowest other shop by 2% (vendPrice). Markdown: a seller's listing unsold 45+ min loses
  5% per 30 min (max 6 steps), floor NPC sell price / half the reference for valuable goods; a sale resets the
  seller, a want resets everyone (log "markdown: ...", count "markdown"). /api/economy/pricing "market" (reference,
  factor, price, ask range, supply, demand) and "markdowns".
  step 3 (built 08:55): econWant also asks for build-table items (guide/auto, not NPC-sold) that someone offers
  ("for my build"); offer lines name the builds that want the item ("gd for hunters"); the matcher prefers guild
  mates. farm.go: mapTradeZeny (trade goods at market price per kill) added to the NPC loot value where broke bots pick
  the richest map (mapFarmValue: beginner.go, lootvalue.go richestLoot); GET /api/economy/farm?lv=N lists sought-after
  drops farmable at a level (map, monster, rate, zeny per kill) for the minds.
  step 4 guildpool.go (built 08:52): a boss-only item on offer that suits an online guild member's build -> the guild
  pools for its highest-level such member: guild mates mail it up to 5,000z each (once an hour, keep 1,500z; shares
  guildFundNext with the alchemist fund) until its purse covers the lowest ask; then it posts a "guild buy" want at
  that price (econWantAll first); gearReserve keeps the pooled money. One pool per guild, ends when bought / item gone /
  3 h / buyer offline. /api/economy "guild_pools", counts "guild pool: *".
- 09:10 first live price table: boss cards stayed at the old fixed 250k (max with fixedStandardPrice) and Holy Avenger
  450k. Formula fixed (built 09:10): boss-only = max(median guild online zeny, 20k) (153k at 09:06); plenty =
  min(standard, median buyer purse); scarce = max(purse of buyer #supply+1, that). Money supply 09:06: 1,010 online,
  2.14M online zeny, median 1,620, est. total 8.5M. Fit 09:05: weapon 72%, armor 95% (hunters 52%). Build-specific
  weapon types (planWeaponSubs: knight 2hSword, crusader 1hSword) lower other types' itemScore to ATK/3.
  dynamic_prices stays off until the fixed formula is live and checked.
- Batch 22 (built 09:45; owner: junk gear goes to the NPC): junkgear.go - plain weapons/armor with NPC sell price under
  100z, fewer than 4 slots, no cards, +0, not an upgrade/plan item (keepsGear; unidentified ones regardless) are junk:
  npcSellable = npcJunk || junkGear is used by the NPC sell list (town.go 0x00c7), hasLoot (trips, regenWeightNeed),
  lootValue and the arrow loot check; shops don't list them (vendable). Vendors with 5+ junk pieces make the south
  gate Tools Merchant trip (it sells first); vendIdentifyWorth needs sell price 100z+ or 4 slots (no Magnifier for a
  Club). Arrows (build agent): arrowTarget = min(3000, 1000 + lv x 30) (was 500 + lv x 10, ran dry in 10-35 min),
  top-up when below a quarter, paid from all zeny after the potion minimum (was half). Also: shops no longer list
  junk gear; /api/buildgear/fit "suitable_items_offered" (unsold suitable goods).
- 09:55 (built): card buyers in pricing.go = bots whose build table (band) lists the card (buildCardValue >= 4 alone made
  every card "wanted" by ~900 bots -> priced at a top-10 purse). /api/economy/pricing?q=<name>&all=1. Table at 09:39:
  boss-only ~150k (guild median 150.6k), Holy Avenger 225k (purse #2 of 50 buyers), no-buyer items keep the fixed price.
- 10:20 dynamic_prices = true (tuning). Cards at 10:15: 910 priced, 545 no buyer (fixed price), 315 boss-only (~150k),
  49 scarce (Raydric 14k, Andre 11k, Matyr 20k, Pupa 7k; median purse ~1.5k). Watch card trades / wants; markdowns start
  45 min after a listing is first seen (restart resets).
- Run 7 (10:20-10:55, built, waiting for a deploy): (a) vendor alchemists: vend.go closes the shop (alchShopClose, max
  1x/20 min) for the guide errand / brewing / supplies and runs the Why "alchemy" bottle trip (guild07's alchemist is a
  vendor: it sat at 102.9k forever); brewers never vend brew materials. (b) guide fund: the final gap is rounded UP
  (gifts of n/100*100 with a 300z minimum could never cover the last <300z; two alchemists stuck at 102.9k). (c)
  parcels: target = a guild mate's open Prontera shop only (the hashed offline vendor accounts never log in); no junk;
  1 item if NPC price 250z+. Send path verified live at the new 100z fee (2 sent, 1 server result 1, items kept).
  parcels_enabled stays false until re-tested after the deploy. (d) mailtrade.go: RODEX sales for cards/gear/valuables
  worth 1,000z+ between far-apart players (seller mails item, buyer mails price; mailOut.Counts for part stacks);
  tuning mail_trades / mail_trade_test_bots, off until tested. (e) hand-offs survive within 20 cells (together.go) and
  anywhere in town (90 s): "party mate too far" was 28/40 failed deals. (f) econSpare skips junk gear. (g) market
  demand counts only units the buyer can pay (Blue Potion bids spiralled to 8.9k from penniless guild-leader-party
  members); Dietrich items (npcMatPrice) have factor ceiling 1.0.
- Run 7 batch 2-3 (live 16:02 / built ~11:40 EST): parcels_enabled + mail_trades ON (16:10). Butterfly Wings follow a
  saving floor (town.go wingSaveFloor/bwWant; a Butterfly Wing is 1,000z in RE); partyRunTown sends the party to
  Prontera for a plain gear piece only if it costs >= 800 x members (fares are 120-180z). Money-flow measurement without zenylog: price
  picklog with the RE item db (lessons.md). Built for the next deploy: dealer trip drives its own Kafra trip (was a
  deadlock), guideSaving() locks the guild fund, dealer trip 45 min, alchemy errand (errand.go kind "alchemy");
  buildgear nextBuildPiece wired: econWant "for my build" first for drop/trade pieces, nextGear saves toward a shop
  piece <= max(3x budget, 5k), solo map picker farms a drop piece's Farm map when it is a safe candidate (80%);
  parcels only for items worth 400z+.
- Run 8 (2026-11-14 ~11:30-12:00 EST): batch 3 live 11:38 (deploy-20261114-113731) incl. the class-weapon ERRAND
  (errand.go kind "weapon" / "weapon (sell loot)": a party member missing its class weapon or with a weapon far below
  its level leaves from a field map - never a dungeon - buys it alone and comes back; ai.go skips the "waits for the
  next party run" line when it can leave; gearStepReady keeps it to 1/h). First guide bought: guild07 lilzensenpaiVend
  16:44 UTC (Geffen Kafra -> Al De Baran works; walking back via mjolnir kills). Built after (not yet deployed):
  Butterfly Wing price from the shops' real list (town.go wingPriceSeen; data.go loadItemOverrides reads the rathena
  db-import item_db.yml mounted into legion/legion-core) - bwWant thresholds scale with it (1.5 / 3 / 30 wings);
  potBuyPlan's HP top-up above potMin keeps wingSaveFloor (restocks left a median 1,049z); deal retry (Deal.busy: a
  buyer refusing while hit makes the seller wait; transient failures block the pair 4 min, not 30).
- Run 8 owner rules (live 12:00, deploy-20261114-115612): HEALING FIRST - healItem (Red..White Potion, Red/Yellow/
  White Herb, fruit/meat/milk; foodHeal) is vendable, loaded/listed first (vend.go healFirst), stocked first by vendors
  (vendbuy.go stockWant, ~60 units on the gate), kept from the NPC (food.go healKeepForShop) and parcelled to the guild's
  open shop (parcel.go parcelHeal, partial stacks via mailOut.Counts); browseShops buys HP potions <= 110% NPC price;
  farm.go tradeGood counts healing drops (broke bots' richestLoot). MARKET OPPORTUNITIES (opportunity.go, GET
  /api/economy/opportunities): score = (wants + sales + build buyers/5 [+30 healing]) - (listed + 3 x chasers), x price
  (healing x2); checkMarket after a Prontera town trip sets a 90-min money goal (item + safe drop maps), solo map
  picker farms it 70%; max 8 chasers/item. lfg.go hook: `b.MoneyFarmGoal(now) *farmGoal` (Kind zeny with Maps).
  MARKET TALK: after a check, maybe one #trade line (gap / spike / glut) - server-wide 1 per 50 s, an item once per 15
  min; a gap line is a 20-min rumor (score x1.5; bots without a goal take it up: "heard on #trade ...").
- Run 8 income (built 12:20, image 9435089): measured per bot-hour (picklog): lv60 ~3.6k loot value, ~40z/kill, ~90
  kills/h; only ~50% of non-vendors fighting (24% travel, 16% roaming). Cause: gear-poor bots on low crowded fields
  (moc_fild01 72 bots avg lv51 on lv7-25 monsters, 16z/kill) - EXP 10x vs drops 1x (EXP rate raised to the owner).
  zenyhour.go MapZenyHour(m, power, fam): value/kill (NPC + market trade goods) x kills/h (HP / dpsAt(powerLv) +
  crowd-scaled search), live / roaming share. Solo picker: best z/h among the near candidates (broke 85%, else 50%);
  goodHere: broke bots leave for a 1.5x map one change away; partyMapSearch / partySpotNear: 3k z/h = one map change
  (max 3). Offline sweep TestZenyHourLive (LEGION_RATHENA=/rdata LEGION_BOTS=snapshot from scratchpad ec8_snap.py):
  median expected z/h +17..46% per band, maps over cap 3 -> 0 (model ranks; absolute values ~3x the measured).
- Run 8 drop rates (built 12:32, b210598): owner set EXP 5x (12:23) and drops 5x at the next rathena restart.
  droprate.go effDropRate = base x item_rate_*/100 (battle_conf import, mounted ro into legion), capped at 100% per
  drop; used by mapLootZeny, mobTradeZ, farmTargets, pricing DropsDay, pieceFarms, MapZenyHour, lookup. Live switch in
  data/tuning.json: drop_rates_from_conf=true after the restart (default 1x), or drop_rate_mul=N.
  Guild brewers: parcel.go parcelHerbs - guild mates mail Yellow/White/Blue Herb + Scell stacks to the guild
  alchemist while it is online with the guide (brewerSeen). Guides bought: guild07 lilzensenpaiVend 16:44 UTC,
  guild06 Neko.fang 17:13 UTC (both then had ~1.5-3k zeny, too little to bid for herbs).
- Run 8 guild supply chain (built 12:56, c121bfc; owner rules 3b): guildstock.go, GET /api/guild-stock - every 5 min per
  guild: stock = its vendors' open shop listings (no guild storage at guild lv1, no Kafra-storage I/O), needs = members'
  next drop/trade pieces + 10 healing per member under potMin, gaps; up to 2 guild farmers (DS Hunters first, then
  Knight/Crusader/Monk; solo, free) get a gap item as money goal and say so in guild chat; the guild TRADER (vendor with
  the biggest open shop) receives parcels first, buys gap items first (stockWant), donates to the guild fund when not
  vending; guild vendors gift healing/potions to guild mates who ask. Creamy Card: every lv15+ char is a buyer in
  pricing, opportunity demand +50, creamyWant B> when affordable. Open: Kafra/guild storage I/O, trader junk sales with
  Overcharge, trader paying gear turns.
- Run 8 glut + markdowns (built 13:02, 9e8866c): market.go glutMul(id) - supply on offer > 3 x (demand+10) -> farming
  value x lim/supply (min 0.1; a "so many X" talk line x0.7 for 20 min); used by mobTradeZ (MapZenyHour, richestLoot),
  farmTargets, opportunities (glutted = skipped), guild farmers; vendors list max 10 of a glutted item, stockers skip
  it; vendPrice never below marketFloor (NPC sell value). Markdown clocks persist in data/markdowns.json (deploys reset
  them before the 45-min mark, so no item was ever marked down before).
- Run 8 late fixes (built 13:19, 726b87f): brewers' bowls/bottles/Scell never NPC-sold (junkgear.go npcSellable - the
  first 2 guides' 20 bowls each were sold at the next shop stop); in-game guilds are all at 16 members (lv1 cap) and 940
  online chars have none - leaders invite the guild alchemist + trader first, persona-guild invites always accepted;
  refiner/Dietrich walks give up after 2 FindPath failures; no refine of a weapon about to be replaced or a <200z
  starter; arrows are a party-member errand (errand.go "arrows"); weapon/arrow errands 20-min cooldown.
- Zeny 02:50 (736 online): total 918k; lv20-29 avg 754 / median 540, lv30-39 1,155 / 813, lv40-49 1,551 / 1,141.
  The 100k Potion Creation Guide is about one whole guild's online zeny (~90k): guild fund gifts (<= 3k, same map,
  1/donor/hour) won't get there in a day. Drop rates are the owner's call.
- Open: party members (most bots) can only trade within 10 cells or by delivery from a solo seller, so cards and shop
  goods rarely reach them ("too far / party" in match_misses). SP potions are the biggest unmet want (no brewer yet).
  Ideas: let party members buy from south-gate shops during the party's Prontera town run (trip in progress);
  vendors buying spare cards/gear from field bots to stock shops.
- Run 9 (2026-11-14 13:20-14:15, built c844a371 / 15c6590d): brewing - a guild alchemist that owns the guide is a
  "known brewer" (data/brewers.json, alchemy.go noteBrewer); its account is always scheduled online (ai.go
  ShouldBeOnline) and plays that character (persona.go pickChar). A broke brewer (< 3k, few bowls/bottles) is topped up
  to 4,000z by guildFund (fundTo) and keeps that out of gear (gearReserve). Pinned (human-party) alchemists may run the
  Alchemist Union errand (guide / bowls) and brew while seated (Silent rogue sat on 108k); humanPartySync stands down
  during an "alchemy" errand. Guild trader funds a stuck party gear turn ("party saving (Xz of Yz)", <= 5k, keeps 10k,
  not while vending; guildstock.go traderFundRound).
  Gear (Ayla's friends QA): townBuyNow - a bot standing in a town buys its class weapon / turn item / next piece sold
  there from its own spare (every 90 s check, a normal gear trip), not only on "town runs"; stickyStep keeps gear_next
  until bought unless a pick is x1.5 better or buyable now while the kept one is >3x the spare; giftableZeny (party
  gear gifts, guide fund, peco fund, guild boss pool, Starveil pool) keeps the reserve/fare, raised turn zeny, mail
  gifts of the last 90 min (earmarkZ, cleared by gear buys) and the own next piece when within 2x spare; pecoNeed holds
  only the rental, not the purse; headgear is compared with everything worn on the slots it covers (Goggles = upper+
  mid) with a 10% margin (equip flapping).
  Human B> (humanwant.go): a human's "B>/WTB/buying <item>[, up to Xk]" on a channel -> human want; a free bot with it
  as spare whispers an offer after 20-60 s (its ask, capped at the stated price, takes the price within 15%), mails the
  item, and the sale settles when it collects zeny mail from that buyer; a vendor listing it points to its shop. Main
  instance only. /api/economy human_wants.
  Measured: median purse lv60-79 1,127z (940 before), next piece affordable 238/885 (21/1010); Red Potion capped 10/shop
  (27 shops); markdown clocks persisted (1,808 at 13:27). Activity sample 13:40-13:51: fight/loot 31%, party "with
  leader" idle 16%, travel 18%, 107 bots with 3+ destinations in 18 min - the biggest income gap is time not farming.
  14:13 (60-min picklog, per bot-hour): NPC sales lv45-59 6.5k (2.8k at 13:20), lv60-79 8.8k (5.0k); NPC spend 5.4k / 7.7k;
  median purse lv60-79 1,666z; total online zeny 2.45M (2.07M). Markdowns live (60 by 14:13). Later fixes (66922bbd,
  2c74b114): party members buy in town only on their party's town run there; one errand per party at a time; a
  party gear run goes only to Prontera (never a far town chosen by shop distance - lillunashot's party ran to Yuno for a
  Hood). Open: trader junk sales with Overcharge (in-person handover), storage.go store-trip loop (reported).
- Run 10 (2026-11-14 14:50-, built): verified the 14:53 deploy (events 19:57-20:10Z): 233 NPC gear buys in ~13 min
  (525k zeny), 0 Yuno trips, errands one per party, humanwant not exercised (no human B> since). Brewing: Yellow Herbs
  join alchemyWant (Blue/White skipped when < 5 on the market: 18 Blue / 1 White vs 282 Yellow on offer); a party brewer
  with herbs but no bottles / Scell takes a "brew supplies" errand to Prontera's Goods Merchant (alchemy.go
  brewSupplyNeed / alchSupplyTrip; Neko.fang 9 Blue Herbs 0 Scell, Silent rogue 0 bottles); humanPartySync stands down
  for it. Market-mind fixes: deals hold the session (ai.go logoutSignal waits up to 15 min while b.mkt / a mail is
  open); a delivery follows the buyer's party to its new map (twice, safe maps only, else whispers "deal off");
  same-map deals with a SOLO partner: the solo side walks, the party member keeps playing (2 min) - "party mate too far"
  was 16 of 26 failures, same-map deals failed 19 vs 12 done; /api/economy deal_fail_reasons + counters_since, and all
  counters persist (econpersist.go, data/econ_counters[_core].json, saved every minute); shop titles are checked
  against the actual listing at every open (vend.go titleMatchesListing / listingTitle); shops stay open >= 40 min,
  restock at most hourly and only for 4+ "points" (card / healing stack = 3), courier deliveries 1 per 40 min; guild
  trader is sticky while its shop is open, /api/guild-stock stock_by_shop (stock = all guild shops together); cheap
  [4] gear (Club [4] 60z) is junk unless the market asks for it (marketDemand); trade lines (B>/S>/WTB/WTS) never get
  typos or cuts, typos only swap letters inside a lowercase filler word, slang swaps whole words ("B> aHt 930z",
  "BlueH erb"). Income (20-min picklog per bot-hour x3): lv45-59 NPC sell 4.6k / NPC buy 5.1k, lv60-79 7.4k / 5.8k -
  the NPC spend is mostly gear (good), Butterfly Wings ~0.5-0.7k/h; purses stay low because gear is bought.
  Loot flow (owner rule "Interesting loot", lootflow.go): cards / real gear / valuable Etc the looter doesn't use
  (keepsCard / keepsGear / refineKeep; a 2nd copy always flows) go party member who can use it -> guild mate -> the
  guild trader (stock, priceOf >= 400) -> sale, one step per item per 2 min, one gift per bot per round, by RODEX (1
  unit, 100z), with party/guild chat ("anyone need a X?" / "me!" / "np take it, mailed"; at most one exchange per giver
  per 4 min). econSpare and parcelItems hold an item until its flow reaches the sale; a bot with no party and no guild
  sells only duplicates. /api/economy loot_flow {kept, party, guild, stock, sold (= released to the market)} this run;
  cumulative in counts "loot flow: ...". Not for Ayla (human): her plugin would need its own offer logic.
  Unidentified gear (QA audit: Knife [4] 581, Club [4] 317, Sword [4] 242 copies online): the [4] exception now needs
  market demand, and an unidentified piece the build wants is kept once (firstCopy), the rest sold to the NPC.

## 6f. Wrong builds: audit and restart (2026-11-14, `tools/wrongbuilds.py`)
- Owner rule: a bot char whose skills/stats don't match its plan is restarted at Novice 1. `python tools/wrongbuilds.py
  audit` -> `legion/data/wrong_builds.json` (wrong list, order_only list, reset_log, Ayla-party and guild-leader
  sections). `python tools/wrongbuilds.py reset NAME,...` or `reset --all` restarts listed chars in place (same
  char_id/name/slot/plan, class 0, stats 1 + 48 points, seeded zeny, starter kit, legion_first/branch from the plan,
  home-town position; inventory/cart goods move to the account storage) - only bot accounts, never the core party,
  and only when every char of the account is offline (rAthena would overwrite the row / storage on save).
  Take a backup first: `docker exec ro-db mariadb-dump -uragnarok -pragnarok ragnarok char inventory cart_inventory
  skill char_reg_num storage ... > backups/pre-rebuild-<ts>.sql`.
- 2026-11-14 00:40: 14 wrong (all vendor Merchants with Mammonite and no Vending), 11 restarted
  (backup `backups/pre-rebuild-20261114-0038.sql` + `-storage-login.sql`); lilmoondragon, TheShadowmage Trade and
  rinmonk Mart were online - rerun `reset --all` later (they may have learned Vending by then; the audit drops them).

- AoE plan change (2026-11-14 08:26): builds.go `Build.Legacy` holds a plan's previous skill goals; wrongbuilds.py
  adds their caps, so characters built under the old plan are never "wrong". Use it whenever a plan is reordered.

## 6f2. AoE casters (2026-11-14, `legion/aoe.go`)
- `aoeCast` runs first in `fight()` for the Mage family: 3+ monsters inside an area spell's radius around a monster in
  range 9 -> Storm Gust / LoV / Meteor / Thunder Storm / Heaven's Drive (0x0366 UseSkillToPos) or Fire Ball (on the
  centre monster); SP reserve max(15%,30); no KS (claims of non-party bots, monsters fighting non-party players), solo
  no boss/swarm; quick spells only while hit without a party tank; solo Fire Wall vs. a walker. Tests: aoe_test.go.

## 6f3. Build-and-strategy review per class (2026-11-14, `legion/weaponskill.go`, `legion/data/buildgear.json`)
(Summarised per class, with the owner's class rules, in `CLASSES.md`; new per-class notes go there too.)
- A standing agent compares each class's build, fight skills and maps against Renewal guides (iRO Wiki) and fixes the
  engine. Plans must stay prefix-compatible (old goals go to `Build.Legacy`), so no character becomes a wrong build.
- Archer line: Arrow Shower (ground, 3x3 / 5x5 from lv6) on 3+ grouped monsters (`weaponAoE`); Improve Concentration
  self-buff at the highest level that leaves >60% SP (`selfBuff.cost`, `buffLevel`); `fightSkills` also runs for bow
  users at range (it used to stop at 3 cells, so ranged classes never buffed). Hunter plan: falcon, Blitz, Steel Crow,
  then Beast Bane 10, Arrow Shower 10, Owl's Eye 10, traps last (there's no trap AI).
- **PENDING: the falcon rental in legion.txt needs a rathena restart** (`rathena/npc-custom/legion.txt`: a char
  with Falconry Mastery and no falcon pays 2,500z for one on login or level-up). Before this, all ~21 Hunters had Blitz
  Beat 5 and Steel Crow ~10 but no falcon.
- Acolyte line: Heal is used as an attack on Undead-element monsters (`healAttack`: solo keeps 20% SP, a party healer
  keeps 50%); a solo support Acolyte casts Blessing and Increase AGI on itself (`fightSkills` used to skip every
  support-role bot).
- All classes: plants and mushrooms (no BaseExp, HP <= 20, `Mob.worthless`) are never picked as targets.
- Crowd skills (owner rule: AoE in crowds on the central monster): `crowdSkills` table in weaponskill.go - Arrow
  Shower, Brandish Spear, Bowling Bash, Cart Revolution, Magnus Exorcismus (undead/demon, Blue Gemstone, 4+) - cast on
  the centre of the best 3+ cluster inside the skill's real splash radius. aoe.go keeps the mage line.
- "key" self-buffs (`selfBuff.key`: Two-Hand Quicken, Adrenaline Rush, Improve Concentration, Auto Guard) are cast
  down to the 30% SP floor (the 60% rule kept Knights from ever casting THQ). Crusader: Auto Guard, Reflect Shield,
  Grand Cross (3+ adjacent, HP >= 70%).
- peco.go: Knights/Crusaders with Riding 1 (Cavalier Mastery no longer required, owner: Knights ride) and price +
  1,000z rent their Peco at the stock breeder through the NPC dialog (knight 55,350 / crusader 232,318); riding =
  0x0229 option bit 0x20, saved with the character. A party's town run goes to Prontera while a member wants one
  (partyRunTown); a solo rider's next trip goes there, plus one "Peco Peco ride" trip (3 h rest). Human-led parties
  (Ayla) choose their own town: Echobunny needs the plugin to take one run to Prontera. DB 09:55: 0/93 Knights, 0/72
  Crusaders lv40+ riding.
  **13:15 fix (root cause):** `optionSeen` was only set by a 0x0229 to ourselves, which rAthena sends at login only
  when opt2 (poison...) is set - so `wantsPeco` was false for almost every rider forever (DB 13:05: 1/125 Knights,
  0/91 Crusaders lv40+ mounted; LilZerofire 30,280z with Riding never tried). Now bot.go reads the saved option from
  the char list (CHARACTER_INFO.effectstate, offset 36). Also: rental + 200z (pecoReserve) instead of + 1,000z; a
  rider's whole purse is held for the Peco (`pecoNeed` -> gearReserve = Zeny; off for 6 h after 3 refused talks);
  retries after 40 min (pecoRetry; peco errand cooldown too) instead of 2-6 h; `pecoFundRound` (econ round): the
  richest free guild/party mate (keeps 2,000z, not a rider itself) mails a short rider its shortfall (rider 20 min /
  donor 30 min); Riding 1 is the FIRST Knight/Crusader skill in every plan and trans plan (owner: "official path to
  get peco as soon as possible"). /api/economy `peco_rented`, `peco_fund_mails`. Verified chain: Neko_sage85 (memory
  "rented a Peco Peco in Prontera", DB option 32) - NPC prontera 55,350 / 232,318, menu 1, setriding.
  Human-led parties (core friends, Echobunny): humanPartySync stands down while a Peco errand is open or a breeder
  visit runs (like the alchemy errand), and a human party's rally no longer blocks the breeder visit in Prontera.
  Trap errand (errand.go "traps"): a partied trapper Hunter low on Traps (trapTripDue) fetches them alone at the
  nearest Trap seller (trap.go startTrapTrip), 20 min errand cooldown; humanPartySync stands down for it too.
  Katar fund (peco.go katarFundRound, shared mateFundRound): an Assassin saving for its katar (coreweapon.go
  katarShortfall) gets part of it mailed by its richest free guild/party mate (half above 2,000z, <= 10,000z a mail,
  rider 20 min / donor 30 min; donors saving for a Peco/core weapon/guide never pay). /api/economy katar_fund_mails.
- Run 2 (09:20-10:00): bow users with 0 arrows buy up to the 50% weight line (the 45% potion cap made every arrow buy
  0), "no arrows" before "out of potions"; Assassin weapon route Main Gauche -> Jur -> Jamadhar -> Katar (Sonic Blow
  needs a katar); Acid Terror / Demonstration need their bottle (progress.go skillItem); a gear-pool turn item is
  dropped while the member lacks its class weapon / arrows; wizard + sage entries in buildgear.json. Back Stab works
  in Renewal (no direction check). Bard songs need an instrument in hand - not done (bow-route bards).
- Priest stats: INT first to 99, then DEX/VIT (`Build.LegacyStats` keeps the old steps valid in wrongbuilds.py).
  Healer rest: sits under `priest_sit_low_sp_pct` (40), rests to 80% (60% when the party moves), never above ~60%.
- Boss hunts: `bosshunt.go` (catalog from boss spawns, strict readiness, hunt phases, retreat), API
  `/api/boss-hunt[/bosses]`, `legionctl boss-list|boss-check|boss-hunt|boss-status|boss-cancel`.
- `legion/data/buildgear.json`: recommended gear and cards per plan id and level band, for the economy agent (it loads
  the file every 5 min). Hunter, bard, dancer and priest are filled in so far.

- Errands (`errand.go`, owner rule 2026-11-14 RULES.md 3 "A member may leave briefly"): a party member (never a bot
  party's leader) that is idle (no fight, nobody in the party losing a fight, no rally, HP >= 60%) leaves for a
  necessary step: says "brb, renting a peco", starts a solo "Peco Peco ride" trip to Prontera (town.go), peco.go rents
  there, then the normal party travel brings it back; "back" once within together_cells of the leader (30 min cap,
  2 h cooldown `buffAt["errand:cd"]`). Published `pubState.Errand`: skipped by needsCollect, partyRester,
  Party.resting, rallyMembers, farmingWell and the human-party "where r u". Bot parties no longer steer the party run
  to Prontera for a member's Peco (only the leader's). Human-led members (Echobunny) use it too. Next candidates for
  errands: job change, falcon, class weapon, quest skill, refine.
- Mage line on a Knife counts as `bowless()` too (no class weapon at all): it may call a party run after 6 min.

- **Run 6 (2026-11-14, build-and-strategy):** variants with AI: `rogue_strip` / `monk_occult` (variantskill.go: Divest,
  Occult Impaction, Mental Strength), `hunter_trap` (trap.go: Claymore / Land Mine, Trap buying + supply trip);
  `coreweapon.go` (a learned core skill's weapon - katar for Sonic Blow - is a savings goal, gearHold);
  `teleslot.go` (Clip [1] is unobtainable here: Belt [1] 20k is the Teleport-card accessory; card bought only with a
  slot); `reset.go` (natural respecs at the Izlude Hypnotist for 1st classes: store everything, stat+skill reset,
  fetch, new plan; resets_pending.json); combo Monk timing (weaponskill.go comboNext waits for 0x01d2);
  `skillaudit.go` (`/api/skillaudit`: skill requests answered / refused / silent; can-act gating of b.skillReady,
  tuning `skill_canact_off`). Packet facts for this server (PACKETVER 20211103): cast start = 0x0b1a (not 0x07fb),
  no-damage skill = 0x09cb (not 0x011a), combo window = 0x01d2; skills sent during canact (every swing: amotion) or
  while casting are dropped WITHOUT a failure packet. Read rathena/src/map/packets_struct.hpp before parsing.

## 6f4. Server cap at transcendent classes (2026-11-14, owner: "cap server at trans class")
- Progression: Novice -> 1st (job 10) -> 2nd (job 40) -> at base 99 / job 50 rebirth -> High Novice (base/job 1, 100
  status points) -> High 1st (job 10) -> transcendent 2nd (job 40; Lord Knight ... Gypsy, same branch as before) ->
  STOP. Trans caps: base 99, job 70 (stock `db/re/job_exp.yml`, kept).
- Server side, for everyone (bots and humans): `rathena/npc-custom/scripts_custom.conf` is mounted over
  `/rathena/npc/scripts_custom.conf` (docker-compose.yml). It is the stock (all-commented) file plus `delnpc:` lines
  that unload the 3rd-job quests (npc/re/jobs/3-1, 3-2), the Expanded Super Novice (supernovice_ex) and the Doram 4th
  job (doram/spirit_handler). It works because npc/re/scripts_main.conf imports scripts_custom.conf LAST;
  `delnpc` in conf/import/map_conf.txt would not (map_conf is read before the NPC lists). Kept: rebirth
  (npc/jobs/valkyrie.txt), trans 2nd-job NPCs (npc/jobs/2-1a, 2-2a), Taekwon/SG/SL, Ninja/Kagerou/Oboro,
  Gunslinger/Rebellion, Super Novice, Doram Summoner. No other loaded script calls `jobchange` (npc/custom/jobmaster.txt
  is not loaded). Only the owner's account (group 99, all_commands) can `@job` into anything; bots are group 0.
  Verified offline: a throwaway map-server (`--run-once`, private network + private MariaDB, never ro-db) parsed every
  script with no errors; script NPCs 19,661 -> 19,262 with the cap.
- Bot side (`legion.txt`): the old "trans 2nd at 99/70 -> 3rd job" block is gone. Rebirth now costs the stock
  1,285,000z (Book of Ymir in the Sage Castle); a 99/50 2nd job without it keeps farming until it has it (the stock
  Valkyrie's "empty inventory" step is skipped as a stand-in for the storage trip). Trans 2nd job is announced.
- Engine (`legion/transplans.go`): `transSkills[plan]` (2nd-job core again, then trans skills with prerequisites) comes
  before the rest of Build.Skills once the class is a trans 2nd job (`isTransSecond`, 4008-4021). Build.Skills is
  unchanged, so no existing character becomes a wrong build. 3rd-job ids stay in the class maps (family, transToSecond)
  only as harmless lookups.
- Takes effect: legion.txt + scripts_custom.conf at the next `docker compose up -d rathena`; engine at the next deploy.
- As of 2026-11-14 10:00 no bot is near: max bot base level 80 (one Knight), none >= 90, none trans or 3rd job.

## 6g. Farming time and town trips (2026-11-14, `town.go` needTrip, `ai.go` explore/deathLoopCheck/partyMapFor)
- Town trips start only for real needs: bag >= `trip_bag_pct` (70%) by weight or slots WITH something to sell, out of
  potions, no usable weapon; at most one non-urgent trip per `trip_min_interval_min` (45). Gear rides along on those
  trips (tripGear: the next upgrade sold in the same town). Every trip start logs
  `heading to <town> (<why>) [weight %, slots n/100, pots n, sp n, zeny n]`.
- Roaming sweeps the map (explore), give-up `roam_give_up_sec` 120. Maps must suit the hunter: fitShare >= 50% of the
  map's monsters under the (weakest member's) target filter in SpotsFor/guideSpotsFor/bestPartySpot/formParty.
  Bot party leaders leave dead maps (partySpotNear + rally); 2 deaths on a map in 10 min -> easier map, a member 10+
  levels under the party leaves; low-level bots flee at 25% HP and carry 5 Fly Wings.
- Guard: tools/sentinel.py checks farming share (>= 85%), <= 1 trip per bot per 45 min, and trips under 70% bag with
  pots > 5. The sentinel process must be restarted to load the check.
- Measured: farming 20% -> 35% (travel 42% -> 25%), trips 0.60 -> 0.36 per bot-hour, 0 unnecessary trips. Still
  short of 90%: low-level travel (43% for lv1-9), warp-gate loops (fix built 2026-11-14 ~05:55, awaiting deploy),
  roaming 15%.
- Routing round 2 (2026-11-14 ~06:10, built, deploy pending; details in legion/data/lessons.md): shop trips stand on
  nav.go `talkCell` (walkable cell of our region nearest the NPC, <= 6 cells) instead of "NPC +1,-1"; every walk to a
  portal goes through `walkToPortal` (sets b.lastWarp first, else safeWalkDest stops the bot at the door);
  `regroupStep` goes round through portals when the leader is in another walk region (prt_fild04 river) and asks for
  help after 2 failures; bot.go keeps the rest of b.route across a map change; stray NPC dialogs are clicked through
  (`answerStrayDialog`: an open dialog blocks walking and items); the seed spawns on walkable cells; walled-in bots
  wing out.

- Shop-door loops + mini-bosses (2026-11-14 ~08:00 EST, built for the 09:05 batch; lessons.md 12:55 UTC):
  warploop.go - in-town shop/refine trips accept only a route ending in the target's room (`routeEndsIn`), the
  warp-loop detector needs the same door twice (`mapVisit.via`) and never bans a town building's door. bossnote.go -
  `bossThreatMob`: Boss-class monsters are a map-wide threat below lv25; champions (Mob.Champion, AegisName C1_-C5_)
  a local one (walk off within 9 cells, no map veto). sitguard.go `fightOn` blocks every sit while a fight is on.

- Roaming round 2 (2026-11-14 ~06:40, built; lessons.md): bot-party members never explore on their own ("with
  <leader>"); thin maps (seen.go `mapReadsThin`, < 2 monsters in view per bot) count as full in crowd.go; a 3-min
  roam share >= 35% on a full/thin map moves the bot/party; explore() walks to shared recent monster sightings
  (seen.go `sightNear`) first. Measured before: roaming 7.2% (members 31% of it, thin maps 17%, blind search 24%).

- Sweep the map (2026-11-14 ~09:05, built; `legion/patrol.go`): leaders and solo bots with nothing in view walk a
  per-map patrol loop (cached waypoints, portal-free, one direction, jump to fresh sightings, rejoin forward).
  A bot party leader paces (never holds): each segment ends <= 9 cells from the slowest member within 20 cells.

- Map crowding cap (2026-11-14 ~02:45, built for the ~03:05 batch; `legion/crowd.go`): every picker asks
  `mapHasRoom(map, n)`: load = bots on the map + heading there (pubState.Goal), cap = non-boss spawns /
  `crowd_spawns_per_bot` (5); 30%+ roamers (`crowd_roam_pct`, 12+ bots) = full. Roaming `roam_crowd_sec` (75 s) on a
  full map relocates the bot / bot party. Before: roaming 16.7-20%, 2,400 kills/min (02:27).

## 6h. Ayla's party of 7 and Ayla's guild Starveil (2026-11-14, `legion/humanguild.go`, `humanguildlead.go`, `guildrecruit.pl`)
- Owner: "add more members to ayla party, a hunter and an alchemy", "add a crusader to ayla party", "Ayla should also
  start a guild and recruit all different jobs", guild name "Starveil", "Make sure she also functions as a guild
  leader, do resource planning and growing members, etc. Recruit the right and balance jobs".
- Friends added (bot accounts, single-character, unguilded, wrongbuilds audit clean): **xXRinfangXx** (lg2337, Hunter
  lv72, plan `hunter`; live since 12:29), **Silent rogue** (lg0917, Alchemist lv77, plan `alchemist`, Aid Potion 5,
  Pharmacy 9, NO Potion Creation Guide yet, 77k zeny; live since 12:29), **vex_x18** (lg1222, aid 2101222, Crusader
  lv76/33, plan `crusader` = VIT shield tank: Auto Guard 10, Holy Cross 10, Faith 10, Guard + Blade - the shield
  tank next to Echobunny's two-hand Knight; needs the next main + core deploy). LEGION_CORE in `.env`, the compose
  defaults, `data/human-parties.json`, wrongbuilds.py PARTY, sentinel.py CORE, progress_report.py all list the six.
  lfg_human.go turns are generic over members. Zen.fox (in-game party leader) invites new friends.
- No Emperium (`guild_emperium_check: no`). Ayla founds it: `ayla.sh guild-create Starveil`. statusout.pl writes
  `guild` + `job_id` to status.json; Legion keeps the name in `data/human-guild.json`.
- Invitations by account id: plugin `openkore-ayla/plugins/cmdfile/guildrecruit.pl`, console `ginvite <aid> [name]`
  (0x0168; rAthena checks no distance; OpenKore's own `guild request` needs the player on screen). **Loading:**
  OpenKore's `plugin load X` matches X as a regex against plugin NAMES in the plugin folders (not a path), so it is
  `plugin load guildrecruit`; `loadPlugins 2` in control/sys.txt means only listed plugins load at start -
  guildrecruit is now in `loadPlugins_list`.
- Balanced recruiting (`GET /api/guild-recruits[?n=3&min_lv=&mark=1]`, `ayla.sh recruit [N]`): her party friends
  first, then the biggest role gap of the 16-member target roster (leader included): tank 3 (Knight/Crusader),
  healer 3 (Priest, Monk), aoe 3 (Wizard/Sage), ranged 3 (Hunter/Bard/Dancer), craft 1 (Blacksmith/Alchemist),
  **trader 1** (owner rule: every guild has a merchant trader - a Merchant-line vendor with Vending and an open
  south-gate shop, `isVendorTrader`; recruited right after the friends, level not required; guildstock.go then picks
  it as the "human-guild" trader), dps 2 (Assassin/Rogue); equal gaps go in that order; inside a role a job the guild lacks first, 2nd/trans before 1st
  jobs, then nearest her level (min level max(40, hers - 30)). Online, unguilded, non-leader bots only (no poaching).
- Answers: her friends accept only her guild and refuse all others; other bots accept with max(0.8, fit); a decline
  isn't suggested again for 6 h, a suggestion (mark=1) for 20 min; a new member says hello in guild chat.
- **Guild leader** (`GET /api/human-guild`, `ayla.sh guild`): members (both instances; role, level, map, party, zeny,
  spare, gear_next, gear per level, brewer/guide, in_guild), roles vs targets, role_gaps, recruits, the guild
  alchemist, the week's goal (every member +3 levels in 7 days, in a party on a fitting map, the alchemist owns the
  guide, next gear pieces bought; boss prep later; state in data/human-guild-week.json), the pool (half the online
  members' spare, 5k max each) and **actions** with exact commands: found, invite, guide (fund the alchemist's
  Potion Creation Guide), fund (the weakest-geared member's shop piece when the pool covers it), party (main-instance
  members without a party grouped within 10 levels on bestPartySpot -> `legionctl party ...`), carry (members 15+
  levels under the median), recruit-line (#global text of the gaps), week.
- `POST /api/human-guild/fund {"to","amount","why"}` (`ayla.sh guild-fund NAME AMOUNT WHY`): Starveil members of both
  instances mail zeny by RODEX (2% fee), each at most 5,000z and half its spare, once an hour; a guide-saving
  alchemist never donates. guildOfAccount (alchemy.go) returns "human-guild" for Starveil members and her friends, so
  the guild alchemist pick, guild fund, guild-mate trade matching and herb parcels work for Starveil too (per
  instance: main-instance members' herb parcels need a main-instance Starveil alchemist - open item).
- Guild storage needs the guild skill Guild Storage Expansion (not at guild level 1); members keep Kafra storage.
- **Starveil's trader** (`humanguildtrader.go`, owner rule "every guild has a merchant"): Starveil filled 16/16 with
  fighters, no vendor. Its trader is a member (never a party friend) with Vending + Pushcart - a vendor-role member
  first, else the highest level: today **Crimsonfang28** (Blacksmith lv70: Vending 3, Pushcart 3, Discount 3,
  Overcharge 10). It runs **shop shifts**: after 3 h of farming, when not in a party and with goods (canVend), it goes
  to Prontera, opens its shop (vend.go) for 40 min, says so in guild chat, then closes and farms again (~80% farming).
  economy.go counts it as a guild keeper, so guildstock.go picks it as the "human-guild" trader (stock, buys for the
  gaps). The pick runs in a background refresh (callers hold their b.mu). The members' account alts ("Kimchi Bazaar",
  "Mintrogue Goods", "Wolfrose") are lv1 Novices - no Vending yet; a vendor alt that joins later takes over.
- Guild fund donors give from surplus only (bug 14:46-14:58: Zen.fox, saving for its gear-turn Staff, and Silent
  rogue, short of the potion guide, were donors): guildFundLocal skips members of a party with an active gear turn,
  alchemists without the guide and guide savers, and gives at most half of (giftable zeny - own next gear piece -
  2,000z), max 5k; traders/vendors first, then the biggest surplus. /api/human-guild's "pool" uses the same rule
  (fundShare).
- Loyalty (friendship agent's loyalty.go): /api/human-guild puts its Starveil items FIRST in actions - "keep"
  (members at risk of leaving, with loyalty.go's suggested `ayla.sh guild-fund` keep gift) and "help" (open help
  requests from Starveil members: gear/pots -> guild-fund, party/rescue -> a guild-chat call). Starveil members may
  leave like anyone; only her 6 party friends are fixed.
- **Guild EXP tax and guild skills** (owner: "every guild should tax to level up", "definitely tax the guild to open
  more slots"; `legion/guildtax.go`, plugin `guildrecruit.pl`). Guild levels come only from members' taxed EXP
  (db/re/exp_guild.yml: 100k guild EXP for level 2, 400k for 3, 900k for 4 ...); each level = 1 guild skill point;
  Guild Extension = +6 members a level (16 + 6 x lv, rAthena int_guild.cpp). Bot guild masters: CZ_REQ_GUILD_MENU
  0x014f (2 positions, 3 skills) every 10 min, CZ_REG_CHANGE_GUILD_POSITIONINFO 0x0161 sets 10% on every position
  (names/permissions kept), CZ_UPGRADE_SKILLLEVEL 0x0112 spends points: Extension 10, then the Guild Storage chain
  (Approval, Guard Research, Battle Orders, Regeneration, Emergency Call, Glory of Wounds, Cold Heart, Leadership,
  Sharp Gaze, Guild Storage 5). Any member bot requests the basic info (0x0b7b) every 10 min per guild:
  `GET /api/guild-levels` (level, exp, next, members online/max, tax_set, rates, skill points/levels, first_seen for
  growth). Starveil's recruiting cap = its real max (`humanGuildMax()` from that, else 16).
  Starveil (Ayla is human): plugin commands `gtax <member%> [master%]` (saved in control/guildtax.txt, re-applied every
  10 min and after restarts), `gskill [auto|off|GD_NAME]` (auto by default: spends points every 10 min, Extension
  first), `ginfo`; ayla.sh `guild-tax`, `guild-skill`, `guild-info`.
- Founded 2026-11-14 12:42 (guild_id 11, Ayla master; Silent rogue, Zendancer84, Echobunny joined). New members learn
  their guild from the 0x0a30 name update (bot.go; rAthena sends no 0x016c on join) - needs the next deploy; until
  then members who joined before it show as unguilded in the engine until they relog.
- ayla.sh: `guild`, `recruits`, `recruit` format through tools/rogui/ayla_guild.py; `global TEXT` / `trade TEXT` post
  on the rAthena channels (OpenKore `pm "#global" ...` = channel whisper, as the bots' channelSay does); `guildchat TEXT`.

## 6i. Friendship system (2026-11-14, `legion/friends.go`; RULES.md 3 "Friendship system")
- Owner: "need a friendship system. bots that spend time together grow in friendship and more likely to continue to
  party together or be in the same guild. Bots can change guild because of their friends. This is a separate system
  we need to track." Its own module and data: `data/friends.json` (main) / `data/friends_core.json` (core), saved
  every 10 min (atomic), rotated to .bak1..3 hourly, a save that would halve the pair count is refused. Each instance
  scores pairs its own bots are in and reads the other's file as peer pairs (max).
- Score per character pair 0-100, gains shrink near the top (pts x (1 - s/110)): partied on the same map +0.30/min,
  both killing in that party +0.10/min, heal from the other +0.30 (1/min), party chat or whisper +0.20 / guild chat
  +0.05 (1 per 2 min), trade window done +1.5 per side, zeny mail +4, same guild + same map not partied +0.05/min.
  Ayla (human) counts as a member of her party when her status shows her on the member's map. Decay: after 6 h
  without contact, 7-day half-life; < 1 point and 3 days silent = dropped. Memory relations written by the minds
  (`legionctl remember --person X --relation friend|close friend|rival|enemy`) add +10/+20/-15/-30 (each side half).
  Tiers: acquaintance 10, friend 35, close friend 65. Seeded once (no file, 5 min after start): party mates 18, Ayla's
  party 30, guild mates 4 (big guilds: 5 random mates each).
- Hooks (one line each): bot.go party chat 0x0109, guild chat 0x017f (new case), whisper 0x09de, guild leave/expel
  0x015a/0x015c (new: `onGuildLeft` clears GuildID/GuildName/guildRoster); heal.go healRecv; economy.go marketDone;
  mail.go endMail; guilds.go guildThink (`friendMoveWants` accepts the move invite; `friendRecruit`: a Legion guild
  leader invites its unguilded friends (score >= 35) before party members); main.go `friendsManager`; api.go
  `registerFriends`.
- Effects: login greeting (a bot online 20 s-5 min greets its best online friend by party chat / guild chat /
  whisper, p 0.55 friend / 0.85 close friend, once per 2 h; the friend answers 85%; no greetings in the first 3 min
  after a restart); party banter between friends (<= 4 lines/min server-wide); diary entries on reaching friend /
  close friend. **Guild moves (main instance only):** a bot (not master, not guild leader, not pinned, not Starveil /
  Ayla's party, not its guild's alchemist or trader) with 2+ close friends in another Legion guild whose master bot
  is online, their scores > 1.5x its own guild's friends + 40, moves with ~12% chance per qualifying day (per-minute
  roll), max 3 moves a day server-wide, once a week per bot: goodbye line in guild chat ("gonna join my buddies in
  X, gl all"), CZ_REQ_LEAVE_GUILD 0x0159, the target master bot sends 0x0168 (up to 3 tries), hello + a friend's
  welcome in the new guild chat. Stages leaving -> invite -> done/failed with reasons, kept in the file (last 100).
- For the goal parties (lfg.go, not edited): `FriendScore(a, b string) float64`, `bestFriendsOnline(b *Bot, n int)
  []*Bot`, `friendPartyBonus(c *Bot, ms []*Bot) int` (0..40, add in lfgScore), `friendsFirst(leader *Bot, cands
  []*Bot)` (stable reorder). All safe with any b.mu held (own mutex, never takes a bot lock).
- `GET /api/friends[?n=30][&bot=Name]`: pairs_total, tiers, thresholds, top pairs (score, raw, tier, partied_min,
  fight_min, heals, chats, trades, guild_min, last contact, guilds), guild_moves, guild_changes_due_to_friends,
  counters, move_policy, one character's friends. Dashboard card "Friendships" (after the Leaderboard).

## 6i2. Guild loyalty and mutual help (2026-11-14, `legion/loyalty.go`; RULES.md 3b "Guild loyalty and mutual help")
- Built on friends.go, saved in the same friends files (`loyalty`, `help`, `loyalty_counters`). Starveil members may now
  move like anyone; only Ayla's six pinned party friends stay fixed.
- Loyalty per character to its current guild, 0-100 (50 when first seen or after a guild change). Gains: zeny mail from
  a guild mate +6, trade with one +1, heal from one +0.15 (1/min), a minute partied with guild mates +0.05 (+0.05 when
  carried, 10+ levels under them), help answered +8, a leader's keep gift +12; gains shrink near 100. Losses: -3/day
  after 24 h without any care, -5 per unanswered request. Hooked through friendNote (deferred, after the friends lock;
  lock order friends -> loyalty, never the other way).
- Guild move rule (friends.go): the friends' pull must beat 1.5x own-guild friends + 40 + 0.8 x loyalty, the chance is
  x 2(1 - loyalty/100), loyalty >= 85 never moves.
- At risk = loyalty < 40 and friends elsewhere summing >= 60. A bot guild leader (main instance) mails 2-20k (max 1/8
  of its purse, keeps 30k; 1 per leader per 2 h, 1 per member per day) with a guild-chat line; Starveil's at-risk
  members carry an `ayla.sh guild-fund "NAME" AMOUNT "keep: next gear"` action in /api/guild-loyalty for the ayla
  mind (NOT yet merged into /api/human-guild actions: that is the guild agent's file).
- Help requests: a member asks in guild chat when it needs gear (<= 30k short of its gear_next), pots (below half its
  potion minimum, < 1.5k zeny), a party (solo 30+ min, main instance) or rescue (3 deaths in 15 min); 10%/min x
  (0.5 + sociability) while the need holds; 1 per member per 3 h, 1 open per guild, 6/h per instance. 20-120 s later a
  guild mate of the same instance answers (asker's friends first, then the richest): RODEX zeny (helpGive, 14:30 fix for a
  poor server - the first rule "keep 25k + 3x" answered 0 of 6 live asks at a ~1.1k median purse: keep 3k, give at most
  half the rest and 30% of the purse a day; full help first, else a partial gift >= 1/3 of the ask and 500z; once per
  30 min), formParty (unpartied, +-15 levels), or rescue (5+ levels higher, solo, same map or <= 3 hops: hunts on the asker's map for 10 min, then its old goal back; else pots money). 10 min
  without help = unanswered (asker sometimes says "nvm"). Thanks line from the asker.
- `GET /api/guild-loyalty`: guilds (members, avg loyalty, low, help asked/answered), least_loyal, at_risk, help_recent,
  counters; `/api/friends` has a `loyalty` summary; the dashboard Friendships card shows help/at-risk/keep counts.

## 6i3. Memory/friendship name filter (2026-11-14, `legion/memory.go` memoryNameOK)
- 357 memories listed "AIBot" (the switched-off seed test bot) as a person; also "Observer" (old watcher), NPC
  speakers from before bot.go split NPC talk ("Resting Adventurer", "Trailmark") and channel-prefixed "[Global] Ayla".
  memoryNameOK blocks those, any "[..."-prefixed name and any non-Legion name containing "bot". Applied in meet(), on
  every memory load (memoryClean drops the people + diary lines naming them, marks the file dirty) and by a startup
  sweep (memoryCleanSweep: each instance loads its own accounts' files that contain a blocked name, so the next flush
  writes them clean); friends.go ignores such pairs and drops them on load. Files on disk are cleaned by the engine
  after the next deploy, not by hand (a hand edit would be overwritten by the running engine's flush).

## 6j. Travel/services QA: GM test character (2026-11-14, owner: "Use a GM account with unlimited money to move around maps and use all service to make sure everything works"; RULES.md 1)
- **Account** `qatest`/`qatest` (account_id 2000002, group 99 = GM; outside the bot range 2100001+, not the owner's
  `player`), created like the seed does (rows in `login` + `char`). Characters: **Trailmark** (char_id 200100, slot 0,
  the tester; ~10M zeny, `@hide` on - `@hide` TOGGLES, check "Invisible: On") and **Trailpost** (200101, slot 1, Novice,
  only a RODEX receiver). Never chats, never parties, keeps out of the economy; levels/jobs are changed freely with
  `@blvl/@jlvl/@jobchange/@allskill` (keep base level modest so it doesn't sit on the leaderboard).
- **Container** `ro-openkore-qa` = compose service `openkore-qa`, profile **qa** (a plain `up` never starts it), same
  image as Ayla, `network_mode: service:rathena`. Start/stop: `docker compose --profile qa up -d --no-deps openkore-qa` /
  `docker stop ro-openkore-qa`. Its entrypoint copies `openkore/fields-re/*.fld2.gz` over the image's fields and
  `openkore-qa/tables/*.txt` (portals.txt / npcs.txt copies) over the tables, then runs the normal entrypoint; a changed
  portals.txt costs a ~4 min LOS compile at start. Control dir `openkore-qa/control` (Ayla's config with XKore 0,
  username qatest, char 0, no attack/loot/auto-buy/party, portalRecord 0, portalUpdatePosition 0), plugins
  `openkore-qa/plugins/qa` (cmdfile + pkt2021fix).
- **Driving it**: `bash openkore-qa/qa.sh [-w SEC] "cmd" ...` appends OpenKore commands to control/commands.txt and
  prints the new log lines (`c @warp map x y`, `move map`, `talknpc x y <seq>`, `storage ...`, `rodex ...`). Send one
  command at a time when it matters: cmdfile can drop a line appended while it truncates the file.
  `openkore-qa/kafratest.sh` warps next to every teleporting Kafra and takes its first/last destination (39/39 ok).
- **Root cause of "Ayla can't reach the Orc Dungeon" (fixed)**: OpenKore's tables/portals.txt carried the official
  kRO Kafra destination lists; our Kafras use F_KafSet (rathena/npc-custom/functions_kafras.txt). Prontera Kafra here =
  Izlude, Geffen, Payon, Morocc, **Orc Dungeon**, Alberta; OpenKore had r4 alberta / r5 aldebaran / r6 comodo / r7
  umbala and no Orc Dungeon line, the Izlude Kafra "r0 prontera" (real: Geffen), Juno Kafra einbroch/lighthalzen/...
  (real: only Al De Baran), a Prontera Kafra at 35,208 (real 29,207), etc. Ayla's rally to Aldebaran took the Prontera
  Kafra "r5" and landed in Alberta, then her route to Geffen went by ship into (then broken) Izlude. Fix:
  `openkore/fix_kafra_portals.py tables/portals.txt [functions_kafras.txt]` regenerates every Kafra teleport line from
  the server data (idempotent, owns ALL Kafra `c r2 c rN` lines; other patch scripts must not write Kafra lines and run
  before it). Applied live to Ayla 2026-11-14 19:57Z (`reload portals` + `portals recompile`), and in openkore/Dockerfile.
  Verified: Payon -> orcsdun01 52 s, Prontera -> orcsdun01 16 s (Prontera Kafra -> Orc Dungeon), Prontera -> Al De
  Baran, Al De Baran -> Juno, Juno -> Morocc, Morocc -> pyramid / sphinx, Izlude -> prt_fild08.
- `portalUpdatePosition 1` (not portalRecord) made OpenKore rewrite portals.txt whenever a warp landed a few cells off
  ("[processPortalRecording] Updated portal coordinates") and forced a LOS recompile; set to 0 on Ayla and QA.
- **OpenKore packet layouts that are wrong for client 20210107** (found with the QA char; plugin
  `openkore-qa/plugins/qa/pkt2021fix.pl` fixes them at runtime): RefineUI (0AA2 materials / 0AA3 request use 4-byte
  item ids: without it every refine was refused locally), NPC markets like Dietrich's ores (09D5 / 09D6, 4-byte ids: the
  list showed garbage prices), and **RODEX read 0B63** (OpenKore only knows 09EB: "Unknown switch: 0B63", so a read mail
  is never "current" - getzeny/getitems refused and Ayla's aylagear `rodex_mail` hook never fired: "mailbox: no answer
  at step 3"). With the plugin: refine +1 Katana with Phracon, Phracon bought from Dietrich, zeny and items taken from
  mail. Ayla needs the plugin in her loadPlugins_list to read mails (main decides).
- Services table (QA 2026-11-14): see the travel-QA report in the session; every Kafra teleport, Kafra save/storage,
  Butterfly/Fly Wing, NPC buy/sell, Peco breeder, Alchemist Union guide + bowls, Izlude Hypnotist skill reset, RODEX
  send/receive, guild create work; OpenKore `guild break` gets no answer (use `@breakguild`); guild invite not tested
  (needs a second online character).
- **Field files vs the server's map cache**: `openkore/gen_fields_from_mapcache.py` (run in an OpenKore container with
  the server's db/map_cache.dat + db/re/map_cache.dat) found 97 OpenKore maps whose walkable cells differ from the
  server (prontera 1,286 cells, yuno_fild02 669, aldebaran 137, comodo 112, yuno 53, moc_fild08-22, anthell01/02,
  izlude_in, geffen 8, ...; the RE cache overrides alberta/izlude/morocc/prontera/prt_church/prt_fild05/prt_fild08/
  prt_in). Server-exact files are in `openkore/fields-srv/` (fields-re = the plugin engineer's 13 maps, same source).
  Not yet applied to Ayla or the QA overlay: 4 Prontera walking tests with the old prontera field had no "Stuck", so
  apply them when a stuck/route bug points there (copy into /openkore/fields, delete that map's .dist/.weight, reload).
- Observation for the economy: rAthena's RE `Tool Dealer#Extended_*` shops (npc/re/merchants/shops.txt, e.g. morocc
  146,103, prt_in 126,76, payon_in01 5,49, yuno 218,97) sell White Potion 1,200z, Yggdrasil Leaf 12,000z, Blue Gemstone
  450z, Awakening/Berserk Potions next to the classic Tool Dealer on the same cell.

## 7. Open issues / next steps
0. Player economy: BUILT in `legion/economy.go` (fair price standard priceOf, wants/offers posted on #trade,
   econManager matching every 15 s, real trades via the trade window, /api/economy) - needs live verification after
   its first deploy: look for "sold ... / bought ..." market events and trades_total > 0.

1. **Followers losing the leader** (`legion/data/lessons.md`, bugs.md): human-party members learn Ayla's map only
   through `noteHuman` sightings, so they lag when she changes maps; Echobunny got stuck on moc_fild02 for 35+ min.
   Suggested: path to the leader's last known map, time out after ~3 min, fall back to the checkpoint town.
2. **mjolnir_10 death spike** (bugs.md 18:20): ~250 deaths in 15 min; danger logic should move bots off it.
3. **Ayla mind didn't read its inbox** in an earlier run (owner messages stayed unread) - check the `ayla` mind
   actually calls `legionctl orders ayla` each cycle.
4. Fold `legion/data/lessons.md` into the engine regularly (owner's explicit wish).
5. Ideas not applied yet: `research/proposed_builds.md` section D (Earth Spike vs wind, Turn Undead, potion tiers).
6. rAthena restarts disrupt the core party for ~1-2 min; avoid unnecessary ones. Main Legion deploys no longer touch
   it (it runs in `legion-core`); prefer `data/tuning.json` over a deploy for number changes.
8. **Potions / weight / weapons** (2026-11-13, lessons.md): potion targets per class, 45% weight cap, sell surplus at 50%
   (no regen above), SP potions for SP users (tuning keys weight_regen_pct, pot_weight_cap_pct, pot_hp_acolyte,
   pot_sp_target). Open: no NPC sells cheap SP potions (only Blue Syrup at 7,000z), so most SP users carry none; ~80 of
   94 main-Legion acolytes had NO weapon (unusable starter Knife; their "buying Mace" trips timed out). The fixes (weapon
   first, outdoor shop preferred, in-town shop room routing) are in the image but main `legion` still needs a batch deploy.
9. **Gear progression** (2026-11-14, lessons.md, `legion/gearpool.go`): build-aware item scores (casters value MATK,
   archers bows), `invReady()` gate (no trips on a half-loaded login), "no weapon" is a necessary trip at any level,
   party gear turns + zeny gifts between party mates (zeny-only market deal) + planned far gear runs piggybacked on the
   next real-need party town run; `gear_next` on /api/bots/<name> and the party dashboard ("next gear" column, gear
   turn line). Tuning: gear_trip_min_lv 15, gear_trip_cooldown_min 30, gear_pool_min_gift 200, gear_turn_max_min 40.
   Live in legion-core; main `legion` needs the next batch deploy. Unverified: a gift completing through the trade
   window (the market trade code has 0 completed trades so far: check `/api/economy` and "gave/got ...z" events).
   Not built: account pooling (a farmer character funding its alts) - needs Kafra storage support.
7. Owner's client graphics: HD sprites via `sprites_hd2.grf` (DATA.INI order 0=sprites_hd2.grf, 1=rdata_ok.grf,
   2=data.grf); dgVoodoo was removed (`client\dgvoodoo-off`); Magpie (`G:\RO\tools\magpie`) downloaded but untested.

---------------------------------------------------------------------------------------------------------------------
## 8. Quick checks

```
# is everyone up?
wsl -d Ubuntu-24.04 -- docker ps --format '{{.Names}} {{.Status}}'
curl -s http://127.0.0.1:8090/api/overview        # world health
curl -s http://127.0.0.1:8090/api/parties/detail  # parties (Ayla's first)
wsl -d Ubuntu-24.04 -- bash /mnt/g/RO/legion/tools/rogui/ayla.sh log 30     # what Ayla is doing
# watch window frozen/black: restart ai-display.sh, then ai-client.sh (both long-running)
# Ayla not attacking: ayla.sh cmd "conf attackAuto 2"; wrong map: ayla.sh map pay_fild01
```

### Background loops the main thread keeps running (2026-11-14)
- `wsl -d Ubuntu-24.04 -- python3 /mnt/g/RO/legion/tools/sentinel.py` (exits on a problem; restart after acting on it).
- `python tools/unwall.py --loop` (from G:\RO\legion): moves OFFLINE lg* bot chars off blocked cells every 30 s. Walled-in
  bots log out for 3 min so it can fix their row; without this loop they come back on the same cell.
- Deploys: only the main thread runs dep3.sh/depcore.sh (scratchpad), batched; subagents report "built".
  The scripts now live in `tools/deploy/` (section 0.5); a per-batch deploy agent runs them (section 0.4 item 8).
- Deploy cadence (2026-11-14 02:50): every main/core restart makes all bots relog and re-evaluate needs at once, giving a
  ~15 min wave of town/Kafra trips and split parties (QA measured it after each deploy). Now that the big fixes are out,
  deploy at most once an hour, and only when a batch has real fixes; critical crash fixes are the exception.
