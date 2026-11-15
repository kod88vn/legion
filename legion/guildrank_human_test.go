package main

import (
	"testing"
	"time"
)

func TestHumanPurseOf(t *testing.T) {
	now := time.Now()
	s := humanStatus{Name: "Ayla", Guild: "Starveil", Zeny: 52000, TS: now.Unix() - 60}
	if n, z, _, ok := humanPurseOf(s, "starveil ", now); !ok || n != "Ayla" || z != 52000 {
		t.Fatalf("fresh status of the guild's master counts: %v %v %v", n, z, ok)
	}
	if _, _, _, ok := humanPurseOf(s, "Crimson Oath", now); ok {
		t.Error("another guild")
	}
	old := s
	old.TS = now.Unix() - 25*3600
	if _, _, _, ok := humanPurseOf(old, "Starveil", now); ok {
		t.Error("over a day old")
	}
	if _, _, _, ok := humanPurseOf(s, "", now); ok {
		t.Error("no guild known")
	}
}
