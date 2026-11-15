package main

// Starveil's merchant trader (owner rule 2026-11-14: every guild has a merchant trader who vends the guild's surplus
// at the Prontera south gate and buys what the guild needs; zeny to the guild fund). The bot guilds use vendor-role
// characters; Starveil filled its 16 slots with fighters, so its trader is a member that already has the Merchant
// skills - a Blacksmith/Merchant/Alchemist with Vending and Pushcart, not one of Ayla's party friends (they play with
// her) - running shop SHIFTS between farming sessions, like a real player who farms, then sits down to sell:
//   - every traderFarmFor of farming (counted from its first login) it goes to Prontera, opens its shop
//     (vend.go vendThink: pitch, cart, prices) for traderShiftFor, then closes it and farms again (~80% farming);
//   - a shift starts only while it is not in a party (it finishes the party's goal first) and has goods (canVend);
//   - while it is the trader, economy.go counts it as a guild keeper, so guildstock.go picks it as Starveil's trader
//     (stock, buys for the guild's gaps, gifts of healing items to guild mates).
// A vendor-role member (a shop alt that joins later) is preferred over a fighter: it vends all the time.

import (
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	traderFarmFor   = 3 * time.Hour
	traderShiftFor  = 40 * time.Minute
	traderPickEvery = time.Minute
)

var guildTrader2 = struct {
	sync.Mutex
	at   time.Time
	name string // Starveil's trader character
	busy bool   // a refresh is running
}{}

// traderCandidate (b.mu held): a Starveil member (not a party friend) with Vending and Pushcart.
func (b *Bot) traderCandidate() bool {
	return humanGuildMember(b) && !b.humanPartyFriend() && family(b.Class) == famMerchant &&
		b.skillLv(mcVending) > 0 && b.skillLv(mcPushcart) > 0
}

// humanGuildTraderName: Starveil's trader - a vendor-role member first, then the highest-level candidate; sticky
// while it stays a candidate online. Re-picked at most once a minute over this instance's bots.
// Callers often hold their own b.mu (ai.go, economy.go), so the pick never runs inline: a stale cache starts one
// background refresh (it locks every bot) and the caller gets the last pick.
func humanGuildTraderName(now time.Time) string {
	guildTrader2.Lock()
	defer guildTrader2.Unlock()
	if now.Sub(guildTrader2.at) >= traderPickEvery && !guildTrader2.busy {
		guildTrader2.busy = true
		go refreshGuildTrader()
	}
	return guildTrader2.name
}

func refreshGuildTrader() {
	bots, _ := allBots.Load().([]*Bot)
	type cand struct {
		name   string
		vendor bool
		lv     int
	}
	var cs []cand
	var cur *cand
	for _, b := range bots {
		b.mu.Lock()
		if b.Online && b.traderCandidate() {
			cs = append(cs, cand{b.charName(), b.charRole() == "vendor", b.BaseLvOr()})
		}
		b.mu.Unlock()
	}
	guildTrader2.Lock()
	defer guildTrader2.Unlock()
	guildTrader2.at, guildTrader2.busy = time.Now(), false
	for i := range cs {
		if strings.EqualFold(cs[i].name, guildTrader2.name) {
			c := cs[i] // a copy: cs is sorted below
			cur = &c
		}
	}
	sort.Slice(cs, func(i, j int) bool {
		if cs[i].vendor != cs[j].vendor {
			return cs[i].vendor
		}
		return cs[i].lv > cs[j].lv
	})
	switch {
	case len(cs) == 0:
		guildTrader2.name = ""
	case cur != nil && (cur.vendor || !cs[0].vendor):
		// sticky: keep the current trader (only a vendor alt that joins takes over from a fighter)
	default:
		guildTrader2.name = cs[0].name
	}
}

// isHumanGuildTrader (b.mu held): this character is Starveil's trader.
func (b *Bot) isHumanGuildTrader(now time.Time) bool {
	n := humanGuildTraderName(now)
	return n != "" && strings.EqualFold(n, b.charName())
}

// traderShift (b.mu held; ai.go, before the vendor branch): true while the trader's shop shift runs - vendThink then
// owns the bot. Starts after traderFarmFor of farming when it isn't in a party and has goods; ends after
// traderShiftFor (the shop closes and it goes back to farming).
func (b *Bot) traderShift(inParty bool, now time.Time) bool {
	if b.charRole() == "vendor" || b.Goal.Set == "api" || !b.isHumanGuildTrader(now) {
		return false
	}
	if b.traderNext.IsZero() {
		b.traderNext = now.Add(traderFarmFor)
	}
	if !b.traderUntil.IsZero() {
		if now.Before(b.traderUntil) {
			return true
		}
		// shift over: close the shop, back to farming
		b.traderUntil = time.Time{}
		b.traderNext = now.Add(traderFarmFor)
		if b.vend != nil && b.vend.Opened {
			go b.send(newPkt(0x012e).b)
			b.vend.Opened, b.vend.TriedAt = false, now
			b.cartLoadAt = time.Time{}
		}
		releasePitch(b)
		b.Goal = Goal{}
		econCount("guild trader: shift ended")
		b.event("info", "", "closing my guild shop, back to farming")
		return false
	}
	if now.Before(b.traderNext) || inParty || !b.canVend() {
		return false
	}
	b.traderUntil = now.Add(traderShiftFor)
	econCount("guild trader: shift started")
	b.event("info", "", "Starveil trader: opening the guild shop at the south gate for a while")
	if line := pick("off to sell guild stuff at south gate for a bit", "opening the guild shop in pront, brb farming later",
		"vending for the guild for a while, bring me ur spare loot"); line != "" {
		go b.guildSay(styled(b.P.Typing, line))
	}
	return true
}
