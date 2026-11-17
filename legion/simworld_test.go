package main

// Shared-world scale pilot. All 1,000 subjects share one inventory of farmed goods and
// transfer zeny directly to one another. Farming yields and build goals are synthetic;
// allocation uses legion/marketengine.go (MEMap.Y, meEvaluate), and trade pricing uses
// legion/pricemodel.go (Ask, Willingness, Clear, milestoneNeed, dynZenyNeed). This is
// not a combat, XP, or full rAthena simulation. It omits class-specific yields, actual
// drop/kill distributions, NPC fallback, inventory/travel costs, taxes, and subsequent
// gear milestones. Fixed fixture yields model MEMap.Y for one two-hour shift; calibrate
// them and starting wallets from offline farming/market metrics before extrapolating.
// One auction pass per day also means buyers do not retry an offer repriced after their
// turn. Virtual decisions use a fixed clock; time.Now is used only for wall runtime.

import (
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"reflect"
	"sort"
	"strconv"
	"testing"
	"time"
)

const (
	swSubjectCount = 1000
	swItemCount    = 100
	swItemBase     = 9000000
	swFarmHours    = 2
	swReserveZeny  = 50
)

type swActor struct {
	ID           int
	Name         string
	GoalID       int
	GearOwned    bool
	CompletedDay int
	Zeny         int
	Earned       int
	Farmed       int
	Bought       int
	StarvedDays  int
	StarveReason string
}

type swLot struct {
	ItemID, SellerID, Quantity int
	ListedAt                   time.Time
}

type swDayMetrics struct {
	Day, Gaps, Farmers, Units, Buyers, Purchases, Starved, Unassigned int
	GrossZeny, Stock                                                  int
	StarveReasons                                                     map[string]int
}

type swProgressMetrics struct {
	Day, Active, Completed, Earners int
	Farmed, TransferredZeny         int
	MedianEarnings, P90Earnings     int
	MaxEarnings                     int
	Inactive, Incomplete            int
	InactiveReasons                 map[string]int
}

type swOffer struct {
	Floor, Ask, Price, BuyerWilling, BestWilling, Budget int
	BestBuyer, Buyers                                    int
}

type swWorld struct {
	Seed        int64
	Now         time.Time
	Day         int
	Actors      []swActor
	Lots        []swLot
	Harvested   map[int]int
	Purchased   map[int]int
	InitialZeny int64
	SpentZeny   int64
	Days        []swDayMetrics
}

func swTransfer(lot *swLot, quantity int) bool {
	if lot == nil || quantity <= 0 || lot.Quantity < quantity {
		return false
	}
	lot.Quantity -= quantity
	return true
}

func swItemID(i int) int { return swItemBase + i }

func swDefinition(id int) (MEItem, bool) {
	i := id - swItemBase
	if i < 0 || i >= swItemCount {
		return MEItem{}, false
	}
	return swDefinitions()[i], true
}

func swDefinitions() []MEItem {
	out := make([]MEItem, swItemCount)
	for i := range out {
		id := swItemID(i)
		out[i] = MEItem{
			ID: id, Name: fmt.Sprintf("Build part %03d", i), Price: 500 + i%4*50,
			Maps: []MEMap{
				{Map: fmt.Sprintf("sw_%03d_a", i), Mob: "synthetic field mob", MobLv: 20, Y: 0.5},
				{Map: fmt.Sprintf("sw_%03d_b", i), Mob: "synthetic field mob", MobLv: 20, Y: 0.5},
				{Map: fmt.Sprintf("sw_%03d_c", i), Mob: "synthetic field mob", MobLv: 20, Y: 0.5},
			},
		}
	}
	return out
}

func newSWWorld(seed int64) *swWorld {
	now := time.Date(2026, 11, 17, 0, 0, 0, 0, time.UTC)
	w := &swWorld{
		Seed: seed, Now: now, Harvested: map[int]int{}, Purchased: map[int]int{},
		Actors: make([]swActor, swSubjectCount),
	}
	goals := make([]int, swSubjectCount)
	for i := range goals {
		goals[i] = i % swItemCount
	}
	rand.New(rand.NewSource(seed)).Shuffle(len(goals), func(i, j int) {
		goals[i], goals[j] = goals[j], goals[i]
	})
	for i := range w.Actors {
		zeny := 2000 + i%5*100
		w.Actors[i] = swActor{
			ID: i, Name: fmt.Sprintf("sw%04d", i), GoalID: swItemID(goals[i]),
			Zeny: zeny,
		}
		w.InitialZeny += int64(zeny)
	}
	return w
}

func (w *swWorld) stockByItem() map[int]int {
	stock := map[int]int{}
	for _, lot := range w.Lots {
		if lot.Quantity > 0 {
			stock[lot.ItemID] += lot.Quantity
		}
	}
	return stock
}

func (w *swWorld) outstandingByItem() map[int]int {
	out := map[int]int{}
	for _, a := range w.Actors {
		if !a.GearOwned {
			out[a.GoalID]++
		}
	}
	return out
}

func (w *swWorld) snapshot(at time.Time) MESnapshot {
	outstanding, stock := w.outstandingByItem(), w.stockByItem()
	in := MESnapshot{Now: at, Hours: swFarmHours, Items: swDefinitions(), Farmers: make([]MEFarmer, 0, len(w.Actors))}
	for i := range in.Items {
		it := &in.Items[i]
		it.BuildShort = outstanding[it.ID]
		it.Listed = stock[it.ID]
	}
	for _, a := range w.Actors {
		in.Farmers = append(in.Farmers, MEFarmer{Name: a.Name, Lv: 30})
	}
	return in
}

func (w *swWorld) runDay() error {
	roundAt := w.Now.Add(time.Hour)
	result := meEvaluate(w.snapshot(roundAt))
	actorByName := make(map[string]int, len(w.Actors))
	for i, a := range w.Actors {
		actorByName[a.Name] = i
	}
	metrics := swDayMetrics{
		Day: w.Day + 1, Gaps: len(result.Needs), Farmers: len(result.Allocation.Orders),
		Unassigned: len(result.Allocation.Unassigned), StarveReasons: map[string]int{},
	}
	for _, order := range result.Allocation.Orders {
		actorID, ok := actorByName[order.Farmer]
		if !ok {
			return fmt.Errorf("market policy assigned unknown farmer %q", order.Farmer)
		}
		quantity := int(math.Round(order.YPerH * swFarmHours))
		if quantity <= 0 {
			return fmt.Errorf("synthetic farm yield for %s is not positive: %v", order.Farmer, order.YPerH)
		}
		w.Lots = append(w.Lots, swLot{ItemID: order.ItemID, SellerID: actorID, Quantity: quantity, ListedAt: roundAt})
		w.Harvested[order.ItemID] += quantity
		w.Actors[actorID].Farmed += quantity
		metrics.Units += quantity
	}

	buyers := make([]int, 0, len(w.Actors))
	for i, a := range w.Actors {
		if !a.GearOwned {
			buyers = append(buyers, i)
		}
	}
	rng := rand.New(rand.NewSource(w.Seed + int64(w.Day+1)*7919))
	rng.Shuffle(len(buyers), func(i, j int) { buyers[i], buyers[j] = buyers[j], buyers[i] })
	for _, buyerID := range buyers {
		if w.buyGoal(buyerID, roundAt) {
			metrics.Buyers++
			metrics.Purchases++
		}
	}
	for i := range w.Actors {
		if !w.Actors[i].GearOwned {
			metrics.Starved++
			reason := w.blockedReason(w.Actors[i].ID, roundAt)
			w.Actors[i].StarvedDays++
			w.Actors[i].StarveReason = reason
			metrics.StarveReasons[reason]++
		}
	}
	metrics.GrossZeny = int(w.SpentZeny)
	for _, quantity := range w.stockByItem() {
		metrics.Stock += quantity
	}
	w.Days = append(w.Days, metrics)
	w.Day++
	w.Now = w.Now.Add(24 * time.Hour)
	return w.audit()
}

func (w *swWorld) buyGoal(buyerID int, now time.Time) bool {
	buyer := &w.Actors[buyerID]
	item, ok := swDefinition(buyer.GoalID)
	if !ok {
		return false
	}
	for lotIndex := range w.Lots {
		lot := &w.Lots[lotIndex]
		if lot.ItemID != buyer.GoalID || lot.Quantity <= 0 || lot.SellerID == buyerID {
			continue
		}
		offer := w.quote(buyerID, lot, item, now)
		if offer.Buyers == 0 || offer.BestBuyer != buyerID ||
			offer.Price < offer.Floor || offer.Price > offer.Ask ||
			offer.Price > offer.BuyerWilling || offer.Price > offer.Budget {
			continue
		}
		if !swTransfer(lot, 1) {
			continue
		}
		seller := &w.Actors[lot.SellerID]
		buyer.Zeny -= offer.Price
		seller.Zeny += offer.Price
		seller.Earned += offer.Price
		buyer.GearOwned = true
		buyer.CompletedDay = w.Day + 1
		buyer.Bought++
		w.SpentZeny += int64(offer.Price)
		w.Purchased[item.ID]++
		return true
	}
	return false
}

func (w *swWorld) quote(buyerID int, lot *swLot, item MEItem, now time.Time) swOffer {
	buyer := &w.Actors[buyerID]
	seller := &w.Actors[lot.SellerID]
	floor := item.Price / 2
	ask := Ask(AskIn{
		Ref: item.Price, Floor: floor, Ceil: 2 * item.Price,
		Supply: w.stockByItem()[item.ID], Demand: w.outstandingByItem()[item.ID],
		Greed: 0, ZenyNeed: dynZenyNeed(seller.Zeny),
		ListedHour: now.Sub(lot.ListedAt).Hours(), Seed: seller.Name + "|" + strconv.Itoa(item.ID),
	})
	buyerNeed := milestoneNeed(item.Price, buyer.Zeny)
	budget := max(0, buyer.Zeny-swReserveZeny)
	buyerWilling := Willingness(item.Price, buyerNeed, budget, buyer.Name+"|"+strconv.Itoa(item.ID))
	offer := swOffer{Floor: floor, Ask: ask, BuyerWilling: buyerWilling, Budget: budget, BestBuyer: -1}
	bestNeed := 0.0
	for i := range w.Actors {
		candidate := &w.Actors[i]
		if candidate.GearOwned || candidate.GoalID != item.ID || candidate.ID == lot.SellerID {
			continue
		}
		need := milestoneNeed(item.Price, candidate.Zeny)
		willing := Willingness(item.Price, need, max(0, candidate.Zeny-swReserveZeny),
			candidate.Name+"|"+strconv.Itoa(item.ID))
		if willing < floor {
			continue
		}
		offer.Buyers++
		if willing > offer.BestWilling {
			offer.BestWilling, bestNeed, offer.BestBuyer = willing, need, candidate.ID
		}
	}
	offer.Price = Clear(ClearIn{
		Ask: ask, Floor: floor, Buyers: offer.Buyers,
		BestWilling: offer.BestWilling, BestNeed: bestNeed,
		SellerUrgency: dynZenyNeed(seller.Zeny),
	})
	return offer
}

func (w *swWorld) blockedReason(buyerID int, now time.Time) string {
	buyer := &w.Actors[buyerID]
	stock := w.stockByItem()[buyer.GoalID]
	if stock == 0 {
		return "finite_stock_shortage"
	}
	item, ok := swDefinition(buyer.GoalID)
	if !ok {
		return "unknown_goal_item"
	}
	selfLots, otherLots := 0, 0
	budgetBlocked, reserveBlocked, priceBlocked, outbid, noBidder, afterTurn := false, false, false, false, false, false
	for i := range w.Lots {
		lot := &w.Lots[i]
		if lot.ItemID != buyer.GoalID || lot.Quantity <= 0 {
			continue
		}
		if lot.SellerID == buyerID {
			selfLots++
			continue
		}
		otherLots++
		offer := w.quote(buyerID, lot, item, now)
		switch {
		case offer.Buyers == 0:
			noBidder = true
		case offer.Budget < offer.Floor || offer.BuyerWilling < offer.Floor:
			budgetBlocked = true
		case offer.Price > offer.BuyerWilling:
			priceBlocked = true
		case offer.Price > offer.Budget:
			reserveBlocked = true
		case offer.BestBuyer != buyerID:
			outbid = true
		default:
			afterTurn = true
		}
	}
	if otherLots == 0 && selfLots > 0 {
		return "only_self_owned_stock"
	}
	if outbid {
		return "outbid_by_more_willing_builders"
	}
	if priceBlocked {
		return "clearing_price_above_willingness"
	}
	if reserveBlocked {
		return "buyer_reserve_exceeds_available_budget"
	}
	if budgetBlocked {
		return "wallet_below_market_floor"
	}
	if noBidder {
		return "listed_but_no_bidder_can_pay_market_floor"
	}
	if afterTurn {
		return "offer_cleared_after_buyer_turn"
	}
	return "no_eligible_shared_offer"
}

func (w *swWorld) audit() error {
	zeny, earned := int64(0), int64(0)
	acquired, stock := map[int]int{}, w.stockByItem()
	for _, a := range w.Actors {
		if a.Zeny < 0 || a.Earned < 0 || a.Farmed < 0 || a.Bought < 0 {
			return fmt.Errorf("negative actor ledger for %s: %+v", a.Name, a)
		}
		zeny += int64(a.Zeny)
		earned += int64(a.Earned)
		if a.GearOwned {
			if a.CompletedDay <= 0 || a.CompletedDay > w.Day {
				return fmt.Errorf("invalid completion day for %s: %d at day %d", a.Name, a.CompletedDay, w.Day)
			}
			acquired[a.GoalID]++
		} else if a.CompletedDay != 0 {
			return fmt.Errorf("unfinished subject %s has completion day %d", a.Name, a.CompletedDay)
		}
	}
	if zeny != w.InitialZeny {
		return fmt.Errorf("zeny not conserved: balances=%d initial=%d", zeny, w.InitialZeny)
	}
	if earned != w.SpentZeny {
		return fmt.Errorf("transfer ledger mismatch: seller earnings=%d buyer spending=%d", earned, w.SpentZeny)
	}
	for _, lot := range w.Lots {
		if lot.Quantity < 0 || lot.SellerID < 0 || lot.SellerID >= len(w.Actors) {
			return fmt.Errorf("invalid stock lot: %+v", lot)
		}
	}
	for _, item := range swDefinitions() {
		if w.Purchased[item.ID] != acquired[item.ID] {
			return fmt.Errorf("item %d purchase/gear mismatch: purchased=%d gear=%d",
				item.ID, w.Purchased[item.ID], acquired[item.ID])
		}
		accounted := w.Purchased[item.ID] + stock[item.ID]
		if w.Harvested[item.ID] != accounted {
			return fmt.Errorf("item %d not conserved: harvested=%d purchased=%d stock=%d gear=%d",
				item.ID, w.Harvested[item.ID], w.Purchased[item.ID], stock[item.ID], acquired[item.ID])
		}
	}
	return nil
}

func (w *swWorld) checkpoint() (*swWorld, error) {
	raw, err := json.Marshal(w)
	if err != nil {
		return nil, err
	}
	var resumed swWorld
	if err := json.Unmarshal(raw, &resumed); err != nil {
		return nil, err
	}
	return &resumed, nil
}

func swCounts(w *swWorld) (active, progressed, earned int) {
	for _, a := range w.Actors {
		if a.Farmed > 0 || a.Bought > 0 {
			active++
		}
		if a.GearOwned {
			progressed++
		}
		if a.Earned > 0 {
			earned++
		}
	}
	return active, progressed, earned
}

func swProgressAt(w *swWorld) swProgressMetrics {
	m := swProgressMetrics{
		Day: w.Day, TransferredZeny: int(w.SpentZeny), InactiveReasons: map[string]int{},
	}
	earnings := make([]int, 0, len(w.Actors))
	for _, a := range w.Actors {
		if a.Farmed > 0 || a.Bought > 0 {
			m.Active++
		} else {
			m.Inactive++
			if a.StarveReason != "" {
				m.InactiveReasons[a.StarveReason]++
			}
		}
		if a.GearOwned {
			m.Completed++
		}
		if a.Earned > 0 {
			m.Earners++
		}
		m.Farmed += a.Farmed
		earnings = append(earnings, a.Earned)
	}
	m.Incomplete = len(w.Actors) - m.Completed
	sort.Ints(earnings)
	if len(earnings) > 0 {
		m.MedianEarnings = earnings[(len(earnings)-1)/2]
		m.P90Earnings = earnings[(len(earnings)-1)*90/100]
		m.MaxEarnings = earnings[len(earnings)-1]
	}
	return m
}

func TestSharedWorldRejectsDoubleSpend(t *testing.T) {
	lot := &swLot{ItemID: 1, Quantity: 1}
	if !swTransfer(lot, 1) {
		t.Fatal("first buyer could not purchase the available unit")
	}
	if swTransfer(lot, 1) {
		t.Fatal("second buyer purchased a unit that was already transferred")
	}
}

func TestSharedWorldAuctionRunsOncePerVirtualDay(t *testing.T) {
	w := newSWWorld(77)
	if err := w.runDay(); err != nil {
		t.Fatal(err)
	}
	if len(w.Lots) == 0 {
		t.Fatal("first virtual day created no shared stock")
	}
	firstAuction := w.Lots[0].ListedAt
	firstDayLots := len(w.Lots)
	if err := w.runDay(); err != nil {
		t.Fatal(err)
	}
	if len(w.Lots) == firstDayLots {
		t.Fatal("second virtual day created no shared stock")
	}
	secondAuction := w.Lots[firstDayLots].ListedAt
	if elapsed := secondAuction.Sub(firstAuction); elapsed != 24*time.Hour {
		t.Fatalf("auctions were %s apart, want one virtual day (24h)", elapsed)
	}
}

func TestSharedWorld1000SeededProgressionAndLedgers(t *testing.T) {
	const seed = int64(20261117)
	started := time.Now()
	w := newSWWorld(seed)
	checkpoints := map[int]swProgressMetrics{}
	for day := 0; day < 90; day++ {
		if err := w.runDay(); err != nil {
			t.Fatalf("day %d: %v", day+1, err)
		}
		switch w.Day {
		case 3, 30, 90:
			checkpoints[w.Day] = swProgressAt(w)
		}
	}
	wall := time.Since(started)
	active, progressed, earners := swCounts(w)
	for i, day := range w.Days[:min(10, len(w.Days))] {
		t.Logf("day=%d gaps=%d farmers=%d harvested=%d buyers=%d completed=%d starved=%d unassigned=%d transferred_zeny=%d reasons=%v",
			i+1, day.Gaps, day.Farmers, day.Units, day.Buyers, swSubjectCount-day.Starved,
			day.Starved, day.Unassigned, day.GrossZeny, day.StarveReasons)
	}
	if len(w.Actors) != swSubjectCount {
		t.Fatalf("subjects=%d, want %d", len(w.Actors), swSubjectCount)
	}
	if len(w.Days) < 2 || w.Days[0].Starved == 0 || w.Days[0].Unassigned == 0 {
		t.Fatalf("finite-stock competition was not visible: %+v", w.Days)
	}
	if w.Days[0].StarveReasons["finite_stock_shortage"] == 0 ||
		w.Days[0].StarveReasons["outbid_by_more_willing_builders"]+
			w.Days[0].StarveReasons["clearing_price_above_willingness"]+
			w.Days[0].StarveReasons["listed_but_no_bidder_can_pay_market_floor"] == 0 {
		t.Fatalf("resource and price competition were not distinguishable: %+v", w.Days[0].StarveReasons)
	}
	if active < 900 || progressed < 900 || earners < 100 {
		t.Fatalf("weak participation/progression: active=%d progressed=%d earning=%d", active, progressed, earners)
	}
	if checkpoints[3].Completed == 0 || checkpoints[3].Completed == swSubjectCount ||
		checkpoints[30].Completed != swSubjectCount ||
		checkpoints[90].Completed != swSubjectCount {
		t.Fatalf("shared-world progression did not converge: day3=%+v day30=%+v day90=%+v",
			checkpoints[3], checkpoints[30], checkpoints[90])
	}
	inactiveWithReason := 0
	for _, count := range checkpoints[3].InactiveReasons {
		inactiveWithReason += count
	}
	if inactiveWithReason != checkpoints[3].Inactive {
		t.Fatalf("day-3 inactive subjects lack persisted starvation reasons: inactive=%d reasoned=%d reasons=%v",
			checkpoints[3].Inactive, inactiveWithReason, checkpoints[3].InactiveReasons)
	}
	firstCompleteDay := 0
	for i, day := range w.Days {
		if day.Starved == 0 {
			firstCompleteDay = i + 1
			break
		}
	}
	if firstCompleteDay == 0 {
		t.Fatal("one-piece goals never reached full completion")
	}
	for _, day := range w.Days[firstCompleteDay:] {
		if day.Farmers != 0 || day.Buyers != 0 {
			t.Fatalf("world continued progression after all goals completed: first_complete_day=%d later_day=%+v",
				firstCompleteDay, day)
		}
	}
	if w.Days[89].Farmers != 0 || w.Days[89].Starved != 0 ||
		len(w.stockByItem()) != 0 || len(w.outstandingByItem()) != 0 {
		t.Fatalf("day-90 idle world has unresolved work or starvation: %+v", w.Days[89])
	}
	if err := w.audit(); err != nil {
		t.Fatalf("final ledger audit: %v", err)
	}
	metrics := w.Days[0]
	t.Logf("seed=%d subjects=%d simulated=%s days=%d first_all_goals_day=%d active=%d progressed=%d earners=%d day1_farmers=%d day1_units=%d day1_starved=%d day1_unassigned=%d final_stock=%d gross_zeny=%d wall=%s",
		seed, len(w.Actors), w.Now.Sub(time.Date(2026, 11, 17, 0, 0, 0, 0, time.UTC)), len(w.Days),
		firstCompleteDay, active, progressed, earners, metrics.Farmers, metrics.Units, metrics.Starved, metrics.Unassigned,
		func() int {
			total := 0
			for _, n := range w.stockByItem() {
				total += n
			}
			return total
		}(), w.SpentZeny, wall)
	for _, day := range []int{3, 30, 90} {
		t.Logf("progress day=%d active=%d inactive=%d completed=%d incomplete=%d inactive_reasons=%v earners=%d farmed=%d transferred_zeny=%d earnings_median=%d p90=%d max=%d",
			day, checkpoints[day].Active, checkpoints[day].Inactive, checkpoints[day].Completed,
			checkpoints[day].Incomplete, checkpoints[day].InactiveReasons, checkpoints[day].Earners,
			checkpoints[day].Farmed, checkpoints[day].TransferredZeny,
			checkpoints[day].MedianEarnings, checkpoints[day].P90Earnings, checkpoints[day].MaxEarnings)
	}
}

func TestSharedWorldCheckpointResumeMatchesContinuousDays(t *testing.T) {
	const seed = int64(38122)
	continuous := newSWWorld(seed)
	for day := 0; day < 90; day++ {
		if err := continuous.runDay(); err != nil {
			t.Fatalf("continuous day %d: %v", day+1, err)
		}
	}

	resumed := newSWWorld(seed)
	for day := 0; day < 30; day++ {
		if err := resumed.runDay(); err != nil {
			t.Fatalf("pre-checkpoint day %d: %v", day+1, err)
		}
	}
	resumed, err := resumed.checkpoint()
	if err != nil {
		t.Fatalf("checkpoint: %v", err)
	}
	for day := 30; day < 90; day++ {
		if err := resumed.runDay(); err != nil {
			t.Fatalf("resumed day %d: %v", day+1, err)
		}
	}
	if !reflect.DeepEqual(continuous, resumed) {
		t.Fatalf("checkpoint-resumed world differs from continuous run\ncontinuous=%+v\nresumed=%+v", continuous.Days, resumed.Days)
	}
}

func TestSharedWorldCheckpointResumePreservesActiveStock(t *testing.T) {
	const seed = int64(20261117)
	continuous := newSWWorld(seed)
	resumed := newSWWorld(seed)
	for day := 0; day < 3; day++ {
		if err := continuous.runDay(); err != nil {
			t.Fatalf("continuous day %d: %v", day+1, err)
		}
		if day < 2 {
			if err := resumed.runDay(); err != nil {
				t.Fatalf("pre-checkpoint day %d: %v", day+1, err)
			}
		}
	}
	if err := resumed.audit(); err != nil {
		t.Fatalf("day-2 pre-checkpoint audit: %v", err)
	}
	stockAtCheckpoint := resumed.stockByItem()
	totalStock := 0
	for _, quantity := range stockAtCheckpoint {
		totalStock += quantity
	}
	if totalStock == 0 {
		t.Fatal("day-2 checkpoint has no active shared stock")
	}
	completedAtCheckpoint := swProgressAt(resumed).Completed

	var err error
	resumed, err = resumed.checkpoint()
	if err != nil {
		t.Fatalf("day-2 checkpoint: %v", err)
	}
	if err := resumed.audit(); err != nil {
		t.Fatalf("day-2 resumed checkpoint audit: %v", err)
	}
	if !reflect.DeepEqual(stockAtCheckpoint, resumed.stockByItem()) {
		t.Fatalf("active shared stock changed across checkpoint: before=%v after=%v",
			stockAtCheckpoint, resumed.stockByItem())
	}
	if err := resumed.runDay(); err != nil {
		t.Fatalf("resumed day 3: %v", err)
	}
	if resumed.Days[2].Purchases == 0 ||
		swProgressAt(resumed).Completed <= completedAtCheckpoint {
		t.Fatalf("resumed day 3 made no goal progress: purchases=%d before=%d after=%d",
			resumed.Days[2].Purchases, completedAtCheckpoint, swProgressAt(resumed).Completed)
	}
	if !reflect.DeepEqual(continuous, resumed) {
		t.Fatalf("active-stock checkpoint differs from uninterrupted day 3\ncontinuous=%+v\nresumed=%+v",
			continuous.Days, resumed.Days)
	}
}

func TestSharedWorldOutOfSampleSeedProgresses(t *testing.T) {
	const seed = int64(38122)
	w := newSWWorld(seed)
	for day := 0; day < 30; day++ {
		if err := w.runDay(); err != nil {
			t.Fatalf("seed %d day %d: %v", seed, day+1, err)
		}
	}
	active, progressed, earners := swCounts(w)
	if progressed < 900 || active < 900 || earners < 100 {
		t.Fatalf("out-of-sample seed %d: active=%d progressed=%d earners=%d", seed, active, progressed, earners)
	}
	if err := w.audit(); err != nil {
		t.Fatalf("seed %d ledger: %v", seed, err)
	}
	t.Logf("out_of_sample_seed=%d subjects=%d simulated=%s active=%d progressed=%d earners=%d transferred_zeny=%d",
		seed, len(w.Actors), time.Duration(w.Day)*24*time.Hour, active, progressed, earners, w.SpentZeny)
}

func TestSharedWorldDemandAndStockAreSharedAcrossSubjects(t *testing.T) {
	w := newSWWorld(77)
	before := w.snapshot(w.Now)
	result := meEvaluate(before)
	if result.Farmers != swSubjectCount || result.Allocation.Ordered == 0 {
		t.Fatalf("market policy did not receive the shared cohort: farmers=%d orders=%d", result.Farmers, result.Allocation.Ordered)
	}
	if err := w.runDay(); err != nil {
		t.Fatal(err)
	}
	after := w.snapshot(w.Now)
	produced := w.stockByItem()
	total := 0
	for _, n := range produced {
		total += n
	}
	if total == 0 {
		t.Fatal("no shared stock was created by the policy's allocated work")
	}
	if len(after.Items) != swItemCount {
		t.Fatalf("world item set=%d, want %d", len(after.Items), swItemCount)
	}
	var item MEItem
	harvestedIDs := make([]int, 0, len(w.Harvested))
	for id, created := range w.Harvested {
		if created > 0 {
			harvestedIDs = append(harvestedIDs, id)
		}
	}
	sort.Ints(harvestedIDs)
	if len(harvestedIDs) > 0 {
		item, _ = swDefinition(harvestedIDs[0])
	}
	created := w.Harvested[item.ID]
	if item.ID == 0 || created <= 0 {
		t.Fatal("market policy produced no sample item")
	}
	quote := func(supply int) int {
		return Ask(AskIn{
			Ref: item.Price, Floor: item.Price / 2, Ceil: 2 * item.Price,
			Supply: supply, Demand: 10, Greed: 0, ZenyNeed: 1, Seed: "shared-world|scarcity",
		})
	}
	if scarce, supplied := quote(0), quote(created); scarce <= supplied {
		t.Fatalf("production Ask ignored shared supply: no-stock=%d shared-stock(%d)=%d", scarce, created, supplied)
	}
	// Any purchase removes one exact unit from a seller's lot; the independent audit
	// checks that stock plus purchased gear equals the harvested resource.
	if err := w.audit(); err != nil {
		t.Fatal(err)
	}
}
