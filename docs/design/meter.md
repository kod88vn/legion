# Meter (work package M)

Branch `feat/meter`, 2026-11-14. Always on, read-only for behaviour: nothing in it changes what a bot does.
It measures the two key metrics per bot (kills per minute, zeny per hour), deaths and a time budget, compares bots
inside cohorts, and writes an hourly history.

## Files
| file | what |
|---|---|
| `legion/meter.go` | per-bot counters (`b.meter`, a 60-minute ring + session total) and the hooks that fill them |
| `legion/meterclass.go` | engine state -> time bucket (pure) |
| `legion/metercohort.go` | rates, party credit, cohorts, percentiles, par and gap (pure) |
| `legion/meterapi.go` | background aggregator, price table, Ayla tracker, core merge, history file, HTTP API, own tuning keys |
| `legion/meter_test.go` | unit tests (buckets, fair kill, window roll-over, percentiles/par, cohort keys, income and party credit, tick accounting, Ayla, the HTTP endpoints) |
| `tools/legionctl.py` | `meter`, `meter bots`, `meter bot <name>`, `meter neo` |

Hook-ins to shared files (one line each):
- `bot.go`: field `meter meterCounters` in `Bot`; `b.meterDeath(now)` after `b.Deaths++` (0x0080 type 1, self);
  `b.meterKill(b.ents[id], now)` after `b.Kills++` (0x0080 type 1, our target); `b.meterZeny(...)` before the purse
  update (0x00b1 type 20); `b.meterLoot(...)` after `b.Loots++` (0x0b41 ok); `b.meterRemoved(op, pk)` at the top of
  0x00af/0x07fa; `b.meterUsed(...)` before the count update of a successful 0x01c8; `b.meterLogin(now)` at session
  start (after `kafraLastFromDiary`).
- `ai.go`: `b.meterTick(now)` right after `now := time.Now()` in `think()`.
- `kafra.go`: `b.meterTravel(k.Dest.Price)` at the end of `noteKafraHop`.
- `api.go`: `registerMeter(mux, bots)` after `registerPartyDash`.
- `.gitignore`: `legion/data/meter.jsonl` (runtime file).

## Lock discipline
- Every hook runs where `b.mu` is already held (`handle()`, `think()`, `noteKafraHop()`); it touches only that bot's
  `b.meter`. No new mutex, no global lock, no file I/O, no channel in a bot's tick.
- Shared state read by hooks is lock-free: the meter tuning and the item price table are `atomic.Pointer`s; game data
  (`mobs`, `items`) is immutable after load; an unpriced item id is registered in a `sync.Map` (`meterSeen`) for the
  aggregator to price.
- One goroutine (`meterLoop`, every `meter_period_sec`, default 15 s) does everything else: it locks one bot at a time
  (defer-unlocked) just long enough to sum its ring into the three windows (a 60-slot loop) and copy its identity, then
  fetches the core instance's rows over HTTP, computes parties, cohorts and percentiles with no lock held, and
  publishes an immutable view with one atomic store. `priceOf` (which takes `econ.mu`) is only called from this
  goroutine, never while a bot lock is held.
- Handlers only read the published view; they never lock a bot. The history file is appended by the meter goroutine
  only, with no lock held. A panic in a round is recovered and logged.

## Definitions

### Fair kill
- A **kill** is our own killing blow: the engine's existing rule, monster vanish with type 1 (died) while it is the
  bot's current target (`id == b.target`). Same event as `b.Kills++`.
- It is **fair** when the monster's level (mob_db) >= bot base level - `meter_fair_lv_gap` (default 15). Monsters never
  seen (no entity) count as kills but not fair. **kpm** always means fair kills per minute; `kpm_all` (any level) is
  reported per bot for comparison.
- Approximation: two bots targeting the same monster both count it if it dies while both target it (rare: the claim
  system keeps bots off each other's monsters). A monster killed by someone else while it was our target counts as
  ours (the engine cannot see who landed the blow).

### Party attribution
- Each bot counts its own fair kills and its own income.
- In cohorts, a party member's **kpm** and **zph** are party-credited: the party's total fair kills (income) divided
  by the members' total online minutes in the window, over the members this instance (plus the merged core rows)
  meters. Every member of one party therefore gets the same credited number; healers are not penalised for not
  landing blows, and the looter is not rewarded for picking up the party's drops. `kpm_own` / `zph_own` keep the
  individual numbers.
- Approximation: the party is the bot's current `PartyID`; a member who joined mid-window brings its earlier (solo)
  minutes into the party total. Vendors are never party-credited.

### Income (zeny per hour)
`income = loot value - consumables - travel`, `zph = income / online minutes x 60`.
- **Loot value**: every successful pickup (0x0b41) x the item's market value. Market value = `max(priceOf(id),
  sellPrice(id))`: the economy's standard price (NPC price for NPC-sold items, the dynamic table when
  `dynamic_prices` is on, the fixed formula otherwise), blended with recent player trades and the supply/demand
  factor, never below what an NPC pays. The table is rebuilt by the meter goroutine (new items each round, everything
  every 5 min); until an item is priced, its NPC sell price is used.
- **Consumables**: items used up, valued the same way: successful item use (0x01c8, the drop in count: potions,
  food, etc.), ammo burned (0x07fa reason 0/1 on an Ammo item) and skill materials (0x07fa reason 1: gemstones,
  bottles). Sales (reason 6), storage (4), cart (5), trades and refines are not costs.
- **Travel**: Kafra teleport fares (`kafraDest.Price` when the map change confirms the hop) plus Fly / Butterfly
  Wings used (601 / 602, at market value).
- **Zeny gained is NOT added.** In this world zeny comes from selling loot, so adding the purse change to the loot
  value would count the same loot twice (once picked up, once sold). The purse is still tracked per window as
  `zeny_in` / `zeny_out` (every 0x00b1 change, baseline = the character list's zeny at login) for cross-checks.
- Not counted as costs (investments or services, not farming costs): gear and card purchases, refines, Peco / falcon /
  cart rental, storage fees, gifts and trades between players, guild funds.
- Approximation: market value is the engine's own price model, not a realised sale; plain junk gear (< 100z) is valued
  at its price formula even though it is sold to NPCs. Loot looted and dropped again would count twice (bots don't).

### Deaths
Own death (0x0080 type 1 for our own id), same event as `b.Deaths++`. Reported per hour of online time.

### Time budget
Every `think()` (about every 0.3-0.6 s) adds the time since the previous tick to the bucket of `b.State` - the state
the previous tick set, i.e. what the bot did during that interval. Gaps over 5 s (offline, stalls) are not counted, so
**online minutes** = the sum of all buckets. Classification (`meterclass.go`, first match wins):

| bucket | rule |
|---|---|
| dead | `b.Dead` or state "dead" |
| stuck | the stuck watchdog's clock (`stuckSince`) has run 30 s+, or state "walled in", "no route to", "... (can't get there)" |
| idle | "afk", "hanging out", "idle in", "offline", "login failed" |
| town | town trip, shopping, vending/vendor, trading (incl. "waiting for X to trade", "going to X to buy from", "X's shop"), storing, saving, renting, refining, brewing, compounding, identifying, cart loading, Hypnotist, Dietrich, Alchemist Union, Peco breeder |
| fighting | "standing up to heal/cure", fighting, engaging, tanking, kiting, pulling, provoking, taking, baiting, defending, assisting, casting, area skill, Heal on, playing (songs), Grand Cross, Fury, Asura, Mental Strength, luring, holding, bringing, letting X pull, protecting, coming to help/protect, looting, healing, Kyrie, Assumptio, Aid Potion, buffing, curing (others) |
| resting | resting, sitting (incl. a healer's idle sit), guarding (resting members), low HP, poisoned (n%), curing myself, drinking, standing up, rest over |
| waiting | waiting (regroup, escort, leader, Kafra), rally, collecting, gathering, regrouping, catching up, looking for, running to, leading the way, going back to, "... to cross ..." |
| travelling | travel ->, going to the Kafra, teleported away, fled to town, running away, keeping away, poisoned with no cure |
| searching | roaming, teleporting to find monsters |
| (context) | "with X" / "following X": town -> town, on the hunting map -> searching, else travelling. Anything else (skill names, new states): has a target -> fighting, in town -> town, on the hunting map -> searching, else travelling |

"On the hunting map" = not a town and the map is the bot's goal map (or it has none). The context (map, target) is
read at the end of the interval. **Guild duty**: seconds while `meterDutyHook(b)` returns true (`duty_min`,
`duty_pct`). The hook is a package variable for the guild-direction package (feat/guild) to set in an `init()`; it is
called with `b.mu` held and must not take `b.mu` or any lock ever held while taking a bot lock. Until it is set, duty
is 0.

### Windows
- **10m** = the current minute plus the 9 before it; **1h** = the current minute plus the 59 before it (per-bot ring of
  60 one-minute slots, a slot is cleared when it is reused); **session** = since this engine process started metering
  the bot (counters reset when the account logs in with another character).
- Rates are per **online minute measured in the window**, so a partial current minute or a bot that logged in
  halfway does not bias them.
- A bot **counts** in its cohort for a window when it was online at least `meter_min_online_pct` (25%) of it: 2.5 min
  for 10m, 15 min for 1h, 25% of the session age capped at 15 min for the session. Uncounted bots are still listed
  (`counted: false`) and in `seen`.

### Cohorts, par, gap
- Key `line|band|mode`: line = first-job family (novice, swordman, mage, archer, acolyte, merchant, thief; a Knight,
  Crusader and Lord Knight are all `swordman`), band = base level 1-20, 21-40, 41-60, 61-80, 81-99, mode = `party`
  (in a party), `solo`, or `vendor` (character role vendor: vending is their job, kept out of the fighters' cohorts).
- Per cohort and window: n (counted), seen, mean / median / p75 of kpm and zph (linear interpolation between
  ranks), deaths per hour (cohort deaths / cohort online hours), time shares (cohort seconds per bucket / total).
- **Par = p75.** Gap = max(0, par - mean). **Prize = gap x n** (what the cohort gains if its average reaches par).
  Cohorts are sorted by zeny prize, then kill prize. Per bot: `gap_kpm` / `gap_zph` = par - its credited value
  (negative = above par).
- Server totals: counted bots, fair kills per bot-minute, income per bot-hour, deaths per bot-hour, medians of the
  individual numbers, time shares.

### Neo view
- **Party**: every row from the core instance (legion-core runs exactly Ayla's friends, instances.go) plus any bot in a
  party with one of them. **Starveil**: every bot whose guild is Ayla's (status.json `guild`, default "Starveil").
  Each member against its own cohort's par; summaries: counted, how many at/above par, mean ratio value/par.
- **Ayla** is played by OpenKore, not by the engine. The meter samples `/ayla/status.json` (or
  `LEGION_HUMAN_STATUS`) every round: purse, level, map, HP, sitting. It reports her online minutes, **purse change per
  hour** (`zph_purse`, includes her spending; not the same definition as the bots' income) and a coarse time split
  (hunting / town / resting / dead), with her cohort's par for comparison (`thief|81-99|party` at lv81). Her kills and
  loot value are not observable from that file.

## Endpoints
All read the published view (refreshed every 15 s; 503 until the first round). `window` = `10m`, `1h` (default) or
`session`.

`GET /api/meter[?window=&sort=kpm]`
```json
{"t": "...", "window": "1h", "rows": 812, "core_rows": 6, "fair_lv_gap": 15,
 "buckets": ["fighting","searching","travelling","resting","town","dead","waiting","stuck","idle"],
 "server": {"bots": 640, "seen": 790, "online": 700, "bot_min": 31000, "kills": 52000, "fair": 47000, "deaths": 90,
            "loot": 9100000, "cons": 800000, "travel": 120000, "income": 8180000, "kpm": 1.516, "zph": 15832,
            "deaths_h": 0.17, "kpm_median": 1.4, "zph_median": 11000, "shares": {"fighting": 0.52, "searching": 0.2}},
 "cohorts": [{"key": "swordman|41-60|solo", "line": "swordman", "band": "41-60", "mode": "solo", "n": 40, "seen": 46,
              "kpm": {"mean": 1.2, "median": 1.1, "p75": 1.6, "par": 1.6, "gap": 0.4, "prize": 16},
              "zph": {"mean": 9000, "median": 8000, "p75": 13000, "par": 13000, "gap": 4000, "prize": 160000},
              "deaths_h": 0.2, "shares": {"fighting": 0.5}, "duty_pct": 0}]}
```

`GET /api/meter/bots?cohort=<key prefix>&sort=zph|kpm|gap_zph|gap_kpm|deaths|min|name&order=asc&limit=50&window=&shares=1&all=1`
```json
{"t": "...", "window": "1h", "total": 40, "bots": [{"name": "X", "class": "Knight", "lv": 55,
  "cohort": "swordman|41-60|solo", "instance": "main", "online": true, "map": "gef_fild10", "bucket": "fighting",
  "party": "", "guild": "guild03", "min": 52.3, "counted": true, "kpm": 1.4, "kpm_own": 1.4, "zph": 12000,
  "zph_own": 12000, "deaths_h": 0, "par_kpm": 1.6, "par_zph": 13000, "gap_kpm": 0.2, "gap_zph": 1000}]}
```
`cohort` matches by prefix (`swordman|41-60` = both modes). Bots with no time in the window are hidden unless `all=1`.

`GET /api/meter/bot/{name}`: identity, state, bucket, and per window `{"stats": <bot row as above with shares>,
"raw": {"min", "kills", "fair", "deaths", "loot", "cons", "travel", "zeny_in", "zeny_out", "sec": [9 buckets], "duty_min"}}`.
404 for a name never metered since the engine started.

`GET /api/meter/neo[?window=]`
```json
{"t": "...", "window": "1h",
 "ayla": {"name": "Ayla", "online": true, "lv": 81, "class": "Assassin", "map": "pay_dun00", "zeny": 29142,
          "guild": "Starveil", "cohort": "thief|81-99|party",
          "windows": {"10m": {"min": 9.8, "zph_purse": 4000, "par_zph": 0, "par_kpm": 0, "shares": {"hunting": 0.9}},
                      "1h": {}, "session": {}}, "note": "OpenKore character: ..."},
 "party_summary": {"n": 6, "above_par_kpm": 4, "above_par_zph": 3, "ratio_kpm": 1.12, "ratio_zph": 0.94},
 "party": [<bot rows>], "starveil_summary": {}, "starveil": [<bot rows>]}
```

`GET /api/meter/rows?local=1`: raw rows of this instance (used by the main instance to merge the core's).

### Both instances
Each instance meters its own bots. The main instance fetches the core instance's raw rows (`coreAPIURL()` +
`/api/meter/rows?local=1`, 1.5 s timeout, last good answer kept 2 min, the same pattern as partydash.go) and computes
cohorts, par and the Neo view over main + core rows, so `:8090/api/meter*` covers everyone; core members are found by
`/api/meter/bot/{name}` on :8090 too. The core instance's own `:8091/api/meter*` shows only its bots. Only the main
instance writes the history file.

## History: `legion/data/meter.jsonl`
At the first round after each full hour the main instance appends (one JSON object per line): one `server` row
(`totals`), one `cohort` row per cohort with n > 0 (`key, line, band, mode, n, kpm, zph, deaths_h, shares`), and one
`neo` row (`neo.party`, `neo.starveil` summaries), all for the 1-hour window, `t` = start of the hour just closed (UTC).
About 70 rows / 15 KB an hour. Git-ignored. `meter_history_off: true` stops it.

## Tuning keys (in `legion/data/tuning.json`, read by the meter every minute; missing keys keep the default)
| key | default | meaning |
|---|---|---|
| `meter_fair_lv_gap` | 15 | a kill is fair when monster level >= bot base level - this |
| `meter_min_online_pct` | 25 | % of a window a bot must be online to count in its cohort |
| `meter_period_sec` | 15 | aggregation period (min 5) |
| `meter_history_off` | false | true = no meter.jsonl rows |

The meter reads its own keys from the shared file instead of adding fields to `tuning.go`'s `Tuning`, so the parallel
branches' tuning edits merge without conflicts. tuning.go rewrites the file only when it is missing (then without
these keys: they keep their defaults).

## legionctl
```
python tools/legionctl.py meter [--window 10m|1h|session] [--top 15] [--sort kpm] [--json]
python tools/legionctl.py meter bots [--cohort "swordman|41-60"] [--sort zph|kpm|gap_zph|gap_kpm|deaths|min|name] [--asc] [--limit 30] [--window W] [--json]
python tools/legionctl.py meter bot <name> [--json]
python tools/legionctl.py meter neo [--window W] [--json]
```

## How to verify live (after the owner starts the stack)
1. `curl -s :8090/api/meter | head` returns 503 for the first 15 s, then a view; `rows` ~ the bots online since start,
   `core_rows` = the core instance's bots (0 if legion-core is down: check `:8091/api/meter/rows?local=1`).
2. After 3-5 minutes: `legionctl meter --window 10m`: server kpm should be roughly kills/min of `/api/overview` divided
   by bots fighting; time shares should add to ~100% and match the dashboard's state groups (fighting + searching
   large, stuck / dead small).
3. `legionctl meter bot <a fighting bot>`: `raw.kills` grows with the bot's `kills` in `/api/bots/<name>`; `raw.loot`
   grows on pickups; `raw.cons` grows when it drinks; `raw.travel` jumps by the fare after a Kafra teleport (event
   "Kafra teleport ...").
4. `legionctl meter neo`: the party list = Ayla's friends (instance core); Ayla's `zph_purse` follows her zeny in
   status.json.
5. After the first full hour: `tail -3 legion/data/meter.jsonl` (server, cohort and neo rows).
6. Lock check: the pprof listener (`-pprof`, default 127.0.0.1:6060) `/debug/pprof/goroutine?debug=1` shows one `meterLoop` goroutine, never blocked in a bot lock for long;
   the API stays responsive with ~1,000 bots.

## Known approximations and open risks
- Kill credit is the killing blow on our own target (see Fair kill); kills by party skills on non-targets (AoE) count
  only for monsters that were the caster's target. Party credit evens this out inside a party, not for a solo AoE
  caster (a Wizard's AoE kills of non-targets are undercounted).
- Loot is valued at the engine's price model, not at realised sale prices.
- Consumables rely on rAthena's delete reasons (0x07fa) and the use-item ack (0x01c8); an ammo stack moved by trade
  (reason 0) would count as burned.
- The time budget follows the free-text state strings; a new state string falls back to context. Re-check the table
  when states are added (`meterclass.go`, tested in `TestMeterBucketOf`).
- Ayla: purse change only (no kills, no loot value); a purchase shows as negative zph_purse.
- Party credit uses the current party for the whole window.
- No race-detector run (no gcc in WSL); the design keeps every shared value atomic or single-goroutine.
- Memory: ~6 KB per bot for the ring (about 6 MB for 1,000 bots).
