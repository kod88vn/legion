package main

// PT30 (owner 2026-11-14): a party leader tells the party where to be (the farm map, or the Kafra/town to meet at) in
// party chat, and the members travel there by their own best route; they do not trail the leader through every town
// on the way. QA 2026-11-14: Ayla's friends trailed her toward izlude/yuno - humanPartySync pointed them at the town she
// was crossing (Zen.fox was sent to izlude and parked in Aldebaran on the way).
//
// So a member of a travelling leader heads for the leader's DESTINATION, not the leader's current map:
//   - a human leader's destination comes from her own status file: "dest" (newer partylead.pl: the goto map, the rally
//     town, else her lockMap) or "lock_map" (her hunting map), while she isn't there and isn't on a town run;
//   - open ground the party can handle: straight to the map, own route (Kafra chain, own path);
//   - a weaker party, or a destination that is a dungeon / hostile for us: the Kafra town nearest it (the meeting point);
//   - on the leader's map: with the leader (partyThink follows / regroups), as before.
// Bot party leaders already say the plan on formation and relocation (planLine); the re-plan and boss-hunt return
// paths now say it too (sayPartyDest). Default on; tuning "party_dest_off" restores the old trailing.

import (
	"strings"
	"sync"
	"time"
)

// partyDestOn: the PT30 behaviour is on (tuning party_dest_off turns it off).
func partyDestOn() bool { return !tune().PartyDestOff }

// humanDest: where a human leader is heading ("" = nowhere else: she is there, she is on a town run, or nothing known).
// st must be fresh; herMap is where she stands now; townRun = the party has a town run she called lately.
func humanDest(st humanStatus, herMap string, townRun bool) string {
	if herMap == "" || townRun || st.TownRun {
		return ""
	}
	d := strings.ToLower(strings.TrimSpace(st.Dest))
	if d == "" {
		d = strings.ToLower(strings.TrimSpace(st.LockMap))
		if isTown(d) {
			return "" // an old status file: a town lock map is no travel plan
		}
	}
	if d == "" || d == herMap || townOf(herMap) == d {
		return "" // already there (a building of the destination town counts)
	}
	return d
}

// humanMemberTarget: the map a friend heads for while the leader travels to dest. On her map: her map (stay with her).
// A town destination (a meeting point): that town. Open ground for a strong party: the map itself. Otherwise the Kafra
// town nearest the destination (the meeting point).
func humanMemberTarget(dest, herMap, myMap, meetTown string, strong, hostileDest bool) string {
	if myMap == herMap && herMap != "" {
		return herMap
	}
	if isTown(dest) {
		return dest
	}
	if strong && !hostileDest {
		return dest
	}
	if meetTown != "" {
		return meetTown
	}
	return dest
}

// humanDestNow (b.mu held): the human leader's destination right now and the map this member should head for ("" =
// no destination: the old logic decides). herMap is her current map as humanPartySync resolved it.
func (b *Bot) humanDestNow(p *Party, herMap string, now time.Time) (dest, target string) {
	if !partyDestOn() || p == nil || p.Human == "" || herMap == "" {
		return "", ""
	}
	// inside a dungeon / hostile map she is followed map by map (together.go hostileTransit; QA 19:00Z Sphinx)
	if hostileTransit(herMap, b.powerLv()) {
		return "", ""
	}
	st, ok := humanStatusOf(p.Human)
	if !ok || now.Sub(time.Unix(st.TS, 0)) > 15*time.Second {
		return "", ""
	}
	at, _, by := p.townRun()
	townRun := !at.IsZero() && now.Sub(at) < 10*time.Minute && strings.EqualFold(by, p.Human)
	dest = humanDest(st, herMap, townRun)
	if dest == "" {
		return "", ""
	}
	meet := nearestTown(dest)
	if t := townOf(meet); t != "" {
		meet = t
	}
	hostile := !isTown(dest) && (dungeonMap(dest) || hostileTransit(dest, b.powerLv()))
	return dest, humanMemberTarget(dest, herMap, b.MapName, meet, partyStrong(p), hostile)
}

// ---- bot party leaders say where to be (PT30) ----

var partyDestSaid = struct {
	sync.Mutex
	m map[*Party]destSaid
}{m: map[*Party]destSaid{}}

type destSaid struct {
	m  string
	at time.Time
}

// destSayDue: the party hasn't been told about map m in the last 10 minutes (and records it as told now).
func destSayDue(p *Party, m string, now time.Time) bool {
	partyDestSaid.Lock()
	defer partyDestSaid.Unlock()
	if s, ok := partyDestSaid.m[p]; ok && s.m == m && now.Sub(s.at) < 10*time.Minute {
		return false
	}
	partyDestSaid.m[p] = destSaid{m, now}
	return true
}

// sayPartyDest: the leader l tells party p where to be next (planLine: where, why, where to meet), once per map per
// 10 minutes. typing is l.P.Typing (read by the caller under l.mu). Sends from a goroutine: safe with l.mu held.
func sayPartyDest(l *Bot, typing string, p *Party, m, why string) {
	if !partyDestOn() || l == nil || p == nil || m == "" || isTown(m) || !destSayDue(p, m, time.Now()) {
		return
	}
	line := styled(typing, planLine(p, m, why))
	go l.partySay(line)
}
