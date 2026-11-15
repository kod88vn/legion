package main

// Goal turns for a human-led party (Ayla's; core instance). Owner 2026-11-14: Ayla's party is not exempt from the
// need rule - "they should still have need base rule, this party is an example for us to find bug, but they function
// like other parties", "can move if it makes sense for party to farm other gears ... if it's someone else's turn to
// farm gears, then go" - and "the party is one mind for gear" (equal time for each member's needs).
//
// The party stays together as a party (the owner's test party); what changes is the map choice: the party farms for
// one member's goal at a time, in turns.
//   - Members' goals: the bots' farm goals (lfg.go goalTick; in a human party the healer gets a build goal too, not
//     "heal"); the human's goal is set by its mind (POST /api/party-turn {"human","item","map"} - the engine can't see
//     her inventory) and marked met by it ({"human","met":true}).
//   - A turn belongs to a member whose goal has a farm map (gear / card drop map, safe for the party's bots). It lasts
//     until that goal is met or turn_min (45) minutes, then the next member in order with a mappable goal. Members
//     with a zeny/level goal are served by any map (no turn of their own needed).
//   - The engine does not move the human: GET /api/party-turn tells the human's mind/plugin whose turn it is, which
//     map and why; the mind sends the party there (ayla.sh map NAME). The friends announce turn changes and met goals
//     in party chat ("got my Wand! next is ...").
//   - State is saved in data/party-turns.json (core instance), so a core restart keeps the order and the human's goal.
// The main instance forwards /api/party-turn to the core instance (instances.go coreForward).

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// owner rule 2026-11-14 (RULES.md 3): a party picks one hunting map and stays ~2 hours - a turn lasts 2 h unless its
// goal is met
const humanTurnLen = 2 * time.Hour

// humanGoal: the human's own goal, as its mind set it
type humanGoal struct {
	Kind   string    `json:"kind"` // card / gear / zeny / level
	Item   string    `json:"item,omitempty"`
	ItemID int       `json:"item_id,omitempty"`
	Map    string    `json:"map,omitempty"`
	Why    string    `json:"why,omitempty"`
	Set    time.Time `json:"set"`
	Met    time.Time `json:"met,omitempty"`
	Auto   bool      `json:"auto,omitempty"` // picked by the engine from the human's build gear plan (a mind's goal wins)
	How    string    `json:"how,omitempty"`  // buy / farm / zeny
	Price  int       `json:"price,omitempty"`
}

type turnState struct {
	Human   string               `json:"human"`
	Goal    *humanGoal           `json:"human_goal,omitempty"`
	Turn    string               `json:"turn"` // member whose goal the party farms ("" = nobody has a mappable goal)
	Kind    string               `json:"kind,omitempty"`
	Item    string               `json:"item,omitempty"`
	Map     string               `json:"map,omitempty"`
	Why     string               `json:"why,omitempty"`
	Since   time.Time            `json:"since"`
	Until   time.Time            `json:"until"`
	Order   []string             `json:"order"`
	Best    string               `json:"best_map,omitempty"` // the best map for the party's level and gear (exp + zeny)
	Plan    string               `json:"plan,omitempty"`     // the human's build plan (buildgear.json), default assassin
	Owned   []string             `json:"owned,omitempty"`    // the human's gear (the engine can't see her inventory)
	Served  map[string]float64   `json:"served_min"`         // minutes of turns each member had
	Last    map[string]time.Time `json:"last_turn"`
	History []string             `json:"history,omitempty"`
}

var turns = struct {
	sync.Mutex
	m map[string]*turnState // human (lower case) -> state
}{m: map[string]*turnState{}}

func turnFile() string { return filepath.Join(*dataDir, "party-turns.json") }

func saveTurns() { // turns.Mutex held
	b, err := json.MarshalIndent(turns.m, "", " ")
	if err == nil {
		writeFileAtomic(turnFile(), b)
	}
}

func loadTurns() {
	b, err := os.ReadFile(turnFile())
	if err != nil {
		return
	}
	m := map[string]*turnState{}
	if json.Unmarshal(b, &m) == nil {
		turns.Lock()
		turns.m = m
		turns.Unlock()
	}
}

// bestFarmMap: the best map for the group's level and gear (owner rule 2026-11-14 "Farming should target the best
// map for the party level and equipments"): a guide spot that suits the weakest member (partyMapOK: safe, not too
// easy), gives real exp (not mapExpPoor), has room, near the first member; scored by its level band (exp) plus the
// zeny an hour it pays (MapZenyHour) minus distance. "" when nothing fits. extraLv: a human member's level the bots
// can't see (her base level counts for exp-poor).
func bestFarmMap(ms []*Bot, extraLv int, zeny bool) string {
	if len(ms) == 0 {
		return ""
	}
	minBase := partyMinBase(ms)
	if extraLv > 0 {
		minBase = min(minBase, extraLv)
	}
	minLv, _, known, _ := partyFitLv(ms)
	if known == 0 {
		return ""
	}
	s0, _ := peek(ms[0].P.Name)
	fam := family(ms[0].Class)
	best, bs := "", -1e18
	for _, g := range guideSpots {
		if maps[g.Map] == nil || mapExpPoor(g.Map, minBase) || !partyMapOK(ms, g.Map) || !mapHasRoom(g.Map, len(ms)+1) {
			continue
		}
		h := 0
		if s0.Map != "" && s0.Map != g.Map {
			h = mapHops(s0.Map, s0.X, s0.Y, g.Map)
			if h > 10 {
				continue
			}
		}
		sc := float64(g.Min)*1000 + MapZenyHour(g.Map, minLv, fam)/10 - float64(h)*1500
		if zeny { // saving for gear: zeny an hour first, the level band second
			sc = MapZenyHour(g.Map, minLv, fam) + float64(g.Min)*50 - float64(h)*1500
		}
		if sc > bs {
			best, bs = g.Map, sc
		}
	}
	return best
}

// ownedNames: item name -> id resolved once (findItems scans the whole item db; humanGearGoal runs every 20 s under
// turns.Mutex)
var ownedNames sync.Map

func ownedItemID(name string) int {
	k := strings.ToLower(name)
	if v, ok := ownedNames.Load(k); ok {
		return v.(int)
	}
	id := 0
	if hits := findItems(name, 1); len(hits) > 0 {
		id = hits[0].ID
	}
	ownedNames.Store(k, id)
	return id
}

// humanGearGoal (turns.Mutex held): the human's next gear piece from its build's gear table (buildgear.json, its
// level band): per slot (weapon, armor, garment, shoes, headgear, shield, accessory) the cheapest entry that beats
// what it owns (st.Owned, kept by its mind; "met" adds the item). How: buy when its zeny + the friends' spare zeny
// covers the price, farm when it drops on a map fit for the party's level, else farm zeny.
func humanGearGoal(st *turnState, human string, ms []*Bot, now time.Time) *humanGoal {
	hs, ok := humanStatusOf(human)
	if !ok || hs.BaseLevel <= 0 {
		return nil
	}
	plan := st.Plan
	if plan == "" {
		plan = "assassin" // Ayla: Crit Katar Assassin (HANDOVER "Ayla's gear plan")
	}
	gb := gearBandFor(plan, hs.BaseLevel)
	if gb == nil {
		return nil
	}
	// what she owns, scored for her build style per slot (owned names resolved through the item db, so an owned item
	// missing from this band's table - Jur [2] at lv80 - still counts: the plan offered her a Knife, 2026-11-14)
	style := "melee"
	if bp := builds[plan]; bp != nil {
		style = buildStyle(bp)
	}
	owned := map[string]bool{}
	bestOwned := map[string]int{}
	for _, o := range st.Owned {
		owned[strings.ToLower(o)] = true
		if id := ownedItemID(o); id > 0 {
			if it := items[id]; it != nil {
				sl := slotNameOf(it.Loc)
				bestOwned[sl] = max(bestOwned[sl], buildItemScore(style, it))
			}
		}
	}
	var pick *gearEntry
	for _, slot := range []string{"weapon", "armor", "garment", "shoes", "headgear", "shield", "accessory"} {
		for i := range gb.Slots[slot] {
			e := &gb.Slots[slot][i]
			it := items[e.ID]
			if it == nil || owned[strings.ToLower(e.Name)] || e.MinLv > hs.BaseLevel {
				continue
			}
			if sc := buildItemScore(style, it); sc <= bestOwned[slotNameOf(it.Loc)] {
				continue // not a real upgrade over what she owns
			}
			if pick == nil || (e.Slot == pick.Slot && e.Price < pick.Price) {
				pick = e
			}
		}
		if pick != nil {
			break
		}
	}
	if pick == nil {
		return nil
	}
	price := priceOf(pick.ID)
	if len(gearShops(pick.ID)) > 0 && items[pick.ID] != nil && items[pick.ID].Buy > 0 {
		price = items[pick.ID].Buy
	}
	pool := hs.Zeny
	for _, b := range ms {
		b.mu.Lock()
		pool += b.spareZeny()
		b.mu.Unlock()
	}
	g := &humanGoal{Kind: "gear", Item: pick.Name, ItemID: pick.ID, Auto: true, Price: price, Set: now,
		Why: fmt.Sprintf("%s's next %s from the %s gear plan", human, pick.Slot, plan)}
	if st.Goal != nil && st.Goal.Auto && st.Goal.ItemID == pick.ID {
		g.Set = st.Goal.Set
	}
	switch {
	case price > 0 && pool >= price:
		g.How = "buy"
	default:
		for _, f := range pieceFarms(pick.ID, hs.BaseLevel, 5) {
			if !goalMobLow(f.MobLv, hs.BaseLevel) && (len(ms) == 0 || goalMapSafeItem(ms, f.Map, pick.ID)) && !mapExpPoor(f.Map, hs.BaseLevel) {
				g.How, g.Map = "farm", f.Map
				break
			}
		}
		if g.How == "" {
			g.How, g.Kind = "zeny", "zeny"
		}
	}
	return g
}

// memberGear: one member's gear status for /api/party-turn
type memberGear struct {
	Name   string `json:"name"`
	Next   string `json:"next,omitempty"`
	Price  int    `json:"price,omitempty"`
	Source string `json:"source,omitempty"` // shop / drop / trade
	How    string `json:"how"`              // buy / farm / zeny / done
	Map    string `json:"map,omitempty"`
	Have   int    `json:"have"`   // its own spare zeny
	Pooled int    `json:"pooled"` // the friends' spare zeny on top
}

// memberTurnGoal: one member's goal as a turn candidate (map "" = no turn needed)
type memberTurnGoal struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
	Item string `json:"item,omitempty"`
	Map  string `json:"map,omitempty"`
	b    *Bot
	g    *farmGoal
}

// humanTurnTick: advances every human party's turn (core instance, every 20 s with goal parties on).
func humanTurnTick(now time.Time) {
	parties.Lock()
	type hp struct {
		p     *Party
		human string
		ms    []*Bot
	}
	var hps []hp
	for _, p := range parties.byName {
		if p.Human != "" {
			hps = append(hps, hp{p, p.Human, append([]*Bot(nil), p.Members...)})
		}
	}
	parties.Unlock()
	for _, h := range hps {
		humanTurnStep(h.p, h.human, h.ms, now)
	}
}

func (st *turnState) note(now time.Time, s string) {
	st.History = append(st.History, now.UTC().Format("15:04")+" "+s)
	if len(st.History) > 20 {
		st.History = st.History[len(st.History)-20:]
	}
}

func humanTurnStep(p *Party, human string, ms []*Bot, now time.Time) {
	var cands []memberTurnGoal
	for _, b := range ms {
		b.mu.Lock()
		g := b.fgoal
		name := b.charName()
		on := b.Online
		b.mu.Unlock()
		c := memberTurnGoal{Name: name, b: b, g: g}
		if g != nil {
			c.Kind, c.Item = g.Kind, g.Item
			if g.mapped() && on { // a drop for its build, or a sellable item for its zeny goal
				for _, m := range g.Maps {
					if goalMapSafeItem(ms, m, g.farmItem()) {
						c.Map = m
						break
					}
				}
			}
		}
		cands = append(cands, c)
	}
	key := strings.ToLower(human)
	turns.Lock()
	defer turns.Unlock()
	st := turns.m[key]
	if st == nil {
		st = &turnState{Human: human, Served: map[string]float64{}, Last: map[string]time.Time{}}
		turns.m[key] = st
	}
	if st.Served == nil {
		st.Served = map[string]float64{}
	}
	if st.Last == nil {
		st.Last = map[string]time.Time{}
	}
	// the human's own gear goal from its build's gear plan (owner rule: every member gears up first, Ayla too); a
	// goal its mind set wins until met
	// a goal the mind set that can't be farmed by the party (its map exp-poor / unsafe for the party: Soldier Skeleton
	// Card on pay_dun01 for lv72-78) gives way to the gear plan too (owner: gear first, the best map for the level)
	mindGoalDead := false
	if g := st.Goal; g != nil && !g.Auto && g.Met.IsZero() && g.Map != "" {
		hl := 1
		if hs, ok := humanStatusOf(human); ok {
			hl = max(hs.BaseLevel, 1)
		}
		mindGoalDead = mapExpPoor(g.Map, min(partyMinBase(ms), hl)) || (len(ms) > 0 && !goalMapSafeItem(ms, g.Map, g.ItemID))
	}
	if st.Goal == nil || st.Goal.Auto || !st.Goal.Met.IsZero() || mindGoalDead {
		if g := humanGearGoal(st, human, ms, now); g != nil {
			if mindGoalDead {
				st.note(now, fmt.Sprintf("%s's goal %s @ %s doesn't suit the party's level: back to the gear plan", human, st.Goal.Item, st.Goal.Map))
			}
			st.Goal = g
		} else if st.Goal != nil && st.Goal.Auto {
			st.Goal = nil
		}
	}
	hg := st.Goal
	all := []memberTurnGoal{{Name: human}}
	hlv := 0
	if hs, ok := humanStatusOf(human); ok {
		hlv = hs.BaseLevel
	}
	// the default map: the best zeny/h map while anyone saves zeny for its piece, else the best exp + zeny map
	zenyNeed := hg != nil && hg.Met.IsZero() && hg.How == "zeny"
	for _, c := range cands {
		zenyNeed = zenyNeed || (c.g != nil && c.g.Kind == "zeny")
	}
	st.Best = bestFarmMap(ms, hlv, zenyNeed)
	if hg != nil && hg.Met.IsZero() {
		all[0].Kind, all[0].Item, all[0].Map = hg.Kind, hg.Item, hg.Map
		// owner rule: the map must suit the party's level and gear - the human's item on an exp-poor / unsafe map is
		// bought with zeny instead of farmed (no turn)
		if hg.Map != "" && (mapExpPoor(hg.Map, min(partyMinBase(ms), max(hlv, 1))) || (len(ms) > 0 && !goalMapSafeItem(ms, hg.Map, hg.ItemID))) {
			all[0].Map = ""
			all[0].Kind = "zeny"
		}
	}
	all = append(all, cands...)
	st.Order = st.Order[:0]
	for _, c := range all {
		st.Order = append(st.Order, c.Name)
	}
	cur := -1
	for i, c := range all {
		if strings.EqualFold(c.Name, st.Turn) {
			cur = i
		}
	}
	// the current turn ends: its goal met / gone / no map any more, or its time is up and someone else is waiting
	ended, why := false, ""
	switch {
	case st.Turn == "":
		ended = true
	case cur < 0:
		ended, why = true, st.Turn+" left the party"
	case all[cur].Map == "":
		ended, why = true, st.Turn+"'s goal is done or needs no map ("+st.Item+")"
	case all[cur].Item != st.Item:
		ended, why = true, st.Turn+" now needs "+all[cur].Item+" (had: "+st.Item+")"
	case now.After(st.Until):
		ended, why = true, "turn time is up"
	}
	anyMapped := false
	for _, c := range all {
		anyMapped = anyMapped || c.Map != ""
	}
	if st.Turn == "" && !now.After(st.Until) && !anyMapped {
		st.Map = st.Best // no turn: the best map for the party's level and gear
		return
	}
	if !ended {
		st.Map = all[cur].Map // a safer drop map of the same goal
		return
	}
	if st.Turn != "" {
		st.Served[st.Turn] += now.Sub(st.Since).Minutes()
		st.Last[st.Turn] = now
	}
	// next: the member with a mappable goal who was served least (ties: longest since its last turn); the turn that
	// just ran out goes again only if nobody else has a mappable goal
	next := -1
	for i, c := range all {
		if c.Map == "" || (i == cur && now.After(st.Until) && st.Turn != "") {
			continue
		}
		if next < 0 || st.Served[c.Name] < st.Served[all[next].Name] ||
			(st.Served[c.Name] == st.Served[all[next].Name] && st.Last[c.Name].Before(st.Last[all[next].Name])) {
			next = i
		}
	}
	if next < 0 && cur >= 0 && all[cur].Map != "" {
		next = cur
	}
	prev := st.Turn
	if next < 0 {
		if st.Turn != "" {
			st.note(now, "no member has a goal that needs a map ("+why+"): the party farms where it levels best")
		}
		st.Turn, st.Kind, st.Item, st.Map, st.Why, st.Since, st.Until = "", "", "", st.Best, "no goal drops on a map fit for the party: farm the best map for its level and gear (exp + zeny)", now, now.Add(humanTurnLen)
		saveTurns()
		return
	}
	c := all[next]
	st.Turn, st.Kind, st.Item, st.Map, st.Since, st.Until = c.Name, c.Kind, c.Item, c.Map, now, now.Add(humanTurnLen)
	st.Why = fmt.Sprintf("%s's turn: %s from %s (%.0f min of turns so far)", c.Name, c.Item, c.Map, st.Served[c.Name])
	if prev != c.Name {
		st.note(now, fmt.Sprintf("turn %s -> %s (%s @ %s)%s", orNone(prev), c.Name, c.Item, c.Map, ifWhy(why)))
		go announceTurn(ms, human, c, prev)
	}
	saveTurns()
}

func orNone(s string) string {
	if s == "" {
		return "nobody"
	}
	return s
}

func ifWhy(w string) string {
	if w == "" {
		return ""
	}
	return ": " + w
}

// announceTurn: a friend says whose turn it is in party chat (the human's mind reads /api/party-turn and moves).
func announceTurn(ms []*Bot, human string, c memberTurnGoal, prev string) {
	var by *Bot
	for _, b := range ms {
		if b == c.b {
			by = b
			break
		}
	}
	line := ""
	if by == nil && c.Kind == "hunt" {
		return // the human's current hunt (no goal set yet): nothing to announce
	}
	if by != nil { // a friend's own turn: it asks
		line = pick("can we farm "+mapTalk(c.Map)+" for my "+c.Item+" next?", "my turn? need "+c.Item+" from "+mapTalk(c.Map),
			"anyone mind "+mapTalk(c.Map)+" for a bit? still need "+c.Item)
	} else {
		for _, b := range ms {
			b.mu.Lock()
			on := b.Online
			b.mu.Unlock()
			if on {
				by = b
				break
			}
		}
		line = pick(strings.ToLower(human)+" ur turn, "+c.Item+" at "+mapTalk(c.Map)+"?", "lets get "+human+" that "+c.Item+", "+mapTalk(c.Map)+"?")
	}
	if by == nil || line == "" {
		return
	}
	by.mu.Lock()
	typing, on := by.P.Typing, by.Online
	by.mu.Unlock()
	if !on {
		return
	}
	time.Sleep(time.Duration(3+crand(5)) * time.Second)
	by.partySay(styled(typing, line))
}

// humanPartyGoalMet (no lock held): a friend in a human party met its goal - it says so (it doesn't leave: the
// owner's test party stays together; the turn moves on).
func humanPartyGoalMet(b *Bot, g *farmGoal) {
	b.mu.Lock()
	typing, on := b.P.Typing, b.Online
	b.mu.Unlock()
	if !on || g == nil {
		return
	}
	line := ""
	switch g.Kind {
	case "gear", "card":
		line = pick("got my "+g.Item+"!! ty guys", "finally "+g.Item+" :D", "yes! "+g.Item+" dropped")
	case "zeny":
		line = pick("got enough zeny for my "+g.Item+", ill buy it next town run", "zeny for "+g.Item+" done, ty")
	default:
		return
	}
	go func() {
		time.Sleep(time.Duration(2+crand(4)) * time.Second)
		b.partySay(styled(typing, line))
	}()
}

// registerPartyTurn: GET /api/party-turn[?human=Ayla] (whose turn, map, why, order, members' goals, minutes served);
// POST {"human":"Ayla","item":"Soldier Skeleton Card","map":"pay_dun01"[,"kind":"card","why":"..."]} sets the human's
// own goal; {"human":"Ayla","met":true} marks it met; {"human":"Ayla","next":true} ends the current turn now.
func registerPartyTurn(mux *http.ServeMux) {
	mux.HandleFunc("/api/party-turn", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			var body struct {
				Human string   `json:"human"`
				Kind  string   `json:"kind"`
				Item  string   `json:"item"`
				Map   string   `json:"map"`
				Why   string   `json:"why"`
				Met   bool     `json:"met"`
				Owned []string `json:"owned"`
				Plan  string   `json:"plan"`
				Next  bool     `json:"next"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Human == "" {
				http.Error(w, "need {\"human\":...}", 400)
				return
			}
			now := time.Now()
			key := strings.ToLower(body.Human)
			turns.Lock()
			st := turns.m[key]
			if st == nil {
				st = &turnState{Human: body.Human, Served: map[string]float64{}, Last: map[string]time.Time{}}
				turns.m[key] = st
			}
			switch {
			case body.Owned != nil || body.Plan != "":
				if body.Owned != nil {
					st.Owned = body.Owned
					st.note(now, fmt.Sprintf("%s's gear: %s", body.Human, strings.Join(body.Owned, ", ")))
				}
				if body.Plan != "" {
					if builds[body.Plan] == nil {
						turns.Unlock()
						http.Error(w, "no such build plan "+body.Plan, 400)
						return
					}
					st.Plan = body.Plan
				}
				if st.Goal != nil && st.Goal.Auto {
					st.Goal = nil // re-picked from the new gear list on the next tick
				}
			case body.Met:
				if st.Goal != nil && st.Goal.Met.IsZero() {
					st.Goal.Met = now
					st.note(now, body.Human+" got "+st.Goal.Item)
					if st.Goal.Kind == "gear" || st.Goal.Kind == "zeny" {
						st.Owned = append(st.Owned, st.Goal.Item) // gear plan: the next piece from now on
					}
				}
			case body.Next:
				st.Until = now
				st.note(now, "turn ended early by request")
			case body.Item != "" || body.Map != "":
				g := &humanGoal{Kind: body.Kind, Item: body.Item, Map: strings.TrimSpace(body.Map), Why: body.Why, Set: now}
				if hits := findItems(body.Item, 1); len(hits) > 0 && body.Item != "" {
					g.ItemID, g.Item = hits[0].ID, hits[0].Name
				}
				if g.Kind == "" {
					g.Kind = "gear"
					if it := items[g.ItemID]; it != nil && it.Type == "Card" {
						g.Kind = "card"
					}
				}
				if g.Map != "" && maps[g.Map] == nil {
					turns.Unlock()
					http.Error(w, "no such map "+g.Map, 400)
					return
				}
				st.Goal = g
				st.note(now, fmt.Sprintf("%s's goal: %s @ %s", body.Human, g.Item, g.Map))
			}
			saveTurns()
			turns.Unlock()
		}
		want := strings.ToLower(r.URL.Query().Get("human"))
		turns.Lock()
		out := []map[string]interface{}{}
		for k, st := range turns.m {
			if want != "" && k != want {
				continue
			}
			out = append(out, map[string]interface{}{"human": st.Human, "turn": st.Turn, "kind": st.Kind, "item": st.Item,
				"map": st.Map, "why": st.Why, "since": st.Since, "until": st.Until, "order": st.Order,
				"served_min": st.Served, "human_goal": st.Goal, "history": st.History, "best_map": st.Best,
				"plan": st.Plan, "owned": st.Owned})
		}
		turns.Unlock()
		// the members' current goals (bots)
		parties.Lock()
		goals := map[string][]memberTurnGoal{}
		var hps []*Party
		for _, p := range parties.byName {
			if p.Human != "" {
				hps = append(hps, p)
			}
		}
		parties.Unlock()
		gear := map[string][]memberGear{}
		poolTurn := map[string]string{}
		for _, p := range hps {
			parties.Lock()
			ms := append([]*Bot(nil), p.Members...)
			name := p.Name
			parties.Unlock()
			key := strings.ToLower(p.Human)
			poolTurn[key], _ = gearPoolView(name)
			spare := map[*Bot]int{}
			total := 0 // the friends' spare zeny (mailable between them; the human's isn't counted for a friend)
			for _, b := range ms {
				b.mu.Lock()
				spare[b] = b.spareZeny()
				b.mu.Unlock()
				total += spare[b]
			}
			for _, b := range ms {
				b.mu.Lock()
				g := b.fgoal
				row := memberTurnGoal{Name: b.charName()}
				if g != nil {
					row.Kind, row.Item, row.Map = g.Kind, g.Item, g.Map
				}
				mg := memberGear{Name: b.charName(), Have: spare[b], Pooled: total - spare[b], How: "done"}
				if id := b.turnPiece(); id != 0 && items[id] != nil { // locked until bought: the row shows that piece
					mg.Next, mg.Source, mg.Price = items[id].Name, "shop", items[id].Buy
					if len(gearShops(id)) == 0 {
						mg.Source, mg.Price = "trade", priceOf(id)
					}
					switch {
					case mg.Price > 0 && (mg.Have >= mg.Price || (strings.EqualFold(poolTurn[key], mg.Name) && mg.Have+mg.Pooled >= mg.Price)):
						mg.How = "buy"
					case g != nil && g.Kind == "gear":
						mg.How, mg.Map = "farm", g.Map
					default:
						mg.How = "zeny"
					}
				} else if pc := b.nextBuildPiece(); pc != nil {
					mg.Next, mg.Source, mg.Price = pc.Name, pc.Source, pc.Price
					if pc.NPC > 0 {
						mg.Price = pc.NPC
					}
					// buy: it can pay itself, or it holds the gear pool's turn and the pool covers it (gearpool.go mails the
					// friends' spare zeny to the turn holder) - not every member counting the same pool at once
					switch {
					case mg.Price > 0 && (mg.Have >= mg.Price || (strings.EqualFold(poolTurn[key], mg.Name) && mg.Have+mg.Pooled >= mg.Price)):
						mg.How = "buy"
					case g != nil && g.Kind == "gear" && g.ItemID == pc.ItemID:
						mg.How, mg.Map = "farm", g.Map
					default:
						mg.How = "zeny"
					}
				}
				b.mu.Unlock()
				goals[key] = append(goals[key], row)
				gear[key] = append(gear[key], mg)
			}
		}
		for _, o := range out {
			k := strings.ToLower(o["human"].(string))
			o["member_goals"] = goals[k]
			o["gear_pool_turn"] = poolTurn[k] // gearpool.go: whose piece the party's spare zeny buys now
			rows := gear[k]
			if hg, ok := o["human_goal"].(*humanGoal); ok && hg != nil && hg.Auto {
				hrow := memberGear{Name: o["human"].(string), Next: hg.Item, Price: hg.Price, How: hg.How, Map: hg.Map}
				rows = append([]memberGear{hrow}, rows...)
			}
			o["gear"] = rows
		}
		writeJSON(w, map[string]interface{}{"on": tune().GoalPartiesOn || coreInstance(), "parties": out,
			"note": "the party farms for the turn member's goal (map); the human's mind moves the party there and marks its own goal met"})
	})
}
