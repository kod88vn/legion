package main

import (
	"os"
	"sync"
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

// party member past the cap at a Kafra town (kafraMemberWait): the plan order, one case per branch
func TestMemberWaitPlan(t *testing.T) {
	cap, memberCap := time.Minute, 3*time.Minute
	base := memberWaitIn{useful: true, waited: 61 * time.Second, cap: cap, memberCap: memberCap}
	cases := []struct {
		name string
		mod  func(*memberWaitIn)
		want string
	}{
		{"before the cap keeps waiting", func(m *memberWaitIn) { m.waited = 30 * time.Second }, "wait"},
		{"no teleport possible: walk as before", func(m *memberWaitIn) { m.useful = false }, "walk"},
		{"branch a: carried wing is used", func(m *memberWaitIn) { m.wing = true; m.buy = true }, "wing"},
		{"branch a: buy a wing here (once per goal)", func(m *memberWaitIn) { m.buy = true }, "buy"},
		{"branch b: safe walk", func(m *memberWaitIn) { m.safe = true }, "walk"},
		{"branch c: unsafe, ask the party to collect us, once", func(m *memberWaitIn) {}, "say"},
		{"branch c: told already, a member keeps waiting (no give-up)", func(m *memberWaitIn) { m.said = true; m.waited = 10 * time.Minute }, "wait"},
		{"a member never relocates the party", func(m *memberWaitIn) { m.said = true; m.waited = 10 * time.Minute; m.leader = false }, "wait"},
		{"a stuck leader moves the party after memberCap", func(m *memberWaitIn) { m.leader = true; m.said = true; m.waited = 3 * time.Minute }, "relocate"},
		{"a leader moves the party once per goal", func(m *memberWaitIn) { m.leader = true; m.said = true; m.moved = true; m.waited = 10 * time.Minute }, "wait"},
		{"a leader before memberCap keeps waiting", func(m *memberWaitIn) { m.leader = true; m.said = true; m.waited = 2 * time.Minute }, "wait"},
	}
	for _, c := range cases {
		in := base
		c.mod(&in)
		if got := memberWaitPlan(in); got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}

// memberRig: a bot in a party (leader or member) with the real kafraMemberWait; the trip, the relocation and the
// save-town check are recorded through the test hooks (restored by done)
type memberRig struct {
	mu     sync.Mutex
	trips  []string
	relocs []string
	done   func()
}

func (r *memberRig) relocCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.relocs)
}

func (r *memberRig) tripCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.trips)
}

// memberRigSetup: bot b (goal mt_goal by a party) at a Kafra town mt_town; saveBetter = savedTown is a better start;
// b leads the party when leader. Returns the bot, its party clock and the rig.
func memberRigSetup(t *testing.T, leader, saveBetter bool) (*Bot, *goalClock, *memberRig) {
	r := &memberRig{}
	oldTrip, oldReloc, oldSold, oldSave := memberBuyTrip, leaderRelocate, wingSoldHere, savedStartBetter
	memberBuyTrip = func(b *Bot, why string, now time.Time) {
		r.mu.Lock()
		r.trips = append(r.trips, why)
		r.mu.Unlock()
	}
	leaderRelocate = func(p *Party, from string, x, y int, why string) {
		r.mu.Lock()
		r.relocs = append(r.relocs, from)
		r.mu.Unlock()
	}
	wingSoldHere = func(string) bool { return true }
	savedStartBetter = func(b *Bot, goal string) bool { return saveBetter }
	r.done = func() {
		memberBuyTrip, leaderRelocate, wingSoldHere, savedStartBetter = oldTrip, oldReloc, oldSold, oldSave
	}
	t.Cleanup(r.done)

	lead := &Bot{P: &Persona{Name: "mt_lead", Account: "mt_test_lead", Chars: []CharSpec{{Name: "MtLead"}}}, BaseLv: 50, Zeny: 100000, MapName: "mt_town", Online: true, buffAt: map[string]time.Time{}}
	b := &Bot{P: &Persona{Name: "mt_mem", Account: "mt_test_mem", Chars: []CharSpec{{Name: "MtMem"}}}, BaseLv: 50, Zeny: 100000, MapName: "mt_town", X: 10, Y: 10, Online: true, buffAt: map[string]time.Time{}}
	b.Goal = Goal{Mode: "party", Map: "mt_goal", Set: "party"}
	p := &Party{Name: "mt_party", Leader: lead, Members: []*Bot{lead, b}, Map: "mt_goal"}
	if leader {
		p.Leader = b
	}
	parties.Lock()
	parties.ofBot[lead], parties.ofBot[b] = p, p
	parties.Unlock()
	t.Cleanup(func() {
		parties.Lock()
		delete(parties.ofBot, lead)
		delete(parties.ofBot, b)
		parties.Unlock()
		kafraWaitClocks.Delete(b)
		kafraBuys.Delete(b)
		walkBans.Delete(b)
	})

	c := clockOf(&kafraWaitClocks, b)
	return b, c, r
}

// memberWait: one kafraMemberWait tick for goal, the wait clock at waited (a fresh clock when it was reset)
func memberWait(b *Bot, c *goalClock, waited time.Duration) bool {
	now := time.Now()
	c.goal, c.since, c.seen = "mt_goal", now.Add(-waited), now
	return b.kafraMemberWait("mt_goal", now, c, kafraWaitIn{useful: true, solo: false, waited: waited, cap: time.Minute})
}

// through kafraMemberWait itself: a member never gives up its goal; a stuck leader moves the party once; a wing is
// bought once per goal (a clock reset after the trip does not buy again)
func TestKafraMemberWaitThroughBot(t *testing.T) {
	t.Run("member asks to be collected, keeps the goal", func(t *testing.T) {
		b, c, r := memberRigSetup(t, false, false)
		memberWait(b, c, 61*time.Second)
		if c.saidGoal != "mt_goal" {
			t.Fatal("the member must tell the party once")
		}
		if memberWait(b, c, 10*time.Minute) {
			t.Fatal("a member keeps waiting at the Kafra, it does not walk away")
		}
		if b.Goal.Map != "mt_goal" || b.Goal.Mode != "party" {
			t.Fatalf("the member split from its party: goal %+v", b.Goal)
		}
		if _, banned := walkBans.Load(b); banned {
			t.Fatal("the member's party map must not be banned for it")
		}
		if r.relocCount() != 0 || r.tripCount() != 0 {
			t.Fatalf("a member moves nothing: relocs %d trips %d", r.relocCount(), r.tripCount())
		}
	})

	t.Run("stuck leader moves the whole party once, keeps its own goal", func(t *testing.T) {
		b, c, r := memberRigSetup(t, true, false)
		memberWait(b, c, 61*time.Second)
		memberWait(b, c, 4*time.Minute)
		if b.Goal.Map != "mt_goal" {
			t.Fatalf("the leader must not ban the party map for itself: goal %+v", b.Goal)
		}
		deadline := time.Now().Add(2 * time.Second)
		for r.relocCount() == 0 && time.Now().Before(deadline) {
			time.Sleep(5 * time.Millisecond)
		}
		if r.relocCount() != 1 {
			t.Fatalf("relocations %d, want 1", r.relocCount())
		}
		memberWait(b, c, 6*time.Minute)
		time.Sleep(20 * time.Millisecond)
		if r.relocCount() != 1 {
			t.Fatalf("the party moved again for the same goal: %d", r.relocCount())
		}
	})

	t.Run("buy once per goal, a clock reset after the trip does not buy again", func(t *testing.T) {
		b, c, r := memberRigSetup(t, false, true)
		if !memberWait(b, c, 61*time.Second) {
			t.Fatal("a buy trip is started: the bot does not wait here")
		}
		if r.tripCount() != 1 {
			t.Fatalf("trips %d after the first buy, want 1", r.tripCount())
		}
		// back in town: the wait clock starts again from zero (the trip reset it), the buy failed (no wing carried)
		memberWait(b, c, 61*time.Second)
		memberWait(b, c, 61*time.Second)
		if r.tripCount() != 1 {
			t.Fatalf("trips %d: the buy loops", r.tripCount())
		}
		if c.saidGoal != "mt_goal" {
			t.Fatal("after the failed buy the member tells the party instead")
		}
		if b.Goal.Map != "mt_goal" {
			t.Fatalf("the goal changed: %+v", b.Goal)
		}
	})

	t.Run("no buy when the save town is no better start", func(t *testing.T) {
		b, c, r := memberRigSetup(t, false, false)
		memberWait(b, c, 61*time.Second)
		if r.tripCount() != 0 {
			t.Fatalf("a wing that would go unused was bought: trips %d", r.tripCount())
		}
	})
}

// kafra_member_wait_off restores the open-ended member wait; the default turns the rule on
func TestKafraMemberWaitFlag(t *testing.T) {
	defer curTuning.Store(&defaultTuning)
	curTuning.Store(&defaultTuning)
	if !kafraMemberWaitOn() {
		t.Fatal("member wait rule must be on by default")
	}
	off := defaultTuning
	off.KafraMemberWaitOff = true
	curTuning.Store(&off)
	if kafraMemberWaitOn() {
		t.Fatal("kafra_member_wait_off must switch the member rule off")
	}
	if kafraMemberWaitCap() != 180*time.Second {
		t.Fatalf("default member cap %v, want 3 min", kafraMemberWaitCap())
	}
}
