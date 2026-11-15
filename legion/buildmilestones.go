package main

// buildmilestones.go (EC39, owner 2026-11-15: "list the stats, gears, skills at certain level range for bots to
// achieve ... Farm certain maps unless gear requirements are met"). Every build has milestones per level band (the
// bands of buildgear.go: 1-20, 21-40, 41-60, 61-80, 81-99): the weapon attack (MATK for casters) and the body armor
// DEF a bot should wear by the band's first level, plus the cards that band asks for. The source is the guide's band
// lists in data/buildgear.json, with the build's own routes (builds.go Weapons / Armors) when the guide lists nothing
// reachable. An item counts only once the character reaches its MinLv; a band with nothing reachable has no milestone.
//
// Gate (no cliff): the worn weapon and armor give a gear level each - the highest level the value is adequate for,
// interpolated linearly between the milestone points (a value at 85% of a milestone counts as meeting it). The gear
// level is the lower of the two. The gated level is that gear level plus one band of tolerance (20), kept between
// base-15 (the most a bot is ever dropped) and base. So a bot one band behind its gear is not gated at all, and the
// drop grows smoothly for bots further behind. The fair gate (fairgate.go) uses base-15 as its line too, so the two
// never fight. Cards do not count toward the gate: they stay a gear goal in the table.
//
// The gear goal is the piece that fails in the slot that is lower (weapon or armor): the first milestone item the
// bot does not yet meet. The picker (ai.go chooseHuntingGround) farms for it: a shop piece is a zeny goal on a map fit
// for the gated level, a drop piece is farmed where it drops. The gate is memoized per bot and refreshed when the
// worn gear changes (Bot.gateMemo). Tuning gear_gate_on, default ON. The table is served at /api/buildmilestones.

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	milestoneTolPct = 85 // a worn piece meets a milestone at this percentage of its value
	gateTolerance   = 20 // gear level + one band before a bot is gated
	gateMaxDrop     = 15 // never gated more than this far below base level (fairgate.go's line)
)

type gearMilestone struct {
	Min, Max   int  // level band
	Magic      bool // caster build: the weapon's MATK is the value
	Weapon     int  // weapon attack (MATK for casters) to hold, raw item value
	Armor      int  // body armor DEF to wear, raw item value
	Cards      int  // cards the band asks for (goal only, not the gate): 0 for 1-40, 1 for 41-60, 2 from 61
	WeaponItem int  // item id the weapon milestone reads (0 = nothing reachable)
	ArmorItem  int  // item id the armor milestone reads (0 = nothing reachable)
}

// gearGoal: the piece that fails the gate: its item id and the slot it sits in ("weapon" or "armor").
type gearGoal struct {
	Item int
	Slot string
}

func gearGateOn() bool { return tune().GearGateOn }

var milestoneCache = struct {
	sync.Mutex
	m       map[string][]gearMilestone
	checked time.Time
	mod     time.Time
}{m: map[string][]gearMilestone{}}

// routePickAt: the item of a route that a character reaches by lv (MinLv <= lv) with the highest value; nil when
// none is reached (a band whose only listed items are too high has no milestone: an early band never asks for
// end-game armor).
func routePickAt(route []int, lv int, value func(*Item) int) *Item {
	var best *Item
	for _, id := range route {
		x := items[id]
		if x == nil || x.MinLv > lv {
			continue
		}
		if best == nil || value(x) > value(best) {
			best = x
		}
	}
	return best
}

func cardsForBand(min int) int {
	switch {
	case min >= 61:
		return 2
	case min >= 41:
		return 1
	}
	return 0
}

// milestonesFor: the milestones of one build, one per band (not cached: see milestonesOf). guide = the build's entry
// in data/buildgear.json (nil when there is none).
func milestonesFor(p *Build, guide *bgBuild) []gearMilestone {
	lookupOnce.Do(loadLookup)
	magic := buildStyle(p) == "magic"
	wval := func(x *Item) int {
		if magic {
			return x.Matk
		}
		return x.Atk
	}
	var out []gearMilestone
	for _, be := range bandEdges {
		m := gearMilestone{Min: be[0], Max: be[1], Magic: magic, Cards: cardsForBand(be[0])}
		var gw, ga []int
		if guide != nil {
			for i := range guide.Bands {
				g := &guide.Bands[i]
				if g.Min <= be[1] && g.Max >= be[0] {
					gw, ga = append(gw, g.Weapon...), append(ga, g.Armor...)
				}
			}
		}
		wroute, aroute := p.Weapons, p.Armors
		if w := routePickAt(gw, be[0], wval); w != nil {
			m.WeaponItem, m.Weapon = w.ID, wval(w)
		} else if w := routePickAt(wroute, be[0], wval); w != nil {
			m.WeaponItem, m.Weapon = w.ID, wval(w)
		}
		if a := routePickAt(ga, be[0], func(x *Item) int { return x.Def }); a != nil {
			m.ArmorItem, m.Armor = a.ID, a.Def
		} else if a := routePickAt(aroute, be[0], func(x *Item) int { return x.Def }); a != nil {
			m.ArmorItem, m.Armor = a.ID, a.Def
		}
		out = append(out, m)
	}
	return out
}

// milestonesOf: the cached milestones of a build. The guide file is re-read at most every 5 min; a changed file drops
// the cache.
func milestonesOf(p *Build) []gearMilestone {
	buildGear.Lock()
	changed := false
	if time.Since(milestoneCache.checked) > 5*time.Minute {
		milestoneCache.checked = time.Now()
		changed = loadBuildGearFile()
	}
	var guide *bgBuild
	if buildGear.file != nil {
		guide = buildGear.file.Builds[p.ID]
		if guide == nil {
			guide = buildGear.file.Builds[p.base()]
		}
	}
	mod := buildGear.fileMod
	buildGear.Unlock()

	milestoneCache.Lock()
	defer milestoneCache.Unlock()
	if changed || !mod.Equal(milestoneCache.mod) {
		milestoneCache.m = map[string][]gearMilestone{}
		milestoneCache.mod = mod
	}
	if ms, ok := milestoneCache.m[p.ID]; ok {
		return ms
	}
	ms := milestonesFor(p, guide)
	milestoneCache.m[p.ID] = ms
	return ms
}

// milestoneTable: every build's milestones (build id -> bands): the one table the engine and the Builds tab read.
func milestoneTable() map[string][]gearMilestone {
	out := map[string][]gearMilestone{}
	for id, p := range builds {
		out[id] = milestonesOf(p)
	}
	return out
}

// registerBuildMilestones: GET /api/buildmilestones - the milestone table (build id -> bands) - and the file writer.
func registerBuildMilestones(mux *http.ServeMux) {
	mux.HandleFunc("/api/buildmilestones", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(milestoneTable())
	})
	go milestoneFileLoop()
}

// milestoneFileLoop: writes data/buildmilestones_gear.json now and every 5 min (the Builds tab's format:
// {"builds": {id: {"ranges": [{"min", "max", "slots": {slot: [item ids]}, "weapon_atk", "armor_def", "cards", "magic"}]}}}).
// Runs only from the server (registerBuildMilestones), never from tests.
func milestoneFileLoop() {
	for {
		if err := writeMilestoneFile(filepath.Join(*dataDir, "buildmilestones_gear.json")); err != nil {
			log.Printf("buildmilestones_gear.json: %v", err)
		}
		time.Sleep(5 * time.Minute)
	}
}

type milestoneRangeJSON struct {
	Min       int              `json:"min"`
	Max       int              `json:"max"`
	Slots     map[string][]int `json:"slots"`
	WeaponATK int              `json:"weapon_atk"`
	ArmorDEF  int              `json:"armor_def"`
	Cards     int              `json:"cards"`
	Magic     bool             `json:"magic"`
}

// milestoneFileJSON: the milestone table in the dashboard's format.
func milestoneFileJSON() map[string]interface{} {
	builds := map[string]interface{}{}
	for id, ms := range milestoneTable() {
		var ranges []milestoneRangeJSON
		for _, m := range ms {
			r := milestoneRangeJSON{Min: m.Min, Max: m.Max, WeaponATK: m.Weapon, ArmorDEF: m.Armor, Cards: m.Cards, Magic: m.Magic,
				Slots: map[string][]int{"weapon": {}, "armor": {}}}
			if m.WeaponItem > 0 {
				r.Slots["weapon"] = []int{m.WeaponItem}
			}
			if m.ArmorItem > 0 {
				r.Slots["armor"] = []int{m.ArmorItem}
			}
			ranges = append(ranges, r)
		}
		builds[id] = map[string]interface{}{"ranges": ranges}
	}
	return map[string]interface{}{"builds": builds}
}

// writeMilestoneFile: atomic write (temp file, then rename).
func writeMilestoneFile(path string) error {
	raw, err := json.MarshalIndent(milestoneFileJSON(), "", " ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// gearLevelOf: the highest level a worn value is adequate for. pts = milestone points (level, required value), level
// ascending. The value counts at milestoneTolPct of a requirement; between two points the level is interpolated.
// Below the first point the level is the first point's level; past the last point it is 99.
func gearLevelOf(pts [][2]float64, have float64) float64 {
	if len(pts) == 0 {
		return 99
	}
	v := have * 100 / milestoneTolPct
	if v < pts[0][1] {
		return pts[0][0]
	}
	for j := 0; j+1 < len(pts); j++ {
		if v < pts[j+1][1] {
			f := (v - pts[j][1]) / (pts[j+1][1] - pts[j][1])
			return pts[j][0] + f*(pts[j+1][0]-pts[j][0])
		}
	}
	return 99
}

// wornWeapon / wornBody: the worn weapon's value (ATK plus refine, or MATK for casters) and the body armor's DEF.
func wornWeapon(b *Bot, magic bool) float64 {
	e := b.equipped[0x2]
	if e == nil || items[e.ItemID] == nil {
		return 0
	}
	x := items[e.ItemID]
	if magic {
		return float64(x.Matk)
	}
	return float64(x.Atk + refineAtk(max(1, x.WLv), e.Refine))
}

func wornBody(b *Bot) float64 {
	e := b.equipped[0x10]
	if e == nil || items[e.ItemID] == nil {
		return 0
	}
	return float64(items[e.ItemID].Def + e.Refine)
}

// gearGateOf (b.mu held): the level the bot's gear allows as a gate on its effective level, and the piece that fails
// (nil when nothing fails). 0 = no gate (flag off, no build, band 1-20, or the gear is adequate for the level).
func gearGateOf(b *Bot, p *Build, lv int) (int, *gearGoal) {
	if p == nil || lv < 21 || !gearGateOn() {
		return 0, nil
	}
	ms := milestonesOf(p)
	magic := len(ms) > 0 && ms[0].Magic
	var wp, ap [][2]float64
	for i := range ms {
		m := &ms[i]
		if m.Weapon > 0 && m.WeaponItem > 0 {
			wp = append(wp, [2]float64{float64(m.Min), float64(m.Weapon)})
		}
		if m.Armor > 0 && m.ArmorItem > 0 {
			ap = append(ap, [2]float64{float64(m.Min), float64(m.Armor)})
		}
	}
	lw := gearLevelOf(wp, wornWeapon(b, magic))
	la := gearLevelOf(ap, wornBody(b))
	gl := min(lw, la)
	gate := max(lv-gateMaxDrop, int(gl)+gateTolerance)
	if gate >= lv {
		return 0, nil
	}
	gate = max(gate, 1)
	// the failing piece: the lower slot's first unmet milestone item
	weapon := lw <= la
	for i := range ms {
		m := &ms[i]
		if weapon && m.Weapon > 0 && m.WeaponItem > 0 && wornWeapon(b, magic)*100 < float64(m.Weapon)*milestoneTolPct {
			return gate, &gearGoal{Item: m.WeaponItem, Slot: "weapon"}
		}
		if !weapon && m.Armor > 0 && m.ArmorItem > 0 && wornBody(b)*100 < float64(m.Armor)*milestoneTolPct {
			return gate, &gearGoal{Item: m.ArmorItem, Slot: "armor"}
		}
	}
	return gate, nil
}

// gearSig: a signature of the worn gear (items, refines, cards), so the gate memo refreshes on an equip change.
func (b *Bot) gearSig() uint64 {
	var s uint64
	for slot, e := range b.equipped {
		if e == nil {
			continue
		}
		h := uint64(slot)*2654435761 + uint64(e.ItemID)*40503 + uint64(e.Refine)*9973
		for i, c := range e.Cards {
			h += uint64(c) * uint64(131+i*17)
		}
		s += h
	}
	return s
}

// gateMemo: the last gate of this bot, valid while the gear, the base level and the build are the same.
type gateMemo struct {
	ok    bool
	sig   uint64
	lv    int
	build string
	gate  int
	goal  *gearGoal
}

// gearGateFor (b.mu held): the memoized gate and goal of build p at base level lv.
func (b *Bot) gearGateFor(p *Build, lv int) (int, *gearGoal) {
	if p == nil {
		return 0, nil
	}
	sig := b.gearSig()
	m := &b.gateMemo
	if !m.ok || m.sig != sig || m.lv != lv || m.build != p.ID {
		g, goal := gearGateOf(b, p, lv)
		*m = gateMemo{ok: true, sig: sig, lv: lv, build: p.ID, gate: g, goal: goal}
	}
	return m.gate, m.goal
}

// gearGatedLv (b.mu held): the raw effective level, capped at the gear gate of build p (EC39).
func (b *Bot) gearGatedLv(p *Build, lv, raw int) int {
	if g, _ := b.gearGateFor(p, lv); g > 0 && raw > g {
		return g
	}
	return raw
}

// gearGate (b.mu held): the gate and the failing piece of this bot's own build.
func (b *Bot) gearGate(lv int) (int, *gearGoal) {
	return b.gearGateFor(b.gearPlan(), lv)
}

// gearPlan (b.mu held): the bot's build, nil when it has no character spec yet (tests, a bot not set up).
func (b *Bot) gearPlan() *Build {
	if b.cur == nil && (b.P == nil || len(b.P.Chars) == 0) {
		return nil
	}
	return b.plan()
}

// gearGoalPiece (b.mu held): the failing piece as a build piece for the picker: a shop piece (zeny goal, the NPC
// price), else a drop piece with its farm maps, else a trade piece.
func gearGoalPiece(g gearGoal, lv int) *buildPiece {
	it := items[g.Item]
	if it == nil {
		return nil
	}
	bp := &buildPiece{ItemID: g.Item, Name: it.Name, Slot: g.Slot, Price: priceOf(g.Item)}
	switch {
	case len(gearShops(g.Item)) > 0:
		bp.NPC, bp.Source = it.Buy, "shop"
	case len(pieceFarms(g.Item, lv, 3)) > 0:
		bp.Farm, bp.Source = pieceFarms(g.Item, lv, 3), "drop"
	default:
		bp.Source = "trade"
	}
	return bp
}

// partyGearGate: the lowest gear gate of the online, known members (0 = none). A party's map follows its
// weakest-geared member (EC39); the gates come from the published Gate (party.go publish).
func partyGearGate(ms []*Bot) int {
	g := 0
	for _, b := range ms {
		s, ok := peek(b.P.Name)
		if !ok || !s.Online || !s.Known || s.Gate <= 0 {
			continue
		}
		if g == 0 || s.Gate < g {
			g = s.Gate
		}
	}
	return g
}
