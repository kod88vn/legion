package main

import (
	"testing"
	"time"
)

func TestPT32Allows(t *testing.T) {
	cases := []struct {
		name      string
		x, y      int
		refs      [][2]int
		others    [][2]int
		attackers int
		want      bool
	}{
		{"near, alone", 56, 50, nil, nil, 0, true},
		{"too far", 60, 50, nil, nil, 0, false},
		{"far from us, near the priest", 60, 50, [][2]int{{58, 50}}, nil, 0, true},
		{"aggressive on the way", 57, 50, nil, [][2]int{{54, 51}}, 0, false},
		{"aggressive off the way", 57, 50, nil, [][2]int{{54, 58}}, 0, true},
		{"aggressive beside us", 57, 50, nil, [][2]int{{51, 50}}, 0, true},
		{"two on us", 52, 50, nil, nil, 2, false},
		{"one on us", 52, 50, nil, nil, 1, true},
	}
	for _, c := range cases {
		if got := pt32Allows(50, 50, c.x, c.y, c.refs, c.others, c.attackers); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func TestGoalMoveAllowedPT16(t *testing.T) {
	now := time.Now()
	moved, arrived := now.Add(-50*time.Minute), now.Add(-40*time.Minute)
	if goalMoveAllowed(moved, arrived, now, false) {
		t.Fatal("40 min after arriving: the party stays")
	}
	if !goalMoveAllowed(moved, arrived, now, true) {
		t.Fatal("gear turn moves it")
	}
	if !goalMoveAllowed(now.Add(-3*time.Hour), now.Add(-121*time.Minute), now, false) {
		t.Fatal("after 2 h it may move")
	}
	if !goalMoveAllowed(now.Add(-10*time.Minute), time.Time{}, now, false) {
		t.Fatal("not arrived yet: not a stay")
	}
}
