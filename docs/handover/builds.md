<!-- Moved unchanged from HANDOVER.md lines 1244-1366 (as of commit 63ca923). Index: HANDOVER.md. -->

## 6f. Wrong builds: audit and restart (2026-11-14, `tools/wrongbuilds.py`)
- Owner rule: a bot char whose skills/stats don't match its plan is restarted at Novice 1. `python tools/wrongbuilds.py
  audit` -> `legion/data/wrong_builds.json` (wrong list, order_only list, reset_log, Ayla-party and guild-leader
  sections). `python tools/wrongbuilds.py reset NAME,...` or `reset --all` restarts listed chars in place (same
  char_id/name/slot/plan, class 0, stats 1 + 48 points, seeded zeny, starter kit, legion_first/branch from the plan,
  home-town position; inventory/cart goods move to the account storage) - only bot accounts, never the core party,
  and only when every char of the account is offline (rAthena would overwrite the row / storage on save).
  Take a backup first: `docker exec ro-db mariadb-dump -uragnarok -pragnarok ragnarok char inventory cart_inventory
  skill char_reg_num storage ... > backups/pre-rebuild-<ts>.sql`.
- 2026-11-14 00:40: 14 wrong (all vendor Merchants with Mammonite and no Vending), 11 restarted
  (backup `backups/pre-rebuild-20261114-0038.sql` + `-storage-login.sql`); lilmoondragon, TheShadowmage Trade and
  rinmonk Mart were online - rerun `reset --all` later (they may have learned Vending by then; the audit drops them).

- AoE plan change (2026-11-14 08:26): builds.go `Build.Legacy` holds a plan's previous skill goals; wrongbuilds.py
  adds their caps, so characters built under the old plan are never "wrong". Use it whenever a plan is reordered.

## 6f2. AoE casters (2026-11-14, `legion/aoe.go`)
- `aoeCast` runs first in `fight()` for the Mage family: 3+ monsters inside an area spell's radius around a monster in
  range 9 -> Storm Gust / LoV / Meteor / Thunder Storm / Heaven's Drive (0x0366 UseSkillToPos) or Fire Ball (on the
  centre monster); SP reserve max(15%,30); no KS (claims of non-party bots, monsters fighting non-party players), solo
  no boss/swarm; quick spells only while hit without a party tank; solo Fire Wall vs. a walker. Tests: aoe_test.go.

## 6f3. Build-and-strategy review per class (2026-11-14, `legion/weaponskill.go`, `legion/data/buildgear.json`)
(Summarised per class, with the owner's class rules, in `CLASSES.md`; new per-class notes go there too.)
- A standing agent compares each class's build, fight skills and maps against Renewal guides (iRO Wiki) and fixes the
  engine. Plans must stay prefix-compatible (old goals go to `Build.Legacy`), so no character becomes a wrong build.
- Archer line: Arrow Shower (ground, 3x3 / 5x5 from lv6) on 3+ grouped monsters (`weaponAoE`); Improve Concentration
  self-buff at the highest level that leaves >60% SP (`selfBuff.cost`, `buffLevel`); `fightSkills` also runs for bow
  users at range (it used to stop at 3 cells, so ranged classes never buffed). Hunter plan: falcon, Blitz, Steel Crow,
  then Beast Bane 10, Arrow Shower 10, Owl's Eye 10, traps last (there's no trap AI).
- **PENDING: the falcon rental in legion.txt needs a rathena restart** (`rathena/npc-custom/legion.txt`: a char
  with Falconry Mastery and no falcon pays 2,500z for one on login or level-up). Before this, all ~21 Hunters had Blitz
  Beat 5 and Steel Crow ~10 but no falcon.
- Acolyte line: Heal is used as an attack on Undead-element monsters (`healAttack`: solo keeps 20% SP, a party healer
  keeps 50%); a solo support Acolyte casts Blessing and Increase AGI on itself (`fightSkills` used to skip every
  support-role bot).
- All classes: plants and mushrooms (no BaseExp, HP <= 20, `Mob.worthless`) are never picked as targets.
- Crowd skills (owner rule: AoE in crowds on the central monster): `crowdSkills` table in weaponskill.go - Arrow
  Shower, Brandish Spear, Bowling Bash, Cart Revolution, Magnus Exorcismus (undead/demon, Blue Gemstone, 4+) - cast on
  the centre of the best 3+ cluster inside the skill's real splash radius. aoe.go keeps the mage line.
- "key" self-buffs (`selfBuff.key`: Two-Hand Quicken, Adrenaline Rush, Improve Concentration, Auto Guard) are cast
  down to the 30% SP floor (the 60% rule kept Knights from ever casting THQ). Crusader: Auto Guard, Reflect Shield,
  Grand Cross (3+ adjacent, HP >= 70%).
- peco.go: Knights/Crusaders with Riding 1 (Cavalier Mastery no longer required, owner: Knights ride) and price +
  1,000z rent their Peco at the stock breeder through the NPC dialog (knight 55,350 / crusader 232,318); riding =
  0x0229 option bit 0x20, saved with the character. A party's town run goes to Prontera while a member wants one
  (partyRunTown); a solo rider's next trip goes there, plus one "Peco Peco ride" trip (3 h rest). Human-led parties
  (Ayla) choose their own town: Echobunny needs the plugin to take one run to Prontera. DB 09:55: 0/93 Knights, 0/72
  Crusaders lv40+ riding.
  **13:15 fix (root cause):** `optionSeen` was only set by a 0x0229 to ourselves, which rAthena sends at login only
  when opt2 (poison...) is set - so `wantsPeco` was false for almost every rider forever (DB 13:05: 1/125 Knights,
  0/91 Crusaders lv40+ mounted; LilZerofire 30,280z with Riding never tried). Now bot.go reads the saved option from
  the char list (CHARACTER_INFO.effectstate, offset 36). Also: rental + 200z (pecoReserve) instead of + 1,000z; a
  rider's whole purse is held for the Peco (`pecoNeed` -> gearReserve = Zeny; off for 6 h after 3 refused talks);
  retries after 40 min (pecoRetry; peco errand cooldown too) instead of 2-6 h; `pecoFundRound` (econ round): the
  richest free guild/party mate (keeps 2,000z, not a rider itself) mails a short rider its shortfall (rider 20 min /
  donor 30 min); Riding 1 is the FIRST Knight/Crusader skill in every plan and trans plan (owner: "official path to
  get peco as soon as possible"). /api/economy `peco_rented`, `peco_fund_mails`. Verified chain: Neko_sage85 (memory
  "rented a Peco Peco in Prontera", DB option 32) - NPC prontera 55,350 / 232,318, menu 1, setriding.
  Human-led parties (core friends, Echobunny): humanPartySync stands down while a Peco errand is open or a breeder
  visit runs (like the alchemy errand), and a human party's rally no longer blocks the breeder visit in Prontera.
  Trap errand (errand.go "traps"): a partied trapper Hunter low on Traps (trapTripDue) fetches them alone at the
  nearest Trap seller (trap.go startTrapTrip), 20 min errand cooldown; humanPartySync stands down for it too.
  Katar fund (peco.go katarFundRound, shared mateFundRound): an Assassin saving for its katar (coreweapon.go
  katarShortfall) gets part of it mailed by its richest free guild/party mate (half above 2,000z, <= 10,000z a mail,
  rider 20 min / donor 30 min; donors saving for a Peco/core weapon/guide never pay). /api/economy katar_fund_mails.
- Run 2 (09:20-10:00): bow users with 0 arrows buy up to the 50% weight line (the 45% potion cap made every arrow buy
  0), "no arrows" before "out of potions"; Assassin weapon route Main Gauche -> Jur -> Jamadhar -> Katar (Sonic Blow
  needs a katar); Acid Terror / Demonstration need their bottle (progress.go skillItem); a gear-pool turn item is
  dropped while the member lacks its class weapon / arrows; wizard + sage entries in buildgear.json. Back Stab works
  in Renewal (no direction check). Bard songs need an instrument in hand - not done (bow-route bards).
- Priest stats: INT first to 99, then DEX/VIT (`Build.LegacyStats` keeps the old steps valid in wrongbuilds.py).
  Healer rest: sits under `priest_sit_low_sp_pct` (40), rests to 80% (60% when the party moves), never above ~60%.
- Boss hunts: `bosshunt.go` (catalog from boss spawns, strict readiness, hunt phases, retreat), API
  `/api/boss-hunt[/bosses]`, `legionctl boss-list|boss-check|boss-hunt|boss-status|boss-cancel`.
- `legion/data/buildgear.json`: recommended gear and cards per plan id and level band, for the economy agent (it loads
  the file every 5 min). Hunter, bard, dancer and priest are filled in so far.

- Errands (`errand.go`, owner rule 2026-11-14 RULES.md 3 "A member may leave briefly"): a party member (never a bot
  party's leader) that is idle (no fight, nobody in the party losing a fight, no rally, HP >= 60%) leaves for a
  necessary step: says "brb, renting a peco", starts a solo "Peco Peco ride" trip to Prontera (town.go), peco.go rents
  there, then the normal party travel brings it back; "back" once within together_cells of the leader (30 min cap,
  2 h cooldown `buffAt["errand:cd"]`). Published `pubState.Errand`: skipped by needsCollect, partyRester,
  Party.resting, rallyMembers, farmingWell and the human-party "where r u". Bot parties no longer steer the party run
  to Prontera for a member's Peco (only the leader's). Human-led members (Echobunny) use it too. Next candidates for
  errands: job change, falcon, class weapon, quest skill, refine.
- Mage line on a Knife counts as `bowless()` too (no class weapon at all): it may call a party run after 6 min.

- **Run 6 (2026-11-14, build-and-strategy):** variants with AI: `rogue_strip` / `monk_occult` (variantskill.go: Divest,
  Occult Impaction, Mental Strength), `hunter_trap` (trap.go: Claymore / Land Mine, Trap buying + supply trip);
  `coreweapon.go` (a learned core skill's weapon - katar for Sonic Blow - is a savings goal, gearHold);
  `teleslot.go` (Clip [1] is unobtainable here: Belt [1] 20k is the Teleport-card accessory; card bought only with a
  slot); `reset.go` (natural respecs at the Izlude Hypnotist for 1st classes: store everything, stat+skill reset,
  fetch, new plan; resets_pending.json); combo Monk timing (weaponskill.go comboNext waits for 0x01d2);
  `skillaudit.go` (`/api/skillaudit`: skill requests answered / refused / silent; can-act gating of b.skillReady,
  tuning `skill_canact_off`). Packet facts for this server (PACKETVER 20211103): cast start = 0x0b1a (not 0x07fb),
  no-damage skill = 0x09cb (not 0x011a), combo window = 0x01d2; skills sent during canact (every swing: amotion) or
  while casting are dropped WITHOUT a failure packet. Read rathena/src/map/packets_struct.hpp before parsing.

## 6f4. Server cap at transcendent classes (2026-11-14, owner: "cap server at trans class")
- Progression: Novice -> 1st (job 10) -> 2nd (job 40) -> at base 99 / job 50 rebirth -> High Novice (base/job 1, 100
  status points) -> High 1st (job 10) -> transcendent 2nd (job 40; Lord Knight ... Gypsy, same branch as before) ->
  STOP. Trans caps: base 99, job 70 (stock `db/re/job_exp.yml`, kept).
- Server side, for everyone (bots and humans): `rathena/npc-custom/scripts_custom.conf` is mounted over
  `/rathena/npc/scripts_custom.conf` (docker-compose.yml). It is the stock (all-commented) file plus `delnpc:` lines
  that unload the 3rd-job quests (npc/re/jobs/3-1, 3-2), the Expanded Super Novice (supernovice_ex) and the Doram 4th
  job (doram/spirit_handler). It works because npc/re/scripts_main.conf imports scripts_custom.conf LAST;
  `delnpc` in conf/import/map_conf.txt would not (map_conf is read before the NPC lists). Kept: rebirth
  (npc/jobs/valkyrie.txt), trans 2nd-job NPCs (npc/jobs/2-1a, 2-2a), Taekwon/SG/SL, Ninja/Kagerou/Oboro,
  Gunslinger/Rebellion, Super Novice, Doram Summoner. No other loaded script calls `jobchange` (npc/custom/jobmaster.txt
  is not loaded). Only the owner's account (group 99, all_commands) can `@job` into anything; bots are group 0.
  Verified offline: a throwaway map-server (`--run-once`, private network + private MariaDB, never ro-db) parsed every
  script with no errors; script NPCs 19,661 -> 19,262 with the cap.
- Bot side (`legion.txt`): the old "trans 2nd at 99/70 -> 3rd job" block is gone. Rebirth now costs the stock
  1,285,000z (Book of Ymir in the Sage Castle); a 99/50 2nd job without it keeps farming until it has it (the stock
  Valkyrie's "empty inventory" step is skipped as a stand-in for the storage trip). Trans 2nd job is announced.
- Engine (`legion/transplans.go`): `transSkills[plan]` (2nd-job core again, then trans skills with prerequisites) comes
  before the rest of Build.Skills once the class is a trans 2nd job (`isTransSecond`, 4008-4021). Build.Skills is
  unchanged, so no existing character becomes a wrong build. 3rd-job ids stay in the class maps (family, transToSecond)
  only as harmless lookups.
- Takes effect: legion.txt + scripts_custom.conf at the next `docker compose up -d rathena`; engine at the next deploy.
- As of 2026-11-14 10:00 no bot is near: max bot base level 80 (one Knight), none >= 90, none trans or 3rd job.

