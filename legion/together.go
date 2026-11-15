package main

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// One shared "together" rule for party regroup / collect decisions (owner rule 2026-11-14, RULES.md 3: "together"
// means within 20 cells - nobody holds, collects or regroups for a member within 20 steps of the leader).
//
// Every path that makes a member regroup or wait "coming to get me" (party.go) and every bot leader's collect
// decision (ai.go) goes through these helpers, so the two sides can never disagree. Positions are the bots' own
// last-known ones (b.X/b.Y and the pubState each bot publishes after thinking), never the API snapshot.
//
// Hysteresis: a collect / regroup STARTS only beyond together_cells and, once started, ENDS within
// collect_stop_cells (16): stopping at 20 too made leaders flip "collecting"/"roaming" at the boundary.

// collectDangerHP: a straggler fighting below this HP fraction is in danger - the leader goes to it even though it
// is busy fighting.
const collectDangerHP = 0.5

// partyTogether: two members count as together (same map, both positions known, within together_cells).
func partyTogether(aMap string, ax, ay int, bMap string, bx, by int) bool {
	if aMap == "" || aMap != bMap || ax == 0 && ay == 0 || bx == 0 && by == 0 {
		return false
	}
	return dist(ax, ay, bx, by) <= togetherCells()
}

// regroupNeeded: the hysteresis shared by the member's regroup walk and the leader's collect. active = a regroup /
// collect is already in progress.
func regroupNeeded(d int, active bool) bool {
	if active {
		return d > collectStopCells()
	}
	return d > togetherCells()
}

// needsCollect: should the bot leader on leadMap at (lx, ly) (its livePos) collect member s? committed = it already walks to s.
// Only a member on the same map, beyond together_cells (or still beyond collect_stop_cells for a collect in
// progress), and not busy fighting - unless that fight is going badly (in danger). Members on another map are
// brought over by the travel logic (partyGoalSync), not by a collect walk.
func needsCollect(leadMap string, lx, ly int, s pubState, committed bool) bool {
	sx, sy := livePosOf(s)
	if !s.Online || s.Errand || s.Map == "" || s.Map != leadMap || sx == 0 && sy == 0 || lx == 0 && ly == 0 {
		return false
	}
	if !regroupNeeded(dist(lx, ly, sx, sy), committed) {
		return false
	}
	if s.Target != 0 && s.HPFrac >= collectDangerHP {
		return false // busy fighting and doing fine: it walks back after the fight (party.go), nobody comes for it
	}
	return true
}

// livePos: where the bot really stands now. While a walk is in progress b.X/b.Y is still the walk's START (it only
// jumps to DestX/DestY when the walk ends, ai.go think), up to a whole ~11-cell segment behind on each side: a leader
// and member walking to each other looked 21+ cells apart while really 5-13 apart, so leaders started fresh collects
// ("X wait there, coming") of members right next to them (QA 2026-11-14 13:00Z, 21 cases). Interpolated by the walk
// speed, like Entity.pos. Caller holds b.mu.
func (b *Bot) livePos(now time.Time) (int, int) {
	return walkPos(b.X, b.Y, b.DestX, b.DestY, b.MoveUntil, b.Speed, now)
}

func walkPos(x0, y0, x1, y1 int, until time.Time, speedMs int, now time.Time) (int, int) {
	if !now.Before(until) || x0 == x1 && y0 == y1 || speedMs <= 0 {
		if now.Before(until) {
			return x0, y0
		}
		return x1, y1
	}
	steps := max(abs(x1-x0), abs(y1-y0))
	left := int((until.Sub(now) + time.Duration(speedMs)*time.Millisecond - 1) / (time.Duration(speedMs) * time.Millisecond))
	done := max(steps-left, 0)
	return x0 + (x1-x0)*done/steps, y0 + (y1-y0)*done/steps
}

// livePosOf: a published member's live position (pubState.LX/LY, falling back to X/Y from an old snapshot).
func livePosOf(s pubState) (int, int) {
	if s.LX != 0 || s.LY != 0 {
		return s.LX, s.LY
	}
	return s.X, s.Y
}

// ---- Rest together; no fidgeting (owner rule 2026-11-14, RULES.md 3) ----
// When a party member rests for HP/SP, everyone within restHoldCells holds position: they sit if they need regen too,
// otherwise stand guard and fight only monsters that hit the party or come near the resting members. Nobody (leader
// included) walks around, collects, regroups, roams or relocates during the rest - unless a member is in danger
// (fighting below collectDangerHP, farther than a guard can reach) or the bot is really stuck (stuckWatch side-steps).

const restHoldCells = 25

// restingNow: this bot is resting for regen (caller holds b.mu). Bots sitting "with the party" during someone
// else's rest are NOT counted, so a rest never keeps itself going.
func (b *Bot) restingNow() bool {
	return b.wantSit || strings.HasPrefix(b.State, "resting")
}

// partyRester: another member (not me) resting within restHoldCells of (x, y) on mp (live positions). The bot's
// own rest is its own logic's business (hunt wantSit / the priest's SP sit); this is about waiting for the others.
func partyRester(members []pubState, me, mp string, x, y int) (pubState, bool) {
	for _, s := range members {
		if s.Name == me || !s.Online || s.Errand || !s.Resting || s.Map != mp {
			continue
		}
		if sx, sy := livePosOf(s); dist(x, y, sx, sy) <= restHoldCells {
			return s, true
		}
	}
	return pubState{}, false
}

// restFarmCells: while a non-healer leader takes a breather, fighters keep farming within this many cells of it.
const restFarmCells = 15

// restFarmFor: b (a fighter, not a healer) keeps farming during r's rest because r is the party's leader (the human
// leader of a human-led party, else the bot leader) and not a healer - a leader's short SP/HP sit is not a party stop.
func restFarmFor(b *Bot, r pubState) bool {
	if r.Support || b.isHealer() {
		return false
	}
	p := partyOf(b)
	if p == nil {
		return false
	}
	if p.Human != "" {
		return strings.EqualFold(p.Human, r.Name)
	}
	return p.Leader != nil && p.Leader != b && p.Leader.charName() == r.Name
}

// memberInDanger: fighting and losing - worth breaking a rest for.
func memberInDanger(s pubState) bool {
	return s.Online && s.Target != 0 && s.HPFrac < collectDangerHP
}

// restGuard: hold position during a party rest (caller holds b.mu). Returns false only when the rest must be broken
// (a member in danger out of reach): the normal logic then runs (collect, regroup, defend).
func (b *Bot) restGuard(members []pubState, r pubState, moving bool, now time.Time) bool {
	mx, my := b.livePos(now)
	for _, s := range members {
		if s.Name != b.charName() && s.Map == b.MapName && memberInDanger(s) {
			if sx, sy := livePosOf(s); dist(mx, my, sx, sy) > 12 {
				return false
			}
		}
	}
	engage := func(e *Entity, why string) bool {
		if b.Sitting && now.Sub(b.sitSentAt) > time.Second {
			b.sitSentAt, b.wantSit = now, false
			go b.stand()
		}
		b.target = e.ID
		b.setState(why + " " + mobName(e.Class))
		b.fight(e, moving, now)
		return true
	}
	healer := b.isHealer() // a healer heals (handled before this) and doesn't trade blows for the party
	// 1. whatever is hitting us or a member
	if e := b.defendTarget(members, now); e != nil && dist(mx, my, e.X, e.Y) <= 12 && (!healer || now.Sub(e.LastHitMe) < 4*time.Second) {
		return engage(e, "guarding the resting party from")
	}
	// 1b. a non-healer party LEADER taking a breather (Ayla's short SP sit, owner 2026-11-14: the party must not sit
	// around): the rest is hers, not the party's - fighters keep killing anything within restFarmCells of her
	// (nearest first, no bosses), so nobody walks off and the kills don't stop. Healers keep the old hold.
	if restFarmFor(b, r) {
		rx, ry := livePosOf(r)
		var best *Entity
		bd := 1 << 30
		for _, e := range b.ents {
			if e.Type != objMob {
				continue
			}
			if mb := mobs[e.Class]; mb != nil && mb.Boss {
				continue
			}
			ex, ey := e.pos(now)
			if dist(rx, ry, ex, ey) > restFarmCells {
				continue
			}
			if d := dist(mx, my, ex, ey); d <= restFarmCells && d < bd {
				best, bd = e, d
			}
		}
		if cur := b.ents[b.target]; b.target != 0 && cur != nil && cur.Type == objMob {
			if ex, ey := cur.pos(now); dist(rx, ry, ex, ey) <= restFarmCells {
				best = cur // finish the fight at hand
			}
		}
		if best != nil {
			return engage(best, "farming by "+r.Name+" (breather):")
		}
	}
	// 2. monsters coming near a resting member (not bosses: never pull one onto sitting players)
	for _, e := range b.ents {
		if e.Type != objMob || healer {
			continue
		}
		if mb := mobs[e.Class]; mb != nil && mb.Boss {
			continue
		}
		ex, ey := e.pos(now)
		if dist(mx, my, ex, ey) > 12 {
			continue
		}
		for _, s := range members {
			if s.Online && s.Resting && s.Map == b.MapName && s.Name != b.charName() {
				if sx, sy := livePosOf(s); dist(sx, sy, ex, ey) <= 5 {
					return engage(e, "keeping off the resting party:")
				}
			}
		}
	}
	// 3. a fight already at hand goes on; a far target is dropped (no chasing during a rest)
	if e := b.ents[b.target]; b.target != 0 && e != nil && e.Type == objMob {
		if ex, ey := e.pos(now); dist(mx, my, ex, ey) <= 3 {
			b.setState("fighting " + mobName(e.Class) + " by the resting party")
			b.fight(e, moving, now)
			return true
		}
	}
	b.target = 0
	// 4. regen too while we wait, else stand guard
	needRegen := b.hpFrac() < 0.95 || b.MaxSP > 0 && b.SP*100/b.MaxSP < 90
	if needRegen && !b.overRegenWeight() && !b.threatNear(members, 5, now) { // never seated with a monster coming
		if !b.Sitting && !moving && !b.fightOn(now) && !b.poisoned(now) && now.Sub(b.sitSentAt) > 3*time.Second && b.basicLv() >= 3 {
			b.sitSentAt = now
			go b.sit()
		}
		b.restSitAt = now
		b.setState("sitting with the party (" + r.Name + " resting)")
		return true
	}
	b.wantSit = false // regen done: a rest of our own must not linger (hunt, which clears it, doesn't run while we hold)
	b.setState("guarding " + r.Name + " (resting)")
	return true
}

// resting: a member of the party is resting right now (published state; read like hunt reads p.Members).
func (p *Party) resting() bool {
	for _, m := range p.Members {
		if s, ok := peek(m.P.Name); ok && s.Online && !s.Errand && s.Resting {
			return true
		}
	}
	return false
}

// ---- Announce rests and their end (owner rule 2026-11-14, RULES.md 3) ----
// A member that starts resting says so in party chat once ("need sp, resting a sec"), and says "ok ready" when done.
// The published Resting flag (what the rest of the party holds for) runs from that start to that end, so the party
// moves on right after the last "ready" instead of guessing from sit packets. Human leaders (Ayla) announce with
// party lines, parsed by humanRestHeard; their sitting is only a fallback, with a timeout.

// restMinDur: a rest shorter than this is not a party rest (no announcement, no hold).
const restMinDur = 4 * time.Second

// restAnnounce: called from publish (b.mu held) after every think.
func (b *Bot) restAnnounce(now time.Time) {
	resting := b.Online && !b.Dead && b.restingNow()
	// a rest counts (and is announced) only once it has lasted restMinDur: a sit cut short by a hit a second later
	// used to post "resting a bit, low hp" + "done resting, lets go" within one second (QA 11:30, Holymonk)
	if !resting {
		b.restSince = time.Time{}
	} else if b.restSince.IsZero() {
		b.restSince = now
	}
	if resting && !b.restOn && now.Sub(b.restSince) < restMinDur {
		return
	}
	if resting == b.restOn {
		return
	}
	b.restOn = resting
	p := partyOf(b)
	if p == nil || b.P == nil {
		b.restSaid = false
		return
	}
	typing := b.P.Typing
	if resting {
		b.restSaid = false
		if now.Sub(b.restSaidAt) < 45*time.Second {
			return // a rest right after the last one: no chat spam (the flag still holds the party)
		}
		b.restSaidAt, b.restSaid = now, true
		line := pick("low hp, resting a sec", "hp low, need a quick rest", "resting a bit, low hp")
		if (b.spCaster() || b.isHealer()) && b.MaxSP > 0 && float64(b.SP)/float64(b.MaxSP) < b.hpFrac() {
			line = pick("need sp, resting a sec", "sp low, sitting a sec", "resting for sp, gimme a moment")
		}
		go b.partySay(styled(typing, line))
		return
	}
	if b.restSaid {
		b.restSaid = false
		go b.partySay(styled(typing, pick("ok ready", "ready", "ok, good to go", "done resting, lets go", "ready, lets move")))
	}
}

var humanRest = struct {
	sync.Mutex
	on       map[string]bool      // last announcement: true = "resting", false = "ready"
	at       map[string]time.Time // when it was said
	sitSince map[string]time.Time // fallback: sitting since (zero = standing)
	help     map[string]time.Time // her last call for help
}{on: map[string]bool{}, at: map[string]time.Time{}, sitSince: map[string]time.Time{}, help: map[string]time.Time{}}

var (
	humanHelpLineRe = regexp.MustCompile(`(?i)\b(help|heal me|save me|im dying)\b`)
	// a rest line is a whole clause of her message ("need sp, resting"): a rest word inside a sentence is not one
	// ("the rest of you", "k, sit, we cover u", "tell me if u need a break", "np, rest up" were all false rests)
	humanRestClauseRe  = regexp.MustCompile(`(?i)^((ok|okay|k|kk|yep|np|brb)\s+)?((gotta|got to|need to|have to|gonna|going to)\s+(rest|sit|sit down)|resting|rest|sitting( down)?|sit down|brb (resting|rest|sp)|need (sp|hp|a (sec|break|moment))|regen(ing)?)( (a bit|a sec|a min|a moment|a little|for a (bit|sec|min|moment|little)|now|rn|real quick))?$`)
	humanClauseSplitRe = regexp.MustCompile(`[,.!;]+`)
	humanReadyLineRe   = regexp.MustCompile(`(?i)\b(ready|rdy|let'?s (go|move)|lets (go|move)|moving( on)?|done resting|go go|ok go|full sp)\b`)
)

const (
	humanRestMax    = 3 * time.Minute  // an announced rest with no "ready" (and she's no longer sitting) ends after this
	humanSaidValid  = 5 * time.Minute  // an announcement decides for this long; after it only the sit fallback
	humanSitTimeout = 60 * time.Second // unannounced sitting counts as a rest for at most this long
)

// humanRestHeard: a party chat line "Name : text" - the party's human leader announcing a rest or its end.
func humanRestHeard(p *Party, text string, now time.Time) {
	if p == nil || p.Human == "" {
		return
	}
	i := strings.Index(text, " : ")
	if i < 0 || !strings.EqualFold(strings.TrimSpace(text[:i]), p.Human) {
		return
	}
	humanRestLine(p.Human, text[i+3:], now)
}

// humanRestClause: some clause of the line (split on , . ! ;) is a rest announcement on its own.
func humanRestClause(line string) bool {
	for _, c := range humanClauseSplitRe.Split(line, -1) {
		if humanRestClauseRe.MatchString(strings.TrimSpace(c)) {
			return true
		}
	}
	return false
}

func humanRestLine(name, line string, now time.Time) {
	if humanHelpLineRe.MatchString(line) {
		humanRest.Lock()
		humanRest.help[strings.ToLower(name)] = now // protectSupport: she called for help
		humanRest.Unlock()
	}
	ready, rest := humanReadyLineRe.MatchString(line), humanRestClause(line)
	if !ready && !rest {
		return
	}
	humanRest.Lock()
	defer humanRest.Unlock()
	key := strings.ToLower(name)
	humanRest.on[key] = rest && !ready // "done resting, lets go" is a ready line
	humanRest.at[key] = now
}

// humanSitSeen: the human seen sitting / standing (status file or a sit packet at `at`).
func humanSitSeen(name string, sitting bool, at time.Time) {
	humanRest.Lock()
	defer humanRest.Unlock()
	key := strings.ToLower(name)
	if !sitting {
		humanRest.sitSince[key] = time.Time{}
		return
	}
	if t := humanRest.sitSince[key]; t.IsZero() || at.Before(t) {
		humanRest.sitSince[key] = at
	}
}

// humanResting: the human leader counts as resting - from her "resting" line to her "ready" line; without a recent
// announcement, while seen sitting for under humanSitTimeout.
func humanResting(name string, now time.Time) bool {
	humanRest.Lock()
	defer humanRest.Unlock()
	key := strings.ToLower(name)
	sit := humanRest.sitSince[key]
	if at, ok := humanRest.at[key]; ok && now.Sub(at) < humanSaidValid {
		if !humanRest.on[key] {
			return false // she said "ready": move on even if she still sits
		}
		return now.Sub(at) < humanRestMax || !sit.IsZero()
	}
	return !sit.IsZero() && now.Sub(sit) < humanSitTimeout
}

// regroupProgressWin: a regrouping member that got closer to its leader within this long is "progressing".
const regroupProgressWin = 5 * time.Second

// regroupWalkOn: a member regrouping while something hits it walks on instead of stopping to fight - while it is
// still getting closer (sinceBest = time since its best distance to the leader) and has HP to spare. Stuck, blocked
// or hurt: it fights back (owner rule: a member far from the leader regroups first, fighting only what hits it).
func regroupWalkOn(sinceBest time.Duration, hp float64) bool {
	return sinceBest < regroupProgressWin && hp >= 0.5
}

// healerWalkOn: a healer on its way doesn't stop to trade blows even when hurt (it heals itself - healFirst - and
// escapes by wing - survive); it fights back only when it stopped making headway (blocked in).
func healerWalkOn(sinceBest time.Duration) bool {
	return sinceBest < regroupProgressWin
}

// leaderDetour: a bot party's leader is off on a detour: on a town trip, or its goal map isn't the party map.
func leaderDetour(partyMap string, lead pubState) bool {
	if partyMap == "" || !lead.Online || lead.Map == "" {
		return false
	}
	return lead.Trip || lead.Map != partyMap && lead.Goal != partyMap
}

// detourHold: where a member waits while its leader is on a detour: with the leader when it is on the member's own
// map (partyThink regroups) or already on the party map, else in the party's checkpoint town (the Kafra town nearest the party map; owner rule:
// parties regroup at the Kafra nearest the destination and set out together).
// detour = the leader is off elsewhere; otherwise it heads for the party map but hasn't reached the checkpoint yet.
// follow = the leader (setting out, not on a detour) just crossed from our map to its current one.
// A member on the leader's map stays with it (partyThink keeps it within reach; the leader waits for it, ai.go) and
// follows it map by map through the portals - the party walks out together, via the checkpoint town where everyone
// saves (QA 10:28 guild02: members parked on gef_fild09 while the Sage leader crossed the Mjolnir maps alone).
func detourHold(partyMap, checkpoint, leadMap, myMap string, detour, follow bool) string {
	if myMap == partyMap || leadMap == myMap {
		return myMap // already on the party map (no walking back), or with the leader
	}
	if follow && !detour {
		return leadMap // right behind it: through the same portal
	}
	if checkpoint == "" {
		checkpoint = nearestTown(partyMap)
	}
	return checkpoint
}

// ---- Party loot is free for all (owner rule 2026-11-14: "let party member pickup anything") ----
// rAthena keeps a drop for its killer for a few seconds unless the party's item pickup option is "Party Share"
// (party.item & 1): then any member may pick it up (and keeps it; item & 2 would hand it out randomly - not set).
// Parties are created with it (0x01e8 item1 = 1) and every option change keeps it (0x07d7 exp, 1, 0); parties that
// already existed get it from their in-game leader once per session (and again every 30 min, harmless if set).
func (b *Bot) partyLootShare(now time.Time) {
	if b.PartyID == 0 || !b.Online || partyOf(b) == nil || !strings.EqualFold(b.partyLeader, b.charName()) ||
		now.Sub(b.lootShareAt) < 30*time.Minute {
		return
	}
	b.lootShareAt = now
	go b.send(newPkt(0x07d7).u32(1).u8(1).u8(0).b) // CZ_GROUPINFO_CHANGE_V2: even exp, Party Share pickup, no random share
}

// leaderWaitMax: a travelling bot party leader waits at most this long per map for members to catch up.
const leaderWaitMax = 90 * time.Second

// leaderStraggler: a member on the leader's own map but beyond together_cells (live positions) - a travelling leader
// waits for it before moving on (owner rule: the leader doesn't set off alone; a fragile Sage leader died walking the
// Mjolnir maps alone with its party a map behind).
func leaderStraggler(members []pubState, me, myMap string, x, y int) (pubState, bool) {
	for _, s := range members {
		if s.Name == me || !s.Online || s.Map != myMap {
			continue
		}
		if sx, sy := livePosOf(s); (sx != 0 || sy != 0) && dist(x, y, sx, sy) > togetherCells() {
			return s, true
		}
	}
	return pubState{}, false
}

// partyLeaderSetOut (b.mu held, travel branch): a bot party leader on its way to the party map
//  1. waits on each map for members on that map that are beyond together_cells (at most leaderWaitMax per map), so the
//     party walks out together instead of the leader crossing hostile maps alone;
//  2. goes via the party's checkpoint town (the Kafra town nearest the party map) once per party map when its save
//     point is elsewhere, so it saves there (travel's kafraSavePlan) - owner rule: save at the Kafra nearest the
//     grinding ground; ThePuddinglord kept a Payon save while hunting Mjolnir, so each escape threw it far away.
//
// Returns true while it waits.
func (b *Bot) partyLeaderSetOut(now time.Time) bool {
	p := partyOf(b)
	if p == nil || p.Leader != b || p.Human != "" || b.Goal.Set != "party-leader" || isTown(b.Goal.Map) {
		return false
	}
	if b.kafra == nil && !b.underAttack(now) {
		_, _, members := b.partyView(p, now)
		lx, ly := b.livePos(now)
		if s, ok := leaderStraggler(members, b.charName(), b.MapName, lx, ly); ok {
			if b.leadWaitMap != b.MapName {
				b.leadWaitMap, b.leadWaitSince = b.MapName, now
			}
			if now.Sub(b.leadWaitSince) < leaderWaitMax {
				if b.farmNear(8, "while waiting for "+s.Name, now) { // owner rule: farm while waiting
					return true
				}
				b.target = 0
				b.setState("waiting for " + s.Name + " before moving on")
				return true
			}
		}
		// (no reset when the straggler is momentarily in range: the 90 s are per map visit - QA after the 15:00 deploy:
		// guild01's Silver_heart "waiting for xxvexfoxxx before moving on" 3+ min, 22 leaders in that state; a member
		// flickering in and out of together_cells restarted the wait every time)
	}
	parties.Lock()
	cp := p.Checkpoint
	parties.Unlock()
	if cp == "" || nearestTown(b.Goal.Map) != cp {
		cp = nearestTown(b.Goal.Map)
	}
	// only with a KNOWN save point (QA 11:04: after a restart savedTown was empty, the detour fired, the leader left
	// gef_fild09 for Geffen and back without saving), and never when the party already hunts there - it saves when a
	// route passes the town (travel's kafraSavePlan)
	saved, known := b.knownSave()
	if cp == "" || !known || b.viaTown != "" || b.kafra != nil || b.MapName == cp || saved == cp || len(kafraSpots[cp]) == 0 ||
		b.saveStillGood(cp, b.Goal.Map) {
		return false
	}
	for _, m := range p.Members {
		if s, ok := peek(m.P.Name); ok && m != b && s.Online && s.Map == b.Goal.Map {
			return false // the party is already on its map: go straight back to it
		}
	}
	key := "cpvia:" + b.Goal.Map
	if _, done := b.buffAt[key]; done {
		return false
	}
	b.buffAt[key] = now
	b.viaTown, b.viaFor, b.route, b.path = cp, b.Goal.Map, nil, nil
	b.event("info", "", fmt.Sprintf("party: meeting at the Kafra in %s to save before %s (saved in %s)", cp, b.Goal.Map, b.savedTown))
	return false
}

// noteSaved (b.mu held): the bot's real save point - from a Kafra save or a respawn - kept in its memory file so a
// restart doesn't forget it (the engine used to assume the home town after every login).
func (b *Bot) noteSaved(town string) {
	b.savedTown, b.saveKnown = town, true
	if b.P == nil || len(b.P.Chars) == 0 || town == "" {
		return
	}
	m := memoryOf(b.P)
	m.mu.Lock()
	if m.Saved == nil {
		m.Saved = map[string]string{}
	}
	if m.Saved[b.charName()] != town {
		m.Saved[b.charName()] = town
		m.dirty = true
	}
	m.mu.Unlock()
}

// knownSave (b.mu held): the save town when it is really known (this session or remembered), else ("", false).
func (b *Bot) knownSave() (string, bool) {
	if b.saveKnown {
		return b.savedTown, true
	}
	if b.P == nil || len(b.P.Chars) == 0 {
		return "", false
	}
	m := memoryOf(b.P)
	m.mu.Lock()
	t := m.Saved[b.charName()]
	m.mu.Unlock()
	if t == "" {
		return "", false
	}
	b.savedTown, b.saveKnown = t, true
	return t, true
}

// ---- Healer safety (QA 2026-11-14 11:02-11:30: party healer deaths 4 -> 28 per ~25 min) ----
// Most died "fighting Muka / Peco Peco / Orc Warrior" (a party healer fell through to hunt and engaged monsters on its
// own), walking on hurt while monsters hit it, or seated next to approaching monsters. A party healer heals first,
// sits only when nothing threatening is near, stands the moment a monster comes close or targets it, and never pulls.

// healerThreatCells: a healer doesn't sit (and stands up) with an aggressive monster this close, or any monster
// coming at it / a member this close.
const healerThreatCells = 7

// threatNear (b.mu held): a monster within cells of the bot that is aggressive, targets the bot or a party member, or
// hit us lately. Plants and the like (worthless) don't count.
func (b *Bot) threatNear(members []pubState, cells int, now time.Time) bool {
	mx, my := b.livePos(now)
	ours := map[uint32]bool{b.aid: true}
	for _, s := range members {
		if s.AID != 0 && s.Map == b.MapName {
			ours[s.AID] = true
		}
	}
	for _, e := range b.ents {
		if e.Type != objMob {
			continue
		}
		mb := mobs[e.Class]
		if mb.worthless() {
			continue
		}
		ex, ey := e.pos(now)
		if dist(mx, my, ex, ey) > cells {
			continue
		}
		if mb == nil || mb.Aggressive || mb.Boss || ours[e.Target] && now.Sub(e.TargetAt) < 6*time.Second ||
			now.Sub(e.LastHitMe) < 6*time.Second {
			return true
		}
	}
	return false
}

// partyHealer (b.mu held): an Acolyte/Priest-line member of a party (not its leader).
func (b *Bot) partyHealer() bool {
	if !b.isHealer() {
		return false
	}
	p := partyOf(b)
	return p != nil && p.Leader != b
}

// ---- Higher-level parties (owner 2026-11-14: "with higher level party, the mechanism changes a bit as bots can
// survive better. Member should run straight to party leaders instead of leader has to collect them one by one. Still
// tanks and melee need to protect range and support") ----

// partyStrongLv: the weakest member's effective (gear) level from which a party counts as strong.
const partyStrongLv = 50

// partyStrong: every online member's effective level is known and at least partyStrongLv. Such a party's leader
// doesn't collect members one by one (only one in danger) and members go straight to the leader / party map when
// their route is safe (partyGoalSync routeSafeFrom); weaker parties keep the leader-collects / checkpoint rules.
func partyStrong(p *Party) bool {
	if p == nil {
		return false
	}
	n := 0
	for _, m := range p.Members {
		s, ok := peek(m.P.Name)
		if !ok || !s.Online {
			continue
		}
		if !s.Known || s.Power < partyStrongLv {
			return false
		}
		n++
	}
	return n >= 2
}

// meleePeel (b.mu held): a melee party member takes a monster off a ranged / support member (pubState.Back, Support,
// Fragile) within 10 cells - the tank's peel (tankTakeAggro) for the rest of the front line.
func (b *Bot) meleePeel(members []pubState, moving bool, now time.Time) bool {
	var backs []pubState
	for _, s := range members {
		if s.Back || s.Support || s.Fragile {
			backs = append(backs, s)
		}
	}
	peel, who, pd := b.tankPeelTarget(backs, now)
	if peel == nil || pd > 10 {
		return false
	}
	if b.target != peel.ID {
		b.target, b.targetAt = peel.ID, time.Time{}
	}
	b.setState("taking " + mobName(peel.Class) + " off " + who)
	b.fight(peel, moving, now)
	return true
}

// humanLeaving: the human leader is on her way to another hunting map - her own status file (fresh) names a lock map
// that isn't the map she stands on (QA 2026-11-14 13:03: her mind sent her to gl_church; the friends kept pulling on
// pay_dun00 and she stopped to fight every few steps).
func humanLeaving(name string, now time.Time) bool {
	st, ok := humanStatusOf(name)
	if !ok || st.LockMap == "" || st.Map == "" || now.Sub(time.Unix(st.TS, 0)) > 10*time.Second {
		return false
	}
	return st.LockMap != st.Map && !isTown(st.Map)
}

// repointTownRun: the party's town run now goes to town (a fresh run time, so members join it again).
func (p *Party) repointTownRun(town string, now time.Time) {
	parties.Lock()
	p.TownRunAt, p.TownRunTown = now, town
	parties.Unlock()
}

// ---- Leaders announce the plan (owner 2026-11-14: "Party leaders must always communicate the plan ... do not let the
// party follow her blindly") ----

// planLine: the party chat line a leader says before a move - where, why, and where to meet: a strong party goes
// straight to the map (members route there themselves), a weaker one meets at the Kafra town nearest it first
// (partyGoalSync acts on the same plan: Goal.Map = the party map / its checkpoint town).
func planLine(p *Party, m, why string) string {
	where := mapTalk(m)
	meet := ""
	if cp := nearestTown(m); cp != "" && cp != m {
		if partyStrong(p) {
			meet = "go straight there, meet me on the map"
		} else {
			meet = "meet at the kafra in " + cp + " first"
		}
	}
	line := "next: " + where
	if why != "" {
		line += " (" + why + ")"
	}
	if meet != "" {
		line += ", " + meet
	}
	return line
}

// ---- Party Kafra fares (QA 2026-11-14 18:32Z: on Ayla's party move to Morocc, xXRinfangXx and vex_x18 couldn't pay
// the Prontera Kafra, the walk west hit a banned warp - "no route to morocc" for 2 min - until Ayla mailed them zeny) ----

// followingParty (b.mu held): the bot is moving with its party - a human leader's friend, or a bot party member / leader
// heading for the party map.
func (b *Bot) followingParty() bool {
	p := partyOf(b)
	if p == nil {
		return false
	}
	if p.Human != "" {
		return true
	}
	parties.Lock()
	pm := p.Map
	parties.Unlock()
	return pm != "" && b.Goal.Map == pm && (b.Goal.Mode == "party" || b.Goal.Set == "party-leader")
}

// askFare (b.mu held): a party member short of a Kafra fare says so and a party mate with zeny to spare mails it.
// At most once per 2 minutes.
func (b *Bot) askFare(need int, town, goal string, now time.Time) {
	if need <= 0 || b.buffAt == nil || now.Before(b.buffAt["fareask"]) {
		return
	}
	b.buffAt["fareask"] = now.Add(2 * time.Minute)
	need += 100 // a little over the fare
	p := partyOf(b)
	if p == nil {
		return
	}
	me, typing := b.charName(), ""
	if b.P != nil {
		typing = b.P.Typing
	}
	b.event("info", "", fmt.Sprintf("party: %dz short of the Kafra in %s toward %s, asking the party", need, town, goal))
	go func() {
		b.partySay(styled(typing, pick(fmt.Sprintf("need %dz for the kafra to %s, anyone?", need, mapTalk(goal)),
			fmt.Sprintf("broke lol, can someone send %dz for the kafra?", need))))
		for _, m := range p.Members {
			if m == b {
				continue
			}
			m.mu.Lock()
			ok := m.Online && !m.Dead && m.Zeny >= mailCost(need)+3000 && m.mailZeny(me, need, "kafra fare", "for the kafra, see u there", nil)
			name, mt := m.charName(), ""
			if m.P != nil {
				mt = m.P.Typing
			}
			if ok {
				m.event("info", me, fmt.Sprintf("mailing %dz to %s for the Kafra fare", need, me))
			}
			m.mu.Unlock()
			if ok {
				m.partySay(styled(mt, pick(fmt.Sprintf("%s sent u %dz, check mail", me, need), "sent, check ur mail")))
				_ = name
				return
			}
		}
	}()
}

// ---- Save at the hunting map's nearest Kafra before going in (owner rule, enforced 2026-11-14: "there was a rule saying
// always save location to a kafra closest to hunting ground. If they go to pyramid why are their spawn location
// somewhere other than moroc?") ----

// saveGate (b.mu held, travel branch): heading for a hunting map whose nearest Kafra town isn't our KNOWN save point ->
// go via that town first (travel's viaTown; kafraSavePlan saves there on arrival, the save reported in party chat).
// One detour per goal per 15 minutes, so a Kafra that won't answer can't loop us.
func (b *Bot) saveGate(now time.Time) bool {
	goal := b.Goal.Map
	if goal == "" || isTown(goal) || townOf(goal) != "" || b.trip != nil || b.viaTown != "" || b.kafra != nil ||
		b.Goal.Mode == "town" || b.Goal.Mode == "vend" || b.buffAt == nil {
		return false
	}
	cp := nearestTown(goal)
	if p := partyOf(b); p != nil && p.Human != "" {
		cp = humanSaveTown(p) // her save / rally town (unknown: no detour - the friends follow her)
	}
	if cp == "" || len(kafraSpots[cp]) == 0 || cp == b.MapName {
		return false // in that town already: travel's kafraSavePlan saves before we walk out
	}
	if saved, known := b.knownSave(); known && saved == cp {
		return false
	}
	// far from that town: no forced walk there (QA 14:22: Ayla's friends WALKED from the Geffen / Mjolnir fields to
	// Payon "via the Kafra in payon"); normal travel takes the Kafra and usually lands in it - the save happens there
	// (travel's kafraSavePlan) - and the gate checks again once we are close
	if mapHops(b.MapName, b.X, b.Y, cp) > 2 {
		return false
	}
	key := "savegate:" + goal
	if now.Before(b.buffAt[key]) {
		return false
	}
	b.buffAt[key] = now.Add(15 * time.Minute)
	saved, _ := b.knownSave()
	b.viaTown, b.viaFor = cp, goal
	b.event("info", "", fmt.Sprintf("saving at the Kafra in %s before %s (save point %q)", cp, goal, saved))
	return true
}

// saveMismatch: an online bot whose known save town isn't the nearest Kafra town of the map it hunts (the QA check
// behind GET /api/savecheck).
type saveMismatch struct {
	Name  string `json:"name"`
	Map   string `json:"map"`
	Goal  string `json:"goal"`
	Saved string `json:"saved"`
	Want  string `json:"want"`
	Known bool   `json:"known"`
}

// hostileTransit: a map the party crosses together, close to its leader - a dungeon, or a map with aggressive
// monsters at/above our effective level (mapDanger without other players' deaths).
func hostileTransit(m string, lv int) bool {
	if m == "" || isTown(m) || townOf(m) != "" {
		return false
	}
	if dungeonMap(m) {
		return true
	}
	return mapDanger(m, lv)-recentDeaths(m)*3 > 20
}

// dungeonMap: a dungeon by its map name.
func dungeonMap(m string) bool {
	return strings.Contains(m, "dun") || strings.HasPrefix(m, "in_sphinx") || strings.HasPrefix(m, "moc_pryd") ||
		strings.HasPrefix(m, "anthell") || strings.HasPrefix(m, "orcsdun") || strings.HasPrefix(m, "gl_") ||
		strings.HasPrefix(m, "prt_sewb") || strings.HasPrefix(m, "treasure") || strings.HasPrefix(m, "alde_dun")
}

// mobbedNow (b.mu held): monsters that hit us in the last 3 s.
func (b *Bot) mobbedNow() int {
	n := 0
	now := time.Now()
	for _, e := range b.ents {
		if e.Type == objMob && now.Sub(e.LastHitMe) < 3*time.Second {
			n++
		}
	}
	return n
}

// tankGoHelp (b.mu held): a party mate on our map with 3+ monsters on it, beyond our peel range (12) but within 40
// cells: the tank goes to it, fighting only what hits it on the way (QA 19:00Z Sphinx: "help! N mobs on me" went
// unanswered and three friends died).
func (b *Bot) tankGoHelp(gm *GameMap, members []pubState, moving bool, now time.Time) bool {
	if gm == nil {
		return false
	}
	mx, my := b.livePos(now)
	var tgt pubState
	bd := 99
	for _, s := range members {
		if s.Name == b.charName() || !s.Online || s.Map != b.MapName || s.Mobbed < 3 {
			continue
		}
		sx, sy := livePosOf(s)
		if d := dist(mx, my, sx, sy); d > 12 && d <= 40 && d < bd {
			tgt, bd = s, d
		}
	}
	if bd == 99 {
		return false
	}
	if b.Sitting && now.Sub(b.sitSentAt) > time.Second {
		b.sitSentAt, b.wantSit = now, false
		go b.stand()
	}
	b.target = 0
	b.setState(fmt.Sprintf("coming to help %s (%d on it)", tgt.Name, tgt.Mobbed))
	if !moving {
		sx, sy := livePosOf(tgt)
		b.stepAlong(gm, sx, sy)
	}
	return true
}

// humanSaveTown: where a human leader's friends save - her own save point (status.json save_map, set by her plugin's
// verified Kafra save), else the town of a rally in progress; "" when unknown (then nobody detours or saves on a guess).
func humanSaveTown(p *Party) string {
	if p == nil || p.Human == "" {
		return ""
	}
	if st, ok := humanStatusOf(p.Human); ok && st.SaveMap != "" && len(kafraSpots[st.SaveMap]) > 0 {
		return st.SaveMap
	}
	if t := p.rallyInfo().Town; t != "" {
		return t
	}
	return ""
}

// farmNear (b.mu held): owner rule 2026-11-14 "Farm while waiting" - a bot waiting on a field for party mates (not a
// town Kafra rally, not a rest) fights a monster within radius of where it stands instead of idling: the one it is
// already on, else the nearest fitting one (not a boss / champion pack, within its level filter, nobody else's).
// A healer never pulls (healer safety).
func (b *Bot) farmNear(radius int, why string, now time.Time) bool {
	if isTown(b.MapName) || townOf(b.MapName) != "" || b.isHealer() || b.hpFrac() < 0.5 {
		return false
	}
	mx, my := b.livePos(now)
	if e := b.ents[b.target]; b.target != 0 && e != nil && e.Type == objMob {
		if ex, ey := e.pos(now); dist(mx, my, ex, ey) <= radius+2 {
			b.setState("fighting " + mobName(e.Class) + " " + why)
			b.fight(e, false, now)
			return true
		}
	}
	maxLv := b.targetMaxLv()
	var pick *Entity
	pd := radius + 1
	for _, e := range b.ents {
		if e.Type != objMob {
			continue
		}
		mb := mobs[e.Class]
		if mb == nil || mb.Boss || mb.Champion || mb.Level > maxLv || mb.worthless() {
			continue
		}
		ex, ey := e.pos(now)
		if d := dist(mx, my, ex, ey); d < pd && claims.free(b.MapName, e.ID, b.aid) {
			pick, pd = e, d
		}
	}
	if pick == nil || !claims.try(b.MapName, pick.ID, b.aid, false) {
		return false
	}
	b.target, b.targetAt = pick.ID, time.Time{}
	b.setState("fighting " + mobName(pick.Class) + " " + why)
	b.fight(pick, false, now)
	return true
}

// humanCalledHelp: the human leader said "help" (or similar) in party chat in the last 45 s.
func humanCalledHelp(name string, now time.Time) bool {
	humanRest.Lock()
	defer humanRest.Unlock()
	at, ok := humanRest.help[strings.ToLower(name)]
	return ok && now.Sub(at) < 45*time.Second
}

// ---- Tanks and melee protect the supports (owner rule 2026-11-14: "need to protect support when they call for help or
// being attacked or low hp") ----

// protectRange: how far (same map) a tank / melee goes to protect a support.
const protectRange = 28

// supportNeed: why a party member needs protecting, "" if it doesn't. Supports = ranged / healer / fragile members;
// a human leader only when she is hurt AND called for help (core: Ayla runs her own fights).
func supportNeed(s pubState, human bool) string {
	if !s.Online {
		return ""
	}
	if human {
		if s.Help && s.HPFrac < 0.6 {
			return "called for help"
		}
		return ""
	}
	if !(s.Back || s.Support || s.Fragile) {
		return ""
	}
	switch {
	case s.Help:
		return "called for help"
	case s.Mobbed > 0:
		return "being attacked"
	case s.HPFrac > 0 && s.HPFrac < 0.5:
		return "low hp"
	}
	return ""
}

// protectSupport (b.mu held): a tank (or a melee member when no tank is closer to it) protects the nearest support in
// need on this map within protectRange: a monster targeting it within our sight is taken off it ("taking X off Y"),
// otherwise we walk to it. Comes before the tank's own pulls.
func (b *Bot) protectSupport(gm *GameMap, members []pubState, moving bool, now time.Time) bool {
	mx, my := b.livePos(now)
	isTank := partyRole(b) == roleTank
	p := partyOf(b)
	var tgt pubState
	why, bd := "", protectRange+1
	for _, s := range members {
		if s.Name == b.charName() || s.Map != b.MapName {
			continue
		}
		human := p != nil && p.Human != "" && strings.EqualFold(s.Name, p.Human)
		w := supportNeed(s, human)
		if w == "" {
			continue
		}
		sx, sy := livePosOf(s)
		d := dist(mx, my, sx, sy)
		if d >= bd {
			continue
		}
		if !isTank { // melee: only when no party tank is nearer to it
			tankNear := false
			for _, t := range members {
				if t.Tank && t.Online && t.Map == s.Map && t.Name != b.charName() {
					if tx, ty := livePosOf(t); dist(tx, ty, sx, sy) < d {
						tankNear = true
					}
				}
			}
			if tankNear {
				continue
			}
		}
		tgt, why, bd = s, w, d
	}
	if why == "" {
		return false
	}
	// a monster on it that we can see: take it off
	var peel *Entity
	pd := 99
	for _, e := range b.ents {
		if e.Type != objMob || tgt.AID == 0 || e.Target != tgt.AID || now.Sub(e.TargetAt) > 4*time.Second {
			continue
		}
		ex, ey := e.pos(now)
		if d := dist(mx, my, ex, ey); d < pd {
			peel, pd = e, d
		}
	}
	if peel == nil && bd <= 3 {
		return false // right next to it and nothing on it we can see: the peel / defend logic carries on
	}
	if b.Sitting && now.Sub(b.sitSentAt) > time.Second {
		b.sitSentAt, b.wantSit = now, false
		go b.stand()
	}
	if peel != nil {
		if b.target != peel.ID {
			b.target, b.targetAt = peel.ID, time.Time{}
		}
		b.setState("taking " + mobName(peel.Class) + " off " + tgt.Name + " (" + why + ")")
		b.fight(peel, moving, now)
		return true
	}
	b.target = 0
	b.setState("coming to protect " + tgt.Name + " (" + why + ")")
	if !moving && gm != nil {
		sx, sy := livePosOf(tgt)
		b.stepAlong(gm, sx, sy)
	}
	return true
}

// escortGate (b.mu held): a human leader's friend outside the dungeon / hostile map she is on (in a town or on a field;
// a friend already on another floor of the dungeon goes on to her). It goes in only together and restocked:
//   - first to the entrance town (her save town, else the map's nearest Kafra town);
//   - it waits there while it is short of potions (needTrip buys them in town) or alone - it goes when at least one
//     more friend outside is in that town too, or when it is the only friend outside and its route to her is safe.
//
// Returns the town to hold in and why, or "" to go on.
func (b *Bot) escortGate(p *Party, herMap string, now time.Time) (string, string) {
	// dungeons only (a field next to town is no reason to hold a friend in town: QA 20:19Z xXRinfangXx stood at the
	// Payon respawn while Ayla walked pay_fild10)
	if p == nil || p.Human == "" || b.MapName == herMap || !dungeonMap(herMap) {
		escortRestockClear(b)
		return "", ""
	}
	// inside a dungeon: a floor is crossed only with a friend on the same floor (PT04, PT32b: members travel together
	// or with the leader; QA 2026-11-15: members walked pay_dun00-02 alone, 4x monsters, and died on the way). Alone on
	// a floor of her dungeon, the member goes back to the entrance town to gather there instead.
	// Alone on a floor of her dungeon she is elsewhere: it holds here, and follows only when she is on the next floor
	// up or down (a portal is a normal one-floor lag, not a reason to walk out of the dungeon). Back to town only when
	// hurt or out of potions. Deeper than her, or on another dungeon, the same rule.
	if dungeonMap(b.MapName) {
		if dungeonFamily(b.MapName) == dungeonFamily(herMap) && !friendOnFloor(p, b, b.MapName, now) && !floorAdjacent(b.MapName, herMap) {
			if !b.outOfPotions() && !b.hurtBadly() {
				return b.floorHoldStep(p, herMap, now)
			}
		} else {
			escortRestockClear(b)
			return "", ""
		}
	}
	town := humanSaveTown(p)
	if town == "" {
		town = nearestTown(herMap)
	}
	if town == "" || len(kafraSpots[town]) == 0 {
		escortRestockClear(b)
		return "", ""
	}
	if b.MapName != town {
		escortRestockClear(b)
		return town, "gathering before going in"
	}
	if b.outOfPotions() { // the same test that starts the trip (town.go needTrip): the hold is bounded, see escortRestockOK
		if escortRestockOK(b, now) {
			return town, "restocking potions before going in"
		}
	} else {
		escortRestockClear(b)
	}
	// short of the survival minimum but not out of potions: top up at the town's potion seller before going in, the
	// way a player at the Kafra would. No trip away and no party run: the trip stays in this town (forceTown).
	if potTopUpNeed(b, town) && escortRestockOK(b, now) {
		if b.trip == nil && (b.lastTripAt.IsZero() || now.Sub(b.lastTripAt) >= 2*time.Minute) {
			b.forceTown = town
			b.startTrip(topUpWhy, now) // chatter.go tripAnnounce: "grabbing pots, 1 min", not "out of pots, brb"
			b.forceTown = ""
		}
		if b.trip != nil {
			return town, "restocking potions before going in"
		}
	}
	outside, inTown := 0, 0
	for _, m := range p.Members {
		if m == b {
			continue
		}
		s, ok := peek(m.P.Name)
		if !ok || !s.Online || s.Map == herMap || hostileTransit(s.Map, b.powerLv()) {
			continue // offline, or already in the dungeon
		}
		outside++
		if s.Map == town {
			inTown++
		}
	}
	if inTown >= 1 || outside == 0 && b.routeSafeFrom(b.MapName, b.X, b.Y, herMap) {
		return "", "" // together with another friend, or the last one out on a safe way: go
	}
	if outside == 0 {
		return town, "the way in is too dangerous alone"
	}
	return town, "waiting for the others to go in together"
}

// dungeonFamily: the dungeon a floor belongs to ("pay_dun03" -> "pay_dun"); "" for a map that is no dungeon floor.
func dungeonFamily(m string) string {
	if !dungeonMap(m) {
		return ""
	}
	return strings.TrimRight(m, "0123456789")
}

// dungeonFloorNo: the floor number of a dungeon map ("pay_dun03" -> 3); false when the map has none.
func dungeonFloorNo(m string) (int, bool) {
	n, err := strconv.Atoi(m[len(dungeonFamily(m)):])
	return n, err == nil
}

// floorAdjacent: a and b are floors of the same dungeon, one floor apart (a portal away).
func floorAdjacent(a, b string) bool {
	fa, oka := dungeonFloorNo(a)
	fb, okb := dungeonFloorNo(b)
	if !oka || !okb || dungeonFamily(a) != dungeonFamily(b) {
		return false
	}
	return fa-fb == 1 || fb-fa == 1
}

// hurtBadly: under half HP - a member hurt alone in a dungeon goes back to town to heal.
func (b *Bot) hurtBadly() bool {
	return b.MaxHP > 0 && b.HP*2 < b.MaxHP
}

// friendOnFloor: the human leader or another online friend of the party is on floor m (seen recently).
func friendOnFloor(p *Party, self *Bot, m string, now time.Time) bool {
	if h, ok := humanAt(p.Human); ok && h.Map == m && now.Sub(h.At) < 2*time.Minute {
		return true
	}
	for _, o := range p.Members {
		if o == self {
			continue
		}
		if s, ok := peek(o.P.Name); ok && s.Online && s.Map == m {
			return true
		}
	}
	return false
}

// escortRestock: when each bot's restock hold in town began (bot name -> since). A hold ends after escortRestockMax
// even if no trip came, so a friend cannot wait in town forever (QA 2026-11-15: Zen.fox held 15 Red Potions in Payon
// for the rest of the night).
var escortRestock = struct {
	sync.Mutex
	since map[string]time.Time
}{since: map[string]time.Time{}}

const escortRestockMax = 5 * time.Minute

// escortRestockOK: the restock hold is still within its time (starts it on the first call).
func escortRestockOK(b *Bot, now time.Time) bool {
	escortRestock.Lock()
	defer escortRestock.Unlock()
	key := strings.ToLower(b.P.Name)
	t, ok := escortRestock.since[key]
	if !ok {
		escortRestock.since[key] = now
		return true
	}
	return now.Sub(t) < escortRestockMax
}

// escortRestockClear: the bot is no longer holding for a restock (in the dungeon, out of the hold, or restocked). It also
// ends a floor hold (floorHold below): every exit of escortGate that is not that hold comes through here.
func escortRestockClear(b *Bot) {
	escortRestock.Lock()
	delete(escortRestock.since, strings.ToLower(b.P.Name))
	escortRestock.Unlock()
	floorHold.Lock()
	delete(floorHold.st, strings.ToLower(b.P.Name))
	floorHold.Unlock()
}

// topUpWhy: the trip reason of a potion top-up at the dungeon gate (below the survival minimum, not out of potions).
// Its party line is "grabbing pots, 1 min" (chatter.go), not "out of pots, brb".
const topUpWhy = "grabbing pots"

// potTopUpNeed: a member short of the survival minimum in a town with a potion seller tops up before going in (the
// way a player at the Kafra would). Healers heal themselves.
func potTopUpNeed(b *Bot, town string) bool {
	return !b.isHealer() && b.realPotCount() < b.potMin() && b.Zeny >= 500 && potionShop(town) != nil
}

// floorHold: a member alone on a dungeon floor, its leader two or more floors away (see floorHoldStep).
var floorHold = struct {
	sync.Mutex
	st map[string]floorHoldState
}{st: map[string]floorHoldState{}}

type floorHoldState struct {
	floor string    // the floor it holds on: a change of floor restarts the timer
	since time.Time // when it began holding here
	asked time.Time // its last call for collection in party chat
}

const (
	floorHoldMove = 3 * time.Minute // held this long: the member moves one floor toward her, or asks to be collected
	floorHoldAsk  = 3 * time.Minute // the collection call in party chat, at most this often
)

// floorHoldFor: how long the bot has held on its current floor.
func floorHoldFor(b *Bot, now time.Time) time.Duration {
	floorHold.Lock()
	defer floorHold.Unlock()
	key := strings.ToLower(b.P.Name)
	s, ok := floorHold.st[key]
	if !ok || s.floor != b.MapName {
		s = floorHoldState{floor: b.MapName, since: now, asked: s.asked}
		floorHold.st[key] = s
	}
	return now.Sub(s.since)
}

// floorHoldAskDue: the bot may call for collection now (once per floorHoldAsk); it marks the call.
func floorHoldAskDue(b *Bot, now time.Time) bool {
	floorHold.Lock()
	defer floorHold.Unlock()
	key := strings.ToLower(b.P.Name)
	s := floorHold.st[key]
	if !s.asked.IsZero() && now.Sub(s.asked) < floorHoldAsk {
		return false
	}
	s.asked = now
	floorHold.st[key] = s
	return true
}

// nextFloorToward: the floor one step from `from` toward `to`, in the same dungeon ("pay_dun00" toward "pay_dun03" is
// "pay_dun01"); false when they are not two floors of one dungeon or already the same floor.
func nextFloorToward(from, to string) (string, bool) {
	fa, oka := dungeonFloorNo(from)
	fb, okb := dungeonFloorNo(to)
	if !oka || !okb || fa == fb || dungeonFamily(from) != dungeonFamily(to) {
		return "", false
	}
	step := 1
	if fb < fa {
		step = -1
	}
	digits := from[len(dungeonFamily(from)):]
	return fmt.Sprintf("%s%0*d", dungeonFamily(from), len(digits), fa+step), true
}

// floorHoldStep: a member alone on a floor of her dungeon, she two or more floors away. It HOLDS here: it keeps fighting
// what comes to it and keeps safe (the goal stays on this floor; it does not walk on alone). After floorHoldMove it moves
// one floor toward her when a friend is on that floor or the route there is safe; otherwise it asks in party chat to
// be collected. It never walks out of the dungeon from here (only escortGate's hurt / out-of-potions case does).
func (b *Bot) floorHoldStep(p *Party, herMap string, now time.Time) (string, string) {
	if floorHoldFor(b, now) >= floorHoldMove {
		if next, ok := nextFloorToward(b.MapName, herMap); ok && (friendOnFloor(p, b, next, now) || b.routeSafeFrom(b.MapName, b.X, b.Y, next)) {
			return next, "moving one floor toward " + p.Human
		}
		if floorHoldAskDue(b, now) {
			typing := b.P.Typing
			go b.partySay(styled(typing, pick("stuck on "+b.MapName+", can someone come get me?", "im alone on "+b.MapName+", can u come back for me?")))
		}
	}
	return b.MapName, "waiting on this floor for " + p.Human
}

// humanTrail: the maps a human leader walked through lately (oldest first), so her friends take the same road.
var humanTrail = struct {
	sync.Mutex
	m  map[string][]string
	at map[string]time.Time
	ts map[string][]time.Time // when she reached each map of m
}{m: map[string][]string{}, at: map[string]time.Time{}, ts: map[string][]time.Time{}}

// humanTrailFresh: a step of her road older than this no longer guides members (she left that road)
const humanTrailFresh = 5 * time.Minute

// humanTrailNote: she is on map m now (called with every fresh view of her).
func humanTrailNote(name, m string, now time.Time) {
	if m == "" {
		return
	}
	key := strings.ToLower(name)
	humanTrail.Lock()
	defer humanTrail.Unlock()
	t, ts := humanTrail.m[key], humanTrail.ts[key]
	if now.Sub(humanTrail.at[key]) > 15*time.Minute || len(ts) != len(t) {
		t, ts = nil, nil // an old road is no guide
	}
	humanTrail.at[key] = now
	if len(t) > 0 && t[len(t)-1] == m {
		return
	}
	for i, x := range t { // back on a map she crossed: cut the loop
		if x == m {
			t, ts = t[:i], ts[:i]
			break
		}
	}
	t, ts = append(t, m), append(ts, now)
	if len(t) > 12 {
		t, ts = t[len(t)-12:], ts[len(ts)-12:]
	}
	humanTrail.m[key], humanTrail.ts[key] = t, ts
}

// humanTrailNext: from map `from` (one she crossed on her way to `cur`), the map she went to next; "" if `from` isn't
// on her recent road.
func humanTrailNext(name, cur, from string) string {
	humanTrail.Lock()
	defer humanTrail.Unlock()
	t, ts := humanTrail.m[strings.ToLower(name)], humanTrail.ts[strings.ToLower(name)]
	if len(t) == 0 || t[len(t)-1] != cur {
		return ""
	}
	return trailNextFresh(t, ts, from, time.Now())
}

// trailNextFresh: the map after `from` on her road, only if she took that step within humanTrailFresh
func trailNextFresh(t []string, ts []time.Time, from string, now time.Time) string {
	for i := 0; i < len(t)-1; i++ {
		if t[i] == from {
			if i+1 < len(ts) && now.Sub(ts[i+1]) > humanTrailFresh {
				return "" // an old step: follow her current map instead
			}
			return t[i+1]
		}
	}
	return ""
}
