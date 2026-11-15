package main

import (
	"strings"
	"testing"
	"time"
)

// testMap: every cell blocked except the ones open() frees.
func testMap(name string, w, h int) *GameMap {
	m := &GameMap{Name: name, W: w, H: h, cells: make([]byte, w*h)}
	for i := range m.cells {
		m.cells[i] = 1
	}
	return m
}

func (m *GameMap) open(x0, y0, x1, y1 int) {
	for y := min(y0, y1); y <= max(y0, y1); y++ {
		for x := min(x0, x1); x <= max(x0, x1); x++ {
			m.cells[x+y*m.W] = 0
		}
	}
}

// A Mjolnir-style winding map: the only way from the start (2,2) to the portal (97,2) goes 95 cells down, 95
// across and back up, so the straight-line distance doesn't improve for ~190 cells (> 60 s of walking). Walking
// that path steadily must never count as stalled (QA 2026-11-14: 156 "still stuck ... Butterfly Wing home" on
// mjolnir_01/02 with bots 137-318 cells from the portal, on perfectly good paths).
func TestTravelStalledFollowsWindingPath(t *testing.T) {
	m := testMap("stalltest_wind", 100, 100)
	m.open(2, 2, 2, 97)
	m.open(2, 97, 97, 97)
	m.open(97, 97, 97, 2)
	maps[m.Name] = m
	defer delete(maps, m.Name)
	b := &Bot{P: &Persona{Name: "stalltest", Account: "stalltest", Chars: []CharSpec{{Name: "stalltest"}}}, MapName: m.Name, X: 2, Y: 2}
	defer travelProgs.Delete(b)
	tx, ty := 97, 2
	b.path = m.FindPath(b.X, b.Y, tx, ty, 80000)
	if len(b.path) < 280 {
		t.Fatalf("test map path = %d cells", len(b.path))
	}
	now := time.Now()
	best, noGain, maxNoGain := dist(b.X, b.Y, tx, ty), 0, 0
	for len(b.path) > 0 {
		if b.travelStalled(tx, ty, now) {
			t.Fatalf("declared stalled at %d,%d with %d path cells left (walking steadily)", b.X, b.Y, len(b.path))
		}
		step := min(2, len(b.path)) // 2 cells/s: slow walking, fights on the way
		b.X, b.Y = b.path[step-1][0], b.path[step-1][1]
		b.path = b.path[step:]
		now = now.Add(time.Second)
		if d := dist(b.X, b.Y, tx, ty); d < best-2 {
			best, noGain = d, 0
		} else if noGain++; noGain > maxNoGain {
			maxNoGain = noGain
		}
	}
	if maxNoGain < 61 {
		t.Fatalf("test map too easy: straight-line distance stalled only %d s", maxNoGain)
	}
}

// A bot that really makes no progress: 60 s re-plan, 120 s avoid that portal (this bot only) and route via another
// exit, and only then the Butterfly Wing. Regions: the route graph never plans a portal in another walk region.
func TestTravelStalledTriesAnotherExitBeforeWing(t *testing.T) {
	a := testMap("stalltest_a", 60, 60)
	a.open(5, 5, 50, 20)  // region 1: start + exits E1 (50,10) and E2 (5,20)
	a.open(5, 40, 50, 50) // region 2 (a ledge): exit E3 (50,45), unreachable from region 1
	c := testMap("stalltest_c", 20, 20)
	c.open(1, 1, 18, 18)
	for _, m := range []*GameMap{a, c} {
		maps[m.Name] = m
	}
	e1 := Warp{From: a.Name, X: 50, Y: 10, To: c.Name, ToX: 10, ToY: 10}
	e2 := Warp{From: a.Name, X: 5, Y: 20, To: c.Name, ToX: 3, ToY: 3}
	e3 := Warp{From: a.Name, X: 50, Y: 45, To: c.Name, ToX: 15, ToY: 15}
	warpsFrom[a.Name] = []Warp{e3, e1, e2}
	defer func() {
		delete(maps, a.Name)
		delete(maps, c.Name)
		delete(warpsFrom, a.Name)
	}()
	// region check: from region 1, E3 (on the ledge) is never planned even though it is listed first
	if r := RouteMaps(a.Name, 45, 10, c.Name, 50); len(r) != 1 || r[0] != e1 {
		t.Fatalf("route from region 1 = %v, want E1", r)
	}
	if r := RouteMaps(a.Name, 45, 45, c.Name, 50); len(r) != 1 || r[0] != e3 {
		t.Fatalf("route from the ledge = %v, want E3", r)
	}

	b := &Bot{P: &Persona{Name: "stalltest2", Account: "stalltest2", Chars: []CharSpec{{Name: "stalltest2"}}}, MapName: a.Name, X: 45, Y: 10}
	defer travelProgs.Delete(b)
	defer travelAvoid.Delete(b)
	b.route = b.routeFor(c.Name)
	if len(b.route) != 1 || b.route[0] != e1 {
		t.Fatalf("routeFor = %v, want E1", b.route)
	}
	b.lastWarp = e1
	tx, ty := portalCell(a, b.X, b.Y, e1)
	b.path = a.FindPath(b.X, b.Y, tx, ty, 80000)
	now := time.Now()
	stalls := 0
	for s := 0; s <= 125; s++ { // standing still (blocked by something the map doesn't show)
		if b.travelStalled(tx, ty, now) {
			stalls++
			if b.path == nil { // travel re-plans the walk
				b.path = a.FindPath(b.X, b.Y, tx, ty, 80000)
			}
		}
		now = now.Add(time.Second)
	}
	if stalls != 2 {
		t.Fatalf("stalls after 125 s = %d, want 2 (re-plan, then another way)", stalls)
	}
	if !b.avoidsWarp(e1) {
		t.Fatal("the stuck portal must be avoided by this bot")
	}
	if warpBad(e1) {
		t.Fatal("one bot's trouble must not mark the portal bad for everyone at this stage")
	}
	if r := b.routeFor(c.Name); len(r) != 1 || r[0] != e2 {
		t.Fatalf("re-planned route = %v, want the other exit E2", r)
	}
	if r := RouteMaps(a.Name, 45, 10, c.Name, 50); len(r) != 1 || r[0] != e1 {
		t.Fatalf("other bots' route = %v, want E1 still", r)
	}
	// the only way on is still used when every exit is avoided
	warpsFrom[a.Name] = []Warp{e3, e1}
	if r := b.routeFor(c.Name); len(r) != 1 || r[0] != e1 {
		t.Fatalf("only-exit route = %v, want E1 (avoided, but the only way)", r)
	}
	if !strings.HasPrefix(warpKey(e1.From, e1.X, e1.Y), a.Name+":") || b.avoidedOn(a.Name, now) != 1 {
		t.Fatalf("avoidedOn = %d", b.avoidedOn(a.Name, now))
	}
}
