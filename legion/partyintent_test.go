package main

import (
	"encoding/json"
	"testing"
	"time"
)

func TestPartyChatDedupeAndOrder(t *testing.T) {
	now := time.Unix(1000, 0)
	partyChatHeard("TestHuman", "Zen : hi", now)
	partyChatHeard("TestHuman", "Zen : hi", now.Add(time.Second)) // the same line via another friend
	partyChatHeard("TestHuman", "Ayla : lets go", now.Add(2*time.Second))
	partyChatHeard("TestHuman", "Zen : hi", now.Add(20*time.Second)) // said again later
	l := partyChatOf("testhuman")
	if len(l) != 3 || l[0].Text != "hi" || l[1].From != "Ayla" || l[1].Text != "lets go" {
		t.Fatalf("got %+v", l)
	}
	for i := 0; i < 40; i++ {
		partyChatHeard("TestHuman", "X : line"+string(rune('a'+i)), now.Add(time.Duration(30+i)*time.Second))
	}
	if n := len(partyChatOf("TestHuman")); n != partyChatKeep {
		t.Fatalf("kept %d", n)
	}
}

func TestExpSampleAndMemberJSONRoundTrip(t *testing.T) {
	t0 := time.Unix(5000, 0)
	expSample("ExpTester", 10, 100, t0)
	expSample("ExpTester", 10, 600, t0.Add(time.Minute))
	h, m := expSample("ExpTester", 11, 400, t0.Add(2*time.Minute)) // level-up: +400 of the new level
	if m != 2 || h != 27000 {
		t.Fatalf("exp/h %d over %v min", h, m)
	}
	// the main instance decodes the core's party JSON (mergeCoreParties): the embedded extras must survive
	in := []partyView{{Name: "x", Human: "H", Members: []partyMemberView{{Name: "b", PartyMemberExtras: &PartyMemberExtras{DistLead: 7, KPM: 1.5}},
		{Name: "H", Human: true, Intent: "farming", Detail: &humanDetail{humanExtras: humanExtras{Intent: "farming"}}}}}}
	raw, _ := json.Marshal(in)
	var out []partyView
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if out[0].Members[0].PartyMemberExtras == nil || out[0].Members[0].DistLead != 7 || out[0].Members[1].Intent != "farming" || out[0].Members[1].Detail.Intent != "farming" {
		t.Fatalf("round trip lost fields: %s", raw)
	}
}
