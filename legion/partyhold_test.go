package main

import (
	"testing"
	"time"
)

// QA 2026-11-14 19:27-20:06Z: a member's "waiting in" target flipped izlude -> prt_in -> prontera within a minute, and
// members chased their leader's Kafra hops into other towns. The wait is the party's one regroup town.
func TestStableHold(t *testing.T) {
	now := time.Now()
	b := &Bot{P: &Persona{}}
	if got := b.stableHold("prt_in", "prt_fild08", "prontera", "prt_fild05", pubState{Map: "prt_in"}, now); got != "prontera" {
		t.Fatalf("a building is never the hold: got %q, want prontera", got)
	}
	if got := b.stableHold("izlude", "prt_fild08", "izlude", "prt_fild05", pubState{Map: "prontera"}, now.Add(30*time.Second)); got != "prontera" {
		t.Fatalf("hold flipped to %q within 30 s", got)
	}
	if got := b.stableHold("prt_fild08", "prt_fild08", "prontera", "prt_fild05", pubState{}, now.Add(time.Minute)); got != "prt_fild08" {
		t.Fatalf("party map: got %q", got)
	}
	// the leader hops to alberta (a town): members wait at the regroup town, they don't follow it there
	c := &Bot{P: &Persona{}}
	if got := c.stableHold("alberta", "pay_fild01", "payon", "prontera", pubState{Map: "alberta"}, now); got != "payon" {
		t.Fatalf("chasing the leader into alberta: got %q, want the regroup town payon", got)
	}
	// following the leader across a field portal stays as it is
	if got := c.stableHold("prt_fild06", "pay_fild01", "payon", "prt_fild05", pubState{Map: "prt_fild06"}, now); got != "prt_fild06" {
		t.Fatalf("field follow: got %q", got)
	}
}
