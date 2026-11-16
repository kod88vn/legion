package main

// pricemodel.go (EC48, tuning dynamic_price_on, default off). Owner rule EC48: "Prices are never fixed: they move with
// supply and demand, with how fast someone is trying to sell, and with how badly a buyer needs the piece and how much
// money they have. A seller in a hurry, or a buyer short of money and desperate for gear that is on the market, pulls
// the price down so it sells sooner."
//
// Every function here is pure: no bot, no lock, no global read. The callers (vend.go vendPriceDyn, vendbuy.go,
// economy.go askPriceDyn) read the market and the bot and pass the numbers in, so the rules are pinned by tests.
//
//   Ask (seller's posted price, Ask):
//     ask = ref x scarcity x greed x urgency x markdown x jitter, clamped to [floor, ceil]
//       ref        reference value (priceOf: item db / market standard)
//       scarcity   sqrt((demand+1)/(supply+1)), clamped to [0.6, 1.6]: more buyers than units is dearer
//       greed      0.85 + 0.3 x greed, the seller's margin (as askPrice / vendPrice do today)
//       urgency    1 - 0.30 x zenyNeed - 0.15 x pressure, clamped to [0.5, 1]: a seller in a hurry lowers the price
//                  (zenyNeed: the seller is poor; pressure: the bags/cart are nearly full)
//       markdown   0.95^steps, steps = 0 under 45 min listed, then one per 30 min, at most 6 (the old market.go rule,
//                  now continuous in time instead of a stored step counter)
//       jitter     0.97..1.03, fixed per seller and item (fnv of the seed), so two sellers never post the same number
//     floor = the NPC sell value / marketFloor; ceil = 2 x ref (vendPrice and npcWindow still apply the NPC window)
//
//   Willingness (buyer's most, Willingness):
//     w = min(ref x (0.8 + 0.3 x need) x jitter, budget)
//       need    0..1: how badly the buyer needs the piece (a milestone gap, a build that stops without it)
//       budget  zeny the buyer can spend now (its purse less its reserve)
//
//   Clear (price the posted listing is pulled to when a buyer is interested, Clear):
//     if no buyer or the best buyer already pays the ask, the ask stands.
//     otherwise the seller concedes part of the gap between ask and that buyer's willingness:
//       concession = clamp(0.5 x sellerUrgency + 0.5 x desperation, 0, 1)
//       desperation = need x poverty x single,  poverty = 1 - budget/ref (0..1),  single = 1/(1 + (buyers-1)/2)
//       price = max(floor, ask - gap x concession)
//     So a poor, desperate buyer who is the only one interested pulls the price down, and a seller in a hurry does too.
//
// Flag off: none of these functions is called (vend.go / vendbuy.go / economy.go keep their fixed code paths).

import (
	"fmt"
	"hash/fnv"
	"math"
)

const (
	dynMarkdownFreeHours = 0.75 // 45 min before the first step (as market.go markdown)
	dynMarkdownStepHours = 0.5  // 30 min per step
	dynMarkdownMaxSteps  = 6
	dynMarkdownFactor    = 0.95
	dynZenyFullNeed      = 20000 // a seller holding this much zeny is not in need of a sale
)

// dynMarkdownSteps: how many 5% markdown steps an item listed hours ago has taken
func dynMarkdownSteps(hours float64) int {
	if hours < dynMarkdownFreeHours {
		return 0
	}
	return min(dynMarkdownMaxSteps, 1+int((hours-dynMarkdownFreeHours)/dynMarkdownStepHours))
}

// dynJitter: a deterministic 0.97..1.03 per seed (a seller or buyer name plus the item id)
func dynJitter(seed string) float64 {
	h := fnv.New32a()
	fmt.Fprint(h, seed)
	return 0.97 + 0.06*float64(h.Sum32()%1000)/1000
}

// dynClamp01 clamps v to [0, 1]
func dynClamp01(v float64) float64 {
	return math.Max(0, math.Min(1, v))
}

// dynZenyNeed: 0 (rich, no hurry) .. 1 (broke, needs the sale). Zeny at or above dynZenyFullNeed means no need.
func dynZenyNeed(zeny int) float64 {
	return dynClamp01(1 - float64(zeny)/dynZenyFullNeed)
}

// dynPressure: 0 while the bags are at half weight or less, 1 when full. max is the max weight in server units.
func dynPressure(weight, maxWeight int) float64 {
	if maxWeight <= 0 {
		return 0
	}
	return dynClamp01((float64(weight)/float64(maxWeight) - 0.5) * 2)
}

// dynScarcity: >1 when buyers outnumber the units on offer. Clamped to [0.6, 1.6].
func dynScarcity(supply, demand int) float64 {
	s := math.Sqrt(float64(max(demand, 0)+1) / float64(max(supply, 0)+1))
	return math.Max(0.6, math.Min(1.6, s))
}

// AskIn: the inputs of the seller's posted price
type AskIn struct {
	Ref        int     // reference value (priceOf)
	Floor      int     // lowest ask (marketFloor: NPC sell value, half the reference for valuable goods)
	Ceil       int     // highest ask
	Supply     int     // units on offer (marketSupply, 0 if unknown)
	Demand     int     // units buyers want and can pay for (marketDemand)
	Greed      float64 // the seller's persona greed 0..1
	ZenyNeed   float64 // dynZenyNeed of the seller
	Pressure   float64 // dynPressure of the seller's bags
	ListedHour float64 // hours the item has been on the market at this price
	Seed       string  // seller name + "|" + item id
}

// Ask: the seller's posted price (see the header). Always within [Floor, Ceil] when Floor <= Ceil.
func Ask(in AskIn) int {
	if in.Ref <= 0 {
		return 0
	}
	urgency := math.Max(0.5, math.Min(1, 1-0.30*dynClamp01(in.ZenyNeed)-0.15*dynClamp01(in.Pressure)))
	markdown := math.Pow(dynMarkdownFactor, float64(dynMarkdownSteps(in.ListedHour)))
	greed := 0.85 + 0.3*dynClamp01(in.Greed)
	p := float64(in.Ref) * dynScarcity(in.Supply, in.Demand) * greed * urgency * markdown * dynJitter(in.Seed)
	ceil := in.Ceil
	if ceil <= 0 {
		ceil = 2 * in.Ref
	}
	return clampInt(int(math.Round(p)), in.Floor, max(ceil, in.Floor))
}

// Willingness: the most a buyer pays for an item (see the header). 0 when the budget is 0.
func Willingness(ref int, need float64, budget int, seed string) int {
	if ref <= 0 || budget <= 0 {
		return 0
	}
	w := float64(ref) * (0.8 + 0.3*dynClamp01(need)) * dynJitter(seed)
	return min(int(math.Round(w)), budget)
}

// ClearIn: the posted ask and the best interested buyer
type ClearIn struct {
	Ask           int     // the seller's posted price (Ask)
	Floor         int     // lowest price the seller takes
	Ref           int     // reference value (for the buyer's poverty)
	Buyers        int     // interested buyers (0: nothing to pull toward)
	BestWilling   int     // the most the best buyer pays (Willingness)
	BestNeed      float64 // that buyer's need 0..1
	BestBudget    int     // that buyer's spendable zeny
	SellerUrgency float64 // 0 (no hurry) .. 1 (must sell now): dynZenyNeed and dynPressure combined by the caller
}

// Clear: the price a listing is pulled to by the interested buyers (see the header). Never above the ask, never
// below the floor.
func Clear(in ClearIn) int {
	if in.Buyers <= 0 || in.BestWilling >= in.Ask || in.Ask <= 0 {
		return in.Ask
	}
	gap := float64(in.Ask - in.BestWilling)
	poverty := 0.0
	if in.Ref > 0 {
		poverty = dynClamp01(1 - float64(in.BestBudget)/float64(in.Ref))
	}
	single := 1 / (1 + float64(in.Buyers-1)/2)
	desperation := dynClamp01(in.BestNeed) * poverty * single
	concession := dynClamp01(0.5*dynClamp01(in.SellerUrgency) + 0.5*desperation)
	return clampInt(int(math.Round(float64(in.Ask)-gap*concession)), in.Floor, in.Ask)
}

func clampInt(v, lo, hi int) int {
	if hi < lo {
		hi = lo
	}
	return min(max(v, lo), hi)
}
