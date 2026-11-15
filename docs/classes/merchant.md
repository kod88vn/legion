# Merchant line: Blacksmith, Alchemist

General class rules: `all.md`. Owner words: `docs/rules/archive.md`.

## Merchant -> Blacksmith / Alchemist

### Rules
- CL14. Every guild eventually has an Alchemist who brews potions (SP potions included) and sells them (EC11). (2026-11-14)
- CL11. Vendor Merchants learn Vending: a vendor plan starts Enlarge Weight Limit 5, Pushcart 3, Vending 3. Vendor Merchants
  with Mammonite and no Vending were wrong builds and were restarted. (2026-11-14)

### Blacksmith: "STR/DEX Axe Blacksmith" (plan `blacksmith`, end goal Whitesmith)
- **Party role:** melee DPS; vends on the side.
- **Stats:** STR 20, DEX 15, VIT 10, AGI 10 -> STR 40, DEX 30, VIT 25, AGI 25 -> STR 60, DEX 45, VIT 40, AGI 45 ->
  STR 80, DEX 55, AGI 60, VIT 50, LUK 20 -> STR 90.
- **Skills (order):** Enlarge Weight Limit 5, Pushcart 3, Mammonite 10, Vending 3, Discount 3, Overcharge 10, Enlarge
  Weight Limit 10 | Blacksmith: Hammer Fall 2, Adrenaline Rush 5, Hilt Binding 1, Weaponry Research 2, Weapon
  Perfection 3, Power-Thrust (Over Thrust) 5, Weaponry Research 10, Weapon Perfection 5, Maximize Power 5, Hammer Fall
  5, Weapon Repair 1, Skin Tempering 5, Pushcart 10.
- **Use:** Adrenaline Rush is a key buff (needs an axe or mace; 30% SP floor); Cart Revolution on 3+ around the target
  (needs the cart); Hammer Fall stuns crowds.
- **Gear:** lv1-30 Axe / Battle Axe ([4]); lv16-60 Battle Axe -> Hammer -> Buster; lv30-99 Buster[2] / Two-Handed
  Axe[2]; Adventurer's Suit / Padded Armor / Chain Mail. Cards (top): Andre (ATK+20), Raydric, Matyr, Mantis.
- **Boss role:** melee DPS: Adrenaline Rush (party ASPD), Over Thrust, Hammer Fall stuns the adds.

### Variant: "VIT/STR Mace Blacksmith" (plan `blacksmith_mace`, Base blacksmith; run 4)
- **Stats:** STR 90, VIT 70, DEX 50, AGI 15. **Skills:** Merchant part as the main plan | Hammer Fall 5 first, Adrenaline
  Rush 5, Hilt Binding, Weaponry Research 2, Weapon Perfection 3, Power-Thrust 5, Maximize Power 5, Skin Tempering 5,
  Weaponry Research 10, Weapon Perfection 5, Weapon Repair, Pushcart 10. No forging skills (no forge AI).
- **Weapon:** maces only (planWeaponSubs Mace / 2hMace): Club[4] / Mace[4] -> Smasher / Flail / Chain[3] -> Morning
  Star / Sword Mace. Adrenaline Rush works with maces; Hammer Fall on crowds (skillbuff.go). Cards: Andre, race/size,
  Mantis, Raydric, Matyr, Pupa (MaxHP+700), Marc-type VIT+2 accessory.

### Alchemist: "STR/INT Alchemist" (plan `alchemist`, end goal Creator)
- **Party role:** support / DPS; the guild's potion brewer.
- **Stats:** STR 20, DEX 15, INT 10, VIT 15 -> STR 40, DEX 30, VIT 30, INT 25 -> STR 60, DEX 45, VIT 45, INT 40 ->
  STR 75, DEX 60, VIT 60.
- **Skills (order):** Merchant as the Blacksmith | Alchemist: Potion Research 5, Prepare Potion (Pharmacy) 5 first, Axe
  Mastery 10, Aid Potion (Potion Pitcher) 5, Bioethics 1, Vaporize 1, Call Homunculus 1, Homunculus Resurrection 1,
  Potion Research 10, Prepare Potion 10, Bomb (Demonstration) 5, Acid Terror 5.
- **Use:** brews potions (alchemy.go: Potion Creation Guide 100k + Medicine Bowls at the Guild Dealer in alde_alche,
  bottles + Scell at the south gate Goods Merchant; herbs are offered to alchemists, never NPC-sold); Acid Terror /
  Demonstration only with their bottle in the bag.
- **Gear:** lv1-30 Axe / Battle Axe; lv16-99 Battle Axe / Axe / Mace, Adventurer's Suit / Saint's Robe. Cards as the
  Blacksmith.
- **Special items:** Acid Bottle (Acid Terror), Bottle Grenade / Fire Bottle (Demonstration), Empty Bottles, Medicine
  Bowls, the Potion Creation Guide.
- **Boss role:** support: potions for the party (Potion Pitcher), Acid Terror on the boss.

### Variant: "STR/DEX Battle Alchemist" (plan `alchemist_battle`, Base alchemist; run 4)
- **Stats:** STR 90, DEX 55, VIT 55, AGI 30. **Skills:** Merchant part as the main plan | Axe Mastery 10 first, Potion
  Research 5, Prepare Potion 5, Acid Terror 5, Bomb 5, homunculus (Bioethics, Vaporize, Call, Resurrection), Aid Potion
  5, Potion Research / Prepare Potion 10. Still a guild-alchemist candidate (it has Prepare Potion).
- **Gear:** Axe[4] / Battle Axe[4] (Axe Mastery); Acid Bottles / Bottle Grenades for Acid Terror / Bomb (progress.go
  skillItem). Cards: Andre, race/size, Mantis, Picky, Raydric, Matyr.

### Variant: "INT/DEX/LUK Potion Alchemist" (plan `alchemist_potion`, Base alchemist; run 4)
- **Stats:** INT 90, DEX 80, LUK 60, STR 30, VIT 35 (Renewal brew rate: Potion Research, Prepare Potion, job level,
  INT, DEX, LUK; some STR to farm).
- **Skills:** Merchant: Enlarge Weight Limit 5, Pushcart 3, **Vending 3** (sells its potions), Mammonite 5, Discount 3,
  Overcharge 10, Enlarge Weight Limit 10, Mammonite 10 | Alchemist: Potion Research 5 -> Prepare Potion 5 -> both 10,
  Aid Potion 5, homunculus, Bomb 5, Acid Terror 5, Axe Mastery 10.
- **Use:** alchemy.go prefers it as the guild's alchemist (+500 on the pick score, ~5 job levels). Aid Potion
  (transfight.go aidPotion, every Alchemist): throws the best potion it carries (Red/Orange/Yellow/White up to the
  learned level) at the most hurt party member under 45% HP within 9 cells. Gear: axes; cards
  Elder Willow (INT+2), Zerom (DEX+3), DEX+2 accessory, Poring (LUK+2), Drops, Sohee, Raydric. A weak fighter: it
  levels with its party (RULES.md 2 "Gear for classes that can't farm").

### Build milestones and playstyle (rebalanced 2026-11-15)
- Per-range stats and skills (replayed from the plans at each gear-band end) and the solo / MVP / PvP playstyle of every build of this line are in `legion/data/buildmilestones.json` (`blacksmith`, `alchemist`, `alchemist_battle`, `alchemist_potion`, `blacksmith_mace`). Gear is by band in `buildgear.json` (curated) and with its alternatives, refine and card plan in the same file as the stats (EC40-EC43); the party role stays in code.
- Plan budget at level 99 (1,273 stat points, 88 skill points), before and after the 2026-11-15 rebalance:
  - `blacksmith` (STR/DEX Axe Blacksmith): stat goals 1,262 before (budget 1,273), skill points 88 before (budget 88), not reached before: none; now stat goals 1,262 (spare 11), skill points 84, ends STR 90, AGI 60, VIT 50, DEX 55, LUK 20, not reached now: none.
  - `alchemist` (STR/INT Alchemist): stat goals 1,047 before (budget 1,273), skill points 88 before (budget 88), not reached before: none; now stat goals 1,269 (spare 4), skill points 84, ends STR 85, VIT 60, INT 60, DEX 60, not reached now: none.
  - `alchemist_battle` (STR/DEX Battle Alchemist): stat goals 1,072 before (budget 1,273), skill points 88 before (budget 88), not reached before: none; now stat goals 1,260 (spare 13), skill points 84, ends STR 90, AGI 30, VIT 70, DEX 65, not reached now: none.
  - `alchemist_potion` (INT/DEX/LUK Potion Alchemist): stat goals 1,420 before (budget 1,273), skill points 88 before (budget 88), not reached before: DEX 80; LUK 60; now stat goals 1,256 (spare 17), skill points 84, ends STR 30, VIT 35, INT 85, DEX 75, LUK 50, not reached now: none.
  - `blacksmith_mace` (VIT/STR Mace Blacksmith): stat goals 1,098 before (budget 1,273), skill points 88 before (budget 88), not reached before: none; now stat goals 1,241 (spare 32), skill points 77, ends STR 95, AGI 15, VIT 80, DEX 50, not reached now: none.
- Gear and cards at each build's end band (`legion/data/buildmilestones.json`, the Builds tab shows every band):
  - `blacksmith` gear at the end band 30-99 (EC40): weapon Two-Handed Axe (3 alternatives), shield Bradium Shield (3 alternatives), armor Chain Mail (3 alternatives), headgear Hat (3 alternatives), garment Manteau (3 alternatives), shoes Boots (3 alternatives), accessory Ring (3 alternatives); cards (EC41): 10 usable now, 2 at the refine target, 0 fallback.
  - `alchemist` gear at the end band 16-99 (EC40): weapon Mace (3 alternatives), shield Bradium Shield (3 alternatives), armor Saint's Robe (3 alternatives), headgear Hat (3 alternatives), garment Manteau (3 alternatives), shoes Boots (3 alternatives), accessory Ring (3 alternatives); cards (EC41): 10 usable now, 2 at the refine target, 0 fallback.
  - `alchemist_battle` gear at the end band 16-99 (EC40): weapon Smasher (3 alternatives), shield Bradium Shield (3 alternatives), armor Saint's Robe (3 alternatives), headgear Hat (3 alternatives), garment Manteau (3 alternatives), shoes Boots (3 alternatives), accessory Ring (3 alternatives); cards (EC41): 10 usable now, 4 at the refine target, 0 fallback.
  - `alchemist_potion` gear at the end band 16-99 (EC40): weapon Mace (3 alternatives), shield Bradium Shield (3 alternatives), armor Saint's Robe (3 alternatives), headgear Hat (3 alternatives), garment Manteau (3 alternatives), shoes Boots (3 alternatives), accessory Ring (3 alternatives); cards (EC41): 7 usable now, 2 at the refine target, 0 fallback.
  - `blacksmith_mace` gear at the end band 27-99 (EC40): weapon Sword Mace (3 alternatives), shield Bradium Shield (3 alternatives), armor Chain Mail (3 alternatives), headgear Hat (3 alternatives), garment Manteau (3 alternatives), shoes Boots (3 alternatives), accessory Ring (3 alternatives); cards (EC41): 10 usable now, 5 at the refine target, 0 fallback.
- Existing characters: a changed plan keeps the earlier goals in `Legacy` / `LegacyStats` (builds.go), so they are not wrong builds; `tests/tools/test_wrongbuilds_replay.py` replays the pre-rebalance plans through the wrongbuilds.py rules.
