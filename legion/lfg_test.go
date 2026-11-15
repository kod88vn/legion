package main

import (
	"fmt"
	"math/rand"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestFarmGoalServes(t *testing.T) {
	g := &farmGoal{Kind: "gear", Item: "Ninja Suit", Map: "pay_dun01", Maps: []string{"pay_dun01", "pay_dun02"}}
	if !g.fixed() || !g.serves("pay_dun02") || g.serves("prt_fild08") {
		t.Fatal("gear goal must be served only by its drop maps")
	}
	for _, k := range []string{"zeny", "level", "heal"} {
		if f := (&farmGoal{Kind: k}); f.fixed() || !f.serves("anything") {
			t.Fatalf("%s goal must be flexible", k)
		}
	}
	if (&farmGoal{Kind: "card", Mob: "Hydra"}).desc() != "Hydra card" {
		t.Fatal("card desc")
	}
}

func lfgTestParty(m string, avg int, roles map[int]int, healer bool) *lfgParty {
	P := &lfgParty{m: m, avg: avg, lo: avg, hi: avg}
	for r, n := range roles {
		P.roles[r] += n
		P.size += n
		if r == roleSupport && healer {
			continue
		}
		P.nonHealers += n
	}
	P.healer = healer
	return P
}

func TestLfgFits(t *testing.T) {
	m := "pay_fild04"
	P := lfgTestParty(m, 40, map[int]int{roleTank: 1, roleRanged: 1}, false)
	healer := &lfgCand{lv: 45, role: roleSupport, healer: true, at: m}
	if !lfgFits(healer, P) {
		t.Fatal("a healer within 10 levels joins a party without one")
	}
	if lfgFits(&lfgCand{lv: 55, role: roleSupport, healer: true, at: m}, P) {
		t.Fatal("level band +-10")
	}
	P.healer = true
	if lfgFits(healer, P) {
		t.Fatal("one healer per party")
	}
	fixed := &lfgCand{lv: 40, role: roleRanged, at: m, g: &farmGoal{Kind: "gear", Map: "moc_fild03", Maps: []string{"moc_fild03"}}}
	if lfgFits(fixed, P) {
		t.Fatal("an item goal on another map doesn't fit")
	}
	flex := &lfgCand{lv: 40, role: roleRanged, at: m, g: &farmGoal{Kind: "level"}}
	if !lfgFits(flex, P) {
		t.Fatal("a flexible goal fits any party map")
	}
	full := lfgTestParty(m, 40, map[int]int{roleRanged: 3}, false)
	if lfgFits(flex, full) {
		t.Fatal("max 3 ranged")
	}
	five := lfgTestParty(m, 40, map[int]int{roleTank: 2, roleRanged: 2, roleMelee: 1}, false)
	if lfgFits(&lfgCand{lv: 40, role: roleMelee, at: m, g: &farmGoal{Kind: "level"}}, five) {
		t.Fatal("max 5 non-healers")
	}
	if !lfgFits(healer, five) {
		t.Fatal("a full party still takes its healer (5+1)")
	}
	if lfgFits(&lfgCand{lv: 40, role: roleNone, at: m, g: &farmGoal{Kind: "level"}}, P) {
		t.Fatal("no Novices")
	}
}

func TestLfgScorePrefersHealerThenMonk(t *testing.T) {
	P := lfgTestParty("x", 40, map[int]int{roleTank: 1}, false)
	h := lfgScore(&lfgCand{lv: 40, role: roleSupport, healer: true}, P)
	mk := lfgScore(&lfgCand{lv: 40, role: roleTank, monk: true}, P)
	r := lfgScore(&lfgCand{lv: 40, role: roleRanged}, P)
	if !(h > mk && mk > r) {
		t.Fatalf("priority healer > monk > other: %d %d %d", h, mk, r)
	}
}

func TestLfgLinesInCharacter(t *testing.T) {
	bad := []string{"bot", "ai ", "script", "legion", "npc"}
	cs := []*lfgCand{
		{class: "priest", lv: 50, healer: true, m: "pay_dun01", g: &farmGoal{Kind: "heal"}},
		{class: "assassin", lv: 52, m: "pay_dun01", g: &farmGoal{Kind: "gear", Item: "Ninja Suit"}},
		{class: "hunter", lv: 40, m: "moc_fild03", g: &farmGoal{Kind: "card", Mob: "Hode"}},
		{class: "knight", lv: 40, m: "moc_fild03", g: &farmGoal{Kind: "level"}},
	}
	for i := 0; i < 50; i++ {
		for _, c := range cs {
			for _, l := range []string{lfgLine(c), lfgAnswer(c), metLine(c.g)} {
				low := strings.ToLower(l)
				for _, w := range bad {
					if strings.Contains(low, w) {
						t.Fatalf("line %q contains %q", l, w)
					}
				}
				if len(l) > 80 {
					t.Fatalf("line too long: %q", l)
				}
			}
		}
	}
}

func TestLfgTakeRateLimit(t *testing.T) {
	lfg.Lock()
	lfg.tokens, lfg.fill = 0, time.Time{}
	lfg.Unlock()
	now := time.Now()
	lfgTake(now) // starts the clock
	n := 0
	for s := 0; s < 600; s += 20 { // 10 minutes, a tick every 20 s, everyone wanting to post
		for k := 0; k < 5; k++ {
			if lfgTake(now.Add(time.Duration(s) * time.Second)) {
				n++
			}
		}
	}
	per := tune().LfgGlobalPerMin
	if n > per*10+2 || n < per*10-4 {
		t.Fatalf("%d posts in 10 min, want about %d", n, per*10)
	}
}

func TestHumanTurnRotation(t *testing.T) {
	*dataDir = t.TempDir()
	turns.Lock()
	turns.m = map[string]*turnState{"ayla": {Human: "Ayla", Goal: &humanGoal{Kind: "card", Item: "Soldier Skeleton Card", Map: "pay_dun01"}}}
	turns.Unlock()
	now := time.Now()
	humanTurnStep(nil, "Ayla", nil, now)
	turns.Lock()
	st := turns.m["ayla"]
	if st.Turn != "Ayla" || st.Map != "pay_dun01" {
		turns.Unlock()
		t.Fatalf("Ayla's goal should hold the turn: %+v", st)
	}
	st.Goal.Met = now
	turns.Unlock()
	humanTurnStep(nil, "Ayla", nil, now.Add(time.Minute))
	turns.Lock()
	defer turns.Unlock()
	if st.Turn != "" || st.Served["Ayla"] < 0.9 {
		t.Fatalf("a met goal ends the turn and counts its minutes: %+v", st)
	}
}

func TestZenySellGoalMapped(t *testing.T) {
	g := &farmGoal{Kind: "zeny", Item: "Claymore", Sell: "Yellow Herb", Map: "pay_fild08", Maps: []string{"pay_fild08"}}
	if !g.mapped() || g.fixed() || !g.serves("prt_fild08") || !g.onMap("pay_fild08") || g.onMap("prt_fild08") {
		t.Fatal("a zeny goal with a sellable item has its own map but fits any party")
	}
	if g.desc() != "farming Yellow Herb" {
		t.Fatal(g.desc())
	}
	if (&farmGoal{Kind: "zeny"}).mapped() {
		t.Fatal("plain zeny goal has no map")
	}
}

// TestLfgColdStartSim: ~1,000 solo bots (lv20-90) right after a restart with no saved parties - how fast direct
// matching (same map, then across maps; no #global posts) gets them into parties. 20 s ticks; maps, joins and forms
// are stubs (every match succeeds, map fit = level band handled by the cands' own maps).
func TestLfgColdStartSim(t *testing.T) {
	oh, om, oj, of := lfgHops, lfgMapOK, lfgJoin, lfgForm
	defer func() { lfgHops, lfgMapOK, lfgJoin, lfgForm = oh, om, oj, of }()
	type simMap struct {
		name   string
		lo, hi int
	}
	var sm []simMap
	for i := 0; i < 40; i++ {
		lo := 18 + i*2
		sm = append(sm, simMap{fmt.Sprintf("m%02d", i), lo, lo + 12})
	}
	band := map[string]simMap{}
	for _, m := range sm {
		band[m.name] = m
	}
	lfgHops = func(a string, ax, ay int, b string) int {
		if a == b {
			return 0
		}
		return 3
	}
	lfgMapOK = func(P *lfgParty, c *lfgCand) bool { // the party map must suit the newcomer's level
		m := band[P.m]
		return c.lv >= m.lo-2 && c.lv <= m.hi+8
	}
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
	rng := rand.New(rand.NewSource(7))
	var all []*lfgCand
	for i := 0; i < 1000; i++ {
		lv := 20 + rng.Intn(71)
		var fit []simMap
		for _, m := range sm {
			if lv >= m.lo && lv <= m.hi {
				fit = append(fit, m)
			}
		}
		at := fit[rng.Intn(len(fit))].name
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
			gm := fit[rng.Intn(len(fit))].name
			c.g = &farmGoal{Kind: "card", Map: gm, Maps: []string{gm}}
		case r < 0.6:
			gm := fit[rng.Intn(len(fit))].name
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
	inParty := map[*Bot]bool{}
	now := time.Now()
	var last float64
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
		used := map[*Bot]bool{}
		lfgMatch(cs, open, used, now.Add(time.Duration(tick)*20*time.Second))
		for _, P := range parties {
			for _, b := range P.ms {
				inParty[b] = true
			}
		}
		n, heal := 0, 0
		for _, P := range parties {
			if P.healer || P.monk {
				heal++
			}
		}
		for _, c := range all {
			if inParty[c.b] {
				n++
			}
		}
		last = float64(n) / float64(len(all)) * 100
		if tick == 1 || tick == 3 || tick%5 == 0 {
			t.Logf("t+%3ds: %4.1f%% of 1000 lv20+ bots partied (%d parties, %d%% with a healer/Monk)", tick*20, last,
				len(parties), heal*100/max(1, len(parties)))
		}
	}
	if last < 80 {
		t.Fatalf("cold start: only %.1f%% partied after 5 min", last)
	}
}

// TestGuildLeadersOnlyLead: a guild leader is never a member of someone else's party - not through matching (as a
// healer or a farmer, same map or across maps), not through joinGoalParty, not through formPartyOpt.
func TestGuildLeadersOnlyLead(t *testing.T) {
	oh, om, oj, of := lfgHops, lfgMapOK, lfgJoin, lfgForm
	defer func() { lfgHops, lfgMapOK, lfgJoin, lfgForm = oh, om, oj, of }()
	lfgHops = func(a string, ax, ay int, b string) int { return 0 }
	lfgMapOK = func(P *lfgParty, c *lfgCand) bool { return true }
	var asMember []*Bot
	var anchors []*Bot
	lfgJoin = func(p *Party, c *lfgCand, now time.Time) bool {
		if c.leader {
			asMember = append(asMember, c.b)
		}
		return true
	}
	lfgForm = func(anchor *lfgCand, group []*lfgCand, now time.Time, local bool) {
		anchors = append(anchors, anchor.b)
		for _, g := range group {
			if g.leader {
				asMember = append(asMember, g.b)
			}
		}
	}
	mk := func(role int, healer, leader bool) *lfgCand {
		return &lfgCand{b: &Bot{}, lv: 60, role: role, healer: healer, leader: leader, at: "m1", m: "m1", g: &farmGoal{Kind: "level"}, posted: true}
	}
	healerLead := mk(roleSupport, true, true)
	healerLead.g = &farmGoal{Kind: "heal"} // a healer guild leader keeps its hunting map (lfgGather)
	farmLead := mk(roleRanged, false, true)
	cs := []*lfgCand{healerLead, farmLead, mk(roleTank, false, false), mk(roleRanged, false, false), mk(roleMelee, false, false),
		mk(roleSupport, true, false), mk(roleTank, false, false)}
	open := soloParty(mk(roleTank, false, false))
	open.p = &Party{}
	lfgMatch(cs, []*lfgParty{open}, map[*Bot]bool{}, time.Now())
	if len(asMember) > 0 {
		t.Fatalf("%d guild leader(s) matched as members", len(asMember))
	}
	// the real join refuses a leader outright
	if joinGoalParty(&Party{}, &lfgCand{b: &Bot{}, leader: true}, time.Now()) {
		t.Fatal("joinGoalParty took a guild leader")
	}
	// formPartyOpt drops guild leaders from the member list
	gl := &Bot{}
	guildLeaders.Lock()
	guildLeaders.ofBot[gl] = &GuildDef{}
	guildLeaders.Unlock()
	defer func() {
		guildLeaders.Lock()
		delete(guildLeaders.ofBot, gl)
		guildLeaders.Unlock()
	}()
	plain := &Bot{}
	if got := leadersOut([]*Bot{gl, plain}); len(got) != 1 || got[0] != plain {
		t.Fatal("leadersOut must drop the guild leader")
	}
}

// TestPartyStrengthRealParties: the party's effective level for the real parties QA traced (2026-11-14 13:55), from
// their published state - lv61-66 guild parties with starter weapons rate ~37-45 per member (gearpower.go), the
// fragile Alchemist leader 6 lower.
func TestPartyStrengthRealParties(t *testing.T) {
	mk := func(name string, lv, power int, fragile, healer, tank bool) *Bot {
		b := &Bot{P: &Persona{Name: "t_" + name, Chars: []CharSpec{{Name: name}}}}
		v, _ := pubs.LoadOrStore(b.P.Name, &atomic.Value{})
		v.(*atomic.Value).Store(pubState{Name: name, Online: true, Known: true, Lv: lv, Power: power, Fragile: fragile, Support: healer, Tank: tank})
		return b
	}
	blaze := []*Bot{mk("Blaze.mage", 64, 43, true, false, false), mk("Hana_queen", 63, 45, false, false, true),
		mk("Nova rogue", 62, 41, false, false, false), mk("xXMoonkunXx22", 64, 40, false, false, false), mk("skylord", 66, 45, false, false, true)}
	minLv, _, known, _ := partyFitLv(blaze)
	t.Logf("Blaze.mage pt (no healer): weakest effective %d, strength +%d", minLv, partyStrength(blaze))
	if known != 5 || minLv != 37+4 { // tank +3, 4+ online +1; no healer
		t.Fatalf("Blaze.mage pt: weakest %d", minLv)
	}
	star := []*Bot{mk("Star.soul", 64, 40, false, false, false), mk("Holy_sage", 62, 45, false, true, false),
		mk("TheGhostshot", 57, 38, true, false, false), mk("FrostPriest", 50, 30, false, true, false)}
	minLv, _, _, _ = partyFitLv(star)
	t.Logf("Star.soul pt (2 priests, no tank): weakest effective %d, strength +%d", minLv, partyStrength(star))
	if minLv != 30+5 {
		t.Fatalf("Star.soul pt: weakest %d", minLv)
	}
	if raw, _, _, _ := partyFitLvRaw(star); raw != 30 {
		t.Fatalf("item-goal maps use the weakest member alone (no strength bonus): %d", raw)
	}
	if partyMapTooEasy(star, "pay_fild02") != true {
		t.Fatal("pay_fild02 (13-27) must read outgrown for Star.soul pt")
	}
	// capped at the weakest real base level
	low := []*Bot{mk("a", 20, 19, false, true, false), mk("b", 21, 21, false, false, true), mk("c", 22, 22, false, false, false), mk("d", 22, 22, false, false, false)}
	if minLv, _, _, _ = partyFitLv(low); minLv > 20 {
		t.Fatalf("strength may not lift past the weakest base level: %d", minLv)
	}
}

// TestFitHold: a party on its way to a new map keeps it until it arrives and settles; a town run keeps the map.
func TestFitHold(t *testing.T) {
	if !fitHold("prt_fild08", "pay_dun01", 14*time.Minute, 0, true, false) {
		t.Fatal("walking to pay_dun01 (14 min after the move): no re-pick")
	}
	if fitHold("prt_fild08", "pay_dun01", 14*time.Minute, 0, true, true) {
		t.Fatal("the map turned deadly: re-pick at once")
	}
	if !fitHold("alberta", "pay_fild02", 14*time.Minute, 0, true, false) {
		t.Fatal("a town run on the way keeps the party's map (RULES.md 3: return to the same map)")
	}
	if fitHold("prt_fild08", "pay_dun01", 50*time.Minute, 0, true, false) {
		t.Fatal("45 min without arriving: re-fit")
	}
	if !fitHold("pay_dun01", "pay_dun01", 20*time.Minute, 5*time.Minute, false, false) {
		t.Fatal("arrived 5 min ago: settle first")
	}
	if fitHold("pay_dun01", "pay_dun01", 30*time.Minute, 12*time.Minute, false, false) {
		t.Fatal("settled: the fit check runs")
	}
}

// TestMapStayOK: the ~2 h stay keeps a map that is merely a bit easy, not one outgrown by far.
func TestMapStayOK(t *testing.T) {
	mk := func(name string, lv, power int) *Bot {
		b := &Bot{P: &Persona{Name: "s_" + name, Chars: []CharSpec{{Name: name}}}}
		v, _ := pubs.LoadOrStore(b.P.Name, &atomic.Value{})
		v.(*atomic.Value).Store(pubState{Name: name, Online: true, Known: true, Lv: lv, Power: power})
		return b
	}
	blaze := []*Bot{mk("b1", 62, 41), mk("b2", 64, 44)}
	if !mapStayOK(blaze, "gef_fild02") {
		t.Fatal("lv62-64 (eff 41) on gef_fild02 (28-44): a bit easy, stays")
	}
	star := []*Bot{mk("s1", 50, 35), mk("s2", 64, 40)}
	if mapStayOK(star, "pay_fild02") {
		t.Fatal("lv50-64 on pay_fild02 (13-27): outgrown by far, moves")
	}
}

// TestPartyNameSuffix: a renamed party never stacks suffixes ("echodragon87 pt2 pt2").
func TestPartyNameSuffix(t *testing.T) {
	for in, want := range map[string]string{"echodragon87 pt2": "echodragon87", "echodragon87 pt2 pt2": "echodragon87", "Kira pt": "Kira", "Opt pt": "Opt"} {
		if got := cleanPartyName(partySuffix.ReplaceAllString(in, "")); got != want {
			t.Fatalf("%q -> %q, want %q", in, got, want)
		}
	}
}
