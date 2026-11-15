---
name: legion-session
description: Start or resume a main session of the Legion world after the 2026-11-14 redesign - what to start and in what order, the main session's job (listen to the owner and the bug queue, assign fixers, one planning pass before each 3-hour deploy), and what never to do. Use at the start of every main session, or when asked to start or stop the stack.
---

# Legion session (redesign)

Read `STATUS.md` first. The old always-on setup (15 minds, standing agents) is gone; the old skill is `legion-session-old`
(reference only). The world runs without AI: the engine plays the bots, scripts watch it, and AI agents are one-shot.

## The main session's job
1. **Listen** to the owner (rants, steering, bug reports) and to the bug queue. Write every new owner rule into the rules
   files in the same turn.
2. **Assign**: each queue item goes to a `fixer` (Sonnet; `hard-fixer` only for locking, networking, deploy scripts, or a bug
   that failed twice). One fixer per item, own worktree, builds only. Resume an agent with SendMessage; never spawn a
   duplicate. No coding in the main session.
3. **One planning pass before each 3-hour deploy**: the queue watcher wakes you at about 2h40 after the last deploy. Then:
   `analyst-review` (league + keep/revert), pick the batch, fixers finish, the `deploy` agent deploys with the fix list.
   Critical fixes (something is blocked) may deploy early with `--critical "<reason>"`; nothing else does.
4. **Keep a short list of decisions only the owner can make** and raise it under its own heading until answered.
5. Wake `watcher` (Haiku) when `partycheck` files violations and for a 30-minute review; `qa` (Haiku) for class sampling.

## Startup order (only on the owner's word; stop everything in reverse)
Nothing starts unless the owner says so. When he does:
1. **Stack**: `python tools/system/stackctl.py up` (removes `stack.hold`, writes `stack.up`), then
   `wsl -d Ubuntu-24.04 -- bash -c "cd /mnt/g/RO/legion && docker compose up -d"` and wait about 2 minutes
   (`python tools/legionctl.py status`). The first start after a folder move recreates every container; that is expected.
2. **Sentinel** (WSL, background): `wsl -d Ubuntu-24.04 -- python3 /mnt/g/RO/legion/tools/sentinel.py`. It files problems
   into the queue and exits only for an outage; restart it after an exit (`--mute KEY --mute-until HH:MM` for a key already
   handed to a fixer).
3. **Dispatcher** (puppet masters; package P): `python tools/puppet/dispatch.py` in the background. Its heartbeat file is
   what the session gate checks. (Skip with the owner's word if the world should run bare.)
4. **Bug-queue watcher** (background; its exit wakes you): `python tools/system/bugq.py watch`. When it exits, read what it
   printed ("what to assign"), act, and start it again.
5. **Party check** (background): `python tools/system/partycheck.py`.
5b. **Unwall loop** (background): `python tools/unwall.py --loop` (moves walled-in bots every 30 s; dropped in the
   refactor by mistake, restored 2026-11-14).
5c. **After any restart or recreate, verify live state** before saying anything works: `bash tools/system/fieldcheck.sh`
   (OpenKore fields), Ayla online and farming in her log, `python tools/system/bugq.py summary` triaged.
6. Ayla's keeper and the watch client (`tools/rogui/keeper.sh`, `ai-display.sh`, `ai-client.sh`) only when the owner wants
   her online. `keeper.sh` does nothing while `stack.hold` exists.
**Stopping:** `python tools/system/stackctl.py down` first (writes `stack.hold`, removes `stack.up`, so the keeper and the gate
stay quiet), then stop the loops, then `docker compose stop`.
Check what is running before starting anything: exactly one copy of each loop.

## Gates you will see
- `mindgate.py` (hooks): while `stack.up` exists it checks the dispatcher and sentinel heartbeats and says so once per
  problem; it never blocks more than one message. Pause: file `tools/mindgate.paused`.
- Deploy scripts: refuse a deploy within 3 hours of the last (use `--critical "<reason>"` only when something is blocked);
  need `--fix "<what>|<metric>|<cohort>"` per fix; rathena recreates at most once an hour.
- Reports: `python tools/system/league.py` (cohort league), `--compare-deploy` (keep or revert the last deploy's fixes).

## Never
- Start containers, loops or agents without the owner's word. Never use the real mouse or keyboard on any client.
- Control Ayla: no map orders, rallies, party lines or moves from the main session. Find the root-cause bug instead.
  Her requests arrive as Neo entries in the queue; those jump the queue.
- Send a bare `ai`, `ai off` or `ai manual` to her OpenKore.
- Deploy outside the deploy agent, deploy twice inside 3 hours without `--critical`, or recreate rathena by accident
  (Legion deploys are `--no-deps` on legion/legion-core only). Never touch the database by hand.
- Do a fixer's work, or let fixers deploy. Delete human characters or Ayla's party.
