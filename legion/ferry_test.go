package main

import (
	"os"
	"strconv"
	"testing"
)

// open map helper: all cells walkable
func openMap(name string, w, h int) *GameMap {
	return &GameMap{Name: name, W: w, H: h, cells: make([]byte, w*h)}
}

func TestFerryMakesByalanReachable(t *testing.T) {
	names := []string{"fz_town", "fz_isle", "fz_dun"}
	saved := map[string]*GameMap{}
	for _, n := range names {
		saved[n] = maps[n]
		maps[n] = openMap(n, 60, 60)
	}
	oldW, oldF := map[string][]Warp{}, ferriesFrom
	for _, n := range names {
		oldW[n] = warpsFrom[n]
	}
	defer func() {
		for _, n := range names {
			if saved[n] == nil {
				delete(maps, n)
			} else {
				maps[n] = saved[n]
			}
			warpsFrom[n] = oldW[n]
		}
		ferriesFrom = oldF
	}()
	ferriesFrom = map[string][]Warp{}
	warpsFrom["fz_isle"] = []Warp{{From: "fz_isle", X: 30, Y: 55, To: "fz_dun", ToX: 30, ToY: 5, XS: 1, YS: 1}}
	warpsFrom["fz_dun"] = []Warp{{From: "fz_dun", X: 30, Y: 2, To: "fz_isle", ToX: 30, ToY: 50, XS: 1, YS: 1}}
	if r := RouteMaps("fz_town", 10, 10, "fz_dun"); r != nil {
		t.Fatalf("no boat yet: want no route, got %v", r)
	}
	ferriesFrom["fz_town"] = []Warp{{From: "fz_town", X: 40, Y: 40, To: "fz_isle", ToX: 30, ToY: 10, NPC: "Byalan Island", Fare: 150}}
	r := RouteMaps("fz_town", 10, 10, "fz_dun")
	if len(r) != 2 || r[0].NPC != "Byalan Island" || r[1].To != "fz_dun" {
		t.Fatalf("want boat then warp, got %+v", r)
	}
	// warp masks never include the boat (it is a talk NPC, nobody gets warped walking past it)
	if maps["fz_town"].OnWarp(40, 40) {
		t.Fatal("the Sailor's cell must not be a warp trigger")
	}
	// a bot that gave up on the boat finds no way
	skip := func(w Warp) bool { return w.NPC != "" }
	if r := routeMapsSkip("fz_town", 10, 10, "fz_dun", 0, skip); len(r) != 2 {
		t.Logf("avoided boat is still used when nothing else gets there (like bad warps): %v", r)
	}
}

func TestFerryChoice(t *testing.T) {
	opts := []string{"Byalan Island -> 150 Zeny.", "Alberta Marina  -> 500 Zeny.", "Cancel."}
	if c := ferryChoice(opts, "Byalan Island"); c != 1 {
		t.Fatalf("want 1, got %d", c)
	}
	if c := ferryChoice([]string{"Yeah, I'm tired to death.", "Nope, I love this place!"}, "Yeah"); c != 1 {
		t.Fatalf("want 1, got %d", c)
	}
	if c := ferryChoice(opts, "Comodo"); c != 255 {
		t.Fatalf("want cancel, got %d", c)
	}
}

func TestRouteEdgesDoesNotAliasWarps(t *testing.T) {
	old, oldF := warpsFrom["fz_alias"], ferriesFrom
	defer func() { warpsFrom["fz_alias"], ferriesFrom = old, oldF }()
	ws := make([]Warp, 1, 8) // spare capacity: a plain append would write into warpsFrom's array
	ws[0] = Warp{From: "fz_alias", To: "a"}
	warpsFrom["fz_alias"] = ws
	ferriesFrom = map[string][]Warp{"fz_alias": {{From: "fz_alias", To: "b", NPC: "x"}}}
	if e := routeEdges("fz_alias"); len(e) != 2 {
		t.Fatalf("want 2 edges, got %v", e)
	}
	if len(warpsFrom["fz_alias"]) != 1 || ws[:2][1].To == "b" {
		t.Fatal("routeEdges wrote into warpsFrom")
	}
}

const kafraFixture = `function	script	F_KafTele	{
	if (@wrpD$[.@j] == "Al De Baran") warp "aldebaran",168,112;
	else if (@wrpD$[.@j] == "Geffen") warp "geffen", 120, 39;
	else if (@wrpD$[.@j] == "Izlude") {
		if (checkre(0) == true)
			warp "izlude", 128, 98;
		else
			warp "izlude", 91, 105;
	}
	else if (@wrpD$[.@j] == "Payon") warp "payon", 161, 58;
	else if (@wrpD$[.@j] == "Morocc") warp "morocc", 156, 46;
	else if (@wrpD$[.@j] == "Prontera") warp "prontera", 116, 72;
	end;
}
function	script	F_KafSet	{
	.@map$ = strnpcinfo(4);
	if (.@map$ == "prontera") {
		setarray @wrpD$[0], "Izlude", "Geffen";
		setarray @wrpP[0], 60, 120;
	} else if (.@map$ == "izlude") {
		setarray @wrpD$[0], "Geffen", "Payon", "Morocc", "Al De Baran";
		setarray @wrpP[0], 120, 120, 120, 180;
	} else if (.@map$ == "payon") {
		setarray @wrpD$[0], "Prontera", "Morocc";
		setarray @wrpP[0], 120, 120;
	} else if (.@map$ == "geffen") {
		setarray @wrpD$[0], "Prontera", "Al De Baran";
		setarray @wrpP[0], 120, 120;
	} else if (.@map$ == "morocc") {
		setarray @wrpP[0], 120, 120;
		setarray @wrpD$[0], "Prontera", "Payon";
	}
	return;
}
`

func TestParseKafraTable(t *testing.T) {
	tb := parseKafraTable(kafraFixture)
	if tb == nil {
		t.Fatal("did not parse")
	}
	if d := tb["prontera"]; len(d) != 2 || d[0] != (kafraDest{"Izlude", "izlude", 128, 98, 60}) {
		t.Fatalf("prontera: %+v (Izlude must land on the Renewal cell 128,98)", d)
	}
	if d := tb["izlude"]; len(d) != 4 || d[3] != withPrice(kdAldeb, 180) || d[0] != kdGeffen {
		t.Fatalf("izlude: %+v", d)
	}
	if d := tb["morocc"]; len(d) != 2 || d[1] != kdPayon { // prices listed before names
		t.Fatalf("morocc: %+v", d)
	}
	// the built-in table agrees with the server where both list a town
	for town, ds := range tb {
		if len(kafraTableDiff(map[string][]kafraDest{town: kafraDests[town]}, map[string][]kafraDest{town: ds})) > 0 &&
			town != "prontera" && town != "payon" && town != "geffen" && town != "morocc" { // trimmed in the fixture
			t.Errorf("%s differs: built-in %+v, parsed %+v", town, kafraDests[town], ds)
		}
	}
}

// TestIzDunRouteLive (LEGION_RATHENA = a rAthena tree overlaid like the containers): the queue item b1794712028nlq -
// Prontera to iz_dun00 goes by the Izlude Sailor's boat, and back.
func TestIzDunRouteLive(t *testing.T) {
	rd := os.Getenv("LEGION_RATHENA")
	if rd == "" {
		t.Skip("offline measurement: set LEGION_RATHENA")
	}
	if len(maps) < 100 {
		*rathena = rd
		loadGameData()
	}
	r := RouteMaps("prontera", 156, 191, "iz_dun00", 30)
	var hops []string
	boat := false
	for _, w := range r {
		hops = append(hops, w.From+">"+w.To)
		boat = boat || w.NPC == "Byalan Island"
	}
	if r == nil || !boat || r[len(r)-1].To != "iz_dun00" {
		t.Fatalf("prontera -> iz_dun00: %v", hops)
	}
	t.Logf("prontera -> iz_dun00: %v", hops)
	if back := RouteMaps("iz_dun00", 168, 168, "prontera", 30); back == nil {
		t.Fatal("no way back from iz_dun00")
	}
	if w := warpAt("aldebaran", 35, 140); w == nil || w.To != "alde_gld" {
		t.Fatalf("aldebaran 35,140 must go to alde_gld (the te_alde_gld gate is commented out): %+v", w)
	}
	for _, k := range [][3]int{{10, 27, 219}, {14, 371, 219}} {
		if w := warpAt("gef_fild"+strconv.Itoa(k[0]), k[1], k[2]); w != nil {
			t.Fatalf("removed Renewal Geffen link still a warp: %+v", w)
		}
	}
}

func warpAt(m string, x, y int) *Warp {
	for i, w := range warpsFrom[m] {
		if w.X == x && w.Y == y {
			return &warpsFrom[m][i]
		}
	}
	return nil
}
