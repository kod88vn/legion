# Build audit 2026-11-15

Read-only audit of the 37 builds. Nothing was changed. Sources checked: `legion/legion/builds.go` (Stats, LegacyStats,
Skills, Legacy, Weapons, Armors, Extras), `legion/data/buildmilestones.json`, `legion/data/buildgear.json` (the gear and card
table the milestones point to), `legion/tools/design/*`, `legion/tools/wrongbuilds.py`, `legion/docs/classes/*.md`.

Server truth was read from the running `ro-rathena` container (read only, `docker exec cat`): `db/re/statpoint.yml`,
`job_exp.yml`, `job_stats.yml`, `job_aspd.yml`, `skill_tree.yml`, `skill_db.yml`, `item_db_equip.yml`, `item_db_etc.yml`,
`item_db_usable.yml`, `mob_db.yml`, `db/import/item_db.yml`, `conf/import/battle_conf.txt`, `conf/battle/player.conf`,
`src/map/pc.cpp`, and the stock shop scripts under `npc/re`. No live data (DB, wine, running-state files) was touched.

## 1. What was verified and is correct

| check | result | source |
|---|---|---|
| stat points at base 99 | 1273; `planlib.stat_points` and `persona.go statPoints` equal `statpoint.yml` for every level 1-99 | statpoint.yml |
| stat cost per point | `(v-1)/10+2` is the rAthena rule below 100 | pc.cpp PC_STATUS_POINT_COST |
| stat cap | 99 (`max_parameter` 99); no plan goal exceeds 99; no goal is a no-op repeat | player.conf |
| job levels | Novice 10, every 1st and 2nd job 50, Super Novice 99; Novice 9 + 1st job 39 (engine changes at job 40, legion.txt:61) + 2nd job 49 = 88 plan points | job_exp.yml |
| plan skills exist, belong to the build's own Novice / 1st / 2nd tree, are within max level, and every prerequisite is also a goal at the needed level | all 37 plans pass | skill_tree.yml |
| no plan lists more than 88 skill points, or more than 49 in the 2nd job | pass | |
| no plan's stat goals exceed 1273 points | pass (max 1273, `crusader_gc`) | |
| milestones JSON replays | every stat row and skill row of every band of every build equals a fresh replay of `planlib` against the live tables (0 mismatches); `stat_points_available` equals statpoint.yml | |
| card ids exist, are type Card and sit in a slot the card can be worn in | pass for every card in every band (no wrong-slot card) | item_db |
| band item ids exist and the slot matches; no item needs a level above the band's max except the Belt (F2) | pass otherwise | item_db |
| weapon type fits the build in every `buildgear.json` band (bow lines bow, Sonic Blow katar, Two-Hand Quicken Knight 2h sword, Hindsight book, spear plans spear) | pass | item_db SubType |
| ASPD at 99 without buffs (Renewal amotion rule, `job_aspd.yml`) | 143 to 175 for every plan, nothing implausible (per-build table, section 4) | job_aspd.yml |

## 2. Findings, ranked by impact

Impact: H = the engine or the owner-facing data does the wrong thing now; M = wrong or misleading, partly absorbed by a
fallback; L = cosmetic, historical or latent.

| # | impact | build(s) | field | value now | correct / expected | source |
|---|---|---|---|---|---|---|
| F1 | H | all 37 (milestone replay); 16 plans in game | skill replay and first-job budget | `planlib.sim_skills` lets 2nd-job points be spent while the first-job pool still holds points. The server does not: 2nd-job skills stay out of the tree until total spent points reach 9 + (change job level - 1) = 48 (`player_skillup_limit: yes`, `pc_calc_skilltree_normalize_job_sub`; `change_level_2nd` is set to the job level at the change, pc.cpp:10891). 16 plans list fewer than 39 first-job points: crusader 38, bard 35, dancer 35, knight_spear 35, crusader_gc 35, monk 33, alchemist 35, sage_support 29, alchemist_battle 35, alchemist_potion 35, blacksmith_mace 35, rogue_bow 29, knight_pierce 35, crusader_spear 33, wizard_fire 33, wizard_frost 32 | each plan puts at least 39 points into Novice/1st-job skills, or the replay models the lock. Today the missing 1 to 10 points are spent by the fallback in `progress()` (`skillPriority`, then lowest skill id): unplanned 1st-job skills, and the planned 2nd-job skills arrive later than `buildmilestones.json` says | pc.cpp 2893-2930, 10891; player.conf:35; progress.go:226-243 |
| F2 | H | hunter, hunter_blitz (hunter_agi, hunter_trap by fallback), bard, dancer (arrow variants by fallback) | `buildgear.json` bands 1-20 and 33-99 | accessory list `[Belt 2627]` in band 1-20 (Belt needs level 25, band max 20); garment list `[Manteau 2506]` in band 33-99 (Archer line cannot wear Manteau) | every slot lists at least one item the class can wear at the band's levels | item_db EquipLevelMin, Jobs |
| F3 | H | priest, priest_me, priest_battle | `buildgear.json` band 55-99 | garment `[Manteau]`, shoes `[Boots]`: the Acolyte line wears neither | Hood / Muffler, Sandals / Shoes (Acolyte line can wear them) | item_db Jobs |
| F4 | H | wizard, sage, sage_support, sage_hindsight | `buildgear.json` bands 25-65 and 60-99 | Biretta 2217 (Acolyte line only), Saint's Robe 2326 (Acolyte / Merchant / Monk / Priest), Boots 2406, Manteau 2506 listed for the mage line (copied from Priest) | Circlet 2232, Silk Robe 2322, Mage Coat 2334 (lv50), Hood / Muffler | item_db Jobs |
| F5 | M | 26 class/item pairs behind F2-F4 (87 band-slot entries, 14 fully dead slots) | `buildgear.json` | unusable but listed: Monk (Padded Armor 2313, Chain Mail 2315, Helm 2229, Manteau 2506, Boots 2406); Assassin, Rogue, Blacksmith, Alchemist (Helm 2229, Swordman line only); Hunter, Bard, Dancer (Silk Robe 2322); Bard, Dancer (Hunter Bow 1726, Hunter only; Belt in 1-20) | replace or remove; the engine skips them (nextBuildPiece checks canEquip) so the effect is a hole in the table, not a crash | item_db Jobs; list in section 5 |
| F6 | M | wizard, sage, sage_support, sage_hindsight (60-99); knight_pierce (33-60, 48-99) | `buildgear.json` shield with weapon | a shield list sits in the same band as a two-handed weapon: Wizardry Staff 1473 (2hStaff); Guisarme 1451, Glaive 1454, Partizan 1457 (2hSpear). A shield cannot be worn with them | no shield in a band whose best weapon is two-handed, or a note that the bot picks one | item_db Locations Both_Hand vs Left_Hand |
| F7 | M | 28 of 37 plans | plan completeness | plans list fewer than 88 points: sage_hindsight 65, wizard_fire 68, rogue_bow 69, hunter 71, sage_support 75, blacksmith_mace 77, knight_pierce 79, crusader_spear 79, sage 80, bard 80, dancer 80, wizard_frost 80, hunter_agi 80, bard_arrow 80, dancer_arrow 80, monk 81, knight_spear 81, crusader_gc 82, priest_battle 82, and 9 more at 84-87. The class docs say "largest skill total 88" and "37 of 37 reach every goal": true, but 1 to 23 points per bot at 99 are unplanned and go to the id-order fallback | every plan sums to 88, or states what the spare points are for | gen_milestones plan_check; section 4 |
| F8 | M | bard, dancer, bard_arrow, dancer_arrow | gear vs skills | song and dance skills are in the plan (Whistle, Assassin Cross of Sunset, Poem of Bragi, Apple of Idun, Humming, Don't Forget Me, Fortune's Kiss, Service for You) but every band lists bows only; songs need an instrument or whip in hand (known gap, builds.md "Bard songs need an instrument in hand - not done") | an instrument / whip in the gear path, or no song points until the engine can play | skill_db, builds.md |
| F9 | M | 19 cards, many bands | card availability | cards listed in low bands whose lowest-level dropper in `mob_db.yml` is far above the band: Raydric 4133 (Raydric is Lv 115; all.md calls it a common card "in every band", band max 20-30), Zerom 4064 (Lv 70 vs band max 20), Matyr 4097 (58 vs 20), Mantis 4079 (65 vs 26), Desert Wolf 4082 (103 vs 26), Kobold 4091 (101 vs 30), Nine Tail 4159 (72 vs 30), Evil Druid 4141 (80 vs 26), Sohee 4100 (64 vs 26), Minorous 4126 (58 vs 26), Verit 4107 (52 vs 26), Eggyra 4070 (53 vs 26), Phen 4077 (52 vs 26), Orc Skeleton 4085 (53 vs 20), Archer Skeleton 4094 (50 vs 20), Whisper 4102 (46 vs 20), Skeleton Worker 4092 (44 vs 26), Goblin 4060 (44 vs 20), Kukre 4027 (42 vs 20). At 5x drops a card is 0.05% (Elusive / Swift variants) or 0.01% (normal) per kill: 400 to 2000 kills at the card's own level | cards a bot of that band can farm, or marked trade-only | mob_db Drops and Level; battle_conf item_rate_card 500 |
| F10 | M | all class docs | "Stats:" lines | old goals. 18 of the 20 plans with a parsable doc line disagree with `builds.go`: knight AGI 85 / DEX 40 / VIT 60 (plan AGI 75, DEX 30, VIT 61); crusader VIT 80 (89); hunter VIT 30 LUK 40 (VIT 40, LUK 30); wizard DEX 80 (70); sage VIT 50 (60); sage_support VIT 70 (60); priest DEX 90 VIT 90 (DEX 60, VIT 70); priest_me DEX 90 VIT 70 (DEX 70, VIT 60); monk STR 90 AGI 80 DEX 45 INT 15 (STR 85, AGI 85, DEX 35, no INT); monk_asura STR 90 (85); bard INT 40 (60); alchemist STR 75 INT 40 (85, 60); alchemist_battle DEX 55 VIT 55 (65, 70); alchemist_potion INT 90 DEX 80 LUK 60 (85, 75, 50); blacksmith_mace STR 90 VIT 70 (95, 80); assassin AGI 90 STR 85 (80, 80); assassin_sonic 90 / 90 (85 / 85); assassin_dagger AGI 90 DEX 50 (85, 55); rogue_bow AGI 70 VIT 35 (80, 45). Read by hand in all.md: hunter_trap VIT 35 (plan 50); rogue_strip DEX 90 AGI 50 VIT 40 (DEX 75, AGI 60, VIT 45); monk_occult VIT 80 (75, AGI 10); crusader_spear VIT 70 (80); sage_hindsight STR 30 (STR 1) | docs generated from the plan, or checked against it (only the "Build milestones" section is generated; the "Stats:" and "Skills:" bullets are hand text) | builds.go vs docs/classes |
| F11 | M | swordman.md and the other line files | "Skills (order)" lines | stale: Knight doc "Magnum Break 5, Endure 1, Increase HP Recovery 7" for the Swordman part (plan Magnum Break 10, Endure 10, HP Recovery 10); the same pattern in other lines (not enumerated) | generated from the plan | builds.go knight |
| F12 | M | docs/classes headers, all.md | end goals | 13 headers say "end goal Lord Knight / Paladin / High Wizard / Professor / Sniper / Clown / Gypsy / High Priest / Champion / Whitesmith / Creator / Assassin Cross / Stalker"; all.md line 21 says ST01 means "the end goal of each plan is its transcendent class". ST01 / G10 (2026-11-15): no rebirth, every plan ends at its 2nd job | "end goal Knight / Crusader / Wizard ..." | settings.md ST01, G10 |
| F13 | M | `tools/wrongbuilds.py` TREE | 1st-job skill tables | hard-coded copy of the first-job tree: missing 16 skills that exist in `skill_tree.yml` (Moving HP Recovery, Fatal Blow, Auto Berserk, Energy Coat, Making Arrow, Charge Arrow, Holy Light, Cart Revolution, Change Cart, Loud Exclamation, Buying Store, Cart Decorate, Sprinkle Sand, Back Sliding, Pick Stone, Throw Stone); Holy Water (id 31) has prerequisite Ruwach 1 in the tool and none on the server. Also hard-coded `assert ... == 13` main plans, and 2nd-job skills are never audited | tables read from the server file, as `tools/design/planlib.Tree` does | skill_tree.yml |
| F14 | M | builds.go shared routes `extrasMelee`, `extrasCast`, `extrasHoly`, `armorLight` ... | route lists | Clip 2607 is in every route (all 37 builds): not sold by any stock shop and unobtainable (all.md "Run 6 fix"). Routes also hold items the class cannot wear: 69 hits in 31 builds (Helm 17, Manteau 14, Boots 12, Silk Robe 8, Cap 6, Shield 5, Full Plate 5, Circlet 2) | routes per class family, entries filtered through `Jobs` | item_db Jobs, shop scripts |
| F15 | L | hunter_agi, wizard_fire, wizard_frost, bard_arrow, dancer_arrow, rogue_strip, hunter_trap, monk_occult | `buildgear.json` | no own table (fallback to the base plan's) while all.md CL04 says every variant has its own | own table, or CL04 reworded | buildgear.json keys |
| F16 | L | 17 builds | `LegacyStats` | legacy stat plans cost more than 1273 points (priest 1688, priest_me 1500, alchemist_potion 1420, knight 1408, assassin 1390, assassin_sonic 1359, monk 1351, crusader_gc 1340 ...): their last steps were never reachable | harmless for wrongbuilds (caps only); note it | planlib spent() |
| F17 | L | docs/classes/all.md | variant mix | "planForClass gives new characters the main plan 3 times in 5"; code `pickVariant` gives the main plan 2 in 5 (40%) | one number | builds.go:481-507 |
| F18 | L | builds.go weapons route order | "weakest first" | Monk (Club 23, Mace 37, then Waghnak 30), Assassin (Jamadhar 165 before Katar 148, ordered by price), rogue_bow (Knife 17 before Bow 15; Gakkung 100 after Arbalest 90) | order by ATK, or say "by price" | item_db Attack |
| F19 | L | `buildgear.json` card "why" text | descriptions | Thara Frog 4058 labelled "MaxHP+400 (Thief Bug Egg)" for knight_pierce (script: -30% Demi-Human); Drops 4004 "DEX+1 HIT+3: faster casts" for wizard, sage, sage_hindsight (script: DEX+1 HIT+3 only); Nine Tail 4159 "AGI+2 FLEE+20" for assassin, rogue (FLEE+20 only when refined above +8) | text equal to the card script | item_db Script |
| F20 | L | `buildgear.json` | item level inside the band | items need a level above the band's min (Katar lv33 in assassin 18-60, Wizardry Staff lv90 in 60-99, Mage Coat lv50 in 25-65, Tights lv45 in 33-99, Full Plate lv40 in 27-99): fine as upgrades, the notes read as if wearable from the band start | engine already checks level; document only | item_db EquipLevelMin |
| F21 | L | milestone pace | `job_points` assumption | "first job 40 at base 40, 2nd job level = base - 39" is an assumption; job and base exp are separate tables (5x each), so the skill rows can be off by several levels either side. Job-level stat bonuses (`job_stats.yml` BonusStats) are not counted in the stat rows | measure real job level vs base level from `char_levels.json` once | job_exp.yml |
| F22 | L | design choice | job change at job 40 | the 1st job caps at 50, so changing at 40 forgoes 10 skill points (88 instead of 98) | the owner's call; only noted | job_exp.yml, legion.txt:61 |

Stat sanity (informational, not errors): crusader_gc ends INT 90 VIT 85 with 0 points spare; all INT/DEX casters end STR 1, so
carry weight comes almost only from the job base (`job_stats.yml` MaxWeight 24000-28000) and potion stock is small.

## 3. Zeny feasibility of buying the listed gear

One priciest NPC-priced item per slot, summed per band (item_db `Buy`), against the league means (2026-11-14 21:17: zeny/h
8k to 80k per cohort, server mean 27k, before the 5x drop change):

| band | most expensive set | hours at 30k zeny/h |
|---|---|---|
| low (1-20 / 1-32) | 61k (priest) to 208k (assassin, rogue) | 2 to 7 |
| mid | 247k to 269k (assassin_dagger 24-60, crusader 14-50) | 8 to 9 |
| top | 278k to 315k (knight 48-99, crusader 27-99) | 9 to 10 |

None is impossible. The early sets of Assassin, Rogue, Blacksmith, Alchemist and Monk (190k to 208k for a 1-32 band) are the
tightest against the 1-20 cohorts (24k to 27k zeny/h in the league report). 32 of the 104 distinct band items are not sold by any
stock shop (slotted `[1]` Sandals 2402, Hood 2502, Ring 2601, Crossbow 1711, Arbalest 1715, Gakkung 1716, Hunter Bow 1726,
Gladius 1220, Damascus 1226, Smasher 1508, Buckler[1] 2104 ...): they are drop or trade pieces, so the cost table is a lower
bound for them. (The shop list was read from the stock `npc/re`, `merchants`, `custom` scripts only.)

## 4. Per-build table (37 builds)

Stat cost = points needed for all the plan's stat goals (budget 1273 at 99). "unspent at 99 (replay)" is
`skill_points_unspent` in the last band of `buildmilestones.json`. "unusable gear entries" counts band slot entries of the
build's table the 2nd class or the band's levels cannot use; "(dead slots)" are slots where no listed item is usable. "doc stat
claims that differ" counts stats in the class doc's Stats line that differ from the plan's maximum goal (n/a = no parsable
line).

| build | 2nd class | stat goals cost (spare of 1273) | skill pts listed 1st-job / 2nd-job / total (of 88) | unspent at 99 (replay) | ASPD at 99, no buffs | unusable gear entries (dead slots) | doc stat claims that differ | gear table |
|---|---|---|---|---|---|---|---|---|
| knight | Knight | 1272 (1) | 56 / 31 / 87 | 1 | 165 | 0 (0) | 3 | own |
| crusader | Crusader | 1269 (4) | 38 **<39** / 47 / 85 | 3 | 160 | 0 (0) | 1 | own |
| wizard | Wizard | 1233 (40) | 49 / 39 / 88 | 0 | 151 | 4 (0) | 1 | own |
| sage | Sage | 1233 (40) | 39 / 41 / 80 | 8 | 149 | 4 (0) | 1 | own |
| hunter | Hunter | 1235 (38) | 45 / 26 / 71 | 17 | 171 | 5 (2) | 2 | own |
| bard | Bard | 1254 (19) | 35 **<39** / 45 / 80 | 8 | 167 | 6 (2) | 1 | own |
| dancer | Dancer | 1254 (19) | 35 **<39** / 45 / 80 | 8 | 167 | 6 (2) | n/a | own |
| priest | Priest | 1233 (40) | 39 / 49 / 88 | 0 | 144 | 3 (2) | 2 | own |
| knight_spear | Knight | 1265 (8) | 35 **<39** / 46 / 81 | 7 | 148 | 0 (0) | n/a | own |
| crusader_gc | Crusader | 1273 (0) | 35 **<39** / 47 / 82 | 6 | 160 | 0 (0) | n/a | own |
| hunter_blitz | Hunter | 1229 (44) | 50 / 35 / 85 | 3 | 159 | 5 (2) | n/a | own |
| priest_me | Priest | 1233 (40) | 39 / 49 / 88 | 0 | 144 | 3 (2) | n/a | own |
| monk | Monk | 1264 (9) | 33 **<39** / 48 / 81 | 7 | 175 | 8 (0) | 4 | own |
| blacksmith | Blacksmith | 1262 (11) | 42 / 42 / 84 | 4 | 163 | 3 (0) | 0 | own |
| alchemist | Alchemist | 1269 (4) | 35 **<39** / 49 / 84 | 4 | 151 | 2 (0) | 2 | own |
| assassin | Assassin | 1242 (31) | 39 / 45 / 84 | 4 | 173 | 3 (0) | 2 | own |
| rogue | Rogue | 1240 (33) | 39 / 49 / 88 | 0 | 173 | 2 (0) | 0 | own |
| sage_support | Sage | 1233 (40) | 29 **<39** / 46 / 75 | 13 | 149 | 4 (0) | 1 | own |
| monk_asura | Monk | 1244 (29) | 39 / 49 / 88 | 0 | 162 | 8 (0) | 1 | own |
| alchemist_battle | Alchemist | 1260 (13) | 35 **<39** / 49 / 84 | 4 | 158 | 2 (0) | 2 | own |
| alchemist_potion | Alchemist | 1256 (17) | 35 **<39** / 49 / 84 | 4 | 152 | 2 (0) | 3 | own |
| blacksmith_mace | Blacksmith | 1241 (32) | 35 **<39** / 42 / 77 | 11 | 157 | 3 (0) | 2 | own |
| assassin_sonic | Assassin | 1259 (14) | 39 / 45 / 84 | 4 | 174 | 3 (0) | 2 | own |
| assassin_dagger | Assassin | 1272 (1) | 39 / 49 / 88 | 0 | 175 | 3 (0) | 2 | own |
| rogue_bow | Rogue | 1270 (3) | 29 **<39** / 40 / 69 | 19 | 171 | 3 (0) | 2 | own |
| knight_pierce | Knight | 1270 (3) | 35 **<39** / 44 / 79 | 9 | 159 | 0 (0) | n/a | own |
| crusader_spear | Crusader | 1241 (32) | 33 **<39** / 46 / 79 | 9 | 157 | 0 (0) | n/a | own |
| wizard_fire | Wizard | 1233 (40) | 33 **<39** / 35 / 68 | 20 | 151 | 4 (0) | n/a | base:wizard |
| wizard_frost | Wizard | 1233 (40) | 32 **<39** / 48 / 80 | 8 | 151 | 4 (0) | n/a | base:wizard |
| sage_hindsight | Sage | 1270 (3) | 39 / 26 / 65 | 23 | 170 | 4 (0) | n/a | own |
| hunter_agi | Hunter | 1251 (22) | 50 / 30 / 80 | 8 | 173 | 5 (2) | n/a | base:hunter |
| priest_battle | Priest | 1270 (3) | 54 / 28 / 82 | 6 | 159 | 3 (2) | n/a | own |
| bard_arrow | Bard | 1226 (47) | 50 / 30 / 80 | 8 | 171 | 6 (2) | n/a | base:bard |
| dancer_arrow | Dancer | 1226 (47) | 50 / 30 / 80 | 8 | 171 | 6 (2) | n/a | base:dancer |
| rogue_strip | Rogue | 1244 (29) | 39 / 49 / 88 | 0 | 169 | 2 (0) | n/a | base:rogue |
| hunter_trap | Hunter | 1230 (43) | 39 / 49 / 88 | 0 | 162 | 5 (2) | n/a | base:hunter |
| monk_occult | Monk | 1238 (35) | 39 / 49 / 88 | 0 | 164 | 8 (0) | n/a | base:monk |

## 5. Unusable (class, item) pairs in `buildgear.json`

| class | item | slot | builds |
|---|---|---|---|
| Alchemist | Helm 2229 | headgear | alchemist, alchemist_battle, alchemist_potion |
| Assassin | Helm 2229 | headgear | assassin, assassin_dagger, assassin_sonic |
| Bard / Dancer | Belt 2627 (lv25) | accessory (band 1-20) | bard, dancer |
| Bard / Dancer | Hunter Bow 1726 | weapon | bard, dancer |
| Bard / Dancer | Manteau 2506 | garment | bard, dancer |
| Bard / Dancer | Silk Robe 2322 | armor | bard, dancer |
| Blacksmith | Helm 2229 | headgear | blacksmith, blacksmith_mace |
| Hunter | Belt 2627 (lv25) | accessory (band 1-20) | hunter, hunter_blitz |
| Hunter | Manteau 2506 | garment | hunter, hunter_blitz |
| Hunter | Silk Robe 2322 | armor | hunter, hunter_blitz |
| Monk | Boots 2406, Chain Mail 2315, Helm 2229, Manteau 2506, Padded Armor 2313 | shoes, armor, headgear, garment | monk, monk_asura |
| Priest | Boots 2406, Manteau 2506 | shoes, garment | priest, priest_battle, priest_me |
| Rogue | Helm 2229 | headgear | rogue, rogue_bow |
| Sage | Biretta 2217, Boots 2406, Manteau 2506, Saint's Robe 2326 | headgear, shoes, garment, armor | sage, sage_hindsight, sage_support |
| Wizard | Biretta 2217, Boots 2406, Manteau 2506, Saint's Robe 2326 | headgear, shoes, garment, armor | wizard |

## 6. Proposed automated checks (what a test should assert)

Offline tests, reading repo files plus a copy of the server tables (`skill_tree.yml`, `skill_db.yml`, `job_exp.yml`,
`statpoint.yml`, `item_db_*.yml`, `mob_db.yml`, `job_aspd.yml`) kept in a fixtures folder so no container is needed (G23).
Put the table loader in `tools/design/planlib.py` and reuse it in `wrongbuilds.py`.

Stats
1. For every build, `Stats` and `LegacyStats`: every value 1..99, no goal that is not above that stat's earlier goal, stat cost
   of the final per-stat maxima <= `statpoint.yml` Points at 99 (1273). (LegacyStats may be over budget: assert only <= 99
   and report the excess.)
2. For every level 1..99, `planlib.stat_points`, `persona.go statPoints` (parse or a Go test) and `statpoint.yml` agree.
3. Every milestone range: `stat_points_used <= stat_points_available`, stats never below the previous range, and a fresh
   replay equals the JSON (the replay is already tested; keep it).

Skills
4. Every skill id exists in `skill_db.yml`; its name is in the build's Novice, first-job or second-job tree; goal level <= the
   tree's MaxLevel; each goal's prerequisite chain is met by goals in the same plan.
5. Budget: points in the 2nd-job tree <= 49, total <= 88, and (new) points in Novice + 1st-job skills >= 39 so the
   `player_skillup_limit` lock cannot hold the 2nd-job skills (F1). Also assert `sum(plan) >= 80` or list the spare points
   explicitly (F7).
6. Replay fidelity: `sim_skills` must refuse a 2nd-job skill while spent points < 9 + 39; add that rule and assert the milestone
   JSON equals the replay.
7. `wrongbuilds.py TREE` equals the server's 1st-job tree for ids and prerequisites (or is generated from it).

Gear (`builds.go` routes and `buildgear.json`)
8. Every id exists and is the right type: Weapon / Armor; the location matches the slot it is listed in (weapon Right_Hand or
   Both_Hand, armor Armor, shield Left_Hand, headgear Head_*, garment Garment, shoes Shoes, accessory *_Accessory); ammo is
   type Ammo.
9. For each band and slot, at least one listed item has `Jobs` containing the build's 2nd class (map BardDancer, no
   `!` entry) and `EquipLevelMin <= band.max`; no item in a slot may be unusable by the class unless another listed item
   covers the slot (F2-F5). Same check for the `builds.go` route slices (F14).
10. No band lists a shield together with a weapon whose Locations is Both_Hand unless the band marks the choice (F6); no
    plan with a two-handed route also has a shield in `Extras`.
11. A weapon's SubType must be one the plan's skills allow (Two-Hand Quicken: 2hSword; Sonic Blow: Katar; Bow lines: Bow;
    spear plans: spears; Hindsight: Book); songs and dances in a plan need an instrument / whip in the gear path (F8).
12. Weapon route order is ascending ATK (or documented as price order); the `Slots` quoted in band notes ("Rod[4]") equal
    the item's `Slots` for the id used (those already agree).
13. Clip 2607 and every item with no `Buy` and no shop and no dropper must not be in a route (F14).

Cards
14. Card id exists, type Card, card Locations intersect the slot the card is listed under; the band has at least one listed
    item in that slot with `Slots > 0`.
15. Each card has a dropper in `mob_db.yml`; its lowest-level dropper `Level <= band.max + 20`, else the entry must carry a
    `trade_only` flag (F9). Expected kills = 10000 / (rate * 5) must be below a threshold (e.g. 2500).
16. `why` text matches the card script: compare the bonuses named in the text with `Script` (stat and number), and flag a card
    whose script has an `if (getrefine() ...)` condition (F19).

Docs and cross-file
17. Each `docs/classes/*.md` "Stats:" line and "Skills:" line for a plan equals the plan in `builds.go` (generate the lines;
    until then fail on a mismatch). Each header's "end goal" must be the plan's own 2nd class (F12).
18. Every build id in `builds.go` appears in `buildmilestones.json`; every `buildgear.json` key is a build id; builds with no
    own gear table are listed in a single allow-list (F15).
19. ASPD sanity: for every build at level 99 with its final stats and best route weapon, ASPD (Renewal rule, `job_aspd.yml`)
    stays between 140 and 190 (`max_aspd` 190); HP / SP formulas from `job_stats.yml` stay under `max_hp_lv99` 330000.
20. Zeny feasibility: per band, sum of the highest `Buy` per slot divided by the cohort's p25 zeny/h from the latest league
    report must stay under a limit (e.g. 12 hours); warn above.
21. `pickVariant` mix (Go test): over 10000 draws the main plan share is 40%; the docs' number is read from one place.

## 7. Not checked / limits

- Refine levels, enchants and prices of refine materials do not appear in the build data; nothing to verify.
- MVP data: the milestone JSON has MVP playstyle text only (no boss names or levels); the only mob level claim found is "hunts
  monsters 5-10 levels under its own" (knight playstyle), which differs from all.md's "normal monsters up to 10 levels above the
  bot" for drop farming; not a number error, a wording conflict.
- HP / SP were not computed per level; ASPD uses the plain Renewal amotion rule (4 x AGI + DEX), without skill buffs and
  without the shield penalty.
- NPC availability was taken from the stock shop scripts (`npc/re`, `merchants`, `custom`, `custom-local`); Legion custom shops
  (`rathena/npc-custom/legion_shops.txt`) were not merged.
