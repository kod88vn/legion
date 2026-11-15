package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"testing"
)

// TestZenyHourLive (offline measurement, skipped unless LEGION_RATHENA and LEGION_BOTS are set): the real game data
// and a saved snapshot of the online bots (scratchpad ec8_snap.py: name, class, level, map, weapon, body armor).
// Before: expected zeny/hour of each solo bot's current map with the current crowds. After: each bot picks, in turn,
// the guide spot that pays it the most per hour among the spots fitting its effective level within (nearest + 1) map
// changes - the new solo rule - with the crowd counted as the bots pick. Prints median z/h per level band and the
// most crowded maps (bots / mapCap) before and after.
//
//	LEGION_RATHENA=/rdata LEGION_BOTS=/snap/ec8_snap.json go test -run TestZenyHourLive -v
func TestZenyHourLive(t *testing.T) {
	rd, bf := os.Getenv("LEGION_RATHENA"), os.Getenv("LEGION_BOTS")
	if rd == "" || bf == "" {
		t.Skip("offline measurement: set LEGION_RATHENA and LEGION_BOTS")
	}
	*rathena = rd
	loadGameData()
	if v := os.Getenv("LEGION_DROPMUL"); v != "" { // e.g. 5: the server's drop rates after the restart
		nt := *tune()
		fmt.Sscan(v, &nt.DropRateMul)
		curTuning.Store(&nt)
	}
	raw, err := os.ReadFile(bf)
	if err != nil {
		t.Fatal(err)
	}
	var bots []struct {
		Name, Class, Map  string
		Level, W, A, X, Y int
		Party             bool
	}
	if err := json.Unmarshal(raw, &bots); err != nil {
		t.Fatal(err)
	}
	classID := map[string]int{}
	for c := 0; c < 4400; c++ {
		if n := className(c); n != "" {
			if _, ok := classID[n]; !ok {
				classID[n] = c
			}
		}
	}
	band := func(lv int) string {
		switch {
		case lv < 30:
			return "a <30"
		case lv < 45:
			return "b 30-44"
		case lv < 60:
			return "c 45-59"
		case lv < 80:
			return "d 60-79"
		}
		return "e 80+"
	}
	before := map[string]int{}
	for _, b := range bots {
		before[b.Map]++
	}
	type row struct {
		lv        int
		zb, za    float64
		from, to  string
		power     int
		fam       int
		x, y      int
		party     bool
		candCount int
	}
	var rows []*row
	for _, b := range bots {
		if isTown(b.Map) || maps[b.Map] == nil {
			continue
		}
		f := family(classID[b.Class])
		def := 0
		if x := items[b.A]; x != nil {
			def = x.Def
		}
		p := effectiveLv(f, b.Level, items[b.W], def)
		rows = append(rows, &row{lv: b.Level, from: b.Map, power: p, fam: f, x: b.X, y: b.Y, party: b.Party,
			zb: mapZenyHourCalc(b.Map, p, f, before[b.Map])})
	}
	// after: solo bots re-pick in turn; party members stay (their party picker is scored separately)
	after := map[string]int{}
	for _, r := range rows {
		if r.party {
			after[r.from]++
		}
	}
	for _, r := range rows {
		if r.party {
			r.to, r.za = r.from, mapZenyHourCalc(r.from, r.power, r.fam, before[r.from])
			continue
		}
		type cand struct {
			m string
			h int
		}
		var cs []cand
		bestH := 99
		for _, g := range guideSpots {
			if r.power < g.Min || r.power > g.Max+3 || maps[g.Map] == nil || fitShare(g.Map, r.power+5) < minFitShare ||
				after[g.Map] >= mapCap(g.Map) {
				continue
			}
			h := 0
			if g.Map != r.from {
				h = mapHops(r.from, r.x, r.y, g.Map)
			}
			if h > 8 {
				continue
			}
			cs = append(cs, cand{g.Map, h})
			bestH = min(bestH, h)
		}
		r.to, r.za = r.from, mapZenyHourCalc(r.from, r.power, r.fam, after[r.from]+1)
		if after[r.from] >= mapCap(r.from) {
			r.za = -1 // over the cap: the bot doesn't stay (goodHere / mapHasRoom)
		}
		for _, c := range cs {
			if c.h > bestH+1 {
				continue
			}
			r.candCount++
			if z := mapZenyHourCalc(c.m, r.power, r.fam, after[c.m]+1); z > r.za {
				r.to, r.za = c.m, z
			}
		}
		if r.za < 0 { // nothing with room: stays anyway
			r.za = mapZenyHourCalc(r.from, r.power, r.fam, after[r.from]+1)
		}
		after[r.to]++
	}
	med := func(xs []float64) float64 {
		if len(xs) == 0 {
			return 0
		}
		sort.Float64s(xs)
		return xs[len(xs)/2]
	}
	bb, ba := map[string][]float64{}, map[string][]float64{}
	moved := 0
	for _, r := range rows {
		bb[band(r.lv)] = append(bb[band(r.lv)], r.zb)
		ba[band(r.lv)] = append(ba[band(r.lv)], r.za)
		if r.to != r.from {
			moved++
		}
	}
	var keys []string
	for k := range bb {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		t.Logf("band %s: %d bots, median expected z/h before %.0f -> after %.0f", k, len(bb[k]), med(bb[k]), med(ba[k]))
	}
	t.Logf("solo bots that would move: %d of %d", moved, len(rows))
	crowd := func(m map[string]int) string {
		type mc struct {
			m string
			n int
		}
		var l []mc
		over := 0
		for k, v := range m {
			if isTown(k) {
				continue
			}
			l = append(l, mc{k, v})
			if v > mapCap(k) {
				over++
			}
		}
		sort.Slice(l, func(i, j int) bool { return l[i].n > l[j].n })
		s := fmt.Sprintf("%d maps over cap;", over)
		for _, x := range l[:min(6, len(l))] {
			s += fmt.Sprintf(" %s %d/%d", x.m, x.n, mapCap(x.m))
		}
		return s
	}
	t.Logf("crowding before: %s", crowd(before))
	t.Logf("crowding after:  %s", crowd(after))
	for _, m := range []string{"moc_fild01", "prt_fild07", "gef_fild02", "pay_fild07", "pay_fild09", "mjolnir_01"} {
		t.Logf("  %s: z/h at power 35 (mage %0.f, melee %.0f), crowd %d", m, mapZenyHourCalc(m, 35, famMage, before[m]),
			mapZenyHourCalc(m, 35, famSword, before[m]), before[m])
	}
}
