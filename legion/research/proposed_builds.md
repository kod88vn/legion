# Proposed changes to builds.go (for the main session to apply)

All skill orders below were machine-checked against this server's `db/re/skill_tree.yml` / `skill_db.yml`
(prerequisites satisfied in order, levels <= max) and fit the scripted job changes: Novice 9 points, **1st job 39
points** (job 10 -> 40), 2nd job 49 points (job 50; 69 as transcendent). Gear IDs were checked against
`item_db_equip.yml` and NPC shops that Legion can actually reach (RouteMaps from the start towns).

## A. Bugs found in the current plans (prerequisite dead-ends: the skill is never learned)
`planSkill` skips a skill that isn't `Upgradable`; if its prerequisite is not in the list at all, it is skipped forever.

| plan | blocked skill | missing prerequisite |
|---|---|---|
| wizard | Storm Gust (89) | Frost Diver 1 (15) |
| wizard | Earth Spike (90), Heaven's Drive (91) | Stone Curse 1 (16) |
| wizard | Meteor Storm (83) | Sight (10) -> Sightrasher 2 (81) |
| wizard/sage | Soul Strike (13) ordered before Napalm Beat 4 (only delays) | - |
| crusader | Devotion (255) | Reflect Shield 5 (252) |
| priest | Lex Aeterna (78) | Lex Divina 5 (76) |
| priest | Crucis (32) | Demon Bane 3 (23) |
| priest | Pneuma (25) ordered before Warp 4 (only delays) | - |
| monk | Finger Offensive (267) | Investigate 3 (266) |
| blacksmith | Weapon Research (107) -> Weapon Perfection (112) -> Maximize (114) | Hilt Binding 1 (105) |
| blacksmith | Repair Weapon listed at 5, max is 1 | - |
| assassin | Hiding (51) -> Cloaking (135) -> Grimtooth (137) | Steal 5 (only Steal 1 listed) |
| assassin | Enchant Poison (138), Poison React (139) | Envenom 1 (52) |
| rogue | Raid (214), Intimidate (219) | Tunnel Drive 2 (213) |
| hunter | Falconry (127) / Steel Crow (128) ordered before their prereqs (only delays) | - |

Also: the current 1st-job lists overflow 39 points (wizard 55, crusader 45, knight 42), so the tail of the 1st-job
list is learned in 2nd job before the 2nd-job skills - e.g. a Wizard puts 10 points into Soul Strike/Napalm before Jupitel.

`rathena/npc-custom/legion.txt` S_AutoSkill (human play-test Assassin) has the same Steal 1 / Hiding 1 issue (it uses
`skill`, which bypasses prerequisites, so it works but is not reachable by a real player).

## B. Gear: shops Legion cannot reach
`gearShop` prefers any `_in` map, chosen in random map order, so it can pick an unreachable shop:
- Thief Clothes (2335): sold only in Comodo (unreachable) -> remove from `armorThief`.
- Padded Armor, Chain Mail, Shield, Helm, Full Plate: also sold in `cmd_in01` (unreachable) besides `izlude_in`/`aldeba_in`/`prt_in`.
- Katars (1250/1252/1254): `morocc_in` (unreachable) or `ein_in01` (reachable).
- Instruments and whips (Bard/Dancer): only `cmd_in01` -> bards/dancers must stay on bows.

Fix suggestion (builds.go, `gearShop`): skip shops on maps where `RouteMaps("prontera", 156, 191, s.Map, 99) == nil`
(cache it), then prefer `_in`. Without it, some bots will walk toward Comodo/Morroc forever (or until `gearFailed`).

## C. Proposed plans (Go-ready)

```go
// shared routes (reachable NPC gear only)
armorLight  = []int{2301, 2305, 2321, 2307, 2309}             // cotton shirt, adventurer's suit, silk robe, mantle, coat
armorHeavy  = []int{2301, 2305, 2328, 2312, 2314, 2316}       // ... wooden mail, padded, chain mail, full plate
armorThief  = []int{2301, 2305, 2307, 2309, 2312, 2314}       // ... mantle, coat, padded, chain mail (Thief Clothes is Comodo-only)
armorRobe   = []int{2301, 2305, 2321, 2332}                   // ... silk robe, silver robe (mage line)
armorHoly   = []int{2301, 2305, 2321, 2325}                   // ... silk robe, Saint's Robe (prt_church; acolyte/monk/priest)
extrasMelee = []int{2220, 2226, 2228, 2501, 2503, 2505, 2401, 2403, 2405, 2627, 2608} // + Rosary (lv20, prt_church)
extrasCast  = []int{2220, 2226, 2232, 2501, 2503, 2505, 2401, 2403, 2405, 2627, 2608}
extrasHoly  = []int{2220, 2216, 2501, 2503, 2401, 2403, 2405, 2627, 2608}           // Biretta (prt_church)

"knight": Stats: st(stSTR, 20, stAGI, 20, stVIT, 20, stDEX, 15, stSTR, 40, stAGI, 40, stVIT, 35, stDEX, 25, stSTR, 60, stAGI, 60, stVIT, 50, stSTR, 80, stAGI, 75, stDEX, 40, stVIT, 60, stSTR, 90, stAGI, 85),
  Skills: sk(2, 1, 5, 10, 3, 10, 7, 5, 6, 5, 8, 1, 4, 7, 60, 10, 61, 5, 62, 10, 63, 1, 64, 5, 7, 10, 4, 10),
  Weapons: []int{1201, 1116, 1151, 1154, 1157}   // knife, Katana (Lv4, 2000z), Slayer, Bastard Sword, Two-Handed Sword: two-handers only (THQ/BB need them)

"crusader": Stats: st(stVIT, 20, stSTR, 20, stDEX, 15, stINT, 10, stVIT, 40, stSTR, 40, stDEX, 30, stINT, 25, stVIT, 60, stSTR, 60, stDEX, 45, stVIT, 80, stSTR, 75, stDEX, 55, stINT, 40),
  Skills: sk(2, 10, 5, 10, 6, 5, 8, 1, 4, 10, 7, 3, 248, 7, 253, 10, 248, 10, 249, 10, 250, 3, 251, 5, 252, 5, 254, 4, 63, 1, 64, 1, 255, 1),
  Weapons: []int{1201, 1104, 1107, 1110, 1113, 1126, 1119, 1129} // + Tsurugi (prt_in)

"wizard": Stats: st(stINT, 20, stDEX, 15, stVIT, 10, stINT, 40, stDEX, 30, stVIT, 20, stINT, 60, stDEX, 50, stVIT, 35, stINT, 80, stDEX, 65, stVIT, 50, stINT, 99, stDEX, 80, stVIT, 60),
  Skills: sk(19, 10, 14, 10, 20, 10, 11, 1, 15, 1, 16, 1, 10, 1, 17, 5, 84, 10, 89, 10, 90, 3, 91, 3, 21, 1, 85, 10, 18, 5, 81, 2, 83, 5),

"sage": Stats: (as wizard),
  Skills: sk(19, 10, 14, 10, 20, 10, 11, 1, 15, 1, 16, 1, 10, 1, 17, 5, 274, 5, 283, 1, 90, 5, 91, 5, 275, 1, 278, 4, 279, 10, 276, 3, 280, 2, 281, 2, 278, 10, 274, 10),

"hunter": Stats: st(stDEX, 20, stAGI, 10, stDEX, 40, stAGI, 20, stVIT, 10, stLUK, 10, stDEX, 60, stAGI, 40, stLUK, 20, stVIT, 20, stDEX, 80, stAGI, 60, stLUK, 30, stDEX, 99, stAGI, 75, stVIT, 30, stLUK, 40),
  Skills: sk(46, 10, 43, 3, 44, 10, 45, 10, 47, 5, 43, 4, 126, 1, 127, 1, 129, 5, 128, 10, 115, 1, 117, 3, 116, 5, 120, 1, 121, 1, 119, 1, 122, 1, 118, 1, 123, 5, 126, 10, 43, 8),

"bard":   Stats: st(stDEX, 20, stAGI, 10, stDEX, 40, stAGI, 25, stVIT, 15, stDEX, 60, stAGI, 40, stVIT, 25, stINT, 20, stDEX, 80, stAGI, 55, stVIT, 40, stINT, 40, stDEX, 99),
  Skills: sk(46, 10, 43, 3, 44, 10, 45, 10, 47, 5, 43, 4, 43, 10, 315, 10, 316, 5, 304, 1, 305, 1, 317, 3, 321, 10, 319, 5),
"dancer": Stats: (as bard),
  Skills: sk(46, 10, 43, 3, 44, 10, 45, 10, 47, 5, 43, 4, 43, 10, 323, 10, 324, 5, 304, 1, 305, 1, 325, 3, 330, 10, 327, 5),
  // weapons stay bows (instruments/whips are Comodo-only)

"priest": Stats: st(stINT, 20, stVIT, 15, stDEX, 10, stINT, 40, stVIT, 30, stDEX, 25, stINT, 60, stVIT, 50, stDEX, 35, stINT, 80, stVIT, 70, stDEX, 45, stINT, 99, stVIT, 85),
  Skills: sk(28, 3, 22, 5, 34, 10, 29, 10, 28, 10, 35, 1, 24, 1, 33, 2, 73, 10, 74, 3, 66, 3, 67, 3, 70, 7, 75, 1, 26, 2, 27, 4, 25, 1, 31, 1, 68, 4, 76, 3, 74, 5, 72, 1, 9, 4, 54, 1, 77, 3, 76, 5, 78, 1, 12, 1, 79, 10, 75, 5),
  Armors: armorHoly, Extras: append(append([]int{}, extrasHoly...), shields...),

"monk": Stats: st(stSTR, 20, stAGI, 20, stVIT, 15, stDEX, 15, stSTR, 40, stAGI, 40, stVIT, 25, stDEX, 25, stSTR, 60, stAGI, 60, stVIT, 40, stDEX, 35, stINT, 15, stSTR, 80, stAGI, 80, stVIT, 50, stDEX, 45, stSTR, 90),
  Skills: sk(22, 10, 23, 10, 28, 3, 29, 10, 34, 6, 259, 5, 261, 5, 265, 5, 263, 10, 272, 5, 273, 5, 262, 1, 270, 3, 259, 10, 266, 3, 267, 3, 271, 5),
  Weapons: []int{1501, 1504, 1801, 1803, 1805, 1807}, Armors: armorHoly, Extras: extrasHoly,

"blacksmith": Stats: st(stSTR, 20, stDEX, 15, stVIT, 10, stAGI, 10, stSTR, 40, stDEX, 30, stVIT, 25, stAGI, 25, stSTR, 60, stDEX, 45, stVIT, 40, stAGI, 45, stSTR, 80, stDEX, 55, stAGI, 60, stVIT, 50, stLUK, 20, stSTR, 90),
  Skills: sk(36, 5, 39, 3, 42, 10, 41, 3, 37, 3, 38, 10, 36, 10, 110, 2, 111, 5, 105, 1, 107, 2, 112, 3, 113, 5, 107, 10, 112, 5, 114, 5, 110, 5, 108, 1, 109, 5, 39, 10),

"alchemist": Stats: st(stSTR, 20, stDEX, 15, stINT, 10, stVIT, 15, stSTR, 40, stDEX, 30, stVIT, 30, stINT, 25, stSTR, 60, stDEX, 45, stVIT, 45, stINT, 40, stSTR, 75, stDEX, 60, stVIT, 60),
  Skills: sk(36, 5, 39, 3, 42, 10, 41, 3, 37, 3, 38, 10, 36, 10, 226, 10, 227, 5, 228, 5, 231, 5, 238, 1, 244, 1, 243, 1, 247, 1, 227, 10, 228, 10, 229, 5, 230, 5),
  // Acid Terror / Bomb last: they need Acid Bottle / Bottle Grenade, which bots can't buy (see D.3)

"assassin": Stats: st(stAGI, 20, stSTR, 20, stDEX, 10, stAGI, 40, stSTR, 35, stLUK, 15, stVIT, 15, stAGI, 60, stSTR, 50, stLUK, 30, stDEX, 25, stVIT, 30, stAGI, 80, stSTR, 70, stLUK, 40, stDEX, 35, stAGI, 90, stSTR, 85, stVIT, 40),
  Skills: sk(48, 10, 49, 10, 50, 5, 51, 2, 52, 10, 53, 1, 51, 3, 134, 4, 136, 10, 134, 10, 135, 2, 137, 5, 138, 5, 139, 5, 135, 5, 140, 5),
  Armors: armorThief,

"rogue": Stats: st(stAGI, 20, stSTR, 20, stDEX, 15, stAGI, 40, stSTR, 35, stVIT, 20, stDEX, 25, stAGI, 60, stSTR, 50, stVIT, 30, stDEX, 40, stAGI, 75, stSTR, 70, stVIT, 40, stDEX, 50, stAGI, 85, stSTR, 80),
  Skills: sk(48, 10, 49, 10, 50, 5, 51, 1, 52, 10, 53, 1, 51, 3, 210, 4, 211, 4, 212, 4, 213, 2, 214, 5, 212, 10, 219, 5, 44, 10, 46, 5, 225, 4),
  Armors: armorThief,
```

Points check (1st / 2nd job): knight 39/39, crusader 39/50, wizard 39/49, sage 39/49, hunter 39/49, bard/dancer 39/41,
priest 39/69 (Resurrection, Turn Undead, Magnus come in High Priest), monk 39/55, blacksmith 39/49, alchemist 39/49,
assassin 39/45, rogue 39/49. Overflow simply continues after rebirth.

## D. Engine ideas outside builds.go (not applied; files owned by others)
1. **progress.go offense**: Mage line should cast Earth Spike (90) vs Wind targets and Soul Strike (13) vs Ghost; Jupitel
   first is fine vs Water/neutral but wastes 47 SP vs targets a bolt kills. Priests should use Turn Undead (77) / Magnus (79)
   on Undead/Demon targets and Holy Light; Acolytes can Heal-damage Undead (heal on monster = holy damage).
2. **skills.go boltFor**: add `case "Wind": Earth Spike if known`, `case "Ghost": Soul Strike if known`, `case "Undead": Fire`.
   (Fire vs Wind is only 100%; Lightning vs Wind 25%.)
3. **progress.go merchant offense** lists Acid Terror first: without Acid Bottle (item 7135) in the bag it fails every cast.
   Check inventory before choosing it.
4. **party.go**: the leader should choose `GuideSpot.Party` maps for the party's band (lowest member level); now possible
   because `guideSpotsFor` doubles Party spots for bots in a party.
5. **town.go**: buy Green Potions (511) for casters going to Harpy maps, Fly Wings (601) as an aggro escape, and switch
   Red -> Orange (from ~30) -> Yellow (from ~50) potions.
