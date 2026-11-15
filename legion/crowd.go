package main

// Map crowding cap (QA 2026-11-14 02:24, 729 online: 20% of the Legion "roaming" with nothing to fight, 95 of 133
// roamers seeing no monster at all, on the few starter/mid fields every picker chose: prt_fild04 41 bots for 146
// spawns, pay_fild02 75 for 213, pay_fild01/07 36-41 with 36-44% of them roaming). The old limit was a flat
// stay_crowd of 70 bots on any map, and a map's load only counted bots already standing on it, so a whole wave
// picked the same field before anyone arrived.
//
// Now every picker (solo chooseHuntingGround, goodHere, partyMapFor, partySpotNear, the guild leaders'
// bestPartySpot, and the after-death "easier map") asks mapHasRoom:
//   - load = online bots standing on the map or heading to it (their published goal), refreshed every 5 s; every
//     pick reserves its seats at once (crowdNote) so a wave spreads out within the same 5 s
//   - cap = the map's non-boss spawn count / crowd_spawns_per_bot (tuning.json, default 5), at least 6
//   - a map where crowd_roam_pct (30%) or more of 12+ bots are roaming is full as well, whatever its spawn count:
//     over-levelled bots empty a low field faster than it shows monsters to them
// A bot that roams roam_crowd_sec (75 s) on a full map gives it up (solo) or moves its party (bot party leader).

import (
	"sync"
	"time"
)

var crowd = struct {
	sync.Mutex
	at   time.Time
	load map[string]int // bots on the map or heading to it
	roam map[string]int // of those, roaming on it
}{}

// mapSpawnCount: the map's non-boss spawn count (rAthena npc/re/mobs, before mob_count_rate)
func mapSpawnCount(m string) int {
	n := 0
	for _, s := range spawnsByMap[m] {
		if mb := mobs[s.MobID]; mb != nil && !mb.Boss && s.Count > 0 {
			n += s.Count
		}
	}
	return n
}

// mapCap: how many bots a map holds before it counts as full
func mapCap(m string) int {
	return max(6, mapSpawnCount(m)/max(tune().CrowdSpawnsPerBot, 1))
}

// crowdRefreshLocked rebuilds the load from the bots' published snapshots (crowd lock held)
func crowdRefreshLocked(now time.Time) {
	if now.Sub(crowd.at) < 5*time.Second && crowd.load != nil {
		return
	}
	load, roam := map[string]int{}, map[string]int{}
	pubs.Range(func(_, v any) bool {
		s, ok := v.(interface{ Load() any }).Load().(pubState)
		if !ok || !s.Online {
			return true
		}
		dest := s.Goal
		if dest == "" || isTown(dest) {
			dest = s.Map
		}
		load[dest]++
		if s.Roaming && s.Map == dest {
			roam[dest]++
		}
		return true
	})
	crowd.at, crowd.load, crowd.roam = now, load, roam
}

// mapLoad: bots on / heading to m, and how many of them are roaming there
func mapLoad(m string) (int, int) {
	crowd.Lock()
	defer crowd.Unlock()
	crowdRefreshLocked(time.Now())
	return crowd.load[m], crowd.roam[m]
}

// crowdNote reserves n seats on m until the next refresh sees the bots' new goals
func crowdNote(m string, n int) {
	if m == "" {
		return
	}
	crowd.Lock()
	crowdRefreshLocked(time.Now())
	crowd.load[m] += n
	crowd.Unlock()
}

func roamCrowded(load, roam int) bool {
	return load >= 12 && roam*100 >= tune().CrowdRoamPct*load
}

// mapHasRoom: n more bots fit on m (the cap holds, the bots there are finding monsters, and they see enough of them:
// seen.go mapReadsThin)
func mapHasRoom(m string, n int) bool {
	l, r := mapLoad(m)
	return l+n <= mapCap(m) && !roamCrowded(l, r) && !mapReadsThin(m)
}

// mapCrowded: m is over its cap, or its bots are roaming instead of fighting, or they see few monsters (thin)
func mapCrowded(m string) bool {
	l, r := mapLoad(m)
	return l > mapCap(m) || roamCrowded(l, r) || mapReadsThin(m)
}

// mapPacked: clearly over (25% past the cap) or its bots roam instead of fighting - a party leaves / skips it
// (partyMapOK). Softer than mapCrowded so a party at the cap doesn't churn.
func mapPacked(m string) bool {
	l, r := mapLoad(m)
	return l*4 > mapCap(m)*5 || roamCrowded(l, r)
}

// crowdPct: load in % of the cap (to pick the least crowded map when all are full)
func crowdPct(m string) int {
	l, _ := mapLoad(m)
	return l * 100 / mapCap(m)
}
