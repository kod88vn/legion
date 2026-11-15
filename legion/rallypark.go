package main

// Rally / Kafra-wait park guard (fix/rally-sitstand, 2026-11-14).
//
// Seen live: Ayla's Wizard Zen.fox (legion-core), on a party rally to Yuno, stood in Geffen sitting down and standing
// up every 1-4 s at full SP for minutes. rallyThink (rally.go, member not in the rally town) stood it up every second
// ("seated: the server ignores walking"), then called travel, whose kafraWaitInTown (kafra.go: Kafra teleport refused
// by the hard limit, the walk crosses maps too strong for us) sat it down again 3 s after the last sit/stand. Two
// state machines owned the same sit. Afterwards it parked in Aldebaran ("the walk to izlude is too dangerous", chasing
// a town Ayla only passed through), away from the party, idle.
//
// The guard (tuning rally_park_off = false, i.e. ON by default; rally_park_cap_min, default 12 = Ayla's own rally
// timeout in partylead.pl):
//  1. one owner of the waiting sit: kafraWaitInTown sits through waitSit, and the generic stand-ups (rallyThink,
//     hunt's "something left us seated") leave a sit alone that a waiter claimed in the last 2 s;
//  2. a rally leg the member cannot travel (Kafra refused + the walk too dangerous) is not walked or waited out: a
//     Butterfly Wing to a save town nearer the rally town if we carry one, else the member leaves the rally (the party
//     doesn't wait for it), says so once, and waits in its safe town; the party follow (humanPartySync / party.go)
//     takes it to the party's map once the Kafra opens again;
//  3. a park detector: a bot held by kafraWaitInTown toward the same goal for the cap logs it and gives that goal up
//     (a Butterfly Wing to a nearer save town if it has one; a human-led party member switches to the party's farm
//     map instead of the town the leader crossed - humanPartySync parkRedirect; a solo bot picks another ground), and
//     a member on a rally for longer than the cap leaves it.

import (
	"fmt"
	"sync"
	"time"
)

func rallyParkOn() bool { return !tune().RallyParkOff }

func rallyParkCap() time.Duration {
	m := tune().RallyParkCapMin
	if m <= 0 {
		m = 12
	}
	return time.Duration(m) * time.Minute
}

// ---------------------------------------------------------------- 1. one owner of the waiting sit

var waitSits sync.Map // *Bot -> time.Time: last tick a waiter (Kafra wait) wanted us seated

const waitSitHold = 2 * time.Second

// waitSitDue: a waiter should send a sit now (not seated, not walking, and no sit/stand in the last 3 s).
func waitSitDue(sitting, moving bool, lastSitStand, now time.Time) bool {
	return !sitting && !moving && now.Sub(lastSitStand) > 3*time.Second
}

// waitSit (b.mu held): the waiter's sit, claimed for waitSitHold so nobody else stands us up meanwhile.
func (b *Bot) waitSit(moving bool, now time.Time) {
	waitSits.Store(b, now)
	if waitSitDue(b.Sitting, moving, b.sitSentAt, now) {
		b.sitSentAt = now
		go b.sit()
	}
}

// waitSitHeld (b.mu held): a waiter claimed our sit within waitSitHold - other logic must not stand us up.
func (b *Bot) waitSitHeld(now time.Time) bool {
	if !rallyParkOn() {
		return false
	}
	v, ok := waitSits.Load(b)
	return ok && now.Sub(v.(time.Time)) < waitSitHold
}

// kafraWaitApplies (b.mu held): kafraWaitInTown's test without its side effects - we are in a Kafra town, the Kafra
// teleport is refused by the hard limit, and the walking way to goal is too dangerous.
func (b *Bot) kafraWaitApplies(goal string, now time.Time) bool {
	if goal == "" || goal == b.MapName || !isTown(b.MapName) || len(kafraSpots[b.MapName]) == 0 || !now.Before(b.kafraHardUntil) {
		return false
	}
	return !b.routeSafeFrom(b.MapName, b.X, b.Y, goal)
}

// ---------------------------------------------------------------- 2. a rally leg we cannot travel

type rallyLegIn struct {
	sinceRally time.Duration // since the rally started
	cap        time.Duration
	blocked    bool   // Kafra refused and the walk too dangerous
	here, town string // our map, the rally town
	saved      string // our save town
	hopsHere   int    // map changes from here to the rally town
	hopsSaved  int    // ... from the save town
	wing       bool   // we carry a usable Butterfly Wing
	wingUsed   bool   // already winged for this rally
}

// rallyLegPlan: "" (travel as usual), "giveup" (rally over the cap for us), "wing" (Butterfly Wing to the nearer
// save town), "out" (leave the rally, wait here for the party).
func rallyLegPlan(in rallyLegIn) string {
	if in.cap > 0 && in.sinceRally > in.cap {
		return "giveup"
	}
	if !in.blocked {
		return ""
	}
	if in.wing && !in.wingUsed && in.saved != "" && in.saved != in.here && in.hopsSaved < in.hopsHere {
		return "wing"
	}
	return "out"
}

var rallyWinged sync.Map // *Bot -> time.Time (the rally's start) we already winged for

// rallyExclude: the member is left out of the running rally; the rally ends without it.
func (p *Party) rallyExclude(name string) {
	parties.Lock()
	if p.Rally.Town != "" {
		if p.Rally.excluded == nil {
			p.Rally.excluded = map[string]bool{}
		}
		p.Rally.excluded[name] = true
	}
	parties.Unlock()
}

// rallyLeg (b.mu held; rallyThink, member not in the rally town): done = rallyLeg handled this tick, own = the rally
// keeps owning the bot (rallyThink returns it).
func (b *Bot) rallyLeg(p *Party, r rallyState, now time.Time) (done, own bool) {
	if !rallyParkOn() || p == nil {
		return false, false
	}
	in := rallyLegIn{sinceRally: now.Sub(r.At), cap: rallyParkCap(), here: b.MapName, town: r.Town, saved: b.savedTown}
	in.blocked = townOf(b.MapName) != r.Town && b.kafra == nil && b.viaTown == "" && b.kafraWaitApplies(r.Town, now)
	if in.blocked {
		in.hopsHere, in.hopsSaved = townHops(b.MapName, r.Town), townHops(b.savedTown, r.Town)
		in.wing = b.escItemOK(itemButterflyWing, now)
		if v, ok := rallyWinged.Load(b); ok && v.(time.Time).Equal(r.At) {
			in.wingUsed = true
		}
	}
	switch rallyLegPlan(in) {
	case "giveup":
		p.rallyExclude(b.charName())
		b.parkEvent(fmt.Sprintf("rally: still not in %s after %d min, giving it up (state %q)", r.Town, int(in.sinceRally.Minutes()), b.State))
		return true, false
	case "wing":
		rallyWinged.Store(b, r.At)
		b.parkEvent(fmt.Sprintf("rally: the walk from %s to %s is too dangerous and the Kafra is closed: Butterfly Wing to %s", b.MapName, r.Town, b.savedTown))
		b.useEscapeItem(itemButterflyWing, now, "rally to "+r.Town+": walk too dangerous, Kafra closed")
		return true, true
	case "out":
		p.rallyExclude(b.charName())
		b.parkEvent(fmt.Sprintf("rally: the walk from %s to %s is too dangerous and the Kafra is closed for %ds: waiting here for the party",
			b.MapName, r.Town, int(b.kafraHardUntil.Sub(now).Seconds())))
		if b.buffAt != nil && now.Sub(b.buffAt["rallyout"]) > 10*time.Minute {
			b.buffAt["rallyout"] = now
			here, town := b.MapName, r.Town
			go b.partySay(pick("cant get to "+town+" safely, ill wait in "+here, "too dangerous for me to walk to "+town+", waiting in "+here,
				"ill stay in "+here+" for now, cant make it to "+town))
		}
		return true, false
	}
	return false, false
}

// parkEvent: an info event (skipped for test bots without a character).
func (b *Bot) parkEvent(text string) {
	if b.buffAt == nil {
		return
	}
	b.event("info", "", text)
}

// ---------------------------------------------------------------- 3. park detector

type parkRec struct {
	goal   string
	since  time.Time
	seen   time.Time
	gaveUp string // the goal we gave up (parkRedirect)
	gaveAt time.Time
}

var parks sync.Map // *Bot -> *parkRec

// parkStep: one kafraWaitInTown tick toward goal; true exactly once, when the park reaches the cap. A break of 30 s or
// a new goal starts the clock again.
func parkStep(r *parkRec, goal string, now time.Time, cap time.Duration) bool {
	if r.goal != goal || now.Sub(r.seen) > 30*time.Second {
		r.goal, r.since = goal, now
	}
	r.seen = now
	if cap <= 0 || now.Sub(r.since) < cap || (r.gaveUp == goal && now.Sub(r.gaveAt) < cap) {
		return false
	}
	r.gaveUp, r.gaveAt = goal, now
	return true
}

// parkTick (b.mu held; kafraWaitInTown while it holds us): at the cap, log and give the goal up.
func (b *Bot) parkTick(goal string, now time.Time) {
	if !rallyParkOn() {
		return
	}
	v, _ := parks.LoadOrStore(b, &parkRec{})
	r := v.(*parkRec)
	if !parkStep(r, goal, now, rallyParkCap()) {
		return
	}
	b.parkEvent(fmt.Sprintf("parked in %s for %d min waiting to go to %s (walk too dangerous, Kafra closed): giving it up",
		b.MapName, int(now.Sub(r.since).Minutes()), goal))
	if b.savedTown != "" && b.savedTown != b.MapName && townHops(b.savedTown, goal) < townHops(b.MapName, goal) &&
		b.escItemOK(itemButterflyWing, now) {
		b.useEscapeItem(itemButterflyWing, now, "parked "+b.MapName+" -> "+goal)
		return
	}
	if partyOf(b) == nil && b.Goal.Set != "api" && b.Goal.Map == goal {
		b.Goal, b.route, b.path = Goal{}, nil, nil // pick another hunting ground
	}
}

// parkRedirect (b.mu held; humanPartySync): m is the map a human-led party member heads for. When we gave m - a town
// the leader passed through - up after parking for the cap, head for the party's farm map instead (where she hunts).
func (b *Bot) parkRedirect(p *Party, m string, now time.Time) string {
	if !rallyParkOn() || p == nil || !isTown(m) {
		return m
	}
	v, ok := parks.Load(b)
	if !ok {
		return m
	}
	r := v.(*parkRec)
	if r.gaveUp != m || now.Sub(r.gaveAt) > 15*time.Minute {
		return m
	}
	farm := ""
	if p.Human != "" {
		if st, ok := humanStatusOf(p.Human); ok && st.LockMap != "" {
			farm = st.LockMap
		}
	}
	if farm == "" {
		farm = p.rallyDest()
	}
	return parkFarm(m, farm)
}

// parkFarm: the farm map replaces the given-up town when it is a real hunting map.
func parkFarm(m, farm string) string {
	if farm == "" || farm == m || isTown(farm) || townOf(farm) != "" {
		return m
	}
	return farm
}
