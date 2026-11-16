package main

import "testing"

// base: a neutral listing (balanced market, no hurry, fresh) for a 1,000z item
func neutralAsk() AskIn {
	return AskIn{Ref: 1000, Floor: 100, Ceil: 2000, Greed: 0.5, Seed: "seller|1"}
}

func TestAskFallsWithSellerUrgency(t *testing.T) {
	calm := neutralAsk()
	poor := calm
	poor.ZenyNeed = 1
	jam := poor
	jam.Pressure = 1
	a, b, c := Ask(calm), Ask(poor), Ask(jam)
	if !(a > b && b > c) {
		t.Fatalf("ask must fall as urgency rises: calm %d, broke %d, broke and full bags %d", a, b, c)
	}
}

func TestAskRisesWithScarcity(t *testing.T) {
	glut := neutralAsk()
	glut.Supply, glut.Demand = 10, 0
	balanced := neutralAsk()
	balanced.Supply, balanced.Demand = 3, 3
	short := neutralAsk()
	short.Supply, short.Demand = 0, 10
	g, m, s := Ask(glut), Ask(balanced), Ask(short)
	if !(g < m && m < s) {
		t.Fatalf("ask must rise with scarcity: glut %d, balanced %d, short %d", g, m, s)
	}
}

func TestClearFallsWhenOnlyBuyerIsPoorAndDesperate(t *testing.T) {
	const ask, ref = 1000, 1000
	// a rich buyer with no need: pays the ask
	rich := ClearIn{Ask: ask, Floor: 100, Ref: ref, Buyers: 1,
		BestWilling: Willingness(ref, 0, 100000, "rich|1"), BestNeed: 0, BestBudget: 100000}
	// the only buyer: short of money (300z) and desperate for the piece
	poor := ClearIn{Ask: ask, Floor: 100, Ref: ref, Buyers: 1,
		BestWilling: Willingness(ref, 1, 300, "poor|1"), BestNeed: 1, BestBudget: 300}
	r, p := Clear(rich), Clear(poor)
	if r != ask {
		t.Fatalf("a rich buyer with no need should not pull the price: %d", r)
	}
	if p >= r {
		t.Fatalf("a poor desperate lone buyer must pull the price down: poor %d, rich %d", p, r)
	}
	// the same poor buyer among many buyers pulls less (others compete for the piece)
	crowd := poor
	crowd.Buyers = 5
	if c := Clear(crowd); c < p {
		t.Fatalf("with 5 buyers the lone poor buyer should pull less: crowd %d, lone %d", c, p)
	}
	// a hurried seller also comes down, even with the rich buyer
	hurry := rich
	hurry.BestWilling = 600
	hurry.SellerUrgency = 1
	if h := Clear(hurry); h >= ask {
		t.Fatalf("a seller in a hurry must come down toward a 600z buyer: %d", h)
	}
}

func TestMarkdownOverTime(t *testing.T) {
	at := func(h float64) int {
		in := neutralAsk()
		in.ListedHour = h
		return Ask(in)
	}
	fresh, half, one, two, long := at(0), at(0.5), at(1), at(2), at(12)
	if fresh != half {
		t.Fatalf("no markdown in the first 45 min: fresh %d, 30 min %d", fresh, half)
	}
	if !(one < half && two < one && long < two) {
		t.Fatalf("markdown must keep falling over time: 0.5h %d, 1h %d, 2h %d, 12h %d", half, one, two, long)
	}
	if long != at(100) {
		t.Fatalf("markdown stops at six steps: 12h %d, 100h %d", long, at(100))
	}
}

func TestFloorAndCeilingHold(t *testing.T) {
	top := AskIn{Ref: 1000, Floor: 100, Ceil: 1500, Greed: 1, Supply: 0, Demand: 1000}
	if a := Ask(top); a > 1500 || a < 100 {
		t.Fatalf("ask above the ceiling: %d", a)
	}
	bottom := AskIn{Ref: 1000, Floor: 700, Ceil: 2000, Greed: 0, Supply: 1000, Demand: 0, ZenyNeed: 1,
		Pressure: 1, ListedHour: 200}
	if a := Ask(bottom); a != 700 {
		t.Fatalf("ask below the floor: %d (want the floor 700)", a)
	}
	// a clear never goes under the floor nor above the ask
	c := Clear(ClearIn{Ask: 1000, Floor: 800, Ref: 1000, Buyers: 1, BestWilling: 1, BestNeed: 1, BestBudget: 1,
		SellerUrgency: 1})
	if c != 800 {
		t.Fatalf("clear below the floor: %d", c)
	}
	if c := Clear(ClearIn{Ask: 1000, Floor: 100, Ref: 1000, Buyers: 1, BestWilling: 5000}); c != 1000 {
		t.Fatalf("clear above the ask: %d", c)
	}
	if w := Willingness(1000, 1, 250, "x"); w > 250 {
		t.Fatalf("willingness above the budget: %d", w)
	}
}

func TestNotConstantAcrossConditions(t *testing.T) {
	seen := map[int]bool{}
	for _, zn := range []float64{0, 0.5, 1} {
		for _, sup := range []int{0, 2, 8} {
			for _, h := range []float64{0, 1, 3} {
				in := neutralAsk()
				in.ZenyNeed, in.Supply, in.ListedHour = zn, sup, h
				seen[Ask(in)] = true
			}
		}
	}
	if len(seen) < 10 {
		t.Fatalf("the ask must move with conditions, got only %d distinct prices", len(seen))
	}
	// two sellers, same item, same conditions: not the same number
	a := neutralAsk()
	distinct := map[int]bool{}
	for _, seed := range []string{"s1|1", "s2|1", "s3|1", "s4|1", "s5|1", "s6|1"} {
		a.Seed = seed
		distinct[Ask(a)] = true
	}
	if len(distinct) < 2 {
		t.Fatalf("sellers with the same conditions all post %v", distinct)
	}
}

func TestJitterDeterministicAndBounded(t *testing.T) {
	for i := 0; i < 500; i++ {
		s := "bot" + string(rune('a'+i%26)) + "|" + string(rune('0'+i%10))
		j := dynJitter(s)
		if j < 0.97 || j > 1.03 {
			t.Fatalf("jitter %v out of range for %q", j, s)
		}
		if dynJitter(s) != j {
			t.Fatalf("jitter not deterministic for %q", s)
		}
	}
}

func TestWillingnessRisesWithNeedAndBudget(t *testing.T) {
	lo := Willingness(1000, 0, 5000, "b|1")
	hi := Willingness(1000, 1, 5000, "b|1")
	if hi <= lo {
		t.Fatalf("a bot that needs the piece bids more: need 0 %d, need 1 %d", lo, hi)
	}
	if Willingness(1000, 1, 0, "b|1") != 0 {
		t.Fatalf("no budget, no bid")
	}
	if Willingness(1000, 1, 300, "b|1") != 300 {
		t.Fatalf("a short purse caps the bid at the purse")
	}
}

// the flag off: every wired call returns exactly the value of the fixed code it replaces
func TestFlagOffKeepsFixedPrices(t *testing.T) {
	const id = 987654
	saved := *tune()
	off := saved
	off.DynamicPriceOn = false
	curTuning.Store(&off)
	t.Cleanup(func() { curTuning.Store(&saved) })
	items[id] = &Item{ID: id, Name: "Test Gem", Type: "Card", Buy: 0}
	t.Cleanup(func() { delete(items, id) })

	b := &Bot{P: &Persona{Name: "Off Test", Greed: 0.6}, vend: &VendState{}, Zeny: 5000}
	if got, want := b.vendPrice(id), b.vendPriceFixed(id); got != want {
		t.Fatalf("vendPrice with the flag off: %d, fixed %d", got, want)
	}
	if got, want := askPrice(b.P, id), askPriceFixed(b.P, id); got != want {
		t.Fatalf("askPrice with the flag off: %d, fixed %d", got, want)
	}
	w := &Want{ItemID: id, Each: 1234}
	if got := wantCap(b, w, 5000); got != 1234*11/10 {
		t.Fatalf("wantCap with the flag off: %d", got)
	}
	if got := stockBid(b, id, 5000); got != roundPrice(priceOf(id)*9/10) {
		t.Fatalf("stockBid with the flag off: %d", got)
	}
}

// the flag on: the vendor's listing follows the seller's urgency and the buyer's budget, end to end
func TestFlagOnVendPriceFollowsUrgencyAndBuyers(t *testing.T) {
	const id = 987655
	saved := *tune()
	on := saved
	on.DynamicPriceOn = true
	curTuning.Store(&on)
	t.Cleanup(func() { curTuning.Store(&saved) })
	items[id] = &Item{ID: id, Name: "Test Plate", Type: "Armor", Loc: 1, Buy: 0}
	t.Cleanup(func() { delete(items, id) })
	econ.mu.Lock()
	oldWants := econ.wants
	econ.wants = map[string]*Want{}
	econ.mu.Unlock()
	t.Cleanup(func() {
		econ.mu.Lock()
		econ.wants = oldWants
		econ.mu.Unlock()
	})

	rich := &Bot{P: &Persona{Name: "Rich seller", Greed: 0.5}, vend: &VendState{}, Zeny: 900000}
	broke := &Bot{P: &Persona{Name: "Broke seller", Greed: 0.5}, vend: &VendState{}, Zeny: 0}
	if b := broke.vendPrice(id); b >= rich.vendPrice(id) {
		t.Fatalf("a broke seller must ask less than a rich one: broke %d", b)
	}

	// one interested buyer, poor and desperate for the piece, pulls a rich seller's price down
	seller := &Bot{P: &Persona{Name: "Lone seller", Greed: 0.5}, vend: &VendState{}, Zeny: 900000}
	before := seller.vendPrice(id)
	econ.mu.Lock()
	econ.wants["poor buyer"] = &Want{Buyer: &Bot{P: &Persona{Name: "poor buyer"}, Zeny: 200}, ItemID: id, Count: 1, Each: 100}
	econ.mu.Unlock()
	after := seller.vendPrice(id)
	econ.mu.Lock()
	delete(econ.wants, "poor buyer")
	econ.mu.Unlock()
	if after >= before {
		t.Fatalf("a lone poor desperate buyer must pull the listing down: before %d, after %d", before, after)
	}
}
