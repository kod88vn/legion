package main

// pricedyn.go (EC48, tuning dynamic_price_on, default off). The bot-side wiring of pricemodel.go. With the flag off every
// function here hands over to the fixed code it wraps, so the existing behaviour is unchanged. With it on:
//   - vend listing: vend.go vendPrice -> vendPriceDyn (replaces vendPriceFixed: greed x personal x markdown x undercut)
//   - undercut: vendPriceDyn undercuts the lowest other shop by 2%, then Clear pulls to the best interested buyer
//   - markdown: pricemodel.go Ask (continuous, from the time listed) replaces market.go markdown() steps; listingHours
//     keeps the same clock, so a sale (marketSold) and a want (marketTick) still reset it
//   - seller asks outside a shop: economy.go askPrice -> askPriceDyn (replaces askPriceFixed)
//   - buy offers: every want-posting site calls Bot.wantBid (replaces bidPrice); the shop-listing acceptance is wantCap
//     (replaces w.Each*11/10) and stock bids are stockBid (replaces priceOf*0.9)
//   - buyer need: stored on the Want when it is posted (Want.Need, from wantNeed). Probes that post nothing store nothing.
//   - buyers' side has no scarcity term on purpose: a buyer's willingness is its own valuation (its purse and need).
//     Scarcity moves the seller's ask (Ask); Clear then pulls that ask toward the buyer. Putting scarcity on both sides
//     would count it twice.
//   - a buyer counts as interested only if it can pay the seller's floor: a broke buyer with a want does not pull a
//     listing down to a price no sale can happen at.

import (
	"fmt"
	"log"
	"strconv"
	"time"
)

// dynamicPriceOn: tuning dynamic_price_on (default false)
func dynamicPriceOn() bool { return tune().DynamicPriceOn }

// listingHours: hours the item has been on the market for this seller (the markdown clock of market.go; the entry is
// created and kept alive as the old markdown() does). fresh is true when this call reached a new markdown step.
func listingHours(seller string, id int, now time.Time) (h float64, fresh bool) {
	k := seller + "|" + fmt.Sprint(id)
	market2.Lock()
	defer market2.Unlock()
	e := market2.md[k]
	if e == nil {
		e = &mdEntry{since: now}
		market2.md[k] = e
	}
	e.seen = now
	h = now.Sub(e.since).Hours()
	if steps := dynMarkdownSteps(h); steps > e.steps {
		e.steps = steps
		fresh = true
	}
	return h, fresh
}

// logDynMarkdown: the markdown log line for a new step (old = the ask before any markdown, new = the ask now)
func logDynMarkdown(seller string, id, old, neu int, now time.Time) {
	market2.Lock()
	market2.mdLogs = append(market2.mdLogs, mdLog{now, seller, itemName(id), old, neu})
	if len(market2.mdLogs) > 60 {
		market2.mdLogs = market2.mdLogs[len(market2.mdLogs)-60:]
	}
	market2.Unlock()
	log.Printf("markdown(dyn): %s %s %d->%d", itemName(id), seller, old, neu)
}

// dynSupplyDemand: units on offer and units wanted at the last market tick. Unknown supply (no tick yet, -1) is neutral:
// it is taken equal to the demand, so the scarcity term is 1.0 rather than a pretend empty market.
func dynSupplyDemand(id int) (supply, demand int) {
	demand = max(marketDemand(id), 0)
	supply = marketSupply(id)
	if supply < 0 {
		supply = demand
	}
	return supply, demand
}

// lowestOtherShop: the cheapest listing of the item in another open shop (0 if none)
func lowestOtherShop(id int, self *Bot) int {
	low := 0
	econ.mu.Lock()
	defer econ.mu.Unlock()
	for _, sh := range econ.shops {
		if sh.vendor == self {
			continue
		}
		for _, l := range sh.items {
			if l.ItemID == id && l.Count > 0 && l.Price > 0 && (low == 0 || l.Price < low) {
				low = l.Price
			}
		}
	}
	return low
}

// interestedBuyers: the bots that want the item and can pay the seller's floor, and the best of them: how many, the most
// the best one pays, and its need. A buyer whose willingness is under the floor is not a customer (a broke buyer must
// not pull every listing of the item down to a price no sale can happen at). The need is the one stored on its want
// (Want.Need, set when the want was posted). The buyer's purse is read without its lock, as market.go does.
func interestedBuyers(id, ref, floor int, self string) (n, bestW int, bestNeed float64) {
	econ.mu.Lock()
	defer econ.mu.Unlock()
	for name, w := range econ.wants {
		if w == nil || w.ItemID != id || w.Buyer == nil || name == self {
			continue
		}
		need := w.Need
		if need <= 0 {
			need = dynWantNeed
		}
		W := Willingness(ref, need, w.Buyer.Zeny, name+"|"+strconv.Itoa(id))
		if W < floor {
			continue
		}
		n++
		if W > bestW {
			bestW, bestNeed = W, need
		}
	}
	return
}

// wantBid: what this bot offers for an item it wants (the want-posting calls). Flag off: the fixed bidPrice, unchanged.
// Flag on: the buyer's willingness from its need and its purse. Pure with respect to the bot: it writes nothing, so a
// probe that posts no want leaves no trace. Caller holds b.mu.
func (b *Bot) wantBid(id int) int {
	if !dynamicPriceOn() {
		return bidPrice(b.P, id)
	}
	bid := roundPrice(Willingness(priceOf(id), b.buildNeed(id), b.Zeny, b.P.Name+"|"+strconv.Itoa(id)))
	if np := npcMatPrice[id]; np > 0 {
		bid = min(bid, np) // as bidPrice: nobody bids over Dietrich's price for refine materials
	}
	return bid
}

// wantNeed: the need to store on a want for this item (Want.Need). 0 with the flag off, so an old-path want is unchanged.
// Caller holds b.mu.
func (b *Bot) wantNeed(id int) float64 {
	if !dynamicPriceOn() {
		return 0
	}
	return b.buildNeed(id)
}

// buildNeed (b.mu held): the need of this bot for the item. The gated gear piece of its build (gearGate) carries the
// milestone gap; any other want is a generic 0.3.
func (b *Bot) buildNeed(id int) float64 {
	_, goal := b.gearGate(b.BaseLvOr())
	if goal == nil || goal.Item != id {
		return dynGenericNeed
	}
	price := 0
	if it := items[goal.Item]; it != nil {
		price = it.Buy
	}
	return milestoneNeed(price, b.Zeny)
}

// dynGenericNeed: need of a bot for a want that is not its gated build piece
const dynGenericNeed = 0.3

// dynWantNeed: need assumed for a want posted without one (Want.Need 0: a want from before the flag, or a non-bot buyer)
const dynWantNeed = 0.6

// dynStockNeed: need of a bot buying stock for its own shop (not a build piece)
const dynStockNeed = 0.3

// vendPriceDyn: the dynamic listing price of the item in this vendor's shop (flag on)
func (b *Bot) vendPriceDyn(id int) int {
	if items[id] == nil {
		return 1000
	}
	now := time.Now()
	ref := priceOf(id)
	greed := b.P.Greed
	if b.vend != nil && b.vend.Markup != 0 { // the market mind's markup: greed = where 0.95 + 0.35 x greed lands
		greed = dynClamp01((b.vend.Markup - 0.95) / 0.35)
	}
	zn := dynZenyNeed(b.Zeny)
	pr := dynPressure(b.Weight, b.MaxWeight)
	supply, demand := dynSupplyDemand(id)
	in := AskIn{Ref: ref, Floor: marketFloor(id, ref), Ceil: 2 * ref, Supply: supply,
		Demand: demand, Greed: greed, ZenyNeed: zn, Pressure: pr, Seed: b.P.Name + "|" + strconv.Itoa(id)}
	var fresh bool
	in.ListedHour, fresh = listingHours(b.P.Name, id, now)
	p := Ask(in)
	if low := lowestOtherShop(id, b); low > 0 && p > low {
		p = max(low*98/100, in.Floor) // undercut the lowest other shop by 2%
	}
	n, bestW, bestNeed := interestedBuyers(id, ref, in.Floor, b.P.Name)
	urgency := dynClamp01(0.6*zn + 0.4*pr)
	p = Clear(ClearIn{Ask: p, Floor: in.Floor, Buyers: n, BestWilling: bestW, BestNeed: bestNeed,
		SellerUrgency: urgency})
	p = npcMatCap(id, npcGearCap(id, p))
	out := npcWindow(id, max(roundPrice(p), in.Floor, 2))
	if fresh {
		noMarkdown := in
		noMarkdown.ListedHour = 0
		if old := Ask(noMarkdown); out < old {
			logDynMarkdown(b.P.Name, id, old, out, now)
		}
	}
	return out
}

// askPriceDyn: the dynamic ask of a persona outside a shop (flag on): no zeny or bag pressure is known here
func askPriceDyn(p *Persona, id int) int {
	now := time.Now()
	ref := priceOf(id)
	supply, demand := dynSupplyDemand(id)
	in := AskIn{Ref: ref, Floor: marketFloor(id, ref), Ceil: 2 * ref, Supply: supply,
		Demand: demand, Greed: p.Greed, Seed: p.Name + "|" + strconv.Itoa(id)}
	in.ListedHour, _ = listingHours(p.Name, id, now)
	ask := npcMatCap(id, npcGearCap(id, Ask(in)))
	return npcWindow(id, roundPrice(max(ask, in.Floor)))
}

// wantCap: the most this bot pays for a shop listing of the item when it has an open want for it. Flag off: the fixed
// w.Each x 1.1. Flag on: the lower of the willingness from the want's own need and the purse, and the want's own
// posted bid x 1.1 (a buyer never pays above what its own want says).
func wantCap(b *Bot, w *Want, budget int) int {
	if !dynamicPriceOn() {
		return w.Each * 11 / 10
	}
	need := w.Need
	if need <= 0 {
		need = dynWantNeed
	}
	c := Willingness(priceOf(w.ItemID), need, budget, b.P.Name+"|"+strconv.Itoa(w.ItemID))
	if w.Each > 0 {
		c = min(c, w.Each*11/10)
	}
	return roundPrice(c)
}

// stockBid: what this bot bids for stock for its own shop (flag off: priceOf x 0.9)
func stockBid(b *Bot, id, budget int) int {
	if !dynamicPriceOn() {
		return roundPrice(priceOf(id) * 9 / 10)
	}
	return roundPrice(Willingness(priceOf(id), dynStockNeed, budget, b.P.Name+"|"+strconv.Itoa(id)))
}
