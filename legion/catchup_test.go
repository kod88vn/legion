package main

import (
	"math/rand"
	"sync/atomic"
	"testing"
	"time"
)

// PT32b (owner 2026-11-15): a party member more than 20 cells from its leader catches up first; a monster 5 cells
// away that is not hitting it is not engaged.
func TestCatchUpBeforeEngaging(t *testing.T) {
	stickTune(t, true)
	lead := stickBot("CatchLeader", "pay_fild01", 90, 50)
	mem := stickBot("CatchMember", "pay_fild01", 50, 50)
	stickParty(t, lead, mem)
	stickPub(lead)
	stickPub(mem)
	gm := &GameMap{Name: "pay_fild01", W: 200, H: 100, cells: make([]byte, 200*100)}
	now := time.Now()
	mem.mu.Lock()
	mem.rng = rand.New(rand.NewSource(1))
	mem.ents = map[uint32]*Entity{7: {ID: 7, Type: objMob, Class: 1002, X: 55, Y: 50, DX: 55, DY: 50}}
	mem.regrouping = false
	mem.partyThink(gm, false, now)
	target, state := mem.target, mem.State
	var lastX int
	if n := len(mem.path); n > 0 {
		lastX = mem.path[n-1][0]
	}
	mem.mu.Unlock()
	if target != 0 {
		t.Fatalf("member 40 cells behind engaged monster %d (state %q)", target, state)
	}
	if lastX <= 50 {
		t.Fatalf("member did not walk toward the leader: path end x %d state %q", lastX, state)
	}
}

// A member 40 cells behind whose leader is coming to collect it does not farm the monsters next to it meanwhile.
func TestCatchUpWaitDoesNotFarm(t *testing.T) {
	stickTune(t, true)
	lead := stickBot("CatchLeader2", "pay_fild01", 90, 50)
	mem := stickBot("CatchMember2", "pay_fild01", 50, 50)
	stickParty(t, lead, mem)
	v := &atomic.Value{}
	v.Store(pubState{Name: lead.P.Name, Map: lead.MapName, X: 90, Y: 50, Online: true, Collect: mem.P.Name})
	pubs.Store(lead.P.Name, v)
	stickPub(mem)
	gm := &GameMap{Name: "pay_fild01", W: 200, H: 100, cells: make([]byte, 200*100)}
	now := time.Now()
	mem.mu.Lock()
	mem.rng = rand.New(rand.NewSource(1))
	mem.ents = map[uint32]*Entity{7: {ID: 7, Type: objMob, Class: 1002, X: 55, Y: 50, DX: 55, DY: 50}}
	mem.partyThink(gm, false, now)
	target, state := mem.target, mem.State
	mem.mu.Unlock()
	if target != 0 {
		t.Fatalf("member waiting for its leader 40 cells back farmed monster %d (state %q)", target, state)
	}
}
