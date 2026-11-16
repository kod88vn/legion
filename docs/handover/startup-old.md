> **Reference only: OpenKore and 15-mind era; current state is in HANDOVER.md and STATUS.md.**

<!-- Moved unchanged from HANDOVER.md lines 9-162 (as of commit 63ca923). Index: HANDOVER.md. -->

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
