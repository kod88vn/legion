package main

import (
	"os"
	"testing"
	"time"
)

// TestKafraWalkLive (offline, skipped unless LEGION_RATHENA is set): walking-cell estimates on the real maps, and
// whether the time rule now takes the Kafra for the QA cases of 2026-11-14 13:41Z.
//
//	LEGION_RATHENA=/rdata go test -run TestKafraWalkLive -v
func TestKafraWalkLive(t *testing.T) {
	rd := os.Getenv("LEGION_RATHENA")
	if rd == "" {
		t.Skip("offline measurement: set LEGION_RATHENA")
	}
	*rathena = rd
	loadGameData()
	for _, c := range []struct {
		from     string
		x, y     int
		goal     string
		wantKafr bool
	}{
		{"geffen", 120, 62, "mjolnir_02", true},
		{"morocc", 156, 97, "prt_fild08", true},
		{"prontera", 152, 29, "prt_fild08", false},
		{"payon", 181, 104, "pay_fild01", false},
	} {
		b := &Bot{P: &Persona{BaseLevel: 60, Chars: []CharSpec{{Name: "t"}}}, BaseLv: 60, MapName: c.from, X: c.x, Y: c.y, Zeny: 50000}
		w := walkCells(c.from, c.x, c.y, c.goal)
		k := b.kafraPlan(c.goal, timeNowForTest())
		dest := ""
		if k != nil {
			dest = k.Dest.Name
		}
		t.Logf("%s %d,%d -> %s: walk %d cells (~%.0f s), hops %d, kafra %q", c.from, c.x, c.y, c.goal, w,
			float64(w)*walkCellSec, mapHops(c.from, c.x, c.y, c.goal), dest)
		if (k != nil) != c.wantKafr {
			t.Errorf("%s -> %s: kafra %v, want %v", c.from, c.goal, k != nil, c.wantKafr)
		}
	}
	// from the field: moc_fild03 -> prt_fild08 across the desert
	b := &Bot{P: &Persona{BaseLevel: 60, Chars: []CharSpec{{Name: "t"}}}, BaseLv: 60, MapName: "moc_fild03", X: 150, Y: 150, Zeny: 50000}
	tw, wing := b.kafraVia("prt_fild08", timeNowForTest())
	t.Logf("moc_fild03 -> prt_fild08: walk %d cells, hops %d, via %q wing %v", walkCells("moc_fild03", 150, 150, "prt_fild08"),
		mapHops("moc_fild03", 150, 150, "prt_fild08"), tw, wing)
	// Renewal Morocc is sealed on foot (Satan Morocc): its exits lead to moc_fild07/12/19/20 and moc_ruins, which reach
	// only each other and Comodo; moc_fild01/13/07/11 and prt_fild09/10 reach moc_fild20 only through the "Continental
	// Guard" dialog NPC (npc/re/quests/quests_morocc.txt), and moc_fild20 back only through a guard to morocc 160,61.
	// So no walking route morocc <-> moc_fild01-03 / Prontera is correct; the Kafra is the way in and out.
	if r := RouteMaps("morocc", 156, 93, "prt_fild08"); r != nil {
		t.Logf("morocc -> prt_fild08 walks now (%d maps): the Morocc seal changed", len(r))
	}
	if k := (&Bot{P: &Persona{BaseLevel: 60, Chars: []CharSpec{{Name: "t"}}}, BaseLv: 60, MapName: "morocc", X: 156, Y: 93, Zeny: 5000}).kafraPlan("prt_fild08", timeNowForTest()); k == nil {
		t.Error("a bot in Morocc must take the Kafra out (no walking route)")
	}
	for _, m := range []string{"moc_fild07", "moc_fild12", "moc_fild19", "moc_ruins"} {
		if nt := nearestTown(m); nt != "morocc" {
			t.Errorf("nearestTown(%s) = %s, want morocc", m, nt)
		}
	}
	t.Logf("nearest town %s, walk there %d, tele cells from it %d, spawn %v", nearestTown("moc_fild03"),
		walkCells("moc_fild03", 150, 150, nearestTown("moc_fild03")), b.kafraTeleCells(nearestTown("moc_fild03"), "prt_fild08"),
		townSpawn[nearestTown("moc_fild03")])
}

func timeNowForTest() time.Time { return time.Now() }

// QA 2026-11-14: lilzensenpaiVend (Alchemist lv61, starter gear) walked prontera -> mjolnir_08 (lv59 Flora) -> Al De
// Baran and died; the chained Kafra prontera -> Geffen -> Al De Baran must win, also from the field next to Prontera.
func TestKafraChainLive(t *testing.T) {
	if os.Getenv("LEGION_RATHENA") == "" {
		t.Skip("offline measurement: set LEGION_RATHENA")
	}
	*rathena = os.Getenv("LEGION_RATHENA")
	loadGameData()
	for _, g := range []string{"alde_alche", "aldebaran", "yuno"} {
		b := &Bot{P: &Persona{BaseLevel: 61, Chars: []CharSpec{{Name: "t"}}}, BaseLv: 61, MapName: "prontera", X: 151, Y: 45, Zeny: 100000}
		k := b.kafraPlan(g, time.Now())
		if k == nil {
			t.Errorf("prontera -> %s: walks (unsafe mjolnir route), want the Kafra", g)
			continue
		}
		t.Logf("prontera -> %s: Kafra to %s (walk safe %v)", g, k.Dest.Name, b.kafraWalkSafe(g, time.Now()))
	}
	b := &Bot{P: &Persona{BaseLevel: 61, Chars: []CharSpec{{Name: "t"}}}, BaseLv: 61, MapName: "geffen", X: 120, Y: 39, Zeny: 100000}
	if k := b.kafraPlan("alde_alche", time.Now()); k == nil || k.Dest.Map != "aldebaran" {
		t.Errorf("geffen -> alde_alche: want the Kafra to Al De Baran, got %+v", k)
	}
	f := &Bot{P: &Persona{BaseLevel: 61, Chars: []CharSpec{{Name: "t"}}}, BaseLv: 61, MapName: "prt_fild05", X: 200, Y: 200, Zeny: 100000}
	tw, _ := f.kafraVia("alde_alche", time.Now())
	t.Logf("prt_fild05 -> alde_alche: via %q (walk safe %v)", tw, f.kafraWalkSafe("alde_alche", time.Now()))
	// a strong bot may still walk a safe short way: payon -> pay_fild01
	s := &Bot{P: &Persona{BaseLevel: 61, Chars: []CharSpec{{Name: "t"}}}, BaseLv: 61, MapName: "payon", X: 181, Y: 104, Zeny: 100000}
	if k := s.kafraPlan("pay_fild01", time.Now()); k != nil {
		t.Errorf("payon -> pay_fild01: teleports to %s, want a walk", k.Dest.Name)
	}
}

// QA 2026-11-14 16:44Z: TheShadowsenpai, Kafra prontera -> Geffen, walked out to gef_fild00 and then flew back to
// Prontera by Butterfly Wing "for the trip to alde_alche" (prontera -> Geffen -> Prontera). The way on is the Geffen
// Kafra; and the way back from Al De Baran takes its Kafra (the mjolnir walk is deadly).
func TestKafraNoWingBackLive(t *testing.T) {
	if os.Getenv("LEGION_RATHENA") == "" {
		t.Skip("offline measurement: set LEGION_RATHENA")
	}
	*rathena = os.Getenv("LEGION_RATHENA")
	loadGameData()
	now := time.Now()
	for _, x := range []int{50, 200, 370} {
		b := &Bot{P: &Persona{BaseLevel: 69, Chars: []CharSpec{{Name: "t"}}}, BaseLv: 69, MapName: "gef_fild00", X: x, Y: 200, Zeny: 100000,
			savedTown: "prontera", saveKnown: true, inv: map[int]*InvItem{5: {Index: 5, ItemID: itemButterflyWing, Count: 4}}}
		b.kafraLast = kafraHop{At: now.Add(-3 * time.Minute), From: "prontera", To: "geffen"}
		b.kafraHops = []time.Time{b.kafraLast.At}
		tw, wing := b.kafraVia("alde_alche", now)
		t.Logf("gef_fild00 %d,200 -> alde_alche: via %q wing %v", x, tw, wing)
		if wing && tw == "prontera" {
			t.Errorf("gef_fild00 %d,200: flies back to Prontera right after the Kafra from there", x)
		}
	}
	r := &Bot{P: &Persona{BaseLevel: 61, Chars: []CharSpec{{Name: "t"}}}, BaseLv: 61, MapName: "aldebaran", X: 140, Y: 131, Zeny: 3293}
	if k := r.kafraPlan("prontera", now); k == nil {
		t.Error("aldebaran -> prontera with 3,293z: walks through mjolnir, want the Kafra")
	} else {
		t.Logf("aldebaran -> prontera: Kafra to %s", k.Dest.Name)
	}
}

// QA 2026-11-14 17:13-17:27Z: Ryustorm (Hunter lv67) lapped payon <-> Morocc by Kafra every 30-60 s for pay_dun03:
// payon -> Morocc -> Payon "saved" the walk inside Payon (the Payon Kafra lands at 161,58), and the unsafe walk
// through pay_dun01/02 lifted the A -> B -> A block. Payon dungeon goals walk from Payon; Morocc goes back to Payon once.
func TestKafraPayonDungeonLive(t *testing.T) {
	if os.Getenv("LEGION_RATHENA") == "" {
		t.Skip("offline measurement: set LEGION_RATHENA")
	}
	*rathena = os.Getenv("LEGION_RATHENA")
	loadGameData()
	now := time.Now()
	for _, lv := range []int{30, 50, 67, 80} {
		for _, g := range []string{"pay_dun00", "pay_dun01", "pay_dun03", "pay_dun04"} {
			for _, p := range [][2]int{{162, 233}, {181, 104}, {175, 226}, {161, 58}} {
				b := &Bot{P: &Persona{BaseLevel: lv, Chars: []CharSpec{{Name: "t"}}}, BaseLv: lv, MapName: "payon", X: p[0], Y: p[1], Zeny: 50000}
				if k := b.kafraPlan(g, now); k != nil {
					t.Errorf("lv%d payon %v -> %s: Kafra to %s, want a walk", lv, p, g, k.Dest.Name)
				}
			}
		}
		m := &Bot{P: &Persona{BaseLevel: lv, Chars: []CharSpec{{Name: "t"}}}, BaseLv: lv, MapName: "morocc", X: 156, Y: 46, Zeny: 50000}
		if k := m.kafraPlan("pay_dun03", now); k == nil || k.Dest.Map != "payon" {
			t.Errorf("lv%d morocc -> pay_dun03: want the Kafra to Payon, got %+v", lv, k)
		}
	}
	// the loop guard: just came morocc -> Payon, the walk on reads unsafe: no teleport back to Morocc
	b := &Bot{P: &Persona{BaseLevel: 30, Chars: []CharSpec{{Name: "t"}}}, BaseLv: 30, MapName: "payon", X: 181, Y: 104, Zeny: 50000}
	b.kafraLast = kafraHop{At: now.Add(-time.Minute), From: "morocc", To: "payon"}
	b.kafraHops = []time.Time{now.Add(-3 * time.Minute), now.Add(-2 * time.Minute), b.kafraLast.At}
	if b.kafraWalkSafe("pay_dun03", now) {
		t.Log("(walk reads safe here; the guard is still checked)")
	}
	if !b.kafraHoldQ("morocc", "pay_dun03", now, true) {
		t.Error("payon -> Morocc right after morocc -> Payon must be held even when the walk on is unsafe")
	}
	if k := b.kafraPlan("pay_dun03", now); k != nil && k.Dest.Map == "morocc" {
		t.Error("kafraPlan sends it back to Morocc")
	}
}

// QA 2026-11-14 18:30Z: 16 "no route from morocc 156,46 to X: Butterfly Wing home" in 15 min. A bot landing in sealed
// Morocc (Kafra landing 156,46) must take the Morocc Kafra on at once, and no plan may land it in Morocc when the hop
// on from there will be refused (a chain around the A -> B -> A block).
func TestKafraMoroccLandingLive(t *testing.T) {
	if os.Getenv("LEGION_RATHENA") == "" {
		t.Skip("offline measurement: set LEGION_RATHENA")
	}
	*rathena = os.Getenv("LEGION_RATHENA")
	loadGameData()
	now := time.Now()
	for _, g := range []string{"pay_fild03", "izlude", "gef_fild11", "gef_fild03", "moc_fild01"} {
		b := &Bot{P: &Persona{BaseLevel: 60, Chars: []CharSpec{{Name: "t"}}}, BaseLv: 60, MapName: "morocc", X: 156, Y: 46, Zeny: 20000}
		// it just came here by Kafra from Payon, the 4th teleport this hour
		b.kafraLast = kafraHop{At: now, From: "payon", To: "morocc"}
		b.kafraHops = []time.Time{now.Add(-30 * time.Minute), now.Add(-20 * time.Minute), now.Add(-10 * time.Minute), now}
		k := b.kafraPlan(g, now)
		if k == nil {
			t.Errorf("morocc 156,46 -> %s: no Kafra (no walking route): stranded", g)
			continue
		}
		t.Logf("morocc 156,46 -> %s: Kafra to %s", g, k.Dest.Name)
	}
	// alberta -> Payon a minute ago; from Payon the way to pay_fild03 must not be payon -> Morocc -> Alberta
	b := &Bot{P: &Persona{BaseLevel: 30, Chars: []CharSpec{{Name: "t"}}}, BaseLv: 30, MapName: "payon", X: 161, Y: 58, Zeny: 20000}
	b.kafraLast = kafraHop{At: now.Add(-time.Minute), From: "alberta", To: "payon"}
	b.kafraHops = []time.Time{b.kafraLast.At}
	if k := b.kafraPlan("pay_fild03", now); k != nil && k.Dest.Map == "morocc" {
		t.Error("payon -> Morocc chained around the A -> B -> A block")
	}
	if k := b.kafraPlan("alberta", now); k != nil && k.Dest.Map == "morocc" {
		t.Error("payon -> Morocc for alberta right after alberta -> Payon")
	}
}

// QA 2026-11-14 19:00Z: bots bound for Yuno lapped prontera <-> Morocc by Kafra (12 teleports in 2.5 min) until broke.
// The way is Geffen -> Al De Baran -> (Juno); Morocc is never on it; and 3 teleports in 10 min stop everything.
func TestKafraYunoLive(t *testing.T) {
	if os.Getenv("LEGION_RATHENA") == "" {
		t.Skip("offline measurement: set LEGION_RATHENA")
	}
	*rathena = os.Getenv("LEGION_RATHENA")
	loadGameData()
	now := time.Now()
	for _, st := range []struct {
		m    string
		x, y int
	}{{"prontera", 116, 72}, {"prontera", 152, 29}, {"payon", 161, 58}, {"payon", 181, 104}, {"morocc", 156, 46}, {"geffen", 120, 39}, {"aldebaran", 168, 112}} {
		b := &Bot{P: &Persona{BaseLevel: 60, Chars: []CharSpec{{Name: "t"}}}, BaseLv: 60, MapName: st.m, X: st.x, Y: st.y, Zeny: 20000}
		k := b.kafraPlan("yuno", now)
		if k == nil {
			t.Errorf("%s %d,%d -> yuno: no plan", st.m, st.x, st.y)
			continue
		}
		t.Logf("%s %d,%d -> yuno: Kafra to %s", st.m, st.x, st.y, k.Dest.Name)
		if k.Dest.Map == "morocc" {
			t.Errorf("%s -> yuno goes through Morocc (no way on from there)", st.m)
		}
	}
	// hard cap: 3 teleports in 10 min stop even a town run, and X -> Y -> X within 10 min is refused
	b := &Bot{P: &Persona{BaseLevel: 60, Chars: []CharSpec{{Name: "t"}}}, BaseLv: 60, MapName: "prontera", X: 116, Y: 72, Zeny: 20000, trip: &Trip{}}
	b.kafraLast = kafraHop{At: now.Add(-time.Minute), From: "morocc", To: "prontera"}
	b.kafraHops = []time.Time{now.Add(-5 * time.Minute), now.Add(-3 * time.Minute), b.kafraLast.At}
	if k := b.kafraPlan("yuno", now); k != nil {
		t.Errorf("4th teleport in 10 min on a town run: Kafra to %s", k.Dest.Name)
	}
	b.kafraHops = b.kafraHops[2:]
	if !b.kafraHoldQ("morocc", "yuno", now, true) {
		t.Error("prontera -> Morocc 1 min after morocc -> Prontera on a town run must be refused")
	}
}

// the wait itself: in Al De Baran, Kafra held, the walk to Prontera crosses mjolnir: wait, don't walk
func TestKafraWaitInTownLive(t *testing.T) {
	if os.Getenv("LEGION_RATHENA") == "" {
		t.Skip("offline measurement: set LEGION_RATHENA")
	}
	*rathena = os.Getenv("LEGION_RATHENA")
	loadGameData()
	now := time.Now()
	b := &Bot{P: &Persona{BaseLevel: 69, Chars: []CharSpec{{Name: "t"}}}, BaseLv: 69, MapName: "aldebaran", X: 140, Y: 131, Zeny: 90000}
	b.kafraHardUntil = now.Add(3 * time.Minute)
	if !b.kafraWaitInTown("prontera", now) {
		t.Error("aldebaran -> prontera with the Kafra held and a deadly walk: must wait in town")
	}
	b.kafraHardUntil = now.Add(-time.Second)
	if b.kafraWaitInTown("prontera", now) {
		t.Error("limit cleared: no more waiting")
	}
	p := &Bot{P: &Persona{BaseLevel: 69, Chars: []CharSpec{{Name: "t"}}}, BaseLv: 69, MapName: "payon", X: 162, Y: 233, Zeny: 90000, kafraHardUntil: now.Add(3 * time.Minute)}
	if p.kafraWaitInTown("pay_fild01", now) {
		t.Error("a safe walk (payon -> pay_fild01) never waits")
	}
}
