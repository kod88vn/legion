package main

// Party roles and composition: every bot party aims for the classic setup
//   tank / bait - takes the hits and pulls monsters: Swordman line, Monk, or an AGI (evasion) Thief/Assassin/Rogue
//   ranged      - damage from a distance on the tank's target: Archer line, Mage line
//   support     - heals and buffs: Acolyte / Priest
// The tank leads (picks and pulls targets, peels monsters off squishy members), ranged and support hang back,
// melee damage dealers (Merchant line, STR Thieves) assist.

import "sort"

const (
	roleTank = iota
	roleRanged
	roleSupport
	roleMelee
	roleNone
)

func partyRole(b *Bot) int {
	c := b.Class
	if base, ok := transToSecond[c]; ok {
		c = base
	}
	switch c {
	case 15, 4016: // Monk / Champion: frontline
		return roleTank
	}
	switch family(b.Class) {
	case famSword:
		return roleTank
	case famArcher, famMage:
		return roleRanged
	case famAcolyte:
		return roleSupport
	case famThief:
		if b.stats[stAGI] >= 2*b.stats[stSTR] || b.stats[stAGI] >= 30 { // evasion build: a dodge tank
			return roleTank
		}
		return roleMelee
	case famMerchant:
		return roleMelee
	}
	return roleNone // Novices
}

// tankRank: preferred tanks first (true tanks, then evasion thieves).
func tankRank(b *Bot) int {
	if family(b.Class) == famThief {
		return 1
	}
	return 0
}

// formRoleParties groups the free hunters on one map into tank + support + ranged (+1) parties.
func formRoleParties(m string, cands []*Bot) {
	by := map[int][]*Bot{}
	for _, c := range cands {
		if c.P.Sociability*c.tactics().Party < 0.15 || c.alwaysOnline() { // guild leaders lead their own party
			continue
		}
		r := partyRole(c)
		by[r] = append(by[r], c)
	}
	sort.SliceStable(by[roleTank], func(i, j int) bool { return tankRank(by[roleTank][i]) < tankRank(by[roleTank][j]) })
	used := map[*Bot]bool{}
	pick := func(role int, lv int) *Bot {
		var best *Bot
		bd := 16
		for _, c := range by[role] {
			if used[c] {
				continue
			}
			if d := abs(c.BaseLvOr() - lv); d < bd {
				best, bd = c, d
			}
		}
		return best
	}
	for _, tank := range by[roleTank] {
		if used[tank] || tank.basicLv() < 7 {
			continue
		}
		lv := tank.BaseLvOr()
		used[tank] = true
		var group []*Bot
		for _, role := range []int{roleSupport, roleRanged, roleRanged, roleMelee} {
			if len(group) >= 3 {
				break
			}
			if c := pick(role, lv); c != nil {
				used[c] = true
				group = append(group, c)
			}
		}
		if len(group) == 0 || formParty(tank, group, m) == nil {
			used[tank] = false
			for _, g := range group {
				used[g] = false
			}
		}
	}
	// no tank around: ranged + support (+ melee) still team up, led by whoever can found a party
	var rest []*Bot
	for _, c := range cands {
		if !used[c] && c.P.Sociability >= 0.15 && partyRole(c) != roleNone {
			rest = append(rest, c)
		}
	}
	sort.Slice(rest, func(i, j int) bool { return rest[i].BaseLvOr() < rest[j].BaseLvOr() })
	for i := 0; i+1 < len(rest); i += 3 {
		lead := rest[i]
		var group []*Bot
		for _, c := range rest[i+1 : min(i+3, len(rest))] {
			if c.BaseLvOr()-lead.BaseLvOr() <= 15 {
				group = append(group, c)
			}
		}
		if lead.basicLv() >= 7 && len(group) > 0 {
			formParty(lead, group, m)
		}
	}
}

// shouldBait: in a bot party with a much weaker member, the strong character only tags monsters (one hit to pull
// them) and lets the weaker members kill them - the classic power-levelling etiquette. Mentoring guilds do it at a
// smaller level gap. With even exp share the strong character still gets its share.
func (b *Bot) shouldBait() bool {
	p := partyOf(b)
	if p == nil || p.Human != "" {
		return false
	}
	gap := 10
	if b.tactics().Mentor {
		gap = 6
	}
	my := b.BaseLvOr()
	for _, m := range p.Members {
		if m == b {
			continue
		}
		// only for a weaker member who is right here and can kill (QA 2026-11-14: Swordmen 10+ levels above their
		// Acolyte tagged monsters and stood still - a healer never finishes them, and a member on another map can't)
		if s, ok := peek(m.P.Name); ok && s.Online && partyRole(m) != roleSupport && s.Map == b.MapName &&
			dist(b.X, b.Y, s.X, s.Y) <= 12 && my-m.BaseLvOr() >= gap {
			return true
		}
	}
	return false
}
