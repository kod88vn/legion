# Pricing audit, 2026-11-15

Scope: how prices are formed in the Go engine (`legion/`) at `main` 7984c90, against EC48 ("prices are never fixed; they
move with supply and demand, with how fast someone is trying to sell, and with how badly a buyer needs the piece and how
much money they have"). Each row says what the number is, where it is, and which force it ignores: scarcity (supply vs
demand), urgency (seller's zeny need, bag pressure, time listed) and budget (buyer's purse and need).

Line numbers are as read at 7984c90. "Flag on" means `dynamic_price_on` (default off, tuning.go) and the code paths in
`pricedyn.go` and `pricemodel.go`. Every flag-on change is behind `dynamicPriceOn()`; with the flag off the old code runs.

## A. Reference prices (the base the model starts from)

| # | Where | What is fixed | Scarcity | Urgency | Budget | Flag on |
|---|---|---|---|---|---|---|
| A1 | economy.go `fixedStandardPrice` | Cards 3,000 default, 500 + mob level x 250 (cap 50,000), boss cards 250,000; gear 800 + MinLv x 150 + Atk x 30 + Def x 500, slots x (10+3s)/10; Etc sell x 2.5 or x 1.2 | no | no | no | kept as the fallback for items with no market data (see H) |
| A2 | economy.go `standardPrice` | NPC-sold items use the NPC buy price | no | no | no | kept as the reference for NPC-sold items (see H) |
| A3 | economy.go `priceOf` | reference = standardPrice blended 30% with the average of the last trades (3+ trades); then the market factor | via B1 | no | no | market factor removed; the trade blend stays as market data |
| A4 | pricing.go `priceRound` (flag `dynamic_prices`, default off) | rarity table: buyers vs copies; boss-only items cost max(median guild wealth, 20,000) | yes | no | yes | used as the reference only when `dynamic_prices` is also on (off by default) |

## B. Market factor, glut and floor (market.go, farm.go)

| # | Where | What is fixed | Scarcity | Urgency | Budget | Flag on |
|---|---|---|---|---|---|---|
| B1 | market.go `marketTick` factor | +4% per tick when wants exceed units, -3% when units are over twice the wants, else drift 5% home; bounds 0.6 to 2.0 | yes | no | partly (a want counts only the units the buyer can pay for) | removed from `priceOf`; scarcity is now the `Ask` model's input (`dynScarcity`); the factor is still computed for the report |
| B2 | market.go `glutMul` | farming value down to 0.1 for a glut (units above 3 x (wants + 10)); 0.7 when a glut line was said on #trade | yes | no | no | still applies to the farming choice (farm.go: what a bot farms) and the count of units listed (vend.go openShop, max 10). Neither is the asking price. Kept on purpose: it steers which item a bot farms (EC34) |
| B3 | market.go `markdown` | 45 min, then 5% per 30 min, at most 6 steps, never under the floor | no | time only | no | replaced by the continuous markdown in `Ask` (same clock: `listingHours` uses the same `market2.md` entries) |
| B4 | market.go `marketFloor` | NPC sell value (sellPrice); half the reference for cards, valuable materials and drop-only gear | no | no | no | kept (see H) |

## C. Sellers (asks and vending)

| # | Where | What is fixed | Flag on |
|---|---|---|---|
| C1 | economy.go `askPrice` | greed 0.85 to 1.15 x personal +-5%, time markdown, NPC window | `askPriceDyn`: `Ask` model (scarcity, greed, time markdown, jitter), then the NPC window (C4) |
| C2 | vend.go `vendPrice` | margin 0.95 + 0.35 x greed x personal, undercut the lowest other shop by 2% (fixed), markdown, NPC window | `vendPriceDyn`: `Ask` (adds seller zeny need and bag pressure), undercut 2%, then `Clear` against the interested buyers |
| C3 | vend.go `openShop` | at most 10 units of a glutted item on the shelf | kept as a listing-count guard (not a price) |
| C4 | economy.go `npcWindow` (EC15) | for an item an NPC sells: 62% to 76% of the NPC buy price | kept as a guard rail (see H) |
| C5 | economy.go `npcGearCap` | used gear an NPC also sells: 93% of the NPC buy price | kept as a guard rail (see H) |
| C6 | dietrich.go `npcMatCap` | Dietrich's price cap for Phracon / Emveretarcon | kept as a guard rail (see H) |

## D. Buyers (bids, wants and purchases)

| # | Where | What is fixed | Flag on |
|---|---|---|---|
| D1 | economy.go `bidPrice` | priceOf x (1.15 - 0.25 x greed), +0.2 for consumables | not used when the flag is on: `wantBid` (pricedyn.go) gives the willingness from the buyer's purse and need |
| D2 | the want-posting sites (alchemy.go 903/936, cards.go 476/507, chatintent.go 260, economy.go 399/424/450/496, guildstock.go 345, huntfit.go 187, refine.go 137, vendbuy.go 467) | all used D1 | all now call `b.wantBid(id)` (one function; flag off = `bidPrice`, byte-identical) |
| D3 | vendbuy.go shop scan | accept a listing at or under the want's Each x 1.1 | `wantCap`: the buyer's willingness from its purse and need |
| D4 | vendbuy.go stock-for-shop bid | priceOf x 0.9 | `stockBid`: willingness from the purse and a stock need of 0.3 |
| D5 | cards.go 540 "urgent" bid | `max(bidPrice x 1.2, lowest ask)` | `max(wantBid x 1.2, lowest ask)`: the same function as D2 |
| D6 | market.go `marketTick` demand | a want counts min(count, zeny / Each) units | kept (the budget already enters demand) |

## E. Other places a price is set

| # | Where | What is fixed | How handled |
|---|---|---|---|
| E1 | chatter.go 238 `trade` chat line | sellPrice x (1.2 to 2.2, random) | flag on: the base is `max(priceOf, sellPrice)`, so the chat line quotes the model's reference |
| E2 | meterapi.go 124 `meterPriceOf` | max(priceOf, sellPrice) | a measurement, not a trade price: follows the model reference automatically |
| E3 | farm.go 49 and 133 | value of a drop = priceOf x glut | kept: farming choice (see B2); priceOf is the model reference when the flag is on |
| E4 | economy.go 1106 `startMailTrade` (mailtrade.go) | the price comes from the deal match (`m.price`) | kept: the match takes the seller's ask and the buyer's want (both model prices when the flag is on). Not verified in a live run |
| E5 | town.go potion buys (pickHPPotion, emergencyStack) | NPC buy prices, fixed by the NPC shop | kept: an NPC sells at its own price; NPC is the last resort (EC47) |
| E6 | guildduty.go, guildpool.go, guildstock.go, lfg_human.go, lootflow.go | priceOf for valuations and stock gaps | follow the model reference (priceOf without the market factor when the flag is on) |

## F. Still missing before this change

1. Seller zeny need, bag pressure: added (`Ask`, flag on).
2. Buyer need: added. Each want stores its need (`Want.Need`, set when the want is posted by `wantNeed`: milestone gap for the gated gear piece, 0.3 otherwise). Probes that post nothing store nothing.

   Buyers' reference has no scarcity term on purpose: a buyer's willingness is its own valuation. Scarcity moves the seller's ask; `Clear` then pulls it toward the buyer. Putting scarcity on both sides would count it twice.

   A buyer counts as interested only if its willingness reaches the seller's floor. A broke buyer with a want does not collapse a thin market (tests: `TestThinMarketBrokeBuyerDoesNotCollapse`, `TestThinMarketSolventBuyerPulls`).
3. Clearing toward the buyer present: added (`Clear`, flag on), using each buyer's posted need and purse.
4. Stock age for cards and gear held in bags: not added (the time-on-market markdown covers listed goods only).

## G. Flag on: what is still fixed, and why

| Constant or rule | Where | Why it stays |
|---|---|---|
| NPC sell value as the floor | `marketFloor` / `sellPrice` = Buy / 2 | Item-db fact (what an NPC pays). Nobody sells under what an NPC pays for the same item; without it a glut would sell junk for 2z. Rule EC15/EC34 |
| Half the reference floor for valuable goods | `marketFloor` (cards, valuable materials, gear no NPC sells) | Guard against a glut or a lone desperate buyer pulling a card to nothing. Constant to tune with metrics |
| NPC buy price as the reference for NPC-sold items | `standardPrice` / `fixedStandardPrice` (A2) | Item-db reference: the NPC shop sells at this price, so the reference is the NPC price. The model moves the player price inside the window below |
| NPC window 62% to 76% of the NPC buy price | `npcWindow` (C4) | Owner rule EC15: goods are cheaper than the NPC and the seller gets more than the NPC pays. A rule of the owner, not a market price. Guard rail |
| Used gear at 93% of the NPC buy price | `npcGearCap` (C5) | Measured in batch 18: asks above the NPC price for used gear got no buyers. Guard rail |
| Dietrich's price cap | `npcMatCap` (C6) | The NPC sells Phracon / Emveretarcon at a fixed price, buyers never pay over it. Guard rail |
| Fallback formula (fixedStandardPrice) | A1 | Used only where the item has no market data (no trade, no NPC price). Its constants (500 + 250 x level, 800 + 150 x MinLv, ...) are reference values, not asks |
| Dynamic rarity table | A4 (`dynamic_prices`, off) | Its own flag; not used when `dynamic_price_on` is set unless `dynamic_prices` is also on |
| Glut (farming value, listing count) | B2, C3, E3 | Steers which item a bot farms (EC34). It does not set the asking price. Owner-visible through metrics |
| Trade blend 30% after 3 trades | A3 | Market data, not a fixed price. Kept |

## H. Constants that need metrics before anyone calls the flag better

- Zeny at which a seller is in no need: 20,000 (`dynZenyFullNeed`, zeny need = 1 - zeny/20,000).
- Urgency weights: 0.30 for zeny need, 0.15 for bag pressure, combined as 0.6 x need + 0.4 x pressure in the clearing step.
- Scarcity band: sqrt((demand+1)/(supply+1)) clamped to [0.6, 1.6].
- Markdown: 45 min free, 5% per 30 min, 6 steps (kept from market.go, never measured against sales).
- Jitter band: 0.97 to 1.03.
- Willingness: 0.8 + 0.3 x need, capped at the purse.
- Buyer need: generic 0.3 (`dynGenericNeed`); want 0.6 (`dynWantNeed`) in the fallback; stock 0.3; milestone need 0.5 + 0.5 x gap/price.
- Concession: 0.5 x seller urgency + need x (1 / (1 + (buyers-1)/2)), clamped to 1.
- Undercut: 2% under the lowest other shop.
- Floor for valuable goods: half the reference.

Every one of these is a first guess. Measure zeny per hour and kills per minute (Article 3.2) with the flag off, then on in
a test party, before any of them is changed.
