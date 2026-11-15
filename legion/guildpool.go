package main

// guildpool.go (economy batch 21 step 4). Owner 2026-11-14: "if it's an item that only comes from boss, an entire
// guild must contribute in to purchase."
//
// pricing.go prices boss-only goods (every spawning source a boss/MVP) at a guild's pooled wealth. Here the guild
// pools for one: when such an item is on offer (a #trade spare or a shop) and suits an online member's build, the
// guild picks the member it helps most (highest level among those whose build wants it - the guild leader's call,
// made by the engine), and guild mates mail it zeny (up to 5,000z each, once an hour, keeping 1,500z) until its purse
// covers the lowest ask; the member then posts a want at that price ("guild buy") and the matcher (guild mates
// first) or a shop sells it. One pool per guild, 3 hours at most. The buyer's pooled money is kept from other gear
// buys (gearReserve). /api/economy "guild_pools".

import (
	"fmt"
	"sort"
	"sync"
	"time"
)

type guildPool struct {
	Guild   string    `json:"guild"`
	Buyer   string    `json:"buyer"`
	ItemID  int       `json:"item_id"`
	Item    string    `json:"item"`
	Price   int       `json:"price"`
	Raised  int       `json:"raised"`
	Started time.Time `json:"started"`
	bot     *Bot
}

var guildPools = struct {
	sync.Mutex
	m map[string]*guildPool
}{m: map[string]*guildPool{}}

// poolOf: the active pool this bot buys for (nil if none)
func poolOf(b *Bot) *guildPool {
	guildPools.Lock()
	defer guildPools.Unlock()
	for _, p := range guildPools.m {
		if p.bot == b {
			return p
		}
	}
	return nil
}

// guildPoolWant: the pool's buyer asks for the item once its purse covers the price (b.mu held)
func (b *Bot) guildPoolWant() *Want {
	p := poolOf(b)
	if p == nil || b.countItem(p.ItemID) > 0 || b.Zeny < p.Price {
		return nil
	}
	return &Want{ItemID: p.ItemID, Count: 1, Each: p.Price, Why: "guild buy"}
}

// buildWants: the build (plan) of this character wants the item at its level (b.mu held)
func (b *Bot) buildWants(id int) bool {
	p := b.plan()
	x := items[id]
	if p == nil || x == nil {
		return false
	}
	if x.Type == "Card" {
		return buildCardValue(p, id) >= 4
	}
	return b.canEquip(x) && suitsBuild(p.ID, b.BaseLvOr(), id)
}

// guildPoolRound: start, feed and end guild pools (econRound, no lock held)
func guildPoolRound(snaps []*econSnap, now time.Time) {
	// boss-only goods on offer, and their lowest ask
	dynPrices.RLock()
	rows := dynPrices.rows
	dynPrices.RUnlock()
	offered := map[int]int{}
	for _, r := range rows {
		if !r.BossOnly {
			continue
		}
		if la := boardLowAsk(r.ID); la > 0 {
			offered[r.ID] = la
		}
	}
	byGuild := map[string][]*econSnap{}
	for _, s := range snaps {
		if s.guild != "" {
			byGuild[s.guild] = append(byGuild[s.guild], s)
		}
	}
	guildPools.Lock()
	cur := map[string]*guildPool{}
	for g, p := range guildPools.m {
		cur[g] = p
	}
	guildPools.Unlock()
	for g, p := range cur { // end: bought, gone, timed out, buyer offline (bot locks outside guildPools)
		done := p.bot.countItemLocked(p.ItemID) > 0
		_, still := offered[p.ItemID]
		if done || !still || now.Sub(p.Started) > 3*time.Hour || !p.bot.isOnlineAs(p.Buyer) {
			why := "item gone"
			switch {
			case done:
				why = "bought"
			case now.Sub(p.Started) > 3*time.Hour:
				why = "timed out"
			case still:
				why = "buyer offline"
			}
			econCount("guild pool: ended (" + why + ")")
			guildPools.Lock()
			delete(guildPools.m, g)
			guildPools.Unlock()
			delete(cur, g)
		}
	}
	active := cur

	ids := make([]int, 0, len(offered))
	for id := range offered {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	for g, members := range byGuild {
		if active[g] != nil || len(ids) == 0 {
			continue
		}
		var buyer *econSnap
		item := 0
		for _, id := range ids {
			for _, s := range members {
				if !s.free || s.b.charRole() == "vendor" {
					continue
				}
				s.b.mu.Lock()
				ok := s.b.buildWants(id) && s.b.countItem(id) == 0
				s.b.mu.Unlock()
				if ok && (buyer == nil || s.lv > buyer.lv) {
					buyer, item = s, id
				}
			}
			if buyer != nil {
				break
			}
		}
		if buyer == nil {
			continue
		}
		p := &guildPool{Guild: g, Buyer: buyer.name, ItemID: item, Item: itemName(item), Price: offered[item], Started: now, bot: buyer.b}
		guildPools.Lock()
		guildPools.m[g] = p
		guildPools.Unlock()
		active[g] = p
		econCount("guild pool: started")
		buyer.b.event("market", "", fmt.Sprintf("the guild is pooling zeny so I can buy %s (%dz)", p.Item, p.Price))
	}
	// feed: one donor per pool per round, by mail
	for g, p := range active {
		if p.bot.zenyLocked() >= p.Price {
			continue
		}
		need := p.Price - p.bot.zenyLocked() + 500
		var best *econSnap
		guildFundNext.Lock()
		for _, d := range byGuild[g] {
			if d.b == p.bot || !d.free || d.gift < 1000 || now.Before(guildFundNext.m[d.name]) {
				continue
			}
			if best == nil || d.gift > best.gift {
				best = d
			}
		}
		guildFundNext.Unlock()
		if best == nil {
			continue
		}
		amount := min(best.gift/2, 5000, need) / 100 * 100
		if amount < 300 {
			continue
		}
		pool := p
		donor := best
		donor.b.mu.Lock()
		ok := donor.b.Online && donor.b.charName() == donor.name && donor.b.mkt == nil && donor.b.trip == nil &&
			donor.b.Zeny >= mailCost(amount)+1500 &&
			donor.b.mailZeny(pool.Buyer, amount, "guild buy", "for the "+pool.Item+", from the guild", func(ok bool, note string) {
				if !ok {
					return
				}
				guildPools.Lock()
				pool.Raised += amount
				guildPools.Unlock()
				econCount("guild pool: mailed")
				donor.b.event("market", pool.Buyer, fmt.Sprintf("mailed %dz to %s for the guild's %s", amount, pool.Buyer, pool.Item))
			})
		donor.b.mu.Unlock()
		if ok {
			guildFundNext.Lock()
			guildFundNext.m[donor.name] = now.Add(60 * time.Minute)
			guildFundNext.Unlock()
		}
	}
}

func (b *Bot) countItemLocked(id int) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.countItem(id)
}

func (b *Bot) zenyLocked() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.Zeny
}

func (b *Bot) isOnlineAs(name string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.Online && b.charName() == name
}

// guildPoolsReport: for /api/economy
func guildPoolsReport() []guildPool {
	guildPools.Lock()
	defer guildPools.Unlock()
	var out []guildPool
	for _, p := range guildPools.m {
		out = append(out, *p)
	}
	return out
}
