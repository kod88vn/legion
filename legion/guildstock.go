package main

// guildstock.go (economy run 8). Owner rules 2026-11-14 (RULES.md 3b):
//   - "guild leader needs to track the overall storage and member development so that there are supplies when members
//     need. Guild storage should have supplies of pots and gears that common members should have or specific class
//     build. Guild leaders also need dedicated farmers with high individual survivability to hunt items. DS hunters
//     are the best hunters with hit and run"
//   - "The teleport card must be prioritized ... The card should be high priority in the market" (Creamy Card)
//
// The guild's stock: guild storage needs the guild skill Guild Storage Expansion (level-1 guilds don't have it) and
// Legion has no Kafra-storage I/O yet, so the stock is what the guild's vendor characters hold in their open
// Prontera shops (healing parcels and surplus gear already flow there: parcel.go). Every guildStockEvery the round:
//   - needs: each online member's next build piece (buildgear.go nextBuildPiece, drop / trade pieces) and its open
//     want; HP potions for members under their survival minimum (potMin);
//   - stock: units of those items listed in the guild's shops; gaps = needed - stocked (healing: 10 per short member);
//   - farmers: up to guildFarmers members with the best solo survivability (Double Strafe Hunters / Snipers first -
//     hit and run - then sturdy melee) get a gap item that drops on a map safe for them as their money goal
//     (opportunity.go goals: the solo map picker farms it, the loot is parcelled to the guild shops) and say so in
//     guild chat;
//   - withdraw: a guild vendor hands healing items / potions to a guild mate who asks (#trade want) as a gift (economy.go
//     matcher, stock keepers), like the brewer's gifts.
// GET /api/guild-stock: per guild stock, needs, gaps and farmers.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"time"
)

const (
	guildStockEvery = 5 * time.Minute
	guildFarmers    = 2
	itemCreamyCard  = 4040 // Teleport (owner: every bot aims for it)
)

type guildStockRow struct {
	Guild     string                    `json:"guild"`
	Members   int                       `json:"members_online"`
	PotShort  int                       `json:"members_short_of_potions"`
	Stock     map[string]int            `json:"stock"` // every guild shop together
	ByShop    map[string]map[string]int `json:"stock_by_shop"`
	Needs     map[string]int            `json:"needs"`
	Gaps      []string                  `json:"gaps"`
	Farmers   []string                  `json:"farmers"`
	Trader    string                    `json:"trader"`            // the guild's merchant: sells surplus, buys what the stock lacks
	Storage   map[string]map[string]int `json:"storage,omitempty"` // guildstore.go: stock in the leader's / treasurer's Kafra storage (GD05)
	UpdatedAt time.Time                 `json:"updated_at"`
}

var gstock = struct {
	sync.Mutex
	next    time.Time
	rows    map[string]*guildStockRow
	farmers map[*Bot]time.Time // assigned until
	trader  map[string]string  // guild -> its trader's character name
	gapIDs  map[string][]int   // guild -> the items its stock lacks (traders buy these first)
}{rows: map[string]*guildStockRow{}, farmers: map[*Bot]time.Time{}, trader: map[string]string{}, gapIDs: map[string][]int{}}

// guildTrader: the guild's merchant character ("" none online)
func guildTrader(guild string) string {
	gstock.Lock()
	defer gstock.Unlock()
	return gstock.trader[guild]
}

// guildGap: item id is one the guild's stock lacks
func guildGap(guild string, id int) bool {
	gstock.Lock()
	defer gstock.Unlock()
	for _, x := range gstock.gapIDs[guild] {
		if x == id {
			return true
		}
	}
	return false
}

// guildStockDue: the snapshot round should fill the stock fields (nextPiece) this time
func guildStockDue(now time.Time) bool {
	gstock.Lock()
	defer gstock.Unlock()
	return !now.Before(gstock.next)
}

// soloScore (b.mu held): how well the character survives farming alone - Double Strafe Hunters / Snipers first (hit and
// run), then sturdy melee, then the rest; 0 for vendors, healers and characters under lv 30.
func (b *Bot) soloScore() int {
	lv := b.BaseLvOr()
	if lv < 30 || b.charRole() == "vendor" || b.isHealer() {
		return 0
	}
	switch b.Class {
	case 11, 4012: // Hunter, Sniper
		if b.skillLv(46) >= 5 && b.bowEquipped() {
			return 1000 + lv
		}
	case 7, 14, 15, 4008, 4015, 4016: // Knight, Crusader, Monk (+ trans)
		return 500 + lv
	}
	return lv
}

// guildStockRound: needs vs stock per guild; farmers for the gaps (no lock held; called by econRound after guildFund).
func guildStockRound(snaps []*econSnap, now time.Time) {
	gstock.Lock()
	if now.Before(gstock.next) {
		gstock.Unlock()
		return
	}
	gstock.next = now.Add(guildStockEvery)
	gstock.Unlock()
	byGuild := map[string][]*econSnap{}
	for _, s := range snaps {
		if s.guild != "" {
			byGuild[s.guild] = append(byGuild[s.guild], s)
		}
	}
	for g, ss := range byGuild {
		row := &guildStockRow{Guild: g, Stock: map[string]int{}, Needs: map[string]int{}, UpdatedAt: now}
		stock, need := map[int]int{}, map[int]int{}
		healStock := 0
		for _, s := range ss {
			for _, l := range s.shop {
				stock[l.ItemID] += l.Count
				if healItem(l.ItemID) {
					healStock += l.Count
				}
			}
			if s.keeper {
				continue
			}
			row.Members++
			if s.potShort {
				row.PotShort++
			}
			if s.nextPiece > 0 {
				need[s.nextPiece]++
			}
		}
		var gaps []stockGap
		for id, n := range need {
			row.Needs[itemName(id)] = n
			if have := stock[id]; have < n {
				gaps = append(gaps, stockGap{id, n - have, float64(n-have) * float64(priceOf(id))})
			}
		}
		for id, n := range stock {
			if n > 0 && (healItem(id) || need[id] > 0) {
				row.Stock[itemName(id)] += n
			}
		}
		if hn := row.PotShort * 10; hn > healStock {
			row.Needs["healing items"] = hn
			gaps = append(gaps, stockGap{itemWhitePotion, hn - healStock, 1e9}) // healing first (owner); farmed as herbs/food
		}
		sort.Slice(gaps, func(i, j int) bool { return gaps[i].v > gaps[j].v })
		for _, x := range gaps {
			if x.id == itemWhitePotion && x.v == 1e9 {
				row.Gaps = append(row.Gaps, fmt.Sprintf("healing items x%d", x.n))
				continue
			}
			row.Gaps = append(row.Gaps, fmt.Sprintf("%s x%d", itemName(x.id), x.n))
		}
		row.Farmers = assignGuildFarmers(g, ss, gaps2ids(gaps), now)
		// the guild's trader (owner 3b: "a guild should have a merchant to sell surplus and buy what the guild
		// needs"): its vendor with the biggest open shop, else any online vendor of the guild
		// run 10 (market mind: Silentdragon Shop / yukishot Shop flipped as trader): the trader stays the trader while
		// its shop is open; a new one is picked only when it is gone
		trader, best := "", -1
		gstock.Lock()
		prev := gstock.trader[g]
		gstock.Unlock()
		for _, s := range ss {
			if s.keeper && s.name == prev && len(s.shop) > 0 {
				trader, best = s.name, 1<<30
			}
		}
		for _, s := range ss {
			if s.keeper && len(s.shop) > best {
				trader, best = s.name, len(s.shop)
			}
		}
		// run 10: "stock" is every guild shop together; stock_by_shop says which shop holds what (the market mind read
		// the trader's row as its own cart)
		row.ByShop = map[string]map[string]int{}
		for _, s := range ss {
			for _, l := range s.shop {
				if l.Count > 0 && (healItem(l.ItemID) || need[l.ItemID] > 0) {
					if row.ByShop[s.name] == nil {
						row.ByShop[s.name] = map[string]int{}
					}
					row.ByShop[s.name][itemName(l.ItemID)] += l.Count
				}
			}
		}
		row.Trader = trader
		var names []string
		for _, s := range ss {
			names = append(names, s.name)
		}
		row.Storage = guildStoreView(names) // guildstore.go (GD05)
		gstock.Lock()
		gstock.rows[g] = row
		gstock.trader[g] = trader
		gstock.gapIDs[g] = gaps2ids(gaps)
		gstock.Unlock()
	}
}

type stockGap struct {
	id, n int
	v     float64
}

func gaps2ids(gs []stockGap) []int {
	var out []int
	for _, g := range gs {
		out = append(out, g.id)
	}
	return out
}

// healFarmItems: what a guild farmer hunts for a healing gap (drops that heal: herbs and food, best value first)
var healFarmItems = []int{itemWhiteHerb, itemYellowHerb, 517, itemRedHerb, 512, 515}

// assignGuildFarmers: the most survivable free solo members get a gap item as their money goal (max guildFarmers
// per guild, each for oppGoalLife). Returns the farmers' "name: item on map" lines. No lock held by the caller.
func assignGuildFarmers(guild string, ss []*econSnap, gapIDs []int, now time.Time) []string {
	var cands []*econSnap
	for _, s := range ss {
		if s.soloScore > 0 && !s.sameMapOnly && s.free {
			cands = append(cands, s)
		}
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].soloScore > cands[j].soloScore })
	var out []string
	busy := 0
	gstock.Lock()
	for b, t := range gstock.farmers {
		if now.After(t) {
			delete(gstock.farmers, b)
		}
	}
	for _, s := range ss {
		if t, ok := gstock.farmers[s.b]; ok && now.Before(t) {
			busy++
			out = append(out, s.name+" (farming)")
		}
	}
	gstock.Unlock()
	for _, s := range cands {
		if busy >= guildFarmers || len(gapIDs) == 0 {
			break
		}
		gstock.Lock()
		_, already := gstock.farmers[s.b]
		gstock.Unlock()
		if already {
			continue
		}
		b := s.b
		b.mu.Lock()
		var g *moneyGoal
		lv := b.BaseLvOr()
		for _, id := range gapIDs {
			ids := []int{id}
			if id == itemWhitePotion {
				ids = healFarmItems
			}
			for _, it := range ids {
				if glutted(it) {
					continue // market.go: plenty on sale already
				}
				ms, mob := b.fitFarms(pieceFarms(it, lv, 4), it)
				if len(ms) == 0 && id == itemWhitePotion {
					// healing drops come from low monsters (plants, Peco Pecos): fitFarms skips monsters far under a
					// lv40+ bot, but a guild farmer hunting herbs for the stock goes there on purpose (any safe map)
					for _, f := range pieceFarms(it, lv, 8) {
						if b.soloMapOK(f.Map) {
							ms, mob = []string{f.Map}, f.Mob
							break
						}
					}
				}
				if len(ms) > 0 {
					g = &moneyGoal{ItemID: it, Item: itemName(it), Mob: mob, Maps: ms, Until: now.Add(oppGoalLife)}
					break
				}
			}
			if g != nil {
				break
			}
		}
		if g != nil {
			opps.Lock()
			opps.goals[b] = g
			if opps.chase[g.ItemID] == nil {
				opps.chase[g.ItemID] = map[*Bot]time.Time{}
			}
			opps.chase[g.ItemID][b] = g.Until
			opps.n["guild farmers assigned"]++
			opps.Unlock()
			b.Goal = Goal{} // re-pick: the solo map picker takes the money goal's map (ai.go moneyFarmPick)
			b.event("market", "", fmt.Sprintf("guild farmer for %s: hunting %s on %s (%s) for the guild stock", guild, g.Item, g.Maps[0], g.Mob))
			say := pick("ill farm "+g.Item+" for the guild stock on "+g.Maps[0], "on it, getting "+g.Item+" for the stock",
				"heading to "+g.Maps[0]+" for "+g.Item+", will send them to the shop")
			go b.guildSay(say)
		}
		b.mu.Unlock()
		if g != nil {
			gstock.Lock()
			gstock.farmers[b] = g.Until
			gstock.Unlock()
			busy++
			out = append(out, fmt.Sprintf("%s: %s on %s", s.name, g.Item, g.Maps[0]))
		}
	}
	return out
}

func registerGuildStock(mux *http.ServeMux) {
	mux.HandleFunc("/api/guild-stock", func(w http.ResponseWriter, r *http.Request) {
		gstock.Lock()
		var rows []*guildStockRow
		for _, x := range gstock.rows {
			rows = append(rows, x)
		}
		gstock.Unlock()
		sort.Slice(rows, func(i, j int) bool { return rows[i].Guild < rows[j].Guild })
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(rows)
	})
}

// creamyWant (b.mu held): the Teleport card is a top gear goal for every bot from lv 15 (owner rule 3b): a B> want as
// soon as the purse above the gear reserve covers its bid (the price follows rarity: pricing.go counts every
// character as a buyer). Never NPC-sold (a card).
func (b *Bot) creamyWant() *Want {
	if b.BaseLvOr() < 15 || b.charRole() == "vendor" || b.hasCard(itemCreamyCard) {
		return nil
	}
	bid := b.wantBid(itemCreamyCard)
	if bid <= 0 || b.Zeny-b.gearReserve() < bid {
		return nil
	}
	if !b.creamyReady(bid) {
		return nil // teleslot.go: no slotted accessory to put it in (and no money for a Belt [1] too)
	}
	return &Want{ItemID: itemCreamyCard, Count: 1, Each: bid, Why: "teleport card"}
}

// ---------------------------------------------------------------- the trader funds gear turns (economy run 9)

// traderFundNext: trader / receiver -> next time it may give / receive
var traderFundNext = struct {
	sync.Mutex
	m map[string]time.Time
}{m: map[string]time.Time{}}

// traderFundRound (owner 3b: the guild merchant sells the surplus and buys what the guild needs; "nobody is left a
// weakling"): a guild trader with money above its working stock (traderKeep) mails a guild member the zeny its party
// gear turn still lacks when the party can't raise it ("party saving"). One gift per trader per 30 min, at most
// traderMaxGift each. No lock held by the caller.
const (
	traderKeep    = 10000
	traderMaxGift = 5000
)

func traderFundRound(snaps []*econSnap, now time.Time) {
	used := map[*econSnap]bool{}
	for _, r := range snaps {
		if r.gearShort <= 0 || r.guild == "" || used[r] {
			continue
		}
		need := (r.gearShort + 99) / 100 * 100
		if need > traderMaxGift {
			continue // a big piece: the party saves for it itself
		}
		traderFundNext.Lock()
		wait := now.Before(traderFundNext.m["r|"+r.name])
		traderFundNext.Unlock()
		if wait {
			continue
		}
		var best *econSnap
		traderFundNext.Lock()
		for _, d := range snaps {
			if d == r || used[d] || !d.keeper || !d.idle || d.guild != r.guild || d.needGuide ||
				d.zeny < traderKeep+mailCost(need) || now.Before(traderFundNext.m["d|"+d.name]) {
				continue
			}
			if best == nil || d.zeny > best.zeny {
				best = d
			}
		}
		traderFundNext.Unlock()
		if best == nil {
			continue
		}
		if mailTraderFund(best.b, best.name, r.name, need) {
			used[r], used[best] = true, true
			traderFundNext.Lock()
			traderFundNext.m["d|"+best.name] = now.Add(30 * time.Minute)
			traderFundNext.m["r|"+r.name] = now.Add(30 * time.Minute)
			traderFundNext.Unlock()
		}
	}
}

// mailTraderFund: the guild trader mails a member the rest of its gear turn (mail.go, 2% fee)
func mailTraderFund(donor *Bot, dName, rName string, amount int) bool {
	donor.mu.Lock()
	defer donor.mu.Unlock()
	if !donor.Online || donor.charName() != dName || donor.mkt != nil || donor.trip != nil || donor.kafra != nil ||
		donor.Dead || donor.Zeny < traderKeep+mailCost(amount) || (donor.vend != nil && donor.vend.Opened) {
		return false
	}
	typing := donor.P.Typing
	return donor.mailZeny(rName, amount, "guild gear money", "from the guild shop, get your gear", func(ok bool, note string) {
		if !ok {
			return
		}
		econCount("guild trader: gear turn funded")
		donor.event("market", rName, fmt.Sprintf("guild trader mailed %dz to %s for its gear turn", amount, rName))
		go func() {
			time.Sleep(time.Duration(2+crand(3)) * time.Second)
			donor.whisper(rName, styled(typing, pick(fmt.Sprintf("mailed u %dz from the guild shop for ur gear", amount),
				"check ur mail, guild money for ur gear", fmt.Sprintf("sent %dz, shop sales went well. go gear up", amount))))
		}()
	})
}
