# Class-QA agent: restart notes (saved 2026-11-14 16:40 local, end of run 7 start)

Role: standing class-QA agent (RULES.md 0). It's read-only: it observes, reports bugs to main (SendMessage), and posts a
heartbeat every ≤5 min with `python tools/legionctl.py think qa "<status>"`. It never sends commands to bots and never
touches Ayla's OpenKore.

## How to restart
- Scripts are in `tools/qa/` (copied from the session scratchpad). Run them with `python -I` from a working dir that holds
  `qa2_events.jsonl` (`qa2_evlog.py` writes it next to itself), or copy them to a scratch dir first.
- Loggers (one copy each, all stopped at 16:40):
  - `qa2_evlog.py`: /api/events -> qa2_events.jsonl
  - `qa3_gl.py`: guild leaders -> qa3_gl_log.jsonl
  - `qa3_ayla.py`: Ayla party positions
  - `qa6_lat.sh`: API latency every ~3 min -> qa6_lat.txt
- DB scripts (`*.sh`) run inside WSL: `MSYS_NO_PATHCONV=1 wsl -d Ubuntu-24.04 -- bash <path>`. Git Bash has no docker.
  Several `.sh` files hard-code the old scratchpad path for output; edit `O=` / the paths before reuse.
- Event times are UTC (local = UTC-5). DB picklog time is UTC+1 (DB now() 21:25 when UTC was 20:25).

## Useful scripts
- `qa3_metrics.py START END`: party runs, Kafra teleports (and A->B->A), deaths, wings per window.
- `qa5_deaths.py START END`: deaths/min, total and party members. Death events are double-counted ~18%, see below.
- `qa6_wings.py START END`: wing bursts (bots with ≥6 wings in 2 min).
- `qa5_flip.py`: destination flips.
- `qa5_idle.py`: bots idle with 0 kills, under 30 cells moved, over 2.5 min, per map. Heavy on /api/bots.
- `qa2_loop.sh` / `qa2_cycle.py`: one-bot-per-class sample with flags.
- `qa2_heavy.py`: bots at 50%+ weight tracked over ~4 min.
- `qa5_bowless.py`: bow line without a bow, or without arrows.
- `qa3_aoe.py`: Mages/Wizards without AoE.
- `qa3_snap.py`, `qa3_crowd.py`: strong parties on beginner maps, leader states, crowding.
- `qa3_glan.py SINCE`: guild-leader bounce, travel and offline.
- `qa5_peco.sh`: lv40+ Knights/Crusaders mounted (option&32), with Riding, with zeny.
- `qa5_tele*.sh`, `qa5_belt.sh`: Creamy Card / Clip / Belt ownership.
- `qa6_db2-4.sh` + `qa6_gear.py` / `qa6_gear2.py`: gear audit from DB plus rathena item_db yml. Dumps go to `itemdb/`.
- `qa7_deaths.py`: pay_fild03 deaths with the 6 preceding events.
- `qa7_db7.sh`: picklog mobs per map, which shows monsters not in the spawn files.
- `qa7_db8.sh`: metamorphosis skills.

## Run 7 verification list (the deploy/rollback restarted Legion at 16:35; NOT verified yet)
1. API latency: /api/bots and /api/overview were still 30 s timeouts at 16:38, 3 min after the 16:35 restart.
   - /api/bots/<name> also hung from 16:20 before the restart.
   - Needs the LFG lock fix.
2. LFG fixes, all baseline-broken before:
   - beginner party maps near home: kirafire (Novice) led Payon kids to prt_fild06;
   - escort timeout: kirafire/Mochiboy deadlocked for 25+ min;
   - no town as hunt map: xXLilybunnyXx prontera, Sky_mage26 payon, "auto (nowhere reachable)";
   - turn purchases.
3. Magnifier + identify, accessory buying. Gear audit baseline below; 0 identify/Magnifier events in the log before.
4. Restock ≤40% weight.
5. Champion thresholds (build agent's new rule). Baseline: about 1,000+ "Ringleader ... keeping away" per hour (Fabre
   Ringleader lv6 avoided by everyone; Swift Creamy avoided by a lv46 Creamy-farming party; 213 in 10 min after a restart).
6. MVP-map avoidance.
7. Low-HP wing loop. Baseline: guild05 TheShadowsenpai, 12-15 Fly Wings in 2 min at 20-30% HP, no potion, died
   (mjolnir_12, mjolnir_10).
8. Loot-flow counters.
9. AIBot gone from memories. Baseline: 357 of 3,464 `legion/data/memory/*.json` contain "AIBot".

## Baselines (2026-11-14)
- Deaths, party members: 3.85/min before today's deploys (17:25-18:20Z); 1.1-2.6/min after the 14:55 batch.
  Raw counts include ~18% duplicates.
- Wing bursts (≥6 in 2 min): 11 bots before the batch -> 0-4 after.
- Kafra A->B->A: 25-75 per 15-20 min before the caps -> 6-18 per 10-15 min after.
- 50% weight: heavy bots 27-43 -> ~10 after the store/town-run fix.
- Peco, lv40+ mounted: Knights 1 -> 14 -> 29 -> 42; Crusaders 0 -> 8 -> 18 -> 26.
- Bow line: 2 of 129 without a bow.
- Gear audit (DB, 830 online lv20+ chars, Ayla excluded), % wearing each slot:
  - weapon 100, armor 100, shoes 93, garment 74, head top 57, shield 41, head mid 18, head low 1, accessory 2.
  - Only 28 of 830 own any accessory.
  - 2,190 unidentified equipment items on 730 characters (88%).
  - 53 characters have an unidentified piece for an EMPTY slot or a better armor/hat, and none of them has a Magnifier.
    Example: TheRyufox68, Knight 60, unidentified Hood/Sandals/Fancy Flower with those slots empty.
  - 131 characters carry a Magnifier plus non-upgrade unidentified gear.
  - Junk never sold: unidentified Knife 491, Club 318, Sword 214, Wand 153, Javelin 142. About 158 weight per character.
  - Identified-but-unworn better gear: about 0, so the equip logic is fine.
  - 5 cards in bags fit a free slot on worn gear.
  - Clip 0, Belt 1 on the server; 1 Creamy Card compounded, 7 loose.

## Root causes found (reported to main)
- pay_fild03 deaths come from metamorphosis, not the spawn list. rathena mob_skill_db: Fabre -> Pupa -> Creamy
  (NPC_METAMORPHOSIS). picklog shows Creamy (lv23) killed 791 times on pay_fild03 in 6 h, so lv1-4 Novices die to it.
  mapinfo/danger don't know about metamorphosis or summon targets. No Dead Branch use (0 consumed).
- Duplicate death events: 53 of 293 (18%) arrive within 1 s for the same bot. A de-dupe is needed.
- pay_fild10 herding: plants were counted as monsters (seen.go). Fixed at 14:39 and verified (bots spread out and kill).
- Morocc Kafra arrival 156,46 had no route: fixed by the 13:51 deploy (16 events -> 1).
- Party town run to Yuno for a Hood plus a prontera<->Morocc Kafra loop: fixed by the 14:55 batch (0 since).
- mjolnir_02 deaths: members crossed alone to mjo_dun01 through aggressive Elder Willows. Fixed by escorted crossings.
- Teleport card chain: there were no Clips anywhere. Belt[1] (20k, Prontera armor shop) replaces it; the chain is
  still slow.
- Aggregate API hang: the LFG goal loop holds per-bot locks during expensive goal picks. The fix is pending.
- Sage-plan Mages have no AoE until about job 35-40. That matches the plan but conflicts with the owner rule; open for
  the owner or the build agent.
