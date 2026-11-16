package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Fixture economies only: no live data (G23). Tests that write a file use t.TempDir().

var meNow = time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)

// meMaps: n maps named prefix1..prefixN, each with yield y per farmer-hour and mob level lv.
func meMaps(prefix string, n int, y float64, lv int) []MEMap {
	var out []MEMap
	for i := 1; i <= n; i++ {
		out = append(out, MEMap{Map: fmt.Sprintf("%s%d", prefix, i), Mob: prefix + " mob", MobLv: lv, Y: y})
	}
	return out
}

// meFarmers: n farmers f00.. at level lv.
func meFarmers(n, lv int) []MEFarmer {
	var out []MEFarmer
	for i := 0; i < n; i++ {
		out = append(out, MEFarmer{Name: fmt.Sprintf("f%02d", i), Lv: lv})
	}
	return out
}

func meCount(orders []MEOrder, item int) int {
	n := 0
	for _, o := range orders {
		if o.ItemID == item && !o.Held {
			n++
		}
	}
	return n
}

func TestMENeedFormula(t *testing.T) {
	it := MEItem{ID: 1, Demand: 20, GuildNeed: 3, BuildShort: 4, Listed: 5, Chasers: 2}
	if got := meNeed(it); got != 16 { // 20 + 3 + 4 - 5 - 3*2
		t.Fatalf("need = %d, want 16", got)
	}
	it.Listed = 100
	if got := meNeed(it); got != 0 {
		t.Fatalf("need with a big listing = %d, want 0 (clamped)", got)
	}
}

func TestMECoveredDropsOut(t *testing.T) {
	item := MEItem{ID: 10, Name: "Hunting Bow", Price: 500, Demand: 4, Maps: meMaps("m", 2, 1, 20)} // y*H = 2 per farmer
	in := MESnapshot{Now: meNow, Items: []MEItem{item}, Farmers: meFarmers(4, 30)}
	if gaps := meNeeds(in); len(gaps) != 1 || gaps[0].Remain != 4 {
		t.Fatalf("no holds: gaps %+v, want one gap with remain 4", gaps)
	}
	in.Farmers[0].Held = &MEHold{ItemID: 10, Map: "m1"}
	in.Farmers[1].Held = &MEHold{ItemID: 10, Map: "m2"}
	if gaps := meNeeds(in); len(gaps) != 0 {
		t.Fatalf("two holds of 2 units cover need 4: gaps %+v, want none", gaps)
	}
}

func TestMESkipsGlutBossNPCAndJunk(t *testing.T) {
	base := func(id int) MEItem {
		return MEItem{ID: id, Name: fmt.Sprintf("i%d", id), Price: 300, Demand: 10, Maps: meMaps("m", 2, 0.5, 20)}
	}
	glut := base(1)
	glut.Glut = true
	boss := base(2)
	boss.BossOnly = true
	npc := base(3)
	npc.NPCSold = true
	healNPC := base(4)
	healNPC.NPCSold, healNPC.Heal = true, true
	noMap := base(5)
	noMap.Maps = nil
	cheap := base(6)
	cheap.Price = 5
	in := MESnapshot{Now: meNow, Items: []MEItem{glut, boss, npc, healNPC, noMap, cheap}, Farmers: meFarmers(4, 30)}
	gaps := meNeeds(in)
	if len(gaps) != 1 || gaps[0].ItemID != 4 {
		t.Fatalf("gaps %+v: only the healing item an NPC sells stays (heals are the exception)", gaps)
	}
}

func TestMEProportionalToNeed(t *testing.T) {
	// Three items, need 12 / 6 / 3, each yields 1 unit per farmer-hour over three maps. Covered items drop out, so each
	// item gets exactly its need: the spread follows the need.
	items := []MEItem{
		{ID: 1, Name: "A", Price: 100, Demand: 12, Maps: meMaps("a", 3, 0.5, 10)},
		{ID: 2, Name: "B", Price: 100, Demand: 6, Maps: meMaps("b", 3, 0.5, 10)},
		{ID: 3, Name: "C", Price: 100, Demand: 3, Maps: meMaps("c", 3, 0.5, 10)},
	}
	in := MESnapshot{Now: meNow, Items: items, Farmers: meFarmers(60, 20)}
	res := meEvaluate(in)
	want := map[int]int{1: 12, 2: 6, 3: 3}
	for id, n := range want {
		if got := meCount(res.Allocation.Orders, id); got != n {
			t.Errorf("item %d got %d farmers, want %d (need)", id, got, n)
		}
	}
	if len(res.Allocation.Orders) != 21 {
		t.Errorf("orders %d, want 21", len(res.Allocation.Orders))
	}
}

func TestMEItemCap(t *testing.T) {
	// 10 farmers: per item min(12, ceil(30%)) = 3, even with a need of 100.
	in := MESnapshot{Now: meNow, Items: []MEItem{{ID: 1, Name: "A", Price: 100, Demand: 100, Maps: meMaps("a", 2, 0.5, 10)}},
		Farmers: meFarmers(10, 20)}
	res := meEvaluate(in)
	if got := meCount(res.Allocation.Orders, 1); got != 3 {
		t.Fatalf("farmers on one item = %d, want 3 (30%% of 10)", got)
	}
	if res.Allocation.Caps.PerItem != 3 {
		t.Fatalf("per-item cap = %d, want 3", res.Allocation.Caps.PerItem)
	}
}

func TestMEMapCap(t *testing.T) {
	// 20 farmers: per item min(12, 6) = 6, but the only map takes 5.
	in := MESnapshot{Now: meNow, Items: []MEItem{{ID: 1, Name: "A", Price: 100, Demand: 100, Maps: meMaps("a", 1, 0.5, 10)}},
		Farmers: meFarmers(20, 20)}
	res := meEvaluate(in)
	if got := res.Allocation.Dist.ByMap["a1"]; got != 5 {
		t.Fatalf("farmers on one map = %d, want 5", got)
	}
}

func TestMEOrderedCap(t *testing.T) {
	// 10 farmers: at most 70% (7) hold an order; 5 items each want 50 with plenty of maps.
	var items []MEItem
	for i := 1; i <= 5; i++ {
		items = append(items, MEItem{ID: i, Name: fmt.Sprintf("i%d", i), Price: 100, Demand: 50, Maps: meMaps(fmt.Sprintf("m%d_", i), 3, 0.5, 10)})
	}
	res := meEvaluate(MESnapshot{Now: meNow, Items: items, Farmers: meFarmers(10, 20)})
	if n := len(res.Allocation.Orders); n != 7 {
		t.Fatalf("orders = %d, want 7 (70%% of 10)", n)
	}
	if n := len(res.Allocation.Unassigned); n != 3 {
		t.Fatalf("unassigned = %d, want 3", n)
	}
}

func TestMERotationMovesFirstPick(t *testing.T) {
	// One item, one farmer allowed on it (30% of 3 rounds up to 1). The first pick follows the hour.
	in := MESnapshot{Now: meNow, Items: []MEItem{{ID: 1, Name: "A", Price: 100, Demand: 1, Maps: meMaps("a", 1, 1, 10)}},
		Farmers: meFarmers(3, 20)}
	winner := func(now time.Time) string {
		in.Now = now
		for _, o := range meEvaluate(in).Allocation.Orders {
			if !o.Held {
				return o.Farmer
			}
		}
		return ""
	}
	a, b := winner(meNow), winner(meNow.Add(time.Hour))
	if a == "" || b == "" || a == b {
		t.Fatalf("winners %q and %q: the first pick must move from hour to hour", a, b)
	}
}

func TestMEMinLevelNoOrder(t *testing.T) {
	in := MESnapshot{Now: meNow, Items: []MEItem{{ID: 1, Name: "A", Price: 100, Demand: 5, Maps: meMaps("a", 1, 1, 10)}},
		Farmers: []MEFarmer{{Name: "low", Lv: 10}, {Name: "ok", Lv: 40}}}
	res := meEvaluate(in)
	if len(res.Allocation.Orders) != 1 || res.Allocation.Orders[0].Farmer != "ok" {
		t.Fatalf("orders %+v, want only the level 40 farmer", res.Allocation.Orders)
	}
	if len(res.Allocation.Unassigned) != 1 || res.Allocation.Unassigned[0] != "low" {
		t.Fatalf("unassigned %v, want [low]", res.Allocation.Unassigned)
	}
}

func TestMERecentlyOrderedNeedsDoubleValue(t *testing.T) {
	// Two farmers (f00 is the one with the recent order); per item cap 1, so f01 takes whatever f00 leaves.
	mk := func(xPrice int) MESnapshot {
		farmers := meFarmers(2, 30)
		farmers[0].Recent = map[int]time.Time{1: meNow.Add(-time.Hour)} // item 1 ordered an hour ago
		return MESnapshot{Now: meNow, Farmers: farmers, Items: []MEItem{
			{ID: 1, Name: "X", Price: xPrice, Demand: 50, Maps: meMaps("x", 1, 0.5, 10)},
			{ID: 2, Name: "Y", Price: 100, Demand: 50, Maps: meMaps("y", 1, 0.5, 10)},
		}}
	}
	pickOf := func(orders []MEOrder, farmer string) int {
		for _, o := range orders {
			if o.Farmer == farmer {
				return o.ItemID
			}
		}
		return -1
	}
	if got := pickOf(meEvaluate(mk(150)).Allocation.Orders, "f00"); got != 2 {
		t.Fatalf("X at 150 vs 100 (under 2x): f00 got item %d, want 2", got)
	}
	if got := pickOf(meEvaluate(mk(250)).Allocation.Orders, "f00"); got != 1 {
		t.Fatalf("X at 250 vs 100 (2x or more): f00 got item %d, want 1", got)
	}
}

func TestMEHeldOrderCounts(t *testing.T) {
	// Two farmers: per item cap 1. The first holds item 1; the second must not get the same item.
	farmers := meFarmers(2, 30)
	farmers[0].Held = &MEHold{ItemID: 1, Map: "a1"}
	in := MESnapshot{Now: meNow, Farmers: farmers, Items: []MEItem{{ID: 1, Name: "A", Price: 100, Demand: 10, Maps: meMaps("a", 2, 0.5, 10)}}}
	res := meEvaluate(in)
	if res.Needs == nil || len(res.Needs) != 1 || res.Needs[0].FarmersOn != 1 {
		t.Fatalf("needs %+v: the held order is counted", res.Needs)
	}
	for _, o := range res.Allocation.Orders {
		if !o.Held && o.ItemID == 1 {
			t.Fatalf("second farmer got item 1 past the per-item cap: %+v", o)
		}
	}
	if len(res.Allocation.Unassigned) != 1 {
		t.Fatalf("unassigned %v, want the second farmer", res.Allocation.Unassigned)
	}
}

func TestMEOffByDefault(t *testing.T) {
	if defaultTuning.MarketEngineOn || defaultTuning.GuildMarketFeedOn {
		t.Fatal("market_engine_on and guild_market_feed_on must default to false")
	}
	meState.Lock()
	meState.result, meState.feed, meState.runs = nil, nil, 0
	meState.Unlock()
	marketEngineRound(nil, meNow, true) // flag off: must compute nothing
	meState.Lock()
	computed := meState.runs
	meState.Unlock()
	if computed != 0 {
		t.Fatalf("engine ran %d times with the flag off", computed)
	}
	mux := http.NewServeMux()
	registerMarketEngine(mux)
	for _, path := range []string{"/api/market/engine", "/api/market/feed?guild=guild03"} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		var got map[string]interface{}
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || got["status"] != "off" {
			t.Errorf("%s with the flag off: %s", path, rec.Body.String())
		}
	}
}

func TestMEEndpointsOn(t *testing.T) {
	prev := tune()
	on := defaultTuning
	on.MarketEngineOn = true
	curTuning.Store(&on)
	defer curTuning.Store(prev)
	res := meEvaluate(MESnapshot{Now: meNow, Items: []MEItem{{ID: 1, Name: "A", Price: 100, Demand: 4, Maps: meMaps("a", 1, 1, 10)}},
		Farmers: meFarmers(4, 30)})
	feed := meBuildFeed(res, MESnapshot{Now: meNow, Items: []MEItem{{ID: 1, Name: "A", Price: 100, Demand: 4, Maps: meMaps("a", 1, 1, 10)}}},
		MEFeedServer{Online: 4}, map[string]MEFeedGuild{"guild03": {ID: "guild03", Needs: []MEFeedGuildNeed{{Item: "A", N: 2}}}}, meFeedMissingWP2)
	meState.Lock()
	meState.result, meState.feed, meState.runs = &res, &feed, 1
	meState.Unlock()
	defer func() {
		meState.Lock()
		meState.result, meState.feed, meState.runs = nil, nil, 0
		meState.Unlock()
	}()
	mux := http.NewServeMux()
	registerMarketEngine(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/market/engine", nil))
	var eng map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &eng)
	if eng["status"] != "ok" {
		t.Fatalf("engine endpoint status %v", eng["status"])
	}
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/market/feed?guild=guild03", nil))
	var fd struct {
		Status string       `json:"status"`
		Guild  *MEFeedGuild `json:"guild"`
		Guilds interface{}  `json:"guilds"`
	}
	json.Unmarshal(rec.Body.Bytes(), &fd)
	if fd.Status != "partial" || fd.Guild == nil || fd.Guild.ID != "guild03" || len(fd.Guild.Needs) != 1 || fd.Guilds != nil {
		t.Fatalf("feed guild03: %s", rec.Body.String())
	}
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/market/feed?guild=guild09", nil))
	if !strings.Contains(rec.Body.String(), `"id":"guild09"`) {
		t.Fatalf("unknown guild should get an empty block: %s", rec.Body.String())
	}
}

func TestMEFeedSchema(t *testing.T) {
	in := MESnapshot{Now: meNow, Items: []MEItem{{ID: 7, Name: "Red Potion", Price: 40, Demand: 30, Heal: true, Band: "18-32", Maps: meMaps("p", 2, 1, 10)}},
		Farmers: meFarmers(2, 30)}
	res := meEvaluate(in)
	feed := meBuildFeed(res, in, MEFeedServer{Bots: 10, Online: 5, Bands: []MEBand{{Band: "18-32", Bots: 3}}}, nil, meFeedMissingWP2)
	raw, err := json.Marshal(feed)
	if err != nil {
		t.Fatal(err)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"t", "status", "missing", "server", "short", "glut", "zeny_per_hour"} {
		if _, ok := top[k]; !ok {
			t.Errorf("feed lacks %q", k)
		}
	}
	var short []map[string]interface{}
	json.Unmarshal(top["short"], &short)
	if len(short) != 1 {
		t.Fatalf("short = %d rows, want 1", len(short))
	}
	for _, k := range []string{"item_id", "item", "kind", "band", "demand", "listed", "price", "drop_maps", "farmers_on"} {
		if _, ok := short[0][k]; !ok {
			t.Errorf("short row lacks %q", k)
		}
	}
	for _, banned := range []string{"set_by", "should", "direction", "algorithm", "engine"} {
		if strings.Contains(strings.ToLower(string(raw)), banned) {
			t.Errorf("feed contains %q: it must carry data only (EC43)", banned)
		}
	}
	if feed.Status != "partial" {
		t.Errorf("status %q, want partial (WP2 names what it cannot read)", feed.Status)
	}
}

func TestMECreamyNeedIsCapped(t *testing.T) {
	// The Creamy Card's bonus (+50) must not keep it first forever: the need stops at meCreamyCap.
	it := MEItem{ID: itemCreamyCard, Name: "Creamy Card", Price: 90000, Demand: 60 + meCreamyBonus, Cap: meCreamyCap, Maps: meMaps("c", 1, 0.01, 20)}
	if got := meNeed(it); got != meCreamyCap {
		t.Fatalf("creamy need = %d, want the cap %d", got, meCreamyCap)
	}
	gaps := meNeeds(MESnapshot{Now: meNow, Items: []MEItem{it}, Farmers: meFarmers(4, 40)})
	if len(gaps) != 1 || gaps[0].Need != meCreamyCap {
		t.Fatalf("gaps %+v, want one creamy gap with need %d", gaps, meCreamyCap)
	}
}

func TestMEFastMobIsPenalised(t *testing.T) {
	// Same yield and price; the fast melee map (value / meFastFit) loses to the slow one.
	item := MEItem{ID: 1, Name: "A", Price: 100, Demand: 1, Maps: []MEMap{
		{Map: "fast1", Mob: "fast mob", MobLv: 10, Y: 1, Fast: true},
		{Map: "slow1", Mob: "slow mob", MobLv: 10, Y: 1},
	}}
	res := meEvaluate(MESnapshot{Now: meNow, Items: []MEItem{item}, Farmers: meFarmers(2, 30)}) // 70% of 2 allows one order
	if len(res.Allocation.Orders) != 1 || res.Allocation.Orders[0].Map != "slow1" {
		t.Fatalf("orders %+v, want the slow map", res.Allocation.Orders)
	}
}

func TestMEFeedAtomicWrite(t *testing.T) {
	dir := t.TempDir()
	feed := MEFeed{T: meNow, Status: "ok", Short: []MEFeedItem{}, Glut: []MEFeedGlut{}, ZenyPerHour: []MEFeedZeny{}}
	if err := writeMarketFeed(dir, feed); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "market_feed.json"))
	if err != nil {
		t.Fatal(err)
	}
	var back MEFeed
	if err := json.Unmarshal(raw, &back); err != nil || back.Status != "ok" {
		t.Fatalf("feed file does not round-trip: %v %s", err, raw)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("temp files left behind: %d entries", len(entries))
	}
}
