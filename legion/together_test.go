package main

import (
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCollectBoundaryMatchesPT06(t *testing.T) {
	member := func(x int) pubState {
		return pubState{Name: "m", Map: "pay_fild04", X: x, Y: 100, Online: true, HPFrac: 1}
	}

	got, ok := chooseCollectTarget("pay_fild04", 100, 100, []pubState{member(120), member(121)})
	if !ok || got.X != 121 {
		t.Fatalf("PT06 boundary selection = (%d, %v), want 21-cell member", got.X, ok)
	}
	if got, ok = chooseCollectTarget("pay_fild04", 100, 100, []pubState{member(120)}); ok {
		t.Fatalf("PT06 must not collect at exactly 20 cells, got %+v", got)
	}
}

// QA 2026-11-14 07:31: 11 of 14 collecting bot leaders were collecting a member within 20 cells or busy fighting,
// and members 10-20 cells away waited "coming to get me". together.go is the one rule both sides use.
func TestTogetherRule(t *testing.T) {
	if togetherCells() != 20 || collectStopCells() != 16 {
		t.Fatalf("defaults: together %d stop %d, want 20 / 16", togetherCells(), collectStopCells())
	}
	if !partyTogether("pay_fild04", 100, 100, "pay_fild04", 110, 100) {
		t.Error("10 cells on the same map must be together")
	}
	if !partyTogether("pay_fild04", 100, 100, "pay_fild04", 120, 115) {
		t.Error("20 cells on the same map must be together")
	}
	if partyTogether("pay_fild04", 100, 100, "pay_fild04", 121, 100) {
		t.Error("21 cells is not together")
	}
	if partyTogether("pay_fild04", 100, 100, "pay_fild03", 100, 100) {
		t.Error("another map is not together")
	}
	if partyTogether("pay_fild04", 100, 100, "pay_fild04", 0, 0) {
		t.Error("unknown position is not together")
	}

	m := func(x int, target uint32, hp float64) pubState {
		return pubState{Name: "m", Map: "pay_fild04", X: x, Y: 100, Online: true, Target: target, HPFrac: hp}
	}
	cases := []struct {
		name      string
		s         pubState
		committed bool
		want      bool
	}{
		{"10 cells, new", m(110, 0, 1), false, false}, // lily.hunter -> Lilymage at 10
		{"20 cells, new", m(120, 0, 1), false, false},
		{"21 cells, new", m(121, 0, 1), false, true},
		{"18 cells, in progress", m(118, 0, 1), true, true}, // hysteresis: keep going
		{"16 cells, in progress", m(116, 0, 1), true, false},
		{"40 cells, busy fighting", m(140, 7, 0.9), false, false},
		{"40 cells, fighting in danger", m(140, 7, 0.3), false, true},
		{"40 cells, busy fighting, in progress", m(140, 7, 0.9), true, false},
		{"other map", pubState{Name: "m", Map: "pay_fild03", X: 140, Y: 100, Online: true, HPFrac: 1}, false, false},
		{"offline", pubState{Name: "m", Map: "pay_fild04", X: 140, Y: 100, HPFrac: 1}, false, false},
	}
	for _, c := range cases {
		if got := needsCollect("pay_fild04", 100, 100, c.s, c.committed); got != c.want {
			t.Errorf("%s: needsCollect = %v, want %v", c.name, got, c.want)
		}
	}

	// the member's regroup walk uses the same hysteresis
	for _, c := range []struct {
		d      int
		active bool
		want   bool
	}{{10, false, false}, {20, false, false}, {21, false, true}, {18, true, true}, {16, true, false}} {
		if got := regroupNeeded(c.d, c.active); got != c.want {
			t.Errorf("regroupNeeded(%d, %v) = %v, want %v", c.d, c.active, got, c.want)
		}
	}
}

// QA 2026-11-14 13:00Z: b.X/b.Y is the walk's start until the walk ends, so two bots walking to each other looked
// 21+ apart while 5-13 apart: livePos interpolates.
func TestWalkPos(t *testing.T) {
	now := time.Now()
	// 10-step walk east at 150 ms/step, 4 steps done (6 left = 900 ms)
	if x, y := walkPos(100, 50, 110, 50, now.Add(900*time.Millisecond), 150, now); x != 104 || y != 50 {
		t.Errorf("mid-walk = %d,%d, want 104,50", x, y)
	}
	if x, y := walkPos(100, 50, 110, 50, now.Add(-time.Second), 150, now); x != 110 || y != 50 {
		t.Errorf("walk over = %d,%d, want 110,50", x, y)
	}
	if x, y := walkPos(100, 50, 110, 50, now.Add(1500*time.Millisecond), 150, now); x != 100 || y != 50 {
		t.Errorf("walk just started = %d,%d, want 100,50", x, y)
	}
	// leader and member walking toward each other: start positions 24 apart, live 8 apart -> no new collect
	lx, ly := walkPos(100, 50, 110, 50, now.Add(150*time.Millisecond), 150, now) // 109
	s := pubState{Name: "m", Map: "pay_fild04", X: 124, Y: 50, Online: true, HPFrac: 1}
	s.LX, s.LY = walkPos(124, 50, 114, 50, now.Add(450*time.Millisecond), 150, now) // 117
	if needsCollect("pay_fild04", lx, ly, s, false) {
		t.Errorf("collect started on stale walk-start positions (live %d vs %d)", lx, s.LX)
	}
	if needsCollect("pay_fild04", 100, 50, s, false) != (dist(100, 50, s.LX, s.LY) > 20) {
		t.Error("needsCollect must use the published live position")
	}
}

// QA 2026-11-14 guild02: the leader detoured to Payon to heal while the members marched on to mjo_dun02.
func TestLeaderDetour(t *testing.T) {
	lead := pubState{Name: "L", Online: true, Map: "pay_gld", Goal: "payon", Trip: false}
	if !leaderDetour("mjo_dun02", lead) {
		t.Error("goal elsewhere = detour")
	}
	lead = pubState{Name: "L", Online: true, Map: "prt_fild05", Goal: "mjo_dun02"}
	if leaderDetour("mjo_dun02", lead) {
		t.Error("heading to the party map is no detour")
	}
	lead.Trip = true
	if !leaderDetour("mjo_dun02", lead) {
		t.Error("a town trip is a detour whatever the goal says")
	}
	if got := detourHold("mjo_dun02", "aldebaran", "payon", "mjolnir_02", true, false); got != "aldebaran" {
		t.Errorf("member en route waits at the checkpoint, got %s", got)
	}
	if got := detourHold("mjo_dun02", "aldebaran", "payon", "mjo_dun02", true, false); got != "mjo_dun02" {
		t.Errorf("member already on the party map stays, got %s", got)
	}
	if got := detourHold("mjo_dun02", "aldebaran", "mjolnir_02", "mjolnir_02", true, false); got != "mjolnir_02" {
		t.Errorf("leader detouring from our map: stay with it, got %s", got)
	}
	// QA 10:28: a mind's --map mjolnir_02 while the party stood together on gef_fild09: stay with the leader and
	// follow it portal by portal
	if got := detourHold("mjolnir_02", "prontera", "gef_fild09", "gef_fild09", false, false); got != "gef_fild09" {
		t.Errorf("leader setting out from our map: stay with it, got %s", got)
	}
	if got := detourHold("mjolnir_02", "prontera", "gef_fild07", "gef_fild09", false, true); got != "gef_fild07" {
		t.Errorf("leader just crossed from our map: follow it, got %s", got)
	}
	if got := detourHold("mjolnir_02", "prontera", "payon", "gef_fild09", true, true); got != "prontera" {
		t.Errorf("leader winged off on a detour: wait at the checkpoint, got %s", got)
	}
	if got := detourHold("mjolnir_02", "prontera", "geffen", "gef_fild09", false, false); got != "prontera" {
		t.Errorf("leader elsewhere, not at the checkpoint yet: meet at the checkpoint, got %s", got)
	}
	ms := []pubState{{Name: "L", Map: "gef_fild09", X: 100, Y: 100, Online: true},
		{Name: "tank", Map: "gef_fild09", X: 130, Y: 100, Online: true}, {Name: "far", Map: "geffen", X: 300, Y: 100, Online: true}}
	if s, ok := leaderStraggler(ms, "L", "gef_fild09", 100, 100); !ok || s.Name != "tank" {
		t.Error("a member 30 cells away on the leader's map is a straggler to wait for")
	}
	ms[1].X = 115
	if _, ok := leaderStraggler(ms, "L", "gef_fild09", 100, 100); ok {
		t.Error("within 20 cells: no wait; another map: not waited for here")
	}
}

// QA 2026-11-14: Echobunny fought Skeletons "on the way to Ayla" for 5+ min 58 cells from her.
func TestRegroupWalkOn(t *testing.T) {
	if !regroupWalkOn(2*time.Second, 0.9) {
		t.Error("progressing with HP: walk on")
	}
	if regroupWalkOn(10*time.Second, 0.9) {
		t.Error("no progress for 10 s (blocked): fight back")
	}
	if regroupWalkOn(time.Second, 0.3) {
		t.Error("hurt: fight back")
	}
}

// owner 2026-11-14: higher-level parties - members come to the leader; low-level parties keep leader-collects.
func TestPartyStrong(t *testing.T) {
	mk := func(name string) *Bot { return &Bot{P: &Persona{Name: name, Chars: []CharSpec{{Name: name}}}} }
	a, c := mk("strongA"), mk("strongB")
	p := &Party{Leader: a, Members: []*Bot{a, c}}
	pubFor := func(name string, power int, known bool) {
		v, _ := pubs.LoadOrStore(name, &atomic.Value{})
		v.(*atomic.Value).Store(pubState{Name: name, Online: true, Power: power, Known: known})
	}
	pubFor("strongA", 70, true)
	pubFor("strongB", 55, true)
	if !partyStrong(p) {
		t.Error("weakest effective lv55: strong")
	}
	pubFor("strongB", 45, true)
	if partyStrong(p) {
		t.Error("weakest effective lv45: not strong")
	}
	pubFor("strongB", 60, false)
	if partyStrong(p) {
		t.Error("unknown level (still logging in): not strong")
	}
}

// owner 2026-11-14: leaders announce the plan (where, why, where to meet), not "follow me".
func TestPlanLine(t *testing.T) {
	p := &Party{}
	l := planLine(p, "pay_fild09", "Ryublade's turn: bandana")
	if !strings.Contains(l, "Ryublade's turn") || !strings.Contains(l, "kafra in ") {
		t.Errorf("weak party plan line %q: want the why and the regroup Kafra", l)
	}
}

// owner rule 2026-11-14: tanks/melee protect supports that call for help, are attacked or are low on HP.
func TestSupportNeed(t *testing.T) {
	pr := pubState{Online: true, Support: true, HPFrac: 1}
	if supportNeed(pr, false) != "" {
		t.Error("a healthy, quiet priest needs nothing")
	}
	pr.Mobbed = 1
	if supportNeed(pr, false) != "being attacked" {
		t.Error("a priest being hit needs protecting")
	}
	pr.Mobbed, pr.HPFrac = 0, 0.4
	if supportNeed(pr, false) != "low hp" {
		t.Error("a priest under 50% needs protecting")
	}
	pr.Help = true
	if supportNeed(pr, false) != "called for help" {
		t.Error("a priest calling for help comes first")
	}
	knight := pubState{Online: true, HPFrac: 0.3, Mobbed: 4}
	if supportNeed(knight, false) != "" {
		t.Error("a front-liner isn't a support")
	}
	ayla := pubState{Online: true, HPFrac: 0.4}
	if supportNeed(ayla, true) != "" {
		t.Error("Ayla hurt but not calling: she runs her own fights")
	}
	ayla.Help = true
	if supportNeed(ayla, true) == "" {
		t.Error("Ayla hurt and calling for help: protect her")
	}
	humanRestLine("TestHelpHuman", "help pls", time.Now())
	if !humanCalledHelp("TestHelpHuman", time.Now()) {
		t.Error("her 'help' line is heard")
	}
}

// QA 20:23Z: friends took their own road (pay_fild01/07) while Ayla went Payon -> pay_fild08 -> pay_fild10.
func TestHumanTrail(t *testing.T) {
	now := time.Now()
	for _, m := range []string{"payon", "pay_fild08", "pay_fild10"} {
		humanTrailNote("TrailHuman", m, now)
	}
	if got := humanTrailNext("TrailHuman", "pay_fild10", "payon"); got != "pay_fild08" {
		t.Errorf("from payon follow her to pay_fild08, got %q", got)
	}
	if got := humanTrailNext("TrailHuman", "pay_fild10", "pay_fild01"); got != "" {
		t.Errorf("a map off her road: no guide, got %q", got)
	}
}
