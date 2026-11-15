package main

// Town trips: when the bag is heavy or potions run out, go to town (Butterfly Wing if carried, otherwise walk),
// talk to a Tool Dealer, sell loot (Etc items), buy potions, and head back out.
//   CZ_CONTACTNPC 0x0090 (npc, 1) -> ZC_SELECT_DEALTYPE 0x00c4 -> CZ_ACK_SELECT_DEALTYPE 0x00c5 (npc, 0 buy / 1 sell)
//   sell: ZC_PC_SELL_ITEMLIST 0x00c7 (idx u16, price u32, overcharge u32) -> CZ_PC_SELL_ITEMLIST 0x00c9 (idx, amount)
//   buy:  ZC_PC_PURCHASE_ITEMLIST 0x00c6 (price u32, dc u32, type u8, item u32) -> CZ_PC_PURCHASE_ITEMLIST 0x00c8 (amount u16, item u32)

import (
	"fmt"
	"sync/atomic"
	"time"
)

type Trip struct {
	Stage   string // wing, go, talk, selling, buying, done
	Town    string
	Shop    *Shop
	Started time.Time
	At      time.Time // last stage change
	Why     string
	Sells   int             // sell conversations so far
	Next    *Shop           // a second shop on the same trip (SP potions: Advanced Potion Merchant next to the Tool Dealer)
	NoPath  int             // failed path searches to the shop counter (give up after 2)
	Extra   int             // gear steps bought on top of the first one in this town (at most 2 more)
	Ammo    *Shop           // ammo.go: the town's arrow seller, visited when the trip's other shops don't sell arrows
	Gear    *Shop           // the gear shop visited after the Tool Dealer (b.gearItem), when potions come first
	Slots   map[uint32]bool // gear slots (gearSlot) already bought on this trip: one item per slot per trip
}

type npcDeal struct {
	npc  uint32
	want string // sell / buy
}

const (
	itemRedPotion     = 501
	itemOrangePotion  = 502
	itemYellowPotion  = 503
	itemWhitePotion   = 504
	itemFlyWing       = 601
	itemButterflyWing = 602
)

func (b *Bot) potionCount() int {
	n := 0
	for _, it := range b.inv {
		if isHealPotion(it.ItemID) {
			n += it.Count
		}
	}
	return n
}

// buyPotCount: Red..White Potions (what the buy step tops up to hpPotTarget); potionCount also counts the starter kit
// (569 Novice, 11500 Light potions), so "restocked: 176 potions" was 148 + 13 starter + 15 Red, not an overstock
func (b *Bot) buyPotCount() int {
	n := 0
	for _, it := range b.inv {
		if it.ItemID >= itemRedPotion && it.ItemID <= itemWhitePotion {
			n += it.Count
		}
	}
	return n
}

// heavyTripGap: the least time between two "bag is heavy" trips, even over the regen line (ai.go trip block)
const heavyTripGap = 10 * time.Minute

// outOfPotions: the one restock trigger for a trip (needTrip) and for a party friend's hold in town before it goes in
// (together.go escortGate). The two must agree: a hold on a higher potion minimum than the trip starts waited with
// no trip, forever (QA 2026-11-15: Zen.fox 15 Red Potions held in Payon from 09:05 to the end of the log).
func (b *Bot) outOfPotions() bool {
	return b.realPotCount() < 3 && (b.Zeny >= 500 || b.hasLoot()) && !b.isHealer()
}

// realPotCount: the HP potions that count as stock. From lv20 only bought-grade potions (Red..White): the starter kit
// (Novice 30 HP, Light potions) heals a few dozen HP, nothing for a 1,600 HP Hunter (QA 2026-11-14: 105 lv30+ bots had
// 0 Red Potions and 100-150 starter potions, and the buy step thought them stocked).
func (b *Bot) realPotCount() int {
	if b.BaseLvOr() < 20 {
		return b.potionCount()
	}
	return b.buyPotCount()
}

// potMin: the survival minimum of real HP potions bought FIRST on every shop visit (before arrows, SP potions, gear).
// Healers heal themselves: a few for emergencies.
func (b *Bot) potMin() int {
	lv := b.BaseLvOr()
	n := 0
	switch {
	case lv < 20:
		return 0 // the starter kit is enough at low levels
	case lv < 40:
		n = 12
	case lv < 60:
		n = 15
	default:
		n = 20
	}
	if b.MaxHP > 3000 {
		n += 5
	}
	if b.isHealer() {
		n = 5
	}
	return n
}

func (b *Bot) invIndex(itemID int) int {
	for _, it := range b.inv {
		if it.ItemID == itemID && it.Count > 0 {
			return it.Index
		}
	}
	return -1
}

// townInteriors: buildings that belong to a town (most Tool Dealers are indoors, e.g. Prontera's is in prt_in)
var townInteriors = map[string][]string{
	"prontera": {"prt_in"}, "geffen": {"geffen_in"}, "payon": {"payon_in01", "payon_in02"},
	"morocc": {"morocc_in"}, "alberta": {"alberta_in"}, "aldebaran": {"aldeba_in"}, "izlude": {"izlude_in"},
	"yuno": {"yuno_in01"}, "comodo": {"cmd_in01"}, "umbala": {"um_in"}, "einbroch": {"ein_in01"},
	"lighthalzen": {"lhz_in02", "lhz_in03"}, "rachel": {"ra_in01"}, "veins": {"ve_in"}, "hugel": {"hu_in01"},
}

// potWorthPct: an HP potion is worth carrying when it heals at least this share of max HP (3% of 3,000 = 90 HP)
const potWorthPct = 3

// potStockMin: the stock a restock must be able to pay for before a potion tier is bought (pickHPPotion)
const potStockMin = 5

// potPickReserve: zeny kept back from a potion restock (as potBuyPlan keeps it)
const potPickReserve = 500

// hpPotOrder: the HP potions, strongest first
var hpPotOrder = []int{itemWhitePotion, itemYellowPotion, itemOrangePotion, itemRedPotion}

// potLevelOK: the lowest level a bot buys each HP potion at (Red at any level)
func potLevelOK(id, lv int) bool {
	switch id {
	case itemOrangePotion:
		return lv >= 30
	case itemYellowPotion:
		return lv >= 60
	case itemWhitePotion:
		return lv >= 80
	}
	return id == itemRedPotion
}

// pickHPPotion: the HP potion a restock buys from a shop's list (pk, the NPC sell packet), strongest first among the
// potions the level allows and the shop lists: (1) one that heals at least potWorthPct of max HP and whose potStockMin
// stock is affordable, (2) the strongest with an affordable potStockMin stock, (3) the strongest the bot can buy at all.
// 0, 0 when nothing is affordable. QA 2026-11-15: lv60-85 bots restocked Red Potions (+45 HP) against 3,000-9,700 max HP
// because they could not afford a full Orange/Yellow stock; a Red stock does almost nothing at that HP.
func pickHPPotion(pk []byte, lv, maxHP, zeny int) (id, price int) {
	var ids, prices []int
	for _, c := range hpPotOrder {
		if pr := listedPrice(pk, c); pr > 0 && potLevelOK(c, lv) {
			ids, prices = append(ids, c), append(prices, pr)
		}
	}
	afford := func(pr int) int { return (zeny - potPickReserve) / pr }
	for i, c := range ids {
		if potHeal(c)*100 >= maxHP*potWorthPct && afford(prices[i]) >= potStockMin {
			return c, prices[i]
		}
	}
	for i, c := range ids {
		if afford(prices[i]) >= potStockMin {
			return c, prices[i]
		}
	}
	for i, c := range ids {
		if afford(prices[i]) >= 1 {
			return c, prices[i]
		}
	}
	return 0, 0
}

// potTierFor: the HP potion this bot restocks from its level and max HP, whatever the shop lists: the strongest the
// level allows that heals at least potWorthPct of max HP, else the strongest the level allows.
func potTierFor(lv, maxHP int) int {
	for _, c := range hpPotOrder {
		if potLevelOK(c, lv) && potHeal(c)*100 >= maxHP*potWorthPct {
			return c
		}
	}
	for _, c := range hpPotOrder {
		if potLevelOK(c, lv) {
			return c
		}
	}
	return itemRedPotion
}

// potionShop: the shop in the town (or one of its buildings) that sells the strongest HP potion (Red..White)
func potionShop(m string) *Shop {
	var best *Shop
	bestTier := 0
	for _, mm := range append([]string{m}, townInteriors[m]...) {
		for _, s := range shopsByMap[mm] {
			for _, id := range s.Items {
				if id >= itemRedPotion && id <= itemWhitePotion && id > bestTier {
					best, bestTier = s, id
				}
			}
		}
	}
	return best
}

// needTrip decides whether it's time to go to town.
func (b *Bot) needTrip() string {
	if !b.invReady() {
		return ""
	}
	// Only REAL needs start a trip (owner, said many times: "they make town trips way too often ... 90% of the time
	// should be on farming"). Measured 2026-11-14: 93% of ~4.8 trips per bot-hour were "buying <gear>" with a mostly
	// empty bag, plus "selling loot for <gear>" and a 50%-weight sell rule that fired on one Jellopy. Gear and loot
	// selling now ride along on the real trips (onDeal chains the gear shop after the Tool Dealer).
	if b.bagFull() && b.hasLoot() {
		return "bag is full"
	}
	if why := b.regenWeightNeed(); why != "" { // weight.go: over the 50% regen line with real loot to sell
		return why
	}
	if b.storeNeed() { // storage.go: over the line with only kept items (cards, ores, spare gear): store them
		return "bag is heavy (store)"
	}
	// arrows before potions: a bow user with no arrows can't attack at all (QA 2026-11-14 LilRinkun, Archer lv51: a
	// Bow in the bag, 0 arrows, Knife in hand; "out of potions" won every check and only waited for a party run)
	if b.noArrows() && b.arrowsWanted(arrowPrice(), false) >= minArrowTrip && time.Now().After(b.arrowNeedAt) { // affordable; 20 min rest after a trip / failed visit
		return "no arrows" // a bow without arrows can't attack (ammo.go): necessary, like "no weapon"
	}
	if b.arrowLootOK() && time.Now().After(b.arrowNeedAt) {
		return "no arrows (sell loot)" // too poor for arrows, but the junk loot pays: sell, then buy, one trip (ammo.go)
	}
	// affordable at all (10 Red Potions) or paid for by the loot sold on the same trip (QA 2026-11-14 12:10: Zen.fox,
	// Wizard lv72 in Ayla's party, had 0 potions and 1,024z - under the old 1,100z line it never even asked, and died
	// five times crossing pay_dun00)
	if b.outOfPotions() {
		return "out of potions"
	}
	if it, _ := b.weaponNeed(); it != nil {
		return "no weapon" // a necessary trip (one cheap weapon at the nearest shop), even in a party
	}
	// an Archer-line bot with no bow (or instrument / whip) can't do its job at all: like "no weapon", no step delay
	if it, _ := b.classWeaponNeed(); it != nil && gearSlot(it.Loc) == 0x2 && (b.gearStepReady(time.Now()) || b.bowless()) {
		return "class weapon" // a Knife in an Archer's / Mage's hand: the class weapon is a necessary buy (builds.go)
	}
	if it, _ := b.classWeaponLootNeed(); it != nil && b.gearStepReady(time.Now()) {
		return "class weapon (sell loot)" // too poor, but the junk loot pays for it: sell, then buy, one trip
	}
	// already standing in a town whose shop sells the next affordable upgrade: buy it now, no trip away from the
	// hunting ground (a bot respawned in its save town, or back from a run, walked out past the shop with 3,000z)
	// Solo bots only: a party shops together on its town runs (ai.go bestGearIn), nobody peels off passing through town.
	if tw := townOf(b.MapName); tw != "" && partyOf(b) == nil && !b.gearHold() {
		if it, s := b.nextGear(); it != nil && s != nil && (s.Map == tw || townOf(s.Map) == tw) && !b.gearIsFar(it.ID) {
			return "gear in town"
		}
	}
	// already standing in a town with a shop and over the regen line (no HP/SP regen at 50% weight): sell here,
	// that's no trip away from the hunting ground (Zendancer84 sat at 85% in Payon unable to regen SP)
	if b.overRegenWeight() && b.hasLoot() && potionShop(townOf(b.MapName)) != nil {
		return "selling in town"
	}
	// peco.go: a solo Knight/Crusader with Riding and no Peco makes ONE trip to Prontera's breeder (owner: Knights ride);
	// startTrip sends it to Prontera and sets a 3 h rest, so a failed rental can't loop
	if partyOf(b) == nil && b.pecoTown(time.Now()) != "" && time.Now().After(b.buffAt["peco:trip"]) {
		return "Peco Peco ride"
	}
	return ""
}

// fwTarget: Fly Wings a bot carries after a restock (danger.go escapes)
const fwTarget = 5

// flyWingsWanted: Fly Wings to buy from this buy list (target fwTarget) with the zeny left after potions
func (b *Bot) flyWingsWanted(pk []byte, budget int) int {
	have := 0
	if idx := b.invIndex(itemFlyWing); idx >= 0 {
		if it := b.inv[idx]; it != nil {
			have = it.Count
		}
	}
	for o := 4; o+13 <= len(pk); o += 13 {
		pr, id := int(le.Uint32(pk[o+4:])), int(le.Uint32(pk[o+9:]))
		if id == itemFlyWing && pr > 0 {
			return max(0, min(fwTarget-have, budget/pr))
		}
	}
	return 0
}

// bwTarget: Butterfly Wings a bot carries after a restock - one for a far town trip / Kafra shortcut and the
// escape reserve (the last one is never spent on travel: town.go tripWingOK, kafra.go kafraVia).
const bwTarget = 4 // owner: stock basic utilities; one for each town trip and one escape reserve, with room to spare

// wingSaveFloor (economy run 7): zeny a bot keeps for its gear before it buys Butterfly Wings (owner: town runs use a
// Butterfly Wing "when affordable"). A Butterfly Wing costs 1,000z on this server (rAthena RE item db), and topping up
// to 4 from every spare zeny after potions took ~700k/h of the ~2.0M/h the Legion earns from NPC loot sales (picklog
// 14:53-15:53): lv60-79 median purse 917z, nobody could save for gear or a 3,500z Peco.
func wingSaveFloor(lv int) int { return 1000 + 40*lv }

// wingPriceSeen (economy run 8): the Butterfly Wing price the NPC shops really list (Tool Dealer buy lists, seen by
// a bot without Discount). The owner moves it from 1,000z (rAthena RE db) to the official 300z through the rathena
// db-import override, live only at the next rathena recreate; the engine must budget right before and after.
var wingPriceSeen atomic.Int64

// wingPrice: the Butterfly Wing's NPC price - as last listed by a shop, else the item db (with the db-import override).
func wingPrice() int {
	if p := wingPriceSeen.Load(); p > 0 {
		return int(p)
	}
	if x := items[itemButterflyWing]; x != nil && x.Buy > 0 {
		return x.Buy
	}
	return 1000
}

// bwWant: Butterfly Wings worth carrying for this purse - none while poor (walk / Fly Wing escapes), 1 once 1.5
// wings are spare above the floor, 2 (a trip + the escape reserve) at 3 wings, bwTarget at 30 wings' worth (at
// 1,000z: 1,500 / 3,000 / 30,000z as in run 7; at 300z: 450 / 900 / 9,000z) (b.mu held)
func (b *Bot) bwWant() int { return b.bwWantParty(b.bwWantBase()) } // travelwing.go: Ayla's friends keep 2

func (b *Bot) bwWantBase() int {
	spare := b.Zeny - wingSaveFloor(b.BaseLv)
	wp := wingPrice()
	switch {
	case b.Zeny >= 30*wp && spare >= 3*wp:
		return bwTarget
	case spare >= 3*wp:
		return 2
	case spare >= wp*3/2:
		return 1
	}
	return 0
}

// butterflyWingsWanted: Butterfly Wings to buy from this buy list (up to bwWant) with the zeny left after potions
// (budget) above the saving floor, keeping the bag under the potion weight cap after the potions bought in the same
// deal (extraW).
func (b *Bot) butterflyWingsWanted(pk []byte, budget, extraW int) int {
	need := b.bwWant() - b.countItem(itemButterflyWing)
	pr := listedPrice(pk, itemButterflyWing)
	if pr > 0 && b.skillLv(skDiscount) == 0 {
		wingPriceSeen.Store(int64(pr)) // the real price on this server, before or after the db-import change
	}
	floor := wingSaveFloor(b.BaseLv)
	if partyWingStock(b.pin != nil || partyOf(b) != nil, b.countItem(itemButterflyWing)) {
		floor = 1000 // a party member's first 2 wings come before the gear savings (queue b17947117209mo)
	}
	budget -= floor
	if need <= 0 || pr <= 0 || budget < pr || b.MaxWeight <= 0 {
		return 0
	}
	room := (b.MaxWeight*tune().PotWeightCapPct/100 - b.Weight - extraW) / itemWeight(itemButterflyWing)
	return max(0, min(need, budget/pr, room))
}

// itemWeight: an item's weight in the server's units (10 = 1.0), 10 when unknown.
func itemWeight(id int) int {
	if x := items[id]; x != nil && x.Weight > 0 {
		return x.Weight
	}
	return 10
}

// fareHome: the Kafra fare (plus the Kafra's own keep) from town to the hunting map, when a teleport there (or to a
// town nearer it) saves walking; 0 when walking is short. Gear on a town run never eats it (QA 2026-11-14: a party
// spent down to 183z on Shoes and arrows, then walked 3-4 maps home for 12+ min).
func (b *Bot) fareHome(town string) int {
	goal := b.Goal.Map
	sp, ok := townSpawn[town]
	if goal == "" || !ok || goal == town || townOf(goal) == town {
		return 0
	}
	here := mapHops(town, sp[0], sp[1], goal)
	bh, fare := here-1, 0 // the return leg takes the Kafra for any saving (kafra.go kafraSaving)
	for _, d := range kafraDests[town] {
		if !b.kafraLandingOK(d, goal) {
			continue
		}
		if h := mapHops(d.Map, d.X, d.Y, goal) + 1; h <= bh {
			bh, fare = h, d.Price
		}
	}
	if fare == 0 {
		return 0
	}
	keep := tune().KafraKeep
	if b.BaseLvOr() < beginnerLv() {
		keep = tune().KafraKeepBeginner
	}
	return fare + keep
}

// gearKeep: zeny gear never spends on a town run: the fare home plus the cost of the missing potion minimum
func (b *Bot) gearKeep(town string) int {
	// the potion this bot restocks (potTierFor): an Orange or Yellow stock costs more than Red, and gear must not eat it
	pot := 50
	if x := items[potTierFor(b.BaseLvOr(), b.MaxHP)]; x != nil && x.Buy > 0 {
		pot = x.Buy
	}
	// batch 16: an hour's HP stock (hpPotTarget), not only the survival minimum, comes before gear
	return b.fareHome(town) + max(0, max(b.potMin(), b.hpPotTarget())-b.realPotCount())*pot + b.wingKitCost()
}

// wingKitCost (b.mu held): zeny for the missing utility wings (Butterfly to bwTarget, Fly to fwTarget). Owner: potions
// first, then wings, then gear. A bot with no weapon / no class weapon buys that first (it can't fight without it).
func (b *Bot) wingKitCost() int {
	if b.equipped[0x2] == nil || b.classWeaponMissing() {
		return 0
	}
	bwP, fwP := wingPrice(), 60
	if x := items[itemFlyWing]; x != nil && x.Buy > 0 {
		fwP = x.Buy
	}
	return max(0, b.bwWant()-b.countItem(itemButterflyWing))*bwP + max(0, fwTarget-b.countItem(itemFlyWing))*fwP
}

// tripGear: the plan's next affordable upgrade sold in the trip's town (after spending `spent` on potions)
func (b *Bot) tripGear(t *Trip, spent int) (*Item, *Shop) {
	if t.Why == "calling it a day" || t.Why == "gear" {
		return nil, nil
	}
	budget := b.Zeny - spent - max(b.gearReserve(), b.gearKeep(t.Town))
	if id := b.turnPiece(); id != 0 && !b.gearHold() { // lfg.go: only the turn piece the party's money is for
		if it := items[id]; it != nil && budget >= it.Buy {
			if sh := shopIn(id, t.Town); sh != nil {
				return b.gearFromShop(t, it, sh)
			}
		}
		return nil, nil
	}
	if b.gearHold() {
		// the class weapon is all this trip may buy at a gear shop (arrows ride along via t.Ammo; money otherwise waits)
		if b.classWeaponMissing() {
			if it, _ := b.classWeaponPick(budget); it != nil {
				if sh := shopIn(it.ID, t.Town); sh != nil {
					return b.gearFromShop(t, it, sh)
				}
			}
		}
		return nil, nil
	}
	// the party gear-turn item first: the zeny the party raised for it must turn into the item (gearpool.go)
	if x, sh := b.gearTurnIn(t.Town, budget); x != nil {
		return b.gearFromShop(t, x, sh)
	}
	it, s := b.nextGearBudget(budget)
	if it == nil || s == nil || (s.Map != t.Town && townOf(s.Map) != t.Town) {
		return nil, nil
	}
	return b.gearFromShop(t, it, s)
}

// gearFromShop (economy batch 13, vendbuy.go shopGearPick): on a Prontera trip, gear a south-gate player shop sells
// for no more than the NPC is bought there (browseShops at the end of the trip) instead of from the NPC.
func (b *Bot) gearFromShop(t *Trip, x *Item, sh *Shop) (*Item, *Shop) {
	if t.Town == "prontera" && x.Buy > 0 && b.charRole() != "vendor" && b.cheaperInShop(x.ID, x.Buy) {
		shopGearPick.Store(b, x.ID)
		econCount("trip gear: player shop instead of NPC")
		return nil, nil
	}
	return x, sh
}

// invSlots: inventory entries in use (RO bags hold 100 distinct entries)
func (b *Bot) invSlots() int {
	n := 0
	for _, it := range b.inv {
		if it.Count > 0 {
			n++
		}
	}
	return n
}

// bagFull: weight or slots at trip_bag_pct (70%) - the only "bag" reason for a town trip
func (b *Bot) bagFull() bool {
	p := tune().TripBagPct
	return b.weightPct() >= p || b.invSlots() >= p
}

// tripNumbers: the numbers that justified a trip, logged with every trip start so an unnecessary one shows at once
// (tools/sentinel.py parses this: "[weight 72%, slots 34/100, pots 12, sp 0, zeny 5400]")
func (b *Bot) tripNumbers() string {
	return fmt.Sprintf("[weight %d%%, slots %d/100, pots %d, sp %d, zeny %d]", b.weightPct(), b.invSlots(), b.potionCount(), b.spPotionCount(), b.Zeny)
}

func (b *Bot) startTrip(why string, now time.Time) {
	// the nearest town with a tool dealer, not the home town (bots hunting far from home used to walk across the
	// world to sell loot)
	town := nearestTown(b.MapName)
	if town == "" || potionShop(town) == nil {
		town = b.P.Home
	}
	if town == "" || potionShop(town) == nil {
		town = "prontera"
	}
	if why == "calling it a day" && b.P.Home != "" {
		town = b.P.Home // going home doesn't need a shop
	}
	if b.forceTown != "" && potionShop(b.forceTown) != nil {
		town = b.forceTown // party town run: everyone goes to the same town
	}
	if why == "Peco Peco ride" {
		b.buffAt["peco:trip"] = now.Add(pecoRetry)
	}
	if b.forceTown == "" && (partyOf(b) == nil || b.errand != nil) && why != "calling it a day" {
		if t := b.pecoTown(now); t != "" {
			town = t // peco.go: a solo Knight/Crusader rents its Peco on this trip (Prontera's breeder)
		}
	}
	if why == "no arrows" || why == "no arrows (sell loot)" {
		town = b.arrowTown(town) // a town whose shops sell arrows
	}
	if why == "no arrows (sell loot)" {
		why = "selling loot for arrows (necessary: no arrows)" // the Tool Dealer buys the junk, then the arrow seller
	}
	if why == "class weapon (sell loot)" {
		// sell junk loot at the Tool Dealer of the weapon shop's town, then tripGear buys the class weapon there
		it, s := b.classWeaponLootNeed()
		if it == nil || s == nil {
			return
		}
		town = townOf(s.Map)
		why = fmt.Sprintf("selling loot for %s (necessary: class weapon)", it.Name)
		b.noteGearStep(now)
	}
	t := &Trip{Stage: "go", Town: town, Shop: potionShop(town), Started: now, At: now, Why: why}
	// SP users also stock SP potions: if the Tool Dealer doesn't sell any, visit the town's SP potion shop after it
	if t.Shop != nil && why != "with the party" && why != "calling it a day" && why != "gear" && !shopSellsSP(t.Shop) {
		if s, price := spShop(town); s != nil && b.spPotsWanted(price) > 0 {
			t.Next = s
		}
	}
	noWeapon := why == "no weapon"
	classWeapon := why == "class weapon" // builds.go classWeaponNeed: a bow for an Archer, a rod for a Mage
	inTown := why == "gear in town"      // already in the shop's town: the next upgrade, no trip away
	if noWeapon || classWeapon || inTown {
		why, t.Why = "gear", "gear"
	}
	if why == "gear" {
		onRun := b.gearPick != 0 // bought on a party town run someone called for a real need
		it, s := b.nextGear()
		if noWeapon {
			it, s = b.weaponNeed()
		} else if classWeapon {
			it, s = b.classWeaponNeed()
		}
		if b.gearPick != 0 { // a party town run picked the best upgrade this town sells (ai.go bestGearIn)
			if x := items[b.gearPick]; x != nil {
				for _, sh := range gearShops(b.gearPick) {
					if townOf(sh.Map) == b.forceTown || sh.Map == b.forceTown {
						it, s = x, sh
						break
					}
				}
			}
			b.gearPick = 0
		}
		if it == nil || s == nil || classWeapon && gearSlot(it.Loc) != 0x2 {
			return // armor is never a necessary trip: it rides along on a real run ("(on the party town run)")
		}
		t.Shop, t.Town, b.gearItem = s, nearestTown(s.Map), it.ID
		if tw := townOf(s.Map); tw != "" {
			t.Town = tw
		}
		town = t.Town // the event used to name the potion town ("heading to izlude (buying Mace)" for a Prontera shop)
		why = "buying " + it.Name
		if onRun {
			why += " (on the party town run)"
		}
		if noWeapon {
			why += " (necessary: no usable weapon)"
		} else if classWeapon {
			why += " (necessary: " + b.weaponWhy(it) + ")"
			b.noteGearStep(now)
		} else if inTown {
			why += " (necessary: in town already)"
		} else if b.gearIsFar(it.ID) {
			b.farGearAt = now // one planned far trip per cooldown (builds.go farGearOK)
			why += fmt.Sprintf(" in %s (planned trip, %dz)", s.Map, it.Buy)
		}
		// below the survival minimum of real HP potions: the town's Tool Dealer first, then the gear shop (QA 2026-11-14:
		// party gear runs never met a Tool Dealer, Kimchi_07 Monk lv63 bought Guard, Shoes and Hat with 0 potions)
		// ... and below 2 Butterfly Wings (one for the next town trip, one escape reserve: owner, stock utilities)
		if b.realPotCount() < b.potMin() || b.countItem(itemButterflyWing) < min(2, b.bwWant()) {
			if ps := potionShop(t.Town); ps != nil && ps != t.Shop {
				t.Gear, t.Shop, t.Why = t.Shop, ps, "potions before gear"
				why += " (potions first)"
			}
		}
	}
	// arrows ride along on every trip of a bow user (or a bow purchase) whose shop doesn't sell them (ammo.go)
	if t.Shop != nil && t.Why != "calling it a day" && t.Why != "with the party" && !shopSells(t.Shop, itemArrow) {
		buyingBow := t.Why == "gear" && isBow(items[b.gearItem])
		if s := b.arrowShopIn(t.Town); s != nil && (b.arrowsWanted(1, buyingBow) > 0 || b.arrowLootOK()) { // loot sale pays
			t.Ammo = s
		}
	}
	if why == "no arrows" {
		why = "buying arrows (necessary: no arrows)"
	}
	if b.MapName != town {
		if idx := b.invIndex(itemButterflyWing); idx >= 0 && b.tripWingOK(town) {
			t.Stage = "wing"
			b.wingEvent(itemButterflyWing, "town trip: "+why, now)
			go b.useItem(idx)
		}
	}
	b.trip = t
	b.target, b.path, b.route = 0, nil, nil
	if b.noArrows() && t.Why != "calling it a day" {
		b.arrowNeedAt = now.Add(20 * time.Minute) // ammo.go: one arrow attempt per 20 min, whatever happens on it
	}
	if t.Why != "calling it a day" {
		b.lastTripAt = now
		b.lastTripOverLine = true // until the trip is done and shows the bag cleared (case "done")
	}
	b.event("info", "", fmt.Sprintf("heading to %s (%s) %s", town, why, b.tripNumbers()))
	if p := partyOf(b); p != nil && t.Why != "calling it a day" {
		b.tripAnnounce(why, town)
	}
	if p := partyOf(b); p != nil && t.Why == "calling it a day" {
		b.dayEndSay(p) // leadertalk.go: logging off for the day is said, not a silent vanish
	}
}

// tripWingOK (b.mu held): a town trip uses a Butterfly Wing only when the town is far (3+ maps away) or the bot is in
// trouble (no potions, under 30% HP); a party run from a field next to town walks (owner: don't waste money on
// travel; QA 2026-11-14: 73 of 114 Butterfly Wings in 5 min were town trips, every member winging on each run).
func (b *Bot) tripWingOK(town string) bool {
	if b.potionCount() == 0 || b.MaxHP > 0 && b.HP*100 < 30*b.MaxHP {
		return true
	}
	if townOf(b.MapName) == town {
		return false // a building of the town itself
	}
	// owner (2026-11-14): "when affordable, a town run should utilize butterfly wings for a quick teleport instead of
	// walking back. That saves time." Any spare wing is used; the last one stays for escapes (danger.go)
	// party town runs (owner 2026-11-14: "they need to use butterfly for the town run"): every member flies, even on
	// its last wing - it buys new ones in town (the potion stop above restocks below 2), the party returns together
	// ... but a Butterfly Wing lands on the SAVE point: only when that is the trip's town (QA 12:37: Ayla's friends winged
	// to Morocc / Prontera on her Payon run and walked back for minutes); otherwise walk / Kafra there
	if saved, known := b.knownSave(); !known || saved != town {
		// savepoints.go (owner 2026-11-15): on a party town run, a save point nearer the run town than this field ->
		// Butterfly Wing home, then the Kafra / walk to the meeting Kafra (not a long walk from the field)
		if p := partyOf(b); known && p != nil && b.countItem(itemButterflyWing) >= 1 {
			if at, _, _ := p.townRun(); time.Since(at) < 10*time.Minute &&
				saveWingHelps(townHops(saved, town), mapHops(b.MapName, b.X, b.Y, town)) {
				return true
			}
		}
		return false
	}
	if p := partyOf(b); p != nil && b.countItem(itemButterflyWing) >= 1 {
		if at, _, _ := p.townRun(); time.Since(at) < 10*time.Minute {
			return true
		}
	}
	return b.countItem(itemButterflyWing) >= 2
}

// tripThink returns true while a trip is in progress (it owns the bot's actions).
func (b *Bot) tripThink(gm *GameMap, moving bool, now time.Time) bool {
	t := b.trip
	if t == nil {
		return false
	}
	if now.Sub(t.Started) > 12*time.Minute || (t.Shop == nil && t.Why != "calling it a day" && t.Why != "with the party" && t.Why != "bag is heavy (store)") {
		if t.Why == "gear" && b.gearItem != 0 {
			b.gearFailed[b.gearItem] = true // couldn't get there: don't start the same trip again this session
			b.gearItem = 0
		}
		b.trip = nil // give up; try again later
		b.tripAgain = now.Add(5 * time.Minute)
		return false
	}
	b.setState("town trip: " + t.Stage)
	switch t.Stage {
	case "wing":
		if b.MapName == t.Town || now.Sub(t.At) > 8*time.Second {
			t.Stage, t.At = "go", now
		}
		return true
	case "go":
		if t.Shop == nil || t.Why == "with the party" || t.Why == "calling it a day" && b.potionCount() >= 3 && !b.hasLoot() {
			// just heading home: done once we're in town
			if b.MapName == t.Town {
				t.Stage = "done"
				return true
			}
			saved := b.Goal
			b.Goal = Goal{Mode: "town", Map: t.Town, Set: saved.Set}
			b.travel(gm, moving)
			b.Goal = saved
			return true
		}
		if b.MapName != t.Shop.Map {
			b.wrongRoomMap = "" // left the building: the next entry counts as a new try
			// already in the shop's town: walk to the door that leads to the shop's ROOM. travel() goes to the map's
			// nearest door: in Payon that is payon_in01's Tool Dealer room, so every weapon/armor trip entered the wrong
			// room 3 times and gave up ("its room can't be reached"); Zendancer84 never got a weapon.
			if tw := townOf(b.MapName); tw != "" && tw == townOf(t.Shop.Map) {
				// warploop.go: the route must end in the shop's ROOM - a route kept from a field (travel() to the nearest
				// door of the interior map) also "ends on t.Shop.Map" and took bots into the wrong room first
				if len(b.route) == 0 || b.route[0].From != b.MapName || !routeEndsIn(b.route, t.Shop.Map, t.Shop.X, t.Shop.Y) {
					b.route = RouteMapsPoint(b.MapName, b.X, b.Y, t.Shop.Map, t.Shop.X, t.Shop.Y, b.BaseLvOr())
					b.path = nil
				}
				if len(b.route) > 0 {
					if !moving {
						b.walkToPortal(gm, b.route[0]) // nav.go: lastWarp set first, or safeWalkDest stops us at the door
					}
					return true
				}
			}
			saved := b.Goal
			b.Goal = Goal{Mode: "town", Map: t.Shop.Map, Set: saved.Set}
			b.travel(gm, moving)
			b.Goal = saved
			return true
		}
		// stand on a walkable cell in front of the counter (nav.go talkCell), not "one cell beside the NPC", which was
		// often a wall: "no path on izlude_in from 65,87 to 61,126" every minute for 5 min (QA 2026-11-14)
		sx, sy, standOK := t.Shop.X, t.Shop.Y, true
		if gm != nil {
			sx, sy, standOK = talkCell(gm, b.X, b.Y, t.Shop.X, t.Shop.Y)
		}
		if dist(b.X, b.Y, t.Shop.X, t.Shop.Y) > 3 && !(standOK && dist(b.X, b.Y, sx, sy) <= 1) {
			// buildings like payon_in01 have separate rooms: entered through the wrong door, the shop's room can't be
			// reached from here (Zen.fox wandered 12 min). Route out and back in through the right entrance.
			if gm != nil && !standOK {
				// give up after two tries: the right room may not be reachable through the known doors (Zen.fox looped
				// prontera <-> prt_in for minutes)
				if b.lastWrongRoom != t.Started {
					b.lastWrongRoom, b.wrongRoomTries = t.Started, 0
				}
				if b.MapName != b.wrongRoomMap {
					b.wrongRoomMap = b.MapName
					b.wrongRoomTries++
				}
				if b.wrongRoomTries > 2 {
					if b.gearItem != 0 {
						b.gearFailed[b.gearItem] = true
					}
					b.event("info", "", "gave up on the shop in "+t.Shop.Map+": its room can't be reached")
					b.trip, b.route, b.path = nil, nil, nil
					return false
				}
				if len(b.route) == 0 || b.route[0].From != b.MapName || !routeEndsIn(b.route, t.Shop.Map, t.Shop.X, t.Shop.Y) {
					b.route = RouteMapsPoint(b.MapName, b.X, b.Y, t.Shop.Map, t.Shop.X, t.Shop.Y, b.BaseLvOr())
					b.path = nil
				}
				if len(b.route) > 0 && !moving {
					b.setState("town trip: wrong room, going round to the shop")
					b.walkToPortal(gm, b.route[0])
				}
				return true
			}
			if !moving && gm != nil {
				before := b.replanAt
				b.stepAlong(gm, sx, sy)
				if b.path == nil && b.replanAt != before { // FindPath just failed (a warp area in the way)
					t.NoPath++
					if t.NoPath > 2 {
						if b.gearItem != 0 {
							b.gearFailed[b.gearItem] = true
						}
						b.event("info", "", fmt.Sprintf("gave up on the shop at %s %d,%d: no path from %d,%d to %d,%d", t.Shop.Map, t.Shop.X, t.Shop.Y, b.X, b.Y, sx, sy))
						b.trip, b.route, b.path = nil, nil, nil
						b.tripAgain = now.Add(5 * time.Minute)
						return false
					}
				}
			}
			return true
		}
		t.Stage, t.At = "talk", now
		fallthrough
	case "talk":
		npc := b.npcAt(t.Shop.X, t.Shop.Y)
		if npc == 0 {
			if now.Sub(t.At) > 20*time.Second {
				b.trip = nil
			}
			return true
		}
		want := "buy"
		if t.Why == "gear" {
			want = "gear"
		}
		if t.Why == "alchemy" {
			want = "alchemy" // alchemy.go: potion bottles and Scell for the guild alchemist
		}
		if t.Why == "magnifier" {
			want = "magnifier" // identify.go: a vendor's Magnifiers for the gear it lists
		}
		if t.Why == "traps" {
			want = "traps" // trap.go: the trapper Hunter's supply trip
		}
		if b.hasLoot() && t.Sells < 2 { // at most two sell rounds per shop (a sell the server refused must not loop)
			want = "sell"
			t.Sells++
		}
		b.npcDeal = &npcDeal{npc: npc, want: want}
		t.Stage, t.At = want+"ing", now
		go b.send(newPkt(0x0090).u32(npc).u8(1).b)
		return true
	case "selling", "buying", "alchemying", "magnifiering", "trapsing":
		if now.Sub(t.At) > 10*time.Second { // server didn't answer: retry the conversation
			t.Stage, t.At = "talk", now
		}
		return true
	case "sold":
		t.Stage, t.At = "talk", now
		return true
	case "done":
		if !b.canStoreIn(b.MapName) { // a store step follows in a Kafra town (storage.go): it sets the flag then
			b.lastTripOverLine = b.overRegenWeight()
		}
		b.trip = nil
		b.startKafraMeet(t.Why, now) // kaframeet.go (PT22): a bot party's town run ends at the Kafra, the party returns together
		b.checkMarket(now)     // opportunity.go (run 8): what's missing in the market becomes the money goal
		b.browseShops(now)     // vendbuy.go (economy batch 9): look over the south gate shops before going back
		b.refineAfterTrip(now) // refine.go (batch 14): the weapon refined to its safe limit if the bag has the material
		if partyOf(b) != nil && t.Why != "calling it a day" {
			typing := b.P.Typing
			go b.partySay(styled(typing, pick("back", "back, omw to u", "done, coming back", "ok back")))
		}
		if t.Why == "calling it a day" {
			b.Goal = Goal{Mode: "idle", Map: b.MapName, Set: "auto"}
		}
		if t.Shop != nil && t.Why != "magnifier" {
			b.event("info", "", fmt.Sprintf("restocked: %d potions, %d zeny (bought-grade %d, starter kit %d)", b.potionCount(), b.Zeny, b.buyPotCount(), b.potionCount()-b.buyPotCount()))
		}
		return false
	}
	return true
}

func (b *Bot) npcAt(x, y int) uint32 {
	var best uint32
	bd := 3
	for _, e := range b.ents {
		if e.Type != objPC && e.Type != objMob && e.Type != objItem {
			if d := dist(e.X, e.Y, x, y); d < bd {
				best, bd = e.ID, d
			}
		}
	}
	return best
}

// valuableEtc: Etc items that players trade, never dump at an NPC (refine ores, gemstones, crafting materials):
// owner: "they are not selling any important items like cards or important gears right?"
var valuableEtc = map[int]bool{
	984: true, 985: true, 756: true, 757: true, // Oridecon, Elunium, Rough Oridecon, Rough Elunium
	1010: true, 1011: true, // Phracon, Emveretarcon
	715: true, 716: true, 717: true, // Yellow / Red / Blue Gemstone
	1000: true, 998: true, 999: true, // Star Crumb, Iron, Steel
	7054: true, 994: true, 995: true, 996: true, 997: true, // elemental ores: Flame Heart, Mystic Frozen, Rough Wind, Great Nature
	732: true, 731: true, 730: true, 729: true, 728: true, 727: true, 726: true, 725: true, 724: true, 723: true, 720: true, 719: true, 718: true, // jewels
	7023: true, 7019: true, 1039: true, 7063: true, 7101: true, // quest/craft-valuable drops
}

// npcJunk: what a bot sells to an NPC: Etc loot only, worth little. Cards (type Card), weapons, armor and the
// valuable Etc above are kept for players (trade, vending).
func npcJunk(x *Item) bool {
	return x.Type == "Etc" && !valuableEtc[x.ID] && sellPrice(x.ID) < 1500
}

// loot to sell: worthless Etc items, plus potions beyond what the bot carries (cards, gear and valuable materials
// are kept)
func (b *Bot) hasLoot() bool {
	for _, it := range b.inv {
		if b.npcSellable(it) { // junkgear.go: junk gear too
			return true
		}
	}
	return len(b.surplusPots()) > 0
}

// ---------------------------------------------------------------- consumables: how many potions, weight cap
// Lesson (2026-11-13): the buy step bought 30-60 Red Potions on EVERY trip regardless of stock, so Zendancer84
// (acolyte, heals itself) carried 256 (17,920 of 24,300 weight = 85%). rAthena stops natural HP/SP regen at 50%
// weight, so the party priest sat "resting for SP" at 66/325 forever. Now: a target count per class, buys never
// push the bag past pot_weight_cap_pct, surplus heavy potions are sold, and SP users stock SP potions too
// (owner: acolytes heal themselves, a few HP potions only; their zeny and weight go to SP potions).

// spPotionIDs: SP-restoring items, best first. NPCs here sell only Blue Syrup (Advanced Potion Merchants, Prontera
// south gate); the rest come from drops and players.
var spPotionIDs = []int{11624, 505, 11502, 533, 578, 518}

func isSPPotion(id int) bool {
	for _, x := range spPotionIDs {
		if x == id {
			return true
		}
	}
	return false
}

func (b *Bot) spPotionCount() int {
	n := 0
	for _, it := range b.inv {
		if isSPPotion(it.ItemID) {
			n += it.Count
		}
	}
	return n
}

// usesSP: the acolyte and mage lines, and anyone fighting with skills, need SP potions as well as HP potions
func (b *Bot) usesSP() bool {
	f := family(b.Class)
	return f == famAcolyte || f == famMage || b.bestAttack() != nil
}

// hpPotTarget: HP potions to carry. The acolyte line heals itself: a small emergency stock only.
func (b *Bot) hpPotTarget() int {
	if b.isHealer() {
		return tune().PotHPAcolyte
	}
	return 30 + int(b.P.Risk*30)
}

func (b *Bot) spPotTarget() int {
	if !b.usesSP() {
		return 0
	}
	return tune().PotSPTarget
}

func (b *Bot) weightPct() int {
	if b.MaxWeight <= 0 {
		return 0
	}
	return b.Weight * 100 / b.MaxWeight
}

// overRegenWeight: no natural HP/SP regen at this weight (rAthena: >= 50%); sitting to rest is pointless
func (b *Bot) overRegenWeight() bool {
	return b.MaxWeight > 0 && b.weightPct() >= tune().WeightRegenPct
}

// potRoom: how many more of this item fit before the bag passes the potion weight cap
func (b *Bot) potRoom(id int) int {
	if b.MaxWeight <= 0 {
		return 0
	}
	w := 10
	if x := items[id]; x != nil && x.Weight > 0 {
		w = x.Weight
	}
	room := b.MaxWeight*tune().PotWeightCapPct/100 - b.Weight
	if room <= 0 {
		return 0
	}
	return room / w
}

// spPotsWanted: how many SP potions at this price the bot would buy now (target, zeny, weight)
func (b *Bot) spPotsWanted(price int) int {
	need := b.spPotTarget() - b.spPotionCount()
	if need <= 0 || price <= 0 {
		return 0
	}
	// at most half the spare zeny goes to SP potions (the acolyte line: most of it); HP potions and gear need the rest
	spare := (b.Zeny - 500) / 2
	if b.isHealer() {
		spare = (b.Zeny - 500) * 3 / 4
	}
	return max(0, min(need, spare/price, b.potRoom(spPotionIDs[0])))
}

// surplusPots: inventory index -> how many to sell. Heavy HP potions (Red..White) beyond the bot's target, lowest
// grade sold first. Light starter potions weigh 1 and are kept.
// Only at or over the regen weight line (QA 2026-11-14: "sold 5 surplus potions (bag was 13%)" x15, LilLunablade with
// 8 bought-grade + 142 starter): below it the potions cost nothing to carry.
func (b *Bot) surplusPots() map[int]int {
	keep := max(b.hpPotTarget(), b.potMin())
	out := b.surplusConsumables() // food.go: Green Herbs, Fly Wings, fruit past their caps are always loot
	if !b.overRegenWeight() {
		return out
	}
	for id := itemWhitePotion; id >= itemRedPotion; id-- {
		if b.canBrew() && brewedKeep[id] > 0 {
			continue // alchemy.go: an alchemist's brewed potions are for players, never sold to an NPC
		}
		for _, it := range b.inv {
			if it.ItemID != id || it.Count <= 0 || it.Equipped {
				continue
			}
			k := min(keep, it.Count)
			keep -= k
			if it.Count > k {
				out[it.Index] = it.Count - k
			}
		}
	}
	return out
}

// shopSellsSP / spShop: SP potions sold by a shop / by a shop in the town or its buildings (with its price)
func shopSellsSP(s *Shop) bool {
	for _, id := range s.Items {
		if isSPPotion(id) {
			return true
		}
	}
	return false
}

func spShop(town string) (*Shop, int) {
	for _, mm := range append([]string{town}, townInteriors[town]...) {
		for _, s := range shopsByMap[mm] {
			for _, id := range s.Items {
				if isSPPotion(id) {
					if x := items[id]; x != nil && x.Buy > 0 {
						return s, x.Buy
					}
				}
			}
		}
	}
	return nil, 0
}

func (b *Bot) spPotionIndex() int {
	for _, id := range spPotionIDs {
		if idx := b.invIndex(id); idx >= 0 {
			return idx
		}
	}
	return -1
}

// drinkSP: drink an SP potion (at most one per second). True if one was used.
func (b *Bot) drinkSP(now time.Time) bool {
	if now.Before(b.potAt) {
		return false
	}
	if idx := b.spPotionIndex(); idx >= 0 {
		b.potAt = now.Add(time.Second)
		go b.useItem(idx)
		return true
	}
	return false
}

// potBuyPlan: what one Tool Dealer visit buys (best/price: the HP potion for this level, spBest/spPrice: the SP
// potion, ap: the arrow price; 0 = not sold). Priority (QA 2026-11-14: Zenrogue, Hunter lv52, "bought 0 HP + 0 SP
// potions, 839 arrows", then a 1,000z Hood, and left with only starter potions):
//  1. the survival minimum of real HP potions (potMin, counted without the starter kit), keeping only the zeny for
//     30 arrows when a bow user has none;
//  2. arrows (arrowsWanted, with the zeny left after 1);
//  3. SP potions, 4. the HP top-up to hpPotTarget, above the 500z reserve.
//
// shopCapPct: a restock leaves the bag at most this full (50% stops natural regen and forces a town run; run 7)
const shopCapPct = 40

// itemNPCPrice: an item's NPC buy price from the item db (0 = unknown).
func itemNPCPrice(id int) int {
	if x := items[id]; x != nil {
		return x.Buy
	}
	return 0
}

func (b *Bot) potBuyPlan(best, price, spBest, spPrice, ap int) (hpN, spN, arrN int) {
	// run 7 (main 20:28Z: xXRinfangXx bought 49 HP potions + 2,233 arrows + 4 Butterfly Wings and stood at 50% weight
	// one minute later, forcing a whole-party town run): every part of the restock used to measure its room against
	// the weight BEFORE the purchase. Now one shared weight budget: the bag ends at most at shopCapPct (40%).
	wLeft := 1 << 30
	if b.MaxWeight > 0 {
		wLeft = b.MaxWeight*shopCapPct/100 - b.Weight
	}
	take := func(n, w int) int { // n items of weight w that still fit; spends the budget
		if w <= 0 {
			return n
		}
		n = max(0, min(n, wLeft/w))
		wLeft -= n * w
		return n
	}
	room := 0
	if best > 0 && price > 0 {
		room = b.potRoom(best)
		arrMin := 0
		if b.bowUser() && b.arrowCount() == 0 {
			// no arrows: up to 200 arrows come before the potions, also when THIS shop sells none and the arrow seller
			// is the trip's next stop (QA 05:48 wolfclaw70: loot sold -> 370z, 5 Red Potions first, then only 69 arrows)
			pr := ap
			if pr <= 0 {
				pr = arrowPrice()
			}
			arrMin = max(minArrowTrip, min(minArrowLootTrip*2, (b.Zeny-arrowReserve)/pr)) * pr
		}
		hpN = max(0, min(b.potMin()-b.realPotCount(), (b.Zeny-arrowReserve-arrMin)/price, room))
		if b.realPotCount() > 0 { // with no potion at all the survival minimum always comes
			hpN = take(hpN, itemWeight(best))
		} else {
			wLeft -= hpN * itemWeight(best)
		}
	}
	reserve := 500
	if b.guideSaving() {
		// economy batch 14: the guild's guide fund is not potion money - only the survival minimum (above) comes out
		// of it. LilTacox spent 7,500z of the fund on 49 potions in one restock (8,925 -> 537z).
		reserve = max(reserve, b.gearReserve())
	}
	// run 8 (main 20:56Z: Dark_blade, Hunter 74, bought 3,000 arrows and 0 HP / SP potions / Butterfly Wings): the
	// order is HP minimum -> wings (their money is held back here; town.go buys them right after this plan) -> SP
	// potions -> arrows -> HP top-up. Arrows used to come second and spend everything above 300z.
	wingRes := 0
	if b.arrowCount() > 0 || !b.bowUser() {
		wingRes = max(0, b.bwWant()-b.countItem(itemButterflyWing))*wingPrice() +
			max(0, fwTarget-b.countItem(itemFlyWing))*max(itemNPCPrice(itemFlyWing), 60)
	}
	budget := b.Zeny - reserve - hpN*price - wingRes
	if spBest > 0 && spPrice > 0 {
		spN = take(max(0, min(b.spPotsWanted(spPrice), budget/spPrice)), itemWeight(spBest))
		budget -= spN * spPrice
	}
	if ap > 0 {
		z := b.Zeny
		if b.arrowCount() > 0 {
			b.Zeny = max(0, budget) + 300 // arrowsWanted keeps 300z itself: arrows get what is left after the rest
		} else {
			b.Zeny -= hpN * price // none at all: arrows still come right after the HP minimum (can't fight without)
		}
		arrN = b.arrowsWanted(ap, false)
		b.Zeny = z
		if b.arrowCount() > 0 {
			arrN = take(arrN, arrowWeight()) // with none at all arrowsWanted's own 200-arrow floor stands
		} else {
			wLeft -= arrN * arrowWeight()
		}
		budget -= arrN * ap
	}
	if best > 0 && price > 0 {
		// economy run 8: the HP top-up above the survival minimum keeps the wing saving floor (1,000 + 40 x lv) in
		// the purse. Restocks spent every zeny above 500z on 30-60 potions (1,009 restocks: median 1,049z left, 169
		// under 300z), so nobody could save for gear, a Peco or a Butterfly Wing. potMin still comes first (above).
		if b.BaseLvOr() >= 20 && !b.guideSaving() {
			budget -= max(0, wingSaveFloor(b.BaseLvOr())-reserve)
		}
		hpN += take(max(0, min(budget/price, b.hpPotTarget()-b.realPotCount()-hpN, room-hpN)), itemWeight(best))
	}
	return hpN, spN, arrN
}

// needsPots: below the HP or SP potion stock it wants (and could buy some): business on a party town run
func (b *Bot) needsPots() bool {
	if b.realPotCount() < max(b.potMin(), min(10, b.hpPotTarget())) {
		return true
	}
	_, price := spShop(nearestTown(b.MapName))
	return b.spPotionCount() < b.spPotTarget()/2 && b.spPotsWanted(price) > 0
}

// onDeal handles NPC shop and vending dialog packets (b.mu held).
func (b *Bot) onDeal(op uint16, pk []byte) {
	switch op {
	case 0x012d: // vending: server asks for the shop contents
		b.openShop(int(le.Uint16(pk[2:])))
		return
	}
	d := b.npcDeal
	if d == nil {
		return
	}
	switch op {
	case 0x00c4:
		t := byte(0)
		if d.want == "sell" {
			t = 1
		}
		go b.send(newPkt(0x00c5).u32(d.npc).u8(t).b)
	case 0x00c7: // sell list: junk loot, and potions beyond the bot's stock (consumables, never cards/gear/materials)
		p := newPkt(0x00c9).u16(0)
		n := 0
		surplus := b.surplusPots()
		sold := 0
		for o := 4; o+10 <= len(pk); o += 10 {
			idx := int(le.Uint16(pk[o:]))
			it := b.inv[idx]
			if it == nil {
				continue
			}
			oc := int(le.Uint32(pk[o+6:])) // the price with Overcharge
			if b.npcSellable(it) { // junk loot and junk gear (junkgear.go)
				p.u16(uint16(idx)).u16(uint16(it.Count))
				n++
				noteMarketFlow("npc_sell", oc*it.Count, it.ItemID, 0)
			} else if c := surplus[idx]; c > 0 {
				p.u16(uint16(idx)).u16(uint16(c))
				n++
				sold += c
				noteMarketFlow("npc_sell", oc*c, it.ItemID, 0)
			}
		}
		if n > 0 {
			go b.send(p.varlen())
		} else {
			go b.send(newPkt(0x09d4).b) // nothing to sell: close the shop window, or sd->npc_shopid freezes walking
		}
		if sold > 0 {
			b.event("info", "", fmt.Sprintf("sold %d surplus potions (bag was %d%%)", sold, b.weightPct()))
		}
		if b.trip != nil {
			b.trip.Stage, b.trip.At = "sold", time.Now()
		}
		b.npcDeal = nil
	case 0x00c6: // buy list
		if d.want == "alchemy" {
			b.alchemyBuy(pk) // alchemy.go
			return
		}
		if d.want == "magnifier" {
			b.magnifierBuy(pk) // identify.go
			return
		}
		if d.want == "traps" {
			b.trapBuy(pk) // trap.go
			return
		}
		if d.want == "gear" {
			bought, listed, price, boughtID, arrCost := false, false, 0, 0, 0
			fare := 0 // the Kafra fare home stays in the purse (a bot with no weapon at all buys one anyway)
			if t := b.trip; t != nil && b.equipped[0x2] != nil {
				fare = b.fareHome(t.Town)
			}
			for o := 4; o+13 <= len(pk); o += 13 {
				pr, id := int(le.Uint32(pk[o+4:])), int(le.Uint32(pk[o+9:]))
				if id == b.gearItem {
					listed, price = true, pr
				}
				if id == b.gearItem && pr <= b.Zeny-fare {
					bought, boughtID = true, id
					bp := newPkt(0x00c8).u16(0).u16(1).u32(uint32(id))
					// a bow (or a bow user's upgrade): arrows from the same shop in the same buy (ammo.go)
					if ap := listedPrice(pk, itemArrow); ap > 0 {
						zeny := b.Zeny
						b.Zeny -= pr
						n := b.arrowsWanted(ap, isBow(items[id]))
						b.Zeny = zeny
						if n > 0 {
							bp.u16(uint16(n)).u32(itemArrow)
							arrCost = n * ap
							b.event("info", "", fmt.Sprintf("bought %d arrows", n))
						}
					}
					go b.send(bp.varlen())
					if x := items[id]; x != nil {
						b.event("info", "", fmt.Sprintf("bought %s for %dz", x.Name, pr))
						noteMarketFlow("npc_buy", pr, x.ID, 0) // marketflow.go (run 11)
					}
					b.earmarkZ = max(0, b.earmarkZ-pr) // gearpool.go giftableZeny: the gift became the gear it was for
				}
			}
			if !bought {
				go b.send(newPkt(0x09d4).b) // close the shop window (see below)
			}
			if !bought && b.gearItem != 0 && !(listed && price <= b.Zeny) { // listed but only short of the fare: try again later
				// don't plan the same trip again this session: the shop doesn't list it (wrong NPC) or it costs more
				b.gearFailed[b.gearItem] = true
				b.event("info", "", fmt.Sprintf("could not buy %s here (listed %v, price %d, zeny %d)", itemName(b.gearItem), listed, price, b.Zeny))
			}
			b.gearItem = 0
			// QA 2026-11-14: one bot made a Katana trip, then a Suit trip, then a Sandals trip. Every affordable step this
			// town sells is bought on the SAME visit (up to 2 more), never a trip of its own
			// QA 2026-11-14 Star claw: Wooden Mail, Shoes, then Sandals (worse, same slot); LilStormsoul left with 183z and
			// the party walked home for 12 min. One item per slot per trip (nextGearBudget skips t.Slots), and the chain
			// keeps the Kafra fare home plus the potion minimum (gearKeep)
			if t := b.trip; bought && t != nil {
				if x := items[boughtID]; x != nil {
					if t.Slots == nil {
						t.Slots = map[uint32]bool{}
					}
					t.Slots[gearSlot(x.Loc)] = true
				}
			}
			if t := b.trip; bought && t != nil && t.Extra < 2 {
				if it, s := b.nextGearBudget(b.Zeny - price - arrCost - max(b.gearReserve(), b.gearKeep(t.Town))); it != nil && s != nil && it.ID != boughtID &&
					(s.Map == t.Town || townOf(s.Map) == t.Town) {
					t.Extra++
					b.gearItem, t.Shop, t.Why, t.Stage, t.At, t.Sells = it.ID, s, "gear", "go", time.Now(), 0
					b.event("info", "", fmt.Sprintf("also buying %s in %s on this trip", it.Name, s.Map))
					b.npcDeal = nil
					return
				}
			}
			if t := b.trip; t != nil && t.Ammo != nil && (b.arrowCount() == 0 || bought && isBow(items[boughtID])) &&
				listedPrice(pk, itemArrow) == 0 {
				t.Shop, t.Ammo, t.Why, t.Stage, t.At, t.Sells = t.Ammo, nil, "ammo", "go", time.Now(), 0 // on to the arrow seller
				b.npcDeal = nil
				return
			}
			if b.trip != nil {
				b.trip.Stage, b.trip.At = "done", time.Now()
			}
			b.npcDeal = nil
			return
		}
		spBest, spPrice := 0, 0
		lv := b.BaseLvOr()
		for o := 4; o+13 <= len(pk); o += 13 {
			pr, id := int(le.Uint32(pk[o+4:])), int(le.Uint32(pk[o+9:]))
			if isSPPotion(id) && pr > 0 && (spBest == 0 || pr < spPrice) {
				spBest, spPrice = id, pr
			}
		}
		// the HP potion to buy from this list: the strongest one that suits the bot's level and max HP and that it can
		// afford in a real stock (pickHPPotion). Red Potions (+45 HP) are no restock for a 5,000 HP bot.
		best, price := pickHPPotion(pk, lv, b.MaxHP, b.Zeny)
		// HP potions up to the target count, SP potions for SP users; neither pushes the bag past the weight cap
		p := newPkt(0x00c8).u16(0)
		vendorPots := townOf(b.MapName) == "prontera" && best > 0 && price > 0 && b.realPotCount() >= b.potMin() &&
			b.cheaperInShop(best, price*95/100) // run 11 (owner: Prontera vendors beat the NPC): browseShops buys them
		bought := 0
		// order (potBuyPlan): the survival minimum of real HP potions, arrows, SP potions, then the HP top-up; gear
		// (tripGear) only gets what is left. One failed total = nothing bought.
		arrID, ap := arrowChoice(pk, b.huntMap(), b.Zeny) // ammo.go: the arrow that fits the hunting map
		hpN, spN, arrN := b.potBuyPlan(best, price, spBest, spPrice, ap)
		if vendorPots && hpN > 0 {
			shopPotPick.Store(b, best)
			econCount("npc potions skipped: cheaper at a vendor")
			hpN = 0
		}
		budget := b.Zeny - 500 - arrN*ap - spN*spPrice
		if arrN > 0 {
			p.u16(uint16(arrN)).u32(uint32(arrID))
			bought++
		}
		if hpN > 0 {
			p.u16(uint16(hpN)).u32(uint32(best))
			bought++
		}
		if spN > 0 {
			p.u16(uint16(spN)).u32(uint32(spBest))
			bought++
		}
		// Butterfly Wings: the escape reserve (danger.go keeps the last one; town trips / Kafra shortcuts only use one
		// when 2+ are carried). QA 2026-11-14: 0 bought since 08:00 against ~700 used an hour - escapes ran dry.
		// Topped up to 3 on this run only (never a trip of its own), from spare zeny, under the potion weight cap.
		spW := 0
		if spBest > 0 {
			spW = spN * itemWeight(spBest)
		}
		bw := b.butterflyWingsWanted(pk, budget-hpN*price, hpN*itemWeight(best)+spW+arrN*arrowWeight()) // run 7: arrows weigh too
		if bw > 0 {
			p.u16(uint16(bw)).u32(uint32(itemButterflyWing))
			bought++
			budget -= bw * listedPrice(pk, itemButterflyWing)
		}
		// a few Fly Wings to teleport out of a losing fight (danger.go escape); never more than spare zeny allows
		if fw := b.flyWingsWanted(pk, budget-hpN*price); fw > 0 {
			p.u16(uint16(fw)).u32(uint32(itemFlyWing))
			bought++
		}
		// poison.go: a few Green Potions (cure poison) for a bot hunting among poisoners or poisoned lately, no Cure skill
		if gn, gp := b.greensWanted(pk, budget-hpN*price-300); gn > 0 {
			p.u16(uint16(gn)).u32(uint32(itemGreenPot))
			bought++
			budget -= gn * gp
			b.event("info", "", fmt.Sprintf("bought %d Green Potions (poison cure) for %s", gn, b.Goal.Map))
		}
		// a Magnifier for an unidentified drop worth wearing (identify.go)
		if mg := b.magnifiersWanted(pk, budget-hpN*price-300); mg > 0 {
			p.u16(uint16(mg)).u32(uint32(itemMagnifier))
			bought++
		}
		// run 11 (owner: the stock "Extended" Tool Dealers stay - White Potion, Yggdrasil Leaf, Blue Gemstone): gemstones
		// for the skills that eat them, and one Yggdrasil Leaf for a well-off lv60+ party member without a priest's
		// Resurrection, wherever a shop lists them
		if gn, gp := b.gemsWanted(pk, budget-hpN*price-300); gn > 0 {
			p.u16(uint16(gn)).u32(uint32(itemBlueGem))
			bought++
			budget -= gn * gp
			b.event("info", "", fmt.Sprintf("bought %d Blue Gemstones for %dz", gn, gn*gp))
		}
		if yp := listedPrice(pk, itemYggLeaf); yp > 0 && b.yggWanted(budget-hpN*price, yp) {
			p.u16(1).u32(uint32(itemYggLeaf))
			bought++
			budget -= yp
			b.event("info", "", fmt.Sprintf("bought a Yggdrasil Leaf for %dz", yp))
		}
		// trap.go: a trapper Hunter tops up its Traps wherever they are sold (Izlude, Prontera's Goods Merchant...)
		if tp := listedPrice(pk, itemTrap); tp > 0 {
			if tn := b.trapsWanted(tp, budget-hpN*price); tn > 0 {
				p.u16(uint16(tn)).u32(uint32(itemTrap))
				bought++
				b.event("info", "", fmt.Sprintf("bought %d Traps", tn))
			}
		}
		if bought > 0 {
			for o := 4; o+6 <= len(p.b); o += 6 { // marketflow.go (run 11): what the NPC got
				n, id := int(le.Uint16(p.b[o:])), int(le.Uint32(p.b[o+2:]))
				noteMarketFlow("npc_buy", n*listedPrice(pk, id), id, 0)
			}
			go b.send(p.varlen())
			b.event("info", "", fmt.Sprintf("bought %d HP + %d SP potions, %d arrows, %d Butterfly Wings (bag %d%%)", hpN, spN, arrN, bw, b.weightPct()))
		} else {
			// nothing to buy: close the shop window (CZ_NPC_TRADE_QUIT). rAthena sets sd->npc_shopid when the buy/sell
			// window opens and clears it only on a buy/sell list or this packet; while it is set pc_cant_act() refuses
			// every walk (QA 2026-11-14: wolfdancer frozen at alberta_in 180,94 next to the Tool Dealer, 13 stuck tries)
			go b.send(newPkt(0x09d4).b)
		}
		// an arrow seller (or a shop we believed sold arrows) that gave no arrows to a bow user with none: never loop (ammo.go)
		if t := b.trip; t != nil && t.Shop != nil && arrN == 0 && b.noArrows() && (t.Why == "ammo" || shopSells(t.Shop, itemArrow)) {
			b.arrowVisitFailed(t.Shop, listedPrice(pk, itemArrow) > 0, time.Now())
		}
		if t := b.trip; t != nil {
			if t.Next != nil { // on to the SP potion shop
				t.Shop, t.Next, t.Stage, t.At, t.Sells = t.Next, nil, "go", time.Now(), 0
			} else if t.Ammo != nil && arrN == 0 && t.Ammo != t.Shop { // on to the arrow seller (ammo.go)
				t.Shop, t.Ammo, t.Why, t.Stage, t.At, t.Sells = t.Ammo, nil, "ammo", "go", time.Now(), 0
			} else if t.Gear != nil { // potions bought: on to the gear shop the trip was planned for
				t.Shop, t.Gear, t.Why, t.Stage, t.At, t.Sells = t.Gear, nil, "gear", "go", time.Now(), 0
			} else if it, s := b.tripGear(t, hpN*price+spN*spPrice+arrN*ap); it != nil {
				// one trip does it all (owner: gear is bought on the real runs, never a trip of its own): the plan's next
				// upgrade, if a shop in this same town sells it
				b.gearItem, t.Shop, t.Why, t.Stage, t.At, t.Sells = it.ID, s, "gear", "go", time.Now(), 0
				b.event("info", "", fmt.Sprintf("also buying %s in %s on this trip", it.Name, s.Map))
			} else {
				t.Stage, t.At = "done", time.Now()
			}
		}
		b.npcDeal = nil
	}
}

// isHealPotion: HP potions bots drink (Novice Potion, Red..White, Light Yellow/White from the starter kit).
func isHealPotion(id int) bool {
	return (id >= 501 && id <= 504) || id == 569 || id == 11500 || id == 11501
}
