package main

import (
	"encoding/binary"
	"testing"
)

// weightPacket: the server's own status update 0x00b0 with type 24 (weight) and the given value.
func weightPacket(w int) []byte {
	pk := make([]byte, 8)
	binary.LittleEndian.PutUint16(pk[0:], 0x00b0)
	binary.LittleEndian.PutUint16(pk[2:], 24)
	binary.LittleEndian.PutUint32(pk[4:], uint32(w))
	return pk
}

// onStorageOpen only queues the moves: the trip flag must follow the weight the server sends after them, not the
// stale weight held when the store opened (storage.go). A store that cleared the bag must not hold the next
// heavy-bag trip back by heavyTripGap (ai.go).
func TestStoreSettlesTripFromServerWeight(t *testing.T) {
	items[90930] = &Item{ID: 90930, Name: "TestStoreCard", Type: "Card", Weight: 1}
	newBot := func(weight int) *Bot {
		b := &Bot{P: &Persona{BaseLevel: 60, Chars: []CharSpec{{Name: "storesettle"}}}, MapName: "prontera"}
		b.MaxWeight, b.Weight, b.Zeny = 1000, weight, 5000
		b.inv = map[int]*InvItem{5: {Index: 5, ItemID: 90930, Count: 1}}
		b.kafra = &kafraTrip{Store: true, Town: "prontera", Stage: "store"}
		b.lastTripOverLine = true
		return b
	}

	// the store cleared the bag: 900 (over the line) before, 100 after the server's weight update
	b := newBot(900)
	if len(b.storeCandidates()) == 0 {
		t.Fatal("test setup: the bag needs an item to store")
	}
	b.onStorageOpen()
	if !b.lastTripOverLine || !b.storeSettle {
		t.Fatal("the moves are only queued: the flag waits for the server's weight, it must not read 900 now")
	}
	b.handle(weightPacket(100))
	if b.lastTripOverLine || b.storeSettle {
		t.Fatalf("a store that cleared the bag must clear lastTripOverLine (line %v, settle %v)", b.lastTripOverLine, b.storeSettle)
	}

	// the store did not clear the bag: still over the line after the server's weight update
	b = newBot(900)
	b.onStorageOpen()
	b.handle(weightPacket(950))
	if !b.lastTripOverLine {
		t.Fatal("a store that left the bag over the line keeps the heavy-trip gap")
	}

	// nothing to store: no weight update will come, so the flag is decided at once
	b = newBot(100)
	b.inv = map[int]*InvItem{}
	b.kafra = &kafraTrip{Store: true, Town: "prontera", Stage: "store"}
	b.lastTripOverLine = true
	b.onStorageOpen()
	if b.lastTripOverLine || b.storeSettle {
		t.Fatal("an empty store step settles at once: the bag is under the line")
	}
}
