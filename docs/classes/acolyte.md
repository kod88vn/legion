# Acolyte line: Priest, Monk

General class rules: `all.md`. Owner words: `docs/rules/archive.md`.

## Acolyte -> Priest / Monk (support line)

### Rules
- CL09 (also PT14). Priests heal first: SP is kept for healing (buffs only above 60% SP). When nobody needs a heal or buff the priest sits only for low SP (CL17, PT29) (heal, buff
  or move with the party, otherwise sit). Idle sitting does not stop the party; only an announced rest (SP really low,
  "need sp" / "ready") makes the others wait. (2026-11-14)
- CL17. A healer (Acolyte, Priest, Monk with Heal) heals itself instead of sitting to rest for HP, and a Priest keeps Kyrie Eleison on itself at all times; it sits only for low SP when nobody needs a heal, and stands up as soon as someone does. (2026-11-14)
- CL10. Acolyte and Priest builds max out INT first, then DEX/VIT as support; existing characters grow into it with new stat
  points. (2026-11-14)
- Healers carry few HP potions and mostly SP potions (EC29). Ayla's party keeps its priest when others rotate jobs (AY15).

### Priest: "Full Support Priest" (plan `priest`, end goal High Priest)
- **Party role:** healer and buffer; never the damage dealer.
- **Stats:** INT 99 first, then DEX 40, VIT 40 -> DEX 70, VIT 70 -> DEX 90, VIT 90 (old INT/VIT/DEX steps kept in
  `LegacyStats`; every new point goes to INT until 99).
- **Skills (order):** Heal 5 first, Divine Protection 5, Blessing 10, Increase AGI 10, Heal 10, Cure 1, Ruwach 1,
  Angelus 2 | Priest: Kyrie Eleison 10, Magnificat 3, Impositio Manus 3, Suffragium 3, Sanctuary 7, Gloria 1,
  Teleport 2, Warp Portal 4, Pneuma 1, Aqua Benedicta 1, Aspersio 4, Lex Divina 3, Magnificat 5, Status Recovery 1,
  Increase SP Recovery 4, Resurrection 1, Turn Undead 3, Lex Divina 5, Lex Aeterna 1, Safety Wall 1, Magnus
  Exorcismus 10, Gloria 5.
- **Use:** heal the most hurt party mate within 9 cells under 80% (self under 60%), standing up to cast; buffs
  (Blessing, Increase AGI, Kyrie on the tank) only above 60% SP; Heal as an attack on Undead-element monsters (solo
  keeps 20% SP, a party healer 50%); Magnus Exorcismus on 4+ undead/demons (Blue Gemstone, long cast). A solo support
  Acolyte casts Blessing and Increase AGI on itself.
- **Rest:** sits under `priest_sit_low_sp_pct` (40%), rests to 80% (60% when the party moves); sits only for low SP when nobody needs a heal or buff (CL17, PT29); otherwise
  heal, buff or move without holding the party.
- **Gear:** lv1-26 Rod / Mace, Silk Robe, Guard / Buckler, Hat / Biretta, Hood/Muffler, Sandals/Shoes, Rosary; lv24-60
  Wand / Arc Wand, Saint's Robe, Buckler, Biretta, Manteau, Rosary (undead dungeons with a party); lv55-99 Arc Wand /
  Healing Staff (lv55), Boots. Cards (top): Elder Willow (headgear, INT+2), Raydric, Sohee (shoes, MaxSP+15%); Pupa /
  Peco Peco armor, Thief Bug Egg / Horn shield, Evil Druid armor in undead dungeons, Phen (accessory, cast not
  interrupted).
- **Special items:** SP potions (mostly), a few HP potions; Blue Gemstones for Magnus Exorcismus (and Safety Wall /
  Warp Portal).
- **Boss role:** healer: Heal the tank first, Kyrie on the tank, Blessing/Increase AGI on all; never fight; Lex Aeterna
  before big hits; the party retreats if the priest dies.
- **Do:** heal first, keep SP for healing, sit only for low SP when nobody needs a heal or buff (CL17, PT29). **Don't:** melee monsters, spend SP on buffs below 60%,
  stop the party for idle sitting.

### Variant: "Magnus Exorcismus Priest" (plan `priest_me`, Base priest; support role allowed)
- INT 99, DEX 90, VIT 70. Acolyte part as the support priest | Priest: Kyrie 5, Aqua Benedicta, Impositio 3, Aspersio 4,
  Sanctuary 3, Safety Wall 1, Status Recovery, Increase SP Recovery 4, Resurrection, Lex Divina 5, Turn Undead 3, Lex
  Aeterna, Magnus Exorcismus 10 early, then Magnificat 5, Kyrie 10, Sanctuary 7, Gloria 5. Heals the party first;
  Magnus Exorcismus on 4+ undead/demons (crowdSkills, Blue Gemstone).

### Monk: "AGI/STR Combo Monk" (plan `monk`, end goal Champion)
- **Party role:** melee DPS / off-tank.
- **Stats:** STR 20, AGI 20, VIT 15, DEX 15 -> STR 40, AGI 40, VIT 25, DEX 25 -> STR 60, AGI 60, VIT 40, DEX 35,
  INT 15 -> STR 80, AGI 80, VIT 50, DEX 45 -> STR 90.
- **Skills (order, build review run 3):** Heal 3 first (a monk-plan acolyte may be the party's only support), Divine
  Protection 10, Demon Bane 10, Increase AGI 10, Blessing 6 | Monk: Iron Fists (Iron Hand) 5, Summon Spirit Sphere 5,
  Dodge (Flee) 5, Raging Trifecta Blow (Triple Attack) 5, Raging Quadruple Blow (Chain Combo) 5, Raging Thrust (Combo
  Finish) 5, Triple Attack 10, Absorb Spirit Sphere 1, Fury (Critical Explosion) 3, Iron Fists 10, Occult Impaction
  (Investigate) 3, Throw Spirit Sphere 3, Asura Strike 5. (The combo comes before Triple Attack 10: DB 2026-11-14 had
  only 9 of 75 Monks with Chain Combo and 1 with Combo Finish. Old order in `Legacy`.)
- **Use:** combo Triple Attack -> Chain Combo -> Combo Finish (weaponskill.go comboNext, sent when the previous step
  hits). Asura Strike only as the combo's finisher after Combo Finish, on a tough target (boss or 5,000+ HP), with SP
  >= 85% and 4+ spirit spheres - never on trash. Combo Finish spends a spirit sphere: once it (or Asura) is learned,
  Call Spirits is a key skill, resummoned down to the 30% SP floor (run 5: TheZerolord cast Chain Combo 71x and
  Combo Finish 0x in 5 min - the combo kept SP under the 60% buff line, so no sphere was ever summoned).
- **Combo timing (run 6 root cause, weaponskill.go comboNext):** with the sphere / combo-window trace, 6 Monks sent a
  Chain Combo after every Triple Attack and 0 ever hit: rAthena refuses a combo step sent during the previous step's
  delay (canact_tick; the SKILLINTERVAL failure is hidden unless @showdelay) and the combo state lasts only delay +
  300 ms. The server announces the delay (ZC_COMBODELAY 0x01d2, 630-650 ms here) just before the 0x01de - the next
  step is now sent ~40 ms after that delay ends (Chain Combo, Combo Finish, the Asura finisher).
- **Gear:** lv1-30 Mace / Waghnak ([4]), Adventurer's Suit / Padded Armor / Chain Mail; lv12-99 knuckles Waghnak ->
  Studded Knuckles -> Claw, Saint's Robe. Cards (top): Andre (ATK+20), Raydric, Matyr, Mantis.
- **Boss role:** tank/DPS: Triple Attack -> Chain Combo -> Combo Finish; Asura Strike burst later.

### Variant: "Asura Strike Monk" (plan `monk_asura`, Base monk; run 4)
- **Stats:** STR 90, INT 70, DEX 55, VIT 50 (STR/INT/DEX/VIT steps; INT = SP for Asura and SP regen).
- **Skills:** Acolyte part as the combo Monk | Monk: Iron Fists 5, Summon Spirit Sphere 5, Absorb Spirit Sphere 1, Fury
  3, Occult Impaction 3, Throw Spirit Sphere 3, Asura Strike 5, Dodge 5, Root 2, Spiritual Cadence 5, Fury 5, Iron
  Fists 10, Triple Attack 5.
- **Use (weaponskill.go `asuraBurst`, after fightSkills in fight()):** keeps 5 spheres (Call Spirits); on a boss /
  champion or a monster with HP >= 20 x its base level, at 85%+ SP with 5 spheres: Fury (3 min), then (spheres back)
  Asura Strike; trash gets auto-attacks. The combo Monk never bursts outside its combo.
- **Gear:** Mace[4] / Waghnak[4] -> knuckles or a Morning Star; SP gear (Sohee shoes, INT headgear); Andre, race cards,
  Mantis, Picky, Raydric.

### Build milestones and playstyle (draft for owner review, 2026-11-15)
- Per-range stats and skills (replayed from the plans at each gear-band end) and the solo / MVP / PvP playstyle of every Acolyte-line build are in `legion/data/buildmilestones.json` (builds priest, priest_me, priest_battle, monk, monk_asura, monk_occult). Draft for owner review, 2026-11-15; the party role stays as above (code). Gear is by band in `buildgear.json`.
- Findings (the existing plan versus Renewal and the server's limits; plans unchanged):
  - `priest`: INT 99 first leaves AGI, DEX and VIT at 1 until about base level 65; the plan costs 1,688 points (1,273 exist) so DEX ends 70 and VIT 65. 110 skill points are listed: Lex Aeterna, Turn Undead, Magnus Exorcismus, Resurrection and Safety Wall are never reached, which the boss-role text (Lex Aeterna before big hits) assumes. Real Renewal support Priests carry DEX 40+ and VIT 30+ much earlier.
  - `monk`: Asura Strike (and Throw Spirit Sphere 3) is never reached; STR ends 82. `monk_asura` reaches Asura with INT 67 of 70.
