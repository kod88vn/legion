# Ayla-plugin engineer: restart notes (2026-11-14, ~21:40 UTC container time)

Role: improve the OpenKore plugins that play Ayla (account `player`, Assassin lv80, container ro-openkore-ayla).
Reports go to main. Never send `ai` / `ai off` / `ai manual`, never weaken the cmdfile.pl guard, never steer Ayla
(no move/map/rally commands from engineering), no tests on live Ayla (use openkore-qa). Plugin reloads are allowed;
an OpenKore restart needs main's OK. Ayla keeps at least 10k zeny (strict floor, FLOOR constant / can_spend).

## Files
- Plugins: `server/openkore-ayla/plugins/cmdfile/` - partylead.pl (party leading), aylagear.pl (gear plan, give,
  deals, mail, gifts, popup suppression), cmdfile.pl (command inbox, ai guard, 10k zeny guard), guildrecruit.pl,
  pkt2021fix.pl (RefineUI 0AA2/0AA3, NPC market 09D5/09D6, RODEX read 0B63).
- Config: `server/openkore-ayla/control/` config.txt, sys.txt (loadPlugins_list ...,guildrecruit,aylagear,pkt2021fix),
  items_control.txt, routeweights.txt.
- Logs/state: `server/openkore-ayla/logs/` deals.log, alerts.log, topups.json, itemcache.json, items_lut.tsv,
  safe_reload.log, status.json.
- Tools: `tools/rogui/ayla.sh` (cmd, give, gear, deals, rally), `tools/rogui/ayla_safe_reload.sh [--anywhere] PLUGINS`.
- HANDOVER.md has a bullet for each change; lessons in legion/data/lessons.md.

## LIVE (loaded in Ayla's OpenKore)
- give NAME ZENY via RODEX; gear plan (gearPlan_list), town-of-hunting-map buying, gear turns via core
  /api/party-turn, one top-up per member (topups.json), 10k floor on every outgoing zeny path.
- #trade deals from humanwant whispers (Creamy Card bought 8,700z at 19:53:52).
- Mail handled list-based; popups suppressed (rodex, vender, deal, NPC/storage during auto runs, Kafra save).
- Storage-held plan pieces fetched via getAuto.
- partylead: rally replace / rally <town> [map], lockMap guard, verified Kafra save, help-call fetch, wait_farm,
  sweep, crowd alerts, death handling, no loot under pressure, town-run reasons/deferral, hold hysteresis.
- Config: potions hp<50% notInTown; buyAuto Orange Potion 40, Fly Wing 2/10; teleportAuto_useSkill 0; saveMap payon.

## BUILT, NOT LOADED (syntax OK, waiting for a safe reload)
1. **Escape fix (safety-critical), partylead.pl**: root cause = OpenKore's teleport task only runs when it is the top
   AI action; attack/route/loot queued above it stalled it ("NPC or Teleport in queue", 43 s stall 21:22:55, Eddga
   death 20:49). Fix: while AI::inQueue('teleport'), clear attack/route/move/items_take/take/items_gather/sitting/
   sitAuto above it. Per-tick thresholds: healer on map -> teleportAuto_hp 30, minAggressives 0, plus plugin escape at
   HP<45% with 4+ aggressives via AI::ai_useTeleport(1) (5 s limit); alone -> hp 35, minAggressives 6 (7 in lockMap).
2. **Gift by trade, aylagear.pl**: giftItems "Great Bow [3]=xXRinfangXx" - direct deal when he is within 12 cells,
   mail only when zeny > 10k. Report to main when "GAVE ... by trade" appears.
3. **Item cache** (itemcache.json pre-filled) in aylagear.
4. **Hurt rule**: friend counts as hurt at <60% HP (help-call fetch).

## Safe reloader status
STOPPED (owner's save request). It never fired: Ayla was in pay_dun01 with Archer Skeletons shooting her, so the
"no monster attacking" condition never held. To resume:
`wsl -d Ubuntu-24.04 -- bash -lc "bash /mnt/g/RO/tools/rogui/ayla_safe_reload.sh --anywhere partylead aylagear 2>&1 | tee /mnt/g/RO/server/openkore-ayla/logs/safe_reload.log"`
(run in background; it reloads one plugin at a time, 30 s apart, at a calm moment: no attacker, HP > 80%, no
attack/teleport/NPC step, holding=0 rally=- depart=-). If her map keeps her permanently under fire, ask main whether
to relax the "attacks you" check to "no attacker in the last ~10 lines".
After loading: verify the first escape - expect "Queueing teleport task" then Fly Wing within ~1 s, and no
"NPC or Teleport in queue". Report timing to main.

## Open items
- **Ayla stuck on pay_dun01**: lockMap route restarts / "standing still too long" every 40-60 s, stale distances to
  friends. Not yet diagnosed - suspect the lockMap guard re-queuing a route while the sweep/mob_target also moves her,
  or partylead using stale actor positions (friends' positions not refreshed when off-screen). Check partylead's
  lockMap guard + sweep interaction and where friend distance comes from (party packet vs actor list).
- **Respawn takes ~33 s / second processDead**: likely the stuck teleport task; re-check after the escape fix loads.
- Ayla zeny ~5,400 (below the 10k floor): no outgoing money until she earns more.
- Watch the Great Bow trade to xXRinfangXx (after aylagear reload).
- Container clock is UTC.
