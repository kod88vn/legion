# Branch triage 2026-11-15

Read-only triage of the 21 unmerged branches in `G:\RO\legion` (none has a review file). Compared against main at
3aca736. Nothing was merged, deleted or deployed. Method: tip and last commit (`git log`), diff vs the merge base
(`git diff main...<branch> --stat`), patch-id match on main (`git cherry`), added-line match on main (each added line
searched in main's copy of the same file), and conflict check (`git merge-tree --write-tree main <branch>`). Context:
`docs/reports/triage-20261115.md`, `STATUS.md`, the bug queue (`legion/data/bugqueue.jsonl`), `docs/rules/ayla.md`
(AY11, AY15), and the tuning code.

Verdicts: (a) SUPERSEDED: the same change is on main (evidence named). (b) STILL WANTED: not on main, still relevant
(conflicts named). (c) OBSOLETE: OpenKore-only or retired behaviour, or contradicts a current rule (rule named).

## Table

| Branch | Tip | Last commit | What it changes (vs merge base) | Verdict | Evidence and notes |
|---|---|---|---|---|---|
| fix/ayla-buy | bd36170 | 2026-11-15 01:33 "no cross-map auto-buy trip" | 2 files, +15/-2: the Gear Slot buyAuto block is managed and off by default; Fly Wing a town shop slot | (b) STILL WANTED | Not on main (main `config.txt` still has `gearSlot 1`). Merge clean. Queue `b1794736237191-004` (auto-buy route loop, assigned, low). OpenKore-only. |
| fix/ayla-cross | 4a853b5 | 2026-11-15 08:01 "straggler walk capped to 40 cells" | 16 files, +2318/-244: Ayla plugin stack (partylead.pl, statusout.pl, aylagear.pl), PT32 engine, puppet briefs | (a) SUPERSEDED in bulk; leftovers (b) | 48 of 63 commits are patch-equal on main (cherry-picked as "Ayla partylead.pl from fix/ayla-cross ..."). Of the 15 unique commits, 84-100% of added lines already exist on main, except `fb50f1c` (PT32 walk-past threshold, 16%) and `451d631` (fight first monster within 6 cells, 67%). Merge conflicts in ai.go, pt32.go, partylead.pl, statusout.pl, tests, puppet files. Do not merge the stack; cherry-pick the two leftovers if still wanted. OpenKore-only. |
| fix/ayla-exp | c3fac66 | 2026-11-15 07:59 "map choice by measured exp/h" | 2 files, +181/-35 | (a) SUPERSEDED | Main `1b97264` "Ayla partylead.pl from fix/ayla-exp c3fac66" is patch-equal. |
| fix/ayla-fetch-tune | 6224d43 | 2026-11-15 08:11 "fetch tuned (AY23)" | 1 file, +9/-2 | (a) SUPERSEDED | Main `26badaf` is patch-equal. |
| fix/ayla-othermap-wait | f0448aa | 2026-11-15 08:42 "a wait only holds for members on her map" | 1 file, +10 | (a) SUPERSEDED | Main `b3028a5` is patch-equal. |
| fix/ayla-townreq | 605438a | 2026-11-15 08:15 "wait cap: a pack wait over 3 min goes on" | 1 file, +37/-5 | (a) SUPERSEDED | Two unique commits, but 100% (605438a) and 96% (ace1922) of added lines already on main's `partylead.pl`. Queue fix for the wait cap is on main. |
| fix/ayla-wait | c561afc | 2026-11-15 08:08 "fetch a member more than 20 cells back" | 1 file, +31 | (a) SUPERSEDED | Main `a465131` is patch-equal. |
| fix/catch-up-first | 82e78d4 | 2026-11-15 08:12 "party members catch up with the leader" | 2 files, +63/-3 (includes test) | (a) SUPERSEDED | Main `496790d` (PT32b) is patch-equal. Merge would add nothing. |
| fix/loot-filter | b19f809 | 2026-11-15 05:19 "engine loot filter for party members (PR16)" | 4 files, +4764/-4706 as stated; real change +60/-2 ignoring CR | (b) STILL WANTED, partly | Main `party.go` already applies spareGearFull and heavyJunk over the regen line (batch 18). Not on main: the 30%-weight rule and the junk tier at any weight (`lootfilter.go` absent). The commit converted `ai.go` and `party.go` from CRLF to LF (whole-file churn; main is CRLF). Merge conflicts in ai.go and party.go. Rebase with CRLF kept (see batch 6dcd11e). |
| fix/market-stall | a5ce919 | 2026-11-15 03:46 "market stall: pitches no leak, stuck cart reset" | 7 files, +368/-2 (vendpitch.go, vend.go, economy.go, tuning.go) | (b) STILL WANTED | `vendpitch.go` absent on main; 5% of added lines on main. Merge clean. Fixes queue `b1794719728496-252` (no deals in the south market, kpm 0), which the queue marks **duplicate of `b1794770968pp2`**. That status is wrong for this cause; correct the queue. |
| fix/metrics-loop | ad0bc99 | 2026-11-15 08:03 "server leaderboard tool; Ayla kpm, exp/h, rank" | 14 files, +847/-17 | (a) SUPERSEDED | Leaderboard commit is on main as `3c2e692` (patch-equal). "Meter counters survive engine restarts" (`f90a1ba`) is on main as `8744ae5` (15:12, same subject, later content; 99% of lines present). `meterpersist.go` on main. Merge conflicts in meterapi.go and meterpersist.go. Check that queue `b1794770968pp2` is closed by 8744ae5. |
| fix/rally-follow | 117b399 | 2026-11-15 01:41 "a rally ends once the leader leaves the town" | 3 files, +46 | (a) SUPERSEDED | Main `b587eb9` is patch-equal. |
| fix/rotate-safe | a37cea5 | 2026-11-15 06:48 "AY15 rotation only while Ayla is in a town" | 4 files, +73/-8 | (c) OBSOLETE | Contradicts AY15 (2026-11-15, `docs/rules/ayla.md`): "No member rotation ... the rotation experiment is stopped". Main `tuning.json` has `ayla_rotation_off: true` (commit `1f2ed57`). Clean merge, but do not merge while AY15 stands. Revisit only if the owner reinstates rotation. |
| fix/rotation-off | 7009cec | 2026-11-15 07:54 "rotation off by default; roster to her 4 friends" | 8 files, +53/-10 (`.env`, `docker-compose.yml`, `aylarotate.go`, `tuning.go`, tools) | (b) STILL WANTED for the default flip; roster change needs the owner | Main's code default is still ON (`tuning.go`: "ON by default"; the runtime file is off). The flip makes the code match AY15. Problems: the branch tracks `.env`, which main ignores (`.gitignore:105`): modify/delete conflict; `docker-compose.yml` conflicts. The roster change removes Echobunny and LilLilyblade from `LEGION_CORE`: that moves two members out of Ayla's party (G20, AY11). Owner decision needed. Keep only the code default. |
| fix/savepoints | 7066975 | 2026-11-15 01:29 "member never targets another map outside a town" | 10 files, +147/-16 | Tip: (a) SUPERSEDED. Unique `ea10c2a`: (b) STILL WANTED | Tip is on main as `69fb613` (patch-equal). Merge `ef3a6c3` is on main. `ea10c2a` (engine conservative party maps: `partycons.go`, start at -10, one death -5, step up only after 20 min) is not on main (2% of lines). Main has `partyPerfAdjust` with no conservative rule. The OpenKore half is on main (`5acb7d8`). Owner order 2026-11-15 ("party not strong enough"). Cherry-pick `ea10c2a` alone onto main; conflict in `tuning.go`. |
| fix/sentinel-algo | 2819df8 | 2026-11-15 08:34 "sentinel: rule-based algo loop with bounded restarts" | 4 files, +880 (`sentinel_algo.py`, tests, tool hooks) | (b) STILL WANTED | `sentinel_algo.py` absent on main; main `sentinel.py` has no algo. Matches Constitution 3.8 (plain-code keep-alive). Conflict only in `tools/unwall.py`. Review before any merge; not started by this triage. |
| fix/sweep-skip | 61d5a58 | 2026-11-15 08:31 "waypoint with no progress is skipped" | 1 file, +18/-1 | (a) SUPERSEDED | Main `8e0985b` is patch-equal. Queue `b1794749416648-572` is a duplicate. |
| fix/townreq-main | 0efb38d | 2026-11-15 08:39 "lockMap route loop" | 1 file, +8/-6 | (a) SUPERSEDED | All 3 commits are patch-equal on main (`7f20c52`, `da66a54`, `5a0bd91`). Queue `b1794731063215-580` is closed. |
| fix/travel-share | 42db8c2 | 2026-11-15 07:23 "travel share: solo bots stay on a map that pays" | 6 files, +315/-4 (`travelshare.go`, ai.go, bot.go, ferry.go, tuning.go) | (b) STILL WANTED | `travelshare.go` absent on main. Main has P4 (`947f347`) without it. Targets the open field-efficiency items: `b1794709221xoc` (high; travelling 14%), `b1794733252795-688` (travel 51%), `b1794735068469-692` (travel 54%). Conflict in `tuning.go`. Good next candidate after a rebase. |
| backup/field-efficiency-0e32400 | 0e32400 | 2026-11-15 15:26 "fair spot gate keeps a lv45 Acolyte off gef_fild05" | 6 files, +1156/-1023 (real change +137/-4 ignoring CR) | (a) SUPERSEDED | P4 merged to main as `947f347` (15:44) with `fairgate.go` and `fairgate_test.go` (gef_fild05 lv45 case, lines 119-128). The three commits are equivalent to main's P4 (94% of lines). Keep as a backup; do not merge (CRLF churn in `data.go` and `tuning.go`, conflicts). |
| batch/regular-2 | 6dcd11e | 2026-11-15 07:46 "keep main's CRLF line endings" | 18 files, +816/-16 (a merge batch of travel-share, rotate-safe, loot-filter, market-stall, plus the CRLF fix) | Duplicate batch: do not merge | Carries four branches, each in the table above. Based on a stale base (a94a6c9; `ai.go` 3187 lines vs main 3346). Its CRLF fix is the only part worth taking into the loot-filter rebase. Carries rotate-safe (obsolete). Conflict in `tuning.go`. |

## Patch-id and content checks (summary)

- Patch-equal on main (`git cherry`): ayla-exp, ayla-fetch-tune, ayla-othermap-wait, ayla-wait, catch-up-first,
  metrics-loop (leaderboard), rally-follow, savepoints (tip), sweep-skip, townreq-main (all 3), ayla-cross (48 of 63).
- Unique to branch (no patch-equal on main): ayla-buy, ayla-townreq (2), metrics-loop `f90a1ba` (same subject on main
  as `8744ae5`), savepoints `ea10c2a`, loot-filter, market-stall, rotate-safe, rotation-off, sentinel-algo,
  travel-share, field-efficiency (3, of which 0e32400 is present by content in `fairgate_test.go`), batch (5).
- Clean merges with main (`merge-tree` exit 0): ayla-buy, rotate-safe, market-stall, catch-up-first, sweep-skip.
- Conflicts with main (`merge-tree` exit 1): all others.

## Points for the owner and the main session

1. **Ayla roster.** `docker-compose.yml` on main defaults `LEGION_CORE` to six names (Zendancer84, Echobunny, Zen.fox,
   xXRinfangXx, LilLilyblade, vex_x18). AY11 says the party has five members. The rotation-off branch would make it four
   friends. Removing two members from Ayla's party is an owner decision (G20; AY11 says leavers stay as Starveil bots).
   Also: `.env` is not tracked on main, so the live roster is read from an untracked file. Verify which list runs.
2. **Code default vs AY15.** `legion/tuning.go` says rotation is "ON by default" while the runtime file has it off.
   Fix the default (rotation-off branch) so a missing key does not turn rotation on.
3. **Queue vs branch.** Queue item `b1794719728496-252` is marked duplicate of `b1794770968pp2`, but the market-stall branch
   fixes it by a different cause. Correct the queue.
4. **Ayla OpenKore branches.** All Ayla plugin branches are OpenKore-only. They are not obsolete while Ayla runs on
   OpenKore (STATUS: cut-over waits for the owner). They become obsolete at cut-over. Constitution 2.8: her fixes go to
   the whole Legion, so the engine equivalents (loot, party conservative step) matter more after cut-over.
5. **Not triaged here.** Queue items assigned to `fix/ayla-rest`, `fix/ayla-fightback` and `fix/party-band` (the last is
   built, pending deploy) name branches that are not in this list. Check them separately.

## Recommendations

Rebase and review next (in order):
1. `fix/market-stall` (clean merge; fixes a real queue item). Sonnet review (Constitution 4.2) before any merge.
2. `fix/ayla-buy` (clean merge, 15 lines; queue `b1794736237191-004`).
3. `ea10c2a` cherry-picked from `fix/savepoints` onto main (engine conservative party rule; owner order).
4. `fix/sentinel-algo` (Constitution 3.8 keep-alive; conflict in `tools/unwall.py` only).
5. `fix/travel-share` (P4 leftovers; the high-priority farming-share item; conflict in `tuning.go`).
6. `fix/loot-filter`: rebase onto main with CRLF kept. Take only the 30%-weight rule and the junk tier. Use the CRLF fix
   from `6dcd11e`, not the LF rewrite.
7. `fix/rotation-off`: take only the code default (`ayla_rotation_off` true). Drop `.env` and the roster lines unless the
   owner orders the roster change.
8. `fb50f1c` and `451d631` from `fix/ayla-cross`, if the owner still wants them.

Leave alone (do not merge; do not delete without the owner's word):
- Superseded: ayla-exp, ayla-fetch-tune, ayla-othermap-wait, ayla-townreq, ayla-wait, catch-up-first, metrics-loop,
  rally-follow, savepoints (tip), sweep-skip, townreq-main, ayla-cross (except the two leftovers), backup/field-efficiency.
- Obsolete: rotate-safe (AY15). Keep it parked; revisit only if the owner reinstates rotation.
- Duplicate: batch/regular-2 (its parts are triaged above).
