# All classes

Class rules apply with the general rules in `RULES.md` and `docs/rules/`. Builds follow the plans in `legion/builds.go`
(stats and skill order; `Legacy`/`LegacyStats` keep older plans valid), the gear table `legion/data/buildgear.json`, and the
build/class master's reviews (`docs/handover/builds.md`, `legion/data/lessons.md`). Skill names are the rAthena names, with
the classic name in brackets where they differ. The owner's words for each class rule are in `docs/rules/archive.md`.

## Rules for every class
- CL01. Every class with an area skill (Magnum Break, Bowling Bash, Brandish Spear, Arrow Shower, Grand Cross, Hammer Fall,
  Cart Revolution, Meteor Assault, Heaven's Drive, Storm Gust ...) casts it when monsters are grouped, aimed at the monster
  in the middle of the group. (2026-11-14)
- CL02 (also PT13). Tanks are active: they engage first, pick up every monster that targets a party member, take aggro off others, keep
  attacking, and never sit back. (2026-11-14)
- CL03. A tank may pull big groups; when surrounded it uses its area skills (Bowling Bash, Magnum Break, Brandish Spear, Grand
  Cross) and potions with the priest healing, and does not escape except as a last resort near death (Butterfly Wing to the party's save Kafra when in a party (PT31); Fly Wing only solo). (2026-11-14)
- CL04. Every class has its common build variants (e.g. Knight 2H-sword and spear, Priest full-support and battle, Wizard AoE
  and bolt, Hunter trap/DEX, Assassin katar and dagger, Blacksmith battle and forge, Monk combo and Asura, Sage battle and
  support), spread across the bots, each with its own gear table in buildgear.json. A bot's goal is to complete its build: it
  farms the drops and zeny for its next gear piece or trades for it. The build/class master compares each class with good
  online Renewal guides and improves the engine. (2026-11-14)
- Related general rules: EC01, EC02 (wrong build), ST01 (trans cap; the end goal of each plan is its transcendent class), EC20
  (gear upgrades), EC26 and EC29 (utilities, SP potions), PT23 (errands), EC19 (boss hunts).

### Build variants (2026-11-14)
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
  before a wing (danger.go, same rules: Butterfly Wing to the party's save Kafra when in a party (PT31); Fly Wing only solo); a solo hunter with nothing to fight for 6 s on its hunting map teleports
  to a random cell instead of roaming (not in a party: it would split it; keeps 25% SP for an escape). The skill's warp
  list (0x0abe) is answered "Random" (0x011b); a map where the position didn't change is skipped for 30 min.

### Build variety across the Legion (2026-11-14)
- CL15. **The Legion's builds must be varied** (owner request, 2026-11-14): bots of the same
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

### Build milestones (rebalanced 2026-11-15; EC39)
- `legion/data/buildmilestones.json` holds, for every build in `builds.go`, the stats and skills reached by the end of each gear band (replay of the ordered plans, `tools/design/gen_milestones.py`) and the solo / MVP / PvP playstyle. Tests: `tests/tools/test_buildmilestones.py` (budgets, reach) and `tests/tools/test_wrongbuilds_replay.py` (old plans flag nothing). Gear stays in `buildgear.json`; the party role stays in code.
- Limits that shape every plan: 1,273 stat points at level 99; skill points 9 (Novice, Basic Skill, outside the plan) + 39 (first job, changes at job 40) + 49 (every second job caps at job level 50 here) = 88 for the plan.
- Before the 2026-11-15 rebalance 17 plans listed stat goals above 1,273 points, 16 listed more than 88 skill points, and 25 left at least one goal unreached. Now 37 of 37 plans reach every goal; the largest stat total is 1,273 and the largest skill total 88. Per line: the class file's "Build milestones" section.
