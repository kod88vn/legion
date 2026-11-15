package main

import (
	"os"
	"testing"
	"time"
)

func TestKafraWaitPlan(t *testing.T) {
	cap := time.Minute
	cases := []struct {
		in   kafraWaitIn
		want string
	}{
		// TheKai2k / LilSkystorm: the held destination would stay refused after the limit: no wait at all
		{kafraWaitIn{useful: false, solo: true, cap: cap}, "repick"},
		{kafraWaitIn{useful: false, solo: false, cap: cap}, "walk"},
		// a wait that ends in a teleport: up to the cap
		{kafraWaitIn{useful: true, solo: true, waited: 30 * time.Second, cap: cap}, "wait"},
		{kafraWaitIn{useful: true, solo: true, waited: 61 * time.Second, cap: cap, wing: true}, "wing"},
		{kafraWaitIn{useful: true, solo: true, waited: 61 * time.Second, cap: cap}, "repick"},
		{kafraWaitIn{useful: true, solo: false, waited: 61 * time.Second, cap: cap}, "wait"},
		{kafraWaitIn{useful: true, solo: false, waited: 61 * time.Second, cap: cap, wing: true}, "wing"},
	}
	for i, c := range cases {
		if got := kafraWaitPlan(c.in); got != c.want {
			t.Errorf("case %d %+v: got %q want %q", i, c.in, got, c.want)
		}
	}
}

func TestGoalClock(t *testing.T) {
	var c goalClock
	t0 := time.Now()
	if d := c.step("pay_fild07", t0, 30*time.Second); d != 0 {
		t.Fatalf("first tick: %v", d)
	}
	if d := c.step("pay_fild07", t0.Add(20*time.Second), 30*time.Second); d != 20*time.Second {
		t.Fatalf("same goal: %v", d)
	}
	if d := c.step("pay_fild07", t0.Add(80*time.Second), 30*time.Second); d != 0 {
		t.Fatalf("a break over the gap restarts: %v", d)
	}
	if d := c.step("gef_fild02", t0.Add(81*time.Second), 30*time.Second); d != 0 {
		t.Fatalf("a new goal restarts: %v", d)
	}
}

func TestLongWalk(t *testing.T) {
	if longWalk(500, 900) || !longWalk(1500, 900) || longWalk(99999, 900) {
		t.Fatal("long walk: >= limit and a known route only")
	}
}

func TestWalkBan(t *testing.T) {
	b := &Bot{P: &Persona{}, MapName: "prontera"}
	defer walkBans.Delete(b)
	now := time.Now()
	b.walkBan("iz_dun00", now)
	if !b.walkBanned("iz_dun00") || !b.deadlyFor("iz_dun00") {
		t.Fatal("a given-up goal is skipped by the pickers")
	}
	if b.walkBanned("prt_fild08") {
		t.Fatal("other maps stay open")
	}
	b.MapName = "iz_dun00"
	if b.walkBanned("iz_dun00") {
		t.Fatal("never the map we stand on")
	}
	b.MapName = "prontera"
	walkBans.Store(b, map[string]time.Time{"iz_dun00": now.Add(-time.Second)})
	if b.walkBanned("iz_dun00") {
		t.Fatal("the ban runs out")
	}
}

// the no-route ping-pong (prontera <-> prt_fild08 for iz_dun00, 5 min): a solo bot gives the goal up after 90 s
func TestNoRouteGiveUp(t *testing.T) {
	b := &Bot{P: &Persona{}, MapName: "prontera", Goal: Goal{Mode: "hunt", Map: "iz_dun00", Set: "auto"}}
	defer walkBans.Delete(b)
	defer noRouteClocks.Delete(b)
	t0 := time.Now()
	for i := 0; i <= 89; i++ {
		if b.noRouteGiveUp(t0.Add(time.Duration(i) * time.Second)) {
			t.Fatalf("gave up after %d s", i)
		}
	}
	if !b.noRouteGiveUp(t0.Add(91 * time.Second)) {
		t.Fatal("no route for 90 s: give the goal up")
	}
	if b.Goal.Map != "" || !b.walkBanned("iz_dun00") {
		t.Fatalf("goal dropped and banned, got %+v", b.Goal)
	}
	// an API order is not ours to drop
	a := &Bot{P: &Persona{}, MapName: "prontera", Goal: Goal{Mode: "hunt", Map: "iz_dun00", Set: "api"}}
	defer noRouteClocks.Delete(a)
	a.noRouteGiveUp(t0)
	if a.noRouteGiveUp(t0.Add(100*time.Second)) || a.Goal.Map != "iz_dun00" {
		t.Fatal("an api goal stays")
	}
}

// TheKai2k, prontera 02:50Z: the X -> Y -> X check of one destination (the town just left) set the hard limit; a quiet
// estimate or the probe must not, and the probe ignores the hard limit altogether.
func TestKafraHardProbe(t *testing.T) {
	now := time.Now()
	b := &Bot{P: &Persona{}}
	b.kafraLast = kafraHop{At: now.Add(-1 * time.Minute), From: "alberta", To: "prontera"}
	b.kafraHops = []time.Time{b.kafraLast.At}
	if !b.kafraHoldQ("alberta", "", now, true) || !b.kafraHardUntil.IsZero() {
		t.Fatal("straight back held, but a quiet check must not record the hard limit")
	}
	b.kafraNoHard = true
	held := b.kafraHoldQ("payon", "", now, false)
	b.kafraNoHard = false
	if held || !b.kafraHardUntil.IsZero() {
		t.Fatal("probe: another destination is open and nothing recorded")
	}
	if !b.kafraHoldQ("alberta", "", now, false) || b.kafraHardUntil.IsZero() {
		t.Fatal("a real decision records the hard limit")
	}
}

// TheKai2k (Arrow Bard 50; a lv12 here so the walk rates unsafe offline too), prontera 02:50Z: back from a town run (alberta -> prontera), the hard limit recorded for
// the Alberta destination, the walk to pay_fild07 rated unsafe: it waited 10 min although the Kafra was open.
func TestKafraWaitOpenLive(t *testing.T) {
	if os.Getenv("LEGION_RATHENA") == "" {
		t.Skip("offline measurement: set LEGION_RATHENA")
	}
	*rathena = os.Getenv("LEGION_RATHENA")
	loadGameData()
	now := time.Now()
	b := &Bot{P: &Persona{BaseLevel: 12, Chars: []CharSpec{{Name: "t"}}}, BaseLv: 12, MapName: "prontera", X: 156, Y: 191, Zeny: 9000}
	b.kafraLast = kafraHop{At: now.Add(-time.Minute), From: "alberta", To: "prontera", ForTrip: true}
	b.kafraHops = []time.Time{b.kafraLast.At}
	b.kafraHardUntil = now.Add(9 * time.Minute)
	unsafe := !b.routeSafeFrom("prontera", 156, 191, "pay_fild07")
	if !unsafe {
		t.Skip("walk rated safe in this data: nothing to wait for")
	}
	if b.kafraWaitInTown("pay_fild07", now) {
		t.Fatalf("must not wait: the Kafra is open (unsafe walk %v)", unsafe)
	}
	if unsafe && b.kafra == nil {
		t.Fatal("unsafe walk and the Kafra open: the Kafra trip must be set up")
	}
	t.Logf("unsafe=%v kafra=%+v", unsafe, b.kafra)
}
