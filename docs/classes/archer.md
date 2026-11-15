# Archer line: Hunter, Bard, Dancer

General class rules: `all.md`. Owner words: `docs/rules/archive.md`.

## Archer -> Hunter / Bard / Dancer (bow line)

### Rules
- CL13 (also EC26, PT23). Arrows are part of the class's basic utilities (EC26) and buying them is a necessary errand that may take a member away
  briefly (PT23). (2026-11-14)
- Every class uses AoE on crowds: Arrow Shower on the central monster (`all.md`, CL01).

### Hunter: "DEX/AGI Bow Hunter" (plan `hunter`, end goal Sniper)
- **Party role:** ranged DPS from 9 cells.
- **Stats:** DEX 20, AGI 10 -> DEX 40, AGI 20, VIT 10, LUK 10 -> DEX 60, AGI 40, LUK 20, VIT 20 -> DEX 80, AGI 60,
  LUK 30 -> DEX 99, AGI 75, VIT 30, LUK 40.
- **Skills (order):** Archer: Double Strafe 10, Owl's Eye 3, Vulture's Eye 10, Improve Concentration 10, Arrow Shower 5,
  Owl's Eye 4 | Hunter: Beast Bane 1, Falconry Mastery 1, Blitz Beat 5, Steel Crow 10, Beast Bane 10, Arrow Shower 10,
  Owl's Eye 10, then traps (Skid Trap 1, Ankle Snare 3, Land Mine 5, Flasher, Freezing Trap, Sandman, Blast Mine,
  Shockwave Trap, Claymore Trap 5) last - there is no trap AI.
- **Use:** Double Strafe on one monster; Arrow Shower (ground, 3x3, 5x5 from lv6) on 3+ grouped, aimed at the centre;
  Improve Concentration is a key self-buff, cast at the highest level that leaves > 30% SP (key floor; Double Strafe keeps its cost back while it is
  due - run 5 traces: 2 Hunters cast 54 Double Strafes and 0 Improve Concentration in 5 min); the falcon's Blitz Beat.
- **Gear:** lv1-20 Bow / Composite Bow, Adventurer's Suit / Silk Robe, Hat/Cap, Hood/Muffler, Sandals/Shoes; lv18-40
  Great Bow / Crossbow, Silk Robe / Mink Coat, Glasses / Sunglasses, Manteau, Glove; lv33-99 Gakkung Bow / Arbalest /
  Hunter Bow, Mink Coat / Tights (lv45), Sunglasses / Apple of Archer (DEX+3), Boots. Cards (top): Archer Skeleton
  (weapon, +10% ranged), Raydric, Matyr, Zerom (accessory, DEX+3).
- **Special items:** arrows - Arrow, Silver Arrow (vs undead), Fire Arrow (vs earth/plants), Crystal Arrow, Immaterial
  Arrow; elemental arrows matched to the map. **Falcon** (rented by legion.txt for 2,500z on login/level-up for a char
  with Falconry Mastery and no falcon) - live since the rAthena restart: DB 2026-11-14 12:20 (run 5) 33 of 115 Hunters
  with Falconry Mastery carry a falcon; the rest are short of the 2,500z (poverty, economy item).
- **Boss role:** ranged DPS: Double Strafe on the boss with element arrows, Arrow Shower on the adds, falcon procs.
- **Hit and run** (owner 2026-11-14, DS Hunters are the best hunters; `kite.go`, every bow user):
  when the target or a monster that hit it in the last 2 s stands within 2 cells, the archer steps 4-6 cells straight
  away (any free direction when blocked) and shoots again; Ankle Snare on the chaser's cell when it carries a Trap
  (1065). Not with 3+ monsters on it (escape logic), not while a party tank holds the monster. Guild farmers
  (economy agent) come from these solo DS Hunters.
- **Do:** keep arrows stocked; fight from range. **Don't:** run out of arrows (with 0 arrows a bow user buys up to
  the 50% weight line; "no arrows" outranks "out of potions").

### Variant: "DEX/INT Blitz Hunter" (plan `hunter_blitz`, Base hunter)
- DEX/INT (DEX 99, INT 70, LUK 20, VIT 30): Renewal Blitz Beat scales with INT/DEX. Archer basics | Beast Bane 1,
  Falconry 1, Blitz Beat 5, Steel Crow 10, Beast Bane 10, Arrow Shower 10, Owl's Eye 10, then traps. Bow route, caster
  extras (headgear/garment). Needs the falcon (legion.txt rental).

### Bard: "DEX/INT Bard" (plan `bard`, male, end goal Clown)
- **Party role:** solo, a Double Strafe archer; in a party, a support singer.
- **Stats:** DEX 20, AGI 10 -> DEX 40, AGI 25, VIT 15 -> DEX 60, AGI 40, VIT 25, INT 20 -> DEX 80, AGI 55, VIT 40,
  INT 40 -> DEX 99.
- **Skills (order):** Archer basics (Double Strafe 10, Owl's Eye, Vulture's Eye 10, Improve Concentration 10, Arrow
  Shower 5, Owl's Eye 10) | Bard: Amp (Adaptation) 1, Encore 1, Music Lessons 1, Unchained Serenade (Dissonance) 3,
  Magic Strings (A Poem of Bragi) 10, Impressive Riff (Assassin Cross of Sunset) 10, Perfect Tablature (A Whistle) 10,
  Song of Lutie (Apple of Idun) 10, Music Lessons 10, Melody Strike (Musical Strike) 5.
- **Use (song.go, build review run 3):** in a party, with a song learned, the instrument/whip outranks the bow
  (itemScore 200+) and stays in hand; solo the bow wins. While the party fights (leader within 8 cells, SP >= 30% and
  >= 60) it plays: Apple of Idun if a member is under 60% HP, Bragi if the party has a caster/healer, else Assassin
  Cross, Whistle. While playing (180 s; normal attacks are blocked by rAthena) it follows the leader and uses Musical
  Strike (instrument + arrows) on monsters hitting the party. Never swaps weapons mid-song (unequipping ends it). A
  server refusal (0x0110) or SP < 3 ends the song.
- **Gear:** the Archer bow route above (same bands); for songs a Violin (4,000z, Prontera / Comodo weapon shop) -
  bought on an errand (errand.go "brb, buying a Violin for my songs") when it has a song and the price + 500z.
  Run 4 (QA: 0 song casts, all 7 partied singers held only a Bow): for a partied singer the instrument / whip is its
  class step (builds.go classWeaponNeed -> song.go songWeaponNeed): a party leader's run goes to a town that sells it
  and bestGearIn buys it first; until it owns one, every other gear purchase waits (gearHold: songSaving), so the
  zeny is saved for it. In a party it holds the instrument (itemScore 200+), solo the bow.
- **Boss role:** support songs (Bragi for the casters/priest, Assassin Cross for the melee, Apple of Idun when hurt).
- DB 2026-11-14: 26 Bards avg lv59 but job 18-29 - only Moon.fang has a song (Bragi 2).

### Dancer: "DEX/INT Dancer" (plan `dancer`, female, end goal Gypsy)
- **Party role / stats:** as the Bard.
- **Skills (order):** Archer basics | Dancer: Amp 1, Encore 1, Dance Lessons 1, Hip Shaker (Ugly Dance) 3, Gypsy's Kiss
  (Service for You) 10, Focus Ballet (Humming) 10, Slow Grace (Please Don't Forget Me) 10, Lady Luck (Fortune's Kiss)
  10, Dance Lessons 10, Slinging Arrow (Throw Arrow) 5.
- **Use:** dances with a whip in hand in a party (song.go): Please Don't Forget Me on 3+ monsters near it, Service for
  You with a caster/healer, else Humming, Fortune's Kiss; Throw Arrow (whip + arrows) while dancing; solo the bow build.
- **Gear:** the Archer bow route; Rope (2,500z, Prontera) for dances, bought on an errand. **Boss role:** support
  dances (Service for You for the casters, Please Don't Forget Me on the adds).

### Engine notes (Archer line)
- `fightSkills` runs for bow users at range (it used to stop at 3 cells, so ranged classes never buffed); any
  "in the fight" check with a 3-cell radius silently disables ranged classes.
- Arrows: arrowTarget = min(3000, 1000 + lv x 30), top-up below a quarter, paid from all zeny after the potion minimum.
- A gear-pool turn item is dropped while a member lacks its class weapon / arrows.

