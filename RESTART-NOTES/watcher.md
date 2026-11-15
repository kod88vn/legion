# Ayla party watcher - restart notes (saved 2026-11-14 ~21:38Z, end of run 6)

Role: read-only watcher of Ayla's party (RULES 0 "party-watcher agent"). Never sends anything to Ayla's OpenKore
(no ai / ai off / ai manual, no map/rally/party orders). Reports every bug to main via SendMessage with times, names,
log lines. Heartbeat every <=4 min: `cd G:\RO\server && python tools/legionctl.py think watcher "cycle N ok: ..."`.

## Tools (copied to `tools/watch/`, run from WSL: `wsl -d Ubuntu-24.04 -- bash /mnt/g/RO/server/tools/watch/<x>`)
- `mon.sh` - event stream for a Monitor: filtered console_player_2.txt lines (deaths, processDead, argh, DEAL/TOPUP,
  town run, come back to, crowded, Teleporting/escaping/queue, Fly Wing, Orange Potion, goto/rally/fetch, help calls,
  whispers, disconnects, equip/storage), deals.log, and every 180 s `KILLS last3min=N` + a party SNAP line.
  Monitor command: `export MSYS_NO_PATHCONV=1; wsl -d Ubuntu-24.04 -- bash /mnt/g/RO/server/tools/watch/mon.sh`
  (max 30 min per Monitor; re-arm). After TaskStop, kill leftovers: `pkill -f tools/watch/mon.sh; pkill -f "tail -n0 -F /mnt/g/RO/server/openkore-ayla/logs"`.
- `snap.py` / `party.py` - piped into `docker exec -i ro-openkore-ayla python3 -`: party positions/distances/HP/state
  from core API 127.0.0.1:8091/api/human-party?human=Ayla (party.py = full table, snap.py = one line).
- `diary.py` - core friends' diaries (`docker exec -e SINCE=20:15 -e NAMES=xXRinfangXx -i ro-openkore-ayla python3 - < diary.py`).
- `waitmap.sh MAP SECS` - waits until status.json map == MAP. `waithelp.sh SECS` - waits for the next friend help line.
- Gotcha: double quotes inside `wsl ... bash -c '...'` get stripped by Windows arg passing - put greps with quotes in a script file.

## Party state at 21:37Z
All 7 together on pay_dun01 (Ayla 260,35, friends 2-14 cells), heading to pay_dun03 (mind plan 21:07: "cave 3 for 2 hours",
saved at payon Kafra 21:09:14, verified). Ayla: lv80, zeny 5,399 (below the 10k reserve: no gifts/top-ups possible),
Fly Wing 20 / Orange 40 / Red 38 in bag (21:06), Boots equipped. Kills ~1-4 per 3 min all evening.

## Open bugs (all reported to main)
1. Ayla wanders pay_dun01 12+ min (21:21-21:37): "standing still too long, stepping to ..." every 40-60 s, lockMap route
   to pay_dun03 recalculated repeatedly; partylead "waiting for Zen.fox 39-62 cells" while core says 3 cells (stale distance?).
2. Ayla map-server disconnects: 21:14:46 (Timeout on Map Server, 10 s after a live cmdfile plugin reload + give test)
   and 21:35:30 ("Disconnected from Map Server", cause unknown; back ~1 min later each time).
3. Escape latency: teleportAuto trigger -> Fly Wing took 43 s (21:22:55 -> 21:23:38); at the 20:49 Eddga death the
   teleport stalled ("NPC or Teleport in queue"). Escape fix pending reload (safe_reload.log existed but empty at 21:38).
4. items_control.txt numeric-ID lines (601 Fly Wing, 602 Butterfly Wing, 501 Red Potion...) are ignored -> `all 0 1 0`
   stores them at every Kafra storage visit (all 10 Fly Wings stored 20:21/20:30 -> no escape -> Eddga death). Needs
   name-based lines. Confirm with main whether fixed. (Boots/gear storage was fixed + verified 20:22.)
5. Death flow: 20:49 death had 2 processDead lines and 33 s to respawn (target 1 line, ~4 s). PASS: no partylead while
   dead, "argh died..." line.
6. "hurt" reason given at 79% HP (Zen.fox 21:12:27) - should be <60%.
7. Mind's "regroup at payon kafra" text not turned into a rally (20:55); fixed by the mind using `partylead rally` at 21:07.
8. Friends route via town while Ayla routes direct (3 splits: 20:24, 20:32, 21:04); "come back to pay_fild09, kafra to
   payon" sent to friends standing in payon.
9. Template lines that lie: "nice one" with no kill, "ok resting a bit" at full HP/SP, "staying here, exp is good" while
   passing through, "bag full" when weight 26%.
10. Gear turns: Rinfang got 14,170z (Claire Suits) + 13,853z (Crossbow, topups.json) but bought Mantle+Bandana; he wants a
    Great Bow - Ayla has a Great Bow [3] in her Kafra storage (stored 20:21:44).
11. Map choice: pay_fild10 has Eddga (MVP) - mind called it "all passive". Mind changed destination 5 times 20:20-21:07
    (each announced).
12. Watch client (display :98) down/black since ~21:09Z ("login failed", wined3d context errors); crashes every ~30 min
    with GL_OUT_OF_MEMORY.
13. crowd_check "map too crowded: nothing to fight 60s, 0 other players" fires while the party is holding (misleading).
14. #trade: many bots "cant find a single Orange Potion in pront" (market supply gap); "B> aHt" scrambled item names.
15. LilRyuheart was promised a Starveil invite at 19:47 but `ginvite <name>` needs an account id - never invited.

## Fix-verification checklist (next run)
- Escape fix after safe reload: "Queueing teleport task" -> Fly Wing within ~1 s, no "NPC or Teleport in queue";
  with healer on map OpenKore teleport only <30% HP, plugin "[partylead] escaping: ..." <45% HP with 4+ mobs; alone
  <35% or 6+ mobs; MVP/boss immediately.
- Death flow: exactly one processDead, respawn ~4 s, no partylead lines while dead, "argh died", Kafra regroup.
- Orange Potions used under 50% HP (Yellow > Orange > Red); no potions in town. (Not yet seen.)
- No looting with 3+ mobs / <60% HP (PASS 21:22:57). One fetch target until reached/2 min (PASS 21:11-21:13).
- Enters destination map when friends are ahead; no "come back to <map>" to friends on the hunt map.
- Town runs only when 2+ friends need it (single friend goes alone); real reason in announcement.
- One top-up per friend until bought (logs/topups.json; 10k reserve PASS 21:14:40).
- Kills/min on the hunting map once arrived; crowd_check false alerts.
