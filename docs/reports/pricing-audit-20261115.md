# Pricing audit, 2026-11-15

Scope: how prices are formed in the Go engine at `main` 7984c90, against EC48 ("prices are never fixed; they move with
supply and demand, with how fast someone is trying to sell, and with how badly a buyer needs the piece and how much money
they have"). Each row says what the number is, where it is, and which of the three forces it ignores: scarcity (supply vs
demand), urgency (seller's zeny need, stock age, bag pressure, time listed) and budget (buyer's purse and need).

Files are in `legion/` (the Go engine). Line numbers are as read at 7984c90.

## A. Reference prices (what everything else is based on)

| # | Where | What is fixed | Scarcity | Urgency | Budget |
|---|---|---|---|---|---|
| A1 | economy.go `fixedStandardPrice` | Cards 3,000 default, 500 + mob level x 250 (cap 50,000), boss cards 250,000; gear 800 + MinLv x 150 + Atk x 30 + Def x 500, slots x (10+3s)/10; Etc sell x 2.5 or x 1.2 | no | no | no |
| A2 | economy.go `standardPrice` | NPC-sold items use the NPC buy price, always | no | no | no |
| A3 | economy.go `priceOf` | reference = standardPrice blended 30% with the average of the last trades (3+ trades); market factor 0.6 to 2.0 (1.0 ceiling for NPC goods) | yes (through A7) | no | no |
| A4 | pricing.go `priceRound` (flag `dynamic_prices`, default off) | rarity table: buyers vs copies in circulation; boss-only items cost max(median guild wealth, 20,000) | yes | no | yes (purses, not urgency) |

## B. Market factor and markdown (market.go)

| # | Where | What is fixed | Scarcity | Urgency | Budget |
|---|---|---|---|---|---|
| B1 | market.go `marketTick` | factor +4% per tick when wants exceed units, -3% when units are more than twice the wants, else drift 5% home; bounds 0.6 to 2.0 | yes | no | partly (a want counts only as many units as the buyer can pay for) |
| B2 | market.go `glutMul` | farming value down to 0.1 for a glut (units above 3 x (wants + 10)) | yes | no | no |
| B3 | market.go `markdown` | 45 min, then 5% per 30 min, at most 6 steps, never under `marketFloor` | no | time only | no |
| B4 | market.go `marketFloor` | NPC sell value; half the reference for cards, valuable materials and drop-only gear | no | no | no |

B3 is the only urgency rule that exists, and it is time only: it does not look at the seller's zeny, the bags, or how
much stock is left. It also keeps its own step counter, so a restart of the clock is possible.

## C. Sellers (asks and vending)

| # | Where | What is fixed | Scarcity | Urgency | Budget |
|---|---|---|---|---|---|
| C1 | economy.go `askPrice` (holdings for #trade, alchemy.go) | greed 0.85 to 1.15 (x) personal +-5% per persona and item, then B3 markdown, then the NPC window | via priceOf | time only | no |
| C2 | vend.go `vendPrice` | margin 0.95 + 0.35 x greed (or the market mind's markup) x personal +-5%; undercut the lowest other shop by 2% (fixed); B3 markdown; NPC window | via priceOf | time only | no |
| C3 | vend.go `openShop` | at most 10 units of a glutted item on the shelf (fixed count) | yes | no | no |
| C4 | economy.go `npcWindow` (EC15) | for an item an NPC sells: between 62% and 76% of the NPC buy price (fixed band; a rule of the owner) | no | no | no |
| C5 | economy.go `npcGearCap` | used gear an NPC also sells: 93% of the NPC buy price (fixed) | no | no | no |
| C6 | dietrich.go `npcMatCap` | Dietrich's price cap for Phracon / Emveretarcon (fixed) | no | no | no |

No seller-side rule uses the seller's zeny need or the bag pressure. `vendPrice` and `askPrice` do not know who the buyer is.

## D. Buyers (bids and purchases)

| # | Where | What is fixed | Scarcity | Urgency | Budget |
|---|---|---|---|---|---|
| D1 | economy.go `bidPrice` | priceOf x (1.15 - 0.25 x greed), +0.2 for consumables; capped at the Dietrich price for refine materials | via priceOf | no | no |
| D2 | ~15 want-posting call sites (economy.go 391-488, cards.go 476/507/540, chatintent.go 260, alchemy.go 903/936, refine.go 137, huntfit.go 187, guildstock.go 345) | all of them use D1 | via priceOf | no | no |
| D3 | vendbuy.go shop scan | accept a listing at or under the want's Each x 1.1 (fixed); spare potions at the want's Each; cards at bidPrice x 1.2; gear at priceOf x 1.2 | via priceOf | no | the purse only: listings over `budget` are skipped |
| D4 | vendbuy.go stock-for-shop bid | priceOf x 0.9 (fixed), quantity min(3, budget / bid, gap) | via priceOf | no | yes (quantity only) |
| D5 | cards.go 540 | "urgent" bid = max(bidPrice x 1.2, lowest ask), fixed multiplier | via priceOf | a fixed flag, not measured | no |
| D6 | market.go `marketTick` demand | a want counts min(count, buyer's zeny / Each) units | yes | no | yes (count only) |

The buyer side has no need term at all. A bot short of money for a build piece bids exactly what a rich bot bids
(D1 uses greed only). The purse caps how many units are bought, not how much is offered.

## E. Other places a price is set

| # | Where | What is fixed | Note |
|---|---|---|---|
| E1 | chatter.go 238 | chat offers of sellPrice x (1.2 to 2.2, random) | Not market-based. Chat text only, but it is a price a human reads. |
| E2 | meterapi.go 124 `meterPriceOf` | max(priceOf, sellPrice) | Used by the meter's zeny value; follows A3. |
| E3 | farm.go 49 and 133 | value of a drop = priceOf x glut (down to 0.1) | Follows B2; farming choice is driven by these fixed values. |
| E4 | mailtrade.go 44 `startMailTrade` | the price is passed in by the caller (not read in this audit) | Not audited in detail; the caller's formula is unknown here. |
| E5 | town.go (potion buys, pickHPPotion, emergencyStack) | NPC buy prices, fixed | NPC is the last resort (EC47); fixed by the NPC shop. |
| E6 | guildduty / guildpool / lfg_human | priceOf for trades and guild valuations | Follow A3. |

## F. What is missing entirely

1. Seller zeny need. Nothing reads how badly a seller needs the money.
2. Bag / inventory pressure on a price. Only the vendor's cart space limits what is listed.
3. Buyer need (milestone gap) in a bid. The gate price in buildmilestones.go `gatedPrice` exists, but a want does not carry it.
4. A clearing rule. A posted price is never pulled toward the buyer who is actually there; a poor desperate buyer
   and a rich buyer see the same listing.
5. Time on the market as a price input other than the 5% steps (B3): no stock-age term for cards or gear held in bags.

## G. Result of the audit

With `dynamic_price_on` off (the shipped default) every row above is what the engine does. With it on, the following
replace the fixed code (see `legion/pricedyn.go` and `legion/pricemodel.go`):

- C2 `vendPrice` -> `vendPriceDyn` (vend listing, undercut and markdown in one function, supply/demand from B1, seller
  zeny need and bag pressure, and the buyer clearing step).
- C1 `askPrice` -> `askPriceDyn` (persona ask, time-based markdown).
- B3 `markdown` steps -> continuous markdown in the ask model (the same 45-min / 30-min clock; `listingHours` keeps the
  same `market2.md` entries, so `marketSold` and the want reset in `marketTick` still work).
- D3 shop-listing acceptance `w.Each*11/10` -> `wantCap` (a buyer's willingness from its purse and need).
- D4 stock bid `priceOf*9/10` -> `stockBid` (willingness from the purse and a stock need of 0.3).

Still fixed with the flag on (not replaced): A1, A2, A4 (the table is off unless `dynamic_prices` is set), B1, B2, B4,
C4, C5, C6, D1 and the want-posting call sites in D2 (so a want's `Each` stays at the old bid until those move too), D5,
E1 to E6. These are listed so the owner can see what the model does not yet touch.
