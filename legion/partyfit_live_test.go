package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"sync/atomic"
	"testing"
	"time"
)

// TestPartyFitLive (offline measurement, skipped unless LEGION_RATHENA and LEGION_PARTIES are set): loads the real
// game data and a saved /api/parties/detail, and counts bot parties whose party map is more than 15 levels over its
// weakest member ("map level" = spawn-weighted mean level of the map's non-boss monsters), before and after the
// party-fit check (partyMapFor) re-picks the map.
//
//	LEGION_RATHENA=/rdata LEGION_PARTIES=/src/pd.json go test -run TestPartyFitLive -v
func TestPartyFitLive(t *testing.T) {
	rd, pf := os.Getenv("LEGION_RATHENA"), os.Getenv("LEGION_PARTIES")
	if rd == "" || pf == "" {
		t.Skip("offline measurement: set LEGION_RATHENA and LEGION_PARTIES")
	}
	*rathena = rd
	loadGameData()
	raw, err := os.ReadFile(pf)
	if err != nil {
		t.Fatal(err)
	}
	var pd []struct {
		Name, Map, Human string
		Members          []struct {
			Name, Class, Map string
			Level            int
		}
	}
	if err := json.Unmarshal(raw, &pd); err != nil {
		t.Fatal(err)
	}
	mapLv := func(m string) int {
		n, sum := 0, 0
		for _, s := range spawnsByMap[m] {
			if mb := mobs[s.MobID]; mb != nil && !mb.Boss && s.Count > 0 {
				n += s.Count
				sum += s.Count * mb.Level
			}
		}
		if n == 0 {
			return 0
		}
		return sum / n
	}
	before, after, total, unfit, unfitAfter, onBeg, easy45, easyAfter := 0, 0, 0, 0, 0, 0, 0, 0
	var lines []string
	for _, p := range pd {
		if p.Human != "" || p.Map == "" || isTown(p.Map) {
			continue
		}
		total++
		var ms []*Bot
		minLv, who := 999, ""
		for _, m := range p.Members {
			b := &Bot{P: &Persona{Name: "zzfit_" + m.Name, BaseLevel: m.Level, Aggression: 0.5}, BaseLv: m.Level, Online: true, invWaitFrom: time.Now()} // no gear data: power = level
			v := &atomic.Value{}
			v.Store(pubState{Name: b.P.Name, Lv: m.Level, Power: m.Level, Online: true, Known: true, Map: m.Map})
			pubs.Store(b.P.Name, v)
			ms = append(ms, b)
			if m.Level < minLv {
				minLv, who = m.Level, m.Name
			}
		}
		ml := mapLv(p.Map)
		if beginnerMap(p.Map) {
			onBeg++
			if minLv >= 45 {
				easy45++
				pp := &Party{Members: ms, Map: p.Map}
				t.Logf("EASY %s on %s weakest lv%d: ok=%v tooEasy=%v spotNear=%q mapNear=%q", p.Name, p.Map, minLv, partyMapOK(ms, p.Map), partyMapTooEasy(ms, p.Map), partySpotNear(pp, p.Map, -1, -1), partyMapNear(ms, nearestTown(p.Map), townSpawn[nearestTown(p.Map)][0], townSpawn[nearestTown(p.Map)][1], p.Map))
			}
		}
		ok := partyMapOK(ms, p.Map)
		nm := p.Map
		if !ok { // partyFitCheck -> relocate -> partySpotNear (measured from the map's town)
			pp := &Party{Members: ms, Map: p.Map}
			if r := partySpotNear(pp, p.Map, -1, -1); r != "" {
				nm = r
			}
			unfit++
			if !partyMapOK(ms, nm) {
				unfitAfter++
			}
		}
		if minLv >= 45 && beginnerMap(nm) {
			easyAfter++
		}
		nl := mapLv(nm)
		if ml-minLv > 15 {
			before++
		}
		if nl-minLv > 15 {
			after++
		}
		if ml-minLv > 15 || nm != p.Map || !ok {
			fl := max(minLv+2+3, 8)
			why := fmt.Sprintf(" [fit %.2f swarm %v mean+%d]", fitShare(p.Map, fl), swarmVeto(p.Map, minLv), ml-minLv)
			lines = append(lines, p.Name+": "+p.Map+" lv"+itoa(ml)+" -> "+nm+" lv"+itoa(nl)+", weakest "+who+" lv"+itoa(minLv)+why)
		}
	}
	for _, g := range guideSpots {
		if mapMeanLv(g.Map) > g.Min+2+partyMapOverLv {
			t.Logf("guide spot %s (%d-%d): monsters average lv%d, a party only uses it from lv%d", g.Map, g.Min, g.Max, mapMeanLv(g.Map), mapMeanLv(g.Map)-partyMapOverLv)
		}
	}
	t.Logf("on beginner fields: %d parties; every member lv45+: %d before, %d after the fit check", onBeg, easy45, easyAfter)
	sort.Strings(lines)
	for _, l := range lines {
		t.Log(l)
	}
	t.Logf("bot parties: %d; weakest member >15 levels under the map: %d before, %d after; map unfit for the weakest member: %d before, %d after the fit check", total, before, after, unfit, unfitAfter)
}

// TestPartyMapMeanLv: a map whose filter share passes (60% of the monsters under the weakest member's limit) but
// whose other monsters are far over it is not a party map for that member (QA: Kimchi07 lv39 on a lv59 map).
func TestPartyMapMeanLv(t *testing.T) {
	spawnsByMap["zz_fild09"] = []Spawn{{Map: "zz_fild09", MobID: 990101, Count: 60}, {Map: "zz_fild09", MobID: 990102, Count: 40}}
	mobs[990101], mobs[990102] = &Mob{Level: 30}, &Mob{Level: 75}
	defer func() { delete(spawnsByMap, "zz_fild09"); delete(mobs, 990101); delete(mobs, 990102) }()
	mk := func(name string, lv int) *Bot {
		b := &Bot{P: &Persona{Name: name, BaseLevel: lv}, BaseLv: lv}
		v := &atomic.Value{}
		v.Store(pubState{Name: name, Lv: lv, Power: lv, Online: true, Known: true})
		pubs.Store(name, v)
		return b
	}
	defer func() { pubs.Delete("zz_weak"); pubs.Delete("zz_strong") }()
	weak, strong := mk("zz_weak", 30), mk("zz_strong", 60)
	if fitShare("zz_fild09", 32) < minFitShare {
		t.Fatal("setup: the filter share should pass")
	}
	if partyMapOK([]*Bot{strong, weak}, "zz_fild09") {
		t.Errorf("mean lv%d is %d over the weakest member: not a party map", mapMeanLv("zz_fild09"), mapMeanLv("zz_fild09")-30)
	}
	if !partyMapOK([]*Bot{strong, mk("zz_weak", 45)}, "zz_fild09") {
		t.Error("a lv45 weakest member farms it")
	}
}

// beginnerMap: a guide field whose band tops out at lv20 or below (prt_fild08, pay_fild03, gef_fild07 ...)
func beginnerMap(m string) bool {
	for _, g := range guideSpots {
		if g.Map == m && g.Max <= 28 {
			return true
		}
	}
	return false
}

// TestPartyFitIgnoresUnknownLevels: a member offline or still logging in reads as the persona's lv1 default; it must
// not drag the party's map down (QA 07:20: 66 parties of lv45+ moved to Novice fields after a deploy). A map the
// weakest member has outgrown is not OK (the party moves up).
func TestPartyFitIgnoresUnknownLevels(t *testing.T) {
	spawnsByMap["zz_fild08"] = []Spawn{{Map: "zz_fild08", MobID: 990111, Count: 100}}
	spawnsByMap["zz_fild07"] = []Spawn{{Map: "zz_fild07", MobID: 990112, Count: 100}}
	mobs[990111], mobs[990112] = &Mob{Level: 52}, &Mob{Level: 30}
	defer func() {
		delete(spawnsByMap, "zz_fild08")
		delete(spawnsByMap, "zz_fild07")
		delete(mobs, 990111)
		delete(mobs, 990112)
	}()
	mk := func(name string, lv int, online, known bool) *Bot {
		b := &Bot{P: &Persona{Name: name, BaseLevel: 1}}
		v := &atomic.Value{}
		v.Store(pubState{Name: name, Lv: lv, Power: lv, Online: online, Known: known})
		pubs.Store(name, v)
		return b
	}
	defer func() {
		for _, n := range []string{"zz_a", "zz_b", "zz_c"} {
			pubs.Delete(n)
		}
	}()
	ms := []*Bot{mk("zz_a", 55, true, true), mk("zz_b", 1, false, false), mk("zz_c", 1, true, false)}
	if minLv, _, known, unknown := partyFitLv(ms); minLv != 55 || known != 1 || unknown != 1 {
		t.Errorf("partyFitLv = lv%d known %d unknown %d, want lv55 1 1", minLv, known, unknown)
	}
	if !partyMapOK(ms, "zz_fild08") {
		t.Error("a lv52 map suits a lv55 party; offline / logging-in lv1 defaults don't count")
	}
	if !partyMapTooEasy(ms, "zz_fild07") || partyMapOK(ms, "zz_fild07") {
		t.Error("lv30 monsters are too easy for a lv55 weakest member")
	}
}

// TestLeaderMapMemory: two party deaths on a map keep that LEADER's parties off it, also after the party is
// disbanded and re-formed (QA guild07: re-formed ~6 times onto gef_fild10 / gef_fild03 / moc_fild02-03).
func TestLeaderMapMemory(t *testing.T) {
	spawnsByMap["zz_fild06"] = []Spawn{{Map: "zz_fild06", MobID: 990121, Count: 100}}
	mobs[990121] = &Mob{Level: 52}
	defer func() {
		delete(spawnsByMap, "zz_fild06")
		delete(mobs, 990121)
		leaderMem.Lock()
		delete(leaderMem.m, "zz_lead")
		leaderMem.Unlock()
		pubs.Delete("zz_lead")
	}()
	lead := &Bot{P: &Persona{Name: "zz_lead", BaseLevel: 1}}
	v := &atomic.Value{}
	v.Store(pubState{Name: "zz_lead", Lv: 55, Power: 55, Online: true, Known: true})
	pubs.Store("zz_lead", v)
	ms := []*Bot{lead}
	if !partyMapOK(ms, "zz_fild06") {
		t.Fatal("setup: a lv52 map suits lv55")
	}
	leaderNoteDeath("zz_lead", "zz_fild06")
	if !partyMapOK(ms, "zz_fild06") {
		t.Error("one death is not a ban")
	}
	leaderNoteDeath("zz_lead", "zz_fild06")
	if partyMapOK(ms, "zz_fild06") {
		t.Error("two party deaths: the leader's parties keep off the map")
	}
	p := &Party{Leader: lead, Members: ms} // a freshly re-formed party: no Party.Left of its own
	if !p.partyLeftRecently("zz_fild06", time.Now()) {
		t.Error("the ban must survive re-forming the party")
	}
}

// TestPartyMapAggro: aggressive maps need a stronger weakest member (QA 12:26-13:13: gef_fild03, 92% aggressive Orcs,
// mean lv41: 32 deaths among 3 bots of "lv 45 vs 44").
func TestPartyMapAggro(t *testing.T) {
	spawnsByMap["zz_fild05"] = []Spawn{{Map: "zz_fild05", MobID: 990131, Count: 92}, {Map: "zz_fild05", MobID: 990132, Count: 8}}
	mobs[990131], mobs[990132] = &Mob{Level: 41, Aggressive: true}, &Mob{Level: 41}
	defer func() { delete(spawnsByMap, "zz_fild05"); delete(mobs, 990131); delete(mobs, 990132) }()
	if !partyMapTooHard("zz_fild05", 45) {
		t.Error("92% aggressive lv41 map: too hard for a weakest power of 45")
	}
	if partyMapTooHard("zz_fild05", 56) {
		t.Error("a weakest power of 56 farms it")
	}
}

// TestPartyMapDeadlyAndFragile: a map with 5+ deaths in 30 min (1 per 3 bots) is skipped; a mage counts 6 levels weaker.
func TestPartyMapDeadlyAndFragile(t *testing.T) {
	for i := 0; i < 5; i++ {
		noteDeathAt("zz_fild04")
	}
	defer func() {
		deathTimes.Lock()
		delete(deathTimes.m, "zz_fild04")
		deathTimes.Unlock()
	}()
	if !partyMapDeadly("zz_fild04") || partyMapDeadly("zz_fild03") {
		t.Error("5 deaths in 30 min on an empty map: deadly; no deaths: not")
	}
	v := &atomic.Value{}
	v.Store(pubState{Name: "zz_mage", Lv: 40, Power: 40, Online: true, Known: true, Fragile: true})
	pubs.Store("zz_mage", v)
	defer pubs.Delete("zz_mage")
	if minLv, _, _, _ := partyFitLv([]*Bot{{P: &Persona{Name: "zz_mage"}}}); minLv != 40-partyFragileLv {
		t.Errorf("a lv40 mage rates lv%d, want %d", minLv, 40-partyFragileLv)
	}
}

// TestPartyTrainMap: 100+ aggressive spawns gather trains - the weakest member needs 14 levels over them (QA mjolnir_02:
// 46 deaths in 38 min at effective levels 39-51 over lv34-38 monsters, 10-31 of them at once).
func TestPartyTrainMap(t *testing.T) {
	spawnsByMap["zz_fild02"] = []Spawn{{Map: "zz_fild02", MobID: 990141, Count: 180}, {Map: "zz_fild02", MobID: 990142, Count: 90}}
	mobs[990141], mobs[990142] = &Mob{Level: 38, Aggressive: true}, &Mob{Level: 20}
	defer func() { delete(spawnsByMap, "zz_fild02"); delete(mobs, 990141); delete(mobs, 990142) }()
	if !partyMapTooHard("zz_fild02", 45) {
		t.Error("a train map with lv38 aggressive monsters is too hard for a weakest power of 45")
	}
	if partyMapTooHard("zz_fild02", 52) {
		t.Error("power 52 handles it")
	}
}

// TestAssistPackTrain: an assist pack (Wolves, AI 03) counts toward the train rule (QA moc_fild03: 164 Wolves, 25
// deaths in 30 min at effective levels 43-48).
func TestAssistPackTrain(t *testing.T) {
	spawnsByMap["zz_fild01"] = []Spawn{{Map: "zz_fild01", MobID: 990151, Count: 164}, {Map: "zz_fild01", MobID: 990152, Count: 23}, {Map: "zz_fild01", MobID: 990153, Count: 39}}
	mobs[990151], mobs[990152], mobs[990153] = &Mob{Level: 45, Assist: true}, &Mob{Level: 48, Aggressive: true}, &Mob{Level: 30}
	defer func() {
		delete(spawnsByMap, "zz_fild01")
		delete(mobs, 990151)
		delete(mobs, 990152)
		delete(mobs, 990153)
	}()
	if !partyMapTooHard("zz_fild01", 48) {
		t.Error("a Wolf-pack map is too hard for power 48")
	}
	if partyMapTooHard("zz_fild01", 63) {
		t.Error("power 63 handles it")
	}
}
