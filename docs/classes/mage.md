# Mage line: Wizard, Sage

General class rules: `all.md`. Owner words: `docs/rules/archive.md`.

## Mage -> Wizard / Sage (caster line)

### Rules
- CL06 (also EC03). Mages and Wizards are AoE builds: their plans focus on area spells, cast whenever 3 or more monsters are
  grouped. Existing bolt Mages are not wrong builds; they keep their levels and every new skill point goes to area spells. (2026-11-14)
- CL07. A Wizard or Mage casts an area spell at every chance a group forms, placed on the cell that covers the most monsters (the
  crowd centre); it never idles or single-targets in a crowd. (2026-11-14)
- CL16. A caster (Wizard, Mage, Sage) whose SP sits full most of the time is a bug: it should be spending SP on spells in every fight and resting only to refill. (2026-11-14)
- CL08. A party with a Wizard plays around it: tank and melee lure and gather monsters into a tight group in the Wizard's range,
  the Wizard drops its AoE on the centre, the others pick off survivors. Applies to every such party, Ayla's included. (2026-11-14)

### Wizard: "INT/DEX Wizard" (plan `wizard`, end goal Wizard)
- **Party role:** ranged AoE damage; clears grouped monsters the tank gathers.
- **Stats:** INT 20, DEX 15, VIT 10, INT 40, DEX 30, VIT 20, INT 60, DEX 50, VIT 35, INT 80, DEX 65, VIT 50, INT 99, DEX 70, VIT 60.
- **Skills (order):** Fire Bolt 5, Fire Ball 5, Lightning Bolt 4, Thunderstorm 5, Sight 1, Fire Wall 5, Napalm Beat 1, Frost Diver 1, Stone Curse 1, Fire Bolt 10, Cold Bolt 10, Lightning Bolt 10, Jupitel Thunder 5, Storm Gust 10, Earth Spike 3, Heaven's Drive 5, Lord of Vermilion 10, Sightrasher 2, Meteor Storm 4.
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

### Sage: "INT/DEX Battle Sage" (plan `sage`, end goal Sage)
- **Party role:** caster support: bolts with Free Cast on the move, Heaven's Drive on adds, endows for the melee.
- **Stats:** INT 20, DEX 15, VIT 10, INT 40, DEX 30, VIT 20, INT 60, DEX 50, VIT 35, INT 80, DEX 65, VIT 50, INT 99, DEX 70, VIT 60.
- **Skills (order):** Fire Bolt 10, Cold Bolt 5, Lightning Bolt 5, Sight 1, Stone Curse 1, Napalm Beat 1, Frost Diver 1, Fire Ball 5, Thunderstorm 5, Fire Wall 5, Study 10, Endow Quake 1, Earth Spike 3, Heaven's Drive 5, Cast Cancel 1, Free Cast 10, Magic Rod 1, Spell Breaker 5, Dispell 5, Endow Quake 2, Endow Quake 3, Endow Quake 4, Endow Quake 5, Earth Spike 4, Earth Spike 5, Cast Cancel 2, Cast Cancel 3.
- **Use:** Heaven's Drive / Fire Ball on 3+ grouped (aoe.go covers the whole Mage family); bolts on single targets,
  the counter element the character has learned (skills.go `boltFor`: Fire monster -> Cold Bolt, Water -> Lightning
  Bolt, Wind -> Earth Spike, else Fire Bolt; level capped at the learned level). Endows need their elemental stones
  (Red Blood / Crystal Blue / Wind of Verdure / Green Live) - no endow AI yet; no Spell Breaker / Dispell AI yet.
  DB 2026-11-14: 62 Sages avg lv56 / job 13; 29 have Heaven's Drive, 48 Earth Spike, 2 Auto Spell.
- **Gear:** as the Wizard; books come later from drops / trade.
- **Boss role:** bolts with Free Cast, Heaven's Drive on adds, Dispell on buffed bosses, endow (Flame Launcher / Frost
  Weapon) for the melee.

### Variant: "Support Sage" (plan `sage_support`, Base sage; run 4, 2026-11-14)
- **Stats:** INT 20, DEX 15, VIT 15, INT 40, DEX 30, VIT 30, INT 60, DEX 45, VIT 45, INT 80, DEX 60, VIT 60, INT 99, DEX 70.
- **Skills (order):** Fire Bolt 10, Cold Bolt 5, Lightning Bolt 5, Sight 1, Stone Curse 1, Napalm Beat 1, Frost Diver 1, Fire Ball 5, Study 10, Endow Quake 3, Endow Blaze 2, Endow Tsunami 2, Endow Tornado 2, Volcano 3, Deluge 3, Whirlwind 3, Magnetic Earth 1, Magic Rod 1, Spell Breaker 3, Dispell 5, Earth Spike 3, Heaven's Drive 5, Cold Bolt 6, Cold Bolt 7, Cold Bolt 8, Cold Bolt 9, Cold Bolt 10, Lightning Bolt 6, Lightning Bolt 7, Lightning Bolt 8, Lightning Bolt 9, Lightning Bolt 10, Endow Quake 4, Endow Quake 5, Endow Blaze 3.
- **Use:** fights with bolts / Earth Spike / Heaven's Drive like the battle Sage (aoe.go, boltFor). The support skills
  have **no AI yet**: endows need elemental stones, Volcano / Deluge / Whirlwind a Blue Gemstone each, Dispell a Yellow
  Gemstone, Magnetic Earth both (open item).
- **Gear:** Rod[4] -> Wand / Staff[3] or Book[3] -> Wizardry Staff / Bible / Tablet; cards Elder Willow (INT+2), Willow
  (MaxSP+80), Drops, Sohee (MaxSP+15%), Verit, Phen (casts not interrupted), Raydric.

### Build milestones and playstyle (rebalanced 2026-11-15)
- Per-range stats and skills (replayed from the plans at each gear-band end) and the solo / MVP / PvP playstyle of every build of this line are in `legion/data/buildmilestones.json` (`wizard`, `sage`, `sage_support`, `wizard_fire`, `wizard_frost`, `sage_hindsight`). Gear is by band in `buildgear.json` (curated) and with its alternatives, refine and card plan in the same file as the stats (EC40-EC43); the party role stays in code.
- Plan budget at level 99 (1,273 stat points, 88 skill points), before and after the 2026-11-15 rebalance:
  - `wizard` (INT/DEX Wizard): stat goals 1,322 before (budget 1,273), skill points 117 before (budget 88), not reached before: VIT 60; Fire Bolt 6/10; Lightning Bolt 5/10; Fire Ball 5/10; Fire Wall 5/10; Cold Bolt 5/10; Jupitel Thunder 5/10; now stat goals 1,233 (spare 40), skill points 88, ends VIT 60, INT 99, DEX 70, not reached now: none.
  - `sage` (INT/DEX Battle Sage): stat goals 1,322 before (budget 1,273), skill points 98 before (budget 88), not reached before: VIT 60; Endow Blaze 1/2; Endow Tsunami 0/2; Endow Tornado 0/2; Dispell 0/5; now stat goals 1,233 (spare 40), skill points 88, ends VIT 60, INT 99, DEX 70, not reached now: none.
  - `sage_support` (Support Sage): stat goals 1,312 before (budget 1,273), skill points 98 before (budget 88), not reached before: VIT 70; Study 5/10; Endow Quake 1/3; Whirlwind 1/3; Magnetic Earth 0/1; now stat goals 1,233 (spare 40), skill points 88, ends VIT 60, INT 99, DEX 70, not reached now: none.
  - `wizard_fire` (INT/DEX Fire Wizard): stat goals 1,336 before (budget 1,273), skill points 92 before (budget 88), not reached before: VIT 55; Lightning Bolt 6/10; now stat goals 1,233 (spare 40), skill points 88, ends VIT 60, INT 99, DEX 70, not reached now: none.
  - `wizard_frost` (INT/DEX Frost Wizard): stat goals 1,322 before (budget 1,273), skill points 83 before (budget 88), not reached before: VIT 60; now stat goals 1,233 (spare 40), skill points 88, ends VIT 60, INT 99, DEX 70, not reached now: none.
  - `sage_hindsight` (INT/AGI Hindsight Sage): stat goals 1,221 before (budget 1,273), skill points 94 before (budget 88), not reached before: Free Cast 9/10; Magic Rod 0/1; Spell Breaker 0/3; Dispell 0/1; now stat goals 1,270 (spare 3), skill points 88, ends AGI 60, VIT 40, INT 90, DEX 70, not reached now: none.
- Gear and cards at each build's end band (`legion/data/buildmilestones.json`, the Builds tab shows every band):
  - `wizard` gear at the end band 60-99 (EC40): weapon Survivor's Rod (Dex) (3 alternatives), shield Guard (3 alternatives), armor Mage Coat (3 alternatives), headgear Circlet (3 alternatives), garment Muffler (3 alternatives), shoes Shoes (3 alternatives), accessory Earring (3 alternatives); cards (EC41): 7 usable now, 2 at the refine target, 0 fallback.
  - `sage` gear at the end band 60-99 (EC40): weapon Survivor's Rod (Dex) (3 alternatives), shield Guard (3 alternatives), armor Mage Coat (3 alternatives), headgear Circlet (3 alternatives), garment Muffler (3 alternatives), shoes Shoes (3 alternatives), accessory Earring (3 alternatives); cards (EC41): 7 usable now, 2 at the refine target, 0 fallback.
  - `sage_support` gear at the end band 60-99 (EC40): weapon Tablet (3 alternatives), shield Guard (3 alternatives), armor Mage Coat (3 alternatives), headgear Circlet (3 alternatives), garment Muffler (3 alternatives), shoes Shoes (3 alternatives), accessory Earring (3 alternatives); cards (EC41): 8 usable now, 2 at the refine target, 0 fallback.
  - `wizard_fire` gear at the end band 60-99 (EC40): weapon Survivor's Rod (Dex) (3 alternatives), shield Guard (3 alternatives), armor Mage Coat (3 alternatives), headgear Circlet (3 alternatives), garment Muffler (3 alternatives), shoes Shoes (3 alternatives), accessory Earring (3 alternatives); cards (EC41): 7 usable now, 2 at the refine target, 0 fallback.
  - `wizard_frost` gear at the end band 60-99 (EC40): weapon Survivor's Rod (Dex) (3 alternatives), shield Guard (3 alternatives), armor Mage Coat (3 alternatives), headgear Circlet (3 alternatives), garment Muffler (3 alternatives), shoes Shoes (3 alternatives), accessory Earring (3 alternatives); cards (EC41): 7 usable now, 2 at the refine target, 0 fallback.
  - `sage_hindsight` gear at the end band 60-99 (EC40): weapon Book of Billows (3 alternatives), shield Guard (3 alternatives), armor Mage Coat (3 alternatives), headgear Circlet (3 alternatives), garment Muffler (3 alternatives), shoes Shoes (3 alternatives), accessory Earring (3 alternatives); cards (EC41): 7 usable now, 2 at the refine target, 0 fallback.
- Existing characters: a changed plan keeps the earlier goals in `Legacy` / `LegacyStats` (builds.go), so they are not wrong builds; `tests/tools/test_wrongbuilds_replay.py` replays the pre-rebalance plans through the wrongbuilds.py rules.
