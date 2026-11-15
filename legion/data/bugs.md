
## 2026-11-13 18:02 EST rathena recreated twice in 3 min, kicked Ayla and party
- What: ro-rathena was recreated at 18:02 EST (container up 46 s at check) and again at ~18:05 (up 3 min at next check); ro-legion recreated at the same times. RestartCount stays 0, so these are external compose up style recreates, not crashes. Ayla's OpenKore log: "Disconnecting (127.0.0.1:5121)... Error: You have been forced to disconnect by a GM". Keeper: "Ayla not connected for ~1 min -> restarting the watch client". Watch client exited at 18:02:55, relogged at 18:03:49.
- Where: rathena / legion (container lifecycle), watch client, openkore-ayla
- Impact: Ayla and all three friends (Zendancer84, Echobunny, Zen.fox) lost their sessions. Party split: Echobunny ended at moc_fild02, the others at payon. World online dropped 725 -> 129 before recovering to ~730.
- Repro/notes: Observed at cycles 3-5 (about 17:50 to 18:08 EST). Not reproduced by the watchdog; no fix attempted because rathena/legion restarts are out of scope. Still happening at last check (18:07), friends online and in party, Ayla re-login is pending. Ayla's position is not yet reported to the engine (human_last_seen empty).

## 2026-11-13 18:11 EST Echobunny stuck on moc_fild02 'waiting for Ayla' after regroup
- What: /api/bots/Echobunny shows state "waiting for Ayla", map moc_fild02, x=178 y=202, goal {mode: party, map: moc_fild02, who: Ayla}, kills 0. The same values held for 3 cycles (about 9 min). Ayla is now in payon (228,326). Echobunny's party_roster view says Ayla is in payon, and the party goal still says moc_fild02.
- Where: legion (follow/party goal logic), Echobunny, moc_fild02
- Impact: Echobunny (lv8) is not with the party, so the core test group is split. She is alone on a map with 4 monsters nearby.
- Repro/notes: Checked at 18:08, 18:11, 18:14. Ayla's leader position is not read by the engine (human_last_seen empty) at 18:08, which may be why followers do not path to her. Not fixed by watchdog; no config change allowed.

## 2026-11-13 18:20 EST mjolnir_10 death spike (deaths/min 2.8 -> 13.3)
- What: deadliest-map counts over the last 15 min: mjolnir_10 89 (18:17) then 134 (18:20); mjolnir_08 30; mjolnir_04 6. World deaths/min went 2.8 (18:03) -> 9.7 (18:08) -> 12.5 (18:17) -> 13.3 (18:20), while kills/min stayed about 21-26.
- Where: legion (monster danger / map choice), bots on mjolnir_10 and mjolnir_08 (Rune Midgard low-level farm maps)
- Impact: a large share of world bots are dying on these maps; deaths lower exp and cost zeny (repair/return trips). Party is not on these maps, so no direct harm to Ayla's group.
- Repro/notes: Counts come from health.sh "deadliest" over 15 min windows; watchdog has not yet listed bot names per death. Likely cause: bots at low level are sent to maps far above their level (see lessons.md, 18:14 entry). Still happening at 18:20.
- Update 18:31: still happening. Echobunny has stayed on moc_fild02 in 'travel -> payon' for 3 cycles (about 9 min) while Ayla and the other two are in payon. Ayla's OpenKore session dropped and was restarted via pkill at 18:22 and 18:28; she is in game again at payon (228,326).

## 2026-11-13 18:33 EST Ayla idle in payon for 7+ min, mind inbox messages not read
- What: Ayla's OpenKore log ends with "You are now: Sitting" / "You are sitting." at payon 228,326 since about 18:24. No attack, route or party message for 7+ min. /api/minds/ayla/inbox still shows my message id 33 (sent 18:25 EST) with "read": false. Earlier messages id 19 and 24 were sent at 18:06 and 18:10 and also checked against the inbox at the time.
- Where: legion (ayla mind / inbox polling), openkore-ayla, payon
- Impact: Zendancer84 and Zen.fox are idle with her; Echobunny is still on moc_fild02, so the core party has made no progress for about 25 min. The owner's regroup instructions are not being acted on.
- Repro/notes: Checked the inbox at 18:33 from the watchdog. The keeper has not reported the watch client as running since 18:14 (keeper log). Not fixed: pkill is not the right fix here because Ayla is in game; restarting the watch client would not wake the mind. Needs the engineer to check whether the ayla mind is polling /inbox.
- Update 18:40: still happening. Ayla has been sitting at payon 228,326 for about 10 min (last seen 605 s ago). Echobunny still on moc_fild02 (about 25 min). Zendancer84 and Zen.fox are idle with Ayla. Watch client is running, so this is not a login problem.

## 2026-11-13 21:46-21:50 (watchdog): population collapse and recovery; Echobunny character reset
- 21:45: 917 online, deaths 5.4/min. 21:47:46: 345 online. 21:48:28: 6 online, Ayla pt offline (all 3 friends). 21:49:18: 287, 21:49:21: 311, 21:50:25: 658. Recovered within ~4 min, so treated as a deploy/outage, not reported. Minds stayed 14/14 active through it.
- Watch client log: 21:40:15 dropped (frozen, Wine lock timeout), 21:41:37 back in game as Ayla.
- CORRECTION (coordinator, 21:52): Echobunny was NOT reset. The DB has her as lv39 Swordman with 4,470z. /api/bots/NAME returns empty fields (lv 0, zeny 0, no gear) while a bot is offline, e.g. during a deploy. My 21:50 reading of lv1 Novice / 0z was that empty output. Only judge a member's level, zeny or gear when online is true.
- Gear snapshot diffs for Zendancer84 and Zen.fox were false positives: their plans were blank while offline, and their gear is unchanged (Zendancer84 still "Cotton Shirt, next buy Club"; Zen.fox "Knife, Cotton Shirt, next buy Wand").

## 2026-11-13 21:57-22:14 (watchdog): repeated population dips, all recovered within ~4 min
- Dips: 21:46-21:50 (917 -> 6, back to 658), 21:57-21:59 (921 -> 253), 22:07 (482), 22:11 (158, chat 12/min), each back above 800 within 3-4 min.
- Each dip looks like a deploy or restart. Treated as deploy, not reported. Count the next dip against the 4-minute rule.
- Ayla pt split 6.3 min at 22:07 (Ayla alone in pay_dun00, Zen.fox on town trip, Zendancer84 and Echobunny offline). Reported to main.
- Zen.fox on Knife with 4,400+ zeny. Knife-plus-2000z timer resets while the bot is offline, so timing is per online stretch.

## 2026-11-13 23:46-00:03 (watchdog): deaths concentrated on gef_fild04
- Deaths/min (m5): 5.4 at 21:45, 7.3 at 23:52, 9.2 at 23:58, 7.7 at 00:00, 6.7 at 00:03. The 8/min threshold was crossed at 23:58 and dropped back below by 00:00, so the 10-minute alert did not trigger.
- Top death map is gef_fild04 by a wide margin: the cumulative top-list count rose from 29 (23:52) to 67 (00:03). mjolnir_06 and mjolnir_08 are next, at about 20 each.
- Ayla pt split episodes: 22:01 (6.3 min), 22:40 to 22:50 (about 2 min/resets), 22:52 (6.2 min), 23:21 (6.2 min), 23:46 (6.2 min). Each reported to main.

## 2026-11-14 00:12-00:45 (watchdog): stopping summaries, deaths spike, dips
- 00:12-00:13: guild06-guild10 posted summaries "Stopping this session". Reported; coordinator says they were restarted (run 5) and chat too. All 14 minds active since.
- Deaths/min reached 9.8 (00:36-00:41) with top pay_fild03 (23), prt_fild05 (16), mjolnir_06 (12). Above the 8/min line for about 6 min, the 10-min alert did not fire before the window ended.
- World dips: 00:05 (333), 00:30 (227), 00:45 (213). Each recovered within 2-4 min except the 00:45 dip, which was still open at the end of the watch.

## 2026-11-14 watchdog (cycles 1-10): status
- Minds 14/14 + watchdog active every cycle. Guild leaders 10/10 online since c3 (6/10 at c1). Online 791-799 (dip to 450 at c1 was a restart, recovered).
- Ayla pt: split at c1-c2 (Zendancer84 at pay_dun00 while the rest were at payon). Together again from c3-c4, moved as one group pay_arche -> pay_dun00 -> pay_dun01 (c10 at 184,82). Kills rising (members k30/k109/k138).
- Deaths/min 2.7 (c1) -> 6.8-9.7 (c3-c9, peak 9.7 at c8) -> 7.8 (c10). Top death maps pay_fild01 31, pay_fild03 15, moc_fild02 14. Above 8/min for under 10 min, no alert yet.
- Watchdog probe fixed: pop dip threshold lowered 800 -> 600 (795 was a false alarm).

## 2026-11-14 watchdog (cycles 11-50): status
- Minds 15/15 (14 + watchdog) active every cycle. No silence over 4 min, no stopping posts.
- Ayla pt: split 6-9 min at c17-c22 (Echobunny and Zen.fox at payon, then pay_dun00). Reported to main. Together on pay_dun00/pay_dun01 from c23 onward, members kills rising (k14->k531).
- Population crashes: c18 (214), c21 (10), c31 (99), c50 (0). Each recovered in 2-4 min except this check at c50, still open.
- Deaths/min over 8 from c24 onward; peak 32.3 at c50. Above 8 for 25 min. Top maps rotate (pay_fild10, moc_fild01, gef_fild10, moc_fild03, mjolnir_06, pay_dun00). Reported to main at 11, 18 and 22 min.

## 2026-11-14 watchdog (cycles 51-99): status
- Population crashes c50 (0), c70 (90), c31 earlier; each recovered in 2-4 min (deploys).
- Minds: guild06-guild10 dropped out at c66, c91, c95, c99 (03:09, 03:16, 03:22 start times) and came back within 1-2 cycles. Pattern: a 5-mind group restart about every 6-8 min. Not over 4 min of silence, so not an alert. Logged for the engineer to check the restart source.
- Ayla pt: split 6-8 min at c57-c60 (members wandering on pay_dun01 and payon), 6-8 min at c92-c93 (Zendancer84 alone at pay_gld). Both reported to main. Together on pay_dun00 from c94, kills moving again (Echobunny 646, Zen.fox 656).
- Deaths/min over 8 for 25 min (c41-c50), again from c53-c60 and c63-c99 (max 12.9 at c66). Top death maps: mjolnir_06, moc_fild01, pay_fild04.

## 2026-11-14 watchdog (cycles 100-120): final status
- Cycle 120 reached. Minds 15/15 active at most cycles; guild06-10 restart gaps at c66/c91/c95/c99/c109-c110 (all under 4 min).
- Ayla pt: split at c111-c113 and c118-c119 (members on payon, Ayla on pay_arche/pay_dun00 or (0,0) during warps); together on pay_dun00 most other cycles. Kills rising.
- Deaths/min over 8 from c112 to c119 (peak 11.6 at c116); DEATHS alert fired at 10 min (c119). Top maps mjolnir_06, moc_fild01, pay_fild04.
- Population crashes at c109 (323), c70 (90), c50 (0): each recovered within 2-4 min.

## 2026-11-14 watchdog (cycles 121-140): status
- Minds 15/15 active every cycle. No silence over 4 min, no stopping posts. Guild leaders 10/10 online throughout.
- Population 775-777 (no dips). Ayla pt 4/4 online and together on pay_dun00 for all 20 cycles (no split alerts).
- Deaths/min above 8 from c121 to c139 (8.1-9.7); the DEATHS alert (over 8/min for 10+ min) fired at c121 and was reported to main. Eased to 7.9 at c140. Top death maps: mjolnir_06 (about 28 at c121, now mjolnir_06/09), moc_fild02 (21 to 25), pay_fild04 (17).
- Ayla's own kill count reads 0 the whole run (kills come from members: Zendancer84 33 to 47, Echobunny 102 to 131, Zen.fox 91 to 125). Ayla fights and loots in her OpenKore log, so this is probably the leader not being counted, not a stall. Logged for the engineer to confirm.
- Ayla stayed on the same cell for 2-3 cycles at c133-c137 (fighting and looting, log confirms). Not a stall: the party's kill sum kept moving.
- Probe note: the DEATHS alert text grows by the time since the first >8 reading; it does not reset while the rate stays high, so the "13m / 14m / 15m" count is the continuous high span.

## 2026-11-14 watchdog (cycles 141-162): status
- Minds: 15/15 active at most cycles. guild06-10 went quiet together from 03:54 to about 03:58 (all five at once, about 4 min, no stopping posts), then came back. Coordinator says one agent runs all five guilds, so a slow cycle silences them together; reported to main at c148 as informational. Rule now: report only if silent over 5 min or a stopping/summary post.
- Ayla pt: split c152-c158 (Ayla on pay_arche / payon while Zendancer84, Echobunny and Zen.fox were on pay_dun00 and then pay_arche). Map history at 08:50-08:58 showed Ayla going payon -> pay_arche -> pay_dun00 -> pay_arche -> payon (bounce), reported to main at c152. At c159 Ayla made a town run in payon (sold 4 items, +678z, auto-buy done) with "hurry back" in party chat, then headed to pay_dun01. Regrouped in payon by c162 (members within about 60 cells). Still a long walk apart. Probe split alert did not fire (under 6 min).
- Deaths/min 6.2-7.8 (below 8 since c148). DEATHS alert from c121-c139 (8.1-9.7) recorded above. Top death maps: mjolnir_06, moc_fild02, pay_fild04.
- Population 775-779, no dips.
- Ayla kills field stays 0 (fights and loots in her log; the leader is probably not counted).

## 2026-11-14 watchdog (cycles 163-180): status
- Minds 15/15 active every cycle from c163. No stopping posts. Guild leaders 10/10 online.
- Ayla pt: town run in payon c159-c162 (announced in party chat, all back by 09:00). Split c165-c166 while Ayla went on to pay_arche ahead of the members, reported to main. Regrouped at pay_arche (81,22) by c168 and together since (pay_arche c168-c175, pay_dun00 from c176). Split alert did not fire (under 6 min each time).
- Deaths/min 6.6-7.8 over the block, world 778-780, no dips.
- Ayla kills field stays 0 in the probe. Her OpenKore log shows her fighting and looting, so the leader is not counted by the kill sum. Still unresolved for the engineer.

## 2026-11-14 watchdog (cycles 181-220): status
- Minds 15/15 active at most cycles. guild06-10 quiet together for about 3 min at c197-c200 (one agent runs all five, per coordinator). Coordinator rule: report only if silent over 5 min or a stopping post. Not reported.
- Ayla pt: bounced between towns c188-c206 (pay_dun00 -> pay_arche -> payon -> pay_arche -> pay_dun00 -> pay_arche -> payon). Reported to main at c152, c165, c201, c205. Party split by map for about 2-3 cycles at each bounce.
- Town run at payon_in01 (shops, indoors) at 08:59 and again at 09:04 (5 min apart). Coordinator says this is the sell-run loop (her bag threshold 48%), with a fix being applied to her plugin. Watch rule: report any town trip within 15 min of the last one. The next check is at 09:19 server time.
- c208-c213: Ayla shopping in payon_in01 while members waited in payon (coordinator: by design, counts as together).
- c214-c220: party regrouped in payon, then moved together to pay_arche (c219) and again on pay_arche (c220). All four within about 20 cells.
- Deaths/min 5.2-6.5, world 778-791 (no dips).

## 2026-11-14 watchdog (cycles 221-240): final status
- Run complete: c121 to c240 (120 cycles). Minds 15/15 active at c240; no stopping posts in this block. Guild leaders 10/10 online throughout.
- Ayla pt 4/4 together on pay_arche (c219-c222) and pay_dun00 (c223-c240, Ayla at 89,164, members within about 10 cells). One short gap at c222 (Zendancer84 lagged on pay_arche for one cycle) resolved at c223.
- No town trip by Ayla after 09:04 (payon_in01). Town-trip watch from the coordinator (within 15 min of the last) cleared at 09:19.
- Deaths/min 5.2-7.8 in this block; world 787-792, no dips.
- Ayla kills field stays 0 in the probe. Her log shows her fighting, so the leader is not counted. Still open for the engineer.
- Open items for the engineer: (1) the Ayla bounce between towns c188-c206; (2) the sell-run loop (coordinator fixing); (3) the probe's Ayla kills count.

## 2026-11-14 watchdog (cycles 241-303): status
- Loop ran c241-c303 (the 120-cycle run restarted once at c278 after the log stopped advancing; the stalled process was stopped and restarted for 80 more cycles, now at c303). Minds 15/15 active at most cycles; guild01-05 idle/not-active for a few minutes around c287 (last seen 272-274 s), active again by c288.
- Population dips: c256 (203), c271 (17) and c302 (153). Each was a server restart (market mind logged "server restart", probe showed uptime 0 min) and recovered within 2-5 min. Not counted as incidents.
- Ayla pt: together on pay_dun01 c260-c277, then the kill-stall alert (friends' kills flat 0-2 for 6-9 min after the restart at c271). Coordinator checked: no combat on pay_dun01 for 4+ min, Floor 2 looks empty (suspected server-side spawn issue); Ayla told to move the party back to pay_dun00 if still empty.
- Ayla pt split c287-c288: Ayla in Payon/pay_arche waiting at the Kafra while Zen.fox was on the Prontera/Payon road and Zendancer84/Echobunny elsewhere. Reported to main at 05:36 and 05:38. Regrouped on pay_dun00 by c302 (4/4 within about 8 cells at (81-87,155-166)).
- Deaths/min 0.5-3.5 over the block (none over 8/min since c278); world 1010 at steady state.
- Open: kill counter reset at each restart makes the "0 kills" probe alert noisy; probe still counts the friends only.

## 2026-11-14 watchdog (cycles 304-390): status
- Loop: restarted at c278 (old process stalled) and again at c352 (80-cycle run ended, new 60-cycle run). Watchdog mind silent over 4 min only at moments when the loop was late; heartbeats current again each time.
- Population dips: c302 (153), c333 (268), c352 (232): each a server restart (uptime 0 min), recovered within 2-5 min. Not reported.
- Ayla pt: split c287-c288 (reported), split 07:20-07:24 with Echobunny and Zen.fox on pay_gld (reported; regrouped at pay_arche by c342). Standing still at pay_arche c343-c347 (reported; moved to pay_dun00 by c348). Split 08:28 with the priest (Zendancer84) alone on pay_dun00 for two checks (reported; rejoined by c375). Standing still in payon c380-c381 with positions identical (reported at 08:44).
- Sentinel problems over the block: bad_trips (muted by main, peaked 63 in a 15-minute window), farming (muted, 82-84% share), trips (muted), and deaths (not muted; reported at 09:02).
- Deaths/min: 1-3 to c361, 4-7.7 c361-c388, 8.1 c389, 9.6 c390 (peak). Above 8 for about 4 min at the time of writing. Top death maps gef_fild10 (97), gef_fild03 (51), mjolnir_02 (43), prt_fild10 (40), moc_fild03 (35).
- Minds: 15/15 active at most cycles. Guild leaders 10/10 at most cycles.
- Open items for the engineer: (1) priest stragglers on pay_dun00, (2) party standing still in payon waiting for a member already within 2 cells, (3) bad_trips and farming economy, (4) death spike on Geffen fields.

## 2026-11-14 watchdog (cycles 430-497): final status
- Loop: 40-cycle run c457-c497 finished (DONE). Minds 15/15 at the end (10:30 restarts of chat and market were done by main; guild06-10 went stuck at "Cycle 6" 10:08-10:23 and were restarted).
- Server restarts (uptime 0 min): c430 (planned/redeploy), c450 (11:03, hourly), c468 (11:40 deploy), c479-c481 (12:00 planned rathena recreate, acked), c491-c494 (12:20 coordinator @reloadmobdb outage, acked), c495-c496 (12:28-12:29 legion deploy). Each recovered within 2-5 min. Population 1009 at c497.
- Ayla party: together on pay_dun01/pay_dun00 most cycles. Zen.fox split at payon (160,58) c459-c482 (kills stuck at 0), reported; re-reported at 12:08 after the 10-min deadline. Zendancer84 (priest) at pay_arche (52,103) at c482, reported. After the 12:28 restart the party was together on pay_dun00 again.
- Deaths/min 1-9.6 over the block; top death maps gef_fild10x, moc_fild03x, mjolnir_02x, pay_fild01x. Peak 9.6 at c390 (earlier block).
- Open: a six-member Ayla party at c494 (xXRinfangXx on pay_fild10 and Silent rogue on mjolnir_09, far from the core four), not yet resolved at the time of writing.
