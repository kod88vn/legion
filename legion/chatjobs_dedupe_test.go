package main

import (
	"testing"
	"time"
)

func askItem(speaker, channel string, itemID int, text string) ChatJobSpec {
	return ChatJobSpec{Kind: ChatKindAsk, Channel: channel, Hearers: []string{"Mate"},
		Facts:    map[string]any{"item": "Shoes", "item_id": itemID},
		Template: []ChatLine{{Speaker: speaker, Channel: channel, Text: text}}}
}

func openJobs() int {
	chatJobs.Lock()
	defer chatJobs.Unlock()
	return len(chatJobs.open)
}

// the same ask for the same item from two guild speakers is queued once while the first one waits
func TestChatAskDedupe(t *testing.T) {
	setupChatJobs(t, true)
	now := time.Now()
	chatJobsPolled(now)
	if id := enqueueChatJobAt(askItem("Asker", "guild", 2301, "anyone got shoes?"), now); id == "" {
		t.Fatal("the first ask waits")
	}
	first := chatJobs.open[0].ID
	if id := enqueueChatJobAt(askItem("Other", "guild", 2301, "guild, spare shoes?"), now); id != first {
		t.Fatalf("the same ask again is not queued: it returns the waiting ask %s, got %q", first, id)
	}
	if n := openJobs(); n != 1 {
		t.Fatalf("open jobs = %d, want 1", n)
	}
	// another item, and the same item in another channel, are other asks
	if id := enqueueChatJobAt(askItem("Asker", "guild", 2302, "anyone got a clip?"), now); id == "" {
		t.Fatal("another item waits")
	}
	if id := enqueueChatJobAt(askItem("Asker", "party", 2301, "party, shoes?"), now); id == "" {
		t.Fatal("the same item in party chat waits")
	}
	if n := openJobs(); n != 3 {
		t.Fatalf("open jobs = %d, want 3", n)
	}
}

// once the first ask is answered (no longer waiting), the same item may be asked again
func TestChatAskDedupeAfterAnswer(t *testing.T) {
	setupChatJobs(t, true)
	now := time.Now()
	chatJobsPolled(now)
	id := enqueueChatJobAt(askItem("Asker", "guild", 2301, "anyone got shoes?"), now)
	if _, _, err := chatAnswer(id, nil, known, now); err != nil {
		t.Fatal(err)
	}
	if got := enqueueChatJobAt(askItem("Other", "guild", 2301, "guild, spare shoes?"), now); got == "" {
		t.Fatal("after the answer a new ask waits")
	}
}

// the flag off keeps the old behaviour: every ask is queued
func TestChatAskDedupeOff(t *testing.T) {
	setupChatJobs(t, true)
	withTuning(t, func(tn *Tuning) {
		tn.ChatJobsOn, tn.ChatJobWaitMinSec, tn.ChatJobWaitMaxSec, tn.ChatJobDedupeOn = true, 20, 40, false
	})
	now := time.Now()
	chatJobsPolled(now)
	enqueueChatJobAt(askItem("Asker", "guild", 2301, "anyone got shoes?"), now)
	enqueueChatJobAt(askItem("Other", "guild", 2301, "guild, spare shoes?"), now)
	if n := openJobs(); n != 2 {
		t.Fatalf("flag off: open jobs = %d, want 2", n)
	}
}

// a bot's main character wins the name over another bot's alt, whatever the order of the bots
func TestCharIndexMainWins(t *testing.T) {
	monk := &Bot{P: &Persona{Chars: []CharSpec{{Name: "kaiz72", Class: 25}}}}
	priest := &Bot{P: &Persona{Chars: []CharSpec{{Name: "kaiz72Prist", Class: 8}, {Name: "kaiz72", Class: 8}}}}
	for _, order := range [][]*Bot{{monk, priest}, {priest, monk}} {
		idx := charIndex(order)
		if idx["kaiz72"] != monk {
			t.Fatalf("kaiz72 must be the Monk main, got another bot")
		}
		if idx["kaiz72prist"] != priest {
			t.Fatalf("the alt still resolves to its own bot")
		}
	}
}

// two guilds asking for the same item at the same time are two asks: the second guild's ask is queued and said
func TestChatAskDedupeOtherGroup(t *testing.T) {
	setupChatJobs(t, true)
	now := time.Now()
	chatJobsPolled(now)
	ask := func(group string, hearers []string, speaker string) ChatJobSpec {
		s := askItem(speaker, "guild", 2301, "anyone got shoes?")
		s.Group, s.Hearers = group, hearers
		return s
	}
	if id := enqueueChatJobAt(ask("guild:g1", []string{"A", "B"}, "A"), now); id == "" {
		t.Fatal("guild 1 asks")
	}
	if id := enqueueChatJobAt(ask("guild:g2", []string{"C", "D"}, "C"), now); id == "" {
		t.Fatal("guild 2 asks for the same item: it is its own ask, not a duplicate")
	}
	if n := openJobs(); n != 2 {
		t.Fatalf("open jobs = %d, want 2 (one per guild)", n)
	}
	// the same guild again is the duplicate
	if id := enqueueChatJobAt(ask("guild:g1", []string{"B", "A"}, "B"), now); id != chatJobs.open[0].ID {
		t.Fatalf("guild 1 again is a duplicate of its waiting ask, got %q", id)
	}
	if n := openJobs(); n != 2 {
		t.Fatalf("open jobs = %d, want 2", n)
	}
}
