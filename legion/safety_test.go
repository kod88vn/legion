package main

import (
	"testing"
	"time"
)

// travel() with a destination equal to the current map: RouteMaps returns an empty, non-nil route, and
// b.route[0] panicked (saveDetour -> travel crashed the whole service, 2026-11-14).
func TestTravelEmptyRouteNoPanic(t *testing.T) {
	if r := RouteMaps("crit_testmap", 10, 10, "crit_testmap"); r == nil || len(r) != 0 {
		t.Fatalf("expected an empty non-nil route for a == b, got %v", r)
	}
	b := &Bot{P: &Persona{}, buffAt: map[string]time.Time{}}
	b.MapName, b.X, b.Y = "crit_testmap", 10, 10
	b.Goal = Goal{Mode: "hunt", Map: "crit_testmap"}
	b.travel(&GameMap{Name: "crit_testmap"}, false)
	if b.route != nil {
		t.Fatalf("route should be cleared, got %v", b.route)
	}
}

// safely() recovers a panic, resets the bot's plans and releases nothing twice.
func TestSafelyRecovers(t *testing.T) {
	b := &Bot{P: &Persona{Name: "crit_test"}}
	b.route = []Warp{{From: "a", To: "b"}}
	b.viaTown = "prontera"
	b.safely("test", func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		var r []Warp
		_ = r[0]
	})
	if b.route != nil || b.viaTown != "" {
		t.Fatalf("state not reset: route %v via %q", b.route, b.viaTown)
	}
	if !b.mu.TryLock() {
		t.Fatal("mutex left locked after a recovered panic")
	}
	b.mu.Unlock()
}
