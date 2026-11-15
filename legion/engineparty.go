package main

// Engine parties (AY24, ayla-switch): a human-led party whose human is named in tuning engine_parties runs as a
// bot-led party instead. The human's own bot (the member pinned to the human's character) becomes the Leader through
// formPartyOpt, so every p.Human == "" path of the engine applies to it. Off by default (engine_parties ""): every
// other human-led party, and this one while the switch is off, keeps the human-led path (startHumanParty).
//
// Persistence: the core instance (instances.go) saves only engine parties, in parties_engine.json (botPartyFile).
// That is the one gate relaxed for the core instance; everything else the core instance does is unchanged.

import (
	"log"
	"strings"
	"time"
)

// inEngineParty: the bot is in a party that runs on the engine (AY24). Its members are pinned like a human party's and
// get farm goals the same way (lfg.go goalTick).
func inEngineParty(b *Bot) bool {
	p := partyOf(b)
	return p != nil && p.Engine
}

// goalPinSkips: a pinned bot gets no goal of its own, except a member of a human-led or engine party (pinned by it).
func goalPinSkips(b *Bot) bool {
	return b.pin != nil && !inHumanParty(b) && !inEngineParty(b)
}

// enginePartyOn: the human's party runs on the engine (tuning engine_parties).
func enginePartyOn(human string) bool {
	return human != "" && nameSet(tune().EngineParties)[strings.ToLower(human)]
}

// persistsBotParties: this instance saves and restores bot parties. The main instance always does (parties.json); the
// core instance does only while an engine party is switched on (parties_engine.json, engine parties only).
func persistsBotParties() bool {
	return ownsBotParties() || strings.TrimSpace(tune().EngineParties) != ""
}

// startEnginePartyFor forms the human's party bot-led: the member pinned to the human's character leads. Returns nil
// (nothing changed) when that member is not in the list or the party cannot be formed; the caller then falls back to
// the human-led party.
func startEnginePartyFor(human string, members []*Bot, chars []string, hold time.Duration) *Party {
	var leader *Bot
	var all []*Bot
	var allChars []string
	for i, m := range members {
		if leader == nil && strings.EqualFold(chars[i], human) {
			leader = m
			all = append([]*Bot{m}, all...)
			allChars = append([]string{chars[i]}, allChars...)
			continue
		}
		all = append(all, m)
		allChars = append(allChars, chars[i])
	}
	if leader == nil {
		return nil
	}
	for _, m := range all {
		if p := partyOf(m); p != nil {
			disband(p)
		}
	}
	var rest []*Bot
	for _, m := range all {
		if m != leader {
			rest = append(rest, m)
		}
	}
	p := formPartyOpt(leader, rest, "", false, func(p *Party) { p.Engine = true })
	if p == nil {
		return nil
	}
	for _, m := range p.Members {
		c := ""
		for j, a := range all {
			if a == m {
				c = allChars[j]
			}
		}
		m.mu.Lock()
		m.pin = &Pin{Char: c, Hold: time.Now().Add(hold)}
		wrongChar := m.Online && c != "" && !strings.EqualFold(m.charName(), c)
		m.mu.Unlock()
		if wrongChar || (hold > 0 && m.Online) {
			m.kickSession()
		}
		select {
		case m.wake <- struct{}{}:
		default:
		}
		mem := memoryOf(m.P)
		for _, o := range p.Members {
			if o != m {
				mem.meet(o.charName(), "partymate", "", false)
			}
		}
	}
	log.Printf("engine party %s formed for %s: leader %s, %d members", p.Name, human, leader.charName(), len(p.Members)-1)
	return p
}
