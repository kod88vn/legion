# Economy engineer: restart notes (2026-11-14 ~16:45 EST, end of runs 10-11)

Standing role: economy engineer. Heartbeat every cycle with `python tools/legionctl.py think econ "..."`.
Don't run go vet/test or image builds: hand "code ready" to the deploy agent. Never post into the minds' order
inboxes (that channel is the owner's); send what a mind should know to main. Never send commands to Ayla.

## Code in the tree (written by me, NOT yet vetted or tested; the deploy agent has the file lists)
Run 10, in HANDOVER 6e "Run 10":
- `lootflow.go` is the loot flow: own use -> party -> guild -> guild-trader stock -> sell. `/api/economy` shows it as `loot_flow`.
- `econpersist.go` keeps the economy counters across deploys in `data/econ_counters.json`; `/api/economy` adds `counters_since` and `deal_fail_reasons`.
- `econrun10_test.go` holds the new tests.
- `alchemy.go`:
  - Yellow Herbs are now in the alchemist's herb want (Blue/White skipped when fewer than 5 are on offer).
  - `brewSupplyNeed` / `alchSupplyTrip`: a "brew supplies" errand fetches bottles and Scell.
- `errand.go` and `humanparty.go` are wired to the brew-supplies errand.
- `market.go`: `marketSupply` and `marketDemand`.
- `chatter.go`: no typos or truncation in trade lines; slang replaces whole words only.
- `economy.go`:
  - an agreed deal holds the bot online past its session end (`dealOpen`);
  - a delivery follows the buyer when it changes map;
  - in a same-map deal with a solo partner, the solo side walks over.
- `vend.go`: the shop title is checked against what is actually listed (`titleMatchesListing`), and shops close and restock less often.
- `vendbuy.go`: one vendor delivery per 40 minutes.
- `guildstock.go`: the guild trader stays the same while its shop is open, and `stock_by_shop` is shown.
- `junkgear.go`:
  - cheap 4-slot gear counts as junk unless the market asks for it;
  - an unidentified piece the bot's build wants is kept once.
- `parcel.go`: items still in the loot flow are not mailed to vendors.
- `ai.go` `logoutSignal`: waits up to 15 minutes while a deal is open.

Run 11:
- `alchemy.go`: character levels are saved to `data/char_levels.json` (I seeded it from the DB with 5,992 characters), and the guild's designated Alchemist stays online. Cause: guilds 01/02/04/08/09/10 showed their real lv58-70 Alchemists as "Novice 1, offline".
- `npcextended.go`: bots buy Blue Gemstones (for Magnus / Safety Wall / Warp Portal / Sage skills) and one Yggdrasil Leaf from the stock "Extended" Tool Dealers.
- `town.go`:
  - calls the gemstone/leaf buys;
  - on a Prontera trip, skips NPC HP potions when a vendor sells them for under 95% of the NPC price;
  - feeds the market_flow counters.
- `opportunity.go`: the "none in the shops" chatter skips anything an NPC shop sells.
- `economy.go`:
  - `npcWindow` keeps player prices for NPC-sold items at 62-76% of the NPC buy price (asks, vendor listings, deal prices);
  - adds `market_flow` to `/api/economy`.
- `marketflow.go` counts purchases (vendor vs NPC) and sales (market vs NPC), and compares vendor prices with NPC prices per item class.
- `vendbuy.go`: the shop browse prefers the potions the trip skipped at the NPC, buys wings cheaper than the NPC, and feeds the counters.
- `mailtrade.go`: feeds the counters.
- New tool, run once: `tools/pairchars.py --apply` was **still running at 16:45** (creating 1,547 DS Hunters and 284 merchants at lv1 on bot accounts, with a backup of personas.json). Check it finished:
  - `SELECT COUNT(*) FROM char WHERE char_id BETWEEN 210000 AND 214999` should be about 1547;
  - personas.json should contain the "Bow"/"Shot" Hunters.
  - If it was interrupted, re-run it: it only adds what is missing, but check for half-inserted chars (char rows without a personas entry).

## Next to build (approved by main)
1. **Farmer cycle.** The draft is `RESTART-NOTES/economy-farmer.go.staged`; copy it to `legion/farmer.go` once the freeze lifts. It needs:
   - Bot fields: `swapChar string`, `storageSeen map[int]*InvItem`, `farmStore`, `farmFetch bool`;
   - `persona.go` `pickChar`: use `swapChar` first (then clear it), then `farmerPick()`;
   - `bot.go` run loop: when the session ended through `swapChar`, skip `yieldSlot` and log straight back in;
   - `reset.go` `resetStorageItem`: call `b.noteStorageItem(idx, it)` first;
   - `storage.go` `onStorageOpen`: `if b.farmerStorage() { return }` after the reset check;
   - `ai.go` `think`: `if b.farmerThink(gm, moving, now) { return }` before the vendor `vendThink` block (~line 303);
   - `lfg.go` candidate filter (~line 1128): `&& !b.farmerChar()`;
   - `/api/economy`: add `"farmers": farmersReport(allBots)`;
   - `etcHasDemand` (item 2).
   - The cycle: the Hunter farms solo for 60-120 minutes on a market-gap money goal, stores its sellables in Kafra storage, and the account swaps to its merchant. The merchant takes the goods from a Prontera Kafra straight into its cart, vends for 20-40 minutes, and swaps back. Merchants that can't vend yet: the Hunter keeps farming and uses the loot flow.
2. **Etc loot to the market** (main asked): `etcHasDemand(id)` = `marketDemand(id) > 0 && !glutted(id)`. Make `npcJunk` exclude those items, and have `econSpare` offer them (Etc with demand); vendors already list non-junk Etc. Split the `market_flow` sales by item type.
3. Owner economy rules not yet fully covered:
   - "Prontera is full of gear": farmers supply it; measure `gear_listings_by_band`.
   - Farmer counters.
   - Persisting the `market_flow` counters.

## Measurements pending (after the run 10/11 deploy)
- Brewing: brews per alchemist (`/api/alchemists`), brew-supplies errands, herb parcels.
- `loot_flow` counters.
- `deal_fail_reasons`: same-map failures should drop; it was 19 failed vs 12 done.
- Shop titles match their listings.
- Unidentified [4] gear carried (DB: Knife[4] 581, Club[4] 317, Sword[4] 242 copies).
- `market_flow` percentages: vendor vs NPC buys, market vs NPC sells, price ratio 0.62-0.76.
- Guilds 01/02/04/08/09/10 alchemists online with real levels.
- Income per band (picklog, 20-min window per bot-hour x3, before): lv45-59 NPC sell 4.6k / NPC buy 5.1k; lv60-79 7.4k / 5.8k.
- Measured after the 14:53 deploy: 233 NPC gear buys in 13 minutes, 0 Yuno trips, gear_next stable (788 of 873).

Scripts are in the session scratchpad (`ec9_*.py`, `ec10_*`): `ec9_aff.py` (gear_next snapshot), `ec9_band.py` + `ec8_pl2.sql` (picklog income), `ec9_alch.py` (alchemists).
