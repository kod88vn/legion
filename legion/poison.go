package main

// poison.go: status ailments (QA 2026-11-14 03:02-03:10: mjolnir_06 was the death hotspot, 17 of 51 deaths in 8 min,
// Poison Spores everywhere). In rAthena SC_POISON drains HP down to 25% and stops natural HP regen, so a poisoned bot
// that sits to "rest" never heals and gets picked off at 25% by the next aggressive Spore. The server tells every
// client in sight about it with ZC_STATE_CHANGE3 0x0229 (id.L bodyState.W healthState.W effectState.L pk.B):
// healthState (opt2) bit 0x0001 = poisoned (0x0100 = deadly poison). Confirmed in a 45 s capture of map traffic
// (poison.pcap): 0x0229 with opt2 & 1 for Vex_kun (Acolyte lv36 on pay_fild08, Poporing) and Envenomed monsters.
//   - track opt2 for ourselves and every unit in sight (Entity.Opt2)
//   - poisoned: never sit (it doesn't heal); a Green Potion / Green Herb / Panacea / Royal Jelly, else our own Cure
//     (acolyte line), else ask a party Cure-caster nearby; an HP potion before HP sinks to the poison floor; with no
//     potion and no cure left, a Butterfly Wing out at low HP
//   - acolytes cast Cure (AL_CURE 35) on poisoned party members in sight (healFirst)
//   - bots hunting maps with poisoners (mob_skill_db NPC_POISON / NPC_POISONATTACK), or poisoned in the last 2 h, carry
//     a few Green Potions (every Tool Dealer#Extended_* sells them: Dealer_Update.txt)
//   - solo bots with no cure pick poisoner-heavy guide maps less often

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	alCure        = 35
	opt2Poison    = 0x0001
	opt2DPoison   = 0x0100
	itemGreenPot  = 506
	itemGreenHerb = 511
	itemPanacea   = 525
	itemRoyalJel  = 526
	greenTarget   = 4                // Green Potions to carry where poison is a risk
	poisonMemory  = 2 * time.Hour    // "recently poisoned": keep a few cures in the bag
	poisonStale   = 90 * time.Second // no 0x0229 refresh for this long: assume it wore off (packet missed)
)

// poisoners: mob IDs with a poisoning skill in db/re/mob_skill_db.txt
var poisoners = map[int]bool{}

func loadMobSkillPoison(path string) {
	f, err := os.Open(path)
	if err != nil {
		log.Printf("mob skill db: %v (no poisoner list)", err)
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		l := sc.Text()
		if strings.HasPrefix(l, "//") || !(strings.Contains(l, "@NPC_POISON,") || strings.Contains(l, "@NPC_POISONATTACK,")) {
			continue
		}
		if id, err := strconv.Atoi(strings.SplitN(l, ",", 2)[0]); err == nil {
			poisoners[id] = true
		}
	}
}

// poisonShare: the share of a map's non-boss spawns that can poison (aggressive ones count double, max 1)
func poisonShare(m string) float64 {
	tot, p := 0, 0
	for _, s := range spawnsByMap[m] {
		mb := mobs[s.MobID]
		if mb == nil || mb.Boss || s.Count <= 0 {
			continue
		}
		tot += s.Count
		if poisoners[s.MobID] {
			p += s.Count
			if mb.Aggressive {
				p += s.Count
			}
		}
	}
	if tot == 0 {
		return 0
	}
	return min(1, float64(p)/float64(tot))
}

// statusRecv (b.mu held): 0x0229 ZC_STATE_CHANGE3 for us or a unit in sight.
func (b *Bot) statusRecv(pk []byte, now time.Time) {
	if len(pk) < 15 {
		return
	}
	id, opt2 := le.Uint32(pk[2:]), le.Uint16(pk[8:])
	if id == b.aid {
		was := b.poisoned(now)
		b.opt2, b.opt2At = opt2, now
		b.opt1 = le.Uint16(pk[6:])                        // stone/freeze/stun/sleep (escapeitem.go: no wing then)
		b.option, b.optionSeen = le.Uint32(pk[10:]), true // peco.go: riding bit
		if b.poisoned(now) {
			b.poisonSeen = now
			if !was {
				b.poisonSince = now
				b.remember("got poisoned on %s", b.MapName)
			}
		}
		return
	}
	if e := b.ents[id]; e != nil {
		e.Opt2, e.Opt2At = opt2, now
	}
}

func (b *Bot) poisoned(now time.Time) bool {
	return b.opt2&(opt2Poison|opt2DPoison) != 0 && now.Sub(b.opt2At) < poisonStale && !b.Dead
}

func (e *Entity) poisoned(now time.Time) bool {
	return e.Opt2&(opt2Poison|opt2DPoison) != 0 && now.Sub(e.Opt2At) < poisonStale
}

// cureIndex: a carried item that cures poison (Green Herb first: lightest and cheapest)
func (b *Bot) cureIndex() int {
	for _, id := range []int{itemGreenHerb, itemGreenPot, itemPanacea, itemRoyalJel} {
		if idx := b.invIndex(id); idx >= 0 {
			return idx
		}
	}
	return -1
}

func (b *Bot) cureCount() int {
	n := 0
	for _, it := range b.inv {
		if it.ItemID == itemGreenHerb || it.ItemID == itemGreenPot || it.ItemID == itemPanacea || it.ItemID == itemRoyalJel {
			n += it.Count
		}
	}
	return n
}

func (b *Bot) hasCureSkill() bool {
	si := b.skills[alCure]
	return si != nil && si.Lv > 0
}

// canCure: the bot can rid itself of poison (Cure, or a cure item)
func (b *Bot) canCure() bool { return b.hasCureSkill() || b.cureCount() > 0 }

// partyCurerHere (b.mu held): a party mate with Cure (acolyte line) on our map within ~12 cells.
func (b *Bot) partyCurerHere(now time.Time) bool {
	p := partyOf(b)
	if p == nil {
		return false
	}
	_, _, members := b.partyView(p, now)
	for _, s := range members {
		if s.Online && s.Support && s.Map == b.MapName && s.Name != b.charName() && dist(b.X, b.Y, s.X, s.Y) <= 12 {
			return true
		}
	}
	return false
}

// greensWanted: Green Potions to buy from this buy list (b.mu held) for a bot that hunts among poisoners or was
// poisoned recently and has no Cure; within the budget and the potion weight room.
func (b *Bot) greensWanted(pk []byte, budget int) (int, int) {
	if b.hasCureSkill() {
		return 0, 0
	}
	m := b.Goal.Map
	if p := partyOf(b); p != nil && p.Map != "" {
		m = p.Map
	}
	if poisonShare(m) < 0.1 && (b.poisonSeen.IsZero() || time.Since(b.poisonSeen) > poisonMemory) {
		return 0, 0
	}
	need := greenTarget - b.cureCount()
	if need <= 0 {
		return 0, 0
	}
	pr := listedPrice(pk, itemGreenPot)
	if pr <= 0 || budget <= 0 {
		return 0, 0
	}
	return max(0, min(need, budget/pr, b.potRoom(itemGreenPot))), pr
}

// poisonGuard (b.mu held): runs right after survive() for every bot. While poisoned it keeps the bot standing (sitting
// doesn't heal), cures (item, own Cure, a party priest), drinks before the poison floor and leaves when it can't
// recover. True while it owns the bot this tick.
func (b *Bot) poisonGuard(gm *GameMap, moving bool, now time.Time) bool {
	if b.Dead || b.MaxHP <= 0 || !b.poisoned(now) {
		return false
	}
	hp := b.hpFrac()
	b.wantSit = false
	if b.Sitting && now.Sub(b.sitSentAt) > time.Second {
		b.sitSentAt = now
		go b.stand()
	}
	if now.Before(b.cureAt) {
		return false
	}
	if idx := b.cureIndex(); idx >= 0 {
		b.cureAt = now.Add(1500 * time.Millisecond)
		go b.useItem(idx)
		if now.After(b.buffAt["curesay"]) {
			b.buffAt["curesay"] = now.Add(5 * time.Minute)
			b.event("info", "", fmt.Sprintf("poisoned on %s: drinking a cure (%d left)", b.MapName, b.cureCount()-1))
		}
		return false
	}
	if si := b.skills[alCure]; si != nil && si.Lv > 0 && b.SP >= max(si.SP, 15) && now.After(b.skillReady) && !b.Sitting {
		b.skillReady, b.cureAt = now.Add(900*time.Millisecond), now.Add(1500*time.Millisecond)
		b.setState("curing myself (poisoned)")
		go b.useSkill(alCure, si.Lv, b.aid)
		return true
	}
	curer := b.partyCurerHere(now)
	if curer && now.After(b.buffAt["cureask"]) {
		b.buffAt["cureask"] = now.Add(30 * time.Second)
		go b.partySay(styled(b.P.Typing, pick("poisoned, cure pls", "cure me pls, poisoned", "poison :( cure?")))
	}
	// poison stops at 25% HP: drink before that, so the next hit doesn't land on a quarter of our HP
	if hp < 0.4 && now.After(b.potAt) {
		if idx := b.potionIndex(); idx >= 0 {
			b.potAt = now.Add(time.Second)
			go b.useItem(idx)
			b.setState(fmt.Sprintf("poisoned (%d%%): drinking a potion", int(hp*100)))
			return false
		}
	}
	// nothing to cure or heal with and HP low: town is better than waiting at 25% among aggressive poisoners
	if hp < 0.35 && b.potionCount() == 0 && !curer && now.Sub(b.poisonSince) > 6*time.Second && !isTown(b.MapName) {
		if idx := b.invIndex(itemButterflyWing); idx >= 0 {
			b.cureAt = now.Add(5 * time.Second)
			b.wingEvent(itemButterflyWing, "poisoned, no cure", now)
			go b.useItem(idx)
			b.setState("poisoned with no cure: back to town")
			b.remember("left %s poisoned with nothing to cure it", b.MapName)
			return true
		}
	}
	return false
}

// cureParty (b.mu held): an acolyte-line bot with Cure casts it on a poisoned party member in sight (or itself).
// Called from healFirst after the heal check, so a heal always comes first.
func (b *Bot) cureParty(now time.Time) bool {
	si := b.skills[alCure]
	if si == nil || si.Lv == 0 || now.Before(b.skillReady) || b.SP < max(si.SP, 15) {
		return false
	}
	p := partyOf(b)
	if p == nil {
		return false
	}
	_, _, members := b.partyView(p, now)
	for _, s := range members {
		if !s.Online || s.Map != b.MapName || s.Name == b.charName() {
			continue
		}
		id := b.entityID(s.Name)
		e := b.ents[id]
		if id == 0 || e == nil || !e.poisoned(now) || dist(b.X, b.Y, e.X, e.Y) > healRange {
			continue
		}
		if b.Sitting { // no casting while seated
			if now.Sub(b.sitSentAt) > 700*time.Millisecond {
				b.sitSentAt, b.wantSit = now, false
				go b.stand()
			}
			b.setState("standing up to cure " + s.Name)
			return true
		}
		b.skillReady = now.Add(900 * time.Millisecond)
		e.Opt2At = now.Add(-poisonStale + 3*time.Second) // don't re-cast before the server's answer (0x0229) arrives
		b.setState("curing " + s.Name + " (poisoned)")
		go b.useSkill(alCure, si.Lv, id)
		return true
	}
	return false
}
