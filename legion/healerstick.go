package main

// healerstick.go - the party's healer stays with the leader (PT04 rule number one, PT06, PT24, PT31; owner 2026-11-14:
// "why is the priest always somewhere else?"), and keeps the leader buffed (owner: "priest should be casting buffs on
// Ayla always. It does nothing.").
//
// Root causes (live 2026-11-15 03:33-03:58Z, Ayla's log + engine state):
//   - humanparty.go checkpoint hold: "waiting in geffen: gef_fild08 too dangerous" for 6 min while Ayla (lv82) farmed
//     gef_fild08 and called it to gl_church - the hold judged the map for the weakest member, the healer included;
//   - kafra.go: after following her geffen -> prontera on her town run, the 50-min "not straight back" block refused
//     the Kafra back to geffen (human-party members were never exempt: kafraPartyMove / kafraCapExempt return false for
//     p.Human != "") - the priest walked prontera -> prt_fild00 -> geffen while she waited "for Zendancer84 to catch up";
//   - partydest.go: the meeting town was a target as long as the destination was a dungeon (fixed in ebed06d);
//   - priestbuff.go: a buff cast was booked as landed for ~220 s whether it landed or not (out of range, behind a
//     wall, she walked off) and the leader ranked like any damage dealer: Ayla got Blessing 6x / Increase AGI 2x /
//     Kyrie 1x in an hour, Echobunny 6x / 6x / 4x.
//
// Flag: tuning healer_stick_off (default off = the behaviour below is on).

import (
	"fmt"
	"sync"
	"time"
)

// healerStickOn: the healer-stays-with-the-leader rules are on.
func healerStickOn() bool { return !tune().HealerStickOff }

// partyNameOf: the engine party the bot belongs to ("" = none), for the API (BotSummary.Party).
func partyNameOf(b *Bot) string {
	if p := partyOf(b); p != nil {
		return p.Name
	}
	return ""
}

// healerFollowCells: how far a party healer lets the leader get on the same map before it walks after her.
const healerFollowCells = 8

// healerHoldsForDanger: a party healer holds back from the leader's map only after dying there itself twice in 10
// minutes - the leader being there is the reason to go (it heals her), not a map rating for the weakest member.
func healerHoldsForDanger(ownDeathsThere int) bool { return ownDeathsThere >= 2 }

// healerOwnDeaths (b.mu held): own deaths on map m in the last 10 minutes.
func (b *Bot) healerOwnDeaths(m string, now time.Time) int {
	n := 0
	for _, v := range b.deathLog {
		if v.m == m && now.Sub(v.at) < 10*time.Minute {
			n++
		}
	}
	return n
}

// humanLeaderLeg (b.mu held): a member of a human-led party is on its way to the leader (its goal is set by
// humanPartySync: her map, her town or her meeting point). The Kafra legs she takes are open to it like they are to
// her: no hourly cap and no 50-min "not straight back" block (the 10-minute hard limits stay).
func (b *Bot) humanLeaderLeg() bool {
	if !healerStickOn() {
		return false
	}
	p := partyOf(b)
	return p != nil && p.Human != "" && b.Goal.Mode == "party" && b.Goal.Set == "human" && b.Goal.Map != "" && b.Goal.Map != b.MapName
}

// leaderOut (b.mu held): the party leader (a human, or the bot leader) is out on a non-town map right now.
func (b *Bot) leaderOut(p *Party, now time.Time) bool {
	if p == nil {
		return false
	}
	var m string
	if p.Human != "" {
		h, ok := b.humanState(p.Human, now)
		if !ok {
			return false
		}
		m = h.Map
	} else {
		if p.Leader == nil || p.Leader == b {
			return false
		}
		s, ok := peek(p.Leader.P.Name)
		if !ok || !s.Online {
			return false
		}
		m = s.Map
	}
	return m != "" && !isTown(m) && townOf(m) == ""
}

// healerTripBlocked: PT24 for the healer - while the leader is out farming it makes no solo town trip; only a bag at
// the urgent weight line is an emergency (a priest heals itself: "out of potions" or "asked 10 min ago" are not).
func healerTripBlocked(healer, leaderOut, weightUrgent bool) bool {
	return healer && leaderOut && !weightUrgent
}

// isPartyLeaderName: name is the leader of party p (the human, or the bot leader's character).
func isPartyLeaderName(p *Party, name string) bool {
	if p == nil || name == "" {
		return false
	}
	if p.Human != "" {
		return name == p.Human
	}
	return p.Leader != nil && p.Leader.charName() == name
}

// buffPending: how long an unconfirmed buff cast is booked before the priest tries again (it lands within ~1 s; no
// confirmation = out of range, a wall, the target walked off). After buffMaxTries unconfirmed casts in a row the old
// full booking applies (a server that never echoes the cast must not make the priest spam).
const (
	buffPending  = 12 * time.Second
	buffMaxTries = 3
)

// buffTries: unconfirmed casts in a row per priest and "<skill>:<name>" (leaf lock).
var buffTries = struct {
	sync.Mutex
	m map[*Bot]map[string]int
}{m: map[*Bot]map[string]int{}}

// buffBookPending (b.mu held, activeBuffs): book the buff cast on name as pending (retry after buffPending) until the
// cast is seen landing. false = too many unconfirmed casts in a row: the caller books the full duration as before.
func (b *Bot) buffBookPending(skill int, name string, now time.Time) bool {
	if !healerStickOn() {
		return false
	}
	k := fmt.Sprintf("%d:%s", skill, name)
	buffTries.Lock()
	t := buffTries.m[b]
	if t == nil {
		t = map[string]int{}
		buffTries.m[b] = t
	}
	t[k]++
	n := t[k]
	if n > buffMaxTries {
		t[k] = 0
	}
	buffTries.Unlock()
	if n > buffMaxTries {
		return false
	}
	b.buffAt[k] = now.Add(buffPending)
	b.buffAt["cast:"+k] = now
	return true
}

// buffConfirm (b.mu held, buffRecv): our own buff landed on dst - book its real duration (renewed ~20 s early).
func (b *Bot) buffConfirm(skill, lv int, dst uint32, now time.Time) {
	if !healerStickOn() || dst == b.aid || (skill != alBlessing && skill != alIncAgi && skill != prKyrie) {
		return
	}
	e := b.ents[dst]
	if e == nil || e.Name == "" {
		return
	}
	k := fmt.Sprintf("%d:%s", skill, e.Name)
	if at, ok := b.buffAt["cast:"+k]; !ok || now.Sub(at) > 5*time.Second {
		return // not a cast activeBuffs is waiting on (the old loops book their own)
	}
	delete(b.buffAt, "cast:"+k)
	if skill == prKyrie {
		b.buffAt[k] = now.Add(kyrieRenew)
	} else {
		b.buffAt[k] = b.buffRenewAt(lv, now)
		if isPartyLeaderName(partyOf(b), e.Name) {
			b.buffAt[k] = b.buffAt[k].Add(-leaderBuffLead) // the leader is never without them: renew earlier
		}
	}
	buffTries.Lock()
	if t := buffTries.m[b]; t != nil {
		delete(t, k)
	}
	buffTries.Unlock()
}

// leaderBuffLead: the leader's Blessing / Increase AGI are renewed this much earlier than the others' (~40 s before
// they run out instead of ~20 s), so a fight or a walk in between never lets them lapse.
const leaderBuffLead = 20 * time.Second

// leaderBuffDue (b.mu held, party healer): the leader is in reach (live position, range + wall line) and her Blessing
// or Increase AGI is due and affordable - the heal loop then lets the buff go first unless someone is under 50% HP.
func (b *Bot) leaderBuffDue(p *Party, members []pubState, now time.Time) bool {
	if !healerStickOn() || p == nil {
		return false
	}
	for _, s := range members {
		if !s.Online || s.Map != b.MapName || s.HPFrac <= 0 || s.Name == b.charName() || !isPartyLeaderName(p, s.Name) {
			continue
		}
		lx, ly := livePosOf(s)
		if !healReachable(maps[b.MapName], b.X, b.Y, lx, ly, healRange) {
			return false
		}
		for _, id := range []int{alBlessing, alIncAgi} {
			si := b.skills[id]
			if si == nil || si.Lv == 0 || now.Before(b.buffAt[fmt.Sprintf("%d:%s", id, s.Name)]) {
				continue
			}
			if b.buffOK(si, now) {
				return true
			}
		}
		return false
	}
	return false
}
