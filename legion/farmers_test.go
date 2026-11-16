package main

import (
	"encoding/json"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// withFarmerTest: farmer flags as given, a throwaway data dir (G23), clean farmer state and clean metrics. Everything
// is restored. accounts (if given) become data/farmers.json in that dir.
func withFarmerTest(t *testing.T, roster, orders bool, accounts ...string) string {
	t.Helper()
	prev := tune()
	on := defaultTuning
	on.FarmerRosterOn, on.FarmerOrdersOn = roster, orders
	curTuning.Store(&on)
	oldData := *dataDir
	dir := t.TempDir()
	*dataDir = dir
	if accounts != nil {
		raw, _ := json.Marshal(map[string]interface{}{"schema": 1, "accounts": accounts})
		if err := os.WriteFile(filepath.Join(dir, "farmers.json"), raw, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	reset := func() {
		farmerMu.Lock()
		farmerLive, farmerNextAt = map[string]*farmerOrder{}, map[string]time.Time{}
		farmerItemAt, farmerParcel = map[string]map[int]time.Time{}, map[string]bool{}
		farmerSupply = map[string][]farmerSupplyRec{}
		farmerLooted, farmerDeaths = map[string]int64{}, map[string]int64{}
		farmerSince, farmerLoaded, farmerSavedAt, farmerDirty = time.Time{}, false, time.Time{}, false
		farmerRosterAt, farmerRosterSt = time.Time{}, nil
		farmerMu.Unlock()
		farmerSessions.Range(func(k, _ interface{}) bool { farmerSessions.Delete(k); return true })
	}
	reset()
	resetMM()
	meState.Lock()
	meState.result, meState.last = nil, time.Time{}
	meState.Unlock()
	t.Cleanup(func() {
		curTuning.Store(prev)
		*dataDir = oldData
		reset()
		resetMM()
		meState.Lock()
		meState.result = nil
		meState.Unlock()
		opps.Lock()
		opps.goals = map[*Bot]*moneyGoal{}
		opps.Unlock()
	})
	return dir
}

// farmerBot: a farmer account with a hunter and a merchant character.
func farmerBot(lv int, greed float64, seed int64) *Bot {
	return &Bot{P: &Persona{Name: "Farmer One", Account: "lg0001", BaseLevel: lv, Greed: greed,
		Chars: []CharSpec{{Name: "Hunter1", Role: "main"}, {Name: "Merch1", Role: "vendor"}}},
		rng: rand.New(rand.NewSource(seed))}
}

// setEngineOffer: the engine's last allocation holds one order for the bot's hunter.
func setEngineOffer(now time.Time, rank ...MERank) {
	meState.Lock()
	meState.result = &MEResult{Allocation: MEAllocation{Orders: []MEOrder{
		{Farmer: "Hunter1", ItemID: rank[0].ItemID, Item: rank[0].Item, Map: rank[0].Map, Mob: rank[0].Mob, Rank: rank}}}}
	meState.last = now
	meState.Unlock()
}

// setEngineNeeds: the engine's gaps (the bands it listed the items for).
func setEngineNeeds(gaps ...MEGap) {
	meState.Lock()
	meState.result.Needs = gaps
	meState.Unlock()
}

// fakeFarmTable: one mob that drops a card at 20 percent on a field with spawns, known to the engine's tables. Its
// keys are removed again at cleanup. Real tables are loaded first, so nothing real is replaced.
func fakeFarmTable(t *testing.T) {
	t.Helper()
	lookupOnce.Do(loadLookup)
	const card, mob, field = 9100, 9001, "fake_fild01"
	items[card] = &Item{ID: card, Name: "Fake Card", Type: "Card"}
	mobs[mob] = &Mob{ID: mob, Name: "FakeMob", Level: 10, HP: 500} // a bot with no gear fights at its effective level (15)
	mobExtras[mob] = &mobExtra{Drops: []mobDrop{{Item: card, Rate: 2000}}}
	droppedBy[card] = []int{mob}
	mobMaps[mob] = []Spawn{{Map: field, MobID: mob, Count: 10}}
	if maps == nil {
		maps = map[string]*GameMap{}
	}
	maps[field] = &GameMap{Name: field}
	t.Cleanup(func() {
		delete(items, card)
		delete(mobs, mob)
		delete(mobExtras, mob)
		delete(droppedBy, card)
		delete(mobMaps, mob)
		delete(maps, field)
	})
}

// mmFarmer: the farmer block of the metrics for one integer key.
func mmFarmer(t *testing.T, now time.Time, key string) int64 {
	t.Helper()
	fm, ok := marketMetricsBlock(now)["farmers"].(map[string]interface{})
	if !ok {
		t.Fatal("metrics block has no farmers entry")
	}
	switch v := fm[key].(type) {
	case int64:
		return v
	case int:
		return int64(v)
	}
	t.Fatalf("farmers.%s has type %T", key, fm[key])
	return 0
}

// mmDeclined: the total declines in the metrics block (its "declined" entry is a map by reason).
func mmDeclined(t *testing.T, now time.Time) int64 {
	t.Helper()
	fm, ok := marketMetricsBlock(now)["farmers"].(map[string]interface{})
	if !ok {
		t.Fatal("metrics block has no farmers entry")
	}
	var n int64
	for _, v := range fm["declined"].(map[string]int64) {
		n += v
	}
	return n
}

// mmSupplied: the supplied units of one item in one band, as the metrics hold them.
func mmSupplied(band string, item int) int64 {
	mm.Lock()
	defer mm.Unlock()
	mmEnsureLocked()
	return mm.supplied[band][item]
}

func TestFarmerFirstFitOrder(t *testing.T) {
	rank := []MERank{{ItemID: 1, Item: "A"}, {ItemID: 2, Item: "B"}, {ItemID: 3, Item: "C"}}
	fit := func(id int) ([]string, string) {
		if id == 3 {
			return []string{"safe_c", "other"}, "mob3"
		}
		if id == 2 {
			return []string{"unsafe_b"}, "mob2"
		}
		return nil, ""
	}
	safe := func(m string) bool { return m != "unsafe_b" }
	none := func(int) bool { return false }
	pk, reason := farmerFirstFit(rank, none, fit, safe)
	if reason != "" || pk.Item != 3 || pk.Maps[0] != "safe_c" || pk.Mob != "mob3" {
		t.Fatalf("first safe item: got %+v reason %q, want item 3 on safe_c", pk, reason)
	}
	pk, reason = farmerFirstFit(rank, func(id int) bool { return id == 1 }, fit, safe)
	if reason != "" || pk.Item != 3 {
		t.Fatalf("cooldown skips item 1: got %+v reason %q", pk, reason)
	}
	pk, reason = farmerFirstFit(rank[:2], none, fit, safe)
	if reason != "unsafe map" || pk.Item != 0 {
		t.Fatalf("nothing safe: got %+v reason %q, want unsafe map", pk, reason)
	}
	_, reason = farmerFirstFit(rank[:1], func(int) bool { return true }, fit, safe)
	if reason != "cooldown" {
		t.Fatalf("cooldown: reason %q", reason)
	}
}

func TestFarmerAcceptAndTiming(t *testing.T) {
	if farmerAcceptP(0) != 0.5 || farmerAcceptP(1) != 1 || farmerAcceptP(2) != 1 || farmerAcceptP(-1) != 0.5 {
		t.Fatalf("acceptance: %v %v %v", farmerAcceptP(0), farmerAcceptP(1), farmerAcceptP(2))
	}
	s, l := farmerTiming(func() float64 { return 0 })
	if s != 0 || l != 90*time.Minute {
		t.Fatalf("earliest timing: start %v life %v", s, l)
	}
	s, l = farmerTiming(func() float64 { return 0.999999 })
	if s < 14*time.Minute || s > farmerStartMax || l < 149*time.Minute || l > 150*time.Minute {
		t.Fatalf("latest timing: start %v life %v", s, l)
	}
}

func TestFarmerFlagsOffNothingChanges(t *testing.T) {
	dir := withFarmerTest(t, false, false, "lg0001")
	b := farmerBot(30, 1, 1)
	now := time.Now()
	setEngineOffer(now, MERank{ItemID: 4040, Item: "Creamy Card", Map: "prt_fild02", Mob: "Creamy"})
	if farmerOn(b, now) {
		t.Fatal("a roster account is no farmer with the flags off")
	}
	if fg := b.farmerGoal(now); fg != nil {
		t.Fatalf("flags off: farmerGoal returned %+v", fg)
	}
	if c := farmerCharPick(b); c != nil {
		t.Fatalf("flags off: pickChar rule returned %s", c.Name)
	}
	noteFarmerLoot(b, 4040, 2)
	noteFarmerSale(b, 4040, 2)
	noteFarmerDeath(b, now)
	noteFarmerSessionStart(b, now)
	noteFarmerSessionEnd(b, now.Add(time.Hour))
	if _, err := os.Stat(filepath.Join(dir, farmerOrdersName)); err == nil {
		t.Fatal("flags off: the orders file was written")
	}
	// roster on, orders off: still no decisions
	curTuning.Store(&Tuning{FarmerRosterOn: true})
	if fg := b.farmerGoal(now); fg != nil {
		t.Fatal("roster on, orders off: farmers must not take orders")
	}
	if mmFarmer(t, now, "offers") != 0 || mmFarmer(t, now, "accepted") != 0 || mmFarmer(t, now, "deaths") != 0 {
		t.Fatal("flags off: hooks must not count")
	}
	if len(farmerLooted)+len(farmerDeaths) != 0 {
		t.Fatal("flags off: loot and death hooks must not count")
	}
}

func TestFarmerRosterFile(t *testing.T) {
	withFarmerTest(t, true, true) // no farmers.json: tolerated, no farmers
	b := farmerBot(30, 1, 1)
	now := time.Now()
	if farmerOn(b, now) || farmerRoster(now) != nil {
		t.Fatal("a missing farmers.json must mean no farmers")
	}
	withFarmerTest(t, true, true, "LG0001") // account names compare without case
	b = farmerBot(30, 1, 1)
	if !farmerOn(b, time.Now()) {
		t.Fatal("a roster account with orders on must be a farmer")
	}
	other := farmerBot(30, 1, 1)
	other.P.Account = "lg0002"
	if farmerOn(other, time.Now()) {
		t.Fatal("an account not on the roster is no farmer")
	}
}

func TestFarmerDeclines(t *testing.T) {
	now := time.Now()
	withFarmerTest(t, true, true, "lg0001")
	creamy := MERank{ItemID: 4040, Item: "Creamy Card", Map: "prt_fild02", Mob: "Creamy"}

	// level: below the minimum base level (meMinLv)
	b := farmerBot(meMinLv-1, 1, 1)
	setEngineOffer(now, creamy)
	if fg := b.farmerGoal(now); fg != nil {
		t.Fatal("a level-14 farmer must not take an order")
	}
	if mmDeclined(t, now) != 1 || mmFarmer(t, now, "offers") != 1 {
		t.Fatalf("level decline not counted in the metrics: offers %d declined %d",
			mmFarmer(t, now, "offers"), mmDeclined(t, now))
	}
	// the decision gap: a second look within an hour is not a new decision
	b.farmerGoal(now.Add(10 * time.Minute))
	if mmFarmer(t, now, "offers") != 1 {
		t.Fatal("a farmer decides at most once an hour")
	}

	// unsafe: no drop map passes soloMapOK (no tables for it)
	withFarmerTest(t, true, true, "lg0001")
	setEngineOffer(now, creamy)
	b = farmerBot(30, 1, 3) // greed 1: the roll always passes
	if fg := b.farmerGoal(now); fg != nil || farmerMMTotals().Declines["unsafe map"] != 1 {
		t.Fatalf("unsafe map: goal %v declines %+v", fg, farmerMMTotals().Declines)
	}
	if farmerLive["lg0001"] != nil {
		t.Fatal("a declined offer must not leave a live order")
	}
}

// TestFarmerBusyIsNotADecision: a farmer on its own goal neither counts an offer nor a decline, and looks again in
// farmerBusyRetry, not after the hour a decision waits for.
func TestFarmerBusyIsNotADecision(t *testing.T) {
	now := time.Now()
	withFarmerTest(t, true, true, "lg0001")
	setEngineOffer(now, MERank{ItemID: 4040, Item: "Creamy Card", Map: "prt_fild02", Mob: "Creamy"})
	b := farmerBot(30, 1, 2)
	opps.Lock()
	opps.goals[b] = &moneyGoal{ItemID: 909, Until: now.Add(time.Hour)}
	opps.Unlock()
	if fg := b.farmerGoal(now); fg != nil {
		t.Fatal("a busy farmer takes no order now")
	}
	if mmFarmer(t, now, "offers") != 0 || mmDeclined(t, now) != 0 {
		t.Fatalf("busy must not count: offers %d declined %d", mmFarmer(t, now, "offers"), mmDeclined(t, now))
	}
	if got := farmerNextAt["lg0001"].Sub(now); got != farmerBusyRetry {
		t.Fatalf("busy retry in %v, want %v", got, farmerBusyRetry)
	}
	// free again after the retry: the offer is looked at (and counted once)
	opps.Lock()
	delete(opps.goals, b)
	opps.Unlock()
	b.farmerGoal(now.Add(farmerBusyRetry + time.Minute))
	if mmFarmer(t, now, "offers") != 1 {
		t.Fatalf("after the retry the offer is decided: offers %d", mmFarmer(t, now, "offers"))
	}
}

// TestFarmerAcceptPath: with a small fake map table, a farmer accepts, waits its start delay, writes its goal, and
// the order ends after its life. The metrics block counts the same events.
func TestFarmerAcceptPath(t *testing.T) {
	now := time.Now()
	withFarmerTest(t, true, true, "lg0001")
	fakeFarmTable(t)
	offer := MERank{ItemID: 9100, Item: "Fake Card", Map: "fake_fild01", Mob: "FakeMob", YPerH: 1, Value: 100}
	setEngineOffer(now, offer)
	b := farmerBot(30, 1, 5) // greed 1: the roll passes
	b.MapName = "fake_fild01"

	first := b.farmerGoal(now)
	if mmFarmer(t, now, "accepted") != 1 || mmDeclined(t, now) != 0 || mmFarmer(t, now, "offers") != 1 {
		t.Fatalf("metrics after accept: offers %d accepted %d declined %d", mmFarmer(t, now, "offers"),
			mmFarmer(t, now, "accepted"), mmDeclined(t, now))
	}
	o := farmerLive["lg0001"]
	if o == nil || o.Item != 9100 || o.Char != "Hunter1" || o.Maps[0] != "fake_fild01" {
		t.Fatalf("live order: %+v", o)
	}
	start, life := o.Start.Sub(now), o.Until.Sub(o.Start)
	if start < 0 || start > farmerStartMax || life < farmerLifeMin || life > farmerLifeMin+farmerLifeJit {
		t.Fatalf("start %v life %v outside the design bounds", start, life)
	}
	if start == 0 {
		if first == nil {
			t.Fatal("no delay: the goal is written at once")
		}
	} else {
		if first != nil {
			t.Fatal("the goal must wait for the start delay")
		}
		if g := b.farmerGoal(o.Start.Add(-time.Second)); g != nil {
			t.Fatal("goal written before the start")
		}
	}
	// at the start: the goal is written (MoneyFarmGoal's record) with the order's end
	g := b.farmerGoal(o.Start)
	if g == nil || g.Kind != "zeny" || g.ItemID != 9100 || g.Map != "fake_fild01" || !g.Until.Equal(o.Until) {
		t.Fatalf("goal at start: %+v", g)
	}
	opps.Lock()
	mg := opps.goals[b]
	opps.Unlock()
	if mg == nil || mg.ItemID != 9100 || !mg.Until.Equal(o.Until) {
		t.Fatalf("opps goal not written: %+v", mg)
	}
	// the goal is the farmer's own money goal now: MoneyFarmGoal returns it
	if g2 := b.MoneyFarmGoal(o.Start.Add(time.Minute)); g2 == nil || g2.ItemID != 9100 {
		t.Fatalf("MoneyFarmGoal must return the farmer's order: %+v", g2)
	}
	// the order ends after its life
	if g := b.farmerGoal(o.Until.Add(time.Second)); g != nil {
		t.Fatal("the order must end after its life")
	}
	if farmerLive["lg0001"] != nil {
		t.Fatal("an ended order is removed")
	}
}

// TestFarmerMetricsMatchDecisions: the accepted and declined counts in the metrics block equal the decisions made,
// and the orders file stats say the same.
func TestFarmerMetricsMatchDecisions(t *testing.T) {
	now := time.Now()
	dir := withFarmerTest(t, true, true, "lg0001")
	fakeFarmTable(t)
	setEngineOffer(now, MERank{ItemID: 4040, Item: "Creamy Card", Map: "prt_fild02", Mob: "Creamy"}) // level decline
	farmerBot(meMinLv-1, 1, 1).farmerGoal(now)
	setEngineOffer(now.Add(2*time.Hour), MERank{ItemID: 9100, Item: "Fake Card", Map: "fake_fild01", Mob: "FakeMob"})
	b := farmerBot(30, 1, 5)
	b.MapName = "fake_fild01"
	b.farmerGoal(now.Add(2 * time.Hour)) // accept
	if a, d := mmFarmer(t, now, "accepted"), mmDeclined(t, now); a != 1 || d != 1 {
		t.Fatalf("metrics: accepted %d declined %d, want 1 and 1", a, d)
	}
	farmerFlush(now)
	raw, err := os.ReadFile(filepath.Join(dir, farmerOrdersName))
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Stats struct {
			Accepted int64 `json:"accepted"`
			Declined int64 `json:"declined"`
		} `json:"stats"`
	}
	if json.Unmarshal(raw, &f) != nil || f.Stats.Accepted != 1 || f.Stats.Declined != 1 {
		t.Fatalf("orders file stats: %s", raw)
	}
}

// TestFarmerSupplyOnlyOrderItem: a sale counts only for the item of an order in force or one that ended within
// farmerSupplyKeep, and is filed under the band the engine listed the item for.
func TestFarmerSupplyOnlyOrderItem(t *testing.T) {
	now := time.Now()
	withFarmerTest(t, true, true, "lg0001")
	fakeFarmTable(t)
	setEngineOffer(now, MERank{ItemID: 9100, Item: "Fake Card", Map: "fake_fild01", Mob: "FakeMob"})
	setEngineNeeds(MEGap{ItemID: 9100, Band: "18-32"})
	b := farmerBot(30, 1, 5)
	b.MapName = "fake_fild01"
	b.farmerGoal(now) // accepted
	o := farmerLive["lg0001"]
	if o == nil || o.Band != "18-32" {
		t.Fatalf("the order carries the engine's band: %+v", o)
	}
	noteFarmerSale(b, 9100, 3)
	if got := mmSupplied("18-32", 9100); got != 3 {
		t.Fatalf("order item sold: supplied %d in 18-32, want 3", got)
	}
	noteFarmerSale(b, 4040, 5) // spare gear, not the order's item
	if got := mmSupplied("18-32", 4040) + mmSupplied(meBand(30), 4040); got != 0 {
		t.Fatalf("a non-order item must not count: %d", got)
	}
	// the order ended long ago: its item no longer counts
	farmerMu.Lock()
	for i := range farmerSupply["lg0001"] {
		farmerSupply["lg0001"][i].Until = time.Now().Add(-farmerSupplyKeep - time.Hour)
	}
	farmerMu.Unlock()
	noteFarmerSale(b, 9100, 7)
	if got := mmSupplied("18-32", 9100); got != 3 {
		t.Fatalf("after the keep window the sale must not count: supplied %d", got)
	}
}

// TestFarmerHoursFeedMetrics: roster sessions add their online time to the metrics; other sessions add nothing, and
// deaths per farmer-hour are then a number, not null.
func TestFarmerHoursFeedMetrics(t *testing.T) {
	now := time.Now()
	withFarmerTest(t, true, true, "lg0001")
	b := farmerBot(30, 1, 1)
	outsider := farmerBot(30, 1, 1)
	outsider.P.Account = "lg0002"
	noteFarmerSessionStart(b, now)
	noteFarmerSessionStart(outsider, now)
	noteFarmerSessionEnd(b, now.Add(2*time.Hour))
	noteFarmerSessionEnd(outsider, now.Add(3*time.Hour))
	noteFarmerSessionEnd(b, now.Add(5*time.Hour)) // no start left: adds nothing
	fm := marketMetricsBlock(now)["farmers"].(map[string]interface{})
	if h, ok := fm["farmer_hours"].(float64); !ok || h != 2 {
		t.Fatalf("farmer_hours %v, want 2 (roster session only)", fm["farmer_hours"])
	}
	noteFarmerDeath(b, now.Add(2*time.Hour))
	fm = marketMetricsBlock(now)["farmers"].(map[string]interface{})
	if d, ok := fm["deaths_per_hour"].(float64); !ok || d != 0.5 {
		t.Fatalf("deaths_per_hour %v, want 0.5", fm["deaths_per_hour"])
	}
}

// TestFarmerMigrationRenameFailure: the old counters are added once. If the rename cannot happen, nothing is added,
// so the next start cannot add them twice.
func TestFarmerMigrationRenameFailure(t *testing.T) {
	now := time.Now()
	dir := withFarmerTest(t, true, true, "lg0001")
	path := filepath.Join(dir, farmerOldCounts)
	if err := os.WriteFile(path, []byte(`{"accepted":3,"declined":{"level":2},"deaths":{"lg0001":1}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	// a directory where the .migrated file should go makes the rename fail
	if err := os.Mkdir(path+".migrated", 0o755); err != nil {
		t.Fatal(err)
	}
	migrateFarmerCounts()
	if mmFarmer(t, now, "accepted") != 0 || mmDeclined(t, now) != 0 {
		t.Fatal("a failed rename must add nothing")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("the old file must stay when the rename fails")
	}
	os.Remove(path + ".migrated")
	migrateFarmerCounts()
	if mmFarmer(t, now, "accepted") != 3 || mmDeclined(t, now) != 2 || mmFarmer(t, now, "deaths") != 1 {
		t.Fatal("the migration adds the old totals once")
	}
	migrateFarmerCounts() // the file is gone now: nothing more
	if mmFarmer(t, now, "accepted") != 3 {
		t.Fatal("the migration must not run twice")
	}
}

// TestFarmerOrderSurvivesRestart: the accepted order (with its band) and the per-account detail are read back from the
// orders file, and the order's item keeps counting for supply.
func TestFarmerOrderSurvivesRestart(t *testing.T) {
	now := time.Now()
	dir := withFarmerTest(t, true, true, "lg0001")
	fakeFarmTable(t)
	setEngineOffer(now, MERank{ItemID: 9100, Item: "Fake Card", Map: "fake_fild01", Mob: "FakeMob"})
	setEngineNeeds(MEGap{ItemID: 9100, Band: "18-32"})
	b := farmerBot(30, 1, 5)
	b.MapName = "fake_fild01"
	b.farmerGoal(now)
	o := farmerLive["lg0001"]
	if o == nil {
		t.Fatal("no order to save")
	}
	raw, err := os.ReadFile(filepath.Join(dir, farmerOrdersName))
	if err != nil {
		t.Fatalf("orders file not written: %v", err)
	}
	var f struct {
		Orders []struct {
			Farmer, Char, Band string
			ItemID             int `json:"item_id"`
		} `json:"orders"`
	}
	if json.Unmarshal(raw, &f) != nil || len(f.Orders) != 1 || f.Orders[0].ItemID != 9100 || f.Orders[0].Char != "Hunter1" || f.Orders[0].Band != "18-32" {
		t.Fatalf("orders file shape: %s", raw)
	}
	// a restart: memory empty, the file is read back
	farmerMu.Lock()
	farmerLive, farmerSupply, farmerLoaded = map[string]*farmerOrder{}, map[string][]farmerSupplyRec{}, false
	farmerMu.Unlock()
	back := farmerLiveGet("lg0001", now)
	if back == nil || back.Item != 9100 || back.Band != "18-32" || !back.Until.Equal(o.Until) || len(back.Maps) == 0 {
		t.Fatalf("order after restart: %+v", back)
	}
	noteFarmerSale(b, 9100, 2)
	if got := mmSupplied("18-32", 9100); got != 2 {
		t.Fatalf("after a restart the live order's item still counts: %d", got)
	}
}

func TestFarmerNoOfferNoDecision(t *testing.T) {
	now := time.Now()
	withFarmerTest(t, true, true, "lg0001")
	b := farmerBot(30, 1, 1)
	if fg := b.farmerGoal(now); fg != nil {
		t.Fatal("no engine offer: no goal")
	}
	setEngineOffer(now.Add(-2*farmerOfferAge), MERank{ItemID: 4040, Item: "Creamy Card"})
	if fg := b.farmerGoal(now); fg != nil || mmDeclined(t, now) != 0 || mmFarmer(t, now, "offers") != 0 {
		t.Fatal("a stale allocation is not offered")
	}
}

func TestFarmerCharSplit(t *testing.T) {
	withFarmerTest(t, true, true, "lg0001")
	b := farmerBot(30, 1, 11)
	vendors := 0
	const n = 4000
	for i := 0; i < n; i++ {
		if farmerCharPick(b).Name == "Merch1" {
			vendors++
		}
	}
	if share := float64(vendors) / n; share < 0.25 || share > 0.35 {
		t.Fatalf("merchant share %.3f, want about 0.30 (70/30 split)", share)
	}
	// a confirmed send to the farmer's own vendor is a parcel: the next session is the merchant, once
	farmerNoteParcel(b, "Merch1")
	if c := farmerCharPick(b); c == nil || c.Name != "Merch1" {
		t.Fatalf("after a parcel the next session must be the merchant, got %+v", c)
	}
	if farmerParcel["lg0001"] {
		t.Fatal("the forced session must clear the parcel flag")
	}
	// a send to anyone else is not a parcel
	farmerNoteParcel(b, "SomeoneElse")
	if farmerParcel["lg0001"] {
		t.Fatal("a mail to a non-vendor is no parcel")
	}
	// a farmer with no merchant has no split: pickChar keeps its usual choice
	solo := &Bot{P: &Persona{Account: "lg0001", Chars: []CharSpec{{Name: "Hunter1", Role: "main"}}}, rng: rand.New(rand.NewSource(1))}
	if farmerCharPick(solo) != nil {
		t.Fatal("no vendor character: no split")
	}
}

func TestFarmerEngineOffersKeepCaps(t *testing.T) {
	now := time.Now()
	withFarmerTest(t, true, true, "lg0001")
	items := []MEItem{
		{ID: 1, Name: "A", Price: 100, Demand: 40, Maps: meMaps("a", 1, 1, 10)},
		{ID: 2, Name: "B", Price: 90, Demand: 40, Maps: meMaps("b", 1, 1, 10)},
		{ID: 3, Name: "C", Price: 80, Demand: 40, Maps: meMaps("c", 1, 1, 10)},
	}
	in := MESnapshot{Now: now, Items: items, Farmers: meFarmers(12, 30)}
	res := meEvaluate(in)
	per := map[int]int{}
	meState.Lock()
	meState.result, meState.last = &res, now
	meState.Unlock()
	for _, f := range in.Farmers {
		b := &Bot{P: &Persona{Account: "lg0001", Chars: []CharSpec{{Name: f.Name, Role: "main"}}}}
		off, ok := farmerEngineOffer(b, now)
		if !ok {
			continue
		}
		per[off.ItemID]++
		if len(off.Rank) == 0 || len(off.Rank) > meRankLen {
			t.Fatalf("offer rank length %d, want 1..%d", len(off.Rank), meRankLen)
		}
	}
	for id, n := range per {
		if n > res.Allocation.Caps.PerItem {
			t.Fatalf("item %d offered to %d farmers, cap %d", id, n, res.Allocation.Caps.PerItem)
		}
	}
	if len(per) == 0 {
		t.Fatal("the engine offered nothing to the roster farmers")
	}
}

// TestFarmerHooksAndPersist: loot and deaths are kept in the orders file and come back after a restart.
func TestFarmerHooksAndPersist(t *testing.T) {
	now := time.Now()
	dir := withFarmerTest(t, true, true, "lg0001")
	b := farmerBot(30, 1, 1)
	noteMMFarmerDecline("level")
	noteFarmerLoot(b, 9100, 4)
	noteFarmerDeath(b, now)
	if mmFarmer(t, now, "deaths") != 1 || mmDeclined(t, now) != 1 {
		t.Fatal("metrics: deaths and declines")
	}
	farmerFlush(now)
	if _, err := os.Stat(filepath.Join(dir, farmerOrdersName)); err != nil {
		t.Fatalf("orders file not written: %v", err)
	}
	// restart: the loot and per-account deaths come back from the file
	farmerMu.Lock()
	farmerLooted, farmerDeaths, farmerLoaded = map[string]int64{}, map[string]int64{}, false
	farmerMu.Unlock()
	farmerLiveGet("lg0001", now)
	if farmerLooted[itemName(9100)] != 4 || farmerDeaths["lg0001"] != 1 {
		t.Fatalf("reloaded loot %+v deaths %+v", farmerLooted, farmerDeaths)
	}
}
