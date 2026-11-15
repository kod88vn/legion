package main

import (
	"sync/atomic"
	"testing"
	"time"
)

// stickBot: a test bot named n on map m at x,y, published like publish() does.
func stickBot(n, m string, x, y int) *Bot {
	b := &Bot{P: &Persona{Name: n, Chars: []CharSpec{{Name: n}}}, MapName: m, X: x, Y: y, DestX: x, DestY: y,
		Online: true, buffAt: map[string]time.Time{}, MaxHP: 100, HP: 100}
	stickPub(b)
	return b
}

func stickPub(b *Bot) {
	v := &atomic.Value{}
	v.Store(pubState{Name: b.P.Name, Map: b.MapName, X: b.X, Y: b.Y, Online: true})
	pubs.Store(b.P.Name, v)
}

func stickParty(t *testing.T, lead *Bot, ms ...*Bot) {
	p := &Party{Leader: lead, Members: append([]*Bot{lead}, ms...)}
	parties.Lock()
	for _, m := range p.Members {
		parties.ofBot[m] = p
	}
	parties.Unlock()
	t.Cleanup(func() {
		parties.Lock()
		for _, m := range p.Members {
			delete(parties.ofBot, m)
			pubs.Delete(m.P.Name)
		}
		parties.Unlock()
	})
}

func stickTune(t *testing.T, on bool) {
	old := curTuning.Load()
	nt := defaultTuning
	nt.PartyStickOn, nt.PartyStickEscapeOn = on, on
	curTuning.Store(&nt)
	t.Cleanup(func() {
		if old != nil {
			curTuning.Store(old)
		} else {
			curTuning.Store(&defaultTuning)
		}
	})
}

// apart: after the action the bot stands beyond together_cells of a party mate it was within (or farther than before).
func apart(fx, fy, x, y int, refs [][2]int) bool {
	return !stickAllows(fx, fy, x, y, refs, togetherCells())
}

// The grinding cases behind the owner's "staying close to the party is rule number one" (2026-11-14) and the live
// partycheck "party apart" items: each is one action a party member or leader took on the party map. Before the gate
// every one of them left the party; with party_stick_on none does.
func TestPartyStickCases(t *testing.T) {
	now := time.Now()
	type tc struct {
		name         string
		leader       bool
		mx, my       int // the bot
		ox, oy       int // the other party member (the leader for a member, the slow member for a leader)
		tx, ty       int // where the action takes it (target / drop / walk)
		wantFiltered bool
	}
	cases := []tc{
		{"member chases a monster 25 cells past the leader", false, 105, 100, 100, 100, 130, 100, true},
		{"member loots a drop 22 cells from the leader", false, 110, 100, 100, 100, 122, 100, true},
		{"member roams 12 cells outward at 15 from the leader", false, 115, 100, 100, 100, 127, 100, true},
		{"member resting far away walks off (low hp: resting)", false, 100, 118, 100, 100, 100, 135, true},
		{"leader sweeps away from a slow member", true, 110, 100, 100, 100, 135, 100, true},
		// allowed moves
		{"member fights next to the leader", false, 105, 100, 100, 100, 112, 104, false},
		{"member far off regroups", false, 140, 100, 100, 100, 125, 100, false},
		{"leader steps toward the member", true, 110, 100, 100, 100, 104, 100, false},
	}
	for _, on := range []bool{false, true} {
		stickTune(t, on)
		left := 0
		for _, c := range cases {
			var self, other *Bot
			if c.leader {
				self, other = stickBot("L-"+c.name, "gef_fild07", c.mx, c.my), stickBot("M-"+c.name, "gef_fild07", c.ox, c.oy)
				stickParty(t, self, other)
			} else {
				self, other = stickBot("M-"+c.name, "gef_fild07", c.mx, c.my), stickBot("L-"+c.name, "gef_fild07", c.ox, c.oy)
				stickParty(t, other, self)
			}
			ok := self.stickNear(c.tx, c.ty, now)
			if on && ok == c.wantFiltered {
				t.Errorf("%s: stickNear=%v, want filtered=%v", c.name, ok, c.wantFiltered)
			}
			if ok && apart(c.mx, c.my, c.tx, c.ty, [][2]int{{c.ox, c.oy}}) {
				left++
			}
		}
		t.Logf("party_stick_on=%v: %d of %d grinding actions leave the party", on, left, len(cases))
		if on && left != 0 || !on && left != 5 {
			t.Errorf("party_stick_on=%v: %d actions leave the party", on, left)
		}
	}
}

func TestPartyStickNoLimit(t *testing.T) {
	stickTune(t, true)
	now := time.Now()
	l := stickBot("Lx", "gef_fild07", 100, 100)
	m := stickBot("Mx", "gef_fild07", 105, 100)
	stickParty(t, l, m)
	if m.stickNear(150, 100, now) {
		t.Fatal("grinding member: 45 cells past the leader must be refused")
	}
	m.Goal = Goal{Mode: "party", Map: "geffen"}
	if !m.stickNear(150, 100, now) {
		t.Fatal("travelling to another map: no limit")
	}
	m.Goal = Goal{}
	m.trip = &Trip{}
	if !m.stickNear(150, 100, now) {
		t.Fatal("town trip (PT22-24): no limit")
	}
	m.trip = nil
	m.MapName = "geffen"
	if !m.stickNear(150, 100, now) {
		t.Fatal("in town: no limit")
	}
	m.MapName = "gef_fild07"
	solo := stickBot("Solo", "gef_fild07", 100, 100)
	if !solo.stickNear(190, 100, now) {
		t.Fatal("solo bot: no limit")
	}
}

func TestStickClamp(t *testing.T) {
	refs := [][2]int{{100, 100}}
	x, y, ok := stickClamp(nil, 105, 100, 140, 100, refs, 20)
	if !ok || x != 120 || y != 100 {
		t.Fatalf("clamp 105->140 around 100: got %d,%d ok=%v, want 120,100", x, y, ok)
	}
	if _, _, ok := stickClamp(nil, 120, 100, 140, 100, refs, 20); ok {
		t.Fatal("at the edge walking straight out: no step allowed")
	}
}

// PT31 (Ayla's random Teleport to gef_fild08 2026-11-14, Zen.fox's Fly Wing into Drainliars): with a party mate on
// the map a losing fight runs to it instead of a random cell; about to die without a Butterfly Wing it still runs.
func TestStickEscapeRunsToMate(t *testing.T) {
	stickTune(t, true)
	now := time.Now()
	l := stickBot("Ls", "gef_fild07", 100, 100)
	m := stickBot("Ms", "gef_fild07", 115, 100)
	stickParty(t, l, m)
	h, fled := m.stickEscape(nil, 0.5, 1, false, true, now, "test")
	if !h || !fled || m.fleeTo != [2]int{100, 100} {
		t.Fatalf("losing with the leader on the map: want a run to it, got handled=%v fled=%v fleeTo=%v", h, fled, m.fleeTo)
	}
	h, fled = m.stickEscape(nil, 0.1, 1, false, true, now, "test")
	if !h || !fled || m.fleeTo != [2]int{100, 100} {
		t.Fatalf("nearly dead, no Butterfly Wing: still run to the party, got handled=%v fled=%v", h, fled)
	}
	m.X, m.DestX = 101, 101
	if h, fled := m.stickEscape(nil, 0.5, 1, false, true, now, "test"); !h || fled {
		t.Fatalf("next to the mate: fight on here, got handled=%v fled=%v", h, fled)
	}
	// the leader runs to its nearest member too
	if h, _ := l.stickEscape(nil, 0.5, 1, false, true, now, "test"); !h {
		t.Fatal("leader with a member on the map: PT31 applies")
	}
	// alone on the map: solo rules
	m.MapName = "gef_fild08"
	stickPub(m)
	if h, _ := m.stickEscape(nil, 0.5, 1, false, true, now, "test"); h {
		t.Fatal("no party mate on the map: solo escape rules")
	}
	stickTune(t, false)
	m.MapName = "gef_fild07"
	stickPub(m)
	if h, _ := m.stickEscape(nil, 0.5, 1, false, true, now, "test"); h {
		t.Fatal("flag off: old rules")
	}
}

// bug b1794746339362-336 (QA 12:37:37): Ayla's members Butterfly-winged to Payon at 10-28% HP with the priest on their
// map. Near death with a healer here (or a potion left) a party member stays: drinks, asks for a heal, backs off to the
// priest. The wing-out stays for a party member with no healer here and nothing to drink.
func TestStickEscapeNoWingWithHealer(t *testing.T) {
	stickTune(t, true)
	now := time.Now()
	l := stickBot("Lh", "pay_dun02", 100, 100)
	pr := stickBot("Ph", "pay_dun02", 110, 100)
	m := stickBot("Mh", "pay_dun02", 130, 100)
	v := &atomic.Value{}
	v.Store(pubState{Name: "Ph", Map: "pay_dun02", X: 110, Y: 100, Online: true, Support: true})
	pubs.Store("Ph", v)
	stickParty(t, l, pr, m)
	m.savedTown = "payon"
	m.inv = map[int]*InvItem{1: {Index: 1, ItemID: itemButterflyWing, Count: 2}}
	t.Cleanup(func() { escPend.Delete(m); escHold.Delete(m); escHoldIdx.Delete(m) })

	// healer on the map, no potions: no wing, run to the priest
	h, fled := m.stickEscape(nil, 0.12, 2, false, true, now, "test")
	if _, winged := escPend.Load(m); winged {
		t.Fatal("priest on the map: must not Butterfly Wing out")
	}
	if !h || !fled || m.fleeTo != [2]int{110, 100} {
		t.Fatalf("priest on the map: want a run to the priest, got handled=%v fled=%v fleeTo=%v", h, fled, m.fleeTo)
	}
	if m.buffAt["healcall"].IsZero() {
		t.Fatal("priest on the map: want a heal request in party chat")
	}

	// no healer here but a potion: drink, stay with the party
	pr.MapName = "pay_dun03"
	stickPub(pr)
	m.inv[2] = &InvItem{Index: 2, ItemID: itemRedPotion, Count: 5}
	m.potAt = time.Time{}
	if h, _ := m.stickEscape(nil, 0.12, 2, false, true, now, "test"); !h {
		t.Fatal("party on the map: PT31 applies")
	}
	if _, winged := escPend.Load(m); winged {
		t.Fatal("potion left: must drink, not wing")
	}
	if !m.potAt.After(now) {
		t.Fatal("potion left: want a potion drunk")
	}

	// no healer here, no potion, nearly dead: the announced wing to the save Kafra
	delete(m.inv, 2)
	if h, fled := m.stickEscape(nil, 0.12, 2, false, true, now, "test"); !h || !fled {
		t.Fatalf("no healer, no potion: want the wing, got handled=%v fled=%v", h, fled)
	}
	if _, winged := escPend.Load(m); !winged {
		t.Fatal("no healer, no potion, nearly dead: want the Butterfly Wing")
	}
}
