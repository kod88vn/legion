package main

import (
	"math/rand"
	"sync/atomic"
	"testing"
	"time"
)

// The session-end idle ("calling it a day", town.go) is the only idle with set_by exactly "auto". A bot held online
// by a pin acquired after login (or a leader's always-online) used to sit on it for good: nothing re-picked a goal
// (b1794735066954-196 review). think() now re-picks it and clears the stale sessionEnd, so "calling it a day" cannot
// fire again on the first step out of town (a re-trip loop). The tests run think() itself, not only the predicate.

// thinkBot: a bot standing on a map with the given goal, its session already over (sessionEnd in the past).
func thinkBot(name, m string, g Goal) *Bot {
	b := stickBot(name, m, 156, 32)
	b.Goal = g
	b.rng = rand.New(rand.NewSource(1))
	b.sessionEnd = time.Now().Add(-time.Hour)
	return b
}

func TestIdleAutoPinnedRepicksAndDropsSessionEnd(t *testing.T) {
	b := thinkBot("IdlePinned", "prontera", Goal{Mode: "idle", Map: "prontera", Set: "auto"})
	b.pin = &Pin{}
	b.think()
	if !b.sessionEnd.IsZero() {
		t.Fatalf("a pinned bot that stays online must drop its stale sessionEnd, has %v", b.sessionEnd)
	}
	if b.Goal.Set == "auto" && b.Goal.Mode == "idle" {
		t.Fatalf("a pinned bot on the session-end idle must pick a goal again, still %+v", b.Goal)
	}
	if b.trip != nil {
		t.Fatalf("no calling-it-a-day trip may start for a bot that stays online")
	}
}

func TestIdleAutoPinnedAfterLoginDoesNotRetrip(t *testing.T) {
	// the loop: a pin acquired after login, sessionEnd set at login (past), the bot out on a field. Before the fix the
	// trip ("calling it a day") fired here again and again: out, home, idle, out.
	b := thinkBot("IdleLoop", "pay_fild01", Goal{Mode: "hunt", Map: "pay_fild01", Set: "auto"})
	b.pin = &Pin{}
	b.think()
	if b.trip != nil && b.trip.Why == "calling it a day" {
		t.Fatalf("a pinned bot in the field started calling it a day from a stale sessionEnd (re-trip loop)")
	}
	if !b.sessionEnd.IsZero() {
		t.Fatalf("stale sessionEnd kept for a pinned bot: %v", b.sessionEnd)
	}
}

func TestIdleAutoPlainIdleWaitsForLogout(t *testing.T) {
	// no pin, no leader: the session-end idle waits in town for logoutSignal; nothing is re-picked
	b := thinkBot("IdlePlain", "prontera", Goal{Mode: "idle", Map: "prontera", Set: "auto"})
	end := b.sessionEnd
	b.think()
	if b.Goal.Mode != "idle" || b.Goal.Set != "auto" {
		t.Fatalf("a plain session-end idle must wait for logout, goal now %+v", b.Goal)
	}
	if !b.sessionEnd.Equal(end) {
		t.Fatalf("a plain bot's sessionEnd changed: %v -> %v", end, b.sessionEnd)
	}
}

func TestIdleAutoOnlyForTheSessionEndIdle(t *testing.T) {
	b := stickBot("IdleOther", "prontera", 156, 32)
	b.pin = &Pin{}
	for _, g := range []Goal{
		{Mode: "idle", Map: "prontera", Set: "auto (nowhere reachable)"}, // huntRetryDue already re-picks these
		{Mode: "hunt", Map: "pay_fild01", Set: "auto"},
		{Mode: "party", Map: "prontera", Set: "auto"},
	} {
		b.Goal = g
		if b.idleAutoNeedsGoal() {
			t.Errorf("goal %+v must not trigger the session-end re-pick", g)
		}
	}
}

func TestProbeIdleAutoOncePerHour(t *testing.T) {
	b := stickBot("IdleProbe", "prontera", 156, 32)
	b.Goal = Goal{Mode: "idle", Map: "prontera", Set: "auto"}
	now := time.Now()
	b.buffAt["idleAuto"] = now.Add(-25 * time.Minute)
	before := atomic.LoadInt64(&idleAutoProbes)
	b.probeIdleAuto(now)
	if got := atomic.LoadInt64(&idleAutoProbes) - before; got != 1 {
		t.Fatalf("first probe after 25 min idle wrote %d records, want 1", got)
	}
	b.probeIdleAuto(now.Add(10 * time.Minute))
	if got := atomic.LoadInt64(&idleAutoProbes) - before; got != 1 {
		t.Fatalf("second probe within the hour wrote a record (total %d)", got)
	}
	b.probeIdleAuto(now.Add(61 * time.Minute))
	if got := atomic.LoadInt64(&idleAutoProbes) - before; got != 2 {
		t.Fatalf("probe after the hour: total %d, want 2", got)
	}
}

func TestProbeIdleAutoNeedsTwentyMinutes(t *testing.T) {
	b := stickBot("IdleYoung", "prontera", 156, 32)
	b.Goal = Goal{Mode: "idle", Map: "prontera", Set: "auto"}
	now := time.Now()
	b.buffAt["idleAuto"] = now.Add(-5 * time.Minute)
	before := atomic.LoadInt64(&idleAutoProbes)
	b.probeIdleAuto(now)
	if atomic.LoadInt64(&idleAutoProbes) != before {
		t.Fatalf("a 5-minute idle wrote a probe record")
	}
}
