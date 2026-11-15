package main

import (
	"fmt"
	"log"
	"strings"
	"time"
)

// rallysync (live 2026-11-15: party-apart x68): Ayla's plugin and the engine ran separate rally clocks - she walked to
// the Payon Kafra while the members' own rally timed out and they left (priest 116 cells away). Now a human-led
// party's rally follows HER rally state from status.json (rally_town / rally_x / rally_y): while she rallies (or walks
// to her meeting Kafra) the members wait at that Kafra with no timeout of their own; when her rally ends (she sets
// off) the mirrored rally ends and the members follow her (humanPartySync).
//
// One rally per call (live 2026-11-15 09:02-09:07: the mirror restarted 15 times, every ~22 s, while she led them into
// pay_dun02 and her status still said payon). A mirrored rally ends only by arrival, by her cancel (rally_town cleared)
// or by rallyMirrorTimeout. Whatever ends it records the call's key in MirrorDone, so the same call is never mirrored
// again; the key is cleared when her rally is over, and a new call starts a new rally.

const rallyMirrorWhy = "mirror: her rally"

// rallyMirrorKey names one of her calls: its town and meeting cell.
func rallyMirrorKey(town string, x, y int) string {
	return fmt.Sprintf("%s@%d,%d", town, x, y)
}

// humanRallySync: true when her fresh status decided the party's rally this tick (no own timeouts then)
func humanRallySync(p *Party, now time.Time) bool {
	st, ok := humanStatusOf(p.Human)
	if !ok || now.Sub(time.Unix(st.TS, 0)) > 15*time.Second {
		return false
	}
	town := strings.ToLower(strings.TrimSpace(st.RallyTown))
	parties.Lock()
	defer parties.Unlock()
	if town == "" {
		p.Rally.MirrorDone = ""                                  // her rally is over: the next call is a new rally
		if p.Rally.Town != "" && p.Rally.Why == rallyMirrorWhy { // her cancel: the members follow her
			old := p.Rally.Town
			p.Rally.Town, p.Rally.AllAt, p.Rally.SplitSince = "", time.Time{}, time.Time{}
			p.Rally.Last, p.Rally.LastAt = p.Human+" set off", now
			log.Printf("party %s: %s ended the rally in %s - members follow her", p.Name, p.Human, old)
			return true
		}
		return false
	}
	if p.Rally.MirrorDone == rallyMirrorKey(town, st.RallyX, st.RallyY) {
		return false // this call's rally already ended (arrival, timeout, no progress): not restarted
	}
	if p.Rally.Town != town || p.Rally.X != st.RallyX || p.Rally.Y != st.RallyY {
		p.Rally.Town, p.Rally.X, p.Rally.Y, p.Rally.Save = town, st.RallyX, st.RallyY, false
		p.Rally.Dest, p.Rally.Why, p.Rally.At = st.Dest, rallyMirrorWhy, now
		p.Rally.AllAt, p.Rally.SplitSince = time.Time{}, time.Time{}
		p.Rally.best, p.Rally.bestAt, p.Rally.excluded, p.Rally.lastPos = 1<<30, now, map[string]bool{}, nil
		log.Printf("party %s: %s rallies at the %s Kafra (%d,%d) - members wait there", p.Name, p.Human, town, st.RallyX, st.RallyY)
	}
	return true
}
