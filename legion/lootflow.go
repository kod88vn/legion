package main

// lootflow.go (economy run 10). Owner rule 2026-11-14 (RULES.md 2 "Interesting loot"): useful loot (cards, gear, rare
// materials) goes
//   1. to the looter, if it fits its own build (it wears / compounds it, or it is its next gear step: keepsGear /
//      keepsCard / refineKeep) - that never enters this flow;
//   2. else to a party member who can use it ("anyone need a Mantis Card?" - "me!" - "np take it"), by RODEX;
//   3. else to a guild mate who can use it (guild chat), by RODEX;
//   4. else into the guild's collection: the guild trader's shop (guildstock.go: the guild's stock is what its vendors
//      list), by RODEX;
//   5. only then it is sold (econSpare: #trade, mail trades, the vendor parcels).
// A bot with no party and no guild keeps one copy of each and sells only the duplicates.
// One step per item per lootStep; one offer per bot per round. Counters: /api/economy loot_flow.

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

const (
	lfParty = iota
	lfGuild
	lfStock
	lfSell
)

const (
	lootStep  = 2 * time.Minute // between the steps of one item (party -> guild -> stock -> sell)
	lootRound = time.Minute
)

type lootRec struct {
	stage int
	at    time.Time
}

var lootFlow = struct {
	sync.Mutex
	recs  map[string]map[int]*lootRec // giver char -> item id -> where it is in the flow
	given map[string]time.Time        // "recipient|item" -> gifted (no second copy for 30 min)
	said  map[string]time.Time        // giver -> last offer line
	kept  map[string]bool             // "char|item" counted as kept by its looter
	next  time.Time
}{recs: map[string]map[int]*lootRec{}, given: map[string]time.Time{}, said: map[string]time.Time{}, kept: map[string]bool{}}

var lootCounts = map[string]*int64{"kept": new(int64), "party": new(int64), "guild": new(int64), "stock": new(int64), "sold": new(int64)}

func lootCount(k string) {
	if p := lootCounts[k]; p != nil {
		atomic.AddInt64(p, 1)
	}
	econCount("loot flow: " + k)
}

// lootFlowReport: /api/economy loot_flow (this run; the cumulative ones are in counts "loot flow: ...")
func lootFlowReport() map[string]int64 {
	out := map[string]int64{}
	for k, p := range lootCounts {
		out[k] = atomic.LoadInt64(p)
	}
	return out
}

// interestingLoot: an item the flow handles - cards, real gear (not junk, identified, +0, no cards), rare materials
func (b *Bot) interestingLoot(it *InvItem) bool {
	x := items[it.ItemID]
	if x == nil || it.Count <= 0 || it.Equipped || it.Unidentified {
		return false
	}
	switch {
	case x.Type == "Card":
		return true
	case valuableEtc[x.ID]:
		return true
	case x.Type == "Weapon" || x.Type == "Armor":
		return it.Refine == 0 && it.Cards == [4]int{} && !b.junkGear(it, x)
	}
	return false
}

// ownUse: the looter itself uses it (step 1)
func (b *Bot) ownUse(x *Item) bool {
	switch {
	case x.Type == "Card":
		return b.keepsCard(x.ID)
	case x.Type == "Weapon" || x.Type == "Armor":
		return b.keepsGear(x)
	}
	return b.refineKeep(x.ID) > 0
}

// lootKeptOnce counts an item the looter keeps for itself, once per character and item
func lootKeptOnce(name string, id int) {
	lootFlow.Lock()
	k := name + "|" + fmt.Sprint(id)
	_, done := lootFlow.kept[k]
	if !done {
		lootFlow.kept[k] = true
	}
	lootFlow.Unlock()
	if !done {
		lootCount("kept")
	}
}

// lootFilter (b.mu held): the spare list without what is still going through the loot flow
func (b *Bot) lootFilter(sp []holding, solo bool) []holding {
	out := sp[:0]
	for _, h := range sp {
		if h.Count = b.lootSellable(h.ItemID, h.Count, solo); h.Count > 0 {
			out = append(out, h)
		}
	}
	return out
}

// lootSellable (b.mu held): how many of n spare units of the item may be sold now (econSpare, parcelItems)
func (b *Bot) lootSellable(id, n int, solo bool) int {
	x := items[id]
	if x == nil || n <= 0 || !(x.Type == "Card" || valuableEtc[id] || x.Type == "Weapon" || x.Type == "Armor") {
		return n
	}
	if solo {
		// no party, no guild: one copy is kept, the duplicates are sold
		have := b.countItem(id)
		return max(0, min(n, have-1))
	}
	lootFlow.Lock()
	defer lootFlow.Unlock()
	if r := lootFlow.recs[b.charName()][id]; r != nil && r.stage >= lfSell {
		return n
	}
	return 0
}

// lootStage (b.mu held): the flow stage of an item (lfParty when new)
func lootStageOf(name string, id int) int {
	lootFlow.Lock()
	defer lootFlow.Unlock()
	if r := lootFlow.recs[name][id]; r != nil {
		return r.stage
	}
	return lfParty
}

type lootCand struct {
	idx, id int
	stage   int
}

// lootFlowRound: one step of the flow for every bot (econRound, no locks held).
func lootFlowRound(snaps []*econSnap, now time.Time) {
	lootFlow.Lock()
	if now.Before(lootFlow.next) {
		lootFlow.Unlock()
		return
	}
	lootFlow.next = now.Add(lootRound)
	for k, t := range lootFlow.given {
		if now.Sub(t) > 30*time.Minute {
			delete(lootFlow.given, k)
		}
	}
	lootFlow.Unlock()
	byGuild := map[string][]*econSnap{}
	for _, s := range snaps {
		if s.guild != "" && !s.keeper {
			byGuild[s.guild] = append(byGuild[s.guild], s)
		}
	}
	for _, s := range snaps {
		if s.keeper || s.brewer && s.b.charRole() == "vendor" {
			continue
		}
		lootFlowBot(s, byGuild[s.guild], now)
	}
}

func lootFlowBot(s *econSnap, guildMates []*econSnap, now time.Time) {
	b := s.b
	p := partyOf(b)
	b.mu.Lock()
	if !b.Online || b.Dead || b.mailBusy() || !b.invReady() || b.Zeny < mailItemFee+200 || b.charName() != s.name {
		b.mu.Unlock()
		return
	}
	name := s.name
	var cands []lootCand
	seen := map[int]bool{}
	for idx, it := range b.inv {
		if seen[it.ItemID] || !b.interestingLoot(it) {
			continue
		}
		seen[it.ItemID] = true
		x := items[it.ItemID]
		if b.ownUse(x) && b.countItem(it.ItemID) <= 1 {
			lootKeptOnce(s.name, it.ItemID) // step 1: its own (a second copy goes on through the flow)
			continue
		}
		cands = append(cands, lootCand{idx: idx, id: it.ItemID})
	}
	b.mu.Unlock()
	if len(cands) == 0 {
		return
	}
	solo := p == nil && s.guild == ""
	lootFlow.Lock()
	recs := lootFlow.recs[name]
	if recs == nil {
		recs = map[int]*lootRec{}
		lootFlow.recs[name] = recs
	}
	for id := range recs { // items gone from the bag leave the flow
		if !seen[id] {
			delete(recs, id)
		}
	}
	var pick *lootCand
	for i := range cands {
		c := &cands[i]
		r := recs[c.id]
		if r == nil {
			r = &lootRec{stage: lfParty, at: now.Add(-lootStep)}
			recs[c.id] = r
		}
		c.stage = r.stage
		if r.stage < lfSell && now.Sub(r.at) >= lootStep && pick == nil {
			pick = c
		}
	}
	lootFlow.Unlock()
	if solo || pick == nil {
		return
	}
	x := items[pick.id]
	advance := func(to int) {
		lootFlow.Lock()
		if r := lootFlow.recs[name][pick.id]; r != nil {
			r.stage, r.at = to, now
		}
		lootFlow.Unlock()
		if to == lfSell {
			lootCount("sold")
		}
	}
	switch pick.stage {
	case lfParty:
		if p != nil {
			for _, m := range p.Members {
				if m == b || !lootCanUse(m, x, now) {
					continue
				}
				if lootGive(b, m, m.charName(), pick, x, "party", now) {
					return
				}
			}
		}
		advance(lfGuild)
	case lfGuild:
		for _, g := range guildMates {
			if g.b == b || (p != nil && partyOf(g.b) == p) || !lootCanUse(g.b, x, now) {
				continue
			}
			if lootGive(b, g.b, g.name, pick, x, "guild", now) {
				return
			}
		}
		advance(lfStock)
	case lfStock:
		if t := guildTrader(s.guild); t != "" && t != name && priceOf(x.ID) >= 4*mailItemFee {
			if lootGive(b, nil, t, pick, x, "stock", now) {
				return
			}
		}
		advance(lfSell)
	}
}

// lootCanUse: the character would wear / compound it, and wasn't just given one (takes m.mu)
func lootCanUse(m *Bot, x *Item, now time.Time) bool {
	m.mu.Lock()
	ok := m.Online && !m.Dead && m.countItem(x.ID) == 0 && m.ownUse(x) && (x.Type != "Weapon" && x.Type != "Armor" || m.canEquip(x))
	mn := m.charName()
	m.mu.Unlock()
	if !ok {
		return false
	}
	lootFlow.Lock()
	defer lootFlow.Unlock()
	_, recent := lootFlow.given[mn+"|"+fmt.Sprint(x.ID)]
	return !recent
}

// lootGive mails one unit to the recipient with the chat lines of the step (no locks held)
func lootGive(b, rb *Bot, to string, c *lootCand, x *Item, kind string, now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	it := b.inv[c.idx]
	if it == nil || it.ItemID != c.id || it.Count <= 0 || it.Equipped || b.mailBusy() || b.Zeny < mailItemFee {
		return false
	}
	giver := b.charName()
	typing := b.P.Typing
	item := x.Name
	m := &mailOut{To: to, Title: item, Body: "for you", Items: []int{c.idx}, Counts: map[int]int{c.idx: 1}, stage: 1, at: now,
		onDone: func(ok bool, note string) { // giver's b.mu held
			if !ok {
				return
			}
			lootCount(kind)
			lootFlow.Lock()
			lootFlow.given[to+"|"+fmt.Sprint(c.id)] = time.Now()
			if r := lootFlow.recs[giver][c.id]; r != nil {
				r.at = time.Now() // a second copy waits a step before it moves on
			}
			lootFlow.Unlock()
			b.event("market", to, fmt.Sprintf("loot flow (%s): mailed %s to %s", kind, item, to))
		}}
	mailOuts.Store(b, m)
	go b.send(newPkt(0x0a08).str(to, 24).b)
	lootFlow.Lock()
	if r := lootFlow.recs[giver][c.id]; r != nil {
		r.at = now // a failed mail is not retried every round
	}
	lastSaid := lootFlow.said[giver]
	talk := now.Sub(lastSaid) > 4*time.Minute
	if talk {
		lootFlow.said[giver] = now
	}
	lootFlow.Unlock()
	if !talk || kind == "stock" {
		if kind == "stock" && talk {
			go b.guildSay(styled(typing, pick("putting a "+item+" in the guild stock, "+to+" has it",
				"sent "+to+" a "+item+" for the guild shop", item+" -> guild stock")))
		}
		return true
	}
	go func() {
		if lootTalkJob(b, rb, kind, item, to, typing) { // exchange.go: as a chat job (chat_jobs_on)
			return
		}
		ask := pick("anyone need a "+item+"?", "got a "+item+", anyone use it?", "who needs "+item+"?")
		say := b.partySay
		if kind == "guild" {
			say = b.guildSay
		}
		say(styled(typing, ask))
		time.Sleep(time.Duration(4+crand(5)) * time.Second)
		if rb != nil {
			rb.mu.Lock()
			rt := rb.P.Typing
			rb.mu.Unlock()
			reply := rb.partySay
			if kind == "guild" {
				reply = rb.guildSay
			}
			reply(styled(rt, pick("me! i can use that", "oh i need that", "me pls", "yes pls, mine is worse")))
		}
		time.Sleep(time.Duration(3+crand(4)) * time.Second)
		say(styled(typing, pick("np take it, mailed", "np, sent it to u "+to, "here u go, check ur mail")))
	}()
	return true
}
