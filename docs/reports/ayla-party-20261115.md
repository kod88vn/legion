# Ayla's party, 2026-11-15: why the team did badly (the members' side)

Read-only investigation, written after the shutdown. It adds to `postmortem-20261115.md` and does not repeat it.
That report covered Ayla's own exp/h, map choice, the waits stacked in `partylead.pl`, her party lines not being sent,
roster churn and rules that conflict. This one is about the members and how the party worked as a team.
Times are local (UTC-5) unless marked Z. Data files are in `ayla-party-20261115/`.

## Sources and their limits
- **Ayla's OpenKore log** (`docker logs ro-openkore-ayla`, 11-14 20:43 to 11-15 09:07, 742 minutes). It shows every
  skill a member casts within her view range (about 14 cells), all party chat and her plugin's decisions.
- **Engine logs.** Both Legion containers were recreated at 08:26 (13:26Z), so their logs cover only the last
  **41 minutes** (08:26-09:07). `ro-legion-core` ran only her 4 members. Full per-packet TRACE exists only for
  the priest Zendancer84 and the wizard Zen.fox. Everything from the engine log is a 41-minute sample.
- **`picklog` in ro-db** (read-only SELECTs). Every pickup and every consumed item, with the map, per character.
  It gives each member's map over time, potion, wing and arrow use, and benchmarks against other bots. Arrows
  consumed are a direct count of a hunter's shots.
- **`meter.jsonl`.** Cohort and "neo" rows only, with no per-bot rows (bug b1794770968pp2). The level book has no
  rows for her members (b1794770968dbk).
- **Database snapshot.** Gear and stats are taken from the live tables, which equal the 14:48 dbsnap (nothing ran
  in between).

Roster at shutdown, in DB party 2 "Ayla pt" (exp even, item share). Note that the party's in-game leader is
**Zen.fox**, not Ayla.

| member | class | lv | STR/AGI/VIT/INT/DEX | max HP/SP | weapon | armour, shield |
|---|---|---|---|---|---|---|
| Ayla | Assassin | 84 | 66/83/1/1/15, **24 stat points unspent** | 4515/359 | Jur | Coat, Muffler, Boots, Cap |
| Zendancer84 | Priest | 81 | 1/1/50/96/35 | 4527/1324 | Staff + Soldier Skeleton card | Silk Robe, Buckler, Biretta |
| Zen.fox | Wizard | 80 | 1/1/43/80/65 | 3171/1389 | Staff (MATK 70, a level-1 staff) | Adventurer's Suit, Guard, Hood, Hat |
| xXRinfangXx | Hunter | 79 | 1/60/20/1/84 | 3802/335 | Great Bow + Skeleton card, plain Arrows | Mantle, Goggles |
| vex_x18 | Crusader | 81 | 60/1/77/25/45 | 9664/507 | Saber | Wooden Mail, Guard, Goggles |

Rotation members during the night: Knight Echobunny, Monk LilLilyblade and Rogue Silent rogue went out; Rogue
Silversage13 came in at 03:36 and Bard Echo.0783 at 06:40.

## 1. Bottom line
- **Exp sharing was not broken.** Even share was on (`party.exp=1`), `party_share_level` is 99 (import file,
  `rathena/conf/import/inter_conf.txt:28`), and the level spread is 79-84. Share has no bonus
  (`party_even_share_bonus: 0`). Only members on the same map and alive share a kill (rathena `party.cpp:1253`).
- **The members were rarely in the fight.** In the 41-minute engine sample the 4 members made **59 kills
  (0.36 kills/min each) and died 6 times**. The priest and the wizard spent 37-38% of their time travelling to
  her, 19-27% sitting or resting, 9-10% dead or fleeing, and 19% fighting. The meter's "neo party" rows put the
  members at **0-29% of their cohort's kills/min par** in every hour that had data (e.g. 09Z ratio_kpm 0, 11Z
  0.15, 13Z 0.29).
- **Benchmark.** Her hunter shot 4,134 arrows from 20:00 to 09:07. The two best party hunters of similar level
  shot 9,885 (Thornlord, lv80) and 8,967 (Mint_cat, lv81); the median of 70 hunters at lv70-90 is 5,216. That is
  **42-46% of the best party hunters**. He was shooting in 263 minutes against 373-434, and at 15.7 arrows per
  active minute against about 23-26. The best parties have the same shop gear and the same Red Potions. The
  difference is that they stay on one **open field** (prt_fild09, mjolnir_07, gef_fild02) and never cross
  dungeon floors.
- **The largest cost on the members' side was getting to her.** She hunted floors 2-3 of Payon Cave. Each member
  walked in alone through floors 0-2, and every monster count there is ×4 (`mob_count_rate: 400`). They were
  mobbed, died or fled, respawned in Payon and walked in again. Rest holds, the town runs set off when she
  entered a building, and the rally mirror that kept switching on and off took most of what was left.

## 2. Causes, ranked by what they cost
The estimates are member time (M) or Ayla time (A) lost over the 12.4-hour log, unless a smaller window is given.

### C1. Members cross the dungeon floors alone and die on the way (high confidence in the sample, medium for the whole night)
- **Evidence from the 41-minute sample** (`core_trace_excerpts.txt`):
  - Priest: rally to the Payon Kafra, then "travel -> pay_dun03" from 08:37 to 09:01, fighting Zombies and
    Furious Zombies on pay_dun00/01 with a Staff (81 damage a hit, 210 hits). He died at 08:35:12, 08:42:37 and
    08:57:13. He cast Teleport 6 times ("losing: 35 monsters on me" at 08:46:26) and fled to town at 08:53:10.
  - Priest damage: he **took 1,051 hits for 35,656 damage, 7.9 times his max HP, in 41 minutes**.
  - Wizard: died at 08:35:08 and 08:45:09, fled to town at 08:58:16 with 10 monsters on him, and spent 08:39 and
    08:52-08:54 in town for potions.
  - Their time over the sample:

    | member | travel | sitting or resting | fighting | heal or buff | dead or fleeing |
    |---|---|---|---|---|---|
    | priest | 37% | 19% | 19% | 16% (mostly on himself) | 9% |
    | wizard | 38% | 27% | 19% | – | 10% |

  - After each death the recovery took 2 minutes (priest) to 9 minutes (wizard 08:45, then a town trip, then
    walking in again at 08:54).
- **Why it is so deadly:**
  - pay_dun03 is reached only through pay_dun00, 01 and 02, and with `mob_count_rate: 400` each floor holds 4
    times the stock monsters (`rathena/conf/import/battle_conf.txt`).
  - The respawn point is Payon, so every death restarts the whole walk.
  - Transit kills are almost worthless. At lv80 the Renewal penalty is 10% for Zombie (lv17), Soldier Skeleton
    (34) and Drainliar (47), and 35% for Archer Skeleton (50) (`db/re/level_penalty.yml`).
  - Walks were also refused at warp cells: "walk to 127,121 refused (warp area in the way)" repeated about 30
    times by both bots at 08:31-08:35.
- **Over the whole night:**
  - Her plugin logged "wait on Zendancer84 ended: … not on our map" 406 times and "AY22: Zendancer84 fell behind"
    78 times.
  - Members said "almost dead/died, winged to … kafra" or "resting" in 129 rest announcements.
  - From picklog: the wizard was on a hunting map with her in only 77 of the 122 minutes in which he did
    anything at all.
- **Cost:** about half of member time in the sample (travel plus dead plus fleeing is 46-48%). Over the night
  I estimate 25-40% of member time. Every on-floor hour without the priest also triggered her own waits (AY22).
  This is the largest single member-side loss.
- **What the best parties do instead:** they stay on an open field (prt_fild09, mjolnir_07) where a straggler
  walks over grass, not through three floors of monsters.

### C2. A member's rest stops Ayla wherever that member is (high confidence in the code, medium on the cost)
- **Plugin.** `partylead.pl:1011-1028` (`party_resting`):
  - Any member's announcement that matches `need sp|resting|sitting|low sp…` (line 1483) counts as a party rest
    for 180 s, with **no map or distance check**.
  - Any member on her map whose engine state contains rest or sit counts too, at **any distance**. For example
    "low HP (13%): resting before moving on" 70 cells away counts.
  - `sweep_tick` then returns at once (line 1233).
- **Measured:**
  - 129 member rest announcements, whose holds add up to **up to 89 minutes (12%) of her time**.
  - Her log shows "sweep waits: … party resting" in **61 distinct minutes**.
  - Many announcements came from members on another floor or in town, e.g. "almost dead, resting" from the
    hunter at 01:07.
- **Engine hold (coordinator's suspicion #1).** together.go:96-117 and 229-237 make members within 25 cells sit or
  guard. It **did not occur** in the 41-minute trace: no "sitting with the party" or "guarding" line for the
  priest or the wizard. Not confirmed live; the cost there is unknown.
- **Related:** bug b1794734716906-168 covers the 12-20-cell case only.
- **Cost:** 4-8% of her farming time (61-89 min).

### C3. Every building she enters sets off a town run for the whole party (high confidence)
- **Code.** `legion/ai.go:350-358`: when the human is inside a town building, `callTownRun` runs for everyone.
- **Her log:** 23 distinct minutes with a Map Change into geffen_in, payon_in01 or gef_tower. Almost every one is
  followed within 0-1 minutes by member lines "ok following to town", "ok town run, i got stuff to sell too" and
  "town run, omw". Examples in Z: 03:14, 03:49, 04:02, 04:10, 04:38, 05:29, 05:45, 06:31, 07:23, 08:38, 09:49,
  10:15, 10:44, 10:58, 12:06.
- **Cost:** the pairs of "town run" and "back" lines add up to about **450 member-minutes** (mean 4.7 min per
  member trip, 96 trips). This is inferred from chat timing.
- **Coordinator's suspicion #6 (members go alone after 10 min, ai.go:477):** the code path exists; I did not
  isolate how often it fired.

### C4. The engine's copy of her rally switches on and off, and members walk to the Kafra while she calls them into the dungeon (high confidence)
- **The loop.** `rallysync.go:22-46` copies her `status.json` rally_town into the members' rally. `rally.go:594-598`
  ("Ayla left payon") then ends it with `endRally(false)` as soon as she is not in the rally town. On the next tick
  the mirror starts it again.
- **Live at the end of the log** (`core_trace_excerpts.txt`):
  - "Ayla rallies at the payon Kafra (181,104) - members wait there" was logged **15 times between 09:02:08 and
    09:07:39**, about every 22 s.
  - The priest said "cant all meet up in payon, lets just go" 7 times in 2.5 minutes (partychat.json).
  - Meanwhile she was on pay_dun02 posting "xXRinfangXx come to me pls, pay_dun02 260,199". The hunter, the
    wizard and the crusader sat at the Kafra ("here at the kafra, waiting") and the wizard switched between
    "rally: waiting at the Kafra" and "going to Ayla (pullback)".
- **Over the night:** the priest's "cant all meet up" line appears in **35 distinct minutes**. Each is one
  mirrored rally that broke.
- **Cost:** 35 broken gatherings, typically 1-5 minutes each for 4 members, so about 100-300 member-minutes, plus
  her own waits for members who were at the Kafra.
- **Coordinator's suspicion #5** (several systems rewriting the goal) is confirmed for this window. The priest
  went "rally: going to the Kafra" to "travel -> pay_dun02" to fighting, and back, each minute from 09:02 to
  09:07. From 08:37 to 09:01 his goal held steady ("travel -> pay_dun03").

### C5. HP potions that are too weak, and a potion-count gap that holds members in town (high confidence)
- **Weak potions.** Members drink Red Potions (+45 HP) at 3,171-9,664 max HP. Overnight the party consumed 463
  Red Potions, 188 Red Herbs and 218 Orange Potions (Ayla). Survival therefore depends on resting, which is
  where C2 comes from, and on fleeing.
- **The gap** (coordinator's suspicion #4), confirmed:
  - `town.go:79-95` sets potMin = 25 for lv60+ with max HP over 3,000.
  - escortGate holds a member in town to "restock potions before going in" when its count is under potMin
    (`together.go:1077-1078`).
  - The town trip that would buy them starts only under **3** potions (`town.go:161`).
  - A member with 3-24 potions therefore waits with no trip and no timeout. Zen.fox (15 Red Potions at shutdown)
    sat in "waiting in payon: restocking potions before going in" at 08:39:42 and again from 09:05:59 to the end
    of the log.
- **Not the differentiator.** The benchmark parties use the same Red Potions (party 3062: 435 Red Potions). Weak
  potions hurt here because of C1.

### C6. Weak exp per kill, made worse by sharing with idle members (high confidence in the mechanics, the size is the postmortem's)
- **Sharing.** Even share works, but there is no bonus, so each member gets party exp per kill ÷ the number of
  members alive on the map. Per-member exp beats soloing only if the party kills c times faster than one bot.
  Members on her map who are resting, sitting or fleeing reduce her share without adding kills.
- **Who killed.** Her log has 6,345 exp events and 2,165 of her own kills, so about two thirds of shared kills
  came from members when they were present. That is mostly Storm Gust (1,157 hits seen), Double Strafe and Arrow
  Shower, Magnum Break and Holy Cross.
- **Tap bonus.** `exp_bonus_attacker: 25` gives +25% exp per extra attacker on a monster. Members attacking her
  target are rare: "assisting Ayla" appears once in the wizard's trace.
- **Level penalty.** Where they fought, the Renewal penalty at lv80 is 10% (Zombie, Soldier Skeleton, Drainliar),
  35% (Archer Skeleton), 60% (Munak, Bongun) and 85% (Sohee). The postmortem's map finding is the main lever here.

### C7. Gear makes kills slow and dungeons deadly (medium confidence on the effect)
- Everything is unrefined level 1-2 shop gear.
- The wizard's level-1 Staff (MATK 70) at lv80.
- The priest's Staff carries a Soldier Skeleton card (crit, useless to a priest).
- The hunter shoots plain Arrows in an undead dungeon, where Silver (holy) or Fire arrows do 1.25-1.5× damage.
- Storm Gust is water and cannot freeze undead (most of Payon Cave), so the wizard's main AoE lost its crowd
  control there.
- The benchmark parties are just as badly geared (party 3062: Great Bow, plain Arrow, Staff), so gear is not why
  her party trails them. It matters on her maps (C1).
- Ayla herself has **24 unspent status points** and DEX 15.

### C8. Rotation members who never came (high confidence, small direct exp effect)
- The bard Echo.0783 joined at 06:40 and was on her map **0%** of his active time up to 09:07 (157 events on
  mjolnir_07).
- The rogue Silversage13 was on her map 9% of the time (night) and 23% (morning).
- Earlier, the monk LilLilyblade was on her map 0% from 12:23 to 20:00.
- Off-map members do not reduce exp, but her plugin's "N members off the map" holds and rallies count them
  (postmortem, roster churn).

## 3. The coordinator's code suspicions, one by one
| # | suspicion | verdict | measured cost |
|---|---|---|---|
| 1 | party-wide engine rest hold within 25 cells | engine side not seen in the 41-min trace (0 "sitting with the party" or "guarding" states); the plugin's own rest hold is worse and confirmed (C2) | engine: not measurable; plugin: 61-89 min of hers |
| 2 | priest rests to 80% SP while the leader stands still | rejected as a cause | 08:26-08:34 the priest sat by her (52 sit/stand state flips, standing to buff every 3-6 s), but she stood still because her sweep was stuck on "waypoint 8/106" with 1-2 kills in 10 min (bug b1794749315573-892) |
| 3 | her rest regex matches "rest"/"sit" anywhere | confirmed, small | 12 matching lines in 12.4 h, 9 of them false ("k, sit, we cover u" ×5, "np, rest up", "tell me if u need a break"); at most about 27 min of holds |
| 4 | escortGate waits without a timeout for 3 to potMin potions | confirmed (C5) | wizard ≥3 min in the 41-min sample, ongoing at shutdown; night cost unknown (no trace) |
| 5 | six systems rewrite the members' goal | partly confirmed: flapping seen 09:02-09:07 with the rally mirror (C4); steady 08:37-09:01 | part of C4 |
| 6 | a building she enters calls a town run for everyone | confirmed (C3) | about 450 member-minutes |
| 7 | built-in waits (90 s per map, 3-min Kafra meet, rest after death) | partly measured: post-death recovery 2-9 min per death (6 deaths in the sample); Kafra meets break 35 times (C4) | in C1 and C4 |

## 4. Smaller observations
- 08:26:49 and 08:27:02: the priest's Blessing on Ayla (account 2000000) was sent twice without the server
  confirming it, while she asked "blessing pls" at 08:27:01. The third try at 08:27:17 worked. When the members
  were together, buff upkeep was fine: about every 4 min on all 5.
- Heals on Ayla: only 75 "casting Heal on you" in 12.4 h. Her own potion use (229 Orange, 159 Red) carried her.
- The DB party leader is Zen.fox, not Ayla (`party.leader_id = 2101589`).
- 21:30-21:50: about 3,200 OpenKore "[getRoute] no valid closest coordinates on field izlude (24,98)" errors, so
  she was stuck about 20 min on an unwalkable cell in Izlude. This is on her side; it is noted for the record.
- 5,553 "Found perfectly hidden Player Wanderer" lines, from the owner's GM near her. `avoidGM_near 0`, so it is
  harmless spam.

## 5. What is verified and what is inferred
- **Verified** (log lines or DB rows quoted): party settings, gear and stats, the 41-minute member trace (deaths,
  states, damage), the rally-mirror loop, the building to town-run correlation, the potion gap (code plus one
  live instance), the arrow and potion counts, and the benchmark arrows.
- **Inferred:**
  - Night-wide member time budgets: picklog only shows minutes with a pickup or consumption, so it is a lower
    bound on presence.
  - Town-run minutes, from the timing of chat pairs.
  - The upper bound of the rest holds, which assumes each announcement held her its full 180 s unless a "ready"
    line came.
  - The 25-40% night-wide transit share, extrapolated from the sample and the "not on our map" counts.
- **Not measurable:** per-member kills or deaths overnight (no per-bot meter rows; bug b1794770968pp2).

## 6. How to reproduce
`ayla-party-20261115/scripts/` holds the four scripts. They read `pick.tsv` (picklog rows for these chars since
11-14 17:00Z), `permin.json` (built by `permin.py` from the ANSI-stripped Ayla log) and the container logs.
