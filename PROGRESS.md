# PROGRESS - what is done

Dated 2026-11-15. Companion to `PLAN.md` (what is next) and `STATUS.md` (what runs). Branch `docs/session-save`.
Merge facts below come from `git log --merges` on `main` (153 merges, 2026-11-14 and 2026-11-15). Review facts come
from `docs/reports/reviews/<second-parent-sha>.md`. Nothing here is verified live: nothing from today is deployed.

## 1. Summary
- Built and merged: the redesign packages (rules trim, bug queue, sentinel, deploy guard, puppet and meter harness,
  guild direction, exchange), the Ayla engine packages A to G (cut-over NOT done), party, town, rally and build fixes,
  the market engine (flags OFF), the dashboard tabs (Players, Builds, Market), the test runner and the deploy gate.
- Not deployed. Not started. No live data touched by this session's tests (G23 tripwire on).
- Stopped on purpose: `legion`, `legion-core`, Ayla's OpenKore, the agency, every agent and loop. `stack.hold` is set.

## 2. Test totals (read from `runsuites.py`, run once on this branch's base, 2026-11-15)
`python tools/system/runsuites.py` (tripwire on, G23), tip `256a99c` (main at save time):

| suite | tests | result |
|---|---|---|
| tests/ayla | 3 | OK |
| tests/dashboard | 101 | OK |
| tests/system | 550 (2 skipped) | OK |
| tests/tools | 58 | OK |
| tools/puppet/tests | 91 | OK |
| **total Python** | **803** | **0 failed** |

Go tests (`tools/dev/gocheck.sh`) were not part of this run. The last recorded Go figure is from the review of
`fef0fc4` (dynamic pricing, a scratch merge): vet ok, 585 pass, 0 fail, 28 skip (skips are the Live and ZZ tests).
That figure predates later merges; re-run `tools/dev/gocheck.sh /g/RO/legion/legion` before the first deploy.

## 3. Verification by merge (what each merge was checked by)
- **Review file present and verdict "merge": 95 merges.** Each one was reviewed by Sonnet (Constitution 4.2).
- **Review file says "do not merge", merged anyway: 4 merges** (`c929057` fix/dash-intent2, `e1b420c` and `c43569c`
  both merge `ee521d6` fix/ayla-log, `50127e0` fix/ayla-rotate). The repo does not record a follow-up review for
  them. Owner or main session to decide whether they need a re-review (`docs/reports/reviews/`).
- **No review file: 54 merges.** 10 are `main` sync merges (no new code). 44 are feature or fix merges from
  2026-11-14 built before the review gate existed (the feat/* packages and many fix/* branches). STATUS.md says
  every merge has a review file; that is not true of these 44 (see section 6).
- Duplicate merges: `ee521d6` (fix/ayla-log) is merged twice (`e1b420c`, `c43569c`), and `cb5ccdc` (fix/ayla-tell)
  twice (`462e632`, `79c7ecd`). Both are visible in the table.
- Tests per merge are not recorded in the repo; the gate (`runsuites.py`) is the test evidence (section 2).
## 4. Not verified live
Every item below is still open. "Verified" in this repo means tests and review only.
- Nothing merged today is in a running image. The deploy has not run (no deploy gate fixes yet: M1 and M2 are open).
- The deploy gate (`fix/deploy-gate`, merged `41a5488`) has not been exercised by a real deploy (none has run since it merged, per STATUS.md).
- Ayla on the engine: packages A to G are merged; the cut-over has NOT happened (waits on the owner). Her
  current runtime is still OpenKore (`ro-openkore-ayla`, stopped). The engine's Ayla parity is tested offline only.
- Market engine (EC43 to EC48) and the crier: merged, all flags OFF (`market_engine_on`, `guild_market_feed_on`,
  `farmer_roster_on`, `farmer_orders_on`, `market_broadcast_on`, `dynamic_price_on`). No farmer exists yet
  (`data/farmers.json` not written). The crier NPC is inert until its table is loaded and the flag is on.
- Party rules (PT32 to PT36, AY11, AY26), rally fixes, town and rest fixes, the no-rebirth cap (ST01 / G10):
  verified by review and tests; not measured on the live server. The meter (kills/min, zeny/h) has no before and
  after comparison yet.
- Guild direction and duty (`guild_duty_on` default off), chat jobs and exchange tiers: merged, offline tests only.
- Sentinel `--algo` mode: opt-in only; blocked by two majors (PLAN step 4). The live sentinel is still `sentinel.py`.
- The dashboard Market tab (DB11) and tabs: tested against fake engines (`tests/dashboard`), not against live data.
- Build plans (EC38 to EC42), wizard plan (Meteor Storm 10), plan realism: tests only.

## 5. State of the world (as given by the session brief and `STATUS.md`; not re-checked by this agent, no docker run)
- Up: `ro-db` (MariaDB) and `ro-rathena` (login, char, map).
- Dashboard: `tools/dashboard/dashserve.py` on 127.0.0.1:8099 under `tools/dashboard/keep.cmd`, reads `ro-db` as the
  SELECT-only user `dash_ro`.
- Stopped on purpose: `legion` (`ro-legion`), `legion-core` (`ro-legion-core`), Ayla's OpenKore (`ro-openkore-ayla`),
  the agency stand-in (`ro-agency`, see README), every agent, mind and loop.
- `stack.hold` is set. Nothing starts without the owner (G21).
- Bug queue, recount 2026-11-15 (read only; `bugq.py` load and rank, `bugq.py list` and `summary` agree): 2136
  lines in `legion/data/bugqueue.jsonl` (append-only). 673 unique ids: open 0, assigned 205 (all live items; no item
  has no status), closed 248, duplicate 220. Of the 205 live items, 201 have a named assignee; 4 have none
  (`b1794773272q31`, `b1794778775768`, `b1794780557h4f`, `b1794780764rrq`). The earlier note "204 open" was wrong:
  the live total is 205 (`bugq.py list` prints 205 lines).
- Stray `tools/system/aylawatch.sh` loops were stopped; their launcher is not identified (bug `b1794787166o9s`).
  The file itself is still untracked in the main checkout.

## 6. Lessons this session (each lesson should change something that lasts: a code path, a test, a check or a rule)
1. **Bug queue keys must be unique.** `bugq.add()` dedupes on the key. A key reused by a later batch
   (`followups-batch-2`) bumped an old item and overwrote its fields. Change: the id guard branch `fix/bugq-id-guard`
   (merged `256a99c`) is the fix; each new item needs its own key. Check: its tests.
2. **Order-id collision.** Named by the session as a lesson; the repo holds no detail on it. Open: file the case
   with its evidence, and check that order ids stay unique across restarts.
3. **CRLF churn.** Several merges changed line endings: `tools/system/crier.py` was converted to LF in the DB11
   merge (`b1794790977uw9`); `ai.go` and `party.go` normalised to LF in `85b0836`; the aylawatch watchdog tests went
   LF to CRLF on a branch (`b17947837738vn`; the fix is to restore LF). Change to make: a check in the gate that flags whole-file line-ending
   changes; keep each file's line endings (REDESIGN section 2).
4. **Merge agents overwrite shared items.** Several branches were merged more than once, and "Merge main into
   batch" was repeated 5 times in the batch branch. Proposed: merge agents touch only their own files, and the
   deploy gate refuses a second merge of a sha already in `main`.
5. **Gatecheck regex skipped 4 rows.** `tools/system/gatecheck.py --coverage` missed 4 rows of
   `docs/rules/coverage.md`. Reported by the session; not re-run here. Change: a test that counts the rows in the
   coverage table and compares with the number of gate IDs.
6. **The deploy gate had two holes**: the sync-merge exemption (M1) and non-merge commits on main (M2). Both are
   open and block the first deploy (PLAN step 2).
7. **Review gap.** 44 feature merges of 2026-11-14 have no review file, and 4 merges went in against a "do not
   merge" verdict. Proposed: the deploy gate counts a merge as unreviewed unless its review file says "merge".
8. **STATUS.md drifted.** Its "Open branches" list names branches that have since merged (for example `fix/ayla-tooling`,
   `fix/deploy-gate`, `fix/sentinel-algo-2`, `fix/partycons`, `fix/protect-support`, `fix/rotation-default`,
   `fix/helper-lastrun`, `fix/market-stall-2`, `fix/travel-share-2`, `fix/ayla-party-four`). The main session
   should update it (this agent does not edit STATUS.md).

## 7. Merges into main, 2026-11-14 and 2026-11-15 (`git log --merges --reverse`)
Each row: short merge sha, date, subject, the second parent (the branch tip merged), the review file, and the verdict.

| # | merge | date | subject (as in git) | 2nd parent | review file | verdict |
|---|---|---|---|---|---|---|
| 1 | `fa0bbcc` | 2026-11-14 20:31 | feat/rules: rules trim (core RULES.md with gates G01-G24, topic files, gatecheck) | `73ba233` | none | none (built before the review gate) |
| 2 | `b6b12bd` | 2026-11-14 20:31 | feat/system: bug queue, session gate, sentinel, deploy guard, league, partycheck, agents | `bbaef5c` | none | none (built before the review gate) |
| 3 | `1562bda` | 2026-11-14 20:31 | feat/puppet: dispatcher, briefings, role briefs, legionctl bug/neo/notes | `fe7b23d` | none | none (built before the review gate) |
| 4 | `48b127e` | 2026-11-14 20:32 | feat/meter: kills/min, zeny/h, time budget, cohorts with par, Neo view | `9926aae` | none | none (built before the review gate) |
| 5 | `d5286e4` | 2026-11-14 20:32 | feat/exchange: relationship price tiers, chat jobs, chat intents | `8394140` | none | none (built before the review gate) |
| 6 | `db5e09f` | 2026-11-14 20:32 | feat/guild: guild directions, daily duty, ledger, kicks, rank, wealth, relations | `20bcbdf` | none | none (built before the review gate) |
| 7 | `38131f1` | 2026-11-14 20:42 | fix/sentinel: define ROOT before use (queue b1794706820063-060) | `e538859` | none | none (built before the review gate) |
| 8 | `5a207a2` | 2026-11-14 20:42 | fix/ayla-exit: ayla entrypoint rewrites XKore_publicIp (queue b1794706820218-608) | `f4b2f0c` | none | none (built before the review gate) |
| 9 | `983e2cf` | 2026-11-14 20:44 | fix/ayla-view: watch client resolves Ayla's IP dynamically | `ce34fa1` | none | none (built before the review gate) |
| 10 | `352d149` | 2026-11-14 20:48 | fix/ayla-gate: watch client online gate uses in-game marker, not title line | `b159201` | none | none (built before the review gate) |
| 11 | `b58e87b` | 2026-11-14 21:09 | fix/ayla-idle: Ayla zero downtime (AY18) watchdog, idle wake | `de22c04` | none | none (built before the review gate) |
| 12 | `14ffacd` | 2026-11-14 21:46 | fix/ayla-mind-cmds: Ayla Haiku helper, buff ask (critical) | `255a053` | none | none (built before the review gate) |
| 13 | `8cc8f63` | 2026-11-14 21:48 | fix/ayla-mind-cmds 99342fe | `99342fe` | none | none (built before the review gate) |
| 14 | `04e59f6` | 2026-11-14 21:50 | fix/izlude-fields: server-exact OpenKore fields for 9 more maps (critical) | `76af5f6` | none | none (built before the review gate) |
| 15 | `3035df1` | 2026-11-14 21:57 | fix/meter-fix b70ca2a | `b70ca2a` | none | none (built before the review gate) |
| 16 | `4f9e222` | 2026-11-14 21:57 | fix/shop-announce 18d33fe | `18d33fe` | none | none (built before the review gate) |
| 17 | `9afe34c` | 2026-11-14 21:57 | fix/starveil-lead c2ea3ae | `c2ea3ae` | none | none (built before the review gate) |
| 18 | `d295f73` | 2026-11-14 21:57 | fix/priest-buffs 1569062 | `1569062` | none | none (built before the review gate) |
| 19 | `40d702c` | 2026-11-14 21:57 | fix/wizard-active c6b529b | `c6b529b` | none | none (built before the review gate) |
| 20 | `830f508` | 2026-11-14 21:58 | fix/rally-sitstand 245242c (CRLF normalized to LF) | `245242c` | none | none (built before the review gate) |
| 21 | `eb4163e` | 2026-11-14 21:58 | fix/login-fix ccc7f67 | `ccc7f67` | none | none (built before the review gate) |
| 22 | `4065133` | 2026-11-14 22:02 | fix/party-destination 6caf3c5 | `6caf3c5` | none | none (built before the review gate) |
| 23 | `695d5f7` | 2026-11-14 22:03 | fix/party-destination fd1f3da | `fd1f3da` | none | none (built before the review gate) |
| 24 | `00b4724` | 2026-11-14 22:07 | fix/gear-maps c2b1e68 | `c2b1e68` | none | none (built before the review gate) |
| 25 | `85b0836` | 2026-11-14 22:07 | main (fix/party-destination fd1f3da) into fix/party-stick; ai.go/party.go normalized to LF | `695d5f7` | none | none (sync merge) |
| 26 | `e352a4e` | 2026-11-14 22:20 | fix/party-stick 2efbaba | `2efbaba` | none | none (built before the review gate) |
| 27 | `e83ce75` | 2026-11-14 22:22 | fix/party-stick e131a75 | `e131a75` | none | none (built before the review gate) |
| 28 | `6106d34` | 2026-11-14 22:32 | fix/party-stick 39a2930 | `39a2930` | none | none (built before the review gate) |
| 29 | `4b51939` | 2026-11-14 22:44 | fix/ayla-cross 77407fd | `77407fd` | none | none (built before the review gate) |
| 30 | `819c75c` | 2026-11-14 22:50 | fix/ayla-cross 56a66ec | `56a66ec` | none | none (built before the review gate) |
| 31 | `6ad17b3` | 2026-11-14 22:52 | fix/priest-heal b10aa77 | `b10aa77` | none | none (built before the review gate) |
| 32 | `b942bd7` | 2026-11-14 22:59 | branch 'fix/ayla-cross' | `d90c4d8` | none | none (built before the review gate) |
| 33 | `6ba9f19` | 2026-11-14 23:02 | branch 'fix/ayla-cross' | `67546b4` | none | none (built before the review gate) |
| 34 | `0a0addd` | 2026-11-14 23:03 | fix/healer-stick ebed06d | `ebed06d` | none | none (built before the review gate) |
| 35 | `53670cb` | 2026-11-14 23:05 | fix/ayla-cross 3ad717c (PT32 engine) | `3ad717c` | none | none (built before the review gate) |
| 36 | `1611937` | 2026-11-14 23:07 | fix/healer-stick 20a884d | `20a884d` | none | none (built before the review gate) |
| 37 | `56cea9c` | 2026-11-14 23:18 | fix/healer-stick 25fe2ea | `25fe2ea` | none | none (built before the review gate) |
| 38 | `d5c652a` | 2026-11-14 23:24 | fix/dash-party 9aeb140 | `9aeb140` | none | none (built before the review gate) |
| 39 | `eed9fe6` | 2026-11-14 23:27 | fix/healer-stick 202f150 | `202f150` | none | none (built before the review gate) |
| 40 | `1ee20ca` | 2026-11-14 23:38 | fix/party-monk 15de206 (AY11) | `15de206` | none | none (built before the review gate) |
| 41 | `e7c0f22` | 2026-11-14 23:42 | branch 'main' into fix/legion-maps | `47a61c2` | none | none (sync merge) |
| 42 | `4fa3b83` | 2026-11-14 23:44 | fix/party-cmd 77d8d48 | `77d8d48` | none | none (built before the review gate) |
| 43 | `455e548` | 2026-11-14 23:48 | fix/healer-stick ac4e60e | `ac4e60e` | none | none (built before the review gate) |
| 44 | `99c5d45` | 2026-11-14 23:48 | fix/dash-intent ca899a6 (partylead.pl: main's logic kept, status_intent added) | `ca899a6` | none | none |
| 45 | `4a73beb` | 2026-11-14 23:55 | fix/farm-share 9ea7200 | `9ea7200` | none | none (built before the review gate) |
| 46 | `96ada76` | 2026-11-14 23:56 | branch 'main' into fix/legion-maps | `4a73beb` | none | none (sync merge) |
| 47 | `d7b70e8` | 2026-11-14 23:58 | fix/legion-maps 96ada76 (PR16) | `96ada76` | none | none (built before the review gate) |
| 48 | `c929057` | 2026-11-15 00:05 | fix/dash-intent2 96e7b3d | `96e7b3d` | docs/reports/reviews/96e7b3d*.md | do not merge |
| 49 | `40ec98e` | 2026-11-15 00:19 | fix/savepoints 5b70342 | `5b70342` | docs/reports/reviews/5b70342*.md | merge |
| 50 | `e8ad043` | 2026-11-15 00:21 | fix/rest-kafra de2c9bb | `de2c9bb` | docs/reports/reviews/de2c9bb*.md | merge |
| 51 | `e1b420c` | 2026-11-15 00:22 | fix/ayla-log ee521d6 | `ee521d6` | docs/reports/reviews/ee521d6*.md | do not merge |
| 52 | `0ff7ef2` | 2026-11-15 00:22 | fix/chat-guild-api d3b87c2 | `d3b87c2` | docs/reports/reviews/d3b87c2*.md | merge |
| 53 | `e4135b6` | 2026-11-15 00:23 | fix/restore-behaviour aa02068 | `aa02068` | docs/reports/reviews/aa02068*.md | merge |
| 54 | `7826246` | 2026-11-15 00:23 | fix/system-layer 5814c60 | `5814c60` | docs/reports/reviews/5814c60*.md | merge |
| 55 | `0e08fbf` | 2026-11-15 00:23 | fix/leader-talk 57475f0 | `57475f0` | docs/reports/reviews/57475f0*.md | merge |
| 56 | `7701790` | 2026-11-15 00:23 | fix/idle-errands d3e077a | `d3e077a` | docs/reports/reviews/d3e077a*.md | merge |
| 57 | `680e888` | 2026-11-15 00:23 | main into batch | `2b567c4` | none | none (sync merge) |
| 58 | `203aae7` | 2026-11-15 00:24 | fix/dash-intent3 0ced460 | `0ced460` | docs/reports/reviews/0ced460*.md | merge |
| 59 | `ef3a6c3` | 2026-11-15 00:26 | fix/savepoints 57b2db0 | `57b2db0` | docs/reports/reviews/57b2db0*.md | merge |
| 60 | `462e632` | 2026-11-15 00:28 | fix/ayla-tell cb5ccdc | `cb5ccdc` | docs/reports/reviews/cb5ccdc*.md | merge |
| 61 | `50127e0` | 2026-11-15 00:30 | fix/ayla-rotate 168822e (AY15) | `168822e` | docs/reports/reviews/168822e*.md | do not merge |
| 62 | `bf29c7e` | 2026-11-15 00:34 | main into batch | `cb881f8` | none | none (sync merge) |
| 63 | `c43569c` | 2026-11-15 00:35 | fix/ayla-log ee521d6 (aylalog.py, AY21 tools only) | `ee521d6` | docs/reports/reviews/ee521d6*.md | do not merge |
| 64 | `79c7ecd` | 2026-11-15 00:38 | fix/ayla-tell cb5ccdc (owner orders via tell, AY21 tools only; main's AYLA_ACT/brief kept) | `cb5ccdc` | docs/reports/reviews/cb5ccdc*.md | merge |
| 65 | `e440f7c` | 2026-11-15 00:38 | main into batch | `79c7ecd` | none | none (sync merge) |
| 66 | `5f4bc0d` | 2026-11-15 00:45 | fix/dash-intent4 bcd7ade | `bcd7ade` | docs/reports/reviews/bcd7ade*.md | merge |
| 67 | `aa9309f` | 2026-11-15 01:29 | main into batch | `944e8ff` | none | none (sync merge) |
| 68 | `0941037` | 2026-11-15 03:33 | main into batch | `79aaddc` | none | none (sync merge) |
| 69 | `a52014a` | 2026-11-15 03:35 | batch/regular-0515 (regular deploy: chat-guild-api, ayla-log, restore-behaviour, system-layer, leader-talk, idle-errands | `0941037` | docs/reports/reviews/0941037*.md | merge |
| 70 | `b1720a5` | 2026-11-15 04:02 | fix/rally-sync 1440dea | `1440dea` | docs/reports/reviews/1440dea*.md | merge |
| 71 | `0d144b6` | 2026-11-15 05:26 | fix/stay-end 47aa589 | `47aa589` | docs/reports/reviews/47aa589*.md | merge |
| 72 | `19dd6be` | 2026-11-15 06:49 | branch 'fix/wake-limit' | `db27aef` | docs/reports/reviews/db27aef*.md | merge |
| 73 | `98846e0` | 2026-11-15 07:43 | fix/rest-follow 9451443 (AY21 critical) | `9451443` | docs/reports/reviews/9451443*.md | merge |
| 74 | `a94a6c9` | 2026-11-15 07:43 | fix/no-solo-wing 800bc93 (AY21 critical) | `800bc93` | docs/reports/reviews/800bc93*.md | merge |
| 75 | `ab720cc` | 2026-11-15 08:05 | fix/dash-retired 27ee455 (owner dashboard request) | `27ee455` | docs/reports/reviews/27ee455*.md | merge |
| 76 | `11ffdff` | 2026-11-15 13:45 | feat/dashboard-tabs: tabbed dashboard with the server status on top, standalone always-on server (DB05, DB06) | `dc951b8` | docs/reports/reviews/dc951b8*.md | merge |
| 77 | `6e24267` | 2026-11-15 15:18 | fix/party-band (bug b1794772058nfq, PT35) reviewed in docs/reports/reviews/52904683ef43ce8b7d45792a0b0ea1caeca45c12.md | `5290468` | docs/reports/reviews/5290468*.md | merge |
| 78 | `df95661` | 2026-11-15 15:19 | fix/rally-flap (bug b1794773273kln) reviewed in docs/reports/reviews/2cbcfae86188eca619e2eb8a7246e1bea018d065.md | `2cbcfae` | docs/reports/reviews/2cbcfae*.md | merge |
| 79 | `1ba3c5f` | 2026-11-15 15:24 | fix/systests (bug b1794774091qf6) reviewed in docs/reports/reviews/8265d057b555257b1a7eea14a4175b5e6a8b08ad.md | `8265d05` | docs/reports/reviews/8265d05*.md | merge |
| 80 | `01a49b6` | 2026-11-15 15:25 | fix/party-members (bugs b1794773273h90, p05, qqx, ko0, b1794711528clf, b1794750697br2) reviewed in docs/reports/reviews/ | `fe0d3d6` | docs/reports/reviews/fe0d3d6*.md | merge |
| 81 | `cfb34d9` | 2026-11-15 15:26 | fix/aylalog-wakes (bugs b1794736126l06, b1794721138oed, b1794739867792-368) reviewed in docs/reports/reviews/607fa6c4f99 | `607fa6c` | docs/reports/reviews/607fa6c*.md | merge |
| 82 | `aedbd0e` | 2026-11-15 15:29 | fix/ayla-rest reviewed in docs/reports/reviews/dbbe10f7818f4919cdc48128902dcb73d3b6c9ff.md | `dbbe10f` | docs/reports/reviews/dbbe10f*.md | merge |
| 83 | `716aee9` | 2026-11-15 15:30 | fix/legionctl-chat reviewed in docs/reports/reviews/9ec2d3a389bf3c5147060c5f180a7c1c5336b145.md | `9ec2d3a` | docs/reports/reviews/9ec2d3a*.md | merge |
| 84 | `c7356c1` | 2026-11-15 15:31 | fix/metrics-disk (bugs b17947684035ch, b17947684035co, b1794770968pp2, b17947709686zu, b1794770968dbk, b1794770968mkl) r | `f11031f` | docs/reports/reviews/f11031f*.md | merge |
| 85 | `d3251b8` | 2026-11-15 15:32 | fix/guild-ledger reviewed in docs/reports/reviews/9fb65349a08153620de7ad9e4493d7b5432e20a2.md | `9fb6534` | docs/reports/reviews/9fb6534*.md | merge |
| 86 | `f4494be` | 2026-11-15 15:40 | branch 'fix/kafra-wait' (kafra-wait P5) | `fc35fa9` | docs/reports/reviews/fc35fa9*.md | merge |
| 87 | `abde42f` | 2026-11-15 15:41 | branch 'fix/party-members-2' (fixes b1794711528clf, b1794750697br2) | `3735599` | docs/reports/reviews/3735599*.md | merge |
| 88 | `54bd990` | 2026-11-15 15:41 | branch fix/ayla-fightback: fight back before the stick, pack and portal waits (bug b1794737139520-016) | `e736281` | docs/reports/reviews/e736281*.md | merge |
| 89 | `e11260f` | 2026-11-15 15:42 | branch fix/flaky-leadertalk (bug b1794774272ubw) | `0a4788d` | docs/reports/reviews/0a4788d*.md | merge |
| 90 | `947f347` | 2026-11-15 15:44 | fix/field-efficiency: P4 field efficiency | `e976c08` | docs/reports/reviews/e976c08*.md | merge |
| 91 | `7a273b7` | 2026-11-15 15:48 | branch 'fix/watchdog' (bugs b17947684039bz, b1794768403gyn, b1794768403bgi, b17947684036wq, b1794768403zrc) | `bbb4d08` | docs/reports/reviews/bbb4d08*.md | merge |
| 92 | `022bb20` | 2026-11-15 15:50 | branch 'fix/town-rest' (bugs b1794751009271-548, b1794751010971-040) | `40499f6` | docs/reports/reviews/40499f6*.md | merge |
| 93 | `db8413f` | 2026-11-15 15:51 | branch 'fix/supplies' (bugs b1794773273mib, b1794773273mfy) | `02c7f1e` | docs/reports/reviews/02c7f1e*.md | merge |
| 94 | `844a39d` | 2026-11-15 15:51 | fix/small-followups into main: bugs b1794775322t5k and b1794775322zpm | `b1ef45e` | docs/reports/reviews/b1ef45e*.md | merge |
| 95 | `b800095` | 2026-11-15 15:55 | branch 'fix/supplies-2' into main | `891f896` | docs/reports/reviews/891f896*.md | merge |
| 96 | `c406cc9` | 2026-11-15 16:00 | branch 'fix/watchdog-2' (review b93a5049bb26389bab8422f0939432f3397cfe43) | `b93a504` | docs/reports/reviews/b93a504*.md | merge |
| 97 | `c0abbcb` | 2026-11-15 16:00 | branch 'fix/mapdensity-imports' | `566fce2` | docs/reports/reviews/566fce2*.md | merge |
| 98 | `2597966` | 2026-11-15 16:08 | branch 'fix/checks' | `7d4510c` | docs/reports/reviews/7d4510c*.md | merge |
| 99 | `70e3c56` | 2026-11-15 16:10 | branch 'fix/party-collapse' (bug b1794773272q31) | `33dfe5b` | docs/reports/reviews/33dfe5b*.md | merge |
| 100 | `78b3014` | 2026-11-15 16:16 | branch 'fix/flaky-leadertalk-2' (bug b1794774272ubw) | `bb82db3` | docs/reports/reviews/bb82db3*.md | merge |
| 101 | `72e5956` | 2026-11-15 16:24 | branch 'fix/no-rebirth' (bug b17947768550ps: no rebirth, ST01/G10) | `09afa5b` | docs/reports/reviews/09afa5b*.md | merge |
| 102 | `e0cce5d` | 2026-11-15 16:36 | branch 'fix/dash-builds' (DB09) | `23d6b2c` | docs/reports/reviews/23d6b2c*.md | merge |
| 103 | `68d80ec` | 2026-11-15 16:46 | branch 'fix/dash-builds-2' (rule DB09, review docs/reports/reviews/cdfada904e21352f982ee06b1556935e5f9ba033.md) | `cdfada9` | docs/reports/reviews/cdfada9*.md | merge |
| 104 | `18d2aa4` | 2026-11-15 16:50 | branch 'feat/build-design' (rules EC38/EC39, review docs/reports/reviews/53390e29685982782d6bf90e63d1f3ef8e7cf84e.md) | `53390e2` | docs/reports/reviews/53390e2*.md | merge |
| 105 | `8cc7ae7` | 2026-11-15 16:52 | branch 'fix/map-guard' (bug b1794778775768) | `30300f7` | docs/reports/reviews/30300f7*.md | merge |
| 106 | `d110085` | 2026-11-15 16:56 | branch 'fix/tests-live-data' (bug b1794777025l9n, G23) | `293bbcc` | docs/reports/reviews/293bbcc*.md | merge |
| 107 | `0961fa4` | 2026-11-15 16:58 | branch 'fix/dash-builds-3' (rules DB09/EC39, review docs/reports/reviews/156310bfb248f839ea6f0f64fdda81fee36a61b8.md) | `156310b` | docs/reports/reviews/156310b*.md | merge |
| 108 | `97256d9` | 2026-11-15 17:04 | branch fix/ayla-overrides (package A: AY25/DB10, fixes bug b1794779645264) | `8b2b6fa` | docs/reports/reviews/8b2b6fa*.md | merge |
| 109 | `1645534` | 2026-11-15 17:04 | fix/ayla-switch: package C (AY24), fixes bug b1794779386o7q | `2003c81` | docs/reports/reviews/2003c81*.md | merge |
| 110 | `6ad32bb` | 2026-11-15 17:09 | branch 'main' into fix/gear-gate | `c09ea50` | none | none (sync merge) |
| 111 | `adb01b3` | 2026-11-15 17:09 | fix/ayla-starveil: package D (AY24, AY13), bug b1794779393w50 | `07e1129` | docs/reports/reviews/07e1129*.md | merge |
| 112 | `f180b5f` | 2026-11-15 17:09 | fix/ayla-switch-2: AY24 package C follow-ups, bug b1794779386o7q | `60b8f20` | docs/reports/reviews/60b8f20*.md | merge |
| 113 | `c875125` | 2026-11-15 17:12 | branch 'main' into fix/gear-gate | `f180b5f` | none | none (sync merge) |
| 114 | `7a98459` | 2026-11-15 17:16 | branch 'fix/gear-gate' into main | `c875125` | docs/reports/reviews/c875125*.md | merge |
| 115 | `ba4d6fc` | 2026-11-15 17:21 | branch 'fix/ayla-watcher' (package G, AY24/AY25) | `401740d` | docs/reports/reviews/401740d*.md | merge |
| 116 | `10f0be8` | 2026-11-15 17:25 | branch 'fix/buildexport-tests' into main | `7652271` | docs/reports/reviews/7652271*.md | merge |
| 117 | `d55fae8` | 2026-11-15 17:28 | branch 'fix/ayla-hookin' into main | `5ac468b` | docs/reports/reviews/5ac468b*.md | merge |
| 118 | `55d4ee4` | 2026-11-15 17:34 | branch 'fix/plan-rebalance' into main | `44c4b0a` | docs/reports/reviews/44c4b0a*.md | merge |
| 119 | `8ab1145` | 2026-11-15 17:40 | branch 'fix/ayla-parity' into main | `0d21c35` | docs/reports/reviews/0d21c35*.md | merge |
| 120 | `b35448b` | 2026-11-15 17:46 | branch 'fix/ayla-dash-tool' into main | `7c64822` | docs/reports/reviews/7c64822*.md | merge |
| 121 | `392a3c8` | 2026-11-15 17:59 | branch 'fix/ayla-watch-engine' into main (reviewed d43f609) | `d43f609` | docs/reports/reviews/d43f609*.md | merge |
| 122 | `4b7b9e9` | 2026-11-15 18:25 | branch 'fix/market-build-feed' into main | `94d4fcc` | docs/reports/reviews/94d4fcc*.md | merge |
| 123 | `1622bff` | 2026-11-15 18:34 | branch 'fix/build-gear-alternatives' into main | `259b54a` | docs/reports/reviews/259b54a*.md | merge |
| 124 | `5dc672c` | 2026-11-15 18:39 | branch 'fix/small-followups' into main | `5e66c11` | docs/reports/reviews/5e66c11*.md | merge |
| 125 | `6220619` | 2026-11-15 18:51 | branch 'fix/build-plan-realism' | `33f625f` | docs/reports/reviews/33f625f*.md | merge |
| 126 | `754af66` | 2026-11-15 19:05 | branch 'fix/market-facts' into main | `2bd8422` | docs/reports/reviews/2bd8422*.md | merge |
| 127 | `800bc4f` | 2026-11-15 19:09 | branch 'fix/market-engine-core' into main | `6fbb42a` | docs/reports/reviews/6fbb42a*.md | merge |
| 128 | `70b71d0` | 2026-11-15 19:11 | branch 'fix/market-crier' into main | `3333403` | docs/reports/reviews/3333403*.md | merge |
| 129 | `4c24cb8` | 2026-11-15 19:19 | branch 'fix/constitution-warnings' | `55ca124` | docs/reports/reviews/55ca124*.md | merge |
| 130 | `060f55b` | 2026-11-15 19:24 | branch 'fix/farmer-roster-tool' (tip 183508da4c505009bda52522b0b4dcfaae807237, review: merge) | `183508d` | docs/reports/reviews/183508d*.md | merge |
| 131 | `4b76b9e` | 2026-11-15 19:29 | branch 'fix/market-metrics' into main | `dbb0dbb` | docs/reports/reviews/dbb0dbb*.md | merge |
| 132 | `74489a2` | 2026-11-15 19:30 | branch 'fix/dynamic-pricing' (dynamic pricing, EC48, flag default off) | `fef0fc4` | docs/reports/reviews/fef0fc4*.md | merge |
| 133 | `6945e58` | 2026-11-15 19:34 | branch 'fix/handover-refresh' (handover refresh, reviewed 1575d8b: merge) | `1575d8b` | docs/reports/reviews/1575d8b*.md | merge |
| 134 | `17065f4` | 2026-11-15 19:41 | branch 'fix/wizard-plan' (79ffd2bcad6feff5775533fcdda39d067ec3483a) | `79ffd2b` | docs/reports/reviews/79ffd2b*.md | merge |
| 135 | `f8d21b8` | 2026-11-15 19:51 | branch 'fix/market-leader-feed' | `e243f4b` | docs/reports/reviews/e243f4b*.md | merge |
| 136 | `84937d4` | 2026-11-15 19:53 | branch 'fix/deploy-script' (e82955d2a585d2d8447dc7675e0cea00a6a5a20c) | `e82955d` | docs/reports/reviews/e82955d*.md | merge |
| 137 | `522b7e7` | 2026-11-15 19:53 | branch 'fix/high-bugs-izlude-stuck' | `16da861` | docs/reports/reviews/16da861*.md | merge |
| 138 | `2c6450e` | 2026-11-15 19:55 | branch 'fix/copilot-takeover' (2c80ae6b88b59e9b2fd726e242a8b29ced725a08) | `2c80ae6` | docs/reports/reviews/2c80ae6*.md | merge |
| 139 | `6a30f6a` | 2026-11-15 19:55 | fix/dashboard-market-facts (ea6b722) into main | `ea6b722` | docs/reports/reviews/ea6b722*.md | merge |
| 140 | `33c0b6d` | 2026-11-15 20:12 | fix/farmer-orders (12de7322) into main | `12de732` | docs/reports/reviews/12de732*.md | merge |
| 141 | `14c47a5` | 2026-11-15 20:23 | branch 'fix/test-speed' | `2c539a2` | docs/reports/reviews/2c539a2*.md | merge |
| 142 | `eca6150` | 2026-11-15 20:27 | branch 'fix/market-stall-2' (market stall pitch fix, flag vend_pitch_fix_off) | `91c739d` | docs/reports/reviews/91c739d*.md | merge |
| 143 | `a33f812` | 2026-11-15 20:27 | branch fix/travel-share-2 (travel share: boat loop and Kafra-wait fixes, flags travel_share_off / roam_stay_on) | `0e722e2` | docs/reports/reviews/0e722e2*.md | merge |
| 144 | `5e9cca5` | 2026-11-15 20:34 | branch 'fix/helper-lastrun' | `cc76b31` | docs/reports/reviews/cc76b31*.md | merge |
| 145 | `41a5488` | 2026-11-15 20:41 | branch fix/deploy-gate (deploy merge gate, review 9c1c6a5) | `9c1c6a5` | docs/reports/reviews/9c1c6a5*.md | merge |
| 146 | `15dfc1f` | 2026-11-15 20:42 | branch 'fix/rotation-default' into main | `15c0571` | docs/reports/reviews/15c0571*.md | merge |
| 147 | `21391d8` | 2026-11-15 20:43 | branch 'fix/ayla-tooling' into main (review fe5ff96, verdict merge) | `fe5ff96` | docs/reports/reviews/fe5ff96*.md | merge |
| 148 | `baf9e18` | 2026-11-15 20:43 | branch 'fix/sentinel-algo-2' into main (review 046fab61, verdict merge) | `046fab6` | docs/reports/reviews/046fab6*.md | merge |
| 149 | `fa7abff` | 2026-11-15 20:44 | branch 'fix/partycons' into main (review 73791349, verdict merge) | `7379134` | docs/reports/reviews/7379134*.md | merge |
| 150 | `edb922f` | 2026-11-15 20:52 | branch 'fix/protect-support' (5b5b879) | `5b5b879` | docs/reports/reviews/5b5b879*.md | merge |
| 151 | `91b54a2` | 2026-11-15 20:57 | branch 'fix/ayla-party-four' (1acf08c) | `1acf08c` | docs/reports/reviews/1acf08c*.md | merge |
| 152 | `d4b6d1f` | 2026-11-15 21:05 | branch 'fix/puppet-orders-test' (review verdict: merge) | `dc32281` | docs/reports/reviews/dc32281*.md | merge |
| 153 | `256a99c` | 2026-11-15 21:09 | branch 'fix/bugq-id-guard' (review verdict: merge) | `dd1ea2f` | docs/reports/reviews/dd1ea2f*.md | merge |
