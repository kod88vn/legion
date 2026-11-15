
## 2026-11-13 18:14 EST followers do not re-path to a leader who moved maps
- Evidence: After the server restart, Ayla moved to payon. Echobunny stayed on moc_fild02 with state "waiting for Ayla" and goal map moc_fild02 for 3+ cycles, while Zendancer84 and Zen.fox (both in payon) were fine. Echobunny kills 0, loots 4.
- Suggested rule for all bots: when the party leader's location is unknown or differs from the follower's map, the follower should path to the leader's last known map and report "waiting for leader" with a timeout. After about 3 min with no progress, it should fall back to the party checkpoint town.
- 2026-11-13 20:32-20:35 (watchdog observation): Ayla went offline and her three friends went offline with her (party 0/4). Ayla was back in ~1 min, the friends in ~3 min. Nothing was restarted by the watchdog. Party re-formed on pay_dun00 at 4/4 by 20:35.
- 2026-11-13 20:45-20:47 (watchdog observation): second drop. Ayla and friends went offline together again (party 0/4) while world online fell from ~914 to 251, then recovered. Ayla was back in ~1 min, friends in ~2 min. The party drops together with Ayla, so the cause is server-level (rathena or OpenKore restart), not party behaviour. Check container logs before blaming the party.

## 2026-11-13 evening - lessons from Ayla's party, applied to all bots (engineer)
Each line: what the core party showed -> the rule now in the engine.
- Friends at level 30+ still had the starter Knife and ~200z: in a human-led party they never looted and pinned bots
  had no town trips -> party members loot near the group (party.go), take town trips, and buy gear by Kafra when
  the shop is far (builds.go gearShopNear).
- Ayla sold loot in town alone while the party stayed behind -> town runs are a party event: leader in town or a
  member asking ("need a town run") sends everyone (party.go callTownRun, ai.go).
- Money sat with the leader (17k) while members had nothing -> bots accept trades from party/guild mates and walk
  up to them (bot.go 0x01f4, ai.go tradeThink); Ayla hands zeny with tools/rogui/ayla-give.sh.
- The tank assisted but never took hits -> tank order: take monsters off others (Provoke), pull next, then assist.
- Leader and follower froze forever ("waiting for X" / "following leader") -> followers pathfind when >10 cells
  behind; leader waits at most 15 s (ai.go, party.go).
- Bots stood "fighting" a monster the server never let them hit -> a target with no landed action for 10 s is dropped.
- 424 of ~660 active bots "roamed" with 0 kills: fields hunted out or only too-strong monsters -> spawns x4 and
  faster respawn (battle_conf), target the nearest UNCLAIMED monster, leave a map after 45 s without a target.
- Ayla looped town<->cave for 20 min: a buyAuto rule named an item the NPC lists under another name -> never use
  buyAuto for one-off gear; buy by hand (talknpc + buy index). Her leadership plugin also cancelled her walks in
  town -> no holding inside towns.
- Ayla said 2 lines in an hour -> leaders talk: Ayla's plugin and bot party leaders call out waits, regroups,
  town runs, level-ups; members announce trips.
- The watch client froze for 10 min unnoticed -> ai-client.sh restarts it on a Wine lock timeout, an error box or
  a screen unchanged for 60 s; tools/sentinel.py checks the whole system every minute and wakes the engineer.
- Canned chatter flooded #global and party chat (398 lines/min) -> #global carries only LFP, trade and guild
  recruiting at 1-3 lines/min; leader call-outs at most once per 2 min; conversation is the LLM minds' job.
- Vendors could never open a shop after the re-seed (no cart rental, no cart loading) -> vend.go rents a cart at
  the Prontera Kafra and loads loot before Vending.

## 2026-11-13 Ayla sat inside payon_in01 after shopping
- Symptom: Ayla idle and "Sitting" at payon_in01 4,46 while her friends waited outside in Payon; AI queue empty.
- Cause: after sell/buy she stayed in the building (sitAuto_idle keeps her seated; route_randomWalk 0; the unstuck
  step skips sitting). Friends never follow a leader into buildings, so the "all together" check never passed.
- Fix: partylead.pl walks the leader out to the town map (stand + ai_route) when idle inside a `_in` map; route_randomWalk 1.
- Legion rule (already in engine): members wait outside buildings; leader must leave the building once shopping is done.
- Note: the OpenKore console command `ai` TOGGLES AI off - never send bare `ai` through commands.txt; use `ai on`.

## 2026-11-13 Ayla's friends walked to Prontera without her (party split 8+ min)
- Symptom: all 3 friends "travel -> prontera" from moc_fild02/pay_arche while Ayla tried to collect them (she bounced pay_gld <-> moc_fild02).
- Cause: a member's real need (out of potions) called a party town run; partyRunTown picked Prontera because Zen.fox could
  afford gear Payon doesn't sell. The human leader never joined: her plugin matched "out of pots" but bots say "out of potions".
- Fix: in a human-led party members only ASK in party chat (the human decides; the human-in-town branch then brings them
  along); partyRunTown never returns Prontera for a human-led party; partylead.pl matches "out of potions".
- General: in any party the leader decides the town; a member's shopping list must never pull the party across the world.

## 2026-11-13 Party priest stuck at 66/325 SP: 256 Red Potions, 85% weight, no weapon
- Symptom: Zendancer84 (acolyte lv42) "resting for SP" for many minutes at 66/325 SP; weight 20600/24300 (85%);
  no weapon equipped (an unusable starter Knife in the bag). Zen.fox (mage) 52%, Echobunny carried 205 Red Potions.
- Cause 1: the Tool Dealer buy step bought 30-60 Red Potions on EVERY trip, whatever the bot already carried, with no
  weight check. rAthena stops natural HP/SP regen at >= 50% weight, so sitting never restored anything.
- Cause 2: weapon/armor trips into payon_in01 travelled to the map's nearest door (the Tool Dealer room), entered the
  wrong room 3 times and gave up ("its room can't be reached"). Payon gear was never bought; the acolyte stayed weaponless.
- Fix (town.go, shared engine): per-class potion targets (acolyte line 8 HP pots, others 30-60) plus SP potions for SP
  users (tuning pot_sp_target), buys capped so the bag stays under pot_weight_cap_pct (45%); surplus heavy HP potions
  are sold (consumables only - cards/gear/materials still kept); needTrip calls a sell run at weight_regen_pct (50%)
  when there is anything to sell; a member over 50% inside a town with a shop may sell there on its own; priests over
  50% don't sit for SP; in-town shop trips route to the shop's ROOM (RouteMapsPoint); a missing weapon is the first
  gear pick (bestGearIn / nextGearBudget); equally far gear shops prefer the outdoor town shop.
- Measured: Zendancer84 84% -> 13% (sold 248, kept 8), SP 66 -> 258 within ~3 min; Zen.fox 52% -> 24%; Echobunny sold
  164; Echobunny bought a Jacket in payon_in01 13 s after the room fix (it had given up there before); on the next
  party run Zendancer84 bought a Rod (weapon-first pick) in payon_in01 and wears it.
- Known gap: no NPC in this world sells cheap SP potions (Grape Juice, Blue Potion are not in any shop; only Blue Syrup
  at 7,000z from Advanced Potion Merchants). Bots buy SP potions only once they can afford Blue Syrup.

## 2026-11-14 Party rally at the destination's Kafra (whole Legion) + Ayla's back-and-forth
- Rule: parties regroup at the Kafra nearest the destination and save there before setting out (legion/rally.go,
  partylead.pl start_rally/rally_tick). Fixed party-chat wording "meet at the Kafra in <town> (x,y)" is the protocol
  between the bots and Ayla's plugin; both sides adopt the other's call. Verified live: Ayla saved at the Payon Kafra
  ("Your Respawn Point has been saved here in the city of Payon"), friends saved via kafraTrip{Save}.
- Ayla ended a rally "everyone is at the Kafra" while Echobunny was 120 cells away: OpenKore's party-list positions go
  stale. Presence checks use what she SEES ($playersList), never the party list alone.
- After a rally she ran ahead alone into pay_dun00: she now leaves the town only while every friend is within 15
  cells (depart_tick), else stops and waits.
- Bots stood still for minutes "travel -> pay_dun00" / "regrouping" while SEATED (rally wait / SP rest): the server
  ignores walking while sitting. travel() and the regroup branch stand up first; a generic stuck watchdog
  (stuckWatch) closes NPC dialog/trade/chat, stands, drops route/path and steps to a free cell after 20 s.
- Back-and-forth (owner: "do not go back and forth"; "together within 20 steps"): one threshold, together_cells=20
  (tuning.json): member regroup, bot-leader collect, rally radius, dashboard with_leader. Ayla waits in place for a
  friend who is merely behind; walks back only for one that asked for help, is below 50% HP or hasn't moved 30 s,
  and finishes that before anything else. Her "never stand still" random step is off while she waits.
- 90% farming: the human merely walking through town (or waiting at the Kafra) no longer calls a party town run
  (only inside a shop building); friends on their own town trip never trigger a rally or "where r u" calls; Ayla
  waits in town only after her own sell/buy errand; rally cooldown 20 min after a completed one.

## 2026-11-14 Casters never upgraded; weaponless acolytes; gear progression (gear agent)
- Symptom 1: Zen.fox (Mage lv44, 2,883z) kept a Knife although Payon sells Rods. Cause: itemScore = Atk*2 + Def*3 for
  every class - a Rod (ATK 15, MATK 30) scored below a Knife (ATK 17). Fix: `b.itemScore` scores by build
  (builds.go gearStyle): casters + support priest = MATK*3 (+ATK/2), archer line = bows (+50), melee = ATK*2 (staves
  only their ATK); the item DB loader now reads MagicAttack and SubType. The support-priest weapon route is now
  Rod > Wand > Staff > Arc Wand (MATK feeds Heal in Renewal; Rods are sold in almost every town).
- Symptom 2: ThePixelstorm Heals "heading to izlude (buying Mace)" 12 times in an hour, never bought. Causes: (a) the
  AI loop starts at login before the item lists arrive - with an empty bag the bot saw "no weapon, no potions"
  (= urgent) and left 0.4 s after every login, and every trip was cut short by the next restart (main Legion was
  redeployed many times that hour); (b) the event named the potion town, not the shop (the Mace is sold in
  Prontera's market / prt_church, never Izlude); (c) a gear trip that reached the shop and didn't buy set no
  gearFailed, so it could repeat. Fix: `invReady()` (inventory received or 20 s) gates needTrip/nextGear/bestGearIn/
  econWant; the event names the shop's town; a failed purchase marks the item failed for the session and logs
  "could not buy X here (listed, price, zeny)".
- Symptom 3 (post-deploy check): 25 of 30 main acolytes (lv5-19) still had no weapon. Causes: gear trips start at
  min_gear_level 10, and in a party gear is never a "real need", so the weapon-first pick never ran. Fix:
  `weaponNeed()` - NO usable weapon is a necessary trip at any level (owner: trips only when necessary): needTrip
  returns "no weapon", it is urgent (a party member may go alone, it's one cheap buy at the nearest shop), and the
  trip logs "buying Rod (necessary: no usable weapon)". Picks the best cheap weapon it can use (plan route, then Rod/
  Club/Knife/Sword/Bow/Axe) at a shop within max_gear_hops.
- Tie-break bug: equally far shops preferred an outdoor shop, so a Payon Rod was routed to Prontera by Kafra (a
  wasted fee). Now a shop in the bot's own nearest town wins a tie (betterTie).
- New: gear progression (gearpool.go). Each bot's next step is on /api/bots/<name> "gear_next" and the party
  dashboard; parties hold a gear TURN (no weapon first, then longest since last gear, then weakest per level), party
  mates on the same map fund the turn member through a zeny-only trade (market Deal with ItemID 0), and a real
  upgrade sold only far away makes the party's NEXT real-need town run go to that town (one planned trip, cooldown
  gear_trip_cooldown_min; never a run called for gear alone). Solo bots from gear_trip_min_lv may plan one far Kafra
  trip (maxGearHops no longer blocks it). Ayla's party: members only ask in party chat; Ayla decides.
- QA (main legion): bot leaders looped prontera <-> prt_in on "rally: going to the Kafra". FindPath exempted the whole
  warp box next to the start cell, so a bot that arrived beside a door walked back through it. Only the ring is exempt
  now, and a rally with no progress for 2 min is abandoned. Bot leader and member also chased each other while
  "collecting" at 3-17 cells: collect only beyond 20, and the collected member stands still.

## 2026-11-14 - walking across a warp (Alberta Novices stuck "travel -> pay_fild03")
- Symptom: ~50 level-1 Alberta Novices bounced alberta 102,153 <-> alberta_in 185,89 every ~5 s, forever.
- Cause: rAthena warps fire on every cell of their area (X±xs, Y±ys; `alb09` at alberta 98,153 is 2,2 = 5x5), and the
  server warps a bot that merely walks across it. Legion parsed the warp but dropped the span, and A* (FindPath)
  ignored warps entirely, so the path from the alberta_in exit (102,153) west to pay_fild03 went straight through the
  `alb09` area. The warp-loop detector couldn't help: it blacklists the *planned* warp (the alberta_in exit, the only
  way out), never the one actually stepped on.
- Fix (nav.go/data.go, all maps): Warp keeps XS/YS; per-map lazy warp mask; FindPath never enters a warp trigger area
  except the one it heads for (or stands in), and pays +40 for the 1-cell ring around warps (the server re-paths each
  short segment itself); LineWalkable (roaming) refuses warp cells. `legion route` now prints "N on other warps" for
  the path to the first portal (0 for alberta/prontera/payon/morocc samples).

## 2026-11-14 Town trips "way too often", ~20% of bot time farming (owner: "90% of the time should be on farming")
- Measured (main legion, ~830 bots, 30 s samples over ~20 min after a deploy): farming ~19%, travel ~48%, roaming
  ~11%, Kafra/rally ~12%, town trips 3-17% (more at higher levels). Town-trip starts ~4.8 per bot-hour, 93% of them
  "buying <gear>" with a mostly empty bag, plus "selling loot for <gear>"; deaths 5.3/min, 58% on gef_fild04
  (a lv1 Novice and a lv7 Thief in one party died 18 + 12 times).
- Causes: needTrip started a trip whenever an affordable upgrade existed ("gear"), whenever loot would pay for one,
  and at 50% weight when ANY junk item was in the bag; the only spacing was a 3-min retry. Roaming shuffled 5-12 cells
  and gave a map up after 45 s (map hopping = travel). Party/guide/hunt-spot pickers accepted maps whose monsters
  were all above the bots' own target filter; party leaders never gave up a dead map; a death loop never moved anyone.
- Fix (shared engine): needTrip = real needs only (bag >= trip_bag_pct 70% by weight or slots WITH something to sell,
  out of potions, no usable weapon; over-regen selling only while already in a town with a shop). At most one
  non-urgent trip per trip_min_interval_min (45). Gear rides along: after the Tool Dealer the trip continues to the
  plan's next upgrade sold in the same town (tripGear). Every trip start logs "[weight %, slots, pots, sp, zeny]".
  Roaming sweeps the map (explore: far cells of the same region), give-up 120 s. fitShare: >= 50% of a map's monsters
  must pass the (weakest) hunter's filter in SpotsFor, guideSpotsFor, bestPartySpot and formParty (partyMapFor);
  a bot party leader relocates a dead map (partySpotNear + rally); 2 deaths on one map in 10 min -> solo drops the
  map 30 min, party relocates, a member 10+ levels under the party leaves it; low-level bots flee at 25% HP and
  carry 5 Fly Wings.
- Guard: tools/sentinel.py flags farming share < 85%, > 1 town trip per bot in 45 min, and trips with bag < 70%
  and pots > 5 (unnecessary), naming the offenders and reasons.

## 2026-11-14 Economy audit: trades work, but nothing to trade and no money
- Seen: the trade window protocol works (2 real bot-to-bot trades, 0 failed deals in the first hour after deploy), but
  95% of the 130 open wants were "cheaper than NPC" asks for NPC gear (Sandals, Hood, Hat) that nobody held spare,
  while offers were cards and starter Knives nobody wanted. 0 vending shops ever opened: 42 of 43 vendor Merchants
  had spent their points on Mammonite before Vending (plan order), and no bot could buy from a shop anyway.
- Seen: bot zeny only goes down: 13.1M total vs ~13.5M seeded; lv 30-44 bots average ~800z (max 2,999 = seed
  money). EXP is 10x, drops 1x: characters out-level their loot income 10 to 1.
- Engine now: vendor characters learn Enlarge Weight 5 -> Pushcart 3 -> Vending 3 first (builds.go vendorFirst);
  shops price at the economy standard (vend.go vendPrice) and list gear/potions too; bots buy what they want from open
  shops at or below their bid (vendbuy.go, 0x0130 -> 0x0b3d -> 0x0801); vendor sales are counted in /api/economy.
- Engine now: each guild has a designated alchemist (alchemy.go, /api/alchemists, legionctl alchemists): Potion
  Research 5 -> Pharmacy 5 first as Alchemist, buys the guide/bowls in alde_alche, bottles + Scell at the south
  gate, brews Blue/White/Yellow Potions, gives a few to its guild leader's party (zero-zeny trade), sells to guild
  mates at 70% and to others at market price; herbs are offered to alchemists, never NPC-sold.
- Open: drop rate vs EXP rate is an owner decision (rathena battle_conf item_rate_*; needs a rathena restart).

## 2026-11-14 routing / inbox / guild leaders (subagent)
- **Portals on blocked centre cells**: FindPath to an unwalkable goal returns nil; `travel` walked to `w.X,w.Y`, so for
  field-edge portals (moc_fild02 67,342, prt_fild08 170,378, ...) bots only nudged randomly in a ~10-cell box for
  7-9 minutes, dodging stuckWatch. Walk to a walkable cell of the trigger area (`nav.go portalCell`, also inside
  `stepAlong`). Check new nav code with `legion route <map> <x> <y> <toMap> [lv]` (prints the walk-target path length).
- **Warp-loop detector banned random portals**: it marked `b.lastWarp` (last route portal, often minutes old and on
  another map) bad server-wide for 30 min - prontera's west gate, geffen 26,119... - which broke routing for everyone
  ("no route", detours, more loops). Identify the portal actually taken from the arrival cell (`warpTaken`); ban only
  our own route portal; and routeMaps falls back to bad portals when they are the only way on (izlude_in tool shop).
- **The server walks its own path between our waypoints**: avoiding warp cells in A* isn't enough; the segment we send
  must not span a warp trigger cell (`boxHitsWarp`), else alberta 102,153 -> west crosses the 5x5 door at 98,153.
- **No-progress watchdog**: stuckWatch only catches "not moving at all"; travel now re-plans after 60 s without
  getting closer to the portal, then wings home / bans that portal (`travelStalled`).
- **Vending with an empty cart froze merchants forever**: the server sets prevend + WIP_DISABLE_ALL on MC_VENDING; not
  answering 0x012d leaves the char unable to walk. Always answer 0x01b2 (flag 0 = cancel).
- **NPC talk uses ZC_NOTIFY_CHAT 0x008d and ZC_NPC_CHAT 0x02c1 (colored npctalk; channels have id 0)**: "Resting Adventurer" (prt_fild08 tip NPC, id 110012270) showed as a
  HUMAN in the minds' inbox. Ids >= 100000000 are objects, not accounts -> event kind "npc".
- **Inbox re-delivery**: each call de-duplicated only its own window, so a line heard by more bots later came back
  every cycle. /api/inbox now skips a line already heard before `since` (same speaker+text within 3 min).
- **One guild leader**: the engine (data/guild_leaders.json, in-game master overrides) is the only source; the
  legion-guild skill used to say "the leader is the highest-level member online", so minds picked others.
- **Healing lived in one branch most bots never reached** (QA 2026-11-14): the only party heal was in partyThink's
  support branch (<70% HP, 9 cells), so acolytes in a rally, town trip, travel, a fight, or leading the party (leaders
  run hunt(), not partyThink) watched mates at 61%, 5%, 3% HP with full SP. `heal.go:healFirst` now runs at the top
  of think() for every acolyte-line bot: heal the most hurt visible party mate within 9 cells under 80% (self under
  60%), stand up to cast, drink SP when short. Skill plans: the monk plan put DP 10 + Demon Bane 10 before Heal, so a
  monk-plan acolyte (the party's support) had no Heal - acolyte plans now learn Heal first (priest: Heal 5).
- **Nobody walks on at very low HP**: `heal.go:lowHPGuard` - under 25% a bot stops (rally, travel, regroup, trip),
  flees/drinks if hit, else drinks or sits until 60% (Tacorose walked a rally at 3/109 HP).
- **Buffs must not drive the SP rest loop** (Ayla's party stood 5+ min, 0 kills): buffs renewed on a fixed
  220-260 s timer whatever their real length, three back to back at full SP, then "sp low, sitting" to 95%, repeat.
  Now: renew ~20 s before the real duration (40+20*lv s), one buff per 10 s, only if SP stays >= 70% after it, never
  while seated; SP rest ends at 70% once the leader moves off or fights (95% only while the party idles).
- 2026-11-14 ROOT CAUSE of "swordman standing still, not attacking" (reported many times, every heuristic fix failed):
  rAthena never walks a PLAYER to its attack target. unit_attack_timer_sub answers an out-of-reach attack with
  0x0139 (ZC_ATTACK_FAILURE_FOR_DISTANCE) and drops the attack; only monsters chase. The bots sent 0x0437 once and
  waited for the server to close in, and nothing handled 0x0139, so any target 2+ cells away (or one that stepped
  off) was never hit: the bot stood still "tanking/fighting X" until the ghost-target watchdog dropped it.
  Evidence (combat trace, LEGION_TRACE / /api/trace, trace.go): Echobunny lv44 tank, 74 s on pay_dun01: 21
  auto-attacks sent, 19 answered 0139 TOO FAR (d 4-14), 0 auto-attack hits, 2 Bash hits. Main legion showed the
  same (Zero fox at one cell for 60 s vs Porings td 5-12; xXShadowfoxXx6 standing at td 8-12 from a Mandragora
  while it shot him). Fix (ai.go closeIn, bot.go 0x0139/0x013a): fight() walks into reach (server attack range
  from 0x013a, rAthena client distance, target's walk destination, re-aim every 400 ms) and only then sends the
  attack; a 0x0139 voids the sent attack and takes the server's positions. Same for the party bait hit.
  After (418 s): 48 sends, 9 refusals, 94 auto-attack hits + 16 skill hits, 17 kills (vs 2 in 74 s), damage
  1787/min (vs 1159), longest stretch with the target in reach and no hit: 1 s. Lesson: when a bot "doesn't
  attack", trace the packets (trace.go) before adding heuristics - the server says exactly why.
- **Via-Kafra ping-pong**: kafraVia priced trips with Price+200 but kafraPlan demands Price+KafraKeep (more for
  beginners), so a bot walked field -> town for the Kafra, was refused, walked out, and decided again (ryuknight20
  pay_fild01 <-> payon every 4-5 s). Same money rule in both; an arrival without a Kafra trip sets kafraNext +5 min.
- **No random walk on "no path"**: stepAlong nudged randomly when FindPath failed, hiding the blocked-portal bug for
  weeks; now it logs "no path on <map> ..." (1/min) and travelStalled escalates (re-plan, wing, other portal).
- **Wrong builds (2026-11-14, owner: "wrong skills or build = delete and start over")**: audit tool
  `tools/wrongbuilds.py audit` (writes `legion/data/wrong_builds.json`) compares every bot char's skills/stats with the
  plan builds.go would follow. Distinguish *irreversible* deviations (skill above the plan's level for it, a stat the
  plan won't reach soon, vendor without Vending after 11+ merchant points, support acolyte without Heal) from
  *order* differences: the planner always raises the first unmet plan goal, so a monk with Divine Protection 8 and no
  Heal (the plan wants DP 10 too) or a knight with AGI 20 before STR 20 converges by itself; 182 such chars were
  left alone, most came from plan edits (monk Heal-first, knight/crusader first-stat swaps), not bugs. Only 14 were
  really wrong (vendor Merchants: Mammonite 3-9, no Vending); 11 offline ones were restarted at Novice 1 with
  `tools/wrongbuilds.py reset` (goods to account storage, backup in server/backups/). Root cause in the planner:
  progress.go marked the requested skill non-upgradable until the server confirmed; a skill-up the server ignored
  (stale skill-point count) blocked that plan skill for the whole session, so later points went to later plan
  entries / the fallback list (Mammonite first for merchants). Now one request per 3 s and the flag stays.
  Lesson: before deleting progress, check whether the planner would fix the deviation on its own; never edit a
  char (or its account storage) while any char of that account is online.

## 2026-11-14 Economy live check: shops of Jellopy, cards nobody could afford
- Seen (00:43, 12 min after the deploy with vendor-first skills): 19 shops open at the south gate, all listing NPC junk
  (Jellopy 3z, Shell 9z, Clover 6z): 0 sales. Card asks at 20-100k while bots hold 1-3k: 0 card trades. Slotted
  drops (Knife [4], Sword [4]) priced 3-4k by the rarity formula next to the NPC's 50-100z namesake. Half the party
  zeny gifts failed: the giver walked to the requester by name, but b.ents often has no name for it.
- Rule: price the standard from what the players hold (cards 500 + mob lv x 250), and from the NPC namesake for
  slotted variants; give found items a use (card compounding into worn gear) so supply meets demand; shops stock
  what players buy, NPC junk goes to the NPC. A market partner's trade request is accepted at once (it only asks
  from 2 cells).
- Zeny: loot is the only income at 1x drops (5-40z per kill on lv1-30 fields, mob_db). Every drop is now looted, and
  broke bots choose the guide map with the best loot per kill.
- Follow-up (same day, after deploy): loops moved to the next gates (alberta <-> pay_fild03, geffen <-> gef_fild04: 508
  in 8 min). Landing cells sit 1-2 cells from the return portal's area, and walks that bypass FindPath (closeIn chasing
  a monster, looting, following, fleeing) walked straight into it - monsters and drops inside a portal area are
  common. Fix: bot.go walkTo -> nav.go safeWalkDest guards EVERY walk: no destination in / box across a trigger area
  unless it is b.lastWarp on this map (the portal we mean to take) or the area we stand in; such a destination moves
  to the nearest safe cell, a clipped box follows the A* path prefix. Targets and drops on warp cells are skipped
  (ai.go hunt). The loop detector bans a warp only if BOTH legs were route portals (it banned the good warp
  alberta 15,234 / geffen 119,213 when the way back was an accident).

## 2026-11-14 - "bought a Club and never equipped it" = looted, UNIDENTIFIED Clubs
- Symptom (QA): acolytes with Club/Rod in the bag, nothing worn, buying again (Thorn.rose [1501,1501], Holy_bunny).
- Cause (DB proof: inventory.identify): Thorn.rose's two Clubs were monster drops with identify=0; legion-wide 25
  unidentified Clubs on acolytes, 0 worn, while 21/22 identified Clubs, 32/32 Morning Stars and 49/49 Rods were worn.
  rAthena refuses to equip unidentified items (ITEM_EQUIP_ACK_FAIL). The engine didn't parse the identify flag, so:
  weaponNeed counted the Club as a usable weapon, equipFromInventory tried it, and the refusal set gearFailed[1501]
  for the session - which then removed Clubs from the SHOPPING list too, so the monk-planned Thorn.rose left for a
  Rod in Geffen with two Clubs in the bag. Bought gear always equipped fine (0x0998/0x0999 are correct).
- Also: 0x0b41 items had Loc 0 in /api/bots (QA filtered them out and called an equipped Rod "vanished"); invSeen was
  set on the normal item list, before the equipment list arrived, so a bot could decide "no weapon" at login; 0x0b39
  cards were read 1 byte off (2021 layout: cards at +15, flags at +67); an equip timeout blacklisted the item ID.
- Fix (identify.go, bot.go, builds.go, progress.go, town.go): parse identified (0x0b39 flag bit, 0x0b41 byte 10) and
  Loc; wearable() = identified && not refused && canEquip; a refusal marks only that item (NoEquip), the ID only if it
  was identified; invSeen on 0x0b0b; weapon-hand first, and a bot with no weapon equips even mid-fight; a Magnifier
  (bought on normal Tool Dealer runs when the bag holds an unidentified upgrade) identifies it (0x0177/0x0178/0x0179).
- Rule: before blaming the equip code, check the server's truth (DB inventory: identify, equip). A failure on one item
  must never blacklist a whole item ID for buying.
- Measured 00:49 -> 00:57 (783 bots online in both snapshots): the bots gained 2.7-4.5 base levels in 8 minutes, and zeny
  moved -13k in total (-17z per bot; ~80% of bots had no zeny change at all). Money doesn't drain fast; characters
  out-level it (10x EXP, 1x drops). First bot-to-bot item trade after the batch: a Club for a bot with no weapon
  ("no weapon" want, 170z).

## 2026-11-14 - class weapons are a necessary buy; gifts only the shortfall; no unidentified gear in trades
- QA scan (main legion): ALL online Mages (123) and Archers (97) held the starter Knife; no Rod/Bow even in the bag,
  though most could pay (a Rod is 50z). The Knife counted as "a usable weapon", so weaponNeed never fired, and gear
  only rode along on rare real-need trips whose town had to sell it. Acolytes: 112/144 had a mace/rod.
  Fix (builds.go `classWeapons` table + `classWeaponNeed`, town.go needTrip/startTrip, ai.go urgent): holding a
  weapon outside the family's class list (Archer: Bow/Musical/Whip; Mage: Staff/2hStaff/Book; Acolyte: Mace/Staff/
  Knuckle/Book) is a necessary trip, alone if need be: the CHEAPEST proper one at the nearest shop (or by Kafra when
  the fees fit), logged "buying Bow (necessary: class weapon)" - the sentinel exempts "necessary". Upgrades after
  that stay on real-need trips (tripGear). A solo bot already standing in the town whose shop sells its next
  affordable step buys it there ("necessary: in town already") - no trip away from the hunting ground.
- Party gear turn overfunded: need was price + the FULL level reserve - zeny, while the buy itself uses
  gearReserve = min(base + lv*per, zeny/div); a member who could already buy a 50z Rod got 400z. Now
  `giftShortfall` uses the buy's own rule and the gift is the gap rounded to 10z.
- Trades: TheYukisage Bless bought two looted (unidentified) Clubs and the server refused both. A buyer now cancels
  when the partner's 0x0b42/0x0a09 shows an unidentified equipment item (byte 11 = 0); 0x0b41 on a stale bag index
  takes the new item's id/location/identify flag instead of keeping the old entry's.

## 2026-11-14 Routing round 2: shop counters, rivers, doors, walls (built, deploy pending)
- Baseline (06:02-06:08, 6.2 min): 65 "no path on" (38 alberta_in shop trips, 24 prt_fild04 regroups), 173 "stuck at".
- **Shop counters**: tripThink walked to "NPC x+1, y-1", often a wall or the far side of the counter (izlude_in 61,126,
  alberta_in 189,20, geffen_in 31,177) -> FindPath nil every minute. Now nav.go talkCell: the walkable cell of OUR
  walk region nearest the NPC (<= 6 cells; rAthena talks from AREA_SIZE+1 with no line-of-sight check); none = wrong
  room (route round, give up after 2 rooms); 3 failed searches to the counter = give up and log.
- **Door walks**: safeWalkDest only lets a walk into a trigger area for b.lastWarp, and the shop-trip / escapeNoRoute
  code set lastWarp only within 1 cell of the centre -> any path that entered the area 2 cells out was cut back to
  the outside: Thorn.rose stood beside geffen door 98,141 for 5+ min (15 stuckWatch fires), same at izlude 112,179,
  alberta 114,41, payon 138,156. Rule: set lastWarp BEFORE walking to a portal (nav.go walkToPortal) - every caller.
- **Rivers**: prt_fild04 is two walk regions (river); the leader stood on the other side, members ran FindPath to it
  forever. regroupStep: other region -> RouteMapsPoint out and back in (here via prt_fild05); blocked leader cell ->
  nearest walkable cell; 2 failures or no way round -> wait 30 s and ask in party chat. Leaders no longer pick a
  member on the other side to collect. 0x0091 used to clear b.route on every map change, so travel re-planned with
  RouteMaps (any region) and could undo such detours; the rest of the route is now kept when we came through its
  first portal.
- **Walled in**: the seed placed Novices at "town spawn +-3" with no walkability check: 227 Legion chars on blocked
  cells, 17 walled in (geffen 116,56 is inside the fountain wall; Silver_storm & co. "travel -> gef_fild07" 11+ min,
  stuckWatch 33 tries). Seed now picks a walkable cell of the spawn's region; a walled-in bot wings out (stepAlong).
- **npc 110000000** in every stuck report is rAthena's fake NPC that runs item scripts (the first NPC id): a dialog
  nobody answers keeps sd->npc_id set, and pc_cant_act() then refuses walking AND item use (Silver_storm's two
  Butterfly Wings were not consumed). Dialogs no feature drives are now clicked through (next/cancel/close).
- Lesson: when a target "has no path", print the grid (walkable + region) around it before touching the A*; the map
  tells you whether it is a wall, a counter, a river or a pocket.
- Follow-up (01:12, live): the class-weapon trip broke "town runs together": Zen.fox left Ayla's party in the cave
  ("brb going to payon to buy Rod (necessary: class weapon)") and a gear trade pulled Echobunny to Payon. Now in a
  party a missing weapon (none at all, or no class weapon) is a PARTY town-run request (callTownRun, to a town that
  sells it - ai.go weaponRunTown), never a solo trip; a human-led party member asks in party chat once per 30 min
  and buys on the leader's next run (bestGearIn / nextGearBudget rank the class weapon first). Bare hands skip the
  45-min interval; the class weapon doesn't. Gifts and trades of a party member happen only with a partner already
  within 10 cells on the same map (economy.go partyTradeOK, vend buys, gearpool donors): no trade walks anyone off.

## 2026-11-14 - survival batch: one survive() check in every state; deaths with potions and Fly Wings unused
- QA death probe (main legion 06:01-06:06 UTC, 26 deaths): lord86 (Acolyte lv7, no weapon) "defending vs Willow" went
  88% -> 66% -> 42% -> dead in 12 s with 11 potions + Fly Wings; Novices lv3-5 died with 5-9 Fly Wings; Lily 07
  (Merchant lv21) "teleported away (a level 47 ...)" at 0% HP and died; LilSkyz died on moc_fild02 and respawned in Alberta.
- Causes: potions were drunk only in hunt(); party paths ("defending", "fighting off X on the way", regroup, assist,
  tank) only called escape(), and lowHPGuard drinks only under 25%. escape() fired at 20-35% HP: one or two big hits
  skip that band entirely (also against a monster 26 levels higher). The save point was set only when a route happened
  to pass through the hunting map's nearest town. Escapes counted as full deaths in recentDeaths (20-min half-life), so a
  few Fly Wings in pay_dun01 held Ayla's Echobunny / Zen.fox at the Payon checkpoint, where they fell through to hunt()
  and showed "roaming". "pots 139 / restocked 133" on Vex_kun was NOT a buy bug: 109 starter Novice Potions (569) + 16
  Light potions (11500) + 8 Red (= PotHPAcolyte); potionCount counts the starter kit, buying stays capped.
- Fix: danger.go survive() at the top of think() (after the Dead check, before healFirst) for every bot: HP history
  (noteHP/hpDrop), a potion under potLine (0.5; 0.6 with >30 pots; 0.3 for a healer with SP; 0.7 while HP falls 20%+
  in 4 s), then escape(); a bot running on foot keeps running while hit (fleeUntil). escape's decision is the pure
  losingFight() (danger_test.go replays the probe cases): + HP lost >=20% in 4 s under 70% (45% for a healer with SP),
  2+ attackers at <=30%, a monster >8 levels above at <90% HP, a weak fighter (no weapon / caster on a Knife) vs a
  monster at its level under 60% (35% with Heal ready); Butterfly Wing under 30% when out of Fly Wings.
  kafra.go saveDetour: a solo hunter whose map's nearest Kafra town isn't its save point walks there once per map
  (1-2 map changes away), saves, and walks back; a respawn town now updates savedTown. Close calls add 0.25 to a map's
  danger; the human-party checkpoint hold uses real deaths in a 10-minute window (>=5) and the member waits explicitly
  ("waiting in payon: pay_dun01 too dangerous") and says once in party chat why ("died twice in there, waiting in payon").
  api.go /api/inbox uses allChars (all profiles): core bots' lines were tagged HUMAN in legionctl.
- Rule: every survival check (potion, wing, run) lives in ONE function called before any state logic; a state path
  that can take damage must never be the place that decides survival. Escape lines must consider damage RATE, not
  only the HP level.
- 2026-11-14 legion crash (all ~785 bots logged out): `panic: index out of range [0]` in travel() <- saveDetour() <-
  think(). RouteMaps/RouteMapsPoint return an EMPTY non-nil slice when the destination is the current map, so a
  `b.route == nil` check is not enough: travel() indexed b.route[0]. Fix: `len(b.route) == 0` guard before indexing
  (ai.go travel). Safety net (safety.go): aiLoop think/keepalive and readLoop handle run through b.safely(), which
  recovers a panic, logs it with the bot name + stack (rate-limited), resets that bot's route/path/trip/kafra/via/Goal
  and carries on. Rule: check slices with len(), never with nil; one bot's bug must never kill the service.
- Follow-up (01:30): the walled-in Novices "winged" 23 times with 0 wings consumed. Reasons: rAthena refuses item use while
  sd->npc_id is set, and pc_setpos keeps a blocked login position (it only re-rolls 0,0 / out-of-bounds coordinates),
  so a relog alone doesn't help. nav.go unwall(): close the dialog, stand, Fly Wing (never Butterfly); otherwise log
  out for 3 min while tools/unwall.py (`--loop`) moves the OFFLINE row (lg* accounts only) to a walkable cell; 207
  offline rows fixed. Second freeze: **sd->npc_shopid** is set when a shop's buy/sell window opens and is cleared only by
  a buy/sell list or 0x09d4. With nothing to buy or sell the bot sent nothing and couldn't walk (wolfdancer, alberta_in
  180,94). Rule: every opened NPC window is closed explicitly (0x09d4 for shops, 0x0146 / 0x00b8 255 for dialogs).

## 2026-11-14 - death regression 6 -> 15-19/min: level-only map choice for bots with starter gear
- Measured: before the 01:12 deploy ~6 deaths/min; after 01:12 16-21/min; after the 01:35 full survival batch still
  136 deaths in 7.2 min (~19/min, 06:38-06:45Z). Top maps pay_dun00, moc_fild03/01/02, prt_fild07, mjolnir_02/06.
  QA probe: the population levelled into lv20-46 (10x EXP) while the gear stayed Knife + Cotton Shirt (Swordman lv40,
  Thief lv46, Merchant lv37); the level-only picker sent them to Wolf packs / Orcs / Mjolnir; they died with 5-23 Fly
  Wings and 15-54 pots. So survival thresholds were the SECOND factor; map choice was the first.
- Fix (gearpower.go): powerLv() = effective level from base level + weapon power ratio (ATK, MATK for casters, vs the
  NPC weapon of the level) + body-armor DEF ratio; Knife + Cotton Shirt at lv40 fights like a lv24-25, at lv25 like a
  lv17; no penalty under lv12. Used by targetMaxLv (so fitShare filters follow), goodHere / chooseHuntingGround /
  guideSpotsFor / SpotsFor, routes (mapDanger level), losingFight's "outmatched", partyMapFor (pubState.Power),
  partySpotNear, bestPartySpot. The "too dangerous -> easier map" escape now bans the map 30 min (badMap) too.
- Guild-leader parties (bestPartySpot): reachable (<= 8 hops from the leader), hops cost score, beginners within 3
  maps of home, and the pick is kept 20 min while it still fits (it was re-picked every leaderPartyManager minute and
  parties bounced between Prontera/Payon/Morroc).
- Kafra: the "Orc Dungeon" teleport lands on gef_fild10 (Orc Village, lv40-56 aggro). kafraLandingOK skips a field
  landing the bot can't handle (mapTooStrong vs targetMaxLv, or recent deaths) unless it is the hunting map itself.
- saveDetour: only at HP >= 70% with potions (or Heal), never across a map too strong for us; aborts the walk under 50%.
- Trades: a trade window freezes walking and item use (pc_cant_act). tradeGuard at the very top of think cancels an
  open window (0x00ed) when hit or under 70% HP, refuses a pending request (0x00e6 4); requests are refused, deals and
  gifts are not started, and the seller does not send 0x00e4 unless tradeSafe (no hits for 8 s, HP >= 70%).
- Gear: classWeapons now covers Swordman (sword/spear), Merchant (axe/mace/sword), Thief (dagger...). With the class
  weapon in hand, gearStepNeed makes a weapon step (power ratio < 0.45 from lv20 -> best affordable NPC weapon lifting
  it by 0.2+) or a body-armor step (DEF ratio < 0.5) a NECESSARY buy through the class-weapon path (solo trip, party
  town run, human party asks), within gearReserve; bestGearIn buys it first on a party run.
- lowHPGuard: before travelling / in town recover from under 45% (was 25%); after 90 s of rest drink if >10 pots; the
  party line asks for a heal only when a party healer is on the same map, once per 10 min (Thornlord asked from Prontera).
- Rule: every "where do I go" decision rates EFFECTIVE power (level AND gear), never level alone; a fast-levelling
  population outruns its gear, and the gear must become a necessary buy before the maps get harder.
- Follow-up (01:50, QA probe 01:44): 17 of 18 bots that went under 35% on death maps died; 10 on moc_fild03 (Argos lv47,
  Steel Chonchon lv48, aggressive). A Fly Wing lands on the SAME map (mochi_rose winged at 30%, dead 4 s later). Now:
  fitShare returns 0 when a common aggressive monster sits > targetMaxLv+4 (>= 3 spawns or 5%), which vetoes the map
  in every picker; escape uses a Butterfly Wing (save town) against a monster 10+ over powerLv or when a Fly Wing
  already failed within 20 s. Tanks keep 25% SP (Provoke / Bash / Magnum Break): Echobunny sat at 11/200 SP.
- Follow-up (01:55): one death to a monster 8+ levels over powerLv now bans that map for the bot for 30 min (bot.go
  death handler; TheLilychan died on gef_fild11 to an Orc Lady and walked straight back - the died-twice guard needs
  two). Second jobs share their first job's family (progress.go familyOf), so the class-weapon / gear-step rules cover
  Knights, Rogues, Blacksmiths etc.
- Ayla gear plan (2026-11-14): she levelled an Assassin to 64 with a Knife and Cotton Shirt - nothing in her OpenKore
  config ever bought gear. Fixed with OpenKore's own buyAuto/equipAuto: gear slots WITHOUT minAmount are bought only
  inside a real sell/restock run (AI.pm shouldStartAutoBuy needs minAmount to start a trip), maxAmount 1 counts the
  equipped item, so no loop. A Prontera-only item rides on the next real-need run (partylead.pl gear_run_start swaps
  the shop NPCs to their pronteraNpc twins for that one run). OpenKore's default items_control "worthless gears" sold
  Thief Clothes / Chain Mail / Adventurer's Suit to NPCs - switched to keep (rule: weapons/armor never go to an NPC).

## 2026-11-14 Two rules that silently killed the market
- A safety rule added elsewhere (partyTradeOK: party members trade only within 10 cells) but not applied inside the
  matcher made it pick the same impossible pair every 15 s: 0 trades for 30 min, no error anywhere. Rule: every gate
  that startDeal applies must also filter the matcher's candidates, and every refusal is counted (match_misses).
- In-memory picks reset on every deploy: the guild alchemist was re-chosen after a restart, so the guild's zeny for
  the 100k Potion Creation Guide went to different characters. Anything that accumulates value must be persisted.
- Follow-up (02:25): the necessary gear step broke "90% farming": at 02:14 fighting/looting 27%, travel/town 31%,
  kills/min 1,093 (vs 1,750+); 45 Adventurer's Suit + 35 Katana + 12 Falchion trips, several per bot, the whole Legion
  leaving in the same minute after login. Now only the WEAPON is a necessary step (class weapon, or ratio < 0.45);
  armor/accessories ride along: after a gear buy the trip chains up to 2 more affordable steps sold in the same town.
  gearStepReady: a random 0-15 min delay after the session starts and one weapon step (trip, party run call or party
  ask) per 60 min; no weapon at all is never gated. Trip lines say why: "(necessary: weapon far below level: Knife at
  lv43)", "(on the party town run)". The 150-200 "pots" are the starter kit (569 + 11500), counted by potionCount;
  buying stays capped at hpPotTarget (30-60). Rule: a "necessary" buy must be rate-limited and spread over time - a
  rule that fires for a whole population at once is a stampede.

## 2026-11-14 07:30 - classes not using their attack skills (SP always full)
- Trace (ryu hunter, Novastorm, TheKirafang, Zenblade Mart, 4 min): Swordmen 0 skill casts and almost no attacks; Assassin 66 Sonic Blow sends, 44 refused (0x0110, wielding a Knife); Merchant 0 Mammonite.
- Causes: (1) bait-and-carry: a party member 10+ levels above any member (an Acolyte too, or one on another map) tagged each monster once and then returned from fight() forever - never attacked or cast again; the healer never killed it. (2) Envenom was not in the Thief offense list. (3) Sonic Blow/Double Strafe need a katar/bow; nothing checked the weapon and refusals were retried forever. (4) Mammonite was gated at 5000+ zeny at lv10 (1000z per cast).
- Fix: bait only for a weaker non-support member on the same map within 12 cells, and fight for real 6 s after the tag; offense list has Envenom; bestAttack skips skills whose weapon is missing (skillWeapon) and any skill the server refused for 60 s (0x0110 -> skillFailUntil); Mammonite from 2000 zeny at lv = zeny/5000.
- Lesson: every "return without acting" branch in fight() needs a time bound; and any skill the server refuses must be backed off, not resent.

## 2026-11-14 02:25 - Bows need arrows (ammo is a consumable)
- QA: after the class-weapon fix all 86 online Archers with a bow had NO arrows; rAthena refuses bow attacks without
  ammo, so they "fought" with full SP and 0 kills. A new weapon type can bring a new consumable: check what it needs.
- Fix (ammo.go, shared): bow users keep 500-1500 Arrows (1750) inside the potion weight room; bought on every shop visit
  that lists them (Tool Dealer buy step, the bow purchase packet itself), else a Trip.Ammo stop at the town's arrow
  seller. Arrows are equipped with 0x0998 loc 0x8000 (EQP_AMMO); the server answers with 0x013c (not 0x0999), and
  equipped arrows show as WearState 0x8000 in the normal item list 0x0b09. "no arrows" is a necessary need (solo trip
  "(necessary: no arrows)", party town run); meanwhile the bot switches to its Knife and equipPass won't re-wear the
  bow until arrows are in the bag.

## 2026-11-14 02:30 - roaming 20%: crowding, not a narrow fitShare
- QA 02:24 (729 online): roaming 20% (146 bots). Measured 02:27 (726 online, 2,400 kills/min): roaming 16.7%; 95 of
  133 roamers saw NO monster at all, 18 only claimed ones, 4 only too-high ones. Crowded fields: pay_fild02 75 bots /
  213 spawns, prt_fild04 41/146, prt_fild02 54/274 (31% roaming), pay_fild01 41/264 (36%), pay_fild07 36/276 (44%),
  mjolnir_09 28/240 (32%); prt_fild08 (616 spawns) and moc_fild01 (339) held 13-14 bots with 0 roaming.
- fitShare was NOT the narrowing: lowering minFitShare 0.5 -> 0.35 adds 0-1 guide maps per band (the guide level
  windows decide), and the veto already counts aggressive monsters only. Spawn capacity per band (spawns/5) is
  500-850 bots: the room exists, the pickers just all chose the nearest fields. The old limit was a flat stay_crowd
  70 and occupancy counted only bots already standing on a map, so a whole wave picked the same field.
- Over-levelled bots (lv30-49 with starter gear -> powerLv 20-25) empty a low field faster than it shows monsters:
  bots/spawn alone doesn't predict roaming (mjolnir_01 0.20 -> 1% roaming, pay_fild07 0.13 -> 44%).
- Fix (crowd.go): load = bots on a map + bots heading there (published goal), refreshed every 5 s, every pick reserves
  its seats at once (crowdNote); cap = non-boss spawns / crowd_spawns_per_bot (5); a map where 30%+ of 12+ bots roam
  is full too. Every picker honours it (goodHere, chooseHuntingGround beginner/guide/SpotsFor branches, partyMapFor,
  partySpotNear incl. after-death, bestPartySpot), nearest map with room first, falling back to any fitting map only
  when all are full (beginners keep their home-region limit). A bot / bot party leader roaming roam_crowd_sec (75 s)
  on a full map relocates (instead of 120 s solo / 240 s party).
- Rule: a capacity limit must count the bots already on their way, and be relative to what the map can feed - a
  flat number per map lets a whole population pick the same "nearest good" field.

## 2026-11-14 Bot parties never existed in-game; parties now survive restarts (partypersist.go)
- Symptom (guild minds): every Legion deploy dissolved all bot parties; leader parties had to be re-formed by hand.
- Root cause found on the way: rAthena runs with `char_name_option: 1` (conf/char_athena.conf: names only letters,
  digits, spaces) and that applies to PARTY names too. The engine named parties "Novaboy's pt", so the char server
  refused every create: the DB `party` table held only "Ayla pt", every bot had party_id 0. No bot party chat was ever
  delivered and exp was never shared. The engine never noticed: it ignored the 0x00fa result code.
- Fix: botPartyName (letters/digits/spaces, unique, <= 23 chars); 0x00fa failures are logged as events;
  botPartyKeeper keeps the in-game party in step every 10 s (create, invite, leave leftover parties, hand the in-game
  lead to the engine leader, rename after 3 refused creates); data/parties.json (main instance only) is saved on
  change + every minute (atomic) and restored at startup (members log in on the saved character; rAthena still has
  the party, so it's just re-attached). Shutdown no longer disbands parties. 90-min refresh skipped for a party that
  is farming well (all online, on the party map), up to 4 h.
- Rule: check the server's answer to every request the engine sends (result packets), and verify engine state against
  the DB (`party`, `char.party_id`) - an engine-side object that "works" can hide a feature that never worked.

## 2026-11-14 08:05 - arrow trips looped: the engine's shop list wasn't the server's (barter Dealer_Update)
- QA 07:49-07:55: "heading to geffen (buying arrows (necessary: no arrows))" -> geffen_in -> "restocked" -> the same
  trip again in the same second (kimchiknight, Iron_fang, Yuki.storm46, Vex kun, Silver fang x7 ...). Got arrows by
  trip town: Prontera 11/18, Payon 9/10, Izlude 7/11, Alberta 0/25, Geffen 0/16.
- Cause: Legion parses the shop .txt files statically. rAthena's npc/re/merchants/Dealer_Update.txt runs at OnInit and,
  with feature.barter on, UNLOADS the old Tool Dealers (#gef, #alb2, #pay, #prt1, #moc3 ...: they list Arrows) in favour
  of "Tool Dealer#Extended_*" on the same cells (no Arrows) and deletes Arrows from every Weapon Dealer. The engine
  thought Geffen/Alberta/Morroc sold arrows; real arrow sellers (plain shops): Prontera south gate Weapons
  Merchant#lgWea0, Izlude Tool Dealer#iz (izlude_in 57,110), Payon Tool Dealer#pay3 (payon 159,96), Einbroch Paddler,
  Lighthalzen Arrow Merchant#lhz. The "Arrow Merchant#over_arrow" NPCs are menu scripts + marketshop: bots can't use them.
- Fix (ammo.go, shared): applyBarterDealers() applies the barter branch's unloadnpc/npcshopdelitem to the parsed shops
  (Shop.ID = full NPC name); arrowTown picks the nearest town (fewest warps) whose arrow seller is real; a visit that
  yields no arrows logs "could not buy arrows at X", marks that shop failed for the session and rests the need 20 min;
  every trip by a bot without arrows also rests the need 20 min; "no arrows" needs zeny for >0 arrows (no futile trip).
- "restocked: 176 potions" was NOT an overstock: potionCount includes the starter kit (148 Novice 569 + 13 Light 11500)
  + 15 Red. The restocked line now shows "bought-grade N, starter kit M".
- Rule: when game data comes from server files, also apply the scripts that change it at startup (OnInit unloadnpc,
  npcshopdelitem, movenpc); verify a "this shop sells X" belief against a real buy list before building trips on it.

## 2026-11-14 03:20 - mjolnir_06 deaths: poison tracked, but the killer is the Spore swarm (poison.go)
- QA 03:02-03:10: mjolnir_06 = 17 of 51 deaths (lv26-51, Poison Spore / Smokie). Hypothesis: poison stops regen, bots
  "rest" and die. Measured 03:12-03:17 (2 s samples of every bot there): 7 deaths, 0 while resting. All died in
  combat within 6-8 s (Crusader lv48 1781 -> 0 in 8 s, Acolyte lv46 775 -> 0 in 12 s), 3 of the first 5 were only
  passing through ("travel -> gef_fild04/05, mjolnir_02"). Poison Spore (1077) is lv26, atk 23, Ai 04 (aggressive),
  100 spawns: mapDanger ignores aggressive monsters 5+ levels under the traveller, so a lv46 route crosses a map
  where a long walk collects a big train.
- Poison IS real: map traffic capture shows ZC_STATE_CHANGE3 0x0229 (id.L bodyState.W healthState.W effectState.L
  pk.B), healthState bit 0x0001 = poisoned (0x0100 deadly poison), sent for us and every unit in sight (Vex_kun,
  Acolyte on pay_fild08 by Poporings). The engine ignored it. rAthena poison drains to 25% HP and stops natural regen.
- Fix (poison.go, shared): opt2 tracked for us and entities; while poisoned no sit anywhere (6 sit sites), cure item
  (Green Herb/Green Potion/Panacea/Royal Jelly) or own Cure (AL_CURE 35), ask a party Cure-caster within 12 cells,
  an HP potion under 40% (poison floors at 25%), Butterfly Wing home at <35% with no potion/cure/curer; lowHPGuard
  waits standing ("poisoned: waiting for it to wear off"). healFirst: when nobody needs a heal, Cure on a poisoned
  party mate in sight. Buy step: up to 4 Green Potions (506, sold by every Tool Dealer#Extended_*) for bots without
  Cure whose (party) map has >=10% poisoners (db/re/mob_skill_db.txt NPC_POISON/NPC_POISONATTACK, aggressive count
  double) or who were poisoned in the last 2 h. Guide picks: a solo bot with no cure skips maps with poisonShare
  >= 0.3 60% of the time. Dockerfile copies mob_skill_db.txt.
- Combat trace (Rinrose, Merchant lv38 635 HP, 08:20 UTC): "travel -> gef_fild04" -> engaged one Spore on the way ->
  11 different Spores hit it within 35 s, 4-6 hits/s of 35-48 dmg (+ NPC_POISONATTACK 188); it stood at 170,269 for
  12 s with 5 on it before "fled to town (5 monsters on me)". Open follow-up (not done here): mapDanger/route cost
  should count dense aggressive packs (100 Spores) for high-level travellers, and travel shouldn't stop to fight
  on such a map.
- Rule: check a death hypothesis against samples of the dying bots' state before building on it - "they die resting"
  was wrong here; the status packet existed and was ignored, but the deaths were burst damage in transit.

## 2026-11-14 03:27 - travellers died crossing mjolnir_06: route cost and transit now count swarms (swarm.go)
- Follow-up of the poison entry above: 3 of the first 5 mjolnir_06 deaths were bots only passing through. mapDanger
  counts aggressive monsters only from 5 levels under the traveller, so 100 aggressive lv26 Poison Spores cost a lv46
  route nothing; travel() then fought whatever hit it, and each fight held the bot still while the next Spores arrived.
- Fix (shared): swarmDanger(map, lv) = sum over aggressive non-boss spawns 5+ levels under us of
  count x (mobLv/lv)^2, faded out below mobLv/lv 0.35, only when the map holds 40+ such monsters (100 Spores: 32 for a
  lv46, 18 for lv55, 0 from ~lv75). Added to the route cost in routeMapsBad (not to mapDanger: the human party's
  checkpoint test uses mapDanger > 60). swarmMap = swarmDanger >= 15. kafraPlan/kafraVia add 2 hops per swarm map on
  the walk (swarmPenalty, cached), so a Kafra teleport around it wins when one exists. A bot whose goal is another map
  doesn't fight on a swarm map (target dropped, walks on) unless it hasn't moved for 3 s (something holds it); in
  escape(), fightView.transit makes 3+ attackers a lost fight at any HP (Fly Wing / run).
- Rule: "too weak to matter" is per monster; a dense aggressive pack is a threat as a whole. Rate a map by what a
  long walk across it collects, not only by its strongest monster.

## 2026-11-14 03:40 - economy batch 9: goods reach buyers where buyers already are
- 0 vendor sales all day: shop goods could only reach "free" bots (no trip), and party members never walked to a
  shop. Bots already pass through the south gate on ~11 Prontera town trips a minute. browseShops at the end of every
  Prontera trip got the first shop sale within minutes of the deploy (Lunatic Card 1,450z).
- 10 of 17 failed deals were "hit by a monster": one hit while the window was open killed the deal for good. Now the
  window is paused and requested again once it's safe (up to 3 times). Rule: an interruption that is only temporary
  should pause the work, not end it.
- Wants nobody can fill (49 "cheaper than NPC" Sandals wants, every offer above the bid) only add noise: post a want
  only when a matching offer exists at that price.
- Session state (b.vend) reset every login, so the market mind's shop titles were lost ~40 times. Anything a mind sets
  must be persisted (data/shoptitles.json).
- Follow-up 08:45: at real sellers the visits still bought 0 ("could not buy arrows ... (listed true, zeny 345-468)"):
  the trigger kept 300z but the buy step capped arrows at Zeny-500 minus SP potions. Now arrowsWanted is the one rule
  for trigger and buy; a bow user with ZERO arrows buys them first and keeps only 50z (arrowReserve); potions get what
  is left above 500z; the trip needs >= 30 affordable arrows (97z qualifies). Alberta's Weapon Dealer#alb is NOT in
  the barter npcshopdelitem list, so it really sells arrows and stays in the engine's list (its failures were zeny).
  Rule: a trigger and the action it triggers must share one budget function, or the trip is futile by construction.

## 2026-11-14 03:50 - escape items that do nothing; boss sightings are shared (escapeitem.go, bossnote.go)
- Kai_wing (lv57 Assassin, on a fetch deal) logged "fled to town (Butterfly Wing)" every 4 s for 2+ min on mjolnir_07
  and never left: escape() re-used the same item forever. rAthena refuses item use while an NPC dialog, a trade
  window, a chat room or an NPC shop window is open. Fix: useEscapeItem closes those first (0x00b9/0x00b8/0x0146,
  0x00ed, 0x00e3, 0x09d4), escCheck 2 s later checks the item count dropped or the map changed; if not, the item is
  skipped 30 s ("X did nothing on ...") and escape falls through to the next option (Fly Wing, then running). A
  fetch/delivery deal (b.mkt) is closed when the bot has to flee.
- pay_fild04: Angeling (lv77) / Ghostring (lv90) killed 7 bots in a few minutes; "avoiding it for 30 min" was per bot
  and Kai_99 walked back in to collect Vex 9913. Fix: noteBoss keeps map-wide spots (boss seen / killed someone,
  10 min) and a sighting adds 2 to the map's decaying death count every 2 min at most (recentDeaths feeds every map
  picker, Kafra landing, guild/party picks, route cost). bossWatch (every tick, before survive): a boss-class monster
  (mob_db Class Boss/MVP) 10+ levels above powerLv within 14 cells -> Fly Wing (Butterfly after a recent Fly Wing),
  else run; an auto hunting goal on that map is dropped and the map banned 30 min. Target picks skip monsters within
  12 cells of a boss spot, leaders don't collect a member standing in one, goodHere is false while a threatening boss
  note exists.
- Rule: a danger one bot learns (a killer monster, a deadly spot) is shared map-wide at once; per-bot memory lets
  the next bot walk into the same death. And every escape action must be verified to have happened.
- Follow-up (03:49): the party gear turn funded a Hood for Onilord21 (Archer lv32, Knife, no bow): weapon-first in
  gearStepFor/turn order only fired on an EMPTY hand. Now "no class weapon" ranks right after an empty hand in
  gearStepFor (+500, cheapest mode too), nextGearBudget and bestGearIn, and the turn order puts such members second
  (poolMember.noClassW). Poor solo bots: Holy 994 (Dancer lv51, Knife, ~300z) carried ~200 sellable loot at 24%
  weight and never sold. classWeaponLootNeed: when the class weapon is unaffordable but 90% of the NPC-junk loot value
  covers it (never cards/gear/valuable materials - npcJunk), needTrip returns "class weapon (sell loot)": one trip to
  the weapon shop's town, "selling loot for Bow (necessary: class weapon)", Tool Dealer first, then tripGear buys it.
  In a party it is a party run request like "class weapon".

## 2026-11-14 04:20 - economy batch 10: zeny moves by mail; diaries must say what really happened
- Guild fund: 0 gifts in batch 9's first 70 min. A trade needs both on one map, and no guild had a member with 2.5k+
  on its alchemist's map (most richer members are in parties on other maps; 1-3 solo ones per guild). The game has
  RODEX: mailing zeny costs 2%, works anywhere. Rule: before building travel for a transfer, check whether the game
  already has a remote way (mail) - travel is the expensive part of every deal here.
- RODEX trap: after CZ_REQ_OPEN_WRITE_MAIL the server keeps `mail_writing` set until 0x0a03 or a map change, and
  while it is set pc_useitem fails (no potions!) and trade requests are refused. Always cancel-write after a mail.
- Life mind vs data: the diaries matched the shop data; the mismatches came from (1) fetch/delivery deals written as
  "same map", (2) buys written twice (b.event("market"/"info") already writes the diary, plus an explicit
  b.remember), (3) the life mind making up trades and saving them as person notes. Anything a mind retells must come
  from real lines; "agreed to sell" is not a sale.
- Zeny is flat (918k at 02:50, 916k at 03:45 online): what bots earn they spend. The 100k guide can only come from
  pooling (the richest guilds hold ~30k above 1.5k per member right now) plus new income over hours.
- 03:55 follow-up (swarm): after the 03:35 deploy mjolnir_06 still had 20 deaths in 14 min, mostly bots HUNTING there
  (party-leader / auto picks) plus party members routed through it. Now every map picker skips a map with
  swarmDanger >= 25 for the bot's effective level (a party: its weakest member): guideSpotsFor, SpotsFor (goodHere,
  auto picks, the "easier map" after close calls), partyMapFor, partySpotNear (party relocate after deaths),
  bestPartySpot, kafraLandingOK. transitSwarm judges by min(level, powerLv) (gear can rate a bot above the swarm while
  its HP is that of its level; Thorn.claw lv45 died walking 896 -> 0 in ~12 s with 16 Fly Wings), and in transit 2
  attackers at < 70% HP or losing 10% in 4 s already count as a lost fight.
- 2026-11-14 04:10 follow-up: Blacksmiths/Alchemists/Monks still kept full SP - their useful skills are not "hit the target" ones. New skillbuff.go fightSkills(): in a fight (target within 3 cells, SP > 60% after the cast) one self-buff per tick on timers from re/skill_db.yml durations - Adrenaline Rush (axe/mace), Over Thrust / Weapon Perfection (any weapon), Two-Hand Quicken (2h sword), Blessing / Increase AGI on itself (non-healers who know them, e.g. Monks), Endure when 2+ monsters are on it, Call Spirits up to its level every 9 min; Hammer Fall (ground skill, packet 0x0366 UseSkillToPos) on 3+ monsters around the target with a 25% SP reserve. Timers reset on death. Mammonite now from 800 zeny at lv = zeny/1500 (lv1-3 for poor merchants). Lesson: check what each class's SP is FOR (buffs, area skills), not only the attack-skill table.

- 2026-11-14 04:05 Starter potions are not stock: 105 lv30+ non-healers carried 0 Red/Orange/Yellow because
  potionCount() counted 100-150 Novice/Light potions (30-50 HP each), so the buy step and "out of potions" never fired
  (Zenrogue lv52: 0 potions, 839 arrows and a 1,000z Hood). Fix (town.go): from lv20 realPotCount() counts only
  Red..White; potBuyPlan buys a survival minimum (potMin: 12/15/20 by level, +5 over 3,000 HP, healers 5) FIRST,
  keeping zeny for 30 arrows when a bow user has none, then arrows, SP potions, the HP top-up; tripGear gets the rest.
  Orange/Yellow fall back to Red when the minimum isn't affordable. Drinking (ai.go potionIndex): in an emergency
  (lv20+, HP<40% or <60% while hit) the strongest potion that doesn't overheal >25%; out of combat the weakest first.

- 2026-11-14 04:25 Town-run gear chain: Star claw bought Wooden Mail, Shoes, then Sandals (worse, same slot - compared
  against the worn item before the new Shoes were equipped); LilStormsoul spent down to 183z and the party walked home
  12+ min. Fix: Trip.Slots marks each slot bought on the trip and nextGearBudget skips it (one item per slot per trip),
  and compares against the best wearable item already in the bag (bestOwnedScore), not only the equipped one; tripGear
  and the "also buying" chain keep max(gearReserve, gearKeep) = Kafra fare home to the hunting map (fareHome, incl. a
  teleport to a nearer town) + the missing potion minimum, and the chain counts arrows bought with the gear.
- Gear turn churn (QA 09:20): the turn rotated after gear_turn_max_min whether or not anything was bought. Zen.fox
  got 2,320z for a Wand (Ayla's Payon runs never sell it), held 3,300z "can pay" for 45 min, then gave 2,625z to
  Echobunny for Shoes; Skysenpai1 (12,855z) and xXLunaroseXx stayed on Knives with funded turns. Now (gearpool.go):
  a turn ends only on the purchase (an unbuyable item - failed trip, no shop - is swapped for a buyable one, SAME
  member; offline holder yields after gear_turn_max_min; a never-funded turn after 3x). The turn item stays fixed
  while buyable. Zeny raised for an unbought item (gearPools.raised) is never gifted on. The purchase is driven:
  every trip (tripGear) and party run (bestGearIn) buys the held turn item first when the town sells it
  (gearTurnIn); bot-led parties already route the next real-need run to the seller's town (Planned); in a
  human-led party the holder asks "need a Wand, can our next town run be in prontera?" every 30 min. Ayla's plugin
  (partylead.pl gear_run_start) only routes to Prontera for HER gearPlan_item - members' items not considered yet.

## 2026-11-14 04:45 - empty maps: judge a map by what bots SEE, not by the spawn file
- QA 04:22: pay_fild01 read empty - 38/40 samples of ~10 bots saw no monster at all; beginners kept being sent there
  and roamed with 0 kills (pay_fild01 roaming 35% vs 23% for all beginners; 14 beginners on it + 19 heading there).
- Engine checked: 0x09fd/0x09fe/0x09ff unit packets parse Willow (1010) as objMob fine (it shows on pay_fild03/the
  edges of pay_fild01), ents are never pruned by age, MapName only comes from 0x0091, DB positions match. Server
  side: payon.txt (Willow 181, Poring 38, Fabre 38) is loaded, mob_count_rate 400 applied (130,452 mobs cached),
  map cache 400x400 with 67,763 walkable cells, no mapflag/custom script touching it. Expected ~6 monsters in view
  per bot (pay_fild03's density gives ~370 visible walkable cells; it reads 9.3), observed 0.2-0.4 => only ~5% of
  pay_fild01's spawns are alive on the server (likely rAthena dynamic_mobs spawn bookkeeping; a map-server restart /
  @reloadscript would show it). Reported to main; rAthena config untouched.
- Fix (seen.go): per-map decaying average (tau 10 min) of monsters in view per bot sample (every bot on a non-town map,
  one sample / 5 s, skipping the first 5 s on a map). >= 24 samples and avg < 0.6 => mapReadsEmpty: skipped by
  guideSpotsFor, SpotsFor (goodHere, beginners' home fields, auto picks), partyMapFor, partySpotNear, bestPartySpot.
  The weight decays when nobody samples it, so the map is retried after ~15-20 min. A solo auto bot that roams with NO
  monster in view for 60 s gives the map up (badMap 10 min) instead of waiting 120 s. Beginners whose every home field
  reads empty still stay home (the near-home field with the best reading). GET /api/mapseen shows the readings.
- Rule: the spawn list is a promise, not a fact. Every picker must also listen to what the bots on a map actually see.

## 2026-11-14 04:45 - "stuck ... unsticking" 90-650 s, always "try 1" (kafra-freeze agent; built, deploy ~05:35)
- Data (5 s samples of /api/bots + events, 09:13-09:33 UTC): 181 stuck events, 55 of them >= 90 s; 33 of those 55
  followed a death in the window. **Most long "stuck" numbers were respawn rests**: a bot dies, respawns on its save
  cell (the Kafra save points: payon 160,58 = the Kafra landing cell, geffen 200,124, alberta 31,230), sits in
  lowHPGuard for 2-4 min, and the old stuckWatch clock (started when the position last changed) said "stuck for 243s"
  the first second it set off again - then dropped its route and random-stepped it. Real long freezes: 1 in 20 min.
- **The old watch couldn't see a real freeze**: it fired only with a walk request sent in the last 5 s. A bot whose
  walks are refused (safeWalkDest returns !ok and sends nothing) or never sent at all was invisible. Live case, 3 bots
  on one cell: prontera 47,226 (Hana_99 659 s "town trip: go", thekimchibunny 10+ min, sun.knight, xXStormxXx 650 s
  "saving at the Kafra"), all landing there with a Butterfly Wing / respawn; the trace showed NOT ONE packet sent for
  12 minutes, then normal walking the moment the 12-min trip timeout ended the trip. Offline replays of
  tripThink/stepAlong/safeWalkDest from 47,226 (test harness in the agent's scratchpad: loads the map cache from the
  legion image) all walk fine with moving=false, so the live bot most likely had `moving` stuck true (MoveUntil far in
  the future) - not proven; the new trace lines below will show it.
- Fix (rally.go stuckWatch, nav.go safeWalkDest, kafra.go, ai.go think):
  - the clock runs only while the state is a walking state ("travel ->", "town trip: go/wing/wrong room", "going to /
    saving at the Kafra" in the walk stage, "rally: going/walking", "no route to"); loose states (regrouping,
    collecting, catching up, "going to ...") count only while walks are being sent or refused. Resting, fighting,
    vending, trading reset it. Fires at 30 s.
  - tries count per spot (within 4 cells, 5 min) and escalate: 1 = close dialog/chat/shop, re-plan, step to a free
    cell 2-5 away (walkable, same region, not a warp cell, nobody on it); 2 = also drop the Kafra plan and via-town
    detour, cancel trade, stand, walk 6-12 cells; 3+ = give up the town trip and Kafra plans for 5 min and Fly Wing
    (else Butterfly Wing, escapeitem.go useEscapeItem); no wing and solo = drop the goal.
  - safeWalkDest records refusals (buffAt "walkrefused"; the stuck event says ", walks refused"); with
    /api/trace on, every refusal and every STUCK try prints moving/MoveUntil/route/path/trip/lastWarp/lastMoveTx.
  - think() clears a MoveUntil more than 30 s ahead (no walk of ours lasts that long) and traces it.
  - a Kafra trip that times out in its dialog stage closes the dialog (next/cancel/close): answerStrayDialog skips
    dialogs while the trip is in "dialog", so a half-answered Kafra menu stayed open server-side and blocked walking.
- Lesson: a stuck counter must measure "trying and failing", not "time since the position changed", and a watchdog
  that needs the bot to be sending walks can never catch a bot that sends nothing. Check the 47,226 traces after the
  05:35 deploy (grep "STUCK try\|move time\|refused" in docker logs ro-legion with tracing on for a bot there).

## 2026-11-14 04:55 - economy batch 13: a skill that costs zeny drained the guild fund
- The guide fund mail worked (64/64), but alchemists' zeny fell with nothing in the diary: lilyrogue 11,166 -> 4,066z
  in 10 min. Polling /api/bots every 8 s showed -200z per hit while fighting: Mammonite (100z x level) at level
  zeny/1500. A richer bot cast a dearer Mammonite, so every gift was burnt as attacks. Rule: anything that spends zeny
  silently (skill costs, fees) needs a reserve and must check whether that money is set aside for something
  (needsGuide); and an unexplained zeny drop is found by polling the bot's zeny against its state, not by reading
  the diary.
- Buying from players only happened after the NPC had already sold the same gear (browse ran at trip end). Rule: a
  player market is checked BEFORE the NPC for the same purchase, or it never gets the sale.

- 2026-11-14 04:52 Party gear runs (startTrip "gear", gearPick) went straight to the gear shop: no Tool Dealer, so
  potBuyPlan never ran and 106 lv30+ bots stayed at 0 real potions while buying Guard/Shoes/Hat. Now a gear trip below
  potMin goes to the town's Tool Dealer first (Trip.Gear holds the gear shop, chained after the potion buy). The gear
  buy keeps the Kafra fare home (a weaponless bot excepted) and doesn't mark the item failed when only the fare was
  short. Party gifts (gearpool) now cover max(giftShortfall, price + gearKeep - zeny) so a gift isn't eaten by the new
  keep. (Kimchi_07's 1,975z for a 5,000z Muffler with 4,694z was the reserve rule working: zeny/4 reserve -> 6,667z.)
  surplusPots sells only at/over the regen weight line and keeps max(hpPotTarget, potMin).

## 2026-11-14 Fly / Butterfly Wings never worked (ZC_AUTORUN_SKILL ignored)
- Symptom: 280 "Fly Wing / Butterfly Wing did nothing" events in 13 min; 43 of 52 deaths came after one. Bots kept
  their wings (picklog: 0 Butterfly Wings consumed in 14 h). Potions worked fine.
- Cause: both wings are DelayConsume items whose script is `itemskill "AL_TELEPORT",1/3`. rAthena acks the use
  (count unchanged) and only sends ZC_AUTORUN_SKILL 0x0147. The client must then cast 0x0438 (skill, lv, own AID),
  and that cast teleports and consumes the wing. The Legion ignored 0x0147. Also, a refused use acks 0x01c8 with
  result 0 and amount 0, and we were deleting the item from the inventory.
- Fix: escapeitem.go onAutorunSkill casts self-target autorun skills on self. 0x01c8 only applies the count when
  ok=1. Lesson: when an item "does nothing", check its item_db Type/Script in the rathena container and the
  server-side picklog (type C) before blaming client state. Any `itemskill` item needs the 0x0147 -> 0x0438 answer.

## 2026-11-14 travelStalled measured straight-line distance: winding Mjolnir paths looked "stuck"
- QA 08:59-09:50 UTC: 156 "still stuck N cells from the portal ...: Butterfly Wing home" on mjolnir_01 (378,256 x92,
  284,18 x36) and mjolnir_02 (28,258, 79,365); "trying another way" once. Bots were 137-318 cells away.
- Not a region problem: `legion route` from the bots' cells gave valid routes and FindPath paths (430-1279 cells), and an
  audit of all 15,727 (arrival cell -> portal in the same region) pairs on every map found 0 FindPath failures, so
  components()/Region already match what FindPath can walk (RouteMaps never plans a portal in another region).
- Cause: travelStalled compared the straight-line distance to the portal. Mjolnir winds: mjolnir_01 98,94 -> 378,256
  is 918 path cells for 280 straight, and every path there passes ~214,129 after a 283-cell stretch that never gets
  nearer as the crow flies (mjolnir_02 270,250 -> 28,258: 666 cells; mjolnir_01 200,200 -> 284,18: 743). With fights
  on the way that is > 60 s -> "re-planning", 120 s -> Butterfly Wing home, walk back, repeat.
- Fix (nav.go): progress = cells left on the planned path (b.path ending at the target); re-plans re-base the yardstick
  without counting as progress. Escalation: 60 s re-plan, 120 s avoid that portal for this bot only (10 min,
  routeFor/routeMapsSkip; another exit or a Kafra town; still used when it is the only way; at most 2 per map),
  180 s Butterfly Wing, then the global 30-min bad mark. Simulated on the real maps at 2 cells/s: 0 stalls (was
  stretches of 141-371 s). Tests: travelstall_test.go.
- Lesson: a "no progress" watchdog must measure progress along the route it is following, never as the crow flies.
- 2026-11-14 05:15 follow-up: Call Spirits on a 9-min timer was refused (0x0110) with 5 spheres still up. Now the sphere count comes from the server (0x01d0/0x01e1 aid+count -> b.spiritCasts) and Call Spirits is cast only below the skill level. Hammer Fall at 3+ monsters around the target never fired in 10 min of 8 traced Blacksmiths -> threshold 2+; trace.go now logs 0x0366 ground-skill sends. Lesson: track buff/resource state from the server's packets, not from guessed timers.
- 05:25 follow-up (batch 14): a sticky choice saved across restarts must survive the restart's blind spot: after a
  deploy, guild membership from the game (not the persona file) and real levels are unknown until each account logs
  in. "Keep the saved pick" failed for a character whose guild comes only from the game, and "never judge an unseen
  pick" kept an offline Novice over a logged-in Alchemist forever. Give the blind spot a time limit.
- Dead stock is a demand problem, not a price problem: 338 Phracon sat unsold at any price because nothing in the
  engine used Phracon. Give goods a use (refining) before tuning their prices.

## 2026-11-14 05:35 - wings work now: Butterfly Wings were firing as town trips (escape-tuning agent; built, deploy ~06:05)
- Measured 10:17-10:23 UTC (picklog type C joined with 1.5 s /api/bots state polls + /api/events): 141 Fly + 114
  Butterfly Wings in 5.4 min (26 FW/min, 21 BW/min). Butterfly Wings by trigger: town trips 73 (46 "on the party town
  run" gear buys, 7 class weapon, 5 party run), Kafra-trip shortcut 12, losing fights ~13, unknown 16. Escapes on
  orc maps went wrong: LilLunamage (Crusader 56) used 6 Fly Wings among gef_fild03 orcs in 3 min, then died; 15 deaths
  within 60 s of a Fly Wing, all on swarm maps (gef_fild03, mjolnir_02) or after repeated wings.
- After a Butterfly Wing, party members rest in town at ~1%/3 s for 1-3 min, then travel back to the party map
  (goal kept) - they do rejoin, but it costs minutes and often a Kafra fare.
- Changes: every wing use logs a "wing" event "<item>: <trigger> (hp, attackers, lv vs lv, solo|party)" (escapeitem.go
  wingEvent; also town trip, Kafra shortcut, no route, poison, walled in). escape(): Butterfly Wing only for a monster
  9+ levels above (outmatched: 8 of 16 Fly Wing escapes from one died <= 60 s, 0 of 5 Butterfly), a swarm map or a recent boss note, a 3rd escape within 2 min, a 2nd Fly Wing within 20 s while
  under 30% HP, or under 15% HP when solo; otherwise a Fly Wing (a party member always flies first). losingFight
  "lost N% HP in seconds": lv20+ non-weak bots with 5+ potions drink first and leave at 50% (Swordman line 40%); a
  single 35%+ drop still leaves at once. stuckWatch try 3: Fly Wing; Butterfly Wing only at try 5 or walled in.
  travelStalled 180 s: one Fly Wing + re-plan instead of a Butterfly Wing home, then the bad-portal mark.
- Rule: when a broken mechanism starts working, re-measure every trigger that was tuned while it was dead - they
  were tuned for "nothing happens", not for "costs a town trip". Next: the party gear runs (46 BW in 5 min) are the
  real Butterfly Wing sink; they break the "town runs only for a real need" rule (party/economy owner).

## 2026-11-14 05:40 - party town runs were one per member need (party town-run agent; built, deploy ~06:05)
- Measured 10:21-10:30 UTC from /api/events (parties grouped by party-chat heard_by, a run = 2+ members of one party
  heading to town within 3 min, caller = the party-chat ask before it): 45 party runs in 8.9 min, 0.87 runs per
  party-hour over 351 party groups (much higher for full parties); callers: weapon asks ("need a Falchion, can we go
  to town?") 64%, leader/human/unknown 20%, no arrows 11%, bag full 2%, out of potions 2%. A member trip was away
  from its map ~3.5 min (median). Escape agent: 73 of 114 Butterfly Wings in 5.4 min were these town trips.
- Cause: "class weapon" (also a weapon / body armor far below the level, gearStepNeed) counted as a real need and
  called a party run; gearStepReady limits each MEMBER to one per 60 min, but the party gate (callTownRun) was only
  6 min, so a 5-member party got a run every ~6-12 min, and startTrip winged every member to town each time.
- Fix: party.go callTownRunGap; members' non-emergency calls (bag full, low potions) need trip_min_interval_min (45)
  since the party's last run; only "no weapon at all" (6 min) and "no arrows" (callTownRun, 6 min; arrowNeedAt 20 min
  per bot) are emergencies; 0 potions / 90% bag already go alone. Class weapon / gear step never calls a run: it is
  bought on the next real-need run (bestGearIn ranks it first); the member says so in party chat at most once per
  30 min (ai.go waitTownSay; also when a real need waits for the interval). New event "party town run called: <why>".
  Town trips use a Butterfly Wing only when the town is 3+ maps away, or with 0 potions / under 30% HP (town.go
  tripWingOK); otherwise they walk.
- Lesson: a per-member rate limit is not a party rate limit - N members x 1/hour each is N runs an hour for the
  party. Gate shared events (town runs, rallies) at the party level, and let member needs ride along.
- The raised-zeny record was memory-only: after the 05:06 deploy Kimchi.senpai gave away 420z + 730z and Silent
  lord86 390z that they had received for their own turn items. It is now saved with the party's gear state
  (partypersist.go -> data/parties.json "gear.raised", restored at startup; each gift marks the parties dirty).
  Human-led parties (Ayla's) aren't in parties.json, so their record still resets on a legion-core restart.

## 2026-11-14 05:40 - warp guard refused every step beside a door (prontera 47,226)
- Symptom: bots arriving from prt_in at prontera 47,226 (west door 50,228, area x 48-52 y 226-230) heading for the
  gear-shop door 177,221 had every walk refused ("walk to 48,225 refused (warp area in the way)") for ~11 min, until
  stuckWatch; then the trip was given up. boxHitsWarp tested the whole box spanned by the walk: the diagonal step
  47,226 -> 48,225 spans 48,226, a corner cell of the west door's area, though the step never enters it.
- Fix (nav.go): segHitsWarp replaces the box test in safeWalkDest and stepAlong's segment trim (ai.go). When
  rAthena's direct path (diagonal first, then straight - path_search tries it before A*) is fully walkable, only its
  cells are checked; when it is blocked the server runs A* and the box test stands. If every candidate is still
  refused, detourStep takes a safe step (<= 3 cells, off any area, nearest the target) instead of standing still.
  Test: warpguard_test.go on the real prontera map cache (LEGION_TEST_RATHENA or /rathena; skips without it).
- Lesson: a conservative guard must model what the server actually does; "any cell of the bounding box" was right
  only for A* walks and blocked harmless steps. A guard that refuses must also offer a way out, never a dead stop.
- Ayla's party (human-led, legion-core) now persists its gear pool too: gearpersist.go saves turn, turn item,
  last-got and each member's raised zeny to data/parties_core.json every 20 s when changed (never an empty snapshot,
  never during shutdown) and restores it at startup (pending until the party re-forms). A restored turn gets a fresh
  TurnSince (grace for the holder to log back in, bot-led restores too), and an offline-timeout rotation no longer
  clears the holder's raised zeny.

## 2026-11-14 05:45 - regression: an empty-map fallback sent Novices into Payon Cave
- After the 05:06 deploy, lv3-18 Payon/Alberta beginners died on pay_dun00 (top death map, 7 in 12 min). DarkPriest
  (Novice lv3): "no monsters in sight on pay_fild01" -> "too dangerous on pay_fild03" (3 close calls: per-bot ban)
  -> both home fields banned -> my seen.go fallback took ANY spawn-list map near home whose monster levels overlapped
  -> "heading to pay_dun01" -> died on pay_dun00 twice.
- Fix (beginner.go): beginners only ever get a GUIDE FIELD fitting their effective level (beginnerField: no _dun, no
  party spot, no non-guide map), from every path: healthy home field -> least crowded home field -> with Kafra money
  (keep + 1200z) a healthy beginner field in another region -> the least-bad home field (bans / empty / deaths only
  lower its rank). A beginner never falls through to the general pickers; goodHere is false for a beginner on a
  non-beginner field.
- Map deaths are level-relative now: reportDeathLv records deaths per 10-level band; recentDeathsFor (own band + half
  the neighbours) and deadlyFor (threshold x max(1, bots there / 10)) replace the absolute count in goodHere, the
  guide pick, the spot score and the guild leaders' party picks.
- Save flip-flop (alberta 10:12, payon 10:17...): no re-save while the saved town is within one map change as near
  the hunting map as the new nearest town (saveStillGood, in kafraSavePlan and the save detour).
- Rule: a fallback must still obey the same hard limits as the main pick. "Least bad" is chosen among allowed maps,
  never from the raw spawn list.

## 2026-11-14 05:45 - poor bow users sell junk for arrows; overweight bots leave cheap junk (arrows-loot agent; built)
- Measured 10:40 UTC (/api/bots + inventories): 40 of 1010 online bots at >= 50% weight (top 69%: dark_storm20,
  Iron2k85); bow users with no arrows and <= 159z: Kimchiboy, Thornlord, kimchiknight, Zero boy81, Echo.0783,
  Ghostfox, Kiradragon44, lilmochirose - all carrying junk loot. 0 "selling loot for arrows" trips (didn't exist).
- Cause 1: "no arrows" needed arrowsWanted >= 30 with Zeny - arrowReserve(50): at 46-50z that is never true, and
  nothing else sells their loot below 70% weight. Fix (ammo.go arrowLootOK): no arrows, can't afford 30, but 90% of
  the npcJunk value (cards/gear/valuables never counted or sold) buys 100+ arrows (weight after the sale) -> needTrip
  "no arrows (sell loot)" -> startTrip arrowTown, event "selling loot for arrows (necessary: no arrows)"; the Tool
  Dealer visit sells first, then the arrow seller (t.Ammo is now set when the loot sale pays, not only the purse).
  In a party it is the "no arrows" emergency (callTownRun, 6 min). arrowNeedAt (20 min) still prevents loops.
- Cause 2: loot pickup only stopped at 80% weight; regen stops at 50% and the trip waits for 70%. Fix (ai.go loot,
  lootvalue.go heavyJunk): over weight_regen_pct a bot skips ground NPC junk selling under 100z each; cards, gear,
  valuable materials, potions and dearer Etc are still picked up. No new town trips; every shop visit already sells
  junk first (tripThink "sell"), and a party-run joiner with loot goes as "town run with the party" (sells).
- Lesson: every purchase rule needs the "poor but has loot" path - a reserve + minimum that the purse can't meet is
  a permanent block unless the bag's junk counts as money. And a pickup rule must respect the same weight line the
  regen rule uses, not a separate higher one.
- Return leg (same batch, from the rally agent's routes): parties took the Kafra OUT to Prontera and walked back
  (xXThornrogueXx prontera > mjolnir_09 > geffen > gef_fild07; Zenboy prontera > moc_fild01 > alberta > pay_fild03),
  because kafraPlan wants a saving of kafra_min_saving (3) map changes and a 3-map walk home only saves 1-2. "no
  Kafra trip from geffen after all" was only the via-town arrival message (kafraVia had aimed at the Geffen save
  town). Now kafraSaving: within 20 min of a town trip (trip over) any saving takes the Kafra home, and fareHome
  keeps the fare for that same rule. Lesson: a hop-count threshold treats a huge mountain map like a town street;
  for the return leg, where the fare is already budgeted, prefer the teleport.
- 2026-11-14 05:45 follow-up: Monks (acolyte family, melee) inherited the healer logic (party.go support/spRest, heal.go healFirst, ai.go lowSP at 25%, pubState.Support) and, now spending SP on self-buffs, sat "resting for SP" 14% of the time with sit-flicker. New isHealer() = acolyte family AND party role support (Monk/Champion excluded) gates all healer behaviour. Melee rest for SP only below 15% with no fight and stand at 40% (hysteresis); SP potions in a fight only for healers or bots with an attack skill. Lesson: "class family" is not "role" - gate behaviour on the role.

- 2026-11-14 05:47 Monks/Champions are melee: town.go's potion rules (hpPotTarget, potMin, "out of potions", SP-potion
  share) now use isHealer() (acolyte family AND support role) instead of the whole acolyte family, so Monks stock HP
  potions for their level and get "out of potions" trips.
- Class weapon still leaked on party runs (QA 05:43): Ravenheart72 (Archer, Knife) bought Sandals + Jacket on party
  runs while saving for its Bow, and sat "waiting for its gear turn (Silverstorm first)"; dark_storm20 (Bow in the
  bag, no arrows) had gear_next = Hood and joined runs as "with the party" (visits no shop) so never bought arrows.
  Now builds.go gearHold() (class weapon missing, or a bow user with 0 arrows) blocks every other gear buy: bestGearIn,
  tripGear (only the class weapon, if sold in that town), gearTurnIn, "gear in town", and gearStepFor (noClassW: class
  weapons only; noArrows: no step at all). A holder that isn't weaponless/Knife-bound and has raised nothing yields the
  turn to such a member. A bow user with 0 arrows joins every party run as "town run with the party", so the arrow
  seller (t.Ammo) rides along.
- 05:55 follow-up (QA 05:48: party archers/acolytes at 71-80% weight for 20+ min): wolfclaw70 carried Novice Potion
  162, Carrot 76, Apple 49, Fly Wing 43 (5.0 each), Green Herb 43; Ghostfang Green Herb 101, Novice 121. Fruit/herbs
  are type Healing (not npcJunk, not potions): never eaten, never sold. Fix (food.go): out of combat under 90% HP a bot
  eats fruit/herbs (weakest first), then Novice Potions past 30 at lv30+; Green Herbs past 10, Fly Wings past 15 and
  fruit past 20 are surplus loot (surplusPots -> hasLoot + the sell list). A party member whose bag-full need waits
  for the 45-min interval now logs "town need waits for the next party run: ..." and says so (once per 30 min); before,
  the tooSoon branch waited silently. Arrows: with no arrows, potBuyPlan keeps up to 200 arrows' zeny before the
  potion minimum, also at a Tool Dealer that sells none (wolfclaw70: loot -> 370z, 5 Red first, then only 69 arrows).
  Lesson: every item class a bot can pick up needs an exit (use, sell, or keep with a cap) - otherwise it is dead
  weight that silently blocks regen.

## 2026-11-14 06:12 - bow users "loaded" with an empty ammo slot: WearState is a location, not "worn" (archer agent; built)
- QA 06:01-06:05: ~19% of fighting Archers (1 of 4 Bards) 0 kills in 90 s with bow + arrows, full SP, target 1-9
  cells, arrow count never dropping. Trace (TheStarsong, Silentleaf, LilFrostwing, Onilord21): Double Strafe 0x0438 +
  attack 0x0437 sent every second, NO damage packet from them, no 0x0110. The DB (inventory.equip, saved at the 06:05
  logout) had the arrows at equip=0 for all of them while the engine showed Equipped=true; 78 of 128 bow wielders with
  arrows had an empty ammo slot. "Working" archers (Pixel shot 44 kills, Kiradragon44) dealt no damage either: their
  targets died to party members and the kill counter counts a target's death ("vanish type 1"), so kills hid it.
- Cause: rAthena clif_item_normal fills NORMALITEM_INFO.WearState (0x0b09) with the item DB's equip LOCATION
  (id->equip), not the item's equip state - every arrow stack carries 0x8000. bot.go marked any such stack worn, so
  after every login (every deploy) ammoThink saw "loaded" and never sent 0x0998; the server refused every shot with
  0x013b type 0 ("Please equip the proper ammunition first", ZC_ACTION_FAILURE), which nothing handled. Only archers
  whose ammo was equipped server-side before the logout worked (the server then sends 0x013c after the list); a bot
  whose arrows were bought while it held a Knife, or that relogged before equipping them, stayed stuck forever.
- Fix (shared, bot.go/ammo.go): 0x0b09 no longer sets Equipped; it clears the ammo slot and 0x013c (sent right after
  the list only when the slot is filled) is the truth. New 0x013b handler: type 0 empties the believed ammo slot and
  resets ammoAt so ammoThink re-equips at once. trace.go logs 0x013b / 0x013c. Test TestArrowWearStateIsNotWorn.
- Lessons: an "equip state" field in a list packet may be something else - check the server's clif_* filler before
  trusting a field. Kill counters that count deaths of our target hide a bot that deals no damage: QA should look at
  damage packets from the bot (trace "RECV 08c8 dmg <our aid> ->") or consumable counts, not only kills.
- 06:22 follow-up (first minutes after the 06:05 deploy, "wing" events): nobody ever BUYS Butterfly Wings (picklog
  type S: 0 since 08:00; 816 of 1014 online chars hold 1654, ~2 each), and town trips + Kafra shortcuts burned ~10/min,
  so escapes found none: Kimchi07 (lv39, party on a lv59 map, BW used at 11:08) then used 30 Fly Wings in 3 min at
  50-86% HP; TheIronfire 7 Fly Wings in 40 s among gef_fild03 orcs, then died. Now town trips (tripWingOK, except
  no potions / <30% HP) and the Kafra-shortcut plan (kafraVia) only use a Butterfly Wing when 2+ are carried: the last
  one is the escape reserve. After 4 escapes in 2 min a Fly Wing is used only under 50% HP (else run). Built, deploy
  with the next batch. Open: Butterfly Wing restocking (economy owner), parties choosing maps 20 levels over a member.
- 06:25 follow-up (built, ships ~07:05): after the 06:05 deploy, party runs per party-hour went UP (sentinel 0.41 ->
  0.79). Cause: TownRunAt (and each bot's lastTripAt) live in memory, so a restart opened the 45-min gate for every
  party at once; the "party town run called" events in 11:09-11:16 UTC were 6 "out of potions" (lv20+ with < 3
  Red..White but 46-167 starter potions, after runs that spent the purse on gear and bought only 16-45 Reds) and 4
  "bag is full" (70%). The members the sentinel flagged (light bag, full pots) were JOINERS of those runs, as designed.
  Fix: callTownRunGap counts the Legion start as a run 25 min ago (party.go townRunBoot). Lesson: a rate limit kept
  only in memory resets with every deploy; with deploys every 30-60 min, measure right after a restart too.
  Open: lv30+ bots run out of Red Potions within the hour because gear eats the purse after potMin (12-20); the
  potion target vs. gear budget belongs to the economy owner.
- 11:25 party maps vs the weakest member + Butterfly Wing restock (shared engine, built, deploy with the next batch).
  Measured offline (partyfit_live_test.go: real rAthena data + a saved /api/parties/detail, 368 bot parties): 0
  parties had their weakest member >15 levels under the map's mean monster level at 11:15, but 17 sat on maps every
  picker vetoes for their weakest member (swarm maps such as mjolnir_06, where Kimchi07 was fleeing while his party
  stayed) and 9 leaders had Goal.Set "auto": the pickers check the weakest member, but (1) a leader whose goal was
  dropped (no route, death, job change, panic reset) re-picked with chooseHuntingGround for its OWN level and the
  members followed it - the path to "lv39 member on a lv59 map"; (2) the party map was never re-checked after
  formation; (3) a member's 3 escapes in 10 min only abandoned the map for solo bots (Set "auto"); (4) a non-guide
  map passed on fitShare alone (half the monsters under the filter, the rest far over).
- Fix: leaderPartyPick (ai.go) - a bot party leader re-plans for the party (current map while it fits the weakest
  member and is reachable, else the nearest fitting guide spot, Set stays "party-leader"); partyFitCheck (party.go,
  every 5 min per party) relocates a party whose map no longer suits its weakest member; a party member with 3 escapes
  in 10 min on the party map asks the party to move (danger.go -> relocate); partyMapOK/partyMapNear/partySpotNear/
  bestPartySpot also require the map's mean monster level <= weakest + 12 (mapMeanLv, data.go; no guide spot loses
  its own band). Offline after: 17 -> 1 unfit party maps (the last has no fitting guide spot within 8 maps: it stays).
- Butterfly Wings: picklog 11:24 - 0 bought (type S) vs ~700 used an hour; Fly Wings are fine (1600/h looted, 1390
  surplus sold in 3 h; the shop already tops up to 5). Now every potion restock at a Tool Dealer tops Butterfly Wings
  up to 3 (bwTarget) from spare zeny after potions, before Fly Wings, under the potion weight cap - inside the normal
  town run only. The "bought ..." event shows the count. Lesson: when a consumable "runs dry", check both sides in
  picklog (S buys vs C use, P/L loot) before assuming; and a picker that respects a constraint is not enough - look
  for every OTHER path that sets the same state (here the leader's solo re-pick and the never-rechecked party map).

## 2026-11-14 06:40 - bots parked at 50-69% weight (no regen), bags of herbs/fruit/starter pots
- QA: 68-75 of ~1,010 online bots sat at 50-69% weight with no town run. The bag trip waits for trip_bag_pct 70,
  heavyJunk stops cheap pickups at 50%, so they hovered between the two forever, without natural HP/SP regen.
  Heavy bags held (75 heavy bots, weight units): Fly Wing 123k (~36 each, keep 15), Green Herb 116k, Novice Potion
  99k (Buy 0: can't be sold), Apple+Carrot 81k, Jellopy/Fluff/Clover 128k - and Club 317k + Knife 78k (gear: never
  NPC-sold by owner rule, see open issue below). food.go (05:52) ate fruit only out of combat below 90% HP and only
  without overhealing, so almost nothing was eaten.
- Fix (weight.go, own functions): snackIndex - food is eaten whenever hurt (fights too, below 80%), and over the
  regen line at any HP loss, heaviest first, plus surplus Novice Potions from lv20; the low-HP rest path eats before
  sitting (LilStar99 sat at 14% HP with Apples/Potatoes/Red Herbs). regenWeightNeed - over the line with sellable loot
  (npcJunk + surplusPots: never cards/gear/valuable materials) >= 10% of the limit, or enough to get back under the
  line (>= 3%), returns "bag is heavy": a realNeed in ai.go (party run batching and trip_min_interval 45 still apply).
  Offline estimate on the live bags: 66/75 heavy bots qualify; 68/75 end under 50% after the sale + eating.
- Open: duplicate starter weapons (avg ~7 Clubs + ~3 Knives per heavy bot, 4-18% of the limit) are gear and are kept
  by rule; they need a player outlet (vending / give to novices) or an owner decision on plain NPC weapons.
- Alchemists "barely fight" (same QA): not reproduced. Poor (<800z, 28) vs rich (9) alchemists: 95 vs 86 kills since
  the 06:05 restart, median 105 = Blacksmith/Knight level. Traces (xxthornbunnyxx, Pixelstorm, xXRyuchanXx) show axe
  auto-attacks of ~180 dmg finishing Horns. Mammonite is (correctly) skipped under 2,500z; Acid Terror/Demonstration
  sit at the end of the plan. The low-kill ones were dead/respawning, travelling or vending. Lesson: compare a class's
  rate with its peers from the API before changing combat code.
- 06:30 (batch 15): a "need X" rule and an "X is spare" rule must share one budget, or two bots trade the same item
  back and forth (Phracon). Spare = holdings minus own plan; and something bought is not resold soon.
- Interior maps (prt_in) are several rooms: "go to map M" is not "go to the NPC". Route to the point
  (RouteMapsPoint) and test the route offline from every entry cell seen in logs before shipping.
- 06:40 roaming breakdown (20 samples x 12 s, 13,099 bot-samples, roaming 7.2%): party members standing next to
  their leader 31% (hunt() fell through to explore() when partyThink had nothing to do: walked 20-80 cells off, past
  together_cells, and the leader turned back to collect them), leaders seeing 0 monsters on populated maps 16%,
  leaders/solo on thin maps (< 2 monsters in view per bot: pay_fild01 0.7, prt_fild02 0.8, mjolnir_09 0.95,
  mjolnir_02 1.5, prt_fild03 1.7) 17%, leaders seeing only claimed monsters 8%, the rest small. Streaks were short
  (489 of 659 lasted one sample): single kills reset roamSince, so the "75 s in a row on a full map" relocation never
  fired on thin maps. Fixes (built, batch after 06:40): a bot-party member with nothing to fight stays "with
  <leader>" (steps in only beyond 6 cells); seen.go mapReadsThin (< 2.0 avg) makes crowd.go mapHasRoom false /
  mapCrowded true, so every picker prefers populated maps and roamers there leave; roam share over a 3-min window
  (>= 35% on a full/thin map) also triggers the move; explore() heads for the nearest monster another bot saw in the
  last 40 s (shared per-map sightings, same region, not the stale spot just reached) before sweeping blind.
  Lesson: a member's "nothing to do" must never reach the solo roam code; measure roaming as a share over time, not
  as an unbroken streak.

## 2026-11-14 ~12:20Z - No back-and-forth Kafra trips (guild07 report: Sunheart's party, kafra.go / ai.go / party.go)
- Report: guild07's leader Sunheart (Monk 54) + party "looped" Payon/Morroc/Geffen 09:00-12:10Z, zeny 9.5k -> 271.
  Diaries show the zeny went mostly on gear (Silk Robe 8000, Mace 1600, Hood 1000, Guard 500 = 11.1k); Sunheart took
  only 2 Kafra teleports. The "loop" was map flips + deaths: leaderPartyManager/guild mind re-formed the party 6 times
  (gef_fild10 -> prt_fild08 -> gef_fild03 -> moc_fild02 ...), each after deaths on Orc maps (recentDeaths -> new spot),
  respawning in the save town (payon/geffen) and walking out again. Hourly restarts re-pick too.
- Legion-wide the Kafra back-and-forth was real: 09:00-12:10Z 517 bots teleported, 88 of them 3+ times; last hour
  211 bots, 13 with 3+ (diary "taking the Kafra" lines). A->B->A within minutes: LilGhostgirl prontera->Geffen 11:35,
  ->Prontera 11:37; after the 12:06 restart prontera->Payon 12:06, ->Prontera 12:08 (restored party member picked a
  solo map before its leader came back, then got the party goal); Raven99 prt->Geffen 12:02, restart, ->Prontera 12:08.
- Fix: one Kafra teleport, then `kafra_cooldown_min` (25) min without another, and never straight back to the town
  just left within 2x that, unless a real need (`kafraNeed`: town run, the way home from it, a respawn since, vending,
  a human-led party). Checked in kafraPlan, kafraVia and kafraTeleCost; the last teleport is recorded on the 0x0091
  map change (`noteKafraHop`, also logs "Kafra teleport A -> B" reliably) and re-read from the diary at login so a
  restart doesn't reset it. A restored party's member waits for its leader on the party map (Set "party-restore")
  instead of picking a solo map. Bot parties remember maps they left (Party.Left, 30 min): partySpotNear skips them,
  leaderPartyPick stays rather than go back; partyFitCheck leaves a party alone 15 min after a move.
- Lesson: when a mind reports "money burned on travel", read the diary before believing it - count the fares
  (60-180z each) against gear buys. And every per-bot travel memory must survive the hourly restart.
- 07:20 REGRESSION from the party-fit change (deployed 07:06): 284 of 369 bot parties on Novice fields, 66 with every
  member lv45+ (Echoz pt lv64-66, ryu hunter pt lv61-67 on prt_fild08...). Cause: every persona's base_level is 1
  in personas.json and BaseLvOr() falls back to it until the server sends the level, so a member offline or still
  logging in (everyone, right after a deploy) published Lv/Power 1. The new 5-minute partyFitCheck and
  leaderPartyPick read that lv1 as the "weakest member" and relocated the party to a lv1-12 field - and nothing ever
  moved a party UP (partyMapOK only checked too hard). Fix: pubState.Known (BaseLv > 0); partyFitLv counts only
  online members with a known level (partyFitCheck waits while any online member is unknown; nobody known = keep the
  map); partySpotNear/bestPartySpot skip offline / unknown-level members; partyMapOK is false when the weakest member
  has outgrown the map (partyMapTooEasy: above every guide band listed for it, or off-list monsters 20+ under it), so
  partyFitCheck moves such parties up; partyMapNear prefers the higher band among equally near spots. Offline on the
  07:20 snapshot: all-lv45+ parties on Novice fields 66 -> 2 (the 2 have no fitting guide spot within 8 maps).
  Lessons: a fallback value (persona lv1) is not a level - carry a "known" flag through snapshots before any picker
  acts on it; and a "fit" check needs both bounds, too hard AND too easy. Test the cold-start state (everyone
  relogging) for any periodic re-planner, not only the steady state.

## 2026-11-14 07:30 - economy batch 18: parcels that could never fire, vendors without Magnifiers, refused pitches
- Batch 17's parcel code filtered out unidentified items, and a scan of ALL online field bots found 0 with two
  identified copies of anything: the spare piles are monster drops, which are always unidentified. Lesson: before
  shipping a feature gated on a population, count the population it acts on (one API scan) - "in the tree, untested"
  hid a feature that would have done nothing.
- 13 of 35 vendors in Prontera spent the whole session "setting up shop": rAthena refuses Vending within 3 cells of an
  NPC (0x0110 skill 41), and the pitch grid includes cells next to the custom south gate merchants. The state looked
  like normal progress. Lesson: a state that is meant to last seconds ("setting up", "loading") must be checked for
  how long bots sit in it; trace one (/api/trace) and read the server's answer.
- Guild-leader parties re-formed onto killer maps (QA 09:00-12:10: guild07 lv52-60 re-formed ~6 times onto
  gef_fild10, gef_fild03 Orc Lady/Warrior, moc_fild02/03 Peco/Wolf/Argos). formLeaderParty disbands and recreates the
  party, so Party.Left (maps left < 30 min) was lost each time, and no party-level death memory existed. Fix
  (party.go leaderMem, per LEADER persona, in memory): maps the leader's party left (30 min), 2 member deaths on a map
  within 30 min -> banned 1 h, a member's "keeps fleeing" -> banned 30 min. bestPartySpot skips banned maps and
  scores recently-left ones -20; partySpotNear never picks a banned map; partyMapOK/partyMapNear check the ban (so
  partyFitCheck moves a party off it, and a guild mind's explicit map is re-fitted); partyLeftRecently includes it.
  Lesson: state that must outlive an object (party) can't live on that object when the object is recreated routinely.
- 07:25 follow-up (built, ships ~08:05): Archers in parties stayed on the Knife with 1,850-2,055z (Ravenrogue lv36,
  a Bow one map away, no trip in 14 min) because "class weapon" waits for the next real-need run and also for the
  gear-step delay. Waiting is right for upgrades, but an Archer-line bot without any bow/instrument/whip can't do its
  job: ai.go bowless() now makes it an emergency like "no weapon" (6-min party gate, skips the per-member interval);
  needTrip skips gearStepReady for it. Other class weapons and upgrades still wait. partyRunTown now also looks at
  each member's waiting class weapon (not only nextGear), so a "need a Rod, whenever we go to town next" promise
  goes to a town that sells it (bestGearIn buys it first). Human-led parties still use the human's town.
  Test: bowless_test.go. Lesson: "can wait" depends on whether the bot can do its job at all without the item.
- Batch 16's "don't loot another spare" filter went into ai.go's solo loot loop only; partyThink has its own loot
  loop, so most bots never got it and the spare piles kept growing (max 15 -> 27). Lesson: before fixing a behaviour,
  grep every code path that does it (here every `b.pickup(` caller) and fix them all.

- 2026-11-14 07:30 (QA: Archers rest 17%, Assassins 18-21%, Hunters/Bards/Dancers 8-12% at FULL HP; Mages 6%, melee 0%):
  once arrows and Double Strafe worked, non-casters emptied SP on skills and then sat to "rest for SP" (lowSP 15% sit /
  40% stand), and in a party party.go stood them up for the leader's next fight so they sat/stood in a loop
  (SIT-FLICKER). Rule: only SP casters (Mage/Wizard/Sage line) and healers (Acolyte/Priest line, isHealer) ever rest
  for SP (`spCaster()`, ai.go lowSP). Everyone whose basic attack costs no SP (melee, bow+arrows) keeps auto-attacking
  while SP regenerates; damage skills fire only above a 30% SP floor (`skillSPFloor`, kept for emergencies); an SP
  potion only in a fight below 15% (`fightSPLow`). Their HP rest ends at 95% HP regardless of SP. After party.go stands
  a member up it can't start a new rest sit for 15 s (`noSitUntil`): sit and stand rules must not fight each other.
  Test: spsit_test.go. Baseline (12 polls x 10 s, 07:26): Archer 12.6% rest, Assassin 20.7, Hunter 10.3, Bard 8.3,
  Dancer 12.5, Rogue 7.3, Thief 6.2 vs Swordman/Knight/Merchant 0.

## 2026-11-14 - legion SIGSEGV: readLoop re-read b.conn after the session nilled it
- Crash 11:34:44Z: `(*Conn).Read(nil)` from readLoop killed legion (all 1,000 bots logged out). Only one run() per
  Bot existed, but readLoop read `b.conn` on every iteration without a lock. When a session ended through
  logoutSignal/stop (not a read error), run() closed and nilled b.conn while readLoop was still handling a packet
  already buffered in bufio; its next `b.conn.Read` hit nil. (It could equally have read the *next* session's conn.)
- Fix (bot.go): readLoop gets its session's conn as a parameter and a quit channel (checked before and after each
  Read, so an old session's buffered packets never reach the next session), recovers its own panics, sends exactly
  one value on done. run() captures the conn once, endSession() clears b.conn only if it is still that conn, and
  waitSessionLoops() waits (15 s cap) for the old read + AI loops to exit before the next login. `Bot.running`
  (atomic CAS) refuses a second run() per Bot. Tests: session_test.go (go test -race).
- Lesson: a goroutine must never re-read a shared pointer that another goroutine resets; capture it at start and
  pass it in. A session's helper goroutines must be joined before the next session reuses the Bot's state.
- Follow-up 07:35: live 07:05, "bag is heavy" as a PARTY caller took party town runs 0.24 -> 1.74 per party-hour
  (92 of 161 party calls in the window; split parties 13% -> 29%, travel 8% -> 22%). Now it is a real need for SOLO
  bots only; a party member eats its food and sells on the next real-need run (bag full 70%, out of pots). Lesson: a
  per-bot need multiplies by party size when it can call a party run - gate new trip reasons on p == nil first.

## 2026-11-14 - Ayla's party split 150 cells after a Prontera town run; 3-min hold deadlock at pay_arche
- 12:14Z a real-need run (OpenKore "Auto-storaging due to itemsMaxWeight", 60%) became a Prontera gear run (Cap).
  Her line "town run to prontera south gate, need my katar" was stale wording (Jur owned) and read like a gear-only
  trip. Now: "bag full. town run to prontera south gate, ill get my cap on the way" (partylead.pl; keeps "town run
  to <town>" for core's townRunLineRe).
- "Change of plan" came from the ayla MIND, not the plugin: 12:16:43 "Kafra in Payon" (correct), 12:17:36 "meet in
  Prontera" because at that moment Ayla, Echobunny and Zen.fox were all in Prontera shopping (she Kafra'd back to
  Payon 10 s later). Fix: cmdfile.pl refuses a party line naming a different meeting town within 10 min of the last
  one; legion-session skill tells the mind: no meeting points during a town run, meeting point = Kafra of the town
  nearest the hunting map, never changed.
- Why Echobunny/Zen.fox walked prontera > prt_fild08 > moc_fild01 > moc_fild02 > pay_gld > payon: kafraThink sets
  kafraNext = +2 min on every teleport arrival; they finished shopping within 2 min, kafraPlan silently said no,
  and travel walked. Zendancer84 shopped longer and took the Kafra. Fix (kafra.go): after a teleport made for a town
  trip, or by a human leader's party member, kafraNext is +15 s (kafraHold still stops loops). Legion-wide: every
  quick town run used to walk home.
- "bag is heavy" at 37%: a canned line in tripAnnounce for joining a party town run. Now only said at >= 50% weight.
- 12:25-12:27 deadlock: plugin held for Zen.fox 24 cells away; core said Zen.fox "with Ayla" (its leader position was
  stale - no bot saw her, humanLocs kept an old cell); the plugin collected only 'lost' friends > 25 cells. Fixes:
  core humanState uses Ayla's own status.json position when it is newer than humanLocs (< 8 s old); plugin: a friend
  > 20 cells away for 60 s is collected by the leader ('is not coming'), collect targets use friend_pos (API) not the
  stale party list. Then after the mind's 'goto pay_dun00' she went straight back to pay_arche for an 8-min-old
  "help" - %helpFrom now expires after 2 min everywhere and is cleared by 'goto'.
- Lesson: a canned line must match the bot's real state; a short anti-loop timer must not block the planned way
  home; two minds waiting for each other on different position sources deadlock - the side that waits needs a
  timeout that turns into action (the leader collects).
- Also: Ayla's items_control sold Phracon/Emveretarcon (sell list "0 0 1"); both now stored (0 1 0).
- Plugin reload of cmdfile itself: `partylead reload-cmdfile` (partylead reloads it from its own tick, never from
  inside cmdfile's command loop).

- 2026-11-14 (TOGETHER-RULE): 10 of 18 collecting bot leaders (07:35) were collecting a member within 20 cells or busy
  fighting, and members 10-20 cells away showed "waiting for X (coming to get me)". Causes: the collect/regroup
  hysteresis ended only at 8 cells (so a collect started at 21 dragged on through 9-20), the leader kept collecting a
  member that was fighting fine, and a dropped commitment left `collectName` published so the member kept waiting.
  Fix: together.go is the ONE rule (partyTogether / regroupNeeded / needsCollect) used by the member regroup + wait in
  party.go and both leader collect paths in ai.go: start beyond together_cells (20), stop within collect_stop_cells
  (now 12), never collect a member busy fighting above 50% HP, other-map members are travel's job; the leader clears
  its promise as soon as it no longer holds. Lesson: when two sides (leader/member) decide the same thing, give them
  one shared function, not two copies of the distance test.
- Follow-up (12:43Z): Ayla toured pay_dun00 (west x 17-43 <-> east x 100-170) because OpenKore chains monster chases
  up to attackRouteMaxPathDistance 20 cells, ~25 cells/min, round the whole floor (her random walk was already only
  12 cells). partylead.pl now keeps one grinding area: anchor + AREA_R 25; unprovoked targets beyond it or near a
  portal are skipped (checkMonsterAutoAttack hook), random steps stay inside, the anchor drifts 1 cell/20 s toward
  her and 10 cells toward the nearest monster after 60 s without a fight; re-anchored on a new map and
  when she ends up >35 cells out (never a walk back). pay_dun00 east stairs = warp 184,33 span 2x7 (x 182-186,
  y 26-40): PORTAL_KEEP raised 5 -> 9 so walk targets and chase targets keep clear of the whole trigger area.
- 07:45 follow-up (built; in the source for the 08:05 build): 28 Archer-line bots with 1,100-5,277z stayed on Knives
  because the party gear turn (gearStepFor with the pooled spare) targeted the 2500z Composite Bow and "kept" it as
  the turn item. gearpool.go poolStep: a member with no class weapon gets the CHEAPEST class weapon the pool can pay
  for now (classWeaponPick), and a pricier turn item already set is swapped to it. Bow-in-bag without arrows: the
  code path is right (bowUser counts a bag bow, the party-run join buys arrows on every run - added by another agent
  - and the Bow outranks the Knife once arrows exist); tests pin it. Tests: bowless_test.go (3 tests).
  Lesson: "the best upgrade the pool can afford" is wrong for a bot that can't do its job yet - first the minimum
  that makes it work, then the upgrades.
- 07:56 parcels: the attach ack is 0x0b3f on this server (PACKETVER 20211103), the code waited for 0x0a05 (older
  clients). A feature gated off and "untested" hid it for hours. Lesson: look each packet up under the server's
  PACKETVER #if in packets_struct.hpp before writing the handler, and test gated features right after the deploy.
- 12:55 UTC shop-door "warp loops" (141 in 9 min; alberta 98,153 / 117,38, prontera 179,184 / 177,221, geffen 43,85 /
  98,141): traces + a 1.5 s /api/bots state poll showed NO accidental step back onto a door (an offline test from every
  town building exit agrees: paths and first segments never touch a trigger cell). Every entry was deliberate: (1) a
  trip planned from a field (travel() to the interior map = its NEAREST door: alberta Room_Entrance 45,145, prontera
  west door 50,228 -> prt_in 128,40) kept that route across the map change, and the in-town check only asked "does the
  route end on the shop's map?" - wrong room, out, right door; (2) the detector called any A,B,A,B a loop, even two
  different doors of one interior map, and banned the shop door for 30 min when both legs were intended. Fix
  (warploop.go): in-town shop/refine trips accept only a route ending in the target's ROOM (routeEndsIn); a loop is
  the same door twice (mapVisit.via); a town's own building doors are never banned. Lesson: before fixing pathing,
  check whether the "bad" walk was intended - log the state with the walk. A loop counter that can't tell two doors
  apart measures our trip logic, not warps.
- 12:55 UTC pay_fild01 deaths: 33 of 36 since 10:30 "killed by Willow", only 14 all day by Willow Ringleaders; the map
  is NOT thin any more (3.98 monsters in view). Half the dead were party Acolytes: the priest-sit rule checked only
  underAttack (a recent damaging hit), so LilRinz sat "resting for SP" with its target alive (a Poring, then 2 s after
  a Willow's hit). Fix: sitguard.go fightOn (target in sight, or a mob swung at us - hit or miss - in 6 s) blocks every
  sit. Mini-bosses: Boss-class monsters close the map for bots under 25 whatever the level gap; champions (AegisName
  C1_-C5_: Ringleader/Swift/Solid/Furious/Elusive) are a LOCAL threat below max(25, lv+15) - keep 9+ cells away on foot,
  no targets within 12 cells - never a map veto, because rAthena puts 1-5 on nearly every field (prt_fild08 has 5) and a
  map-wide rule would close every beginner field. gef_fild03 deaths (31) were lv51-70 bots on a 40-56 map: not a
  picker error (two "Novice lv1" there are re-created wrong-build characters, levels read after the reset).
  Lesson: read who the killer is and what the victim was doing before blaming the map.
  Verified 12:50-12:55Z: she stayed in x 124-152, y 116-149 (before: 60,70 -> 135,125 in 5 min), 115 attacks in 6 min,
  party together. Re-anchoring on every "party is together" made the anchor follow her - don't.
- Follow-up (13:06Z): friends parked 24-27 cells from Ayla. Core followed only beyond 18 cells and measured from a
  stale leader position (a bot's last walk-destination of her, or humanLocs). Now (built, deploy pending): catch up
  beyond 15 (min(followSlack+3, together-5)), and humanState uses her status.json position when fresh (< 5 s) unless
  a walk packet seen after it is newer. The 13:02 split itself: OpenKore "Teleport due to dropping attack target
  already engaged" (teleportAuto_dropTargetEngaged 1) Fly-Winged her 72 cells away - now 0 (danger teleports stay).
  Fly Wings: items_control 601 keep 20 and store the rest (she carried 164, ~820 weight).
- 2026-11-14 (TOGETHER-RULE, part 2): after the deploy, leaders still started collects of members 4-14 cells away
  ("X wait there, coming" at 13 cells). The cause was that a bot's own b.X/b.Y is the START of its current walk until
  the walk ends (ai.go think snaps it to DestX/DestY), so a leader and a member walking toward each other each looked
  up to a segment behind: 5 cells really, 25+ on paper. Fix: Bot.livePos interpolates the walk (like Entity.pos), each
  bot publishes it (pubState.LX/LY), and every together/collect/regroup distance uses live positions on both sides.
  Lesson: any distance between two moving bots must use interpolated positions, never b.X/b.Y alone.
- 08:10: RODEX charges mail_attachment_price (2,500z) per attached item. Parcels of starter gear would have burnt the
  field bots' money. Lesson: read the server's fees/limits (conf/battle/*.conf) before designing a money flow.
- 08:20 deaths 3 -> 5.5/min after parties really moved up (07:44 deploy; split parties 30%, farming 0.71). Measured
  12:26-13:13 (dep12_ev.jsonl, 184 field deaths vs bots standing on each map): the mean-level gap was NOT the
  driver - the share of AGGRESSIVE monsters was: deaths per bot 0.06 (<20% aggressive), 0.40 (20-40%), 0.66
  (40-60%), 0.88 (60-80%), 6.0 (80%+): gef_fild03 Orc Warriors 92% 32 deaths / 3 bots, gef_fild10 96% 28/7,
  mjolnir_06 48% 20/24, prt_fild10 65% 17/31. The dying bots fought at "lv 45 vs 44": gear-poor lv60-70s rate ~45
  effective (power/level median 0.72 over 535 bots' wing events) - pickers use powerLv, the dashboard base level.
  Fix (ai.go partyMapTooHard, all four party pickers): mean monster level <= weakest POWER + 4 - aggressive%/5
  (was + 12, no aggression term): gef_fild03 now needs a weakest power of 55, mjolnir_06 31, moc_fild03 42. "Too
  easy" only when the weakest power is above every guide band's top + 5 (pickers stop choosing at +3: hysteresis, a
  farmable map isn't left for a small margin). Offline sweep (partysweep_test.go, 395 parties, real powers where
  known): parties on >=40% aggressive maps 82 -> 25, none without a fitting map, none on beginner fields; +0 would
  leave 15 parties with no fitting map. One-time re-plan wave: ~129 parties move after the deploy.
  Lesson: before tuning a threshold, find the variable that actually separates deaths from non-deaths (rate per
  exposure by band), and measure in the same units the code uses (effective level, not the dashboard's level).
  08:35 add-on (QA 12:46-13:16: deaths 138 vs 71, gef_fild03 23, gef_fild10 22, mjolnir_06 18 - 9 Mages, prt_fild10
  16, moc_fild03 13): party pickers also skip a map that is killing people NOW (partyMapDeadly: 5+ deaths in 30 min and
  >= 1 per 3 bots on it, danger.go deathsWithin), and a mage-family member counts 6 levels weaker (pubState.Fragile,
  partyFragileLv) in partyFitLv / partySpotNear / bestPartySpot. Swarm veto, boss notes and the leader's 2-death ban
  (leaderMem) already apply to every party pick.
- 2026-11-14 08:26 (AoE casters, owner rule "Mages and Wizards are AoE builds"): fight() only knew bestAttack, a
  single-target list, so no Mage/Wizard ever cast an area spell (0 sends of Fire Ball / Thunder Storm / Storm Gust /
  LoV / Meteor in 10 min of traces; Wizards with Storm Gust 5-8 cast only Jupitel; a Mage bolted 81 times with 34
  monsters around). Measured before (5.3 min, all online): Mage 4.9 kills/min/bot, Wizard 1.5 (!), Archer 7.5,
  Wizards 5 deaths / 13 bots. Fix (aoe.go aoeCast, called first in fight for the mage family): 3+ monsters within a
  spell's radius of one monster cell in range 9 -> best learned affordable spell (SG r4 > LoV r5 > Meteor r3 > Thunder
  Storm r2 > Heaven's Drive r2 > Fire Ball splash 2), ground spells via 0x0366; SP reserve max(15%, 30); own cooldowns
  so the server never refuses (0x0110 locks the skill for 60 s); no AoE on a group holding another player's / another
  party's monster (KS), solo no AoE on a boss/champion/+10 monster or a 7+ aggressive swarm; while hit with no party
  tank only spells <= 3.2 s. Solo Fire Wall 2 cells toward a monster walking in. Casters no longer walk in for a
  melee swing during any cast (was only bolts: a Jupitel/AoE cast was broken by closeIn). Plan: AoE order (Fire Ball,
  Thunder Storm, Fire Wall by 34 Mage points; Wizard HD/SG/LoV/Meteor), bolts 10 kept as a later fill so old bolt
  Mages stay within caps; Build.Legacy keeps the old plan for tools/wrongbuilds.py. Audit after: 0 wrong (132 Mages
  only "order differs", self-correcting: their next points go to Fire Ball / Thunder Storm).
  Lesson: a skill in the plan is worthless until the fight code has a rule that picks it - check both when adding one.
- Follow-up (13:35Z): "the priest went down the stairs alone" was not that. The ayla mind ordered `partylead goto
  pay_dun01` at 13:15; Ayla took the east stairs at 13:18:59 and the priest followed her map. At 13:22:50 Zen.fox showed
  up in Payon and back_tick led the party out (pay_dun01 > pay_dun00 > pay_arche) while the priest sat for SP and
  Echobunny lagged, leaving the priest 90+ cells behind. back_tick now leaves only with every other friend on her map
  within 20 cells (waits up to 2 min). The priest's "where r u Ayla?" (13:26:16) made her fetch it at once.
- Revision (08:30 local, after the 07:44 deploy): the plain 25-min cooldown was wrong - QA saw second hops of one
  trip refused ("took one 2 min ago, walking"), so a lv34 Hunter walked payon -> prontera -> prt_fild10 (lv50 mobs)
  and died 3 times; others walked 14-20 min. Now only repeats are refused: straight back to the town just left
  within `kafra_cooldown_min` (50), or more than `kafra_max_per_hour` (3) teleports an hour. Always allowed: a town
  run and the way back out from it (to any map), a respawn, a Fly/Butterfly Wing escape since the last teleport, a
  human-led party - and any teleport whose walking alternative crosses a map too strong for the bot or with 3+
  recent deaths (`kafraWalkSafe`). Lesson: a travel limiter must count trips, not hops, and must never trade a fare
  for a walk through deadly maps.
- 08:32 (owner rule: town runs use Butterfly Wings when affordable; bots stock utilities; built for the next batch):
  tripWingOK now wings to town whenever 2+ Butterfly Wings are carried (the last stays for escapes), however near
  the town; my earlier "3+ maps away" walk rule is gone. Restock: bwTarget 3 -> 4, fwTarget 5 (named); gearKeep adds
  the missing wing kit (wingKitCost) so gear never eats it (potions, then wings, then gear; skipped for a bot with no
  weapon / no class weapon). A gear trip goes via the Tool Dealer first when under 2 Butterfly Wings (with 1000z+).
  The return leg keeps the Kafra rule (kafraSaving). Test: wings_test.go TestTownRunWingAndKit.
  Lesson: the owner values time over zeny for travel; a money rule I wrote ("walk if near") was the opposite.
- Owner rule 08:30 ("a 50% weight should immediately trigger a town run") overrides the 07:35 damping: "bag is heavy"
  is a realNeed again for every bot and party member (may call the party run; one run serves all, members over the
  line sell on it). No sellable-amount minimum any more; only "anything to sell at all" (hasLoot), since a bag heavy
  only from gear gains nothing at a Tool Dealer. Butterfly Wing to town is already in startTrip (town.go tripWingOK).
- Follow-up (13:50Z): after the 13:33 Payon run partylead's departure wait held 8+ min "waiting for Zendancer84" with
  the priest 2 cells away. The 3-min timeout did fire (13:42:06 "set out from payon"), but the town branch at once
  started a new departure wait (everyone in town = "all done? lets go back") - an endless wait. And the "caught up"
  check used sight only (seen_dist). Now: 2-min hard timeout, no new departure wait in that town for 5 min, and the
  check uses friend_pos (sight, then Legion's API) with the API map. This run went to Payon on foot, so the Kafra
  return wasn't exercised. Zen.fox was stuck 2 min at pay_dun00 76-82,26 ("travel -> payon_in01", unstick x2).
- 08:50 guild03 leader Blaze.mage (Alchemist lv34-38) loop on mjolnir_02 (lv39 mobs, 66% aggressive): 4 mobs ->
  Butterfly Wing at 49% HP -> Geffen -> ~10 min walk back -> swarmed 26 s after arriving -> again; the party waited.
  (1) The leader is a member for every fit check (Members includes it; bestPartySpot gets [leader, members...]); with
  the aggression rule (built 08:35) mjolnir_02 needs a weakest power of 38 - a lv34-38 Alchemist (~27) fails it. Now
  the Alchemist line also counts partyFragileLv (6) weaker, like mages (fragileClass). (2) danger.go: in a party the
  losing-fight escape is a Fly Wing; a Butterfly Wing only for a far stronger monster at <50% HP, a boss, a Fly Wing
  that just landed in another losing fight at <30%, 4+ escapes in 2 min at <35%, or <15% HP with no Fly Wing (swarm
  map / 3 escapes alone no longer send a party member to its save town). (3) Its save point was right: Geffen is the
  nearest Kafra to mjolnir_02 by walking (offline routes: geffen 3 hops ~2140 cells, prontera 8 hops ~3660) - the
  10 min was the walk itself. But a party formed ON its hunting map never rallied, so nobody saved: rallyOnForm now
  also rallies ("saving at the Kafra nearest X") when a member's save town is clearly farther (townHops > nearest +
  1, the saveStillGood slack); moveTo already calls rallyOnForm on every map change.
- Follow-up (13:55Z): at the start of the 13:33 run Ayla walked pay_dun00 151,43 -> exit 21,173 in 40 s; Zen.fox (82,26)
  walked out alone and reached Payon 2.5 min late. partylead's town run now walks at the party's pace on fields and
  dungeons: after the first 15 s she stops while a friend on her map is > 20 cells away or still on a field/dungeon
  she left (sight, then Legion's API map/position), at most 3 min; friends already in town don't hold her.
  09:00 check (QA 13:40 old code: "xxyukifirexx keeps fleeing on moc_fild03: moving the party to gef_fild03", then a
  10+ min walk to the deadliest orc map): relocate's picker is partySpotNear, which now has the aggression rule,
  partyMapDeadly, the leader's death/flee bans and the fragile-class rating. Offline for TheShadowsenpai pt
  (Alchemist 64, Wizard 64, Priest 57, Alchemist 63, Rogue 62; powers ~0.72 x level): weakest rated lv39,
  gef_fild03 (mean 41, 92% aggressive) too hard; the relocation from moc_fild03 now picks moc_fild02 (next map).
  Long walks on relocation: partySpotNear takes the nearest fitting map by hops (<= 8); the leader's travel already
  calls kafraVia (Kafra v2 is another agent's change) - hop counts hide big maps, so a walking-time metric is open.
- Follow-up (09:00 local): kafraPlan / kafraVia compared routes by map changes only, so 3 desert maps or
  geffen -> mjolnir_02 (~2,100 path cells, ~10 min) looked cheap and leaders walked. Now they also compare walking
  time: `walkCells` = A* path cells to each warp of the route (cached per 10x10 block) x1.3, at 0.22 s/cell
  (calibrated on guild03's 10-min walk); the Kafra wins when it saves `kafra_min_save_sec` (120) - walk to the Kafra
  + ~15 s dialog + walk from the landing - and money/landing/loop limits allow it. Offline check:
  `LEGION_RATHENA=/rdata go test -run TestKafraWalkLive -v` (rathena data copied out of the ro-legion image).
  Lesson: "map changes" is not distance; RO maps range from 40 to 400 cells across.
- 2026-11-14 (REST-TOGETHER, owner rule "Rest together; no fidgeting"): while a member rests for HP/SP, the rest of
  the party used to keep moving (leader collecting/roaming, members following/regrouping, priests standing up for a
  leader 4 cells away). Now every bot publishes Resting (pubState), human leaders count as resting when seen sitting
  (0x08c8 action 2, or "sitting" in Ayla's status.json), and together.go restGuard holds everyone within 25 cells:
  fight what hits the party or comes within 5 cells of a rester, else sit if regen helps, else stand guard. No
  collect/regroup/roam/map change during a rest; a member in danger out of reach or a death still breaks it. Guards
  never count as resters themselves, so a rest can't keep itself alive.
- Morocc check (09:20 local): "no route from the Morocc Kafra" is NOT a router bug. In Renewal rAthena, Morocc is sealed
  on foot (Satan Morocc): its exits go to moc_fild07/12/19/20 and moc_ruins, which only reach each other and Comodo
  (moc_fild12 -> 18 -> cmd_fild09). The old moc_fild01/13/07/11 and prt_fild09/10 warps into moc_fild20 are commented
  out ("replaced with new NPCs") and became "Continental Guard" dialog NPCs (npc/re/quests/quests_morocc.txt) that
  send you to moc_fild20 208,207 (Incarnation of Morocc), and moc_fild20 leads back only through a guard to morocc
  160,61. The engine's image /rathena doesn't even carry npc/re/quests. So walking moc_fild01-03 <-> Morocc is
  impossible in this world; bots get in and out by Kafra (kafraPlan: here = 99 hops -> any Kafra dest wins;
  kafraHold lets an unwalkable trip through). nearestTown(moc_fild07/12/19, moc_ruins) = morocc, moc_fild01-03 =
  izlude/payon, which is right. Regression assertions are in kafrawalk_live_test.go. Don't add the guard dialogs as
  portals: they lead into a lv100+ field.

## 2026-11-14 build-and-strategy review (archer, acolyte, knight/crusader)
- A skill in the plan is not a skill in use. Check traces for each class: archers never cast Arrow Shower, knights
  never cast Two-Hand Quicken, hunters had Blitz Beat with no falcon, acolytes never used Heal on undead. Each
  learned skill needs a matching rule in fight() (crowdSkills / selfBuffs / healAttack).
- SP thresholds kill buffs. A generic "buff only above 60% SP" rule plus an attack skill that eats SP means the core
  buff is never cast. Mark the class's core buff `key` (30% floor) so it goes before the attack skills.
- Ranged classes fight from 9 cells: any "in the fight" check with a 3-cell radius silently disables them.
- Riding and falcons are rentals, not skills. Learning Riding / Falconry Mastery does nothing until the NPC rents the
  animal (peco.go breeder dialog; the falcon waits for legion.txt at the next rathena restart). In Renewal, ride only
  with Cavalier Mastery 5 (otherwise it costs ASPD).
- Worthless targets: plants and mushrooms (BaseExp 0, 5 HP) cost a lv49 archer a third of its time.
- Healer rest needs a hysteresis (sit < 40%, stand at 80%); "sit whenever SP isn't full" keeps a 1000-SP priest seated.
- Plan changes stay prefix-compatible: old skill goals go to Build.Legacy and old stat steps to Build.LegacyStats, so
  tools/wrongbuilds.py never flags existing characters (audit after the changes: wrong 0).
- 09:05 sweep the map (owner rule, RULES.md 3): random far cells made leaders circle a corner or bounce. patrol.go
  builds one closed patrol loop per map walk region (waypoints on a 24-cell grid, snapped to walkable cells >= 5 from
  any portal area, nearest-neighbour + 2-opt tour, cached); explore() walks it in one direction, skips a waypoint not
  reached in 45 s, detours to fresh sightings near the next 6 waypoints (or within 30 cells), and after a fight/rest
  rejoins at the nearest waypoint or the one after it - never a step back. Group pace: members follow 4-6 behind,
  beyond 20 cells the collect logic runs first, a resting member holds the party (together.go); no extra hold for a
  member within 20 cells (that would break the "together = 20" rule).
- Follow-up (14:10Z): rest + sweep in partylead.pl. (1) Rest together: while a friend rests on its own (API state
  "resting..."/sitting, not "...Ayla resting") or she rests (sitAuto), she sits too (sit, then clears
  sitAuto_forcedBySitCommand so auto-attack still protects them), random walk is off, and 'not moving'/'not coming'
  collects don't fire (danger / asked-for-help still do). Pitfall found live: core friends now sit "with the party (Ayla
  resting)" when status.json says she sits - counting those as a rest would keep both sides sitting forever; states
  mentioning Ayla are ignored. (2) Sweep: the 25-cell anchor made her circle one corner. Now a patrol loop: 20-cell
  grid of open cells (>= 18/25 walkable, 9+ from portals), nearest-neighbour order closed into a loop, driven from the
  random-walk hook (so holds, rests and fights pause it), unreachable waypoint dropped after 40 s, skip ahead (never
  back) to one of the next 3 waypoints with 3+ monsters. Live 13:59-14:05: pay_dun01 52,69 -> 31,94 -> 46,120 ->
  38,152 -> 62,182 -> 105,176 -> 142,194 -> 172,212 -> 165,240 -> 147,259, one direction, then held for the party.
- 09:12 sweep pacing: "never runs ahead" + "group's pace" + "within 20 = nobody holds" all hold when the leader
  paces instead of holding: stepAlongLimit shortens each walk segment until its end is within 9 cells of the slowest
  member within 20 (live positions) or no farther from it than the leader already is - always >= 1 cell, so the
  leader never stops and never turns back (patrol.go paceLimit/paceTest).
- 09:10 pricing: a new model must not keep the old formula as a floor (max(old, new)) - the old fixed prices were the
  insane part (boss cards 250k, high-level gear 450k). Look at the top of a new price table before switching it on.
- 09:14 (built for the next batch): party gear gifts went through the trade window: Echobunny stood "waiting for
  Zendancer84 to trade" for minutes while the priest fought 34 cells away, the party split and Ayla went to fetch.
  Now startGift mails the zeny (RODEX, 2% fee; the receiver collects on the unread-mail notice), from any free
  mate at any distance - nobody walks or waits. Any other face-to-face market deal of a party member in the field
  (not a town meeting, not a Fetch/Deliver deal) is dropped when the partner is over partyTradeNear (10) cells away
  or after 30 s; the market retries later. Test: giftmail_test.go. Lesson: hand-offs between party members must
  never cost the party its formation - use the channel that needs no meeting (mail) whenever it exists.

## 2026-11-14 09:15 - "Fly Wing did nothing, then died" on the orc fields (escape agent; built, not deployed)
- Still happening after the 09:03 deploy: 14:05-14:15 UTC 14 "did nothing" in 746 wing uses (13 Fly, 1 Butterfly),
  6 of the 14 bots died within 60 s. No noteleport mapflag on gef_fild03/10 (only nomemo/night/airship).
- Trace (vexcat, Acolyte line, gef_fild03, 8 Orcs on it): `SEND 0439` Fly Wing -> `01c8 ok=1` -> `0147 autorun 26`
  -> `SEND skill 28` (Heal, from healFirst) -> `SEND skill 26` -> no teleport. rAthena clif_parse_skill_toid: any
  skill that isn't the pending item skill sets sd->skillitem = 0, so the teleport cast that follows has level 0 and
  silently does nothing. The wing is not consumed (DelayConsume), so the bot "has" it and retries it later. 9 s earlier
  the same bot's wing worked because 26 went out before 28. Fighting classes cast every second on orc fields (Heal,
  Bash, bolts), so they lose the race most. Other silent refusals in the same code: sitting (pc_useitem
  delay_consume + pc_issit, and the cast), a cast bar running (ud.skilltimer), opt1 stun/freeze/sleep/stone.
- Fix (escapeitem.go, skills.go, bot.go, poison.go, danger.go): while a wing is pending (escHold, ~2 s plus any
  wait) useSkill sends nothing but skill 26 and sit() sends nothing; useEscapeItem stands up first and waits out our
  own cast bar (0x07fb, new b.castUntil); no wing while opt1 != 0 (b.opt1 from 0x0229); a refused use ack (0x01c8
  result 0) counts as a failure at once; a failure is skipped 3 s, not 30 (30 s after two in a minute), and clears
  escapeAt so escape() decides again at once; when the Fly Wing just failed, a Butterfly Wing is used under 50% HP.
  The "did nothing" event now says why (use refused / no teleport, sitting, casting, opt1). Test: escapeitem_test.go.
- Rule: an action that needs two packets in a row (item -> autorun cast, dialog step -> answer) must block every
  other sender in between. A goroutine racing the think loop will lose that race on busy maps.
- 2026-11-14 tanks trailing behind (owner: "tanks ... sitting behind"): the tank's peel ran after "keep my current
  target" for tank leaders, a non-tank leader always engaged first (the tank's pull skipped the leader's target), and an
  idle tank followed 4 cells behind the leader. Rule: the role that should take the first hit must get the first
  decision - peel before own target, leader defers fresh pulls to the tank briefly, tank stands at the group's front
  (tanklead.go). Measure role state shares before/after (party states sampler), not just "it fights sometimes".

## 2026-11-14 build review run 2 (bow users, assassins, merchants)
- "Bowless" can be "arrowless": LilRinkun (Archer lv51) had a Bow in the bag, 0 arrows, 46% weight (13 unidentified
  Clubs). Arrow buys shared the 45% potion weight cap -> 0 arrows every run, the bow is never equipped without
  arrows, and the unbuyable "out of potions" came first in needTrip. A bow user with no arrows now buys up to the
  50% line (200 minimum) and "no arrows" is checked before potions. Check the bag before blaming the weapon code.
- Bow users burn ~90 arrows a minute: poor Archers/Bards are back on a Knife 10-35 min after each restock (economy).
- Read the server source before "fixing" a skill: Back Stab has no direction check in Renewal (skill.cpp
  #ifndef RENEWAL); traces showed it hitting for ~1,400. The real class gap was elsewhere (Assassins had no katar, so
  Sonic Blow was never cast; the plan saved for an 8,500z Dirk before the katar).
- Skills that consume an item (Acid Terror: Acid Bottle, Demonstration: Fire Bottle) need an item check, or every
  cast is refused (progress.go skillItem).
- Songs need an instrument (Bard) / whip (Dancer) in hand: with bow-route bards no song AI can fire; deferred.
- 14:40 UTC Geffen north exit banned (guild02 bound for mjo_dun02, 14:34): kafraVia sent members from gef_fild04 back
  into Geffen for the Kafra (its estimate from the town spawn), kafraPlan refused on arrival ("no Kafra trip after
  all"), they walked out through 119,213 again, and the detector saw geffen, gef_fild04, geffen, gef_fild04 through the
  same door with both legs intended - and banned the walking route itself. Fix: a leg that leaves fewer map changes to
  the goal is travel, never a loop (warpLegProgress); a warp the best plan to the goal starts with is never banned,
  only re-planned (warpOnPlan); kafraVia only picks a town where a dry run of kafraPlan from the arrival cell accepts
  (kafraWouldTake). Lesson: two estimates of the same decision drift apart - make the planner call the real check.

## 2026-11-14 09:50 - beginners ping-pong pay_fild01 <-> pay_fild03; a Sage re-hit 3x crossing moc_fild03 (escape agent; built)
- QA 14:05-14:26 UTC: 25 A->B->A Kafra trips in 21 min, 123 "too dangerous" from 60 bots. Holy_lord (lv6-8, gear lv2):
  "Fly Wing: lost 35% HP (hp 31%, 1 on me, lv 6 vs 2)" x2 -> Butterfly Wing -> "too dangerous" -> Kafra -> repeat.
  Three escapes from ONE monster at its own level marked the map bad, and every wing escape (wingEvent ->
  escWingAt) counted as a Kafra "real need", which reopened the A->B->A block each time.
- Fix: fightView.even() = one attacker no more than 2 levels above max(base, gear) level. Under lv20 with 3+ potions and
  a weapon, an even fight never trips "lost HP fast" / weak / outmatched; only the last-resort lines (under 20-25%)
  leave. At any level an even-fight escape no longer counts toward b.escapes (the 3-escapes "too dangerous"
  rule, the party relocate) or the map's close-call danger, and uses no Butterfly Wing unless under 15% HP and solo.
  escWingAt is set only by a Butterfly Wing (a Fly Wing stays on the map), and kafraNeed ignores it below beginner_lv
  (home fields are next to town: walk).
- ThePuddinglord (Sage 63) set off from moc_fild03 at 52-57% after a Fly Wing and was hit again 3 times in 8 min.
  lowHPGuard now rests (or drinks) to 90% before walking on when travelling across a map with aggressive monsters near
  its level (mapDanger > 0) or after an escape on this map in the last 10 min.
- Rule: a threshold built for "is this bot about to die" must not also drive map bans and Kafra hops. Danger to the map
  needs more than one ordinary fight.
- Follow-up (14:50Z): (a) "everyone at 100% SP still sitting": Ayla sat via OpenKore sitAuto_idle 1 while holding,
  core friends sat "guarding Ayla (resting)", and the plugin counted her plain sitting as a rest -> self-sustaining.
  Fixes: sitAuto_idle 0 (config + eval); her own rest only with sitAuto or HP < 80% / SP < 40%; rest_sit runs before
  every hold path and stands her when no one needs regen; friend states mentioning Ayla never count as a rest.
  (b) Stale maps: back_tick held for "Echobunny (on payon)" while it stood beside her - every friend map read now
  prefers Legion's API (< 10 s old) over the party list (friend_map). (c) The 14:39 town run was not a real need:
  the priest asked "when we need town, can we do prontera?", Ayla's canned reply "next town run we go prontera"
  matched core's townRunLineRe and started a party run at 22-28% weight. Reply reworded; core rallyHeard ignores
  "next town run / next run / next time". Members of a human-led party no longer Butterfly-wing on a town run
  (tripWingOK) - they walk it with her (each winged off alone; Zen.fox without wings was left in pay_dun01).
  (d) Announced rests: she says "resting a bit" / "ok ready, lets go"; friends' "need sp / resting" start a rest and
  their "ready" ends it (3 min cap).
- Follow-up (14:58Z, owner override): town runs use a Butterfly Wing for everyone - the core "human party walks"
  clause is reverted (the "next town run" false-trigger fix stays). Ayla: saveMap_warpToBuyOrSell 1 (OpenKore uses a
  Butterfly Wing - teleport level 2 - for sell/buy/storage runs when not in the save map), buyAuto Butterfly Wing
  (no minAmount, maxAmount 5, Payon Tool Dealer) so every real run tops it up; she had 0 at 14:52. After shopping
  she walks to the town's Kafra and says once "meet me at the kafra, we go back together"; the departure wait then
  leads them out. Loot: itemsTakeAuto_party 1, partyAutoShareItem 1 (the party's in-game leader is Zen.fox, so the
  server option comes from core together.go partyLootShare - built by another agent). Errands: a friend's "brb ..."
  (renting/getting my/going to buy) makes Ayla ignore it (no hold/wait/fetch) until "back/omw/coming/done/ready" or
  15 min.

## 2026-11-14 10:00 - errands: necessary steps go alone (build review run 3; built)
- Steering the whole party run to Prontera for one member's Peco never happened in practice (0/93 Knights, 0/72
  Crusaders lv40+ riding): runs are rare (45 min gate), human-led parties pick their own town. Owner: a member leaves
  briefly for a necessary step. errand.go: idle member says "brb, renting a peco", solo trip, rents, comes back, "back";
  a published Errand flag keeps the leader from collecting / the party from resting or rallying for it.
- One flag on the published state, checked in the few shared predicates (needsCollect, partyRester, rallyMembers),
  beats special cases in every caller.
- 2026-11-14 (REST-ANNOUNCE): rests are announced. A bot's published Resting flag runs from its "need sp, resting a
  sec" line to its "ok ready" line (together.go restAnnounce; chat at most once per 45 s); Ayla's party lines drive
  hers (humanRestHeard: "resting"/"need sp" start, "ready"/"lets go"/"moving" end); her sitting is only a fallback
  for 60 s. Guards seated for a rest stand up at once after the last "ready". Lesson: state the party acts on should
  come from what members say, not from guessing at sit packets (friends guarded Ayla for minutes while she sat
  waiting for them).
- 2026-11-14 (REGROUP-FIRST): a party member on its way to the leader (other map: think's travel branch; same map
  >20 cells: party.go regroup) fights only monsters hitting it - hunt runs with attackersOnly (no new pulls, no
  looting, no roaming) - and while it still gets closer with HP >= 50% it walks on instead of stopping (Zendancer84
  fought pay_dun00 Zombies 6+ min; Echobunny fought Skeletons 58 cells from Ayla 5+ min).
- 2026-11-14 (LEADER-DETOUR): bot party members wait at the party's checkpoint Kafra town while the leader is on a
  town trip / heading elsewhere, or hasn't reached the checkpoint (or party map) yet; they stay where they are if
  already on the party map or on the leader's map. Published pubState.Trip makes a town trip visible (its goal map
  still says the party map). guild02's members had marched to mjo_dun02 while ThePuddinglord healed in Payon.
- 2026-11-14 (PARTY-LOOT): party option item pickup = Party Share (0x01e8 item1 = 1, and every 0x07d7 now sends
  exp 1, item 1, 0 - it used to reset pickup to "each take"); the in-game leader re-sends it every 30 min for
  existing parties. Members pick up any drop within 8 cells of them and collect_stop_cells of the leader. Party
  town runs may use the last Butterfly Wing (restocked in town).
- 2026-11-14 a tank's escape trigger is not a squishy's: generic "4+ monsters under 50% HP -> Fly Wing" made the party
  Knight teleport off the crowd it was supposed to hold and left the priest/wizard to die. Role-aware danger: a party
  tank with potions or a priest near holds and AoEs (tanklead.go tankHolds); only near-death / far-stronger / boss
  escapes. Keep SP for the AoE (no single-target Bash spending it to the floor).
- 10:10 after the 09:36 deploy: deaths way down (~84 / 30 min vs 150-197), but all-lv45+ parties on low fields back
  to 18 and moc_fild03 the top death map. Offline (TestZZWhy, fresh snapshot, powers from wing events: median 0.71 x
  level): those lv45-50 parties are gear-poor Thieves/Merchants/Mages rated lv24-33 effective, so prt_fild07 /
  pay_fild02/06/08 really were "safe" for them - but give almost no exp for their BASE level. Fix: mapExpPoor (every
  guide band of the map tops out 15+ under the weakest BASE level) - partyMapNear / partySpotNear search maps with
  real exp first (tier 0), any safe map only if none fits; partyFitCheck moves a party off an exp-poor map when a
  safe real-exp map exists; bestPartySpot scores exp-poor maps -15. moc_fild03 (mean 42, 20% aggressive) passed the
  aggression term at power 42, but its common Argos are lv48 aggressive: partyMapTooHard also requires the strongest
  common (5%+) aggressive monster <= weakest power + 2 (moc_fild03 now needs 46, gef_fild03 43, mjolnir_02 36);
  equally near pickers prefer passive maps (40%+ aggressive = +2 map changes). Sweep (383 parties, real powers where
  known): all-lv45+ on low fields 40 -> 0; parties on 40%+ aggressive maps 12 -> 4; none without a fitting map. A
  one-time re-plan wave (~200 parties in the sweep's approximation) follows the deploy.
  Lesson: "too easy" has two meanings - for SAFETY judge the effective level, for EXP judge the base level (Renewal's
  penalty uses base levels); a picker needs both.
- Follow-up (15:08Z): "sitting around way too much" - route_randomWalk was left at 0 by a rest across plugin reloads,
  so the sweep (driven only by OpenKore's random walk) never ran; only the idle side-step moved her. Now sweep_tick()
  drives the loop from the plugin whenever she is idle on the hunting map, not holding, nobody resting; steps are
  at most 12 cells and only while every friend on the map is within 12 (pace_ok). Own rest: sitAuto 50/80 HP,
  25/50 SP; she sits only for her own need - for a friend's rest she stays put and fights. Protect first:
  protect_tick() switches her target every second to the nearest monster within 15 cells that hit/missed/cast on a
  party member (OpenKore ranks monsters on HER before party monsters). Live 14:58-15:07: 75 attacks / 47 kills in
  ~8 min, 16 "protecting the party" switches, one 1-min rest at SP 25% -> 49%.

## 2026-11-14 10:10 - build review run 3: songs, healer sit, Sage/Monk orders, learned bolts (built)
- A skill plan's order is the real build: 66/75 Monks sat at Triple Attack 10 with no Chain Combo (Triple 10 came
  first), 25/26 Bards had no song (Music Lessons 10 + Musical Strike 5 came first, job levels 18-29). 2nd-job skills
  aren't wrong-build checked, so the useful skills can move to the front freely (old order kept in Legacy).
- Check the server source for performer rules: songs are Self-target, need an instrument/whip in hand, block normal
  attacks (SC_DANCING), end on unequip; Musical Strike / Throw Arrow are AllowWhenPerforming.
- Elemental bolt choice must respect learned skills: Cold/Lightning Bolt was requested from Mages that never learned
  it (refused, 60 s skill block); Wind monsters now get Earth Spike from Wizards/Sages.

## 2026-11-14 10:30 - server cap at transcendent classes (built; rathena part waits for a restart)
- "Cap the server" means the game server, not bot behaviour: unload the job-change NPCs, don't just write a rule.
- rAthena reads conf/import/map_conf.txt BEFORE npc/re/scripts_main.conf, so `delnpc:` there removes nothing. The last
  file in the NPC list is npc/scripts_custom.conf: mount an override of it with the `delnpc:` lines.
- legion.txt had promoted trans 2nd jobs at 99/70 straight into 3rd jobs; check custom scripts for class changes
  whenever the class ladder changes (`grep -rn jobchange npc/`).
- A script change can be checked without touching the live server: map-server `--run-once` in a throwaway container
  on a private docker network with its own MariaDB (alias `db`, main.sql imported). Never point it at ro-db (mapreg
  saves on shutdown would overwrite live server variables).
- Trans skill plans live in a separate list (transplans.go) used only for trans classes, so the 1st/2nd-job plans
  (and the wrong-build audit that reads them) don't move.

- 2026-11-14 (AYLA-BREATHER): the whole party rested ~3x in 6 min because Ayla (Assassin, ~330 SP) burned SP on
  Sonic Blow down to her 25% sitAuto line, and every rest of hers made the friends guard/sit (kills stalled). Fix:
  Sonic Blow only above 40% SP (down to 25% with 3+ monsters on her or a tough/boss target), sitAuto SP 10/40
  (HP 50/80 unchanged), Red Potion at < 35% HP, Blue Potion/Grape Juice/Grape at < 12% SP if she ever holds any;
  her rest line is now "sitting a sec, keep killing near me". Core: together.go restFarmFor - when the resting
  member is the party leader (human or bot) and not a healer, non-healer guards keep killing any non-boss monster
  within 15 cells of it (restGuard step 1b); healers keep the old sit/guard hold. Lesson: a skill-user's SP floor
  belongs on the skill, not on the rest line - drain to the rest line and the whole party pays for it.
- 10:20 roaming after the sweep went live (sample 10:14-10:17 right after the 10:11 restart: 13,975 bot-samples,
  roaming 4.5%, 90% of it party leaders): leaders seeing 0 monsters 26%, only claimed monsters 20%, a takeable
  monster in view 18%, only far monsters 13%, only plants (worthless, skipped by design) 13%. The "takeable in view"
  case was the champion avoidance: a wandering champion (Solid Coco gef_fild02, Bigfoot Ringleader pay_fild07, Solid
  Horn pay_fild09...) left 10-minute 12-cell no-target circles behind it. Fixes (built 10:23): champion notes hold 30 s
  (kill 2 min); a leader/bot may join a monster claimed by its own party member; the sweep walks to a takeable monster
  already in view beyond the 14-cell pick range before the next waypoint; paced steps are >= 3 cells (a 1-cell crawl
  halved the sweep speed); sightings skip claimed/busy monsters; /api/bots/<name> mobs now say "worthless" and
  "boss spot" so the API's "ok" matches what hunt() really takes. Lesson: when a diagnostic says "ok" but the bot
  doesn't act, the diagnostic is missing a rule - make it mirror the real filter before drawing conclusions.
- Cap exemptions (10:35 local, QA: guild02's priest Holy Aco "3 teleports in the last hour already, walking" toward
  mjo_dun02 alone across aggressive maps while its party moved): the hourly cap (`kafra_max_per_hour`) no longer
  applies to (a) a member heading to its bot party's map (party move / regroup), (b) a leader taking its party to the
  map it picked (guild leader relocations), (c) travel after a respawn or a wing escape (`kafraCapExempt`). Those
  still get the A -> B -> A block; respawn and wing escape no longer lift it (they used to be full exemptions).
  Lesson: a limiter on a party member must look at what the party does, not only at the member's own count.
- 2026-11-14 (STRANDED-MEMBER): after a relog, a human-led member didn't know where Ayla was (no party packet / sight
  yet) and sat "waiting for Ayla" on pay_dun00, then fought Zombies there. humanState now falls back to her fresh
  status.json; a member that doesn't know where she is asks "where r u Ayla? im at X" after 30 s (away on another
  map: after 45 s); a travelling party member walks on past attackers while it still makes headway (HP >= 50%).
- 2026-11-14 (SET-OUT-TOGETHER): when a bot party changes map, members on the leader's map stay with it and follow it
  portal by portal (detourHold follow), the leader waits on each map for members there beyond 20 cells (90 s cap),
  goes via the checkpoint Kafra town to save once per party map, and members waiting in the checkpoint town save
  there too. An explicit party map change (mind call) bypasses the 60 s goal hold. guild02's Sage leader had crossed
  the Mjolnir maps alone with a Payon save point and died.

## 2026-11-14 10:45 - economy run 7: vendor-alchemist, mail trades, parcels to open shops, party hand-offs (built)
- A role that owns the bot (vending) silently disables every other system gated on "not vending": guild07's alchemist
  is a vendor, so it sat at 102.9k forever and its bottle trip deadlocked (vendThink ignored trip Why "alchemy").
  vend.go now closes the shop (max 1x / 20 min) for the guide, brewing or supplies (alchShopClose); brewers never vend
  herbs/Scell/bottles/bowls. Check every new "x owns the bot" state against the systems that wait for it.
- Rounding a gift DOWN to 100z plus a 300z minimum made the last <300z of a target ungiftable: Blaze.mage 102,919 and
  lilzensenpaiVend 102,933 stuck under the 103k guide errand. Round the final gap UP.
- Parcels: the send path works at 100z/item (0x0b3f acks, DB rows with attachments), but the receivers were vendor
  characters on accounts that never log in (last_login NULL). A mail target must be a character that is online now:
  parcelTarget = a guild mate's open Prontera shop only; vending characters CAN collect (rathena getattach has no
  vending check). Junk gear is never parcelled.
- Trades by mail (mailtrade.go, tuning mail_trades / mail_trade_test_bots, off until deployed and tested): cards/gear
  worth 1,000z+ between far-apart players - seller mails the item (100z), buyer mails the price (+2%) once it is sent.
  At 15:30 every card bid was >= the lowest ask; misses were all distance (too far 8k vs price 201).
- Failed deals > completed: 28/40 "party mate too far" - matched within 10 cells, killed by the same 10-cell check
  seconds later while they walked together (and partyTradeOK allowed any distance in town, marketThink didn't). The
  hand-off now survives within 20 cells (together.go) and anywhere in a town (90 s). Matcher and executor must use the
  same rule.
- #trade offered 674 Knives / 130 Swords / 37 Clubs (junk gear) - econSpare now skips junkGear like the shops do.
- Money flow without zenylog: price rathena's picklog (type S = NPC shop in/out, M/P = drops/pickups, C = used) with
  the RE item db (docker exec ro-rathena cat /rathena/db/re/item_db_*.yml). 14:53-15:53: NPC loot sales 2.0M/h, NPC
  buys 2.2M/h, of which Butterfly Wings 697k/h - a Butterfly Wing is 1,000z in RE (code comments assumed 300), and
  bwTarget 4 was topped up from every spare zeny. Kafra fares only ~115k/h at 120-180z (party runs dragged to Prontera for any
  member's plain gear piece). Purses stayed flat (~917z median at lv60-79). Wings now follow a saving floor
  (1,000 + 40 x lv: 0/1/2 wings, 4 only at 30k+); a plain gear piece diverts the party to Prontera only if it costs
  >= 800 x members (Kafra fares are 120-180z here, not 1,200). Always check real item prices in the server's db, not remembered pre-RE prices.

## 2026-11-14 11:15 - build review run 4: 8 more variants, next-piece logic, Riding moved up (built, not deployed)
- Variants (builds.go, Base = main plan; gear tables in buildgear.json; CLASSES.md sections): sage_support, monk_asura
  (weaponskill.go asuraBurst: Fury -> Asura Strike at 85%+ SP on a boss / HP >= 20 x base level), alchemist_battle,
  alchemist_potion (alchemy.go +500 pick score as guild brewer), blacksmith_mace, assassin_sonic, assassin_dagger
  (Enchant Poison self-buff, skillbuff.go), rogue_bow (gear style "bow"; Double Strafe in the Thief offense list).
  No Wizard bolt variant (owner AoE rule), no forge Blacksmith (no forge AI).
- Check every plan against the server's skill tree before shipping: db/re/skill_tree.yml has Renewal prerequisites that
  differ from memory (Iron Fists needs Divine Protection 10 + Demon Bane 10; Fire Ball needs Fire Bolt 4; Earth Spike
  needs Endow Quake 1). A plan goal the server won't offer is skipped silently - the build quietly loses its core.
- Peco: only 24/105 Knights and 4/81 Crusaders had Riding because it sat at job ~26 / ~38 while the median Knight is
  job 19. Riding + Cavalier Mastery 5 now right after Two-Hand Quicken / Holy Cross (job ~11-17 / ~18-24). Then
  poverty: 12 Knights / 4 Crusaders hold the rental + 1,000z. A plan position decides who gets a feature: check the
  job-level distribution, not just "is it in the plan".
- Step 2: nextBuildPiece (buildgear.go) + GET /api/buildgear/next: each bot's next item, its source (shop / drop /
  trade) and drop maps; the economy side decides farming / buying / #trade.
- wrongbuilds audit after the change: 0 wrong (order-only 542, harmless).
- 11:20 map-choice batch (QA 15:20-15:58, old code). (1) Crowding: pay_fild07 48 bots / 24 roaming with 0 monsters
  seen, gef_fild02 82, pay_fild09 71, moc_fild01 67 - the party pickers fell back to the NEAREST fitting map when the
  room pass found nothing. Now partyMapNear runs stages room-first (strict real-exp, relaxed passive real-exp, any
  safe), then the same without room choosing the LEAST crowded (+1 map change per 50% over the cap); partySpotNear
  likewise; maps 9+ map changes away are never a party pick; partyFitCheck moves a party off a crowded map
  (mapCrowded) when a fitting map with room exists ("X is packed"). Bug found: partyFitCheck's alternative checks
  measured from (town, 0, 0) - an invalid cell, every route "unreachable" - so the exp-poor move never fired in
  production; now from the town's spawn point. Pure searches no longer reserve crowd seats (only the actual pick).
  (2) guild06: the mind asked for gef_fild03 (too hard); formParty re-fitted it measuring from Geffen's side and the
  leader on gef_fild01 walked 15 min to pay_fild09 - partyMapFor now measures from the leader's position. The party
  then vanished at 15:53 with no event: bot.go disbanded the WHOLE party whenever any member's session ended (play
  schedule). Now the member drops out (Party.drop: the party ends only below 2 members or when the leader leaves),
  and leaderPartyManager's top-up re-form keeps the party's map. (3) mjolnir_02 top death map (46 deaths 15:20-15:58):
  the dead fought "lv 34 vs 39..51" - effective levels ABOVE the monsters - with 10-31 monsters on them: train maps.
  partyMapTooHard: 100+ aggressive spawns need the weakest member 14 over the strongest common aggressive monster
  (mjolnir_02 52, prt_fild10 48, gef_fild03 59, mjolnir_06 40, prt_fild03 52, mjolnir_10 61). (4) Gear-poor lv45+
  parties with no strict real-exp map: partyRelaxedPassive lets a < 20% aggressive real-exp map sit a little over
  them (band start +6, mean +8, 35% filter share), partyMapOK keeps such a map (no ping-pong). Sweep (332 parties,
  only ONLINE members count - offline members read lv1 in /api/parties/detail): all-lv45+ parties on low fields 7 ->
  0, on 40%+ aggressive maps 8 -> 5, none without a map, 99 moves (60 exp-poor moves). Sweep pitfalls fixed: offline
  members, nobody-online parties, pubs with base-level powers, seat reservations from checks.

## 2026-11-14 10:10 - a party member rested alone after a Fly Wing and died (core, Zen.fox on pay_dun01; built)
- Zen.fox (Wizard) at 10% HP "resting before moving on" at 160,58 while Ayla, Echobunny and the priest were at ~236,70.
  A Fly Wing lands on a random cell of the same map, and lowHPGuard sat down wherever that was.
- Fix (heal.go restAnchor + lowHPGuard, shared code): a party member at low HP with its party's healer (else its leader)
  on the same map and more than 10 cells away doesn't sit. It stands, drinks under 50% HP, and walks back
  (regroupStep, which knows other walk regions) to rest next to them. With nobody of the party on the map, or as the
  leader, it rests where it is as before.
- Rule: an escape that moves a party member (Fly Wing, running) must end with it back next to the party. "Safe" means
  next to the healer, not "nothing hitting me this second".
- 2026-11-14 (SAVE-KNOWN): the engine forgot every save point on restart (savedTown empty, then assumed = home), so
  the checkpoint save detour fired on a guess: guild05's leader left gef_fild09 for Geffen and back without saving.
  Save points are now remembered per character in data/memory/<account>.json ("saved_town", written on every Kafra
  save / respawn); the detour runs only with a known save point and never while the party is already on its map.
  Members only say "waiting in <town>" when they really head to that town.
- 16:02 deploy check: mail trades ~25/25 in 10 min, parcels ~40 all collected by open guild shops, same-map deals
  23 done / 2 failed (was 19 / 29). parcels_enabled + mail_trades on for all at 16:10.
- A guard like "return false while b.kafra != nil" at the top of a subsystem that itself starts Kafra trips (dealer
  trip -> travel -> kafraPlan) deadlocks it: nothing else drives the Kafra trip in goal mode idle (Blaze.mage "idle in
  payon" with the guild's 103k). A subsystem that starts a sub-trip must keep driving it.
- "Needs X" flags that turn false once the money is in (needsGuide at >= 103k) unlock the money for everything else
  (gear_next showed a 34k Buster "affordable"). Lock the purpose (guideSaving) until the purchase is made.
- Parcel value floor: RODEX charges 100z/item, so only items worth 4x that (400z+) are mailed; 4-slot Clubs/Swords
  (~120z) are not junk by the rule but not worth a stamp either.

## 2026-11-14 11:30 - Zen.fox (core) died twice in pay_dun01: Fly Wing away from the party, then a 45% respawn walk (built)
- 16:12:52 UTC "teleported away from 3 monsters" -> 16:13:03 died (Drainliar): the Fly Wing put it on a random cell
  ~80 cells from Ayla's party among pay_dun01's aggressive Drainliars. That explains the 80 cells at the 1st death.
  In a dungeon a Fly Wing is a gamble; the party (healer, tank) is the safe place.
- 16:20 UTC: respawned in Payon, rested only to the 45% town line (the 90% "before travel" rule skipped towns),
  walked through pay_dun01 alone, died 55 cells short of Ayla.
- Fix: escape() runs TO the party's healer (else leader) when it is 3-15 cells away (b.fleeTo; survive keeps walking
  there) instead of a Fly Wing, unless under 15% HP or a monster 9+ levels above. After a death (10 min) on the way to
  another map or to a party elsewhere, lowHPGuard rests to 90% HP (and 90% SP for mage/acolyte lines), in town too.
  A party member in a dungeon more than together_cells from its party counts as transit (losingFight leaves at 3+, or
  2+ under 70%; no clearing the way), and says once "omw, at x,y - come get me pls" in party chat. heal.go partyAnchor
  is the shared "who do I stay next to" helper (restAnchor, escape, rejoin). Test: TestRejoinAfterDeath.

- 2026-11-14 need-driven parties (lfg.go): parties used to be permanent (90-min refresh, restored after every deploy;
  352 parties, 66% pairs, 44% with a healer/Monk). Owner: "no permanent party, party should come from need". Each bot
  now gets a farm goal from its build (gear/card drop map, zeny, level; healers "heal"), posts LFG/LFM on #global under
  a server-wide budget (6/min), and parties form only when one side posted and the goal/map/level band fit; they end
  when goals are met or diverge (3 h cap). Lessons while building: (1) an item-goal map is usually "too easy" for
  partyMapOK - anchored parties need a safety-only check (goalMapSafe) or the fit checks move them off the drop map;
  (2) never dissolve all restored parties at once after a switch-on - stagger (20-70 min) or ~900 bots re-route
  together; (3) ship a big behaviour change behind a tuning flag defaulting off so a batched deploy can't break.
- Chained Kafra + never walk through deadly maps (11:35 local, QA: guild07's lilzensenpaiVend, Alchemist lv61 with
  power lv31, walked prontera -> mjolnir_09/08/04/05/12 -> Al De Baran for the Alchemist Union, died to a lv59 Flora
  on mjolnir_08, respawned in Prontera and walked the same way again). kafraPlan looked at ONE teleport + walking:
  Prontera has no Al De Baran Kafra, and Geffen -> Al De Baran on foot is longer than the walk, so it walked. Now
  `kafraOnward` also counts one more Kafra teleport at the landing town (prontera -> Geffen -> Al De Baran,
  fares for both legs kept), in kafraPlan, kafraTeleCost and kafraTeleCells (so kafraVia from the fields sees it too);
  and when the walk crosses a map too strong for the bot (`routeSafeFrom`: mapTooStrong / 3+ recent deaths; position-
  keyed cache, replaces the per-goal kafraWalkSafe cache that kafraWouldTake's virtual moves confused) a teleport
  whose onward way is safe wins even if up to 2 map changes longer. Offline: TestKafraChainLive (prontera ->
  alde_alche / aldebaran / yuno all take Geffen; geffen -> alde_alche takes Al De Baran). A scan of every Kafra town x
  18 goals x lv 25/45/70 left only goals that are themselves too strong (mjo_dun02 at lv25-45, orcsdun01) and
  3-teleport trips to Comodo/Umbala.
- 2026-11-14 (HEALER-SAFETY): after the 11:02 deploy party healer deaths went 4 -> 28 in ~25 min. The last state
  before death was mostly "fighting Muka / Peco Peco / Orc Warrior" (a party healer with nothing to heal fell
  through to hunt and pulled on its own), travelling/walking on while hit, and once "sitting by" its leader. Now a
  party member healer's hunt runs attackersOnly (never pulls), it sits (idle sit, SP rest, rest-hold) only with no
  threat within 7 cells (aggressive monster, or any monster on us / a member / that hit us), stands at a threat within
  5 cells, doesn't trade blows in restGuard, and walks on when hit while travelling/regrouping (self-heal + wing
  escape) unless blocked. Rests shorter than 4 s are no longer announced ("resting" + "done resting" 1 s apart).

## 2026-11-14 11:45 - party tanks still Fly-Winging at 33-48% after the 11:02 deploy (escape agent; built)
- dep1045 data (16:02-16:28 UTC): "losing: N monsters" Fly Wings above 20% HP by party members: Monk 28, Wizard 14,
  Mage 10, ... Top: LilLilyblade (Monk 69) 14, Sunheart (Monk 64) 11. That deploy predates tanklead.go tankStands
  (11:22), which now holds a party tank (Monk/dodge Thief included) while it has potions or a priest within 14 cells,
  so most of these are covered by the next batch (tests: TestTankHoldsSamples).
- Two leftovers fixed in danger.go: (1) the transit rule (swarm map / rejoining) left at 2 attackers under 70%:
  LilLilyblade flew from 2 Spores 18 levels under it at 68%, 6 times in 2 min; now 2 attackers need < 50% HP or a
  15% drop (3+ still leave). (2) Sunheart, out of potions and SP and walking to town, used a Fly Wing every 5 s for
  3 min at 25-33% against one lv47 monster; after 6 escapes in 2 min a Fly Wing is used only under 25%.
  fightView.tank now means partyRole == roleTank (Monks were rated like casters for the "lost HP fast" line).

## 2026-11-14 11:55 - wing "use refused", warp loops from teleport legs, legionctl nearby (escape agent; built)
- Onimage (Wizard) 15:24:50 UTC "Fly Wing did nothing (use refused; sitting false, casting false, opt1 0)": 0.3 s
  after the use, then a Butterfly Wing worked at once. The only refusal left in pc_isUseitem/pc_useitem is
  canuseitem_tick (item_use_interval 100 ms): survive() drinks a potion every second under the potion line, and one
  went out just before the wing. 16:16 UTC Onimage "no teleport": ground skills (0x0366 Storm Gust / Fire Wall /
  Hammer Fall, sent straight from aoe.go / weaponskill.go / skillbuff.go) bypassed the useSkill hold. Fix: b.send drops
  0x0366 and any 0x0439 but the wing's own index while a wing is pending (escBlocks), and useEscapeItem pushes potAt
  past the hold. Test: TestEscBlocksPotionAndGroundSkill.
- Warp loops: LilMoonsage Bless (lv6) "warp loop pay_fild03 <-> alberta" at 16:12 was a shop trip after a Butterfly
  Wing / Kafra leg; warpTaken() finds no portal for a teleport, and an unknown portal counted as "the same door".
  ai.go now marks a map change within 10 s of a Butterfly Wing, 5 s of a Kafra hop or 20 s of a respawn as via
  "tele", and warpLoopLegs ignores those. crimson_x1 geffen <-> gef_fild04 (15:39) predates the warpLegProgress
  deploy. Test: TestWarpLoopIgnoresTeleports.
- legionctl nearby: no error reproduced on any field map from Git Bash or PowerShell. Hardened anyway: map names are
  normalised (case, .gat, trailing words: "pay_fild07 (Payon field)"), missing/null fields don't crash the print, and
  an empty map lists the busy maps with the same prefix.
- 11:40 follow-up (run 4): 0 song casts - all 22 partied singers had no instrument; the Dancer leader Ashcat97 had the
  zeny but leaders never take errands and the bow-only gear route never bought a whip on runs. The song weapon is now
  the singer's class step (classWeaponNeed) and a savings goal (gearHold); the same savings hold for an unmounted
  rider with Riding (pecoSaving). Live 16:39Z: TheCrimsonfox77 / Moonchan "brb, buying a Violin", Moonchan cast
  Bragi (321) within 5 min. Lesson: a build step that isn't on the gear route and isn't a "need" never gets money.
- First live /api/buildgear/next: 235/281 bots pointed at drop-less end-game items ("trade") - the max-gain pick is
  the dream item, not the next step. Now: obtainable first (shop / drop near level / offered now), then the cheapest
  piece with >= 1/3 of the best weighted gain.
- Traces 16:40-16:45Z (14 bots): Knights Bowling Bash 13-14 + THQ + Magnum; Crusader Holy Cross 29 + Auto Guard;
  Wizard Thunderstorm 8 + bolts; Hunter DS 43 + Arrow Shower; Blacksmith Hammer Fall 21; Monk Chain Combo 53 (no Combo
  Finish learned yet); priests healed but cast no Kyrie in 5 min; Assassin Envenom only (Main Gauche, saving 19,500z
  for a Jur - poverty).

## 2026-11-14 11:50 - economy run 8: weapon errand, guide fund raided by a gear turn, purse floor, wing price (built)
- A fund lock must cover EVERY spender: guideSaving() was wired into gearReserve in batch 3, but the old image's party
  gear turn (gearpool.go spareZeny) mailed 65,750z of Blaze.mage's 103k guide fund to a party mate for a Hunter Bow
  20 s before the deploy. Lock money at the one helper all spenders share (gearReserve), not per spender.
- "Waits for the next party run: class weapon" (59-85 lines / 10 min) meant hours on a Knife: party runs only come
  from real needs. The owner's errand rule fits: errand.go "weapon" - 15 errands in the first 6 min, each bought its
  Sword/Staff/Axe in 1-4 min and came back. Errands start only from field maps (the walk back alone through a dungeon
  is where members die). Test bots have no rng: guard b.rng in shared helpers (gearStepReady panicked a test).
- Purses stay near 1k because every restock topped HP potions up to 30-60 with everything above 500z (median 1,049z
  left, 169/1,009 under 300z). The top-up above the survival minimum now keeps 1,000 + 40 x lv.
- A price the engine reads from the item db goes stale when the server's db changes (db-import override, live only at
  a rathena recreate): budget with what the shop actually lists (wingPriceSeen) and use the db only until then.
- The events API keeps only ~3 min of events: run a logger (scratchpad ec8_ev.py) to measure anything over time.
- 2026-11-14 (ERRAND-LEADER / WING-SAVE): guild05's leader went on an Alchemist-guide trip; my detour rule then sent
  its party to wait in Geffen town. A leader on an errand (pubState.Errand: errand.go or an alchemy goal) is not a
  detour - the party keeps farming its map. Butterfly-Wing Kafra routing now uses only a KNOWN save point
  (knownSave), and a Butterfly Wing landing in a town records it as the save point.
- Second hops and the way back (11:55 local; the 11:36 chain build WAS in the 11:37 deploy): (a) lilzensenpaiVend
  landed in Al De Baran by Kafra at 16:43:55Z, bought at the Alchemist Union in 45 s and walked into mjolnir_12 (died)
  - kafraThink set kafraNext = now+2 min on every landing, so kafraPlan/kafraVia refused the way back (the 15 s
  exception covered only b.trip and human parties; alchemy errands aren't trips). (b) TheShadowsenpai landed in Geffen
  for the Geffen -> Al De Baran hop, the same 2 min sent it walking out to gef_fild00, and from there kafraVia picked a
  Butterfly Wing to its save town Prontera (2% fewer estimated cells than walking back to Geffen) - prontera -> Geffen
  -> Prontera. Fix: 15 s after every landing (kafraHold guards loops), and the wing option must beat the nearest-town
  option by kafra_min_save_sec and never flies back to the town the last teleport left within kafra_cooldown_min.
  Tests: TestKafraNoWingBackLive. Lesson: a blanket "no plans for N minutes" after an action silently breaks every
  multi-step plan that needs the next step at once.

- 2026-11-14 Wizard idle in a crowd (owner watching Zen.fox, lv75 Wizard, Ayla's party, pay_dun01: "big crowd and the
  wiz not doing anything"). Trace: 2 Storm Gusts and ~12 single-target Jupitel Thunders in 70 s while monsters died
  in clumps around the Knight. Zen.fox has SG 10 / Fire Ball 5 / Thunderstorm 4 / HD 3, SP 1174/1357, so not the
  build. Causes in aoe.go: (1) "friend" meant only accounts the server had sent party HP (0x080e) for - a monster held
  by the Knight or hitting Ayla counted as someone else's fight (KS guard) and vetoed the whole cluster; (2) centres
  were only monster cells, so a crowd split in two clumps never qualified; (3) 2 strong monsters never counted.
  Fix: partyFriend() = self + party members' aids + the human's AID + party HP (also used by weaponskill.go
  viewMobs for every class's crowd skills); every walkable, in-line cell within range 9 is scored by monsters covered
  (element-weighted: a crowd resisting the element gets the next spell; Fire burns Undead); 2+ with a strong monster
  qualifies; Fire Wall also in a party when being hit; traced bots log "AOE none: N mobs near (F foreign)" when a crowd
  gets no spell. Tests: TestAoEMostImpactfulCell, TestAoEStrongPairAndElement, TestAoEPartyFriendWithoutHP. Lesson: a
  KS guard keyed on a partial party list silently disables a whole class's AoE.

- 2026-11-14 Wizard lure (owner: "if party has a wiz, they should follow that strategy, lure for wiz to cast spell and
  pick off the rest"). tanklead.go tankWizardLure, first step of tankFallBack: a party tank with 3+ monsters on it and
  an area caster in the party (pubState.AoE = Mage line with an area spell) brings the pack to a spot 4 cells in front
  of the caster (zone 2-7 cells: inside cast range 9, never on top of it) and fights there; while the caster's spell
  is in the air (pubState.CastUntil, set by aoeCast to the cast time) and the tank is within 9 cells, it holds still
  instead of relocating. No caster near -> the old priest fall-back. Tests: TestWizLureSpot, TestTankWizardLure.
  Not yet: other melee members don't hold off the pack before the spell lands.

## 2026-11-14 12:10 - Zen.fox 4th death and the priest 80 cells off after a relog (core, pay_dun01; built)
- Diary + TRACE: both logged in at 16:59:55 UTC inside a skeleton pack. Zen.fox (24 cells from Ayla) Fly-Winged
  at once (its last Fly Wing; DB: 0 HP potions, 0 Butterfly Wings) and landed at 244,240, ~180 cells off. It stood
  "waiting for Ayla" (her position not known yet after the relog, so partyAnchor/rejoin couldn't see her). Then
  "running away (3 monsters)": one 4-cell walk into a dead end, and runFrom found no walkable cell straight away, so it
  sent nothing for 12 s under 3 Soldier Skeletons and died. The priest Fly-Winged out of 6 at 17:00:06: that's the
  priest 80 cells off; later it ran from 8 on pay_dun00 for 2+ min without a word.
- Fix (danger.go, heal.go): fleeCell picks the best walkable cell 3-10 steps away in any direction (straight away
  preferred, same walk region); in a dungeon escape() runs to the party's healer/leader from up to 40 cells (was 15)
  instead of a Fly Wing, walking the FindPath path 12 steps at a time (towardOnPath). Running from 3+ in a party calls
  for help in party chat (every 45 s, with map and cell). rejoinSay also speaks when the party is on another map.
  Tests: TestFleeCellDeadEnd.
- Open: core members run dry: Zen.fox had 0 HP potions and 0 Butterfly Wings in pay_dun01. The party town run
  (Ayla's plugin) must notice members at 0 potions.

## 2026-11-14 - Ayla's party grows to 6, Ayla's guild (built, needs legion + core deploy)
- Moving a bot into legion-core = 3 places that must agree: `.env` LEGION_CORE (both services read it; quote it when a
  name has a space, e.g. "Silent rogue"), `data/human-parties.json` (the core rewrites it on party changes, so re-check
  it just before the core deploy) and tools that keep their own copy (wrongbuilds.py PARTY, sentinel.py CORE default,
  progress_report.py). Recreate main legion first (or both together): a new core with the old main still running
  would log the same account in twice.
- Guild invitations: OpenKore's `guild request <player>` only works for players on screen (Match::player), but
  rAthena's CZ_REQ_JOIN_GUILD 0x0168 / guild_invite has no distance check - any online account id works. So a human
  leader recruits server-wide through a tiny plugin command (guildrecruit.pl `ginvite <aid>`) fed by the engine.
- Bots already answered guild invites (guilds.go guildFit) - but a human's guild isn't in guildDefs, so before this
  it got the plain personality chance and her own party friends would also accept any bot guild leader's invite.
  Now her friends refuse every guild but hers (humanguild.go humanGuildAnswer).
- Shared scratchpad: several agents write to the same session scratchpad; a generic name like g.sh got overwritten by
  another agent between write and run. Use your own subfolder.
- 12:25 map choice bypasses (QA 17:05-17:14: moc_fild01 78 bots, prt_fild05 50, prt_fild07 49, pay_fild07 48;
  mjolnir_02 19 bots / 10 deaths in 9 min; lv52-69 parties on Novice fields). Live snapshot 17:16: 106 SOLO bots on
  fields that give their base level little exp (moc_fild01 alone had lv51 average solos). Bypasses found: (1) solo
  pickers (guideSpotsFor / goodHere / SpotsFor fallback) never applied the train / aggression rule unless goal
  parties were on, and had no exp-poor notion -> now partyMapTooHard rates a solo bot like a party of one (mages /
  alchemists 6 weaker) always, the guide candidates drop exp-poor maps when a real-exp one fits, goodHere leaves an
  exp-poor map when a real-exp spot with room fits, the SpotsFor fallback skips too-hard maps and scores exp-poor -500.
  (2) partyMapFor (party formation, incl. lfg's formPartyOpt refit) kept any map partyMapOK accepted - exp-poor or
  packed included: lfg pairs "lvling @ moc_fild01" (lv52-70) were formed on the 78-bot field; now it re-fits when a
  real-exp map or a map with room fits. (3) lfg.go card/gear-goal parties (Anchor) keep their map while goalMapSafe:
  "Pupa card @ prt_fild08" (lv64 party), "Poring card @ prt_fild08", "Rocker card @ prt_fild07", "Willow card @
  pay_fild01" - card goals on Novice fields for lv52-69 parties; goalMapSafe ignores crowding and exp (lfg owner).
- 12:20 income: bots weren't losing money, they never made it. Per bot-hour a lv60 picked ~3.6k of loot (~40z/kill)
  and spent it all on gear/arrows/potions. Gear-poor bots fight at a low effective level, so every picker (EXP +
  safety) sent them to low, crowded fields worth 16z/kill. A map picker that never asks "what does an hour here pay"
  keeps bots poor; and an EXP rate 10x the drop rate lets levels run away from money and gear. zenyhour.go adds z/h.
- Market data first (healing/opportunities): the moment shops listed healing items and bots browsed with a reason,
  vend sales went 14 -> 252 per ~20 min. Supply was the bottleneck, not demand.
- gofmt -w ./... in a shared tree reformats other agents' in-progress files: format only your own files.

## 2026-11-14 12:25 - key buffs starved by attack skills; spheres for Combo Finish (build-and-strategy run 5; built)
- Trace 16:50-16:55Z, 22 bots (1-2 per class, lv55-79): Knight theyukiwing (THQ 10) 0 THQ in 5 min - Provoke 16x,
  Magnum 4x, SP 80/220 while THQ lv10 costs 50; Knight Frost_chan 19 Bowling Bash, 0 THQ; Blacksmith Frost Deals 29
  Hammer Fall, SP 29%, 0 Adrenaline Rush; Hunters 54 Double Strafe, 0 Improve Concentration; Monk TheZerolord (Combo
  Finish 4) 71 Chain Combo, 0 Combo Finish, 0 Call Spirits. A key buff with a 30% floor still never fires when cheaper
  skills run first and keep SP at 30-35%: the floor alone is not a priority.
- Fix (skillbuff.go): key buffs carry their rAthena SP cost per level (a lower level that fits is cast); while one is
  due, attack/crowd skills and Magnum Break hold its lv1 cost back (keyReserve), Hammer Fall waits (keyBuffDue); a
  Monk with Combo Finish / Asura summons spheres down to the 30% floor (spheresKey). Test: keybuff_test.go.
- Checked, fine: Kyrie is cast on the current image (3 priests within a minute); /api/buildgear/next after the 11:56
  deploy: 0 "trade" (was 872/1010), shop 758 / drop 252 - but only 21/1010 can afford their next piece (lv30-60 avg
  1.2-2.4k zeny). Peco riders 0 (5 of 60 riders hold the rental), falcons 33/115, singers buy instruments when they
  have the zeny (Ashcat97 Rope, TheCrimsonfox77 Violin). Poverty is the blocker for every class step.
- Right after a deploy only 19/131 priests were partied; 12 min later 120/128 - measure party state >= 15 min after
  a deploy.
- Run-4 variants (assassin_dagger, sage_support, ...) are only given to NEW characters: personas.json holds 0 of
  them, so dual-wield / endow / Plagiarism AI would have no users yet.
- 2026-11-14 (BROKE-NO-POTS / LEADER-LEAVING): "out of potions" needed > 2,000z (or > 1,100z), so a poor member with
  0 potions (Zen.fox, 1,024z) never even asked for a town run and kept dying. Now 500z or loot to sell is enough; a
  human-led member that asked for a town run 10+ min ago with no party run goes alone (announced) for a real need.
  While the leader leaves (a rally, or a bot leader travelling) the tank and the friends open no new fights - pulls
  had kept Ayla's "protect the party" fights going so her rally never set off (plugin side: partylead.pl patch).
- 2026-11-14 a mass disconnect must never erase state: a rathena recreate right after a deploy disbanded every bot
  party (`on < 2`) and the saver wrote the empty set; the next start restored nothing. Hold a save that drops below
  half of the last one, keep rolling backups, and give parties a grace period offline. And a party system gated by a
  chat budget can't rebuild 1,000 bots' parties: match directly, let chat lines be flavour (cold start sim: 85% in 60 s).

## 2026-11-14 12:23 RELOADMOBDB-CRASH (main thread)
Sending `@reloadmobdb` (via Ayla's GM account) to apply a new exp rate crashed/restarted the rathena map server
(RestartCount 1). Its followers (openkore-ayla, legion-core, openkore, legion) kept the old network namespace and went
offline until `docker restart` of each follower (same rathena container id, so restart - not recreate - rejoins it).
Never use @reloadmobdb / @reloadbattleconf on the live server; change conf/import and apply at a planned,
owner-approved rathena restart (tools/deploy/rathena_recreate.sh), at most once an hour. The restart did load the new
base/job exp rate 500 (5x).
- 12:30 drop rates: the engine valued maps, rarity and goals from the base mob db while the server multiplies drops
  (item_rate_*). Any server-side rate change must reach the engine through one helper (droprate.go effDropRate) with
  the per-drop 100% cap - multiplying an aggregate (z per kill) overstates high-rate drops (Cactus Needle 45% x5).
  A live tuning switch (drop_rates_from_conf) lets main flip it the moment rathena restarts, not at the next deploy.
- After buying the 100k guide the alchemist is broke and can't bid for herbs: a brewer needs supplies routed to it
  (guild herb parcels), not just a market want.
- Payon <-> Morocc Kafra laps (12:38 local; QA 17:13-17:27Z Ryustorm 9 teleports in 20 min for pay_dun03, 32 A->B->A
  server-wide in 20 min): two holes. (1) kafraOnward's chained hop could lead back into the plan's own start town:
  payon -> Morocc -> Payon "saved" the walk inside Payon (the Payon Kafra landing 161,58 vs the bot's spot) or read
  "safe" when the direct walk through pay_dun01/02 read unsafe - so Morocc won from Payon, then Morocc's Kafra sent it
  back, then the Kafra save in Payon moved it and the plan fired again. Now a chain never returns to the start town
  (kafraOnward `from`). (2) the "walk is unsafe" override in kafraHold also lifted the A -> B -> A block; now it lifts
  only the hourly cap (kafraHoldQ; the cost estimates use it quietly). TestKafraPayonDungeonLive fails without (1)
  (lv80 payon -> pay_dun03 picked Morocc) and passes with it. Lesson: any "plan B is safer" rule must still respect
  the loop guard, and a chain planner must exclude its own origin.
- OpenKore `plugin load <x>` is NOT a path: it greps x as a regex over plugin names in the plugin folders and prints
  nothing when none matches (main sent `plugin load plugins/cmdfile/guildrecruit.pl` twice, silence). Use
  `plugin load guildrecruit`, and add the name to `loadPlugins_list` in control/sys.txt when `loadPlugins 2`, or it
  won't load at the next start.
- 2026-11-14 (STRONG-PARTY / MELEE-PEEL / WING-TO-SAVE): a party whose every member is effective lv50+ (partyStrong)
  no longer has its leader collect members one by one (only one in danger); members go straight to the party map /
  leader when their route is safe (routeSafeFrom), no checkpoint wait. Melee DPS (roleMelee: STR thieves, merchant
  line), member or leader, take monsters off ranged/support members (pubState.Back/Support/Fragile) like the tank.
  A Butterfly Wing lands on the save point, so a trip wings only when the known save point IS the trip's town (Ayla's
  friends winged to Morocc/Prontera on a Payon run); human-party members in the party's town save at its Kafra.
  Ayla's plugin (partylead.pl, pending reload): a strong party (all lv50+) - she no longer walks back or fetches for a
  friend that is just far/asking; she tells it where she is; only a hurt friend is collected.
- rAthena tells a NEW guild member nothing via 0x016c (guild_member_added -> clif_guild_belonginfo returns early while
  the member's position isn't known); the joiner only gets the area name update ZC_ACK_REQNAMEALL2 0x0a30 with the
  guild name. Legion bots therefore didn't know they had joined any guild (bot guilds too) until they relogged:
  Starveil 12:42, three friends were in the DB guild while /api/guild-recruits still offered them. Fix: bot.go 0x0a30
  for our own id after an accepted invite -> guilds.go noteGuildJoined.
- Never embed multi-line Python in a single-quoted bash string written through another script: an escaped "\n" turned
  into a real newline and broke `ayla.sh guild` ("unterminated string literal"). bash -n can't see it. The formatter
  now lives in tools/rogui/ayla_guild.py; test such commands against the live API or a mock JSON.
- The map server holds only ~1013 clients: rAthena is built with select() sockets (MAXCONN = FD_SETSIZE = 1024). With
  -max-online 1000 slot holders + 10 guild leaders (outside the slots) + legion-core + Ayla it was full after the
  12:28 rathena recreate; guild01's leader Silver_heart lost the race and looped "map: i/o timeout" -> "login refused
  (0081, code 8)" (char server still holding the half-done login) for 15+ min while rathena logged 38k "accept failed:
  Too many open files" a minute. Fix (leaderroom.go): slots = -max-online minus -reserve (40), and a leader whose map
  login fails makes one ordinary slot holder log off and keep its slot 10 min. A "map:" timeout is a full map server,
  not a network problem: check `ls /proc/<map-server pid>/fd | wc -l` in ro-rathena.
- 12:55 herding after the 12:33 deploy (gef_fild02 139 bots, pay_fild09 76, moc_fild01 61): 132 of gef_fild02's 139
  were PARTY members (36 parties), formed/kept there by lfg goal parties and the party pickers whose new zeny/hour
  term (economy) came from MapZenyHour's 2-minute cache - a stale crowd, so every pick in a login wave saw the same
  "best" map. Fixes: MapZenyHour's cache key includes the load (steps of 3 bots), so each seat reservation moves the
  value; bestZenyMap picks at random among maps within 80% of the best; partyMapSearch / partySpotNear choose at
  random among near-equal candidates (spreadPick); partyMapOK fails on a packed map (crowd.go mapPacked: 25% over the
  cap or its bots roam) - lfg's mapOKFor and formParty's refit use it, the fit check moves parties off; solo goal
  picks are staggered 0-40 s after login. mjolnir_02 (15 deaths with 4 bots there): the dead were party members
  WALKING THROUGH it to mjo_dun01 - pickers now skip a map whose walking route crosses a map too hard for the weakest
  member (routeCrossesTooHard). Sweep fix: publish every member and rebuild the crowd before judging (the crowd
  snapshot was taken after the first party). Sweep (279 parties): gef_fild02 101 -> 66 party members, beginner-field
  lv45+ parties 3 -> 0, 142 moves (load at the origin isn't released in the sweep, so destinations read fuller than
  they will be). Mid-level population exceeds what the mid maps feed (cap = spawns / 5): some maps stay at the cap.
- 13:00 markdowns: a timer kept in memory never fires when deploys come every ~30 min and the timer is 45 min.
  Anything time-based in the economy (markdowns, cooldowns that matter over hours) must persist across restarts.
- Healing-first made vendors list their own NPC-bought potions at the falling market price (Red Potion 5-12z, NPC
  pays 25z): every listing needs the NPC-value floor, and a surplus needs a signal that reaches farming and stocking.
  Add-on: Elder Willow (mjolnir_02) as the top killer in the life mind's diaries - those were transit deaths. The
  route check (routeCrossesTooHard) now also guards solo guide-spot picks (strategy.go guideSpotsFor) and lfg's
  goal-map safety (goalMapSafeItem, from the leader's position) besides the party pickers.

## 2026-11-14 13:20 - build variety, Teleport card, DS hit-and-run (build-and-strategy run 5, part 2; built)
- Variety measured before acting: 5,986/5,986 characters on their line's main plan - the run-4 variants only went to
  NEW characters and none had been made. A variant that only seeds new characters is no variety. 9 variants added
  (every line 2-3 builds), new characters main 40% / variants 60%, seeded support/vendor roles use the families too.
  tools/variety.py moved 2,677 existing characters whose skills AND stats already fit a variant (no deletions).
- Variant caps: a variant inherits only the TRANSCENDENT skills of its main plan's trans list (transFor); counting the
  whole trans list made Two-Hand Sword Mastery 10 look valid for a spear Knight (43 false "compatible" moves, caught by
  the wrongbuilds audit - always re-audit after a plan change).
- personas.json plan changes are live only at a legion restart; characters online (or logging in) in between keep
  spending points under the old plan: leave online characters alone and run wrongbuilds audit + variety.py
  revert-wrong after the restart.
- Teleport (Creamy Card / Acolyte skill): casting AL_TELEPORT yourself (not via a wing's itemskill) opens a warp list
  (0x0abe) that must be answered (0x011b "Random") - skip_teleport_lv1_menu is "no" on this server.
- Hindsight (Auto Spell) on this client is ZC_AUTOSPELLLIST 0x0afb (int32 ids), answered with 0x01ce.
- Python's text mode on Windows writes CRLF: editing ai.go / bot.go / economy.go with a python read-replace-write turned
  them into CRLF files (gofmt -l lists them, every diff is the whole file). Use newline='' (or gofmt -w afterwards).
- A helper called from inside ai.go / economy.go runs with that bot's b.mu held: anything that loops over all bots
  and locks each (a "pick the guild's X" scan) must not run inline there - Go mutexes aren't re-entrant. Cache the
  pick and refresh it from a goroutine (humanguildtrader.go).
- 2026-11-14 (LEADER-TRAVEL): a goto to a far map (gl_church from pay_dun00) stalled: OpenKore's attackAuto_routeToLock 1
  + attackAuto_party 1 made Ayla fight every few steps, and the friends kept pulling (leaderLeaving only knew rallies).
  Legion now also treats the human leader as leaving when her status.json lock_map isn't her map; partylead.pl's
  travel mode turns both switches off in memory while she travels to another map and puts them back on arrival.
- 13:10 brewing never started: the 20 Medicine Bowls bought with each guide were sold to the NPC at the very next shop
  stop - npcJunk = "Etc worth < 1,500z", and crafting materials are exactly that. Every new item a role BUYS for its
  craft must be checked against every "sell the junk" path (npcSellable), not only the shop listing.

- 2026-11-14 Peco: a gate on "state known" must check WHERE the state comes from. optionSeen came only from a self 0x0229, which rAthena sends at login only with opt2 set, so 124/125 Knights never wanted a Peco all day. Read saved state from the char list (CHARACTER_INFO) at login.
- Guild levels are cheap early (100k guild EXP for level 2 in Renewal) but come ONLY from the per-position EXP tax,
  which is 0% by default: no guild on the server had levelled (all lv1, cap 16) until the tax was set
  (guildtax.go / guildrecruit.pl gtax). OpenKore stores guild skill levels only the first time it sees them
  (`$guild{skills}{..}{lv}` is never updated), so the plugin parses ZC_GUILD_SKILLINFO itself.
- 2026-11-14 (HEAVY-NOW / IDLE-SIT-FLAP / STRONG-DANGER): "bag is heavy" (>= 50% weight) now skips the 45-min trip
  interval and calls a party run with the short gap (owner: 50% triggers a town run at once). A party healer's idle
  sit has hysteresis: it sits only after the leader stood still 4 s, stays seated >= 8 s, keeps sitting with the
  leader up to 9 cells (6 to sit) and threats up to 5 cells (7 to sit) - Kira storm flapped 15x/67 s behind a
  roaming leader. A strong human-led party (weakest lv50+) no longer treats other players' deaths on a map as
  danger (Ayla's lv72-78 friends sat in Geffen, "gef_fild08 too dangerous").

## 2026-11-14 13:25 - Sage Mages without AoE, Storm Gust late, 3 variants with AI (build-and-strategy run 6; built)
- "Mages have no area spell" (QA 104/114) was one plan, not the engine: Wizard-plan Mages had Fire Ball / Fire Wall /
  Thunderstorm, the 37 online Sage-plan Mages had bolts 10/10/10 and nothing else until job ~40, because the Sage plan
  maxed the bolts first. Measure skills per PLAN, not per class. The Sage Mage part is now bolts 5 -> Fire Ball 5 ->
  Thunderstorm 5 -> bolts 10 -> Fire Wall 5; reordering keeps the caps, so nobody becomes a wrong build.
- Wizards outside Ayla's party (6 traced 4 min): 0 fails, mostly Jupitel; Storm Gust only where learned - the plan
  had Earth Spike / Heaven's Drive before it. Storm Gust now right after Jupitel 3.
- Python on Windows: open(p,'w') writes CRLF. Editing Go files with a text-mode Python rewrite turned 4 files CRLF
  (gofmt -l lists the whole file). Use binary mode ('rb'/'wb') or the Edit tool.
- Mental Strength (MO_STEELBODY) needs Combo Finish 3 in the Renewal tree: a "Steel Body tank Monk" is a combo-line
  build (monk_occult: Occult Impaction + the combo to Finish 3 + Mental Strength 5). Check the tree before naming a build.
- Traps (1065) are sold only in Izlude / Prontera south gate / Hugel / Malangdo / Dewata / Moscovia: a trap build
  needs its own supply trip, like arrows. errand.go is owned by the Peco fixer, so a partied trapper restocks on
  party runs only (open).
- Friendship system (friends.go, 2026-11-14): two Legion instances share /data, so each instance writes its own
  file (friends.json main, friends_core.json core) and reads the other as read-only "peer" pairs merged by max -
  never let both instances write one file. rAthena sends ZC_ACK_LEAVE_GUILD 0x015a to the whole guild with the
  leaver's name: the engine had no handler, so a bot that left kept GuildID set until relog (now bot.go -> onGuildLeft).
  Guild chat 0x017f was not parsed either (now feeds friendship chat points).
- 2026-11-14 (PLAN-NOT-FOLLOW / TOWN-RUN-TO-HER-TOWN): bot leaders announce every move as a plan ("next: <map> (why),
  meet at the kafra in <town> first" / "go straight there" for strong parties; together.go planLine) and members act on
  it through partyGoalSync. Ayla's friends route to her announced lock map (status.json) - strong party straight
  there, weaker to its Kafra town - instead of tailing her; her town runs go to the town she is actually in (her line
  named Payon while she stored in Geffen: friends scattered to Payon).
- 13:20 members dying on the way / on maps the leader keeps them on (QA 17:47-18:18): moc_fild03 had 25 deaths in 30
  min at effective levels 43-48 with "6-12 on me" - its 164 Wolves (AI 03, assist) were counted as passive. Mob.Assist
  (AI 03/07); assist packs count half toward the aggressive share and the train count, and toward the strongest
  common monster: moc_fild03 is now a train map (needs a weakest power of 62); moc_fild02 (Pecos, 32%) is not.
  Members travelling to their party crossed mjolnir_02 (Onirogue 7 deaths, xXYukiqueenXx 5): swarmPenalty now also
  counts maps partyMapTooHard rates too hard, so kafraPlan / kafraVia teleport around them; partyFitCheck treats a
  member whose field route to the party map crosses such a map, or that died there twice in 30 min (its badMap), as
  "too dangerous for X" - bans the map for the leader 30 min and relocates. A banned or deadly party map is checked
  every minute, skipping the 15-min settle (Zero.dragon died 5x on moc_fild03 while its party stayed).
- 13:40 class sample: 0 of 19 Sonic Blow Assassins owned a katar (lv53-73, gear_next Hat/Shoes/Muffler). A learned
  core skill whose weapon is missing is worth nothing; "next gear piece" picked affordable armor because the katar
  (19,500z) never fit the purse. The build's core weapon is now a savings goal like the Peco / instrument
  (coreweapon.go coreSaving -> gearHold, classWeaponNeed). Check "skill learned AND usable with what it holds" per class.
- Test hygiene: restoring a global map entry with `m[k] = old` when old was nil leaves a nil value that crashes every
  later loop over the map (TestNextBuildPiece panicked in the full run only). Delete the key instead.
- Variety online stays low after the moves (13:30: ~7% of online characters on a variant, 0% at 2nd job): the
  online pool is mostly lv40+ characters whose stats/skills already fit only their main plan. Real visible variety needs
  turnover (new characters) or a build reset - an owner decision.
- 2026-11-14 (PARTY-FARE): on Ayla's move to Morocc two broke friends couldn't pay the Prontera Kafra (zeny < fare +
  potion keep), their walk west hit a warp the loop detector had banned, and they sat "no route to morocc" until Ayla
  mailed zeny - which the guild/gear pools then took again at once. A member following its party's move now keeps no
  potion reserve for the fare, and when short it asks in party chat and a party mate with 3,000z+ to spare mails the
  shortfall (together.go askFare; not from virtual kafraWouldTake planning).
  Also seen: xXRinfangXx re-equipped Goggles/Bandana/Hat/Ribbon every 5 s (headgear equip flap) - not fixed here.
- Ayla plugin (2026-11-14 18:40): a plugin that both routes (a rally) and leaves `lockMap` set lets OpenKore's own lockMap
  routing fight it - she walked back and forth across Geffen for 10+ min. Whoever owns the movement must also clear
  lockMap (stash it, restore later). Same for an order that targets a state machine already running: a mind's
  "rally prontera" was dropped because start_rally returned early while a Geffen rally existed - an explicit order
  must replace, not queue behind, the current plan.
- Announce what you actually do: the town-run line was built from the hunting map's town while the sell NPC was in
  another town ("town run to payon", then Geffen). Derive the announced town from the NPCs the run really uses.
- Check item weight against the character's capacity before putting armor on a gear path: Chain Mail (330) is 78% of
  Ayla's 420 capacity - she would never regen again. OpenKore/rAthena weights are in tenths (714/4200 = 71.4/420).
- OpenKore `plugin load` takes a regex on the plugin NAME (`plugin load ^aylagear$`), not a path; `reload config` resets
  every in-memory switch the plugins made (lockMap holds) - edit %config from a plugin command instead.
- Ayla plugin (19:10): a leader's own automatic plan (a rally because one member respawned in town) must never outrank a
  member on her map calling "help! 6 mobs on me" - she said "omw" and walked away. Rank: friend in danger here > the
  mind's / friends' orders > her own rallies and fetches. In a strong party a respawned member comes back by itself.
- 13:50 mjolnir_02 still 22 of 41 deaths (18:30-18:50): the dying parties' maps were mjo_dun01 / mjo_dun02 (TheHanaz
  pt, xXRavenkunXx pt) - dungeons whose only entrance is on mjolnir_02, so every member walked through its trains
  (a Kafra can't skip a dungeon's entrance field). partyMapTooHard now = the map's own monsters (partyMapTooHardHere)
  OR its way in from the nearest town (mapAccessTooHard); since every picker calls partyMapTooHard - party pickers,
  partyMapOK / fit check, solo guideSpotsFor / goodHere, lfg goalMapSafe / soloMapOK, guild bestPartySpot - all of
  them refuse mjo_dun01/02 below power 52. Test TestMapAccessLive (real data) checks each path.
  Lesson: a destination check must include the way in; put the rule in the ONE function every picker already calls.
- Loyalty (loyalty.go) hooks heal/mail/trade through friends.go friendNote with a deferred call that runs AFTER the
  friends unlock: two subsystem mutexes need one fixed order (friends -> loyalty), never call FriendScore/friendsOf
  while holding loyalty.Mutex. Help answers run on the instance that owns the helper (mailZeny needs its b.mu).
- Town gates + Morocc strandings (13:52 local). (1) The warp-loop detector banned town gates for 30 min
  ("warp loop prt_fild06 <-> prontera" every ~9 s -> Prontera's east gate prt_fild06 23,193 gone; earlier geffen
  119,213 north exit; morocc 160,17 <-> moc_fild12): normal in/out traffic and bots landing on the return portal's
  edge look like A,B,A,B. Now warpLoopBannable is false for any warp into/out of a town or its buildings, a loop ban
  lasts 10 min (warpLoopBanFor), a stalled-portal ban on a town gate 3 min, and the "re-planning" note (no ban) is
  logged at most every 5 min per bot instead of the old misleading "avoiding the warp" line. (2) "no route from morocc
  156,46: Butterfly Wing home" x16: bots landed in sealed Morocc for a chained hop, the 15 s post-landing kafraNext
  made kafraPlan refuse, travel found no walking route and flew home. Now no wait after a landing, travel re-plans
  with the Kafra before escapeNoRoute in any Kafra town, kafraHold never holds when there is no walking route at all,
  and kafraOnward drops a second hop the loop guard would refuse on arrival (payon -> Morocc -> Alberta right after
  alberta -> Payon was a way around the A -> B -> A block). (3) A bot party's member never takes a Kafra landing on a
  field (Orc Dungeon = gef_fild10) unless it is the party map or the leader is there. Tests: TestKafraMoroccLandingLive,
  warploop_test town gates.
- 13:55 Teleport card chain: a gear step must be obtainable HERE - Clip [1] is in no loaded NPC shop and drops only from
  a lone Dragon Fly / 0.05% Alarms, so 0 Clips existed and 7 Creamy Cards sat in bags. Before naming an item in a gear
  path, check the lookup (npc_shops / farm_maps). Belt [1] (NPC 20k, lv25) is the slotted accessory now, and the card
  is bought only when there is something to put it in.
- Resets on this server: only the official Izlude Hypnotist (1st class, weight 0, no cart). A "natural reset" is a
  long errand (store everything, two dialogs, fetch back) - persist it, because a schedule logout in the middle would
  otherwise leave the character naked with its things in storage.
- 2026-11-14 (SAVE-AT-NEAREST / STORAGE / DUNGEON-CLOSE): owner rule enforced - travel into a hunting map goes via its
  nearest Kafra town to save first when the known save point differs (together.go saveGate; one detour per goal per
  15 min); kafraSavePlan no longer accepts "close enough" saves nor waits for a human leader to be in town; a pending
  save is driven wherever think goes; saves are reported in party chat ("saved at morroc"). Ayla's friends save at the
  nearest Kafra of HER destination (status lock_map) - the old code used p.Checkpoint (the previous ground's town), so
  nobody saved at the Morocc rally before the Sphinx. GET /api/savecheck lists online bots breaking the rule.
  Heavy bags with only kept items (cards, ores, spare gear) now go to Kafra storage (storage.go: "Use Storage",
  0x0364 per item, 0x0193 close) instead of staying at 50%+. In dungeons/hostile maps Ayla's friends stay within 8
  cells and don't route ahead to her lock map; a tank goes to a mate with 3+ monsters on it within 40 cells.

## 2026-11-14 14:10 - economy run 9
- A role that needs its character online (the guide-owning brewer) must be scheduled online: a +0.6 pick weight meant
  2 of 4 guides sat on offline accounts. Check the account schedule, not only the character pick.
- Every "give money away" path (party gifts, guild/boss pools, guide and peco funds, a human guild's pool) must use one
  definition of what is giftable. Zendancer84's 9.5k raised for a Staff was mailed on by the Starveil pool within
  30 s because that pool used spareZeny, not the party's raised record.
- "Hold the whole purse for X" rules (guide, Peco) become a permanent freeze when X is never fetched: pinned bots never
  ran alchemyThink, so 108k sat untouched. A hold needs the errand that spends it to be reachable for every bot.
- Slot-keyed equipment maps miss pieces that span slots (Goggles upper+mid): compare against every worn item that
  overlaps the new piece's location bits, or each candidate "beats an empty slot" in turn and the bot swaps forever.
- A shop visit only from "town runs" leaves funded bots standing next to the shop; buying must hang on "standing in a
  town that sells it with the money", whatever brought the bot there.
- 14:05 Combo Monk: "skill sent, no damage, no failure" = rAthena's SKILLINTERVAL refusal, which clif_skill_fail hides
  unless the player has @showdelay. Chain Combo was sent the instant Triple Attack hit, inside TA's delay; the combo
  state only lasts delay + 300 ms. The server says the delay (ZC_COMBODELAY 0x01d2): cast after it. Reading the
  server source (ro-rathena:/rathena/src) settled in minutes what traces could not; any silently dropped skill may be
  the same trap (sent during an after-cast delay).
- Ayla plugin (19:05): in X-Kore 2 every server answer to a bot-driven action also reaches the watch client, which
  opens the matching window (Write Mail stayed open over the owner's view). Drop those packets for the client
  (OpenKore `Network::Receive/mangle` return 2) while the plugin drives the action. And a plugin that swaps config
  for a run must restore only what it set and nobody changed since - the mind's `conf` mid-run was overwritten.
- 14:20 general case of the hidden refusal: a PLAYER's every normal swing sets canact_tick = tick + amotion
  (unit_set_attackdelay), so a melee bot that fires a skill right after a swing loses it without any packet back.
  Skill timing must follow the server's can-act time (our 08c8 attackMT, 01de/011a/0117 + db AfterCastActDelay, 07fb
  cast), not fixed guesses. skillaudit.go measures it (/api/skillaudit) - measure sent vs answered per skill before
  believing any skill fires.
- Yuno laps (14:14 local; QA 19:00Z Dark rogue / Holyboy 12 Kafra teleports in 2.5 min prontera <-> Morocc for Yuno):
  kafraPlan added the swarm penalty of the landing's walking route - 0 for Morocc, which has NO walking route anywhere
  - so "Morocc + its Kafra on" beat Geffen (whose way to Juno via mjolnir carried a penalty); Morocc then sent it back.
  The trip ("real need") lifted every limit. Fix: swarm penalties are added inside kafraOnward to the path actually
  walked, a landing with no way on (walk or its Kafra) is skipped, and hard limits no exemption lifts: at most 3
  teleports in 10 min, never X -> Y -> X within 10 min except a party move or the one way home after a town run.
  TestKafraYunoLive: prontera/payon/morocc/geffen/aldebaran -> yuno never via Morocc (Geffen -> Al De Baran -> Juno).
  Lesson: never add a penalty that is 0 for "unreachable"; and every exemption needs a hard ceiling under it.
- 2026-11-14 (SAVE-FIX-STAGGER / HER-SAVE-TOWN / FARM-WAITING): bots already hunting with a wrong/unknown save fix it
  (solo: Kafra town 1-2 maps away; strong-party members: 1 map) after a random 0-10 min delay, else on the next town
  visit (travel saves in the nearest town). A human leader's friends save where SHE saved (status.json save_map from
  her plugin's verified save, else the rally town), never in a pass-through town (they saved in Alberta while she
  saved in Payon). Waiting on a field for mates (leader waiting for stragglers, member waiting to be collected), bots
  fight monsters within 6-8 cells instead of idling (healers excepted).
- 14:35 two fixes in one build. (a) mjolnir_02 deaths after the access rule went live (19:00-19:15): the parties on
  mjo_dun01/02 (Rin0752 pt, shadowdancer pt, Vexwing33 pt, silvercat pt) never left: the fit check deferred every
  move while any member was "resting" - and after the mjolnir_02 trains someone always was; relocate refused when
  the leader's goal had fallen back to its own pick (silvercat pt: Set "auto" -> it had picked mjo_dun01 itself, since
  a party member's guideSpotsFor skips the solo safety rules). Now: a too-hard / dangerous party map is left even
  while members rest (only exp-poor / packed moves wait); relocate accepts the party's map as "from" and any leader
  goal but api/boss; a party leader with nothing fitting holds ("party-leader-hold": its map if safe, else waits in
  the nearest town) instead of a solo pick. (b) pay_fild10: 50 of 52 bots in one 40x40 corner, 0 kills in 3 min. The
  walk graph is fine (one region, 276 steps to the centre). Cause: seen.go shared worthless Green/Blue Plants as
  monster sightings (every sweeper jumped to them) and counted them as monsters in view (no "map reads empty").
  Plants (Mob.worthless) are now neither. Lesson: anything that attracts bots must use the same "worth fighting"
  filter as the target picker.
- 14:50 escorted crossings (escort.go): a party member travelling to its party never walks into a map too hard for it
  (partyMapTooHardHere at its own rating) on the way: it walks to the last safe map before it, waits there ("waiting
  at mjolnir_01 for an escort through mjolnir_02", party chat every 3 min, sits), published as pubState.EscortAt. The
  party leader then goes back to that safe map, gathers the waiting members within 20 cells, says "crossing now" and
  travels on; the members see the leader beside them and follow for 4 min (escorted). The leader gives an escort up
  after 10 min (the fit check then judges the map too hard for that member). Human-led parties (Ayla) are excluded.
  Test: TestCrossingDangerLive (power 40 from gef_fild04 to mjo_dun01 stops at mjolnir_01 before mjolnir_02).
- Refused Kafra must never mean a deadly walk (14:42 local; QA 19:20Z TheShadowsenpai, Alchemist 69 rated ~51,
  bounced aldebaran <-> mjolnir_12 4x in 12 min and died). The hard limit (3 in 10 min / X->Y->X) now records when it
  clears (kafraHardUntil), and travel waits seated in the Kafra town (kafraWaitInTown) while it is held and the walk
  crosses a map too strong for the bot (routeSafeFrom); a safe walk still walks. Tests: TestKafraHardUntil,
  TestKafraWaitInTownLive.

## 2026-11-14 14:45 - Fly Wing every 5 s from ONE monster at 84% (TheShadowsenpai) + beginner Kafra ping-pong (built)
- TheShadowsenpai (Alchemist 69, gear-rated 51) on mjolnir_12: 15 Fly Wings in 2 min, 84% -> 33% HP, one lv63-65
  monster. losingFight called it "outmatched" (65 > 51+8, leave under 90%), while escape() called it an even fight
  (65 <= base 69+2), and even fights aren't added to b.escapes, so the 4/6-escapes throttle never counted anything.
  Fix: one monster within 2 levels of our BASE level is never "outmatched" or farAbove (a weak gear rating alone is not
  a lost fight); every escape goes into b.escAll (2-min window) for the throttle, while b.escapes stays the map-danger
  list. Tests: TestWingThrottleSequence, danger_test Alchemist cases.
- Beginner ping-pong (38 A->B->A Kafra hops in 19 min, "lost 47% HP (1 on me, lv 8 vs 1)"): "lv 8 vs 1" is a lv8
  monster against a starter-kit gear rating of 1. Under lv20, with a weapon and 3+ potions, one monster up to 10 levels
  over max(base, gear) is a normal fight (potions, not wings; leaves at the < 20-25% last-resort lines). A beginner's
  escapes from 1-2 monsters never count as map danger, and beginners never get the 3-escapes "too dangerous, looking
  for an easier map" re-pick (owner: stay near home).
- Rule: two checks that judge the same fight (losingFight vs escape's even/farAbove) must use the same level. One said
  "far above" by gear, the other "even" by base, and that mismatch disabled the throttle.
- 14:45 skill gating measured (/api/skillaudit): 47.0% of 18,511 skill requests were silently dropped with no gating,
  37.6% with it; melee skills improved most (Holy Cross 39->17%, Envenom 38->22%, Bash 46->33%). Cast-time skills
  stayed ~70% wasted because this server announces casts with 0x0b1a (PACKETVER 20211103), not 0x07fb, and no-damage
  skills with 0x09cb, not 0x011a - check rathena/src/map/packets_struct.hpp for the server's packet version before
  parsing a packet from memory (escapeitem.go's castUntil never worked for the same reason).
- 2026-11-14 (PROTECT-SUPPORT / ESCORT-GATE): tanks (then melee, when no tank is nearer) protect a ranged/healer/fragile
  member on their map within 28 cells that called for help (danger.go helpcall, published pubState.Help), is being hit
  or is under 50% HP - before the tank's own pulls: they take the monster off it, else walk to it. Ayla counts only when
  hurt and calling "help". Ayla's friends outside her dungeon gather in the entrance town (her save town), restock
  potions there, and go in with at least one other friend (or alone only when the route is safe) - after the 14:40
  restart they had walked into pay_dun00/01 one by one and got swarmed. Baseline before: 4 support deaths / 30 min
  (10 deaths total, events window from 19:48Z), 17 bots in a "taking X off Y" state.
- Ayla plugin (20:00): a state that "returns early" (the goto branch in partylead's tick) silently disabled every rule
  below it - the help-call fetch never ran while a goto was active, so she promised "omw" and walked to the next floor
  while the priest called 10 times. Priority rules (friend in danger) must sit above every early return, and a reply
  like "omw" may only be sent by the code that actually starts moving.
- OpenKore `portalRecord 2` rewrites portals.txt whenever a landing cell differs by a step (rAthena warps land on a
  random cell nearby) and recompiles portal LOS for ~30 s each time, freezing the bot. Keep portalRecord 0.
- 15:06 with 0x0b1a parsed (cast-time skills gated to cast end): 18,226 skill requests, 20.5% wasted (was 47.0% with
  no gating, 37.6% with swing-only gating); Fire Bolt 74->26%, Jupitel 65->25%, Kyrie 100->11%, Cold Bolt 75->23%;
  answered requests per 10 min 9.8k -> 14.5k at the same send rate. Still high: Mammonite 42%, Storm Gust 43%,
  Chain Combo 38%, Hammer Fall 33%. Note rAthena sends some no-damage answers with src = the target (Kyrie's 09cb),
  so per-skill audits must count the cast start (0b1a) too.
- Ayla plugin (20:10): Python heredoc patches turned "\b" into a backspace byte (0x08) in several Perl regexes
  (help / danger phrases silently never matched). After any scripted edit of a Perl file, check `grep -c $'\x08'`;
  prefer the Edit tool for regex lines.
- Hold/travel state machines need hysteresis and a minimum segment: holding at 20 cells / resuming at 12 plus a travel
  mode keyed on lockMap ('' while holding) flipped every few seconds and one dungeon floor took 19 minutes.

## 2026-11-14 15:30 - economy run 10
- A typo generator that swaps any two bytes of a line scrambles the payload: "B> aHt 930z", "B> 8x BlueH erb". Style
  effects (typos, slang, truncation) must only touch filler words; item names, prices and B>/S> lines stay exact.
  Same for substring slang: ReplaceAll("are","r") turns "Rare" into "Rr" - replace whole words only.
- Same-map trades between a party member and a solo player failed more often than they succeeded (19 vs 12): the
  party member walked off to the solo partner while its party moved on, and the distance check killed the deal. The
  side without obligations (solo) should walk; the party member keeps playing until the partner is beside it.
- Counters that reset at every deploy make every "is it better now?" question unanswerable across deploys; persist
  them (and say since when) like the markdown clocks.
- A shop title chosen from the cart (map order, unlisted junk) and kept across sell-outs drifts from the real stock;
  derive and check it against the actual listing at each open.
- Brewers idled with full kits because the herb want asked only for the two scarcest herbs (18 Blue / 1 White on the
  market vs 282 Yellow): a want should skip what nobody offers and fall back to what is available.
- Data hygiene for the illusion: anything a mind can read (memory people, diary, friendships) must be filtered at
  the write path AND cleaned on load - "AIBot" sat in 357 memories because meet() recorded every non-Legion speaker.
- 15:20 fixes after the 15:00 deploy. (1) Escort misfires: 4 leaders "going back to aldebaran / pay_fild03 to escort",
  10 members "waiting at ... for an escort" - all members on PARTY TOWN RUNS (goal izlude / payon / alberta), routes
  across the world, safe points in towns. escortThink now acts only on the way to the party's hunting map (p.Map, not
  a town, no trip), only for a danger within 3 maps of it (its approach), never from / at a town (the Kafra goes
  around: swarmPenalty); the leader only escorts at such a safe point while leading to the party map; a member waits
  at most 8 min, then leaves the party politely; a leader gives up after 6 min. (2) "waiting for X before moving on"
  (22 leaders, Silver_heart 3+ min): the 90 s per-map wait restarted whenever the straggler flickered into range -
  now it is per map visit. (3) ~15 parties announced "next: X (too rough here...)" in one second: the fit check's
  first round after a restart checked every party at once, and the model-only "a bit too hard" verdict moved parties
  that farmed fine. Now: a party's first check is spread over 0-5 min, at most 2 relocations per 30 s round (urgent
  excepted), a merely-model "too hard" needs evidence (a party death there lately, the map deadly now, the leader's
  ban) unless it is a train map or behind one; the announced reason matches the real one. (4) A leader that announced
  a new map kept fighting on the old one (Ashqueen on gef_fild02): a travelling party leader now only fights what
  hits it, like its members.
- 15:20 run 7: champion avoidance tuned twice - a gear-based "effective level" punishes ordinary under-geared bots
  (Archer 46 read as ~30). Threat rules on real fights: party = base level, tank+healer = base+5, solo >= 3/4 base;
  threat under champion level +5. Out-of-range skills are dropped silently too (unit_skilluse_id2): measure range
  from myPos, never b.X, and only cast melee skills when both stand still. Natural resets: 149 of 153 eligible bots
  were in goal parties - a filter that excludes partied bots excludes almost everyone in this world.
- 2026-11-14 watch client: the crash every ~30 min was llvmpipe's JIT ("LLVM ERROR: Unable to allocate section memory")
  running out of the 32-bit Wine client's address space, not RAM. Read the crash log's last lines before tuning
  memory. softpipe = black screen, login failed; reverted to llvmpipe (still open).
- 2026-11-14 16:05 watch client fix: measure address space, not RSS - sum /proc/PID/maps below vs above 4 GB. The
  classic 32-bit client starts in game at 3,780 of 4,096 MB (Wine reservations + GRF maps + pulseaudio memfd + Mesa)
  and the JIT eats the last ~300 MB in ~30 min. New-WoW64 Wine (WineHQ wine-stable 11.0 debs, dpkg -x, i386-unix
  removed) moves Mesa/LLVM into a 64-bit host process: 1,778 MB below 4 GB, flat for 28 min. Install side by side in
  $HOME (no apt, no change to the live Wine), test on its own display/prefix/exe name, switch behind a variable.
- 2026-11-14 watch client: fixed-sleep GUI login scripts break under host load; wait for the screen state, not a clock.
- 15:40 gear audit (class QA, 830 bots): unidentified drops pile up because the Magnifier was bought only "when an
  upgrade is waiting" AND from the potion budget's leftovers (usually negative for a poor bot); utilities that unlock
  value (Magnifier 40z) must be standing stock bought first. Gear scored on DEF + price alone made every accessory and
  most headgear worth ~0, so no plan step ever chose one: score the item scripts' stat bonuses for the build.
- Restocks computed each part's weight room from the weight BEFORE the purchase (potions, arrows, wings each "fit"),
  so the sum overshot 50%: one shared weight budget per purchase.
- The live API hung on /api/bots and /api/stats (they lock every bot) while bots kept playing = one bot's mutex held
  forever. Without pprof on the main instance there is no way to find the holder short of killing it: /debug/pprof/
  is now on the API mux.
- 15:35 out of potions never waits (QA 20:17: pixelhunter, party leader, logged "town need waits for the next party
  run: out of potions" with 10 pots and died twice on gef_fild10; kiraknight the same on prt_fild10). ai.go trip
  switch: "out of potions" skips the trip interval, calls the party run with the short gap, and when the party can't
  go now (a run just happened) restocks alone (startTrip: announced, Butterfly Wing per tripWingOK).
  Process rule (coordinator): fixers never run `docker compose build` (it overwrote ro-legion:latest and jammed the
  docker daemon during a critical deploy) - go vet / go test only, one golang container at a time; the deploy agent
  builds images.

- 2026-11-14 travel QA (GM test char Trailmark, ro-openkore-qa): "Ayla can't reach the Orc Dungeon" was OpenKore's
  shipped Kafra table (official kRO destination lists) vs our F_KafSet lists: Prontera Kafra r5 took her to Alberta
  instead of Al De Baran, then the ship into Izlude. Lesson: never trust OpenKore's stock tables for server-specific
  NPC menus - generate them from the server's own scripts (openkore/fix_kafra_portals.py) and verify every NPC with a
  test character (openkore-qa/kafratest.sh, 39/39). Same for map cells: 99 OpenKore field files differ from the
  server's map cache (openkore/gen_fields_from_mapcache.py -> openkore/fields-srv), and for packet layouts of the
  2021 client (RefineUI, NPC market, RODEX read 0B63: openkore-qa/plugins/qa/pkt2021fix.pl).
- Party wait points + missing Kafras (life mind / travel-QA, ~20:10Z). Members' detour hold (party.go detourHold)
  flipped izlude -> prt_in -> prontera within a minute and members chased their leader's Kafra hops into other towns
  (Yukiqueen prontera -> alberta). Now partyhold.go stableHold: a member that must wait waits in the party's one regroup
  town (checkpoint = nearestTown of the party map; a building means its town), never in the leader's current stop, and
  keeps that choice 5 min while the party map is the same; staying put / the party map / following across a field
  portal are unchanged. kafra.go was missing the Izlude Kafra (izlude 128,148 -> Geffen/Payon/Morocc 120, Al De Baran
  180), Morocc's north Kafra (160,258) and the Pharos Beacon Kafra (cmd_fild07 136,134 -> Comodo/Morocc 120), checked
  against F_KafSet and npc/kafras/kafras.txt.
- 2026-11-14 (HEALER-ANCHOR / FOLLOW-HER-ROAD / NO-CRY-WOLF): a party healer had no escape anchor when its leader was
  on the next floor, so every mob made it Teleport to a random cell (Zendancer84: 13 teleports in 13 min on
  pay_dun00/01, up to 21 monsters, logged off); partyAnchor now gives a healer the party's tank, else the leader, else
  the nearest party member on the map, so it runs to them instead. Ayla's friends follow her road map by map
  (humanTrail: from a map she crossed, the map she went to next) and a strong party no longer routes to her lock map on
  its own; the escort gate holds friends in town only for dungeons; "come get me" in a lv50+ party only beyond 40
  cells or another map. Bogus 65535 party-list positions are ignored.
- (run 11) The minds' order inbox (POST /api/minds/{mind}/message) is the OWNER's channel: minds treat it as owner
  orders that override their plans. Engineers never post there; send what a mind should know to main, which relays it.
- (run 11) "Novice 1, offline" for 6 guild alchemists was a display/knowledge bug: levels of offline characters lived
  only in memory (char list at login), so after each restart the picker saw persona seeds. Persist what you learn
  about offline entities (data/char_levels.json) or every restart forgets it.
- 15:55 (my bug, run 6 reset.go): rAthena sends ZC_NOTIFY_STOREITEM_COUNTINFO (0x00f2) after EVERY item moved into
  storage, not only when the window opens. A handler that "deposits everything" on 0x00f2 re-ran per item, appended
  967 entries and rewrote a file each time while holding the bot's lock and a global lock every bot's think touches -
  the API (which locks every bot) hung for an hour. Rules: handle a window-open packet once per opening (flag on the
  trip); never file I/O under a lock taken from think; never take a global lock in every bot's tick; a unit test must
  replay the server's real packet sequence (repeated packets), not one call.
- 16:05 MVP maps (QA 20:49: the engine's best_map sent Ayla's lv72-80 party to pay_fild10; Eddga's Fire Ball, 8-11k,
  wiped it). partyMapTooHardHere now treats a map with a timed MVP spawn (bosshunt.go bossSpawns + the lookup's MVP
  flag: mapMVP) as too hard until the weakest effective level is the MVP's level + 20 - so best_map (bestFarmMap ->
  partyMapOK), party / solo / lfg / guild pickers and the fit check all avoid it; bossThreatMob makes an MVP in sight
  a threat to leave (bossWatch) below its level + 20; mapinfo / drops mark "unsafe: MVP X spawns here". Boss hunts
  (bosshunt.go) are exempt from the fit check. MVP maps hit: pay_fild10 (Eddga), gef_fild03 (Orc Hero), gef_fild10
  (Orc Lord), mjolnir_04 (Mistress), pay_dun04 / gld_dun01 (Moonlight Flower), moc_fild17 (Phreeoni), anthell02,
  gef_dun02, in_sphinx5, moc_pryd04, prt_maze03, prt_sewb4, treasure02, tur_dun04, the guild dungeons.
- 16:30 run 8: a restock's ORDER is a priority list - survival first (HP minimum, escape wings), then class needs
  (SP potions, arrows), then comfort (HP top-up). Arrows sat second with "everything above 300z" and a 3,000 cap from
  lv67, so a lv74 Hunter left town with 3,000 arrows and no potions or wings.

## 2026-11-15 02:45 - a docker cp fix is not permanent until the image is rebuilt or the file is bind-mounted
- Evidence: the Renewal Izlude fields/portals fix (openkore/fields-re, fix_re_portals.sh, fix_kafra_portals.py) was in
  openkore/Dockerfile and docker-cp'd into ro-openkore-ayla, but ro-openkore:latest (built 2026-11-12) was never
  rebuilt. The 20:43 recreate (with rathena) put the classic izlude field back (md5 4a569f48 vs repo 6b51347c) and Ayla
  looped "Cannot calculate a route from izlude (24,98) to yuno" at 02:39; the stock portals.txt also sent her to a
  prontera 98,120 "Juno" NPC that does not exist here (5 failed tries).
- Rule: a docker cp fix is not permanent until the image is rebuilt or the file is bind-mounted. openkore-ayla now
  bind-mounts ./openkore read-only and runs openkore/overlay.sh on every start (copies fields-re, patches portals.txt,
  logs "FIELD MISMATCH"/"PORTAL MISMATCH" to logs/overlay.log and logs/alerts.log when anything still differs).
