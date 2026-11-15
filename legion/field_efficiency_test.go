package main

import (
	"testing"
	"time"
)

// bugq b1794735068469-692: a bot whose level has no fitting hunting spot went idle with no retry marker and never
// picked a ground again. The empty-spots branch must leave a retry marker that think() acts on.
func TestNoFittingSpotRetries(t *testing.T) {
	swapFairTables(t)
	huntSpots = nil
	guideSpots = nil
	b := &Bot{P: &Persona{BaseLevel: 63}, MapName: "izlude", invSeen: true, buffAt: map[string]time.Time{},
		cart: map[int]*InvItem{}, inv: map[int]*InvItem{}, equipped: map[uint32]*InvItem{}}
	b.BaseLv = 63
	b.chooseHuntingGround()
	if b.Goal.Mode != "idle" {
		t.Fatalf("goal = %+v, want idle while no spot fits", b.Goal)
	}
	if b.Goal.Set != "auto (nowhere reachable)" {
		t.Fatalf("goal set = %q: think() only re-plans the retry marker, so an unmarked idle goal is never picked again", b.Goal.Set)
	}
	if d := time.Until(b.buffAt["huntRetry"]); d <= 0 || d > 2*time.Minute {
		t.Fatalf("huntRetry in %v, want about a minute", d)
	}
}
