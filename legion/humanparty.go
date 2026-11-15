package main

// Human-led parties: a few Legion players party up with a real player and play alongside them. They stay online
// (pinned, ignoring their schedule) on a chosen character, join the human's party, follow them across maps, attack
// what the human attacks, defend anyone in the party who's being hit, and the support heals/buffs the human too.
//
// What a bot knows about the human comes from the game, like a real partymate:
//   - position: unit packets while in view (0x09fd/0x09fe/0x09ff/0x0086), party position 0x0107, member list map
//   - target:   the human's attack/skill packets (0x08c8 / 0x01de src = human)
//   - HP:       party HP updates 0x080e

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Pin keeps a bot online on one character, optionally in a human's party.
type Pin struct {
	Char  string    `json:"char"`
	Human string    `json:"human,omitempty"`
	Hold  time.Time `json:"hold,omitempty"` // don't log in before this (lets the operator move the character)
}

type humanSeen struct {
	Map  string
	X, Y int
	AID  uint32
	At   time.Time
	// from the server's party data (member list 0x0ae5 / member update 0x0ae4): authoritative online flag and map,
	// even when no bot can see the human. Before this the dashboard, the watchdog and the sentinel showed Ayla
	// "offline" whenever she was on another map than her friends or hadn't moved for 5 minutes.
	PartyOnline bool
	PartyAt     time.Time
}

// notePartyMember records a human's online state and map from party packets.
func notePartyMember(name, m string, online bool, aid uint32) {
	h, _ := humanAt(name)
	h.PartyOnline, h.PartyAt = online, time.Now()
	if online && m != "" {
		if h.Map != m {
			h.X, h.Y = 0, 0
		}
		h.Map = m
		h.At = time.Now()
	}
	if aid != 0 {
		h.AID = aid
	}
	humanLocs.Store(name, h)
}

var humanLocs sync.Map // name -> humanSeen

func noteHuman(name, m string, x, y int, aid uint32) {
	h := humanSeen{Map: m, X: x, Y: y, AID: aid, At: time.Now(), PartyOnline: true, PartyAt: time.Now()}
	if old, ok := humanLocs.Load(name); ok && aid == 0 {
		h.AID = old.(humanSeen).AID
	}
	humanLocs.Store(name, h)
}

func humanAt(name string) (humanSeen, bool) {
	if v, ok := humanLocs.Load(name); ok {
		return v.(humanSeen), true
	}
	return humanSeen{}, false
}

// inHumanParty: the bot belongs to a party led by a real player (Ayla's friends). Minds can't move or re-party it.
func inHumanParty(b *Bot) bool {
	p := partyOf(b)
	return p != nil && p.Human != ""
}

func humanOf(b *Bot) string {
	if p := partyOf(b); p != nil {
		return p.Human
	}
	return ""
}

func (b *Bot) pinned() *Pin {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.pin
}

// humanState: what this bot knows about the human partymate right now.
func (b *Bot) humanState(name string, now time.Time) (pubState, bool) {
	h, ok := humanAt(name)
	if !ok {
		// nobody saw her and no party packet yet (right after a relog): her own status file still says where she is
		// (QA 2026-11-14 10:20: Zen.fox logged in on pay_dun00 and sat "waiting for Ayla" while she fought on pay_dun01)
		st, sok := humanStatusOf(name)
		if !sok || st.Map == "" || st.X <= 0 || now.Sub(time.Unix(st.TS, 0)) > 8*time.Second {
			return pubState{}, false
		}
		h = humanSeen{Map: st.Map, X: st.X, Y: st.Y, At: time.Unix(st.TS, 0), PartyOnline: true, PartyAt: time.Unix(st.TS, 0)}
	}
	s := pubState{Name: name, Map: h.Map, X: h.X, Y: h.Y, HPFrac: 1, Online: true, AID: h.AID}
	// the human's own client writes its position every ~3 s (statusout.pl -> /ayla/status.json): when that is newer
	// than what the bots last saw or got in a party packet, it wins. QA 2026-11-14 12:25: Zen.fox stood "with Ayla"
	// 24 cells from her for 3 min while her plugin waited for it (a stale leader position = a deadlock).
	var stAt time.Time // when the human's own status file (fresh, this map) was written
	if st, ok := humanStatusOf(name); ok && st.Map != "" && st.X > 0 {
		if at := time.Unix(st.TS, 0); now.Sub(at) < 8*time.Second && at.After(h.At) {
			s.Map, s.X, s.Y = st.Map, st.X, st.Y
		}
		if at := time.Unix(st.TS, 0); now.Sub(at) < 8*time.Second {
			humanSitSeen(name, st.Sitting, now) // together.go: fallback when she didn't announce a rest
		}
		if at := time.Unix(st.TS, 0); now.Sub(at) < 5*time.Second && st.Map == b.MapName {
			stAt = at
			s.X, s.Y = st.X, st.Y // QA 13:02: friends stopped "with Ayla" 24-27 cells from her on stale positions
		}
	}
	if s.Map == b.MapName {
		for _, e := range b.ents {
			if e.Type == objPC && e.Name == name {
				s.AID = e.ID
				// what we saw beats the status file only when it is newer (a walk packet after the file was written)
				if stAt.IsZero() || e.Seen.After(stAt) || e.MoveAt.After(stAt) {
					s.X, s.Y = e.X, e.Y
					if e.DX != e.X || e.DY != e.Y {
						s.X, s.Y = e.DX, e.DY // where they're heading
					}
				}
				if now.Sub(e.TargetAt) < 4*time.Second {
					s.Target = e.Target
				}
				if e.Sitting {
					humanSitSeen(name, true, e.SitAt)
				}
				break
			}
		}
		if hp, ok := b.partyHP[s.AID]; ok && hp[1] > 0 {
			s.HPFrac = float64(hp[0]) / float64(hp[1])
		}
	}
	humanTrailNote(name, s.Map, now) // together.go: her road map by map, for her friends to follow
	if s.X >= 1000 || s.Y >= 1000 {  // a bogus party-list position (65535,65535: "no path ... to 65535,65535")
		s.X, s.Y = 0, 0
	}
	s.Resting = humanResting(name, now) // together.go: her "resting" ... "ready" lines, sitting as a fallback
	s.Help = humanCalledHelp(name, now) // together.go protectSupport: she said "help" lately
	return s, true
}

// partyView: the leader to follow and everyone to look after (bots + the human).
func (b *Bot) partyView(p *Party, now time.Time) (lead pubState, ok bool, members []pubState) {
	for _, m := range p.Members {
		if s, ok := peek(m.P.Name); ok {
			members = append(members, s)
		}
	}
	if p.Human == "" {
		lead, ok = peek(p.Leader.P.Name)
		return
	}
	lead, ok = b.humanState(p.Human, now)
	if ok {
		members = append(members, lead)
	}
	return
}

// defendTarget: a monster that's currently hitting someone in the party (nearest first).
func (b *Bot) defendTarget(members []pubState, now time.Time) *Entity {
	aids := map[uint32]bool{b.aid: true}
	for _, m := range members {
		if m.AID != 0 && m.Map == b.MapName {
			aids[m.AID] = true
		}
	}
	var best *Entity
	for _, e := range b.ents {
		if e.Type != objMob {
			continue
		}
		if (aids[e.Target] && now.Sub(e.TargetAt) < 4*time.Second) || now.Sub(e.LastHitMe) < 4*time.Second {
			if best == nil || dist(b.X, b.Y, e.X, e.Y) < dist(b.X, b.Y, best.X, best.Y) {
				best = e
			}
		}
	}
	return best
}

// humanPartySync keeps a pinned bot's goal pointed at the human (runs every think, under b.mu).
func (b *Bot) humanPartySync(now time.Time) {
	p := partyOf(b)
	if p == nil || p.Human == "" {
		return
	}
	if e := b.errand; e != nil && (e.Kind == "alchemy" || e.Kind == "brew supplies") && !e.Done {
		return // alchemy.go (economy run 9): the Alchemist Union errand drives the goal until it is over
	}
	if e := b.errand; e != nil && (e.Kind == "peco" || e.Kind == "traps") && !e.Done {
		return // peco.go / trap.go: the Peco Peco / Trap errand (solo trip) drives the goal until it is over
	}
	if _, busy := pecoTasks.Load(b); busy {
		return // at the breeder right now (e.g. on a party town run in Prontera): don't walk off mid-rental
	}
	h, ok := b.humanState(p.Human, now)
	m := b.MapName
	if ok && h.Map != "" {
		m = h.Map
		// the leader went into a town building (shop, Kafra office...): wait outside in the town. Following inside
		// sent members through random doors into dead-end rooms of payon_in01 etc., where they got stuck.
		if t := townOf(h.Map); t != "" && t != h.Map {
			m = t
		}
		if !isTown(m) {
			p.Checkpoint = nearestTown(m) // the party's regroup point: the town closest to where the leader hunts
		}
	}
	// her announced plan, not her footsteps (owner 2026-11-14: "Party leaders must always communicate the plan ... do not
	// let the party follow her blindly"): while she travels to another hunting map (status.json lock_map isn't where she
	// stands, outside towns), a strong party routes to that map itself, a weaker one to the Kafra town nearest it
	// ... but only on open ground: inside a dungeon / hostile map the friends stay with her (QA 19:00Z Sphinx: they ran
	// ahead to in_sphinx2 on their own and three died) - together.go hostileTransit
	if ok && h.Map != "" && townOf(h.Map) == "" && !hostileTransit(h.Map, b.powerLv()) {
		if st, sok := humanStatusOf(p.Human); sok && st.LockMap != "" && st.LockMap != h.Map && now.Sub(time.Unix(st.TS, 0)) < 15*time.Second {
			// a strong party follows HER map by map (m stays her current map: the same portals she takes) - routing
			// to the lock map on its own sent the friends down another road (QA 20:23Z: she went via pay_fild08, they
			// planned pay_fild01/07 and posted "lost u"); a weaker party waits at the Kafra town nearest the lock map
			if !partyStrong(p) {
				if cp := nearestTown(st.LockMap); cp != "" {
					m = cp
				}
			}
		}
	}
	// her road, not ours: when we stand on a map she crossed lately, the next map is the one she went to from it (the
	// same portal) - together.go humanTrail (QA 20:23Z: friends took pay_fild01/07 while she went via pay_fild08)
	if ok && h.Map != "" && b.MapName != h.Map && m == h.Map {
		if nx := humanTrailNext(p.Human, h.Map, b.MapName); nx != "" {
			m = nx
		}
	}
	// the leader went somewhere far too dangerous for our level: wait at the checkpoint town to be picked up
	// instead of walking in alone and dying (the leader comes back for us)
	waiting := false
	// one party, one decision: members follow the leader unless the map is deadly for the party's weakest member
	// (each member used to judge alone by its own risk trait - Echobunny waited in Payon while the priest went in)
	if m != b.MapName && !isTown(m) && p.Checkpoint != "" {
		weakest := b.BaseLvOr()
		for _, mm := range p.Members { // published snapshots only: no other bot's lock while holding ours
			if s, ok := peek(mm.P.Name); ok && s.Online && s.Lv > 0 {
				weakest = min(weakest, s.Lv)
			}
		}
		// real deaths in the last 10 minutes (a window, so the hold ends once the map calms down). It used to be
		// recentDeaths > 8, which counted every escape as a full death with a 20-min half-life: Fly Wing close calls in
		// pay_dun01 held Echobunny and Zen.fox "roaming" in Payon while Ayla hunted (QA 2026-11-14)
		// a strong party (weakest lv50+, together.go partyStrongLv): other players' deaths (low-level bots dying on the
		// map) are not a reason for it to stay out - only the monsters against its own level and its own deaths
		// (QA 2026-11-14 13:16: Ayla's lv72-78 friends sat in Geffen, "gef_fild08 too dangerous", while she went on)
		dangerous := func(dm string) bool {
			if weakest >= partyStrongLv {
				own := 0
				for _, v := range b.deathLog {
					if v.m == dm && now.Sub(v.at) < 10*time.Minute {
						own++
					}
				}
				return mapDanger(dm, weakest)-recentDeaths(dm)*3 > 60 || own >= 2
			}
			return mapDanger(dm, weakest) > 60 || deathsWithin(dm, 10*time.Minute) >= 5
		}
		if dm := m; dangerous(dm) {
			if !b.waitingAtCheckpoint || b.waitDanger != dm {
				b.waitDanger = dm
				b.buffAt["dangersay"] = time.Time{} // a new hold: say so once
			}
			m, waiting = p.Checkpoint, true
		}
	}
	b.waitingAtCheckpoint = waiting
	if !waiting {
		b.waitDanger = ""
	}
	// the human's town run goes where SHE is: her party line names a town ("bag full, town run to payon"), but when
	// she is in another town (storage in Geffen) that town wins - the run is re-pointed and party-run trips bound for
	// the old town are dropped, so members join the run again for her town (QA 2026-11-14 13:19: Ayla in Geffen, her
	// friends walking to Payon from prt_fild08 / gef_fild00 / Prontera)
	if ok && h.Map != "" {
		if ht := townOf(h.Map); ht != "" {
			if at, town, by := p.townRun(); !at.IsZero() && now.Sub(at) < 10*time.Minute && strings.EqualFold(by, p.Human) && town != ht {
				p.repointTownRun(ht, now)
				b.event("party", "", fmt.Sprintf("%s is in %s, not %s: the town run goes there", p.Human, ht, town))
			}
			if t := b.trip; t != nil && t.Town != ht && (t.Why == "town run with the party" || t.Why == "with the party" || t.Why == "gear") {
				b.trip, b.tripAgain, b.route, b.path = nil, time.Time{}, nil, nil // rejoin the run for her town
			}
		}
	}
	// in the party's town (a town run, a rally) with the save point elsewhere or unknown: save at its Kafra, so a death or
	// a Butterfly Wing brings us back to the party's town (owner: save at the Kafra nearest the grinding ground; QA 12:37:
	// friends - new ones too, with save points from their old life - winged to Morocc / Prontera on a Payon town run)
	// the hunting map is HER destination (status.json lock map), else where she hunts; the save town is its nearest
	// Kafra town - in that town (a rally, a town run, passing through) every friend saves before going in (owner rule,
	// enforced 2026-11-14: Ayla's friends rallied in Morocc before the Sphinx and never saved - the old code only saved
	// in p.Checkpoint, which still pointed at the previous hunting ground's town)
	hunt := ""
	if st, sok := humanStatusOf(p.Human); sok {
		hunt = st.LockMap
	}
	if hunt == "" && ok && !isTown(h.Map) && townOf(h.Map) == "" {
		hunt = h.Map
	}
	if t := humanSaveTown(p); hunt != "" && t != "" && b.MapName == t && b.kafra == nil && b.trip == nil && !b.underAttack(now) && // her save town (together.go)
		now.After(b.buffAt["hpsave"]) {
		if saved, known := b.knownSave(); !known || saved != t {
			b.buffAt["hpsave"] = now.Add(time.Minute)
			if k := b.kafraSavePlan(hunt, now); k != nil {
				b.kafra = k
				b.event("info", "", "party: saving at the Kafra in "+t+" before "+hunt)
			}
		}
	}
	// separated from the leader for 2+ minutes: ask for help in party chat (owner: "Party member should know to ask for
	// help and ayla should know to go collect them"); Ayla's plugin answers by coming to get them
	if ok && h.Map != "" && h.Map != b.MapName {
		if b.awaySince.IsZero() {
			b.awaySince = now
		} else if now.Sub(b.awaySince) > 45*time.Second && now.After(b.buffAt["helpsay"]) && p.rallyInfo().Town == "" && b.trip == nil && b.errand == nil && townOf(b.MapName) == "" {
			b.buffAt["helpsay"] = now.Add(3 * time.Minute)
			typing, where := b.P.Typing, b.MapName
			go b.partySay(styled(typing, pick("where r u "+p.Human+"? im at "+where, "lost u, im at "+where, "help, cant find u. im in "+where)))
		}
	} else {
		b.awaySince = time.Time{}
	}
	if b.Goal.Mode != "party" || b.Goal.Map != m || b.Goal.Set != "human" {
		if b.Goal.Map != m {
			b.route, b.path = nil, nil
		}
		b.Goal = Goal{Mode: "party", Map: m, Who: p.Human, Set: "human"}
	}
	// she is in a dungeon / on a hostile map and we are outside it (town or field - after a relog, a death, an errand):
	// gather in the entrance town first and go in together, restocked (together.go escortGate; QA 14:40: friends saved
	// in Payon, then walked into pay_dun00/01 one by one and got swarmed)
	b.escortHold = ""
	if ok && h.Map != "" && !waiting {
		if t, why := b.escortGate(p, h.Map, now); t != "" {
			m, b.escortHold = t, why
		}
	}
	if waiting && b.MapName == m {
		b.setState("waiting for " + p.Human + " at " + m + " (too dangerous for me there)")
	}
}

// shareSentAt: when even exp share was last requested per party (humanPartyManager goroutine only)
var shareSentAt = map[string]time.Time{}

// humanPartyManager: forms and maintains the in-game party for human-led groups.
func humanPartyManager(bots []*Bot, stop <-chan struct{}) {
	t := time.NewTicker(10 * time.Second)
	defer t.Stop()
	invited := map[string]time.Time{} // "party/name" -> last invite
	byName := map[string]*Bot{}
	for _, b := range bots {
		for _, c := range b.P.Chars {
			byName[strings.ToLower(c.Name)] = b
		}
	}
	loadHumanParties(byName)
	for {
		select {
		case <-stop:
			return
		case <-t.C:
		}
		parties.Lock()
		var hps []*Party
		for _, p := range parties.byName {
			if p.Human != "" {
				hps = append(hps, p)
			}
		}
		parties.Unlock()
		for _, p := range hps {
			// the in-game party leader does the inviting (only leaders can invite; rAthena keeps parties across
			// restarts); if nobody is in the party yet, the first online member founds it
			var lead, first *Bot
			anyIn := false
			for _, m := range p.Members {
				m.mu.Lock()
				on := m.Online && m.MapName != "" && m.basicLv() >= 5
				in := m.PartyID != 0 && strings.EqualFold(m.PartyName, p.Name)
				isLead := in && strings.EqualFold(m.partyLeader, m.charName())
				m.mu.Unlock()
				anyIn = anyIn || (on && in)
				if on && isLead && lead == nil {
					lead = m
				}
				if on && first == nil && m.basicLv() >= 7 {
					first = m
				}
			}
			if lead == nil && !anyIn {
				lead = first
			}
			if lead == nil {
				continue // leader offline (or it's the human): members wait for an invite
			}
			lead.mu.Lock()
			created := lead.PartyID != 0
			roster := map[string]bool{}
			for n := range lead.roster {
				roster[strings.ToLower(n)] = true
			}
			lead.mu.Unlock()
			if !created {
				if p.Leader != lead {
					p.Leader = lead
				}
				lead.mu.Lock()
				lead.PartyName = p.Name
				lead.mu.Unlock()
				lead.send(newPkt(0x01e8).str(p.Name, 24).u8(1).u8(0).b)
				lead.event("party", "", fmt.Sprintf("created %s for %s", p.Name, p.Human))
				continue
			}
			// friends levelling together share exp evenly (the server only shares within 15 levels)
			if !p.ShareSet && time.Since(shareSentAt[p.Name]) > 30*time.Minute {
				lead.mu.Lock()
				isLeader := strings.EqualFold(lead.partyLeader, lead.charName())
				lead.mu.Unlock()
				if isLeader {
					lead.send(newPkt(0x07d7).u32(1).u8(1).u8(0).b) // CZ_GROUPINFO_CHANGE_V2: exp even share
					p.ShareSet = true
					shareSentAt[p.Name] = time.Now()
					lead.event("party", "", "set "+p.Name+" to even exp share")
				}
			}
			// invite whoever isn't in yet (bots accept automatically; the human gets a popup)
			names := []string{p.Human}
			for _, m := range p.Members {
				if m != lead {
					m.mu.Lock()
					if m.Online && m.PartyID == 0 {
						names = append(names, m.charName())
					}
					m.mu.Unlock()
				}
			}
			for _, n := range names {
				if roster[strings.ToLower(n)] {
					continue
				}
				k := p.Name + "/" + n
				gap := 20 * time.Second
				if n == p.Human {
					gap = 60 * time.Second // don't spam the human with popups
				}
				if time.Since(invited[k]) < gap {
					continue
				}
				invited[k] = time.Now()
				lead.send(newPkt(0x02c4).str(n, 24).b)
			}
		}
	}
}

// startHumanParty pins the given characters and registers a human-led party.
func startHumanParty(human string, members []*Bot, chars []string, hold time.Duration) *Party {
	name := fmt.Sprintf("%.18s pt", human)
	// leave any current parties
	for _, m := range members {
		if p := partyOf(m); p != nil {
			disband(p)
		}
	}
	p := &Party{Name: name, Leader: members[0], Members: members, Map: "", Formed: time.Now(), Human: human}
	parties.Lock()
	if old := parties.byName[name]; old != nil {
		// re-registered (API / mind): keep what the party already did - even share is set once (Ayla's log showed
		// "Party EXP set to Even Share" every 1-3 min), and the rally/hunting-map memory stays
		p.ShareSet, p.Rally, p.Checkpoint = old.ShareSet, old.Rally, old.Checkpoint
		for _, m := range old.Members {
			delete(parties.ofBot, m)
		}
	}
	parties.byName[name] = p
	for _, m := range members {
		parties.ofBot[m] = p
	}
	parties.Unlock()
	for i, m := range members {
		m.mu.Lock()
		m.pin = &Pin{Char: chars[i], Human: human, Hold: time.Now().Add(hold)}
		wrongChar := m.Online && !strings.EqualFold(m.charName(), chars[i])
		m.mu.Unlock()
		mem := memoryOf(m.P)
		mem.meet(human, "partymate", "", true)
		for _, o := range members {
			if o != m {
				mem.meet(o.charName(), "partymate", "", false)
			}
		}
		if wrongChar || (hold > 0 && m.Online) {
			m.kickSession()
		}
		select { // if they're offline waiting on their schedule, log in now
		case m.wake <- struct{}{}:
		default:
		}
	}
	return p
}

// stopHumanParty unpins the members and lets them go back to their own lives.
func stopHumanParty(p *Party) {
	for _, m := range p.Members {
		m.mu.Lock()
		m.pin = nil
		m.mu.Unlock()
	}
	disband(p)
}

// kickSession ends the current session (the run loop logs back in according to pin/schedule).
func (b *Bot) kickSession() {
	if b.alwaysOnline() { // guild leaders stay logged in (RULES.md 3b)
		return
	}
	b.wmu.Lock()
	if b.conn != nil {
		b.conn.Close()
	}
	b.wmu.Unlock()
}

// ---------------------------------------------------------------- persistence (survives Legion restarts)

type savedHumanParty struct {
	Human   string   `json:"human"`
	Members []string `json:"members"`
}

func humanPartyFile() string { return filepath.Join(*dataDir, "human-parties.json") }

func saveHumanParties() {
	if !ownsHumanParties() { // main instance with -exclude: the core instance owns the file (instances.go)
		log.Printf("human parties: not saved, the core instance owns %s", humanPartyFile())
		return
	}
	parties.Lock()
	var out []savedHumanParty
	for _, p := range parties.byName {
		if p.Human == "" {
			continue
		}
		s := savedHumanParty{Human: p.Human}
		for _, m := range p.Members {
			if m.pin != nil {
				s.Members = append(s.Members, m.pin.Char)
			}
		}
		out = append(out, s)
	}
	parties.Unlock()
	b, _ := json.MarshalIndent(out, "", "  ")
	os.WriteFile(humanPartyFile(), b, 0644)
}

func loadHumanParties(byName map[string]*Bot) {
	if !ownsHumanParties() { // main instance with -exclude: the core instance runs the human-led parties
		return
	}
	raw, err := os.ReadFile(humanPartyFile())
	if err != nil {
		return
	}
	var saved []savedHumanParty
	if json.Unmarshal(raw, &saved) != nil {
		return
	}
	for _, s := range saved {
		var ms []*Bot
		var chars []string
		for _, n := range s.Members {
			if m := byName[strings.ToLower(n)]; m != nil {
				ms, chars = append(ms, m), append(chars, n)
			}
		}
		if len(ms) > 0 {
			startHumanParty(s.Human, ms, chars, 0)
			log.Printf("restored human party for %s: %v", s.Human, chars)
		}
	}
}

// humanLevels: base level of human players, from party member lists (0x0ae5 / 0x0ae4)
var humanLevels sync.Map

func humanLevel(name string) int {
	if v, ok := humanLevels.Load(name); ok {
		return v.(int)
	}
	return 0
}

// checkpointWait (b.mu held): a human's party member holding at the checkpoint town because the leader's map is too
// deadly for the party's weakest member stays put, says why once in party chat (so the leader / Ayla's mind can
// react), and shows it in its state - it used to fall through to hunt() in town and show "roaming".
func (b *Bot) checkpointWait(moving bool, now time.Time) bool {
	p := partyOf(b)
	if p == nil || p.Human == "" || !b.waitingAtCheckpoint || b.waitDanger == "" || b.MapName != b.Goal.Map || !isTown(b.MapName) {
		return false
	}
	if b.underAttack(now) {
		return false
	}
	b.target = 0
	if now.After(b.buffAt["dangersay"]) {
		b.buffAt["dangersay"] = now.Add(10 * time.Minute)
		died := 0
		for _, v := range b.deathLog {
			if v.m == b.waitDanger && now.Sub(v.at) < 15*time.Minute {
				died++
			}
		}
		town, dm := b.MapName, b.waitDanger
		line := pick("too rough in "+dm+" for me rn, waiting in "+town, "ill wait in "+town+", "+dm+" is killing us", "waiting in "+town+" til "+dm+" calms down")
		if died >= 2 {
			line = pick("died twice in there, waiting in "+town, "died "+fmt.Sprint(died)+"x in "+dm+", ill wait in "+town)
		} else if died == 1 {
			line = pick("died in there, waiting in "+town+" for now", "got killed in "+dm+", waiting in "+town)
		}
		typing := b.P.Typing
		go b.partySay(styled(typing, line))
	}
	if !b.Sitting && !moving && !b.poisoned(now) && now.Sub(b.sitSentAt) > 20*time.Second && b.basicLv() >= 3 {
		b.sitSentAt = now
		go b.sit()
	}
	b.setState("waiting in " + b.MapName + ": " + b.waitDanger + " too dangerous")
	return true
}
