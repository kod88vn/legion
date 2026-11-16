# Wizard build research, 2026-11-15

Research only. `builds.go` was not edited. Skill ids: Fire Bolt 19, Fire Ball 17, Fire Wall 18, Cold Bolt 14,
Frost Diver 15, Stone Curse 16, Lightning Bolt 20, Thunderstorm 21, Sight 10, Napalm Beat 11, Sightrasher 81,
Meteor Storm 83, Jupitel Thunder 84, Lord of Vermilion 85, Ice Wall 87, Frost Nova 88, Storm Gust 89,
Earth Spike 90, Heaven's Drive 91, Quagmire 92.

## 1. What the server is

- Renewal mode: `rathena/Dockerfile` builds with `--enable-prere=no`, rAthena commit 1ca3e77, PACKETVER 20211103. So use
  Renewal data, not pre-renewal guides (`docs/rules/settings.md` has the rates: 5x base/job EXP, 5x drops incl. cards;
  `battle_conf.txt` has `mob_count_rate: 400`, i.e. 4x monsters per spawn, which favours AoE).
- Cap at 2nd class, no rebirth (ST01/G10). Skill budget 88 = 39 (Mage, job 40) + 49 (Wizard, job 50); Novice's 9 are
  Basic Skill (`docs/classes/all.md` line 201).
- Stat budget check (my own arithmetic, cost to raise a stat from v = floor((v-1)/10)+2): INT 99 = 628, DEX 70 = 342,
  VIT 60 = 263; total 1,233 of 1,273, so 40 are spare. This matches the docs. Both budgets are right.

## 2. Verified skill facts (Renewal values)

Prerequisites (rAthena `db/re/skill_tree.yml`,
https://raw.githubusercontent.com/rathena/rathena/master/db/re/skill_tree.yml):
- Storm Gust: Frost Diver 1 + Jupitel Thunder 3. Frost Diver needs Cold Bolt 5, so Storm Gust's Mage cost is 6 points.
- Lord of Vermilion: Thunderstorm 1 + Jupitel 5. Thunderstorm needs Lightning Bolt 4.
- Meteor Storm: Sightrasher 2 + Thunderstorm 1. Sightrasher needs Lightning Bolt 1 + Sight 1.
- Jupitel: Napalm Beat 1 + Lightning Bolt 1. Earth Spike: Stone Curse 1. Heaven's Drive: Earth Spike 3.
  Quagmire: Heaven's Drive 1. Frost Nova: Ice Wall 1 (Ice Wall: Stone Curse 1 + Frost Diver 1).
- Safety Wall: Napalm Beat 7 + Soul Strike 5 (Soul Strike needs Napalm 4): about 13 points for a defensive wall. Not worth it.
- Energy Coat is a quest skill (30 SP, 5 s fixed cast, 1 level); https://irowiki.org/wiki/Energy_Coat. Not worth a point.

Numbers (iRO Wiki, post 2020 Renewal rework; Divine Pride agrees for Meteor):
- Storm Gust (https://irowiki.org/wiki/Storm_Gust): 78 SP at every level, so level 10 costs the same as level 1; 570% MATK
  per tick at lv10 (120% + 50% per level), ticks every 0.5 s for 4.5 s, 11x11, Water, cooldown 6 s. Freeze chance falls
  with level (60% lv1, 15% lv10). Frozen targets take extra Wind damage and are not hit again by the gust. Knocks back.
- Lord of Vermilion (https://irowiki.org/wiki/Lord_of_Vermilion): SP 56+4*lv (96 at 10), damage 400%+100%*lv (1,400% at 10),
  20 hits, 11x11, 4 s, Wind, cooldown 5 s, blind 40%. It does not knock back, so the crowd stays inside. Wind vs frozen
  (Water) targets is the Storm Gust synergy.
- Meteor Storm (https://www.divine-pride.net/database/skill/83, https://irowiki.org/wiki/Meteor_Storm): cast 1.5 s fixed +
  6.3 s variable at every level; 125% per hit, 7x7 per meteor; cooldown 2+0.5*lv s.

  | Lv | SP | Meteors x hits | Total hits | Cooldown |
  |---|---|---|---|---|
  | 4 | 34 | 4 x 2 | 8 | 4 s |
  | 6 | 44 | 5 x 3 | 15 | 5 s |
  | 8 | 54 | 6 x 4 | 24 | 6 s |
  | 10 | 64 | 7 x 5 | 35 | 7 s |

- Jupitel Thunder (https://irowiki.org/wiki/Jupitel_Thunder): single target, 100% per hit, 3 to 12 hits, SP 17+3*lv.
  Good boss damage with knockback; level 5 is the Lord of Vermilion prerequisite.
- Earth Spike 200% per spike, Heaven's Drive 125% to 625% (5x5, https://ratemyserver.net/index.php?page=skill_db&jid=9),
  Quagmire 5x5 AGI/DEX slow, 5 SP per level (https://irowiki.org/wiki/Quagmire).
- Cast time (https://irowiki.org/wiki/Cast_Time): variable cast is multiplied by 1 - sqrt((2*DEX + INT)/530); fixed cast
  is not reduced by stats. My arithmetic: with INT 99 / DEX 70 that is a 67% cut. Storm Gust or Meteor then casts in
  about 1.5 + 2.1 = 3.6 s, Lord of Vermilion lv10 in about 3.0 s. DEX 80 instead of 70 saves only about 0.17 s on
  Storm Gust, so our DEX 70 (down from 80 in Legacy) is acceptable; gear INT/DEX cut it further.

## 3. Meteor Storm vs Storm Gust vs Lord of Vermilion

- Storm Gust is the best farming AoE: biggest area, cheapest per damage, fixed 78 SP, level 10 is free value. Its knockback
  and freeze scatter or stop the crowd, which is why Lord of Vermilion (no knockback, 11x11) is the second spell and the
  two combine through the freeze-Wind synergy. This matches `aoe.go` order (Storm Gust, Lord of Vermilion, Meteor,
  Thunderstorm, Heaven's Drive, Fire Ball).
- Meteor Storm lands meteors at random cells inside the area and only some overlap a given monster, so real damage is
  below the 125% x hits table; it does not move monsters and it can stun. It stacks with other casts at the same spot.
  Level 10 costs 64 SP for 35 hits (0.55 hits per SP); level 4 costs 34 SP for 8 hits (0.24 hits per SP). Level 10 is
  more than twice as SP-efficient and does 4.4 times the damage per cast for 1.9 times the SP. Cast time is the same at
  every level, so a low level wastes the same 3.6 s cast for much less damage.
- Community view (secondary, mixed versions): Storm Gust is the central AoE; Meteor Storm hits harder than Lord of
  Vermilion but needs crowds that stay put; guides disagree on Jupitel past the prerequisite (GameFAQs,
  https://gamefaqs.gamespot.com/pc/561051-ragnarok-online/faqs/50676 and ratemyserver
  https://write.ratemyserver.net/ragnoark-online-character-guides/playing-a-high-wizard-guide-and-build-and-leveling-guide/
  are pre-renewal, 2008). They agree a leveling Wizard is Storm Gust, Jupitel, Fire Wall, Frost Diver, Quagmire,
  Heaven's Drive.
- Verdict on Meteor Storm 4 in `wizard`: wrong. Level 4 does 8 hits for 34 SP, while Storm Gust costs 78 SP for
  about 9 ticks of 570% on every monster in 11x11. A 4-point-plus-2 Sightrasher investment (6 points) buys a spell that is
  weaker than anything else in the bar. Either take Meteor to 10 or drop Meteor and Sightrasher entirely and spend the 6
  points elsewhere. Recommendation: 10, paid for by trimming bolts (section 5). Meteor is the third spell in `aoe.go`, used
  while Storm Gust (6 s) and Lord of Vermilion (5 s) are on cooldown, so level 10 is what makes that gap worth filling.

## 4. Where our plan is wrong, acceptable or better

| Item | Our plan | Verdict | Source / reason |
|---|---|---|---|
| Stat total INT 99 / DEX 70 / VIT 60 | 1,233 of 1,273 | acceptable | cast formula above; DEX 70 vs 80 is about 0.17 s; INT 99 maximises MATK and SP. Spare 40 best into VIT (to 65) or kept (extra DEX is almost wasted) |
| Storm Gust 10, Lord of Vermilion 10 | main, frost | right | fixed SP at every level; iRO Wiki pages above |
| Meteor Storm 4 | `wizard` | wrong | table above; take 10 or none |
| Bolts at 10/10/10 in `Skills` of `wizard` | 30 points | wrong reason | `tools/wrongbuilds.py` lines 172-179 take each first-job skill cap as the max of `Skills` and `Legacy`, so lowering bolts in `Skills` does not make Legacy characters wrong builds; the comment in `builds.go` (line 115-117) is mistaken. Bolts above about 5 cost the later AoE |
| Fire Ball / Fire Wall / Thunderstorm 5 in Mage | main | acceptable | cheap early levelling AoE, needed for Fire Wall kiting |
| Jupitel 5 | main | acceptable | exactly the Lord of Vermilion prerequisite; 10 not needed |
| Earth Spike 3, Heaven's Drive 5 | main | acceptable | Heaven's Drive 5 is in `aoe.go`; Earth Spike 3 is its prerequisite |
| Quagmire | only in Legacy | acceptable to omit | no AI casts it (no Quagmire id in `aoe.go` or `skills.go`) |
| `wizard_frost` Ice Wall 10 (id 87) | frost | likely a bug | 87 is Ice Wall (a wall, no AI); Frost Nova is 88. Ice Wall 10 is 10 dead points |
| `wizard_frost` Frost Diver 10, Cold Bolt 10, Jupitel 10 | frost | wasteful | Frost Diver 1 is enough (Storm Gust prerequisite); Jupitel 10 not needed |
| `wizard_fire` has no Storm Gust and leaves 20 points unspent | fire | wrong (waste) | Storm Gust is the strongest AoE; 68 of 88 points used |
| `wizard_fire` Fire Ball 10 | fire | acceptable | Fire Ball is a last-choice spell in `aoe.go` but good vs few monsters |
| Meteor cooldown in `aoe.go` (`cdMs: 2000`) | code | wrong at level above 0 | Renewal cooldown is 2+0.5*lv s (7 s at lv10); the comment says the field exists so the server never refuses it. Needs the Haiku fixer to confirm in the live skill_db |
| Safety Wall, Energy Coat | not planned | right | 13 points / quest skill, no AI |

## 5. Recommended plans (skill goals in order; 88 points; pairs are (skill, level))

The first 39 points must all be Mage skills (job change at job 40; the job NPC wants no unspent points), so the Mage
block is exactly 39 and the Wizard block 49. The rule `Legacy` keeps old characters valid, so these lists may cut bolts.
Stats stay as now: INT 20/40/60/80/99, DEX 15/30/50/65/70, VIT 10/20/35/50/60 (1,233 of 1,273); spend the spare 40 on
VIT (60 to 65 costs 39) or leave it, because extra DEX only saves about 0.07 s per cast.

### wizard (main): Meteor Storm 10
- Mage 39: Fire Bolt 10, Fire Ball 5, Lightning Bolt 4, Thunderstorm 5, Sight 1, Fire Wall 5, Napalm Beat 1, Cold Bolt 6,
  Frost Diver 1, Stone Curse 1 (sum 39; Cold Bolt 5 is Frost Diver's prerequisite).
- Wizard 49: Jupitel 3, Storm Gust 10, Jupitel 5, Lord of Vermilion 10, Earth Spike 3, Heaven's Drive 5, Sightrasher 2,
  Meteor Storm 10, Quagmire 3, Jupitel 6 (Jupitel ends at 6; sum 6+10+10+3+5+2+10+3 = 49).
- Ids: `sk(19,10, 17,5, 20,4, 21,5, 10,1, 18,5, 11,1, 14,6, 15,1, 16,1, 84,3, 89,10, 84,5, 85,10, 90,3, 91,5, 81,2, 83,10, 92,3, 84,6)`
- Change from now: Lightning Bolt 10 to 4 and Cold Bolt 10 to 6 free 10 points; Meteor 4 to 10 costs 6; Quagmire 3 and
  Jupitel +1 take the last 4. If the owner wants no Quagmire (no AI casts it), put those 3 into Jupitel (to 9).

### wizard_fire
- Mage 39: Fire Bolt 10, Fire Ball 10, Fire Wall 5, Lightning Bolt 4, Thunderstorm 1, Sight 1, Napalm Beat 1, Stone Curse 1,
  Cold Bolt 5, Frost Diver 1 (sum 39).
- Wizard 49: Sightrasher 2, Meteor Storm 10, Jupitel 3, Storm Gust 10, Jupitel 5, Earth Spike 3, Heaven's Drive 5,
  Lord of Vermilion 10, Quagmire 3, Jupitel 6 (running sum 12, 15, 25, 27, 30, 35, 45, 48, 49).
- Ids: `sk(19,10, 17,10, 18,5, 20,4, 21,1, 10,1, 11,1, 16,1, 14,5, 15,1, 81,2, 83,10, 84,3, 89,10, 84,5, 90,3, 91,5, 85,10, 92,3, 84,6)`
- Why: keeps the fire theme (Meteor first) and finally adds Storm Gust and Lord of Vermilion, which the current plan lacks
  while 20 of 88 points stay unspent. If farming speed matters more than theme, put Storm Gust before Meteor.

### wizard_frost
- Mage 39: Cold Bolt 10, Frost Diver 5, Lightning Bolt 10, Thunderstorm 5, Napalm Beat 1, Stone Curse 1, Fire Bolt 7
  (sum 39; no Sight, because this plan has no Sightrasher or Fire Wall).
- Wizard 41: Jupitel 3, Storm Gust 10, Jupitel 5, Lord of Vermilion 10, Earth Spike 3, Heaven's Drive 5, Quagmire 3,
  Jupitel 10 (sum 10+10+10+3+5+3 = 41, Jupitel ends at 10 for boss damage).
- Post-job Mage top-up 8: Thunderstorm 10 (+5), Fire Bolt 10 (+3). Total 39+41+8 = 88.
- Ids: `sk(14,10, 15,5, 20,10, 21,5, 11,1, 16,1, 19,7, 84,3, 89,10, 84,5, 85,10, 90,3, 91,5, 92,3, 84,10, 21,10, 19,10)`
- Remove Ice Wall 10 (id 87). Optional later: Ice Wall 1 + Frost Nova (88) as a close-range freeze, only after `aoe.go`
  or the fight code can cast it; today nothing casts it, so no points should go there.

## 6. Gear and cards (only items I could verify)

- Weapon: Survivor's Rod (Dex) and Wizardry Staff (+200 MATK, +6 INT, +2 DEX, two-handed, https://irowiki.org/wiki/Wizard);
  Eden Staff II (+150 MATK, free from the Eden Group at level 40, same page). Our band plan already uses these.
- Accessory: Spiritual Ring (+2 INT, +2 DEX set), Clip and Belt have no level requirement (same page).
- Hodremlin Card: a shield card, 15% damage reduction from small, medium and large (db.irowiki.org item 4413). It is a
  defensive card for wearing the Guard shield; useful, not a damage card.
- Marc (armor, freeze immunity) appears in the pre-renewal guide above; I could not verify it on a Renewal page. "Hollow" is
  not a card I could find on any source; not verified, so not recommended.
- Card effects on skills: Salamander Card +40% Meteor Storm damage, Red Ferus +5%, Hill Wind Card +5% Lord of Vermilion
  and Jupitel, Laurell Weinder +5% Storm Gust (iRO Wiki skill pages above). These are rare drops; with 5x card drops
  they matter, but they are upgrades, not plan items.
- Existing doc cards (Elder Willow, Raydric, Sohee, Drops) were not re-checked.

## 7. AoE farming spots by level (low confidence)

I found no Renewal-specific Wizard leveling guide. The only structured list is the 2008 pre-renewal GameFAQs/RMS guide
(https://write.ratemyserver.net/ragnoark-online-character-guides/playing-a-high-wizard-guide-and-build-and-leveling-guide/):
Payon wolves and spores to about 40; lou_fild01 Soils (job 50 / base 64) with Fire Wall and Fire Ball; Siromas, Gazeti and
Roweans near the Ice Cave to about 90 with Jupitel, Frost Diver and Lord of Vermilion on Gazeti; pay_fild11 Nine Tails at 90+
with Storm Gust 5, Heaven's Drive 1, Quagmire 2; party spots Abbey 02 (Storm Gust, Meteor, Jupitel). A search summary of
the Re:Start PvM guide (page itself not reachable) lists Magma Dungeon and Anolian for a Fire Wall Wizard. Also check the
Renewal level-gap EXP penalty before sending bots to low-level maps (`docs/reports/ayla-party-20261115.md` lines 80 and 168).
The Legion's own map choices should come from our kills-per-minute data, not from these old lists.

## 8. Top recommendations

1. Do not keep Meteor Storm at 4: it is 8 hits for 34 SP and loses to Storm Gust on every measure. Use 10 and pay with
   fewer bolts (Lightning Bolt 4, Cold Bolt 6), which `wrongbuilds.py` allows because it uses the max of `Skills` and `Legacy`.
2. Fix `wizard_frost`: Ice Wall 10 (id 87) is dead points (Frost Nova is 88 and has no AI either); Frost Diver 10 and Cold Bolt
   10 are surplus; use the list above.
3. Fix `wizard_fire`: add Storm Gust 10 and Lord of Vermilion 10 and spend all 88 points.
4. Check `aoe.go` Meteor `cdMs: 2000` against the live skill_db: Renewal cooldown is 2+0.5*level seconds (7 s at level 10).
5. Stats are fine: INT 99 / DEX 70 / VIT 60 is 1,233 of 1,273 points; DEX 80 buys only about 0.17 s on Storm Gust.
6. Skip Safety Wall (13 points) and Energy Coat (quest skill); neither has AI.

Not verified: Storm Gust and Lord of Vermilion exact hit counts per cast on the server's rAthena build (the iRO table
gives 20 hits for Lord of Vermilion and about 9 ticks for Storm Gust; it is not clear whether the Lord of Vermilion
percentage is per hit); the rAthena `skill_db.yml` for our commit was not read; Marc and Hollow cards.
