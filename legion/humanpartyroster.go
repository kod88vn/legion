package main

// Human-party roster changes (AY11, owner 2026-11-14: Ayla's Alchemist leaves her party and plays on its own, staying
// in Starveil; a Monk takes its place). data/human-parties.json lists each human's party "members"; "alumni" are
// characters that left that party (owner's order) and now live like any other bot. The file is owned by the core
// instance (instances.go); the main instance only reads it here.
//
//   - alumniLeave (both instances, botPartyKeeper): an alumnus still in the human's in-game party (rAthena keeps a
//     party across restarts and logins) says goodbye in party chat once, then leaves it ~6 s later (CZ_REQ_LEAVE_GROUP),
//     like a player who goes off to farm on his own. Before this nothing ever left that party: the main instance's
//     stale-party check only leaves parties led by one of its own bots, and the human's party is led from the core.
//   - humanPartyStrays (core, humanPartyManager): a new member still in another in-game party (its old bot party)
//     says bye there and leaves it, so the human party's leader can invite it (invites only go to PartyID 0).
//   - keepsHumanGuild (friends.go friendMoveEligible): an alumnus stays in the human's guild - it left the party, not
//     its friends.
//
// An empty "alumni" list (the default) changes nothing.

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// humanPartyName: the in-game name of a human's party (startHumanParty).
func humanPartyName(human string) string { return fmt.Sprintf("%.18s pt", human) }

var hpRoster struct {
	sync.Mutex
	at      time.Time
	parties []savedHumanParty
}

// savedRoster: human-parties.json, re-read at most once a minute (both instances).
func savedRoster() []savedHumanParty {
	hpRoster.Lock()
	defer hpRoster.Unlock()
	if !hpRoster.at.IsZero() && time.Since(hpRoster.at) < time.Minute {
		return hpRoster.parties
	}
	hpRoster.at = time.Now()
	if raw, err := os.ReadFile(humanPartyFile()); err == nil {
		var s []savedHumanParty
		if json.Unmarshal(raw, &s) == nil {
			hpRoster.parties = s
		}
	}
	return hpRoster.parties
}

// alumnusIn: the human whose party char left (listed in alumni, not back in members).
func alumnusIn(roster []savedHumanParty, char string) (string, bool) {
	for _, s := range roster {
		in, gone := false, false
		for _, n := range s.Members {
			in = in || strings.EqualFold(n, char)
		}
		for _, n := range s.Alumni {
			gone = gone || strings.EqualFold(n, char)
		}
		if gone && !in {
			return s.Human, true
		}
	}
	return "", false
}

// keepsHumanGuild: a former party friend of a human stays in that human's guild.
func keepsHumanGuild(char, guild string) bool {
	if guild == "" || !isHumanGuild(guild) {
		return false
	}
	_, ok := alumnusIn(savedRoster(), char)
	return ok
}

// withAlumni (saveHumanParties): keep the alumni lists of the file being rewritten (the engine doesn't hold them).
func withAlumni(out []savedHumanParty) []savedHumanParty {
	raw, err := os.ReadFile(humanPartyFile())
	if err != nil {
		return out
	}
	var old []savedHumanParty
	if json.Unmarshal(raw, &old) != nil {
		return out
	}
	for i := range out {
		for _, o := range old {
			if strings.EqualFold(o.Human, out[i].Human) {
				out[i].Alumni = o.Alumni
			}
		}
	}
	return out
}

// partyBye: when a bot said goodbye to an in-game party it is leaving (alumni / strays), per bot.
var partyBye = struct {
	sync.Mutex
	at map[*Bot]time.Time
}{at: map[*Bot]time.Time{}}

// byeThenLeave: say line in party chat once, leave the in-game party 6 s later (and again every call while the
// server still lists us in it - a lost leave packet).
func (b *Bot) byeThenLeave(now time.Time, line, memo, why string) {
	partyBye.Lock()
	said, ok := partyBye.at[b]
	if !ok {
		partyBye.at[b] = now
	}
	partyBye.Unlock()
	if !ok {
		go b.partySay(styled(b.P.Typing, line))
		if memo != "" {
			memoryOf(b.P).add(memo)
		}
		return
	}
	if now.Sub(said) >= 6*time.Second {
		b.leaveGameParty()
		b.event("party", "", why)
	}
}

// alumniLeave (botPartyKeeper, both instances): true while b is busy leaving the human's party it left.
func (b *Bot) alumniLeave(now time.Time) bool {
	b.mu.Lock()
	on := b.Online && b.MapName != ""
	pid, pname, char, pinned := b.PartyID, b.PartyName, b.charName(), b.pin != nil
	b.mu.Unlock()
	if !on || pid == 0 || pinned {
		return false
	}
	human, ok := alumnusIn(savedRoster(), char)
	if !ok || !strings.EqualFold(pname, humanPartyName(human)) {
		return false
	}
	b.byeThenLeave(now,
		pick("gonna go farm on my own for a while, see u in guild", "im gonna solo for a bit, see u guys in guild",
			"heading off on my own for a while, cya in guild chat"),
		"left "+human+"'s party to farm on my own for a while; still in the guild with them",
		"left "+pname+" (plays on its own now, still in the guild)")
	return true
}

// humanPartyStrays (core, humanPartyManager): members of the human's party still in another in-game party leave it.
func humanPartyStrays(p *Party, now time.Time) {
	for _, m := range p.Members {
		m.mu.Lock()
		on := m.Online && m.MapName != ""
		pid, pname := m.PartyID, m.PartyName
		m.mu.Unlock()
		if !on || pid == 0 || pname == "" || strings.EqualFold(pname, p.Name) {
			if pid != 0 && strings.EqualFold(pname, p.Name) {
				partyBye.Lock()
				delete(partyBye.at, m) // in the right party now
				partyBye.Unlock()
			}
			continue
		}
		m.byeThenLeave(now,
			pick("gonna party with my guild friends for a while, cya", "my guild needs me in their party, cya guys",
				"joining my guildies for a while, thx for the party"),
			"left "+pname+" to join "+p.Human+"'s party (guild friends)",
			"left "+pname+" to join "+p.Name)
	}
}
