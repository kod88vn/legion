package main

import (
	"math/rand"
	"strings"
	"testing"
	"time"
)

// pt35SnapLevels: how many bot characters (accounts 2100001-2199999) stand at each base level (the index) in the
// db snapshot tools/.state/dbsnap/snap-20261115-144812.json.gz: 7,817 bots, 140 of them lv80+.
var pt35SnapLevels = [100]int{
	0, 3130, 19, 18, 14, 27, 22, 27, 20, 14,
	67, 46, 26, 16, 14, 30, 25, 32, 22, 25,
	44, 25, 31, 34, 47, 43, 33, 41, 38, 52,
	46, 60, 72, 56, 73, 73, 71, 96, 67, 64,
	71, 55, 68, 91, 94, 91, 110, 98, 102, 125,
	108, 111, 75, 100, 95, 100, 95, 92, 77, 86,
	94, 83, 63, 90, 86, 85, 79, 51, 65, 55,
	69, 51, 41, 42, 38, 49, 32, 36, 37, 27,
	37, 28, 22, 14, 11, 13, 6, 4, 4, 0,
	1, 0, 0, 0, 0, 0, 0, 0, 0, 0,
}

// pt35OldBand: the band before PT35 (lfgLvBand = 10 around the party's average level).
func pt35OldBand(c *lfgCand, P *lfgParty) bool { return P.avg <= 0 || abs(c.lv-P.avg) <= 10 }

// pt35World: the snapshot's lv20+ bots as LFG candidates, each farming a real guide spot for its level (strategy.go
// guideSpots), with the cold-start simulation's role / goal / sociability mix. pool keeps that share of them
// (the bots online, free and on an auto hunt goal at one moment).
func pt35World(seed int64, pool float64) (all []*lfgCand, spotMin map[string]int) {
	spotMin = map[string]int{}
	for _, g := range guideSpots {
		if v, ok := spotMin[g.Map]; !ok || g.Min < v {
			spotMin[g.Map] = g.Min
		}
	}
	rng := rand.New(rand.NewSource(seed))
	for lv := 20; lv < len(pt35SnapLevels); lv++ {
		var fit []string
		for _, g := range guideSpots {
			if g.Min <= lv && lv <= g.Max {
				fit = append(fit, g.Map)
			}
		}
		if len(fit) == 0 {
			continue
		}
		for i := 0; i < pt35SnapLevels[lv]; i++ {
			if rng.Float64() >= pool {
				continue
			}
			at := fit[rng.Intn(len(fit))]
			c := &lfgCand{b: &Bot{}, lv: lv, at: at, social: rng.Float64() < 0.85}
			switch r := rng.Float64(); {
			case r < 0.15:
				c.role, c.healer = roleSupport, true
			case r < 0.19:
				c.role, c.monk = roleTank, true
			case r < 0.41:
				c.role = roleTank
			case r < 0.71:
				c.role = roleRanged
			default:
				c.role = roleMelee
			}
			switch r := rng.Float64(); {
			case c.healer:
				c.g = &farmGoal{Kind: "heal"}
			case r < 0.3:
				gm := fit[rng.Intn(len(fit))]
				c.g = &farmGoal{Kind: "card", Map: gm, Maps: []string{gm}}
			case r < 0.6:
				gm := fit[rng.Intn(len(fit))]
				c.g = &farmGoal{Kind: "zeny", Map: gm, Maps: []string{gm}}
			default:
				c.g = &farmGoal{Kind: "level"}
			}
			switch {
			case c.healer:
			case c.g.mapped():
				c.m = c.g.Map
			default:
				c.m = at
			}
			all = append(all, c)
		}
	}
	return all, spotMin
}

// pt35Region: a map's area by its name prefix (yuno_fild08 -> yuno; c_tower2 is Aldebaran's Clock Tower).
func pt35Region(m string) string {
	if strings.HasPrefix(m, "c_tower") || strings.HasPrefix(m, "alde") {
		return "alde"
	}
	if i := strings.IndexByte(m, '_'); i > 0 {
		return m[:i]
	}
	return m
}

// pt35Stub: map distance (same map 0, same area 3 hops, another area 7: beyond lfgMaxHops - nobody whispers across
// the world for a party) and the weak side of the map fit (the party's map no harder than partyMapOK's strict rule
// allows the newcomer: guide Min <= its level + 2). The strong side (mapExpPoor) and the band are the real lfgFits.
func pt35Stub(spotMin map[string]int) func() {
	oh, om, oj, of, ol := lfgHops, lfgMapOK, lfgJoin, lfgForm, lfgLvFit
	lfgHops = func(a string, ax, ay int, b string) int {
		switch {
		case a == b || b == "":
			return 0
		case pt35Region(a) == pt35Region(b):
			return 3
		}
		return 7
	}
	lfgMapOK = func(P *lfgParty, c *lfgCand) bool { return P.m == "" || c.lv >= spotMin[P.m]-2 }
	return func() { lfgHops, lfgMapOK, lfgJoin, lfgForm, lfgLvFit = oh, om, oj, of, ol }
}

// pt35PairFit: lv80+ social candidates that can party with at least one other candidate (either joins the other's
// solo party; healers never anchor), and how many such partners they have in all, under the level check in force.
func pt35PairFit(all []*lfgCand) (fit, n, partners int) {
	for _, c := range all {
		if c.lv < 80 || !c.social {
			continue
		}
		n++
		k := 0
		for _, o := range all {
			if o == c || !o.social {
				continue
			}
			if (!o.healer && lfgFits(c, soloParty(o)) && lfgMapOK(soloParty(o), c)) ||
				(!c.healer && lfgFits(o, soloParty(c)) && lfgMapOK(soloParty(c), o)) {
				k++
			}
		}
		partners += k
		if k > 0 {
			fit++
		}
	}
	return
}

// pt35Sim: the real matcher (lfgMatch) from a cold start for 15 ticks (5 min); returns lv80+ bots partied / lv80+
// social bots, and all bots partied / all social bots.
func pt35Sim(all []*lfgCand) (hi, hiN, tot, totN int) {
	var parties []*lfgParty
	lfgJoin = func(p *Party, c *lfgCand, now time.Time) bool { return true }
	lfgForm = func(anchor *lfgCand, group []*lfgCand, now time.Time, local bool) {
		P := soloParty(anchor)
		for _, g := range group {
			P.add(g)
		}
		P.p = &Party{}
		P.joins = 0
		parties = append(parties, P)
	}
	inParty := map[*Bot]bool{}
	now := time.Now()
	for tick := 1; tick <= 15; tick++ {
		var cs []*lfgCand
		for _, c := range all {
			if !inParty[c.b] && c.social {
				cs = append(cs, c)
			}
		}
		var open []*lfgParty
		for _, P := range parties {
			P.joins = 0
			if P.size < lfgMaxMembers+1 {
				open = append(open, P)
			}
		}
		lfgMatch(cs, open, map[*Bot]bool{}, now.Add(time.Duration(tick)*20*time.Second))
		for _, P := range parties {
			for _, b := range P.ms {
				inParty[b] = true
			}
		}
	}
	for _, c := range all {
		if !c.social {
			continue
		}
		totN++
		if inParty[c.b] {
			tot++
		}
		if c.lv >= 80 {
			hiN++
			if inParty[c.b] {
				hi++
			}
		}
	}
	return
}

// TestPT35HighLevelPartiesSnapshot (PT35, bug b1794772058nfq): on the snapshot's real level spread, lv80+ bots find
// a party partner and get partied by the real matcher more often with the share-range band than with the old +-10
// band - at full population and with a quarter of the bots free at one moment (fewer partners, as live).
func TestPT35HighLevelPartiesSnapshot(t *testing.T) {
	if partyLvSpread() != 99 {
		t.Fatalf("party_lv_spread default %d, want 99 (= party_share_level in rathena/conf/import/inter_conf.txt)", partyLvSpread())
	}
	const seeds = 8
	pct := func(a, b int) float64 { return float64(a) * 100 / float64(max(1, b)) }
	for _, pool := range []float64{1, 0.25, 0.1, 0.05} {
		var fitOld, fitNew, n, prOld, prNew, hiOld, hiNew, hiN, totOld, totNew, totN int
		for seed := int64(1); seed <= seeds; seed++ {
			all, spotMin := pt35World(seed, pool)
			restore := pt35Stub(spotMin)
			lfgLvFit = pt35OldBand
			fo, nn, po := pt35PairFit(all)
			ho, hn, to, tn := pt35Sim(all)
			lfgLvFit = lfgLvFits
			fn, _, pn := pt35PairFit(all)
			hw, _, tw, _ := pt35Sim(all)
			restore()
			fitOld, fitNew, n, prOld, prNew = fitOld+fo, fitNew+fn, n+nn, prOld+po, prNew+pn
			hiOld, hiNew, hiN = hiOld+ho, hiNew+hw, hiN+hn
			totOld, totNew, totN = totOld+to, totNew+tw, totN+tn
		}
		t.Logf("pool %3.0f%% (%d seeds): lv80+ social bots with a fitting partner: old +-10 %d/%d (%.0f%%), new %d/%d (%.0f%%); partners each: old %.1f, new %.1f",
			pool*100, seeds, fitOld, n, pct(fitOld, n), fitNew, n, pct(fitNew, n), float64(prOld)/float64(max(1, n)), float64(prNew)/float64(max(1, n)))
		t.Logf("pool %3.0f%% (%d seeds): partied after a 5-min cold start: lv80+ old %d/%d (%.0f%%), new %d/%d (%.0f%%); all lv20+ old %.1f%%, new %.1f%%",
			pool*100, seeds, hiOld, hiN, pct(hiOld, hiN), hiNew, hiN, pct(hiNew, hiN), pct(totOld, totN), pct(totNew, totN))
		if fitNew < fitOld || prNew <= prOld {
			t.Fatalf("pool %.0f%%: the wider band must give lv80+ bots more party partners (fit %d -> %d, partners %d -> %d)", pool*100, fitOld, fitNew, prOld, prNew)
		}
		if pct(totNew, totN) < pct(totOld, totN)-2 {
			t.Fatalf("pool %.0f%%: the wider band must not cost the other levels their parties (%.1f%% -> %.1f%%)", pool*100, pct(totOld, totN), pct(totNew, totN))
		}
	}
}

// TestPT35Band: the band itself - spread within party_lv_spread, the strongest still gets real exp on a map for the
// weakest, and lfgFits takes a lv88 into a lv75 party on a lv76-94 map (the old +-10 band refused it).
func TestPT35Band(t *testing.T) {
	if !partyLvOK(75, 88, 0) || !partyLvOK(60, 85, 0) {
		t.Fatal("lv75-88 and lv60-85 parties share exp evenly and have maps that feed their strongest")
	}
	if partyLvOK(20, 88, 0) {
		t.Fatal("a lv20 weakest drags a lv88 to maps with no exp for it")
	}
	if partyLvOK(50, 90, 0) {
		t.Fatal("lv50's best guide spot tops out at 70: no real exp for a lv90")
	}
	P := lfgTestParty("ein_fild06", 75, map[int]int{roleTank: 1, roleSupport: 1}, true)
	c := &lfgCand{lv: 88, role: roleRanged, at: "ein_fild06", g: &farmGoal{Kind: "level"}}
	if !lfgFits(c, P) {
		t.Fatal("PT35: a lv88 joins a lv75 party on ein_fild06 (lv76-94)")
	}
	if pt35OldBand(c, P) {
		t.Fatal("setup: the old band refused this")
	}
	low := lfgTestParty("pay_fild04", 30, map[int]int{roleTank: 1}, false)
	if lfgFits(&lfgCand{lv: 60, role: roleRanged, at: "pay_fild04", g: &farmGoal{Kind: "level"}}, low) {
		t.Fatal("a lv60 levelling bot doesn't join a party on a lv22-36 map (no exp for it)")
	}
	if !lfgFits(&lfgCand{lv: 60, role: roleRanged, at: "pay_fild04", g: &farmGoal{Kind: "card", Map: "pay_fild04", Maps: []string{"pay_fild04"}}}, low) {
		t.Fatal("a lv60 farming a card on that map may join")
	}
}
