package main

import (
	"encoding/binary"
	"testing"
	"time"
)

func efstPkt(op uint16, efst int, aid uint32, on bool, remainMS int32) []byte {
	pk := make([]byte, 29)
	binary.LittleEndian.PutUint16(pk, op)
	binary.LittleEndian.PutUint16(pk[2:], uint16(efst))
	binary.LittleEndian.PutUint32(pk[4:], aid)
	if on {
		pk[8] = 1
	}
	switch op {
	case 0x043f:
		binary.LittleEndian.PutUint32(pk[9:], uint32(remainMS))
	case 0x0983:
		binary.LittleEndian.PutUint32(pk[9:], uint32(remainMS)) // total
		binary.LittleEndian.PutUint32(pk[13:], uint32(remainMS))
	}
	return pk
}

func TestEffectsOfBot(t *testing.T) {
	b := &Bot{aid: 42, Online: true}
	now := time.Now()
	b.efstRecv(0x0983, efstPkt(0x0983, 10, 42, true, 240000), now)  // Blessing 240 s
	b.efstRecv(0x043f, efstPkt(0x043f, 19, 42, true, 9999), now)    // Kyrie, no timer
	b.efstRecv(0x0196, efstPkt(0x0196, 12, 42, true, 0), now)       // Increase AGI, no time
	b.efstRecv(0x0983, efstPkt(0x0983, 20, 7, true, 30000), now)    // someone else's Magnificat: ignored
	b.efstRecv(0x0983, efstPkt(0x0983, 9999, 42, true, 30000), now) // unknown id: not shown
	b.opt2, b.opt2At = 0x1|0x4, now                                 // poisoned + silenced
	got := b.effectsOf(now.Add(10 * time.Second))
	want := map[string]int{"Blessing": 230, "Kyrie Eleison": -1, "Increase AGI": -1, "Poisoned": -1, "Silenced": -1}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	if !got[0].Debuff || !got[1].Debuff || got[2].Debuff {
		t.Fatalf("debuffs not first: %+v", got)
	}
	for _, e := range got {
		if w, ok := want[e.Name]; !ok || w != e.LeftS {
			t.Fatalf("unexpected %+v", e)
		}
	}
	// the icon goes off; Blessing runs out by time even with a missed off packet
	b.efstRecv(0x0196, efstPkt(0x0196, 19, 42, false, 0), now)
	b.opt2 = 0
	got = b.effectsOf(now.Add(300 * time.Second))
	if len(got) != 1 || got[0].Name != "Increase AGI" {
		t.Fatalf("after off/expiry: %+v", got)
	}
	b.Online = false
	if got = b.effectsOf(now); len(got) != 0 {
		t.Fatalf("offline: %+v", got)
	}
	efstBook.Lock()
	_, kept := efstBook.m[b]
	efstBook.Unlock()
	if kept {
		t.Fatal("offline book not cleared")
	}
}

func TestHumanEffectsView(t *testing.T) {
	now := time.Now()
	s := humanStatus{TS: now.Add(-3 * time.Second).Unix(), Effects: []humanEffect{
		{Handle: "EFST_BLESSING", Name: "Blessing", LeftS: 100},
		{Handle: "HEALTHSTATE_POISON", Name: "Poisoned", LeftS: -1},
		{Handle: "EFST_INC_AGI", Name: "Increase Agility", LeftS: 1}, // ran out since the file was written
		{Handle: "AL_HEAL_DELAY", LeftS: 1},
		{Handle: "EFST_ASSUMPTIO", LeftS: -1}, // no name: from the handle
	}}
	got := humanEffectsView(s, now)
	if len(got) != 3 || got[0].Name != "Poisoned" || !got[0].Debuff {
		t.Fatalf("got %+v", got)
	}
	if got[1].Name != "Blessing" || got[1].LeftS < 96 || got[1].LeftS > 97 || got[2].Name != "assumptio" {
		t.Fatalf("got %+v", got)
	}
}
