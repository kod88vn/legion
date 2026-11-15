package main

// Offline measurement helpers (skipped by default): TestZZDumpMapLv writes each map's [mean lv, aggressive %, guide max]
// (LEGION_MAPLV_OUT); TestZZSweep re-fits a saved /api/parties/detail (LEGION_PARTIES) with member powers from wing
// events ("lv X vs <power>", LEGION_POWERS json name->power; else 72% of the level from lv30) for several partyMapOverLv.

import (
	"encoding/json"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

func TestZZDumpMapLv(t *testing.T) {
	out := os.Getenv("LEGION_MAPLV_OUT")
	if out == "" {
		t.Skip()
	}
	*rathena = os.Getenv("LEGION_RATHENA")
	loadGameData()
	m := map[string][3]int{}
	for k := range spawnsByMap {
		mx, ag, tot := 0, 0, 0
		for _, s := range spawnsByMap[k] {
			if mb := mobs[s.MobID]; mb != nil && !mb.Boss && s.Count > 0 {
				tot += s.Count
				if mb.Aggressive {
					ag += s.Count
				}
			}
		}
		if tot > 0 {
			mx = ag * 100 / tot
		}
		gmax := 0
		for _, g := range guideSpots {
			if g.Map == k {
				gmax = g.Max
			}
		}
		m[k] = [3]int{mapMeanLv(k), mx, gmax}
	}
	b, _ := json.Marshal(m)
	os.WriteFile(out, b, 0644)
}

func TestZZSweep(t *testing.T) {
	if os.Getenv("LEGION_SWEEP") == "" {
		t.Skip()
	}
	*rathena = os.Getenv("LEGION_RATHENA")
	loadGameData()
	var pd []struct {
		Name, Map, Human string
		Members          []struct {
			Name, Map string
			Level     int
			Online    bool
		}
	}
	raw, _ := os.ReadFile(os.Getenv("LEGION_PARTIES"))
	json.Unmarshal(raw, &pd)
	pw := map[string]int{}
	raw, _ = os.ReadFile(os.Getenv("LEGION_POWERS"))
	json.Unmarshal(raw, &pw)
	pow := func(n string, lv int) int {
		if p, ok := pw[n]; ok {
			return p
		}
		if lv >= 30 {
			return lv * 72 / 100
		}
		return lv
	}
	// publish every member first and rebuild the crowd from all of them (production state), then judge
	for _, p := range pd {
		for _, m := range p.Members {
			v := &atomic.Value{}
			v.Store(pubState{Name: "zzs_" + m.Name, Lv: m.Level, Power: pow(m.Name, m.Level), Online: m.Online, Known: m.Online, Map: m.Map})
			pubs.Store("zzs_"+m.Name, v)
		}
	}
	crowd.Lock()
	crowd.at = time.Time{}
	crowd.Unlock()
	for _, x := range []int{4} {
		partyMapOverLv = x
		moved, stuck, tooEasy, total, aggBefore, aggAfter, begAfter, beg45Before := 0, 0, 0, 0, 0, 0, 0, 0
		loadB, loadA := map[string]int{}, map[string]int{}
		why := map[string]int{}
		dest := map[string]int{}
		gaps := map[int]int{}
		for _, p := range pd {
			if p.Human != "" || p.Map == "" || isTown(p.Map) {
				continue
			}
			total++
			var ms []*Bot
			minP, on := 999, 0
			for _, m := range p.Members {
				pv := pow(m.Name, m.Level)
				if m.Online {
					minP = min(minP, pv)
					on++
				}
				b := &Bot{P: &Persona{Name: "zzs_" + m.Name, BaseLevel: 1, Aggression: 0.5}, BaseLv: m.Level, Online: true, invWaitFrom: time.Now()}
				v := &atomic.Value{}
				v.Store(pubState{Name: b.P.Name, Lv: m.Level, Power: pv, Online: m.Online, Known: m.Online, Map: m.Map})
				pubs.Store(b.P.Name, v)
				ms = append(ms, b)
			}
			nm := p.Map
			minBase := partyMinBase(ms)
			if beginnerMapZ(p.Map) && minBase >= 45 && minBase < 999 {
				beg45Before++
			}
			if partyMapOK(ms, p.Map) && mapExpPoor(p.Map, minBase) && partyMapNearTier(ms, nearestTown(p.Map), townSpawn[nearestTown(p.Map)][0], townSpawn[nearestTown(p.Map)][1], p.Map, false) != "" {
				if r := partyMapNear(ms, nearestTown(p.Map), townSpawn[nearestTown(p.Map)][0], townSpawn[nearestTown(p.Map)][1], p.Map); r != "" {
					nm = r
					moved++
					why["poor"]++
				}
			} else if partyMapOK(ms, p.Map) && mapCrowded(p.Map) && partyMapSearch(ms, nearestTown(p.Map), townSpawn[nearestTown(p.Map)][0], townSpawn[nearestTown(p.Map)][1], p.Map, partyAnySafe, true) != "" {
				if r := partyMapNear(ms, nearestTown(p.Map), townSpawn[nearestTown(p.Map)][0], townSpawn[nearestTown(p.Map)][1], p.Map); r != "" {
					nm = r
					moved++
					why["crowd"]++
				}
			} else if !partyMapOK(ms, p.Map) {
				if partyMapTooEasy(ms, p.Map) {
					tooEasy++
				}
				if r := partyMapNear(ms, nearestTown(p.Map), townSpawn[nearestTown(p.Map)][0], townSpawn[nearestTown(p.Map)][1], p.Map); r != "" {
					nm = r
					moved++
				} else {
					stuck++
					if stuck <= 6 {
						mn, ft, kn, _ := partyFitLv(ms)
						t.Logf("stuck %s on %s: weakest %d fit %d known %d hard %v fitshare %.2f hops-from-town %d", p.Name, p.Map, mn, ft, kn, partyMapTooHard(p.Map, mn), fitShare(p.Map, ft), mapHops(nearestTown(p.Map), townSpawn[nearestTown(p.Map)][0], townSpawn[nearestTown(p.Map)][1], "pay_fild09"))
					}
				}
			}
			loadB[p.Map] += on
			loadA[nm] += on
			if nm != p.Map {
				dest[nm]++
			}
			if mapAggroPct(p.Map) >= 40 {
				aggBefore++
			}
			if mapAggroPct(nm) >= 40 {
				aggAfter++
			}
			if beginnerMapZ(nm) && minBase >= 45 && minBase < 999 {
				begAfter++
				if begAfter <= 8 {
					mn, ft, _, _ := partyFitLv(ms)
					t.Logf("  stays %s on %s: rated %d fit %d base %d poor %v ok %v tier0 %q", p.Name, nm, mn, ft, minBase, mapExpPoor(nm, minBase), partyMapOK(ms, nm),
						partyMapNearTier(ms, nearestTown(p.Map), townSpawn[nearestTown(p.Map)][0], townSpawn[nearestTown(p.Map)][1], p.Map, false))
				}
			}
			g := mapMeanLv(nm) - minP
			gaps[(g+300)/4*4-300]++
		}
		t.Logf("overLv %+d: on >=40%% aggressive maps %d -> %d, all-lv45+ parties on beginner fields (band max <= 28) %d -> %d", x, aggBefore, aggAfter, beg45Before, begAfter)
		over := func(l map[string]int) (n, bots int) {
			for m, c := range l {
				if c > mapCap(m) {
					t.Logf("    over: %s %d/%d", m, c, mapCap(m))
					n++
					bots += c - mapCap(m)
				}
			}
			return
		}
		t.Logf("moves by reason %v; destinations %v", why, dest)
		ob, obb := over(loadB)
		oa, oab := over(loadA)
		t.Logf("overLv %+d: maps over their crowd cap (party members only) %d (%d bots over) -> %d (%d over)", x, ob, obb, oa, oab)
		t.Logf("overLv %+d: parties %d, moved %d (too easy %d), no fitting map %d, final gap (mean - weakest power) %v", x, total, moved, tooEasy, stuck, gaps)
	}
}

func beginnerMapZ(m string) bool {
	for _, g := range guideSpots {
		if g.Map == m && g.Max <= 28 {
			return true
		}
	}
	return false
}

// TestZZWhy: for every party with all members lv45+ on a beginner field, why no mid-level map fits (LEGION_WHY=1).
func TestZZWhy(t *testing.T) {
	if os.Getenv("LEGION_WHY") == "" {
		t.Skip()
	}
	*rathena = os.Getenv("LEGION_RATHENA")
	loadGameData()
	var pd []struct {
		Name, Map, Human string
		Members          []struct {
			Name, Class, Map string
			Level            int
		}
	}
	raw, _ := os.ReadFile(os.Getenv("LEGION_PARTIES"))
	json.Unmarshal(raw, &pd)
	pw := map[string]int{}
	raw, _ = os.ReadFile(os.Getenv("LEGION_POWERS"))
	json.Unmarshal(raw, &pw)
	cls := map[string]int{"Mage": 2, "Wizard": 9, "Sage": 16, "Alchemist": 18}
	stuck := 0
	for _, p := range pd {
		if p.Human != "" || !beginnerMapZ(p.Map) {
			continue
		}
		all45 := true
		var ms []*Bot
		for _, m := range p.Members {
			all45 = all45 && m.Level >= 45
			pv, ok := pw[m.Name]
			if !ok {
				pv = m.Level * 72 / 100
			}
			b := &Bot{P: &Persona{Name: "zzw_" + m.Name, Aggression: 0.5}, BaseLv: m.Level, Class: cls[m.Class], Online: true}
			v := &atomic.Value{}
			v.Store(pubState{Name: b.P.Name, Lv: m.Level, Power: pv, Online: true, Known: true, Fragile: fragileClass(cls[m.Class])})
			pubs.Store(b.P.Name, v)
			ms = append(ms, b)
		}
		if !all45 {
			continue
		}
		stuck++
		minLv, fitLv, _, _ := partyFitLv(ms)
		t.Logf("%s on %s: rated weakest lv%d fit %d; ok=%v tooEasy=%v near=%q", p.Name, p.Map, minLv, fitLv, partyMapOK(ms, p.Map), partyMapTooEasy(ms, p.Map),
			partyMapNear(ms, nearestTown(p.Map), townSpawn[nearestTown(p.Map)][0], townSpawn[nearestTown(p.Map)][1], p.Map))
		for _, g := range guideSpots {
			if g.Max < minLv-3 || g.Min > minLv+6 || maps[g.Map] == nil {
				continue
			}
			why := ""
			if g.Min > minLv+2 {
				why += " min"
			}
			if fitShare(g.Map, fitLv) < minFitShare {
				why += " fitshare"
			}
			if swarmVeto(g.Map, minLv) {
				why += " swarm"
			}
			if partyMapTooHard(g.Map, minLv) {
				why += fmt.Sprintf(" toohard(mean%d,aggr%d)", mapMeanLv(g.Map), mapAggroPct(g.Map))
			}
			if g.Ranged {
				why += " ranged"
			}
			t.Logf("    %s %d-%d:%s", g.Map, g.Min, g.Max, why)
		}
	}
	t.Logf("all-lv45+ parties on beginner fields: %d", stuck)
}

func TestZZAggroTop(t *testing.T) {
	if os.Getenv("LEGION_WHY") == "" {
		t.Skip()
	}
	*rathena = os.Getenv("LEGION_RATHENA")
	loadGameData()
	for _, m := range []string{"moc_fild03", "moc_fild02", "moc_fild01", "moc_fild11", "moc_fild12", "pay_fild05", "prt_fild01", "pay_fild01", "prt_fild08", "gef_fild03", "gef_fild10", "mjolnir_02", "mjolnir_06", "prt_fild10", "pay_fild09", "gef_fild02", "gef_fild09", "pay_fild04", "pay_dun00", "mjolnir_01", "pay_fild07", "mjolnir_04", "mjolnir_05", "mjolnir_08", "mjolnir_09", "mjo_dun01", "mjo_dun02", "gef_fild06", "gef_fild11", "gef_fild13", "prt_fild03", "pay_dun01", "orcsdun01", "moc_fild13", "pay_fild10", "prt_fild09", "mjolnir_07", "mjolnir_10", "mjolnir_12"} {
		t.Logf("%s: mean %d, aggressive %d%% of %d spawns, top common aggressive lv %d", m, mapMeanLv(m), mapAggroPct(m), mapSpawnCount(m), mapAggroTopLv(m))
	}
}
