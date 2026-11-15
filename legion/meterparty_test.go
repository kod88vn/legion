package main

import "testing"

// b1794711155fmf: every party bot carries the placeholder PartyID 1; two different parties must not be pooled.
func TestMeterPartyCreditByName(t *testing.T) {
	// party A: 90 fair kills, 9000 z over 60 member-minutes; party B: 30 fair kills, 3000 z over 30 member-minutes;
	// lone: the placeholder id and no name, keeps its own numbers
	a1 := row("a1", 1, 30, 90, 9000, 0, 0)
	a2 := row("a2", 1, 30, 0, 0, 0, 0)
	b1 := row("b1", 1, 30, 30, 3000, 0, 0)
	lone := row("lone", 1, 30, 15, 0, 0, 0)
	a1.Party, a2.Party, b1.Party = "Alpha", "alpha", "Bravo"
	meterDerive([]*meterRow{a1, a2, b1, lone}, [mwN]float64{5, 5, 5})
	for _, r := range []*meterRow{a1, a2} {
		if s := r.stat[mw60]; s.KPM != 1.5 || s.ZPH != 9000 {
			t.Fatalf("%s %+v", r.Name, s)
		}
	}
	if s := b1.stat[mw60]; s.KPM != 1 || s.ZPH != 6000 {
		t.Fatalf("b1 %+v", s)
	}
	if s := lone.stat[mw60]; s.KPM != 0.5 || s.KPMAll != 0.533 {
		t.Fatalf("lone %+v", s)
	}
	if lone.cohort != "swordman|41-60|party" {
		t.Fatalf("cohort %s", lone.cohort)
	}
}

func TestMeterPartyKey(t *testing.T) {
	cases := []struct {
		name string
		id   uint32
		want string
	}{{"", 0, ""}, {"", 1, ""}, {" Alpha ", 1, "n:alpha"}, {"", 42, "#42"}, {"Bravo", 42, "n:bravo"}}
	for _, c := range cases {
		if got := meterPartyKey(c.name, c.id); got != c.want {
			t.Errorf("%q/%d: %q want %q", c.name, c.id, got, c.want)
		}
	}
}

// meterNeo: Ayla's party is the core rows plus main rows in the SAME party, not every party bot.
func TestMeterNeoPartyByName(t *testing.T) {
	core := row("core1", 1, 30, 10, 0, 0, 0)
	core.Party, core.Instance = "Neo", "core"
	mate := row("mate", 1, 30, 10, 0, 0, 0)
	mate.Party, mate.Instance = "neo", "main"
	other := row("other", 1, 30, 10, 0, 0, 0)
	other.Party, other.Instance = "Elsewhere", "main"
	rows := []*meterRow{core, mate, other}
	meterDerive(rows, [mwN]float64{5, 5, 5})
	v := &meterView{Rows: rows}
	out := meterNeo(v, mw60)
	if len(out.Members) != 2 {
		t.Fatalf("members %+v", out.Members)
	}
	for _, m := range out.Members {
		if m.Name == "other" {
			t.Fatal("a different party counted as Ayla's")
		}
	}
}
