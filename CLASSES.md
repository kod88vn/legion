# Classes and builds (owner's class/build instructions)

The reference for every class line: role in a party, stat build, skill plan, skill use in fights, gear path, special
items, boss role and do/don't behaviour. Every AI (engineering sessions, fixers, the build-and-strategy agent, the
LLM minds) follows it together with `RULES.md`, which keeps the general party, economy and safety rules.

**Recording rule:** class- or build-specific owner rules go in this file (in the right class section, or in "All
classes" when they apply to every class), with the date and the owner's words where possible, in the same turn they
are stated. If a new rule replaces an old one, edit the old line instead of keeping both. **The build-and-strategy
agent keeps this file current**: when it reviews a class (builds, skill use, gear, boss tactics) it adds its per-class
notes under "Engine notes" in that class's section.

Sources: owner rules moved here from `RULES.md` on 2026-11-14 (verbatim, marked **Owner rule**), the plans in
`legion/builds.go` (stats and skill order; `Legacy`/`LegacyStats` keep older plans valid), the gear table
`legion/data/buildgear.json` (bands by base level, weakest first; the economy reads it every 5 min), and the
build-and-strategy agent's review (`HANDOVER.md` 6f2/6f3, `legion/data/lessons.md`). Skill names below are the
rAthena names from the plans, with the classic name in brackets where they differ.

---------------------------------------------------------------------------------------------------------------------
## All classes

### Owner rules (moved from RULES.md, verbatim)
- **Every class uses its AoE skills on crowds** (2026-11-14: "all skills should use AOE skills in crowds, targeting
  the central mob"): any class with an area skill (Magnum Break, Bowling Bash, Brandish Spear, Arrow Shower, Grand
  Cross, Hammer Fall, Cart Revolution, Meteor Assault, Heaven's Drive, Storm Gust, ...) casts it when monsters are
  grouped, aimed at the monster in the middle of the group.
- **Tanks are active**: they pull, take aggro off others, and deal damage. They never sit back. (2026-11-14: "make the
  tanks in party be more active and attacking the mobs more aggressively instead of sitting behind": the tank leads
  the fight - it engages first, picks up every monster that targets a party member, and keeps attacking.)
- **Tanks fight a crowd with AoE instead of escaping** (2026-11-14: "knight can pull but must use AOE instead of
  escaping"): a Knight/tank may pull big groups; when surrounded it uses its area skills (Bowling Bash, Magnum Break,
  Brandish Spear, Grand Cross) and potions to clear them, with the priest healing - it does not Fly Wing away from
  the party except as a last resort near death.

- **All build varieties, each with its matching gear; every bot works to perfect its build** (2026-11-14: "make sure
  we have all varieties of builds with matching gears. Each bot should prioritize farming or trading to perfect its
  build"): every class has its common build variants (e.g. Knight 2H-sword and spear, Priest full-support and battle,
  Wizard AoE and bolt, Hunter trap/DEX, Assassin katar and dagger, Blacksmith battle and forge, Monk combo and Asura,
  Sage battle and support...), spread across the bots, each with its own gear table in buildgear.json. A bot's goal
  is to complete its build: it farms the drops and zeny for its next gear piece, or trades for it, as its priority.

### Related general rules (they stay in RULES.md)
- Builds are real: level 1 Novice, a build, a levelling plan and a gear path to 99; **wrong build = delete and start
  over** (bot accounts only) - RULES.md 2.
- The server caps at transcendent classes (Novice -> 1st -> 2nd -> High Novice -> high 1st -> trans 2nd; no 3rd jobs) -
  RULES.md 2. The "end goal" of each plan below is its transcendent class.
- "these build and spell use are common sense": the build-and-strategy agent compares every class with good online
  Renewal guides and improves the engine - RULES.md 0.
- Bots keep upgrading their gear (buy, farm, trade, refine) - RULES.md 2.
- SP users stock SP potions; healers carry only a few HP potions - RULES.md 2 "SP users stock SP potions too".
- Every bot stocks basic utilities (HP/SP potions, Fly Wings, Butterfly Wings, cures/arrows as its class needs) -
  RULES.md 2.
- A member may leave the party briefly for a necessary build/class step (job change, Peco/falcon rental, class weapon
  or arrows, quest skill) - RULES.md 3.
- Boss hunts only when every member is completely built out with high-level gear - RULES.md 2.

### Build variants (owner 2026-11-14: "make sure we have all varieties of builds with matching gears")
- builds.go: a variant is its own plan with `Base: "<main plan>"` (same class, 1st job and branch), its own stat/skill
  order and its own buildgear.json table (falls back to the main plan's). planForClass gives new characters the main
  plan 3 times in 5 and a variant otherwise; existing characters keep their plan. Code that asks "which build" uses
  `p.base()` (gear style, alchemy, buildgear); planWeaponSubs and trans lists fall back to the main plan
  (`transFor` keeps only the transcendent skills). tools/wrongbuilds.py maps variants to their base.
- Step 1 (built): knight_spear, crusader_gc, hunter_blitz, priest_me. Run 4 (built 2026-11-14): sage_support,
  monk_asura, alchemist_battle, alchemist_potion, blacksmith_mace, assassin_sonic, assassin_dagger, rogue_bow (sections
  below each class). Decided against: a Wizard bolt variant (owner rule: Mages/Wizards are AoE builds - the main plan
  already keeps bolts as support behind the area spells); a forge Blacksmith (no forging AI); separate Bard/Dancer
  variants (bow + singer already in one plan). Every plan is checked against the server's skill tree (prerequisites,
  max levels) and `tools/wrongbuilds.py audit` stays at 0 wrong.
- **Step 2, "perfect the build" (built, run 4):** `nextBuildPiece` (buildgear.go) = the one item a bot works toward:
  the best real upgrade on its build's table for its band that it can wear and doesn't own (weapon gains x3, body
  armor x2). Shop pieces: the zeny is the goal; drop-only pieces carry the best farming maps (normal monsters up to
  10 levels above the bot, drop rate x spawn count); neither = trade. `GET /api/buildgear/next[?bot=Name]` (with a
  can_afford flag and a count by source). The economy side (farm.go / wants) decides farming, buying or #trade.

### Engine notes (all classes)
- Plans must stay prefix-compatible: old skill goals go to `Build.Legacy`, old stat steps to `Build.LegacyStats`, so
  `tools/wrongbuilds.py` never flags existing characters. The planner raises the first unmet goal, so order-only
  differences converge by themselves.
- A skill in the plan is not a skill in use: each learned skill needs a matching rule in fight() (`crowdSkills` in
  weaponskill.go, `aoe.go` for the mage line, selfBuffs, healAttack). Check traces per class (`tools/measure/sk.sh`).
- Crowd skills: `crowdSkills` (weaponskill.go) casts the first learned, affordable skill whose best cluster holds 3+
  monsters (Magnus Exorcismus 4+), aimed at the monster at the centre, inside the skill's real splash radius. No group
  with a monster someone outside the party fights; solo, no boss/champion group. Magnum Break (ai.go, tanks: 2+
  around), Hammer Fall and Grand Cross (skillbuff.go) keep their own rules.
- "key" self-buffs (`selfBuff.key`: Two-Hand Quicken, Adrenaline Rush, Improve Concentration, Auto Guard) are cast down
  to a 30% SP floor; other buffs only above 60% SP. Run 5 (2026-11-14): each key buff has its real SP cost per level
  (THQ 14-50, Adrenaline Rush 20-32, Auto Guard 12-30, Improve Concentration 25-70), so the highest level that fits
  gets cast instead of none. While one is due, attack skills, crowd skills and Magnum Break keep its lv1 cost on
  top of their own floor (`keyReserve`), and Hammer Fall waits (`keyBuffDue`). Trace before the fix: Knights cast
  Bowling Bash / Provoke at 27-36% SP and never THQ; a Blacksmith cast Hammer Fall 29x in 5 min and never Adrenaline Rush.
- Plants and mushrooms (no BaseExp, HP <= 20, `Mob.worthless`) are never targets.
- **Run 7 (2026-11-14):** melee skills (range <= 3) go out only when the bot and its target both stand, measured from
  the bot's real position (myPos) - rAthena drops an out-of-range skill without a packet; Hammer Fall is range 1;
  area spells pick cells from myPos. Champions (Ringleader/Swift/...) are a threat only under their level + 5 of the
  bot's champion level (solo >= 3/4 base, with a party mate base, tank+healer base+5); one that drops the bot's goal
  item and that it outclasses is targeted first. Gear scoring counts the item script's stat bonuses for the build
  (accessories/headgear), an empty accessory/headgear/garment/shoe slot is filled cheaply, Magnifiers (2, 40z) are
  standing stock and empty-slot unidentified pieces are identified first, cheap race/element cards (<10k) are slotted
  into free slots, Combo Finish only at a living target; a restock ends at most at 40% weight.
- **Can-act delay (run 6, skillaudit.go):** rAthena refuses a skill sent before the character's canact_tick and hides
  the refusal (SKILLINTERVAL needs @showdelay). Every normal swing sets it to amotion, every skill to its cast time
  and then the db AfterCastActDelay (min 100 ms). The engine predicts it from our own 08c8 (attackMT), 01de / 011a /
  0117 answers and 07fb, and holds b.skillReady until then. `/api/skillaudit` counts sent / answered / refused /
  silent / early per skill; tuning `skill_canact_off` switches the hold off for a comparison.
- Tanks (tanklead.go): `tankTakeAggro` (Provoke or hit any monster targeting a member) runs before the tank's own
  target; the tank pulls around the leader / the group's front (pull radius 12, never beyond 16 from the leader),
  walks at the front when there is nothing to pull, and doesn't sit for its own rest while a member fights nearby
  (HP >= 30%).
- Melee walks into reach itself (ai.go `closeIn`): rAthena never chases for a player.
- Build-aware gear scores (builds.go gearStyle): casters + support priest value MATK, the archer line bows, melee ATK;
  planWeaponSubs (knight 2hSword, crusader 1hSword; variants: knight_spear spears, assassin_sonic katar,
  assassin_dagger dagger, rogue_bow bow, blacksmith_mace mace) lower other weapon types; rogue_bow has the bow style.
- Common cards in every gear band: Raydric (garment, -20% Neutral), Matyr (shoes, MaxHP+10% AGI+1).

---------------------------------------------------------------------------------------------------------------------
### Teleport card for everyone (owner 2026-11-14, RULES.md 3b; built run 5)
- **Run 6 fix (QA 13:50: 7 Creamy Cards uncompounded, 0 Clips on the server):** Clip [1] is NOT sold by any loaded
  NPC shop and drops only from a lone Dragon Fly / 0.05% Alarms - unobtainable. The accessory step is now **Belt [1]
  (2627, NPC 20,000z, lv25, all jobs, Prontera south gate Armor Merchant + 9 towns)** in every buildgear.json band
  (teleslot.go). A bot with a Creamy in the bag and no free-slot accessory buys a Belt [1] first (nextGearBudget +800)
  and saves for it (gearHold); creamyWant bids for the card only when a free-slot accessory is owned or the purse
  covers card + Belt; cardThink compounds the Creamy into an unworn slotted accessory too and wears it; the accessory
  carrying the Creamy is never swapped out. Teleport-to-hunt now writes an event ("teleport skill: ...") for QA.
- (old text) Every build's gear path has a Creamy Card (accessory, Teleport lv1) in a slotted accessory: Clip [1] (2607) was
  the first accessory step in every buildgear.json band and in extrasMelee/Cast/Holy; the card is on every band's card
  list (prio 3) and `cardValue` rates it 60 for every build, so bots ask #trade for it, keep it and compound it.
- Use (`teleport.go`, for anyone with AL_TELEPORT - the card or the Acolyte skill): escape a losing fight with the skill
  before a Fly Wing (danger.go, same rules); a solo hunter with nothing to fight for 6 s on its hunting map teleports
  to a random cell instead of roaming (not in a party: it would split it; keeps 25% SP for an escape). The skill's warp
  list (0x0abe) is answered "Random" (0x011b); a map where the position didn't change is skipped for 30 min.

### Build variety across the Legion (2026-11-14)
- **The Legion's builds must be varied** (owner: "the builds of the legions still lack variety"): bots of the same
  class spread over all the sensible Renewal builds for that class (e.g. Knight: 2H/Bowling Bash, Spear/Pierce,
  Brandish Spear, Peco lancer; Wizard: AoE INT/DEX, Fire/Meteor, Frost/Storm Gust, battle-sage style for Sages;
  Hunter: trapper/Blitz falconer/DS; Assassin: katar crit, Sonic Blow, Grimtooth, dual dagger, EDP later; Priest:
  full support, battle/Magnus exorcist; Monk: combo, Asura, tank-ish; Blacksmith: forger, battle (Mace/Axe);
  Crusader: Grand Cross/holy, shield/devotion tank, spear; Rogue: bow, dagger strip, Plagiarism; Bard/Dancer: song
  support vs. arrow DPS; Alchemist: potion maker, battle, summoner/Acid Demonstration), with matching gear paths -
  a realistic spread like a live server, not everyone on the same template.
- **Run 5 (build-and-strategy agent, 2026-11-14 12:55):** measured first (`python tools/variety.py measure`): all
  5,986 Legion characters were on their line's main plan (100% each; the run-4 variants went only to new characters
  and none had been made). Added 9 variants (sections below: knight_pierce, crusader_spear, wizard_fire, wizard_frost,
  sage_hindsight, hunter_agi, priest_battle, bard_arrow, dancer_arrow), so every line has 2-3 builds. New characters:
  main plan 40% / variants 60% (one variant: 50/50; builds.go pickVariant); support characters get priest or priest_me,
  vendors any Merchant-line variant (persona.go seed). Existing characters: `tools/variety.py assign --apply` moved
  2,677 characters whose current skills AND stats already fit the variant (wrongbuilds rules + 2nd-job caps; nobody
  deleted, never Ayla's party, online characters left alone) - mostly Novices / low levels; lv40+ characters are
  almost all built too far into their main plan to switch. After: main plan 40-73% per line (knight 71, rogue 73,
  monk 67, priest 63 = limited by compatible characters), see `legion/data/variety.json`. After a restart run
  `tools/wrongbuilds.py audit` then `tools/variety.py revert-wrong` (a character that spent points under its old plan
  before the restart goes back to it).
- **Natural resets (RULES.md 3 "Natural build resets", built run 6, reset.go):** the server's only reset service is
  the official Hypnotist in Izlude (127,175; npc/re/other/resetskill.txt): free stat and skill resets for 1st-class
  characters carrying weight 0 and no Pushcart (2nd classes have no reset here: official ones are cash items, so their
  builds stay). A 1st-class bot (lv30+, main role, on its line's main plan, not a Merchant, not partied, not a guild
  leader, never the core instance) rolls 1 in 20 every 15 min, behind a server-wide gate of one start per 30-60 min and
  24 a day; it picks the least represented variant of its line, says so ("finally resetting to a ... build, wish me
  luck"), goes to Izlude, takes its gear off, stores everything at the Izlude Kafra (128,148), has stats then skills
  reset, switches plan (memory + personas.json), takes its things back and rebuilds; points are held until the switch.
  Anything failing after the storage step fetches the items first; a reset left at logout is in resets_pending.json
  and resumes with the fetch at the next login. Events "reset: ..." count them.
- Run 6 (2026-11-14 13:20): trap Hunter, strip Rogue and Steel Body / Occult tank Monk added with their AI (below).
  Still not added: forging Blacksmith (forging = materials + Anvil + the production list: an economy project),
  Plagiarism Rogue (a copied skill has no fight rule).

### Run 6 variants (2026-11-14; variantskill.go, trap.go)
- **hunter_trap** "DEX/INT Trapper Hunter": DEX 99 / INT 70 / VIT 35 / AGI 25; Archer part as the main plan | Skid Trap
  1, Land Mine 5, Ankle Snare 1, Flasher / Freezing Trap / Sandman 1, Blast Mine 5, Shockwave Trap 1, Claymore Trap 5,
  then the falcon (Beast Bane, Falconry, Blitz Beat 5, Steel Crow 10, Beast Bane 10), Ankle Snare 3. Renewal trap damage
  grows with DEX / INT / base level. **Use (trap.go trapCast, after kiteStep):** Claymore Trap (2 Traps, 5x5 fire) or
  Land Mine (1 Trap) on the cell a monster is walking to, within 3 cells, when 2+ monsters are around it or it is tough
  (strongMob / HP >= 20 x level); one trap per 2.5 s, SP floor kept; hit-and-run kiting as every bow user. Any character
  with a trap skill and Traps does the same. **Supply:** Trap (1065, 50z) is sold only by the Izlude / Prontera south
  gate Goods Merchant / Hugel ... Tool Dealers: every shop visit that lists Traps tops a trapper up to 40 + level (max
  120), and a solo trapper under 10 Traps with zeny for 30 makes a supply trip to the nearest seller (every 30 min max).
  Open: a partied trapper has no Trap errand yet (errand.go), it restocks on party runs.
- **rogue_strip** "DEX/STR Divest Rogue": DEX 90 / STR 80 / AGI 50 / VIT 40; Thief part as the main plan | Gank 4, Mug 4,
  Divest Helm / Shield / Armor / Weapon 5 (the strip line; success grows with DEX), Back Stab 10, Stalk 2, Raid 5,
  Gangster's Paradise 1, Snatch 3; daggers / swords. **Use (stripTarget, every Rogue/Stalker with the skills):** on a
  boss, champion, monster at/above its level or 15k+ HP, in melee: Divest Weapon (-25% ATK: the party takes less) then
  Divest Shield (-15% DEF: everyone hits harder), at most 2 tries per skill per monster, above the SP floor; never trash.
- **monk_occult** "VIT/STR Occult Tank Monk": STR 90 / VIT 80 / DEX 50 / INT 35; Acolyte part as the combo Monk | Iron
  Fists 5, Spirit Spheres 5, Occult Impaction 5, Dodge 5, Triple Attack 5, Chain Combo 3, Combo Finish 3 (prerequisite),
  Mental Strength (Steel Body) 5, Iron Fists 10, Absorb 1, Root 2, Spiritual Cadence 5. **Use (occultFight):** Occult
  Impaction (1 sphere) on armoured targets (DEF >= 20 or strong), auto-attack on soft trash; Mental Strength (5 spheres,
  200 SP, DEF/MDEF 90) when 3+ monsters attack it or a boss/champion is next to it; no skills while it lasts. Call
  Spirits is a key skill for it (spheresKey).

### Run 5 variants (2026-11-14)
- **knight_pierce** "AGI/STR Pierce Lancer Knight": STR 90 / AGI 70 / VIT 60 / DEX 40; Swordman Bash 10, Provoke 10,
  Magnum 5, HP Recovery 10 | Spear Mastery 10, Riding, Pierce 5, Cavalier 5, Pierce 10, Spear Boomerang 5, Spear Stab
  3, Brandish Spear 10. One-hand spear + shield (Javelin / Spear / Pike -> Guisarme / Glaive). Pierce is in the Swordman
  offense list (progress.go, spear only), Brandish Spear on crowds (crowdSkills).
- **crusader_spear** "STR/VIT Spear Crusader": STR 90 / VIT 70 / DEX 50 / AGI 30; Swordman as the main plan | Spear
  Mastery 10, Spear Quicken 10 (key self-buff with a two-handed spear, skillbuff.go), Riding / Cavalier 5, Faith 10,
  Holy Cross 10, Grand Cross 5. Two-handed spear (Guisarme / Glaive / Trident), no shield.
- **wizard_fire** "INT/DEX Fire Wizard": Fire Bolt 10 / Fire Ball 10 / Fire Wall 10 | Sightrasher 2, Meteor Storm 10,
  Heaven's Drive 5, Quagmire, Lord of Vermilion. **wizard_frost** "INT/DEX Frost Wizard": Cold Bolt 10 / Frost Diver 10
  / Thunderstorm 10 | Jupitel 3, Storm Gust 10, Lord of Vermilion 10, Heaven's Drive, Quagmire. Both AoE builds
  (owner rule); aoe.go casts whatever area spells are learned, element-weighted.
- **sage_hindsight** "INT/AGI Hindsight Sage": INT 90 / DEX 70 / AGI 60 / STR 30; bolts 10 + Soul Strike 3 | Study,
  Free Cast 4, Hindsight 10, Earth Spike / Heaven's Drive (they join the Hindsight list at 6 / 9). Fights in melee with
  a book (bestAttack skips ranged bolts, spCaster false); Hindsight is a key self-buff and bot.go answers the
  server's spell list (0x0afb) with the spell learned at the highest level (0x01ce).
- **hunter_agi** "AGI/DEX Auto-Attack Hunter": AGI 90 / DEX 85; the Hunter kit with Owl's Eye 10 / Improve
  Concentration first; fast bow auto-attacks, Double Strafe on tough targets.
- **priest_battle** "STR/INT Battle Priest" (main-role characters only): STR 90 / INT 70 / VIT 60 / DEX 40; Heal 3,
  Divine Protection / Demon Bane 10, Increase AGI 10, Blessing | Mace Mastery 10, Impositio 5 (self-buff), Kyrie 5,
  Magnificat 3, Lex Divina 5, Heal 10. Mace + shield; still heals its party.
- **bard_arrow / dancer_arrow** "DEX/AGI Arrow Bard / Dancer": the full Archer kit, then Music / Dance Lessons and the
  ASPD songs for later; keep the bow in a party too (song.go songSupport is false for them).

## Transcendent classes (the server cap; 2026-11-14)
- **Owner rule** (2026-11-14: "cap server at trans class", then "you go and reconfigure the server"): the transcendent
  2nd job is the last class on the server, for everyone. Server side the 3rd-job, Expanded Super Novice and 4th-job
  quest NPCs are unloaded (`rathena/npc-custom/scripts_custom.conf`, HANDOVER 6f4). Never plan or advise 3rd jobs.
- **Path:** 2nd job at base 99 / job 50 with 1,285,000z -> rebirth (High Novice, base 1, 100 status points; legion.txt
  stands in for the Sage Castle / Valkyrie quest at the stock price) -> High 1st job at job 10 -> trans 2nd job at
  job 40 (same branch). Trans caps: base 99, job 70 (69 skill points: ~20 more than the 2nd job, for trans skills).
- **Stats:** the same plan as the 2nd job, from scratch (stat cap stays 99).
- **Skills** (`legion/transplans.go`; the High 1st job follows Build.Skills like the 1st job did; once trans, these come
  first, then the rest of Build.Skills). Prefix-compatible: Build.Skills is untouched.
  - Lord Knight: Two-Hand Sword Mastery 10, Two-Hand Quicken 10, Bowling Bash 10, Magnum Break 5, Aura Blade 5, Frenzy
    (Berserk) 1, Provoke 5, Parrying 10, HP Recovery 5, Riding 1, Spear Mastery 5, Concentration 5, then Pierce 5 / Spear
    Stab 5 / Spiral Pierce 5 (spear option).
  - Paladin: Faith 10, Holy Cross 10, Guard 10, Smite 2, Shield Boomerang 5, Shield Chain 5, Endure 5, Gloria Domini
    (Pressure) 5, Devotion 3, Martyr's Reckoning 5, Divine Protection 3, Demon Bane 5, Battle Chant (Gospel) 10.
  - High Wizard: Heaven's Drive 5, Storm Gust 10, Mystical Amplification 5, Lord of Vermilion 10, Meteor Storm 10,
    Mystical Amplification 10, SP Recovery 5, Soul Strike 7, Soul Drain 10, Napalm Beat 5, Napalm Vulcan 5.
  - Professor: Earth Spike 5, Heaven's Drive 5, Free Cast 10, Hindsight 1 (prerequisite), Double Casting 5, SP Recovery 3,
    Magic Rod 3, Cast Cancel 5, Dispell 3, Soul Siphon 2, Mind Breaker 5, Indulge 5, Soul Siphon 5.
  - Sniper: Blitz Beat 5, Steel Crow 10, Arrow Shower 10, Improve Concentration 10, Focused Arrow Strike (Sharp
    Shooting) 5, Falcon Assault 5, Owl's Eye 10, Vulture's Eye 10, Falcon Eyes (True Sight) 10, Wind Walker 5.
  - Clown / Gypsy: Music / Dance Lessons 10, Dissonance / Ugly Dance 3, Bragi / Service for You 10, Assassin Cross /
    Humming 10, Musical Strike / Throw Arrow 1, Vulcan Arrow 10, Apple of Idun / Fortune's Kiss 10, Tarot Card 5,
    Marionette Control 1.
  - High Priest: Heal 10, Kyrie 10, Magnificat 5, Impositio 3, SP Recovery 5, Aspersio 3, Lex Divina 5, Meditatio 10,
    Angelus 1, Assumptio 5, Gloria 2, Basilica 5.
  - Champion: Iron Fists 7, Spirit Spheres 5, Triple Attack 5, Chain Combo 5, Combo Finish 5, Triple Attack 10, Glacier
    Fist (Tiger Fist) 5, Chain Crush Combo 10, Fury 5, Zen 1, Raging Palm Strike 5.
  - Whitesmith: Adrenaline Rush 5, Power-Thrust 5, Weapon Perfection 5, Maximum Power Thrust 5, Skin Tempering 3, Hilt
    Binding 1, Weaponry Research 5, Shattering Strike (Meltdown) 10, Pushcart 5, Mammonite 10, Hammer Fall 5, Cart Boost
    1, Cart Termination 10 (Cart Boost needs the quest skills Cart Revolution + Change Cart: no quest-skill errand yet,
    so it stays unlearned until one exists).
  - Creator: Axe Mastery 10, Bomb 5, Acid Terror 5, Acid Demonstration 10, Aid Potion 5, Aid Condensed Potion 5.
  - Assassin Cross: Katar Mastery 10, Sonic Blow 10, Advanced Katar Mastery 5, Envenom 10, Detoxify 1, Enchant Poison 6,
    Create Deadly Poison 1, Enchant Deadly Poison 5, Double Attack 5, Cloaking 3, Soul Destroyer 5, Right Hand Mastery
    3, Meteor Assault 5, Soul Destroyer 10, Meteor Assault 10.
  - Stalker: Back Stab 10, Raid 5, Counter Instinct (Reject Sword) 5, Divest Weapon 5, Full Strip (Divest All) 5,
    Plagiarism 10, Preserve 1.
- **Skill use (run 4, `legion/transfight.go`, built 2026-11-14):** Lord Knight Aura Blade (key buff, 30% SP floor) and
  Frenzy (only vs a boss / champion, HP >= 90%, a party healer within 12 cells); High Wizard Mystical Amplification
  right before an area spell (3+ grouped around the target); Professor Double Casting kept up above 50% SP; Assassin
  Cross Soul Destroyer first in the Thief attack list (range 4); High Priest Assumptio on the Kyrie targets (tank
  first, party.go). Not wired yet: Acid Demonstration, Vulcan Arrow, Meteor Assault, Falcon Assault / Focused Arrow
  Strike, Raging Palm / Glacier Fist, Cart Termination, Full Strip, Gospel / Basilica. No bot near rebirth (max lv 80).

---------------------------------------------------------------------------------------------------------------------
## Novice
- **Role:** none yet; levels to Job 10 and changes job (`rathena/npc-custom/legion.txt` does the change; the plan id
  picks the 1st job and the branch).
- **Skills:** Basic Skill 9 first in every plan (`noviceSkills`).
- **Behaviour:** beginners stay near their home town, level first, earn money for gear and Kafra travel (RULES.md 2).
  No gear trips before base level 10. Starter kit: Novice Potions, Knife, Cotton Shirt (junk once replaced).
- **Don't:** wander across the world; walk to far towns for shops.

---------------------------------------------------------------------------------------------------------------------
## Swordman -> Knight / Crusader (tank line)

- **Knights and Crusaders ride a Peco Peco - top priority** (2026-11-14, owner: "the knights are still not riding peco
  which is annoying. Why is it not a priority?"): every Knight/Crusader with Peco Peco Riding rents its Peco as its
  first purchase, before any other gear; guild/party funds help a member who can't afford it; it stays mounted
  (re-rents after death/dismount). Unmounted riders are a bug.
  Owner, 2026-11-14: "knights must follow the official path to get peco as soon as possible. Any party member can leave
  temporarily to advance its build/class": learn Peco Peco Riding on the official skill path (Endure ->
  Riding) as early as the plan allows and rent the Peco right away (no waiting for Cavalier Mastery); a partied
  Knight/Crusader leaves the party briefly for the rental errand and comes back.

### Owner rules
- Tanks are active / fight a crowd with AoE instead of escaping (see "All classes").
- Knights ride a Peco (2026-11-14, owner: "Knights ride"; Cavalier Mastery is no longer required to rent).

### Knight: "Two-Hand Sword Knight" (plan `knight`, end goal Lord Knight)
- **Party role:** main tank (or melee DPS): pulls, Provokes, takes aggro off everyone, clears crowds with Bowling Bash.
- **Stats:** STR 20, AGI 20, VIT 20, DEX 15 -> STR 40, AGI 40, VIT 35, DEX 25 -> STR 60, AGI 60, VIT 50 -> STR 80,
  AGI 75, DEX 40, VIT 60 -> STR 90, AGI 85.
- **Skills (order):** Sword Mastery 1, Bash 10, Two-Handed Sword Mastery 10, Magnum Break 5, Provoke 5, Endure 1,
  Increase HP Recovery 7 | Knight: Two-Hand Quicken 10, Peco Peco Riding 1, Cavalier Mastery 5 (run 4: moved up from
  after Bowling Bash - only 24 of 105 Knights had Riding at a median job 19), Counter Attack (Auto Counter) 5, Bowling
  Bash 10, Magnum Break 10, Increase HP Recovery 10, Endure 10 (Renewal: +MDEF). Old order in `Legacy`.
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
  behind the party, sit while members fight, Fly Wing away except as a last resort near death.

### Crusader: "VIT Holy Cross Crusader" (plan `crusader`, end goal Paladin)
- **Party role:** main tank with a shield; holy damage vs undead/demons.
- **Stats:** VIT 20, STR 20, DEX 15, INT 10 -> VIT 40, STR 40, DEX 30, INT 25 -> VIT 60, STR 60, DEX 45 -> VIT 80,
  STR 75, DEX 55, INT 40.
- **Skills (order):** Bash 10, Provoke 5, Sword Mastery 10, Endure 1, Increase HP Recovery 10, Magnum Break 3 |
  Crusader: Faith 7, Holy Cross 10, Peco Peco Riding 1, Cavalier Mastery 5 (run 4: moved up - 4 of 81 Crusaders had
  Riding), Faith 10, Guard (Auto Guard) 10, Smite (Shield Charge) 3, Shield Boomerang 5, Shield Reflect (Reflect
  Shield) 5, Grand Cross 4, Sacrifice (Devotion) 1.
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

---------------------------------------------------------------------------------------------------------------------
## Mage -> Wizard / Sage (caster line)

### Owner rules (moved from RULES.md, verbatim)
- **Mages and Wizards are AoE builds** (2026-11-14: "mage/wiz build should focus on AOE spells and use it when
  detecting more than 2 mobs together"): their skill plans focus on area spells, and they cast them whenever 3 or more
  monsters are grouped together. Existing Mages built on bolts are NOT wrong builds: they keep their levels and
  grow into AoE (every new skill point goes to area spells; owner's choice, 2026-11-14).
- **AoE at every chance, aimed where it hits most** (2026-11-14: "I see a big crowd and the wiz not doing anything. It
  should do AOE spell at every chance"; "make sure the wiz know to cast AOE spell at the center where it's most
  impactful"): a Wizard/Mage casts an area spell at every chance a group forms, placed on the cell that covers the
  most monsters (the centre of the crowd), never idles or single-targets in a crowd.
- **Parties with a Wizard play around it** (2026-11-14: "if party has a wiz, they should follow that strategy, lure
  for wiz to cast spell and pick off the rest"): the tank/melee lure and gather monsters into a tight group in the
  Wizard's range, the Wizard drops its AoE on the centre, and the others pick off the survivors. Applies to every
  party with a Wizard (Ayla's included).

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
- **Skills (order):** run 6 (owner rule "Mages are AoE builds"; QA 13:10: Sage-plan Mages lv40-50 had bolts 10/10/10
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

---------------------------------------------------------------------------------------------------------------------
## Archer -> Hunter / Bard / Dancer (bow line)

### Owner rules
- Arrows are part of the class's basic utilities (RULES.md 2 "Town runs use a Butterfly Wing ... cures/arrows as its
  class needs"); buying arrows is a necessary errand that may take a member away briefly (RULES.md 3).
- Every class uses AoE on crowds: Arrow Shower on the central monster (see "All classes").

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
- **Hit and run** (owner 2026-11-14: "DS hunters are the best hunters with hit and run"; `kite.go`, every bow user):
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

---------------------------------------------------------------------------------------------------------------------
## Acolyte -> Priest / Monk (support line)

### Owner rules (moved from RULES.md, verbatim)
- **Priests heal first and sit when idle** (2026-11-13): SP is kept for healing (buffs only above 60% SP); when
  nobody needs healing, the priest sits - it prioritises sitting over fighting (2026-11-14: "priest should
  prioritize sitting whenever possible instead of fight. If it's not moving or blessing, it sits"): heal, buff or
  move with the party, otherwise sit. This idle sitting does NOT stop the party: only an announced rest (SP really
  low, "need sp" / "ready") makes the others wait (2026-11-14: "has plenty of sp now. Why is it still resting all
  the time").
- **Acolyte/Priest builds max out INT** (2026-11-14: "acolyte build seems wrong. It should max out intelligence"):
  INT first (to the max), then DEX/VIT as support; existing characters grow into it with their new stat points.

### Related general rules (stay in RULES.md)
- Healers (acolyte line) heal themselves: they carry only a few HP potions (emergency) and mostly SP potions (RULES.md 2).
- Ayla's party keeps its priest when other members rotate through jobs (RULES.md 5).

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
- **Rest:** sits under `priest_sit_low_sp_pct` (40%), rests to 80% (60% when the party moves); idle sitting otherwise
  (heal, buff, move, or sit) without holding the party.
- **Gear:** lv1-26 Rod / Mace, Silk Robe, Guard / Buckler, Hat / Biretta, Hood/Muffler, Sandals/Shoes, Rosary; lv24-60
  Wand / Arc Wand, Saint's Robe, Buckler, Biretta, Manteau, Rosary (undead dungeons with a party); lv55-99 Arc Wand /
  Healing Staff (lv55), Boots. Cards (top): Elder Willow (headgear, INT+2), Raydric, Sohee (shoes, MaxSP+15%); Pupa /
  Peco Peco armor, Thief Bug Egg / Horn shield, Evil Druid armor in undead dungeons, Phen (accessory, cast not
  interrupted).
- **Special items:** SP potions (mostly), a few HP potions; Blue Gemstones for Magnus Exorcismus (and Safety Wall /
  Warp Portal).
- **Boss role:** healer: Heal the tank first, Kyrie on the tank, Blessing/Increase AGI on all; never fight; Lex Aeterna
  before big hits; the party retreats if the priest dies.
- **Do:** heal first, keep SP for healing, sit whenever idle. **Don't:** melee monsters, spend SP on buffs below 60%,
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

---------------------------------------------------------------------------------------------------------------------
## Merchant -> Blacksmith / Alchemist

### Owner rules
- Every guild eventually has an Alchemist who brews potions (SP potions included) and sells them for money (RULES.md 2,
  "The economy is the core of the game").
- Vendor Merchants learn Vending: a vendor plan starts Enlarge Weight Limit 5 -> Pushcart 3 -> Vending 3 (vendor
  Merchants with Mammonite and no Vending were wrong builds and were restarted, 2026-11-14).

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

---------------------------------------------------------------------------------------------------------------------
## Thief -> Assassin / Rogue

### Owner rules
- (none class-specific yet; Ayla herself is an Assassin - her gear plan is in HANDOVER section 5.)

### Assassin: "Crit Katar Assassin" (plan `assassin`, end goal Assassin Cross)
- **Party role:** melee DPS / evasion off-tank (dodges hits, so can tank for a party).
- **Stats:** AGI 20, STR 20, DEX 10 -> AGI 40, STR 35, LUK 15, VIT 15 -> AGI 60, STR 50, LUK 30, DEX 25, VIT 30 ->
  AGI 80, STR 70, LUK 40, DEX 35 -> AGI 90, STR 85, VIT 40.
- **Skills (order):** Double Attack 10, Improve Dodge 10, Steal 5, Hiding 2, Envenom 10, Detoxify 1, Hiding 3 |
  Assassin: Katar Mastery 4, Sonic Blow 10, Katar Mastery 10, Cloaking 2, Grimtooth 5, Enchant Poison 5, Poison React
  5, Cloaking 5, Venom Dust 5.
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
- **Stats:** AGI 90, STR 90, DEX 45, VIT 40 (no LUK: non-crit). **Skills:** Thief part as the main plan | Katar Mastery
  4, Sonic Blow 10, Katar Mastery 10, Enchant Poison 5, Cloaking 2, Grimtooth 5, Righthand Mastery 5, Enchant Poison
  10, Poison React 3.
- **Use:** Sonic Blow (progress.go offense, katar only), Enchant Poison self-buff (skillbuff.go, 15+15 x lv s).
  **Weapon:** katars only (planWeaponSubs Katar): Jur[3] -> Katar[2] / Jamadhar[1]; cards Andre, Hydra, Minorous,
  Skeleton Worker, Desert Wolf (race/size, not crit), Mantis, Zerom.

### Variant: "AGI/STR Dagger Assassin" (plan `assassin_dagger`, Base assassin; run 4)
- **Stats:** AGI 90, STR 80, DEX 50, LUK 20, VIT 30. **Skills:** Thief part as the main plan (Double Attack 10 first) |
  Righthand Mastery 5, Enchant Poison 10, Poison React 5, Venom Dust 5, Cloaking 3, Lefthand Mastery 5, Venom Splasher
  10, Cloaking 5, Poison React 9.
- **Use:** Double Attack (procs only with a dagger), Envenom (offense), Enchant Poison buff. The engine equips one weapon
  (no dual wield yet; Lefthand Mastery waits for it). Venom Dust / Splasher (Red Gemstone) have no AI yet.
- **Weapon:** daggers only (planWeaponSubs Dagger): Main Gauche[4] / Dirk / Stiletto[3] -> Gladius[3] / Damascus[2].

### Rogue: "AGI/STR Dagger Rogue" (plan `rogue`, end goal Stalker)
- **Party role:** evasion "dodge tank" / melee DPS.
- **Stats:** AGI 20, STR 20, DEX 15 -> AGI 40, STR 35, VIT 20, DEX 25 -> AGI 60, STR 50, VIT 30, DEX 40 -> AGI 75,
  STR 70, VIT 40, DEX 50 -> AGI 85, STR 80.
- **Skills (order):** Thief as the Assassin (Hiding 1) | Rogue: Gank (Snatcher) 4, Mug (Steal Coin) 4, Back Stab 4,
  Stalk (Tunnel Drive) 2, Sightless Mind (Raid) 5, Back Stab 10, Snatch (Intimidate) 5, Vulture's Eye 10, Double
  Strafe 5, Intimidate (Plagiarism) 4.
- **Use:** Back Stab works in Renewal (no direction check); Raid on crowds.
- **Gear:** lv1-30 Main Gauche / Dirk / Stiletto; lv24-99 Gladius[3] / Damascus[2] / Stiletto; Thief Clothes / Chain
  Mail. Cards (top): Soldier Skeleton, Raydric, Matyr.
- **Boss role:** melee DPS / evasion off-tank: Back Stab, strip skills later.

### Variant: "DEX/AGI Bow Rogue" (plan `rogue_bow`, Base rogue; run 4)
- **Stats:** DEX 99, AGI 70, VIT 35, LUK 20. **Skills:** Thief part as the main plan | Vulture's Eye 10, Double Strafe
  10, Gank 4, Mug 4, Back Stab 4, Stalk 2, Raid 5, Snatch 5, Plagiarism (Intimidate) 5.
- **Use:** gear style "bow" (builds.go gearStyle / buildgear buildStyle: bows score like an archer's, no shield);
  Double Strafe in the Thief offense list (progress.go; skipped without a bow), arrows like any bow user (ammo.go
  bowUser). A copied (Plagiarism) skill has no fight rule yet.
- **Weapon:** bows only (planWeaponSubs Bow): Bow[4] / Composite[4] -> Great Bow / Crossbow[3] -> Arbalest / Kakkung[2]
  / Bow of Rogue Master; arrows (Arrow, Silver, Fire, Crystal, Steel). Cards: Archer Skeleton (+10% ranged), Andre,
  race cards, Zerom (DEX+3), DEX+2 / AGI+2 accessories, Raydric, Whisper, Matyr.

---------------------------------------------------------------------------------------------------------------------
## Open items (class work not done yet)
- Transcendent skills have plans (transplans.go) but no fight rules yet; no quest-skill errand (Cart Revolution etc.).
- Hunter falcon rental is live (33/115 with a falcon at run 5); the rest lack 2,500z.
- Sage: no endow AI (needs elemental stones), no Spell Breaker / Dispell AI.
- Bard/Dancer: no ensemble skills; song choice is per cast (no swap while a song plays).
- Trap AI exists (run 6, trap.go) but only the hunter_trap plan buys Traps; main-plan Hunters learn traps last.
- Wizards (run 6 trace, 6 lv56-62 outside Ayla's party): mostly Jupitel, Storm Gust only where learned - the plan puts
  Earth Spike 3 / Heaven's Drive 5 before Storm Gust 10. Run 6: the wizard plan now takes Storm Gust 10 right after
  Jupitel 3 (then Earth Spike / Heaven's Drive); caps unchanged, so nobody becomes a wrong build.
- Errand candidates not built yet: job change, falcon, class weapon, quest skill, refine (errand.go only rents Pecos).
- `research/proposed_builds.md` section D ideas (Earth Spike vs wind, Turn Undead, potion tiers) not applied.
- Run 4 variants without AI for their extra skills: Support Sage (endows, Volcano / Deluge / Whirlwind, Magnetic Earth,
  Dispell - gemstones / elemental stones), Dagger Assassin (dual wield, Venom Dust / Splasher), Bow Rogue (using a
  Plagiarism copy). Gemstones are never bought (no NPC stock logic): Magnus / Volcano / Dispell only fire with dropped
  gems - economy item.
- Knight / Crusader Pecos: Riding comes early now, but most riders lack the 3,500 / 4,500z (economy).
