# Map agent: restart notes (2026-11-14 ~16:20 local)

Scope: party/solo hunting-map choice, crowding, danger rules, escorts, Butterfly Wing restock. Engine code is in
`G:\RO\server\legion`. Lessons are in `legion/data/lessons.md` (every change has an entry).

Process rules:
- Never run `docker compose build`.
- No test containers during a freeze.
- Hand "code ready" to the deploy agent (a1576d3eb6b227442). It runs gofmt, vet, test and the image build.

## Live and verified earlier (in deployed images)
- Butterfly Wing restock to 3 at the Tool Dealer, inside normal town runs (town.go butterflyWingsWanted).
- Party map fit:
  - partyMapOK, partyMapNear (staged, room first), partySpotNear (relocate), bestPartySpot, partyMapFor (re-fit from
    the leader's position).
  - leaderPartyPick, plus "party-leader-hold" when nothing fits.
  - partyFitCheck (5 min; urgent every minute).
- Weakest member's rating:
  - effective level, with the 0.72 gear factor coming from powerLv;
  - mages and alchemists count 6 levels lower (fragileClass);
  - only online members whose level is known count (pubState.Known).
- Danger rules (ai.go partyMapTooHard = partyMapTooHardHere + mapAccessTooHard):
  - mean monster level <= weakest + 4 - aggressive%/5;
  - strongest common aggressive monster <= weakest + 2;
  - train maps: 100+ aggressive or assist spawns need weakest >= top + 14;
  - assist packs (AI 03/07) count half.
- Deadly-map skip (partyMapDeadly) and the per-leader death/flee ban (party.go leaderMem).
- Exp-poor maps (mapExpPoor), relaxed passive stage, too-easy hysteresis (band max + 5).
- Crowd:
  - mapPacked in partyMapOK;
  - spreadPick picks at random among near-equal maps;
  - MapZenyHour cache keyed by load;
  - bestZenyMap randomised;
  - first map choice after login staggered by 0-40 s.
- Route checks:
  - routeCrossesTooHard (pickers, solo guideSpotsFor, lfg goalMapSafeItem);
  - swarmPenalty counts too-hard maps (the Kafra routes around them).
- Plants (Mob.worthless) are no longer sightings or "monsters in view" (seen.go).
- A member's logout drops that member only, not the whole party (bot.go).
- Escorted crossings (escort.go).

## Code ready, handed to the deploy agent, NOT vetted yet (my test containers were killed)
- ai.go: MVP maps (mapMVP / isMVP, partyMVPOverLv 20): too hard below MVP level + 20.
- bossnote.go: bossThreatMob treats an MVP below its level + 20 as a threat (bots leave).
- lookup.go: mvpUnsafe(); mapinfo / drops farm entries get fit "unsafe: MVP X ..."; mapinfo gets "mvp".
- ai.go trip switch: "out of potions" never waits (potNeed): no trip interval, short party-run gap, otherwise
  startTrip alone.
- ai.go relocate:
  - the announced reason matches the real one;
  - a travelling party leader only fights what hits it (attackersOnly includes Set "party-leader").
- party.go partyFitCheck:
  - skips boss hunts;
  - first check per party jittered over 0-5 min;
  - at most 2 relocations per 30 s round (fitMovesPerRound);
  - a model-only "too hard" needs evidence (leaderDiedHere / deadly / ban / partyMapTrain / access).
- escort.go:
  - only on the way to p.Map (not a town, no trip);
  - only for a danger within 3 maps of it (escNear);
  - never at or from a town;
  - member wait cap (escortMemberMax, set to 3 min by another agent), then the member leaves the party;
  - leader gives up after 6 min;
  - the leader escorts only toward p.Map.
- together.go: the leader's straggler wait is capped at 90 s per map visit (no reset on flicker).
- Real-data tests (skipped without LEGION_RATHENA) in partyaccess_live_test.go:
  - TestMapAccessLive
  - TestCrossingDangerLive
  - TestMVPMapsLive
- Check after deploy:
  - mjolnir_02 / moc_fild03 / pay_fild10 deaths;
  - "waiting at ... escort" counts;
  - "before moving on" counts;
  - simultaneous "next:" announcements.

## Queued, NOT written yet (class QA, wait for the freeze to lift)
1. Metamorphosis / summon targets as map monsters.
   - mob_skill_db rows: `id,Name@NPC_METAMORPHOSIS|NPC_SUMMONSLAVE|NPC_SUMMONMONSTER,state,skillid,...,val1..val5`.
     The target mob IDs are columns 13-17 (Fabre 1007 -> Pupa 1008 -> Creamy 1018, both "idle always": Pupas turn
     into Creamies over time).
   - Plan:
     - extend poison.go loadMobSkillPoison (it already scans mob_skill_db) to build mobDerived[id] -> []id;
     - add mapDerived(m), recursion depth 2: (mob, from, kind);
     - include aggressive/assist derived mobs in mapAggroTopLv and train counts (a share of the source count);
     - show them in lookup.go mapAnswer monsters / lookupDanger, e.g. "Creamy lv23 (from Pupa)";
     - beginner pickers (beginner.go beginnerField / beginnerPick) avoid maps whose derived monsters are 15+ levels
       over the beginner.
   - Evidence: pay_fild01/02/03, Creamy looted 791x on pay_fild03 in 6 h, 19 deaths since 19:50Z (lv1-4 Novices).
2. Beginner safety (lv < 15):
   - never engage a monster 10+ levels above;
   - step away when one is near;
   - Fly Wing if it targets them.
   - Where: ai.go hunt target filter (targetMaxLv already caps), plus danger.go escape / bossWatch-style avoidance for
     high monsters near beginners.
3. Death de-dupe: bot.go 0x0080 vanish (`id == b.aid && pk[6] == 1`) emits "died on" even when already b.Dead. Wrap
   the whole block in `if !b.Dead { ... }` (only on the alive -> dead transition). QA: 18% of death events came within
   1 s of the previous one for the same bot.

## Open (decided not done / owner-level)
- Mid-level population is larger than the mid maps can feed (cap = spawns / 5); MVP and train bans shrink the pool
  further.
- No automatic "member leaves politely" when the party map's approach is impossible (only via the escort wait cap).
- Guild farmer assignment (guildstock) not traced: if it sets map goals directly it should call
  partyMapTooHard / goalMapSafe.
