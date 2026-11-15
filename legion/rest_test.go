package main

import (
	"strings"
	"testing"
	"time"
)

// owner rule 2026-11-14 "Rest together; no fidgeting": while a member rests, everyone within restHoldCells holds
// position - sits if it needs regen, else guards - and the leader doesn't collect / roam / relocate.
func TestRestTogether(t *testing.T) {
	members := []pubState{
		{Name: "me", Map: "pay_fild04", X: 100, Y: 100, Online: true, HPFrac: 1},
		{Name: "priest", Map: "pay_fild04", X: 110, Y: 100, Online: true, HPFrac: 1, Resting: true},
		{Name: "far", Map: "pay_fild04", X: 160, Y: 100, Online: true, HPFrac: 1, Resting: true},
	}
	if r, ok := partyRester(members, "me", "pay_fild04", 100, 100); !ok || r.Name != "priest" {
		t.Fatalf("resting priest 10 cells away not found: %v %v", r.Name, ok)
	}
	if _, ok := partyRester(members[:1], "me", "pay_fild04", 100, 100); ok {
		t.Error("nobody else resting: no party rest")
	}
	self := []pubState{{Name: "me", Map: "pay_fild04", X: 100, Y: 100, Online: true, Resting: true}}
	if _, ok := partyRester(self, "me", "pay_fild04", 100, 100); ok {
		t.Error("my own rest is not a party rest to hold for")
	}
	if _, ok := partyRester(members[2:], "me", "pay_fild04", 100, 100); ok {
		t.Error("a rester 60 cells away is not ours to hold for")
	}
	if _, ok := partyRester(members[1:2], "me", "pay_fild03", 100, 100); ok {
		t.Error("a rester on another map is not ours to hold for")
	}
	if !memberInDanger(pubState{Online: true, Target: 5, HPFrac: 0.3}) || memberInDanger(pubState{Online: true, HPFrac: 0.3}) {
		t.Error("danger = fighting below half HP")
	}

	b := &Bot{P: &Persona{BaseLevel: 40, Chars: []CharSpec{{Name: "me"}}}, HP: 700, MaxHP: 700, SP: 100, MaxSP: 100,
		X: 100, Y: 100, DestX: 100, DestY: 100, MapName: "pay_fild04", ents: map[uint32]*Entity{},
		inv: map[int]*InvItem{}, equipped: map[uint32]*InvItem{}, buffAt: map[string]time.Time{}}
	b.wantSit = true
	if !b.restGuard(members, members[1], false, time.Now()) || !strings.HasPrefix(b.State, "guarding priest") {
		t.Errorf("full HP/SP during a rest: want guarding, got %q", b.State)
	}
	if b.wantSit || b.restingNow() {
		t.Error("a guard at full HP/SP must not count as resting (a rest would keep itself going)")
	}
	// a member in danger out of reach breaks the rest
	danger := append([]pubState(nil), members...)
	danger = append(danger, pubState{Name: "tank", Map: "pay_fild04", X: 130, Y: 100, Online: true, Target: 9, HPFrac: 0.2})
	if b.restGuard(danger, members[1], false, time.Now()) {
		t.Error("a member in danger 30 cells away must break the hold")
	}
	b.State = "resting for SP"
	if !b.restingNow() {
		t.Error(`"resting for SP" is resting`)
	}
}

// owner rule 2026-11-14 "Announce rests and their end": Ayla's party lines start and end the friends' rest; her
// sitting alone counts only for a minute.
func TestHumanRestAnnouncements(t *testing.T) {
	now := time.Now()
	name := "TestHumanRest"
	if humanResting(name, now) {
		t.Fatal("no announcement, not sitting: not resting")
	}
	humanRestLine(name, "ok resting a bit", now)
	if !humanResting(name, now.Add(30*time.Second)) {
		t.Error(`"ok resting a bit" starts a rest`)
	}
	if humanResting(name, now.Add(humanRestMax+time.Second)) {
		t.Error("an announced rest with no ready and no sitting ends after humanRestMax")
	}
	humanSitSeen(name, true, now)
	humanRestLine(name, "ready, lets go", now.Add(time.Minute))
	if humanResting(name, now.Add(61*time.Second)) {
		t.Error(`"ready" ends the rest even while she still sits`)
	}
	for _, l := range []string{"lets go", "moving on", "done resting, lets go", "rdy"} {
		humanRestLine(name, "resting", now)
		humanRestLine(name, l, now)
		if humanResting(name, now) {
			t.Errorf("%q should end a rest", l)
		}
	}
	humanRestLine(name, "the drop was nice", now) // unrelated lines change nothing
	if humanResting(name, now) {
		t.Error("unrelated line started a rest")
	}
	// sit fallback: only without a recent announcement, at most humanSitTimeout
	other := "TestHumanSit"
	humanSitSeen(other, true, now)
	if !humanResting(other, now.Add(10*time.Second)) || humanResting(other, now.Add(humanSitTimeout+time.Second)) {
		t.Error("unannounced sitting counts as resting for humanSitTimeout only")
	}
	humanSitSeen(other, false, now)
	if humanResting(other, now) {
		t.Error("standing: not resting")
	}
	// party chat line format
	p := &Party{Human: "Ayla"}
	humanRestHeard(p, "Ayla : need sp, resting", now)
	if !humanResting("Ayla", now) {
		t.Error(`"Ayla : need sp, resting" not heard`)
	}
	humanRestHeard(p, "Someone : ready", now)
	if !humanResting("Ayla", now) {
		t.Error("another member's ready must not end Ayla's rest")
	}
	humanRestHeard(p, "Ayla : ok lets go", now)
	if humanResting("Ayla", now) {
		t.Error(`"Ayla : ok lets go" must end the rest`)
	}
}

// owner 2026-11-14: a non-healer leader's breather (Ayla's short SP sit) is not a party stop - fighters keep farming
// near her; a healer's rest, or a rest of an ordinary member, keeps the old hold.
func TestRestFarmForLeaderBreather(t *testing.T) {
	b := &Bot{P: &Persona{BaseLevel: 40, Chars: []CharSpec{{Name: "zz_rfguard"}}}}
	p := &Party{Human: "Ayla", Members: []*Bot{b}}
	parties.Lock()
	parties.ofBot[b] = p
	parties.Unlock()
	defer func() { parties.Lock(); delete(parties.ofBot, b); parties.Unlock() }()
	if !restFarmFor(b, pubState{Name: "ayla"}) {
		t.Error("the human leader resting: fighters keep farming")
	}
	if restFarmFor(b, pubState{Name: "Ayla", Support: true}) {
		t.Error("a healer leader resting: the party holds")
	}
	if restFarmFor(b, pubState{Name: "zz_priest"}) {
		t.Error("a member (not the leader) resting: the party holds")
	}
}

// QA 11:04: after a restart the save point was unknown (empty) and the checkpoint save detour fired anyway.
func TestKnownSave(t *testing.T) {
	b := &Bot{P: &Persona{Account: "test-knownsave-none", Chars: []CharSpec{{Name: "ks"}}}}
	if _, ok := b.knownSave(); ok {
		t.Fatal("no save seen and nothing remembered: unknown")
	}
	b.noteSaved("geffen")
	b2 := &Bot{P: b.P} // a fresh session of the same character (same memory)
	if s, ok := b2.knownSave(); !ok || s != "geffen" {
		t.Errorf("remembered save = %q %v, want geffen", s, ok)
	}
}

// QA 2026-11-14 11:02-11:30: party healer deaths 4 -> 28: healers sat with monsters coming and pulled on their own.
func TestHealerThreat(t *testing.T) {
	now := time.Now()
	mobs[90001] = &Mob{ID: 90001, Name: "AggroTest", Level: 20, HP: 500, Aggressive: true, Exp: 10}
	mobs[90002] = &Mob{ID: 90002, Name: "PassiveTest", Level: 20, HP: 500, Exp: 10}
	defer delete(mobs, 90001)
	defer delete(mobs, 90002)
	b := &Bot{X: 100, Y: 100, DestX: 100, DestY: 100, MapName: "m", aid: 1, ents: map[uint32]*Entity{}}
	if b.threatNear(nil, 7, now) {
		t.Fatal("nothing around: no threat")
	}
	b.ents[5] = &Entity{ID: 5, Type: objMob, Class: 90002, X: 104, Y: 100, DX: 104, DY: 100}
	if b.threatNear(nil, 7, now) {
		t.Error("a passive monster minding its own business is no threat")
	}
	b.ents[5].Target, b.ents[5].TargetAt = 7, now
	if !b.threatNear([]pubState{{Name: "tank", Map: "m", AID: 7}}, 7, now) {
		t.Error("a monster on a party member 4 cells away is a threat")
	}
	b.ents[6] = &Entity{ID: 6, Type: objMob, Class: 90001, X: 106, Y: 100, DX: 106, DY: 100}
	delete(b.ents, 5)
	if !b.threatNear(nil, 7, now) || b.threatNear(nil, 5, now) {
		t.Error("an aggressive monster 6 cells away: threat within 7, not within 5")
	}
	if !healerWalkOn(time.Second) || healerWalkOn(10*time.Second) {
		t.Error("a healer walks on while it progresses, even hurt; fights back only when blocked")
	}
}

// QA 2026-11-14 12:10: Zen.fox (Wizard lv72, Ayla's party) had 0 potions and 1,024z and never asked for a town run.
func TestBrokeOutOfPotions(t *testing.T) {
	items[501] = &Item{ID: 501, Name: "Red Potion", Type: "Healing", Buy: 50, Weight: 70}
	b := &Bot{P: &Persona{BaseLevel: 72, Chars: []CharSpec{{Name: "brokepots"}}}, invSeen: true, Zeny: 1024, Class: 9,
		MaxWeight: 24300, Weight: 3380, MapName: "pay_dun01", MaxHP: 3000, HP: 3000,
		inv: map[int]*InvItem{}, equipped: map[uint32]*InvItem{}, gearFailed: map[int]bool{}}
	if got := b.needTrip(); got != "out of potions" {
		t.Errorf("0 potions with 1,024z: needTrip %q, want out of potions", got)
	}
}
