package main

// buildgear.go (economy batch 21, owner rule 2026-11-14: "the economy bots should know all items suitable for any
// specific build and price them and encourage them to be traded"). Step 1: a per-build item table.
//
// For every build in builds.go and every level band (1-20, 21-40, 41-60, 61-80, 81-99) and slot (weapon, shield,
// armor, garment, shoes, headgear, accessory) it lists the items that suit the build, best first:
//   - "guide": the build-and-strategy agent's picks in data/buildgear.json (re-read every 5 min);
//   - "plan": the builds.go routes (Weapons / Armors / Extras);
//   - "auto": items that exist in this world (an NPC sells them, or a spawning monster drops them), wearable by the
//     band's job (1st job up to lv40, both 41-60, 2nd job above; item_db Jobs and EquipLevelMin), class weapons only
//     for the weapon slot, ranked by the build's style (melee ATK, bow, magic MATK) and DEF, slots as a small bonus.
// Cards per band: the guide's, plus every card in the world whose bonuses suit the build's stat goals (cardValue).
// GET /api/buildgear?build=<id> (no build = the list of builds), /api/buildgear/fit (share of online bots whose worn
// weapon and armor are on their table).

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type bgCard struct {
	ID   int    `json:"id"`
	Slot string `json:"slot"`
	Why  string `json:"why"`
	Prio int    `json:"prio"`
}

type bgBand struct {
	Min       int      `json:"min"`
	Max       int      `json:"max"`
	Weapon    []int    `json:"weapon"`
	Armor     []int    `json:"armor"`
	Shield    []int    `json:"shield"`
	Garment   []int    `json:"garment"`
	Shoes     []int    `json:"shoes"`
	Headgear  []int    `json:"headgear"`
	Accessory []int    `json:"accessory"`
	Ammo      []int    `json:"ammo"`
	Cards     []bgCard `json:"cards"`
	Note      string   `json:"note"`
}

type bgBuild struct {
	Source []string `json:"source"`
	Stats  string   `json:"stats"`
	Bands  []bgBand `json:"bands"`
}

type bgFile struct {
	Version int                 `json:"version"`
	Updated string              `json:"updated"`
	Builds  map[string]*bgBuild `json:"builds"`
}

// gearEntry: one suitable item in a build's table
type gearEntry struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Slot   string `json:"slot"`
	MinLv  int    `json:"min_lv"`
	Price  int    `json:"price"`
	Source string `json:"source"` // guide / plan / auto
	Score  int    `json:"score"`
	Why    string `json:"why,omitempty"`
}

type gearBand struct {
	Min   int                    `json:"min"`
	Max   int                    `json:"max"`
	Slots map[string][]gearEntry `json:"slots"`
	Cards []gearEntry            `json:"cards"`
	Note  string                 `json:"note,omitempty"`
}

var bandEdges = [][2]int{{1, 20}, {21, 40}, {41, 60}, {61, 80}, {81, 99}}

var gearSlots = []struct {
	Name string
	Mask uint32
}{{"weapon", 0x2}, {"shield", 0x20}, {"armor", 0x10}, {"garment", 0x4}, {"shoes", 0x40}, {"headgear", 0x301}, {"accessory", 0x88}}

var buildGear = struct {
	sync.Mutex
	file    *bgFile
	fileMod time.Time
	builtAt time.Time
	table   map[string][]gearBand // build id -> bands
	member  map[string]map[int]bool
}{}

// buildStyle: melee / bow / magic for a build (builds.go gearStyle, by plan id)
func buildStyle(p *Build) string {
	if p.ID == "rogue_bow" {
		return "bow" // a bow variant of a melee class (builds.go gearStyle)
	}
	if p.ID == "priest_battle" || p.ID == "sage_hindsight" {
		return "melee" // run 5 melee variants of caster classes (builds.go gearStyle)
	}
	switch p.base() {
	case "wizard", "sage", "priest":
		return "magic"
	case "hunter", "bard", "dancer":
		return "bow"
	}
	return "melee"
}

// buildItemScore: Bot.itemScore for a build instead of a bot
func buildItemScore(style string, it *Item) int {
	if it.Type == "Weapon" || gearSlot(it.Loc) == 0x2 {
		switch style {
		case "magic":
			return it.Matk*3 + it.Atk/2 + it.Buy/1000
		case "bow":
			if it.Sub == "Bow" || it.Sub == "Musical" || it.Sub == "Whip" {
				return 50 + it.Atk*2 + it.Buy/1000
			}
			return it.Atk / 2
		}
		if it.Sub == "Staff" {
			return it.Atk
		}
		return it.Atk*2 + it.Buy/1000
	}
	v := it.Def*3 + it.Buy/1000
	if style == "magic" {
		v += it.Matk * 2
	}
	return v
}

// buildCardValue: Bot.cardValue for a build
func buildCardValue(p *Build, id int) int {
	cardBonOnce.Do(loadCardBonuses)
	cb := cardBon[id]
	if cb == nil {
		return 0
	}
	var w [6]int
	for _, g := range p.Stats {
		if g.Stat >= 0 && g.Stat < 6 {
			w[g.Stat] = max(w[g.Stat], g.Value/10)
		}
	}
	v, sum := 0, 0
	for s := 0; s < 6; s++ {
		v += cb.stat[s] * w[s] * 2
		sum += w[s]
	}
	v += cb.all * sum * 2
	v += cb.hp/25 + cb.hpPct*3 + cb.def*3 + cb.mdef*2 + cb.hit/2 + cb.flee/2 + cb.flee2*2 + cb.crit + cb.aspd*2
	style := buildStyle(p)
	if style == "magic" || p.base() == "monk" || p.base() == "crusader" {
		v += cb.sp / 15
	}
	if style == "magic" {
		v += cb.matk * 2
	} else {
		v += cb.atk / 2
	}
	return v
}

func usableByJob(it *Item, key string) bool {
	if it.Jobs == nil {
		return true
	}
	if it.Jobs["!"+key] {
		return false
	}
	return it.Jobs["All"] || it.Jobs[key]
}

// inWorld: an NPC sells it, or a monster that spawns somewhere drops it
func inWorld(id int) bool {
	if len(gearShops(id)) > 0 {
		return true
	}
	lookupOnce.Do(loadLookup)
	for _, mid := range droppedBy[id] {
		if len(mobMaps[mid]) > 0 {
			return true
		}
	}
	return false
}

// buildWeaponOK: the weapon types a build fights with
func buildWeaponOK(p *Build, it *Item) bool {
	switch buildStyle(p) {
	case "bow":
		switch it.Sub {
		case "Bow":
			return true
		case "Musical":
			return p.base() == "bard"
		case "Whip":
			return p.base() == "dancer"
		}
		return false
	case "magic":
		switch it.Sub {
		case "Staff", "2hStaff", "Book":
			return true
		case "Mace", "2hMace":
			return p.base() == "priest"
		}
		return false
	}
	return isClassWeapon(family(p.First), it) && planWeaponSub(p.ID, it.Sub)
}

// planWeaponSubs: builds whose core skills need one weapon type (build-and-strategy agent, 2026-11-14: Knight is a
// two-hand sword build - Two-Hand Quicken refuses spears; Crusader is 1H sword + shield). Others: any class weapon.
var planWeaponSubs = map[string][]string{
	"knight":       {"2hSword"},
	"crusader":     {"1hSword"},
	"knight_spear": {"1hSpear", "2hSpear"}, // Brandish Spear / Pierce need a spear
	// build variants run 4: the weapon each variant's skills need
	"assassin_sonic":  {"Katar"},          // Sonic Blow / Katar Mastery
	"assassin_dagger": {"Dagger"},         // Double Attack procs only with a dagger
	"rogue_bow":       {"Bow"},            // Double Strafe
	"blacksmith_mace": {"Mace", "2hMace"}, // the mace smith (Adrenaline Rush takes axes and maces)
	// run 5 variants
	"knight_pierce":  {"1hSpear", "2hSpear"}, // Pierce / Spear Boomerang / Brandish Spear
	"crusader_spear": {"2hSpear", "1hSpear"}, // Spear Quicken needs a two-handed spear
	"priest_battle":  {"Mace"},               // Mace Mastery
	"sage_hindsight": {"Book", "Dagger"},     // Study (book ASPD); Hindsight procs on any melee hit
}

func planWeaponSub(build, sub string) bool {
	l, ok := planWeaponSubs[build]
	if !ok {
		l, ok = planWeaponSubs[builds[build].base()] // a variant without its own rule follows its main plan
	}
	if !ok {
		return true
	}
	for _, s := range l {
		if s == sub {
			return true
		}
	}
	return false
}

func bgList(b *bgBand, slot string) []int {
	switch slot {
	case "weapon":
		return b.Weapon
	case "armor":
		return b.Armor
	case "shield":
		return b.Shield
	case "garment":
		return b.Garment
	case "shoes":
		return b.Shoes
	case "headgear":
		return b.Headgear
	case "accessory":
		return b.Accessory
	}
	return nil
}

func slotNameOf(loc uint32) string {
	for _, s := range gearSlots {
		if loc&s.Mask != 0 {
			return s.Name
		}
	}
	return ""
}

// loadBuildGearFile re-reads data/buildgear.json when it changed (buildGear held)
func loadBuildGearFile() bool {
	fn := filepath.Join(*dataDir, "buildgear.json")
	st, err := os.Stat(fn)
	if err != nil || !st.ModTime().After(buildGear.fileMod) {
		return false
	}
	raw, err := os.ReadFile(fn)
	if err != nil {
		return false
	}
	var f bgFile
	if json.Unmarshal(raw, &f) != nil {
		return false
	}
	buildGear.file, buildGear.fileMod = &f, st.ModTime()
	return true
}

// buildGearTable: the tables, rebuilt every 5 min or when the guide file changed
func buildGearTable() map[string][]gearBand {
	buildGear.Lock()
	defer buildGear.Unlock()
	changed := loadBuildGearFile()
	if buildGear.table != nil && !changed && time.Since(buildGear.builtAt) < 5*time.Minute {
		return buildGear.table
	}
	lookupOnce.Do(loadLookup)
	world := map[int]bool{}
	var gear, cards []*Item
	for id, it := range items {
		switch {
		case (it.Type == "Weapon" || it.Type == "Armor") && it.Loc != 0:
			if inWorld(id) {
				world[id] = true
				gear = append(gear, it)
			}
		case it.Type == "Card":
			if inWorld(id) {
				cards = append(cards, it)
			}
		}
	}
	sort.Slice(gear, func(i, j int) bool { return gear[i].ID < gear[j].ID })
	sort.Slice(cards, func(i, j int) bool { return cards[i].ID < cards[j].ID })
	table := map[string][]gearBand{}
	member := map[string]map[int]bool{}
	for _, p := range builds {
		style := buildStyle(p)
		k1, k2 := jobKey(p.First), jobKey(p.Class)
		var guide *bgBuild
		if buildGear.file != nil {
			guide = buildGear.file.Builds[p.ID]
			if guide == nil {
				guide = buildGear.file.Builds[p.base()] // a variant without its own table uses its main plan's
			}
		}
		mem := map[int]bool{}
		var bands []gearBand
		for _, be := range bandEdges {
			gb := gearBand{Min: be[0], Max: be[1], Slots: map[string][]gearEntry{}}
			keys := []string{k1}
			if be[0] > 40 {
				keys = []string{k1, k2}
			}
			if be[0] > 60 {
				keys = []string{k2}
			}
			wears := func(it *Item) bool {
				if it.MinLv > be[1] {
					return false
				}
				for _, k := range keys {
					if usableByJob(it, k) {
						return true
					}
				}
				return false
			}
			var gbs []*bgBand
			if guide != nil {
				for i := range guide.Bands {
					g := &guide.Bands[i]
					if g.Min <= be[1] && g.Max >= be[0] {
						gbs = append(gbs, g)
						if g.Note != "" {
							gb.Note = g.Note
						}
					}
				}
			}
			for _, s := range gearSlots {
				if s.Name == "shield" && style == "bow" {
					gb.Slots[s.Name] = nil // bows are two-handed
					continue
				}
				seen := map[int]bool{}
				var list []gearEntry
				add := func(it *Item, src string) {
					if it == nil || seen[it.ID] {
						return
					}
					seen[it.ID] = true
					list = append(list, gearEntry{ID: it.ID, Name: it.Name, Slot: s.Name, MinLv: it.MinLv,
						Price: priceOf(it.ID), Source: src, Score: buildItemScore(style, it)})
				}
				for _, g := range gbs { // the guide's picks, best last -> best first
					l := bgList(g, s.Name)
					for i := len(l) - 1; i >= 0; i-- {
						add(items[l[i]], "guide")
					}
				}
				var route []int
				switch s.Name {
				case "weapon":
					route = p.Weapons
				case "armor":
					route = p.Armors
				default:
					for _, id := range p.Extras {
						if x := items[id]; x != nil && x.Loc&s.Mask != 0 {
							route = append(route, id)
						}
					}
				}
				for i := len(route) - 1; i >= 0; i-- {
					if x := items[route[i]]; x != nil && x.MinLv <= be[1] {
						add(x, "plan")
					}
				}
				var auto []*Item
				for _, it := range gear {
					if it.Loc&s.Mask == 0 || slotNameOf(it.Loc) != s.Name || !wears(it) {
						continue
					}
					if s.Name == "weapon" && !buildWeaponOK(p, it) {
						continue
					}
					auto = append(auto, it)
				}
				sc := func(it *Item) int { return buildItemScore(style, it)*10 + it.Slots*5 + it.MinLv/5 }
				sort.SliceStable(auto, func(i, j int) bool { return sc(auto[i]) > sc(auto[j]) })
				for i := 0; i < len(auto) && i < 6; i++ {
					add(auto[i], "auto")
				}
				for _, e := range list {
					mem[e.ID] = true
				}
				gb.Slots[s.Name] = list
			}
			// cards: the guide's, then the best by the build's stat goals
			seenC := map[int]bool{}
			for _, g := range gbs {
				for _, c := range g.Cards {
					if x := items[c.ID]; x != nil && !seenC[c.ID] {
						seenC[c.ID] = true
						gb.Cards = append(gb.Cards, gearEntry{ID: c.ID, Name: x.Name, Slot: c.Slot, Price: priceOf(c.ID),
							Source: "guide", Score: buildCardValue(p, c.ID), Why: c.Why})
					}
				}
			}
			type cv struct {
				it *Item
				v  int
			}
			var cl []cv
			for _, it := range cards {
				if seenC[it.ID] {
					continue
				}
				if m := cardMob(it.Name); m != nil && (m.Boss || m.Level > be[1]+20) {
					continue // MVP / far-too-strong monsters' cards are not this band's trade goods
				}
				if v := buildCardValue(p, it.ID); v >= 4 {
					cl = append(cl, cv{it, v})
				}
			}
			sort.SliceStable(cl, func(i, j int) bool { return cl[i].v > cl[j].v })
			for i := 0; i < len(cl) && i < 8; i++ {
				gb.Cards = append(gb.Cards, gearEntry{ID: cl[i].it.ID, Name: cl[i].it.Name, Slot: slotNameOf(cl[i].it.Loc),
					Price: priceOf(cl[i].it.ID), Source: "auto", Score: cl[i].v})
			}
			for _, e := range gb.Cards {
				mem[e.ID] = true
			}
			bands = append(bands, gb)
		}
		table[p.ID] = bands
		member[p.ID] = mem
	}
	buildGear.table, buildGear.member, buildGear.builtAt = table, member, time.Now()
	return table
}

// gearBandFor: the band of a build table for a level
func gearBandFor(build string, lv int) *gearBand {
	t := buildGearTable()[build]
	for i := range t {
		if lv >= t[i].Min && lv <= t[i].Max {
			return &t[i]
		}
	}
	return nil
}

// suitsBuild: the item is on the build's table for this level's band or the band below (b.mu not needed)
func suitsBuild(build string, lv, id int) bool {
	t := buildGearTable()[build]
	for i := range t {
		if lv >= t[i].Min-20 && lv <= t[i].Max {
			for _, l := range t[i].Slots {
				for _, e := range l {
					if e.ID == id {
						return true
					}
				}
			}
		}
	}
	return false
}

// buildGearFit: share of online bots (with a build) whose worn weapon / armor is on their table
func buildGearFit() map[string]interface{} {
	bots, _ := allBots.Load().([]*Bot)
	type acc struct{ n, w, a int }
	per := map[string]*acc{}
	all := &acc{}
	var offW, offA []string
	for _, b := range bots {
		b.mu.Lock()
		p := b.plan()
		if !b.Online || p == nil || b.charRole() == "vendor" {
			b.mu.Unlock()
			continue
		}
		lv := b.BaseLvOr()
		var wid, aid int
		if it := b.equipped[0x2]; it != nil {
			wid = it.ItemID
		}
		if it := b.equipped[0x10]; it != nil {
			aid = it.ItemID
		}
		name := b.charName()
		b.mu.Unlock()
		x := per[p.ID]
		if x == nil {
			x = &acc{}
			per[p.ID] = x
		}
		x.n++
		all.n++
		if wid != 0 && suitsBuild(p.ID, lv, wid) {
			x.w++
			all.w++
		} else if len(offW) < 15 {
			offW = append(offW, name+" ("+p.ID+", "+itemName(wid)+")")
		}
		if aid != 0 && suitsBuild(p.ID, lv, aid) {
			x.a++
			all.a++
		} else if len(offA) < 15 {
			offA = append(offA, name+" ("+p.ID+", "+itemName(aid)+")")
		}
	}
	pct := func(a, n int) int {
		if n == 0 {
			return 0
		}
		return a * 100 / n
	}
	byBuild := map[string]interface{}{}
	for id, x := range per {
		byBuild[id] = map[string]int{"bots": x.n, "weapon_fit_pct": pct(x.w, x.n), "armor_fit_pct": pct(x.a, x.n)}
	}
	// suitable goods offered but unsold: #trade spares and shop listings that are on some build's table
	econ.mu.Lock()
	offered := map[int]int{}
	for _, o := range econ.offers {
		offered[o.ItemID] += o.Qty
	}
	for _, sh := range econ.shops {
		for _, l := range sh.items {
			offered[l.ItemID] += l.Count
		}
	}
	econ.mu.Unlock()
	buildGear.Lock()
	suitable, units := 0, 0
	var top []string
	for id, n := range offered {
		for _, m := range buildGear.member {
			if m[id] {
				suitable++
				units += n
				if len(top) < 12 {
					top = append(top, fmt.Sprintf("%s x%d", itemName(id), n))
				}
				break
			}
		}
	}
	buildGear.Unlock()
	return map[string]interface{}{"bots": all.n, "weapon_fit_pct": pct(all.w, all.n), "armor_fit_pct": pct(all.a, all.n),
		"by_build": byBuild, "weapon_off_examples": offW, "armor_off_examples": offA,
		"suitable_items_offered": suitable, "suitable_units_offered": units, "suitable_offered_examples": top}
}

func registerBuildGear(mux *http.ServeMux) {
	mux.HandleFunc("/api/buildgear", func(w http.ResponseWriter, r *http.Request) {
		t := buildGearTable()
		id := r.URL.Query().Get("build")
		if id == "" {
			ids := make([]string, 0, len(t))
			for k := range t {
				ids = append(ids, k)
			}
			sort.Strings(ids)
			buildGear.Lock()
			guide := buildGear.file != nil
			buildGear.Unlock()
			writeJSON(w, map[string]interface{}{"builds": ids, "guide_file_loaded": guide})
			return
		}
		writeJSON(w, map[string]interface{}{"build": id, "bands": t[id]})
	})
	mux.HandleFunc("/api/buildgear/fit", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, buildGearFit())
	})
	registerBuildNext(mux) // step 2: each bot's next piece
	registerLFG(mux)       // lfg.go: farm goals, LFG/LFM, need-driven parties
}

// buildsWanting: "hunters/bards" - up to two builds whose table lists the item (no rebuild: safe under econ.mu)
func buildsWanting(id int) string {
	buildGear.Lock()
	defer buildGear.Unlock()
	var out []string
	for _, p := range buildOrder() {
		if buildGear.member[p][id] {
			out = append(out, p+"s")
			if len(out) == 2 {
				break
			}
		}
	}
	if len(out) == 0 || len(out) == 2 && len(buildGear.member) > 0 && countWanting(id) > 4 {
		return "" // half the server wants it (a plain armor): nothing to tell
	}
	return strings.Join(out, "/")
}

func countWanting(id int) int {
	n := 0
	for _, m := range buildGear.member {
		if m[id] {
			n++
		}
	}
	return n
}

func buildOrder() []string {
	ids := make([]string, 0, len(builds))
	for id := range builds {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// ---------------------------------------------------------------- step 2: the next piece of a bot's build
//
// Owner 2026-11-14: "Each bot should prioritize farming or trading to perfect its build." nextBuildPiece is the one
// item a bot works toward next: the best real upgrade on its build's table (guide picks first, then the plan routes and
// the auto list) for its level band that it can wear now and doesn't own. Weapon gains count triple, body armor
// double (the pieces that make a build). For a drop-only piece it lists where to farm it (normal monsters near the
// bot's level, ranked by drop rate x spawn count); for a shop piece the zeny is the goal. The economy side (farm.go,
// wants) decides when a bot farms there, buys, or asks in #trade: GET /api/buildgear/next[?bot=Name].

type buildPiece struct {
	ItemID int         `json:"item_id"`
	Name   string      `json:"name"`
	Slot   string      `json:"slot"`
	Price  int         `json:"price"`  // market price (priceOf)
	NPC    int         `json:"npc"`    // NPC price when a shop sells it (0 = drop/trade only)
	Source string      `json:"source"` // "shop" / "drop" / "trade" (neither: only players have it)
	Gain   int         `json:"gain"`   // itemScore gain over what it wears / holds
	Farm   []pieceFarm `json:"farm,omitempty"`
}

type pieceFarm struct {
	Map   string  `json:"map"`
	Mob   string  `json:"mob"`
	MobLv int     `json:"mob_lv"`
	Rate  float64 `json:"rate_pct"`
	Score float64 `json:"score"` // rate x spawn count on the map
}

// nextBuildPiece (b.mu held): the next item for this bot's build, nil when its band is complete (or no plan).
func (b *Bot) nextBuildPiece() *buildPiece {
	p := b.plan()
	if p == nil || !b.invReady() {
		return nil
	}
	lv := b.BaseLvOr()
	gb := gearBandFor(p.ID, lv)
	if gb == nil {
		return nil
	}
	// candidates: every real upgrade; the pick is the NEXT STEP, not the dream item (first deploy: 235 of 281 bots
	// were pointed at drop-less end-game pieces like Hunter Bow / Grimtooth): pieces the bot can act on come first
	// (an NPC sells it, a monster near its level drops it, or a player offers it now), then the cheapest one whose
	// weighted gain is at least a third of the best - a 2,000z step before a 60,000z one.
	type cand struct {
		bp     *buildPiece
		w      int
		cost   int
		obtain bool
	}
	var cands []cand
	twoHand := false
	if w := b.equipped[0x2]; w != nil {
		if x := items[w.ItemID]; x != nil && x.Loc == 0x22 {
			twoHand = true
		}
	}
	for _, s := range gearSlots {
		for _, e := range gb.Slots[s.Name] {
			it := items[e.ID]
			if it == nil || !b.canEquip(it) || b.gearFailed[e.ID] || b.ownsUnequipped(e.ID) {
				continue
			}
			slot := gearSlot(it.Loc)
			if slot == 0x20 && twoHand {
				continue
			}
			if cur := b.equipped[slot]; cur != nil && cur.ItemID == e.ID {
				continue
			}
			cur := max(b.itemScore(b.curIn(slot)), b.bestOwnedScore(slot))
			if !b.realUpgrade(it, b.curIn(slot)) || b.itemScore(it) <= cur {
				continue
			}
			gain := b.itemScore(it) - cur
			w := gain
			switch slot {
			case 0x2:
				w *= 3
			case 0x10:
				w *= 2
			}
			bp := &buildPiece{ItemID: e.ID, Name: it.Name, Slot: s.Name, Price: priceOf(e.ID), Gain: gain}
			c := cand{bp: bp, w: w, cost: bp.Price}
			if len(gearShops(e.ID)) > 0 {
				bp.NPC, bp.Source, c.cost, c.obtain = it.Buy, "shop", it.Buy, true
			} else if f := pieceFarms(e.ID, lv, 3); len(f) > 0 {
				bp.Farm, bp.Source, c.obtain = f, "drop", true
			} else {
				bp.Source = "trade"
				c.obtain = onBoard(e.ID)
			}
			cands = append(cands, c)
		}
	}
	if len(cands) == 0 {
		return nil
	}
	pool := cands[:0:0]
	for _, c := range cands {
		if c.obtain {
			pool = append(pool, c)
		}
	}
	if len(pool) == 0 {
		pool = cands
	}
	maxW := 0
	for _, c := range pool {
		maxW = max(maxW, c.w)
	}
	var best *cand
	for i := range pool {
		c := &pool[i]
		if c.w*3 < maxW {
			continue
		}
		if best == nil || c.cost < best.cost || c.cost == best.cost && c.w > best.w {
			best = c
		}
	}
	return best.bp
}

// pieceFarms: up to n maps where a normal monster within the level's reach (mob level <= lv+10) drops the item,
// best first by drop rate x spawn count.
func pieceFarms(id, lv, n int) []pieceFarm {
	lookupOnce.Do(loadLookup)
	var out []pieceFarm
	for _, mid := range droppedBy[id] {
		mb, ex := mobs[mid], mobExtras[mid]
		if mb == nil || ex == nil || mb.Boss || ex.Mvp || mb.Level > lv+10 {
			continue
		}
		rate := 0
		for _, d := range ex.Drops {
			if d.Item == id && !d.Mvp {
				rate = max(rate, effDropRate(d.Item, d.Rate, mb.Boss)) // droprate.go
			}
		}
		if rate <= 0 {
			continue
		}
		for _, sp := range mobMaps[mid] {
			if sp.Count <= 0 || spawnKind[sp.Map] == "town" {
				continue
			}
			out = append(out, pieceFarm{Map: sp.Map, Mob: mb.Name, MobLv: mb.Level, Rate: float64(rate) / 100,
				Score: float64(rate) / 100 * float64(sp.Count)})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	if len(out) > n {
		out = out[:n]
	}
	return out
}

// registerBuildNext: GET /api/buildgear/next (every online bot's next piece + a summary by source) or ?bot=Name.
func registerBuildNext(mux *http.ServeMux) {
	mux.HandleFunc("/api/buildgear/next", func(w http.ResponseWriter, r *http.Request) {
		want := strings.ToLower(r.URL.Query().Get("bot"))
		bots, _ := allBots.Load().([]*Bot)
		type row struct {
			Bot    string      `json:"bot"`
			Plan   string      `json:"plan"`
			Lv     int         `json:"lv"`
			Zeny   int         `json:"zeny"`
			Next   *buildPiece `json:"next"`
			Afford bool        `json:"can_afford"`
		}
		var rows []row
		bySrc := map[string]int{}
		done := 0
		for _, b := range bots {
			b.mu.Lock()
			if !b.Online || b.plan() == nil || (want != "" && strings.ToLower(b.charName()) != want) {
				b.mu.Unlock()
				continue
			}
			np := b.nextBuildPiece()
			rw := row{Bot: b.charName(), Plan: b.plan().ID, Lv: b.BaseLvOr(), Zeny: b.Zeny, Next: np}
			if np != nil {
				price := np.Price
				if np.NPC > 0 {
					price = np.NPC
				}
				rw.Afford = price > 0 && b.Zeny-b.gearReserve() >= price
				bySrc[np.Source]++
			} else {
				done++
			}
			b.mu.Unlock()
			rows = append(rows, rw)
		}
		sort.Slice(rows, func(i, j int) bool { return rows[i].Bot < rows[j].Bot })
		writeJSON(w, map[string]interface{}{"bots": len(rows), "band_complete": done, "next_by_source": bySrc, "rows": rows})
	})
}
