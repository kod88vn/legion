package main

// song.go: party Bards and Dancers are support players (main's decision 2026-11-14, from the owner's "build and spell
// use are common sense"). In a party, a Bard / Dancer that has learned a song carries an instrument (Bard) or a whip
// (Dancer / Bard), keeps it in hand while partied, and keeps a song up while the party fights:
//   Bard:   A Poem of Bragi (cast time / delay: casters, healer), Assassin Cross of Sunset (ASPD), A Whistle (FLEE),
//           Apple of Idun (max HP + HP regen, when someone is hurt)
//   Dancer: Service for You (max SP, SP cost: casters, healer), Humming (HIT), Please Don't Forget Me (slows a crowd),
//           Fortune's Kiss (CRIT)
// rAthena: songs are Self-target (Duration 180 s, Please Don't Forget Me 60 s), need an instrument or whip in hand,
// block normal attacks while performing (SC_DANCING) and end when the weapon is unequipped - so the weapon never
// changes mid-song (equipPass). Musical Strike (instrument) / Throw Arrow (whip) are allowed while performing.
// Solo they keep the bow build (itemScore: the bow wins outside a party).

import (
	"time"
)

const (
	baWhistle    = 319
	baACoS       = 320
	baBragi      = 321
	baApple      = 322
	baMusStrike  = 316
	dcHumming    = 327
	dcPDFM       = 328
	dcFortune    = 329
	dcService    = 330
	dcThrowArrow = 324
)

var songSkills = []int{baBragi, baACoS, baWhistle, baApple, dcService, dcHumming, dcPDFM, dcFortune}

// songDur: how long a song lasts (skill_db Duration1).
func songDur(id int) time.Duration {
	if id == dcPDFM {
		return 60 * time.Second
	}
	return 180 * time.Second
}

func isPerformer(class int) bool {
	c := class
	if b, t := transToSecond[c]; t {
		c = b
	}
	return c == 19 || c == 20
}

// songWeapon: a weapon a song can be played with (instrument or whip).
func songWeapon(x *Item) bool { return x != nil && (x.Sub == "Musical" || x.Sub == "Whip") }

// songSupport (b.mu held): a partied Bard/Dancer that knows a song: plays support (instrument/whip in hand).
func (b *Bot) songSupport() bool {
	if !isPerformer(b.Class) || partyOf(b) == nil {
		return false
	}
	if p := b.plan(); p != nil && (p.ID == "bard_arrow" || p.ID == "dancer_arrow") {
		return false // run 5 arrow variants: bow DPS in a party too, no support singing
	}
	for _, id := range songSkills {
		if si := b.skills[id]; si != nil && si.Lv > 0 {
			return true
		}
	}
	return false
}

// performing (b.mu held): a song is (believed to be) playing.
func (b *Bot) performing(now time.Time) bool { return b.songID != 0 && now.Before(b.songUntil) }

// songWeaponOwned (b.mu held): an instrument/whip this character can wear, in hand or in the bag.
func (b *Bot) songWeaponOwned() bool {
	for _, it := range b.inv {
		if x := items[it.ItemID]; songWeapon(x) && (it.Equipped || b.wearable(it, x)) {
			return true
		}
	}
	return false
}

// songBuy: the instrument/whip a song-support character buys first (Prontera's weapon shop sells both).
func (b *Bot) songBuy() int {
	if c, t := transToSecond[b.Class]; t && c == 20 || b.Class == 20 {
		return 1950 // Rope 2,500z
	}
	return 1901 // Violin 4,000z
}

// pickSong (b.mu held): the song the party needs now, among the learned ones.
func (b *Bot) pickSong(members []pubState, now time.Time) int {
	casters, hurt := false, false
	for _, s := range members {
		if s.Name == b.charName() || !s.Online || s.Map != b.MapName {
			continue
		}
		if s.Support || s.Fragile {
			casters = true
		}
		if s.HPFrac < 0.6 {
			hurt = true
		}
	}
	crowd := 0
	for _, e := range b.ents {
		if e.Type == objMob {
			if x, y := e.pos(now); dist(b.X, b.Y, x, y) <= 7 {
				crowd++
			}
		}
	}
	var pref []int
	if hurt {
		pref = append(pref, baApple)
	}
	if crowd >= 3 {
		pref = append(pref, dcPDFM)
	}
	if casters {
		pref = append(pref, baBragi, dcService)
	}
	pref = append(pref, baACoS, dcHumming, baBragi, dcService, baWhistle, dcFortune, baApple)
	for _, id := range pref {
		if si := b.skills[id]; si != nil && si.Lv > 0 && now.After(b.skillFailUntil[id]) {
			return id
		}
	}
	return 0
}

// songThink (b.mu held, from partyThink once the member is with its leader): start a song when the party fights,
// keep it up, follow the leader while playing, and use the attack skill allowed while performing. true = handled.
func (b *Bot) songThink(gm *GameMap, lead pubState, members []pubState, moving bool, now time.Time) bool {
	if !b.songSupport() {
		b.songID = 0
		return false
	}
	cur := b.equipped[0x2]
	if cur == nil || !songWeapon(items[cur.ItemID]) {
		b.songID = 0
		return false // equipPass puts the instrument / whip in hand first
	}
	if b.songID != 0 {
		// the server refused it (0x0110), or SP ran dry: the song is over
		if t, ok := b.skillFailUntil[b.songID]; ok && !t.Before(b.songAt.Add(60*time.Second)) || b.SP < 3 {
			b.songUntil = time.Time{}
		}
	}
	lx, ly := livePosOf(lead)
	fight := lead.Target != 0 || b.membersFighting(members) || b.fightOn(now)
	if b.performing(now) {
		if lead.Name != b.charName() && dist(b.X, b.Y, lx, ly) > 4 && !moving && gm != nil {
			b.stepAlong(gm, lx, ly)
		}
		// Musical Strike / Throw Arrow at a monster hitting the party (allowed while performing; normal attacks aren't)
		atk := baMusStrike
		if items[cur.ItemID].Sub == "Whip" {
			atk = dcThrowArrow
		}
		if si := b.skills[atk]; si != nil && si.Lv > 0 && b.arrowCount() > 0 && now.After(b.skillReady) &&
			b.SP >= si.SP+20 && now.After(b.skillFailUntil[atk]) {
			if e := b.defendTarget(members, now); e != nil {
				if x, y := e.pos(now); dist(b.X, b.Y, x, y) <= 9 {
					b.skillReady = now.Add(time.Second)
					go b.useSkill(atk, si.Lv, e.ID)
				}
			}
		}
		b.target = 0
		b.setState("playing " + skillName(b.songID))
		return true
	}
	b.songID = 0
	if !fight || b.Sitting || now.Before(b.skillReady) || now.Before(b.buffAt["song:next"]) || b.MaxSP <= 0 ||
		b.SP*100/b.MaxSP < 30 || b.SP < 60 || dist(b.X, b.Y, lx, ly) > 8 {
		return false
	}
	id := b.pickSong(members, now)
	if id == 0 {
		return false
	}
	lv := b.skills[id].Lv
	b.songID, b.songAt, b.songUntil = id, now, now.Add(songDur(id))
	b.buffAt["song:next"] = now.Add(8 * time.Second)
	b.skillReady = now.Add(time.Second)
	b.target = 0
	b.setState("playing " + skillName(id))
	go b.useSkill(id, lv, b.aid)
	return true
}

// songWeaponNeed (b.mu held): a partied Bard / Dancer that knows a song but owns no instrument / whip buys one - the
// cheapest (Violin 4,000z / Rope 2,500z; Prontera and Comodo weapon shops) - as its class step (builds.go
// classWeaponNeed: bestGearIn buys it first, the party run goes where it is sold). QA 2026-11-14: 0 song casts - all
// 7 online partied singers held only a Bow. Until it is bought every other gear purchase waits (gearHold: songSaving).
func (b *Bot) songWeaponNeed() (*Item, *Shop) {
	if !b.songSaving() {
		return nil, nil
	}
	id := b.songBuy()
	it := items[id]
	if it == nil || it.Buy <= 0 || b.Zeny-songReserve < it.Buy {
		return nil, nil
	}
	if s := b.gearShopNear(id); s != nil {
		return it, s
	}
	if l := gearShops(id); len(l) > 0 {
		return it, l[0]
	}
	return nil, nil
}

// songReserve: zeny kept for potions when buying the song weapon
const songReserve = 500

// songSaving (b.mu held): a singer still without its instrument / whip (a failed purchase stops it).
func (b *Bot) songSaving() bool {
	return b.invReady() && b.songSupport() && !b.songWeaponOwned() && !b.gearFailed[b.songBuy()]
}
