package main

import (
	"testing"
	"time"
)

// party lines are logged once, with the other hearers listed (economy batch 10)
func TestPartyEventLoggedOnce(t *testing.T) {
	r := &eventRing{}
	now := time.Now()
	r.add(Event{T: now, Bot: "a", Kind: "party", Text: "x : hi"})
	r.add(Event{T: now.Add(time.Second), Bot: "b", Kind: "party", Text: "x : hi"})
	r.add(Event{T: now.Add(time.Second), Bot: "c", Kind: "chat", Text: "x : hi"})
	r.add(Event{T: now.Add(5 * time.Second), Bot: "d", Kind: "party", Text: "x : hi"})
	ev, _ := r.since(0)
	if len(ev) != 3 || len(ev[0].HeardBy) != 1 || ev[0].HeardBy[0] != "b" {
		t.Fatalf("got %+v", ev)
	}
}

func TestMailCost(t *testing.T) {
	if mailCost(3000) != 3060 {
		t.Fatal(mailCost(3000))
	}
}
