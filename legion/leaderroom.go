package main

// Guild leaders always get onto the map server (RULES.md 3b: "each guild leader's account stays logged in at all
// times").
//
// 2026-11-14 17:45Z: guild01's leader Silver_heart stayed offline for 15+ min after the 12:28 rathena recreate. The
// map server was full: rAthena is built with select() sockets, MAXCONN = FD_SETSIZE = 1024, so map-server holds about
// 1013 clients (the rest are its listen socket, char-server link and SQL handles), and every further accept failed
// with "Too many open files" (38k log lines a minute). Legion had 1000 slot holders + 10 leaders (outside the slots)
// online, plus legion-core and Ayla: the leaders that came last after the restart never got in. Their map handshake
// timed out ("map: ... i/o timeout"), the char server kept the account flagged online for a while, so the next try
// got "login refused (0081, code 8)", and so on.
//
// Two fixes: the ordinary slots stop -reserve short of -max-online, so leaders, pinned players, legion-core, Ayla and
// humans always have headroom; and if the map server is still full when a leader logs in, one ordinary slot holder
// logs off and keeps its slot for a while (so the next waiting bot doesn't grab the freed connection first).

import (
	"log"
	"strings"
	"sync"
	"time"
)

// bumpedWhy: the session-end reason of a bot that stepped aside for a guild leader.
const bumpedWhy = "server full: made room for a guild leader"

// leaderRoomHold: how long a bumped bot keeps its slot (and stays offline) before queueing again.
var leaderRoomHold = 10 * time.Minute

var lastRoomFor sync.Map // leader account -> time of the last bump made for it (one per 90 s)

func slotCapacity(maxOnline, reserve int) int {
	n := maxOnline - reserve
	if n < 1 {
		n = 1
	}
	if reserve > 0 {
		log.Printf("online slots: %d (max-online %d - reserve %d for leaders/pinned/core/humans)", n, maxOnline, reserve)
	}
	return n
}

// mapServerFull: the login got through login and char server but the map server never answered (accept failing
// on a full map server leaves the connection in the backlog until our read times out) or refused it.
func mapServerFull(err error) bool {
	return err != nil && strings.HasPrefix(err.Error(), "map")
}

// makeRoomFor asks one ordinary slot holder of this instance to log off so the guild leader can get in. Prefers a
// bot outside any party; never a leader, a pinned (human-led) player or a bot without a slot. Reports whether a
// bot was asked.
func makeRoomFor(leader *Bot) bool {
	now := time.Now()
	if v, ok := lastRoomFor.Load(leader.P.Account); ok && now.Sub(v.(time.Time)) < 90*time.Second {
		return false
	}
	bots, _ := allBots.Load().([]*Bot)
	var pick, fallback *Bot
	for _, x := range bots {
		if x == leader || x.bump == nil || !x.holdsSlot.Load() || x.alwaysOnline() || x.pinned() != nil {
			continue
		}
		x.mu.Lock()
		online := x.Online
		x.mu.Unlock()
		if !online {
			continue
		}
		if partyOf(x) == nil {
			pick = x
			break
		}
		if fallback == nil {
			fallback = x
		}
	}
	if pick == nil {
		pick = fallback
	}
	if pick == nil {
		return false
	}
	select {
	case pick.bump <- struct{}{}:
	default:
		return false // already asked
	}
	lastRoomFor.Store(leader.P.Account, now)
	log.Printf("map server full: %s logs off so guild leader %s can get in", pick.P.Name, leader.P.Name)
	leader.event("guild", "", "map server full; "+pick.P.Name+" logs off to make room")
	return true
}

// yieldSlot: a bumped bot stays offline holding its slot for leaderRoomHold.
func yieldSlot(b *Bot, stop <-chan struct{}) {
	b.mu.Lock()
	b.State = "offline (made room for a guild leader)"
	b.mu.Unlock()
	select {
	case <-stop:
	case <-time.After(leaderRoomHold):
	}
}
