package main

import (
	"os"
	"sync/atomic"
	"testing"
	"time"
)

// TestMapAccessLive (real game data, LEGION_RATHENA): mjo_dun01 / mjo_dun02 are entered from mjolnir_02 (a train map
// needing a weakest power of 52): every picker path must refuse them for a lv40 party and accept them for lv60.
func TestMapAccessLive(t *testing.T) {
	rd := os.Getenv("LEGION_RATHENA")
	if rd == "" {
		t.Skip("set LEGION_RATHENA")
	}
	*rathena = rd
	if len(maps) == 0 {
		loadGameData()
	}
	mk := func(name string, pw int) *Bot {
		b := &Bot{P: &Persona{Name: name, Aggression: 0.5}, BaseLv: pw + 15, Class: 1, Online: true, invWaitFrom: time.Now()}
		v := &atomic.Value{}
		v.Store(pubState{Name: name, Lv: pw + 15, Power: pw, Online: true, Known: true, Map: "geffen"})
		pubs.Store(name, v)
		return b
	}
	weak := []*Bot{mk("zza_w1", 40), mk("zza_w2", 42)}
	strong := []*Bot{mk("zza_s1", 66), mk("zza_s2", 68)}
	for _, m := range []string{"mjo_dun01", "mjo_dun02"} {
		if maps[m] == nil {
			continue
		}
		if !mapAccessTooHard(m, 40) || !partyMapTooHard(m, 40) {
			t.Errorf("%s: the way in (mjolnir_02) must be too hard for power 40", m)
		}
		if partyMapOK(weak, m) {
			t.Errorf("partyMapOK: %s accepted for a power-40 party", m)
		}
		if goalMapSafe(weak, m) {
			t.Errorf("lfg goalMapSafe: %s accepted for a power-40 party", m)
		}
		if mapAccessTooHard(m, 66) {
			t.Errorf("%s: power 66 gets through mjolnir_02", m)
		}
	}
	for _, from := range []string{"geffen", "gef_fild07"} {
		sp := townSpawn["geffen"]
		if r := partyMapNear(weak, from, sp[0], sp[1], ""); r == "mjo_dun01" || r == "mjo_dun02" || r == "mjolnir_02" {
			t.Errorf("partyMapNear from %s picked %s for a power-40 party", from, r)
		}
	}
	p := &Party{Members: weak, Map: "gef_fild07", Leader: weak[0]}
	if r := partySpotNear(p, "gef_fild07", -1, -1); r == "mjo_dun01" || r == "mjo_dun02" || r == "mjolnir_02" {
		t.Errorf("partySpotNear (relocate) picked %s for a power-40 party", r)
	}
	if r := bestPartySpot(weak); r == "mjo_dun01" || r == "mjo_dun02" || r == "mjolnir_02" {
		t.Errorf("bestPartySpot (guild leader) picked %s for a power-40 party", r)
	}
	solo := mk("zza_solo", 40)
	solo.BaseLv = 40 // powerLv() under the lock reads the base level (no gear data)
	solo.MapName, solo.X, solo.Y = "geffen", townSpawn["geffen"][0], townSpawn["geffen"][1]
	solo.mu.Lock()
	for _, g := range solo.guideSpotsFor(40) {
		if g.Map == "mjo_dun01" || g.Map == "mjo_dun02" || g.Map == "mjolnir_02" {
			t.Errorf("solo guideSpotsFor offered %s at power 40", g.Map)
		}
	}
	if solo.soloMapOK("mjo_dun01") || solo.soloMapOK("mjolnir_02") {
		t.Error("lfg soloMapOK accepted a mjolnir_02 map at power 40")
	}
	solo.mu.Unlock()
	_ = strong
}

// TestCrossingDangerLive (real data): a power-40 member walking from gef_fild04 to mjo_dun01 must stop at mjolnir_01
// (the safe point) before mjolnir_02; a power-66 member has no dangerous crossing.
func TestCrossingDangerLive(t *testing.T) {
	rd := os.Getenv("LEGION_RATHENA")
	if rd == "" {
		t.Skip("set LEGION_RATHENA")
	}
	*rathena = rd
	if len(maps) == 0 {
		loadGameData()
	}
	gm := maps["gef_fild04"]
	if gm == nil || maps["mjo_dun01"] == nil {
		t.Skip("maps missing")
	}
	x, y := 187, 42 // arrival from Geffen
	for _, c := range []struct {
		lv           int
		safe, danger string
	}{{40, "mjolnir_01", "mjolnir_02"}, {66, "", ""}} {
		b := &Bot{P: &Persona{Name: "zzc"}, BaseLv: c.lv, Class: 1, MapName: "gef_fild04", X: x, Y: y, invWaitFrom: time.Now()}
		safe, danger := b.crossingDanger("mjo_dun01")
		if safe != c.safe || danger != c.danger {
			t.Errorf("power %d: crossing = %q / %q, want %q / %q", c.lv, safe, danger, c.safe, c.danger)
		}
	}
}

// TestMVPMapsLive (real data): every map with a timed MVP spawn reads too hard for a party under the MVP's level + 20
// (pay_fild10 / Eddga: QA 20:49 wipe of a lv72-80 party). Logs the MVP maps (LEGION_RATHENA).
func TestMVPMapsLive(t *testing.T) {
	rd := os.Getenv("LEGION_RATHENA")
	if rd == "" {
		t.Skip("set LEGION_RATHENA")
	}
	*rathena = rd
	if len(maps) == 0 {
		loadGameData()
	}
	n := 0
	for m := range spawnsByMap {
		if mv := mapMVP(m); mv != nil {
			n++
			t.Logf("MVP map %s: %s lv%d", m, mv.Name, mv.Level)
			if !partyMapTooHardHere(m, mv.Level+partyMVPOverLv-1) || partyMapTooHardHereMVPOnly(m, mv.Level+partyMVPOverLv) {
				t.Errorf("%s: MVP rule wrong", m)
			}
		}
	}
	if mv := mapMVP("pay_fild10"); mv == nil || mv.Name != "Eddga" {
		t.Errorf("pay_fild10 should carry Eddga, got %v", mv)
	}
	if n == 0 {
		t.Error("no MVP maps found")
	}
}

// partyMapTooHardHereMVPOnly: the MVP part of partyMapTooHardHere alone.
func partyMapTooHardHereMVPOnly(m string, lv int) bool {
	mv := mapMVP(m)
	return mv != nil && lv < mv.Level+partyMVPOverLv
}
