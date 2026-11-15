package main

// Parties: Legion forms real rAthena parties (create 0x01e8, invite 0x02c4, accept 0x02c7) of 2-5 bots that hunt
// together: the leader picks targets, damage dealers follow and assist, and an acolyte-line member heals and
// keeps Blessing / Increase AGI up. Masterminds can also form parties through the API.

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// pubState is a lock-free snapshot each bot publishes after thinking, so party members can read each other.
type pubState struct {
	Name     string
	Map      string
	X, Y     int
	LX, LY   int // together.go livePos: interpolated position mid-walk (X/Y is the walk's start until it ends)
	HPFrac   float64
	Target   uint32
	Online   bool
	Support  bool
	Tank     bool // partyroles.go roleTank: a front-liner (party strength, lfg.go partyStrength)
	AID      uint32
	Lv       int    // base level
	Power    int    // effective level from level + gear (gearpower.go powerLv)
	Back     bool   // together.go: a ranged / support member - tanks and melee keep monsters off it
	Mobbed   int    // together.go: monsters hitting it right now (the tank comes to help at 3+)
	Help     bool   // together.go: it called for help in party chat in the last 45 s (danger.go helpcall)
	Fragile  bool   // mage family / Alchemist line (ai.go fragileClass): dies first in a crowd (party map pickers rate it partyFragileLv lower)
	EscortAt string // escort.go: a member waiting at this safe map for the party to cross a dangerous map together
	Known    bool   // the server told us the character's level (BaseLv > 0); Lv/Power are the persona's lv1 default otherwise
	// rally.go: where the respawn point is saved, and waiting at the checkpoint town on purpose
	SavedTown string
	Waiting   bool
	Collect   string    // a bot party leader: the member it is walking to collect (that member waits)
	Goal      string    // crowd.go: the map the bot is hunting on / heading to
	Roaming   bool      // crowd.go: nothing to fight right now
	Trip      bool      // town.go: on a town trip (heal / sell / gear) - a detour, whatever its goal map says
	Errand    bool      // errand.go: away on a necessary step ("brb, renting a peco"), or an alchemy guide trip: nobody collects, waits or rests for it (the party keeps farming)
	Resting   bool      // together.go: resting for HP/SP - the party holds position around it
	DX, DY    int       // tanklead.go: where the bot's current walk ends (the group's direction, for the tank's front)
	AoE       bool      // aoe.go: a Mage-line caster with an area spell - the tank gathers crowds in front of it (wizard lure)
	CastUntil time.Time // aoe.go: casting an area spell until then - the tank holds the pack still
	// gearscore.go: the meter's session counters (kills, fair kills, deaths) for the party performance step
	MKills, MFair, MDeaths int
}

var pubs sync.Map // name -> *atomic.Value(pubState)

func (b *Bot) publish() {
	b.restAnnounce(time.Now()) // together.go: "need sp, resting" / "ok ready" - drives the published Resting
	v, _ := pubs.LoadOrStore(b.P.Name, &atomic.Value{})
	collect := ""
	if b.collectName != "" && time.Since(b.collectAt) < time.Duration(tune().LeaderCollectSec)*time.Second {
		collect = b.collectName
	}
	lx, ly := b.livePos(time.Now())
	v.(*atomic.Value).Store(pubState{Name: b.charName(), Map: b.MapName, X: b.X, Y: b.Y, LX: lx, LY: ly, HPFrac: b.hpFrac(),
		Target: b.target, Online: b.Online, Support: b.isHealer(), Tank: partyRole(b) == roleTank, AID: b.aid, Lv: b.BaseLvOr(), Power: b.powerLv(), Known: b.BaseLv > 0, EscortAt: b.escWaitAt, Fragile: fragileClass(b.Class), Back: partyRole(b) == roleRanged || partyRole(b) == roleSupport, Mobbed: b.mobbedNow(), Help: time.Now().Before(b.buffAt["helpcall"]),
		SavedTown: b.savedTown, Waiting: b.waitingAtCheckpoint, Collect: collect, Goal: b.Goal.Map, Roaming: b.State == "roaming", Trip: b.trip != nil, Errand: b.errand != nil || b.Goal.Set == "alchemy", Resting: b.restOn, DX: b.DestX, DY: b.DestY,
		AoE: b.hasAoESpell(), CastUntil: b.aoeCastUntil, MKills: int(b.meter.sess.Kills), MFair: int(b.meter.sess.Fair), MDeaths: int(b.meter.sess.Deaths)})
}

func peek(name string) (pubState, bool) {
	if v, ok := pubs.Load(name); ok {
		if s, ok := v.(*atomic.Value).Load().(pubState); ok {
			return s, true
		}
	}
	return pubState{}, false
}

type Party struct {
	Name       string
	Leader     *Bot
	Members    []*Bot // includes leader
	Map        string
	Formed     time.Time
	Human      string // human-led party: the real player the members follow
	ShareSet   bool   // even exp share requested (human-led parties)
	Checkpoint string // regroup town: nearest town to where the party hunts
	// town runs are a party event (owner: "They should all do town run together ... apply the fix to other parties
	// as a general rule"): when the leader goes to town, or a member needs town, everyone goes and does their own
	// selling/restocking there. Guarded by parties.Mutex.
	TownRunAt   time.Time
	TownRunTown string
	TownRunBy   string
	// savepoints.go (EC05/PT03): the human leader asked the party to save at her Kafra ("save here", "save at the
	// kafra"): every member in that town saves there too. Guarded by parties.Mutex.
	SaveAskTown string
	SaveAskAt   time.Time
	// party rally at the Kafra nearest the destination (rally.go). Guarded by parties.Mutex.
	Rally rallyState
	// restored from data/parties.json after a Legion restart (partypersist.go): the 90-min refresh counts from here
	Restored time.Time
	// map changes (moveTo): when the party last moved and the maps it left, so the pickers don't send it straight
	// back (A -> B -> A) and partyFitCheck doesn't move it again right after a move. Guarded by parties.Mutex.
	MovedAt time.Time
	Left    map[string]time.Time
	// lfg.go need-driven parties (owner 2026-11-14 "no permanent party, party should come from need"). Guarded by
	// parties.Mutex. Need: formed (or recruiting) around goals; Purpose: what it farms ("farming Ninja Suit @
	// pay_dun01"); Anchor: the map of an item goal (kept while safe even when exp-poor); CheckAfter: members' goal
	// checks start then; EndAt: an old permanent party ends then; Diverge: since when a member's goal isn't served.
	Need       bool
	Purpose    string
	Anchor     string
	CheckAfter time.Time
	EndAt      time.Time
	Diverge    map[*Bot]time.Time
	AnchorItem int       // the item farmed on Anchor (goalMapSafeItem: an exp-poor map only for a valuable item)
	ArrivedAt  time.Time // the leader reached Map after the last move (partyFitCheck: settle before re-fitting)
	// fewer than 2 members online since (partyManager keeps the party 10 min before disbanding it)
	LowSince time.Time
}

// partyLeftRecently (parties.Mutex held): the party left map m in the last 30 minutes.
// Also true for a map its LEADER's earlier parties left or were killed / chased off (leaderMem: survives re-forming).
func (p *Party) partyLeftRecently(m string, now time.Time) bool {
	if at, ok := p.Left[m]; ok && now.Sub(at) < 30*time.Minute {
		return true
	}
	if p.Leader != nil {
		bad, left := leaderAvoids(p.Leader.P.Name, m, now)
		return bad || left
	}
	return false
}

// townOf: the town a map belongs to ("" if it isn't a town or one of its buildings).
func townOf(m string) string {
	if isTown(m) {
		return m
	}
	for t, ins := range townInteriors {
		for _, in := range ins {
			if in == m {
				return t
			}
		}
	}
	return ""
}

// callTownRun starts a party town run unless one happened in the last 6 minutes; returns true if it did. Used for
// emergencies (a member with no weapon at all, a bow without arrows) and the human leader's own runs.
func (p *Party) callTownRun(town, by string, now time.Time) bool {
	return p.callTownRunGap(town, by, 6*time.Minute, now)
}

// callTownRunGap: a party town run only if the last one is at least gap ago. Members' non-emergency needs (bag full,
// low on potions) use the trip interval (45 min): ONE party run every N minutes, not one per member need (QA
// 2026-11-14: 64% of Butterfly Wings were party town runs, called every ~6 min by members' class-weapon asks).
func (p *Party) callTownRunGap(town, by string, gap time.Duration, now time.Time) bool {
	parties.Lock()
	defer parties.Unlock()
	last := p.TownRunAt
	if gap > 6*time.Minute {
		// TownRunAt isn't saved across a Legion restart: count the restart as a run 25 min ago, so a deploy doesn't
		// open the gate for every party at once (QA 06:05 deploy: runs per party-hour doubled, 0.41 -> 0.79)
		last = later(last, townRunBoot.Add(-25*time.Minute))
	}
	if now.Sub(last) < gap {
		return false
	}
	p.TownRunAt, p.TownRunTown, p.TownRunBy = now, town, by
	return true
}

// townRunBoot: when this Legion process started (callTownRunGap)
var townRunBoot = time.Now()

func later(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func (p *Party) townRun() (time.Time, string, string) {
	parties.Lock()
	defer parties.Unlock()
	return p.TownRunAt, p.TownRunTown, p.TownRunBy
}

var parties = struct {
	sync.Mutex
	byName map[string]*Party
	ofBot  map[*Bot]*Party
}{byName: map[string]*Party{}, ofBot: map[*Bot]*Party{}}

func partyOf(b *Bot) *Party {
	parties.Lock()
	defer parties.Unlock()
	return parties.ofBot[b]
}

// formParty makes the leader create a party and invite the members; members accept when the invite arrives.
func formParty(leader *Bot, members []*Bot, mapName string) *Party {
	return formPartyOpt(leader, members, mapName, true, nil)
}

// leadersOut: the members minus guild leaders (live 2026-11-14: guild10's leader Star.soul was grouped into
// thesilversoul's party by the old same-map grouping while goal parties were off).
func leadersOut(ms []*Bot) []*Bot {
	out := ms[:0:0]
	for _, m := range ms {
		if !m.alwaysOnline() {
			out = append(out, m)
		}
	}
	return out
}

// formPartyOpt: formParty; refit false keeps mapName as it is (an item-goal map, lfg.go: farming a drop may be
// exp-poor, it only has to be safe - checked by the caller); setup (parties.Mutex held) fills the party's need fields.
func formPartyOpt(leader *Bot, members []*Bot, mapName string, refit bool, setup func(*Party)) *Party {
	members = leadersOut(members) // guild leaders lead their own party, never someone else's (owner rule 3b)
	if len(members) == 0 {
		return nil
	}
	if refit {
		all := append([]*Bot{leader}, members...)
		if m := partyMapFor(all, mapName); !partyBeginnerFar(all, m) { // fit for the weakest member (ai.go), near home for beginners
			mapName = m
		}
	}
	p := &Party{Name: "", Leader: leader, Members: append([]*Bot{leader}, members...), Map: mapName, Formed: time.Now(),
		Checkpoint: nearestTown(mapName)}
	parties.Lock()
	for _, m := range p.Members {
		if old := parties.ofBot[m]; old != nil {
			parties.Unlock()
			return nil
		}
	}
	// letters, digits and spaces only: rAthena (char_name_option 1) refused "Name's pt" (partypersist.go)
	name := botPartyName(leader.charName())
	p.Name = name
	// every party formed from now on is a need party (lfg.go): mind/API parties too - their members' goals are checked
	// like any other's; only parties restored from before the change count as old permanent ones
	p.Need, p.Purpose = true, "lvling @ "+mapName
	if setup != nil {
		setup(p)
	}
	parties.byName[name] = p
	for _, m := range p.Members {
		parties.ofBot[m] = p
	}
	parties.Unlock()
	markPartiesDirty()
	go func() {
		leader.mu.Lock()
		leader.PartyName = name
		leader.mu.Unlock()
		leader.send(newPkt(0x01e8).str(name, 24).u8(1).u8(0).b)
		time.Sleep(1500 * time.Millisecond)
		leader.send(newPkt(0x07d7).u32(1).u8(1).u8(0).b) // even exp share: supports and carried members level too
		for _, m := range members {
			leader.send(newPkt(0x02c4).str(m.charName(), 24).b)
			time.Sleep(time.Duration(300+leader.rng.Intn(700)) * time.Millisecond)
		}
		for _, m := range p.Members {
			mem := memoryOf(m.P)
			for _, o := range p.Members {
				if o != m {
					mem.meet(o.charName(), "partymate", mapName, false)
				}
			}
			m.mu.Lock()
			m.Goal = Goal{Mode: "party", Map: mapName, Who: leader.charName(), Set: "party"}
			m.route, m.path = nil, nil
			m.mu.Unlock()
		}
		leader.mu.Lock()
		leader.Goal = Goal{Mode: "hunt", Map: mapName, Set: "party-leader"}
		typing := leader.P.Typing
		go func() {
			time.Sleep(8 * time.Second)
			leader.partySay(styled(typing, "ok party up! "+planLine(p, mapName, "levelling spot for all of us"))) // together.go: the plan, not "follow me"
		}()
		leader.mu.Unlock()
		leader.event("party", "", fmt.Sprintf("formed %s with %d members on %s", name, len(p.Members), mapName))
		p.rallyOnForm() // members on other maps: meet at the destination's Kafra and save there first (rally.go)
	}()
	return p
}

func disband(p *Party) {
	parties.Lock()
	delete(parties.byName, p.Name)
	for _, m := range p.Members {
		if parties.ofBot[m] == p {
			delete(parties.ofBot, m)
		}
	}
	parties.Unlock()
	markPartiesDirty()
	for _, m := range p.Members {
		lfgNoteLeft(m) // lfg.go: a short pause before looking for the next party
		go func(m *Bot) {
			m.send(newPkt(0x0100).b) // leave party
			m.mu.Lock()
			m.PartyName, m.PartyID = "", 0
			if m.Goal.Set == "party" || strings.HasPrefix(m.Goal.Set, "party-leader") {
				m.Goal = Goal{}
			}
			m.mu.Unlock()
		}(m)
	}
}

// partyManager periodically groups compatible bots hunting on the same map.
func partyManager(bots []*Bot, stop <-chan struct{}) {
	restoreBotParties(bots)       // partypersist.go: parties saved before the last restart
	go botPartyKeeper(bots, stop) // in-game party in step with the engine's, data/parties.json
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	lastFit := map[*Party]time.Time{}
	for {
		select {
		case <-stop:
			return
		case <-t.C:
		}
		// drop parties whose members went offline or scattered for long
		parties.Lock()
		var stale []*Party
		for _, p := range parties.byName {
			on := 0
			for _, m := range p.Members {
				if s, ok := peek(m.P.Name); ok && s.Online {
					on++
				}
			}
			if p.Human != "" {
				continue // kept until stopped through the API
			}
			if isRestoring(p) {
				continue // members are still logging back in after a restart (partypersist.go)
			}
			// refresh after 90 min so groups reshuffle, but a party that is farming well (everyone online, on the
			// party map) keeps going for up to 4 h
			age := time.Since(p.Formed)
			if p.Restored.After(p.Formed) {
				age = time.Since(p.Restored)
			}
			// lfg.go (owner 2026-11-14 "party should come from need"): need parties end when their goals do
			// (goalPartyCheck, partyGoalFit), not on the old 90-min refresh
			refresh := !goalPartiesOn() && age > 90*time.Minute && (age > 4*time.Hour || !p.farmingWell())
			// fewer than 2 online: kept 10 min first - a rathena restart / mass relog takes everyone offline at once
			// (2026-11-14 17:03: the rathena recreate after a deploy disbanded all ~350 parties and the saver wrote
			// the empty set to parties.json)
			low := false
			if on < 2 {
				if p.LowSince.IsZero() {
					p.LowSince = time.Now()
				}
				low = time.Since(p.LowSince) > 10*time.Minute
			} else {
				p.LowSince = time.Time{}
			}
			if low || (refresh && huntOfLocked(p) == nil) {
				stale = append(stale, p)
			}
		}
		parties.Unlock()
		for _, p := range stale {
			disband(p)
		}
		partyFitCheck(lastFit)
		if goalPartiesOn() {
			continue // lfg.go forms parties from goals (LFG/LFM), no same-map auto grouping
		}
		// candidates: online, hunting, not vending/in town, not in a party
		byMap := map[string][]*Bot{}
		for _, b := range bots {
			b.mu.Lock()
			ok := b.Online && b.Goal.Mode == "hunt" && b.Goal.Set != "party-leader" && b.MapName == b.Goal.Map &&
				b.vend == nil && b.trip == nil && b.charRole() != "vendor" && b.pin == nil && b.basicLv() >= 5
			m := b.MapName
			b.mu.Unlock()
			if ok && partyOf(b) == nil {
				byMap[m] = append(byMap[m], b)
			}
		}
		for m, cands := range byMap {
			if len(cands) < 2 {
				continue
			}
			formRoleParties(m, cands)
		}
	}
}

// partyStayTime: how long a party keeps the map it arrived at (owner rule 2026-11-14, RULES.md 3: "pick one hunting
// map and stay ~2 hours; change only for a real reason"). Town runs come back to it.
const partyStayTime = 2 * time.Hour

// mapStayOK: the party may stay on m during its stay: safe for the weakest member (not deadly, not too hard, no
// swarm / boss note), monsters to fight (not empty) and not outgrown by far (the weakest member's effective level at
// most 12 over the band's top; off the guide list, monsters averaging at most 25 under it). No bot locks.
func mapStayOK(ms []*Bot, m string) bool {
	minLv, _, known, _ := partyFitLv(ms)
	if known == 0 {
		return true
	}
	if partyMapDeadly(m) || partyMapTooHard(m, minLv) || swarmVeto(m, minLv) || bossRecent(m, minLv) || mapReadsEmpty(m) {
		return false
	}
	if bad, _ := leaderAvoids(ms[0].P.Name, m, time.Now()); bad {
		return false
	}
	minBase := partyMinBase(ms)
	listed := false
	for _, g := range guideSpots {
		if g.Map == m {
			listed = true
			if minLv <= g.Max+12 && minBase <= g.Max+20 { // nor 20+ base levels over it (Star.soul pt lv50-64 on lv13-27)
				return true
			}
		}
	}
	if listed {
		return false
	}
	ml := mapMeanLv(m)
	return ml <= 0 || ml >= minLv-25
}

// fitHold: the fit check leaves a party's map alone - it is on its way there (the leader is on another map that is
// within 45 min of the move) or arrived less than 10 min ago - unless the map is killing it (urgent). After arrival
// the ~2 h stay rule (partyStayTime / mapStayOK) takes over; a town run returns to the same map.
func fitHold(leaderMap, partyMap string, sinceMove, sinceArrive time.Duration, notArrived, urgent bool) bool {
	if urgent {
		return false
	}
	if notArrived {
		return leaderMap != "" && leaderMap != partyMap && sinceMove < 45*time.Minute
	}
	return sinceArrive < 10*time.Minute
}

// partyFitCheck: every 5 minutes per bot party, the party map is checked against the weakest member (members level
// apart, a weak member joins, gear changes, a swarm or boss note appears): a map that no longer suits it is left for
// the nearest one that does (QA 2026-11-14: 17 of 368 parties sat on maps the pickers veto for their weakest member;
// Kimchi07's party kept hunting mjolnir_06's Poison Spore swarm while he fled). Called from partyManager.
func partyFitCheck(last map[*Party]time.Time) {
	now := time.Now()
	parties.Lock()
	type fc struct {
		p    *Party
		ms   []*Bot
		m    string
		led  *Bot
		anc  bool // lfg.go: an item goal's map
		itm  int
		stay bool // within the ~2 h stay on the map it arrived at (RULES.md 3)
	}
	var list []fc
	alive := map[*Party]bool{}
	for _, p := range parties.byName {
		alive[p] = true
		// a map that is killing the party (leader ban after 2 deaths, deadly right now) is checked every minute,
		// whatever the settle time (QA 17:47-18:18: Zero.dragon died 5x on moc_fild03 while its party stayed)
		urgent := false
		if p.Leader != nil && p.Map != "" {
			bad, _ := leaderAvoids(p.Leader.P.Name, p.Map, now)
			urgent = bad || partyMapDeadly(p.Map)
		}
		// a party that set out for its map keeps it until it arrives and settles (QA 19:00-19:14: TheShadowsenpai pt
		// was re-pointed gef_fild02 -> pay_dun01 -> pay_fild10 before reaching pay_dun01, 15 min walking, no farming)
		if p.Leader != nil {
			if s, ok := peek(p.Leader.P.Name); ok && s.Online && s.Map == p.Map && (p.ArrivedAt.IsZero() || p.ArrivedAt.Before(p.MovedAt)) {
				p.ArrivedAt = now
			}
		}
		hold := false
		if p.Leader != nil && !p.MovedAt.IsZero() {
			s, _ := peek(p.Leader.P.Name)
			hold = fitHold(s.Map, p.Map, now.Sub(p.MovedAt), now.Sub(p.ArrivedAt), p.ArrivedAt.Before(p.MovedAt), urgent)
		}
		// first sight of a party (a restart, a new party): spread its first check over 0-5 min - a shared first round
		// moved ~15 parties in the same second at 20:01:41 ("next: X (too rough here...)")
		if _, seen := last[p]; !seen && !urgent {
			last[p] = now.Add(-time.Duration(crand(300)) * time.Second)
			continue
		}
		if p.Human != "" || p.Leader == nil || p.Map == "" || isTown(p.Map) || hold ||
			!urgent && (now.Sub(last[p]) < 5*time.Minute || now.Sub(p.Formed) < 2*time.Minute || now.Sub(p.MovedAt) < 15*time.Minute) ||
			urgent && now.Sub(last[p]) < time.Minute {
			continue
		}
		last[p] = now
		// owner rule 2026-11-14 (RULES.md 3): one hunting map for ~2 hours once there; only a real reason moves it
		stay := !urgent && !p.ArrivedAt.IsZero() && !p.ArrivedAt.Before(p.MovedAt) && now.Sub(p.ArrivedAt) < partyStayTime
		list = append(list, fc{p, append([]*Bot(nil), p.Members...), p.Map, p.Leader, p.partyAnchored(), p.AnchorItem, stay})
	}
	parties.Unlock()
	for p := range last {
		if !alive[p] {
			delete(last, p)
		}
	}
	moves := 0 // relocations this round (fitMovesPerRound): parties change maps one by one, like people, not as a wave
	for _, c := range list {
		if isRestoring(c.p) || c.p.bossActive() { // bosshunt.go decides a boss hunt's map
			continue
		}
		if c.anc && goalMapSafeItem(c.ms, c.m, c.itm) {
			continue // lfg.go: an item goal's map stays while safe (little exp is fine, the drop is the point)
		}
		// judge only on real levels: a member still logging in (after every deploy) reads as the persona's lv1 default,
		// which sent 66 parties of lv45+ to Novice fields (QA 2026-11-14 07:20)
		// an unknown level blocks the check only while it can be a login wave (first 5 min after a start, or most of
		// the party unknown): one member stuck "online" without a level (vexfox, Star.soul pt) froze it for good
		_, _, known, unknown := partyFitLv(c.ms)
		if known == 0 || (unknown > 0 && (time.Since(townRunBoot) < 5*time.Minute || unknown*2 >= known+unknown)) {
			delete(last, c.p) // look again on the next round
			continue
		}
		// a safe map that gives the weakest member little exp (ai.go mapExpPoor: lv45+ parties of gear-poor Thieves
		// and Merchants on prt_fild07 / pay_fild0x) is left only when a safe map with real exp exists nearby
		poor, crowded := false, false
		// a member that can't reach the party map safely (its walk crosses a map too hard for it), or that died there
		// twice within 30 min (its badMap), makes the map too hard for the party (QA: priests Onirogue 7 deaths and
		// xXYukiqueenXx 5 crossing mjolnir_02 to parties on mjo_dun01/02)
		hurt := ""
		for _, m := range c.ms {
			s, ok := peek(m.P.Name)
			if !ok || !s.Online || !s.Known {
				continue
			}
			m.mu.Lock()
			bad := m.badMap == c.m && now.Before(m.badMapUntil)
			rate := m.soloRateLv(m.powerLv())
			m.mu.Unlock()
			if bad || s.Map != c.m && townOf(s.Map) == "" && routeCrossesTooHard(s.Map, s.X, s.Y, c.m, rate) { // from a town it takes the Kafra around (swarm.go)
				hurt = m.P.Name
				break
			}
		}
		if hurt != "" {
			if bad, _ := leaderAvoids(c.led.P.Name, c.m, now); !bad {
				leaderNoteBad(c.led.P.Name, c.m, leaderFleeBan)
			}
		} else if c.stay && mapStayOK(c.ms, c.m) && !(mapCrowded(c.m) && partyMapSearch(c.ms, nearestTown(c.m), townSpawn[nearestTown(c.m)][0], townSpawn[nearestTown(c.m)][1], c.m, partyAnySafe, true) != "") {
			continue // the ~2 h stay: safe, not packed, not outgrown by far - a slightly better map is no reason to move
		} else if partyMapOK(c.ms, c.m) {
			minBase := partyMinBase(c.ms)
			switch {
			case !c.stay && mapExpPoor(c.m, minBase) && partyMapNearTier(c.ms, nearestTown(c.m), townSpawn[nearestTown(c.m)][0], townSpawn[nearestTown(c.m)][1], c.m, false) != "":
				poor = true
			case mapCrowded(c.m) && partyMapSearch(c.ms, nearestTown(c.m), townSpawn[nearestTown(c.m)][0], townSpawn[nearestTown(c.m)][1], c.m, partyAnySafe, true) != "":
				// crowd.go: over its cap / its bots roam / it reads thin, and a fitting map with room exists (QA 15:30:
				// pay_fild07 48 bots, 24 roaming with 0 monsters seen)
				crowded = true
			default:
				continue
			}
		}
		s, ok := peek(c.led.P.Name)
		if !ok || !s.Online {
			continue
		}
		x, y := s.X, s.Y
		if s.Map != c.m {
			x, y = -1, -1 // the leader is elsewhere: measure from the map's own town
		}
		minLv, _, _, _ := partyFitLv(c.ms)
		// evidence first (owner: real reasons only): a map the model rates a bit too hard is left only when the party
		// has died there lately, the map kills people right now, or it is a train map / behind one (always deadly);
		// a party that farms it without deaths stays
		if hurt == "" && !crowded && !poor && !partyMapTooEasy(c.ms, c.m) && !partyMapDeadly(c.m) &&
			!partyMapTrain(c.m, minLv) && !mapAccessTooHard(c.m, minLv) && !leaderDiedHere(c.led.P.Name, c.m, now) {
			if bad, _ := leaderAvoids(c.led.P.Name, c.m, now); !bad {
				continue
			}
		}
		if moves >= fitMovesPerRound && hurt == "" && !partyMapDeadly(c.m) {
			delete(last, c.p) // next round (30 s): one by one
			continue
		}
		moves++
		why := fmt.Sprintf("%s no longer suits our weakest member (lv %d)", c.m, minLv)
		if hurt != "" {
			why = fmt.Sprintf("%s is too dangerous for %s (or the way there)", c.m, hurt)
		} else if crowded {
			why = fmt.Sprintf("%s is packed (%d%% of what its monsters feed)", c.m, crowdPct(c.m))
		} else if partyMapTooEasy(c.ms, c.m) || poor {
			why = fmt.Sprintf("%s is too easy for us (weakest lv %d, base lv %d)", c.m, minLv, partyMinBase(c.ms))
		}
		// together.go: no map change while a member rests - unless the map is too hard / killing us (QA 19:00-19:15:
		// parties on mjo_dun01/02 always had someone "resting" after the mjolnir_02 trains, so they never left)
		if !c.p.resting() || hurt != "" || !poor && !crowded {
			go c.p.relocate(c.m, x, y, why)
		} else {
			delete(last, c.p) // resting (two priests sit a lot): look again on the next round, not in 5 min
		}
	}
}

// fitMovesPerRound: at most this many fit-check relocations per partyManager round (30 s), urgent ones excepted.
const fitMovesPerRound = 2

// leaderDiedHere: a member of this leader's parties died on m in the last leaderDeathWin (party.go leaderMem).
func leaderDiedHere(lead, m string, now time.Time) bool {
	leaderMem.Lock()
	defer leaderMem.Unlock()
	n := leaderMem.m[lead][m]
	return n != nil && n.deaths > 0 && now.Sub(n.firstAt) < leaderDeathWin
}

// partyThink runs for party members (Goal.Mode == "party"); returns true if it took an action.
// partyLeash: general rule (owner: "party should generally stay close to each other while grinding for safety"):
// members only start fights within this many cells of the leader, regroup when farther, and a bot party's leader
// waits for stragglers before pulling the next monster (ai.go hunt).
// (value: tune().PartyLeash, live-tunable in data/tuning.json - see tuning.go)

func (b *Bot) partyThink(gm *GameMap, moving bool, now time.Time) bool {
	p := partyOf(b)
	if p == nil {
		b.Goal = Goal{}
		return false
	}
	lead, ok, members := b.partyView(p, now)
	if b.kafra != nil && b.kafraThink(maps[b.MapName], moving, now) {
		return true // humanparty.go: saving at the party town's Kafra
	}
	if b.escortHold != "" && b.MapName == b.Goal.Map && isTown(b.MapName) {
		// humanparty.go escortGate: waiting in the entrance town to go into her dungeon together
		b.target = 0
		if !b.Sitting && !moving && !b.poisoned(now) && now.Sub(b.sitSentAt) > 20*time.Second && b.basicLv() >= 3 {
			b.sitSentAt = now
			go b.sit()
		}
		b.setState("waiting in " + b.MapName + ": " + b.escortHold)
		return true
	}
	if p.Human != "" && (!ok || lead.Map == "") {
		// the human isn't around yet: hang around where we are, sit after a while - and ask where she is, so her
		// plugin comes to collect us (owner: stranded members ask for help)
		if b.lostSince.IsZero() || now.Sub(b.lostSeenAt) > 10*time.Second {
			b.lostSince = now // a new spell of not knowing where she is
		}
		b.lostSeenAt = now
		if now.Sub(b.lostSince) > 30*time.Second && now.After(b.buffAt["helpsay"]) && townOf(b.MapName) == "" {
			b.buffAt["helpsay"] = now.Add(3 * time.Minute)
			typing, where := b.P.Typing, b.MapName
			go b.partySay(styled(typing, pick("where r u "+p.Human+"? im at "+where, "where r u "+p.Human+"? im in "+where)))
		}
		b.setState("waiting for " + p.Human)
		if !b.Sitting && !moving && !b.poisoned(now) && now.After(b.sitSentAt.Add(20*time.Second)) && b.rng.Float64() < 0.02 {
			b.sitSentAt = now
			go b.sit()
		}
		return true
	}
	if ok && lead.Online && lead.Map != b.MapName && townOf(lead.Map) == b.MapName {
		b.setState("waiting outside for " + lead.Name) // leader is in a shop/building of this town
		return true
	}
	// a bot party's leader on another map: go there (fight back only) instead of hunting alone on our map (QA:
	// members fought 200+ cells / a map away from their leader, never "regrouping")
	// (the decision itself lives in partyGoalSync, ai.go: the member's Goal.Map is set to the leader's map only when the
	// leader has stayed there 60 s, and kept for 60 s - a per-tick goal swap here looped xXZeromonkXx moc_fild02 <->
	// moc_fild01 every 3 s while its leader was itself travelling to the party map)
	if false && ok && lead.Online && lead.Map != "" && lead.Map != b.MapName && p.Human == "" && townOf(lead.Map) == "" {
		if b.underAttack(now) {
			for _, e := range b.ents {
				if e.Type == objMob && now.Sub(e.LastHitMe) < 3*time.Second {
					b.target = e.ID
					b.setState("fighting off " + mobName(e.Class) + " on the way to " + lead.Name)
					b.fight(e, moving, now)
					return true
				}
			}
		}
		b.target = 0
		saved := b.Goal
		b.Goal = Goal{Mode: "party", Map: lead.Map, Who: lead.Name, Set: saved.Set}
		b.travel(gm, moving)
		b.Goal = saved
		b.setState("regrouping with " + lead.Name + " on " + lead.Map)
		return true
	}
	if ok && lead.Online && p.Human == "" && b.leadOff && lead.Map != b.MapName && isTown(b.MapName) && b.Goal.Map == b.MapName {
		// partyGoalSync: the leader is on a detour / behind - wait for it here, at the party's checkpoint Kafra town
		b.target = 0
		// ... and save the respawn point at its Kafra meanwhile (owner rule: save at the Kafra nearest the grinding ground)
		parties.Lock()
		pm := p.Map
		parties.Unlock()
		if b.kafra == nil && b.savedTown != b.MapName {
			if k := b.kafraSavePlan(pm, now); k != nil {
				b.kafra = k
				b.event("info", "", "party: saving at the Kafra in "+b.MapName+" while waiting for "+lead.Name)
			}
		}
		if b.kafraThink(gm, moving, now) {
			return true
		}
		if !b.Sitting && !moving && !b.poisoned(now) && now.Sub(b.sitSentAt) > 20*time.Second && b.basicLv() >= 3 {
			b.sitSentAt = now
			go b.sit()
		}
		b.setState("waiting in " + b.MapName + " for " + lead.Name)
		return true
	}
	if !ok || !lead.Online || lead.Map != b.MapName {
		return false // travel logic (Goal.Map) brings us there
	}
	if p.Human != "" && lead.X == 0 && lead.Y == 0 {
		// on the leader's map but no position yet (the server sends it within a few seconds): stay put, don't roam off
		b.setState("looking for " + lead.Name)
		return true
	}
	support := b.isHealer() // skillbuff.go: Acolyte/Priest line only, never Monks/Champions
	human := p.Human != ""
	b.buffAsk(p, members, now) // priestbuff.go PT29: ask the priest for missing buffs
	// regroup first: a member far from the leader only fights what is hitting it and otherwise walks back to the
	// leader (they used to keep "defending" / "taking aggro" 60-70 cells away while the leader waited)
	// (also while a walk is in progress: with "!moving" in this test a far member that was walking fell through to the
	// fight logic below and engaged monsters 200 cells from its leader)
	mx, my := b.livePos(now)
	llx, lly := livePosOf(lead)
	d0 := dist(mx, my, llx, lly) // together.go: live positions on both sides (b.X is a walk's start until it ends)
	// together.go regroupNeeded: the same hysteresis as the leader's collect (starts beyond together_cells, ends
	// within collect_stop_cells), so a member within 20 cells never starts regrouping / waiting "coming to get me"
	// together.go rest together: someone in the party rests near us and the leader is within restHoldCells -> hold
	// position (no regroup / follow / roam), sit or guard (restGuard below, after the healer's own work)
	rester, restOn := partyRester(members, b.charName(), b.MapName, mx, my)
	restHold := restOn && d0 <= restHoldCells && !(lead.X == 0 && lead.Y == 0)
	if !regroupNeeded(d0, b.regrouping) || lead.X == 0 && lead.Y == 0 || restHold {
		b.regrouping = false // back with the leader (or holding for a rest)
	}
	if d := d0; !restHold && regroupNeeded(d, b.regrouping) && !(lead.X == 0 && lead.Y == 0) && gm != nil {
		if !b.regrouping || d < b.regroupBestD || now.Sub(b.regroupBestAt) > 2*time.Minute {
			b.regroupBestD, b.regroupBestAt = d, now
		}
		b.regrouping = true
		// regroup first (owner rule): a member on its way only fights what hits it - and while it is still getting
		// closer with HP to spare it doesn't even stop for that: it walks on (QA 2026-11-14: Echobunny fought Skeletons
		// "on the way to Ayla" for 5+ min, 58 cells from her, each kill followed by the next hit)
		shaking := ""
		hit := false
		for _, e := range b.ents {
			if e.Type == objMob && now.Sub(e.LastHitMe) < 3*time.Second {
				if !b.fightBackOnWay(now) && (regroupWalkOn(now.Sub(b.regroupBestAt), b.hpFrac()) || support && healerWalkOn(now.Sub(b.regroupBestAt))) {
					shaking = mobName(e.Class)
					continue
				}
				hit = true
				b.target = e.ID
				b.setState("fighting off " + mobName(e.Class) + " on the way to " + lead.Name)
				b.fight(e, moving, now)
				break
			}
		}
		if !hit && shaking == "" && !human && lead.Collect == b.charName() {
			// the leader is walking to collect us: only one side moves (QA: leader and member chased each other).
			// But only while it really comes: not 5+ cells closer within 30 s -> walk to it ourselves.
			if b.collectWaitAt.IsZero() || now.Sub(b.collectWaitAt) > 90*time.Second {
				b.collectWaitAt, b.collectWaitD = now, d
			}
			if now.Sub(b.collectWaitAt) < 30*time.Second || d <= b.collectWaitD-5 {
				if d <= b.collectWaitD-5 {
					b.collectWaitAt, b.collectWaitD = now, d // approaching: keep waiting
				}
				// PT32b (2026-11-15): a member behind its leader does not farm while it waits for the collect - it
				// holds still and lets the leader come (no fighting while more than 20 cells behind)
				b.target = 0
				b.setState("waiting for " + lead.Name + " (coming to get me)")
				return true
			}
		}
		if !hit {
			b.target = 0
			if b.Sitting && now.Sub(b.sitSentAt) > time.Second { // the server ignores walking while seated
				b.sitSentAt, b.wantSit = now, false
				go b.stand()
			}
			if shaking != "" {
				b.setState("regrouping with " + lead.Name + " (shaking off " + shaking + ")")
			} else {
				b.setState("regrouping with " + lead.Name)
			}
			if !moving {
				b.regroupStep(gm, lead, now) // nav.go: other walk region -> go round; unreachable -> ask and wait
			}
		}
		return true
	}
	if b.underAttack(now) && b.escape(gm, b.hpFrac(), moving, now) {
		return true
	}
	// song.go: a partied Bard / Dancer keeps a song up while the party fights (support player)
	if b.songThink(gm, lead, members, moving, now) {
		return true
	}
	// stand up when the leader is around and moving off or a fight starts (the server ignores walking/attacking while sitting)
	// the SAME rule as the priest's sit-down below (no distance limit short of the regroup range, nobody hurt): the
	// stand-up used to require the leader within 8 cells while the sit-down had no limit, so with the leader 9+ cells
	// away the acolyte sat, stood, sat... every few seconds and its SP never regenerated (66/325 for minutes)
	spRest := b.isHealer() && b.MaxSP > 0 && b.SP*100/b.MaxSP < b.priestSitLine(lead) && !b.overRegenWeight() && // heal.go: 70% once the party moves
		dist(b.X, b.Y, lead.X, lead.Y) <= togetherCells() && !b.buffDue(members, now)
	if spRest {
		for _, s := range members {
			if s.Online && s.Map == b.MapName && s.HPFrac < healOthersBelow { // heal.go: the same line healFirst heals at
				spRest = false // someone needs healing: stand
			}
		}
	}
	// owner 2026-11-14 ("priest should prioritize sitting whenever possible instead of fight. If it's not moving or
	// blessing, it sits"): a party healer with nobody to heal, no buff due, the leader close and not walking off, and
	// nothing on it sits - even at high SP. This idle sit is NOT a party rest: state "sitting by ...", no wantSit, so
	// together.go restAnnounce says nothing and the party keeps farming. Only the low-SP rest below is announced.
	idleSit := support && b.healerIdleSit(lead, members, mx, my, llx, lly, now)
	// the party rest is over (the last "ready", together.go): a member seated for it stands up and moves on at once
	// (unless it is a healer that still needs SP: then it is resting itself and says so)
	if b.Sitting && !restHold && !spRest && !idleSit && !b.restSitAt.IsZero() && now.Sub(b.restSitAt) < 2*time.Minute &&
		now.Sub(b.sitSentAt) > time.Second {
		b.restSitAt, b.sitSentAt, b.wantSit = time.Time{}, now, false
		b.setState("rest over, moving on")
		go b.stand()
		return true
	}
	if support && b.threatNear(members, 5, now) {
		spRest = false // a monster comes at us or a member within 5 cells: stand up (heal / flee), even mid-rest
	}
	if b.Sitting && !idleSit && !(spRest && !b.underAttack(now) && !b.fightOn(now)) && !(restHold && !b.underAttack(now) && !b.fightOn(now)) && (dist(b.X, b.Y, lead.X, lead.Y) > 3 || lead.Target != 0 || b.underAttack(now) || b.fightOn(now) || support) { // support: not idle any more (a heal / buff is due, the party moves)
		b.wantSit = false
		b.noSitUntil = now.Add(15 * time.Second) // ai.go hunt: don't sit straight back down (SIT-FLICKER, QA 2026-11-14)
		if now.After(b.sitSentAt.Add(time.Duration(300+b.P.ReactionMs) * time.Millisecond)) {
			b.sitSentAt = now
			b.setState("standing up")
			go b.stand()
		}
		return true
	}
	// support: heal the most hurt member, then keep buffs up
	if support {
		var worst *pubState
		for _, s := range members {
			if s.Online && s.Map == b.MapName && s.HPFrac < healOthersBelow &&
				healReachable(maps[b.MapName], b.X, b.Y, s.X, s.Y, healRange) && (worst == nil || s.HPFrac < worst.HPFrac) {
				ss := s
				worst = &ss
			}
		}
		// healerstick.go: heals on a member over 50% HP no longer starve the leader's Blessing / Increase AGI (live
		// 2026-11-15: on gef_fild10 someone was always under 80%, every skill slot went to Heal, Ayla's buffs ran out)
		if worst != nil && now.After(b.skillReady) && !(worst.HPFrac >= 0.5 && b.leaderBuffDue(p, members, now)) {
			if si := b.skills[alHeal]; si != nil && si.Lv > 0 && b.SP < si.SP {
				b.drinkSP(now) // someone needs healing and SP is short: SP potion
			}
			if si := b.skills[alHeal]; si != nil && si.Lv > 0 && b.SP >= si.SP {
				if id := b.entityID(worst.Name); id != 0 || worst.Name == b.charName() {
					if worst.Name == b.charName() {
						id = b.aid
					}
					b.skillReady = now.Add(900 * time.Millisecond)
					b.setState("healing " + worst.Name)
					go b.useSkill(alHeal, si.Lv, id)
					return true
				}
			}
		}
		// SP is for healing first (owner: "why is the acolyte not healing party members?" - it was at 38/347 SP from
		// re-buffing four members every 2.5 min): buff only above 60% SP; with low SP and nobody hurt, sit and recover
		// basic priest tactic (owner: "the priest should be sitting a lot to restore sp if not doing anything"):
		// whenever SP isn't full and nobody needs healing, sit (buffs below still go first when SP is high)
		// at >= 50% weight there is no natural regen: sitting is pointless, follow the leader instead (needTrip sells)
		if b.MaxSP > 0 && b.SP*100/b.MaxSP < b.priestSitLine(lead) && !b.buffDue(members, now) && !b.overRegenWeight() {
			hurt := false
			for _, s := range members {
				if s.Online && s.Map == b.MapName && s.HPFrac < healOthersBelow { // heal.go: the same line healFirst heals at
					hurt = true
				}
			}
			if !hurt && !b.underAttack(now) && !b.fightOn(now) && !b.threatNear(members, healerThreatCells, now) { // sitguard.go: never sit mid-fight; together.go: nor with monsters coming
				if !b.Sitting && !moving && !b.poisoned(now) && now.Sub(b.sitSentAt) > 3*time.Second && b.basicLv() >= 3 {
					b.sitSentAt = now
					go b.sit() // together.go restAnnounce says "need sp, resting" in party chat
				}
				b.setState("resting for SP")
				return true
			}
		}
		if b.activeBuffs(p, members, lead, now) { // priestbuff.go PT29: requests first, tank > melee > others
			return true
		}
		for _, buff := range []int{alBlessing, alIncAgi} {
			si := b.skills[buff]
			if !b.buffOK(si, now) || now.Before(b.skillReady) || b.Sitting { // heal.go: one buff per 10 s, never below 70% SP after it
				continue
			}
			for _, s := range members {
				if !s.Online || s.Map != b.MapName || dist(b.X, b.Y, s.X, s.Y) > 9 {
					continue
				}
				key := fmt.Sprintf("%d:%s", buff, s.Name)
				if now.Before(b.buffAt[key]) {
					continue
				}
				id := b.aid
				if s.Name != b.charName() {
					if id = b.entityID(s.Name); id == 0 {
						continue
					}
				}
				b.buffAt[key] = b.buffRenewAt(si.Lv, now) // heal.go: ~20 s before the buff really runs out
				b.buffAt["buffgap"] = now.Add(buffGap)
				b.skillReady = now.Add(1000 * time.Millisecond)
				b.setState("buffing " + s.Name)
				go b.useSkill(buff, si.Lv, id)
				return true
			}
		}
		// Kyrie Eleison on the tank first, then the leader (heal.go kyrieTargets; SP above the buff floor, buffOK)
		if si := b.skills[prKyrie]; b.buffOK(si, now) && now.After(b.skillReady) && !b.Sitting {
			for _, name := range b.kyrieTargets(p) {
				key := fmt.Sprintf("%d:%s", prKyrie, name)
				if now.Before(b.buffAt[key]) {
					continue
				}
				var id uint32
				for _, s := range members {
					if s.Name == name && s.Online && s.Map == b.MapName && dist(b.X, b.Y, s.X, s.Y) <= 9 {
						id = b.aid
						if name != b.charName() {
							id = b.entityID(name)
						}
					}
				}
				if id == 0 {
					continue
				}
				b.buffAt[key] = now.Add(kyrieRenew)
				b.buffAt["buffgap"] = now.Add(buffGap)
				b.skillReady = now.Add(1000 * time.Millisecond)
				b.setState("Kyrie Eleison on " + name)
				go b.useSkill(prKyrie, si.Lv, id)
				return true
			}
		}
		// High Priest Assumptio (transfight.go): halves the damage the tank takes, 20*lv s; same targets as Kyrie
		if si := b.skills[hpAssumptio]; b.buffOK(si, now) && now.After(b.skillReady) && !b.Sitting {
			for _, name := range b.kyrieTargets(p) {
				key := fmt.Sprintf("%d:%s", hpAssumptio, name)
				if now.Before(b.buffAt[key]) {
					continue
				}
				var id uint32
				for _, s := range members {
					if s.Name == name && s.Online && s.Map == b.MapName && dist(b.X, b.Y, s.X, s.Y) <= 9 {
						id = b.aid
						if name != b.charName() {
							id = b.entityID(name)
						}
					}
				}
				if id == 0 {
					continue
				}
				b.buffAt[key] = now.Add(time.Duration(20*si.Lv-3) * time.Second)
				b.buffAt["buffgap"] = now.Add(buffGap)
				b.skillReady = now.Add(2500 * time.Millisecond)
				b.setState("Assumptio on " + name)
				go b.useSkill(hpAssumptio, si.Lv, id)
				return true
			}
		}
		if idleSit { // nothing to heal or buff, the party isn't moving: sit (idle, not an announced rest)
			if !b.Sitting && !moving && now.Sub(b.sitSentAt) > 3*time.Second {
				b.sitSentAt, b.idleSitAt = now, now
				go b.sit()
			}
			b.target = 0
			b.setState("sitting by " + lead.Name)
			return true
		}
	}
	// rest together (together.go): hold position while a member rests - sit, or fight only what comes at the party
	if restHold && b.restGuard(members, rester, moving, now) {
		return true
	}
	// carrying a much lower-level human: let them fight their own monsters (otherwise we take every kill, and with a
	// gap over 15 levels the server doesn't even share the exp); only step in when they're hurt or being hit
	carry := false
	if human {
		if hl := humanLevel(p.Human); hl > 0 && b.BaseLvOr()-hl > 15 {
			carry = true
		}
	}
	if carry && !support {
		if e := b.defendTarget(members, now); e != nil && dist(b.X, b.Y, e.X, e.Y) <= 12 &&
			(lead.HPFrac < 0.5 || (e.Target == lead.AID && now.Sub(e.TargetAt) < 4*time.Second && lead.HPFrac < 0.8) ||
				now.Sub(e.LastHitMe) < 4*time.Second) {
			b.target = e.ID
			b.setState("protecting " + lead.Name)
			b.fight(e, moving, now)
			return true
		}
		lead.Target = 0 // don't join their fights
	}
	// the tank's job, in order (owner: "the tank in the party seems to do nothing"): 1) take aggro off anyone else
	// being hit - Provoke if known, else hit it; 2) pull the next monster near the group; 3) only then assist the
	// leader's own target. Measured before: it assisted/defended but monsters never hit it (HP stayed full).
	// the leader is leaving (a rally to a Kafra, or a bot leader travelling to another map): nobody opens a new fight
	// (QA 2026-11-14 12:10: Echobunny kept pulling Soldier Skeletons in pay_dun01, so Ayla's "protect the party" fights
	// never ended and her rally to Payon never set off)
	leaderLeaving := p.rallyInfo().Town != "" || !human && lead.Goal != "" && lead.Goal != lead.Map ||
		human && humanLeaving(p.Human, time.Now()) // her hunting map (status.json lock_map) isn't where she is: a trip
	if partyRole(b) == roleTank && !support && !carry {
		// tanklead.go: 0) a crowd on us -> bring it to the priest (AoE it there); 1) take aggro off any member (Provoke or hit), before our
		// own current target
		if b.tankFallBack(gm, members, moving, now) {
			return true
		}
		if b.protectSupport(gm, members, moving, now) { // together.go: a support calling / attacked / low HP comes first
			return true
		}
		if b.tankTakeAggro(members, moving, now) {
			return true
		}
		if b.tankGoHelp(gm, members, moving, now) { // together.go: a mate mobbed by 3+ further away - go to it
			return true
		}
		// 2) keep fighting the monster at hand (no waiting for the leader between kills)
		if e := b.ents[b.target]; b.target != 0 && e != nil && e.Type == objMob {
			if ex, ey := e.pos(now); dist(llx, lly, ex, ey) <= togetherCells()-4 {
				b.setState("tanking " + mobName(e.Class))
				b.fight(e, moving, now)
				return true
			}
		}
		// 3) open the next fight: the nearest monster around the leader or the group's front (tankPullOK). The
		// leader's own target counts too when nobody is on it yet - the tank takes the first hit, not the leader.
		maxLv := b.BaseLvOr() + tune().TankMaxLvOver
		var pick *Entity
		pd := 99
		for _, e := range b.ents {
			if e.Type != objMob || b.wantSit || leaderLeaving { // our own rest (hunt) goes on: the party holds for it (together.go); no pulls while the leader leaves
				continue
			}
			if mb := mobs[e.Class]; mb == nil || (mb.Boss || mb.Level > maxLv) && e.Class != b.bossHuntTarget() || mb.worthless() {
				continue
			}
			ex, ey := e.pos(now)
			if !tankPullOK(lead, ex, ey, tune().TankPullRadius) || gm != nil && gm.OnWarp(ex, ey) {
				continue
			}
			if dd := dist(b.X, b.Y, ex, ey); dd < pd {
				pick, pd = e, dd
			}
		}
		if pick != nil && (pick.ID == lead.Target || claims.try(b.MapName, pick.ID, b.aid, false)) {
			b.target, b.targetAt = pick.ID, time.Time{}
			b.setState("pulling " + mobName(pick.Class) + " for the party")
			b.fight(pick, moving, now)
			return true
		}
		// 4) nothing to pull: walk at the group's front instead of trailing behind (bot leaders on the move only;
		// never during a party rest - restGuard ran above)
		if fx, fy, ok := tankFront(lead); ok && !moving && !b.wantSit && !b.Sitting && dist(mx, my, fx, fy) > 3 && gm != nil &&
			gm.Walkable(fx, fy) && now.After(b.lastMoveTx.Add(time.Duration(600+b.P.ReactionMs)*time.Millisecond)) {
			b.target = 0
			b.setState("leading the way for " + lead.Name)
			go b.walkTo(fx, fy)
			return true
		}
	}
	// melee damage dealers protect the back line too (owner 2026-11-14: "tanks and melee need to protect range and
	// support"): a monster on a ranged / support member is taken off it before anything else (together.go meleePeel)
	if partyRole(b) == roleMelee && !support && !carry && (b.protectSupport(gm, members, moving, now) || b.meleePeel(members, moving, now)) {
		return true
	}
	// ranged / melee members join the tank's fight (it opens the fights; tanklead.go)
	if !support && partyRole(b) != roleTank && lead.Target == 0 {
		for _, m := range p.Members {
			if m == b || partyRole(m) != roleTank {
				continue
			}
			if s, ok := peek(m.P.Name); ok && s.Online && s.Map == b.MapName && s.Target != 0 {
				if e := b.ents[s.Target]; e != nil && e.Type == objMob {
					if ex, ey := e.pos(now); dist(llx, lly, ex, ey) <= togetherCells()-4 {
						b.target = e.ID
						b.setState("assisting " + s.Name)
						b.fight(e, moving, now)
						return true
					}
				}
			}
		}
	}
	// assist: attack whatever the leader is fighting
	if lead.Target != 0 && !support {
		if e, ok := b.ents[lead.Target]; ok && e.Type == objMob {
			b.target = e.ID
			b.setState("assisting " + lead.Name)
			b.fight(e, moving, now)
			return true
		}
	}
	// defend: hit whatever is attacking one of us (supports only when it's on them)
	if e := b.defendTarget(members, now); e != nil && !support && // owner 2026-11-14: party healers never fight
		dist(b.X, b.Y, e.X, e.Y) <= 12 {
		b.target = e.ID
		b.setState("defending vs " + mobName(e.Class))
		b.fight(e, moving, now)
		return true
	}
	// friends at a similar level don't just watch: they fight monsters around the group (and so level too)
	if human && !carry && !support && !leaderLeaving {
		if e := b.ents[b.target]; b.target != 0 && e != nil && e.Type == objMob && dist(lead.X, lead.Y, e.X, e.Y) <= partyLeash()+2 {
			b.setState("fighting " + mobName(e.Class) + " near " + lead.Name)
			b.fight(e, moving, now)
			return true
		}
		if dist(b.X, b.Y, lead.X, lead.Y) <= partyLeash() {
			maxLv := b.BaseLvOr() + 3
			var pick *Entity
			pd := 99
			for _, e := range b.ents {
				if e.Type != objMob || e.ID == lead.Target {
					continue
				}
				if mb := mobs[e.Class]; mb == nil || (mb.Boss || mb.Level > maxLv) && e.Class != b.bossHuntTarget() || mb.worthless() {
					continue
				}
				if dist(lead.X, lead.Y, e.X, e.Y) > partyLeash() {
					continue
				}
				if dd := dist(b.X, b.Y, e.X, e.Y); dd < pd {
					pick, pd = e, dd
				}
			}
			if pick != nil && claims.try(b.MapName, pick.ID, b.aid, false) {
				b.target, b.targetAt = pick.ID, time.Time{}
				b.setState("fighting " + mobName(pick.Class) + " near " + lead.Name)
				b.fight(pick, moving, now)
				return true
			}
		}
	}
	if !support && b.casterHelp(lead, members, leaderLeaving, moving, now) { // casteractive.go (CL16): SP to spare -> cast at a monster near the group
		return true
	}
	// party members pick up drops near the group too (they used to leave everything, so at level 30+ they still had
	// the starter Knife and no zeny for a weapon); the loot pays for their gear trips
	if b.MaxWeight == 0 || b.Weight*100/b.MaxWeight < 80 {
		var best *GroundItem
		bd := 8
		for _, it := range b.items {
			// batch 18: the solo loot filters apply to party members too (batch 16's spareGearFull sat only in ai.go,
			// and most bots are party members: lv30+ spare weapons median 3 -> 4, max 15 -> 27 after it went live)
			if b.spareGearFull(it.ItemID) || b.overRegenWeight() && heavyJunk(it.ItemID) {
				continue
			}
			// party loot is free for all (owner rule 2026-11-14; together.go partyLootShare sets Party Share pickup):
			// any drop of the party's kills near us and within the group's area, not only our own kills
			if d := dist(b.X, b.Y, it.X, it.Y); d < bd && now.Sub(it.Seen) < 60*time.Second && now.Sub(it.Seen) > 500*time.Millisecond &&
				dist(lead.X, lead.Y, it.X, it.Y) <= collectStopCells() {
				best, bd = it, d
			}
		}
		if best != nil {
			b.setState("looting")
			if bd <= 1 {
				id := best.ID
				delete(b.items, id)
				go b.pickup(id)
			} else if !moving {
				go b.walkTo(best.X, best.Y)
			}
			return true
		}
	}
	// with a human leader: stay close but out of the way, catch up when far, never wander off alone
	if human {
		d := dist(b.X, b.Y, lead.X, lead.Y)
		// no back-and-forth for followers either (owner rule; QA: Echobunny walked 133->123->114->123->132 "with Ayla"
		// mirroring every step of hers): within followSlack cells with nothing to fight, stand still; only when the
		// leader has moved further away walk once to the own slot next to it. (partyLeash 8 still limits fights, so
		// the slack stays below together_cells.)
		followSlack := partyLeash() + 4
		// catch up from 15 cells (was 19): "together" is 20 (together.go) and the leader's plugin waits beyond 20 and
		// only carries on within 12 - friends parked at 19-27 cells kept her holding (QA 2026-11-14 12:25, 13:02)
		catchUp := min(followSlack+3, togetherCells()-5)
		if hostileTransit(b.MapName, b.powerLv()) {
			catchUp = 8 // a dungeon / hostile map: stay right with her (QA 19:00Z Sphinx: spread out, three died)
		}
		if support && healerStickOn() { // healerstick.go: the healer stays within ~8 cells of the leader
			catchUp, followSlack = min(catchUp, healerFollowCells), min(followSlack, healerFollowCells-2)
		}
		if d > catchUp && !moving && gm != nil {
			b.setState("catching up with " + lead.Name)
			b.stepAlong(gm, lead.X, lead.Y)
			return true
		}
		// each member keeps its own spot around the leader (never on the leader's cell or on each other)
		sx, sy := b.followSlot(p, gm, lead.X, lead.Y)
		if dist(b.X, b.Y, sx, sy) > 1 && (d > followSlack || d == 0 || b.cellTaken(members, b.X, b.Y)) && !moving &&
			now.After(b.lastMoveTx.Add(time.Duration(400+b.P.ReactionMs)*time.Millisecond)) {
			b.setState("following " + lead.Name)
			go b.walkTo(sx, sy)
			return true
		}
		b.target = 0
		b.setState("with " + lead.Name)
		return true
	}
	// stay close to the leader; ranged and support hang back a little behind the tank
	keep := 4
	if r := partyRole(b); r == roleRanged || r == roleSupport {
		keep = 6
	}
	if d := dist(b.X, b.Y, lead.X, lead.Y); d > keep && !moving {
		b.setState("following " + p.Leader.charName())
		if d > 10 && gm != nil { // far behind: pathfind (a direct walk request that far is refused by the server)
			b.stepAlong(gm, lead.X, lead.Y)
			return true
		}
		x, y := lead.X+b.rng.Intn(5)-2, lead.Y+b.rng.Intn(5)-2
		if gm != nil && gm.Walkable(x, y) {
			go b.walkTo(x, y)
		} else {
			go b.walkTo(lead.X, lead.Y)
		}
		return true
	}
	if support {
		b.target = 0 // a party healer never falls through to hunt (it would melee)
		b.setState("with " + lead.Name)
		return true
	}
	return false
}

func (b *Bot) entityID(name string) uint32 {
	for _, e := range b.ents {
		if e.Type == objPC && e.Name == name {
			return e.ID
		}
	}
	return 0
}

// acceptInvite: answer a pending party invitation (accept from planned parties / sociable players).
func (b *Bot) acceptInvite() {
	if b.partyInvite == 0 {
		return
	}
	id := b.partyInvite
	b.partyInvite = 0
	p := partyOf(b)
	accept := p != nil || b.pin != nil || b.rng.Float64() < b.P.Sociability
	if p != nil && p.Human == "" && b.pin == nil && !strings.EqualFold(b.PartyName, p.Name) {
		accept = false // in a bot party: only its own party's invite (b.PartyName = the invite's party name)
	}
	flag := byte(0)
	if accept {
		flag = 1
		b.PartyID = id
	}
	go b.send(newPkt(0x02c7).u32(id).u8(flag).b)
}

// followSlot: this member's own spot near the leader - a fixed offset per member (so the group spreads out like
// people do), falling back to other walkable spots around the leader.
var followOffsets = [][2]int{{-2, 1}, {2, 1}, {0, -2}, {-2, -2}, {2, -2}, {0, 2}, {-3, 0}, {3, 0}}

func (b *Bot) followSlot(p *Party, gm *GameMap, lx, ly int) (int, int) {
	idx := 0
	for i, m := range p.Members {
		if m == b {
			idx = i
		}
	}
	for k := 0; k < len(followOffsets); k++ {
		o := followOffsets[(idx+k)%len(followOffsets)]
		x, y := lx+o[0], ly+o[1]
		if gm == nil || gm.Walkable(x, y) {
			return x, y
		}
	}
	return lx, ly
}

// cellTaken: someone else in the party (or the leader) is standing on this cell.
func (b *Bot) cellTaken(members []pubState, x, y int) bool {
	for _, m := range members {
		if m.Name != b.charName() && m.Map == b.MapName && m.X == x && m.Y == y {
			return true
		}
	}
	return false
}

// buffDue: a Blessing/Increase AGI on a member near the priest has run out and the priest can afford it.
func (b *Bot) buffDue(members []pubState, now time.Time) bool {
	if b.MaxSP == 0 {
		return false
	}
	if si := b.skills[prKyrie]; b.buffOK(si, now) {
		for _, name := range b.kyrieTargets(partyOf(b)) {
			for _, s := range members {
				if s.Name == name && s.Online && s.Map == b.MapName && dist(b.X, b.Y, s.X, s.Y) <= 9 && now.After(b.buffAt[fmt.Sprintf("%d:%s", prKyrie, name)]) {
					return true
				}
			}
		}
	}
	for _, buff := range []int{alBlessing, alIncAgi} {
		si := b.skills[buff]
		if !b.buffOK(si, now) { // heal.go: due AND affordable without dropping under 70% SP
			continue
		}
		for _, s := range members {
			if s.Online && s.Map == b.MapName && dist(b.X, b.Y, s.X, s.Y) <= 9 && now.After(b.buffAt[fmt.Sprintf("%d:%s", buff, s.Name)]) {
				return true
			}
		}
	}
	return false
}

// partyGoalSync (b.mu held): a bot party member's destination. Normally the party map (p.Map, set at formation); when
// the leader has stayed on another (non-town) map for 60 s - it hunts there now - the member's Goal.Map becomes that
// map. A leader still travelling (its map changing) never redirects members: they go to the party map and meet there.
// Once changed, the goal is kept for at least 60 s, so it can't flip right after crossing a portal.
func (b *Bot) partyGoalSync(now time.Time) {
	if b.Goal.Mode != "party" || b.Goal.Set == "human" {
		return
	}
	p := partyOf(b)
	if p == nil || p.Human != "" || p.Leader == nil || p.Leader == b {
		return
	}
	lead, ok := peek(p.Leader.P.Name)
	if !ok || !lead.Online || lead.Map == "" {
		return
	}
	if lead.Map != b.leadMap {
		b.leadPrevMap, b.leadMap, b.leadMapSince = b.leadMap, lead.Map, now
	}
	parties.Lock()
	want := p.Map // set at once when the leader relocates (moveTo), so members go straight to the new map
	checkpoint := p.Checkpoint
	parties.Unlock()
	// members don't go on alone (QA 2026-11-14 guild02: ThePuddinglord went to Payon to heal, then walked back from
	// prt_fild05, while his three members marched on to mjo_dun02 and the party drifted apart). Owner rule: parties
	// regroup at the Kafra nearest the destination and set out together. So while the leader is on a detour (a town
	// trip, a goal elsewhere) or hasn't reached the party's checkpoint town (or the party map) yet, members wait in
	// the checkpoint town - or stay with the leader on their own map, or stay on the party map if already there.
	// "the leader is there" only when it is hunting on that map - its own goal map (pubState.Goal) - for 60 s and it
	// isn't a town; a leader passing through (a Kafra stop, a transit field) never redirects members (QA: 16 members
	// ping-ponged between the old party map and the leader's intermediate maps while it relocated)
	if townOf(lead.Map) == "" && lead.Goal == lead.Map && now.Sub(b.leadMapSince) >= 60*time.Second {
		want = lead.Map
	}
	if want != p.Map {
		checkpoint = "" // the leader hunts elsewhere: that map's own checkpoint
	}
	if checkpoint == "" {
		checkpoint = nearestTown(want)
	}
	if b.leadCPFor != want {
		// a new party map (a mind's explicit call, a relocation): act on it at once, not after the 60 s goal hold
		b.leadCPFor, b.leadPassedCP, b.goalSyncAt = want, false, time.Time{}
	}
	off := leaderDetour(want, lead)
	// a party strong enough for the way (together.go partyStrong + a safe route): members go straight to the party map
	// / the leader, no checkpoint wait (owner 2026-11-14: higher-level members run straight to the leader)
	if !off && partyStrong(p) && b.routeSafeFrom(b.MapName, b.X, b.Y, want) {
		b.leadPassedCP = true
	}
	if lead.Errand {
		// the leader is on an errand of its own (job change, Alchemist guide, class weapon...): owner rule - a member may
		// leave briefly and the party keeps farming. Members go on to / stay on the party map; no checkpoint wait
		// (QA 11:46 guild05: Ryublade headed for Geffen town while TheShadowsenpai bought a guide in Al De Baran)
		off, b.leadPassedCP = false, true
	}
	if lead.Map == want || lead.Map == checkpoint {
		b.leadPassedCP = true
	} else if off || isTown(lead.Map) {
		b.leadPassedCP = false // detouring, or in another town: it comes back through the checkpoint
	}
	behind := off || !b.leadPassedCP
	if behind != b.leadOff {
		b.leadOff, b.goalSyncAt = behind, time.Time{} // react at once, not after the 60 s goal hold
	}
	if behind {
		// (not held by the 60 s goal hold: the hold point follows the leader map by map)
		follow := !off && b.leadPrevMap == b.MapName && now.Sub(b.leadMapSince) < 90*time.Second && !partyDestOn() // PT30: no trailing - the meeting town by our own route
		hold := b.stableHold(detourHold(want, checkpoint, lead.Map, b.MapName, off, follow), want, checkpoint, b.MapName, lead, now) // partyhold.go
		if hold != "" && hold != b.Goal.Map {
			b.Goal.Map, b.route, b.path, b.goalSyncAt = hold, nil, nil, now
			b.event("info", "", "party: "+lead.Name+" is off to "+lead.Map+" - waiting in "+hold+" (party map "+want+")")
			// say only what we do: wait in a town we are heading to (not when staying put or following the leader)
			if p.Leader != nil && hold != b.MapName && isTown(hold) && now.After(b.buffAt["detoursay"]) {
				b.buffAt["detoursay"] = now.Add(5 * time.Minute)
				typing := b.P.Typing
				go b.partySay(styled(typing, pick("ill wait for you in "+hold, "waiting in "+hold+" til you're back", "ok, meet you in "+hold)))
			}
		}
		return
	}
	if now.Sub(b.goalSyncAt) < 60*time.Second {
		return
	}
	if want != "" && want != b.Goal.Map {
		b.Goal.Map, b.route, b.path, b.goalSyncAt = want, nil, nil, now
		b.event("info", "", "party: heading to "+want+" (leader "+lead.Name+" is there)")
	}
}

// leaderMem: per bot-party LEADER (persona name) the maps its parties left lately and the maps that killed them.
// formLeaderParty (guild leaders) disbands and re-forms the party, so a per-party memory (Party.Left) was lost: QA
// 2026-11-14 09:00-12:10, guild07's lv52-60 party was re-formed ~6 times onto maps that kept killing it (gef_fild10,
// gef_fild03 Orc Lady/Warrior, moc_fild02/03 Peco/Wolf/Argos). Kept in memory only (a restart forgets it).
type leaderMapNote struct {
	left     time.Time // the party moved off this map
	deaths   int       // member deaths here within deathWin of the first one
	firstAt  time.Time
	badUntil time.Time // pickers skip the map for this leader until then
}

var leaderMem = struct {
	sync.Mutex
	m map[string]map[string]*leaderMapNote
}{m: map[string]map[string]*leaderMapNote{}}

const (
	leaderLeftMem   = 30 * time.Minute // not straight back to a map the leader's party left (A -> B -> A)
	leaderDeathWin  = 30 * time.Minute // 2 member deaths on one map within this window ...
	leaderDeathBan  = 60 * time.Minute // ... keep the leader's parties off it this long
	leaderFleeBan   = 30 * time.Minute // a member kept fleeing there
	leaderDeathsBad = 2
)

func leaderNote(lead, m string) *leaderMapNote { // leaderMem held
	mm := leaderMem.m[lead]
	if mm == nil {
		mm = map[string]*leaderMapNote{}
		leaderMem.m[lead] = mm
	}
	n := mm[m]
	if n == nil {
		n = &leaderMapNote{}
		mm[m] = n
	}
	return n
}

// leaderNoteLeft: the leader's party moved off map m.
func leaderNoteLeft(lead, m string) {
	if lead == "" || m == "" {
		return
	}
	leaderMem.Lock()
	leaderNote(lead, m).left = time.Now()
	leaderMem.Unlock()
}

// leaderNoteDeath: a member of the leader's party died on map m; the second death within 30 min bans it for an hour.
func leaderNoteDeath(lead, m string) {
	if lead == "" || m == "" || isTown(m) {
		return
	}
	now := time.Now()
	leaderMem.Lock()
	defer leaderMem.Unlock()
	n := leaderNote(lead, m)
	if now.Sub(n.firstAt) > leaderDeathWin {
		n.deaths, n.firstAt = 0, now
	}
	n.deaths++
	if n.deaths >= leaderDeathsBad {
		n.badUntil = now.Add(leaderDeathBan)
	}
}

// leaderNoteBad: keep the leader's parties off map m for d.
func leaderNoteBad(lead, m string, d time.Duration) {
	if lead == "" || m == "" {
		return
	}
	leaderMem.Lock()
	if n := leaderNote(lead, m); time.Now().Add(d).After(n.badUntil) {
		n.badUntil = time.Now().Add(d)
	}
	leaderMem.Unlock()
}

// leaderAvoids: map m is banned for this leader's parties (deaths / fleeing), or its party left it < 30 min ago.
func leaderAvoids(lead, m string, now time.Time) (bad, left bool) {
	leaderMem.Lock()
	defer leaderMem.Unlock()
	n := leaderMem.m[lead][m]
	if n == nil {
		return false, false
	}
	return now.Before(n.badUntil), now.Sub(n.left) < leaderLeftMem
}

// partyLeaderName: the persona name of b's bot-party leader ("" when b is in no bot party).
func partyLeaderName(b *Bot) string {
	parties.Lock()
	defer parties.Unlock()
	if p := parties.ofBot[b]; p != nil && p.Human == "" && p.Leader != nil {
		return p.Leader.P.Name
	}
	return ""
}
