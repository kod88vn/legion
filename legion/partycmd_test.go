package main

import "testing"

// Ayla's real party lines (docker logs ro-openkore-ayla, 2026-11-14) and the new partycmd.pl vocabulary.
func TestParsePartyCmdAylaLines(t *testing.T) {
	names := []string{"Ayla", "Silent rogue", "vex_x18", "Zen.fox", "xXRinfangXx", "Zendancer84", "Echobunny"}
	cases := []struct {
		line, kind, mp, to string
		x, y               int
	}{
		{"waiting for u at gef_fild10 52,326", "come", "gef_fild10", "", 52, 326},
		{"waiting for u at geffen 127,36", "come", "geffen", "", 127, 36},
		{"crossing gef_fild10 together, stay close", "close", "gef_fild10", "", 0, 0},
		{"help, 3 on me", "pullback", "", "", 0, 0},
		{"4 mobs on me, need help", "pullback", "", "", 0, 0},
		{"too many here, falling back to u in geffen", "stay", "geffen", "", 0, 0},
		{"coming to u, Zendancer84", "stay", "", "Zendancer84", 0, 0},
		{"Silent rogue we go on, meet us on orcsdun02", "map", "orcsdun02", "Silent rogue", 0, 0},
		{"vex_x18 come to orcsdun02, we start there", "map", "orcsdun02", "vex_x18", 0, 0},
		{"going on to orcsdun02, catch up Zendancer84", "map", "orcsdun02", "Zendancer84", 0, 0},
		{"farming at gef_fild10 near the kafra, come here", "come", "gef_fild10", "", 0, 0},
		{"almost died, winged to geffen kafra. meet me there, then we go back to orcsdun02", "come", "geffen", "", 0, 0},
		{"ok everyone here, lets go to orcsdun02", "goto", "orcsdun02", "", 0, 0},
		{"all: follow me, im on gef_fild10 64,307", "follow", "gef_fild10", "", 64, 307},
		{"Echobunny come to me, i'm at gef_fild10 52,326", "come", "gef_fild10", "Echobunny", 52, 326},
		{"wait here", "wait", "", "", 0, 0},
		{"pull back to me", "pullback", "", "", 0, 0},
		{"Echobunny heal me", "heal", "", "Echobunny", 0, 0},
		{"Echobunny buffs pls", "buff", "", "Echobunny", 0, 0},
		{"stay close, we cross gef_fild10 now", "close", "gef_fild10", "", 0, 0},
	}
	for _, c := range cases {
		got, ok := parsePartyCmd(c.line, names)
		if !ok || got.Kind != c.kind || got.Map != c.mp || got.To != c.to || got.X != c.x || got.Y != c.y {
			t.Errorf("%q: got %+v ok=%v, want kind=%s map=%s to=%q %d,%d", c.line, got, ok, c.kind, c.mp, c.to, c.x, c.y)
		}
	}
	// plans, town runs, chatter and questions are not commands
	for _, line := range []string{
		"going orcsdun02 - meet me there, kafra to geffen is fastest",
		"winged out nearly dead, regroup at the kafra then back to orcsdun02, town run to geffen. sell + restock there, meet at the kafra after",
		"xXRinfangXx theres ur Great Bow [3], enjoy",
		"new spot: orcsdun02, see u there",
		"ok", ":)", "where r u guys?",
	} {
		if got, ok := parsePartyCmd(line, names); ok {
			t.Errorf("%q: not a command, got %+v", line, got)
		}
	}
}

func TestPartyCmdTarget(t *testing.T) {
	o := partyCmdOrder{partyCmd: partyCmd{Kind: "come", Map: "geffen", X: 127, Y: 36}}
	// she moved on: her live position wins over the cell she named
	if m, x, y, cell := partyCmdTarget(o, pubState{Map: "gef_fild10", X: 52, Y: 326}, true); m != "gef_fild10" || x != 52 || y != 326 || !cell {
		t.Errorf("live leader: %s %d,%d %v", m, x, y, cell)
	}
	if m, x, y, cell := partyCmdTarget(o, pubState{}, false); m != "geffen" || x != 127 || y != 36 || !cell {
		t.Errorf("named cell: %s %d,%d %v", m, x, y, cell)
	}
	om := partyCmdOrder{partyCmd: partyCmd{Kind: "map", Map: "orcsdun02"}}
	if m, _, _, cell := partyCmdTarget(om, pubState{Map: "gef_fild10", X: 50, Y: 50}, true); m != "orcsdun02" || cell {
		t.Errorf("map order: %s %v", m, cell)
	}
	if r := partyCmdRadius("close"); r != 4 {
		t.Errorf("stay close radius %d", r)
	}
	if r := partyCmdRadius("follow"); r != 6 {
		t.Errorf("follow radius %d", r)
	}
}
