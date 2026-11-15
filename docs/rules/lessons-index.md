# Lessons index

Index into `legion/data/lessons.md` (not rewritten; 67 entries, 2201 lines). Each row: line range and the entry's own heading as
its one-line summary. Entries are grouped by topic by keyword of the heading, so a few sit under a near topic. Read the range
with `sed -n 'A,Bp' legion/data/lessons.md`. Line 1 is the file title; entries start at line 2.

## Ayla and her party (9)

- 8-34: 2026-11-13 evening - lessons from Ayla's party, applied to all bots (engineer)
- 35-42: 2026-11-13 Ayla sat inside payon_in01 after shopping
- 43-50: 2026-11-13 Ayla's friends walked to Prontera without her (party split 8+ min)
- 70-89: 2026-11-14 Party rally at the destination's Kafra (whole Legion) + Ayla's back-and-forth
- 1045-1249: 2026-11-14 - Ayla's party split 150 cells after a Prontera town run; 3-min hold deadlock at pay_arche
- 1549-1572: 2026-11-14 10:10 - a party member rested alone after a Fly Wing and died (core, Zen.fox on pay_dun01; built)
- 1573-1613: 2026-11-14 11:30 - Zen.fox (core) died twice in pay_dun01: Fly Wing away from the party, then a 45% respawn walk (built)
- 1703-1717: 2026-11-14 12:10 - Zen.fox 4th death and the priest 80 cells off after a relog (core, pay_dun01; built)
- 1718-1751: 2026-11-14 - Ayla's party grows to 6, Ayla's guild (built, needs legion + core deploy)

## Engine crashes and API wedge (2)

- 1028-1044: 2026-11-14 - legion SIGSEGV: readLoop re-read b.conn after the session nilled it
- 1779-1848: 2026-11-14 12:23 RELOADMOBDB-CRASH (main thread)

## Parties (8)

- 2-7: 2026-11-13 18:14 EST followers do not re-path to a leader who moved maps
- 51-69: 2026-11-13 Party priest stuck at 66/325 SP: 256 Red Potions, 85% weight, no weapon
- 172-238: 2026-11-14 routing / inbox / guild leaders (subagent)
- 465-479: 2026-11-14 Bot parties never existed in-game; parties now survive restarts (partypersist.go)
- 752-774: 2026-11-14 05:40 - party town runs were one per member need (party town-run agent; built, deploy ~06:05)
- 954-987: 2026-11-14 ~12:20Z - No back-and-forth Kafra trips (guild07 report: Sunheart's party, kafra.go / ai.go / party.go)
- 1484-1510: 2026-11-14 10:45 - economy run 7: vendor-alchemist, mail trades, parcels to open shops, party hand-offs (built)
- 1614-1624: 2026-11-14 11:45 - party tanks still Fly-Winging at 33-48% after the 11:02 deploy (escape agent; built)

## Escape, survival, deaths, danger (13)

- 330-368: 2026-11-14 - survival batch: one survive() check in every state; deaths with potions and Fly Wings unused
- 369-412: 2026-11-14 - death regression 6 -> 15-19/min: level-only map choice for bots with starter gear
- 499-524: 2026-11-14 03:20 - mjolnir_06 deaths: poison tracked, but the killer is the Spore swarm (poison.go)
- 525-538: 2026-11-14 03:27 - travellers died crossing mjolnir_06: route cost and transit now count swarms (swarm.go)
- 557-582: 2026-11-14 03:50 - escape items that do nothing; boss sightings are shared (escapeitem.go, bossnote.go)
- 698-708: 2026-11-14 Fly / Butterfly Wings never worked (ZC_AUTORUN_SKILL ignored)
- 709-732: 2026-11-14 travelStalled measured straight-line distance: winding Mjolnir paths looked "stuck"
- 733-751: 2026-11-14 05:35 - wings work now: Butterfly Wings were firing as town trips (escape-tuning agent; built, deploy ~06:05)
- 1295-1318: 2026-11-14 09:15 - "Fly Wing did nothing, then died" on the orc fields (escape agent; built, not deployed)
- 1339-1376: 2026-11-14 09:50 - beginners ping-pong pay_fild01 <-> pay_fild03; a Sage re-hit 3x crossing moc_fild03 (escape agent; built)
- 1625-1653: 2026-11-14 11:55 - wing "use refused", warp loops from teleport legs, legionctl nearby (escape agent; built)
- 1654-1702: 2026-11-14 11:50 - economy run 8: weapon errand, guide fund raided by a gear turn, purse floor, wing price (built)
- 2055-2097: 2026-11-14 14:45 - Fly Wing every 5 s from ONE monster at 84% (TheShadowsenpai) + beginner Kafra ping-pong (built)

## Routing, travel, stuck (6)

- 123-134: 2026-11-14 - walking across a warp (Alberta Novices stuck "travel -> pay_fild03")
- 298-329: 2026-11-14 Routing round 2: shop counters, rivers, doors, walls (built, deploy pending)
- 445-464: 2026-11-14 02:30 - roaming 20%: crowding, not a narrow fitShare
- 630-647: 2026-11-14 04:45 - empty maps: judge a map by what bots SEE, not by the spawn file
- 648-679: 2026-11-14 04:45 - "stuck ... unsticking" 90-650 s, always "try 1" (kafra-freeze agent; built, deploy ~05:35)
- 775-792: 2026-11-14 05:40 - warp guard refused every step beside a door (prontera 47,226)

## Economy, market, gear (19)

- 90-122: 2026-11-14 Casters never upgraded; weaponless acolytes; gear progression (gear agent)
- 135-155: 2026-11-14 Town trips "way too often", ~20% of bot time farming (owner: "90% of the time should be on farming")
- 156-171: 2026-11-14 Economy audit: trades work, but nothing to trade and no money
- 239-258: 2026-11-14 Economy live check: shops of Jellopy, cards nobody could afford
- 259-280: 2026-11-14 - "bought a Club and never equipped it" = looted, UNIDENTIFIED Clubs
- 281-297: 2026-11-14 - class weapons are a necessary buy; gifts only the shortfall; no unidentified gear in trades
- 413-428: 2026-11-14 Two rules that silently killed the market
- 435-444: 2026-11-14 02:25 - Bows need arrows (ammo is a consumable)
- 480-498: 2026-11-14 08:05 - arrow trips looped: the engine's shop list wasn't the server's (barter Dealer_Update)
- 539-556: 2026-11-14 03:40 - economy batch 9: goods reach buyers where buyers already are
- 583-629: 2026-11-14 04:20 - economy batch 10: zeny moves by mail; diaries must say what really happened
- 680-697: 2026-11-14 04:55 - economy batch 13: a skill that costs zeny drained the guild fund
- 811-858: 2026-11-14 05:45 - poor bow users sell junk for arrows; overweight bots leave cheap junk (arrows-loot agent; built)
- 859-915: 2026-11-14 06:12 - bow users "loaded" with an empty ammo slot: WearState is a location, not "worn" (archer agent; built)
- 916-953: 2026-11-14 06:40 - bots parked at 50-69% weight (no regen), bags of herbs/fruit/starter pots
- 988-1027: 2026-11-14 07:30 - economy batch 18: parcels that could never fire, vendors without Magnifiers, refused pitches
- 1319-1338: 2026-11-14 build review run 2 (bow users, assassins, merchants)
- 1990-2054: 2026-11-14 14:10 - economy run 9
- 2098-2201: 2026-11-14 15:30 - economy run 10

## Builds, classes, skills (9)

- 429-434: 2026-11-14 07:30 - classes not using their attack skills (SP always full)
- 1250-1294: 2026-11-14 build-and-strategy review (archer, acolyte, knight/crusader)
- 1377-1428: 2026-11-14 10:00 - errands: necessary steps go alone (build review run 3; built)
- 1429-1437: 2026-11-14 10:10 - build review run 3: songs, healer sit, Sage/Monk orders, learned bolts (built)
- 1438-1483: 2026-11-14 10:30 - server cap at transcendent classes (built; rathena part waits for a restart)
- 1511-1548: 2026-11-14 11:15 - build review run 4: 8 more variants, next-piece logic, Riding moved up (built, not deployed)
- 1752-1778: 2026-11-14 12:25 - key buffs starved by attack skills; spheres for Combo Finish (build-and-strategy run 5; built)
- 1849-1887: 2026-11-14 13:20 - build variety, Teleport card, DS hit-and-run (build-and-strategy run 5, part 2; built)
- 1888-1989: 2026-11-14 13:25 - Sage Mages without AoE, Storm Gust late, 3 variants with AI (build-and-strategy run 6; built)

## Other (1)

- 793-810: 2026-11-14 05:45 - regression: an empty-map fallback sent Novices into Payon Cave
