package main

// refine.go (economy batch 14): weapons are refined to the safe limit at Hollgrehenn (prt_in 63,60), like real
// players do. Weapon level 1: Phracon + 50z a level up to +7; level 2: Emveretarcon + 200z up to +6 (rAthena
// db/re/refine.yml: 100% up to there, so nothing is ever lost). The materials come from drops and from other players:
// 2026-11-14 04:50 the offers board held 338 Phracon (221 sellers) and 115 Emveretarcon (83 sellers) with NO buyer at
// all - dead stock, and the shops at the south gate listed little else. A bot whose weapon is below its safe limit
// asks for the material on #trade (lowest priority, after gear and potions, only with money to spare), buys it from
// shops on Prontera trips (browseShops: its want), and refines when a Prontera town trip ends and it has some.
//
// Protocol (feature.refineui on): talk to the NPC (0x0090) -> mes/close2 (answerStrayDialog sends 0x0146) ->
// ZC_OPEN_REFINING_UI 0x0aa0 -> CZ_REFINING_SELECT_ITEM 0x0aa1 <index>.W -> ZC_REFINING_MATERIAL_LIST 0x0aa2
// <len>.W <index>.W <blessing>.B {<itemId>.L <chance>.B <zeny>.L}* -> CZ_REQ_REFINING 0x0aa3 <index>.W <itemId>.L
// <blessing>.B -> ZC_ACK_ITEMREFINING 0x0188 <result>.W <index>.W <refine>.W (+ a fresh 0x0aa2) ... -> 0x0aa4 close.

import (
	"fmt"
	"strconv"
	"sync"
	"time"
)

const (
	itemPhracon      = 1010
	itemEmveretarcon = 1011
	refinerMap       = "prt_in"
)

var refinerNPC = [2]int{63, 60} // Hollgrehenn

type refineTask struct {
	Stage     string // go, talk, ui, sent
	Started   time.Time
	At        time.Time
	NPC       uint32
	Index     int
	Mat       int
	Done      int
	Tries     int
	Rooms     int // entries into prt_in (wrong door -> out and round again)
	Buy       int // dietrich.go (batch 19): material to buy from Dietrich first
	Diet      string
	DietAt    time.Time
	DietN     int
	DietPrice int
	reIndexed bool
	dietTried bool
	lastMap   string
	NoPath    int // run 8: stepAlong found no path to the NPC (prt_in 128,40 -> 63,59 for 6 min): give up after 2
	savedGoal Goal
	goalSet   bool
}

var (
	refineTasks  sync.Map // *Bot -> *refineTask
	refineCapped sync.Map // "char|itemID" -> true: the refiner offered less than 100% (safe limit reached)
	refineNext   sync.Map // *Bot -> time.Time: no new refine visit before
)

// weaponLv: item_db WeaponLevel (1 when the entry has none, rAthena's default for weapons)
func weaponLv(x *Item) int {
	if x.WLv == 0 {
		return 1
	}
	return x.WLv
}

// refineFee: the refiner's zeny per level (refine.yml, Normal)
func refineFee(wlv int) int {
	if wlv == 2 {
		return 200
	}
	return 50
}

// refineTarget: the equipped weapon if it can still be refined safely, its material and safe limit (b.mu held)
func (b *Bot) refineTarget() (*InvItem, *Item, int, int) {
	it := b.equipped[0x2]
	if it == nil || it.Unidentified || it.Count <= 0 || !it.Equipped {
		return nil, nil, 0, 0
	}
	x := items[it.ItemID]
	if x == nil || x.Type != "Weapon" || !x.Refineable {
		return nil, nil, 0, 0
	}
	// batch 20 (QA 07:45): only the weapon it will keep. Archers refined the starter Knife before buying a Bow
	// (dark_storm20, Shadow_rogue, wolfclaw70, xxlilysongxx) and lv1-15 Novices refined theirs (novalord, Raven99,
	// Ravenheart72): not below lv20, only a class weapon, and not when the gear plan's next buy is a weapon
	if b.BaseLvOr() < 20 || !isClassWeapon(family(b.Class), x) || b.classWeaponMissing() {
		return nil, nil, 0, 0
	}
	if nx, _ := b.nextGear(); nx != nil && gearSlot(nx.Loc) == 0x2 {
		return nil, nil, 0, 0
	}
	// run 8 (QA): not a weapon the build is about to replace (its next build piece is a weapon), nor a plain cheap
	// starter (Knife 50z, Club, Sword: junk-class) - a Thief refined its Knife to +7 before buying a Main Gauche
	if pc := b.nextBuildPiece(); pc != nil {
		if nx := items[pc.ItemID]; nx != nil && gearSlot(nx.Loc) == 0x2 && pc.ItemID != it.ItemID {
			return nil, nil, 0, 0
		}
	}
	if x.Buy > 0 && x.Buy < 200 && x.Slots == 0 {
		return nil, nil, 0, 0
	}
	mat, safe := 0, 0
	switch weaponLv(x) {
	case 1:
		mat, safe = itemPhracon, 7
	case 2:
		mat, safe = itemEmveretarcon, 6
	default:
		return nil, nil, 0, 0
	}
	if it.Refine >= safe {
		return nil, nil, 0, 0
	}
	if _, capped := refineCapped.Load(b.charName() + "|" + strconv.Itoa(it.ItemID)); capped {
		return nil, nil, 0, 0
	}
	return it, x, mat, safe
}

// refineWant: the material for the next safe refines, from players only (b.mu held). Lowest priority: only money
// above the gear reserve and a 500z cushion is used, and only for a bot that already fights with its weapon (lv 12+).
func (b *Bot) refineWant() *Want {
	if b.BaseLvOr() < 12 || b.charRole() == "vendor" || b.guideSaving() {
		return nil
	}
	it, x, mat, safe := b.refineTarget()
	if it == nil {
		return nil
	}
	need := safe - it.Refine - b.countItem(mat)
	if need <= 0 {
		return nil
	}
	each := b.wantBid(mat)
	spare := b.Zeny - b.gearReserve() - 500
	n := min(need, spare/max(1, each+refineFee(weaponLv(x))))
	if n < 1 {
		return nil
	}
	return &Want{ItemID: mat, Count: n, Each: each, Why: "refining my weapon", Partial: true}
}

// refineAfterTrip: a town trip just ended in Prontera (town.go "done"): refine now if the bag holds the material
// (b.mu held). The party is in town for its run anyway; the refiner is a short walk.
func (b *Bot) refineAfterTrip(now time.Time) {
	if townOf(b.MapName) != "prontera" || b.mkt != nil || b.vendBuying() || b.Dead || b.pin != nil {
		return
	}
	if _, busy := refineTasks.Load(b); busy {
		return
	}
	if t, ok := refineNext.Load(b); ok && now.Before(t.(time.Time)) {
		return
	}
	it, x, mat, safe := b.refineTarget()
	if it == nil || b.Zeny < refineFee(weaponLv(x))+300 {
		return
	}
	buy := 0
	if b.countItem(mat) < 1 { // dietrich.go (batch 19): no player sells it at or below the NPC price -> Dietrich
		if buy = b.dietrichBuyCount(mat, safe-it.Refine, refineFee(weaponLv(x))); buy < 1 {
			return
		}
	}
	refineTasks.Store(b, &refineTask{Stage: "go", Started: now, At: now, Index: it.Index, Mat: mat, Buy: buy})
	econCount("refine visit started")
	b.event("info", "", fmt.Sprintf("going to Hollgrehenn to refine my %s (+%d, %d %s)", x.Name, it.Refine, b.countItem(mat), itemName(mat)))
	typing := b.P.Typing
	go func() {
		if partyOf(b) != nil {
			b.partySay(styled(typing, pick("brb, refining my weapon real quick", "1 min, going to the refiner", "sec, upgrading my "+x.Name)))
		}
	}()
}

func (b *Bot) endRefine(t *refineTask, why string) {
	refineTasks.Delete(b)
	refineNext.Store(b, time.Now().Add(20*time.Minute))
	if t.Stage == "ui" || t.Stage == "sent" {
		go b.send(newPkt(0x0aa4).b)
	}
	if t.goalSet {
		b.Goal, b.route, b.path = t.savedGoal, nil, nil
	}
	econCount("refine visit: " + why)
	if t.Done > 0 {
		b.event("market", "", fmt.Sprintf("refined my weapon %d time(s) at Hollgrehenn (%s)", t.Done, why))
	}
}

// refineThink owns the bot during a refine visit (b.mu held).
func (b *Bot) refineThink(gm *GameMap, moving bool, now time.Time) bool {
	v, ok := refineTasks.Load(b)
	if !ok {
		return false
	}
	t := v.(*refineTask)
	if now.Sub(t.Started) > 6*time.Minute || b.Dead {
		b.endRefine(t, "took too long")
		return false
	}
	if b.mkt != nil || b.trip != nil || b.kafra != nil {
		return false // a deal or a trip comes first; the visit resumes or times out
	}
	if b.MapName != refinerMap && townOf(b.MapName) != "prontera" {
		// batch 20: the party left town (Lilyknight took the Kafra to Geffen right after starting a visit, then walked
		// back from gef_fild00 "going to the refiner"): no trip back across the world for a +1
		b.endRefine(t, "left Prontera")
		return false
	}
	switch t.Stage {
	case "ui", "sent":
		if now.Sub(t.At) > 8*time.Second {
			b.endRefine(t, "refiner did not answer")
			return false
		}
		b.setState("refining at Hollgrehenn")
		return true
	case "talk":
		if now.Sub(t.At) > 15*time.Second {
			if t.Tries >= 2 {
				b.endRefine(t, "no refine window")
				return false
			}
			t.Stage = "go"
		} else {
			return true
		}
	}
	if b.underAttack(now) || b.target != 0 {
		b.hunt(gm, moving, now)
		if b.target != 0 || b.Sitting {
			return true
		}
	}
	if b.Sitting {
		b.wantSit = false
		go b.stand()
		return true
	}
	// prt_in has separate rooms: travel() enters by the nearest door (06:10: 8 bots stood at prt_in 128,40, "no path
	// to 63,59"). Inside Prontera, route to the refiner's ROOM (RouteMapsPoint), like town.go's shop trips; entered
	// through the wrong door, go out and round again (at most 3 entries).
	wrongRoom := false
	if b.MapName == refinerMap && gm != nil {
		_, _, ok := talkCell(gm, b.X, b.Y, refinerNPC[0], refinerNPC[1])
		wrongRoom = !ok
	}
	if (b.MapName != refinerMap || wrongRoom) && townOf(b.MapName) == "prontera" {
		if b.MapName != t.lastMap {
			t.lastMap = b.MapName
			if b.MapName == refinerMap {
				t.Rooms++
			}
		}
		if t.Rooms > 3 {
			b.endRefine(t, "room can't be reached")
			return false
		}
		if len(b.route) == 0 || b.route[0].From != b.MapName || !routeEndsIn(b.route, refinerMap, refinerNPC[0], refinerNPC[1]) { // warploop.go: the refiner's ROOM
			b.route = RouteMapsPoint(b.MapName, b.X, b.Y, refinerMap, refinerNPC[0], refinerNPC[1], b.BaseLvOr())
			b.path = nil
		}
		if len(b.route) > 0 {
			if !moving {
				b.walkToPortal(gm, b.route[0])
			}
			b.setState("going to the refiner")
			return true
		}
		if wrongRoom {
			b.endRefine(t, "no route to the refiner's room")
			return false
		}
	}
	if b.MapName != refinerMap {
		if !t.goalSet {
			t.savedGoal, t.goalSet = b.Goal, true
		}
		if b.Goal.Map != refinerMap || b.Goal.Set != "refine" {
			b.Goal, b.route, b.path = Goal{Mode: "idle", Map: refinerMap, Set: "refine"}, nil, nil
		}
		b.travel(gm, moving)
		if b.Goal.Map != refinerMap {
			b.endRefine(t, "no route")
			return false
		}
		b.setState("going to the refiner")
		return true
	}
	if b.dietrichStep(gm, moving, now, t) { // dietrich.go: the material first
		return true
	}
	if b.countItem(t.Mat) < 1 {
		b.endRefine(t, "out of "+itemName(t.Mat))
		return false
	}
	b.setState("at the refiner")
	if dist(b.X, b.Y, refinerNPC[0], refinerNPC[1]) > 3 {
		tx, ty, found := refinerNPC[0], refinerNPC[1]-1, false
		if gm != nil {
			if cx, cy, ok := talkCell(gm, b.X, b.Y, refinerNPC[0], refinerNPC[1]); ok {
				tx, ty, found = cx, cy, true
			}
		}
		if !(found && b.X == tx && b.Y == ty) {
			if !moving && gm != nil {
				before := b.replanAt
				b.stepAlong(gm, tx, ty)
				if b.path == nil && b.replanAt != before { // FindPath just failed
					if t.NoPath++; t.NoPath >= 2 || !found {
						b.endRefine(t, "no path to the refiner")
						refineNext.Store(b, now.Add(time.Hour))
						return false
					}
				}
			}
			return true
		}
	}
	if moving {
		return true
	}
	npc := b.npcAt(refinerNPC[0], refinerNPC[1])
	if npc == 0 {
		if now.Sub(t.Started) > 4*time.Minute {
			b.endRefine(t, "refiner not seen")
			return false
		}
		return true
	}
	t.NPC, t.Stage, t.At = npc, "talk", now
	t.Tries++
	go b.send(newPkt(0x0090).u32(npc).u8(1).b)
	return true
}

// onRefine: 0x0aa0 window open / 0x0aa2 materials / 0x0188 result (b.mu held)
func (b *Bot) onRefine(op uint16, pk []byte) {
	v, ok := refineTasks.Load(b)
	if !ok {
		if op == 0x0aa0 { // a refine window nobody drives: close it
			go b.send(newPkt(0x0aa4).b)
		}
		return
	}
	t := v.(*refineTask)
	now := time.Now()
	switch op {
	case 0x0aa0:
		t.Stage, t.At = "ui", now
		if cur := b.equipped[0x2]; cur != nil && cur.Count > 0 { // batch 20: the weapon in hand now (7 of 14 "weapon gone")
			t.Index = cur.Index
		}
		go b.send(newPkt(0x0aa1).u16(uint16(t.Index)).b)
	case 0x0aa2:
		if len(pk) < 7 || t.Stage == "" {
			return
		}
		it := b.inv[t.Index]
		if it == nil || it.Count <= 0 || !it.Equipped {
			if cur := b.equipped[0x2]; cur != nil && cur.Count > 0 && cur.Index != t.Index && !t.reIndexed {
				t.reIndexed, t.Index, t.At = true, cur.Index, now // stale index: ask again for the weapon in hand
				go b.send(newPkt(0x0aa1).u16(uint16(t.Index)).b)
				return
			}
			why := "weapon gone"
			if it != nil && it.Count > 0 && !it.Equipped {
				why = "weapon gone (not equipped)"
			} else if it == nil {
				why = "weapon gone (no item at index)"
			}
			b.endRefine(t, why)
			return
		}
		chance, zeny := -1, 0
		for o := 7; o+9 <= len(pk); o += 9 {
			if int(le.Uint32(pk[o:])) == t.Mat {
				chance, zeny = int(int8(pk[o+4])), int(int32(le.Uint32(pk[o+5:])))
			}
		}
		switch {
		case chance < 100:
			// below 100% the weapon could break: stop at the safe limit and remember it
			refineCapped.Store(b.charName()+"|"+strconv.Itoa(it.ItemID), true)
			b.endRefine(t, "safe limit")
		case b.countItem(t.Mat) < 1:
			// dietrich.go (batch 20): out of material mid-visit (13 of 27 visits 07:44-08:07): Dietrich is in the room
			if !t.dietTried {
				t.dietTried = true
				if _, x, _, safe := b.refineTarget(); x != nil {
					if n := b.dietrichBuyCount(t.Mat, safe-it.Refine, refineFee(weaponLv(x))); n > 0 {
						t.Buy, t.Stage, t.At = n, "go", now
						go b.send(newPkt(0x0aa4).b)
						return
					}
				}
			}
			b.endRefine(t, "out of "+itemName(t.Mat))
		case b.Zeny < zeny+200:
			b.endRefine(t, "out of zeny")
		default:
			t.Stage, t.At = "sent", now
			go b.send(newPkt(0x0aa3).u16(uint16(t.Index)).u32(uint32(t.Mat)).u8(0).b)
		}
	case 0x0188:
		if len(pk) < 8 {
			return
		}
		res, idx, lv := int(le.Uint16(pk[2:])), int(le.Uint16(pk[4:])), int(le.Uint16(pk[6:]))
		it := b.inv[idx]
		if it != nil {
			it.Refine = lv
		}
		if res == 0 {
			t.Done++
			econCount("refine: success")
			if it != nil && items[it.ItemID] != nil {
				b.remember("refined %s to +%d", items[it.ItemID].Name, lv)
			}
		} else {
			econCount("refine: failed")
		}
		t.At = now // a fresh 0x0aa2 follows
	}
}

// ---------------------------------------------------------------- no flip-flop (batch 15)

// QA 06:10: bots waiting at the refiner traded Phracon back and forth (xXMochileafXx sold 2 to Taco_blade, bought 3
// back, sold 3 again): econSpare offered every Phracon in the bag while refineWant asked for more. Now the material its
// own weapon still needs is kept out of the spare, and anything bought is not resold for an hour.

var boughtAt sync.Map // "*Bot|itemID" -> time.Time

func boughtKey(b *Bot, id int) string { return fmt.Sprintf("%p|%d", b, id) }

func noteBought(b *Bot, id int) {
	if id > 0 {
		boughtAt.Store(boughtKey(b, id), time.Now())
	}
}

func boughtRecently(b *Bot, id int) bool {
	t, ok := boughtAt.Load(boughtKey(b, id))
	return ok && time.Since(t.(time.Time)) < time.Hour
}

// refineKeep: how many of this material the bot's own weapon still needs for its safe refines (b.mu held)
func (b *Bot) refineKeep(id int) int {
	if id != itemPhracon && id != itemEmveretarcon {
		return 0
	}
	if v, ok := refineTasks.Load(b); ok && v.(*refineTask).Mat == id {
		return 1 << 20 // run 8: a refine visit is under way - none of it is for sale
	}
	it, _, mat, safe := b.refineTarget()
	if it == nil || mat != id {
		return 0
	}
	return max(0, safe-it.Refine)
}
