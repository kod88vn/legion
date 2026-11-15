package main

import (
	"encoding/binary"
	"fmt"
	"testing"
	"time"
)

func pbPriest(name string, sp int) *Bot {
	return &Bot{P: &Persona{Name: name, Chars: []CharSpec{{Name: name}}}, Class: 8, HP: 1000, MaxHP: 1000,
		SP: sp, MaxSP: 1000, MapName: "pay_dun01", X: 50, Y: 50, aid: 1,
		skills: map[int]*SkillInfo{alHeal: {Lv: 10, SP: 40}, alBlessing: {Lv: 10, SP: 64}, alIncAgi: {Lv: 10, SP: 47},
			prKyrie: {Lv: 10, SP: 35}, prMagnificat: {Lv: 3, SP: 40}},
		buffAt: map[string]time.Time{}, ents: map[uint32]*Entity{}}
}

func TestBuffAskParse(t *testing.T) {
	cases := map[string][]int{
		"Echobunny : bless pls":           {alBlessing},
		"Echobunny : agi?":                {alIncAgi},
		"Zen.fox : buff pls":              {alBlessing, alIncAgi},
		"vex_x18 : kyrie + buffs pls":     {prKyrie, alBlessing, alIncAgi},
		"Zen.fox : sp low, magnificat?":   {prMagnificat},
		"Ayla : need a priest for gef":    nil,
		"Ayla : inc! 5 mobs":              nil,
		"Ayla : lets go guys":             nil,
		"no separator bless":              nil,
		"Ayla : no agi needed, im ranged": nil,
	}
	for msg, want := range cases {
		_, got := buffAskParse(msg)
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Errorf("%q: got %v want %v", msg, got, want)
		}
	}
}

// PT29: the old floor kept buffs above 70% SP; the priest now buffs down to the hold line (30%) and a seated priest
// stands for an unasked buff only above 60%.
func TestActiveBuffFloor(t *testing.T) {
	now := time.Now()
	b := pbPriest("zz_pbfloor", 500) // 50% SP: the old code held every buff here
	si := b.skills[alBlessing]
	if !b.activeBuffOK(si, now) {
		t.Fatal("standing at 50% SP: buff")
	}
	b.SP = 340 // 34% -> 27.6% after the cast
	if b.activeBuffOK(si, now) {
		t.Fatal("would drop under the 30% hold line: keep SP for Heal")
	}
	b.SP, b.Sitting = 500, true
	if b.activeBuffOK(si, now) {
		t.Fatal("seated resting at 50%: an unasked buff waits")
	}
	b.buffAt["buffreq"] = now.Add(time.Minute)
	if !b.activeBuffOK(si, now) {
		t.Fatal("seated but asked: get up and buff")
	}
	b.buffAt["buffgap"] = now.Add(time.Second)
	if b.activeBuffOK(si, now) {
		t.Fatal("inside the buff gap")
	}
}

func TestBuffCandsPriority(t *testing.T) {
	now := time.Now()
	b := pbPriest("zz_pbprio", 900)
	members := []pubState{
		{Name: "zz_pbprio", Map: "pay_dun01", X: 50, Y: 50, Online: true, HPFrac: 1, Support: true, Back: true},
		{Name: "zz_ranged", Map: "pay_dun01", X: 52, Y: 50, Online: true, HPFrac: 1, Back: true},
		{Name: "zz_melee", Map: "pay_dun01", X: 52, Y: 51, Online: true, HPFrac: 1},
		{Name: "zz_tank", Map: "pay_dun01", X: 53, Y: 50, Online: true, HPFrac: 1, Tank: true},
		{Name: "zz_far", Map: "pay_dun01", X: 70, Y: 50, Online: true, HPFrac: 1},
	}
	c := b.buffCands(nil, members, false, now)
	if len(c) == 0 || c[0].name != "zz_tank" || c[0].skill != prKyrie {
		t.Fatalf("tank Kyrie first, got %+v", c)
	}
	order := []string{}
	for _, x := range c {
		if x.skill == alBlessing {
			order = append(order, x.name)
		}
		if x.name == "zz_far" {
			t.Fatal("20 cells away: out of reach")
		}
	}
	if fmt.Sprint(order) != "[zz_tank zz_melee zz_ranged zz_pbprio]" {
		t.Fatalf("Blessing order tank > melee > ranged > self, got %v", order)
	}
	b.buffAskHeard("zz_ranged : agi pls", now)
	c = b.buffCands(nil, members, false, now)
	if c[0].name != "zz_ranged" || c[0].skill != alIncAgi {
		t.Fatalf("an ask goes to the front, got %+v", c[0])
	}
	b.buffAt[fmt.Sprintf("%d:zz_melee", alBlessing)] = now.Add(time.Minute) // still on
	for _, x := range b.buffCands(nil, members, false, now) {
		if x.name == "zz_melee" && x.skill == alBlessing {
			t.Fatal("Blessing still running on zz_melee: not due")
		}
	}
	// a dead member loses its buffs in the priest's book
	members[2].HPFrac = 0
	b.buffCands(nil, members, false, now)
	if _, ok := b.buffAt[fmt.Sprintf("%d:zz_melee", alBlessing)]; ok {
		t.Fatal("dead member: buff record cleared")
	}
	// own SP under 50%: Magnificat
	b.SP = 450
	found := false
	for _, x := range b.buffCands(nil, members, false, now) {
		found = found || x.skill == prMagnificat
	}
	if !found {
		t.Fatal("priest under 50% SP with Magnificat: cast it")
	}
}

func TestBuffRecvAndWant(t *testing.T) {
	now := time.Now()
	k := &Bot{P: &Persona{Name: "zz_pbknight", Chars: []CharSpec{{Name: "zz_pbknight"}}}, Class: 7, HP: 1000, MaxHP: 1000,
		MapName: "pay_dun01", aid: 7, buffAt: map[string]time.Time{}, ents: map[uint32]*Entity{}}
	if k.buffWant(now) == "" {
		t.Fatal("no buffs: ask")
	}
	pk := make([]byte, 17)
	binary.LittleEndian.PutUint16(pk, 0x09cb)
	binary.LittleEndian.PutUint16(pk[2:], alBlessing)
	binary.LittleEndian.PutUint32(pk[4:], 10)
	binary.LittleEndian.PutUint32(pk[8:], 7)
	binary.LittleEndian.PutUint32(pk[12:], 1)
	pk[16] = 1
	k.buffRecv(0x09cb, pk, now)
	binary.LittleEndian.PutUint16(pk[2:], alIncAgi)
	k.buffRecv(0x09cb, pk, now)
	if w := k.buffWant(now); w != "" {
		t.Fatalf("both buffs on: nothing to ask, got %q", w)
	}
	if w := k.buffWant(now.Add(241 * time.Second)); w == "" {
		t.Fatal("expired after 240 s: ask")
	}
	// status icon off (EFST_BLESSING 10) ends Blessing early
	st := make([]byte, 9)
	binary.LittleEndian.PutUint16(st, 0x0196)
	binary.LittleEndian.PutUint16(st[2:], 10)
	binary.LittleEndian.PutUint32(st[4:], 7)
	k.buffRecv(0x0196, st, now)
	w := k.buffWant(now)
	if _, got := buffAskParse("zz_pbknight : " + w); len(got) != 1 || got[0] != alBlessing {
		t.Fatalf("Blessing ended: ask for bless only, said %q", w)
	}
	// a priest sees another caster buff a party member: no double cast
	p := pbPriest("zz_pbseer", 900)
	p.ents[7] = &Entity{ID: 7, Type: objPC, Name: "zz_pbknight"}
	binary.LittleEndian.PutUint16(pk[2:], alBlessing)
	binary.LittleEndian.PutUint32(pk[12:], 99)
	p.buffRecv(0x09cb, pk, now)
	if !p.buffAt[fmt.Sprintf("%d:zz_pbknight", alBlessing)].After(now.Add(200 * time.Second)) {
		t.Fatal("someone else's Blessing on a member: renew it later")
	}
}

func TestBuffAskThrottle(t *testing.T) {
	now := time.Now()
	k := &Bot{P: &Persona{Name: "zz_pbask", Chars: []CharSpec{{Name: "zz_pbask"}}}, Class: 7, HP: 1000, MaxHP: 1000,
		MapName: "pay_dun01", X: 50, Y: 50, aid: 9, buffAt: map[string]time.Time{}}
	p := &Party{Name: "zz_pbparty"}
	ms := []pubState{{Name: "zz_pbpr", Map: "pay_dun01", X: 52, Y: 50, Online: true, HPFrac: 1, Support: true}}
	k.buffAsk(p, ms, now)
	if !k.buffAt["buffask"].IsZero() {
		t.Fatal("first notice: give the priest time before asking")
	}
	k.buffAsk(p, ms, now.Add(25*time.Second))
	if k.buffAt["buffask"].IsZero() {
		t.Fatal("lacking 25 s with the priest next to it: ask")
	}
	next := k.buffAt["buffask"]
	if next.Sub(now.Add(25*time.Second)) < 90*time.Second {
		t.Fatal("next ask at least 90 s later")
	}
	k2 := &Bot{P: &Persona{Name: "zz_pbask2", Chars: []CharSpec{{Name: "zz_pbask2"}}}, Class: 7, HP: 1000, MaxHP: 1000,
		MapName: "pay_dun01", X: 50, Y: 50, aid: 10, buffAt: map[string]time.Time{"asklack": now}}
	k2.buffAsk(p, ms, now.Add(30*time.Second))
	if !k2.buffAt["buffask"].IsZero() {
		t.Fatal("the party asked 5 s ago: wait")
	}
	ms[0].Resting = true
	k3 := &Bot{P: &Persona{Name: "zz_pbask3", Chars: []CharSpec{{Name: "zz_pbask3"}}}, Class: 7, HP: 1000, MaxHP: 1000,
		MapName: "pay_dun01", X: 50, Y: 50, aid: 11, buffAt: map[string]time.Time{"asklack": now}}
	k3.buffAsk(&Party{}, ms, now.Add(60*time.Second))
	if !k3.buffAt["buffask"].IsZero() {
		t.Fatal("the priest rests for SP: don't nag")
	}
}
