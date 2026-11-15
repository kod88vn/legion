<!-- Moved unchanged from HANDOVER.md lines 838-1243 (as of commit 63ca923). Index: HANDOVER.md. -->

## 6e. Economy (2026-11-14, economy engineer; `economy.go`, `vend.go`, `vendbuy.go`, `alchemy.go`)
- Owner: "the economy is the core of the game; bots develop naturally". No items are added to NPC shops (no NPC Blue
  Potion / Grape Juice; Blue Syrup 7,000z is the only NPC SP potion). Potions come from guild alchemists.
- Player trades (economy.go): price standard `priceOf`, B>/S> on #trade, econManager matches every 15 s, trade window
  with verification. Measured: works (2 trades, 0 failed in an hour), volume limited by supply (spare goods are cards
  and starter gear; wants are mostly NPC gear). Additions: `Want.Partial` (any amount), guild mates trade at 70% of
  ask, item gifts (price 0: the guild alchemist gives up to 5 potions per 30 min to its guild leader's party, served
  first), wants nobody has in person are matched against open vending shops.
- Vending (vend.go): vendor characters learn Enlarge Weight 5 -> Pushcart 3 -> Vending 3 first (builds.go
  `vendorFirst`; before, 42 of 43 had Mammonite and no Vending, so 0 shops ever opened). Prices = priceOf x vendor
  margin (0.95-1.3, market mind `--markup` multiplies the standard). Carts list loot, cards, spare gear, brewed potions.
- Buying from shops (vendbuy.go): 0x0130 -> 0x0b3d (64-byte entries for 20210107) -> 0x0801; vendor side 0x09e5.
  `/api/economy` has `vend_sales`, `vend_sales_zeny`, `shops` (open shops with listing and sales).
- Alchemists (alchemy.go): `alchemyManager` picks per guild (joined guild, else persona guild) the most advanced
  character with plan "alchemist" (levels of all chars from the char list); it is played more (pickChar +0.6). As an
  Alchemist it learns Potion Research 5 -> Pharmacy 5 first, buys the Potion Creation Guide (100k) and Medicine Bowls
  at the Guild Dealer (alde_alche 24,188, dialog + 0x0142/0x0143), bottles + Scell at the south gate Goods Merchant
  (trip "alchemy"), asks for Blue/White Herbs (B>), brews via 0x0438(228) -> 0x018d -> 0x018e -> 0x018f. Herbs are
  offered to alchemists and never NPC-sold; brewed potions are never NPC-sold. `GET /api/alchemists` (+ `potions`),
  legionctl `alchemists`, `potions`, `economy`. UNVERIFIED live: no character is an Alchemist yet (best Merchant job ~20).
- Zeny: bots only lose money (13.1M total vs 13.5M seeded; lv 30-44 avg ~800z). EXP 10x but drops 1x. Raising
  `item_rate_*` in rathena battle_conf is an owner decision (needs a rathena restart, kicks Ayla).
- Live after the 00:30 deploy (2026-11-14): 19 shops opened at the south gate within 12 min (48/72 vendors had
  Vending) but listed only NPC junk (Jellopy 3z) -> 0 sales; 0 item trades; party zeny gifts 61 ok / 51 "trade
  request not answered".
- Batch 2 (cards.go, lootvalue.go; built 00:50, deploy pending): card compounding (a card that suits the build goes
  into a free slot of worn gear: 0x00ab unequip -> 0x017c -> 0x017d; card value = item_db script bonuses x build stat
  goals), card wants for free slots, bare wants (no weapon / no body armor -> cheapest usable item on the board),
  item variants (a want accepts Knife 1202 for 1201 etc.; `wantAccepts`), card standard 500 + mob lv x 250 (was
  20k+; 1,000 + lv x 400 in batch 2 still priced cards out of the 1-3k purses), slotted gear priced from its NPC namesake +25%/extra slot (`namesakePrice`), vendors stock cards/materials/
  gear/potions only (canVend: 2+ items worth 1,500z), every drop looted (the Greed roll is gone), broke bots
  (`shortOfZeny`: < 500 + lv x 80) prefer the guide map with the best NPC loot per kill (`mapLootZeny`, mob_db drops),
  market partners' trade requests accepted at once. /api/economy: `gear_gifts`, `cards_inserted`.
- Batch 2 went live at 00:44 (4 cards compounded in the first minutes). Batch 3 (built ~01:05): cardMob picks the
  lowest-level non-boss of a name (a lv99 event "Poring" made Poring Card 40k); vendors move NPC junk from the cart
  back to the bag (0x0127, bot.go handles 0x0125) and openShop lists only `vendable` items; "cheaper than NPC" wants
  only when the item or a variant is on the offers board (`onBoard`; they were 114 of 116 wants, all unfillable).
- Batch 4 (deployed 01:12 as image 18d7406b): never sell unidentified gear; a "no weapon" want is blocked by any usable
  weapon in the bag (acolytes bought Club after Club the server refused to equip: identify flag); slot wants (a card
  holder buys a slotted variant of its worn gear, compounds and wears it); failed inserts mark the slot; market trade
  approach checks `sameWalkRegion` (nav.go), the alchemy dealer walk uses `talkCell`. Live 01:15-01:30: shops list
  cards/materials (Lunatic Card 3k, Chonchon 1.4k, Phracon 300); trades: Hornet Card 1,000z x2, 10 Red Potions 110z.
- Built after (01:30, not deployed): card mob level ignores lv0 copies (Hornet/Chonchon priced 1,000 = lv0 copy);
  card wants skip a card already asked by 2x its sellers (25 bots wanted the same 2 Hornet Cards).
- Limits seen: party members trade on their own map only and cross-map meets need 5,000z+ (Kafra fees), so card
  supply scattered over maps rarely meets demand; vend buying is for solo bots (crossOK) or bots already in Prontera.
- Batch 5 (built ~01:50): guild fund (alchemy.go `guildFund`/`needsGuide`): the guild alchemist (Merchant job 30+ or
  Alchemist, no guide) gets up to 3,000z from a free guild mate on its map with 2,500z+ (once per donor per hour, a
  market Deal with ItemID 0 and Why "guild fund"; /api/economy `guild_fund_gifts`). Open shops are on the offers
  board (wants can target shop goods). A vendor-role alchemist receives while not vending (econSnap.idle).
- Batch 6 (deployed 02:04, dbf67c29): `match_misses` in /api/economy (per want: "no seller", "price too high", "too
  far / party", "buyer short of zeny"; "partner busy / not safe at start"; "deal ended: <reason>"). Cause of 0 trades
  01:33-02:04: partyTradeOK (death fix: a party member trades only within 10 cells) was not applied by the matcher,
  which kept picking far holders that startDeal refused. Now the matcher applies it, and delivery deals
  (`Deal.Deliver`) let a solo seller (crossOK) travel to a party member's map (item >= 500z, <= 3 maps unless
  >= 3,000z); the buyer keeps playing until the seller is within 8 cells. First data (9 min): no seller 373 (mostly
  SP potion wants: nobody brews yet), too far/party 49, price 14; guild fund 6 gifts (1.5-2.3k).
- Batch 7 (built 02:25): guild alchemist picks persist in data/alchemists.json (main instance writes) and change only
  for a whole tier (the 02:04 restart re-picked 5 guilds, scattering the fund); gearReserve keeps a saving alchemist's
  money (zeny > 3k: all but 1k) from gear purchases and party gifts.
- Batch 8 (built 02:42, not deployed): the 02:35 deploy still re-picked 3 alchemists, because after a restart a
  character's levels are only the seed (Novice) until its account logs in, so any logged-in Merchant looked "a tier
  further". The current pick is now kept until its own levels are seen. data/alchemists.json was hand-restored for
  guild05-07 (Lily_dancer holds 1.5k of fund), but the running 02:35 instance may rewrite it: re-check after deploy.
- Batch 9 (built 02:51, economy engineer 3): (a) `browseShops` (vendbuy.go, called from town.go trip "done"): a
  non-vendor bot whose town trip ends in Prontera (solo or on the party run) looks over the open shops (econ.shops,
  refreshed every round) and buys ONE thing at a fair price: its #trade want (<= bid +10%), SP potions, a card for a
  free slot (cardValue >= 2, <= bid +20%), or a gear upgrade (<= standard +20%); 3-min shopping task, "brb buying X"
  in party chat. (b) Stockers: a vendor with Vending, not vending, cart < 3 items, lv15+, 1.5k+ zeny buys spare cards /
  valuable materials / slotted or drop-only gear at 90% of the standard (`stockWant`, "stock for my shop"; resells at
  95-130%). (c) Fetch deals (`Deal.Fetch`): the mirror of delivery - a solo buyer travels to the party member's map
  (follows it if the party moves), the party member keeps playing until the buyer is within 8 cells. (d) partyTradeOK:
  inside a town map party members trade at any distance. (e) /api/economy: `counts` (deal started/done/failed by kind
  same/meet/deliver/fetch/gift/town, stock-*; shop buy started/results; "shop browse: nothing for me"),
  `zeny_by_level`, and match_misses "too far: <which rule>".
- Batch 9 went live 03:35: first vendor sale of the day within 2 min (Lunatic Card 1,450z to a bot on a town run);
  4 shop sales (4,350z) by 03:41, but "shop browse: nothing for me" 25 vs 4 bought.
- Batch 9b (built ~03:40, deploy ~04:35; economy.go matcher): a delivery seller short of Kafra money (not crossOK,
  i.e. < 1,000z) still walks a delivery when it is lv15+ and the buyer's map is <= 3 hops away (median purse lv20-39 is
  ~600z, so most solo sellers could never deliver); and no delivery into a map too strong for the seller
  (`mapTooStrong(buyer.mapN, s.lv)`, same guard as fetch).
- Batch 10 (built ~04:20, economy engineer 4): (a) RODEX mail (`mail.go`): a bot can mail zeny to any character
  (0x0a08 -> 0x0a12 -> 0x0a6e -> 0x09ed, then ALWAYS 0x0a03: while "writing a mail" rAthena refuses item use and
  trades); every bot collects zeny mails on 0x09e7 (0x0ac1 -> 0x0ac2 -> 0x09f1 -> 0x09f2, then deletes 0x09f5).
  Fee 2% (mail_zeny_fee). (b) Guild fund by mail: guildFund prefers a same-map donor (trade, no fee), otherwise a free
  guild mate anywhere with 2.5k+ mails min(half of what it has above 1.5k, 3k) once an hour ("guild fund: mailed" in
  counts; /api/economy `mails_sent`/`mails_collected`/`mails_failed`). Measured before: 0 fund gifts in 70 min, 0
  guilds had a 2.5k+ member on the alchemist's map. (c) Diary fixes: deals say how they meet (fetch/delivery were
  all "same map"); purchases, gifts and the guide were written twice (event "market"/"info" already remembers);
  shop sales record the buyer's name in the trade history (was "(shop customer)"). (d) legion-life skill: trade talk
  must match `legionctl market` and real "sold/bought" diary lines (the life mind wrote invented trades into notes,
  e.g. "buys iron ore from Onistorm"). UNVERIFIED live: mail packets (watch counts "mail zeny: *").
  (e) "shop buy: shop did not open" (10 of 16 shop buys): rAthena refuses the WHOLE shop when one item is unidentified
  (vending.cpp `i != j`), but vend.go set Opened when it asked, so e.g. LilPixelfang Bazaar sat "vending" with
  looted unidentified Swords/Clubs and no shop. Now: ZC_ACK_OPENSTORE2 0x0a28 result 1 -> Opened=false ("shop open
  refused" in counts); cart items carry the identify flag (0x0b45 byte 13; the cart's equipment list 0x0b39 type 1
  is now parsed at login); unidentified items are not vendable; vendors identify drops worth 300z+ with a Magnifier.
  (f) Stockers stock by demand (`stockDemand`: buyer wants + past sales minus units listed in shops); a card with no
  listing counts as 1 demand; materials/gear need a buyer. (g) Party chat events are logged once with `heard_by`
  (eventRing.add folds the same party line within 3 s; inbox merges hearers): ~3,800 -> ~1 per line.
  (h) Spoken trip lines drop the log label (`spokenWhy`, chatter.go): no more "(on the party town run)" in party chat.
  (i) Party.ShareSet comes from the server's party option packet 0x07d8 (dashboard even_share was false for 258/259).
- Batch 11 (built 04:09, same image as 10): vendor couriers (vendbuy.go `courierReady/courierSpare/courierTake/
  courierEnd`, Deal.Courier, kind "courier"): a vendor whose shop has been open 5+ min offers its listing to party
  members' wants as a delivery; on a match it closes the shop (0x012e), moves the item cart -> bag (0x0127), walks /
  Kafras to the buyer's map like a delivery seller, and after the deal puts leftovers back in the cart (0x0126) and
  reopens. One trip per vendor per 20 min. Aimed at "too far: both in parties" (1,227) / "partner can't travel" (643).
- Batch 12 (shopcheck.go, built for 05:35): ghost shops off the market. (1) a buyer's "shop did not open" marks the
  vendor a ghost for everyone (`shopGhost`; shopListing/openShops hide it; vendThink closes 0x012e and sets up
  again; cleared only by 0x0a28 result 0). (2) every 2 min `checkShops`: a shop open 2+ min whose sign (ZC_STORE_ENTRY
  0x0131, AREA broadcast on open + sent on coming into view) no bot saw since it opened, while another bot stands
  within 12 cells, is a ghost. 0x0132 is NOT proof (rAthena sends it on out-of-sight too). Legion has no DB driver, so
  the `vendings` table isn't read. (3) `fixShopTitle`: a saved title naming cards/gear/materials the cart lacks is
  replaced by defaultShopTitle (e.g. LilPixelfang "Lunatic Card - quick sale" over Sword/Club).
- Batches 9b-12 went live 04:36 (772c432b). First 10 min: mails 64 sent / 64 collected / 0 failed; DB `vendings` =
  /api/economy shops (28/28: no ghosts; check with `select ... from vendings` in ro-db); shop buys 5 (5.1k), "shop did
  not open" 0; no courier deals (party wants are Blue Potions, no shop has them). 8 of 10 guild alchemists are
  Alchemists with Pharmacy 5 (Silent rogue, LilTacox, Blaze.mage, lilyrogue, Lily_dancer, Star song, ravenclaw85
  Bazaar; LilPuddingheart Merchant job 39); first fund wave took them to 3-18k.
- Batch 13 (built 04:55, a3c610eb): (a) MAMMONITE ate the guide fund: lilyrogue 11,166 -> 4,066z in 10 min, nothing
  in its diary, -200z per hit (Mammonite costs 100z x level; progress.go cast it at level zeny/1500). Now a guide
  saver never casts it; others need 2,500z+, level = 1 per 2,000z above 2,000. (b) alchemyWant only with the guide
  (savers bought herbs with fund money). (c) the guide errand runs even in a party once zeny >= 103k (the party check
  blocked it forever). (d) a guide saver counts as shortOfZeny (loot-rich maps). (e) tripGear/gearFromShop: on a
  Prontera trip, gear a south-gate shop sells at <= NPC price is skipped at the NPC and bought by browseShops
  (shopGearPick; counts "trip gear: player shop instead of NPC"). (f) /api/economy `match_misses_items`.
- Batch 13 went live 05:06: alchemists' zeny only rises since (Silent rogue 1.4k -> 4.2k, ravenclaw85 Bazaar 20.2k ->
  22.6k in 5 min; 0 drops in 40 polls).
- Batch 14 (built 05:25, f40edaa5): (a) alchemist pick after a restart: a saved pick that isn't a candidate yet (joined
  its guild in game, persona guild "": only counted once logged in) is waited for 20 min; an unseen current pick is
  kept only 20 min after start (Blaze.mage, guild03 Alchemist with 9.2k fund, lost the job to an offline Novice at the
  05:06 restart and could never win it back). (b) NEW refine.go: weapons refined to the safe limit at Hollgrehenn
  (prt_in 63,60) via the refine UI (feature.refineui on): 0x0090 talk, mes/close2 (answerStrayDialog 0x0146), 0x0aa0
  window, 0x0aa1 <idx> -> 0x0aa2 materials {itemId.L chance.B zeny.L}, 0x0aa3 <idx><mat>.L<0> -> 0x0188 <res><idx>
  <refine>, 0x0aa4 close. Weapon lv1 Phracon + 50z to +7, lv2 Emveretarcon + 200z to +6 (refine only at 100%
  chance; less = "safe limit", remembered per char+item). Starts after a Prontera trip ("done", after browseShops)
  with the material in the bag; refineWant (lowest priority, econWantAll last) asks players for the material with
  money above gearReserve+500 (lv12+, not vendors/guide savers). InvItem.Refine from the login equip list (byte 65)
  and 0x0188; Item.WLv/Refineable from item_db. Counts: "refine visit started", "refine: success/failed", "refine
  visit: <why>". Why: 338 Phracon / 115 Emveretarcon on the board, 0 buyers. UNVERIFIED live. (c) "cheaper than NPC"
  wants bid the lowest ask if it is <= 97% of the NPC price (346 "Adventurer's Suit: price too high": bid 900 vs 950).
  (d) vendBuyThink walks to talkCell(vendor) instead of the cell south of it (LilGhostx Shop at 136,60 with 136,61
  blocked: 6 of 8 shop buys after 05:06 timed out on "no path").
  (e) fixShopTitle also drops the stale title from data/shoptitles.json (dropShopTitle; Onistorm Deals' saved "Hornet
  Card, fair price" was reloaded and replaced on every open). (f) Alchemist supplies: needBottles keeps 30 bottles
  even without herbs (south gate Goods Merchant sells 1093 + Scell); a party alchemist with the guide that is OUT of
  bowls or bottles makes one solo restock errand (like the guide errand). (g) potBuyPlan: a guide saver's potion
  top-up above the survival minimum is not paid from the fund (reserve = gearReserve).
- Batch 14 live 06:05: 162 player trades in 8 min (Phracon 240-260z for refining; was 3 per 20 min); 154 same-map
  deals done. Onistorm stale title dropped. Refine visits got stuck in prt_in's wrong room (128,40).
- Batch 15 (built 06:20, 766d8459): refineThink routes inside Prontera with RouteMapsPoint to Hollgrehenn's room
  (wrong door -> out and round, <= 3 entries, "room can't be reached").
  Tested offline (copy of the tree + /rathena from the image, go test): every prt_in entry routes out to Prontera
  and in through prontera 179,184 -> prt_in 60,73 (Hollgrehenn's room). Also: econSpare keeps out refineKeep (the
  material its own weapon still needs) and anything bought within the hour (noteBought/boughtRecently; Phracon went
  back and forth between bots); the 20-min limit on unseen alchemist picks is reverted (it replaced 4 offline
  Alchemists holding fund); data/alchemists.json restored. Built 06:32, 2c6ac700.
- Batch 16 (built 06:38, 755423e4): potions - lv30+ take Red Potions unless they can afford an hour's stock
  (hpPotTarget) in Orange (was: unless they could afford potMin), gearKeep reserves hpPotTarget not potMin (baseline
  06:33: lv30+ median 15 HP pots, p25 9). Loot: spareGearFull (lootvalue.go) - a weapon/armor already held as an
  unequipped spare (vendors: 3) stays on the ground (baseline: spare weapons per lv30+ bot median 3, p90 7, max 15).
  Measure: sample 60 online lv30+ bots, count ItemID 501-504 and unequipped weapons in /api/bots/<name> inventory.
- Batch 17 (in the tree 06:43, parcel.go + mail.go): RODEX item parcels. parcelThink (economy round, <= 1 per 30 min,
  idle bots only): surplus gear (identified, unequipped, +0, no cards, not keepsGear, every copy beyond the first per
  item id, max 5) is mailed to the account's own vendor char, else a guild mate's vendor char (fnv-stable pick).
  Packets: 0x0a08 open -> 0x0a12 -> 0x0a04 <idx>.W <amount>.W per item -> 0x0a05 <result>.B... -> 0x0a6e (zeny 0) ->
  0x09ed -> 0x0a03. Receiving: mailbox 0x0ac2 entries with type&4 -> 0x09f3 <mailid>.Q <0> -> 0x09f4 (result byte
  11) -> delete 0x09f5; only below 60% weight. Counts "mail items: sent/failed (why)/collected". UNVERIFIED live.
  Gated: tuning.json parcels_enabled (default false) or parcel_test_bots "Name1,Name2" (re-read every 30 s).
- Batch 16 live 07:05; refining confirmed live (07:20: "refine: success" 10, visits 8, "out of Phracon" 3, "weapon gone" 2).
- Batch 18 (built 07:30, economy engineer 6): (a) parcels as written in batch 17 could never fire: 07:15 scan of all
  936 online field bots found 0 with 2+ IDENTIFIED copies of a weapon/armor - the spare piles are unidentified drops
  (lv30+: unidentified spare median 2, p90 10, max 19; Club 1501/1502 99+49, Knife 36, Sword 18 in 60 bots). parcelItems
  now takes unidentified copies too (one copy per item id stays, the identified one), only when worth a Magnifier
  (vendIdentifyWorth: weapon/armor with priceOf >= 100 - Club yes, Knife 50z no). (b) vendors identify for the shop:
  59 of 74 online vendors held unidentified gear (161 items), 2 had a Magnifier, the old threshold was 300z. Now a
  vendor (shop closed) with such gear and no Magnifier walks to the south gate Tools Merchant (prontera 164,57; trip Why
  "magnifier", run from vendThink because vendors never reach ai.go's tripThink) and buys one per item (max 15), then
  waits up to 20 s per item for identifyThink before opening. Counts "vendor magnifier trip", "vendor magnifiers
  bought". (c) restock: an open shop (20+ min) checks every 30 min; with 2+ listable items in the bag (or 2+ to
  identify) it closes (0x012e) and loads them into the non-empty cart (VendState.restock), then reopens. Count "vendor
  restock: shop closed". (d) Vending pitch refused: 07:25 13 of 35 vendors in Prontera sat "setting up shop" forever,
  re-casting Vending every 12 s and getting 0x0110 (skill 41) - pitches within 3 cells of the south gate merchants
  (min_npc_vendchat_distance). Now 0x0110 for skill 41 drops that pitch for the session and the vendor takes another
  (count "vend pitch refused: moved").
  (e) npcGearCap (economy.go askPrice + vend.go vendPrice): used weapons/armor an NPC also sells ask at most 93% of the
  NPC price (07:25: 33 Adventurer's Suit wants bid 910 vs 5 sellers at 990+, "price too high" 1,036).
  (f) party.go member loot: batch 16's spareGearFull (and heavyJunk over the regen line) were only in ai.go's solo
  loot; party members (most bots) looted every copy (lv30+ spare weapons median 3 -> 4, max 15 -> 27 at 07:27). Both
  filters now apply in partyThink's loot step too.
- Batch 19 (built 07:36, dietrich.go): refine materials. Stock rAthena Dietrich (prt_in 63,69, in Hollgrehenn's room,
  market shop: Phracon 200z, Emveretarcon 1,000z) - main's decision 07:40: players first, NPC when no player shop sells
  at or below the NPC price. refineAfterTrip starts a visit without the material when dietrichBuyCount > 0 (no open
  shop at <= NPC price via cheaperInShop, money above gearReserve+300); in the room the bot talks to Dietrich (menu 1,
  answered in answerStrayDialog via dietrichMenu), market open 0x0b7a (19-byte entries; 0x09d5 15) -> 0x09d6 -> result
  0x0b4e (result.W 0 = ok) -> close 0x09d8. Counts "refine material: *". GATED: tuning dietrich_enabled /
  dietrich_test_bots. Always on: npcMatCap (asks <= 95% of the NPC price, #trade and shops) and bids <= NPC price.
- Batches 18+19 live 07:44 (early deploy after a crash): first 2 min "vend pitch refused: moved" 11, vendor
  magnifier trips 26. Parcel test 07:48: parcel_test_bots = Raven rogue81, ironrogue, Ashrogue87.
- Batch 20 (built 07:55, refine.go refineTarget): refine only the weapon it keeps - lv20+, a class weapon
  (isClassWeapon, classWeaponMissing false), and not when nextGear()'s next buy is a weapon (archers refined the
  starter Knife before their Bow; lv1-15 Novices refined). Checked: "no path on prt_in" diary lines end 12:05:33Z (0
  after batch 15 went live); Phracon trade flip-flops after 12:06Z: 1 (storm_soul, across the 12:44 restart that clears
  boughtAt) vs Nekoqueen's 11 before.
- Parcel test 07:48-07:56: 3/3 "mail items: failed (no answer)", no disconnects. rAthena (PACKETVER 20211103) answers
  CZ_REQ_ADD_ITEM_TO_MAIL with 0x0b3f (64 bytes, RE >= 20200723), not 0x0a05. Fixed in mail.go/bot.go (built 07:58,
  batch 20); parcel_test_bots emptied until it is live. ALWAYS check a packet header against the server's PACKETVER
  in rathena src/map/packets_struct.hpp (DEFINE_PACKET_HEADER under the #if) - recvpackets.txt lists both.
- Dietrich test from 07:58: dietrich_test_bots = yukimonk, lilstormfire, MintBuffs, Kirabunny, TheSilentchan.
- Batch 20 also (built 08:05): refine visits end "left Prontera" when the bot is out of Prontera (Lilyknight started
  a visit, its party took the Kafra to Geffen, then it walked back from gef_fild00); "weapon gone" (7 of 14 visits
  07:44-08:00): 0x0aa0 takes the index of the weapon in hand, a stale index is re-asked once, and the reason now says
  "(not equipped)" / "(no item at index)" - check those counts after the deploy.
- PARCELS ARE UNECONOMIC: rathena conf/battle/misc.conf mail_attachment_price: 2500 - every attached item costs the
  sender 2,500z on send (mail.cpp mail_removeitem). Mailing Clubs (~120z) is absurd; parcel.go stays off
  (parcels_enabled false). Item mail only makes sense for goods worth well over 2,500z (good cards). Changing the
  conf is an owner decision (rathena restart kicks Ayla). Zeny mails: 2% fee only.
- Batch 20 also: a visitor out of material mid-visit (0x0aa2 "out of Phracon": 13 of 27 visits) closes the refine UI,
  buys from Dietrich (when allowed: no player shop at <= NPC price, dietrich gate) and talks to Hollgrehenn again
  (once per visit, refineTask.dietTried). The live Dietrich test (live code) only covers visits that START without
  material; this mid-visit path arrives with the 09:05 deploy.
- 08:28 Dietrich live test clean (Vexwing33: Katana bought on the party run, 0 Phracon -> 2 from Dietrich 400z ->
  Katana +2, packets 0x0b7a/0x09d6/0x0b4e right, no disconnect). dietrich_enabled = true in tuning.json (08:30).
- Batch 21 (owner rules: build gear table, wealth x rarity prices, boss items = a guild's fortune):
  step 1 buildgear.go (built 08:40): per build (13), level band (1-20 ... 81-99) and slot, the suitable items best
  first: "guide" (data/buildgear.json from the build-and-strategy agent; keys = builds.go plan ids; re-read every
  5 min), "plan" (builds.go routes), "auto" (in-world items the band's job can wear, class weapons only, ranked by the
  build's style); cards per band (guide + buildCardValue >= 4, no boss / far-too-strong monster cards). GET
  /api/buildgear?build=X, /api/buildgear/fit (share of online bots wearing table weapon/armor - the owner's measure).
  step 2 pricing.go (built 08:45): every 15 min priceRound measures the money supply (online zeny by level band and
  guild, estimated total) and prices cards + non-NPC weapons/armor: supply = copies held online (bags+carts) + a day
  of normal-monster drops (rate x spawns x 6 x 24); buyers = online characters whose build table/card value wants it;
  supply >= buyers -> old standard; scarce -> the purse of the (supply+1)-th richest buyer; boss-only (every spawning
  source a boss/MVP) -> the median guild's pooled online zeny. GET /api/economy/pricing (money_supply, history, top
  200). GATED: tuning dynamic_prices (standardPrice uses the table only when on; fixedStandardPrice = old formula and
  floor). Next: step 3 wants/adverts from the build table (guild mates first, cards to the builds that use them), and
  guild pooling for boss items.
  step 2b market.go (built 08:55, ALWAYS ON): supply and demand - every 2 min (marketTick from econRound) per item
  units offered (#trade spares + shop listings) vs units wanted: demand > supply -> factor x1.04 (max 2.0; 1.0 for
  NPC-sold), supply > 2x demand -> x0.97 (min 0.6), else drift to 1; priceOf = reference x factor, floor NPC sell
  price. Vendors undercut the lowest other shop by 2% (vendPrice). Markdown: a seller's listing unsold 45+ min loses
  5% per 30 min (max 6 steps), floor NPC sell price / half the reference for valuable goods; a sale resets the
  seller, a want resets everyone (log "markdown: ...", count "markdown"). /api/economy/pricing "market" (reference,
  factor, price, ask range, supply, demand) and "markdowns".
  step 3 (built 08:55): econWant also asks for build-table items (guide/auto, not NPC-sold) that someone offers
  ("for my build"); offer lines name the builds that want the item ("gd for hunters"); the matcher prefers guild
  mates. farm.go: mapTradeZeny (trade goods at market price per kill) added to the NPC loot value where broke bots pick
  the richest map (mapFarmValue: beginner.go, lootvalue.go richestLoot); GET /api/economy/farm?lv=N lists sought-after
  drops farmable at a level (map, monster, rate, zeny per kill) for the minds.
  step 4 guildpool.go (built 08:52): a boss-only item on offer that suits an online guild member's build -> the guild
  pools for its highest-level such member: guild mates mail it up to 5,000z each (once an hour, keep 1,500z; shares
  guildFundNext with the alchemist fund) until its purse covers the lowest ask; then it posts a "guild buy" want at
  that price (econWantAll first); gearReserve keeps the pooled money. One pool per guild, ends when bought / item gone /
  3 h / buyer offline. /api/economy "guild_pools", counts "guild pool: *".
- 09:10 first live price table: boss cards stayed at the old fixed 250k (max with fixedStandardPrice) and Holy Avenger
  450k. Formula fixed (built 09:10): boss-only = max(median guild online zeny, 20k) (153k at 09:06); plenty =
  min(standard, median buyer purse); scarce = max(purse of buyer #supply+1, that). Money supply 09:06: 1,010 online,
  2.14M online zeny, median 1,620, est. total 8.5M. Fit 09:05: weapon 72%, armor 95% (hunters 52%). Build-specific
  weapon types (planWeaponSubs: knight 2hSword, crusader 1hSword) lower other types' itemScore to ATK/3.
  dynamic_prices stays off until the fixed formula is live and checked.
- Batch 22 (built 09:45; owner: junk gear goes to the NPC): junkgear.go - plain weapons/armor with NPC sell price under
  100z, fewer than 4 slots, no cards, +0, not an upgrade/plan item (keepsGear; unidentified ones regardless) are junk:
  npcSellable = npcJunk || junkGear is used by the NPC sell list (town.go 0x00c7), hasLoot (trips, regenWeightNeed),
  lootValue and the arrow loot check; shops don't list them (vendable). Vendors with 5+ junk pieces make the south
  gate Tools Merchant trip (it sells first); vendIdentifyWorth needs sell price 100z+ or 4 slots (no Magnifier for a
  Club). Arrows (build agent): arrowTarget = min(3000, 1000 + lv x 30) (was 500 + lv x 10, ran dry in 10-35 min),
  top-up when below a quarter, paid from all zeny after the potion minimum (was half). Also: shops no longer list
  junk gear; /api/buildgear/fit "suitable_items_offered" (unsold suitable goods).
- 09:55 (built): card buyers in pricing.go = bots whose build table (band) lists the card (buildCardValue >= 4 alone made
  every card "wanted" by ~900 bots -> priced at a top-10 purse). /api/economy/pricing?q=<name>&all=1. Table at 09:39:
  boss-only ~150k (guild median 150.6k), Holy Avenger 225k (purse #2 of 50 buyers), no-buyer items keep the fixed price.
- 10:20 dynamic_prices = true (tuning). Cards at 10:15: 910 priced, 545 no buyer (fixed price), 315 boss-only (~150k),
  49 scarce (Raydric 14k, Andre 11k, Matyr 20k, Pupa 7k; median purse ~1.5k). Watch card trades / wants; markdowns start
  45 min after a listing is first seen (restart resets).
- Run 7 (10:20-10:55, built, waiting for a deploy): (a) vendor alchemists: vend.go closes the shop (alchShopClose, max
  1x/20 min) for the guide errand / brewing / supplies and runs the Why "alchemy" bottle trip (guild07's alchemist is a
  vendor: it sat at 102.9k forever); brewers never vend brew materials. (b) guide fund: the final gap is rounded UP
  (gifts of n/100*100 with a 300z minimum could never cover the last <300z; two alchemists stuck at 102.9k). (c)
  parcels: target = a guild mate's open Prontera shop only (the hashed offline vendor accounts never log in); no junk;
  1 item if NPC price 250z+. Send path verified live at the new 100z fee (2 sent, 1 server result 1, items kept).
  parcels_enabled stays false until re-tested after the deploy. (d) mailtrade.go: RODEX sales for cards/gear/valuables
  worth 1,000z+ between far-apart players (seller mails item, buyer mails price; mailOut.Counts for part stacks);
  tuning mail_trades / mail_trade_test_bots, off until tested. (e) hand-offs survive within 20 cells (together.go) and
  anywhere in town (90 s): "party mate too far" was 28/40 failed deals. (f) econSpare skips junk gear. (g) market
  demand counts only units the buyer can pay (Blue Potion bids spiralled to 8.9k from penniless guild-leader-party
  members); Dietrich items (npcMatPrice) have factor ceiling 1.0.
- Run 7 batch 2-3 (live 16:02 / built ~11:40 EST): parcels_enabled + mail_trades ON (16:10). Butterfly Wings follow a
  saving floor (town.go wingSaveFloor/bwWant; a Butterfly Wing is 1,000z in RE); partyRunTown sends the party to
  Prontera for a plain gear piece only if it costs >= 800 x members (fares are 120-180z). Money-flow measurement without zenylog: price
  picklog with the RE item db (lessons.md). Built for the next deploy: dealer trip drives its own Kafra trip (was a
  deadlock), guideSaving() locks the guild fund, dealer trip 45 min, alchemy errand (errand.go kind "alchemy");
  buildgear nextBuildPiece wired: econWant "for my build" first for drop/trade pieces, nextGear saves toward a shop
  piece <= max(3x budget, 5k), solo map picker farms a drop piece's Farm map when it is a safe candidate (80%);
  parcels only for items worth 400z+.
- Run 8 (2026-11-14 ~11:30-12:00 EST): batch 3 live 11:38 (deploy-20261114-113731) incl. the class-weapon ERRAND
  (errand.go kind "weapon" / "weapon (sell loot)": a party member missing its class weapon or with a weapon far below
  its level leaves from a field map - never a dungeon - buys it alone and comes back; ai.go skips the "waits for the
  next party run" line when it can leave; gearStepReady keeps it to 1/h). First guide bought: guild07 lilzensenpaiVend
  16:44 UTC (Geffen Kafra -> Al De Baran works; walking back via mjolnir kills). Built after (not yet deployed):
  Butterfly Wing price from the shops' real list (town.go wingPriceSeen; data.go loadItemOverrides reads the rathena
  db-import item_db.yml mounted into legion/legion-core) - bwWant thresholds scale with it (1.5 / 3 / 30 wings);
  potBuyPlan's HP top-up above potMin keeps wingSaveFloor (restocks left a median 1,049z); deal retry (Deal.busy: a
  buyer refusing while hit makes the seller wait; transient failures block the pair 4 min, not 30).
- Run 8 owner rules (live 12:00, deploy-20261114-115612): HEALING FIRST - healItem (Red..White Potion, Red/Yellow/
  White Herb, fruit/meat/milk; foodHeal) is vendable, loaded/listed first (vend.go healFirst), stocked first by vendors
  (vendbuy.go stockWant, ~60 units on the gate), kept from the NPC (food.go healKeepForShop) and parcelled to the guild's
  open shop (parcel.go parcelHeal, partial stacks via mailOut.Counts); browseShops buys HP potions <= 110% NPC price;
  farm.go tradeGood counts healing drops (broke bots' richestLoot). MARKET OPPORTUNITIES (opportunity.go, GET
  /api/economy/opportunities): score = (wants + sales + build buyers/5 [+30 healing]) - (listed + 3 x chasers), x price
  (healing x2); checkMarket after a Prontera town trip sets a 90-min money goal (item + safe drop maps), solo map
  picker farms it 70%; max 8 chasers/item. lfg.go hook: `b.MoneyFarmGoal(now) *farmGoal` (Kind zeny with Maps).
  MARKET TALK: after a check, maybe one #trade line (gap / spike / glut) - server-wide 1 per 50 s, an item once per 15
  min; a gap line is a 20-min rumor (score x1.5; bots without a goal take it up: "heard on #trade ...").
- Run 8 income (built 12:20, image 9435089): measured per bot-hour (picklog): lv60 ~3.6k loot value, ~40z/kill, ~90
  kills/h; only ~50% of non-vendors fighting (24% travel, 16% roaming). Cause: gear-poor bots on low crowded fields
  (moc_fild01 72 bots avg lv51 on lv7-25 monsters, 16z/kill) - EXP 10x vs drops 1x (EXP rate raised to the owner).
  zenyhour.go MapZenyHour(m, power, fam): value/kill (NPC + market trade goods) x kills/h (HP / dpsAt(powerLv) +
  crowd-scaled search), live / roaming share. Solo picker: best z/h among the near candidates (broke 85%, else 50%);
  goodHere: broke bots leave for a 1.5x map one change away; partyMapSearch / partySpotNear: 3k z/h = one map change
  (max 3). Offline sweep TestZenyHourLive (LEGION_RATHENA=/rdata LEGION_BOTS=snapshot from scratchpad ec8_snap.py):
  median expected z/h +17..46% per band, maps over cap 3 -> 0 (model ranks; absolute values ~3x the measured).
- Run 8 drop rates (built 12:32, b210598): owner set EXP 5x (12:23) and drops 5x at the next rathena restart.
  droprate.go effDropRate = base x item_rate_*/100 (battle_conf import, mounted ro into legion), capped at 100% per
  drop; used by mapLootZeny, mobTradeZ, farmTargets, pricing DropsDay, pieceFarms, MapZenyHour, lookup. Live switch in
  data/tuning.json: drop_rates_from_conf=true after the restart (default 1x), or drop_rate_mul=N.
  Guild brewers: parcel.go parcelHerbs - guild mates mail Yellow/White/Blue Herb + Scell stacks to the guild
  alchemist while it is online with the guide (brewerSeen). Guides bought: guild07 lilzensenpaiVend 16:44 UTC,
  guild06 Neko.fang 17:13 UTC (both then had ~1.5-3k zeny, too little to bid for herbs).
- Run 8 guild supply chain (built 12:56, c121bfc; owner rules 3b): guildstock.go, GET /api/guild-stock - every 5 min per
  guild: stock = its vendors' open shop listings (no guild storage at guild lv1, no Kafra-storage I/O), needs = members'
  next drop/trade pieces + 10 healing per member under potMin, gaps; up to 2 guild farmers (DS Hunters first, then
  Knight/Crusader/Monk; solo, free) get a gap item as money goal and say so in guild chat; the guild TRADER (vendor with
  the biggest open shop) receives parcels first, buys gap items first (stockWant), donates to the guild fund when not
  vending; guild vendors gift healing/potions to guild mates who ask. Creamy Card: every lv15+ char is a buyer in
  pricing, opportunity demand +50, creamyWant B> when affordable. Open: Kafra/guild storage I/O, trader junk sales with
  Overcharge, trader paying gear turns.
- Run 8 glut + markdowns (built 13:02, 9e8866c): market.go glutMul(id) - supply on offer > 3 x (demand+10) -> farming
  value x lim/supply (min 0.1; a "so many X" talk line x0.7 for 20 min); used by mobTradeZ (MapZenyHour, richestLoot),
  farmTargets, opportunities (glutted = skipped), guild farmers; vendors list max 10 of a glutted item, stockers skip
  it; vendPrice never below marketFloor (NPC sell value). Markdown clocks persist in data/markdowns.json (deploys reset
  them before the 45-min mark, so no item was ever marked down before).
- Run 8 late fixes (built 13:19, 726b87f): brewers' bowls/bottles/Scell never NPC-sold (junkgear.go npcSellable - the
  first 2 guides' 20 bowls each were sold at the next shop stop); in-game guilds are all at 16 members (lv1 cap) and 940
  online chars have none - leaders invite the guild alchemist + trader first, persona-guild invites always accepted;
  refiner/Dietrich walks give up after 2 FindPath failures; no refine of a weapon about to be replaced or a <200z
  starter; arrows are a party-member errand (errand.go "arrows"); weapon/arrow errands 20-min cooldown.
- Zeny 02:50 (736 online): total 918k; lv20-29 avg 754 / median 540, lv30-39 1,155 / 813, lv40-49 1,551 / 1,141.
  The 100k Potion Creation Guide is about one whole guild's online zeny (~90k): guild fund gifts (<= 3k, same map,
  1/donor/hour) won't get there in a day. Drop rates are the owner's call.
- Open: party members (most bots) can only trade within 10 cells or by delivery from a solo seller, so cards and shop
  goods rarely reach them ("too far / party" in match_misses). SP potions are the biggest unmet want (no brewer yet).
  Ideas: let party members buy from south-gate shops during the party's Prontera town run (trip in progress);
  vendors buying spare cards/gear from field bots to stock shops.
- Run 9 (2026-11-14 13:20-14:15, built c844a371 / 15c6590d): brewing - a guild alchemist that owns the guide is a
  "known brewer" (data/brewers.json, alchemy.go noteBrewer); its account is always scheduled online (ai.go
  ShouldBeOnline) and plays that character (persona.go pickChar). A broke brewer (< 3k, few bowls/bottles) is topped up
  to 4,000z by guildFund (fundTo) and keeps that out of gear (gearReserve). Pinned (human-party) alchemists may run the
  Alchemist Union errand (guide / bowls) and brew while seated (Silent rogue sat on 108k); humanPartySync stands down
  during an "alchemy" errand. Guild trader funds a stuck party gear turn ("party saving (Xz of Yz)", <= 5k, keeps 10k,
  not while vending; guildstock.go traderFundRound).
  Gear (Ayla's friends QA): townBuyNow - a bot standing in a town buys its class weapon / turn item / next piece sold
  there from its own spare (every 90 s check, a normal gear trip), not only on "town runs"; stickyStep keeps gear_next
  until bought unless a pick is x1.5 better or buyable now while the kept one is >3x the spare; giftableZeny (party
  gear gifts, guide fund, peco fund, guild boss pool, Starveil pool) keeps the reserve/fare, raised turn zeny, mail
  gifts of the last 90 min (earmarkZ, cleared by gear buys) and the own next piece when within 2x spare; pecoNeed holds
  only the rental, not the purse; headgear is compared with everything worn on the slots it covers (Goggles = upper+
  mid) with a 10% margin (equip flapping).
  Human B> (humanwant.go): a human's "B>/WTB/buying <item>[, up to Xk]" on a channel -> human want; a free bot with it
  as spare whispers an offer after 20-60 s (its ask, capped at the stated price, takes the price within 15%), mails the
  item, and the sale settles when it collects zeny mail from that buyer; a vendor listing it points to its shop. Main
  instance only. /api/economy human_wants.
  Measured: median purse lv60-79 1,127z (940 before), next piece affordable 238/885 (21/1010); Red Potion capped 10/shop
  (27 shops); markdown clocks persisted (1,808 at 13:27). Activity sample 13:40-13:51: fight/loot 31%, party "with
  leader" idle 16%, travel 18%, 107 bots with 3+ destinations in 18 min - the biggest income gap is time not farming.
  14:13 (60-min picklog, per bot-hour): NPC sales lv45-59 6.5k (2.8k at 13:20), lv60-79 8.8k (5.0k); NPC spend 5.4k / 7.7k;
  median purse lv60-79 1,666z; total online zeny 2.45M (2.07M). Markdowns live (60 by 14:13). Later fixes (66922bbd,
  2c74b114): party members buy in town only on their party's town run there; one errand per party at a time; a
  party gear run goes only to Prontera (never a far town chosen by shop distance - lillunashot's party ran to Yuno for a
  Hood). Open: trader junk sales with Overcharge (in-person handover), storage.go store-trip loop (reported).
- Run 10 (2026-11-14 14:50-, built): verified the 14:53 deploy (events 19:57-20:10Z): 233 NPC gear buys in ~13 min
  (525k zeny), 0 Yuno trips, errands one per party, humanwant not exercised (no human B> since). Brewing: Yellow Herbs
  join alchemyWant (Blue/White skipped when < 5 on the market: 18 Blue / 1 White vs 282 Yellow on offer); a party brewer
  with herbs but no bottles / Scell takes a "brew supplies" errand to Prontera's Goods Merchant (alchemy.go
  brewSupplyNeed / alchSupplyTrip; Neko.fang 9 Blue Herbs 0 Scell, Silent rogue 0 bottles); humanPartySync stands down
  for it. Market-mind fixes: deals hold the session (ai.go logoutSignal waits up to 15 min while b.mkt / a mail is
  open); a delivery follows the buyer's party to its new map (twice, safe maps only, else whispers "deal off");
  same-map deals with a SOLO partner: the solo side walks, the party member keeps playing (2 min) - "party mate too far"
  was 16 of 26 failures, same-map deals failed 19 vs 12 done; /api/economy deal_fail_reasons + counters_since, and all
  counters persist (econpersist.go, data/econ_counters[_core].json, saved every minute); shop titles are checked
  against the actual listing at every open (vend.go titleMatchesListing / listingTitle); shops stay open >= 40 min,
  restock at most hourly and only for 4+ "points" (card / healing stack = 3), courier deliveries 1 per 40 min; guild
  trader is sticky while its shop is open, /api/guild-stock stock_by_shop (stock = all guild shops together); cheap
  [4] gear (Club [4] 60z) is junk unless the market asks for it (marketDemand); trade lines (B>/S>/WTB/WTS) never get
  typos or cuts, typos only swap letters inside a lowercase filler word, slang swaps whole words ("B> aHt 930z",
  "BlueH erb"). Income (20-min picklog per bot-hour x3): lv45-59 NPC sell 4.6k / NPC buy 5.1k, lv60-79 7.4k / 5.8k -
  the NPC spend is mostly gear (good), Butterfly Wings ~0.5-0.7k/h; purses stay low because gear is bought.
  Loot flow (owner rule "Interesting loot", lootflow.go): cards / real gear / valuable Etc the looter doesn't use
  (keepsCard / keepsGear / refineKeep; a 2nd copy always flows) go party member who can use it -> guild mate -> the
  guild trader (stock, priceOf >= 400) -> sale, one step per item per 2 min, one gift per bot per round, by RODEX (1
  unit, 100z), with party/guild chat ("anyone need a X?" / "me!" / "np take it, mailed"; at most one exchange per giver
  per 4 min). econSpare and parcelItems hold an item until its flow reaches the sale; a bot with no party and no guild
  sells only duplicates. /api/economy loot_flow {kept, party, guild, stock, sold (= released to the market)} this run;
  cumulative in counts "loot flow: ...". Not for Ayla (human): her plugin would need its own offer logic.
  Unidentified gear (QA audit: Knife [4] 581, Club [4] 317, Sword [4] 242 copies online): the [4] exception now needs
  market demand, and an unidentified piece the build wants is kept once (firstCopy), the rest sold to the NPC.

