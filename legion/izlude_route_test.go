package main

import "testing"

// Izlude (Renewal, 268x300) route check, offline. The OpenKore error "[getRoute] Called with no valid closest
// coordinates on field izlude (24, 98)" (bug b1794721137499, 2026-11-13..15) names a start cell that the server map
// cache and OpenKore's own field agree is walkable. This test feeds the engine the same window (x 12..63, y 88..107)
// of openkore/fields-re/izlude.fld2.gz, painted into a GameMap, and routes from that cell to the izlude exit at 20,98
// (warp to prt_fild08, the trigger span assumed 2). No live data is read: the window is pasted below from the repo
// field file (1 = walkable, # = not).

const izludeWindowX0, izludeWindowY0 = 12, 88

var izludeWindow = []string{
	"####################################################", // y88
	"################################............########", // y89
	"################################............########", // y90
	"################################....................", // y91
	"################################....................", // y92
	"################################....................", // y93
	"################################....................", // y94
	"################################....................", // y95
	"########............................................", // y96
	"########............................................", // y97
	"########............................................", // y98
	"########............................................", // y99
	"########............................................", // y100
	"################################....................", // y101
	"################################....................", // y102
	"################################....................", // y103
	"################################....................", // y104
	"################################............########", // y105
	"################################............########", // y106
	"####################################################", // y107
}

// izludeMap: a 268x300 izlude with the pasted window; everything else is blocked (the test never leaves the window).
func izludeMap() *GameMap {
	gm := &GameMap{Name: "izlude", W: 268, H: 300, cells: make([]byte, 268*300)}
	for i := range gm.cells {
		gm.cells[i] = 1
	}
	for r, row := range izludeWindow {
		for c := 0; c < len(row); c++ {
			if row[c] == '.' {
				gm.cells[(izludeWindowX0+c)+(izludeWindowY0+r)*gm.W] = 0
			}
		}
	}
	return gm
}

// withIzludeExit installs the exit warp (20,98 -> prt_fild08) for one test and restores the warp table after it.
func withIzludeExit(t *testing.T) {
	t.Helper()
	saved := warpsFrom["izlude"]
	warpsFrom["izlude"] = []Warp{{From: "izlude", X: 20, Y: 98, To: "prt_fild08", ToX: 367, ToY: 212, XS: 2, YS: 2}}
	t.Cleanup(func() { warpsFrom["izlude"] = saved })
}

func TestIzludeStartCellIsWalkable(t *testing.T) {
	gm := izludeMap()
	if !gm.Walkable(24, 98) {
		t.Fatalf("izlude (24,98) must be walkable: the server cache and the field file both say so")
	}
}

func TestIzludeRouteToExitFromStart(t *testing.T) {
	withIzludeExit(t)
	gm := izludeMap()
	p := gm.FindPath(24, 98, 20, 98, 20000)
	if p == nil {
		t.Fatalf("no path from izlude (24,98) to the exit at 20,98")
	}
	if last := p[len(p)-1]; last != [2]int{20, 98} {
		t.Fatalf("path ends at %v, want 20,98", last)
	}
	for _, c := range p {
		if !gm.Walkable(c[0], c[1]) {
			t.Fatalf("path steps on unwalkable cell %v", c)
		}
	}
}

func TestIzludeCorridorEastToExit(t *testing.T) {
	// the corridor (x 20..63, y 96..100) is the only walking way out of the west end; a bot released from the east
	// side of it (x 44) must reach the exit too
	withIzludeExit(t)
	gm := izludeMap()
	if p := gm.FindPath(44, 98, 20, 98, 20000); p == nil {
		t.Fatalf("no path from izlude (44,98) to the exit at 20,98")
	}
}

func TestIzludeStartIsNotInsideExitWarp(t *testing.T) {
	// a start inside the warp's trigger area would be the OpenKore warp-cell case (payon_in01 26,115 lesson); 24,98 is
	// clear of the 20,98 trigger (span 2 -> x 18..22) and of its ring (x 23)
	withIzludeExit(t)
	gm := izludeMap()
	if m := gm.warpMask(); m[24+98*gm.W] != 0 {
		t.Fatalf("izlude (24,98) lies in a warp area (mask %d): the start would be treated as on-portal", m[24+98*gm.W])
	}
}
