# Ragnarok Online Renewal: levelling and play strategy (for Legion)

Digest of community knowledge for the 2020-2021 kRO/iRO Renewal era, checked against **this server's own data**
(rAthena `db/re/mob_db.yml`, `npc/re/mobs`, `db/re/attr_fix.yml`, warp graph). Machine-readable version:
`strategy_data.json`. The engine table lives in `../strategy.go` (`guideSpots`).

Sources: [iRO Wiki Leveling Spots](https://irowiki.org/wiki/Leveling_Spots), [iRO Wiki Experience](https://irowiki.org/wiki/Experience),
class pages ([Hunter](https://irowiki.org/wiki/Hunter), [Priest](https://irowiki.org/wiki/Priest), [Wizard](https://irowiki.org/wiki/Wizard),
[Monk](https://irowiki.org/wiki/Monk), [Assassin](https://irowiki.org/wiki/Assassin)), [Tranquil RO Renewal leveling guide](https://tranquil-ragnarok-wiki.gitbook.io/renewal/getting-started-in-tranquil-ragnarok/leveling-guide),
[Penghaw's Newbie Guide to Mid Game](https://wiki.muhro.eu/Penghaw's_Newbie_Guide_To_Mid_Game), r/RagnarokOnline FAQ, plus the rAthena data files above.

## 0. This server, in numbers
| setting | value | consequence |
|---|---|---|
| base / job exp | 10x | Novice -> 1st job in minutes, 2nd job (job 40) around base 40-50 |
| drops | 1x (cards 0.01%) | loot sells mostly to NPCs; cards are genuinely rare and valuable |
| `mob_count_rate` | **200%** | fields are twice as crowded as iRO: aggressive maps are much more dangerous, passive maps much faster |
| `party_share_level` | 15 | even share only if all members are within 15 base levels |
| `party_even_share_bonus` | 0 | no +20%/member bonus here: a party is about safety and kill speed, not bonus exp |
| job changes | job 10 / job 40 (scripted) | 1st job has only **39** skill points, 2nd job 49 (69 transcendent) |

**Monster exp in this Renewal DB is nearly flat** (a Lv1 Poring gives 150 base exp, a Lv15 Rocker 174, a Lv44 Orc
Warrior 361) while HP grows fast. Exp per HP is therefore highest on the weakest monster you still get ~100% for,
and the Renewal level modifier decides the rest:

| monster lv - your lv | exp | | monster lv - your lv | exp |
|---|---|---|---|---|
| +16 or more | 40% | | -6 .. -10 | 95% |
| +10 | **140%** (peak) | | -11 .. -15 | 90% |
| +5 | 115% | | -16 .. -20 | 85% |
| +2 .. -5 | 100% | | -21 .. -25 | 60% |
| | | | -26 .. -30 / -31+ | 35% / 10% |

Rule of thumb: hunt monsters from ~5 below to ~10 above your level; move on once they are 20+ below you.

## 1. Reachability (important for Legion)
Legion walks the warp graph only (no NPC transports). Verified with `RouteMaps` from all five start towns:
142 maps with spawns are reachable. **Not reachable**: Byalan (`iz_dun*`, Izlude boat), Ant Hell, Prontera Culvert,
Sunken Ship / Turtle Island (Alberta sailors), Sograt desert beyond `moc_fild03/13`, Sphinx, Pyramids, Morroc town,
Comodo / Umbala / Amatsu / Ayothaya, Rachel Sanctuary, Abyss Lake, Bio Lab, the new-world maps.
Consequences: Thief Clothes, katars from Morroc, Comodo instruments/whips cannot be bought (see proposed_builds.md).
Rachel, Lighthalzen, Juno, Einbroch, Hugel, Veins, Al De Baran, Glast Heim **are** reachable (via the Shadow
Workshop `s_atelier` warps and Mt. Mjolnir).

## 2. Levelling route by band
Columns: towns nearby / melee / ranged & magic / party. Aggro = monsters attack on sight. Element = what to hit with.

| band | melee solo | ranged / magic solo | party / dungeon | why |
|---|---|---|---|---|
| 1-12 | `prt_fild08` (Poring/Lunatic/Fabre, by Prontera+Izlude), `gef_fild07` (Pupa never fight back, Geffen), `pay_fild03`/`pay_fild01` (Fabre/Willow, Payon+Alberta) | same + `gef_fild00` Chonchon | - | 50-60 HP monsters die in 1-3 hits: best exp/HP of the game (2.2-3.0) |
| 8-24 | `prt_fild07` Rocker/Savage Babe, `mjolnir_09` Savage Babe, `prt_fild05` Hornet (fast!) | `gef_fild04` **Mandragora** (stationary, ranged only), `gef_fild01`/`prt_fild00` Roda Frog (lightning) | - | passive earth/water monsters; Vocal mini-boss on prt_fild07 is a Novice killer |
| 12-28 | `pay_fild06` Wormtail, `pay_fild02` Boa, `pay_fild08` Spore | `prt_fild04` Ambernite (high DEF: magic/arrows) | - | Payon forest is the 2nd beginner hub |
| 18-36 | `moc_fild01/02` Muka/Peco Peco (assist!), `pay_fild07`/`mjolnir_01` Bigfoot/Caramel, `gef_fild05` Creamy | `prt_fild02` Stainer (earth/fire), `mjolnir_06` Poison Spore (AoE, aggro) | `pay_dun00` Payon Cave 1 (Zombies aggro, undead: Holy/fire), `mjo_dun01` | first undead dungeon; acolytes heal-damage zombies |
| 28-48 | `pay_fild09` Horn, `gef_fild09`/`gef_fild02` Coco, `prt_fild03` Yoyo (aggro) | `prt_fild10`/`mjolnir_02` Elder Willow (fire plants: cold bolt) | `pay_dun01` Payon Cave 2 (Soldier/Archer Skeleton), `mjo_dun02` | archers skeletons shoot: priests cast Pneuma |
| 40-60 | `moc_fild03` Wolves, `mjolnir_10` Argos, `gef_fild11` Goblins | `gef_fild10`/`gef_fild03` **Orc Village** (mobby, all aggro, fire AoE), `moc_fild13` Steel Chonchon | **`orcsdun01` Orc Dungeon** (Orc Zombie mobs: Magnus/Turn Undead/Storm Gust), `pay_dun02`, `mjo_dun03`, `gef_dun00` (Hunter Flies) | the classic party band |
| 50-72 | `pay_fild10` / `mjolnir_07/08` Savage/Beetle King/Greatest General (**all passive**), `prt_fild09` Magnolia | `prt_fild11` Goblin archers | `orcsdun02`, `pay_dun03` Sohee (passive, wind), `gef_dun01` Ghoul/Jakk/Nightmare | passive high-HP fields = safe solo grind |
| 58-84 | `mjolnir_04/03/05/11` Driller/Side Winder/Argiope (aggro), `yuno_fild01/12` Geographer (passive) | `mjolnir_12` Dustiness, `gef_fild08` Petite (Sky) corners (AoE, earth) | - | Mt. Mjolnir is also the road to Al De Baran - low levels die crossing it |
| 70-92 | `yuno_fild08/09/11` Grand Peco/Goat (fire: water weapons, passive), `ein_fild06/07/08` Holden/Metaling (passive) | `yuno_fild04` Harpy (silence: green pots), `gl_step` Wind Ghost, `gef_fild06` Petite (Ground) | `c_tower1/2` Clock Tower, `gl_church` Glast Heim Church (Evil Druid/Wraith, holy) | |
| 80-104 | `ra_fild12` Roween (earth), `ra_fild01` Desert Wolf (water), `hu_fild01/05` Novus, `ra_fild08` Anacondaq | `lhz_fild02/03` Breeze/Stem Worm (earth) | `gl_chyard` Dark Priest, `gl_prison/1` Underprison, `c_tower3`, `alde_dun02`, `ein_dun01/02` | Glast Heim = the undead party zone 80-110 |
| 92-120 | `ve_fild01/02/04/07` Drosera/Stapo/Muscipular (passive, fire), `ice_dun01` Siroma (passive, wind) | `ve_fild03` Magmaring (passive looters bunch on a dropped item: AoE them) | `mag_dun01/02` Nogg Road (mag_dun02 extremely mobby, AoE party), `gl_sew02/03/04`, `ice_dun02/03`, `ra_fild05/06` Kobolds | |
| 104-150 | - | - | `gl_knt01/02` Raydric/Khalitzburg, `juperos_01` Venatu (mobby), `gl_cas02`, `thor_v01/02` | few reachable maps; generic spawn search fills in |
| 150-195 | `ra_fild10/11` Ashen Goat | - | `verus03`, `ver_eju` robots | iRO's real 100-175 path (Abyss, Bio Lab, Rachel Sanctuary, Dicastes, Oz) needs NPC/quest access |

Towns: Prontera/Izlude -> prt_fild*, moc_fild01-03, mjolnir_*, Clock Tower via Mt. Mjolnir; Payon/Alberta -> pay_fild*,
pay_dun*; Geffen -> gef_fild*, gef_dun*, Orc Village/Dungeon, Glast Heim (through gef_fild06).

## 3. Class notes (Renewal PvM)
Skill orders below are prerequisite-checked against this server's `skill_tree.yml` and fit 39 (1st job) / 49 (2nd job)
points; the exact Go-ready lists are in `proposed_builds.md`.

- **Knight** (tank): AGI/STR 2H sword -> Bowling Bash. Bash 10, 2H Mastery 10, Magnum Break 5, Provoke 5, Endure 1, then THQ 10, Counter Attack 5, Bowling Bash 10, Peco. BB pushes the target through the pile: pull 3-5, BB. Katana (Lv4, 2,000z) -> Slayer -> Bastard Sword -> Two-Handed Sword, all NPC. Knights tank parties, so VIT 40-60 matters more than iRO "glass" builds.
- **Crusader** (tank): VIT/STR shield + Holy Cross (holy) -> best in undead dungeons. Faith 7 -> Holy Cross 10 -> Faith 10 -> Auto Guard 10 -> Shield Charge/Boomerang -> Reflect Shield 5 -> Grand Cross 4 (-> Devotion). Faith 10 also unlocks Heal for undead.
- **Wizard** (AoE): INT/DEX/VIT. Bolts 10 each, Frost Diver 1, Stone Curse 1, Sight, Fire Ball 5 in 1st job; then Jupitel 10, Storm Gust 10 (needs Frost Diver 1 + Jupitel 3), Earth Spike/Heaven's Drive, Lord of Vermilion, Fire Wall, Meteor Storm (needs Sightrasher 2). Storm Gust behind a tank is the iconic party farm (Orc Dungeon, Glast Heim, Nogg Road). Renewal cast: variable cast reaches 0 at DEX*2+INT = 530; 20% of cast is fixed.
- **Sage**: Free Cast (walk while casting) + Auto Spell, Earth Spike via Seismic Weapon 1, endows party weapons (Flame Launcher vs earth, Frost Weapon vs fire), Dispell, Land Protector.
- **Hunter**: DEX/AGI/LUK falconer. Double Strafe 10, Owl/Vulture/Concentration, Arrow Shower 5; Beast Bane, Falconry, Blitz Beat 5, Steel Crow 10, traps (Ankle Snare, Land Mine, Claymore Trap needs Shockwave + Blast Mine chain). Auto-Blitz procs at ~LUK/3 % and costs no SP; element arrows (fire vs earth, crystal vs fire, wind vs water).
- **Bard / Dancer**: level as archers (Double Strafe) until songs; instruments and whips are only sold in Comodo (unreachable here), so on this server they stay bow users. Songs: Poem of Bragi, A Whistle / Service For You, Fortune's Kiss.
- **Priest**: Heal 10, Blessing 10, Increase AGI 10, Angelus 2 (Kyrie prereq), Ruwach, Cure; then Kyrie 10, Magnificat, Impositio, Suffragium 3, Sanctuary 7, Gloria, Warp/Pneuma, Aspersio, Lex Divina -> Resurrection (needs Status Recovery 1 + SP Recovery 4) -> Turn Undead -> Lex Aeterna -> Safety Wall 1 -> Magnus Exorcismus. Exorcist leveling: Turn Undead/Magnus in Orc Dungeon and Glast Heim.
- **Monk**: Divine Protection 10 + Demon Bane 10 (Iron Fists prereq, uses 20 of 39 points), Increase AGI; then Iron Fists, Spirit Spheres 5, Dodge 5 -> Triple Attack 10 -> Chain Combo 5 -> Combo Finish 5 -> Absorb Spirits/Fury -> Investigate (ignores DEF) -> Finger Offensive -> Asura Strike.
- **Blacksmith**: Mammonite 10, cart, Discount/Overcharge (Overcharge 10 = +24% when selling loot); Hammer Fall 2 -> Adrenaline Rush 5 -> Hilt Binding -> Weapon Research -> Weapon Perfection -> Power Thrust 5 -> Maximize. Axes: Battle Axe -> Hammer -> Buster -> Two-Handed Axe.
- **Alchemist**: Axe Mastery 10, Potion Research/Prepare Potion (brew to sell), Aid Potion, Homunculus; Acid Terror/Bomb need Acid Bottle / Bottle Grenade (crafted, not NPC-sold) - only useful once the bot can make them.
- **Assassin**: Double Attack 10, Improve Dodge 10, Steal 5 -> Hiding 2, Envenom; Katar Mastery 4 -> Sonic Blow 10 -> Katar Mastery 10 -> Cloaking 2 -> Grimtooth 5 (AoE from cloak) -> Enchant Poison -> Poison React. Katars double crit rate; AGI assassins dodge-tank.
- **Rogue**: Steal 5 -> Hiding (Tunnel Drive prereq), Snatcher 4 (auto-steal), Steal Coin 4, Back Stab 10 (works from front in Renewal), Tunnel Drive 2 -> Raid 5 (AoE stun from hiding), Intimidate, Vulture's Eye 10 -> Double Strafe (bow rogue), Plagiarism.

## 4. Party tactics
- **Composition**: 1 tank (Knight/Crusader/Monk or AGI Assassin), 1 priest, 1-3 damage (wizard best for mobbing, hunter/assassin single target). A second priest or a sage for long dungeon sessions.
- **Tank / bait**: tank walks ahead, pulls 2-5 monsters (Provoke / Shield Boomerang / first hit), backs into a corner so mobs stack; wizard drops Storm Gust/Magnus on the stack; ranged never pulls. Tank peels monsters off the priest first (priority: anything hitting the priest > anything hitting ranged > own targets).
- **Priest priorities**: Heal tank below ~65%, squishies below ~75% > Kyrie on tank (refresh when broken) > Blessing + Increase AGI on all (refresh every ~4 min) > Magnificat when the party sits / SP low > Sanctuary under the tank when 3+ mobs > Impositio on damage dealers > Aspersio on melee vs undead/demon > Pneuma vs archers. Keep 20%+ SP for emergency heals; announce "sp" and sit.
- **Resting**: whole party sits together after a pull when the priest or wizard is under ~30% SP or anyone under 40% HP; Magnificat doubles SP regen. Pull again only when the priest is back over ~70% SP.
- **Kiting**: archers/mages keep >= 3 cells, step back after each shot/cast; Fire Wall / Ice Wall / Ankle Snare between you and melee mobs; Frost Diver / Storm Gust freeze.
- **Exp share (rAthena)**: even share requires every online member within 15 base levels and on the same map; the share goes to members present. No bonus here (`party_even_share_bonus 0`).
- **Power-levelling etiquette**: a high-level friend outside the 15-level window can't even-share; classic "leech" is the high player killing while the low one sits in party within range (only works with level gap <= 15), or a priest "support-levelling" by buffing/healing a party. Kill-stealing other players' monsters is rude: if someone is already fighting a monster, leave it; ask before joining a crowded spot ("mind if I hunt here?").
- **MVP etiquette**: whoever does most damage gets the MVP reward; don't steal another party's MVP; call "MVP" in party chat (Orc Lord in Orc Village, Phreeoni, Golden Thief Bug, Doppelganger in Geffen Dungeon 3).

## 5. Economy
- **What sells**: cards (any card is worth 10k-millions; never NPC-sell a card), rare equips (slotted weapons/armor), and high-volume Etc to NPCs: Jellopy, Fluff, Shell, Feather, Sticky Mucus, Orcish Voucher, Rotten Bandage, Skel-Bone, Horrendous Mouth, Iron Ore/Elunium/Oridecon (refine materials sell to players). Strawberries/Grapes (Spore, Elder Willow) and Yggdrasil leaves/berries sell to players. Potions brewed by alchemists sell to players at near NPC price.
- **Vending**: merchants price Etc at 1.2-1.6x NPC sell price, refine ores and pots near NPC buy price, cards by rarity; Overcharge 10 (+24%) for NPC sales, Discount 10 (-24%) for NPC buying. Vendors sit in Prontera main street.
- **Potions**: Red (50z) early, Orange (200z) from ~30, Yellow (550z) from ~50; Fly Wings (60z) for stuck/aggro escape, Butterfly Wing (300z) to go home; Green Potion vs silence/poison. Buy at Tool Dealers (Prontera, Payon, Geffen, Alberta, Al De Baran). A merchant with Discount saves ~24%.
- **When to go to town**: weight >= 50% (above 50% no natural regen; 90% no attacking), out of potions, or loot worth more than ~10x the trip; always before entering a dungeon.
- **Refining**: Elunium/Oridecon at the refiner (Prontera, Geffen); weapons +4 (Lv1) / +7 (Lv2) / +6 (Lv3) / +5 (Lv4) and armor +4 are safe (no break). Safe-refine NPC gear early; buy better slotted gear later.
- **Kafra**: storage (40z) for cards/rare loot; save point at your training town; Kafra teleport between towns costs zeny but saves walking.

## 6. Elements and races
Attack element (row) vs monster element level 1 (full 4-level table in strategy_data.json):

| atk \ def | Neutral | Water | Earth | Fire | Wind | Poison | Holy | Dark | Ghost | Undead |
|---|---|---|---|---|---|---|---|---|---|---|
| Neutral | 100 | 100 | 100 | 100 | 100 | 100 | 100 | 100 | **90** | 100 |
| Water | 100 | 25 | 100 | **150** | 90 | 150 | 100 | 100 | 100 | 100 |
| Earth | 100 | 100 | 25 | 90 | **150** | 150 | 100 | 100 | 100 | 100 |
| Fire | 100 | 90 | **150** | 25 | 100 | 150 | 100 | 100 | 100 | **125** |
| Wind | 100 | **150** | 90 | 100 | 25 | 150 | 100 | 100 | 100 | 100 |
| Holy | 100 | 100 | 100 | 100 | 100 | 75 | 0 | **125** | 100 | **125** |
| Ghost | 90 | 100 | 100 | 100 | 100 | 75 | 90 | 90 | **125** | 100 |

Higher element levels exaggerate it (Holy vs Undead 4 = 200%, Neutral vs Ghost 4 = 0%).
- Cold Bolt vs Fire (Peco, Elder Willow, Grand Peco, Goat, Desert Wolf, Magmaring); Lightning vs Water (Roda Frog, Spore, Marina, Sohee, Magnolia); Fire vs Earth and Undead (Willow, Rocker, Mandragora, Savage, Holden, zombies); **Earth Spike vs Wind** (Chonchon, Hornet, Creamy, Stainer, Dustiness, Harpy, Petite Sky, Roween) - note lightning does 25% there; Soul Strike / Holy vs Ghost (Whisper, Nightmare, Marionette).
- Holy vs Undead and Dark/Demon (Turn Undead, Magnus, Holy Cross, Aspersio); Poison monsters take 125-150% from all four bolt elements.
- Race: Demon Bane / Divine Protection (undead+demon), Beast Bane (brute+insect), Magnus hits only undead/demon.
- **Low-level dangers**: Mandragora (melee Novices), Vocal on prt_fild07, Poison Spore (aggro + poison), Hornet/Chonchon/Thief Bug swarms, Peco Peco assists, Smokie/Yoyo loot your drops, Payon Cave Zombies, Elder Willow (aggro Lv34), Hunter Fly (Lv63, aggro, fast; also on mjolnir_12), Orc Village trains (+ Orc Lord), Mt. Mjolnir's Argiope/Side Winder on the road to Al De Baran, Harpy silence.

## 7. What Legion should do with this (summary)
1. `strategy.go` (done): 117 reachable guide spots covering 1-195, Party flag for dungeons, class affinity weighting.
2. `proposed_builds.md`: prerequisite-valid skill orders (several current ones dead-end), VIT for tanks/casters, gear routes without unreachable shops.
3. Engine ideas (not applied): bolt choice should use Earth Spike vs Wind and Soul Strike vs Ghost; party leader should pick `Party` spots in party band; priests should use Turn Undead/Magnus on undead maps; gearShop must prefer reachable shops.
