<!-- Moved unchanged from HANDOVER.md lines 1367-1408 (as of commit 63ca923). Index: HANDOVER.md. -->

## 6g. Farming time and town trips (2026-11-14, `town.go` needTrip, `ai.go` explore/deathLoopCheck/partyMapFor)
- Town trips start only for real needs: bag >= `trip_bag_pct` (70%) by weight or slots WITH something to sell, out of
  potions, no usable weapon; at most one non-urgent trip per `trip_min_interval_min` (45). Gear rides along on those
  trips (tripGear: the next upgrade sold in the same town). Every trip start logs
  `heading to <town> (<why>) [weight %, slots n/100, pots n, sp n, zeny n]`.
- Roaming sweeps the map (explore), give-up `roam_give_up_sec` 120. Maps must suit the hunter: fitShare >= 50% of the
  map's monsters under the (weakest member's) target filter in SpotsFor/guideSpotsFor/bestPartySpot/formParty.
  Bot party leaders leave dead maps (partySpotNear + rally); 2 deaths on a map in 10 min -> easier map, a member 10+
  levels under the party leaves; low-level bots flee at 25% HP and carry 5 Fly Wings.
- Guard: tools/sentinel.py checks farming share (>= 85%), <= 1 trip per bot per 45 min, and trips under 70% bag with
  pots > 5. The sentinel process must be restarted to load the check.
- Measured: farming 20% -> 35% (travel 42% -> 25%), trips 0.60 -> 0.36 per bot-hour, 0 unnecessary trips. Still
  short of 90%: low-level travel (43% for lv1-9), warp-gate loops (fix built 2026-11-14 ~05:55, awaiting deploy),
  roaming 15%.
- Routing round 2 (2026-11-14 ~06:10, built, deploy pending; details in legion/data/lessons.md): shop trips stand on
  nav.go `talkCell` (walkable cell of our region nearest the NPC, <= 6 cells) instead of "NPC +1,-1"; every walk to a
  portal goes through `walkToPortal` (sets b.lastWarp first, else safeWalkDest stops the bot at the door);
  `regroupStep` goes round through portals when the leader is in another walk region (prt_fild04 river) and asks for
  help after 2 failures; bot.go keeps the rest of b.route across a map change; stray NPC dialogs are clicked through
  (`answerStrayDialog`: an open dialog blocks walking and items); the seed spawns on walkable cells; walled-in bots
  wing out.

- Shop-door loops + mini-bosses (2026-11-14 ~08:00 EST, built for the 09:05 batch; lessons.md 12:55 UTC):
  warploop.go - in-town shop/refine trips accept only a route ending in the target's room (`routeEndsIn`), the
  warp-loop detector needs the same door twice (`mapVisit.via`) and never bans a town building's door. bossnote.go -
  `bossThreatMob`: Boss-class monsters are a map-wide threat below lv25; champions (Mob.Champion, AegisName C1_-C5_)
  a local one (walk off within 9 cells, no map veto). sitguard.go `fightOn` blocks every sit while a fight is on.

- Roaming round 2 (2026-11-14 ~06:40, built; lessons.md): bot-party members never explore on their own ("with
  <leader>"); thin maps (seen.go `mapReadsThin`, < 2 monsters in view per bot) count as full in crowd.go; a 3-min
  roam share >= 35% on a full/thin map moves the bot/party; explore() walks to shared recent monster sightings
  (seen.go `sightNear`) first. Measured before: roaming 7.2% (members 31% of it, thin maps 17%, blind search 24%).

- Sweep the map (2026-11-14 ~09:05, built; `legion/patrol.go`): leaders and solo bots with nothing in view walk a
  per-map patrol loop (cached waypoints, portal-free, one direction, jump to fresh sightings, rejoin forward).
  A bot party leader paces (never holds): each segment ends <= 9 cells from the slowest member within 20 cells.

- Map crowding cap (2026-11-14 ~02:45, built for the ~03:05 batch; `legion/crowd.go`): every picker asks
  `mapHasRoom(map, n)`: load = bots on the map + heading there (pubState.Goal), cap = non-boss spawns /
  `crowd_spawns_per_bot` (5); 30%+ roamers (`crowd_roam_pct`, 12+ bots) = full. Roaming `roam_crowd_sec` (75 s) on a
  full map relocates the bot / bot party. Before: roaming 16.7-20%, 2,400 kills/min (02:27).

