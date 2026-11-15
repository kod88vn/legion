package main

// marketflow.go (economy run 11). Owner rule 2026-11-14 (RULES.md 2 "Abundant and cheap; the Prontera market beats
// the NPCs"): bots buy from Prontera vendors before NPC shops and sell through the market before NPCs. These counters
// show whether it happens: zeny and items bought from vendors vs NPC shops, sold on the market (vendor sales, trades,
// mail trades) vs to NPCs, and per item class the average vendor listing price against the NPC price.
// /api/economy "market_flow".

import (
	"sync"
)

var marketFlow = struct {
	sync.Mutex
	zeny map[string]int64 // npc_buy / vendor_buy / npc_sell / market_sell -> zeny
	n    map[string]int64 // ... -> events
}{zeny: map[string]int64{}, n: map[string]int64{}}

// noteMarketFlow: one purchase or sale (kind npc_buy, vendor_buy, npc_sell, market_sell)
func noteMarketFlow(kind string, zeny, itemID, count int) {
	if zeny <= 0 {
		return
	}
	marketFlow.Lock()
	marketFlow.zeny[kind] += int64(zeny)
	marketFlow.n[kind]++
	marketFlow.Unlock()
}

// marketFlowReport: shares of zeny through the market, and vendor price / NPC price by item class from open shops
func marketFlowReport() map[string]interface{} {
	marketFlow.Lock()
	z := map[string]int64{}
	n := map[string]int64{}
	for k, v := range marketFlow.zeny {
		z[k] = v
	}
	for k, v := range marketFlow.n {
		n[k] = v
	}
	marketFlow.Unlock()
	pct := func(a, b int64) float64 {
		if a+b == 0 {
			return 0
		}
		return float64(int(float64(a)*1000/float64(a+b))) / 10
	}
	// vendor price vs NPC price, per item class, over the open shops' listings of NPC-sold items
	type acc struct {
		sum float64
		n   int
	}
	byClass := map[string]*acc{}
	for _, l := range allShopListings() {
		x := items[l.ItemID]
		if x == nil || x.Buy <= 0 || l.Price <= 0 || len(gearShops(l.ItemID)) == 0 {
			continue
		}
		a := byClass[x.Type]
		if a == nil {
			a = &acc{}
			byClass[x.Type] = a
		}
		a.sum += float64(l.Price) / float64(x.Buy)
		a.n++
	}
	ratio := map[string]float64{}
	for k, a := range byClass {
		ratio[k] = float64(int(a.sum/float64(a.n)*100)) / 100
	}
	return map[string]interface{}{
		"zeny":                      z,
		"count":                     n,
		"buy_from_vendors_pct":      pct(z["vendor_buy"], z["npc_buy"]),
		"sell_on_market_pct":        pct(z["market_sell"], z["npc_sell"]),
		"vendor_vs_npc_price_ratio": ratio, // 1.0 = the NPC price; the rule wants 0.62-0.76
	}
}

// allShopListings: every open shop's listing (takes econ.mu)
func allShopListings() []vendListing {
	econ.mu.Lock()
	defer econ.mu.Unlock()
	var out []vendListing
	for _, sh := range econ.shops {
		out = append(out, sh.items...)
	}
	return out
}
