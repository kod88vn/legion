package main

import (
	"strings"
	"testing"
	"time"
)

func resetConvoState() {
	convoState.Lock()
	convoState.lastBot = map[*Bot]time.Time{}
	convoState.lastPlace = map[string]time.Time{}
	convoState.started = nil
	convoState.Unlock()
}

// G18: no line may hint that a player is a bot, an AI, a script or a simulation
var convoTells = []string{"bot", " ai", "ai ", "script", "simulat", "npc", "program", "auto", "algorithm", "legion"}

func checkConvo(t *testing.T, lines []convoLine) {
	t.Helper()
	if len(lines) < 2 || len(lines) > 4 {
		t.Fatalf("a conversation has 2-4 lines, got %d: %v", len(lines), lines)
	}
	if lines[0].who != 0 {
		t.Fatalf("the first speaker starts: %v", lines)
	}
	seen := map[int]bool{}
	for i, l := range lines {
		seen[l.who] = true
		if l.who != 0 && l.who != 1 {
			t.Fatalf("bad speaker %d", l.who)
		}
		if i > 0 && l.who == lines[i-1].who {
			t.Fatalf("speakers alternate: %v", lines)
		}
		if l.text == "" || len(l.text) > 80 {
			t.Fatalf("bad line %q", l.text)
		}
		low := " " + strings.ToLower(l.text) + " "
		for _, w := range convoTells {
			if strings.Contains(low, w) {
				t.Fatalf("line %q contains %q (G18)", l.text, w)
			}
		}
	}
	if !seen[1] {
		t.Fatalf("both speak: %v", lines)
	}
}

func TestConvoLinesNatural(t *testing.T) {
	a := convoSide{name: "Kira", class: "Knight", lv: 52, mapN: "prontera", hunt: "gef_fild10", want: "Claymore", guild: "Crimson Oath"}
	b := convoSide{name: "Mel", class: "High Wizard", lv: 44, mapN: "prontera", hunt: "gl_church", loot: "Jellopy", guild: "Crimson Oath"}
	plain := convoSide{name: "Bo", class: "Novice", lv: 5, mapN: "prontera"}
	for i := 0; i < 400; i++ {
		checkConvo(t, convoTownLines(a, b))
		checkConvo(t, convoTownLines(b, a))
		checkConvo(t, convoTownLines(plain, plain))
		checkConvo(t, convoPartyLines(a, b))
		checkConvo(t, convoPartyLines(plain, plain))
	}
	for _, c := range []string{"Priest", "Lord Knight", "Sniper", "Stalker", "Whitesmith", "Champion", "Gypsy", "Professor", "Novice"} {
		if s := classTalk(c); s == "" || len(s) > 60 {
			t.Fatalf("classTalk(%s) = %q", c, s)
		}
	}
}

func TestConvoPairNearAndFree(t *testing.T) {
	far := []convoSide{{mapN: "prontera", x: 10, y: 10}, {mapN: "prontera", x: 40, y: 40}}
	if i, _ := convoPair(far, convoNearCells); i >= 0 {
		t.Fatal("players 30 cells apart do not talk")
	}
	busy := []convoSide{{mapN: "prontera", x: 10, y: 10, busy: true}, {mapN: "prontera", x: 12, y: 11}}
	if i, _ := convoPair(busy, convoNearCells); i >= 0 {
		t.Fatal("a fighting player does not start chatting")
	}
	other := []convoSide{{mapN: "prontera", x: 10, y: 10}, {mapN: "geffen", x: 10, y: 10}}
	if i, _ := convoPair(other, convoNearCells); i >= 0 {
		t.Fatal("different maps do not talk")
	}
	near := []convoSide{{mapN: "prontera", x: 100, y: 100}, {mapN: "prontera", x: 10, y: 10}, {mapN: "prontera", x: 15, y: 12}}
	for k := 0; k < 50; k++ {
		i, j := convoPair(near, convoNearCells)
		if i < 1 || j < 1 || i == j {
			t.Fatalf("expected the two near players, got %d,%d", i, j)
		}
	}
	if i, _ := convoPair(near[:1], convoNearCells); i >= 0 {
		t.Fatal("one player has nobody to talk to")
	}
}

func TestConvoRateLimits(t *testing.T) {
	resetConvoState()
	defer resetConvoState()
	now := time.Unix(1_800_000_000, 0)
	b1, b2, b3, b4, b5, b6 := &Bot{}, &Bot{}, &Bot{}, &Bot{}, &Bot{}, &Bot{}
	if !convoAllowed("map:prontera", b1, b2, now, 2) {
		t.Fatal("first conversation allowed")
	}
	if convoAllowed("map:prontera", b3, b4, now.Add(30*time.Second), 2) {
		t.Fatal("same place within its cooldown")
	}
	if convoAllowed("map:geffen", b1, b3, now.Add(40*time.Second), 2) {
		t.Fatal("same player within its cooldown")
	}
	if !convoAllowed("map:geffen", b3, b4, now.Add(50*time.Second), 2) {
		t.Fatal("another place, other players: allowed")
	}
	if convoAllowed("map:payon", b5, b6, now.Add(55*time.Second), 2) {
		t.Fatal("per-minute cap of 2 reached")
	}
	if !convoAllowed("map:payon", b5, b6, now.Add(61*time.Second), 2) {
		t.Fatal("the first one left the minute: allowed again")
	}
	if !convoAllowed("map:prontera", b1, b2, now.Add(convoBotCooldown+time.Second), 2) {
		t.Fatal("cooldowns over: the same two may talk again")
	}
}

func TestAmbientTalkDefaultsOn(t *testing.T) {
	if !defaultTuning.AmbientTalkOn || defaultTuning.AmbientTalkDiv <= 0 || defaultTuning.AmbientTalkMaxPerMin <= 0 {
		t.Fatalf("ambient talk defaults: %+v", defaultTuning)
	}
	tn := Tuning{AmbientTalkOn: true}
	tn.sanitize()
	if tn.AmbientTalkDiv != defaultTuning.AmbientTalkDiv || tn.AmbientTalkMaxPerMin != defaultTuning.AmbientTalkMaxPerMin {
		t.Fatal("sanitize fills the ambient talk rates")
	}
	ambientTalk(nil, nil) // no players: nothing, no panic
}
