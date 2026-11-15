package main

import "testing"

func TestKafraMeetDone(t *testing.T) {
	ms := []pubState{{Name: "a", Map: "geffen", X: 120, Y: 60, Online: true}, {Name: "b", Map: "geffen", X: 121, Y: 62, Online: true}, {Name: "c", Map: "gef_fild07", X: 1, Y: 1, Online: true}}
	if !kafraMeetDone(ms, "a", "geffen", 120, 62) {
		t.Fatal("both in geffen at the Kafra, c still on the field: met")
	}
	ms[1].X = 160
	if kafraMeetDone(ms, "a", "geffen", 120, 62) {
		t.Fatal("b still shopping 40 cells away: not met")
	}
}

func TestPartyWingStock(t *testing.T) {
	if !partyWingStock(true, 0) || !partyWingStock(true, 1) || partyWingStock(true, 2) || partyWingStock(false, 0) {
		t.Fatal("party members keep 2 wings ahead of gear savings")
	}
}

func TestNoAheadOfLeader(t *testing.T) {
	if !noAheadOfLeader("pay_arche", "pay_dun00") {
		t.Fatal("on her field, the next dungeon is ahead of her: stay")
	}
	if noAheadOfLeader("pay_arche", "pay_arche") || noAheadOfLeader("payon", "pay_arche") {
		t.Fatal("same map, or she is in town: no override")
	}
}
