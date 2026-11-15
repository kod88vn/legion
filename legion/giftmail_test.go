package main

import (
	"testing"
	"time"
)

// 2026-11-14 (Ayla's party): a party gear gift through the trade window kept Echobunny "waiting for Zendancer84 to
// trade" for minutes while the priest fought 34 cells away. Gifts now go by mail: no walking, no waiting, any
// distance, and the raised zeny is booked when the mail is sent.
func TestGearGiftByMail(t *testing.T) {
	donor := &Bot{P: &Persona{Name: "giftd", Chars: []CharSpec{{Name: "GiftDonor"}}}, Online: true, Zeny: 5000, MapName: "prt_fild08", X: 10, Y: 10}
	rcpt := &Bot{P: &Persona{Name: "giftr", Chars: []CharSpec{{Name: "GiftRcpt"}}}, Online: true, Zeny: 100, MapName: "prt_fild08", X: 150, Y: 150}
	it := &Item{ID: 1701, Name: "Bow", Buy: 1000}
	defer mailOuts.Delete(donor)
	if !startGift(donor, rcpt, "GiftDonor", "GiftRcpt", 900, it, time.Now()) {
		t.Fatal("a mail gift needs no meeting: it must start at any distance")
	}
	v, ok := mailOuts.Load(donor)
	if !ok {
		t.Fatal("no mail started")
	}
	m := v.(*mailOut)
	if m.To != "GiftRcpt" || m.Amount != 900 {
		t.Fatalf("mail to %q for %d", m.To, m.Amount)
	}
	if rcpt.mkt != nil || donor.mkt != nil {
		t.Fatal("a mail gift opens no trade (nobody waits)")
	}
	gearPools.Lock()
	before := gearPools.raised["GiftRcpt"]
	gearPools.Unlock()
	donor.mu.Lock()
	donor.endMail(m, true, "")
	donor.mu.Unlock()
	gearPools.Lock()
	after := gearPools.raised["GiftRcpt"]
	delete(gearPools.raised, "GiftRcpt")
	delete(gearPools.funded, "GiftRcpt")
	gearPools.Unlock()
	if after-before != 900 {
		t.Fatalf("raised %d -> %d, want +900", before, after)
	}
}
