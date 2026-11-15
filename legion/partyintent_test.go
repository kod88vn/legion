package main

import (
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
