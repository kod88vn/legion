package main

import (
	"testing"
	"time"
)

func TestIdleWaitState(t *testing.T) {
	cases := []struct {
		s       string
		inParty bool
		want    bool
	}{
		{"rally: waiting at the Kafra in geffen", true, true}, // the party gathers at a Kafra: errands ok
		{"rally: waiting at the Kafra in geffen", false, true},
		{"waiting for Zen.fox", true, false}, // a party member farms near the party instead (PT18)
		{"waiting in geffen for Ayla", true, false},
		{"waiting outside for Ayla", true, false},
		{"waiting for Zen.fox", false, true}, // solo: any wait
		{"waiting in geffen: escort", false, true},
		{"town trip: go", false, false},
		{"farming", true, false},
		{"", false, false},
	}
	for _, c := range cases {
		if got := idleWaitState(c.s, c.inParty); got != c.want {
			t.Errorf("idleWaitState(%q, party=%v) = %v, want %v", c.s, c.inParty, got, c.want)
		}
	}
}

func TestIdleErrandWhy(t *testing.T) {
	tu := defaultTuning
	w := 60 * time.Second
	cases := []struct {
		name      string
		off       bool
		waited    time.Duration
		shop      bool
		wpct, sl  int
		loot, pot bool
		zeny      int
		want      string
	}{
		{"heavy bag in town", false, w, true, 40, 20, true, false, 100, "selling loot while waiting"},
		{"slots nearly full", false, w, true, 10, 90, true, false, 100, "selling loot while waiting"},
		{"light bag", false, w, true, 20, 20, true, false, 100, ""},
		{"heavy but nothing to sell", false, w, true, 60, 20, false, false, 100, ""},
		{"low pots with zeny", false, w, true, 10, 10, false, true, 2000, "restocking while waiting"},
		{"low pots broke", false, w, true, 10, 10, false, true, 100, ""},
		{"not waited long enough", false, 30 * time.Second, true, 60, 20, true, false, 100, ""},
		{"no shop here", false, w, false, 60, 20, true, false, 100, ""},
		{"switched off", true, w, true, 60, 20, true, false, 100, ""},
	}
	for _, c := range cases {
		tu.IdleErrandOff = c.off
		if got := idleErrandWhy(&tu, c.waited, c.shop, c.wpct, c.sl, c.loot, c.pot, c.zeny); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

// Ayla's town-run lines (partylead.pl announces every sellAuto chain, idleerrands.pl's included) name the town the
// members join (rally.go rallyHeard -> callTownRun -> ai.go "town run with the party")
func TestIdleErrandTownRunLineParsed(t *testing.T) {
	for line, town := range map[string]string{
		"storage run, town run to geffen. sell + restock there, meet at the kafra after":    "geffen",
		"town run to payon, restock. butterfly wing, shop, then meet me at the payon kafra": "payon",
	} {
		m := townRunLineRe.FindStringSubmatch(line)
		if m == nil || m[1] != town {
			t.Errorf("%q: got %v, want %s", line, m, town)
		}
	}
}
