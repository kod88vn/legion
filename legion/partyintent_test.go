package main

import (
	"encoding/json"
	"testing"
	"time"
)

// partyChatReset points the chat at a temp data dir (never the live legion/data, G23).
func partyChatReset(t *testing.T) {
	old := *dataDir
	*dataDir = t.TempDir()
	t.Cleanup(func() { time.Sleep(2500 * time.Millisecond); *dataDir = old }) // let a scheduled save land in the temp dir
	partyChat.Lock()
	partyChat.byHuman, partyChat.loaded = nil, false
	partyChat.Unlock()
}

func TestPartyChatPersists(t *testing.T) {
	partyChatReset(t)
	partyChatHeard("PersistHuman", "Zen : saved line", time.Unix(2000, 0))
	time.Sleep(2500 * time.Millisecond)
	partyChat.Lock()
	partyChat.byHuman, partyChat.loaded = nil, false // a restart
	partyChat.Unlock()
	l := partyChatOf("persisthuman")
	if len(l) != 1 || l[0].Text != "saved line" || l[0].From != "Zen" {
		t.Fatalf("after reload: %+v", l)
	}
}

func TestLeadPosPick(t *testing.T) {
	now := time.Unix(20000, 0)
	st := &humanDetail{Map: "pay_dun00", X: 26, Y: 166, StatusTS: now.Unix() - 1, Fresh: true, Online: true}
	// the party data moved her after the warp (newer than status.json): the party map wins
	m, x, y := "pay_arche", 39, 131
	if src := leadPosPick(st, &m, &x, &y, now, now); src != "party_newer" || m != "pay_arche" || x != 39 {
		t.Fatalf("%s %s %d,%d", src, m, x, y)
	}
	// same map: status.json's cell (fresher than sight)
	m, x, y = "pay_dun00", 20, 175
	if src := leadPosPick(st, &m, &x, &y, now.Add(-time.Minute), now); src != "status" || x != 26 {
		t.Fatalf("%s %d", src, x)
	}
	// a party update over 2 min old on another map: status.json
	m, x, y = "payon", 0, 0
	if src := leadPosPick(st, &m, &x, &y, now.Add(-3*time.Minute), now); src != "status_newer" || m != "pay_dun00" {
		t.Fatalf("%s %s", src, m)
	}
}

func TestLeadPosSource(t *testing.T) {
	now := time.Unix(10000, 0)
	m, x, y := "pay_dun00", 0, 0 // party packets: map, no cell
	d := &humanDetail{Map: "pay_dun00", X: 166, Y: 42, StatusTS: now.Unix() - 45}
	if src := leadPosSource(d, &m, &x, &y, now); src != "status_old" || x != 166 || y != 42 {
		t.Fatalf("src %s %d,%d", src, x, y)
	}
	if !sameMap("pay_dun00.gat", "PAY_DUN00") {
		t.Fatal("sameMap")
	}
}

func TestPartyChatDedupeAndOrder(t *testing.T) {
	partyChatReset(t)
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
