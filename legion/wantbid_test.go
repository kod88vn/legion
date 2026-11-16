package main

import (
	"fmt"
	"testing"
	"time"
)

// setPriceFlag: dynamic_price_on for this test only (restored on cleanup)
func setPriceFlag(t *testing.T, on bool) {
	saved := *tune()
	off := saved
	off.DynamicPriceOn = on
	curTuning.Store(&off)
	t.Cleanup(func() { curTuning.Store(&saved) })
}

// simMarket: one item, the flag on, an empty board and no supply/demand history (in memory only)
func simMarket(t *testing.T, id int) {
	setPriceFlag(t, true)
	items[id] = &Item{ID: id, Name: "Sim Plate", Type: "Armor", Loc: 1}
	t.Cleanup(func() { delete(items, id) })
	econ.mu.Lock()
	oldWants, oldShops := econ.wants, econ.shops
	econ.wants, econ.shops = map[string]*Want{}, nil
	econ.mu.Unlock()
	t.Cleanup(func() {
		econ.mu.Lock()
		econ.wants, econ.shops = oldWants, oldShops
		econ.mu.Unlock()
	})
	market2.Lock()
	oldSup, oldDem := market2.supply, market2.demand
	market2.supply, market2.demand = map[int]int{}, map[int]int{}
	market2.Unlock()
	t.Cleanup(func() {
		market2.Lock()
		market2.supply, market2.demand = oldSup, oldDem
		market2.Unlock()
	})
}

// setMarket: the supply and demand a market tick would have measured for the item
func setMarket(id, supply, demand int) {
	market2.Lock()
	market2.supply[id], market2.demand[id] = supply, demand
	market2.Unlock()
}

// postBuyer: a bot with an open want for the item (as the want board holds it), with its zeny and need
func postBuyer(name string, id, zeny int, need float64) *Bot {
	pb := &Bot{P: &Persona{Name: name}, Zeny: zeny}
	econ.mu.Lock()
	econ.wants[name] = &Want{Buyer: pb, ItemID: id, Count: 1, Need: need}
	econ.mu.Unlock()
	return pb
}

// simAge: the item has been on the market for this seller for hours (the markdown clock of market.go)
func simAge(seller string, id int, hours float64) {
	market2.Lock()
	defer market2.Unlock()
	k := seller + "|" + fmt.Sprint(id)
	e := market2.md[k]
	if e == nil {
		e = &mdEntry{}
		market2.md[k] = e
	}
	now := time.Now()
	e.since = now.Add(-time.Duration(hours * float64(time.Hour)))
	e.seen = now
	e.steps = 0
}

// The buyer side: a poor bot bids less than a rich calm one, and the bid rises with the milestone gap.
func TestBuyerBidPoorDesperateBelowRichCalm(t *testing.T) {
	const id = 987661
	simMarket(t, id)
	ref := priceOf(id)
	// a poor bot short of its piece (need from the gap) against a rich calm one (generic need)
	poorNeed := milestoneNeed(5000, 300)
	poor := Willingness(ref, poorNeed, 300, "poor|1")
	rich := Willingness(ref, dynGenericNeed, 1000000, "rich|1")
	if poor >= rich {
		t.Fatalf("a poor desperate bidder must bid less: poor %d (need %.2f), rich calm %d", poor, poorNeed, rich)
	}
	// through the bot's own bid (no gated piece: generic need)
	pb := &Bot{P: &Persona{Name: "Poor bidder", Greed: 0.5}, Zeny: 300}
	rb := &Bot{P: &Persona{Name: "Rich bidder", Greed: 0.5}, Zeny: 1000000}
	if p, r := pb.wantBid(id), rb.wantBid(id); p > 300 || p >= r {
		t.Fatalf("wantBid: poor bot bid %d (purse 300), rich bot bid %d", p, r)
	}
}

func TestBuyerBidRisesWithMilestoneGap(t *testing.T) {
	const price = 10000
	full := milestoneNeed(price, price)
	half := milestoneNeed(price, price/2)
	none := milestoneNeed(price, 0)
	if !(none > half && half > full) {
		t.Fatalf("need must rise with the gap: zeny 0 %.2f, half %.2f, full %.2f", none, half, full)
	}
	if milestoneNeed(0, 0) != 0.5 {
		t.Fatalf("a gated piece without a shop price: need 0.5, got %.2f", milestoneNeed(0, 0))
	}
	ref := 1000
	lo := Willingness(ref, full, 1000000, "g|1")
	hi := Willingness(ref, none, 1000000, "g|1")
	if hi <= lo {
		t.Fatalf("with money in hand the bid rises with the gap: full %d, empty %d", lo, hi)
	}
}

// The flag off: every want-posting call returns the old bid, byte for byte.
func TestWantBidFlagOffIsOldBid(t *testing.T) {
	const healID, armorID = 987662, 987663
	setPriceFlag(t, false)
	items[healID] = &Item{ID: healID, Name: "Sim Potion", Type: "Healing", Buy: 50}
	items[armorID] = &Item{ID: armorID, Name: "Sim Hat", Type: "Armor", Loc: 1, Buy: 900}
	t.Cleanup(func() { delete(items, healID); delete(items, armorID) })
	for _, greed := range []float64{0, 0.3, 0.9} {
		for _, zeny := range []int{0, 300, 5000000} {
			b := &Bot{P: &Persona{Name: "Off bidder", Greed: greed}, Zeny: zeny}
			for _, id := range []int{healID, armorID} {
				if got, want := b.wantBid(id), bidPrice(b.P, id); got != want {
					t.Fatalf("wantBid(%d) greed %.1f zeny %d: %d, the old bid %d", id, greed, zeny, got, want)
				}
			}
		}
	}
}

// The end to end market, in memory, over several rounds: prices move with the supply, with the seller's hurry,
// and a listing sells sooner to a poor desperate buyer than to a rich calm one.
func TestSimMarketPricesMove(t *testing.T) {
	const id = 987664
	simMarket(t, id)

	// 1. supply drops: the ask rises (scarcity), and comes back when supply returns
	seller := &Bot{P: &Persona{Name: "Sim seller supply", Greed: 0.5}, vend: &VendState{}, Zeny: 900000}
	setMarket(id, 4, 1)
	glut := seller.vendPrice(id)
	setMarket(id, 2, 2)
	balanced := seller.vendPrice(id)
	setMarket(id, 0, 1)
	short := seller.vendPrice(id)
	if !(glut < balanced && balanced < short) {
		t.Fatalf("supply drop must lift the ask: glut %d, balanced %d, short %d", glut, balanced, short)
	}

	// 2. a seller in a hurry (broke, bags full) lists below a calm rich one in the same market
	setMarket(id, 2, 2)
	calm := &Bot{P: &Persona{Name: "Sim seller calm", Greed: 0.5}, vend: &VendState{}, Zeny: 900000}
	hurry := &Bot{P: &Persona{Name: "Sim seller hurry", Greed: 0.5}, vend: &VendState{}, Zeny: 0, Weight: 100, MaxWeight: 100}
	c, h := calm.vendPrice(id), hurry.vendPrice(id)
	if h >= c {
		t.Fatalf("a seller in a hurry must ask less: hurry %d, calm %d", h, c)
	}
}

func TestSimPoorDesperateBuyerSellsSooner(t *testing.T) {
	const id = 987665
	simMarket(t, id)
	setMarket(id, 1, 1)
	ref := priceOf(id)
	const grain = 25 // roundPrice rounds the listing, so a sale may be a few zeny above the willingness

	// sold reports the round (half an hour each) in which the listing reaches what the buyer pays
	sold := func(seller string, buyer *Bot, need float64) int {
		s := &Bot{P: &Persona{Name: seller, Greed: 0.5}, vend: &VendState{}, Zeny: 900000}
		for r := 0; r < 40; r++ {
			simAge(seller, id, float64(r)*0.5)
			listed := s.vendPrice(id)
			w := Willingness(ref, need, buyer.Zeny, buyer.P.Name+"|"+fmt.Sprint(id))
			if listed <= w+grain {
				return r
			}
		}
		return -1
	}
	econ.mu.Lock()
	econ.wants = map[string]*Want{}
	econ.mu.Unlock()
	poor := postBuyer("Poor desperate buyer", id, ref*3/4, 1.0)
	soldPoor := sold("Sim seller poor", poor, 1.0)
	econ.mu.Lock()
	econ.wants = map[string]*Want{}
	econ.mu.Unlock()
	rich := postBuyer("Rich calm buyer", id, 1000000, 0.3)
	soldRich := sold("Sim seller rich", rich, 0.3)
	if soldPoor < 0 || soldRich < 0 {
		t.Fatalf("both listings must sell within 20 h: poor round %d, rich round %d", soldPoor, soldRich)
	}
	if soldPoor >= soldRich {
		t.Fatalf("a poor desperate buyer must buy sooner: poor round %d, rich calm round %d", soldPoor, soldRich)
	}
}

// A broke buyer with an open want does not pull the listings of a thin market down: its willingness is under the
// floor, so it is not a customer. Several sellers keep their asks.
func TestThinMarketBrokeBuyerDoesNotCollapse(t *testing.T) {
	const id = 987666
	simMarket(t, id)
	setMarket(id, 3, 3)
	sellers := []string{"thin seller a", "thin seller b", "thin seller c"}
	bots := make([]*Bot, 0, len(sellers))
	base := map[string]int{}
	for _, name := range sellers {
		b := &Bot{P: &Persona{Name: name, Greed: 0.5}, vend: &VendState{}, Zeny: 900000}
		bots = append(bots, b)
		base[name] = b.vendPrice(id)
	}
	ref := priceOf(id)
	floor := marketFloor(id, ref)
	postBuyer("broke buyer", id, 200, 1.0)
	for _, b := range bots {
		if got := b.vendPrice(id); got != base[b.P.Name] || got < floor {
			t.Fatalf("%s: a broke buyer moved the listing: %d -> %d (floor %d)", b.P.Name, base[b.P.Name], got, floor)
		}
	}
}

// A buyer who can pay the floor does pull the listing (the same market, one buyer with 600z).
func TestThinMarketSolventBuyerPulls(t *testing.T) {
	const id = 987667
	simMarket(t, id)
	setMarket(id, 3, 3)
	name := "solvent seller"
	b := &Bot{P: &Persona{Name: name, Greed: 0.5}, vend: &VendState{}, Zeny: 900000}
	before := b.vendPrice(id)
	floor := marketFloor(id, priceOf(id))
	postBuyer("solvent buyer", id, 600, 1.0)
	after := b.vendPrice(id)
	if after >= before || after < floor {
		t.Fatalf("a solvent buyer must pull the listing down but not under the floor %d: %d -> %d", floor, before, after)
	}
}

// Unknown supply (no market tick yet) is neutral: the scarcity term is 1.0, not an empty market.
func TestUnknownSupplyIsNeutral(t *testing.T) {
	const id = 987668
	simMarket(t, id)
	market2.Lock()
	market2.supply = nil
	market2.demand = map[int]int{id: 2}
	market2.Unlock()
	s, d := dynSupplyDemand(id)
	if s != d || d != 2 {
		t.Fatalf("unknown supply must equal demand: supply %d, demand %d", s, d)
	}
	if sc := dynScarcity(s, d); sc != 1 {
		t.Fatalf("neutral scarcity must be 1.0, got %.2f", sc)
	}
}

// wantCap with the flag on never pays above the want's own posted bid (x 1.1), whatever the purse.
func TestWantCapUsesOwnBid(t *testing.T) {
	const id = 987669
	simMarket(t, id)
	b := &Bot{P: &Persona{Name: "cap buyer", Greed: 0.5}, Zeny: 900000}
	w := &Want{ItemID: id, Each: 100, Need: 1}
	if c := wantCap(b, w, 900000); c > 110 {
		t.Fatalf("a want posted at 100z must not accept a listing at %d", c)
	}
}
