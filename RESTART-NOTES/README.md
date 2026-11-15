# Restart notes - 2026-11-14 ~16:40 local (21:40Z)

> **SUPERSEDED 2026-11-14:** the restart order / agent roster described here belongs to the old setup. Read
> `STATUS.md` at the repo root first and start nothing (containers, loops, minds, agents) without the owner's word.

The AI does the whole restart (owner: "I do not do it myself, AI does"). Entry point: AGENTS.md (top block)
points here. Do every step below yourself; only real owner decisions go to the owner.

Saved because the owner asked: "save all agent work in a way we can restart. Everything is so lagging now".
Each agent wrote its own file in this folder (deploy.md, build.md, economy.md, party.md, map.md, ayla-plugin.md,
qa.md, watcher.md, ayla-mind.md, watchdog.md, market.md, chat.md, life.md, guild01-05.md, guild06-10.md).
Read this file first, then RULES.md (many new owner rules today), then the per-agent files, then HANDOVER.md.

## 1. Live state at save time
- Containers: ro-rathena (up since 12:28, 5x rates, Butterfly Wing 300z), ro-openkore-ayla (Ayla online),
  ro-legion + ro-legion-core on image `ro-legion:deploy-20261114-145352` (deployed 14:55; main restarted 15:43).
- **Legion API (port 8090) is wedged**: all-bot and even per-bot endpoints time out/reset since ~20:19Z; only
  /api/events and the minds bus answer. Bots still play. Root causes found:
  1. reset.go (build agent, live since 14:01): resetOnStorage ran on EVERY 0x00f2 (one per stored item), re-sending
     deposits and writing resets_pending.json on G: while holding the global resetGate that every bot tick takes.
     resets_pending.json is now `{}` (cleared 21:32Z) but the API was still wedged after that.
  2. LFG goal loop (lfg.go): up to ~1000 goal picks every 20 s under each bot's lock; /api/lfg locked every bot.
  Both are fixed in the tree (not deployed).
- **Docker daemon is very slow** (~20-30 s per `docker` call) since this afternoon's overload (9 parallel golang test
  containers + 2 stray `docker compose build legion` runs; all killed). A Docker/WSL restart would fix it but takes
  rathena and Ayla offline - owner's call (owner asked for a restart now).
- **Code freeze** on legion/*.go: the tree passed full `go vet` + `go test` at 16:26 (169 files) and is what the
  deploy agent's dep3 try 3 builds. Do not edit before it is deployed.
- Watch client (owner's view): back in game since 16:20 local on old Wine 9.0 + llvmpipe. Known ~30-min crash
  (llvmpipe JIT exhausting 32-bit address space) still open; the WoW64 (Wine 11) option exists in ai-client.sh behind
  ROAI_WOW64=1 but FAILED on :98 ("Cannot activate context") - don't use it without a new fix.

## 1b. Deploy state (deploy agent was stopped before writing deploy.md)
- Nothing new deployed since 14:55. dep3 try 3 (started 16:24) was stopped in its guard step; nothing built.
- Tries 1-2 failed `go vet` because fixers saved half-finished files (town.go b.gemsWanted / economy.go
  noteMarketFlow) - both are now complete on disk. A pre-test of the frozen tree passed at 16:26 (169 .go files,
  go vet OK, full go test OK); after that only build.md's restock change (town.go/ammo.go, saved ~16:28, complete) and
  economy's npcextended.go/marketflow.go landed - re-run vet/test once before building.
- Deploy path: tools/deploy/dep3.sh (main legion, guarded, --no-deps) then tools/deploy/depcore_nobuild.sh (core,
  same tag). Never recreate rathena by accident. ro-legion:latest was overwritten by a stray compose build - always
  build and pin a fresh `deploy-<timestamp>` tag.
- Ask the deploy agent to run TestHumanTrail and TestSupportNeed (party agent) in the full test pass.
- Staged but NOT in the tree: economy-farmer.go.staged (farmer accounts), party_strand_edit.py (never-strand fix) -
  apply after the first successful deploy, then test and deploy again.

## 2. Restart order (after a Docker/WSL/PC restart)
1. `docker compose up -d` brings db, rathena, agency, legion, legion-core, openkore-ayla (never profiles legacy/qa).
   If rathena was recreated, recreate the followers (tools/deploy/rathena_recreate.sh --yes).
2. Deploy the frozen tree (deploy.md has exact commands): main legion first, then core, `--no-deps`.
   Verify /api/overview < 2 s, then lift the code freeze.
3. Background loops (exactly one each, from the main session): tools/sentinel.py, tools/unwall.py --loop,
   tools/rogui/ai-display.sh (VIEW_SCALE 1.39), tools/rogui/ai-client.sh (no ROAI_WOW64).
4. Agents (HANDOVER section 0 / /legion-session): minds ayla, chat, life, market, guild01-05, guild06-10, watchdog;
   party watcher, class QA, build-and-strategy, economy engineer, deploy agent; Ayla-plugin engineer as needed.
5. Apply pending code (in order, deploy agent tests each batch; fixers never run test containers or compose builds):
   - party agent: scratchpad strand_edit.py (leader flees -> party follows; party map follows leader; healers never
     stranded) - see party.md (copy the script out of the old scratchpad first).
   - map agent: metamorphosis targets in map danger (pay_fild01-03 roaming Creamy), beginners avoid +10-level mobs,
     death-event de-dupe.
   - economy: farmer accounts + Merchant/DS-Hunter pairs (design approved), etc loot with demand to market first.
   - build: verification list (champions, skillaudit, gear numbers, restock order).

## 3. New owner rules today (all in RULES.md)
Ayla keeps >= 10k zeny (hard floor in her plugin); stock NPC shops stay; deploy agent may stop stray test
containers; interesting loot: use -> party -> guild -> guild stock -> sell; economy abundant and cheap, Prontera
market beats NPCs both ways; bots buy from NPCs minimally, Prontera full of gear; many solo farmer accounts (mostly
archers) that farm then vend; every Merchant account has a DS Hunter and every Hunter account a Merchant.

## 4. Open problems (most important first)
1. Legion API wedge -> needs the deploy (section 1).
2. Ayla's party has barely farmed since 20:14Z: deaths (pay_dun02 crowd 20:14, Eddga on pay_fild10 20:49), map
   changes, regroups. Latest: stuck on pay_dun01 with the lockMap route restarting ("standing still too long" every
   40-60 s) - see ayla-plugin.md / watcher.md. Escape-stall fix (teleport task blocked 43 s) built, maybe not loaded.
3. Bots disconnect (EOF) right after a job change or death (life.md).
4. Bot parties strand members/priests when the leader wings out or hunts another map (party.md fix prepared).
5. MVP maps (pay_fild10 Eddga) now excluded in the tree (map agent).
6. Watch client 30-min crash (section 1).
7. savecheck ~12% of hunting bots saved at the wrong Kafra (not settling).

## 5. Lessons from today (process)
- Main thread must not tell the watchdog an outage is "expected": an API outage > 5 min is always an alert, and the
  main thread acts on it at once (owner was rightly furious).
- Only the deploy agent runs go vet/test and builds; fixers hand "code ready". Code freeze during a deploy.
- Never hot-reload or test on the live Ayla in a fight; item cache + safe reloader exist.
- Never switch the owner's watch client to an untested setup; keep the old one until the new one is proven on :98.
