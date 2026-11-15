# Thief line: Assassin, Rogue

General class rules: `all.md`. Owner words: `docs/rules/archive.md`.

## Thief -> Assassin / Rogue

### Rules
- No class-specific owner rules yet. Ayla herself is an Assassin; her gear plan is in `docs/handover/ayla-live-view.md`.

### Assassin: "Crit Katar Assassin" (plan `assassin`, end goal Assassin)
- **Party role:** melee DPS / evasion off-tank (dodges hits, so can tank for a party).
- **Stats:** AGI 20, STR 20, DEX 10, VIT 15, AGI 40, STR 35, LUK 15, AGI 60, STR 50, LUK 30, DEX 25, VIT 30, AGI 80, STR 70, LUK 40, DEX 35, STR 80, VIT 40, DEX 40.
- **Skills (order):** Double Attack 10, Improve Dodge 10, Steal 5, Hiding 2, Envenom 10, Detoxify 1, Hiding 3, Katar Mastery 4, Sonic Blow 10, Katar Mastery 10, Cloaking 2, Grimtooth 5, Enchant Poison 5, Poison React 5, Cloaking 5, Venom Dust 5, Cloaking 6, Cloaking 7, Cloaking 8, Cloaking 9.
- **Use:** Sonic Blow and Katar Mastery need a katar.
- **Gear:** a katar right after the Main Gauche: lv1-32 Main Gauche / Dirk / Stiletto; lv18-60 Jur[3] -> Katar[2];
  lv33-99 Katar / Jamadhar; Padded Armor / Thief Clothes / Chain Mail. Cards (top): Soldier Skeleton (weapon, CRIT+9),
  Raydric, Matyr. (Before the review all 31 online Assassins lv51-76 held a dagger, saving for a Dirk, and never cast
  Sonic Blow.)
- **Boss role:** melee DPS / evasion off-tank: Sonic Blow on the boss, dodge with high FLEE.
- **Katar first (run 6, coreweapon.go):** 13:40 sample - 19 of 19 online Assassins with Sonic Blow (most lv10) held a
  Main Gauche / Falchion / Knife, gear_next a Hat / Shoes / Muffler, purse 170-5,700z: Sonic Blow was never usable.
  Now, once Sonic Blow is learned on a katar plan (assassin / assassin_sonic), the katar is the savings goal: gearHold
  stops every other gear purchase and classWeaponNeed buys the cheapest katar it can wear (Jur 19,500z) when the
  purse covers it + 500z (solo trip, or the party member's "weapon" errand). Same rule for a Knight with Two-Hand
  Quicken (two-handed sword), spear Knights with Pierce / Brandish Spear and the spear Crusader with Spear Quicken.
  Open: no guild/party fund mailing for the katar yet (the Peco has one, peco.go pecoFundRound).

### Variant: "STR/AGI Sonic Blow Assassin" (plan `assassin_sonic`, Base assassin; run 4)
- **Stats:** AGI 20, STR 20, DEX 15, VIT 15, AGI 40, STR 40, DEX 25, AGI 60, STR 60, DEX 35, VIT 30, AGI 80, STR 80, DEX 45, VIT 40, AGI 85, STR 85.
- **Skills (order):** Double Attack 10, Improve Dodge 10, Steal 5, Hiding 2, Envenom 10, Detoxify 1, Hiding 3, Katar Mastery 4, Sonic Blow 10, Katar Mastery 10, Enchant Poison 5, Cloaking 2, Grimtooth 5, Righthand Mastery 5, Enchant Poison 10, Poison React 3, Cloaking 3, Cloaking 4, Cloaking 5, Cloaking 6.
- **Use:** Sonic Blow (progress.go offense, katar only), Enchant Poison self-buff (skillbuff.go, 15+15 x lv s).
  **Weapon:** katars only (planWeaponSubs Katar): Jur[3] -> Katar[2] / Jamadhar[1]; cards Andre, Hydra, Minorous,
  Skeleton Worker, Desert Wolf (race/size, not crit), Mantis, Zerom.

### Variant: "AGI/STR Dagger Assassin" (plan `assassin_dagger`, Base assassin; run 4)
- **Stats:** AGI 20, STR 20, DEX 15, VIT 15, AGI 40, STR 35, DEX 25, AGI 60, STR 55, DEX 35, LUK 10, VIT 30, AGI 80, STR 75, DEX 50, LUK 20, STR 80, AGI 85, DEX 55.
- **Skills (order):** Double Attack 10, Improve Dodge 10, Steal 5, Hiding 2, Envenom 10, Detoxify 1, Hiding 3, Righthand Mastery 5, Enchant Poison 10, Poison React 5, Venom Dust 5, Cloaking 3, Lefthand Mastery 5, Venom Splasher 10, Cloaking 5, Poison React 9.
- **Use:** Double Attack (procs only with a dagger), Envenom (offense), Enchant Poison buff. The engine equips one weapon
  (no dual wield yet; Lefthand Mastery waits for it). Venom Dust / Splasher (Red Gemstone) have no AI yet.
- **Weapon:** daggers only (planWeaponSubs Dagger): Main Gauche[4] / Dirk / Stiletto[3] -> Gladius[3] / Damascus[2].

### Rogue: "AGI/STR Dagger Rogue" (plan `rogue`, end goal Rogue)
- **Party role:** evasion "dodge tank" / melee DPS.
- **Stats:** AGI 20, STR 20, VIT 20, DEX 15, AGI 40, STR 35, DEX 25, AGI 60, STR 50, VIT 30, DEX 40, AGI 75, STR 70, VIT 40, DEX 50, AGI 85, STR 80.
- **Skills (order):** Double Attack 10, Improve Dodge 10, Steal 5, Hiding 1, Envenom 10, Detoxify 1, Hiding 3, Gank 4, Mug 4, Back Stab 4, Stalk 2, Sightless Mind 5, Back Stab 10, Snatch 5, Vulture's Eye 10, Double Strafe 5, Intimidate 4.
- **Use:** Back Stab works in Renewal (no direction check); Raid on crowds.
- **Gear:** lv1-30 Main Gauche / Dirk / Stiletto; lv24-99 Gladius[3] / Damascus[2] / Stiletto; Thief Clothes / Chain
  Mail. Cards (top): Soldier Skeleton, Raydric, Matyr.
- **Boss role:** melee DPS / evasion off-tank: Back Stab, strip skills later.

### Variant: "DEX/AGI Bow Rogue" (plan `rogue_bow`, Base rogue; run 4)
- **Stats:** DEX 20, VIT 15, AGI 15, DEX 40, AGI 30, DEX 60, AGI 45, LUK 10, VIT 25, DEX 80, AGI 60, VIT 35, DEX 99, AGI 70, LUK 20, VIT 45, AGI 80.
- **Skills (order):** Improve Dodge 10, Steal 5, Hiding 1, Envenom 10, Detoxify 1, Hiding 3, Vulture's Eye 10, Double Strafe 10, Gank 10, Mug 4, Stalk 5, Remove Trap 1, Steal 6, Steal 7, Steal 8, Steal 9, Steal 10, Sand Attack 1, Back Slide 1, Find Stone 1, Stone Fling 1, Hiding 4, Divest Helm 1, Divest Helm 2, Divest Helm 3, Divest Helm 4, Divest Helm 5, Mug 5, Mug 6, Mug 7, Mug 8.
- **Use:** gear style "bow" (builds.go gearStyle / buildgear buildStyle: bows score like an archer's, no shield);
  Double Strafe in the Thief offense list (progress.go; skipped without a bow), arrows like any bow user (ammo.go
  bowUser). A copied (Plagiarism) skill has no fight rule yet.
- **Weapon:** bows only (planWeaponSubs Bow): Bow[4] / Composite[4] -> Great Bow / Crossbow[3] -> Arbalest / Kakkung[2]
  / Bow of Rogue Master; arrows (Arrow, Silver, Fire, Crystal, Steel). Cards: Archer Skeleton (+10% ranged), Andre,
  race cards, Zerom (DEX+3), DEX+2 / AGI+2 accessories, Raydric, Whisper, Matyr.

### Build milestones and playstyle (rebalanced 2026-11-15)
- Per-range stats and skills (replayed from the plans at each gear-band end) and the solo / MVP / PvP playstyle of every build of this line are in `legion/data/buildmilestones.json` (`assassin`, `rogue`, `assassin_sonic`, `assassin_dagger`, `rogue_bow`, `rogue_strip`). Gear is by band in `buildgear.json`; the party role stays in code.
- Plan budget at level 99 (1,273 stat points, 88 skill points), before and after the 2026-11-15 rebalance:
  - `assassin` (Crit Katar Assassin): stat goals 1,390 before (budget 1,273), skill points 84 before (budget 88), not reached before: STR 85; VIT 40; now stat goals 1,267 (spare 6), skill points 88, ends STR 80, AGI 80, VIT 40, DEX 40, LUK 40, not reached now: none.
  - `rogue` (AGI/STR Dagger Rogue): stat goals 1,240 before (budget 1,273), skill points 88 before (budget 88), not reached before: none; now stat goals 1,240 (spare 33), skill points 88, ends STR 80, AGI 85, VIT 40, DEX 50, not reached now: none.
  - `assassin_sonic` (STR/AGI Sonic Blow Assassin): stat goals 1,359 before (budget 1,273), skill points 84 before (budget 88), not reached before: STR 90; VIT 40; now stat goals 1,259 (spare 14), skill points 88, ends STR 85, AGI 85, VIT 40, DEX 45, not reached now: none.
  - `assassin_dagger` (AGI/STR Dagger Assassin): stat goals 1,288 before (budget 1,273), skill points 88 before (budget 88), not reached before: STR 80; now stat goals 1,272 (spare 1), skill points 88, ends STR 80, AGI 85, VIT 30, DEX 55, LUK 20, not reached now: none.
  - `rogue_bow` (DEX/AGI Bow Rogue): stat goals 1,127 before (budget 1,273), skill points 88 before (budget 88), not reached before: none; now stat goals 1,270 (spare 3), skill points 88, ends AGI 80, VIT 45, DEX 99, LUK 20, not reached now: none.
  - `rogue_strip` (DEX/STR Divest Rogue): stat goals 1,290 before (budget 1,273), skill points 88 before (budget 88), not reached before: STR 80; now stat goals 1,244 (spare 29), skill points 88, ends STR 80, AGI 60, VIT 45, DEX 75, not reached now: none.
- Existing characters: a changed plan keeps the earlier goals in `Legacy` / `LegacyStats` (builds.go), so they are not wrong builds; `tests/tools/test_wrongbuilds_replay.py` replays the pre-rebalance plans through the wrongbuilds.py rules.
