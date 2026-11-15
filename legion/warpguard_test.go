package main

import (
	"os"
	"path/filepath"
	"testing"
)

// loadRealMaps loads the real map cache and warps (LEGION_TEST_RATHENA, default /rathena); skips when absent.
func loadRealMaps(t *testing.T) {
	root := os.Getenv("LEGION_TEST_RATHENA")
	if root == "" {
		root = "/rathena"
	}
	if _, err := os.Stat(filepath.Join(root, "db", "re", "map_cache.dat")); err != nil {
		t.Skip("no rAthena map cache at " + root)
	}
	if maps["prontera"] != nil && len(warpsFrom["prontera"]) > 0 {
		return
	}
	for _, f := range []string{filepath.Join(root, "db", "map_cache.dat"), filepath.Join(root, "db", "re", "map_cache.dat")} {
		if err := loadMapCache(f); err != nil {
			t.Fatal(err)
		}
	}
	loadNPCs(filepath.Join(root, "npc", "re", "warps"), filepath.Join(root, "npc", "warps"))
}

// 2026-11-14: bots arriving from prt_in at prontera 47,226 (beside the west door 50,228, area x 48-52 y 226-230)
// heading for the gear-shop door 177,221 had every step refused ("warp area in the way") for ~11 min: the box test
// counted the door's corner cell 48,226 for the diagonal step 47,226 -> 48,225.
func TestWarpGuardStepBesideDoor(t *testing.T) {
	loadRealMaps(t)
	gm := maps["prontera"]
	var door Warp
	for _, w := range warpsFrom["prontera"] {
		if w.X == 177 && w.Y == 221 {
			door = w
		}
	}
	if door.To == "" {
		t.Fatal("no prontera 177,221 warp")
	}
	b := &Bot{P: &Persona{Name: "wgtest", Account: "wgtest", Chars: []CharSpec{{Name: "wgtest"}}}, MapName: "prontera",
		X: 47, Y: 226, DestX: 47, DestY: 226, lastWarp: door}
	for _, s := range [][2]int{{48, 225}, {49, 224}} {
		x, y, ok := b.safeWalkDest(s[0], s[1])
		if !ok || (x == 47 && y == 226) {
			t.Fatalf("step to %d,%d refused at 47,226 (got %d,%d ok=%v)", s[0], s[1], x, y, ok)
		}
		if gm.OnWarp(x, y) {
			t.Fatalf("step to %d,%d lands on a trigger cell %d,%d", s[0], s[1], x, y)
		}
	}
	// the route's first segment from 47,226 toward the door is not trimmed to nothing
	p := gm.FindPath(47, 226, 177, 221, 80000)
	if len(p) == 0 {
		t.Fatal("no path 47,226 -> 177,221")
	}
	if gm.segHitsWarp(47, 226, p[0][0], p[0][1], 177, 221) {
		t.Fatalf("first path step %v counted as entering a foreign warp", p[0])
	}
	x, y, ok := b.safeWalkDest(p[0][0], p[0][1])
	if !ok || gm.OnWarp(x, y) {
		t.Fatalf("first path step %v refused (%d,%d ok=%v)", p[0], x, y, ok)
	}
	// a walk straight through the west door's area is still refused / rerouted off it
	if !gm.segHitsWarp(47, 228, 53, 228, 177, 221) {
		t.Fatal("walk across the west door area not detected")
	}
	// detour: when every step is refused, a safe short step exists
	if dx, dy, ok := gm.detourStep(47, 226, 50, 228); !ok || gm.OnWarp(dx, dy) {
		t.Fatalf("no safe detour step from 47,226 (%d,%d ok=%v)", dx, dy, ok)
	}
}
