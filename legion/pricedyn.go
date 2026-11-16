package main

// pricedyn.go (EC48, tuning dynamic_price_on, default off). The bot-side wiring of pricemodel.go. With the flag off every
// function here hands over to the fixed code it wraps, so the existing behaviour is unchanged. With it on:
//   - vend listing: vend.go vendPrice -> vendPriceDyn (replaces vendPriceFixed: greed x personal x markdown x undercut)
//   - undercut: vendPriceDyn undercuts the lowest other shop by 2%, then the Clear step pulls to the best buyer
//   - markdown: pricemodel.go Ask (continuous, from the time listed) replaces market.go markdown() steps; listingHours
//     keeps the same clock, so a sale (marketSold) and a want (marketTick) still reset it
//   - seller asks outside a shop: economy.go askPrice -> askPriceDyn (replaces askPriceFixed)
//   - buy offers: vendbuy.go accepts a shop listing up to wantCap (replaces w.Each*11/10) and posts stock bids at
//     stockBid (replaces priceOf*9/10)
// Not wired yet (still bidPrice, unverified): the want-posting calls in economy.go, cards.go, chatintent.go and
// guildstock.go, so a want's Each stays at the fixed bid until those are moved too.

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

// interestedBuyers: the bots that want the item (wants on the board, not the seller itself) and the best of them:
// how many, the most the best one pays, its need and its budget. A want from a bot with no zeny at all is not counted.
// Reads the buyer's zeny without its lock, as market.go does (an int snapshot is fine here).
func interestedBuyers(id, ref int, self string) (n, bestW int, bestNeed float64, bestBudget int) {
	econ.mu.Lock()
	defer econ.mu.Unlock()
	for name, w := range econ.wants {
		if w == nil || w.ItemID != id || w.Buyer == nil || name == self {
			continue
		}
		budget := w.Buyer.Zeny
		W := Willingness(ref, dynWantNeed, budget, name+"|"+strconv.Itoa(id))
		if W == 0 {
			continue
		}
		n++
		if W > bestW {
			bestW, bestNeed, bestBudget = W, dynWantNeed, budget
		}
	}
	return
}

// dynWantNeed: the need of a buyer with an open want for the item. Fixed until the build's milestone gap is passed
// through the want (buildmilestones.go gatedPrice); see the header of pricedyn.go.
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
	in := AskIn{Ref: ref, Floor: marketFloor(id, ref), Ceil: 2 * ref, Supply: max(marketSupply(id), 0),
		Demand: max(marketDemand(id), 0), Greed: greed, ZenyNeed: zn, Pressure: pr,
		Seed: b.P.Name + "|" + strconv.Itoa(id)}
	var fresh bool
	in.ListedHour, fresh = listingHours(b.P.Name, id, now)
	p := Ask(in)
	if low := lowestOtherShop(id, b); low > 0 && p > low {
		p = max(low*98/100, in.Floor) // undercut the lowest other shop by 2%
	}
	n, bestW, bestNeed, bestBudget := interestedBuyers(id, ref, b.P.Name)
	urgency := dynClamp01(0.6*zn + 0.4*pr)
	p = Clear(ClearIn{Ask: p, Floor: in.Floor, Ref: ref, Buyers: n, BestWilling: bestW, BestNeed: bestNeed,
		BestBudget: bestBudget, SellerUrgency: urgency})
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
	in := AskIn{Ref: ref, Floor: marketFloor(id, ref), Ceil: 2 * ref, Supply: max(marketSupply(id), 0),
		Demand: max(marketDemand(id), 0), Greed: p.Greed, Seed: p.Name + "|" + strconv.Itoa(id)}
	in.ListedHour, _ = listingHours(p.Name, id, now)
	ask := npcMatCap(id, npcGearCap(id, Ask(in)))
	return npcWindow(id, roundPrice(max(ask, in.Floor)))
}

// wantCap: the most this bot pays for a shop listing of the item when it has an open want for it (flag off: the fixed
// w.Each x 1.1)
func wantCap(b *Bot, w *Want, budget int) int {
	if !dynamicPriceOn() {
		return w.Each * 11 / 10
	}
	return roundPrice(Willingness(priceOf(w.ItemID), dynWantNeed, budget, b.P.Name+"|"+strconv.Itoa(w.ItemID)))
}

// stockBid: what this bot bids for stock for its own shop (flag off: priceOf x 0.9)
func stockBid(b *Bot, id, budget int) int {
	if !dynamicPriceOn() {
		return roundPrice(priceOf(id) * 9 / 10)
	}
	return roundPrice(Willingness(priceOf(id), dynStockNeed, budget, b.P.Name+"|"+strconv.Itoa(id)))
}
