package main

import (
	"testing"
	"time"
)

func TestTrailNextFresh(t *testing.T) {
	now := time.Now()
	road := []string{"moc_fild02", "pay_gld", "payon", "pay_fild07"}
	ts := []time.Time{now.Add(-20 * time.Minute), now.Add(-12 * time.Minute), now.Add(-2 * time.Minute), now.Add(-time.Minute)}
	if got := trailNextFresh(road, ts, "moc_fild02", now); got != "" {
		t.Fatalf("an old step guides members: %q", got)
	}
	if got := trailNextFresh(road, ts, "payon", now); got != "pay_fild07" {
		t.Fatalf("a fresh step: %q, want pay_fild07", got)
	}
}
