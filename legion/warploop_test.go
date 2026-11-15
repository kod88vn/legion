package main

import (
	"testing"
	"time"
)

// QA 2026-11-14 12:25: "warp loop X <-> X_in" flagged two different shop doors of one interior map and banned the door.
func TestWarpLoopLegsSameDoorOnly(t *testing.T) {
	now := time.Now()
	h := func(v1, v3 string) []mapVisit {
		return []mapVisit{{m: "alberta", at: now}, {m: "alberta_in", at: now, via: v1}, {m: "alberta", at: now}, {m: "alberta_in", at: now, via: v3}}
	}
	if warpLoopLegs(h("alberta:45:145", "alberta:98:153")) {
		t.Error("two different doors into alberta_in counted as a loop")
	}
	if !warpLoopLegs(h("alberta:98:153", "alberta:98:153")) {
		t.Error("the same door twice is a loop")
	}
	if !warpLoopLegs(h("", "alberta:98:153")) {
		t.Error("unknown first door: keep the old A,B,A,B rule")
	}
	if warpLoopLegs([]mapVisit{{m: "a"}, {m: "b"}, {m: "c"}, {m: "b"}}) {
		t.Error("A,B,C,B is no loop")
	}
}

func TestWarpLoopNeverBansShopDoors(t *testing.T) {
	if warpLoopBannable(Warp{From: "prontera", X: 179, Y: 184, To: "prt_in"}) {
		t.Error("prontera's refiner door must never be banned")
	}
	if warpLoopBannable(Warp{From: "alberta_in", X: 189, Y: 89, To: "alberta"}) {
		t.Error("an exit door of a town building must never be banned")
	}
	if warpLoopBannable(Warp{From: "prt_fild06", X: 23, Y: 193, To: "prontera"}) || warpLoopBannable(Warp{From: "prontera", X: 22, Y: 203, To: "prt_fild05"}) ||
		warpLoopBannable(Warp{From: "geffen", X: 119, Y: 213, To: "gef_fild04"}) {
		t.Error("a town gate must never be banned by the loop detector")
	}
	if !warpLoopBannable(Warp{From: "prt_fild05", X: 1, Y: 1, To: "mjolnir_09"}) {
		t.Error("a field portal may be banned")
	}
}

// The in-town trip check: a route kept from a field (to the nearest door of alberta_in: Room_Entrance 45,145) "ends on
// alberta_in" but not in the Tool Dealer's room; the RouteMapsPoint route does.
func TestRouteEndsInShopRoom(t *testing.T) {
	loadRealMaps(t)
	near := RouteMaps("alberta", 47, 160, "alberta_in", 40)
	if len(near) == 0 {
		t.Fatal("no route alberta -> alberta_in")
	}
	tool := [2]int{182, 97} // Tool Dealer, behind alberta 98,153
	if near[len(near)-1].X != 98 && routeEndsIn(near, "alberta_in", tool[0], tool[1]) {
		t.Errorf("nearest-door route %v accepted for the Tool Dealer's room", near)
	}
	pt := RouteMapsPoint("alberta", 47, 160, "alberta_in", tool[0], tool[1], 40)
	if !routeEndsIn(pt, "alberta_in", tool[0], tool[1]) {
		t.Errorf("RouteMapsPoint route %v rejected", pt)
	}
	// prontera: the refiner's room is only behind 179,184
	ref := RouteMapsPoint("prontera", 47, 226, refinerMap, refinerNPC[0], refinerNPC[1], 40)
	if !routeEndsIn(ref, refinerMap, refinerNPC[0], refinerNPC[1]) || ref[len(ref)-1].X != 179 || ref[len(ref)-1].Y != 184 {
		t.Errorf("refiner route from the west door: %v", ref)
	}
	if west := RouteMaps("prt_fild05", 360, 205, refinerMap, 40); len(west) > 0 && routeEndsIn(west, refinerMap, refinerNPC[0], refinerNPC[1]) &&
		west[len(west)-1].X != 179 {
		t.Errorf("a route through another prt_in door accepted for the refiner: %v", west)
	}
}

// Leaving a shop: from the outdoor arrival cell of every town building's exit, the planned path (and the segment the
// server walks for the first step) never touches a trigger cell of another warp - in particular the door just used.
func TestShopExitFirstStepAwayFromDoor(t *testing.T) {
	loadRealMaps(t)
	for town, ins := range townInteriors {
		gm := maps[town]
		if gm == nil {
			continue
		}
		goals := [][2]int{}
		if sp, ok := townSpawn[town]; ok {
			goals = append(goals, sp)
		}
		for _, w := range warpsFrom[town] {
			goals = append(goals, [2]int{w.X, w.Y})
		}
		for _, in := range ins {
			for _, ex := range warpsFrom[in] {
				if ex.To != town || !gm.Walkable(ex.ToX, ex.ToY) || gm.OnWarp(ex.ToX, ex.ToY) {
					continue
				}
				for _, g := range goals {
					gx, gy := g[0], g[1]
					if !gm.Walkable(gx, gy) || gm.Region(gx, gy) != gm.Region(ex.ToX, ex.ToY) {
						continue
					}
					p := gm.FindPath(ex.ToX, ex.ToY, gx, gy, 80000)
					if len(p) == 0 {
						continue
					}
					exempt := gm.warpBoxesAt(gx, gy)
					for _, c := range p {
						if gm.OnWarp(c[0], c[1]) && !(gm.wmask[c[0]+c[1]*gm.W] == 2 && inWarpBoxes(exempt, c[0], c[1])) {
							t.Errorf("%s: path from the %s exit %d,%d to %d,%d crosses a warp at %d,%d", town, in, ex.ToX, ex.ToY, gx, gy, c[0], c[1])
							break
						}
					}
					// the first segment stepAlong sends (shortened while segHitsWarp)
					n := min(len(p), 11)
					for n > 1 && gm.segHitsWarp(ex.ToX, ex.ToY, p[n-1][0], p[n-1][1], gx, gy) {
						n--
					}
					if gm.segHitsWarp(ex.ToX, ex.ToY, p[n-1][0], p[n-1][1], gx, gy) && !inWarpBoxes(exempt, p[n-1][0], p[n-1][1]) {
						t.Errorf("%s: first step from %d,%d toward %d,%d runs over a warp", town, ex.ToX, ex.ToY, gx, gy)
					}
				}
			}
		}
	}
}

func TestBossThreatLevels(t *testing.T) {
	willowRL := &Mob{ID: 2615, Name: "Willow Ringleader", Level: 8, Champion: true}
	vocal := &Mob{Name: "Vocal", Level: 18, Boss: true}
	angeling := &Mob{Name: "Angeling", Level: 77, Boss: true}
	poring := &Mob{Name: "Poring", Level: 1}
	for _, c := range []struct {
		mb   *Mob
		lv   int
		want bool
	}{
		{willowRL, 10, true}, {willowRL, 24, true}, {willowRL, 30, false},
		{vocal, 10, true}, {vocal, 24, true}, {vocal, 30, false},
		{angeling, 60, true}, {angeling, 90, false},
		{poring, 1, false},
	} {
		if got := bossThreatMob(c.mb, c.lv); got != c.want {
			t.Errorf("%s vs lv%d: threat %v, want %v", c.mb.Name, c.lv, got, c.want)
		}
	}
}

// champions are a local note: they never veto a whole map (every field has some), real bosses below lv25 do
func TestChampionNeverClosesMap(t *testing.T) {
	mobs[2615] = &Mob{ID: 2615, Name: "Willow Ringleader", Level: 8, Champion: true}
	mobs[1088] = &Mob{ID: 1088, Name: "Vocal", Level: 18, Boss: true}
	noteBoss("zz_test_fild", 2615, 100, 100, false)
	if bossRecent("zz_test_fild", 10) {
		t.Error("a champion sighting closed the map for a lv10 bot")
	}
	if !bossSpotNear("zz_test_fild", 105, 105, 12, 10) {
		t.Error("a lv10 bot must keep away from the champion's spot")
	}
	if bossSpotNear("zz_test_fild", 105, 105, 12, 40) {
		t.Error("a lv40 bot needn't avoid a lv8 champion")
	}
	if recentDeaths("zz_test_fild") > 0 {
		t.Error("a champion sighting raised the map's danger for everyone")
	}
	noteBoss("zz_test_fild2", 1088, 50, 50, false)
	if !bossRecent("zz_test_fild2", 12) || bossRecent("zz_test_fild2", 40) {
		t.Error("a boss must close the map for bots under 25 only")
	}
}

func TestFightOnBlocksSit(t *testing.T) {
	now := time.Now()
	b := &Bot{aid: 7, ents: map[uint32]*Entity{}}
	if b.fightOn(now) {
		t.Fatal("no monsters: no fight")
	}
	b.ents[9] = &Entity{ID: 9, Type: objMob, Target: 7, TargetAt: now.Add(-2 * time.Second)}
	if !b.fightOn(now) {
		t.Error("a monster that swung at us 2 s ago (even a miss) is a fight")
	}
	b.ents[9].TargetAt = now.Add(-10 * time.Second)
	if b.fightOn(now) {
		t.Error("10 s ago is over")
	}
	b.target = 9
	if !b.fightOn(now) {
		t.Error("our target still in sight is a fight")
	}
}

// QA 14:34: geffen <-> gef_fild04 on the way to mjo_dun02 is travel (each leg through 119,213 gets closer), not a loop,
// and Geffen's north exit is on the plan - never banned.
func TestWarpLoopGeffenNorthExitIsProgress(t *testing.T) {
	loadRealMaps(t)
	badWarps.Range(func(k, _ any) bool { badWarps.Delete(k); return true })
	hopCacheReset()
	var north Warp
	for _, w := range warpsFrom["geffen"] {
		if w.X == 119 && w.Y == 213 {
			north = w
		}
	}
	if north.To != "gef_fild04" {
		t.Fatal("no geffen 119,213 -> gef_fild04 warp")
	}
	if !warpLegProgress(north, north.ToX, north.ToY, "mjo_dun02") {
		t.Error("geffen 119,213 toward mjo_dun02 must count as progress")
	}
	if !warpOnPlan(north, "mjo_dun02") {
		t.Error("geffen 119,213 is the planned exit toward mjo_dun02")
	}
	if warpLegProgress(Warp{From: "gef_fild04", X: 187, Y: 39, To: "geffen", ToX: 119, ToY: 210}, 119, 210, "mjo_dun02") {
		t.Error("walking back into Geffen is no progress toward mjo_dun02")
	}
	if warpLegProgress(north, north.ToX, north.ToY, "") {
		t.Error("no goal: no progress claim")
	}
}

// kafraVia only sends a bot back into town when kafraPlan will take it from there (no "after all" detours).
func TestKafraViaMatchesKafraPlan(t *testing.T) {
	loadRealMaps(t)
	now := time.Now()
	mk := func(zeny int) *Bot {
		return &Bot{P: &Persona{Name: "kv", BaseLevel: 63, Chars: []CharSpec{{Name: "kv"}}}, MapName: "gef_fild04", X: 187, Y: 44,
			Zeny: zeny, Class: 7, inv: map[int]*InvItem{}, equipped: map[uint32]*InvItem{}, buffAt: map[string]time.Time{}, savedTown: "geffen"}
	}
	rich := mk(200000)
	if vt, _ := rich.kafraVia("mjo_dun02", now); vt != "" && !rich.kafraWouldTake(vt, "mjo_dun02", now) {
		t.Errorf("kafraVia chose %s but kafraPlan would refuse there", vt)
	}
	if rich.MapName != "gef_fild04" || rich.X != 187 || rich.Y != 44 {
		t.Fatal("kafraWouldTake did not restore the position")
	}
	poor := mk(0)
	if vt, _ := poor.kafraVia("mjo_dun02", now); vt != "" {
		t.Errorf("a broke bot was sent to the Kafra in %s", vt)
	}
}
