package main

import "time"

// Shop-door "warp loops" (QA 2026-11-14 12:25-12:34 UTC: 141 "warp loop X <-> X_in" events in 9 min; hotspots alberta
// 98,153, alberta 117,38, prontera 179,184 / 177,221, geffen 43,85 / 98,141). Traced bots did NOT step back onto the door
// by accident: every entry was a deliberate walk into a door. Two causes:
//  1. A trip planned from outside the town (travel() to the shop's map, or a party/refine trip) routed to the NEAREST door
//     of the interior map (alberta Room_Entrance 45,145, prontera's west door 50,228 -> prt_in 128,40) and bot.go kept
//     that route across the map change, so the in-town check "route ends on the shop's map" accepted it: wrong room,
//     out, then the right door - X, X_in, X, X_in within 2 minutes.
//  2. The detector flagged any A, B, A, B, even through two DIFFERENT doors of one interior map (two shops, a wrong room
//     then the right one), and with both legs intended it banned the shop door for everyone for 30 minutes.
// Fix: in-town trips accept a route only if it ends in the target's ROOM (routeEndsIn); a loop is the same door taken
// twice (warpLoopLegs); a town's own building doors are never banned (warpLoopBannable).

// warpLoopLegs: the last four map visits bounce A, B, A, B and B was entered through the same portal both times (an
// unknown portal counts as the same).
func warpLoopLegs(h []mapVisit) bool {
	if len(h) < 4 {
		return false
	}
	h = h[len(h)-4:]
	if h[0].m != h[2].m || h[1].m != h[3].m || h[0].m == h[1].m {
		return false
	}
	for _, v := range h[1:] {
		if v.via == "tele" { // a Butterfly Wing / Kafra teleport / respawn leg (ai.go): not a portal bounce
			return false
		}
	}
	return h[1].via == "" || h[3].via == "" || h[1].via == h[3].via
}

// warpLoopBannable: a loop through w may mark it bad server-wide. Doors between a town and its own buildings (or between
// rooms of one building) are not: every shop trip needs them, and a ping-pong there is trip logic, not a broken warp.
//
// Town gates are not either (QA 2026-11-14 ~17:40Z: "warp loop prt_fild06 <-> prontera" every ~9 s banned the way
// between Prontera and its west field, and walkers west had "no route"; earlier geffen <-> gef_fild04 hid Mjolnir).
// A gate is the only way in or out of a town: normal in/out traffic and bots landing on the edge of the return portal
// look like A, B, A, B, but the gate is never broken - re-plan, never ban it.
func warpLoopBannable(w Warp) bool {
	tf, tt := townOf(w.From), townOf(w.To)
	if tf != "" || tt != "" {
		return false
	}
	return true
}

// townGate: w leads into or out of a town (or one of its buildings).
func townGate(w Warp) bool { return townOf(w.From) != "" || townOf(w.To) != "" }

// warpLoopBanFor: how long a loop through w marks it bad (short: a wrong ban hurts every bot's routing).
const warpLoopBanFor = 10 * time.Minute

// routeEndsIn: route r arrives on map m in the walk region that can reach (x, y) (the shop's / NPC's room).
func routeEndsIn(r []Warp, m string, x, y int) bool {
	if len(r) == 0 {
		return false
	}
	last := r[len(r)-1]
	if last.To != m {
		return false
	}
	mm := maps[m]
	if mm == nil {
		return true
	}
	return mm.Region(last.ToX, last.ToY) == nearRegionCell(mm, x, y)
}

// warpLegProgress: taking w (arriving at ax, ay) brought the bot closer to goal - fewer map changes left. Such a leg is
// travel, never a loop, even when the bot came this way before (QA 2026-11-14 14:34: guild02 members bound for
// mjo_dun02 walked back into Geffen for the Kafra, were refused, and walked out through 119,213 again; the detector
// called geffen <-> gef_fild04 a loop and banned Geffen's north exit - the walking route itself).
func warpLegProgress(w Warp, ax, ay int, goal string) bool {
	if goal == "" || goal == w.From {
		return false
	}
	if w.To == goal {
		return true
	}
	after := mapHops(w.To, ax, ay, goal)
	return after < 99 && after < mapHops(w.From, w.X, w.Y, goal)
}

// warpOnPlan: the best route from w's map to goal starts with w - a loop through it is our plan ping-ponging, not a
// broken warp: re-plan, never ban it (banning the planned exit sent bots round and round).
func warpOnPlan(w Warp, goal string) bool {
	if goal == "" {
		return false
	}
	r := RouteMaps(w.From, w.X, w.Y, goal)
	return len(r) > 0 && r[0].X == w.X && r[0].Y == w.Y && r[0].From == w.From
}
