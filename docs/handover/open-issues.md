<!-- Moved unchanged from HANDOVER.md lines 1607-1659 (as of commit 63ca923). Index: HANDOVER.md. -->

## 7. Open issues / next steps
0. Player economy: BUILT in `legion/economy.go` (fair price standard priceOf, wants/offers posted on #trade,
   econManager matching every 15 s, real trades via the trade window, /api/economy) - needs live verification after
   its first deploy: look for "sold ... / bought ..." market events and trades_total > 0.

1. **Followers losing the leader** (`legion/data/lessons.md`, bugs.md): human-party members learn Ayla's map only
   through `noteHuman` sightings, so they lag when she changes maps; Echobunny got stuck on moc_fild02 for 35+ min.
   Suggested: path to the leader's last known map, time out after ~3 min, fall back to the checkpoint town.
2. **mjolnir_10 death spike** (bugs.md 18:20): ~250 deaths in 15 min; danger logic should move bots off it.
3. **Ayla mind didn't read its inbox** in an earlier run (owner messages stayed unread) - check the `ayla` mind
   actually calls `legionctl orders ayla` each cycle.
4. Fold `legion/data/lessons.md` into the engine regularly (owner's explicit wish).
5. Ideas not applied yet: `research/proposed_builds.md` section D (Earth Spike vs wind, Turn Undead, potion tiers).
6. rAthena restarts disrupt the core party for ~1-2 min; avoid unnecessary ones. Main Legion deploys no longer touch
   it (it runs in `legion-core`); prefer `data/tuning.json` over a deploy for number changes.
8. **Potions / weight / weapons** (2026-11-13, lessons.md): potion targets per class, 45% weight cap, sell surplus at 50%
   (no regen above), SP potions for SP users (tuning keys weight_regen_pct, pot_weight_cap_pct, pot_hp_acolyte,
   pot_sp_target). Open: no NPC sells cheap SP potions (only Blue Syrup at 7,000z), so most SP users carry none; ~80 of
   94 main-Legion acolytes had NO weapon (unusable starter Knife; their "buying Mace" trips timed out). The fixes (weapon
   first, outdoor shop preferred, in-town shop room routing) are in the image but main `legion` still needs a batch deploy.
9. **Gear progression** (2026-11-14, lessons.md, `legion/gearpool.go`): build-aware item scores (casters value MATK,
   archers bows), `invReady()` gate (no trips on a half-loaded login), "no weapon" is a necessary trip at any level,
   party gear turns + zeny gifts between party mates (zeny-only market deal) + planned far gear runs piggybacked on the
   next real-need party town run; `gear_next` on /api/bots/<name> and the party dashboard ("next gear" column, gear
   turn line). Tuning: gear_trip_min_lv 15, gear_trip_cooldown_min 30, gear_pool_min_gift 200, gear_turn_max_min 40.
   Live in legion-core; main `legion` needs the next batch deploy. Unverified: a gift completing through the trade
   window (the market trade code has 0 completed trades so far: check `/api/economy` and "gave/got ...z" events).
   Not built: account pooling (a farmer character funding its alts) - needs Kafra storage support.
7. Owner's client graphics: HD sprites via `sprites_hd2.grf` (DATA.INI order 0=sprites_hd2.grf, 1=rdata_ok.grf,
   2=data.grf); dgVoodoo was removed (`client\dgvoodoo-off`); Magpie (`G:\RO\tools\magpie`) downloaded but untested.

---------------------------------------------------------------------------------------------------------------------
## 8. Quick checks

```
# is everyone up?
wsl -d Ubuntu-24.04 -- docker ps --format '{{.Names}} {{.Status}}'
curl -s http://127.0.0.1:8090/api/overview        # world health
curl -s http://127.0.0.1:8090/api/parties/detail  # parties (Ayla's first)
wsl -d Ubuntu-24.04 -- bash /mnt/g/RO/legion/tools/rogui/ayla.sh log 30     # what Ayla is doing
# watch window frozen/black: restart ai-display.sh, then ai-client.sh (both long-running)
# Ayla not attacking: ayla.sh cmd "conf attackAuto 2"; wrong map: ayla.sh map pay_fild01
```

### Background loops the main thread keeps running (2026-11-14)
- `wsl -d Ubuntu-24.04 -- python3 /mnt/g/RO/legion/tools/sentinel.py` (exits on a problem; restart after acting on it).
- `python tools/unwall.py --loop` (from G:\RO\legion): moves OFFLINE lg* bot chars off blocked cells every 30 s. Walled-in
  bots log out for 3 min so it can fix their row; without this loop they come back on the same cell.
- Deploys: only the main thread runs dep3.sh/depcore.sh (scratchpad), batched; subagents report "built".
  The scripts now live in `tools/deploy/` (section 0.5); a per-batch deploy agent runs them (section 0.4 item 8).
- Deploy cadence (2026-11-14 02:50): every main/core restart makes all bots relog and re-evaluate needs at once, giving a
  ~15 min wave of town/Kafra trips and split parties (QA measured it after each deploy). Now that the big fixes are out,
  deploy at most once an hour, and only when a batch has real fixes; critical crash fixes are the exception.
