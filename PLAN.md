# PLAN - what comes next, in order

Dated 2026-11-15. Saved by the session on branch `docs/session-save`. Read `STATUS.md` first (what runs, what is
held); `PROGRESS.md` says what is done. Owner rules: `RULES.md` (G21: nothing starts until the owner says so).
Redesign packages: `docs/design/REDESIGN.md`. Market design: `docs/design/market-engine.md`.

Roles used below: **fixer** = a Haiku implementer (Constitution 4.1), **reviewer** = a Sonnet review before merge or
load (4.2), **deploy agent** = the assigned agent that runs `tools/deploy/deploy.py` (G17; the main session never
deploys), **ops agent** = an assigned agent for files and stack checks, **owner** = the only person who starts
anything (G21). Each item ends with a verification line (Constitution 3.6): no item is done on "should work".

## Standing state (facts the plan rests on)
- Nothing from today's merges is deployed. Only `ro-db` and `ro-rathena` are up. The dashboard runs on 127.0.0.1:8099.
- `stack.hold` is set. `legion`, `legion-core` and Ayla's OpenKore are stopped on purpose.
- Deploy batch: `tools/deploy/deploy.py`, run by the deploy agent. Dry run first, read by eye, then the real run.
- Two deploy-gate follow-ups must be fixed before the first deploy: **M1** (the sync-merge exemption reads `main` but
  HEAD may be another branch; it must refuse unless HEAD is `refs/heads/main`), **M2** (non-merge commits on main
  are not gated). Bugs `b179479365305x` (M1) and `b1794793670m7g` (M2).
- Bug queue (recounted 2026-11-15, read only): 673 unique ids; open 0, assigned 205 (the live total, all in
  `bugq.py list`), no status 0, closed 248, duplicate 220. Of the 205, 201 have a named assignee and 4 have none.
  93 are assigned to the group `followups-batch-2` (the earlier brief said about 200; the real figure is 93).

## Order

### Stage 1. Broken loop first (Constitution 3.10), no owner word needed (code and files only, nothing restarted)

1. **aylawatch:stray-loops** (`b1794787166o9s`, high, assigned `aylawatch-launcher`). The 18 orphan
   `tools/system/aylawatch.sh` loops were stopped; their launcher was never identified. Who: fixer finds the
   launcher (read logs and the scheduler, no start). Waits on: nothing. Verify: a log or process listing with zero
   stray loops after 30 min and a note on the item. The file `tools/system/aylawatch.sh` is still untracked in the
   main checkout (`git status` shows `??`). Do not delete it yet (see step 12).
2. **M1 and M2 deploy-gate fixes** (`b179479365305x`, `b1794793670m7g`). Who: fixer implements, reviewer reviews,
   merge by an assigned merge agent. Waits on: nothing. Verify: gate tests added for both cases, and a dry-run
   refusal shown on a throwaway branch.
3. **Ayla tooling blind-spot** is already merged (`21391d8`, `fix/ayla-tooling`). Check only: the partycheck and
   sentinel checks must read engine parties. Who: fixer runs the test suite line for it. Waits on: nothing.
4. **Sentinel algo majors** (`b1794793987mgy`, `b1794795209ipt`). M1: its Ayla checks are OpenKore-era only
   (ignore `aylaswitch` and `engine_parties`). M2: its report writer swallows write errors. Who: fixer, reviewer.
   Waits on: nothing for the fix. **Making `--algo` the live sentinel** waits on the owner (the watchdog launches
   `sentinel.py` without `--algo` today).

### Stage 2. Hygiene that does not touch live state

5. **Go formatting cleanup** (22 files; `gofmt -l` clean). Includes `legion/tuning.go` lines for
   `MarketEngineOn` and `GuildMarketFeedOn` (`b1794788146ieu`). Who: fixer, reviewer. Waits on: nothing.
   Verify: `tools/dev/gocheck.sh` gofmt step clean, and vet and test still pass.
6. **Roster edit** (Echobunny and LilLilyblade to alumni, in `legion/data/human-parties.json` and `.env` `LEGION_CORE`,
   together). **Verified 2026-11-15 by reading only** (no write, no bug queue entry): `legion/data/human-parties.json`
   (git-ignored) lists both under `alumni`, and `.env` `LEGION_CORE` has four entries, equal to the party's four
   members, with neither of the two names in it. Result: the roster agrees. Still open: writing this check onto the
   bug queue (ops agent) and, to apply `LEGION_CORE` to the running engine, restarting `legion-core`, which waits on the
   owner.
7. **followups-batch-2** (93 items; the other 112 live items sit with named fixers). Who: named fixers by area, one change at a time
   per part (3.5). Waits on: nothing for the fixes; each fix is verified before the next to the same part.
   Known items for this group: crier CRLF to LF churn (`b1794790977uw9`), crier `default_mode` hard-coded,
   `partycheck.py:152` blind timer, stale seed names in the docs (`owner-wants-layout.md`, `ayla-party-guild.md`).

### Stage 3. First deploy (the owner's word; G21)

8. **First deploy batch.** Contents: only reviewed commits (the deploy gate refuses unreviewed merges once M1 and M2
   are fixed). Who: the deploy agent runs `python tools/deploy/deploy.py` dry run, reads the plan by eye, then the real
   run for `legion` and `legion-core` with `--no-deps` (G15). The main session does not deploy (Article 1.3).
   Waits on: stage 1 steps 1, 2 and 4 (the gate fixes and the sentinel's blind spot), and the **owner's word**
   for the deploy and for the stack start. Verify: image tags match the reviewed sha; metrics compared before and
   after (3.6).
9. **Stack start.** Who: owner's word, then an assigned ops agent runs
   `python tools/system/stackctl.py up` and then `docker compose up -d` in WSL (commands in `STATUS.md`, "How to
   start"). Waits on: the owner. Restarting `rathena` at most once an hour (G16); recreating it also recreates Ayla's
   OpenKore and the legion containers (G15), so do not do it by accident.
10. **Baseline meter** with every market flag off (kills per minute, zeny per hour, leaderboard; `league.py`). Who:
    analyst (script). Waits on: step 9. Nothing is called better or worse until the meter reads (3.2).

### Stage 4. Market rollout (all flags are OFF in `tuning.json`; `docs/design/market-engine.md` section 6)

11. **Market flags, one at a time, each verified on live evidence before the next:**
    a. `market_engine_on`: the feed reads sane for a day (`GET /api/market/engine`, `data/market_feed.json`).
    b. `farmer_roster_on` with 20 farmers from `data/farmers.json` (the roster file is not yet written; WP4 adds
       `tools/system/addchars.py`, additive INSERT only, bot range 2100001-2199999, G06).
    c. `farmer_orders_on` with 20 farmers, then 150.
    d. `guild_market_feed_on` (leaders read the feed as information; flag needs step a).
    e. `dynamic_price_on` and `party_conservative_on` and `party_protect_support_on`: each on its own after a
       baseline; each needs its own evidence line.
    Who: the owner says which flag; the main session assigns the flag change to an ops agent; the change is a
    `tuning.json` edit followed by the agreed reload. Waits on: step 9 and the owner. Rollback: flag false, no restart.
    Note: `market_broadcast_on` is read by `tools/system/crier.py` from `tuning.json`, but `legion/tuning.go` has no
    field for it (only a comment). Fixer to decide whether a field is needed (no behaviour change).
12. **Crier go-live** (WP7; the NPC script is merged and inert until the flag is on and the table is loaded). Steps:
    load `rathena/sql/legion_notice.sql` into `ro-db`; dry run first (writes `data/market_notices.jsonl` only); then
    `docker restart ro-rathena` in a deploy window (never a compose recreate; at most once an hour, G16); then set
    `market_broadcast_on` true. Who: ops agent, owner's word. Waits on: step 9, step 11a, and the owner. Verify: a
    notice text read by a human-visible announce line in the log, and the banned-word test still green.

### Stage 5. Ayla cut-over (the owner's words: "start Ayla on the engine")

13. **Ayla on the engine.** Steps from `STATUS.md`: stop `ro-openkore-ayla`; migration dry run, then `--apply`; add Ayla
    to `LEGION_CORE`; set `engine_parties`; recreate `legion-core --no-deps`; verify restore and the party lead
    hand-over. Rollback: flip the switch and restore `human-parties.json.pre-ayla-engine`. Who: owner's words first;
    then an ops agent runs the steps and reports each one on the queue. Waits on: step 4 (sentinel blind spot),
    step 6 (roster agrees), step 9, and the owner. Constraints: Ayla's party is five (AY11) and always protects its
    support (AY26); G19 (back online within 5 min); G05 (never a bare `ai` to her OpenKore).

### Stage 6. Final cleanup (after the proofs; owner decides every deletion, G22)

14. **Shared route lists** (final cleanup item from this session).
15. **Delete `tools/system/aylawatch.sh`** only after `tools/system/aylawatch.py` is proven live (heartbeat
    `aylawatch.heartbeat` fresh and the Ayla watch reading from the engine). Waits on: step 13 and the owner.
16. **Remove merged fixer worktrees.** The worktree list holds about 190 entries, most of them merged. Delete only
    branches already merged into `main`. Waits on: the owner's word (deleting data, G22).

## Open decisions for the owner
- Words to start: the stack (step 9), the first deploy (step 8), each market flag (step 11), the crier (step 12),
  Ayla on the engine (step 13), and any deletion (steps 15 and 16).
- Constitution changes proposed in `docs/design/market-engine.md` section 2.2 (move "market" from 2.5 to 2.6;
  add the farmer sentence to 2.3). The owner decides; no agent edits the Constitution.
- Whether the AI market master is switched off (`docs/design/market-engine.md` section 4.3). Only the owner decides.
