package main

// Farming share fixes for solo bots (fix/farm-share, 2026-11-14). Sentinel: farming share 70% < 85% (travelling 14%,
// waiting 7%). Measured live (10 samples of ~850 fighters, 02:55-03:00Z):
//
//  1. Kafra waits that never end in a teleport. QA: TheKai2k (Arrow Bard 50) sat in prontera 511 s "waiting for the
//     Kafra, the walk to pay_fild07 is too dangerous". kafraHardUntil is sticky: any kafraHold check of ONE destination
//     (the town it just left, Alberta) raised it - quiet cost estimates and virtual plans from another town too - and
//     kafraWaitInTown then waited out the whole 10 minutes without asking whether another destination was open
//     (Payon was) or whether the held destination would be allowed at all once the hard limit cleared (an X -> Y -> X
//     stays refused by the soft 50-min rule unless there is a real need: LilSkystorm / lunasenpai waited 10 min in
//     geffen for mjo_dun02 and then walked anyway). 1.3% of all fighter time was "waiting in X for the Kafra".
//     Now: an open Kafra plan is taken at once; a wait that cannot end in a teleport is not started (a solo bot
//     picks a hunting ground it can reach, a party member walks as it would after the wait); a useful wait is capped
//     at kafra_wait_cap_sec (60), then a Butterfly Wing to the save town when that is a better start, else a solo
//     bot picks another ground. Quiet estimates and virtual plans no longer raise kafraHardUntil.
//  2. No-route ping-pong: 5 solo bots spent the whole 5 minutes in "no route to iz_dun00: leaving prontera" /
//     "leaving prt_fild08" (a portal they gave up on is skipped by routeFor; escapeNoRoute walks out of the nearest
//     door, which on the next map leads straight back). After 90 s without a route to the same goal a solo bot drops
//     that goal for 20 min and picks another ground. 0.6% of fighter time ("stuck").
//  3. The hourly Kafra cap ("3 teleports in the last hour already, walking") sent bots on 5-10 min cross-region walks
//     (Mint_cat gef_fild01 -> prontera -> pay_fild04, 8+ min). EC04: long trips go by Kafra. The cap no longer holds
//     a walk of kafra_long_walk_cells (900) or more; the A -> B -> A blocks (PR33) stay.
//
// tuning farm_share_off = false (ON by default); kafra_wait_cap_sec, kafra_long_walk_cells (0 = defaults).

import (
	"fmt"
	"sync"
	"time"
)

func farmShareOn() bool { return !tune().FarmShareOff }

func kafraWaitCap() time.Duration {
	s := tune().KafraWaitCapSec
	if s <= 0 {
		s = 60
	}
	return time.Duration(s) * time.Second
}

// kafraMemberWaitOn: the party member's Kafra-wait rule (kafraMemberWait); tuning kafra_member_wait_off switches it off.
func kafraMemberWaitOn() bool { return farmShareOn() && !tune().KafraMemberWaitOff }

func kafraMemberWaitCap() time.Duration {
	s := tune().KafraMemberWaitCapSec
	if s <= 0 {
		s = 180
	}
	return time.Duration(s) * time.Second
}

func kafraLongWalkCells() int {
	c := tune().KafraLongWalkCells
	if c <= 0 {
		c = 900
	}
	return c
}

// kafraLongWalk (b.mu held): the walk from where we (virtually) stand to goal is long enough that the hourly Kafra cap
// must not make us walk it (EC04). An unknown walk (no route: 99999) is not "long" - mapHops handles that case.
func (b *Bot) kafraLongWalk(goal string) bool {
	if !farmShareOn() || goal == "" || goal == b.MapName {
		return false
	}
	return longWalk(walkCells(b.MapName, b.X, b.Y, goal), kafraLongWalkCells())
}

func longWalk(cells, limit int) bool { return cells >= limit && cells < 99999 }

// ---------------------------------------------------------------- per-goal clocks

// goalClock: how long one bot has been in some condition toward the same goal; a break longer than gap restarts it.
type goalClock struct {
	goal       string
	since      time.Time
	seen       time.Time
	wingedGoal string // kafra wait: already flew home with a Butterfly Wing for this goal
	saidGoal   string // kafra wait (member): the party was told about the wait for this goal
	movedGoal  string // kafra wait (leader): the party was moved for this goal
}

func (c *goalClock) step(goal string, now time.Time, gap time.Duration) time.Duration {
	if c.goal != goal || c.seen.IsZero() || now.Sub(c.seen) > gap {
		c.goal, c.since = goal, now
	}
	c.seen = now
	return now.Sub(c.since)
}

var kafraWaitClocks, noRouteClocks sync.Map // *Bot -> *goalClock

func clockOf(m *sync.Map, b *Bot) *goalClock {
	v, _ := m.LoadOrStore(b, &goalClock{})
	return v.(*goalClock)
}

// ---------------------------------------------------------------- goals given up (the map pickers skip them)

var walkBans sync.Map // *Bot -> map[string]time.Time (b.mu held for the bot's own map)

const walkBanFor = 20 * time.Minute

func (b *Bot) walkBan(m string, now time.Time) {
	v, _ := walkBans.LoadOrStore(b, map[string]time.Time{})
	bans := v.(map[string]time.Time)
	for k, exp := range bans {
		if now.After(exp) {
			delete(bans, k)
		}
	}
	bans[m] = now.Add(walkBanFor)
}

// walkBanned (b.mu held; deadlyFor): we gave m up lately because we could not get there. Never the map we stand on.
func (b *Bot) walkBanned(m string) bool {
	if !farmShareOn() || m == "" || m == b.MapName {
		return false
	}
	v, ok := walkBans.Load(b)
	if !ok {
		return false
	}
	exp, ok := v.(map[string]time.Time)[m]
	return ok && time.Now().Before(exp)
}

// soloGoal (b.mu held): our goal is our own choice (no party, no API order, no town run): we may pick another one.
func (b *Bot) soloGoal() bool {
	if b.trip != nil || b.Goal.Mode != "hunt" || b.Goal.Set != "auto" && b.Goal.Set != "" {
		return false
	}
	return partyOf(b) == nil
}

// giveUpGoal (b.mu held): drop the goal map for walkBanFor and let the picker choose another hunting ground.
func (b *Bot) giveUpGoal(why string, now time.Time) {
	g := b.Goal.Map
	b.walkBan(g, now)
	b.Goal, b.route, b.path, b.viaTown = Goal{}, nil, nil, ""
	b.parkEvent(fmt.Sprintf("giving up on %s (%s): looking for a hunting ground I can reach", g, why))
}

// ---------------------------------------------------------------- 1. Kafra waits

type kafraWaitIn struct {
	useful bool          // a Kafra plan exists once the hard limit is ignored (the wait can end in a teleport)
	solo   bool          // soloGoal
	waited time.Duration // on this goal
	cap    time.Duration
	wing   bool // a usable Butterfly Wing, the save town a better start, not used yet for this goal
}

// kafraWaitPlan: "wait", "walk" (no wait: walk as after the wait), "repick" (give the goal up), "wing" (fly home).
func kafraWaitPlan(in kafraWaitIn) string {
	if !in.useful {
		if in.solo {
			return "repick"
		}
		return "walk"
	}
	if in.waited < in.cap {
		return "wait"
	}
	if in.wing {
		return "wing"
	}
	if in.solo {
		return "repick"
	}
	return "wait" // a party member: the party logic (rallypark park cap, party follow) owns the longer wait
}

// kafraWaitUseful (b.mu held): would a Kafra teleport toward goal be taken if the hard limit were lifted now? When not,
// waiting the limit out is pointless (the soft rules or the fare still say no afterwards).
func (b *Bot) kafraWaitUseful(goal string, now time.Time) bool {
	b.kafraNoHard = true
	k := b.kafraPlan(goal, now)
	b.kafraNoHard = false
	return k != nil
}

// kafraWaitSkip (b.mu held; kafraWaitInTown, the walk to goal unsafe and the hard limit on): true = don't wait (a
// Kafra trip was set up, the goal was given up, or walking is what would happen anyway).
func (b *Bot) kafraWaitSkip(goal string, now time.Time) bool {
	if !farmShareOn() {
		return false
	}
	if k := b.kafraPlan(goal, now); k != nil { // another destination is open: go now
		b.kafra = k
		b.parkEvent(fmt.Sprintf("taking the Kafra from %s to %s (heading to %s)", k.Town, k.Dest.Name, goal))
		return true
	}
	c := clockOf(&kafraWaitClocks, b)
	in := kafraWaitIn{useful: b.kafraWaitUseful(goal, now), solo: b.soloGoal(), cap: kafraWaitCap()}
	in.waited = c.step(goal, now, 30*time.Second)
	if !in.solo && in.waited >= in.cap && kafraMemberWaitOn() {
		return b.kafraMemberWait(goal, now, c, in)
	}
	if in.waited >= in.cap {
		in.wing = c.wingedGoal != goal && b.savedTown != "" && b.savedTown != b.MapName &&
			townHops(b.savedTown, goal) < townHops(b.MapName, goal) && b.escItemOK(itemButterflyWing, now)
	}
	switch kafraWaitPlan(in) {
	case "walk":
		return true
	case "repick":
		why := "the walk is too dangerous and the Kafra won't take me there"
		if in.useful {
			why = fmt.Sprintf("waited %ds for the Kafra", int(in.waited.Seconds()))
		}
		b.giveUpGoal(why, now)
		return true
	case "wing":
		c.wingedGoal = goal
		b.parkEvent(fmt.Sprintf("waited %ds in %s for the Kafra toward %s: Butterfly Wing to %s", int(in.waited.Seconds()), b.MapName, goal, b.savedTown))
		b.useEscapeItem(itemButterflyWing, now, "kafra wait "+b.MapName+" -> "+goal)
	}
	return false
}

// memberWaitIn: a party member's state once it has waited the cap at the Kafra (kafraMemberWait).
type memberWaitIn struct {
	useful    bool          // a Kafra plan exists once the hard limit is ignored
	waited    time.Duration // on this goal
	cap       time.Duration // kafra_wait_cap_sec
	memberCap time.Duration // kafra_member_wait_cap_sec: after it a stuck leader moves the party
	safe      bool          // the walk to the goal is safe for our level (kafraWaitInTown already rated it unsafe)
	wing      bool          // a usable Butterfly Wing and a better save town, not used for this goal yet
	buy       bool          // a wing can be bought here, it helps (save town better), none bought for this goal yet
	leader    bool          // we lead the party
	said      bool          // the party was already told about the wait for this goal
	moved     bool          // a leader already moved the party for this goal
}

// memberWaitPlan: "walk" (no wait: the Kafra can't help, or the route is safe), "wait" (keep sitting), "say" (ask the
// party to collect us, once), "wing" (fly to the save town with a wing we carry), "buy" (buy wings here, once per goal),
// "relocate" (a stuck leader moves the whole party to a reachable map after memberCap). A party member never gives up
// its goal: it keeps waiting at the Kafra until the Kafra opens (the hard limit) or the party collects it (PT12).
func memberWaitPlan(in memberWaitIn) string {
	if !in.useful {
		return "walk"
	}
	if in.waited < in.cap {
		return "wait"
	}
	switch {
	case in.wing:
		return "wing"
	case in.buy:
		return "buy"
	case in.safe:
		return "walk"
	case in.leader && in.waited >= in.memberCap && !in.moved:
		return "relocate"
	case !in.said:
		return "say"
	}
	return "wait"
}

// test hooks: the real actions in production, replaced by the tests
var (
	memberBuyTrip    = func(b *Bot, why string, now time.Time) { b.startTrip(why, now) }
	leaderRelocate   = func(p *Party, from string, x, y int, why string) { p.relocate(from, x, y, why) }
	wingSoldHere     = func(m string) bool { return potionShop(m) != nil }
	savedStartBetter = func(b *Bot, goal string) bool {
		return b.savedTown != "" && b.savedTown != b.MapName && townHops(b.savedTown, goal) < townHops(b.MapName, goal)
	}
)

// kafraBuys: Butterfly Wing buys per goal for one bot (*Bot -> map[goal]int). Not cleared by the goal clock: a trip
// out of town resets the wait clock, and a failed buy must not start the trip again. Used under b.mu.
var kafraBuys sync.Map

func wingBuysFor(b *Bot, goal string) int {
	v, ok := kafraBuys.Load(b)
	if !ok {
		return 0
	}
	return v.(map[string]int)[goal]
}

func markWingBuy(b *Bot, goal string) {
	v, _ := kafraBuys.LoadOrStore(b, map[string]int{})
	v.(map[string]int)[goal]++
}

// kafraMemberWait (b.mu held; kafraWaitSkip for a party member past the cap): acts on memberWaitPlan and returns
// true when the bot should not wait. Never gives the goal up: a stuck member keeps its party's map. Only a bot-led
// party's leader relocates it; a human-led party (Ayla's friends) is never moved by its bot members.
func (b *Bot) kafraMemberWait(goal string, now time.Time, c *goalClock, in kafraWaitIn) bool {
	p := partyOf(b)
	mi := memberWaitIn{
		useful:    in.useful,
		waited:    in.waited,
		cap:       in.cap,
		memberCap: kafraMemberWaitCap(),
		safe:      b.routeSafeFrom(b.MapName, b.X, b.Y, goal),
		leader:    p != nil && p.Leader == b && p.Human == "",
		said:      c.saidGoal == goal,
		moved:     c.movedGoal == goal,
		wing:      c.wingedGoal != goal && savedStartBetter(b, goal) && b.escItemOK(itemButterflyWing, now),
		buy:       b.canBuyWingHere(goal, now),
	}
	switch memberWaitPlan(mi) {
	case "walk":
		return true
	case "wing":
		c.wingedGoal = goal
		b.parkEvent(fmt.Sprintf("waited %ds in %s for the Kafra toward %s: Butterfly Wing to %s", int(in.waited.Seconds()), b.MapName, goal, b.savedTown))
		if p != nil { // PT12: a member that leaves the party's area says so
			b.partySay(styled(b.P.Typing, pick(
				"flying to "+b.savedTown+" with a wing, the walk to "+goal+" is too rough, meet me there",
				"taking a wing back to "+b.savedTown+", the way to "+goal+" is too dangerous for me")))
		}
		b.useEscapeItem(itemButterflyWing, now, "kafra wait "+b.MapName+" -> "+goal)
	case "buy":
		markWingBuy(b, goal)
		b.parkEvent(fmt.Sprintf("waited %ds in %s for the Kafra toward %s: buying Butterfly Wings for the walk", int(in.waited.Seconds()), b.MapName, goal))
		memberBuyTrip(b, "buying a Butterfly Wing for the Kafra walk", now)
		return true
	case "relocate":
		c.movedGoal = goal
		b.parkEvent(fmt.Sprintf("parked %ds in %s for the Kafra toward %s, the walk is too dangerous: moving the party", int(in.waited.Seconds()), b.MapName, goal))
		go leaderRelocate(p, b.MapName, b.X, b.Y, "kafra closed, the walk to "+goal+" is too dangerous")
	case "say":
		c.saidGoal = goal
		b.parkEvent(fmt.Sprintf("waiting in %s for the Kafra toward %s: the walk is too dangerous, asking the party to collect me", b.MapName, goal))
		if p != nil {
			b.partySay(styled(b.P.Typing, pick(
				"the kafra is closed and the walk to "+goal+" is too rough for me, can someone come get me?",
				"stuck at the kafra here, the way to "+goal+" is too dangerous, come get me when you can",
				"waiting at the kafra in "+b.MapName+", the walk to "+goal+" is too rough, come collect me")))
		}
	}
	return false
}

// canBuyWingHere (b.mu held): we stand in a town whose tool dealer sells Butterfly Wings, carry none, the save town
// is a better start toward goal (else a wing would go unused), the zeny covers one (bwWant), we are not on a trip, and
// we have not bought for this goal yet.
func (b *Bot) canBuyWingHere(goal string, now time.Time) bool {
	if b.trip != nil || b.countItem(itemButterflyWing) > 0 || !wingSoldHere(b.MapName) || !savedStartBetter(b, goal) {
		return false
	}
	return wingBuysFor(b, goal) == 0 && b.bwWant() > 0
}

// ---------------------------------------------------------------- 2. no-route ping-pong

const noRouteGiveUpAfter = 90 * time.Second

// noRouteGiveUp (b.mu held; travel, no route to the goal): a solo bot that found no route to the same goal for 90 s
// (walking out of doors that lead back, flying home) gives the goal up.
func (b *Bot) noRouteGiveUp(now time.Time) bool {
	if !farmShareOn() || b.Goal.Map == "" {
		return false
	}
	waited := clockOf(&noRouteClocks, b).step(b.Goal.Map, now, time.Minute)
	if waited < noRouteGiveUpAfter || !b.soloGoal() {
		return false
	}
	b.giveUpGoal(fmt.Sprintf("no way there from %s for %ds", b.MapName, int(waited.Seconds())), now)
	return true
}
