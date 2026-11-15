package main

// Party rallies (owner, 2026-11-13: "parties should prefer regrouping at the closest Kafra to their intended
// destination. Save location with Kafra before setting out." - applies to the whole Legion).
//
// A party that is spread over different maps - a fresh party setting out, or members split for more than 45 s -
// meets at the Kafra of the town nearest its destination (bot party: p.Map; human-led party: the map the human
// hunts on), every member saves its respawn point there, and when everyone is within 12 cells of that Kafra (and
// saved, or 3 minutes after the last one arrived) the party sets out together. Same-map stragglers keep the
// leader-collects-one-by-one logic (ai.go). A rally that doesn't complete in 12 minutes is abandoned with a 5 minute
// cooldown, so it can never loop. Ayla (OpenKore, partylead.pl) joins in when she reads "meet at the Kafra in <town>"
// in party chat, and starts one herself after 90 s of a cross-map split (the bots join on her line).

import (
	"fmt"
	"hash/fnv"
	"log"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	rallySplitAfter   = 45 * time.Second // members on different maps this long -> rally
	rallySplitForce   = 6 * time.Minute  // ... even when the stragglers look like they're catching up / on a town trip
	rallyTimeout      = 5 * time.Minute  // a rally never lasts longer
	rallyCooldown     = 30 * time.Minute // no new rally for the same party within 30 min (after any rally)
	rallyDoneCooldown = 30 * time.Minute
	rallyWaitCells    = 4                // members stand this close to the Kafra while waiting
	rallyAfterArrived = 3 * time.Minute  // all here but someone couldn't save: go anyway after this
	humanHuntSettle   = 20 * time.Second // (was 3 min) a human's map counts as its hunting map after this long there
)

// rallyState lives on Party (guarded by parties.Mutex).
type rallyState struct {
	Town       string    `json:"town,omitempty"`
	X          int       `json:"x,omitempty"`
	Y          int       `json:"y,omitempty"`
	Save       bool      `json:"save,omitempty"` // the town has a Kafra: members save there
	Dest       string    `json:"dest,omitempty"`
	Why        string    `json:"why,omitempty"`
	At         time.Time `json:"started,omitempty"`
	AllAt      time.Time `json:"all_arrived,omitempty"`
	Next       time.Time `json:"next_allowed,omitempty"` // cooldown: no new rally before this
	SplitSince time.Time `json:"split_since,omitempty"`
	Last       string    `json:"last,omitempty"` // outcome of the previous rally
	LastAt     time.Time `json:"last_at,omitempty"`
	// human-led parties: the map the human hunts on (the destination), from where they stay for a while
	HumanHunt string `json:"human_hunt,omitempty"`
	hhCand    string
	hhCandAt  time.Time
	// progress watchdog: the best "distance left" score so far and when it last improved
	best   int
	bestAt time.Time
	// members that can't route to the rally town: left out, the rally ends without them
	excluded map[string]bool
	// where each member stood at the last tick: a member still walking (another map, a big field) is progress too
	lastPos map[string][3]interface{}
}

func (p *Party) rallyInfo() rallyState {
	parties.Lock()
	defer parties.Unlock()
	return p.Rally
}

// rallyZone: the map a member counts as being on (town buildings count as their town).
func rallyZone(m string) string {
	if t := townOf(m); t != "" {
		return t
	}
	return m
}

// rallySpot: the Kafra of the town on the side the party leaves toward dest (fallback: the town's spawn point).
func rallySpot(town, dest string) (x, y int, save, ok bool) {
	spots := kafraSpots[town]
	if len(spots) == 0 {
		if sp, ok := townSpawn[town]; ok {
			return sp[0], sp[1], false, true
		}
		return 0, 0, false, false
	}
	best, bd := spots[0], 1<<30
	for _, s := range spots {
		if !kafraReachable(town, s) {
			continue // only a Kafra reachable from the town centre without portals (QA: prontera <-> prt_in loops)
		}
		d := 999
		if r := RouteMaps(town, s.X, s.Y, dest); len(r) > 0 {
			d = dist(s.X, s.Y, r[0].X, r[0].Y) // distance to the town's exit warp toward the destination
		}
		if d < bd {
			best, bd = s, d
		}
	}
	if bd == 1<<30 { // no Kafra reachable on foot: meet at the spawn point, no save
		if sp, ok := townSpawn[town]; ok {
			return sp[0], sp[1], false, true
		}
		return 0, 0, false, false
	}
	return best.X, best.Y, true, true
}

var kafraReach sync.Map // "town,x,y" -> bool

// kafraReachable: a walkable cell next to the Kafra can be reached from the town's spawn point on foot, without
// stepping on any warp (FindPath avoids warp trigger areas).
func kafraReachable(town string, s kafraSpot) bool {
	key := fmt.Sprintf("%s,%d,%d", town, s.X, s.Y)
	if v, ok := kafraReach.Load(key); ok {
		return v.(bool)
	}
	ok := false
	gm, sp := maps[town], townSpawn[town]
	if gm != nil && sp != [2]int{} {
		for dy := -2; dy <= 2 && !ok; dy++ {
			for dx := -2; dx <= 2 && !ok; dx++ {
				if (dx != 0 || dy != 0) && gm.Walkable(s.X+dx, s.Y+dy) && gm.FindPath(sp[0], sp[1], s.X+dx, s.Y+dy, 400000) != nil {
					ok = true
				}
			}
		}
	}
	kafraReach.Store(key, ok)
	return ok
}

// rallyDest: where the party is heading.
func (p *Party) rallyDest() string {
	if p.Human == "" {
		return p.Map
	}
	parties.Lock()
	hh := p.Rally.HumanHunt
	parties.Unlock()
	if hh != "" {
		return hh
	}
	if h, ok := humanAt(p.Human); ok && h.Map != "" && rallyZone(h.Map) == h.Map && !isTown(h.Map) {
		return h.Map
	}
	return ""
}

// speaker: who talks for the party (the bot leader, or the first online member of a human-led party).
func (p *Party) speaker() *Bot {
	if p.Human == "" && p.Leader != nil {
		return p.Leader
	}
	for _, m := range p.Members {
		if s, ok := peek(m.P.Name); ok && s.Online {
			return m
		}
	}
	return nil
}

// rallyOn: rallies allowed for this party right now (tuning.json kill switches)
func (p *Party) rallyOn() bool {
	if p.Human != "" {
		return !tune().RallyHumanDisabled
	}
	t := tune()
	// rally_enabled true = every bot party; otherwise rally_bot_share % of them (staged rollout). rally_enabled stays
	// false in tuning.json so binaries without the share setting (it is read live) don't switch on for all parties
	if t.RallyEnabled {
		return true
	}
	if t.RallyBotShare <= 0 {
		return false
	}
	// staged rollout (rally_bot_share %, live-tunable): a stable subset of bot parties by party-name hash
	h := fnv.New32a()
	h.Write([]byte(p.Name))
	return int(h.Sum32()%100) < t.RallyBotShare
}

// startRally begins a rally toward dest; false if one is running, on cooldown or there's no town to meet in.
func (p *Party) startRally(dest, why string, now time.Time) bool {
	if dest == "" || isTown(dest) || !p.rallyOn() {
		return false
	}
	town := nearestTown(dest)
	x, y, save, ok := rallySpot(town, dest)
	if !ok {
		return false
	}
	parties.Lock()
	if p.Rally.Town != "" || now.Before(p.Rally.Next) {
		parties.Unlock()
		return false
	}
	p.Rally.Town, p.Rally.X, p.Rally.Y, p.Rally.Save, p.Rally.Dest, p.Rally.Why = town, x, y, save, dest, why
	p.Rally.At, p.Rally.AllAt, p.Rally.SplitSince = now, time.Time{}, time.Time{}
	p.Rally.best, p.Rally.bestAt, p.Rally.excluded, p.Rally.lastPos = 1<<30, now, map[string]bool{}, nil
	parties.Unlock()
	if sp := p.speaker(); sp != nil {
		sp.event("party", "", fmt.Sprintf("rally at the Kafra in %s (%d,%d) for %s: %s", town, x, y, dest, why))
		// fixed wording, no typing style: Ayla's plugin parses this line
		// natural, varied wording (QA: an all-caps "AT THE KAFRA IN PAYON, WAITING" read robotic); every variant keeps
		// "meet/regroup ... kafra in <town> (x,y)", which Ayla's plugin and rallyHeard parse
		go sp.partySay(pick(fmt.Sprintf("moving to %s, lets meet at the kafra in %s (%d,%d) first", mapTalk(dest), town, x, y),
			fmt.Sprintf("lets meet at the kafra in %s (%d,%d)", town, x, y),
			fmt.Sprintf("regroup at the kafra in %s, %d,%d", town, x, y), fmt.Sprintf("meet at the kafra in %s (%d,%d) guys", town, x, y)))
	}
	return true
}

// endRally finishes the running rally (ok = everyone met).
func (p *Party) endRally(ok bool, why string, now time.Time) {
	parties.Lock()
	if p.Rally.Town == "" {
		parties.Unlock()
		return
	}
	town := p.Rally.Town
	p.Rally.Town, p.Rally.AllAt, p.Rally.SplitSince = "", time.Time{}, time.Time{}
	p.Rally.Last, p.Rally.LastAt = why, now
	if ok {
		p.Rally.Next = now.Add(rallyDoneCooldown)
	} else {
		p.Rally.Next = now.Add(rallyCooldown)
	}
	parties.Unlock()
	if sp := p.speaker(); sp != nil {
		sp.event("party", "", "rally in "+town+" ended: "+why)
		if ok {
			go sp.partySay(pick("all here, let's go", "ok everyone's here, lets go", "all here, lets head out"))
		} else {
			go sp.partySay("cant all meet up in " + town + ", lets just go")
		}
	}
}

// rallyManager runs every party's rally logic (start on a cross-map split, end when everyone met or timed out).
func rallyManager(stop <-chan struct{}) {
	t := time.NewTicker(3 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
		}
		parties.Lock()
		ps := make([]*Party, 0, len(parties.byName))
		for _, p := range parties.byName {
			ps = append(ps, p)
		}
		parties.Unlock()
		now := time.Now()
		for _, p := range ps {
			p.rallyTick(now)
		}
	}
}

type rallyMember struct {
	name     string
	zone, m  string
	x, y     int
	saved    string
	waiting  bool
	human    bool
	posKnown bool
}

func (p *Party) rallyMembers(now time.Time) []rallyMember {
	var out []rallyMember
	ex := map[string]bool{}
	parties.Lock()
	for k := range p.Rally.excluded {
		ex[k] = true
	}
	parties.Unlock()
	for _, m := range p.Members {
		if s, ok := peek(m.P.Name); ok && s.Online && !s.Errand && s.Map != "" && !ex[s.Name] { // errand.go: not waited for
			out = append(out, rallyMember{name: s.Name, zone: rallyZone(s.Map), m: s.Map, x: s.X, y: s.Y,
				saved: s.SavedTown, waiting: s.Waiting, posKnown: true})
		}
	}
	if p.Human != "" {
		if h, ok := humanAt(p.Human); ok && h.Map != "" {
			online := now.Sub(h.At) < 5*time.Minute
			if !h.PartyAt.IsZero() {
				online = h.PartyOnline
			}
			if online {
				out = append(out, rallyMember{name: p.Human, zone: rallyZone(h.Map), m: h.Map, x: h.X, y: h.Y,
					human: true, posKnown: h.X+h.Y > 0})
			}
		}
	}
	return out
}

func (p *Party) rallyTick(now time.Time) {
	// human-led party: learn the human's hunting map (where they stay a few minutes outside town)
	if p.Human != "" {
		// her own hunting map from her status file (lockMap) wins at once (QA: after the mind moved her to pay_dun00
		// the destination kept pointing at pay_dun01 for ~3 min)
		if st, ok := humanStatusOf(p.Human); ok && st.LockMap != "" && !isTown(st.LockMap) {
			parties.Lock()
			p.Rally.HumanHunt, p.Rally.hhCand, p.Rally.hhCandAt = st.LockMap, st.LockMap, now
			parties.Unlock()
		} else if h, ok := humanAt(p.Human); ok && h.Map != "" && rallyZone(h.Map) == h.Map && !isTown(h.Map) {
			parties.Lock()
			if p.Rally.hhCand != h.Map {
				p.Rally.hhCand, p.Rally.hhCandAt = h.Map, now
			}
			if p.Rally.HumanHunt == "" || now.Sub(p.Rally.hhCandAt) > humanHuntSettle {
				p.Rally.HumanHunt = h.Map
			}
			parties.Unlock()
		}
	}
	if !p.rallyOn() {
		if p.rallyInfo().Town != "" {
			p.endRally(false, "rallies switched off", now)
		}
		return
	}
	if p.Human != "" && humanRallySync(p, now) { // rallysync.go: her rally state drives the members' rally
		return
	}
	ms := p.rallyMembers(now)
	r := p.rallyInfo()
	if r.Town != "" {
		if now.Sub(r.At) > rallyTimeout && p.Human == "" { // a human-led party's rally ends with her (rallysync.go)
			p.endRally(false, "abandoned after 5 min", now)
			return
		}
		// no progress for 2 minutes (nobody got closer: a warp loop, a blocked path) -> abandon (QA: bot leaders
		// looped prontera <-> prt_in "rally: going to the Kafra")
		score := 0
		for _, m := range ms {
			switch {
			case m.m == r.Town && m.posKnown:
				score += min(dist(m.x, m.y, r.X, r.Y), 200)
			case m.zone == r.Town:
				score += 250
			default:
				score += 500
			}
		}
		parties.Lock()
		if score < p.Rally.best-2 {
			p.Rally.best, p.Rally.bestAt = score, now
		}
		// the score only moves once a member reaches the town: a member crossing pay_gld (300 cells) for 2+ min was
		// "no progress" and 4 of 7 bot rallies were abandoned (QA 2026-11-14). Any member that moved 4+ cells or changed
		// map since the last tick counts as progress too.
		if p.Rally.lastPos == nil {
			p.Rally.lastPos = map[string][3]interface{}{}
		}
		for _, m := range ms {
			if old, ok := p.Rally.lastPos[m.name]; ok {
				if old[0] != m.m || dist(old[1].(int), old[2].(int), m.x, m.y) >= 4 {
					p.Rally.bestAt = now
				}
			}
			p.Rally.lastPos[m.name] = [3]interface{}{m.m, m.x, m.y}
		}
		stalled := now.Sub(p.Rally.bestAt) > 2*time.Minute
		parties.Unlock()
		all, saved := len(ms) > 0, true
		for _, m := range ms {
			if m.human {
				// the human counts as here in the town (a Kafra office / shop of the town counts), near the Kafra when
				// we know where they stand
				if m.zone != r.Town || (m.m == r.Town && m.posKnown && dist(m.x, m.y, r.X, r.Y) > togetherCells()) {
					all = false
				}
				continue
			}
			if m.m != r.Town || dist(m.x, m.y, r.X, r.Y) > togetherCells() {
				all = false
			}
			if r.Save && m.saved != r.Town {
				saved = false
			}
		}
		if !all {
			if stalled && p.Human == "" { // human-led: no own "no progress" clock (rallysync.go)
				p.endRally(false, "no progress for 2 min", now)
				return
			}
			if !r.AllAt.IsZero() {
				parties.Lock()
				p.Rally.AllAt = time.Time{}
				parties.Unlock()
			}
			return
		}
		if r.AllAt.IsZero() {
			parties.Lock()
			p.Rally.AllAt = now
			parties.Unlock()
			r.AllAt = now
		}
		if saved {
			p.endRally(true, "everyone met and saved", now)
		} else if now.Sub(r.AllAt) > rallyAfterArrived {
			p.endRally(true, "everyone met (not all could save)", now)
		}
		return
	}
	// no rally: watch for a cross-map split - bot parties only. A human-led party's members travel to the human by
	// themselves (humanPartySync); splitting rallies dragged Ayla's party out of the dungeon (owner: rally once when
	// setting out, ~90% of the time farming).
	if p.Human != "" {
		return
	}
	split, catching, waiting := false, true, false
	dest := p.rallyDest()
	ref := ""
	if p.Human != "" {
		for _, m := range ms {
			if m.human {
				ref = m.zone
			}
		}
	} else if p.Leader != nil {
		if s, ok := peek(p.Leader.P.Name); ok && s.Online {
			ref = rallyZone(s.Map)
		}
	}
	zones := map[string]bool{}
	for _, m := range ms {
		zones[m.zone] = true
		if m.waiting {
			waiting = true // deliberately waiting at the checkpoint town (the leader's map is too dangerous)
		}
		if ref != "" && m.zone != ref && !m.human {
			// a straggler a map or two from the destination, on its way: let it catch up (a respawned member walking
			// back shouldn't drag the whole party to town)
			// (a member in a town is on a town trip and comes back by itself)
			if townOf(m.m) == "" && (dest == "" || !m.posKnown || mapHops(m.m, m.x, m.y, dest) > 2) {
				catching = false
			}
		}
	}
	split = len(zones) > 1
	if at, _, _ := p.townRun(); now.Sub(at) < 3*time.Minute {
		split = false // a party town run: members are in shops all over town
	}
	if !split || waiting || dest == "" || now.Before(r.Next) {
		if !r.SplitSince.IsZero() {
			parties.Lock()
			p.Rally.SplitSince = time.Time{}
			parties.Unlock()
		}
		return
	}
	if r.SplitSince.IsZero() {
		parties.Lock()
		p.Rally.SplitSince = now
		parties.Unlock()
		return
	}
	if el := now.Sub(r.SplitSince); el > rallySplitForce || (el > rallySplitAfter && !catching) {
		p.startRally(dest, "party split across maps", now)
	}
}

// rallyOnForm: a party that sets out with members on other maps meets at the destination's Kafra first. So does a
// party formed on its hunting map when a member's respawn point is clearly farther from the map than the nearest
// Kafra town (owner rule: save at the Kafra nearest the grinding ground; a party formed in the field never rallied,
// so nobody saved - a death sent the member to a far town while the party waited).
func (p *Party) rallyOnForm() {
	town := nearestTown(p.Map)
	for _, m := range p.Members {
		s, ok := peek(m.P.Name)
		if !ok || !s.Online {
			continue
		}
		if rallyZone(s.Map) != p.Map {
			p.startRally(p.Map, "setting out", time.Now())
			return
		}
		if p.Map != "" && !isTown(p.Map) && s.SavedTown != "" && s.SavedTown != town && len(kafraSpots[town]) > 0 &&
			townHops(s.SavedTown, p.Map) > townHops(town, p.Map)+1 {
			p.startRally(p.Map, "saving at the Kafra nearest "+p.Map, time.Now())
			return
		}
	}
}

var townRunLineRe = regexp.MustCompile(`(?i)\btown run\b(?:.*?\b(?:to|in) (?:the )?([a-z]+))?`)

var rallyLineRe = regexp.MustCompile(`(?i)(?:meet|regroup|gather)\b[^.!?]*?kafra in ([a-z]+)(?:\D+(\d+)\D+(\d+))?`)

// rallyHeard: the human leader called a rally in party chat ("meet at the Kafra in payon"): the bots join it.
func rallyHeard(p *Party, text string) {
	if p == nil || p.Human == "" {
		return
	}
	i := strings.Index(text, " : ")
	if i < 0 || !strings.EqualFold(strings.TrimSpace(text[:i]), p.Human) {
		return
	}
	// the human announces a town run ("town run, bag full, follow me to payon"): a party town run for everyone, so
	// the members start their trips with her instead of staying in the dungeon (owner: town runs are done together)
	// "next town run we go prontera" / "next time" is a plan, not a call (QA 2026-11-14 14:39: Ayla's reply to the priest's
	// question started a party run at 22-28% weight and split the party)
	if low := strings.ToLower(text[i+3:]); strings.Contains(low, "next town run") || strings.Contains(low, "next run") || strings.Contains(low, "next time") {
		return
	}
	noteSaveAsk(p, text[i+3:], time.Now()) // savepoints.go: "save here" -> every member in her town saves at her Kafra
	if tm := townRunLineRe.FindStringSubmatch(text[i+3:]); tm != nil {
		town := strings.ToLower(tm[1])
		if _, ok := townSpawn[town]; !ok {
			parties.Lock()
			town = nearestTown(p.Rally.HumanHunt)
			parties.Unlock()
		}
		if town != "" && p.callTownRun(town, p.Human, time.Now()) {
			log.Printf("party %s: %s called a town run to %s", p.Name, p.Human, town)
		}
		return
	}
	if !p.rallyOn() {
		return
	}
	mm := rallyLineRe.FindStringSubmatch(text[i+3:])
	if mm == nil {
		return
	}
	town := strings.ToLower(mm[1])
	if _, ok := townSpawn[town]; !ok {
		return
	}
	now := time.Now()
	parties.Lock()
	// her newest meeting point replaces an older rally in another town (live 00:36: Echobunny / vex_x18 waited at the
	// Payon Kafra for her earlier rally while she called "meet at the kafra in geffen"); the same town keeps it
	if p.Rally.Town != "" && p.Rally.Town != town {
		old := p.Rally.Town
		p.Rally.Town, p.Rally.AllAt, p.Rally.SplitSince = "", time.Time{}, time.Time{}
		p.Rally.Last, p.Rally.LastAt, p.Rally.Next = "replaced by "+p.Human+"'s meeting in "+town, now, time.Time{}
		log.Printf("party %s: rally in %s replaced by %s's new meeting point %s", p.Name, old, p.Human, town)
	}
	if p.Rally.Town != "" || now.Before(p.Rally.Next) && !strings.EqualFold(p.Human, strings.TrimSpace(text[:i])) {
		parties.Unlock()
		return
	}
	dest := p.Rally.HumanHunt
	if dest == "" || nearestTown(dest) != town {
		dest = town
	}
	x, y, save, ok := rallySpot(town, dest)
	if mm[2] != "" {
		x, _ = strconv.Atoi(mm[2])
		y, _ = strconv.Atoi(mm[3])
	}
	if !ok {
		parties.Unlock()
		return
	}
	p.Rally.Town, p.Rally.X, p.Rally.Y, p.Rally.Save, p.Rally.Dest, p.Rally.Why = town, x, y, save, dest, p.Human+" called it"
	p.Rally.At, p.Rally.AllAt, p.Rally.SplitSince = now, time.Time{}, time.Time{}
	p.Rally.best, p.Rally.bestAt, p.Rally.excluded, p.Rally.lastPos = 1<<30, now, map[string]bool{}, nil
	parties.Unlock()
}

// rallyThink: a member's part in its party's rally (b.mu held); true while the rally owns the bot.
func (b *Bot) rallyThink(gm *GameMap, moving bool, now time.Time) bool {
	p := partyOf(b)
	var r rallyState
	if p != nil {
		r = p.rallyInfo()
	}
	if r.Town == "" {
		if b.rallySat { // rally over: stand up and set out
			b.rallySat = false
			b.wantSit = false
			if b.Sitting {
				b.sitSentAt = now
				go b.stand()
			}
		}
		return false
	}
	parties.Lock()
	out := p.Rally.excluded[b.charName()]
	parties.Unlock()
	if out {
		return false
	}
	// rallyleave.go: the human leader left the rally town - the rally ends, members follow her (the healer first)
	if p.Human != "" {
		if h, ok := b.humanState(p.Human, now); ok && humanLeftRally(r.Town, h.Map, r.At, now) {
			p.endRally(false, p.Human+" left "+r.Town, now)
			return false
		}
	}
	if b.rallyID != r.At {
		b.rallyID, b.rallySaid, b.rallyCell = r.At, false, [2]int{}
	}
	// fight back if something jumps us on the way / while waiting
	if b.underAttack(now) {
		if b.Sitting {
			b.sitSentAt = now
			go b.stand()
		}
		b.hunt(gm, moving, now)
		if b.target != 0 {
			return true
		}
	}
	b.target = 0
	if b.MapName != r.Town {
		if done, own := b.rallyLeg(p, r, now); done { // rallypark.go: a leg we can't travel, or the rally over its cap for us
			return own
		}
		if b.Sitting && now.Sub(b.sitSentAt) > time.Second && !b.waitSitHeld(now) {
			b.sitSentAt = now
			go b.stand()
		}
		if townOf(b.MapName) != r.Town && b.rallyRouteOK != r.At {
			// a member that can't route to the rally town is left out; the rally ends without it
			if RouteMaps(b.MapName, b.X, b.Y, r.Town, b.BaseLvOr()) == nil {
				parties.Lock()
				if p.Rally.excluded != nil {
					p.Rally.excluded[b.charName()] = true
				}
				parties.Unlock()
				b.event("info", "", "rally: no route from "+b.MapName+" to "+r.Town+", left out")
				return false
			}
			b.rallyRouteOK = r.At
		}
		saved := b.Goal
		b.Goal = Goal{Mode: "town", Map: r.Town, Set: saved.Set}
		b.travel(gm, moving)
		b.Goal = saved
		b.setState("rally: going to the Kafra in " + r.Town)
		return true
	}
	// in the rally town: save at its Kafra first (owner: "Save location with Kafra before setting out")
	if saved, known := b.knownSave(); r.Save && (!known || saved != r.Town) && b.kafra == nil && now.After(b.kafraNext) { // savepoints.go: a guessed save point is no save
		b.kafra = &kafraTrip{Save: true, Spot: kafraSpot{r.X, r.Y}, Town: r.Town, Stage: "walk", Started: now, At: now}
	}
	if b.kafraThink(gm, moving, now) {
		return true
	}
	if b.rallyCell == ([2]int{}) && gm != nil { // a free walkable cell near the Kafra (members spread a little)
		b.rallyCell = [2]int{r.X, r.Y}
		for try := 0; try < 40; try++ {
			x, y := r.X+b.rng.Intn(2*rallyWaitCells+1)-rallyWaitCells, r.Y+b.rng.Intn(2*rallyWaitCells+1)-rallyWaitCells
			if (x != r.X || y != r.Y) && gm.Walkable(x, y) {
				b.rallyCell = [2]int{x, y}
				break
			}
		}
	}
	if dist(b.X, b.Y, r.X, r.Y) > rallyWaitCells+1 {
		if b.Sitting && now.Sub(b.sitSentAt) > time.Second {
			b.sitSentAt = now
			go b.stand()
		}
		if !moving && gm != nil {
			b.stepAlong(gm, b.rallyCell[0], b.rallyCell[1])
		}
		b.setState("rally: walking to the Kafra in " + r.Town)
		return true
	}
	if !b.rallySaid {
		b.rallySaid = true
		typing := b.P.Typing
		_ = typing // plain lowercase: a styled ALL-CAPS protocol line read robotic
		go b.partySay(pick("at the kafra, waiting for u guys", "im at the kafra", "here at the kafra, waiting", "at the kafra now"))
	}
	if !b.Sitting && !moving && !b.poisoned(now) && now.Sub(b.sitSentAt) > 3*time.Second && b.basicLv() >= 3 {
		b.sitSentAt = now
		go b.sit()
	}
	b.rallySat = true
	b.setState("rally: waiting at the Kafra in " + r.Town)
	return true
}

// rallyView: the rally state for /api/human-party and the party dashboard.
func rallyView(p *Party, now time.Time) map[string]interface{} {
	r := p.rallyInfo()
	out := map[string]interface{}{"active": r.Town != "", "dest": p.rallyDest(), "human_hunt": r.HumanHunt}
	if r.Town != "" {
		out["town"], out["kafra"], out["save"], out["why"] = r.Town, [2]int{r.X, r.Y}, r.Save, r.Why
		out["for_s"] = int(now.Sub(r.At).Seconds())
		if !r.AllAt.IsZero() {
			out["all_arrived_s"] = int(now.Sub(r.AllAt).Seconds())
		}
	}
	if !r.SplitSince.IsZero() {
		out["split_s"] = int(now.Sub(r.SplitSince).Seconds())
	}
	if r.Last != "" {
		out["last"], out["last_ago_s"] = r.Last, int(now.Sub(r.LastAt).Seconds())
	}
	if now.Before(r.Next) {
		out["cooldown_s"] = int(r.Next.Sub(now).Seconds())
	}
	return out
}

// stuckWatch: a bot that means to go somewhere (travel, regroup, rally, Kafra, town trip) but stands on the same cell
// for 30 s is stuck (seen live: Zendancer84 and Zen.fox at payon 159,94 "travel -> pay_dun00" for minutes; Hana_99 at
// prontera 47,226 "town trip: go" for 10+ min without sending a single walk). rAthena ignores walking while an NPC
// dialog, a trade, a chat room, a shop window or a sit is still open server-side, safeWalkDest (nav.go) can refuse
// every step next to a warp area, and a stale route/path keeps re-sending the same refused step.
// QA 2026-11-14 09:00-09:11 (94 events, 14 of them >= 120 s, all "try 1"): the old watch needed a walk request in the
// last 5 s, so a bot whose walks were refused (or never sent) was never caught, and its clock ran from the moment it
// reached the cell, so a bot resting 4 minutes at its respawn point (the Kafra save cell: payon 160,58, geffen
// 200,124) after a death was "stuck for 243s" the first second it set off again. Now the clock only runs while the
// state is a "going" state, and the tries count per spot (within 4 cells, 5 minutes) and escalate:
//
//	try 1: close dialogs / trade / chat / shop, stand, drop the route and path (re-plan), step to a free cell 2-5 away
//	try 2: the same, drop the Kafra plan and the via-town detour, and walk 6-12 cells in a random direction
//	try 3+: Fly Wing (else Butterfly Wing), and give up the current town trip / Kafra trip for 5 minutes
//
// b.mu held.
type stuckSpot struct {
	m     string
	x, y  int
	tries int
	at    time.Time
}

var stuckSpots sync.Map // *Bot -> *stuckSpot

// walking states: standing 30 s on one cell in one of these is stuck by itself (Hana_99 sent no walk at all)
var stuckWalking = []string{"travel ->", "town trip: go", "town trip: wing", "town trip: wrong room", "going to the Kafra",
	"saving at the Kafra", "renting a pushcart", "rally: going", "rally: walking", "no route to"}

// states that also include standing still on purpose (a follower in its slot, a leader waiting for a straggler):
// stuck only while the bot keeps sending (or having refused) walk requests
var stuckTrying = []string{"regrouping", "catching up", "collecting", "going to "}

func (b *Bot) stuckWatch(gm *GameMap, moving bool, now time.Time) {
	going := false
	for _, pre := range stuckWalking {
		if strings.HasPrefix(b.State, pre) {
			going = true
			break
		}
	}
	if b.kafra != nil && b.kafra.Stage != "walk" {
		going = false // talking to the Kafra (kafraThink times that out itself)
	}
	if !going && (now.Sub(b.lastMoveTx) < 5*time.Second || now.Sub(b.buffAt["walkrefused"]) < 5*time.Second) {
		for _, pre := range stuckTrying {
			if strings.HasPrefix(b.State, pre) && !strings.Contains(b.State, ": waiting") {
				going = true
				break
			}
		}
	}
	pos := [3]interface{}{b.MapName, b.X, b.Y}
	// the clock runs only while the bot means to walk somewhere and stands on this cell: resting after a respawn,
	// fighting, vending or trading don't count (a follower standing in its slot isn't in a going state either)
	if pos != b.stuckPos || !going || gm == nil || b.vend != nil || b.saleRoom != "" || b.target != 0 || b.mkt != nil || b.Dead {
		b.stuckPos, b.stuckSince = pos, now
		return
	}
	if now.Sub(b.stuckSince) < 30*time.Second || now.Sub(b.unstuckAt) < 20*time.Second {
		return
	}
	v, _ := stuckSpots.LoadOrStore(b, &stuckSpot{})
	sp := v.(*stuckSpot)
	if sp.m != b.MapName || dist(sp.x, sp.y, b.X, b.Y) > 4 || now.Sub(sp.at) > 5*time.Minute {
		*sp = stuckSpot{m: b.MapName, x: b.X, y: b.Y}
	}
	sp.tries++
	sp.at = now
	b.unstuckAt = now
	b.stuckTries = sp.tries
	npc := b.dialogNPC
	refused := ""
	if now.Sub(b.buffAt["walkrefused"]) < 30*time.Second {
		refused = ", walks refused"
	}
	b.event("info", "", fmt.Sprintf("stuck at %s %d,%d for %ds (try %d, state %q, sitting %v, npc %d%s): unsticking",
		b.MapName, b.X, b.Y, int(now.Sub(b.stuckSince).Seconds()), sp.tries, b.State, b.Sitting, npc, refused))
	if b.tracing() {
		trip := "-"
		if t := b.trip; t != nil {
			trip = fmt.Sprintf("%s/%s", t.Stage, t.Why)
			if t.Shop != nil {
				trip += fmt.Sprintf(" shop %s %d,%d", t.Shop.Map, t.Shop.X, t.Shop.Y)
			}
		}
		b.tracef("STUCK try %d at %d,%d state=%q moving=%v moveUntil=%v route=%d path=%d kafra=%v trip=%s lastWarp=%v lastMoveTx=%v",
			sp.tries, b.X, b.Y, b.State, moving, b.MoveUntil.Sub(now), len(b.route), len(b.path), b.kafra != nil, trip, b.lastWarp,
			now.Sub(b.lastMoveTx))
	}
	b.route, b.path = nil, nil
	travelProgs.Delete(b) // nav.go travelStalled: start its clock again from the new cell
	b.wantSit = false
	b.dialogNPC = 0
	if b.kafra != nil && (b.kafra.Stage == "walk" || sp.tries >= 2) {
		b.kafra, b.kafraNext = nil, now.Add(5*time.Minute)
	}
	if sp.tries >= 2 {
		b.viaTown = ""
	}
	if sp.tries >= 3 {
		// a teleport out, and the trip that walked us here waits 5 minutes
		if b.trip != nil {
			b.event("info", "", fmt.Sprintf("giving up the town trip to %s for 5 min: stuck at %s %d,%d", b.trip.Town, b.MapName, b.X, b.Y))
			b.trip, b.tripAgain = nil, now.Add(5*time.Minute)
		}
		b.kafra, b.kafraNext = nil, now.Add(5*time.Minute)
		// QA 2026-11-14 10:17 UTC: a Butterfly Wing here is a town trip; a Fly Wing (same map) unsticks just as well. The
		// Butterfly Wing only when re-plans and steps failed two more times on this spot (try 5: truly stuck)
		// or the cell is walled in (no step works).
		switch {
		case b.escItemOK(itemFlyWing, now):
			b.useEscapeItem(itemFlyWing, now, fmt.Sprintf("stuck try %d", sp.tries)) // escapeitem.go: closes dialog/trade/chat/shop first, checks it worked
			return
		case (sp.tries >= 5 || gm.enclosed(b.X, b.Y)) && b.escItemOK(itemButterflyWing, now):
			b.useEscapeItem(itemButterflyWing, now, fmt.Sprintf("stuck try %d, no Fly Wing", sp.tries))
			return
		}
		if b.Goal.Set != "api" && partyOf(b) == nil && b.Goal.Map != b.MapName {
			b.event("info", "", fmt.Sprintf("no wing to leave %s %d,%d: dropping the goal %s", b.MapName, b.X, b.Y, b.Goal.Map))
			b.Goal = Goal{} // pick another goal (the old one keeps leading through this cell)
		}
	}
	// a free cell to step to: walkable, same walk region, not a warp cell, nobody standing on it
	lo, hi := 2, 5
	if sp.tries >= 2 {
		lo, hi = 6, 12
	}
	taken := map[[2]int]bool{}
	for _, e := range b.ents {
		if e.Type != objItem {
			taken[[2]int{e.X, e.Y}] = true
		}
	}
	reg := gm.Region(b.X, b.Y)
	x0, y0 := b.X, b.Y
	tx, ty := x0, y0
	for try := 0; try < 60; try++ {
		r := lo + b.rng.Intn(hi-lo+1)
		x, y := b.X+b.rng.Intn(2*r+1)-r, b.Y+b.rng.Intn(2*r+1)-r
		if dist(x, y, b.X, b.Y) < lo || !gm.Walkable(x, y) || gm.OnWarp(x, y) || taken[[2]int{x, y}] ||
			(reg != 0 && gm.Region(x, y) != reg) {
			continue
		}
		tx, ty = x, y
		break
	}
	sitting, trade, tries := b.Sitting, !b.tradeOpen.IsZero() || b.tradeFrom != "", sp.tries
	go func() {
		if npc != 0 { // whatever state the dialog is in: next, cancel the menu, close
			b.send(newPkt(0x00b9).u32(npc).b)
			b.send(newPkt(0x00b8).u32(npc).u8(255).b)
			b.send(newPkt(0x0146).u32(npc).b)
		}
		if trade || tries >= 2 {
			b.send(newPkt(0x00ed).b) // cancel a trade
		}
		b.send(newPkt(0x00e3).b) // leave a chat room
		b.send(newPkt(0x09d4).b) // close an NPC shop window (sd->npc_shopid blocks walking, town.go onDeal)
		if sitting || tries >= 2 {
			b.stand()
		}
		time.Sleep(600 * time.Millisecond)
		if tx != x0 || ty != y0 {
			b.walkTo(tx, ty)
		}
	}()
}
