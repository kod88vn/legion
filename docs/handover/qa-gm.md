> **Reference only: OpenKore era; current state is in HANDOVER.md and STATUS.md.**

<!-- Moved unchanged from HANDOVER.md lines 1555-1606 (as of commit 63ca923). Index: HANDOVER.md. -->

## 6j. Travel/services QA: GM test character (2026-11-14, owner: "Use a GM account with unlimited money to move around maps and use all service to make sure everything works"; RULES.md 1)
- **Account** `qatest`/`qatest` (account_id 2000002, group 99 = GM; outside the bot range 2100001+, not the owner's
  `player`), created like the seed does (rows in `login` + `char`). Characters: **Trailmark** (char_id 200100, slot 0,
  the tester; ~10M zeny, `@hide` on - `@hide` TOGGLES, check "Invisible: On") and **Trailpost** (200101, slot 1, Novice,
  only a RODEX receiver). Never chats, never parties, keeps out of the economy; levels/jobs are changed freely with
  `@blvl/@jlvl/@jobchange/@allskill` (keep base level modest so it doesn't sit on the leaderboard).
- **Container** `ro-openkore-qa` = compose service `openkore-qa`, profile **qa** (a plain `up` never starts it), same
  image as Ayla, `network_mode: service:rathena`. Start/stop: `docker compose --profile qa up -d --no-deps openkore-qa` /
  `docker stop ro-openkore-qa`. Its entrypoint copies `openkore/fields-re/*.fld2.gz` over the image's fields and
  `openkore-qa/tables/*.txt` (portals.txt / npcs.txt copies) over the tables, then runs the normal entrypoint; a changed
  portals.txt costs a ~4 min LOS compile at start. Control dir `openkore-qa/control` (Ayla's config with XKore 0,
  username qatest, char 0, no attack/loot/auto-buy/party, portalRecord 0, portalUpdatePosition 0), plugins
  `openkore-qa/plugins/qa` (cmdfile + pkt2021fix).
- **Driving it**: `bash openkore-qa/qa.sh [-w SEC] "cmd" ...` appends OpenKore commands to control/commands.txt and
  prints the new log lines (`c @warp map x y`, `move map`, `talknpc x y <seq>`, `storage ...`, `rodex ...`). Send one
  command at a time when it matters: cmdfile can drop a line appended while it truncates the file.
  `openkore-qa/kafratest.sh` warps next to every teleporting Kafra and takes its first/last destination (39/39 ok).
- **Root cause of "Ayla can't reach the Orc Dungeon" (fixed)**: OpenKore's tables/portals.txt carried the official
  kRO Kafra destination lists; our Kafras use F_KafSet (rathena/npc-custom/functions_kafras.txt). Prontera Kafra here =
  Izlude, Geffen, Payon, Morocc, **Orc Dungeon**, Alberta; OpenKore had r4 alberta / r5 aldebaran / r6 comodo / r7
  umbala and no Orc Dungeon line, the Izlude Kafra "r0 prontera" (real: Geffen), Juno Kafra einbroch/lighthalzen/...
  (real: only Al De Baran), a Prontera Kafra at 35,208 (real 29,207), etc. Ayla's rally to Aldebaran took the Prontera
  Kafra "r5" and landed in Alberta, then her route to Geffen went by ship into (then broken) Izlude. Fix:
  `openkore/fix_kafra_portals.py tables/portals.txt [functions_kafras.txt]` regenerates every Kafra teleport line from
  the server data (idempotent, owns ALL Kafra `c r2 c rN` lines; other patch scripts must not write Kafra lines and run
  before it). Applied live to Ayla 2026-11-14 19:57Z (`reload portals` + `portals recompile`), and in openkore/Dockerfile.
  Verified: Payon -> orcsdun01 52 s, Prontera -> orcsdun01 16 s (Prontera Kafra -> Orc Dungeon), Prontera -> Al De
  Baran, Al De Baran -> Juno, Juno -> Morocc, Morocc -> pyramid / sphinx, Izlude -> prt_fild08.
- `portalUpdatePosition 1` (not portalRecord) made OpenKore rewrite portals.txt whenever a warp landed a few cells off
  ("[processPortalRecording] Updated portal coordinates") and forced a LOS recompile; set to 0 on Ayla and QA.
- **OpenKore packet layouts that are wrong for client 20210107** (found with the QA char; plugin
  `openkore-qa/plugins/qa/pkt2021fix.pl` fixes them at runtime): RefineUI (0AA2 materials / 0AA3 request use 4-byte
  item ids: without it every refine was refused locally), NPC markets like Dietrich's ores (09D5 / 09D6, 4-byte ids: the
  list showed garbage prices), and **RODEX read 0B63** (OpenKore only knows 09EB: "Unknown switch: 0B63", so a read mail
  is never "current" - getzeny/getitems refused and Ayla's aylagear `rodex_mail` hook never fired: "mailbox: no answer
  at step 3"). With the plugin: refine +1 Katana with Phracon, Phracon bought from Dietrich, zeny and items taken from
  mail. Ayla needs the plugin in her loadPlugins_list to read mails (main decides).
- Services table (QA 2026-11-14): see the travel-QA report in the session; every Kafra teleport, Kafra save/storage,
  Butterfly/Fly Wing, NPC buy/sell, Peco breeder, Alchemist Union guide + bowls, Izlude Hypnotist skill reset, RODEX
  send/receive, guild create work; OpenKore `guild break` gets no answer (use `@breakguild`); guild invite not tested
  (needs a second online character).
- **Field files vs the server's map cache**: `openkore/gen_fields_from_mapcache.py` (run in an OpenKore container with
  the server's db/map_cache.dat + db/re/map_cache.dat) found 97 OpenKore maps whose walkable cells differ from the
  server (prontera 1,286 cells, yuno_fild02 669, aldebaran 137, comodo 112, yuno 53, moc_fild08-22, anthell01/02,
  izlude_in, geffen 8, ...; the RE cache overrides alberta/izlude/morocc/prontera/prt_church/prt_fild05/prt_fild08/
  prt_in). Server-exact files are in `openkore/fields-srv/` (fields-re = the plugin engineer's 13 maps, same source).
  Not yet applied to Ayla or the QA overlay: 4 Prontera walking tests with the old prontera field had no "Stuck", so
  apply them when a stuck/route bug points there (copy into /openkore/fields, delete that map's .dist/.weight, reload).
- Observation for the economy: rAthena's RE `Tool Dealer#Extended_*` shops (npc/re/merchants/shops.txt, e.g. morocc
  146,103, prt_in 126,76, payon_in01 5,49, yuno 218,97) sell White Potion 1,200z, Yggdrasil Leaf 12,000z, Blue Gemstone
  450z, Awakening/Berserk Potions next to the classic Tool Dealer on the same cell.

