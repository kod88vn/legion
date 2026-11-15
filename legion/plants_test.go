package main

import (
	"testing"
	"time"
)

// TestPlantsAreNoSightings: worthless monsters (plants) are neither shared sightings nor "monsters in view" (QA
// 14:28: 50 of 52 bots on pay_fild10 sat in its plant corner with 0 kills).
func TestPlantsAreNoSightings(t *testing.T) {
	mobs[990161] = &Mob{Level: 1, HP: 10, Exp: 0}
	defer delete(mobs, 990161)
	b := &Bot{P: &Persona{Name: "zz_plant"}, MapName: "zz_fild11"}
	b.ents = map[uint32]*Entity{1: {ID: 1, Type: objMob, Class: 990161, X: 50, Y: 50}}
	now := time.Now()
	b.seenMap, b.seenMapAt = "zz_fild11", now.Add(-time.Hour)
	b.noMobSince = now.Add(-time.Minute)
	b.noteMobSight(now)
	if _, _, ok := sightNear("zz_fild11", 0, 0, 0, 999, 99, now, func(int, int) bool { return true }); ok {
		t.Error("a plant was shared as a sighting")
	}
	if !b.noMobSince.Before(now.Add(-30 * time.Second)) {
		t.Error("a plant counted as a monster in view")
	}
}
