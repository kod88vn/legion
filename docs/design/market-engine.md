# Market engine (EC43-EC47): design

Status (2026-11-15): engine core, crier, roster tool, market metrics and dynamic pricing are merged to main (`marketengine.go` and the crier flags OFF, `dynamic_price_on` OFF); farmer orders, leader feed and the dashboard Market tab (DB11) are not yet merged. Current state: `STATUS.md`. Owner words: `docs/rules/archive.md`, section PR46 / EC43-EC47. Paths below are under `legion/` unless absolute. Go code is in `legion/legion/` (package main), static tables in `legion/legion/data/`.

Verification limit: `docker` is not installed in this session, so the read-only `docker exec ro-rathena` checks could not be run. Section 1 marks every fact as VERIFIED (in repo files) or UNVERIFIED (needs the commands in 1.1).

## 0. Decisions in one screen

1. The market is an in-process Go engine (`marketengine.go`), not a puppet master and not Python. Every input it needs (`stockDemand`, `dynPrices`, `pieceFarms`, `fitFarms`, `glutted`, chasers) already lives in the engine process (`opportunity.go`). `tools/puppet/progress.py` stays as the read-only briefing for the dashboard and the leaders' briefs; the need computation moves into Go and `progress.py` reads it from `GET /api/market/engine`.
2. Guild leaders get information only: `GET /api/market/feed?guild=guildNN` plus a file `data/market_feed.json`. The `set_by market` channel (EC43 first version) is retired (section 4.3).
3. Farmers reuse the existing money-goal machinery (`opps.goals`, `moneyGoal`, `farmGoal` kind "zeny"); an order is advice with a ranked list that the farmer's own code filters (safe map, EC39 gear gate), so code still decides (Constitution 2.7).
4. Broadcast has two channels: (A) bot-voiced `#trade` lines (exists today: `marketTalk`, rumors), (B) a new floating NPC "crier" that `announce`s a server message with no character in the world (like the job-change announce already in `rathena/npc-custom/legion.txt`). No GM, no GM command.
5. Roster: 150 farmers in phase 1, converted from guild-less personas that already exist (85 have hunter main plus vendor alt today); no new accounts and no re-seed.

## 1. Facts to verify first

### 1.1 Commands for the fixer (read-only, run inside ro-rathena)

```
docker exec ro-rathena sh -c 'grep -n -B2 -A14 "AegisName: Creamy_Card" /rathena/db/re/item_db_etc.yml /rathena/db/re/item_db_*.yml | head -60'
docker exec ro-rathena sh -c 'grep -n -A12 "AegisName: Belt\b" /rathena/db/re/item_db_equip.yml | head -30'
docker exec ro-rathena sh -c 'grep -n -A8 "AegisName: Trap$" /rathena/db/re/item_db_*.yml'
docker exec ro-rathena sh -c 'grep -n -A10 "Name: Creamy$" /rathena/db/re/mob_db.yml'
docker exec ro-rathena sh -c 'grep -n "HT_ANKLESNARE\|HT_SKIDTRAP\|AC_DOUBLE\|HT_BLITZBEAT" -A6 /rathena/db/re/skill_db.yml | head -60'
```
Record the output in this section; if the card id is not 4040 or the slot is not Accessory, fix `itemCreamyCard` (`guildstock.go:36`) and `itemCreamy` (`teleport.go:19`) first.

### 1.2 Creamy card (what is known)

- VERIFIED in repo: the engine treats "Creamy Card" as id 4040, an accessory-slot card giving Teleport lv 1 (`teleport.go:19`, `docs/classes/all.md` "Teleport card for everyone"). A card needs a slotted accessory: Belt [1] (2627, NPC 20,000z, lv 25) is the engine's accessory step (`teleslot.go`). Creamy is also the mob (Swift Creamy / Creamy on prt_fild02, gef_fild05; `strategy.go:55`).
- UNVERIFIED in the container: id, slot, the card's script (`skill AL_TELEPORT,1`), drop rate and drop mob (the card drops from the Creamy mob; official rate 0.01 percent of Renewal rate, which is very low, so the card is rare and expensive; check `mob_db`).
- Is it the right card for a hit-and-run hunter? Partly. Teleport is an escape and a ground-finder, not a kiting tool. What actually makes hit-and-run work is distance (Double Strafe at 9 cells), the kite step (`kite.go`), Ankle Snare, and the falcon's Blitz Beat. Teleport adds the "run" half when a fight turns bad (`danger.go` already casts it before wings) and removes dead time looking for spawns (`teleport.go`: a solo hunter with nothing to fight for 6 s teleports). Recommendation: keep the Creamy card as the farmer's accessory card (it matches GD08 and the owner's wish), and add two cheap run tools: Fly Wing / Butterfly Wing stock (300z) as the fallback while the card is not yet owned, and Traps (item 1065) for Ankle Snare. No other card replaces it: for pure kiting stats the real alternative is a speed source (Increase AGI buff, Matyr [AGI+1], Boots of AGI), but those are small. Do not wait for the card to start farming: until the farmer owns it, wings do the "run".

### 1.3 Hunter hit-and-run: can the engine do it today?

- VERIFIED in code: `kite.go` (bow users, Archer/Thief line): steps 4-6 cells straight away when the target or a recent attacker is within 2 cells (`kiteEvery` 1.2 s), lays Ankle Snare (skill 117) on the closest chaser when it carries Trap (1065). Limits: not with 3+ monsters on it, not next to a tank, not rooted or sitting, never vs plants. `docs/classes/archer.md` and `buildmilestones.json` (`hunter` playstyle) describe the same.
- Skills the plan has: Double Strafe 10, Owl's Eye, Vulture's Eye, Improve Concentration 5, Arrow Shower 10, Blitz Beat 5 (needs falcon, rented by `legion.txt` for 2,500z), Ankle Snare 5, Skid Trap, Land Mine. Weapon: bow plus arrows (`ammo.go`, "no arrows" outranks "out of potions").
- UNVERIFIED live: how much kiting helps in numbers. There is no kpm or deaths split for kiters. First work package measures it (`kite_steps`, `kite_deaths` counters per farmer) before the roster grows past 150.
- Gap found: kiting is not selective by monster speed. A fast melee monster closes the gap faster than the 1.2 s step; `danger.go` handles 3+ monsters but not "this mob is faster than me". The farmer's map choice must therefore prefer slow or ranged-passive mobs (`mob_db` speed field), a filter the allocator needs in any case (section 3.2).

### 1.4 Merchant second character

- VERIFIED: vending exists end to end: `vend.go` (Merchant-line `role "vendor"` walks to a south-gate pitch, casts Vending 41, opens a shop from cart, restocks every 30 min, markdown logic in `pricing.go`), `vendbuy.go` (other bots buy from player shops; NPC is the fallback, which already is EC47 in practice: `shopGearPick`, `shopPotPick`), `parcel.go` (RODEX to the account's own vendor "mule", up to 5 items a mail), `storage.go` (Kafra storage). Vendor builds: Blacksmith / Alchemist plans (`persona.go` seed: class 10 or 18, build "str-dex").
- Engine assumption: one account logs in as one character per session (`pickChar` in `persona.go`: weighted random, vendor 0.35, 0.8 for archetype merchant; `pin` overrides). So the hunter and the merchant are never online together. The hunter hands goods to the merchant by RODEX; the merchant collects on its next login and vends. This works but is slow. Farmer rule: session split 70 percent hunter / 30 percent merchant, with a forced merchant session after the hunter parcels (section 3.5).
- Not built: gearpool.go says account pooling (farmer funds its alts) is not built; the farmer account needs only the parcel path, not pooling.

## 2. Farmer roster spec

### 2.1 Who and how many

- Data today (personas.json, 4000 personas): 817 guild-less; 432 of them have a hunter-line main (`hunter` 385, `hunter_agi` 19, `hunter_blitz` 18, `hunter_trap` 10); 85 of those already have a vendor alt (class Blacksmith/Alchemist plan; vendor alts reset to Novice level 1 at seed like everyone).
- Phase 1: 150 farmers = the 85 complete guild-less pairs + 65 more guild-less hunter-main accounts that get a vendor character added in a free slot (EC18 already says "missing characters are created at level 1 on bot accounts, free slots"). Phase 2 target up to 300 (about 7 percent of 4000), raised only when `coverage` (section 3.6) says shortfalls stay open for 3 days.
- No account is taken out of a guild: farmers are chosen only among accounts whose `guild` is "" (no leave or kick needed, G20/G25 untouched). No new accounts in phase 1. If ever needed: account ids 2104001-2199999 only, personas index 4001+, slots 0 (hunter) and 1 (merchant).
- Extra characters on a chosen account (support in slot 2, a third char) stay offline: `pickChar` gets a farmer rule (hunter and vendor only).
- Mark them in a new file `data/farmers.json` (`{"accounts":["lg0123",...],"since":"..."}`), not by editing `personas.json` (the file is the seed source; a re-seed rewrites it). Adding the vendor characters uses an additive tool `tools/system/addchars.py`: INSERT only, no DELETE, bot range 2100001-2199999 only (G06), run offline against a copy first (G23). Do not run `reseed`.

### 2.2 Goal in the Constitution 2.3 sense

- Farmer's ultimate build = the hunter's end build from `buildmilestones.json` / `buildgear.json` (plan `hunter`: DEX 99 / AGI 75, Hunter Bow, Tights, Siege Manteau, accessory Belt [1] with Creamy, band 33-99), plus its merchant's vending shop. The market's orders are how the farmer earns toward it; the EC39 gear gate still applies (a farmer behind on its milestone farms for its own gear first, and buys the pieces from the player market, EC47).
- Conflicts to flag (owner decides; I do not edit the constitution):
  1. 2.5 lists "market" among the puppet masters (AI agents that never drive a character). The engine is code, not AI, and it addresses farmers. Proposed wording: move "market" from the 2.5 list to the 2.6 list as "the market engine (an algorithm, not an agent): it computes what the server needs, feeds the guild leaders, and gives farmers advice they weigh in their own code".
  2. 2.3 says every bot works toward its ultimate build by its own code. Proposed addition: "A farmer is a person with the same goal; it earns for that goal by working the market's wants, as a worker takes jobs."
  3. 2.4 says roles come from leaders and from the market. Farmer is a market-made role and fits; no edit needed.
- Hunter level and build plan: use plan `hunter` for phase 1 (385 of the candidates already run it; no new plan, no new EC42 tests). Optional phase 2: a `hunter_farmer` variant (skip Land Mine 1-5 and Skid Trap 2-5, keep Ankle Snare 5, Blitz Beat 5, more AGI) built to `builds.go` conventions with tests; only if 1.3 measurements show it helps. Level plan: normal levelling, gated orders (section 3.2): a farmer takes orders only from level 15 (like `checkMarket`).
- Merchant: existing vendor plans (Blacksmith `blacksmith`, `blacksmith_mace`, Alchemist variants); level goal the vendor default (lv 30-60 Merchant, job 40). It needs Vending, Discount, Overcharge, Pushcart.

## 3. Allocation algorithm

### 3.1 Inputs (all read-only, per tick every 5 minutes)

| input | source today |
|---|---|
| shortfall per item by band: build gear and card tables for the bands bots stand in, minus supply | `buildgear.json`, `buildmilestones.json` bands (`progress.py` `level_bands`, `short_goods`), `/api/buildgear/fit` |
| demand: live wants, sales, buyers | `stockDemand()` (`vendbuy.go`), `dynPrices` rows (`Buyers`, `BossOnly`, `Standard`) |
| supply: units listed, chasers (each counts 3), farmers already allocated | `opps.chase`, listings |
| guild stock needs | `/api/guild-stock`, `guildstock.go` gaps |
| zeny shortfall: bands with low zeny/h | `/api/meter/bots`, `progress.py` |
| price and rarity | `priceOf`, `glutted`, `glutMul` |
| yield per farmer-hour y(item,map) | `pieceFarms(id, lv, n)`: drop rate x spawn count; times the farmer's kpm from `meter` (fallback 3 kills/min) |
| danger vs farmer | `danger.go`, `huntfit.go`, `fitFarms`, `gearscore.go` (effective level) |
| current allocation | the engine's own table `farmers.orders` |

### 3.2 Score and constraints

For each candidate item i and each map m that drops it (pieceFarms, mob level <= farmer level + 10, then `fitFarms` for the individual farmer at pickup time):

```
need_i      = max(0, demand_i + guildneed_i + buildshort_i - listed_i - 3*chasers_i)    (units)
remain_i    = need_i - sum over farmers already holding an order for i of  y(i,m_f) * H     (H = 2 h)
value(i,f)  = min(remain_i, y(i,m)*H) * price_i * band_weight(i) * heal_mult(i)
                * (1 - risk(f,m)) / (1 + travel_min(f,m)/30) * speed_fit(m)
```
- risk(f,m) from effective level against mob level and the map's `danger` rating (0..0.8, above 0.8 not allowed); speed_fit penalises fast melee mobs (1.3), 1.0 otherwise (needs mob speed from `mob_db`); healing items weighted 2 as in `opportunity.go`; Creamy card gets the `+50` demand bonus already coded.
- Allocation: greedy water-filling. Sort items by remaining value; for each idle farmer pick the item with the highest value(i,f); subtract y*H from remain_i; recompute. A covered item drops out, so farmers spread in proportion to need without a quota table.
- Hard constraints: farmers per item at most `max(1, ceil(0.30 * online farmers))` and at most 12; farmers per map at most 5 (spawn contention and server load; also "crowded maps are skipped" already in `fitFarms`); never boss-only drops (`BossOnly`); glutted items skipped (`glutted`); drops that an NPC shop sells skipped (same rule as `marketOpps`); at most 70 percent of online farmers hold an order at one time (the rest live their routine, 2.3).
- Human pacing: order life 90-150 min (jitter around `oppGoalLife` 90); a farmer accepts an order with probability `0.5 + 0.5*greed` (persona trait) and starts after a 0-15 min delay; same item not re-ordered to the same farmer within 6 h unless remain score is 2x the next best; an order is dropped early only when the item is covered or glutted; order changes per farmer at most once per 60 min.

### 3.3 Output order format and channel

```json
{"farmer":"lg0123","char":"Name","order_id":"o-20261115-0031","t":"...","until":"...",
 "rank":[{"item_id":4040,"item":"Creamy Card","maps":["prt_fild02"],"mob":"Creamy","y_per_h":0.4,"value":12000}, ... up to 3],
 "reason":"band 18-32 short 14 units, price 82,000z, listed 0"}
```
- Channel: the existing in-process goal mechanism. The engine writes `opps.goals[bot]` / `farmGoal{Kind:"zeny"}` through a new `farmers.orders[bot]` that `MoneyFarmGoal` reads first for farmers (`opportunity.go:351`; consumers `ai.go` solo picker, `lfg.go`). The farmer's own code takes the first entry of `rank` that passes `fitFarms` and `soloMapOK`; none fit means the order is declined and logged (`order_declined` with reason). Persistence: `data/farmer_orders.json` (atomic write, loaded on start), like `guild_duty.json`.
- Observability and manual use: `GET /api/market/orders`, and `POST /api/market/orders/{farmer}` for the owner or Ayla's tooling to clear one (never to set one: the engine is the only writer).

### 3.4 Report-back and selling

- Loot order stays EC23 (use, party, guild, stock, sell). A farmer has no party or guild, so: keep one copy of own build gear, sell the rest.
- Hunter: sells small stacks itself on `#trade` and by the existing shop path; at >= 50 percent weight or a full order block, parcels the surplus to its own merchant by RODEX (`parcel.go` mule path). After a parcel, the farmer's next session is forced to the merchant (the new `pickChar` farmer rule), which collects, loads the cart and vends at a south-gate pitch (`vend.go`, prices from `pricing.go`).
- Zeny: the merchant's shop revenue stays on the merchant; the hunter buys its own gear from the player market first (EC47) through `vendbuy.go`, NPC last.
- The engine counts what each farmer supplied: `farmer_items_supplied{band,item}` from loot and sale events (`b.event` kinds already exist).

### 3.5 Metrics (flag-independent counters, persisted like `econ_counters.json`)

zeny per hour per farmer and by band (`meter`); items supplied per band and item vs need (coverage = supplied / need over 24 h); open need per band; price stability = stddev of price/standard for ordered items over 24 h, target under 1.5; order acceptance rate and decline reasons; distribution (farmers per item, per map, max share); deaths per farmer-hour and stuck rate (kiting check); kite steps vs deaths; idle share (no order). Judge success by coverage up and price stability, not by order count.

## 4. Guild leaders and market data

### 4.1 Feed

- File `data/market_feed.json` (written every 5 min) and `GET /api/market/feed?guild=guildNN` (the guild's view of the same):
```json
{"t":"...","status":"ok|partial|no_data","missing":[],
 "server":{"bots":4000,"online":850,"bands":[{"band":"18-32","bots":410}]},
 "short":[{"item_id":..,"item":"..","kind":"none|short","band":"18-32","demand":14,"listed":0,"price":..,"drop_maps":["prt_fild02"],"farmers_on":3}],
 "glut":[{"item":"..","listed":60}],
 "zeny_per_hour":[{"band":"18-32","n":120,"zph_mean":8300}],
 "guild":{"id":"guild03","needs":[{"item":"..","n":5}],"gear_gap_members":[{"band":"18-32","n":22}],"farmers_note":"info only"}}
```
- It carries data only: no `set_by`, no direction, no "should". A missing source is named in `missing`; no number is invented (same rules as `progress.py`).

### 4.2 Which leader decisions use it

- Existing code decisions: `autopilotDirs` (`guildduty.go:399`) takes `gaps` from the guild stock only; add the feed's `short` items for the guild's bands as extra candidates (still its own pick, still 15-minute autopilot directions); `dutyMaps` `dirWealth` branch picks via `MoneyFarmGoal`: score uses the same `marketOpps` plus the feed's `farmers_on` so guild members do not pile onto what farmers already cover.
- Leader (the daily puppet master) gets the feed as its briefing block (`tools/puppet/briefing.py` replaces the `progress` block by the guild's feed slice); it decides directions with `guild-direction-set --by leader` on its own.
- Answer to the owner's question "can guild leaders' farming decisions be done by algorithm?": yes, the farming half already is: `autopilotDirs`, `dutyMaps`, `dutyPlan` choose what, where and who with no AI. What stays AI: rivalry, diplomacy, chat voice, who to favour, whether to start a boss hunt, and overriding the autopilot. So the leader AI shrinks to a once-a-day pass (Constitution 2.9).

### 4.3 Retiring the market direction channel

Retire, not repurpose: the feed replaces it. Steps (WP6): 1) keep `market_directions_on=false` (default now); 2) `briefs/market.md` is rewritten: the market master stops setting directions and farm advice (no AI market role remains for those); 3) one release later delete `gduty.market`, `guildMarketMax`, `marketDirOn`, the `by=market` branch in `guildduty_api.go`, `market_set` in the guild view, tests in `guildduty_market_test.go`; 4) dashboard shows the feed in its place (G08: add, do not remove: keep the `market_set` field shown as "retired, empty" until the owner confirms removal). Decision for the owner: what happens to the AI market master's other jobs (vendor shop titles and markups `shop --markup`, `#trade` lines, answering humans via `inbox --role market`). Recommendation: pricing and shop markups are already code (`pricing.go`), `#trade` lines are already code (`marketTalk`), human questions go to the chat agent; so the AI market master is switched off only when the owner decides (Constitution 2.9: no agent is retired without the owner).

## 5. Broadcast (EC44)

### 5.1 Mechanism

- What exists: (A) `marketTalk` / `heardRumor` in `opportunity.go`: a bot that looked over the market says one line on `#trade`, about one a minute server-wide, same item once per 15 min, and hearers may take it up (rumor life 20 min). (B) rathena already announces from an NPC script with no character (`announce ..., bc_all|bc_blue` in `rathena/npc-custom/legion.txt:98`, "X has become a Y!"). A GM character or GM command is not used and not needed (G03, G04).
- Build (B2): a floating NPC `MarketCrier` in `rathena/npc-custom/legion_crier.txt` (scripts_custom.conf loads it) with an `OnTimer` every 60 s that runs `query_sql "SELECT id,msg FROM legion_notice WHERE sent=0 ORDER BY id LIMIT 1"`, `announce` the text with `bc_all` (no name prefix, one fixed colour), then marks `sent=1`. The table `legion_notice(id, msg, created, sent)` is created by a new `rathena/sql/legion_notice.sql`. The engine writes a row; engine has no SQL client today, so the writer is a small system tool `tools/system/crier.py` (docker exec into `ro-db`, a single INSERT; the engine calls it by file hand-off: it appends to `data/market_notices.jsonl`, the tool tails that and inserts). Tests use a throwaway DB (G23). Loading a changed NPC script needs a map-server restart or script reload: that is a deploy-window item (G16, G17), not an in-session action.
- Why not a console broadcast: rAthena's console has no reliable broadcast command, and it would need a shell into the server; the NPC route is data-driven and safe.

### 5.2 Cadence and wording

- At most 4 notices a day, at least 3 h apart, only when the top shortfall has `remain` above a threshold for 2 consecutive ticks; the same item at most once per 12 h; never at night local to the owner's server time window (08:00-23:00 only); stop when the item is covered. Channel (A) lines run on top within their existing limits.
- Wording rules (Constitution 2.2, G18, G25): the text must read as a server or town event notice that any normal online game has; same for every player, no human special case. Style: plain, short, named place and item, no numbers computed by the system (no "short by 14 units"), no server stats, no words "bot", "AI", "algorithm", "engine", "Legion", "simulation", "market engine", "the system". Allowed shape: `[Prontera Merchants] Poring Coins are selling fast; hunters are wanted around Prontera field 02.` Use 6-8 hand-written templates per message kind (want, glut, event) with item and place slots; the engine only fills the slots, never writes free text. A test rejects any message containing a banned word or more than 120 characters.

## 6. Work packages (parallel by file ownership)

Flags (tuning.json, all default false, names fixed): `market_engine_on` (computes and serves the feed, no effect on bots), `farmer_orders_on` (farmers take orders), `market_broadcast_on` (channel B notices), `guild_market_feed_on` (autopilot and `dutyMaps` read the feed), `farmer_roster_on` (farmer `pickChar` rule and session split). Rollback for each: set the flag false and restart nothing; the table or file stays unread.

| WP | owner files | content | tests (no live data, G23) |
|---|---|---|---|
| WP1 verify | this file (section 1), `docs/classes/archer.md` | run 1.1 commands, record facts; add `kite_steps`/`kite_deaths` counters in `kite.go` | LEGION_DATA fixtures |
| WP2 engine core | new `marketengine.go`, `marketengine_test.go`; touches `tuning.go` (flags) only | needs, scoring, water-filling, API `/api/market/engine`, `/api/market/feed`, `data/market_feed.json` | pure function tests with fixture economy: distribution, caps, rotation, glut skip |
| WP3 farmer orders | new `farmers.go`, `farmers_test.go`; touches `opportunity.go` (`MoneyFarmGoal` hook), `persona.go` (`pickChar` rule) | roster load (`farmers.json`), orders, accept probability, report-back, forced merchant session | fake bots: order declined when unsafe, spread over items |
| WP4 roster tool | new `tools/system/addchars.py`, tests in `tests/system/` | additive INSERT only in 2100001-2199999, offline copy first, dry-run default | refuses ids outside range, refuses DELETE, G06 check |
| WP5 feed consumers | `guildduty.go` (`autopilotDirs`, `dutyMaps`), `tools/puppet/briefing.py`, `progress.py` | leaders read the feed, flag `guild_market_feed_on` | `guildduty_test.go` additions |
| WP6 retire market directions | `guildduty*.go`, `guildduty_api.go`, `guildduty_market_test.go`, `tools/puppet/briefs/market.md`, `dispatch.py` | steps in 4.3; second step a release later | existing tests updated; G08 check |
| WP7 broadcast | new `rathena/npc-custom/legion_crier.txt`, `rathena/sql/legion_notice.sql`, `tools/system/crier.py`; `scripts_custom.conf`; templates in `marketengine.go` (small) | section 5 | banned-word and length test; throwaway DB; no live announce |
| WP8 metrics | `meter*.go` additions, dashboard tab | section 3.5, add only (G08) | meter tests |

Order: WP1 and WP2 first (parallel), then WP3 and WP4 (parallel), then WP5, WP7, WP8, WP6 last. Each deployed as one 3-hour batch by the deploy agent (G17), reviewed by Sonnet (4.2). Rollout: flags on one at a time, each verified on live evidence before the next (3.5, 3.6): `market_engine_on` (feed reads sane for a day), `farmer_roster_on` with 20 farmers, `farmer_orders_on` with 20, then 150; broadcast last, first as a dry run that only writes `market_notices.jsonl` without the crier loading. Ayla's party first for anything touching combat (2.8), but farmers do not touch her party.

## 7. Risks and unverified

- Container facts in section 1 are unverified (no docker here).
- `farmGoal` and the solo hunting picker interact with party and guild blocks; a farmer never joins a party or guild, so this should be clean, but `lfg.go` zeny-goal parties could pull farmers into parties: set `lfg` to skip farmer accounts.
- Order stacking: `opps.goals` is shared with ordinary bots' own money goals; counting both as chasers is intended, but WP2 must make sure farmers do not starve ordinary bots of the best items (cap 30 percent).
- Creamy card supply is tiny (very low drop); the engine may rank it first forever; cap its demand bonus so `remain` falls as farmers are put on it, and treat it as a long-run item.
- Kiting effectiveness vs fast melee is unmeasured (1.3). If farmers die more than ordinary bots, phase 2 stays closed.
- Shop economy: 150 vendors at the south gate need pitches; `market.free` is a fixed grid; check pitch capacity (merchant sessions are only 30 percent, so about 45 online at a time).
- Broadcasts reach humans too; wording must stay natural; a wrong item or map is visible. Hence templates and a daily cap.
- `progress.py` bands use class names, not the build id ("approximate", its own note); WP2 should use the build id from the persona.
- Not verified: `mob_db` speed field availability in the engine's `mobs` table; `char slot` limit of the server (personas use slots 0-3 only).
