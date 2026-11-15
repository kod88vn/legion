package main

import (
	"math"
	"testing"
	"time"
)

// The patrol loop covers the open map evenly, skips other regions and portals, and is a short closed tour (owner
// rule "Sweep the map": a planned loop, not one corner, not back and forth).
func TestPatrolRouteLoop(t *testing.T) {
	m := testMap("patroltest", 200, 200)
	m.open(0, 0, 199, 149)   // big open field
	m.open(0, 160, 199, 199) // a separate pocket (other region)
	warpsFrom[m.Name] = []Warp{{X: 100, Y: 12, XS: 2, YS: 2}}
	defer delete(warpsFrom, m.Name)
	reg := m.Region(50, 50)
	r := patrolRoute(m, reg)
	if len(r) < 30 {
		t.Fatalf("only %d waypoints on a 200x150 field", len(r))
	}
	wm := m.warpMask()
	for _, p := range r {
		if m.Region(p[0], p[1]) != reg || p[1] >= 150 {
			t.Errorf("waypoint %v outside the region", p)
		}
		if dist(p[0], p[1], 100, 12) <= 4 || wm[p[0]+p[1]*m.W] != 0 {
			t.Errorf("waypoint %v next to the portal", p)
		}
	}
	// a decent tour: closing edge included, total length close to n * grid step, no leg crossing half the map
	total, longest := 0.0, 0.0
	for i := range r {
		d := edist(r[i], r[(i+1)%len(r)])
		total += d
		longest = math.Max(longest, d)
	}
	if total > float64(len(r))*patrolStep*1.6 || longest > 100 {
		t.Errorf("tour too long: total %.0f for %d points, longest leg %.0f", total, len(r), longest)
	}
	if &patrolRoute(m, reg)[0] != &r[0] {
		t.Error("the route must be cached")
	}
}

// Pacing (owner: "the leader never runs ahead"): a sweeping leader's segment ends within patrolLeadMax of the slowest
// member, or no farther from it than the leader already is; a solo bot is not limited.
func TestPaceTest(t *testing.T) {
	if paceTest(nil, 0, 0) != nil {
		t.Error("no members: no limit")
	}
	ok := paceTest([][2]int{{50, 50}}, 55, 50) // member 5 cells behind
	if !ok(58, 50) || !ok(59, 50) {
		t.Error("ends up to 9 cells from the member are fine")
	}
	if ok(62, 50) {
		t.Error("an end 12 cells ahead of the member runs ahead")
	}
	ok = paceTest([][2]int{{50, 50}}, 62, 50) // already 12 ahead (the member got stuck a moment)
	if ok(64, 50) {
		t.Error("going farther away is refused")
	}
	if !ok(62, 52) || !ok(61, 50) {
		t.Error("no farther than now is fine (sideways / closer)")
	}
	ok = paceTest([][2]int{{50, 50}, {56, 50}}, 55, 50)
	if ok(60, 50) {
		t.Error("the slowest of several members sets the pace")
	}
}

// A champion's note closes its spot for 30 s (it walks on and is re-noted while in view); a Boss keeps 10 minutes.
func TestChampionSpotShortLived(t *testing.T) {
	if (bossSpot{champ: true}).ttl() != 30*time.Second || (bossSpot{champ: true, kill: true}).ttl() != 2*time.Minute ||
		(bossSpot{}).ttl() != bossSpotTTL {
		t.Error("champion sighting 30 s, champion kill 2 min, boss bossSpotTTL")
	}
}
