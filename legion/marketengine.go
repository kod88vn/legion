package main

// marketengine.go (WP2, EC43-EC47; design docs/design/market-engine.md sections 3.1-3.2, 3.5, 4.1).
//
// The market engine is an algorithm, not an agent (EC46). It reads the engine's own data, works out what the server
// needs, ranks the gaps, and offers each idle farmer its best (item, map) pair. It is INFORMATION ONLY (EC43): it never
// writes a guild direction, a bot goal or an order that a bot must follow. Farmers' own code decides (Constitution 2.7).
//
// API for the farmer code (WP3, farmers.go). Everything below is pure: no locks, no globals, safe with fixtures.
//
//   type MESnapshot   Now, Items []MEItem, Farmers []MEFarmer, Hours (H, default 2)
//   type MEItem       ID, Name, Price, Heal, Demand, BuildShort, GuildNeed, Listed, Chasers, Glut, BossOnly, NPCSold, Band, Maps []MEMap
//   type MEMap        Map, Mob, MobLv, Y (units of the item per farmer-hour on this map), Danger (0..0.8), Fast (faster than a step)
//   type MEFarmer     Name, Lv (>= 15 to get an order), Greed (kept, not used here), Travel (minutes per map), Held (an order
//                     already live: kept and counted), Recent (item -> when this farmer last had it)
//   meNeeds(in)       []MEGap: need = max(0, demand + guildNeed + buildShort - listed - 3*chasers) (units); remain = need minus
//                     what live holds will bring (Y*H each); covered, glut, boss-only, NPC-sold and junk items drop out.
//                     Ranked by remain * price (healing x2).
//   meAllocate(in, gaps) MEAllocation: greedy water-filling. Farmers in a rotated name order (the start moves each hour)
//                     each take their best option; value = min(remain, Y*H) * price * healMult * (1-risk) / (1+travel/30),
//                     divided by 1.3 for a fast mob. Caps: per item min(12, max(1, ceil(30% of farmers))), per map 5, ordered
//                     at most 70% of farmers (floor). A recently ordered item (6 h) is taken only when it is worth at least
//                     twice the best other option. Output: orders (held ones included) with a rank of up to 3 choices.
//   meEvaluate(in)    MEResult: gaps + allocation, with FarmersOn per item.
//
// Live side (the only part that reads the running engine). It runs inside econRound on the rounds the stock round is due
// (every guildStockEvery), and only when tuning flag market_engine_on is true; with the flag off it computes nothing:
//   marketEngineRound(snaps, now, stockDue)   -> builds the snapshot, evaluates, writes <data>/market_feed.json (atomic)
//   GET /api/market/engine                    -> status, last run, caps, gaps, distribution (or "off")
//   GET /api/market/feed[?guild=guildNN]      -> the feed (EC43 data only: no set_by, no direction) (or "off")
// Flag guild_market_feed_on (tuning.go) is declared here only: the guild leaders' autopilot does not read the feed yet
// (WP5). Farmers come from <data>/farmers.json (accounts list), read-only; WP3 owns that roster.
//
// Not verified in WP2 (see the report): the mob speed field (Fast is always false), map danger (always 0), spawn
// counts beyond the drop rate x kills per hour (Y uses 3 kills a minute), zeny per hour (meter, WP8), farmer greed.

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	meHoldHours      = 2.0           // H: an order is worth this many farmer-hours of yield
	meMinLv          = 15            // orders from this base level (checkMarket's rule)
	meMapLvReach     = 10            // a map's mob may be this far above the farmer's level (pieceFarms)
	meDangerMax      = 0.8           // above this a map is never offered
	meItemMax        = 12            // farmers on one item, at most
	meItemSharePct   = 30            // ...and at most this % of farmers (rounded up, at least 1)
	meMapMax         = 5             // farmers on one map
	meOrderedPct     = 70            // at most this % of farmers hold an order at one time (floor)
	meRecentGap      = 6 * time.Hour // same item to the same farmer again only if it is worth twice the next best
	meFastFit        = 1.3           // a melee mob at or under meMeleeFastMs: value divided by this
	meMeleeFastMs    = 150           // mob_db WalkSpeed at or under this (ms per cell; player default 150) is "fast" for a hunter
	meCreamyCap      = 10            // the Creamy Card (base rate 1/10000, server card rate x500) is capped at this need: it stays ranked only while short
	meCreamyBonus    = 50            // the card's demand bonus (opportunity.go), before the cap
	meRankLen        = 3             // choices offered to the farmer's own code
	meFallbackKPM    = 3.0           // kills a minute when the farmer's meter is unknown
	meGapsOut        = 40            // gaps written to the feed
	meRoundFarmerMin = 1             // a farmer with no order is listed as unassigned
)

// MEMap is one (map, mob) pair where an item drops.
type MEMap struct {
	Map    string  `json:"map"`
	Mob    string  `json:"mob"`
	MobLv  int     `json:"mob_lv"`
	Y      float64 `json:"y_per_h"` // units of the item per farmer-hour here
	Danger float64 `json:"danger"`
	Fast   bool    `json:"fast"`
}

// MEItem is one item the market may want.
type MEItem struct {
	ID         int     `json:"id"`
	Name       string  `json:"name"`
	Price      int     `json:"price"`
	Heal       bool    `json:"heal,omitempty"`
	Demand     int     `json:"demand"`      // live wants, sales and buyers (marketOpps terms)
	BuildShort int     `json:"build_short"` // online bots whose next build piece this is
	GuildNeed  int     `json:"guild_need"`  // guilds whose stock lacks it
	Listed     int     `json:"listed"`      // units in shops
	Chasers    int     `json:"chasers"`     // bots already farming it (each counts 3)
	Glut       bool    `json:"glut,omitempty"`
	BossOnly   bool    `json:"boss_only,omitempty"`
	NPCSold    bool    `json:"npc_sold,omitempty"` // an NPC sells it (not a heal): nobody pays a farmer more
	Band       string  `json:"band,omitempty"`     // the level band with the largest build shortfall (feed)
	Cap        int     `json:"cap,omitempty"`      // 0 = no cap; else the need is never more than this (rare cards)
	Maps       []MEMap `json:"maps"`
}

// MEHold is a farmer's live order: its item and map. Its yield is already counted in the gap.
type MEHold struct {
	ItemID int    `json:"item_id"`
	Map    string `json:"map"`
}

// MEFarmer is one online farmer.
type MEFarmer struct {
	Name   string             `json:"name"`
	Lv     int                `json:"lv"`
	Greed  float64            `json:"greed"`
	Travel map[string]float64 `json:"travel,omitempty"` // minutes to each map (missing = 0)
	Held   *MEHold            `json:"held,omitempty"`
	Recent map[int]time.Time  `json:"-"`
}

// MESnapshot is everything the pure core needs.
type MESnapshot struct {
	Now     time.Time
	Items   []MEItem
	Farmers []MEFarmer
	Hours   float64 // H (0 = meHoldHours)
}

// MEGap is an item the server is short of, ranked.
type MEGap struct {
	ItemID    int      `json:"item_id"`
	Item      string   `json:"item"`
	Band      string   `json:"band,omitempty"`
	Kind      string   `json:"kind"`
	Demand    int      `json:"demand"`
	Need      int      `json:"need"`   // units short (before live orders)
	Remain    float64  `json:"remain"` // units not yet covered by live orders
	Listed    int      `json:"listed"`
	Price     int      `json:"price"`
	Score     float64  `json:"score"`
	Maps      []string `json:"drop_maps"`
	FarmersOn int      `json:"farmers_on"`
	Heal      bool     `json:"heal,omitempty"`
}

// MERank is one choice in an order's rank.
type MERank struct {
	ItemID int     `json:"item_id"`
	Item   string  `json:"item"`
	Map    string  `json:"map"`
	Mob    string  `json:"mob"`
	YPerH  float64 `json:"y_per_h"`
	Value  float64 `json:"value"`
}

// MEOrder is one farmer's offer (held = a live order the engine keeps counting).
type MEOrder struct {
	Farmer string   `json:"farmer"`
	ItemID int      `json:"item_id"`
	Item   string   `json:"item"`
	Map    string   `json:"map"`
	Mob    string   `json:"mob"`
	YPerH  float64  `json:"y_per_h"`
	Value  float64  `json:"value"`
	Held   bool     `json:"held,omitempty"`
	Rank   []MERank `json:"rank,omitempty"`
}

// MECaps are the limits in force for one allocation.
type MECaps struct {
	PerItem int `json:"per_item"`
	PerMap  int `json:"per_map"`
	Ordered int `json:"ordered"`
}

// MEDist is the spread of the orders.
type MEDist struct {
	ByItem       map[int]int    `json:"by_item"`
	ByMap        map[string]int `json:"by_map"`
	MaxItemShare float64        `json:"max_item_share"`
}

// MEAllocation is the output of meAllocate.
type MEAllocation struct {
	Orders     []MEOrder `json:"orders"`
	Unassigned []string  `json:"unassigned"`
	Ordered    int       `json:"ordered"`
	Caps       MECaps    `json:"caps"`
	Dist       MEDist    `json:"distribution"`
}

// MEResult is one evaluation.
type MEResult struct {
	T          time.Time    `json:"t"`
	Farmers    int          `json:"farmers"`
	Needs      []MEGap      `json:"needs"`
	Allocation MEAllocation `json:"allocation"`
}

func meHours(in MESnapshot) float64 {
	if in.Hours > 0 {
		return in.Hours
	}
	return meHoldHours
}

func meItemIndex(items []MEItem) map[int]*MEItem {
	out := make(map[int]*MEItem, len(items))
	for i := range items {
		out[items[i].ID] = &items[i]
	}
	return out
}

// meNeed: units of the item the market is short of (before live orders).
func meNeed(it MEItem) int {
	n := max(0, it.Demand+it.GuildNeed+it.BuildShort-it.Listed-oppChaseSupply*it.Chasers)
	if it.Cap > 0 {
		n = min(n, it.Cap)
	}
	return n
}

// meMapName lists a map's name for the gap (best yield first, at most four).
func meDropMaps(it *MEItem) []string {
	ms := append([]MEMap(nil), it.Maps...)
	sort.SliceStable(ms, func(i, j int) bool { return ms[i].Y > ms[j].Y })
	var out []string
	seen := map[string]bool{}
	for _, m := range ms {
		if m.Y > 0 && !seen[m.Map] && len(out) < 4 {
			seen[m.Map] = true
			out = append(out, m.Map)
		}
	}
	return out
}

// meNeeds: the gaps, ranked by score (remain x price, healing x2). Pure.
func meNeeds(in MESnapshot) []MEGap {
	hours := meHours(in)
	byID := meItemIndex(in.Items)
	holdUnits := map[int]float64{}
	holdCount := map[int]int{}
	for _, f := range in.Farmers {
		if f.Held == nil {
			continue
		}
		holdCount[f.Held.ItemID]++
		if it := byID[f.Held.ItemID]; it != nil {
			for _, m := range it.Maps {
				if m.Map == f.Held.Map {
					holdUnits[it.ID] += m.Y * hours
				}
			}
		}
	}
	var out []MEGap
	for i := range in.Items {
		it := &in.Items[i]
		if it.Glut || it.BossOnly || (!it.Heal && it.NPCSold) || it.Price < 10 || len(it.Maps) == 0 {
			continue
		}
		need := meNeed(*it)
		if need <= 0 {
			continue
		}
		remain := float64(need) - holdUnits[it.ID]
		if remain <= 0 {
			continue // covered by live orders
		}
		score := remain * float64(it.Price)
		if it.Heal {
			score *= oppHealMult
		}
		out = append(out, MEGap{ItemID: it.ID, Item: it.Name, Band: it.Band, Kind: "short", Demand: it.Demand, Need: need,
			Remain: remain, Listed: it.Listed, Price: it.Price, Score: score, Maps: meDropMaps(it), FarmersOn: holdCount[it.ID], Heal: it.Heal})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].ItemID < out[j].ItemID
	})
	return out
}

// meOpt is one candidate (item, map) for one farmer.
type meOpt struct {
	ItemID int
	Item   string
	Map    string
	Mob    string
	Y      float64
	Value  float64
	Recent bool
}

// meOptions: the farmer's candidates over the open gaps, best first (no caps beyond the per-item and per-map ones).
func meOptions(f MEFarmer, gaps []MEGap, byID map[int]*MEItem, remain map[int]float64, itemCount map[int]int,
	mapCount map[string]int, caps MECaps, now time.Time, hours float64) []meOpt {
	var out []meOpt
	for _, g := range gaps {
		r := remain[g.ItemID]
		if r <= 0 || itemCount[g.ItemID] >= caps.PerItem {
			continue
		}
		it := byID[g.ItemID]
		if it == nil {
			continue
		}
		recent := false
		if t, ok := f.Recent[g.ItemID]; ok && now.Sub(t) < meRecentGap {
			recent = true
		}
		for _, m := range it.Maps {
			if m.Y <= 0 || m.MobLv > f.Lv+meMapLvReach || m.Danger > meDangerMax || mapCount[m.Map] >= caps.PerMap {
				continue
			}
			risk := math.Min(0.8, m.Danger+0.05*float64(max(0, m.MobLv-f.Lv)))
			v := math.Min(r, m.Y*hours) * float64(it.Price) * (1 - risk) / (1 + f.Travel[m.Map]/30)
			if it.Heal {
				v *= oppHealMult
			}
			if m.Fast {
				v /= meFastFit
			}
			if v <= 0 {
				continue
			}
			out = append(out, meOpt{ItemID: it.ID, Item: it.Name, Map: m.Map, Mob: m.Mob, Y: m.Y, Value: v, Recent: recent})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Value != out[j].Value {
			return out[i].Value > out[j].Value
		}
		if out[i].ItemID != out[j].ItemID {
			return out[i].ItemID < out[j].ItemID
		}
		return out[i].Map < out[j].Map
	})
	return out
}

// mePick: the option a farmer takes. A recently ordered item is taken only when it is worth twice the best other one.
func mePick(opts []meOpt) (meOpt, bool) {
	if len(opts) == 0 {
		return meOpt{}, false
	}
	if !opts[0].Recent {
		return opts[0], true
	}
	var best *meOpt
	for i := range opts {
		if !opts[i].Recent {
			best = &opts[i]
			break
		}
	}
	if best == nil {
		return meOpt{}, false
	}
	if opts[0].Value >= 2*best.Value {
		return opts[0], true
	}
	return *best, true
}

// meRank: up to meRankLen distinct items, each with its best map, best first.
func meRank(opts []meOpt) []MERank {
	var out []MERank
	seen := map[int]bool{}
	for _, o := range opts {
		if seen[o.ItemID] {
			continue
		}
		seen[o.ItemID] = true
		out = append(out, MERank{ItemID: o.ItemID, Item: o.Item, Map: o.Map, Mob: o.Mob, YPerH: o.Y, Value: o.Value})
		if len(out) == meRankLen {
			break
		}
	}
	return out
}

// meAllocate: greedy water-filling over the gaps (see the header). Pure.
func meAllocate(in MESnapshot, gaps []MEGap) MEAllocation {
	hours := meHours(in)
	n := len(in.Farmers)
	caps := MECaps{
		PerItem: min(meItemMax, max(1, (n*meItemSharePct+99)/100)),
		PerMap:  meMapMax,
		Ordered: n * meOrderedPct / 100,
	}
	byID := meItemIndex(in.Items)
	remain := map[int]float64{}
	for _, g := range gaps {
		remain[g.ItemID] = g.Remain
	}
	itemCount := map[int]int{}
	mapCount := map[string]int{}
	alloc := MEAllocation{Caps: caps}
	var pool []MEFarmer
	for _, f := range in.Farmers {
		if f.Held != nil {
			name, mob, y := "", "", 0.0
			if it := byID[f.Held.ItemID]; it != nil {
				name = it.Name
				for _, m := range it.Maps {
					if m.Map == f.Held.Map {
						mob, y = m.Mob, m.Y
					}
				}
			}
			alloc.Orders = append(alloc.Orders, MEOrder{Farmer: f.Name, ItemID: f.Held.ItemID, Item: name, Map: f.Held.Map, Mob: mob, YPerH: y, Held: true})
			itemCount[f.Held.ItemID]++
			mapCount[f.Held.Map]++
			alloc.Ordered++
			continue
		}
		if f.Lv >= meMinLv {
			pool = append(pool, f)
		} else {
			alloc.Unassigned = append(alloc.Unassigned, f.Name)
		}
	}
	sort.Slice(pool, func(i, j int) bool { return pool[i].Name < pool[j].Name })
	if len(pool) > 0 {
		off := int(in.Now.Unix()/3600) % len(pool) // the first pick moves every hour: no farmer is always first
		rot := append(append([]MEFarmer(nil), pool[off:]...), pool[:off]...)
		pool = rot
	}
	ordered := alloc.Ordered
	assigned := map[string]bool{}
	for ordered < caps.Ordered {
		progress := false
		for _, f := range pool {
			if ordered >= caps.Ordered {
				break
			}
			if assigned[f.Name] {
				continue
			}
			opts := meOptions(f, gaps, byID, remain, itemCount, mapCount, caps, in.Now, hours)
			pick, ok := mePick(opts)
			if !ok {
				continue
			}
			assigned[f.Name] = true
			ordered++
			progress = true
			itemCount[pick.ItemID]++
			mapCount[pick.Map]++
			remain[pick.ItemID] -= pick.Y * hours
			alloc.Orders = append(alloc.Orders, MEOrder{Farmer: f.Name, ItemID: pick.ItemID, Item: pick.Item, Map: pick.Map,
				Mob: pick.Mob, YPerH: pick.Y, Value: pick.Value, Rank: meRank(opts)})
		}
		if !progress {
			break
		}
	}
	for _, f := range pool {
		if !assigned[f.Name] {
			alloc.Unassigned = append(alloc.Unassigned, f.Name)
		}
	}
	sort.Strings(alloc.Unassigned)
	alloc.Ordered = ordered
	alloc.Dist = meDistribution(alloc.Orders)
	return alloc
}

// meDistribution: farmers per item and per map, and the largest share one item takes.
func meDistribution(orders []MEOrder) MEDist {
	d := MEDist{ByItem: map[int]int{}, ByMap: map[string]int{}}
	for _, o := range orders {
		d.ByItem[o.ItemID]++
		d.ByMap[o.Map]++
	}
	top := 0
	for _, c := range d.ByItem {
		top = max(top, c)
	}
	if len(orders) > 0 {
		d.MaxItemShare = math.Round(float64(top)/float64(len(orders))*1000) / 1000
	}
	return d
}

// meEvaluate: gaps and allocation for one snapshot. Pure.
func meEvaluate(in MESnapshot) MEResult {
	gaps := meNeeds(in)
	alloc := meAllocate(in, gaps)
	for i := range gaps {
		gaps[i].FarmersOn = alloc.Dist.ByItem[gaps[i].ItemID]
	}
	return MEResult{T: in.Now, Farmers: len(in.Farmers), Needs: gaps, Allocation: alloc}
}

// ---------------------------------------------------------------- live side

// MEBand is a level band with its bot count (feed).
type MEBand struct {
	Band string `json:"band"`
	Bots int    `json:"bots"`
}

// MEFeedItem is one short item in the feed (EC43: data, no direction).
type MEFeedItem struct {
	ItemID    int      `json:"item_id"`
	Item      string   `json:"item"`
	Kind      string   `json:"kind"`
	Band      string   `json:"band,omitempty"`
	Demand    int      `json:"demand"`
	Listed    int      `json:"listed"`
	Price     int      `json:"price"`
	DropMaps  []string `json:"drop_maps"`
	FarmersOn int      `json:"farmers_on"`
}

// MEFeedGuildNeed is one thing a guild's members lack.
type MEFeedGuildNeed struct {
	Item string `json:"item"`
	N    int    `json:"n"`
}

// MEFeedGuild is one guild's view of the feed.
type MEFeedGuild struct {
	ID             string            `json:"id"`
	Needs          []MEFeedGuildNeed `json:"needs"`
	GearGapMembers []MEBand          `json:"gear_gap_members"`
	FarmersNote    string            `json:"farmers_note"`
}

// MEFeed is the market feed (design 4.1). It carries data only.
type MEFeed struct {
	T           time.Time              `json:"t"`
	Status      string                 `json:"status"`
	Missing     []string               `json:"missing"`
	Server      MEFeedServer           `json:"server"`
	Short       []MEFeedItem           `json:"short"`
	Glut        []MEFeedGlut           `json:"glut"`
	ZenyPerHour []MEFeedZeny           `json:"zeny_per_hour"`
	Guild       *MEFeedGuild           `json:"guild,omitempty"`
	Guilds      map[string]MEFeedGuild `json:"guilds,omitempty"`
}

// MEFeedServer: the bots in the world and per band.
type MEFeedServer struct {
	Bots   int      `json:"bots"`
	Online int      `json:"online"`
	Bands  []MEBand `json:"bands"`
}

// MEFeedGlut: an item in surplus.
type MEFeedGlut struct {
	Item   string `json:"item"`
	Listed int    `json:"listed"`
}

// MEFeedZeny: zeny per hour by band (the meter is WP8: empty until it is wired).
type MEFeedZeny struct {
	Band    string  `json:"band"`
	N       int     `json:"n"`
	ZPHMean float64 `json:"zph_mean"`
}

// meFeedMissingWP2 names what this feed cannot yet read, so no number is invented.
var meFeedMissingWP2 = []string{
	"zeny_per_hour: meter feed not wired (WP8)",
	"bands: approximate level bands, not progress.py's list",
	"mob speed and map danger: not read (always 0 / false)",
}

func meBand(lv int) string {
	switch {
	case lv < 18:
		return "1-17"
	case lv < 33:
		return "18-32"
	case lv < 51:
		return "33-50"
	case lv < 71:
		return "51-70"
	}
	return "71-99"
}

// meBuildFeed: the feed from one evaluation. Pure: the caller passes the server counts and guild views.
func meBuildFeed(res MEResult, in MESnapshot, server MEFeedServer, guilds map[string]MEFeedGuild, missing []string) MEFeed {
	f := MEFeed{T: res.T, Missing: append([]string{}, missing...), Server: server, Guilds: guilds,
		Short: []MEFeedItem{}, Glut: []MEFeedGlut{}, ZenyPerHour: []MEFeedZeny{}}
	for i, g := range res.Needs {
		if i >= meGapsOut {
			break
		}
		f.Short = append(f.Short, MEFeedItem{ItemID: g.ItemID, Item: g.Item, Kind: g.Kind, Band: g.Band, Demand: g.Demand,
			Listed: g.Listed, Price: g.Price, DropMaps: g.Maps, FarmersOn: g.FarmersOn})
	}
	for _, it := range in.Items {
		if it.Glut {
			f.Glut = append(f.Glut, MEFeedGlut{Item: it.Name, Listed: it.Listed})
		}
	}
	sort.SliceStable(f.Glut, func(i, j int) bool { return f.Glut[i].Item < f.Glut[j].Item })
	switch {
	case len(in.Items) == 0 && server.Online == 0:
		f.Status = "no_data"
	case len(f.Missing) > 0:
		f.Status = "partial"
	default:
		f.Status = "ok"
	}
	return f
}

// meFeedGuildView: a guild block from the guild's member needs and its stock gaps.
func meFeedGuildView(id string, needs map[int]int, names map[int]string, bands map[string]int) MEFeedGuild {
	g := MEFeedGuild{ID: id, Needs: []MEFeedGuildNeed{}, GearGapMembers: []MEBand{}, FarmersNote: "info only"}
	var ids []int
	for item := range needs {
		ids = append(ids, item)
	}
	sort.Ints(ids)
	for _, item := range ids {
		g.Needs = append(g.Needs, MEFeedGuildNeed{Item: names[item], N: needs[item]})
	}
	var bs []string
	for b := range bands {
		bs = append(bs, b)
	}
	sort.Strings(bs)
	for _, b := range bs {
		g.GearGapMembers = append(g.GearGapMembers, MEBand{Band: b, Bots: bands[b]})
	}
	return g
}

// meFarmerRoster: the farmer accounts (WP3 owns the file), read-only. nil when there is no roster.
func meFarmerRoster() map[string]bool {
	raw, err := os.ReadFile(filepath.Join(*dataDir, "farmers.json"))
	if err != nil {
		return nil
	}
	var f struct {
		Accounts []string `json:"accounts"`
	}
	if json.Unmarshal(raw, &f) != nil {
		return nil
	}
	out := map[string]bool{}
	for _, a := range f.Accounts {
		out[strings.ToLower(a)] = true
	}
	return out
}

// meMapsFor: the item's drop maps from the engine's tables (level filter is done by the core per farmer).
// The drop rate comes from pieceFarms (effDropRate: the server's item_rate_* multipliers, card rate included). Spawns
// are whatever mobMaps says (Creamy: prt_fild02, gef_fild05, pay_fild07, mjolnir_01/06, not hard-coded here).
// Fast: a melee mob whose WalkSpeed is at or under meMeleeFastMs. Mobs are matched by name (pieceFarm carries no id).
func meMapsFor(id int) []MEMap {
	fast := map[string]bool{}
	for _, mid := range droppedBy[id] {
		if mb := mobs[mid]; mb != nil && mb.WalkSpeed > 0 && mb.WalkSpeed <= meMeleeFastMs {
			fast[mb.Name] = true
		}
	}
	var out []MEMap
	for _, pf := range pieceFarms(id, 200, 8) {
		out = append(out, MEMap{Map: pf.Map, Mob: pf.Mob, MobLv: pf.MobLv,
			Y:    pf.Rate / 100 * meFallbackKPM * 60, // Rate is percent of kills that drop it
			Fast: fast[pf.Mob]})
	}
	return out
}

// meState: the last run, served by the endpoints. Written only by marketEngineRound.
var meState struct {
	sync.Mutex
	runs   int
	last   time.Time
	result *MEResult
	feed   *MEFeed
}

// marketEngineRound: called from econRound after guildStockRound. stockDue: the round in which nextPiece is filled.
func marketEngineRound(snaps []*econSnap, now time.Time, stockDue bool) {
	if !tune().MarketEngineOn || !stockDue {
		return
	}
	in := meLiveSnapshot(snaps, now)
	res := meEvaluate(in)
	server := MEFeedServer{Online: len(snaps), Bands: []MEBand{}}
	if bots, ok := allBots.Load().([]*Bot); ok {
		server.Bots = len(bots)
	}
	bandN := map[string]int{}
	for _, s := range snaps {
		bandN[meBand(s.lv)]++
	}
	var bs []string
	for b := range bandN {
		bs = append(bs, b)
	}
	sort.Strings(bs)
	for _, b := range bs {
		server.Bands = append(server.Bands, MEBand{Band: b, Bots: bandN[b]})
	}
	guilds := meLiveGuilds(snaps, in)
	feed := meBuildFeed(res, in, server, guilds, meFeedMissingWP2)
	meState.Lock()
	meState.runs++
	meState.last = now
	meState.result = &res
	meState.feed = &feed
	meState.Unlock()
	if err := writeMarketFeed(*dataDir, feed); err != nil {
		fmt.Printf("market engine: feed not written: %v\n", err)
	}
}

// meLiveSnapshot: the engine's own data, read the way marketOpps reads it (same demand terms). Takes econ.mu,
// dynPrices, opps and gstock one at a time, never b.mu.
func meLiveSnapshot(snaps []*econSnap, now time.Time) MESnapshot {
	need, listed := stockDemand()
	buyers := map[int]int{}
	boss := map[int]bool{}
	dynPrices.RLock()
	for _, r := range dynPrices.rows {
		buyers[r.ID], boss[r.ID] = r.Buyers, r.BossOnly
	}
	dynPrices.RUnlock()
	ids := map[int]bool{}
	for id := range need {
		ids[id] = true
	}
	for id, n := range buyers {
		if n > 0 {
			ids[id] = true
		}
	}
	for id := range foodHeal {
		ids[id] = true
	}
	for id := itemRedPotion; id <= itemWhitePotion; id++ {
		ids[id] = true
	}
	ids[itemWhiteHerb] = true
	gapCount := map[int]int{}
	gstock.Lock()
	for _, gids := range gstock.gapIDs {
		for _, id := range gids {
			gapCount[id]++
			ids[id] = true
		}
	}
	gstock.Unlock()
	buildN := map[int]map[string]int{}
	for _, s := range snaps {
		if s.nextPiece > 0 && !s.keeper {
			if buildN[s.nextPiece] == nil {
				buildN[s.nextPiece] = map[string]int{}
			}
			buildN[s.nextPiece][meBand(s.lv)]++
			ids[s.nextPiece] = true
		}
	}
	roster := meFarmerRoster()
	in := MESnapshot{Now: now, Hours: meHoldHours}
	opps.Lock()
	chasers := map[int]int{}
	for id := range ids {
		chasers[id] = chasersLocked(id, now)
	}
	opps.Unlock()
	for id := range ids {
		x := items[id]
		if x == nil {
			continue
		}
		heal := healItem(id)
		if !heal && !tradeGood(x) {
			continue // junk is not a market item (marketOpps)
		}
		d := need[id] + buyers[id]/5
		if heal {
			d += oppHealDemand
		}
		capN := 0
		if id == itemCreamyCard {
			d += meCreamyBonus
			capN = meCreamyCap
		}
		band, best := "", 0
		for b, c := range buildN[id] {
			if c > best || c == best && b < band {
				band, best = b, c
			}
		}
		in.Items = append(in.Items, MEItem{ID: id, Name: x.Name, Price: priceOf(id), Heal: heal, Demand: d,
			BuildShort: buildTotal(buildN[id]), GuildNeed: gapCount[id], Listed: listed[id], Chasers: chasers[id], Cap: capN,
			Glut: glutted(id), BossOnly: boss[id], NPCSold: !heal && len(gearShops(id)) > 0, Band: band, Maps: meMapsFor(id)})
	}
	sort.Slice(in.Items, func(i, j int) bool { return in.Items[i].ID < in.Items[j].ID })
	for _, s := range snaps {
		if roster == nil || !roster[strings.ToLower(s.b.P.Account)] {
			continue
		}
		in.Farmers = append(in.Farmers, MEFarmer{Name: s.name, Lv: s.lv})
	}
	return in
}

func buildTotal(m map[string]int) int {
	t := 0
	for _, c := range m {
		t += c
	}
	return t
}

// meLiveGuilds: each guild's needs (members' next build pieces) and gear-gap members per band, plus the stock gaps.
func meLiveGuilds(snaps []*econSnap, in MESnapshot) map[string]MEFeedGuild {
	names := map[int]string{}
	for _, it := range in.Items {
		names[it.ID] = it.Name
	}
	needs := map[string]map[int]int{}
	bands := map[string]map[string]int{}
	for _, s := range snaps {
		if s.guild == "" || s.keeper {
			continue
		}
		if s.nextPiece > 0 {
			if needs[s.guild] == nil {
				needs[s.guild] = map[int]int{}
				bands[s.guild] = map[string]int{}
			}
			needs[s.guild][s.nextPiece]++
			bands[s.guild][meBand(s.lv)]++
		}
	}
	out := map[string]MEFeedGuild{}
	for g := range needs {
		out[g] = meFeedGuildView(g, needs[g], names, bands[g])
	}
	return out
}

// writeMarketFeed: data/market_feed.json, atomically (temp file in the same folder, sync, rename).
func writeMarketFeed(dir string, f MEFeed) error {
	raw, err := json.MarshalIndent(f, "", " ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "market_feed-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, filepath.Join(dir, "market_feed.json")); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}

// ---------------------------------------------------------------- API

func registerMarketEngine(mux *http.ServeMux) {
	off := func(w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "off", "flag": "market_engine_on",
			"note": "the market engine is off (tuning.json market_engine_on); nothing is computed"})
	}
	mux.HandleFunc("/api/market/engine", func(w http.ResponseWriter, r *http.Request) {
		if !tune().MarketEngineOn {
			off(w)
			return
		}
		meState.Lock()
		defer meState.Unlock()
		out := map[string]interface{}{"status": "no_data", "runs": meState.runs, "last_run": meState.last}
		if res := meState.result; res != nil {
			out["status"] = "ok"
			out["farmers"] = res.Farmers
			out["caps"] = res.Allocation.Caps
			out["ordered"] = res.Allocation.Ordered
			out["unassigned"] = len(res.Allocation.Unassigned)
			out["distribution"] = res.Allocation.Dist
			out["needs"] = res.Needs
			out["orders"] = res.Allocation.Orders
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(out)
	})
	mux.HandleFunc("/api/market/feed", func(w http.ResponseWriter, r *http.Request) {
		if !tune().MarketEngineOn {
			off(w)
			return
		}
		meState.Lock()
		var f MEFeed
		if meState.feed != nil {
			f = *meState.feed
		} else {
			f = MEFeed{Status: "no_data", Missing: []string{"no run yet"}, Short: []MEFeedItem{}, Glut: []MEFeedGlut{}, ZenyPerHour: []MEFeedZeny{}}
		}
		var gv *MEFeedGuild
		if g := r.URL.Query().Get("guild"); g != "" {
			v, ok := meState.feed.guildOf(g)
			if !ok {
				v = MEFeedGuild{ID: g, Needs: []MEFeedGuildNeed{}, GearGapMembers: []MEBand{}, FarmersNote: "info only"}
			}
			gv = &v
		}
		meState.Unlock()
		f.Guilds = nil
		f.Guild = gv
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(f)
	})
}

// guildOf: one guild's block from a stored feed (nil feed: not found). Called with meState locked.
func (f *MEFeed) guildOf(id string) (MEFeedGuild, bool) {
	if f == nil {
		return MEFeedGuild{}, false
	}
	g, ok := f.Guilds[id]
	return g, ok
}
