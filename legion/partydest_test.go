package main

import (
	"testing"
	"time"
)

// PT30 (2026-11-14): Ayla crossed izlude on her way to yuno_fild04 / payon -> geffen for gl_church; her friends were sent
// to whatever town she stood in (izlude, payon, prontera) instead of where she said to be.
func TestHumanDest(t *testing.T) {
	cases := []struct {
		name    string
		st      humanStatus
		herMap  string
		townRun bool
		want    string
	}{
		{"crossing a town to her hunting map", humanStatus{LockMap: "gl_church"}, "izlude", false, "gl_church"},
		{"crossing a field", humanStatus{LockMap: "gl_church"}, "gef_fild00", false, "gl_church"},
		{"there already", humanStatus{LockMap: "gl_church"}, "gl_church", false, ""},
		{"on a town run she called", humanStatus{LockMap: "gl_church"}, "payon", true, ""},
		{"town run flag from her plugin", humanStatus{LockMap: "gl_church", TownRun: true}, "geffen", false, ""},
		{"announced dest beats lock map", humanStatus{LockMap: "gl_church", Dest: "geffen"}, "prontera", false, "geffen"},
		{"in a building of the dest town", humanStatus{Dest: "payon"}, "payon_in01", false, ""},
		{"old status file, town lock map", humanStatus{LockMap: "payon"}, "prontera", false, ""},
		{"nothing known", humanStatus{}, "prontera", false, ""},
	}
	for _, c := range cases {
		if got := humanDest(c.st, c.herMap, c.townRun); got != c.want {
			t.Errorf("%s: humanDest = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestHumanMemberTarget(t *testing.T) {
	// she stands in izlude on the way to gl_church; we are in payon
	if got := humanMemberTarget("gl_church", "izlude", "payon", "geffen", true, true); got != "geffen" {
		t.Fatalf("dungeon destination: meet in its Kafra town, got %q", got)
	}
	if got := humanMemberTarget("gef_fild07", "izlude", "payon", "geffen", true, false); got != "geffen" {
		t.Fatalf("strong party, she is in a town: the meeting town (no own long walk), got %q", got)
	}
	// QA 2026-11-15 03:55Z: she left the meeting town geffen for gef_fild10 on her way to orcsdun02 - follow her there
	if got := humanMemberTarget("orcsdun02", "gef_fild10", "geffen", "geffen", true, true); got != "gef_fild10" {
		t.Fatalf("she left the meeting town: her current map, got %q", got)
	}
	if got := humanMemberTarget("gef_fild07", "gef_fild00", "payon", "geffen", false, false); got != "gef_fild00" {
		t.Fatalf("she is on open ground: her current map, got %q", got)
	}
	if got := humanMemberTarget("gef_fild07", "izlude", "payon", "geffen", false, false); got != "geffen" {
		t.Fatalf("weaker party: the meeting town, got %q", got)
	}
	if got := humanMemberTarget("geffen", "prontera", "payon", "geffen", true, false); got != "geffen" {
		t.Fatalf("a town destination is the meeting point, got %q", got)
	}
	if got := humanMemberTarget("gl_church", "izlude", "izlude", "geffen", true, true); got != "izlude" {
		t.Fatalf("on her map: stay with her, got %q", got)
	}
	// never the town she is crossing
	for _, strong := range []bool{true, false} {
		if got := humanMemberTarget("yuno_fild04", "izlude", "aldebaran", "yuno", strong, false); got == "izlude" {
			t.Fatalf("trailing her into izlude (strong=%v)", strong)
		}
	}
}

func TestDestSayDue(t *testing.T) {
	p := &Party{Name: "t-pt"}
	now := time.Now()
	if !destSayDue(p, "gef_fild07", now) {
		t.Fatal("first announcement must be due")
	}
	if destSayDue(p, "gef_fild07", now.Add(time.Minute)) {
		t.Fatal("same map within 10 min: no repeat")
	}
	if !destSayDue(p, "gl_church", now.Add(2*time.Minute)) {
		t.Fatal("a new map is announced")
	}
	if !destSayDue(p, "gl_church", now.Add(13*time.Minute)) {
		t.Fatal("after 10 min it may be said again")
	}
}

// QA 02:46-02:52Z: Zen.fox / Echobunny walked prontera -> geffen after 3 hops in 10 min.
func TestKafraChainHop(t *testing.T) {
	l := kafraHop{At: time.Now(), From: "payon", To: "prontera", Goal: "gl_church"}
	if !kafraChainHop(l, "geffen", "gl_church", 3) {
		t.Fatal("the next hop of the same trip must pass the 3-in-10-min cap")
	}
	if kafraChainHop(l, "payon", "gl_church", 3) {
		t.Fatal("straight back is never a chain")
	}
	if kafraChainHop(l, "geffen", "yuno_fild04", 3) {
		t.Fatal("a different goal is not the same trip")
	}
	if kafraChainHop(l, "geffen", "gl_church", kafraChainMax) {
		t.Fatal("the chain has its own cap")
	}
	if kafraChainHop(kafraHop{From: "payon"}, "geffen", "gl_church", 3) {
		t.Fatal("a hop without a recorded goal (restored from the diary) is no chain")
	}
}

func TestWarpListReplySave(t *testing.T) {
	pk := make([]byte, 6+32)
	pk[0], pk[1] = 0xbe, 0x0a
	pk[2] = byte(len(pk))
	pk[4] = alTeleport
	copy(pk[6:], "Random")
	copy(pk[22:], "payon.gat")
	r := warpListReplySave(pk, true)
	if r == nil || string(r[4:13]) != "payon.gat" {
		t.Fatalf("save pick: got %q", r)
	}
	if r := warpListReplySave(pk, false); r == nil || string(r[4:10]) != "Random" {
		t.Fatalf("default must stay Random: got %q", r)
	}
	short := pk[:22]
	if r := warpListReplySave(short, true); r == nil || string(r[4:10]) != "Random" {
		t.Fatalf("lv1 list (Random only): got %q", r)
	}
}
