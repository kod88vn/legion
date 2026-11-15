# Build-and-strategy agent - restart notes (2026-11-14 16:40 local)

Role: standing build-and-strategy agent (RULES.md 0). Heartbeat: `python tools/legionctl.py think build "..."` each
cycle. Report "built"/"code ready" to main; never deploy, never restart legion/core/rathena, NO docker image builds
and NO test containers (deploy agent a1576d3eb6b227442 runs gofmt/vet/test). Respect code freezes.
Reference docs kept current: CLASSES.md, legion/data/lessons.md (run 6-8 entries), HANDOVER.md 6f3.

## Live vs tree
- LIVE: legion runs deploy-20261114-145352 (restarted 20:43Z and 21:35Z on that same old image). The live API
  (/api/bots, /api/stats) hangs 30-60 s+. Root cause = my run-6 reset.go storage loop: rAthena sends 0x00f2 after
  EVERY item stored; resetOnStorage re-ran per item, rewrote resets_pending.json under locks. Fixed in tree only.
- TREE (code ready, not yet deployed):
  - reset.go: storage handled once per opening; no file I/O under resetGate; resume check every 10 min; party
    members (not leaders) may leave to respec; tuning `resets_off` kill-switch (tuning.go). Test in variant6_test.go.
  - skillaudit.go: /debug/pprof/ on the API mux (registerDebugAPI in api.go).
  - bossnote.go: champion threat = champLv < level+5 (champLv: solo max(gear lv, 3/4 base); party mate on map ->
    base; tank+healer -> base+5).
  - ai.go: melee skills (range<=3) only when bot and target both stand, distance from myPos. aoe.go: AoE cell from
    myPos. skillbuff.go: Hammer Fall only adjacent (range 1). weaponskill.go: Combo Finish only at a living target.
  - identify.go: Magnifier = standing stock (2, 4 if an unidentified upgrade waits); identify empty-slot pieces first.
  - cards.go: item-script bonuses for equipment (gearBonus); cheap (<10k) race/element cards compounded; compounding
    also between hits. builds.go: itemScore adds gearBonus; empty accessory/headgear/garment/shoe slot +10 gain.
  - town.go: restock = one 40% weight budget; ORDER: HP minimum -> wing money held -> SP potions -> arrows -> HP
    top-up (itemNPCPrice helper; file normalised to LF). ammo.go arrowTarget = min(2000, 800+lv*20).
- Already live earlier today (run 6): skill can-act gating + 0x0b1a cast fix (waste 47% -> 20.5%), combo timing
  (Chain Combo lands ~50%), coreweapon (katar savings - works: Jur funding seen), teleslot (Belt[1]), trap/strip/
  occult variants + AI, Sage/Wizard AoE plan order, natural resets (buggy, see above).

## Run-8 verification list (after the new image is live)
1. API latency (/api/stats < 5 s); if it wedges: set `"resets_off": true` in legion/data/tuning.json (live in 30 s)
   and use `curl 127.0.0.1:8090/debug/pprof/goroutine?debug=2` to find the lock holder.
2. Natural resets: events "reset: ..." go store -> hypno -> fetch -> done; resets_pending.json stays small.
3. Champion keep-away events gone for Swift Creamy (gef_fild05) / Bigfoot Ringleader (pay_fild07).
4. /api/skillaudit (?reset=1 then 10 min): Mammonite (was 42%), Storm Gust (43%), Hammer Fall (33%), Combo Finish
   (78%) waste; total was 20.5%.
5. Gear vs class-QA baseline (830 lv20+: 2,190 unidentified pieces on 88%, 0 Magnifiers, accessory worn 2%, mid
   headgear 18%, low 1%, 5 bots with a fitting card in bag). Script idea: scratch gear.py (per-bot /api/bots/<name>).
6. Restock: no bow user leaves town with arrows but 0 potions/wings; bags <= 40% after shopping.

## Pending / done items
- Butterfly Wing price: DONE - 300z live (rathena/db-import/item_db.yml, rathena restarted 12:28 after it); engine
  reads the override + shop-listed price (wingPrice). No fix needed.
- Restock order: CODE READY (handed to deploy agent).
- lv70+ poverty: my side = arrow hoarding (fixed in tree); rest is the economy engineer's.
- Not done: forging Blacksmith variant; Plagiarism use; partied trapper Trap errand (errand.go owned by Peco fixer);
  katar guild fund (peco.go owner); class-by-class Renewal review continues (Bard/Dancer, Alchemist next).
- "metamorphosis": no notes on this item in my work; ask main what it refers to.
