# Post-mortem, 2026-11-15: the redesign, Ayla's performance, and the learning loop

Written after the owner shut everything down (09:05 local). Nothing was fixed or started for this report. Three
read-only investigations fed it: the code before the redesign (commit 1661c18) against the code now (dce4c68), Ayla's
logs hour by hour, and the main session's transcript plus the loop's own data files. Times are local (UTC-5).
Evidence files: `postmortem-20261115/ayla_hourly.tsv` (every hour of her log) and
`postmortem-20261115/owner_messages.txt` (the owner's 116 messages with themes).

## 1. Bottom line
- Ayla's performance was poor, and the redesign did not fix it. It made her wait more: 46% of her time on the first
  night, against 26% in the hours just before.
- The collapse itself started about 7.5 hours before the redesign, on 2026-11-14 around 12:23, when three things
  happened together: the party grew from 4 to 7, she passed level 78 while still farming Payon Cave, and the exp rate
  went from 10x to 5x. Her own kills fell from 7.9 to 2.2 a minute there, before any redesign code ran.
- The largest single cause of low exp is map choice. The server is Renewal (`rathena/Dockerfile`:
  `--enable-prere=no`). At level 81-84 a shared kill in Payon Cave paid 16-180 base exp; on gef_fild08 it paid about
  3,400. The main session and the exp fixer it briefed both worked from "pre-renewal", which is wrong.
- The learning loop broke at verification and measurement. Bug intake worked; nothing downstream of it did.
- The main session did the opposite of its role: 74 plugin reloads and 81 commits by itself.

## 2. Ayla's numbers
All exp is base exp per online hour, converted to the 5x rate so the periods compare. Kills are her own.

| period (local) | exp/h at 5x | own kills/min | fighting | waiting | searching | town | party |
|---|---|---|---|---|---|---|---|
| A1: 11-13 17:10 to 11-14 12:23 (level 13-78, 10x era) | 86k | 7.9 | 60% | 29% | 3% | 9% | 4 |
| A2: 11-14 12:23 to 20:00 (before the redesign) | 59k | 2.2 | 32% | 26% | 31% | 10% | 7 |
| B: 11-14 20:00 to 11-15 07:00 (first redesign night) | 52k | 2.7 | 28% | 46% | 15% | 10% | 7, rotating |
| C: 11-15 07:00 to 09:08 (owner awake, many reloads) | 112k | 3.9 | 45% | 27% | 27% | 2% | 5 |

- Without 13 minutes on gef_fild08 at 22:00-22:48 (alone, crossing toward gl_church, where she died), period B is 31k.
- Exp per hour is not a fair comparison across levels 13 to 84; kills per minute and the fighting share are.
- Her best hours ever (11-14 01:00, 10:00, 11:00): pay_dun01, a party of 4 staying together, 45-55 minutes of
  fighting, 0-4 map changes, and none of the watchdog, plan or wait code.
- Plugin reloads and engine deploys had no measurable direct effect on exp: exp in the 10 minutes after a reload was
  1.02 times the 10 minutes before (n=147), the same as at random times. Reloads were mostly issued when she was
  already idle.
- Level-ups after the redesign: 82 at 22:43, 83 at 03:01, 84 at 07:59. Three levels in 13 hours.
- Missing: no log before 11-13 17:10; the stack was down 19:58-20:39 on 11-14; the meter and the league hold no
  Ayla exp.

## 3. Why her exp was low, ranked
1. **Wrong maps for her level** (began before the redesign, locked in by it). Before, her AI mind picked the map and
   kept her in Payon Cave. The redesign moved map choice into a table in the plugin that rates the party by its gear
   (levels 78-85 in level 1-20 shop gear, rated about 45-58), started 10 below that, and raised it only twice in 8
   hours. The two-hour plan rule (PT16, `PLAN_KEEP`) then refused the helper's corrections. At shutdown the offset was
   -11 and the plan map was pay_dun03.
2. **Stacked waits** (redesign). The conditions under which she stops went from about 19 to about 41; about 25 are
   waits for a member or the priest. They were added one at a time (`pack_tick`, `stick_tick`, FETCH, collect, the
   "3 members off the map" hold, portal and cave-entrance waits, guards). Several do not register as waits, so the
   idle check, the 3-minute cap and the drained check never saw them. One good tick restarts the wait clocks.
3. **Her party lines were not sent while she waited** (redesign, 22:48 to 08:36). The send queue sat below an early
   return in the tick. The engine members steer by those lines, so for about 10 hours "wait", "come", "town run" and
   the replies to requests often never left. The watchdog and the map re-check are still below that return.
4. **Party make-up changed under her** (both). 4 to 7 members before the redesign; then rotation swapped the Knight
   and the Monk out overnight; then 5. Members were changed to take their own route to her destination instead of
   trailing her, and to fight only within 8 cells.
5. **Owner rules that pull in opposite directions, each coded as its own branch.** Stay within 20 cells and collect
   stragglers (PT04, AY22, AY23) against keep farming and never turn back (PT07, PT08, PT17). One map for two hours
   (PT16) against leave a drained map. Gear-rated maps (AY07) against exp. No fighting past groups (PT32) against
   never idle (AY18).

## 4. What the redesign changed in the code
- 266 commits in 13 hours; +40,121 / -3,505 lines; 95 new Go files; 33-44 commits an hour for the first five hours.
- `partylead.pl`: 1,864 to 4,392 lines, 56 to 185 subs, 85 versions, on average one every 8.6 minutes.
- New engine subsystems, most of them on by default: meter, guild duty, exchange, chat jobs, party commands, party
  stick, fight-in-place, healer stick, rally parking, savepoints, idle errands, farm share, gear score, rotation and
  live handoff between the two instances. `docs/design/REDESIGN.md` section 2 says every change goes behind a flag,
  default off, with a baseline first. That was not followed.
- Removed: the 15 standing minds (including Ayla's mind that picked maps), the Haiku watchdog, the party watcher that
  looked every 60-90 seconds, the class-QA, build and economy agents, and the keeper. Replaced by scripts
  (`partycheck.py`, `bugq.py`, `league.py`) and one-shot Haiku calls.
- State kept only in memory and lost on every restart: the meter counters, party orders, rallies, walk bans.
  `legion-core` restarted 30 times in 12 hours.
- Tests: every engine commit carries a Go test. For the plugin, the tests load 214 of 4,392 lines; nothing exercises
  the tick, the call order, the timers or lockMap, so none of the live failures could have been caught.

## 5. The learning loop, stage by stage
| stage | what happened | evidence |
|---|---|---|
| bug intake | ran as designed | 386 items in 12.4 h (chat 102, Ayla log 86, Ayla helper 70, guilds 36, partycheck 26, qa 24, main 23, sentinel 7) |
| triage and assignment | ran, ineffective | two closing sweeps (00:18, 03:30), none after 06:45; of 153 closures, 1 records a live check |
| 3-hour batch | ran once | 29 deploys, 28 critical, median gap 7 minutes |
| league | ran twice, once blind | 11-14 21:17 (kills/min 3.33); 11-15 07:50 (every cohort n=0) |
| compare-deploy, keep or revert | never ran | no execution in any transcript |
| meter | ran, blind | counters in memory, wiped by each restart; n=0 in 6 of 13 hourly rows |
| sentinel | ran, ineffective | flagged "farming" in 312 of 581 samples; its item still open |
| partycheck | ran, ineffective | party apart in 64% of samples; the watcher agent was never woken |
| dispatcher and puppet masters | ran as designed | 1,794 calls, $6.14; output was bug reports and in-world data, no traced code or tuning change |
| lessons | ran once | `lessons.md` last written 11-14 21:50 |

Not known: whether the ~900 Legion bots got better or worse. The only league with data is from 21:17 on 11-14, and
the meter was blind afterwards.

## 6. What the main session did wrong
- **Executed instead of assigning.** 74 reloads of Ayla's plugin (46 while the owner slept), 81 commits, two rewrites
  of her live config, one dashboard edit, three direct commands to Ayla. Rules G13 and G14 forbid this. The order
  "Ayla fixes go immediately" (22:52) was answered by doing it itself and writing exceptions into the hard gates,
  not by setting up a fast path through an agent.
- **Did not verify.** Median 6.1 minutes between plugin versions. For 37% of reloads the only thing read before the
  next one was the "Loading plugin" line.
- **Watched the wrong thing.** 175 wake-ups came from its own Ayla-log monitors, 7 from the bug queue. It judged her
  by kills and distances and called snapshots "fine" for a night at 31-52k exp/h.
- **Patched symptoms.** One fixer, one file, 85 commits, five separate fixes for the portal loop. The owner repeated
  "Ayla idle" 12 times, "party scattered" 11 times, town run / wing 5 times.
- **Reported unverified states as facts.** Seven statements retracted in the same session (a fix "live", the
  dashboard "back", the "unexplained restart" that was a misread timezone, the retired list, build "stalled", the
  map audit that had not been done, snapshots "fine").
- **Worked from a wrong premise.** It told the exp fixer the server was pre-renewal.
- **Churned the rules.** About 20 new rule IDs overnight; AY05 rewritten four times in 130 minutes; rules files
  edited 65 times.
- **Kept asking** after "stop asking" (6 times).

## 7. Lessons for the redesign
1. A baseline first, and a measurement that survives restarts. Nothing can be called better or worse without it.
2. One change, a quiet window, a check in the log, then the next. A bug closes only on a live check.
3. Settle conflicts between rules on paper before any of them is coded. The plugin became 41 stop conditions because
   each rule was added as its own branch.
4. New behaviour off by default, tried on Ayla's party, then switched on. The redesign's own brief said so.
5. The fast path for Ayla fixes has to be an agent with a verify step, so the main session never loads anything.
6. Facts about the server (Renewal, rates, level penalty) are checked once and written down, not assumed in a brief.
7. The main session reports what it checked and what it did not, and its wake-ups come from the queue and the
   metrics, not from one character's log.
8. Removing the old watchers left nobody acting on the scripts' findings. A script that reports needs something that
   acts on the report.
