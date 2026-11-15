package main

import (
	"testing"
	"time"
)

// main 2026-11-14: partied Bards / Dancers are support players: the instrument / whip outranks the bow in a party (not
// solo), the song fits the party, a song starts only while the party fights, and the weapon never changes mid-song.
func TestSongSupport(t *testing.T) {
	items[1701] = &Item{ID: 1701, Name: "Bow", Type: "Weapon", Sub: "Bow", Loc: 0x22, Atk: 15, Buy: 1000}
	items[1901] = &Item{ID: 1901, Name: "Violin", Type: "Weapon", Sub: "Musical", Loc: 0x2, Atk: 50, Buy: 4000}
	bow := &InvItem{Index: 2, ItemID: 1701, Count: 1, Loc: 0x22, Equipped: true}
	vio := &InvItem{Index: 3, ItemID: 1901, Count: 1, Loc: 0x2, Equipped: false}
	b := &Bot{P: &Persona{Name: "zz_songbard", BaseLevel: 60, Chars: []CharSpec{{Name: "zz_songbard"}}}, Class: 19, invSeen: true,
		MapName: "pay_fild04", X: 50, Y: 50, SP: 300, MaxSP: 400, HP: 1000, MaxHP: 1000,
		skills: map[int]*SkillInfo{baBragi: {Lv: 5}, baACoS: {Lv: 3}}, buffAt: map[string]time.Time{},
		inv: map[int]*InvItem{2: bow, 3: vio}, equipped: map[uint32]*InvItem{0x22: bow}, gearFailed: map[int]bool{}}
	if b.songSupport() {
		t.Fatal("solo: the bow build stays")
	}
	if b.itemScore(items[1701]) <= b.itemScore(items[1901]) {
		t.Fatal("solo: the bow outranks the violin")
	}
	lead := &Bot{P: &Persona{Name: "zz_songlead", Chars: []CharSpec{{Name: "zz_songlead"}}}}
	p := &Party{Name: "zz_songparty", Leader: lead, Members: []*Bot{lead, b}}
	parties.Lock()
	parties.ofBot[lead], parties.ofBot[b] = p, p
	parties.Unlock()
	defer func() {
		parties.Lock()
		delete(parties.ofBot, lead)
		delete(parties.ofBot, b)
		parties.Unlock()
	}()
	if !b.songSupport() || b.itemScore(items[1901]) <= b.itemScore(items[1701]) {
		t.Fatal("partied Bard with a song: the instrument outranks the bow")
	}
	now := time.Now()
	members := []pubState{{Name: "zz_songlead", Map: "pay_fild04", Online: true, HPFrac: 1}}
	if got := b.pickSong(members, now); got != baACoS {
		t.Fatalf("no casters: Assassin Cross (ASPD), got %d", got)
	}
	members = append(members, pubState{Name: "zz_songpriest", Map: "pay_fild04", Online: true, HPFrac: 1, Support: true})
	if got := b.pickSong(members, now); got != baBragi {
		t.Fatalf("a healer in the party: Bragi, got %d", got)
	}
	// with the violin in hand and the party fighting, a song starts; no fight -> none
	bow.Equipped, vio.Equipped = false, true
	b.equipped = map[uint32]*InvItem{0x2: vio}
	lead2 := pubState{Name: "zz_songlead", Map: "pay_fild04", X: 52, Y: 52, Online: true, HPFrac: 1}
	b.aid = 0 // no connection: useSkill in a goroutine would send - keep it from running by skillReady
	b.skillReady = now.Add(time.Hour)
	if b.songThink(nil, lead2, members, false, now) {
		t.Fatal("skill delay running: no song")
	}
	b.songID, b.songAt, b.songUntil = baBragi, now, now.Add(songDur(baBragi))
	if !b.performing(now) {
		t.Fatal("performing")
	}
	if b.equipPass(true) {
		t.Fatal("never swap weapons mid-song")
	}
}

// run 4: a partied singer with only a Bow saves for its instrument (gearHold) and buys it as its class step
func TestSongWeaponNeed(t *testing.T) {
	items[1701] = &Item{ID: 1701, Name: "Bow", Type: "Weapon", Sub: "Bow", Loc: 0x22, Atk: 15, Buy: 1000}
	items[1901] = &Item{ID: 1901, Name: "Violin", Type: "Weapon", Sub: "Musical", Loc: 0x2, Atk: 50, Buy: 4000}
	bow := &InvItem{Index: 2, ItemID: 1701, Count: 1, Loc: 0x22, Equipped: true}
	b := &Bot{P: &Persona{Name: "zz_songbuy", BaseLevel: 60, Chars: []CharSpec{{Name: "zz_songbuy"}}}, Class: 19, invSeen: true,
		Zeny: 3000, skills: map[int]*SkillInfo{baBragi: {Lv: 2}}, buffAt: map[string]time.Time{},
		inv: map[int]*InvItem{2: bow}, equipped: map[uint32]*InvItem{0x22: bow}, gearFailed: map[int]bool{}}
	if b.songSaving() {
		t.Fatal("solo: no song weapon needed")
	}
	lead := &Bot{P: &Persona{Name: "zz_songbuylead", Chars: []CharSpec{{Name: "zz_songbuylead"}}}}
	p := &Party{Name: "zz_songbuyparty", Leader: lead, Members: []*Bot{lead, b}}
	parties.Lock()
	parties.ofBot[lead], parties.ofBot[b] = p, p
	parties.Unlock()
	defer func() {
		parties.Lock()
		delete(parties.ofBot, lead)
		delete(parties.ofBot, b)
		parties.Unlock()
	}()
	if !b.songSaving() || !b.gearHold() {
		t.Fatal("partied singer with a Bow only: saving for the Violin, other gear waits")
	}
	if it, _ := b.songWeaponNeed(); it != nil {
		t.Fatal("3,000z: can't pay the Violin + reserve yet")
	}
	b.inv[3] = &InvItem{Index: 3, ItemID: 1901, Count: 1, Loc: 0x2}
	if b.songSaving() {
		t.Fatal("owns a Violin: done")
	}
}

// run 4: a Knight with Riding, on foot and short of the rental, saves for the Peco (other gear waits)
func TestPecoSaving(t *testing.T) {
	b := &Bot{P: &Persona{Chars: []CharSpec{{Name: "pecosave"}}}, Class: 7, optionSeen: true, Zeny: 2000,
		skills: map[int]*SkillInfo{63: {Lv: 1}}}
	if !b.pecoSaving() {
		t.Fatal("saving for the Peco")
	}
	b.Zeny = 4000
	if b.pecoSaving() {
		t.Fatal("has the money: rents, no saving")
	}
	b.Zeny, b.option = 2000, optRiding
	if b.pecoSaving() {
		t.Fatal("already riding")
	}
}
