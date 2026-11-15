package main

// coreweapon.go: the weapon a build's core skill needs comes before any other gear (build-and-strategy run 6).
//
// Measured 2026-11-14 13:40: 19 of 19 online Assassins with Sonic Blow learned (most at 10) held a Main Gauche /
// Falchion / Knife - not one katar - while their gear_next was a Hat, Shoes or a Muffler. Sonic Blow (and Katar
// Mastery, and the katar's double crit) need a katar; without one an Assassin auto-attacks with a dagger and casts
// Envenom. A sensible player saves for the Jur (19,500z, the cheapest katar) right after the job change and buys
// nothing else first. Same frame as the singer's instrument (song.go songSaving) and the rider's Peco (peco.go
// pecoSaving): while saving, gearHold stops every other gear purchase, and classWeaponNeed returns the weapon (the
// cheapest one of the type it can equip) the moment the purse covers it plus the potion reserve.
//
// Cases (only once the core skill is learned, so a fresh Thief still levels with its dagger):
//   - Sonic Blow (AS_SONICBLOW 136) on a katar plan (assassin / assassin_sonic): a Katar;
//   - Two-Hand Quicken (KN_TWOHANDQUICKEN 60) on the knight plan: a two-handed sword (Katana 2,000z);
//   - Pierce / Brandish Spear (56 / 57) on a spear plan, Spear Quicken (258) on crusader_spear: a spear.

const coreReserve = 500 // zeny kept for potions when the core weapon is bought

// coreWeaponList: NPC-sold weapons of the core types, tried when the plan's own route has none it can equip: katars
// (Jur, Katar, Jamadhar), Katana (two-handed sword), spears (Javelin, Spear, Pike, Guisarme, Glaive).
var coreWeaponList = []int{1250, 1251, 1254, 1116, 1401, 1404, 1407, 1451, 1454}

// coreWeaponSubs (b.mu held): the weapon SubTypes the build's learned core skill needs (nil = no such need).
func (b *Bot) coreWeaponSubs() []string {
	p := b.plan()
	if p == nil {
		return nil
	}
	has := func(id int) bool { si := b.skills[id]; return si != nil && si.Lv > 0 }
	switch {
	case (p.ID == "assassin" || p.ID == "assassin_sonic") && family(b.Class) == famThief && has(136):
		return []string{"Katar"}
	case p.ID == "knight" && family(b.Class) == famSword && has(60):
		return []string{"2hSword"}
	case (p.ID == "knight_spear" || p.ID == "knight_pierce") && family(b.Class) == famSword && (has(56) || has(57)):
		return []string{"1hSpear", "2hSpear"}
	case p.ID == "crusader_spear" && family(b.Class) == famSword && has(258):
		return []string{"2hSpear"}
	}
	return nil
}

func subIn(sub string, l []string) bool {
	for _, s := range l {
		if s == sub {
			return true
		}
	}
	return false
}

// coreWeaponOwned (b.mu held): a weapon of the core type in hand or in the bag (wearable, or one the character could
// wear once identified).
func (b *Bot) coreWeaponOwned(subs []string) bool {
	for _, it := range b.inv {
		x := items[it.ItemID]
		if x == nil || it.Count <= 0 || gearSlot(x.Loc) != 0x2 || !subIn(x.Sub, subs) {
			continue
		}
		if it.Equipped || b.canEquip(x) {
			return true
		}
	}
	return false
}

// coreWeaponBuy (b.mu held): the cheapest NPC-sold weapon of the core type the character can equip (the plan's route
// first), 0 = none sold.
func (b *Bot) coreWeaponBuy(subs []string) int {
	best, price := 0, 0
	try := func(id int) {
		x := items[id]
		if x == nil || x.Buy <= 0 || !subIn(x.Sub, subs) || gearSlot(x.Loc) != 0x2 || !b.canEquip(x) || b.gearFailed[id] {
			return
		}
		if len(gearShops(id)) == 0 {
			return
		}
		if best == 0 || x.Buy < price {
			best, price = id, x.Buy
		}
	}
	if p := b.plan(); p != nil {
		for _, id := range p.Weapons {
			try(id)
		}
	}
	if best == 0 {
		for _, id := range coreWeaponList {
			try(id)
		}
	}
	return best
}

// coreSaving (b.mu held): the build's core skill is learned but its weapon type is missing: the zeny goes to it first.
func (b *Bot) coreSaving() bool {
	if !b.invReady() {
		return false
	}
	subs := b.coreWeaponSubs()
	if subs == nil || b.coreWeaponOwned(subs) {
		return false
	}
	return b.coreWeaponBuy(subs) != 0
}

// coreWeaponNeed (b.mu held): the core weapon and its nearest shop once the purse covers it (else nil: keep saving).
func (b *Bot) coreWeaponNeed() (*Item, *Shop) {
	subs := b.coreWeaponSubs()
	if subs == nil || b.coreWeaponOwned(subs) {
		return nil, nil
	}
	id := b.coreWeaponBuy(subs)
	it := items[id]
	if it == nil || b.Zeny-coreReserve < it.Buy {
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

// katarShortfall (b.mu held): zeny an Assassin saving for its katar still lacks (katar + coreReserve; 0 = none).
// peco.go katarFundRound: guild / party mates mail part of it.
func (b *Bot) katarShortfall() int {
	subs := b.coreWeaponSubs()
	if len(subs) != 1 || subs[0] != "Katar" || b.coreWeaponOwned(subs) {
		return 0
	}
	it := items[b.coreWeaponBuy(subs)]
	if it == nil {
		return 0
	}
	return max(0, it.Buy+coreReserve-b.Zeny)
}
