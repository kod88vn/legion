package main

import (
	"encoding/binary"
	"testing"
	"time"
)

// owner 2026-11-14 (RULES.md 3b): Teleport card for every bot; DS Hunters hit and run.
func TestTeleportCardAndSkill(t *testing.T) {
	// the warp list of our own Teleport cast is answered with "Random"
	pk := make([]byte, 6+16)
	binary.LittleEndian.PutUint16(pk, 0x0abe)
	binary.LittleEndian.PutUint16(pk[2:], uint16(len(pk)))
	binary.LittleEndian.PutUint16(pk[4:], alTeleport)
	copy(pk[6:], "Random")
	r := warpListReply(pk)
	if len(r) != 4+16 || binary.LittleEndian.Uint16(r) != 0x011b || binary.LittleEndian.Uint16(r[2:]) != alTeleport || string(r[4:10]) != "Random" {
		t.Fatalf("warp list reply % x", r)
	}
	binary.LittleEndian.PutUint16(pk[4:], 27) // Warp Portal: not ours to answer
	if warpListReply(pk) != nil {
		t.Fatal("only Teleport's list is answered")
	}
	now := time.Now()
	b := &Bot{MapName: "pay_fild09", SP: 50, MaxSP: 200, skills: map[int]*SkillInfo{alTeleport: {Lv: 1, SP: 10}},
		buffAt: map[string]time.Time{}, skillFailUntil: map[int]time.Time{}}
	if !b.canSkillTele(now) {
		t.Fatal("Teleport (card) usable on a field")
	}
	b.MapName = "prontera"
	if b.canSkillTele(now) {
		t.Fatal("no Teleport in town")
	}
	b.MapName = "pay_fild09"
	b.buffAt["tele:no:pay_fild09"] = now.Add(time.Minute)
	if b.canSkillTele(now) {
		t.Fatal("a map that refused Teleport is skipped")
	}
	// the Creamy Card outranks stat cards for any build
	k := &Bot{Class: 7}
	k.P = &Persona{Chars: []CharSpec{{Plan: "knight", Role: "main"}}}
	cardBonOnce.Do(func() {})
	if v := k.cardValue(itemCreamy); v < 60 {
		t.Fatalf("Creamy Card value %d", v)
	}
}

func TestKiteCell(t *testing.T) {
	m := &GameMap{Name: "kite_test", W: 30, H: 30, cells: make([]byte, 900)}
	x, y, ok := kiteCell(m, 10, 10, 11, 10) // monster east of us: step west
	if !ok || x >= 10 || dist(x, y, 11, 10) < 5 {
		t.Fatalf("kite cell %d,%d %v", x, y, ok)
	}
	m = &GameMap{Name: "kite_test2", W: 30, H: 30, cells: make([]byte, 900)}
	for i := 0; i < 30; i++ { // wall west of us: any other free direction
		m.cells[i*30+5] = 1
	}
	if x, y, ok := kiteCell(m, 7, 10, 8, 10); !ok || !m.Walkable(x, y) || x <= 5 {
		t.Fatalf("cornered kite cell %d,%d %v", x, y, ok)
	}
}
