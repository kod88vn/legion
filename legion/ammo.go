package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ---------------------------------------------------------------- ammo: arrows for bow users
// Lesson (QA 2026-11-14 02:20): the class-weapon fix gave every Archer a Bow, but all 86 online Archers with a bow
// had NO arrows (none in the bag, none equipped): rAthena refuses a bow attack without ammo, so they "fought" with
// full SP and 0 kills. Arrows are a consumable like potions: bought on every Tool Dealer / shop visit that lists them
// (and on the bow purchase itself), kept inside the potion weight room, equipped into the ammo slot (EQP_AMMO 0x8000)
// at once, and "no arrows with a bow" is a necessary trip. Meanwhile the bot fights with its melee weapon (Knife).

const (
	itemArrow = 1750
	eqpAmmo   = 0x8000 // rAthena EQP_AMMO; the server answers an ammo equip with 0x013c, not 0x0999
)

// isArrow: ammo a bow fires (Arrow, Silver Arrow, Fire Arrow ...)
func isArrow(id int) bool {
	if x := items[id]; x != nil {
		return x.Type == "Ammo" && x.Sub == "Arrow"
	}
	return id == itemArrow
}

func isBow(x *Item) bool { return x != nil && x.Sub == "Bow" }

// arrowCount: arrows carried (the equipped stack is in the bag too)
func (b *Bot) arrowCount() int {
	n := 0
	for _, it := range b.inv {
		if it.Count > 0 && isArrow(it.ItemID) {
			n += it.Count
		}
	}
	return n
}

// bowEquipped: a bow in the weapon hand
func (b *Bot) bowEquipped() bool {
	w := b.equipped[0x2]
	return w != nil && w.Count > 0 && isBow(items[w.ItemID])
}

// bowInBag: a bow this character can wear, not in hand (it switched to a melee weapon for want of arrows)
func (b *Bot) bowInBag() *InvItem {
	for _, it := range b.inv {
		if x := items[it.ItemID]; !it.Equipped && isBow(x) && b.wearable(it, x) {
			return it
		}
	}
	return nil
}

func (b *Bot) bowUser() bool { return b.bowEquipped() || b.bowInBag() != nil }

// arrowTarget: arrows a bow user keeps (rises with level: more shots per minute, more zeny)
func (b *Bot) arrowTarget() int {
	// batch 22 (build-and-strategy agent): bow users fire ~90 arrows/min and ran dry 10-35 min after a restock (target
	// was 500 + lv x 10), then fought with a Knife. Now about half an hour to an hour of shooting, within the weight
	// room (potRoom) - arrows are 1z each.
	// run 8: enough until the next real trip, not a hoard (main 20:56Z: Dark_blade bought 3,000 arrows and no potions /
	// wings); the restock buys them after the HP minimum, wings and SP potions, within the 40% weight budget
	return min(2000, 800+b.BaseLvOr()*20)
}

// arrowReserve: the zeny a bow user with NO arrows keeps when it buys them (arrows ~1z: 100-200 let it earn again).
// arrowsWanted is the ONE rule for both the "no arrows" trigger and every buy step.
const arrowReserve = 50

// minArrowTrip: a "no arrows" trip only when it can buy at least this many
const minArrowTrip = 30

// minArrowLootTrip: a "no arrows (sell loot)" trip only when the junk sale pays for at least this many arrows
const minArrowLootTrip = 100

// arrowLootOK (b.mu held): a bow user with no arrows that can't afford minArrowTrip, but 90% of its junk loot's NPC
// value (npcJunk only: never cards, gear or valuable materials - RULES.md) pays for minArrowLootTrip+ arrows.
// Lesson (QA 2026-11-14 05:40): Thornlord lv49, kimchiknight lv46, Zero boy81 lv49, Kimchiboy (72 loot items) sat at
// 46-50z with no arrows: the 50z arrowReserve + the 30-arrow minimum blocked them forever while they carried junk
// loot that sells for hundreds. Same path as the class weapon (builds.go classWeaponLootNeed): sell, then buy.
func (b *Bot) arrowLootOK() bool {
	if !b.noArrows() || !b.hasLoot() || b.arrowsWanted(arrowPrice(), false) >= minArrowTrip {
		return false
	}
	loot, lootW := 0, 0
	for _, it := range b.inv {
		if x := items[it.ItemID]; x != nil && b.npcSellable(it) {
			loot += x.Buy / 2 * it.Count
			lootW += x.Weight * it.Count
		}
	}
	// arrowsWanted with the purse and the bag as they will be after the sale
	z, w := b.Zeny, b.Weight
	b.Zeny, b.Weight = b.Zeny+loot*9/10, max(0, b.Weight-lootW)
	n := b.arrowsWanted(arrowPrice(), false)
	b.Zeny, b.Weight = z, w
	return n >= minArrowLootTrip
}

// arrowsWanted: arrows to buy at this price (bow users, or a bot buying a bow now), within zeny and the weight room
func (b *Bot) arrowsWanted(price int, buyingBow bool) int {
	if price <= 0 || !buyingBow && !b.bowUser() {
		return 0
	}
	need := b.arrowTarget() - b.arrowCount()
	if need < b.arrowTarget()/4 && b.arrowCount() > 0 {
		return 0 // stocked: no top-up of a few dozen
	}
	need = max(need, 0)
	spare := b.Zeny - 300 // batch 22: arrows are a bow user's potions - after the potion minimum, before gear
	if b.arrowCount() == 0 {
		// none at all: arrows FIRST, before potions and the 500z potion reserve (QA 08:40: every "could not buy arrows
		// (listed true)" had 345-468z: the trigger kept 300z but the buy step kept 500z + SP potions first, so 0 arrows)
		spare = b.Zeny - arrowReserve
	}
	room := b.potRoom(itemArrow)
	if b.arrowCount() == 0 {
		// none at all: an arrow weighs 0.1 - the potion weight cap (45%) must not stop the first 200 (QA 2026-11-14
		// LilRinkun at 46% weight: "bought 0 arrows" on every run, so its Bow stayed in the bag and it fought with a
		// Knife). Room up to the 50% regen line, and at least 200 below the urgent line.
		room = max(room, (b.MaxWeight*tune().WeightRegenPct/100-b.Weight)/arrowWeight())
		if b.MaxWeight > 0 && b.Weight+200*arrowWeight() < b.MaxWeight*tune().TripUrgentWeightPct/100 {
			room = max(room, 200)
		}
	}
	return max(0, min(need, spare/price, room))
}

// arrowWeight: the weight units of one Arrow (item_db 0.1 -> 1)
func arrowWeight() int {
	if x := items[itemArrow]; x != nil && x.Weight > 0 {
		return x.Weight
	}
	return 1
}

// arrowShop: a shop in the town or one of its buildings that sells Arrows (skipping shops in `failed`, keyed by shopKey)
func arrowShop(town string, failed map[string]bool) *Shop {
	for _, mm := range append([]string{town}, townInteriors[town]...) {
		for _, s := range shopsByMap[mm] {
			if shopSells(s, itemArrow) && !failed[shopKey(s)] {
				return s
			}
		}
	}
	return nil
}

// arrowShopIn (b.mu held): the town's arrow seller this bot hasn't found empty this session
func (b *Bot) arrowShopIn(town string) *Shop { return arrowShop(town, b.arrowFailed) }

func shopKey(s *Shop) string { return fmt.Sprintf("%s,%d,%d", s.Map, s.X, s.Y) }

func shopSells(s *Shop, id int) bool {
	if s == nil {
		return false
	}
	for _, x := range s.Items {
		if x == id {
			return true
		}
	}
	return false
}

// arrowTown: the town to get arrows in: the given one (nearest town / the party's run town) when it has an arrow
// seller and a potion shop, else the town with an arrow seller fewest warps away. QA 2026-11-14 07:50: arrow trips
// went to Geffen/Alberta whose (barter-era) Tool Dealers sell no arrows - 0 of 41 bots got arrows there.
func (b *Bot) arrowTown(first string) string {
	ok := func(t string) bool { return t != "" && b.arrowShopIn(t) != nil && potionShop(t) != nil }
	if ok(first) {
		return first
	}
	towns := make([]string, 0, len(townSpawn))
	for t := range townSpawn {
		towns = append(towns, t)
	}
	sort.Strings(towns)
	best, bl := "", 999
	for _, t := range towns {
		if !ok(t) {
			continue
		}
		hops := 0
		if townOf(b.MapName) != t {
			r := RouteMaps(t, townSpawn[t][0], townSpawn[t][1], b.MapName)
			if r == nil {
				continue
			}
			hops = len(r)
		}
		if hops < bl {
			best, bl = t, hops
		}
	}
	if best != "" {
		return best
	}
	return first
}

// arrowVisitFailed (b.mu held): an arrow seller's buy list had no arrows (or nothing was bought): never again this
// session when it doesn't list them, and the "no arrows" need rests 20 min, so a failed trip can't loop (kimchiknight
// went geffen_in -> "restocked" -> "heading to geffen (buying arrows)" in the same second, again and again).
func (b *Bot) arrowVisitFailed(s *Shop, listed bool, now time.Time) {
	if !listed {
		if b.arrowFailed == nil {
			b.arrowFailed = map[string]bool{}
		}
		b.arrowFailed[shopKey(s)] = true
	}
	b.arrowNeedAt = now.Add(20 * time.Minute)
	b.event("info", "", fmt.Sprintf("could not buy arrows at %s %s (listed %v, zeny %d)", s.Name, shopKey(s), listed, b.Zeny))
}

// applyBarterDealers: rAthena's npc/re/merchants/Dealer_Update.txt runs at OnInit and, with feature.barter on (the
// default, and this server), UNLOADS the old Tool Dealers (Tool Dealer#gef, #alb2, #pay, #prt1, #moc3 ... which list
// Arrows) in favour of "Tool Dealer#Extended_*" at the same spots (no Arrows), and deletes Arrows from the Weapon
// Dealers. The engine parses the shop files statically, so it believed those shops sold Arrows. Apply the same edits.
func applyBarterDealers(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	unload, delItems := map[string]bool{}, map[string][]int{}
	reName := regexp.MustCompile(`^\s*(unloadnpc|npcshopdelitem)\s+"([^"]+)"\s*(.*?);`)
	in := false
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		l := sc.Text()
		if strings.Contains(l, `getbattleflag("feature.barter")`) {
			in = true
			continue
		}
		if in && strings.Contains(l, "} else {") {
			break
		}
		m := reName.FindStringSubmatch(l)
		if !in || m == nil {
			continue
		}
		if m[1] == "unloadnpc" {
			unload[m[2]] = true
			continue
		}
		for _, e := range strings.Split(m[3], ",") {
			if id, err := strconv.Atoi(strings.TrimSpace(e)); err == nil {
				delItems[m[2]] = append(delItems[m[2]], id)
			}
		}
	}
	nu, nd := 0, 0
	for mp, list := range shopsByMap {
		kept := list[:0]
		for _, s := range list {
			if unload[s.ID] {
				nu++
				continue
			}
			if del := delItems[s.ID]; len(del) > 0 {
				var its []int
				for _, id := range s.Items {
					drop := false
					for _, d := range del {
						drop = drop || d == id
					}
					if !drop {
						its = append(its, id)
					}
				}
				nd += len(s.Items) - len(its)
				s.Items = its
			}
			kept = append(kept, s)
		}
		shopsByMap[mp] = kept
	}
	log.Printf("barter dealers: %d shops unloaded, %d shop items removed (%s)", nu, nd, filepath.Base(path))
}

// noArrows: a bow user with no arrows at all (an URGENT need: it can't attack with the bow)
func (b *Bot) noArrows() bool {
	return b.invReady() && b.bowUser() && b.arrowCount() == 0
}

// listedPrice: the price of item id in an NPC buy list (0x00c6), 0 when not listed
func listedPrice(pk []byte, id int) int {
	for o := 4; o+13 <= len(pk); o += 13 {
		if int(le.Uint32(pk[o+9:])) == id {
			return int(le.Uint32(pk[o+4:]))
		}
	}
	return 0
}

// ammoThink (b.mu held, from progress): arrows into the ammo slot whenever a bow is in hand and the slot is empty;
// a bow user out of arrows takes its melee weapon instead (equipPass won't put the bow back until arrows arrive).
func (b *Bot) ammoThink(now time.Time) {
	if !b.invReady() || now.Before(b.ammoAt) {
		return
	}
	if b.bowEquipped() {
		if b.arrowCount() > 0 {
			// the arrow stack that fits the hunting map's monsters best (Fire Arrows vs Undead); ties keep the loaded one
			m := b.huntMap()
			var best *InvItem
			for _, it := range b.inv {
				if it.Count > 0 && isArrow(it.ItemID) && (best == nil || ammoFit(it.ItemID, m) > ammoFit(best.ItemID, m)) {
					best = it
				}
			}
			if a := b.equipped[eqpAmmo]; a != nil && a.Count > 0 && b.inv[a.Index] == a && isArrow(a.ItemID) {
				if best == nil || ammoFit(a.ItemID, m) >= ammoFit(best.ItemID, m)-0.01 {
					return // loaded
				}
			}
			if best != nil {
				b.ammoAt = now.Add(5 * time.Second)
				go b.send(newPkt(0x0998).u16(uint16(best.Index)).u32(eqpAmmo).b)
			}
			return
		}
		// out of arrows: the best melee weapon we carry, until arrows are bought
		if b.equipWanted != 0 {
			return
		}
		var best *InvItem
		for _, it := range b.inv {
			x := items[it.ItemID]
			if it.Equipped || x == nil || gearSlot(x.Loc) != 0x2 || isBow(x) || !b.wearable(it, x) {
				continue
			}
			if best == nil || b.itemScore(x) > b.itemScore(items[best.ItemID]) {
				best = it
			}
		}
		b.ammoAt = now.Add(30 * time.Second)
		if best != nil {
			b.equipWanted, b.equipIdx, b.equipAt = best.ItemID, best.Index, now
			b.event("info", "", fmt.Sprintf("no arrows: switching to %s until I buy some", itemName(best.ItemID)))
			go b.send(newPkt(0x0998).u16(uint16(best.Index)).u32(items[best.ItemID].Loc).b)
		}
	}
}

// onArrowEquip: 0x013c (ZC_EQUIP_ARROW): the ammo slot now holds this inventory index
func (b *Bot) onArrowEquip(pk []byte) {
	if len(pk) < 4 {
		return
	}
	it := b.inv[int(le.Uint16(pk[2:]))]
	if it == nil {
		return
	}
	if old := b.equipped[eqpAmmo]; old != nil && old != it {
		old.Equipped = false
	}
	it.Equipped = true
	b.equipped[eqpAmmo] = it
}

// onActionFailure: 0x013b (ZC_ACTION_FAILURE). Type 0 "Please equip the proper ammunition first": the server refused
// a bow attack / Double Strafe because its ammo slot is empty, whatever we believed. Forget the ammo slot so ammoThink
// equips the arrows again now (rAthena battle_weapon_attack -> clif_arrow_fail(ARROWFAIL_NO_AMMO)).
func (b *Bot) onActionFailure(pk []byte, now time.Time) {
	if len(pk) < 4 || le.Uint16(pk[2:]) != 0 {
		return
	}
	if a := b.equipped[eqpAmmo]; a != nil {
		a.Equipped = false
		delete(b.equipped, eqpAmmo)
		b.ammoAt = time.Time{}
	} else if now.After(b.ammoAt.Add(-4 * time.Second)) {
		b.ammoAt = time.Time{} // an equip sent >1 s ago did not take: try again
	}
}

// arrowPrice: an Arrow's NPC price (item db), 1z when unknown
func arrowPrice() int {
	if x := items[itemArrow]; x != nil && x.Buy > 0 {
		return x.Buy
	}
	return 1
}
