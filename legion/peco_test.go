package main

import (
	"testing"
	"time"
)

// Owner 2026-11-14: "why the knight is not riding peco is still a puzzle". Riding 1 is enough (Cavalier Mastery 0-2
// kept most Knights lv66+ on foot), and a solo Knight with no Peco makes one trip to Prontera for it.
func TestPecoRidingOneIsEnough(t *testing.T) {
	b := &Bot{P: &Persona{BaseLevel: 70, Chars: []CharSpec{{Name: "pecotest"}}}, Class: 7, invSeen: true, optionSeen: true,
		Zeny: 5000, MapName: "pay_fild04", skills: map[int]*SkillInfo{63: {Lv: 1}}, buffAt: map[string]time.Time{},
		inv: map[int]*InvItem{}, equipped: map[uint32]*InvItem{}, gearFailed: map[int]bool{}}
	if _, _, ok := b.wantsPeco(time.Now()); !ok {
		t.Fatal("a Knight with Riding 1, Cavalier 0 and 5000z wants a Peco")
	}
	if b.pecoTown(time.Now()) != "prontera" {
		t.Fatal("its next trip goes to Prontera")
	}
	b.option = optRiding
	if _, _, ok := b.wantsPeco(time.Now()); ok {
		t.Fatal("already riding")
	}
	b.option, b.Zeny = 0, 2600
	if _, _, ok := b.wantsPeco(time.Now()); ok {
		t.Fatal("2,500z + 200z reserve")
	}
	if !b.pecoSaving() || b.pecoShortfall() != 100 || b.gearReserve() != b.Zeny {
		t.Fatal("short by 100z: saving, all zeny held for the Peco")
	}
	b.Zeny = 2700
	if _, _, ok := b.wantsPeco(time.Now()); !ok {
		t.Fatal("the rental + 200z is enough")
	}
}

// pecoFundRound: only guild / party mates with money to spare mail a rider its Peco money
func TestPecoFundPicksMate(t *testing.T) {
	mk := func(name, guild string, zeny int, short int) *econSnap {
		b := &Bot{P: &Persona{Chars: []CharSpec{{Name: name}}}, Online: true, Zeny: zeny, buffAt: map[string]time.Time{}}
		return &econSnap{b: b, name: name, zeny: zeny, free: true, guild: guild, pecoShort: short, pecoNeed: short > 0}
	}
	r := mk("rider", "g1", 1000, 1700)
	poor := mk("poor", "g1", 2500, 0)
	other := mk("other", "g2", 90000, 0)
	pecoFundRound([]*econSnap{r, poor, other}, time.Now())
	if _, ok := mailOuts.Load(other.b); ok {
		t.Fatal("a stranger's guild never pays")
	}
	if _, ok := mailOuts.Load(poor.b); ok {
		t.Fatal("a mate without 1,700z + fee + 2,000z keeps its money")
	}
}

// katarFundRound: a mate saving for its own core weapon never pays; a stranger's guild never pays
func TestKatarFundSkipsSavers(t *testing.T) {
	mk := func(name, guild string, zeny int) *econSnap {
		b := &Bot{P: &Persona{Chars: []CharSpec{{Name: name}}}, Online: true, Zeny: zeny, buffAt: map[string]time.Time{}}
		return &econSnap{b: b, name: name, zeny: zeny, free: true, guild: guild}
	}
	r := mk("sin", "g1", 2000)
	r.katarShort, r.coreNeed = 18000, true
	saver := mk("saver", "g1", 50000)
	saver.coreNeed = true
	other := mk("other", "g2", 90000)
	katarFundRound([]*econSnap{r, saver, other}, time.Now())
	if _, ok := mailOuts.Load(saver.b); ok {
		t.Fatal("a mate saving for its own core weapon keeps its money")
	}
	if _, ok := mailOuts.Load(other.b); ok {
		t.Fatal("a stranger's guild never pays")
	}
}
