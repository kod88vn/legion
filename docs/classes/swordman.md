# Swordman line: Knight, Crusader

General class rules: `all.md`. Owner words: `docs/rules/archive.md`.

## Swordman -> Knight / Crusader (tank line)

- CL05. Knights and Crusaders with Peco Peco Riding rent the Peco as their first purchase, before any other gear. Guild and party
  funds help a member who cannot afford it. It stays mounted (re-rents after death or dismount); an unmounted rider is a bug.
  Learn Peco Peco Riding on the official skill path (Endure, then Riding) as early as the plan allows and rent right away (no
  waiting for Cavalier Mastery). A partied rider leaves the party briefly for the rental errand (PT23). (2026-11-14)
- Tanks are active and fight a crowd with AoE: see `all.md` (CL02, CL03).

### Knight: "Two-Hand Sword Knight" (plan `knight`, end goal Knight)
- **Party role:** main tank (or melee DPS): pulls, Provokes, takes aggro off everyone, clears crowds with Bowling Bash.
- **Stats:** STR 20, AGI 20, VIT 20, DEX 15, STR 40, AGI 40, VIT 35, DEX 25, STR 60, AGI 60, VIT 50, STR 80, AGI 70, VIT 60, DEX 30, STR 90, AGI 75, VIT 61.
- **Skills (order):** Sword Mastery 1, Bash 10, Two-Handed Sword Mastery 10, Magnum Break 10, Provoke 5, Endure 10, Increase HP Recovery 10, Peco Peco Riding 1, Twohand Quicken 10, Cavalier Mastery 5, Counter Attack 5, Bowling Bash 10, Spear Mastery 1.
- **Use:** Two-Hand Quicken is a key buff (cast down to 30% SP; needs a two-handed SWORD, not a spear). Bowling Bash on
  the central monster of 3+ (5x5 splash); Brandish Spear when holding a spear; Magnum Break with 2+ around; Provoke to
  pull monsters off members; Endure vs knock-back.
- **Gear (buildgear.json):** lv1-32 Katana -> Slayer / Bastard Sword, Adventurer's Suit / Padded Armor, Hat/Helm,
  Hood/Muffler, Sandals/Shoes, Ring; lv33-60 Bastard Sword / Two-Handed Sword / Claymore, Padded Armor / Chain Mail,
  Manteau, Boots, refine the weapon; lv48-99 Two-Handed Sword / Claymore / Zweihander[2], Chain Mail / Full Plate
  (lv40+ for a tank). Cards (top): Mantis (accessory, STR+3), Raydric, Matyr; also Andre (ATK+20), Minorous (+15% vs
  Large), Desert Wolf (+15% vs Small), race cards, Picky (armor, STR+1 ATK+10).
- **Special items:** Peco Peco (rented at the stock breeder, knight 55,350, 2,500z, with Riding 1 and the price +
  200z; guild/party mates mail the shortfall; peco.go).
- **Boss role:** main tank or melee DPS: holds the boss (Provoke), Two-Hand Quicken + Bowling Bash on the adds,
  Endure vs knock-back.
- **Do:** engage first, pull the next group, stay in front, use AoE and potions when surrounded. **Don't:** trail
  behind the party, sit while members fight, escape except as a last resort near death (Butterfly Wing to the party's save Kafra when in a party (PT31); Fly Wing only solo).

### Crusader: "VIT Holy Cross Crusader" (plan `crusader`, end goal Crusader)
- **Party role:** main tank with a shield; holy damage vs undead/demons.
- **Stats:** VIT 20, STR 20, DEX 15, INT 10, VIT 40, STR 40, DEX 30, INT 25, VIT 60, STR 60, DEX 45, VIT 80, STR 75, DEX 55, INT 40, VIT 89.
- **Skills (order):** Bash 10, Provoke 5, Endure 10, Increase HP Recovery 10, Magnum Break 3, Peco Peco Riding 1, Cavalier Mastery 5, Faith 10, Holy Cross 10, Guard 5, Smite 3, Shield Boomerang 3, Shield Reflect 5, Grand Cross 4, Sacrifice 1, Provoke 6, Guard 6, Guard 7.
- **Use:** Auto Guard is a key buff (30% SP floor); Reflect Shield; Grand Cross on 3+ adjacent monsters while HP >= 70%;
  Holy Cross on single targets; Provoke to take aggro.
- **Gear:** lv1-26 Sword / Falchion / Blade + Guard / Buckler, Adventurer's Suit / Padded Armor; lv14-50 Rapier /
  Scimitar / Ring Pommel Saber + Buckler / Shield, Chain Mail; lv27-99 Tsurugi / Saber / Haedonggum, Chain Mail / Full
  Plate (lv40), Shield / Mirror Shield. Cards (top): Thief Bug Egg (shield, MaxHP+400), Peco Peco (armor, MaxHP+10%),
  Raydric, Matyr; Thara Frog (-30% Demi-Human) / Horn (-35% ranged) shields, Evil Druid (undead armor) vs undead maps.
- **Special items:** Grand Peco (crusader 232,318 at the breeder, 3,500z; Riding 1 and the price + 200z).
- **Boss role:** main tank: Auto Guard + Reflect Shield, Holy Cross / Grand Cross vs undead/demon bosses, Devotion on
  the priest.
- **Do / don't:** same as the Knight.

### Variant: "Brandish Spear Knight" (plan `knight_spear`, Base knight; build variants step 1, 2026-11-14)
- STR/VIT/DEX (STR 90, VIT 80, DEX 55, AGI 20). Swordman: Bash 5, Provoke 5, Endure 1, Magnum Break 10, HP Recovery 10,
  Bash 10, Provoke 10 | Knight: Spear Mastery 10, Riding 1, Pierce 5, Spear Stab 3, Brandish Spear 10, Cavalier Mastery
  5, Spear Boomerang 5, Spear Stab 10, Pierce 10, Endure 10. Weapon: spear only (Javelin -> Spear -> Pike -> Guisarme ->
  Glaive -> Partizan; planWeaponSubs). Brandish Spear on crowds (crowdSkills), on a Peco. New characters only.

### Variant: "INT/VIT Grand Cross Crusader" (plan `crusader_gc`, Base crusader)
- VIT/INT/DEX (INT 99, VIT 80, DEX 60). Swordman: HP Recovery 10, Provoke 5, Endure 1, Sword Mastery 10, Bash 10,
  Magnum Break 10 | Crusader: Faith 10, Holy Cross 6, Grand Cross 10, Auto Guard 5, Cure, Divine Protection 5, Demon
  Bane 5, Heal 5, Shield Charge 3, Shield Boomerang 3, Reflect Shield 5, Devotion 1, Holy Cross 10 (run 4: Riding 1
  right after Grand Cross 10, Cavalier Mastery 5 last - GC is magic, the ASPD penalty matters little). 1H sword + shield.
  Grand Cross on undead/demon crowds (skillbuff.go rule: 3+ adjacent, HP >= 70%).

### Engine notes (Swordman line)
- peco.go: riding = 0x0229 option bit 0x20, saved with the character. A party's town run goes to Prontera while a
  member wants a Peco (only the leader's for bot parties; members use an errand, errand.go "brb, renting a peco"). In
  Renewal, riding without Cavalier Mastery 5 costs ASPD (the plan learns Cavalier Mastery 5 right after Riding). DB
  09:55 2026-11-14: 0/93 Knights, 0/72 Crusaders lv40+ riding (rentals only started then). DB 11:00 (run 4): 105
  Knights / 81 Crusaders, all lv40+, median job 19; Riding 24 / 4, Cavalier 5: 5 / 0, mounted 0 / 0; zeny >= the
  rental + 1,000 (3,500 / 4,500): 12 / 4 (avg zeny 2,108 / 1,596). Riding now comes at job ~11 (Knight) / ~18
  (Crusader); poverty is the next blocker (flagged to the economy). wantsPeco needs Riding 1 + price + 1,000z; the
  party-member errand (errand.go) and a solo rider's next town trip (pecoTown) do the rental.
  Run 4: a rider with Riding, on foot and short of price + 1,000z saves for the bird - no other gear purchases or
  gear steps until then (peco.go pecoSaving -> builds.go gearHold / classWeaponNeed).
- 13:15 fix: riders never knew they were on foot (optionSeen only from a self 0x0229, which rAthena sends at login
  only when poisoned); now from the char list. Rental + 200z, whole purse held for the Peco, guild/party mates mail
  the shortfall (peco.go pecoFundRound), Riding 1 first after the Swordman part in every Knight/Crusader plan.
- Before the 30% key-buff floor, the 60% rule kept Knights from ever casting Two-Hand Quicken.

### Build milestones and playstyle (rebalanced 2026-11-15)
- Per-range stats and skills (replayed from the plans at each gear-band end) and the solo / MVP / PvP playstyle of every build of this line are in `legion/data/buildmilestones.json` (`knight`, `crusader`, `knight_spear`, `crusader_gc`, `knight_pierce`, `crusader_spear`). Gear is by band in `buildgear.json`; the party role stays in code.
- Plan budget at level 99 (1,273 stat points, 88 skill points), before and after the 2026-11-15 rebalance:
  - `knight` (Two-Hand Sword Knight): stat goals 1,408 before (budget 1,273), skill points 87 before (budget 88), not reached before: STR 90; AGI 85; now stat goals 1,272 (spare 1), skill points 88, ends STR 90, AGI 75, VIT 61, DEX 30, not reached now: none.
  - `crusader` (VIT Holy Cross Crusader): stat goals 1,180 before (budget 1,273), skill points 93 before (budget 88), not reached before: Grand Cross 0/4; Sacrifice 0/1; now stat goals 1,269 (spare 4), skill points 88, ends STR 75, VIT 89, INT 40, DEX 55, not reached now: none.
  - `knight_spear` (Brandish Spear Knight): stat goals 1,236 before (budget 1,273), skill points 101 before (budget 88), not reached before: Endure 1/10; Pierce 6/10; now stat goals 1,265 (spare 8), skill points 88, ends STR 90, AGI 20, VIT 83, DEX 55, not reached now: none.
  - `crusader_gc` (INT/VIT Grand Cross Crusader): stat goals 1,340 before (budget 1,273), skill points 115 before (budget 88), not reached before: INT 99; Holy Cross 6/10; Demon Bane 4/5; Heal 0/5; Smite 0/3; Shield Boomerang 0/3; Shield Reflect 0/5; Sacrifice 0/1; Cavalier Mastery 0/5; now stat goals 1,273 (spare 0), skill points 88, ends VIT 85, INT 90, DEX 60, not reached now: none.
  - `knight_pierce` (AGI/STR Pierce Lancer Knight): stat goals 1,270 before (budget 1,273), skill points 89 before (budget 88), not reached before: Endure 9/10; now stat goals 1,270 (spare 3), skill points 88, ends STR 90, AGI 70, VIT 60, DEX 40, not reached now: none.
  - `crusader_spear` (STR/VIT Spear Crusader): stat goals 1,152 before (budget 1,273), skill points 90 before (budget 88), not reached before: Grand Cross 3/5; now stat goals 1,241 (spare 32), skill points 88, ends STR 90, AGI 30, VIT 80, DEX 50, not reached now: none.
- Existing characters: a changed plan keeps the earlier goals in `Legacy` / `LegacyStats` (builds.go), so they are not wrong builds; `tests/tools/test_wrongbuilds_replay.py` replays the pre-rebalance plans through the wrongbuilds.py rules.
