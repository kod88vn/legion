package main

// ferry.go - the Sailor boats as route edges (bug b1794712028nlq: 5 solo bots looped prontera <-> prt_fild08 with
// "no route to iz_dun00: leaving"). Byalan Island (izlu2dun, the way into iz_dun00..04) has no warp from the rest of
// the world: players take the Sailor's boat in Izlude. The warp graph (srvwarps.go) had no such edge, so every route
// to iz_dun* failed although hunt picking offered those maps. Players and Ayla's OpenKore take the boat (portals.txt
// NPC lines, fix_re_portals.sh); now the engine does too.
//
// Server truth (rAthena 1ca3e77, both loaded in Renewal):
//   npc/cities/izlude.txt Sailor_izlude, duplicated at izlude 197,205 by npc/re/cities/izlude.txt:
//     menu "Byalan Island -> 150 Zeny." -> warp "izlu2dun",107,50
//   npc/cities/izlude.txt Sailor#2izlude at izlu2dun 108,27: menu "Yeah, I'm tired to death." -> (checkre)
//     warp "izlude",197,210
// tuning ferries_off = true removes the edges (routes to iz_dun* fail again).

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

var ferries = []Warp{
	{From: "izlude", X: 197, Y: 205, To: "izlu2dun", ToX: 107, ToY: 50, NPC: "Byalan Island", Fare: 150},
	{From: "izlu2dun", X: 108, Y: 27, To: "izlude", ToX: 197, ToY: 210, NPC: "Yeah", Fare: 0},
}

var ferriesFrom = map[string][]Warp{}

// loadFerries: index the boats whose maps exist (call after the map cache is loaded).
func loadFerries() {
	for _, f := range ferries {
		if maps[f.From] != nil && maps[f.To] != nil {
			ferriesFrom[f.From] = append(ferriesFrom[f.From], f)
		}
	}
}

// routeEdges: the ways off map m for route planning - its warps, plus its boats unless ferries_off.
func routeEdges(m string) []Warp {
	fs := ferriesFrom[m]
	if len(fs) == 0 || tune().FerriesOff {
		return warpsFrom[m]
	}
	ws := warpsFrom[m]
	out := make([]Warp, 0, len(ws)+len(fs))
	return append(append(out, ws...), fs...)
}

type ferryState struct {
	key     string
	w       Warp
	stage   string // walk, talk, dialog
	ax, ay  int    // walkable cell beside the Sailor
	npc     uint32
	started time.Time
	at      time.Time
}

var ferryStates sync.Map // *Bot -> *ferryState

func ferryKey(w Warp) string { return warpKey(w.From, w.X, w.Y) + ">" + w.To }

// ferryStep (b.mu held): walk to the Sailor, talk, pick the boat. Called by travel and walkToPortal for an NPC edge.
func (b *Bot) ferryStep(gm *GameMap, w Warp, now time.Time) {
	if gm == nil || b.MapName != w.From {
		return
	}
	v, _ := ferryStates.Load(b)
	st, _ := v.(*ferryState)
	if st == nil || st.key != ferryKey(w) || now.Sub(st.started) > 3*time.Minute {
		st = &ferryState{key: ferryKey(w), w: w, stage: "walk", started: now, at: now}
		st.ax, st.ay = ferryStand(gm, b.X, b.Y, w)
		ferryStates.Store(b, st)
	}
	b.lastWarp = w
	if b.Zeny < w.Fare {
		if b.ferryBroke(st, now) { // travelshare.go: a solo goal is given up; no retry every tick
			return
		}
		b.ferryGiveUp(st, fmt.Sprintf("can't pay the boat at %s (%dz, have %d)", w.From, w.Fare, b.Zeny), now)
		return
	}
	b.setState("going to the Sailor (boat to " + w.To + ")")
	switch st.stage {
	case "walk":
		if dist(b.X, b.Y, w.X, w.Y) > 3 {
			if b.travelStalled(st.ax, st.ay, now) { // nav.go: re-plan / avoid / wing like any portal
				return
			}
			b.stepAlong(gm, st.ax, st.ay)
			return
		}
		st.stage, st.at = "talk", now
		fallthrough
	case "talk":
		npc := b.npcAt(w.X, w.Y)
		if npc == 0 {
			if now.Sub(st.at) > 10*time.Second {
				b.ferryGiveUp(st, fmt.Sprintf("no Sailor seen at %s %d,%d", w.From, w.X, w.Y), now)
			}
			return
		}
		st.npc, st.stage, st.at = npc, "dialog", now
		if b.Sitting {
			go b.stand()
		}
		go b.send(newPkt(0x0090).u32(npc).u8(1).b)
	case "dialog":
		if now.Sub(st.at) > 15*time.Second { // the boat never left: close the dialog, try another way
			npc := st.npc
			b.dialogNPC = 0
			go func() {
				b.send(newPkt(0x00b9).u32(npc).b)
				b.send(newPkt(0x00b8).u32(npc).u8(255).b)
				b.send(newPkt(0x0146).u32(npc).b)
			}()
			b.ferryGiveUp(st, fmt.Sprintf("the Sailor at %s %d,%d never sailed", w.From, w.X, w.Y), now)
		}
	}
}

// ferryStand: the walkable cell within 2 of the Sailor nearest to (x, y) (the NPC's own cell may be blocked).
func ferryStand(gm *GameMap, x, y int, w Warp) (int, int) {
	bx, by, bd := w.X, w.Y, 1<<30
	for dy := -2; dy <= 2; dy++ {
		for dx := -2; dx <= 2; dx++ {
			cx, cy := w.X+dx, w.Y+dy
			if (dx != 0 || dy != 0) && gm.Walkable(cx, cy) && !gm.OnWarp(cx, cy) {
				if d := dist(x, y, cx, cy); d < bd {
					bx, by, bd = cx, cy, d
				}
			}
		}
	}
	return bx, by
}

// ferryGiveUp: this bot avoids the boat for 10 minutes (travelAvoid, like a portal it got stuck at) and re-plans.
func (b *Bot) ferryGiveUp(st *ferryState, why string, now time.Time) {
	ferryStates.Delete(b)
	m, _ := travelAvoid.Load(b)
	am, _ := m.(map[string]time.Time)
	if am == nil {
		am = map[string]time.Time{}
	}
	am[warpKey(st.w.From, st.w.X, st.w.Y)] = now.Add(10 * time.Minute)
	travelAvoid.Store(b, am)
	b.route, b.path = nil, nil
	b.event("info", "", why+": trying another way")
}

// ferryDialogOwned: a boat dialog this bot is driving right now (answerStrayDialog leaves it alone).
func (b *Bot) ferryDialogOwned(now time.Time) bool {
	v, ok := ferryStates.Load(b)
	if !ok {
		return false
	}
	st := v.(*ferryState)
	return st.stage == "dialog" && b.MapName == st.w.From && now.Sub(st.at) < 20*time.Second
}

// onFerryDialog (b.mu held): next / close / pick the boat's menu option.
func (b *Bot) onFerryDialog(op uint16, pk []byte) {
	if !b.ferryDialogOwned(time.Now()) || len(pk) < 6 {
		return
	}
	v, _ := ferryStates.Load(b)
	st := v.(*ferryState)
	switch op {
	case 0x00b5:
		st.at = time.Now()
		go b.send(newPkt(0x00b9).u32(le.Uint32(pk[2:])).b)
	case 0x00b6:
		go b.send(newPkt(0x0146).u32(le.Uint32(pk[2:])).b)
	case 0x00b7:
		if len(pk) < 9 {
			return
		}
		id := le.Uint32(pk[4:])
		choice := ferryChoice(strings.Split(cstr(pk[8:]), ":"), st.w.NPC)
		st.at = time.Now()
		go b.send(newPkt(0x00b8).u32(id).u8(byte(choice)).b)
		if choice == 255 {
			b.ferryGiveUp(st, "the Sailor's menu has no "+st.w.NPC, time.Now())
		}
	}
}

// ferryChoice: the 1-based menu index of the option starting with want, 255 (cancel) if none.
func ferryChoice(opts []string, want string) int {
	for i, o := range opts {
		if strings.HasPrefix(strings.TrimSpace(o), want) {
			return i + 1
		}
	}
	return 255
}
