# Mage line: Wizard, Sage

General class rules: `all.md`. Owner words: `docs/rules/archive.md`.

## Mage -> Wizard / Sage (caster line)

### Rules
- CL06 (also EC03). Mages and Wizards are AoE builds: their plans focus on area spells, cast whenever 3 or more monsters are
  grouped. Existing bolt Mages are not wrong builds; they keep their levels and every new skill point goes to area spells. (2026-11-14)
- CL07. A Wizard or Mage casts an area spell at every chance a group forms, placed on the cell that covers the most monsters (the
  crowd centre); it never idles or single-targets in a crowd. (2026-11-14)
- CL08. A party with a Wizard plays around it: tank and melee lure and gather monsters into a tight group in the Wizard's range,
  the Wizard drops its AoE on the centre, the others pick off survivors. Applies to every such party, Ayla's included. (2026-11-14)

### Wizard: "INT/DEX Wizard" (plan `wizard`, end goal High Wizard)
- **Party role:** ranged AoE damage; clears grouped monsters the tank gathers.
- **Stats:** INT 20, DEX 15, VIT 10 -> INT 40, DEX 30, VIT 20 -> INT 60, DEX 50, VIT 35 -> INT 80, DEX 65, VIT 50 ->
  INT 99, DEX 80, VIT 60.
- **Skills (order):** Mage: Fire Bolt 5, Lightning Bolt 5 (prerequisites and early single target), Fire Ball 5,
  Thunderstorm 5, Sight 1, Fire Wall 5 (kiting), Cold Bolt 5, Frost Diver 1, Napalm Beat 1, Stone Curse 1 (Wizard
  prerequisites); a Mage's remaining points fill Thunderstorm / Fire Bolt / Lightning Bolt to 10 | Wizard: Jupitel
  Thunder 3, Storm Gust 10 (run 6: moved up), Earth Spike 3, Heaven's Drive 5, Jupitel Thunder 5, Lord of Vermilion 10, Sightrasher 2,
  Meteor Storm 10, Quagmire 3, then bolts / Fire Ball / Jupitel / Fire Wall to 10. Legacy bolt plan (Fire Bolt 10, Cold
  Bolt 10, Lightning Bolt 10 ...) stays valid; its new points go to the first unmet goals = area spells.
- **Use (aoe.go `aoeCast`, runs first in fight()):** (2026-11-14 fix) every walkable cell in range 9 in line of sight is
  scored by the monsters the spell's area covers (element-weighted; resisted spells skipped, Fire on Undead), the best
  cell wins; 3+ monsters (2+ with a strong one: boss/champion, our level+, 15k+ HP); partymates (bots + the human)
  never count as KS. Was: 3+ monsters inside a spell's radius around a monster within range
  9 -> Storm Gust / Lord of Vermilion / Meteor Storm / Thunderstorm / Heaven's Drive on the ground, or Fire Ball on the
  centre monster. SP reserve max(15%, 30). Solo: no boss/swarm, Fire Wall vs a walker; quick spells only while being hit
  without a party tank. No KS (monsters claimed by non-party bots / fighting non-party players).
- **Wizard lure (party, tanklead.go `tankWizardLure`, 2026-11-14):** a party tank with 3+ monsters on it takes the pack
  4 cells in front of the party's area caster (2-7 cells counts as in front: inside cast range, never on it) and fights
  there; while the caster is casting (published `CastUntil`) the tank holds still so the pack stays in the spell. No
  caster near: the tank brings the crowd to the priest as before. Open: other melee don't hold off before the spell.
- **Gear:** lv1-30 Rod (Rod[4] with Drops cards), Silk Robe, Guard, Hat, Hood, Sandals, Rosary; lv25-65 Wand / Staff
  ([3] with Drops cards), Silk Robe / Mage Coat (2nd job), Biretta / Circlet, Muffler, Shoes, Rosary / Clip; lv60-99 Arc
  Wand / Wizardry Staff / Survivor's Rod (Dex) (drops and player trade), Mage Coat / Saint's Robe, Circlet, Manteau,
  Boots, Clip / Earring. Cards (top): Elder Willow (headgear, INT+2), Raydric, Sohee (shoes, MaxSP+15%); Drops (weapon,
  DEX+1 HIT+3: faster casts).
- **Special items:** SP potions (no NPC Blue Potions: they come from guild Alchemists; RULES.md 2); Blue Gemstones for
  Safety Wall if learned.
- **Boss role:** AoE / burst caster: Storm Gust and Lord of Vermilion on the adds, Jupitel/bolts on the boss from max
  range; Safety Wall / Quagmire when the boss closes in.
- **Do:** AoE whenever 3+ monsters are grouped, stay behind the tank, carry SP potions. **Don't:** fight on a Knife (a
  mage line with no class weapon counts as `bowless()` and may call a party run after 6 min); melee.

### Sage: "INT/DEX Battle Sage" (plan `sage`, end goal Professor)
- **Party role:** caster support: bolts with Free Cast on the move, Heaven's Drive on adds, endows for the melee.
- **Stats:** same as the Wizard (INT 99, DEX ~80, VIT 50-60).
- **Skills (order):** run 6 (rule CL06, Mages are AoE builds; QA 13:10: Sage-plan Mages lv40-50 had bolts 10/10/10
  and no area spell until job ~40): Fire Bolt 5, Cold Bolt 5, Lightning Bolt 5, Napalm Beat 1, Frost Diver 1, Stone
  Curse 1, Sight 1, Fire Ball 5, Thunderstorm 5, bolts to 10, Fire Wall 5 (49 points; Support and Hindsight Sages too,
  the Hindsight Sage keeps bolts 10 first, then Fire Ball / Thunderstorm before Napalm 4 / Soul Strike 3). The old
  bolts-first order is in `Legacy`: those Mages put their next points into Fire Ball / Thunderstorm | Sage (build review run 3, 2026-11-14): Study (Advanced Book) 5, Endow Quake 1 (prerequisite), Earth
  Spike 5, Heaven's Drive 5, Cast Cancel 1, Free Cast 4, Magic Rod 1, Spell Breaker 5, Study 10, Free Cast 10, Endow
  Blaze / Tsunami / Tornado 2, Dispell 5. **Auto Spell (Hindsight) is left out** - it is for melee Sages; existing
  Sages that took it are fine (2nd-job skills aren't wrong-build checked; old order in `Legacy`).
- **Use:** Heaven's Drive / Fire Ball on 3+ grouped (aoe.go covers the whole Mage family); bolts on single targets,
  the counter element the character has learned (skills.go `boltFor`: Fire monster -> Cold Bolt, Water -> Lightning
  Bolt, Wind -> Earth Spike, else Fire Bolt; level capped at the learned level). Endows need their elemental stones
  (Red Blood / Crystal Blue / Wind of Verdure / Green Live) - no endow AI yet; no Spell Breaker / Dispell AI yet.
  DB 2026-11-14: 62 Sages avg lv56 / job 13; 29 have Heaven's Drive, 48 Earth Spike, 2 Auto Spell.
- **Gear:** as the Wizard; books come later from drops / trade.
- **Boss role:** bolts with Free Cast, Heaven's Drive on adds, Dispell on buffed bosses, endow (Flame Launcher / Frost
  Weapon) for the melee.

### Variant: "Support Sage" (plan `sage_support`, Base sage; run 4, 2026-11-14)
- **Stats:** INT 99, DEX 70, VIT 70 (INT/DEX/VIT steps; tougher than the battle Sage).
- **Skills:** Mage part as the battle Sage (bolts 10) | Sage: Study 5, Endow Quake 1, Earth Spike 3, Heaven's Drive 3,
  Cast Cancel 1, Free Cast 4, Magic Rod 1, Spell Breaker 3, Dispell 5, Endow Blaze / Tsunami / Tornado 2, Volcano 3,
  Deluge 3, Whirlwind 3, Magnetic Earth 1, Study 10, Endow Quake 3.
- **Use:** fights with bolts / Earth Spike / Heaven's Drive like the battle Sage (aoe.go, boltFor). The support skills
  have **no AI yet**: endows need elemental stones, Volcano / Deluge / Whirlwind a Blue Gemstone each, Dispell a Yellow
  Gemstone, Magnetic Earth both (open item).
- **Gear:** Rod[4] -> Wand / Staff[3] or Book[3] -> Wizardry Staff / Bible / Tablet; cards Elder Willow (INT+2), Willow
  (MaxSP+80), Drops, Sohee (MaxSP+15%), Verit, Phen (casts not interrupted), Raydric.

