# Market engine: verified facts (WP1)

Companion to `docs/design/market-engine.md` section 1 (the design file is not edited). Checked 2026-11-15 by the fixer
on branch `fix/market-facts`. Source: read-only `docker exec ro-rathena` and `docker cp` of the rathena db/npc/conf files
into WSL `/tmp/rofacts` (nothing written in the container). Paths below are inside the container `/rathena/...`.

## 1. Creamy Card

| fact | value | source |
|---|---|---|
| item id, AegisName, name | 4040, `Creamy_Card`, "Creamy Card", Type Card | `db/re/item_db_etc.yml:3200` |
| slot | Card with `Both_Accessory: true` (no slot of its own; goes into an accessory that has a slot) | same |
| script | `skill "AL_TELEPORT",1;` | same |
| accessory that takes it | Belt, id 2627, `Slots: 1`, `Both_Accessory`, `EquipLevelMin: 25`, Buy 20000 | `db/re/item_db_equip.yml:33725` |
| dropper (normal) | mob 1018 `CREAMY` "Creamy", lv 23, `Creamy_Card` `Rate: 1`, `StealProtected` | `db/re/mob_db.yml:901` |
| dropper (champion) | mob 2863 `C1_CREAMY` "Swift Creamy", lv 23, `Creamy_Card` `Rate: 5` | `db/re/mob_db.yml:76377` |
| not droppers | 1231 `META_CREAMY` (lv 16), 2576 `G_CREAMY` (lv 23) have no card; 1293 "Creamy Fear" drops `Cremy_Fear_Card` (another card) | `mob_db.yml` |
| server drop multiplier | `conf/import/battle_conf.txt`: `item_rate_card: 500` (and common/equip/use/heal 500, `base_exp_rate` 500). Imported by `conf/battle_athena.conf:64` | container conf |
| effective card chance | Creamy 1/10000 x 5 = **0.05 %** per kill (about 1 in 2,000 kills); Swift Creamy 5/10000 x 5 = **0.25 %** (about 1 in 400 kills) | derived |
| spawns (`npc/re/mobs/fields`) | Creamy 1018: prt_fild02 x40 (prontera.txt:39), gef_fild05 x143 (geffen.txt:93), pay_fild07 x26 (payon.txt:79), mjolnir_01 x23, mjolnir_06 x30. Swift Creamy: gef_fild05 x2 as a champion (`championmobs.txt:87`, respawn 180 s) | container npc files |

Engine constants:
- `itemCreamyCard = 4040` (`legion/guildstock.go:36`) and `itemCreamy = 4040` (`legion/teleport.go:19`) both match the item db.
  No code change was needed. `legion/kitecount_test.go` (`TestCreamyCardIDMatchesItemDB`) pins both to 4040.

Skill: `AL_TELEPORT` (skill_db id 26) has `MaxLevel: 2`. Level 1 is the card's level (random cell); level 2 opens the
save-point menu, as `legion/teleport.go` already assumes.

Supply consequence: at the real server rate a farmer needs about 2,000 Creamy kills on average for one card (about 11
hours at 3 kills/min, which is the design's fallback), or about 400 Swift Creamy kills. The design's "0.01 %" was the
official base rate and is 5x too low for this server.

## 2. Hunter skills (`db/re/skill_db.yml`)

| skill (id) | MaxLevel | Range | notes |
|---|---|---|---|
| AC_DOUBLE, Double Strafe (46) | 10 | -9 | Multi_Hit, HitCount 2, Element Weapon. Flags `AlterRangeVulture: true` (Vulture's Eye changes its range) |
| AC_SHOWER, Arrow Shower (47) | 10 | -9 | Ground, Knockback 2 |
| HT_ANKLESNARE, Ankle Snare (117) | 5 | 3 | Ground, Status Ankle; needs a Trap item (engine `itemTrap = 1065`) |
| HT_SKIDTRAP, Skid Trap (115) | 5 | 3 | Ground, Knockback, Duration2 3000 |
| HT_LANDMINE, Land Mine (116) | 5 | 3 | Element Earth, Status Stun |
| HT_BLITZBEAT, Blitz Beat (129) | 5 | 5 | Multi_Hit, CastTime 800, FixedCastTime 200 (falcon requirement is in its Requires block, not re-read) |
| AC_OWL (43), AC_VULTURE (44) | 10 | - | Owl's Eye / Vulture's Eye |
| AL_TELEPORT (26) | 2 | - | see above |

Trap item: id **1065**, `AegisName: Booby_Trap`, Name "Trap", Type Etc, Buy 50 (`item_db_etc.yml:1904`).
`kite.go` `itemTrap = 1065` is correct. The design's grep `AegisName: Trap$` finds nothing because the AegisName is
`Booby_Trap`; the display name is "Trap".

Negative Range (Double Strafe -9): the sign convention is not documented in `doc/skill_db.txt` (it only says Range is the
skill range and that below 5 is melee). The "9 cells" in the design is therefore not verified in the server source;
it is consistent with a bow's base range of 9.

## 3. Mob speed (the field is `WalkSpeed`)

- The key is **`WalkSpeed`**, in milliseconds per cell: lower is faster. The default player walk speed is
  `DEFAULT_WALK_SPEED 150` (`src/common/mmo.hpp:93`). No mob called `Speed` exists in `mob_db.yml`.
- Examples: Creamy 1018 WalkSpeed 150; Swift Creamy 2863 WalkSpeed 112; Creamy Fear 1293 WalkSpeed 155; plants (Green Plant, Flora) 1000.
- Typical farmer maps (every `prt_fild*`, `pay_fild*`, `moc_fild*`, `gef_fild*`, `mjolnir_*` spawn line in
  `npc/re/mobs/fields`, weighted by spawn count):

| share of spawns | prt_fild | pay_fild | moc_fild | gef_fild | mjolnir |
|---|---|---|---|---|---|
| WalkSpeed < 150 (faster than a default walker) | 0 % except prt_fild11 4 % | 0 % | 0 % | 0 % except gef_fild11 25 % | 0 % |
| WalkSpeed = 150 (same as a default walker) | Hornet, Thief Bug (prt_fild05, a large share) | a few (Coco, Savage) | a few (Steel Chonchon, Condor) | Creamy, Coco | Creamy, Savage, Dustiness |
| ranged (AttackRange >= 2) | prt_fild11 41 % (Goblin Archer, Rotar Zairo) | pay_fild10 47 % (Greatest General) | 0 % | gef_fild04 61 %, gef_fild08 15 % | 14-16 % (Flora, range 3) |

The only clearly fast melee mobs on the farm maps are the gef_fild11 Goblin variant at WalkSpeed 100 (lv 48; the other
Goblin variants are 150 and 250). The Morocc
incarnations (110, lv 132) are not a farmer map. Most farm mobs walk at 200-400, slower than a default walker.

## 4. Character slots per account

- `MAX_CHARS` is 15 when `PACKETVER >= 20180124`, 12 for `>= 20100413`, else 9 (`src/common/mmo.hpp:66-72`).
- `chars_per_account: 0` (`conf/login_athena.conf:102`) means "use MAX_CHARS". `MAX_CHAR_VIP 6` (`src/config/core.hpp:65`)
  is the VIP extra, not the normal limit.
- The build's PACKETVER was not read (not in the conf files checked), so the limit is 15 if the server was built with
  PACKETVER >= 20180124, otherwise 12 or 9. Personas use slots 0-3 (design section 2.1), which is safe either way.

## 5. Differences from the design (`market-engine.md`)

1. **Creamy drop**: the design says 0.01 % (official base rate). This server imports `item_rate_card: 500`, so a normal
   Creamy drops at **0.05 %** and a Swift Creamy at **0.25 %**. Yield math in section 3.1 must use the 5x rate.
2. **Speed filter (section 3.2, speed_fit 1.3 for fast melee)**: on the farm maps almost no mob has WalkSpeed < 150. The
   filter would fire only on the gef_fild11 Goblins. The useful distinction is WalkSpeed 150 (same as a default walker,
   for example Creamy, Coco, Savage) against 200-400. Proposal: apply the 1.3 to WalkSpeed <= 150 melee mobs, and read
   the key `WalkSpeed`, not `Speed`.
3. **Creamy maps**: the design lists prt_fild02 and gef_fild05. The spawn files also have Creamy on pay_fild07, mjolnir_01
   and mjolnir_06.
4. **Trap item name**: `Booby_Trap` (Name "Trap"), id 1065 as the design says. Only the grep was wrong.
5. **Double Strafe range**: the sign of Range -9 is not verified in the source (section 2).
6. **Docker**: the design says docker was not installed in its session. It is installed now and `ro-rathena` was running
   during this check; the facts above come from read-only copies (nothing was started or changed).
7. **Teleport card (design 1.2)**: confirmed. Card = `AL_TELEPORT,1`, accessory-slotted, Belt slot 1, Creamy the mob.
