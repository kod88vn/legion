package main

// Travel share (fix/travel-share, bug b1794709221xoc, 2026-11-15). Sentinel: farming share 61% < 85% over 919 bots,
// travelling 26% (14% earlier in the night). Measured live 12:07-12:17Z (10 snapshots of ~970 online bots, 17k
// events): 251 bots travelling per snapshot, 67% of them solo bots walking to an "auto" goal map, 16% on a town run,
// 14% solo via a Kafra; party travel ~3%. Solo bots changed their auto goal 336 times in 9 minutes (one change per
// bot every ~25 min, against PT16's ~2 h on a map). What started the travel legs that followed a goal change:
//
//  1. Roam give-up map hopping (113 changes plus ~55 silent ones while fighting/looting = 51% of the attributed travel):
//     a solo bot leaves its map after roam_give_up_sec (120 s) of roaming, roam_crowd_sec (75 s) on a "crowded" map
//     or 35% roaming in a 3-min window - even while it is killing well: the 116 bots that hopped had a median
//     3.25 kills/min on the map they left in the minutes before (bots that stayed: 4.64), 85% of them >= 1 kill/min.
//     They ping-ponged mjolnir_07 <-> mjolnir_08 <-> yuno_fild01 (32 + 25 + 14 moves). Real players don't leave a
//     map that pays a kill a minute because of a two-minute dry patch (PT16, EC04).
//     NEW BEHAVIOUR, staged OFF: before a solo bot gives its map up for roaming, it counts its kills on this map in
//     the last roam_stay_window_sec (300). At roam_stay_kpm (1.0) or more it stays and keeps exploring (the roam
//     clocks restart); a map that reads empty (no monster in sight 60 s) with no kill in the last 90 s is still left.
//     Turned on by roam_stay_on (tuning) only, by the owner's word.
//  2. Kafra-wait give-up loop (47 changes, 12%; 35 bots, 249 events in 10 min, Lily_heart 111x in 2 min "giving up
//     on pay_fild10 (the walk is too dangerous ...)"): giveUpGoal walk-bans the map, but the SpotsFor fallback picker
//     (15% of picks) never asked walkBanned, so it picked the banned map again at once. Now it skips banned maps.
//  3. The Byalan boat loop (9 bots, 8,506 "can't pay the boat at izlude (150z, have 76)" events in 10 min - the event
//     ring overflowed in 2 min): the picker offered iz_dun* to bots with less than the 150z fare; ferryStep gave up,
//     re-planned the same boat every tick. Now the pickers skip a map whose route needs a fare the bot can't pay, and
//     a solo bot at the Sailor without the fare gives the goal up (walk ban) instead of retrying every tick.
//
// Items 2 and 3 are fixes of stuck loops in shared engine code: ON by default (travel_share_off true switches them
// off). Item 1 is new behaviour: OFF by default (roam_stay_on true switches it on).
//
// Also measured (not changed here): town runs that end in a re-pick after resting in town (40 changes, 8%), "too
// dangerous" escapes (37, 10%), boss flee (12, 6%).
//
// tuning: travel_share_off (false = fixes 2 and 3 on); roam_stay_on (false = item 1 off), roam_stay_kpm
// (0 = 1.0), roam_stay_window_sec (0 = 300).

import (
	"fmt"
	"math"
	"sync"
	"time"
)

// travelShareOn: the loop fixes (items 2 and 3) run unless travel_share_off is true.
func travelShareOn() bool { return !tune().TravelShareOff }

// roamStayOn: the new stay-on-a-paying-map rule (item 1) runs only when roam_stay_on is true. Default off.
func roamStayOn() bool { return tune().RoamStayOn }

func roamStayKpm() float64 {
	if k := tune().RoamStayKpm; k > 0 {
		return k
	}
	return 1.0
}

func roamStayWindow() time.Duration {
	if s := tune().RoamStayWindowSec; s > 0 {
		return time.Duration(s) * time.Second
	}
	return 5 * time.Minute
}

// ---------------------------------------------------------------- kills on the current map

const mapKillCap = 256 // kill times kept per bot (a fast Hunter makes ~10/min: well over the window)

type mapKillLog struct {
	m  string
	at []time.Time
}

var mapKillLogs sync.Map // *Bot -> *mapKillLog (b.mu held)

// noteMapKill (b.mu held; bot.go, our target died): one kill on the map we stand on. Recorded only while the
// roam-stay rule is on, so with it off nothing is kept.
func (b *Bot) noteMapKill(now time.Time) {
	if !roamStayOn() {
		return
	}
	v, _ := mapKillLogs.LoadOrStore(b, &mapKillLog{})
	l := v.(*mapKillLog)
	if l.m != b.MapName {
		l.m, l.at = b.MapName, l.at[:0]
	}
	l.at = append(l.at, now)
	if len(l.at) > mapKillCap {
		l.at = append(l.at[:0], l.at[len(l.at)-mapKillCap:]...)
	}
}

// mapKillsSince (b.mu held): our kills on the map we stand on within d before now, and the time of the last one.
func (b *Bot) mapKillsSince(d time.Duration, now time.Time) (int, time.Time) {
	v, ok := mapKillLogs.Load(b)
	if !ok {
		return 0, time.Time{}
	}
	l := v.(*mapKillLog)
	if l.m != b.MapName || len(l.at) == 0 {
		return 0, time.Time{}
	}
	n := 0
	for i := len(l.at) - 1; i >= 0 && now.Sub(l.at[i]) <= d; i-- {
		n++
	}
	return n, l.at[len(l.at)-1]
}

// ---------------------------------------------------------------- 1. stay on a map that still pays (new, off)

type roamStayIn struct {
	kills       int           // kills on this map in the window
	window      time.Duration // roam_stay_window_sec
	kpm         float64       // roam_stay_kpm
	seesNothing bool          // no monster at all in view for 60 s (seen.go)
	sinceKill   time.Duration // since the last kill on this map (large when none)
}

// roamStayDecision: true = keep hunting this map instead of giving it up for roaming.
func roamStayDecision(in roamStayIn) bool {
	if in.seesNothing && in.sinceKill > 90*time.Second {
		return false // the map reads empty and we aren't killing: move on
	}
	need := int(math.Ceil(in.kpm * in.window.Minutes()))
	return need > 0 && in.kills >= need
}

// roamStay (b.mu held; ai.go hunt, a solo auto goal about to be given up for roaming): stay when this map still pays
// a kill a minute; the roam clocks restart so the next check is a full give-up period away. Off unless roam_stay_on.
func (b *Bot) roamStay(now time.Time) bool {
	if !roamStayOn() {
		return false
	}
	w := roamStayWindow()
	n, last := b.mapKillsSince(w, now)
	since := time.Duration(math.MaxInt64)
	if !last.IsZero() {
		since = now.Sub(last)
	}
	if !roamStayDecision(roamStayIn{kills: n, window: w, kpm: roamStayKpm(), seesNothing: b.seesNothing(now), sinceKill: since}) {
		return false
	}
	b.roamSince, b.roamWin, b.roamAcc = now, now, 0
	econCount("travel share: stayed on a map that still pays")
	return true
}

// ---------------------------------------------------------------- 2./3. pickers: banned maps, unpayable boats

// routeFare: zeny the boats on route r charge.
func routeFare(r []Warp) int {
	f := 0
	for _, w := range r {
		f += w.Fare
	}
	return f
}

// fareShort (b.mu held; map pickers): route r needs a boat fare we can't pay.
func (b *Bot) fareShort(r []Warp) bool {
	return travelShareOn() && routeFare(r) > b.Zeny
}

// pickSkip (b.mu held; SpotsFor picker): a map given up lately (walk ban) - the other pickers skip it via deadlyFor.
func (b *Bot) pickSkip(m string) bool {
	return travelShareOn() && b.walkBanned(m)
}

var ferryBrokeAt sync.Map // *Bot -> time.Time of the last "can't pay" event

// ferryBroke (b.mu held; ferryStep, not enough zeny for the boat): a solo bot gives the goal up (walk ban, the
// pickers skip it); anyone else re-plans as before, with the event at most once a minute. true = handled.
func (b *Bot) ferryBroke(st *ferryState, now time.Time) bool {
	if !travelShareOn() {
		return false
	}
	why := fmt.Sprintf("can't pay the boat at %s (%dz, have %d)", st.w.From, st.w.Fare, b.Zeny)
	if b.soloGoal() && b.Goal.Map != "" && b.Goal.Map != b.MapName {
		ferryStates.Delete(b)
		b.giveUpGoal(why, now)
		return true
	}
	if v, ok := ferryBrokeAt.Load(b); ok && now.Sub(v.(time.Time)) < time.Minute {
		ferryStates.Delete(b)
		b.route, b.path = nil, nil
		return true // quiet re-plan
	}
	ferryBrokeAt.Store(b, now)
	return false
}
