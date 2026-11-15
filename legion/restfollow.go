package main

import (
	"fmt"
	"time"
)

// restfollow (live 2026-11-15, bug b1794746024920-544): Silversage13 (Rogue 80) was 61 cells back fighting a Bongun when
// Ayla took the party from pay_dun02 to pay_dun03; it then sat "low HP (28%): resting before moving on" alone on
// pay_dun02. A party member at low HP whose party (the human leader, else the bot leader) is on another map doesn't
// rest alone in a field / dungeon: it drinks and follows the party to its map, and rests there with the healer.
// (With the party on this map but far, restAnchor already walks it back; near the party it rests and asks for a heal.)

// partyAwayMap (b.mu held): the party leader's map when it is another map than ours (not in a town), else ok=false
func (b *Bot) partyAwayMap(now time.Time) (string, bool) {
	p := partyOf(b)
	if p == nil || isTown(b.MapName) || townOf(b.MapName) != "" {
		return "", false
	}
	var lm string
	if p.Human != "" {
		if h, ok := b.humanState(p.Human, now); ok {
			lm = h.Map
		}
	} else if p.Leader != nil && p.Leader != b {
		if s, ok := peek(p.Leader.P.Name); ok && s.Online {
			lm = s.Map
		}
	}
	return lm, restFollowNeeded(b.MapName, lm)
}

func restFollowNeeded(myMap, leaderMap string) bool {
	return leaderMap != "" && leaderMap != myMap
}

// restFollowSay (b.mu held): one line per 3 min
func (b *Bot) restFollowSay(m string, now time.Time) {
	b.setState(fmt.Sprintf("low HP: following the party to %s, resting there", m))
	if now.Before(b.buffAt["restfollow"]) {
		return
	}
	b.buffAt["restfollow"] = now.Add(3 * time.Minute)
	typing := b.P.Typing
	go b.partySay(styled(typing, pick("low hp, coming to u, heal pls when im there", "omw, low hp - heal pls when i catch up")))
}
