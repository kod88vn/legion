package main

import "testing"

func TestEffectiveLv(t *testing.T) {
	knife, pike, rod, staff := &Item{Atk: 17, Sub: "Dagger"}, &Item{Atk: 60, Sub: "1hSpear"}, &Item{Atk: 15, Matk: 30}, &Item{Atk: 40, Matk: 70}
	cases := []struct {
		name   string
		f, lv  int
		w      *Item
		def    int
		lo, hi int
	}{
		{"novice lv5 knife", famNovice, 5, knife, 10, 5, 5},
		{"swordman lv40 knife cotton", famSword, 40, knife, 10, 22, 28},
		{"swordman lv40 pike adv suit", famSword, 40, pike, 20, 34, 38},
		{"thief lv25 knife", famThief, 25, knife, 10, 16, 20},
		{"mage lv40 rod", famMage, 40, rod, 10, 31, 36},
		{"mage lv40 staff", famMage, 40, staff, 20, 37, 40},
		{"bare hands lv30", famSword, 30, nil, 0, 15, 18},
	}
	for _, c := range cases {
		if got := effectiveLv(c.f, c.lv, c.w, c.def); got < c.lo || got > c.hi {
			t.Errorf("%s: effectiveLv = %d, want %d-%d", c.name, got, c.lo, c.hi)
		}
	}
}
