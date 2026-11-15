package main

// Need-driven parties (owner rule 2026-11-14, RULES.md section 3 "Parties come from need, not permanence": "Other
// than priests, each class need to have a priority which is from its build, a gear, a card, etc. then it goes to farm
// where it needs to. Then it seeks party who has the same goal via global chat. Priest go with the party with similar
// level and earn their share of loots and exp. A party prioritize a priest or a monk and any other class that makes
// sense. no permanent party, party should come from need.")
//
//   1. Goals (goalTick, every bot every ~2 min): every non-healer bot has a farm goal from its build:
//        gear  - the next build piece (buildgear.go nextBuildPiece) when a monster drops it on a map safe for the bot
//        card  - a card from the build's band table for a free slot in what it wears, dropped on a safe map
//        zeny  - the piece is sold by an NPC / only traded: earn its price (the bot buys it on a real-need town run)
//        level - the band is complete: gain 1-2 base levels
//      gear/card goals are "fixed" (they need one of their drop maps); zeny/level goals are "flexible" (any safe map).
//      Healers (Acolyte line, support build) get "heal": they look for a party of similar level (+-10).
//      A goal ends when met, after its deadline (cards rarely drop: 60 min) or when it no longer applies (a better
//      piece, a new band); the bot then picks the next one. Solo bots hunt on their goal map (chooseHuntingGround).
//   2. LFG/LFM (lfgTick, every 20 s): a free bot posts one short #global line for its goal ("LFP payon cave 2,
//      farming Ninja Suit, lv52 assassin"), a party missing a healer or members posts LFM ("LFM priest for orc
//      field, lv45-50"). Posts are rate-limited server-wide (lfg_global_per_min, default 6 a minute; #global stays
//      for important lines). Someone with a compatible goal answers by whisper and joins: a party is formed (the
//      poster leads unless it is the healer) or the poster's party grows. A party wants a healer first (Priest /
//      Acolyte, then a Monk), then the roles it lacks; max 5 + 1 healer; levels within 10 of the party. Without a
//      match a bot simply farms solo.
//   3. No permanent parties (goalPartyCheck / goalTick): a member whose goal is met and whose next goal is elsewhere
//      leaves politely ("got my Ninja Suit! gl guys"); a member whose fixed goal isn't served by the party's map for
//      10 min leaves; a healer the party outgrew leaves; when the leader leaves the party ends; every party ends
//      after 3 h at most. Parties from before this change (restored from data/parties.json) end at a random time
//      20-70 min after the first check, so they don't all break up at once.
//   4. Guild leaders still lead (guildlead.go leaderPartyManager), but their parties form around the leader's goal
//      and recruit guild mates whose goals fit. Ayla's party (human-led) is never touched. Only the main instance runs
//      this (ownsBotParties); goal_parties_on (tuning.json, default off) switches it on; off keeps the old permanent grouping.
//
// GET /api/lfg[?bot=Name]: counters (goals set/met/expired by kind, posts, joins, parties formed/dissolved by
// reason), the open posts, goal counts by kind, and one bot's goal.

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// farmGoal: what a bot is working toward right now (guarded by b.mu, Bot.fgoal).
type farmGoal struct {
	Kind   string    `json:"kind"` // gear / card / zeny / level / heal
	ItemID int       `json:"item_id,omitempty"`
	Item   string    `json:"item,omitempty"`
	Mob    string    `json:"mob,omitempty"`
	Map    string    `json:"map,omitempty"`  // the best drop map (fixed goals)
	Maps   []string  `json:"maps,omitempty"` // every safe drop map: a party on any of them serves the goal
	Target int       `json:"target,omitempty"`
	Sell   string    `json:"sell,omitempty"` // zeny goal: the sellable item farmed for it (opportunity.go)
	SellID int       `json:"sell_id,omitempty"`
	Set    time.Time `json:"set"`
	Until  time.Time `json:"until"`
}

// farmItem: the item actually farmed (the sellable item of a zeny goal, else the goal's item)
func (g *farmGoal) farmItem() int {
	if g == nil {
		return 0
	}
	if g.Kind == "zeny" {
		return g.SellID
	}
	return g.ItemID
}

func (g *farmGoal) fixed() bool { return g != nil && (g.Kind == "gear" || g.Kind == "card") }

// mapped: the goal has its own farm map - an item drop (fixed) or a zeny goal farming a sellable item there
// (opportunity.go MoneyFarmGoal). A zeny goal still fits any party (serves), but solo it farms its item's map.
func (g *farmGoal) mapped() bool {
	return g.fixed() || (g != nil && g.Kind == "zeny" && len(g.Maps) > 0)
}

// onMap: m is one of the goal's own maps
func (g *farmGoal) onMap(m string) bool {
	if g == nil {
		return false
	}
	for _, x := range g.Maps {
		if x == m {
			return true
		}
	}
	return g.Map != "" && g.Map == m
}

// serves: a party hunting on map m serves this goal
func (g *farmGoal) serves(m string) bool {
	if g == nil || !g.fixed() {
		return true
	}
	for _, x := range g.Maps {
		if x == m {
			return true
		}
	}
	return g.Map == m
}

// desc: the goal in a few words for chat ("farming Ninja Suit", "Hydra card", "lvling")
func (g *farmGoal) desc() string {
	if g == nil {
		return "lvling"
	}
	switch g.Kind {
	case "gear":
		return "farming " + g.Item
	case "card":
		return g.Mob + " card"
	case "zeny":
		if g.Sell != "" {
			return "farming " + g.Sell
		}
		return "farming zeny for " + g.Item
	case "heal":
		return "healing"
	case "buy":
		return "buying " + g.Item
	}
	return "lvling"
}

// ---------------------------------------------------------------- counters

var lfgStats = struct {
	sync.Mutex
	started   time.Time
	set       map[string]int
	met       map[string]int
	expired   map[string]int
	changed   map[string]int
	posts     map[string]int
	postTimes []time.Time
	joins     int
	formed    int
	local     int // parties formed through local chat
	dissolved map[string]int
	leaves    map[string]int
}{started: time.Now(), set: map[string]int{}, met: map[string]int{}, expired: map[string]int{}, changed: map[string]int{},
	posts: map[string]int{}, dissolved: map[string]int{}, leaves: map[string]int{}}

func lfgCount(m map[string]int, k string) {
	lfgStats.Lock()
	m[k]++
	lfgStats.Unlock()
}

// goalPartiesOn: the need-driven party system runs (main instance, not switched off in tuning.json)
func goalPartiesOn() bool { return ownsBotParties() && tune().GoalPartiesOn }

// ---------------------------------------------------------------- picking a goal

// soloMapOK (b.mu held): map m is safe for this bot on its own, has room, is close enough to go to, and isn't an
// exp-poor map while a real-exp spot fits (map agent 2026-11-14: lv60+ parties sat on prt_fild08 for a Pupa card).
func (b *Bot) soloMapOK(m string) bool { return b.soloMapOKFor(m, false) }

// soloMapOKFor: soloMapOK; valuable = farming an item worth the trip (an exp-poor drop map is fine then)
func (b *Bot) soloMapOKFor(m string, valuable bool) bool {
	if m == "" || maps[m] == nil || isTown(m) || strings.Contains(m, "_in") {
		return false
	}
	if m == b.badMap && time.Now().Before(b.badMapUntil) {
		return false
	}
	lv := b.powerLv()
	if fragileClass(b.Class) {
		lv = max(1, lv-partyFragileLv)
	}
	if b.deadlyFor(m) || partyMapDeadly(m) || partyMapTooHard(m, lv) || swarmVeto(m, lv) || bossRecent(m, lv) ||
		mapReadsEmpty(m) || (m != b.MapName && !mapHasRoom(m, 1)) {
		return false
	}
	if b.BaseLvOr() < beginnerLv() && b.nearHome(m) > 3 {
		return false // beginners stay around their home town (owner rule)
	}
	if mapHops(b.MapName, b.X, b.Y, m) > 6 {
		return false
	}
	if m != b.MapName && mapCrowded(m) {
		return false
	}
	return valuable || !(mapExpPoor(m, b.BaseLvOr()) && b.realExpFitsMemo())
}

// realExpMemo: realExpSpotFits per bot, kept 2 min - soloMapOK runs for every candidate drop map of every goal pick
// under the bot's lock, and realExpSpotFits walks the whole guide list (API wedged 2026-11-14 20:19: bots' locks
// held through goal picks)
var realExpMemo sync.Map // *Bot -> realExpVal

type realExpVal struct {
	at time.Time
	ok bool
}

// realExpFitsMemo (b.mu held)
func (b *Bot) realExpFitsMemo() bool {
	if v, ok := realExpMemo.Load(b); ok && time.Since(v.(realExpVal).at) < 2*time.Minute {
		return v.(realExpVal).ok
	}
	r := b.realExpSpotFits(b.powerLv())
	realExpMemo.Store(b, realExpVal{time.Now(), r})
	return r
}

// goalValuable: an item worth farming on a map below the bot's level. Owner rule 2026-11-14 (RULES.md 5): "Farming
// should target the best map for the party level and equipments" (Ayla's lv72-78 party camped a low dungeon for a
// rarely dropping SS card) - no item justifies an exp-poor map any more: such an item becomes a zeny goal (buy it).
func goalValuable(id, lv int) bool { return false }

// goalMobLow: a drop from a monster far under the bot's level band (lv60+ bots don't hunt Pupa / Poring / Rocker
// cards): the monster must be within 20 levels under it from base level 40
func goalMobLow(mobLv, lv int) bool { return lv >= 40 && mobLv < lv-20 }

// fitFarms (b.mu held): the safe maps among item id's drop maps, best first, and the monster on the best one. A
// monster far under the bot's level band doesn't count unless the item is valuable; crowded maps are skipped.
func (b *Bot) fitFarms(fs []pieceFarm, id int) ([]string, string) {
	var out []string
	mob := ""
	seen := map[string]bool{}
	lv := b.BaseLvOr()
	val := goalValuable(id, lv)
	pw := b.soloRateLv(b.powerLv())
	for _, f := range fs {
		if seen[f.Map] || (!val && goalMobLow(f.MobLv, lv)) || !b.soloMapOKFor(f.Map, val) {
			continue
		}
		// a farm goal is a long stay: the monster it hunts may not out-level the bot's effective level, and the map
		// must be safe with 4 levels to spare (life mind 19:28-19:59: lv36-46 parties and low bots kept dying on
		// moc_fild01/02 for the Peco Peco card)
		if f.MobLv > pw || partyMapTooHard(f.Map, pw-4) || recentDeathsFor(f.Map, lv) >= 3 {
			continue
		}
		seen[f.Map] = true
		if mob == "" {
			mob = f.Mob
		}
		out = append(out, f.Map)
	}
	return out, mob
}

// hasCard (b.mu held): the bot holds the card or wears it in a slot
func (b *Bot) hasCard(id int) bool {
	if b.ownsUnequipped(id) {
		return true
	}
	for _, e := range b.equipped {
		if e == nil {
			continue
		}
		for _, c := range e.Cards {
			if c == id {
				return true
			}
		}
	}
	return false
}

// wears (b.mu held): the item is equipped or owned
func (b *Bot) hasItem(id int) bool {
	if b.ownsUnequipped(id) {
		return true
	}
	for _, e := range b.equipped {
		if e != nil && e.ItemID == id {
			return true
		}
	}
	return false
}

// pickFarmGoal (b.mu held): the bot's next goal (never nil once the bot's level and inventory are known). prev is the
// kind of the goal that just ended: after a zeny goal a card hunt comes first (a build is gear AND cards - owner:
// "a gear, a card, etc."), otherwise half the time; most build pieces are trade-only (live 2026-11-14: 865 of 1,010),
// which made zeny the only goal anyone had.
func (b *Bot) pickFarmGoal(now time.Time, prev string) *farmGoal {
	lv := b.BaseLvOr()
	if b.isHealer() && !inHumanParty(b) { // in a human party (Ayla's) the priest's gear gets its turn too (lfg_human.go)
		return &farmGoal{Kind: "heal", Set: now, Until: now.Add(2 * time.Hour)}
	}
	if inHumanParty(b) {
		return b.humanPartyGearGoal(now, lv)
	}
	var zeny *farmGoal
	if pc := b.nextBuildPiece(); pc != nil {
		if pc.Source == "drop" {
			if ms, mob := b.fitFarms(pc.Farm, pc.ItemID); len(ms) > 0 {
				return &farmGoal{Kind: "gear", ItemID: pc.ItemID, Item: pc.Name, Mob: mob, Map: ms[0], Maps: ms, Set: now,
					Until: now.Add(90 * time.Minute)}
			}
		}
		price := pc.Price
		if pc.NPC > 0 {
			price = pc.NPC
		}
		if price > 0 && b.Zeny-b.gearReserve() >= price {
			// it can pay: buying it is the goal (a met zeny goal became a card hunt elsewhere and the member left its
			// party with the pooled money unspent - LilSilenthunter, Battle Axe, 2026-11-14 20:01-20:06)
			return &farmGoal{Kind: "buy", ItemID: pc.ItemID, Item: pc.Name, Target: price, Set: now, Until: now.Add(60 * time.Minute)}
		}
		if price > 0 && b.Zeny-b.gearReserve() < price {
			zeny = &farmGoal{Kind: "zeny", ItemID: pc.ItemID, Item: pc.Name, Target: price + b.gearReserve(), Set: now,
				Until: now.Add(2 * time.Hour)}
			// opportunity.go: farm a sellable, in-demand item for the zeny (owner: bots read the market and farm
			// what's missing) - its safe drop maps become the goal's maps
			if mg := b.MoneyFarmGoal(now); mg != nil && len(mg.Maps) > 0 {
				zeny.Sell, zeny.SellID, zeny.Mob, zeny.Map, zeny.Maps = mg.Item, mg.ItemID, mg.Mob, mg.Maps[0], mg.Maps
				if mg.Until.Before(zeny.Until) && mg.Until.After(now) {
					zeny.Until = mg.Until
				}
			}
		}
	}
	cardFirst := prev == "zeny" || (prev != "card" && b.rng.Float64() < 0.5)
	if zeny != nil && !cardFirst {
		return zeny
	}
	if c := b.cardGoal(lv, now); c != nil {
		return c
	}
	if zeny != nil {
		return zeny
	}
	step := 2
	if lv >= 70 {
		step = 1
	}
	return &farmGoal{Kind: "level", Target: lv + step, Set: now, Until: now.Add(2 * time.Hour)}
}

// turnPiece (b.mu held): in a human-led party, the piece this member's goal is locked on (buy / zeny / gear) until
// it owns it - the party's money for it buys exactly that piece, nothing else (watcher 20:15-20:30: Rinfang got
// 14,170z for Claire Suits, bought a Mantle + Bandana, the piece changed to Crossbow and got topped up again). 0 = none.
func (b *Bot) turnPiece() int {
	// the party's gear turn (gearpool.go): the member whose piece the pool funds buys exactly that piece, in any party
	// (Silent rogue kept receiving "for ur Wooden Mail" gifts and never bought it, 2026-11-14 20:30-21:12)
	if p := partyOf(b); p != nil {
		parties.Lock()
		name := p.Name
		parties.Unlock()
		id := 0
		gearPools.Lock()
		if pl := gearPools.m[name]; pl != nil && pl.TurnItem != 0 && strings.EqualFold(pl.Turn, b.charName()) {
			id = pl.TurnItem
		}
		gearPools.Unlock()
		if id != 0 && !b.hasItem(id) {
			return id
		}
	}
	g := b.fgoal
	if g == nil || g.ItemID == 0 || (g.Kind != "buy" && g.Kind != "zeny" && g.Kind != "gear") || !inHumanParty(b) || b.hasItem(g.ItemID) {
		return 0
	}
	return g.ItemID
}

// humanPartyGearGoal (b.mu held): in a human-led party (Ayla's) every member gears up first (owner rule 2026-11-14,
// RULES.md 5: "they should prioritize getting good gears, either farming or buying. No money then they need to farm
// for gears. for each member"): the next build piece - bought when the member can pay (gear pool / its own zeny),
// else farmed where it drops (the turn checks the map fits the party's level), else zeny farmed on the party's best
// zeny map until it can pay.
func (b *Bot) humanPartyGearGoal(now time.Time, lv int) *farmGoal {
	if pc := b.nextBuildPiece(); pc != nil {
		price := pc.Price
		if pc.NPC > 0 {
			price = pc.NPC
		}
		if price > 0 && b.Zeny-b.gearReserve() >= price {
			return &farmGoal{Kind: "buy", ItemID: pc.ItemID, Item: pc.Name, Target: price, Set: now, Until: now.Add(60 * time.Minute)}
		}
		if pc.Source == "drop" {
			var ms []string
			mob := ""
			for _, f := range pieceFarms(pc.ItemID, lv, 5) {
				if !goalMobLow(f.MobLv, lv) && !isTown(f.Map) {
					ms = append(ms, f.Map)
					if mob == "" {
						mob = f.Mob
					}
				}
			}
			if len(ms) > 0 {
				return &farmGoal{Kind: "gear", ItemID: pc.ItemID, Item: pc.Name, Mob: mob, Map: ms[0], Maps: ms, Target: price, Set: now,
					Until: now.Add(90 * time.Minute)}
			}
		}
		if price > 0 {
			return &farmGoal{Kind: "zeny", ItemID: pc.ItemID, Item: pc.Name, Target: price + b.gearReserve(), Set: now,
				Until: now.Add(2 * time.Hour)}
		}
	}
	step := 2
	if lv >= 70 {
		step = 1
	}
	return &farmGoal{Kind: "level", Target: lv + step, Set: now, Until: now.Add(2 * time.Hour)}
}

// cardGoal (b.mu held): a card from the build's band table it doesn't have, dropped on a map safe for it - one for a
// free slot in what it wears first, then any (it keeps or trades it; slotted gear comes later). 60 minutes at most.
func (b *Bot) cardGoal(lv int, now time.Time) *farmGoal {
	p := b.plan()
	if p == nil {
		return nil
	}
	gb := gearBandFor(p.ID, lv)
	if gb == nil {
		return nil
	}
	tried := 0
	for pass := 0; pass < 2; pass++ {
		for _, c := range gb.Cards {
			if b.hasCard(c.ID) || (pass == 0 && b.cardSlotFor(c.ID) == nil) {
				continue
			}
			if tried++; tried > 6 {
				return nil // the band's top cards only: each try scores its drop maps under the bot's lock
			}
			if ms, mob := b.fitFarms(pieceFarms(c.ID, lv, 3), c.ID); len(ms) > 0 {
				return &farmGoal{Kind: "card", ItemID: c.ID, Item: c.Name, Mob: mob, Map: ms[0], Maps: ms, Set: now,
					Until: now.Add(60 * time.Minute)}
			}
		}
	}
	return nil
}

// goalStatus (b.mu held): "ok", "met", "expired" or "changed" (no longer the right goal).
func (b *Bot) goalStatus(g *farmGoal, now time.Time) string {
	if g == nil {
		return "changed"
	}
	if g.Kind == "guild" {
		return b.guildBlockStatus(g, now) // guildduty.go: a guild duty block
	}
	switch g.Kind {
	case "gear":
		if b.hasItem(g.ItemID) {
			return "met"
		}
	case "card":
		if b.hasCard(g.ItemID) {
			return "met"
		}
	case "zeny":
		if b.Zeny >= g.Target || b.hasItem(g.ItemID) {
			return "met"
		}
	case "buy":
		if b.hasItem(g.ItemID) {
			return "met"
		}
		if b.Zeny-b.gearReserve() < g.Target && !inHumanParty(b) {
			return "changed" // spent the zeny on something else: farm again
		}
	case "level":
		if b.BaseLvOr() >= g.Target {
			return "met"
		}
	case "heal":
		if !b.isHealer() || inHumanParty(b) {
			return "changed" // job change into a non-healer / joined a human's party
		}
	}
	locked := inHumanParty(b) && (g.Kind == "buy" || g.Kind == "zeny" || g.Kind == "gear") && g.ItemID != 0
	if now.After(g.Until) && !locked {
		return "expired"
	}
	if (b.isHealer() && !inHumanParty(b)) != (g.Kind == "heal") {
		return "changed"
	}
	switch g.Kind {
	case "gear":
		if pc := b.nextBuildPiece(); !locked && (pc == nil || pc.ItemID != g.ItemID) {
			return "changed" // a better piece (a new band, a gift) comes first now
		}
	}
	// solo on a fixed goal: its maps must still be safe (a death loop, a boss note, a new crowd)
	if g.mapped() && partyOf(b) == nil {
		ok := false
		for _, m := range g.Maps {
			if b.soloMapOKFor(m, goalValuable(g.farmItem(), b.BaseLvOr())) {
				ok = true
				break
			}
		}
		if !ok {
			return "changed"
		}
	}
	return "ok"
}

// goalMapSafe: an item-goal party map is kept while it is SAFE for the weakest member, even when it gives little
// exp (partyMapOK's "too easy" / exp-poor tests): farming a drop is the point of the party. No bot locks.
func goalMapSafe(ms []*Bot, m string) bool { return goalMapSafeItem(ms, m, 0) }

// goalMapSafeItem: goalMapSafe for farming item id: also not crowded, and not exp-poor for the weakest member unless
// the item is valuable (map agent 2026-11-14: card parties of lv60+ on beginner fields) - so the fit checks move them.
func goalMapSafeItem(ms []*Bot, m string, id int) bool {
	if m == "" || isTown(m) || len(ms) == 0 {
		return false
	}
	minLv, _, known, _ := partyFitLvRaw(ms) // the weakest member on its own: a farm goal is no reason to stretch
	if known == 0 {
		return true
	}
	if bad, _ := leaderAvoids(ms[0].P.Name, m, time.Now()); bad {
		return false
	}
	if mapCrowded(m) {
		return false
	}
	// map agent: the walk there must not cross a train map too hard for the weakest member (QA 17:48-17:53: Elder
	// Willow / Stem Worm trains on mjolnir_02 killed parties walking to mjo_dun01)
	if s, ok := peek(ms[0].P.Name); ok && s.Online && s.Map != "" && s.Map != m && routeCrossesTooHard(s.Map, s.X, s.Y, m, minLv) {
		return false
	}
	if mb := partyMinBase(ms); mapExpPoor(m, mb) && !goalValuable(id, mb) {
		return false
	}
	return !partyMapDeadly(m) && !partyMapTooHard(m, minLv) && !swarmVeto(m, minLv) && !bossRecent(m, minLv) && !mapReadsEmpty(m)
}

// partyStrength: how many levels a composed party fights above its weakest member's effective level - a healer +4,
// a tank +3, 4+ members online +1 (max 8; 13:51 deploy had 3/2/1), never past the weakest member's real base level. QA 2026-11-14 13:30:
// guild-leader parties of lv61-71 sat on gef_fild02 (lv28-44 monsters): gear-poor members rate ~(lv+1)/2 and the
// fragile mage -6, so every picker sized the party's map for a lv35 solo character. No bot locks (published state).
func partyStrength(ms []*Bot) int {
	heal, tank, on := false, false, 0
	for _, b := range ms {
		if s, ok := peek(b.P.Name); ok && s.Online && s.Known {
			on++
			heal = heal || s.Support
			tank = tank || s.Tank
		}
	}
	if on < 2 {
		return 0
	}
	n := 0
	if heal {
		n += 4
	}
	if tank {
		n += 3
	}
	if on >= 4 {
		n++
	}
	return n
}

// partyBeginnerFar: map m is more than 3 maps from the home town of a beginner member (base level < beginner_lv;
// home = its persona's home, else the town nearest where it stands) - RULES.md 2 "beginners stay near their home town".
// Class QA 2026-11-14 20:05: Payon Novices in a party sent to prt_fild06 sat at pay_fild03 waiting for an escort.
// No bot locks (published state + persona).
func partyBeginnerFar(ms []*Bot, m string) bool {
	if m == "" {
		return false
	}
	for _, b := range ms {
		s, ok := peek(b.P.Name)
		if !ok || !s.Online || !s.Known || s.Lv >= beginnerLv() {
			continue
		}
		home := b.P.Home
		if home == "" {
			home = nearestTown(s.Map)
		}
		if townHops(home, m) > 3 {
			return true
		}
	}
	return false
}

// partyAnchored (parties.Mutex held): the party farms an item goal on its current map
func (p *Party) partyAnchored() bool { return p.Anchor != "" && p.Anchor == p.Map }

// ---------------------------------------------------------------- the goal loop

// goalTickMax: bots whose goal is (re)checked per 20 s tick (1,000 bots: every ~5-6 min at worst)
const goalTickMax = 60

// goalTick: re-checks each bot's goal every ~2 min, picks the next one when it ends, and acts on its party:
// a member whose goal is met / moved elsewhere leaves politely. Called every 20 s by lfgManager.
func goalTick(bots []*Bot, last map[*Bot]time.Time, now time.Time) {
	done := 0
	for _, b := range bots {
		if now.Sub(last[b]) < 2*time.Minute {
			continue
		}
		if done >= goalTickMax {
			break // spread the picks: a burst of 1,000 picks held bots' locks for seconds (API wedge 20:19)
		}
		done++
		b.mu.Lock()
		// pinned bots are skipped - except a human party's members (pinned online by humanparty.go): Ayla's friends got
		// no goals at all and every turn read "nobody needs a map" (live 2026-11-14 19:25)
		if !b.Online || b.BaseLv <= 0 || !b.invReady() || b.charRole() == "vendor" || (b.pin != nil && !inHumanParty(b)) {
			b.mu.Unlock()
			continue
		}
		last[b] = now
		old := b.fgoal
		st := b.goalStatus(old, now)
		if st == "ok" {
			b.mu.Unlock()
			b.partyGoalFit(now)
			continue
		}
		if old != nil {
			switch st {
			case "met":
				lfgCount(lfgStats.met, old.Kind)
				b.fgoalDone, b.fgoalDoneAt = old, now
				if inHumanParty(b) {
					go humanPartyGoalMet(b, old) // lfg_human.go: announced; the party stays, the turn moves on
				}
			case "expired":
				lfgCount(lfgStats.expired, old.Kind)
			default:
				lfgCount(lfgStats.changed, old.Kind)
			}
		}
		prev := ""
		if old != nil {
			prev = old.Kind
		}
		g := b.pickFarmGoal(now, prev)
		if old != nil && st == "met" && old.Kind == "zeny" && old.ItemID != 0 && inHumanParty(b) && !b.hasItem(old.ItemID) {
			// the zeny for its piece is in: the goal stays on THAT piece until bought (no re-pick to another one)
			price := old.Target - b.gearReserve()
			if it := items[old.ItemID]; it != nil && it.Buy > 0 && len(gearShops(old.ItemID)) > 0 {
				price = it.Buy
			}
			g = &farmGoal{Kind: "buy", ItemID: old.ItemID, Item: old.Item, Target: max(price, 1), Set: now, Until: now.Add(2 * time.Hour)}
		}
		b.fgoal = g
		lfgCount(lfgStats.set, g.Kind)
		where := ""
		if g.Map != "" {
			where = " on " + g.Map
		}
		why := ""
		if old != nil {
			why = fmt.Sprintf(" (%s %s)", old.desc(), st)
		}
		b.event("info", "", fmt.Sprintf("goal: %s%s%s", g.desc(), where, why))
		// a solo bot hunting where its own picker sent it goes to the new goal's map (chooseHuntingGround)
		solo := partyOf(b) == nil
		if solo && g.mapped() && b.Goal.Mode == "hunt" && b.Goal.Set == "auto" && !g.onMap(b.Goal.Map) && b.trip == nil {
			b.Goal, b.route, b.path = Goal{}, nil, nil
		}
		b.mu.Unlock()
		if !solo {
			b.partyGoalFit(now)
		}
	}
}

// partyGoalFit (no lock held): a party member whose goal the party no longer serves leaves it - at once after a goal
// is met ("got my X"), after 10 min when a fixed goal's maps aren't the party map. Healers leave a party that
// outgrew them. Ayla's party, boss hunts, restoring parties and parties still inside their first check are skipped.
func (b *Bot) partyGoalFit(now time.Time) {
	if !goalPartiesOn() {
		return
	}
	p := partyOf(b)
	if p == nil {
		return
	}
	parties.Lock()
	skip := p.Human != "" || p.Leader == nil || now.Before(p.CheckAfter) || p.CheckAfter.IsZero()
	pm, lead := p.Map, p.Leader
	others := []*Bot{}
	for _, m := range p.Members {
		if m != b {
			others = append(others, m)
		}
	}
	human := p.Human != ""
	parties.Unlock()
	if b == lead && !human && pm != "" && !isRestoring(p) && !p.bossActive() {
		// the leader leads: its goal and the party map are the same map (live 2026-11-14: guild02's leader sat on
		// pay_fild01 with a Sohee card goal while its party farmed moc_fild03). A guild leader always reconciles; any
		// other leader too, unless its goal was just met (then the party ends politely below)
		b.mu.Lock()
		done, doneAt, guildLead := b.fgoalDone, b.fgoalDoneAt, b.alwaysOnline()
		b.mu.Unlock()
		if guildLead || done == nil || now.Sub(doneAt) >= 5*time.Minute {
			b.leaderGoalSync(p, pm, others, now)
			return
		}
	}
	if skip || isRestoring(p) || p.bossActive() || pm == "" {
		return
	}
	b.mu.Lock()
	g, done, doneAt, typing, healer := b.fgoal, b.fgoalDone, b.fgoalDoneAt, b.P.Typing, b.isHealer()
	lv := b.BaseLvOr()
	b.mu.Unlock()
	line, why := "", ""
	b.mu.Lock()
	guildLead := b.alwaysOnline()
	b.mu.Unlock()
	switch {
	case guildLead && b != lead:
		// a guild leader leads its own party (owner rule 3b); one left in another's party (old grouping, a restart)
		// leaves politely and leaderPartyManager forms its guild party around its goal
		line, why = pick("gotta go run my guild's party, ty all", "guild calls, ty for the party gl", "off to lead my guild pt, gl guys"), "guild leader leads its own party"
	case healer:
		sum, n := 0, 0
		for _, o := range others {
			if s, ok := peek(o.P.Name); ok && s.Known {
				sum, n = sum+s.Lv, n+1
			}
		}
		if n > 0 && abs(sum/n-lv) > 12 {
			line, why = pick("you guys outlevel me now, gl all!", "gonna find a party closer to my lvl, ty all", "lvl gap is too big now, ty for the party"), "healer level gap"
		}
	case p.gearTurnHolder(b):
		// its gear step is what the party's pooled zeny is buying (gearpool.go): it stays until bought
	case done != nil && now.Sub(doneAt) < 5*time.Minute && g != nil && !g.serves(pm) && done.Kind != "zeny":
		line, why = metLine(done), "goal met"
	case g != nil && !g.serves(pm):
		if p.divergedFor(b, now) >= 10*time.Minute {
			line, why = pick("heading to "+mapTalk(g.Map)+" for my "+g.Item+", ty for the party", "gonna go "+g.desc()+" at "+mapTalk(g.Map)+", gl guys",
				"my "+g.Item+" doesnt drop here, off to "+mapTalk(g.Map)+". ty all"), "goal elsewhere"
		}
	default:
		p.divergedFor(b, time.Time{}) // served: clear
	}
	if line == "" {
		return
	}
	b.fgoalDone = nil
	lfgCount(lfgStats.leaves, why)
	if b == lead {
		go endGoalParty(p, b, styled(typing, line+pick(" - thats it for me", " party's over", "")), why)
		return
	}
	go func() {
		b.partySay(styled(typing, line))
		time.Sleep(3 * time.Second)
		b.event("party", "", fmt.Sprintf("left %s: %s", p.Name, why))
		p.drop(b)
	}()
}

// leaderGoalSync (no lock held): a party leader whose own goal map isn't the party map either takes the party there
// (the map is safe for the whole group, the party hasn't just moved) or leads on the party's map with a levelling
// goal for now. Members follow the leader (partyGoalSync), so the leader is with its party either way.
func (b *Bot) leaderGoalSync(p *Party, pm string, others []*Bot, now time.Time) {
	b.mu.Lock()
	g, lv := b.fgoal, b.BaseLvOr()
	at, ax, ay := b.MapName, b.X, b.Y
	b.mu.Unlock()
	if g == nil || !g.mapped() || g.onMap(pm) {
		return
	}
	ms := append([]*Bot{b}, others...)
	parties.Lock()
	moved := p.MovedAt
	parties.Unlock()
	if now.Sub(moved) >= 15*time.Minute {
		for _, m := range g.Maps {
			if m == pm || !goalMapSafeItem(ms, m, g.farmItem()) || mapHops(at, ax, ay, m) > 8 {
				continue
			}
			parties.Lock()
			p.Anchor, p.AnchorItem, p.Purpose, p.Need = m, g.farmItem(), g.desc()+" @ "+m, true
			parties.Unlock()
			markPartiesDirty()
			b.mu.Lock()
			b.Goal, b.route, b.path = Goal{Mode: "hunt", Map: m, Set: "party-leader"}, nil, nil
			b.event("party", "", fmt.Sprintf("taking the party from %s to %s: %s", pm, m, g.desc()))
			b.mu.Unlock()
			lfgCount(lfgStats.leaves, "leader moved party to its goal")
			go func() {
				b.partySay(planLine(p, m, g.desc())) // together.go: the leader announces the plan (fixed wording, not styled)
				p.moveTo(m)
			}()
			return
		}
	}
	// no safe way to take the group to its goal: it leads where the party is, and levels there for now
	step := 2
	if lv >= 70 {
		step = 1
	}
	b.mu.Lock()
	b.fgoal = &farmGoal{Kind: "level", Target: lv + step, Set: now, Until: now.Add(60 * time.Minute)}
	b.event("info", "", fmt.Sprintf("goal: lvling with the party on %s (%s needs a map the group can't take)", pm, g.desc()))
	b.mu.Unlock()
	lfgCount(lfgStats.changed, g.Kind+" (leader adopts party map)")
}

// gearTurnHolder: b holds the party's gear turn (gearpool.go) - the party funds its next piece
func (p *Party) gearTurnHolder(b *Bot) bool {
	parties.Lock()
	name := p.Name
	parties.Unlock()
	turn, _ := gearPoolView(name)
	return turn != "" && strings.EqualFold(turn, b.charName())
}

// metLine: the polite leave line once a goal is met
func metLine(g *farmGoal) string {
	switch g.Kind {
	case "gear", "card":
		return pick("got my "+g.Item+"! gl guys", "finally got my "+g.Item+" :D ty for the party", "thats my "+g.Item+", ty all, gl")
	case "zeny":
		return pick("got the zeny for my "+g.Item+", gotta go buy it. ty all", "thats enough zeny for me, ty for the party gl")
	}
	return pick("got what i came for, ty all gl", "ok im done here, ty for the party", "lvl up! ty guys, gl")
}

// divergedFor: how long member b's goal hasn't been served by the party map (zero `since` clears it).
func (p *Party) divergedFor(b *Bot, now time.Time) time.Duration {
	parties.Lock()
	defer parties.Unlock()
	if now.IsZero() {
		delete(p.Diverge, b)
		return 0
	}
	if p.Diverge == nil {
		p.Diverge = map[*Bot]time.Time{}
	}
	at, ok := p.Diverge[b]
	if !ok {
		p.Diverge[b] = now
		return 0
	}
	return now.Sub(at)
}

// endGoalParty: the leader says goodbye in party chat, a member answers, and the party disbands (own goroutine).
func endGoalParty(p *Party, by *Bot, line, why string) {
	if by != nil && line != "" {
		by.partySay(line)
		parties.Lock()
		var other *Bot
		for _, m := range p.Members {
			if m != by {
				other = m
				break
			}
		}
		parties.Unlock()
		if other != nil {
			time.Sleep(time.Duration(2+crand(3)) * time.Second)
			other.mu.Lock()
			typing := other.P.Typing
			other.mu.Unlock()
			other.partySay(styled(typing, pick("ty gl", "gl!", "ty for the party", "cya", "gl hf")))
		}
		time.Sleep(2 * time.Second)
		by.event("party", "", fmt.Sprintf("party %s ended: %s", p.Name, why))
	}
	lfgCount(lfgStats.dissolved, why)
	disband(p)
}

// goalPartyCheck: party-level ends - the 3 h cap, parties from before the change (restored without a goal) end at a
// random time, and a first-check delay is set for every party. Called every 20 s by lfgManager.
func goalPartyCheck(now time.Time) {
	parties.Lock()
	var end []*Party
	for _, p := range parties.byName {
		if p.Human != "" || p.Leader == nil {
			continue
		}
		if p.CheckAfter.IsZero() {
			if p.Need {
				p.CheckAfter = now.Add(5 * time.Minute)
			} else {
				// a party from the old system: members check their goals 5-40 min from now, the party ends 20-70 min from now
				p.CheckAfter = now.Add(time.Duration(5+crand(35)) * time.Minute)
				p.EndAt = now.Add(time.Duration(30+crand(90)) * time.Minute) // gradual: 30-120 min
			}
		}
		start := p.Formed
		if p.Restored.After(start) && !p.Need {
			start = p.Restored
		}
		if (!p.EndAt.IsZero() && now.After(p.EndAt)) || now.Sub(start) > 3*time.Hour {
			end = append(end, p)
		}
	}
	parties.Unlock()
	for _, p := range end {
		if isRestoring(p) || p.bossActive() || p.resting() {
			continue
		}
		parties.Lock()
		lead, legacy := p.Leader, !p.Need
		p.EndAt = time.Time{}
		parties.Unlock()
		lead.mu.Lock()
		typing := lead.P.Typing
		lead.mu.Unlock()
		why := "3 h cap"
		line := pick("been at it for hours, gonna split up. ty all gl", "ok thats enough for today with this group, ty all", "lets call it here, gl everyone")
		if legacy {
			why = "old permanent party"
			line = pick("ok gonna split up, gl all", "im off to do my own thing, ty for the party", "party's over guys, ty and gl")
		}
		go endGoalParty(p, lead, styled(typing, line), why)
	}
}

// ---------------------------------------------------------------- LFG / LFM

type lfgPost struct {
	Bot   *Bot
	Party *Party // an LFM post by a party's leader (nil: a solo LFG)
	Map   string
	At    time.Time
	Line  string
	Kind  string // goal kind / "lfm" / "lfm-heal"
}

var lfg = struct {
	sync.Mutex
	posts     map[*Bot]*lfgPost
	lastPost  map[*Bot]time.Time
	partyPost map[*Party]time.Time
	leftAt    map[*Bot]time.Time
	tokens    float64
	fill      time.Time
}{posts: map[*Bot]*lfgPost{}, lastPost: map[*Bot]time.Time{}, partyPost: map[*Party]time.Time{}, leftAt: map[*Bot]time.Time{}}

const (
	lfgPostLife   = 10 * time.Minute // a post can be answered this long
	lfgRepost     = 10 * time.Minute // one post per bot / party per this
	lfgLvBand     = 10               // levels within the party's average
	lfgMaxMembers = 5                // non-healers in a party
	lfgMaxHops    = 6
)

// lfgCand: a free bot looking for (or open to) a party, snapshot without locks
type lfgCand struct {
	b              *Bot
	name, class    string
	lv, role       int
	healer, monk   bool
	g              *farmGoal
	at             string
	x, y           int
	m              string // the map it farms on ("" for a healer: anywhere)
	home           string
	beginner       bool
	typing         string
	posted, social bool
	leader         bool // a guild leader: leads (anchors / posts), never joins another party
}

// lfgParty: an open bot party, snapshot
type lfgParty struct {
	p                *Party
	lead             *Bot
	ms               []*Bot
	m, anchor        string
	avg, lo, hi      int
	roles            [5]int
	healer, monk     bool
	nonHealers, size int
	posted           bool
	joins            int
	item             int // the anchor's goal item (goalMapSafeItem)
}

func isMonkClass(c int) bool {
	if base, ok := transToSecond[c]; ok {
		c = base
	}
	return c == 15
}

// lfgFits: candidate c may join party/anchor P (pure; no locks besides the map helpers).
func lfgFits(c *lfgCand, P *lfgParty) bool {
	if c.healer {
		if P.healer || P.size >= lfgMaxMembers+1 {
			return false
		}
	} else if P.nonHealers >= lfgMaxMembers {
		return false
	}
	if P.avg > 0 && abs(c.lv-P.avg) > lfgLvBand {
		return false
	}
	if !c.healer && c.g != nil && !c.g.serves(P.m) {
		return false
	}
	switch c.role { // what makes sense: a healer, up to 2 tanks, 3 ranged, 2 melee
	case roleTank:
		if P.roles[roleTank] >= 2 {
			return false
		}
	case roleRanged:
		if P.roles[roleRanged] >= 3 {
			return false
		}
	case roleMelee:
		if P.roles[roleMelee] >= 2 {
			return false
		}
	case roleNone:
		return false
	}
	if c.beginner && (townHops(c.home, P.m) > 3 || lfgHops(c.at, c.x, c.y, P.m) > 2) {
		return false // beginners party in their own area, near home (RULES.md 2)
	}
	return lfgHops(c.at, c.x, c.y, P.m) <= lfgMaxHops
}

// lfgScore: how much party P wants candidate c (a healer first, then a Monk, then a missing role)
func lfgScore(c *lfgCand, P *lfgParty) int {
	s := 0
	switch {
	case c.healer && !P.healer:
		s += 100
	case c.monk && !P.healer && !P.monk:
		s += 60
	}
	if P.roles[c.role] == 0 {
		s += 30
	}
	if c.g != nil && c.g.mapped() && c.g.onMap(P.m) {
		s += 15
	}
	if c.posted {
		s += 5
	}
	s += lfgFriendBonus(c, P) // friends.go: players party with their friends first (0-40)
	return s - abs(c.lv-P.avg)
}

// lfgFriendBonus: friendPartyBonus for real bots (the offline simulation's stub bots have no persona)
func lfgFriendBonus(c *lfgCand, P *lfgParty) int {
	if c.b == nil || c.b.P == nil {
		return 0
	}
	ms := make([]*Bot, 0, len(P.ms))
	for _, m := range P.ms {
		if m != nil && m.P != nil {
			ms = append(ms, m)
		}
	}
	return friendPartyBonus(c.b, ms)
}

// mapOKFor: the party map stays right with c in it (an item-goal anchor only needs to be safe)
func (P *lfgParty) mapOKFor(c *lfgCand) bool {
	ms := append(append([]*Bot(nil), P.ms...), c.b)
	if P.anchor == P.m {
		return goalMapSafeItem(ms, P.m, P.item)
	}
	return partyMapOK(ms, P.m)
}

func (P *lfgParty) add(c *lfgCand) {
	P.ms = append(P.ms, c.b)
	P.size++
	if c.healer {
		P.healer = true
	} else {
		P.nonHealers++
	}
	P.monk = P.monk || c.monk
	P.roles[c.role]++
	P.avg = (P.avg*(P.size-1) + c.lv) / P.size
	P.lo, P.hi = min(P.lo, c.lv), max(P.hi, c.lv)
	P.joins++
}

// lfgTokens: the server-wide #global budget for LFG/LFM lines
func lfgTake(now time.Time) bool {
	lfg.Lock()
	defer lfg.Unlock()
	per := float64(max(1, tune().LfgGlobalPerMin))
	if !lfg.fill.IsZero() {
		lfg.tokens = min(2, lfg.tokens+now.Sub(lfg.fill).Minutes()*per)
	}
	lfg.fill = now
	if lfg.tokens < 1 {
		return false
	}
	lfg.tokens--
	return true
}

// lfgGather: the free candidates and the open parties right now.
func lfgGather(bots []*Bot, now time.Time) ([]*lfgCand, []*lfgParty) {
	lfg.Lock()
	for b, p := range lfg.posts {
		if now.Sub(p.At) > lfgPostLife {
			delete(lfg.posts, b)
		}
	}
	posted := map[*Bot]bool{}
	pposted := map[*Party]bool{}
	for b, p := range lfg.posts {
		if p.Party != nil {
			pposted[p.Party] = true
		} else {
			posted[b] = true
		}
	}
	left := map[*Bot]time.Time{}
	for b, t := range lfg.leftAt {
		left[b] = t
	}
	lfg.Unlock()
	var cs []*lfgCand
	for _, b := range bots {
		if partyOf(b) != nil || now.Sub(left[b]) < 3*time.Minute {
			continue
		}
		b.mu.Lock()
		g := b.fgoal
		ok := b.Online && b.BaseLv > 0 && g != nil && b.pin == nil && b.vend == nil && b.trip == nil && !b.Dead &&
			b.charRole() != "vendor" && b.basicLv() >= 5 && b.Goal.Mode == "hunt" && strings.HasPrefix(b.Goal.Set, "auto") &&
			b.MapName != ""
		if !ok {
			b.mu.Unlock()
			continue
		}
		c := &lfgCand{b: b, name: b.charName(), class: strings.ToLower(className(b.Class)), lv: b.BaseLvOr(), role: partyRole(b),
			healer: b.isHealer(), monk: isMonkClass(b.Class), g: g, at: b.MapName, x: b.X, y: b.Y, home: b.P.Home,
			beginner: b.BaseLvOr() < beginnerLv(), typing: b.P.Typing, posted: posted[b], leader: b.alwaysOnline(),
			social: b.P.Sociability*b.tactics().Party >= 0.15}
		if c.home == "" {
			c.home = nearestTown(b.MapName)
		}
		switch {
		case c.healer && !c.leader: // a healer goes where a party is; a healer guild leader leads where it hunts
			c.m = ""
		case g.mapped():
			c.m = g.Map
			if g.onMap(b.Goal.Map) {
				c.m = b.Goal.Map
			}
		default:
			c.m = b.Goal.Map
		}
		b.mu.Unlock()
		if !c.social || ((!c.healer || c.leader) && (c.m == "" || isTown(c.m))) {
			continue
		}
		cs = append(cs, c)
	}
	parties.Lock()
	type praw struct {
		p      *Party
		ms     []*Bot
		lead   *Bot
		m      string
		anchor string
		item   int
	}
	var raws []praw
	for _, p := range parties.byName {
		if p.Human != "" || p.Leader == nil || p.Map == "" || isTown(p.Map) || len(p.Members) >= lfgMaxMembers+1 {
			continue
		}
		raws = append(raws, praw{p, append([]*Bot(nil), p.Members...), p.Leader, p.Map, p.Anchor, p.AnchorItem})
	}
	parties.Unlock()
	var ps []*lfgParty
	for _, r := range raws {
		if isRestoring(r.p) || r.p.bossActive() {
			continue
		}
		if s, ok := peek(r.lead.P.Name); !ok || !s.Online {
			continue
		}
		P := &lfgParty{p: r.p, lead: r.lead, ms: r.ms, m: r.m, anchor: r.anchor, item: r.item, lo: 999, posted: pposted[r.p]}
		sum, n := 0, 0
		for _, m := range r.ms {
			m.mu.Lock()
			role, healer, monk, lv, known := partyRole(m), m.isHealer(), isMonkClass(m.Class), m.BaseLvOr(), m.BaseLv > 0
			m.mu.Unlock()
			P.size++
			if healer {
				P.healer = true
			} else {
				P.nonHealers++
			}
			P.monk = P.monk || monk
			P.roles[role]++
			if known {
				sum, n = sum+lv, n+1
				P.lo, P.hi = min(P.lo, lv), max(P.hi, lv)
			}
		}
		if n == 0 {
			continue
		}
		P.avg = sum / n
		ps = append(ps, P)
	}
	return cs, ps
}

// injectable for the offline cold-start simulation (lfg_test.go): map distance, map fit, joining and forming
var (
	lfgHops  = mapHops
	lfgMapOK = func(P *lfgParty, c *lfgCand) bool { return P.mapOKFor(c) }
	lfgJoin  = joinGoalParty
	lfgForm  = func(anchor *lfgCand, group []*lfgCand, now time.Time, local bool) {
		if local {
			lfgLocalSay(anchor, group)
			formLocalParty(anchor, group)
			return
		}
		formGoalParty(anchor, group, now)
	}
)

// lfgCrossMax: new parties formed per tick across maps (whispers) after the same-map ones - bounds the work
const lfgCrossMax = 60

// lfgMatch: free candidates join open parties, then form new ones - on their own map first (local chat), then across
// maps (whispers). Direct matching by goal map, level band and roles: no #global post needed (posts are flavour and
// fill the last places). Live 2026-11-14 12:00-12:13: gated by posts (6/min) only 43 parties re-formed in 11 min,
// 80% of the bots soloed and solo deaths doubled. Returns how many joined and how many parties formed.
func lfgMatch(cs []*lfgCand, ps []*lfgParty, used map[*Bot]bool, now time.Time) (joins, formed int) {
	// 1. open parties take candidates: no-healer parties first; up to 2 per party per tick
	sort.SliceStable(ps, func(i, j int) bool {
		if ps[i].healer != ps[j].healer {
			return !ps[i].healer
		}
		return ps[i].posted && !ps[j].posted
	})
	// 0. a free player whose friend is in an open party with room joins that one first, when its goal fits there
	// (friends.go bestFriendsOnline; owner rule: friends party together)
	inP := map[*Bot]*lfgParty{}
	for _, P := range ps {
		for _, m := range P.ms {
			inP[m] = P
		}
	}
	for _, c := range cs {
		if used[c.b] || c.leader || c.b == nil || c.b.P == nil {
			continue
		}
		for _, f := range bestFriendsOnline(c.b, 3) {
			P := inP[f]
			if P == nil || P.joins >= 2 || !lfgFits(c, P) || !lfgMapOK(P, c) {
				continue
			}
			used[c.b] = true
			if lfgJoin(P.p, c, now) {
				P.add(c)
				joins++
			}
			break
		}
	}
	for _, P := range ps {
		for P.joins < 2 {
			var best *lfgCand
			bs := -1 << 30
			for _, c := range cs {
				if used[c.b] || c.leader || !lfgFits(c, P) {
					continue
				}
				if s := lfgScore(c, P); s > bs && lfgMapOK(P, c) {
					best, bs = c, s
				}
			}
			if best == nil {
				break
			}
			used[best.b] = true
			if !lfgJoin(P.p, best, now) {
				continue
			}
			P.add(best)
			joins++
		}
	}
	// 2. same map (local chat), then across maps (whispers): posters anchor first, then item-goal farmers
	formed += lfgLocal(cs, used, now)
	order := append([]*lfgCand(nil), cs...)
	sort.SliceStable(order, func(i, j int) bool {
		pi, pj := order[i].posted, order[j].posted
		if pi != pj {
			return pi
		}
		return order[i].g != nil && order[i].g.mapped() && !(order[j].g != nil && order[j].g.mapped())
	})
	cross := 0
	for _, c := range order {
		if cross >= lfgCrossMax {
			break
		}
		if used[c.b] {
			continue
		}
		anchor := c
		if c.healer && !c.leader { // a healer never anchors (it joins the nearest-level farmer) - a guild leader always leads
			anchor = nil
			bs := -1 << 30
			for _, o := range cs {
				if used[o.b] || o == c || o.healer || o.m == "" || abs(o.lv-c.lv) > lfgLvBand {
					continue
				}
				P := soloParty(o)
				if !lfgFits(c, P) {
					continue
				}
				if s := -abs(o.lv - c.lv); s > bs && lfgMapOK(P, c) {
					anchor, bs = o, s
				}
			}
			if anchor == nil {
				continue
			}
		}
		P := soloParty(anchor)
		var group []*lfgCand
		if anchor != c {
			group = append(group, c)
			P.add(c)
		}
		for len(group) < 3 {
			var best *lfgCand
			bs := -1 << 30
			for _, o := range cs {
				if used[o.b] || o.leader || o == anchor || o == c || !lfgFits(o, P) {
					continue
				}
				if s := lfgScore(o, P); s > bs && lfgMapOK(P, o) {
					best, bs = o, s
				}
			}
			if best == nil {
				break
			}
			group = append(group, best)
			P.add(best)
		}
		if len(group) == 0 {
			continue
		}
		used[anchor.b] = true
		for _, g := range group {
			used[g.b] = true
		}
		cross++
		formed++
		lfgForm(anchor, group, now, false)
	}
	return
}

// lfgTick: matching first, then new posts within the #global budget (flavour: they also fill the last places).
func lfgTick(bots []*Bot, now time.Time) {
	cs, ps := lfgGather(bots, now)
	used := map[*Bot]bool{}
	lfgMatch(cs, ps, used, now)
	// 3. new posts: parties missing a healer, free healers, fixed-goal farmers, small parties, then the rest
	type poster struct {
		prio int
		c    *lfgCand
		P    *lfgParty
	}
	var queue []poster
	lfg.Lock()
	for _, P := range ps {
		if P.joins > 0 || now.Sub(lfg.partyPost[P.p]) < lfgRepost {
			continue
		}
		switch {
		case !P.healer && !P.monk:
			queue = append(queue, poster{0, nil, P})
		case P.size < 3:
			queue = append(queue, poster{3, nil, P})
		}
	}
	for _, c := range cs {
		if used[c.b] || c.posted || now.Sub(lfg.lastPost[c.b]) < lfgRepost {
			continue
		}
		pr := 4
		switch {
		case c.healer:
			pr = 1
		case c.g.mapped():
			pr = 2
		}
		queue = append(queue, poster{pr, c, nil})
	}
	lfg.Unlock()
	// round robin over the priorities (random order inside each): a party missing a healer posts first, but ~190 old
	// healer-less parties must not starve every solo farmer's post (live 2026-11-14: 19 of 19 posts were LFM priest)
	buckets := map[int][]poster{}
	for _, q := range queue {
		buckets[q.prio] = append(buckets[q.prio], q)
	}
	for _, b := range buckets {
		for i := range b {
			j := i + crand(len(b)-i)
			b[i], b[j] = b[j], b[i]
		}
	}
	queue = queue[:0]
	for more := true; more; {
		more = false
		for pr := 0; pr <= 4; pr++ {
			if b := buckets[pr]; len(b) > 0 {
				queue = append(queue, b[0])
				buckets[pr] = b[1:]
				more = true
			}
		}
	}
	for _, q := range queue {
		if !lfgTake(now) {
			break
		}
		if q.P != nil {
			lfmPost(q.P, now)
		} else {
			lfgPostSolo(q.c, now)
		}
	}
}

// lfgLocalMax: parties formed through local chat per tick (20 s)
const lfgLocalMax = 15

// lfgLocal: free candidates standing on the same map form parties through local chat. The anchor (a farmer whose
// goal map is this map, an item goal first) asks; a healer and the roles that make sense answer.
func lfgLocal(cs []*lfgCand, used map[*Bot]bool, now time.Time) int {
	byMap := map[string][]*lfgCand{}
	for _, c := range cs {
		if !used[c.b] && c.at != "" && !isTown(c.at) {
			byMap[c.at] = append(byMap[c.at], c)
		}
	}
	formed := 0
	for m, list := range byMap {
		if len(list) < 2 {
			continue
		}
		sort.SliceStable(list, func(i, j int) bool { // item-goal farmers anchor first
			return list[i].g != nil && list[i].g.mapped() && !(list[j].g != nil && list[j].g.mapped())
		})
		for _, a := range list {
			if formed >= lfgLocalMax {
				return formed
			}
			if used[a.b] || (a.healer && !a.leader) || a.m != m {
				continue // the anchor must farm this very map
			}
			P := soloParty(a)
			var group []*lfgCand
			for len(group) < 3 {
				var best *lfgCand
				bs := -1 << 30
				for _, o := range list {
					if used[o.b] || o == a || o.leader || !lfgFits(o, P) {
						continue
					}
					if s := lfgScore(o, P); s > bs && lfgMapOK(P, o) {
						best, bs = o, s
					}
				}
				if best == nil {
					break
				}
				group = append(group, best)
				P.add(best)
			}
			if len(group) == 0 {
				continue
			}
			used[a.b] = true
			for _, g := range group {
				used[g.b] = true
			}
			formed++
			lfgStats.Lock()
			lfgStats.local++
			lfgStats.Unlock()
			lfgForm(a, group, now, true)
		}
	}
	return formed
}

// lfgLocalSay: the anchor asks in local chat, one of the group answers (own goroutine)
func lfgLocalSay(a *lfgCand, group []*lfgCand) {
	go func() {
		a.b.mu.Lock()
		a.b.say(styled(a.typing, pick("anyone wanna party? "+a.g.desc(), "party up? "+a.g.desc()+" here", "lf party here, "+a.g.desc())))
		a.b.mu.Unlock()
		time.Sleep(time.Duration(2+crand(4)) * time.Second)
		g := group[0]
		g.b.mu.Lock()
		g.b.say(styled(g.typing, pick("sure", "me!", "inv", "ok inv me")))
		g.b.mu.Unlock()
	}()
}

// soloParty: a free farmer as the anchor of a party to be
func soloParty(c *lfgCand) *lfgParty {
	P := &lfgParty{lead: c.b, ms: []*Bot{c.b}, m: c.m, avg: c.lv, lo: c.lv, hi: c.lv, size: 1}
	if c.g != nil && c.g.mapped() {
		P.anchor, P.item = c.m, c.g.farmItem()
	}
	if c.healer {
		P.healer = true
	} else {
		P.nonHealers = 1
	}
	P.monk = c.monk
	P.roles[c.role]++
	return P
}

func lfgPostSolo(c *lfgCand, now time.Time) {
	line := lfgLine(c)
	lfg.Lock()
	lfg.posts[c.b] = &lfgPost{Bot: c.b, Map: c.m, At: now, Line: line, Kind: c.g.Kind}
	lfg.lastPost[c.b] = now
	lfg.Unlock()
	lfgStats.Lock()
	lfgStats.posts[c.g.Kind]++
	lfgStats.postTimes = append(lfgStats.postTimes, now)
	if len(lfgStats.postTimes) > 500 {
		lfgStats.postTimes = lfgStats.postTimes[len(lfgStats.postTimes)-500:]
	}
	lfgStats.Unlock()
	c.b.channelSay("#global", styled(c.typing, line))
}

func lfmPost(P *lfgParty, now time.Time) {
	P.lead.mu.Lock()
	typing := P.lead.P.Typing
	P.lead.mu.Unlock()
	mt := mapTalk(P.m)
	kind := "lfm"
	var line string
	if !P.healer && !P.monk {
		kind = "lfm-heal"
		line = pick(fmt.Sprintf("LFM priest for %s, we're %s", mt, lvRange(P.lo, P.hi)), fmt.Sprintf("need a priest for %s, lv%d ish party", mt, P.avg),
			fmt.Sprintf("LFM heal for %s, %d/6, lv%d+", mt, P.size, P.lo), fmt.Sprintf("any priest lv%d-%d? we're at %s", P.avg-5, P.avg+5, mt))
	} else {
		need := "dps"
		if P.roles[roleTank] == 0 {
			need = "tank"
		} else if P.roles[roleRanged] == 0 {
			need = "ranged"
		}
		line = pick(fmt.Sprintf("LFM %s, %s, need %s", mt, lvRange(P.lo, P.hi), need), fmt.Sprintf("party at %s lf 1-2 more, %s", mt, lvRange(P.lo, P.hi)),
			fmt.Sprintf("LFM %d/6 %s, %s pls", P.size, mt, need))
	}
	lfg.Lock()
	lfg.posts[P.lead] = &lfgPost{Bot: P.lead, Party: P.p, Map: P.m, At: now, Line: line, Kind: kind}
	lfg.partyPost[P.p] = now
	lfg.Unlock()
	lfgStats.Lock()
	lfgStats.posts[kind]++
	lfgStats.postTimes = append(lfgStats.postTimes, now)
	if len(lfgStats.postTimes) > 500 {
		lfgStats.postTimes = lfgStats.postTimes[len(lfgStats.postTimes)-500:]
	}
	lfgStats.Unlock()
	P.lead.channelSay("#global", styled(typing, line))
}

// lvRange: "lv48" or "lv45-52"
func lvRange(lo, hi int) string {
	if lo >= hi {
		return fmt.Sprintf("lv%d", hi)
	}
	return fmt.Sprintf("lv%d-%d", lo, hi)
}

// lfgLine: a free bot's LFG line for its goal
func lfgLine(c *lfgCand) string {
	mt := mapTalk(c.m)
	cls, lv := c.class, c.lv
	switch {
	case c.healer:
		return pick(fmt.Sprintf("%s %d LFP, full support", cls, lv), fmt.Sprintf("lf party, %s lv%d, heal+bless+inc agi", cls, lv),
			fmt.Sprintf("lv%d %s looking for a party to heal, lv%d-%d", lv, cls, lv-10, lv+10))
	case c.g.Kind == "gear":
		return pick(fmt.Sprintf("LFP %s, farming %s, lv%d %s", mt, c.g.Item, lv, cls), fmt.Sprintf("lv%d %s lf party at %s, after a %s", lv, cls, mt, c.g.Item),
			fmt.Sprintf("anyone farming %s at %s? lv%d %s", c.g.Item, mt, lv, cls))
	case c.g.Kind == "card":
		return pick(fmt.Sprintf("LFP %s hunting %s card, lv%d %s", mt, c.g.Mob, lv, cls), fmt.Sprintf("lv%d %s camping %s for %s card, party?", lv, cls, mt, c.g.Mob))
	case c.g.Kind == "zeny" && c.g.Sell != "":
		return pick(fmt.Sprintf("LFP farming %s on %s, lv%d %s", c.g.Sell, mt, lv, cls), fmt.Sprintf("lv%d %s lf party, %s at %s", lv, cls, c.g.Sell, mt))
	case c.g.Kind == "zeny":
		return pick(fmt.Sprintf("LFP %s, farming zeny, lv%d %s", mt, lv, cls), fmt.Sprintf("lv%d %s lf party for zeny farm, %s", lv, cls, mt))
	}
	return pick(fmt.Sprintf("LFP %s lvling, lv%d %s", mt, lv, cls), fmt.Sprintf("lv%d %s looking for party, %s", lv, cls, mt),
		fmt.Sprintf("any party at %s for lv%d %s?", mt, lv, cls))
}

// lfgAnswer: what an answering bot whispers the poster
func lfgAnswer(c *lfgCand) string {
	if c.healer {
		return pick(fmt.Sprintf("hi, lv%d %s here, need heals?", c.lv, c.class), fmt.Sprintf("can i join? %s %d", c.class, c.lv))
	}
	return pick(fmt.Sprintf("hi, can i join? lv%d %s", c.lv, c.class), fmt.Sprintf("inv pls, %s %d", c.class, c.lv),
		fmt.Sprintf("me! lv%d %s, %s", c.lv, c.class, c.g.desc()))
}

// joinGoalParty: candidate c answers party p's post (or p's leader answers c's) and joins; the keeper
// (partypersist.go botPartyKeeper) sends the invite.
func joinGoalParty(p *Party, c *lfgCand, now time.Time) bool {
	if c.leader || c.b.alwaysOnline() {
		return false // a guild leader leads, it never joins
	}
	parties.Lock()
	if parties.byName[p.Name] != p || parties.ofBot[c.b] != nil || len(p.Members) >= lfgMaxMembers+1 || p.Leader == nil {
		parties.Unlock()
		return false
	}
	p.Members = append(p.Members, c.b)
	parties.ofBot[c.b] = p
	m, lead, name := p.Map, p.Leader, p.Name
	if !p.Need {
		// an old party that recruits by need is a need party from now on - re-formed now: its 3 h cap counts from here
		// (with its old Formed time a party restored for hours ended at once)
		p.Need, p.EndAt, p.Formed = true, time.Time{}, time.Now()
		if p.Purpose == "" {
			p.Purpose = "lvling @ " + m
		}
	}
	parties.Unlock()
	markPartiesDirty()
	lfg.Lock()
	delete(lfg.posts, c.b)
	lfg.Unlock()
	lfgStats.Lock()
	lfgStats.joins++
	lfgStats.Unlock()
	leadName := lead.charName()
	c.b.mu.Lock()
	c.b.Goal = Goal{Mode: "party", Map: m, Who: leadName, Set: "party"}
	c.b.route, c.b.path = nil, nil
	c.b.event("party", "", fmt.Sprintf("joined %s (%s) on %s", name, c.g.desc(), m))
	c.b.mu.Unlock()
	go func() {
		if c.posted {
			lead.mu.Lock()
			lt := lead.P.Typing
			lead.mu.Unlock()
			lead.whisper(c.name, styled(lt, pick("hey, we're at "+mapTalk(m)+", want in?", "inv sent", "join us at "+mapTalk(m))))
		} else {
			c.b.whisper(leadName, styled(c.typing, lfgAnswer(c)))
		}
		time.Sleep(time.Duration(20+crand(20)) * time.Second) // the keeper's invite arrives meanwhile
		memoryOf(c.b.P).meet(leadName, "partymate", m, false)
		c.b.partySay(styled(c.typing, pick("hi all", "hey, ty for inv", "yo, "+c.g.desc()+" here too", "hi! omw")))
		time.Sleep(time.Duration(2+crand(4)) * time.Second)
		lead.mu.Lock()
		lt := lead.P.Typing
		lead.mu.Unlock()
		lead.partySay(styled(lt, pick("welcome", "hey "+c.name, "o/", "welcome, we're at "+mapTalk(m))))
	}()
	return true
}

// formGoalParty: anchor (the poster, or the farmer a healer answered) forms a party with group on its map.
// formLocalParty: formGoalParty without the whispers (the local chat lines asked and answered)
func formLocalParty(anchor *lfgCand, group []*lfgCand) {
	ms := make([]*Bot, 0, len(group))
	for _, g := range group {
		ms = append(ms, g.b)
	}
	purpose := anchor.g.desc()
	fixed := anchor.g != nil && anchor.g.mapped()
	p := formPartyOpt(anchor.b, ms, anchor.m, !fixed, func(p *Party) {
		p.Need, p.Purpose = true, purpose+" @ "+anchor.m
		if fixed {
			p.Anchor, p.AnchorItem = anchor.m, anchor.g.farmItem()
		}
	})
	if p == nil {
		return
	}
	lfgStats.Lock()
	lfgStats.formed++
	lfgStats.joins += len(group)
	lfgStats.Unlock()
}

func formGoalParty(anchor *lfgCand, group []*lfgCand, now time.Time) {
	ms := make([]*Bot, 0, len(group))
	for _, g := range group {
		ms = append(ms, g.b)
	}
	purpose := anchor.g.desc()
	fixed := anchor.g != nil && anchor.g.mapped()
	p := formPartyOpt(anchor.b, ms, anchor.m, !fixed, func(p *Party) {
		p.Need, p.Purpose = true, purpose+" @ "+anchor.m
		if fixed {
			p.Anchor, p.AnchorItem = anchor.m, anchor.g.farmItem()
		}
	})
	if p == nil {
		return
	}
	lfg.Lock()
	delete(lfg.posts, anchor.b)
	for _, g := range group {
		delete(lfg.posts, g.b)
	}
	lfg.Unlock()
	lfgStats.Lock()
	lfgStats.formed++
	lfgStats.joins += len(group)
	lfgStats.Unlock()
	go func() {
		for _, g := range group {
			to, from, typing := anchor.name, g, g.typing
			if g.posted && !anchor.posted {
				to, from, typing = g.name, anchor, anchor.typing
			}
			from.b.whisper(to, styled(typing, lfgAnswer(from)))
			time.Sleep(time.Duration(1+crand(3)) * time.Second)
		}
	}()
}

// lfgNoteLeft: a bot just left / lost its party: it waits a little before looking again (no instant re-join)
func lfgNoteLeft(b *Bot) {
	lfg.Lock()
	lfg.leftAt[b] = time.Now()
	lfg.Unlock()
}

// lfgManager: goals, party ends, LFG matching and posts (main instance; only with goal_parties_on).
func lfgManager(bots []*Bot, stop <-chan struct{}) {
	t := time.NewTicker(20 * time.Second)
	defer t.Stop()
	last := map[*Bot]time.Time{}
	boot := time.Now()
	if coreInstance() {
		loadTurns() // lfg_human.go: the human party's turn order and the human's goal survive a core restart
	}
	for {
		select {
		case <-stop:
			return
		case <-t.C:
		}
		if coreInstance() {
			// the core instance runs only the human-led party: goals for its bots + the party's goal turns, whatever
			// goal_parties_on says (owner: Ayla's party follows the need rule; the turns don't depend on the flag)
			now := time.Now()
			goalTick(bots, last, now)
			humanTurnTick(now)
			continue
		}
		if !goalPartiesOn() {
			continue
		}
		now := time.Now()
		goalTick(bots, last, now)
		goalPartyCheck(now)
		if now.Sub(boot) < 3*time.Minute {
			continue // after a restart: let everyone log in and parties restore first
		}
		lfgTick(bots, now)
	}
}

// ---------------------------------------------------------------- API

func registerLFG(mux *http.ServeMux) {
	// GET /api/lfg/fit?party=<name or leader>: why a party sits on its map - the fit check's numbers (weakest effective
	// level, strength bonus, map OK / too easy / exp-poor / crowded, the maps the relocation would pick) and, for every
	// guide spot, the first reason it is rejected.
	mux.HandleFunc("/api/lfg/fit", func(w http.ResponseWriter, r *http.Request) {
		q := strings.ToLower(r.URL.Query().Get("party"))
		var p *Party
		parties.Lock()
		for _, x := range parties.byName {
			if strings.ToLower(x.Name) == q || (x.Leader != nil && strings.ToLower(x.Leader.charName()) == q) {
				p = x
			}
		}
		var ms []*Bot
		m, moved, formed, anchored := "", time.Time{}, time.Time{}, false
		if p != nil {
			ms, m, moved, formed, anchored = append([]*Bot(nil), p.Members...), p.Map, p.MovedAt, p.Formed, p.partyAnchored()
		}
		parties.Unlock()
		if p == nil {
			http.Error(w, "no such party", 404)
			return
		}
		minLv, fitLv, known, unknown := partyFitLv(ms)
		minBase := partyMinBase(ms)
		type mrow struct {
			Name    string `json:"name"`
			Lv      int    `json:"lv"`
			Power   int    `json:"power"`
			Fragile bool   `json:"fragile"`
			Online  bool   `json:"online"`
			Known   bool   `json:"known"`
			Support bool   `json:"healer"`
			Tank    bool   `json:"tank"`
		}
		var rows []mrow
		for _, b := range ms {
			s, _ := peek(b.P.Name)
			rows = append(rows, mrow{b.charName(), s.Lv, s.Power, s.Fragile, s.Online, s.Known, s.Support, s.Tank})
		}
		t := nearestTown(m)
		sp := townSpawn[t]
		near, any := "", ""
		if len(sp) >= 2 {
			near = partyMapNearTier(ms, t, sp[0], sp[1], m, false)
			any = partyMapSearch(ms, t, sp[0], sp[1], m, partyAnySafe, true)
		}
		type cand struct {
			Map    string `json:"map"`
			Band   string `json:"band"`
			Reject string `json:"reject,omitempty"`
		}
		var cs []cand
		for _, g := range guideSpots {
			if g.Max+15 < minBase || g.Min > minLv+12 {
				continue
			}
			why := ""
			switch {
			case maps[g.Map] == nil:
				why = "no map data"
			case g.Min > minLv+2:
				why = fmt.Sprintf("band starts at %d > weakest %d+2", g.Min, minLv)
			case fitShare(g.Map, fitLv) < minFitShare:
				why = fmt.Sprintf("monsters over the target filter (fit lv %d)", fitLv)
			case partyMapTooHard(g.Map, minLv):
				why = fmt.Sprintf("too hard for weakest %d (mean %d, aggro %d%%, top aggro %d)", minLv, mapMeanLv(g.Map), mapAggroPct(g.Map), mapAggroTopLv(g.Map))
			case mapExpPoor(g.Map, minBase):
				why = "exp-poor"
			case partyMapDeadly(g.Map):
				why = "deadly now"
			case swarmVeto(g.Map, minLv) || mapReadsEmpty(g.Map) || bossRecent(g.Map, minLv):
				why = "swarm / empty / boss note"
			case !mapHasRoom(g.Map, len(ms)):
				why = "no room"
			}
			cs = append(cs, cand{g.Map, fmt.Sprintf("%d-%d", g.Min, g.Max), why})
		}
		now := time.Now()
		writeJSON(w, map[string]interface{}{"party": p.Name, "map": m, "members": rows, "weakest_effective": minLv,
			"fit_lv": fitLv, "known": known, "unknown": unknown, "strength_bonus": partyStrength(ms), "min_base": minBase,
			"map_ok": partyMapOK(ms, m), "too_easy": partyMapTooEasy(ms, m), "exp_poor": mapExpPoor(m, minBase),
			"crowded": mapCrowded(m), "anchored": anchored, "resting": p.resting(),
			"moved_min_ago": int(now.Sub(moved).Minutes()), "formed_min_ago": int(now.Sub(formed).Minutes()),
			"relocation_real_exp": near, "relocation_any_safe": any, "spots": cs})
	})
	registerPartyTurn(mux) // lfg_human.go: the human-led party's goal turns (core instance; main forwards)
	mux.HandleFunc("/api/lfg", func(w http.ResponseWriter, r *http.Request) {
		bots, _ := allBots.Load().([]*Bot)
		now := time.Now()
		if want := strings.ToLower(r.URL.Query().Get("bot")); want != "" {
			// by name without taking every bot's lock (one busy bot stalled the whole lookup)
			if b := botByCharName(r.URL.Query().Get("bot")); b != nil {
				bots = []*Bot{b}
			}
			for _, b := range bots {
				b.mu.Lock()
				if strings.ToLower(b.charName()) == want {
					g := b.fgoal
					var cp farmGoal
					if g != nil {
						cp = *g
					}
					out := map[string]interface{}{"bot": b.charName(), "goal": cp, "has_goal": g != nil, "lv": b.BaseLvOr(), "map": b.MapName}
					b.mu.Unlock()
					if p := partyOf(b); p != nil {
						parties.Lock()
						out["party"], out["party_map"], out["party_purpose"] = p.Name, p.Map, p.Purpose
						parties.Unlock()
					}
					writeJSON(w, out)
					return
				}
				b.mu.Unlock()
			}
			http.Error(w, "no such bot", 404)
			return
		}
		kinds := map[string]int{}
		fixedServed, fixedN, sellN, mappedN, mappedOn := 0, 0, 0, 0, 0
		for _, b := range bots {
			b.mu.Lock()
			if b.Online && b.fgoal != nil {
				kinds[b.fgoal.Kind]++
				if b.fgoal.Sell != "" {
					sellN++
				}
				if b.fgoal.mapped() {
					mappedN++
					if b.fgoal.onMap(b.MapName) {
						mappedOn++
					}
				}
				if b.fgoal.fixed() {
					fixedN++
					if b.fgoal.serves(b.MapName) {
						fixedServed++
					}
				}
			}
			b.mu.Unlock()
		}
		type postView struct {
			Bot  string `json:"bot"`
			Kind string `json:"kind"`
			Map  string `json:"map"`
			Line string `json:"line"`
			Age  int    `json:"age_s"`
		}
		var posts []postView
		lfg.Lock()
		for _, p := range lfg.posts {
			posts = append(posts, postView{p.Bot.charName(), p.Kind, p.Map, p.Line, int(now.Sub(p.At).Seconds())})
		}
		lfg.Unlock()
		sort.Slice(posts, func(i, j int) bool { return posts[i].Age < posts[j].Age })
		lfgStats.Lock()
		perMin := 0
		for _, t := range lfgStats.postTimes {
			if now.Sub(t) < 10*time.Minute {
				perMin++
			}
		}
		cp := func(m map[string]int) map[string]int {
			o := make(map[string]int, len(m))
			for k, v := range m {
				o[k] = v
			}
			return o
		}
		out := map[string]interface{}{
			"on": goalPartiesOn(), "since_min": int(now.Sub(lfgStats.started).Minutes()),
			"goals_now": kinds, "fixed_goal_bots": fixedN, "fixed_goal_on_goal_map": fixedServed,
			"zeny_goals_farming_sellable": sellN, "mapped_goal_bots": mappedN, "mapped_goal_on_own_map": mappedOn,
			"goals_set": cp(lfgStats.set), "goals_met": cp(lfgStats.met), "goals_expired": cp(lfgStats.expired),
			"goals_changed": cp(lfgStats.changed), "posts": cp(lfgStats.posts), "posts_per_min_10m": float64(perMin) / 10,
			"joins": lfgStats.joins, "parties_formed": lfgStats.formed, "parties_formed_local": lfgStats.local, "parties_dissolved": cp(lfgStats.dissolved),
			"members_left": cp(lfgStats.leaves), "open_posts": posts,
		}
		lfgStats.Unlock()
		writeJSON(w, out)
	})
}
