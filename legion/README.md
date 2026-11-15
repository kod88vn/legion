# Legion: high-volume bot engine

Legion runs **1,000 bots in one process** against the local rAthena server (PACKETVER 20210107 RE), alongside a
human player. Measured on this PC (i9-12900K, 64 GB) with 1,000 bots online:

| | CPU | RAM |
|---|---|---|
| Legion (all 1,000 bots) | ~0.3-0.7 of one core | ~650 MB |
| rAthena (login/char/map) | ~0.25 of one core | ~800 MB |
| whole machine | ~13% | |

(OpenKore needs ~200 MB and noticeable CPU per bot.)

## Layers
1. **Legion (Go, `legion/`)**: the 99% done by algorithm. Speaks the RO protocol directly (login → char → map),
   keeps a world model per bot from server packets, and runs a behaviour loop per bot:
   - movement: map-cache A*, warp graph over connected map regions, danger-weighted Dijkstra routing (deaths feed a
     shared map-danger table, so the population learns to avoid death traps);
   - combat: target selection with kill-claims (no monster stealing), class skills with element choice, looting,
     sitting to rest (confirmed by the server), potions, heals, respawn, fighting back while travelling;
   - progression: stat and skill points (`progress.go`), Renewal leveling-guide spots per level/class
     (`strategy.go`), job changes up to 3rd job via `../rathena/npc-custom/legion.txt`;
   - town trips (`town.go`): Butterfly Wing home, sell loot / buy potions at the Tool Dealer when heavy or out of pots;
   - parties (`party.go`): bots form/accept real rAthena parties; support classes heal under 70% HP and buff
     Blessing / Increase AGI; members assist the leader's target and follow;
   - vending (`vend.go`): merchant characters walk to a pitch in the central Prontera market and open a shop
     from their cart;
   - human rhythm (`ai.go`): daily play windows per timezone (longer at weekends), session routines, town hangouts,
     AFK breaks, "calling it a day", a watchdog for stuck bots.
2. **Personas (`data/personas.json`)**: each profile is a "player": archetype, traits (aggression, risk,
   chattiness, patience, greed, sociability, ...), country/timezone, schedule, typing style, bio, guild, and up to
   3 characters (main, vendor merchant, support acolyte/priest; `pickChar` chooses who logs in). Traits drive the
   algorithm and the LLM agents' voice.
3. **Memory (`data/memory/<account>.json`)**: facts, goals, people (relations + notes), a diary written
   automatically (level-ups, job changes, deaths, parties, sales) and maps visited. Flushed every 60 s; agents read
   and write it through the API.
4. **LLM agents**: the minds (chat, ambient life, guild leaders, market) run as Claude Code or Copilot sub-agents
   that act only through `../tools/legionctl.py`. See `../AGENTS.md` and the `legion-*` skills. (The old
   `../mastermind/` container is kept behind the compose profile `fallback`.)

## Run
```
cd G:\RO\server
docker compose up -d                      # db, rathena, agency, legion
```
`.env`: `LEGION_BOTS` (profiles, now 4000) and `LEGION_MAX_ONLINE` (concurrent cap, 1000). With `-schedule`,
profiles log in and out on their own timetables, so "online" ramps up over a few minutes and follows the time of day.

### Two instances: main and core
The bots of the owner's core test party (Ayla's friends) run in a second Legion instance, `legion-core`, so a deploy
of the main one doesn't log them out (`instances.go`).
- `legion run ... -exclude "Zendancer84,Echobunny,Zen.fox"`: the main instance (service `legion`, API :8090) skips
  those profiles, doesn't restore or write `human-parties.json`, and owns the minds bus, guild leaders and
  `guild_roster.json`. It forwards `/api/human-party`, `/api/bots/{core member}` and `/api/memory/{core member}` to
  the core instance and merges the core instance's human-led parties into `/api/parties/detail` (`"instance":"core"`;
  the last good answer is kept for 2 min while the core instance restarts). `-core-api URL` overrides the core
  address (default `http://127.0.0.1:8091` when `-exclude` is set).
- `legion run -only "Zendancer84,Echobunny,Zen.fox" -api 127.0.0.1:8091 -pprof 127.0.0.1:6061`: the core instance
  (service `legion-core`) runs only the profiles with those characters (found anywhere in personas.json, `-n` is
  ignored), restores the human party from `human-parties.json` and writes it, has no minds bus, picks no guild
  leaders, writes no guild roster, and its chatter rates have no minimums (a handful of bots).
- Both names lists come from `LEGION_CORE` in `.env` (default the trio above); they must match, or a bot is run by
  both instances (double logins) or by neither. Rotating a member in/out of Ayla's party therefore means: change
  `LEGION_CORE`, update `data/human-parties.json` (or POST `/api/human-party` after the restart), redeploy both.
- Deploy: `docker compose build legion && docker compose up -d legion` (main; the core party stays online) and
  `docker compose up -d legion-core` (core; only when party-relevant code changed). Both use the `ro-legion` image.
- `-pprof ADDR` (default `127.0.0.1:6060`, empty = off): both instances share one network namespace, so the ports differ.

### Live tuning (`data/tuning.json`, `tuning.go`)
Behaviour numbers that used to be constants are read from `/data/tuning.json` and re-read every 30 s: edit the file
and the change applies within half a minute, no restart. Each change is logged (`tuning: party_leash 8 -> 10`); a file
that doesn't parse is logged and ignored (old values stay); missing fields keep their defaults; the file is written
with the defaults if it doesn't exist. Both instances read it. Fields (default):

| field | default | meaning |
|---|---|---|
| `party_leash` | 8 | members only start fights within this many cells of the leader, catch up beyond it |
| `regroup_extra` | 4 | a member farther than `party_leash`+this walks back to the leader before anything else |
| `tank_pull_radius` | 12 | a party tank pulls monsters at most this far from the leader |
| `tank_max_lv_over` | 5 | ...and at most this many levels above its base level |
| `priest_buff_min_sp_pct` | 60 | party priest buffs (Blessing, Increase AGI) only above this SP % |
| `priest_sit_below_sp_pct` | 95 | with nobody hurt, a party priest sits while SP % is below this |
| `priest_buff_renew_min_sec` / `_max_sec` | 220 / 260 | a buff is renewed after a random time in this range |
| `beginner_lv` | 20 | below this base level a bot stays near its home town; Kafra keeps more money |
| `stay_crowd` | 70 | a bot stays on a fitting map while it holds at most this many bots |
| `target_lv_over`, `target_lv_aggression`, `target_lv_min` | 2, 6, 8 | max monster level = max(base + over + aggression*this, min) |
| `roam_give_up_sec` | 45 | a solo bot that finds nothing to fight this long gives the map up |
| `ghost_target_sec` | 10 | a target neither reached nor hit this long is dropped |
| `leader_collect_sec` | 90 | a bot party leader commits to collecting one straggler for at most this long |
| `trip_urgent_weight_pct` | 90 | bag weight % at which a party member may go to town alone |
| `kafra_min_saving` | 3 | Kafra teleport only if it saves at least this many map changes |
| `kafra_keep_zeny_beginner` / `kafra_keep_zeny` | 1000 / 200 | zeny kept after paying a teleport (beginners / others) |
| `min_gear_level` | 10 | no gear shopping trips below this base level |
| `max_gear_hops` | 5 | gear only from a shop at most this many map changes away |
| `gear_reserve_base`, `gear_reserve_per_lv`, `gear_reserve_zeny_div` | 500, 20, 4 | zeny kept for potions when buying gear = min(base + lv*per_lv, zeny/div) |
| `chat_global_div/min/max` | 300 / 1 / 3 | #global+#trade lines per minute = online/div, clamped |
| `chat_local_div/min/max` | 35 / 6 / 28 | local chat lines per minute |
| `chat_party_div/min/max` | 50 / 4 / 20 | party chat lines per minute |
| `chat_guild_div/min/max` | 90 / 3 / 10 | guild chat lines per minute |
| `chat_global_trade`, `chat_global_lfp` | 0.45, 0.80 | #global pick: below trade = #trade line, below lfp = looking for party |
| `chat_local_reply`, `chat_party_reply` | 0.6, 0.5 | chance someone answers a local / party line |
| `chat_party_human` | 0.3 | chance a party line in a human-led party addresses the human |
| `chat_guild_leader` | 0.5 | chance a guild leader's line is a leader line |
| `chat_react_base`, `chat_react_chattiness` | 0.25, 0.5 | chance to announce a milestone = base + chattiness*this |
| `chat_react_jobchange`, `chat_react_death` | 0.25, 0.3 | extra chance for a job change / death announcement |

Re-seed (wipes bot characters, progress and memory):
```
docker compose stop legion legion-core
docker compose run --rm legion seed -personas 4000 -guilds 10        # personas.json + seed.sql
docker exec -i ro-db mariadb -uragnarok -pragnarok ragnarok < legion/data/seed.sql
docker compose up -d legion-core legion
```
Accounts `lg0001..` (password `legion`, account ids 2100001+); character ids: main 170001+, vendor 180000+n,
support 190000+n.

## API (http://127.0.0.1:8090)
- `GET /api/stats`, `/api/bots[?guild=&map=&state=]`, `/api/bots/{name}`, `/api/guilds`, `/api/events?since=N`,
  `/api/inbox`, `/api/humans`, `/api/market`, `/api/party`, `/api/parties`, `/api/spots?level=N`, `/api/memory`
- `POST /api/bots/{name}/goal {"mode":"hunt|idle|follow","map":"...","who":"..."}`, `.../say`, `.../whisper`,
  `.../party-chat`, `.../shop`, guild goals, party forming, memory updates
- `GET /api/economy` (economy.go): #trade wants and offers, deals in progress, recent bot-to-bot trades, per-item
  average prices against the price standard `priceOf`.
- `../tools/legionctl.py` wraps all of this (`python tools/legionctl.py help`).
- **Dashboard**: `http://127.0.0.1:8090/dashboard`: one-page bird's-eye monitor (auto-refresh 10 s): KPIs, trends,
  level/class/state charts, build-plan progress, busiest maps, activity rates, deadliest maps, economy, humans
  (`dashboard.go` + embedded `dashboard.html`). Data: `GET /api/overview` (one JSON snapshot, ~1-5 ms) and
  `GET /api/overview/history` (one point per minute, last 6 h, in memory; resets when Legion restarts).
  The page also has a **Minds** section (each LLM mind's thoughts, newest first, and a box to send it orders;
  refresh 5 s) and a **Guilds** table (from `GET /api/guilds/detail`; leader, strategy, online/members, top members,
  parties, busiest maps, kills/h, "message mind").
- **Minds bus** (`minds.go`): `POST /api/minds/{mind}/thought {"text","kind","status"}`, `GET /api/minds`,
  `GET /api/minds/{mind|all}/thoughts?since=N&limit=M`, `POST /api/minds/{mind}/message {"text"}` (owner -> mind),
  `GET /api/minds/{mind}/inbox?ack=1`. Rings of 500 thoughts / 500 messages, saved to `/data/minds.json` every 30 s
  and on shutdown. legionctl: `think`, `orders`, `minds`.
- profiling: `127.0.0.1:6060/debug/pprof` (inside the rathena network namespace)

## Gotchas
- rAthena drops skills seeded straight into the DB when a character loads, so `legion.txt` grants each class's
  core skills at login with flag 3 (permanent grant) and gives merchants a cart. Use numeric skill ids there:
  skill-name constants passed through `callsub` arrive as 0.

## Not done yet
- Bots buying from other players' shops, storage, equipment upgrades, NPC-dialog features.
- Real guilds in rAthena (guild names exist in personas only).
- The LLM agents are set up but haven't been run for a long session yet.
