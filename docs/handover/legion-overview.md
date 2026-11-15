<!-- Moved unchanged from HANDOVER.md lines 435-775 (as of commit 63ca923). Index: HANDOVER.md. -->

## 5. Legion: what exists (G:\RO\legion\legion)

Key files: `bot.go` (protocol/session), `ai.go` (think loop, fighting, potions), `nav.go`, `strategy.go` (117 reachable
hunting spots), `builds.go` (13 build plans: stats/skills/gear to 99; research in `research/proposed_builds.md`),
`party.go` + `partyroles.go` (role parties, bait one-hit tagging, even share), `humanparty.go` (parties led by a
human: members follow the human's map, wait at the checkpoint town when the map is too dangerous; pinned characters
in `data/human-parties.json`), `guilds.go` (10 GuildDefs, leaders chosen by ambition, found at Base 15, recruit by fit;
roster `data/guild_roster.json`), `danger.go` (escape with Fly/Butterfly Wing/run, easier map after 3 escapes),
`sale.go` / `vend.go` (shops, "S>" chat rooms), `minds.go` (minds bus), `dashboard.go/.html` (dashboard),
`partydash.go` (`/api/parties/detail`, dashboard "Parties" card), `memory.go` (per-player memory + diary).

### General bot rules added 2026-11-13 evening (owner's words in quotes)
- **Level, don't march** ("prioritize levelling up rather than walking around"; "Stop the march"):
  - `goodHere()` (ai.go): a bot on a map that fits its level and holds <= 70 bots stays and levels there.
  - Gear trips (builds.go): none before base level 10; only to the nearest shop within 5 map changes; a gear trip
    that times out isn't retried that session. Sell/potion trips go to the nearest town with a tool dealer, not the
    home town. Before this fix 500+ Novices were walking to `yuno_in01` / `alberta_in`, and the average level stayed ~1.
- **Kafra fast travel** ("they have kafra for fast travel"; "make kafra teleport cheaper"; kafra.go):
  - A bot in a Kafra town whose route saves >= 3 map changes by teleporting goes to the Kafra Employee and picks
    "Use Teleport Service" -> destination.
  - Prices are 1/10 of official (60-180z) via `rathena/npc-custom/functions_kafras.txt`, a copy of the stock file
    mounted over `npc/kafras/functions_kafras.txt`. (Defining F_KafSet a second time doesn't work: the stock file
    loads after the custom ones and wins.)
- **Save at the Kafra nearest the grinding ground** ("save the spawn at the closest kafra near grinding ground"):
  a bot heading to a hunting map that passes through that map's nearest town saves there (Kafra menu "Save").
  Ayla's mind does the same with OpenKore `talknpc`.
- **All shops at Prontera's south gate** ("all shops to be in pron", "south gate of pron should be the market place"):
  - `rathena/npc-custom/legion_shops.txt` (generated from every normal NPC shop) puts Weapons/Armor/Tools/Goods
    merchants at prontera 142-168,57-60. Legion parses it (npc-custom is mounted into ro-legion).
  - Player vending pitches (vend.go `initMarket`) are at the south gate too: x 133-172, y 45-78, with the road
    x 152-160 kept clear.
- **Tanks lead** ("the tanks must be a lot more active. It cannot sit back"; party.go): a tank in a party pulls the
  next monster near the group by itself, and rests at half the usual HP threshold.
  2026-11-14 (`legion/tanklead.go`, owner: "attacking the mobs more aggressively instead of sitting behind"): measured
  before, member tanks fought 38% of samples and trailed "with/following/waiting/regrouping" 27%; non-tank leaders
  engaged first while their tanks followed 7-12 cells behind. Now: `tankTakeAggro` (Provoke or hit any monster
  targeting a member) runs BEFORE the tank's own target, for member tanks and tank leaders (hunt); the member tank
  keeps fighting its target up to together-4 from the leader, pulls around the leader or the group's front
  (`tankFront`: 5 cells ahead on the leader's walk, pubState DX/DY; pull radius 12, never beyond 16 from the leader)
  incl. the leader's own target, and with nothing to pull walks at the front ("leading the way for X") instead of
  trailing; a non-tank bot leader assists its tank's target and holds a fresh pull up to 3 s per 8 s
  (`leaderDefer`, "letting X pull Y"); ranged/melee members assist the tank's target when the leader has none; a
  party tank doesn't sit for its own rest while a member fights nearby (`tankHoldsRest`, HP >= 30%). Party rests
  (restGuard) and escape are unchanged. Test: tanklead_test.go. Check after deploy: share of "tanking/pulling/taking/
  provoking/leading the way" in tank states vs "following/with"; leaders "letting X pull" should be brief.
  Crowds (owner: "knight can pull but must use AOE instead of escaping"; Echobunny winged off 8-12 monsters at 46-48%
  on pay_dun01 and left the priest): no pull limit. A party tank with potions or a priest within 14 cells is exempt
  from escape()'s "N monsters on me"/"losing" triggers (`tankStands`/`tankHolds`); it wings out only under 10% HP,
  under 20% with no potions or no priest near, vs a monster 8+ levels above, or a boss. With 3+ monsters on it and the
  priest 6+ cells away it walks the crowd to the priest ("bringing N monsters to X", `tankFallBack`); its crowd skills
  fire from fight() (Bowling Bash/Brandish = weaponskill.go crowd table, Magnum Break ai.go, Grand Cross skillbuff.go);
  fighting a single monster it keeps SP for floor + 2 crowd casts (`tankAoEReserve`, in fight()'s reserve), and with a
  crowd on it drinks an SP potion when the next crowd cast doesn't fit (`tankCrowdSP`). HP potions: survive() (50%+).
- **Melee must walk into reach itself** (2026-11-14, ai.go `closeIn`): rAthena never chases for a player; an attack
  on an out-of-reach target gets 0x0139 and is dropped. fight() closes in first and re-sends after a 0x0139. This
  was the real cause of "swordman standing still, not attacking". Debug any combat problem with the per-bot packet
  trace (`legion/trace.go`: env `LEGION_TRACE=Name` or `GET /api/trace?bot=Name&on=1|0`, kept in `data/trace.txt`;
  lines `TRACE <name>` in the container log).
- **Party stays together** ("party should generally stay close to each other while grinding for safety"):
  - `partyLeash = 8` (party.go): members only start fights within 8 cells of the leader and catch up beyond that.
  - A bot party's leader waits for stragglers before the next pull (ai.go hunt).
- **Ayla's party picks its own grinding spot**: that's the Haiku mind's job (see section 4).
- **Party rallies at the destination's Kafra** (owner rule 2026-11-13, `legion/rally.go`): `Party.Rally` holds the
  rally (town, Kafra x,y, dest, start, all-arrived, cooldown, human's hunting map). `rallyManager` (every 3 s) starts a
  rally when a party is formed with members off the destination (`rallyOnForm` in formParty) or when online members
  are on different maps for >45 s (stragglers <=2 maps from the destination get 3 min to catch up; never during a
  party town run or while someone waits at the checkpoint on purpose). Rally town = nearestTown(dest); spot = the
  town's Kafra nearest the exit warp toward dest (`rallySpot`). Members (and a bot leader) in `rallyThink` (ai.go,
  after tripThink): travel there (fight back), save (`kafraTrip{Save}`), wait within 4 cells sitting. Ends when every
  online member (and the human: in the town, within 12 cells if seen) is within 12 cells and saved (or 3 min after
  all arrived) -> "all here, let's go", 3 min cooldown; 12 min timeout -> abandon, 5 min cooldown.
  Party chat line "meet at the Kafra in <town> (x,y)" is fixed wording: Ayla's partylead.pl parses it, and the bots
  join a rally Ayla calls with the same line (`rallyHeard`, bot.go 0x0109). Ayla: `start_rally`/`rally_tick` in
  partylead.pl route to the Kafra, `talknpc x y c r~/Save/ n`, sit, wait for all within 12 cells, restore lockMap.
  Her old cross-map chasing (ai_route to the straggler's map) is replaced by calling a rally. State shown in
  `/api/human-party` (`rally`) and `/api/parties/detail` (`rally`) + the dashboard Parties card.
  `GET/POST /api/rally?party=<name|human|leader>` shows / starts a rally. Rally radius = together_cells (20).
- **Together = 20 cells** (owner 2026-11-14): `tune().TogetherCells` (`together_cells` in tuning.json) is the one
  threshold for member regroup (party.go), bot-leader collect (ai.go), rally completion and dashboard `with_leader`.
  partyLeash (8) still limits where members start fights. Ayla's plugin: TOGETHER 20 / RESUME 17; waits for followers,
  walks back only for a friend that needs help (asked / HP<50% / not moving 30 s), finishes that commitment.
- **Stuck watchdog** (`stuckWatch`, rally.go, every think): sending walks but same cell 20 s -> close NPC dialog
  (last id from 0x00b4-0x00b7), cancel trade, leave chat, stand, drop route/path, step to a free cell. Logged as
  "stuck at ...: unsticking" events. travel() and the regroup branch stand up when seated.
- **Town runs from the human**: only when the human is inside a town building (shopping), not walking through town.
- **Rally kill switches** (tuning.json): `rally_enabled` (bot parties; default false = OFF since 2026-11-14 00:28 -
  bot-party rallies stalled guild leader parties legion-wide) and `rally_human_disabled` (Ayla's party; false = on).
  Rally max 5 min, 30 min between rallies of the same party, members with no route to the rally town are left out,
  the rally Kafra must be reachable on foot from the town spawn without warps (`kafraReachable`, else spawn point).
- **Rally progress watchdog**: a rally whose "distance left" score doesn't improve for 2 min is abandoned (5 min
  cooldown); a completed one has a 20 min cooldown. Ayla's own rallies: 20 min after success, 10 after a failure.
- **FindPath warp rule** (nav.go): only the goal's warp box is exempt; the box the bot stands next to is exempt only
  for its ring, never its trigger area (bots arriving at prontera 47,228 from prt_in walked straight back through the
  door warp 50,228 - prontera <-> prt_in loop).
- **Collect/regroup hysteresis** (`collect_stop_cells` 8 in tuning.json): a bot leader's collect and a member's
  regroup start beyond together_cells (20) and keep going until within 8; the leader keeps one straggler until done.
  Ayla's plugin: waits when a friend is > 20 away, carries on when all are within 12 (RESUME).
- **Ayla knows where her friends really are**: partylead.pl reads Legion's core API (`127.0.0.1:8091/api/human-party`,
  same network namespace, every 4 s, ~1 ms) and uses those positions when a friend is out of sight (the party list's
  positions go stale or are missing; missing ones used to be skipped, so she ran ahead with holding=0).
- **No lone walk back after a death**: when a friend that was out hunting shows up in a town (respawn or a solo trip),
  Ayla holds the party; in a dungeon she first leads it out to the first map outside the dungeon on the way to the
  friend (e.g. pay_arche) and waits there until the friend is within 20 cells (7 min max) - `back_tick`.
- **Ayla's random walk is short**: partylead.pl hooks `ai_processRandomWalk` and walks to a random cell within 12
  cells (OpenKore's own picks any cell of the map); `route_randomWalk_maxRouteTime 10`. Passing through a town she
  also stops while a friend is > 20 cells behind or still on the previous map (3 min max per stop).
- **Ayla's gear plan advanced (2026-11-14 10:40Z)**: she owns Jur [2] (equipped since 08:04Z). The 10:2xZ Prontera run
  did NOT buy a second one (no 19,500z debit in her log; gear_run_end printed "bought" for "owned" - fixed: it now
  compares the count before/after). Step 3: `buyAuto Muffler` / `buyAuto Shoes` (Payon Armor Dealer payon_in01 7 119,
  no minAmount, maxAmount 1, zeny >= 9000), equipAuto robe/shoes; step 4: `gearPlan_item Cap` (15000, Prontera Armor
  Merchant 146 57, `buyAuto Cap` disabled+gearRun). gear_run_start never enables a gearRun slot for an item she owns;
  after a run the plan advances via `gearPlan_next` ("Item=zeny;..."). In-memory config was set with `eval` (no full
  `reload config`, which resets lockMap); config.txt carries the same blocks for the next start.
- **Mind -> plugin orders** (partylead.pl `Commands::register('partylead')`, via commands.txt or `ayla.sh`):
  `partylead rally cancel` / `partylead rally <town>` / `partylead goto <map>` / `partylead status`. The mind's
  explicit order wins at once (QA: the mind said "no rally, wait at pay_dun00" and the plugin kept walking to the
  Kafra). `ayla.sh map NAME` now sends `partylead goto`. Documented in the legion-session skill for the ayla mind.
- **Ayla's gear plan** (2026-11-14). She is an Assassin (STR 49 / AGI 67 / VIT 1 / INT 1 / DEX 15 / LUK 31,
  Katar Mastery 4, Sonic Blow 7, Double Attack 10) who was wearing only Knife [3] + Cotton Shirt with ~10k zeny; her
  friends have ~1k each, so she funds herself (the Legion gear pool does not include her). Path (rAthena re prices;
  every item is also at the Prontera south-gate market, `npc-custom/legion_shops.txt`): **1** Adventurer's Suit 1,000z
  (DEF 20; Payon Armor Dealer `payon_in01 7 119`) -> **2** Jur [2] 19,500z (katar ATK 125, unlocks Sonic Blow; only
  Prontera `142 57` / Morroc) -> Muffler 5,000 + Shoes 3,500 (Payon) -> Cap 12,000 (Prontera) -> Boots 18,000 ->
  Manteau 32,000 (Payon) -> Katar [1] 41,000 or Jamadhar 37,200 -> Thief Clothes 74,000 (DEF 40, AGI+1, weight 10).
  Wooden Mail / Circlet / Biretta are not wearable by Assassins; Chain Mail (weight 330 of her 387) is too heavy.
  Implemented with OpenKore's own mechanisms in `control/config.txt`: gear `buyAuto` slots have **no minAmount**
  (then `shouldStartAutoBuy` never starts a trip for them; they are bought only inside a real sell/restock run) and
  `maxAmount 1` (equipped items count, so no Main Gauche loop); `equipAuto_0` wears them (`armor Adventurer's Suit`,
  `rightHand Jur [2]`); `attackSkillSlot Sonic Blow` only `whenEquipped Jur [2]`. The Jur slot is `disabled 1` +
  `gearRun 1`: partylead.pl `gear_run_start/gear_run_end` turns a **real-need** town run (sellAuto at
  itemsMaxWeight_sellOrStore 48%, potions <= 10, or a friend's overweight call) into a Prontera south-gate run when
  she lacks `gearPlan_item` and has `gearPlan_zeny` (21000): sellAuto_npc and every buyAuto npc are swapped in
  memory to their `pronteraNpc` twin, the slot enabled, and all put back when the errand ends (max one per 30 min;
  Kafra Payon<->Prontera is 120-240z). Next step: when she owns the Jur, set `gearPlan_item` to the next Prontera-only
  item (or add Payon items as plain no-minAmount slots). items_control: OpenKore's default "worthless gears"
  (Adventurer's Suit, Thief Clothes, Chain Mail, Main Gauche, ...) were set to sell; all switched to keep (owner rule:
  weapons/armor never go to an NPC). `attackEquip_rightHand Main Gauche [3]` was cleared (she'd have swapped to it).
- **Ayla's gear path v2 + her purse: plugin `aylagear.pl` (2026-11-14 ~18:40Z)**. It replaces the gearPlan_item /
  pronteraNpc twin system above (partylead's old gear_run_start stays as a dead fallback: `sellAuto_pronteraNpc` is gone).
  - `ayla.sh give NAME ZENY [why]` (console `give "NAME" ZENY why`): RODEX zeny mail (open -> write NAME -> name check ->
    zeny/title/body -> send -> cancel-write -> close; rAthena fee 2%), one gift at a time, then party chat "sent Echobunny
    22k for the X". Verified 18:32Z (Echobunny's core diary: "got zeny by mail from Ayla"). Legion friends need no change:
    mail.go collects zeny mail from any sender.
  - `gearPlan_list` (config.txt) = ordered "IDS=BUDGET; ..." (first id = the piece, others count as owning it; "CARD>ITEM"
    = compound the card into that item). Sellers come from `:8090/api/lookup/item` (npc_shops). On every real-need town
    run partylead calls `aylagear::run_start(huntMap)`: the first unowned piece she can pay (NPC) or that a Prontera vendor
    lists (player-only, `/api/market`) picks the town = the seller closest to the hunting town (%NEAR, Kafra hops; a
    friend's "can pay" shop town gets a 2.5-rank bonus, at most once per 30 min); `sellAuto_npc`, every buyAuto slot with
    `townShop tool` and `storageAuto_npc` (town Kafra) point to that town, and slot `buyAuto Gear Slot` (`gearSlot 1`)
    gets the item id / NPC / zeny. partylead announces "town run to <that town>" (fixes "said payon, went to geffen").
    `run_end()` restores everything; a vendor piece is then bought in Prontera (walk to the shop, `sendEnteringVender`,
    buy if price <= budget, partylead paused via `aylagear::busy()`), never tested live yet (no vendor had one).
    Player-only pieces nobody vends: one "B> <item>, paying up to Xk" on #trade every 30 min (Legion has no code that
    answers a human's B>, so this is flavour until the economy side adds it).
  - Equip: every 10 s an owned-but-unworn plan piece is worn; first `equipAuto_0_<slot>` is set to it (configModify,
    file + memory) so equipAuto doesn't put the old piece back, and a new weapon replaces the old one in every
    `*_whenEquipped` (Sonic Blow is "whenEquipped Jur [2]"). Card: equipAuto slot cleared, item taken off, `card forceuse`,
    carded item worn.
  - Her list: Boots 18k -> Manteau 32k -> Clip [1] (player-only: no NPC sells 2607 here) -> Creamy Card into it ->
    Katar [1] 41k (Prontera/Morroc) -> Coat 22k -> Helm 44k -> Thief Clothes [1] (player). **No Chain Mail / Padded
    Armor**: weight 330 / 280 of her 420 capacity (status weight 714/4200 is in tenths).
  - `ayla.sh gear` prints the plan; console `gear dry [map]` shows the town/piece a run would pick, `gear applytest [map]`
    applies + restores the switches, `gear set KEY VALUE` / `gear unset KEY` edit %config in memory (no `reload config`).
- **partylead fixes (same time)**: `partylead rally <town>` from the mind replaces a running rally in another town (it was
  silently ignored); while a rally runs, a lockMap set meanwhile is stashed in `$saved` and cleared (OpenKore routed to
  gl_church and the rally back to the Geffen Kafra every 15 s); town_of_map knows gl_* / orcsdun (Geffen), in_sphinx /
  moc_pryd (Morroc). `goto_tick`: a mind's goto stands her up and starts `ai_route` itself (retried every 10 s; also
  after 15 s of an empty ai_seq off her lockMap when not holding/resting) - she sat at the Morroc Kafra with lockMap set.
- **Strong-party respawn + danger priority (2026-11-14 ~19:10Z)**: back_tick - in a strong party (party_strong: all
  lv50+) a friend that reappears in a town (death/respawn) gets "X come back to <map>, kafra to <town>" and comes by
  itself; no rally, no leaving the hunting map (weak parties keep the town rally). `%dangerFrom`: a fight-help call
  ("help! 6 mobs on me", "dying", "heal pls", ...) counts as danger for 90 s, like HP < 50%: she turns back for it even
  in a strong party, back_tick starts nothing while a friend on her map is in danger, a self-started rally (`self`
  flag) pauses (rally_tick returns 0, the collect logic goes to the friend) and travel mode is off so she fights.
- **Verified Kafra save + rally destination (2026-11-14 ~19:20Z, owner rule RULES 2 "save at the Kafra nearest the
  grinding ground", enforced)**: rally_tick saves with `talknpc x y c r~/Save/ n` and counts it only when the Kafra's
  "...has been saved here" / "Saved." line arrives (hook `npc_talk` -> npc_said) in that town, up to 3 tries; then
  `saveMap` is written (configModify) and she says "saved at <town>". Friends' "saved at X" lines are tracked
  (%friendSaved); the rally ends when everyone is within 12 cells and has said it (2 min grace after all arrived, since
  the Legion side may not say it yet) with "everyone saved at X? ok going in, <map>". `partylead goto <map>` whose town
  (town_of_map) is not her saveMap starts with a save-stop rally at that town's Kafra. `partylead rally <town> [map]`
  (ayla.sh rally TOWN [MAP]): a mind-ordered rally drops the old lockMap; with a map the party departs there after
  regrouping, without one everyone waits at the Kafra for a goto (verified live 19:15Z: "Kafra: respawn point saved in
  payon", "rally: respawn point saved at the payon Kafra (verified)").
- **aylagear: #trade deals, popups, gear town (2026-11-14 ~19:05Z)**.
  - Deals (Legion humanwant.go answers her "B> X, paying up to Nk"): `heard_pm` reads offers in whispers (an unowned
    plan piece's name + "12k"/"12,500z"); within the piece's budget (and zeny + 2% + 1k) she answers "deal, mail it" (a
    vendor offer: "ill pass by ur shop"), above it "cant do 90k atm" (verified 19:03Z with a test whisper). `mail_tick`
    opens the mailbox every 3 min (8 s after "sent, check ur mail", 5 s after an unread notice), reads each mail with an
    attachment, takes a wanted piece at the body's "for Nz" / the agreed price (no price = gift), takes zeny, leaves the
    rest (whispers "sorry, cant take"), then pays each seller by RODEX (give queue, `sale => 1`, thank-you whisper,
    3 retries, else an UNPAID line). Every step -> `logs/deals.log` + console "[aylagear] DEAL: ..."; `ayla.sh deals`.
  - Popups (owner rule RULES 5): the watch client never gets the server's answers for actions the plugins drive:
    `Network::Receive/willMangle`+`mangle` drop rodex_* and vender shop packets while a give / mail check / vendor buy
    runs (+5 s), and npc_talk*/npc_store*/npc_sell_list/storage_* while sellAuto/buyAuto/storageAuto run or partylead's
    Kafra save sets `$aylagear::quietNpcUntil`. Shot after two gives (shots/aylagive_after.png): no mail window.
  - Gear town = the town of her current hunting map (lockMap -> town_of_map, else saveMap); a piece is bought only if
    that town sells it (vendor pieces only on a Prontera run); no detour for gear, no friend-steered town. run_end puts
    back only switches still holding the value it set (a mind's `conf` meanwhile wins). Clip [1] -> Belt [1] (2627, NPC
    20k Prontera/Aldebaran; Clip has no NPC seller), Creamy goes into the Belt.
- **Farm, don't wait (2026-11-14 ~19:30Z, owner: "as soon as priest is there, they can already start farming", "farm
  while waiting", "ayla just walking around in circle")**: partylead holds for stragglers only while the healer
  (Priest/Acolyte by `class` from the core API, `healer_near`) is NOT within 15 cells; with it, she farms and the rest
  catch up; pace_ok (sweep steps) also follows only the healer then. She turns back only for a friend hurt / in danger /
  asking for help (no more "not moving / not coming" fetches). `wait_farm`: while she does wait on a field (hold, dungeon
  exit, party rest she doesn't need) she attacks monsters within 12 cells of the wait spot and walks back to it;
  never in town. Sweep: `mob_target` walks toward the nearest monster in sight with no other player within 6 cells, else a
  spot where monsters were seen in the last 2 min, before the fixed waypoint loop. `crowd_check`: 60 s with nothing to
  fight on her hunting map -> "map too crowded: ... pick another map" in the console + `logs/alerts.log` (5 min apart).
- **One hunting map, no shifting (2026-11-14 ~19:45Z, owner rule RULES 3)**: partylead keeps `$homeMap` = the map the
  mind chose last (goto, `rally <town> <map>`, its own `conf lockMap` seen while not holding, a lockMap set during a
  rally). restore() and the post-rally departure go back to homeMap, never to a stale lockMap from an older hold. The
  plugin starts no destination change of its own: back_tick no longer calls a town rally or walks out of the dungeon
  for a respawned member (she says "X come back to <map>, kafra to <town>, we wait here" and waits farming; strong party
  or healer present: no wait at all); a friends' "meet at the Kafra in X" is joined only when she is not on her hunting
  map. Self-started trips left: the real-need town run (returns to homeMap) and the goto save stop.
- **Renewal maps in OpenKore (2026-11-14 ~20:30Z, Ayla stuck in Izlude)**: the OpenKore image's fields/portals for
  izlude were the classic layout (268x268) while the server runs Renewal Izlude (268x300): every getRoute in it failed.
  A check of all 1,296 maps in the server map caches (db/map_cache.dat + db/re/map_cache.dat, re wins) against
  fields/*.fld2.gz found 13 mismatches: izlude, morocc, alberta, iz_ac01, iz_ac02, iz_int, prt_castle, sec_pri,
  int_land(01-04). New fields generated from the map cache (gat 0->1 walk, 1->0, 2/4->1, 3->5 water, 5->10 cliff;
  mapping checked on payon/prontera/gef_fild10/pay_dun03) are in `openkore/fields-re/`, copied into the running
  ro-openkore-ayla (docker cp; .dist/.weight deleted to rebuild). `openkore/fix_re_portals.sh` rewrites the izlude
  lines of tables/portals.txt to rathena npc/re/warps/cities/izlude.txt (Kafra 128,148, exits 20,98 / 110,182 /
  52,172, ship landing 195,212); applied live + `reload portals`. Offline A* test on the new fields: izlude 195,212 ->
  20,98 (235 steps), 128,148 -> 110,182, morocc 156,97 -> 160,260 all route. routeweights.txt: izlude 500 (was 10000
  for an hour). Persisted in `openkore/Dockerfile` (COPY fields-re + the portals patch) - takes effect on the next image
  build; a container recreate before that loses the docker-cp fix. Open: `portals recompile` (LOS for the new izlude
  portals) blocks OpenKore for a while - run `ayla.sh cmd portals recompile` once while she's safe in town.
- **RODEX read is broken on this PACKETVER; help calls really fetch (2026-11-14 ~20:50Z)**: `rodex read` answers with
  0x0B63, unknown to this OpenKore ("Packet Parser: Unknown switch: 0B63"), so aylagear's mail step 3 timed out every
  3 min and lunaqueen's Creamy Card (accepted 19:39 for 8,700z) sat in the mailbox. aylagear no longer reads mails: the
  list gives sender / title / attachment, the price is the whispered deal (%offers, rebuilt from deals.log ACCEPTED
  lines after a restart); `rodex getitems <id>` (1-3 digit ids by list index), pays only after the item count rises,
  logs TAKING / GOT / FAILED / LEFT. Mails with items from a party friend = gifts; anything else without a deal stays.
  partylead: heard() no longer answers a help call with "omw" (that promise came with no action); a help call counts
  in a strong party too (she goes; only a merely-far friend comes by itself); the needy list puts the healer first;
  cross-map help fetches work in a strong party; goto_tick and off-map routes wait while a friend on her map called
  for help / is in danger (help_pending). The "til ~16:50" plan line is the ayla mind's text: the containers run UTC.
- **Gear turn, storage, portal recording, help vs goto (2026-11-14 ~21:00Z)**: aylagear asks the core
  `/api/party-turn?human=Ayla` (cached 60 s): while the `gear_pool_turn` member's piece is unpaid (have < price) Ayla buys
  nothing for herself (next_piece returns nothing, new #trade offers get "not now, saving up for a friend's gear") and
  each town run mails that member the shortfall +5% (keeps 5k; once per member per 30 min; TOPUP line in deals.log).
  items_control.txt: her worn / planned gear (Boots, Manteau, Belt [1], Creamy Card, Katar, Coat, Helm, ...) is
  `0 0 0` - `all 0 1 0` had made storageAuto put the new Boots in Kafra storage; a plan piece found in the storage list
  counts as owned (never re-bought) and the next town run takes it out (getAuto_0) for equip_tick. `portalRecord 0`
  (it rewrote portals.txt on every warp and recompiled LOS ~30 s, freezing her; payon_in01's field equals the server's -
  the getRoute errors came from standing on its 26,115 internal warp cell). partylead: the goto branch no longer
  returns before the hold logic while a friend here called for help (help_pending), "21 on me" / "cant find u" count as
  danger, the fetch uses the friend's live map, and goto_tick does not pull her out of town during the post-run Kafra meeting.
- **ginvite by name (2026-11-14 ~21:20Z)**: guildrecruit.pl `ginvite <name>` resolves the account id from a player in
  sight, else `persona.account_id` of `/api/bots/<name>` (:8090, then :8091); it prints "ginvite SENT to X (account N)"
  and, from ZC_ACK_REQ_JOIN_GUILD 0x0169, "ginvite RESULT for X: ACCEPTED - joined Starveil / declined / already in a
  guild / guild is full" (tested: LilRyuheart -> "already in a guild" - it is in Starveil's member list).
  `ginvite <account_id> [name]` still works. fix_re_portals.sh writes no Kafra lines (fix_kafra_portals.py owns them and
  runs after it in the Dockerfile); `portalUpdatePosition 0` + `portalRecord 0` in Ayla's config.
- **Death handling (2026-11-14 ~21:45Z, Ayla died 20:14 in pay_dun02, dead on screen 70 s, then idle 4 min in Payon)**:
  partylead and aylagear do nothing while `$char->{dead}` (their attack/route/fetch calls displaced OpenKore's `dead`
  task, processDead re-queued it ~40 times and the 4 s respawn timer kept restarting); on death she says once "argh
  died, respawning in <save>, regroup at the kafra"; the town run after a death says "i died and respawned here, regroup
  at the kafra" (not "bag full"). In town a sitAuto no longer blocks storage/sell/buy (she sat 4 min at the respawn
  point). No looting with 3+ aggressives or < 60% HP; protect_tick finishes the current target and adds no new monster
  while she is under 50% HP or has 3+ on her. config.txt: `useSelf_item Yellow Potion, Orange Potion, Red Potion`
  at hp < 50% (timeout 0.5, notInTown 1 - she drank 22 Red Potions standing in town), `buyAuto Orange Potion` (40, 50z),
  `teleportAuto_hp 35`, `teleportAuto_minAggressives 6` / `InLock 7` (was 10% / off: teleport only at 3% HP).
  Not done: waiting for a priest's Resurrection before respawning (no res AI known on the Legion priest).
- **partylead 20:20-20:41 (container clock UTC) fixes**: one fetch target is kept until reached (<= 4 cells), gone, on
  another map or 2 min (its last known cell bridges API gaps; was 8 changes in 2 min); "hurt" = real HP < 60% (party list,
  else the core API hp "x/y"), a "N on me" line counts only under 80% HP; the fetch log names its own reason; status
  prints the target's name. A goto issued in town with the party scattered first rallies at that town's Kafra (the plan's
  "meet at the kafra"), then departs together. A single friend's "overweight / bag full" is its own errand ("butterfly to
  town and sell, we keep farming"); 2+ within 5 min make a party run; the run's line gives the real reason (friend names,
  "i died ...", "restock" under 45% weight). A friend already on her destination map counts as ahead (no hold by the
  portal); back_tick only reacts to a fresh core-API town map (stale party-list maps had sent friends standing on
  pay_fild10 to Payon) and its "come back" names her hunting map. aylagear: one top-up per member until the topped-up
  piece is in that member's inventory (core /api/bots, logs/topups.json), and 10k (or her restock cost) always stays.
- **items_control names, wings, gifts, pkt2021fix (2026-11-14 20:50-21:05 container time)**: OpenKore ignores numeric
  item-id lines in items_control.txt, so `all 0 1 0` stored her Fly Wings (death to Eddga 20:49, no wing to teleport)
  and other kept items: 1,311 id lines rewritten to names (from OpenKore's %items_lut dump, slots as "Name [n]"),
  consumables (Fly / Butterfly Wing, potions, gems, Magnifier, Arrow) `0 0 0`. config: `buyAuto Fly Wing` min 2 / max
  10, `getAuto Fly Wing` min 3 / max 10, `teleportAuto_useSkill 0` (no Teleport skill: Fly Wing directly). aylagear:
  `giftItems "Item=Friend; ..."` - fetched from storage on the next storage visit (getAuto_2) and mailed with the item
  attached (`rodex add`), counted as that friend's turn help (Great Bow [3] -> xXRinfangXx). In transit (travel mode)
  no loot (itemsTakeAuto / itemsGatherAuto 0) and protect only within 5 cells. A mind's goto with the party scattered
  rallies at the current town, or the destination's town when she is on a field. homeMap always follows a non-empty
  lockMap. pkt2021fix.pl (from openkore-qa: RefineUI 0AA2/0AA3, NPC market 09D5/09D6, RODEX read 0B63) is in her
  plugins + loadPlugins_list and in openkore/plugins-extra (Dockerfile COPY to plugins/extra). Mail with zeny from a guild
  mate collected 20:53:32 ("GOT zeny by mail from LilGhostgirl").
- **10k zeny floor (owner rule RULES 5, 2026-11-14 ~21:15 container time)**: aylagear `FLOOR => 10000`, `can_spend($cost)`:
  `give` refuses ("give BLOCKED: Ayla must keep 10k ..."), the give queue drops gifts / top-ups and holds seller
  payments (HELD line, 3 retries, then UNPAID in deals.log) when the mail + fee would leave her under 10k; #trade offers
  are accepted only above it; vendor buys and the gear slot (`buyAuto ... zeny >= price + 10000`) too; top-ups keep
  max(10k, restock cost). cmdfile.pl blocks `rodex setzeny N` and `deal add z N` that would go under 10k ("BLOCKED ...
  blocked: Ayla must keep 10k"). Potion / wing restock stays allowed. Tested live at 5,399z: both guards fired.
- **Gift by trade, item cache, safe reloads (2026-11-14 ~21:20 container time)**: aylagear gives a `giftItems` piece by
  a direct trade (deal: open, add the item, OK, trade; no zeny, no fee) when the friend is within 12 cells - the Legion
  friend accepts its party human's trade, walks next to her and confirms; by mail only above the 10k floor. Deal windows
  are kept off the watch client. Item lookups are cached in `openkore-ayla/logs/itemcache.json` (pre-filled for her plan)
  and at most one uncached lookup per second - after a reload the plan was re-fetched in the main loop (likely cause of
  the 21:14 "Timeout on Map Server" kick). partylead: "hurt" = under 60% HP only (a help line at 79% is "asked").
  **No tests on the live Ayla** (use openkore-qa) and **reload only when she is idle in town**:
  `tools/rogui/ayla_safe_reload.sh PLUGIN...` waits for that (and for no rally/departure before partylead), one plugin
  every 30 s.
- **Escape fix (2026-11-14 ~21:30 container time)**: OpenKore's teleport task only runs while it is the CURRENT AI
  action; attack / route / loot queued on top (partylead protect / farm / fetch, attackAuto) left it waiting ("NPC or
  Teleport in queue", 43 s at 21:22:55, and the Eddga death). partylead now, while a teleport is queued, clears
  attack / route / move / take / sit actions above it and does nothing else until it is done. Escape thresholds are set
  per tick: with the party's healer on her map `teleportAuto_hp 30`, no aggressive-count trigger, plus a plugin escape
  at HP < 45% with 4+ aggressives (AI::ai_useTeleport(1)); alone `teleportAuto_hp 35`, `minAggressives 6` (InLock 7);
  MVPs/bosses keep mon_control's teleport-on-sight.
- **Town-run return + hold fixes (2026-11-14 12:40Z, lessons.md)**: kafra.go - a teleport for a town trip or by a
  human leader's member sets kafraNext +15 s (was 2 min: quick shoppers walked home 6 maps). humanparty.go humanState
  prefers Ayla's status.json position when newer than humanLocs. partylead.pl: a friend > 20 cells for 60 s is
  collected ("is not coming"); %helpFrom expires after 2 min and `goto` clears it; `partylead reload-cmdfile` reloads
  cmdfile.pl safely. cmdfile.pl blocks a party line naming a different meeting town within 10 min of the last.
  items_control: Phracon/Emveretarcon stored, not sold. Core changes BUILT, not deployed.
  Grinding area (12:43Z): partylead.pl anchors her grind (AREA_R 25, slow drift, checkMonsterAutoAttack skips far /
  near-portal unprovoked targets); PORTAL_KEEP 9 covers the pay_dun00 east stairs (184,33, x 182-186 y 26-40).
  REPLACED 14:00Z by a sweep loop (20-cell grid of open cells, nearest-neighbour loop, driven by the random-walk hook)
  and 'rest together' (she sits while a friend rests on its own; no collect for 'not moving/coming' during a rest).
- **Ayla's SP / breather** (2026-11-14): config.txt Sonic Blow only at SP > 40% (2 extra blocks: > 25% with
  `aggressives > 2` or a tough-monster list), sitAuto SP 10/40, HP 50/80, Red Potion at HP < 35%, SP items at < 12%.
  partylead.pl own_rest uses the same numbers; breather_line() says "sitting a sec, keep killing near me" (must keep
  matching together.go humanRestLineRe). Core together.go restFarmFor/restGuard 1b: while the party leader (not a
  healer) rests, non-healer guards fight any non-boss mob within restFarmCells (15) of it.
- **No split rallies for Ayla's party** (server or plugin): friends travel to her map on their own; a friend calling for
  help from a map of the same area (next floor/field) is fetched by her (`$fetchMap`, one trip, 3 min max).
  Rally protocol lines are lowercase and varied; parsers accept "meet|regroup|gather ... kafra in <town> (x,y)".
- **Far members regroup first** (party.go): beyond together_cells a member regroups even while a walk is in progress
  (the old `!moving` test let it fall through to fighting); a bot party's member on another map than the leader
  travels there; "waiting (coming to get me)" ends after 30 s unless the leader got 5+ cells closer; the leader
  finishes a collect before new fights (ai.go hunt), fighting only what hits it.
- **Bot-leader collect** (ai.go): only members beyond together_cells (20); the member being collected
  (`pubState.Collect`) waits instead of walking toward the leader, so only one side moves.

rAthena settings that matter: `conf/import/battle_conf.txt` `base_exp_rate: 1000`, `job_exp_rate: 1000` (10x);
`conf/import/inter_conf.txt` `party_share_level: 99`; battle conf `mob_count_rate 200`,
`guild_emperium_check: no`; `npc-custom/legion.txt` (job changes, auto skills for `legion_autojob` chars incl. Ayla,
one-time starter kit `legion_starter`).

---------------------------------------------------------------------------------------------------------------------
