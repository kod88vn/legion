package main

import (
	"math/rand"
	"sync/atomic"
	"testing"
	"time"
)

// AY26 tests. The member flow is driven through partyThink with real mob entities (catchup_test.go's harness), so
// removing the hook in party.go fails the state assertions below.

const (
	protSupAID  = 900
	protTankAID = 901
	protArchAID = 902
)

// protTune: the rule on or off for bot parties, with the engine default off (EngineParties empty).
func protTune(t *testing.T, on bool) {
	withTuning(t, func(tn *Tuning) {
		tn.PartyProtectSupportOn = on
		tn.PartyProtectSupportOff = false
		tn.EngineParties = ""
		tn.TankPullRadius = 1 // no pulls: the hook is what runs, not the tank's own pull
	})
}

// protBot: a bot named n with class and aid on pay_fild01 at x,y.
func protBot(n string, class, aid, x, y int) *Bot {
	b := stickBot(n, "pay_fild01", x, y)
	b.Class = class
	b.aid = uint32(aid)
	return b
}

// protPub publishes b's state the way publish() does, with the given HP fraction.
func protPub(b *Bot, hp float64) {
	v := &atomic.Value{}
	v.Store(pubState{Name: b.P.Name, Map: b.MapName, X: b.X, Y: b.Y, Online: true, Support: b.isHealer(),
		Tank: partyRole(b) == roleTank, Back: partyRole(b) == roleRanged || b.isHealer(), HPFrac: hp, AID: b.aid,
		Lv: 50, Power: 50, Known: true})
	pubs.Store(b.P.Name, v)
}

// protScene: a priest (party leader, support) at 100,100 and one member of the given class/aid at fx,fy.
func protScene(t *testing.T, class, aid, fx, fy int, supHP float64) (sup, fr *Bot, gm *GameMap) {
	sup = protBot("ProtPriest", 4, protSupAID, 100, 100) // Acolyte line: isHealer, roleSupport
	fr = protBot("ProtMember", class, aid, fx, fy)
	stickParty(t, sup, fr)
	protPub(sup, supHP)
	protPub(fr, 1)
	gm = &GameMap{Name: "pay_fild01", W: 200, H: 200, cells: make([]byte, 200*200)}
	return sup, fr, gm
}

// protThink runs one member tick with the given monsters and returns the state and whether the AY26 hook fired.
func protThink(fr *Bot, gm *GameMap, ents []*Entity, now time.Time) (state string, fired bool) {
	before := protectSpotMoves.Load()
	fr.mu.Lock()
	fr.rng = rand.New(rand.NewSource(1))
	fr.ents = map[uint32]*Entity{}
	for _, e := range ents {
		fr.ents[e.ID] = e
	}
	fr.partyThink(gm, false, now)
	state = fr.State
	fr.mu.Unlock()
	return state, protectSpotMoves.Load() > before
}

func protMob(id uint32, x, y int, target uint32, at time.Time) *Entity {
	return &Entity{ID: id, Type: objMob, Class: 1002, X: x, Y: y, DX: x, DY: y, Target: target, TargetAt: at}
}

// Off: the same scene that the hook answers does nothing at all with the flag off (not an engine party).
func TestProtectSupportOffInert(t *testing.T) {
	protTune(t, false)
	_, fr, gm := protScene(t, 1, protTankAID, 92, 100, 0.6)
	now := time.Now()
	state, fired := protThink(fr, gm, []*Entity{protMob(7, 103, 100, 0, now)}, now)
	if fired || contains(state, "standing between") {
		t.Fatalf("flag off: the AY26 hook ran (state %q)", state)
	}
}

// Frontline: a monster 3 cells from a support at 60% HP, no target yet: the tank steps between them.
func TestProtectSupportFrontlineGuard(t *testing.T) {
	protTune(t, true)
	_, fr, gm := protScene(t, 1, protTankAID, 92, 100, 0.6)
	now := time.Now()
	state, fired := protThink(fr, gm, []*Entity{protMob(7, 103, 100, 0, now)}, now)
	if !fired || !contains(state, "standing between the support and the monsters") {
		t.Fatalf("frontline did not step between the support and the monster (state %q, fired %v)", state, fired)
	}
}

// The tank's own peel comes first: a monster that targets the priest is taken off it, not walked around.
func TestProtectSupportPeelFirst(t *testing.T) {
	protTune(t, true)
	_, fr, gm := protScene(t, 1, protTankAID, 92, 100, 0.6)
	now := time.Now()
	state, fired := protThink(fr, gm, []*Entity{protMob(7, 103, 100, protSupAID, now)}, now)
	if fired || !contains(state, "taking") {
		t.Fatalf("a monster on the priest was not taken off it first (state %q, fired %v)", state, fired)
	}
}

// Healthy support, monster near it that does not target it: no real threat, the frontline keeps its own routine.
func TestProtectSupportNoThreatHealthy(t *testing.T) {
	protTune(t, true)
	_, fr, gm := protScene(t, 1, protTankAID, 92, 100, 1.0)
	now := time.Now()
	state, fired := protThink(fr, gm, []*Entity{protMob(7, 103, 100, 0, now)}, now)
	if fired {
		t.Fatalf("a healthy support with no monster on it moved the frontline (state %q)", state)
	}
}

// Ranged: a monster chasing the archer (targets it, close) makes it step behind the support.
func TestProtectSupportRangedBehind(t *testing.T) {
	protTune(t, true)
	_, fr, gm := protScene(t, 3, protArchAID, 106, 100, 1.0)
	now := time.Now()
	state, fired := protThink(fr, gm, []*Entity{protMob(7, 108, 100, protArchAID, now)}, now)
	if !fired || !contains(state, "keeping behind the support") {
		t.Fatalf("chased archer did not step behind the support (state %q, fired %v)", state, fired)
	}
}

// Ranged: a monster near it that is not chasing it keeps its firing position.
func TestProtectSupportRangedNotChasedStays(t *testing.T) {
	protTune(t, true)
	_, fr, gm := protScene(t, 3, protArchAID, 106, 100, 1.0)
	now := time.Now()
	state, fired := protThink(fr, gm, []*Entity{protMob(7, 108, 100, 0, now)}, now)
	if fired {
		t.Fatalf("archer not being chased moved out of its firing position (state %q)", state)
	}
}

// Leash: 19 cells from the support (18-20: the existing 20-cell regroup has not started) and nothing hitting it: walks back first.
func TestProtectSupportLeashWalks(t *testing.T) {
	protTune(t, true)
	_, fr, gm := protScene(t, 3, protArchAID, 119, 100, 1.0)
	now := time.Now()
	state, fired := protThink(fr, gm, nil, now)
	if !fired || !contains(state, "catching up with the support") {
		t.Fatalf("member 30 cells from its support did not walk back (state %q, fired %v)", state, fired)
	}
}

// Leash with a hit: being hit far from the support, the member fights what hits it first (PT04): no walk.
func TestProtectSupportLeashSkipsBeingHit(t *testing.T) {
	protTune(t, true)
	_, fr, gm := protScene(t, 3, protArchAID, 130, 100, 1.0)
	now := time.Now()
	m := protMob(7, 131, 100, 0, now)
	m.LastHitMe = now
	state, fired := protThink(fr, gm, []*Entity{m}, now)
	if fired || contains(state, "catching up") {
		t.Fatalf("a member being hit walked away from the fight to its support (state %q)", state)
	}
}

// Walkable: the guard spot is a wall, so no move is made.
func TestProtectSupportSpotMustBeWalkable(t *testing.T) {
	protTune(t, true)
	_, fr, gm := protScene(t, 1, protTankAID, 92, 100, 0.6)
	gm.cells[102+100*gm.W] = 1
	now := time.Now()
	state, fired := protThink(fr, gm, []*Entity{protMob(7, 103, 100, 0, now)}, now)
	if fired {
		t.Fatalf("moved to a wall cell (state %q)", state)
	}
}

// No ping-pong: 12 s of member ticks (every 200 ms) with the threat staying put: at most one reposition per cooldown.
func TestProtectSupportNoPingPong(t *testing.T) {
	protTune(t, true)
	_, fr, gm := protScene(t, 1, protTankAID, 92, 100, 0.6)
	start := time.Now()
	before := protectSpotMoves.Load()
	for i := 0; i < 60; i++ {
		now := start.Add(time.Duration(i) * 200 * time.Millisecond)
		protThink(fr, gm, []*Entity{protMob(7, 103, 100, 0, now)}, now)
	}
	n := protectSpotMoves.Load() - before
	if n < 1 || n > 5 {
		t.Fatalf("repositionings over 12 s = %d, want 1..5 (cooldown %v)", n, supportSpotCooldown)
	}
}

// Lock: a member tick that repositions completes with b.mu held by the caller (no self-deadlock, no second lock).
func TestProtectSupportLockNoDeadlock(t *testing.T) {
	protTune(t, true)
	_, fr, gm := protScene(t, 1, protTankAID, 92, 100, 0.6)
	now := time.Now()
	done := make(chan bool, 1)
	go func() {
		fr.mu.Lock()
		fr.rng = rand.New(rand.NewSource(1))
		fr.ents = map[uint32]*Entity{7: protMob(7, 103, 100, 0, now)}
		fr.partyThink(gm, false, now)
		ok := contains(fr.State, "standing between")
		fr.mu.Unlock()
		done <- ok
	}()
	select {
	case ok := <-done:
		if !ok {
			t.Fatal("the hook did not run under the lock")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("partyThink with b.mu held did not return: deadlock")
	}
}

// Engine party: on by default, the off flag wins over it.
func TestProtectSupportEngineOffSwitch(t *testing.T) {
	withTuning(t, func(tn *Tuning) { tn.PartyProtectSupportOn = false; tn.EngineParties = "Ayla" })
	lead := stickBot("Ayla", "pay_fild01", 100, 100)
	p := &Party{Leader: lead, Members: []*Bot{lead}, Engine: true}
	if !partyProtectOn(p) {
		t.Fatal("engine party with no flags: the rule is not on by default")
	}
	withTuning(t, func(tn *Tuning) {
		tn.PartyProtectSupportOn = false
		tn.PartyProtectSupportOff = true
		tn.EngineParties = "Ayla"
	})
	if partyProtectOn(p) {
		t.Fatal("party_protect_support_off did not switch the engine party off")
	}
	withTuning(t, func(tn *Tuning) {
		tn.PartyProtectSupportOn = true
		tn.PartyProtectSupportOff = true
		tn.EngineParties = ""
	})
	if partyProtectOn(&Party{Members: []*Bot{lead}}) {
		t.Fatal("off flag did not win over the on flag")
	}
}

// Pure: the real-threat test and the leash boundary.
func TestSupportInDanger(t *testing.T) {
	healthy := pubState{HPFrac: 1}
	hurt := pubState{HPFrac: 0.6}
	mobbed := pubState{HPFrac: 1, Mobbed: 1}
	helping := pubState{HPFrac: 1, Help: true}
	cases := []struct {
		name string
		s    pubState
		t    bool
		d    int
		want bool
	}{
		{"monster targets support, 2 cells", healthy, true, 2, true},
		{"monster targets support, 5 cells is not near", healthy, true, 5, false},
		{"near, not targeting, healthy support", healthy, false, 2, false},
		{"near, not targeting, support hurt", hurt, false, 2, true},
		{"near, not targeting, support being hit", mobbed, false, 3, true},
		{"near, not targeting, support called for help", helping, false, 1, true},
	}
	for _, c := range cases {
		if got := supportInDanger(c.s, c.t, c.d); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func TestSupportLeashBoundary(t *testing.T) {
	if supportLeashed("m", 117, 100, "m", 100, 100) {
		t.Error("17 cells leashed (tighter than the tank pull range)")
	}
	if !supportLeashed("m", 118, 100, "m", 100, 100) {
		t.Error("18 cells not leashed")
	}
	if supportLeashed("m", 200, 100, "n", 100, 100) {
		t.Error("a support on another map is not a leash")
	}
}

func TestGuardAndBackSpots(t *testing.T) {
	if x, y, ok := guardSpot(100, 100, 110, 100, supportGuardGap); !ok || x != 102 || y != 100 {
		t.Fatalf("guardSpot east: %d,%d %v", x, y, ok)
	}
	if x, y, ok := backSpot(100, 100, 110, 100, supportBackGap); !ok || x != 97 || y != 100 {
		t.Fatalf("backSpot east: %d,%d %v", x, y, ok)
	}
	if x, y, ok := guardSpot(100, 100, 108, 108, 2); !ok || x != 102 || y != 102 {
		t.Fatalf("guardSpot diagonal: %d,%d %v", x, y, ok)
	}
	if _, _, ok := guardSpot(100, 100, 100, 100, 2); ok {
		t.Fatal("guardSpot on the support's cell picked a spot")
	}
	if _, _, ok := backSpot(100, 100, 100, 100, 3); ok {
		t.Fatal("backSpot on the support's cell picked a spot")
	}
}

func TestProtectSpotRoles(t *testing.T) {
	if x, y, _, ok := protectSpot(roleRanged, 0, 0, 200, 200, 0, 0, false, false, true); !ok || x != 200 || y != 200 {
		t.Fatalf("leashed ranged: %d,%d %v", x, y, ok)
	}
	if _, _, _, ok := protectSpot(roleTank, 60, 100, 100, 100, 108, 100, false, false, false); ok {
		t.Fatal("frontline moved with no threat")
	}
	if _, _, _, ok := protectSpot(roleRanged, 104, 100, 100, 100, 108, 100, false, false, false); ok {
		t.Fatal("ranged moved with nothing chasing it")
	}
	if _, _, _, ok := protectSpot(roleTank, 102, 100, 100, 100, 108, 100, true, false, false); ok {
		t.Fatal("frontline already between moved again")
	}
	if _, _, _, ok := protectSpot(roleSupport, 60, 100, 100, 100, 108, 100, true, true, false); ok {
		t.Fatal("support role moved by this rule")
	}
}

func TestSupportOfPicksNearest(t *testing.T) {
	far := pubState{Name: "Far", Map: "m", X: 90, Y: 10, Online: true, Support: true}
	near := pubState{Name: "Near", Map: "m", X: 20, Y: 10, Online: true, Support: true}
	members := []pubState{{Name: "Me", Map: "m", X: 10, Y: 10, Online: true}, far, near}
	s, ok := supportOf(members, "Me", "m", 10, 10)
	if !ok || s.Name != "Near" {
		t.Fatalf("support of Me: %+v %v", s, ok)
	}
	if _, ok := supportOf(members[:1], "Me", "m", 10, 10); ok {
		t.Fatal("a party with no other support reported one")
	}
}

func contains(s, sub string) bool {
	return len(sub) == 0 || len(s) >= len(sub) && (s == sub || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
