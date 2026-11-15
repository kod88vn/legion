# Party-logic fixer: restart notes (2026-11-14, ~16:45 local)

Role: engineering subagent for bot-party / Ayla's-friends behaviour in `legion/` (together.go is "mine"; also edits in
party.go, ai.go, humanparty.go, kafra.go, heal.go, storage.go, town.go, danger.go, api.go). Owner rules: server/RULES.md
section 3. Lessons written to `legion/data/lessons.md` (tags TOGETHER-RULE ... HEALER-ANCHOR).

Current process rules (coordinator): never run `docker compose build`; never start golang test containers; only the
deploy agent runs vet/test/builds. Fixers hand it "code ready". Respect code freezes (no legion/*.go saves while the
deploy agent snapshots).

## 1. Code-ready (in the tree, handed to the deploy agent, NOT verified by me - my last vet/test runs never finished)
Written after the 14:53 deploy (`ro-legion:deploy-20261114-145352`, which already contains escortGate,
protectSupport, farmNear, humanSaveTown):
- heal.go `partyAnchor`: a healer anchors on the party tank, then the leader, then the nearest member on the map (stops
  random-cell Teleport escapes; Zendancer84 19:43-19:56Z). Before: 11 healer teleport/fly escapes / 30 min (4 healers).
- heal.go `rejoinSay`: in a lv50+ party "come get me" only when the leader is on another map or > 40 cells away.
- humanparty.go: bogus 65535 party-list positions ignored; strong party follows Ayla's actual map (lock-map shortcut
  removed; weaker party still waits at the lock map's Kafra town); `humanTrail` - a friend on a map she crossed goes to
  the map she went to next (same portal). Test `TestHumanTrail` (together_test.go) - not yet run.
- together.go `escortGate`: holds friends in the entrance town only when her map is a dungeon (`dungeonMap`, by name).
Ask the deploy agent to include `TestHumanTrail`, `TestSupportNeed` in its run.

## 2. Prepared, NOT applied (code freeze at the time): never strand a member
Script: `G:\RO\server\RESTART-NOTES\party_strand_edit.py` (copy of the scratchpad original).
Apply AFTER the deploy agent reports main+core deployed / freeze lifted: `python G:\RO\server\RESTART-NOTES\party_strand_edit.py`
(targeted replacements with exact-match asserts; it aborts without writing if a target changed - then re-apply by hand).
It does:
1. danger.go (both Butterfly escape branches) -> `leaderFledCall`: a bot party leader that wings to town calls a party
   town run to its save town + party chat "had to wing out, everyone to <town>, meet at the kafra".
2. ai.go after `partyGoalSync` -> `leaderSyncPartyMap`: a bot leader whose own hunting goal != party map moves the
   party there (planLine announcement + `moveTo`), max once/min.
3. party.go `partyGoalSync`: a healer, or a member on a dungeon/hostile map, follows the leader's current hunting map
   after 30 s (not during the leader's town trip).
Evidence (diaries): Neko_z winged out 20:54:52, Mint chan alone on pay_fild10 died 21:11:20; LilEchodancer on
pay_fild10 while priest silverdragon stayed in pay_dun00/01, died 21:21; Thornsong alone on pay_fild10 while Zero monk
pt was on prt_fild09. Then: hand "code ready" to the deploy agent, add a lesson (tag NEVER-STRAND).

## 3. Open items / not done
- Ayla gear plan (partylead.pl gearPlan_next empty after Cap): next pieces Chain Mail / Manteau / Boots at aldeba_in
  20,60 (no Prontera NPC); Katar / Clip[1] drop-only -> market API purchase step; `ayla.sh give NAME ZENY` (RODEX) - plugin
  has no mail/trade code. Suggested for the Ayla-plugin engineer.
- partylead.pl patches I wrote (rally ignores party fights, goto releases holds/town gates, strong-party no-fetch,
  travel mode, departure waits for friends' saves) - reload status unknown; the Ayla-plugin engineer may have merged/
  rewritten them. statusout.pl fields added: `sitting`, `save_map` (need `plugin reload statusout`).
- Ayla teleport config: set `teleportAuto_atkMiss 0`, `teleportAuto_maxDmg 0` (keep hp 10, deadly 1) - not applied by me.
- Kafra storage (storage.go) untested live: watch for "stored N kinds of items in Kafra storage" events.
- /api/savecheck: mismatched save points; target < 5% (was 130/865 at 14:00).
- Not mine but seen: headgear equip flapping (xXRinfangXx), guild/gear pools spending travel money, prt_fild06 23,193
  warp-loop ban blocking Prontera west exit, songs while heavy.
