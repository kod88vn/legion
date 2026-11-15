package main

// Class skill kits: what each class gets seeded with, and which skill the AI uses in combat.
// UseSkillToId packet 0x0438: u16 skill_lv, u16 skill_id, u32 target.

import (
	"fmt"
	"math/rand"
	"sort"
	"strings"
)

type SkillUse struct {
	ID, Lv    int
	SP        int  // approximate SP cost at that level
	Range     int  // cells
	CastMs    int  // approximate cast + after-cast delay (for pacing)
	Self      bool // self-target (heal)
	Elemental bool // bolt: pick element against the target
}

const (
	nvBasic   = 1
	smBash    = 5
	mgCold    = 14
	mgFire    = 19
	mgLight   = 20
	alHeal    = 28
	smProvoke = 6 // Swordman: Provoke (taunt)
	smMagnum  = 7 // Swordman: Magnum Break (area damage around self)
	mcMammo   = 42
	acDouble  = 46
	tfDouble  = 48
)

// skills seeded per class (id -> level); passives included
var classSkills = map[int]map[int]int{
	1: {smBash: 10}, 7: {smBash: 10}, 14: {smBash: 10},
	2: {mgCold: 7, mgFire: 7, mgLight: 7}, 9: {mgCold: 10, mgFire: 10, mgLight: 10}, 16: {mgCold: 10, mgFire: 10, mgLight: 10},
	3: {acDouble: 10}, 11: {acDouble: 10},
	4: {alHeal: 7}, 8: {alHeal: 10}, 15: {alHeal: 5},
	5: {mcMammo: 10}, 10: {mcMammo: 10}, 18: {mcMammo: 10},
	6: {tfDouble: 10}, 12: {tfDouble: 10}, 17: {tfDouble: 10},
}

// combat skill choice per class line
func attackSkill(class int) *SkillUse {
	switch class {
	case 1, 7, 14:
		return &SkillUse{ID: smBash, Lv: 10, SP: 15, Range: 1, CastMs: 700}
	case 2, 9, 16:
		lv := 7
		if class != 2 {
			lv = 10
		}
		return &SkillUse{ID: mgFire, Lv: lv, SP: 12 + 2*lv, Range: 9, CastMs: 500 + lv*450, Elemental: true}
	case 3, 11:
		return &SkillUse{ID: acDouble, Lv: 10, SP: 12, Range: 9, CastMs: 900}
	case 5, 10, 18:
		return &SkillUse{ID: mcMammo, Lv: 10, SP: 5, Range: 1, CastMs: 900}
	}
	return nil
}

func healSkill(class int) *SkillUse {
	switch class {
	case 4, 8, 15:
		lv := classSkills[class][alHeal]
		return &SkillUse{ID: alHeal, Lv: lv, SP: 10 + 3*lv, Range: 9, CastMs: 1000, Self: true}
	}
	return nil
}

// boltFor picks the bolt element that counters the monster's element (Fire->Cold, Water->Lightning, else Fire;
// Earth/Undead are weak to Fire).
func boltFor(m *Mob) int {
	if m == nil {
		return mgFire
	}
	switch m.Element {
	case "Fire":
		return mgCold
	case "Water":
		return mgLight
	case "Wind":
		return mgFire
	}
	return mgFire
}

// boltFor (b.mu held): the counter bolt this character has really learned (build review 2026-11-14: Mages without
// Cold Bolt had it requested against Fire monsters and refused; Wind monsters - weak to Earth - got Fire Bolt even
// from Wizards/Sages with Earth Spike). Falls back to Fire Bolt; the level is capped at the learned level.
func (b *Bot) boltFor(m *Mob, lv int) (int, int) {
	learned := func(id int) int {
		if si := b.skills[id]; si != nil {
			return si.Lv
		}
		return 0
	}
	want := boltFor(m)
	if m != nil && m.Element == "Wind" && learned(wzEarthSpike) > 0 {
		want = wzEarthSpike
	}
	if learned(want) <= 0 {
		want = mgFire
	}
	if l := learned(want); l > 0 {
		lv = min(lv, l)
	}
	return want, lv
}

const wzEarthSpike = 90

func (b *Bot) useSkill(id, lv int, target uint32) {
	if b.escHolding(id) {
		return // a wing is being used: another skill would cancel its teleport (escapeitem.go)
	}
	b.send(newPkt(0x0438).u16(uint16(lv)).u16(uint16(id)).u32(target).b)
}

func skillSQL(c CharSpec) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "INSERT INTO skill (char_id,id,lv,flag) VALUES (%d,%d,9,0)", c.CharID, nvBasic)
	sk := map[int]int{}
	for id, lv := range classSkills[c.Class] {
		sk[id] = lv
	}
	switch c.Role {
	case "vendor": // Pushcart, Vending, Enlarge Weight Limit, Discount, Overcharge
		for id, lv := range map[int]int{mcPushcart: 10, mcVending: 10, 36: 10, 37: 10, 38: 10} {
			sk[id] = lv
		}
	case "support": // Heal, Blessing, Increase AGI, Divine Protection, Angelus
		for id, lv := range map[int]int{alHeal: 10, alBlessing: 10, alIncAgi: 10, 22: 10, 33: 5} {
			sk[id] = lv
		}
	}
	if family(c.Class) == famAcolyte {
		sk[alBlessing], sk[alIncAgi] = max(sk[alBlessing], 5), max(sk[alIncAgi], 5)
	}
	for id, lv := range sk {
		fmt.Fprintf(&sb, ",(%d,%d,%d,0)", c.CharID, id, lv)
	}
	sb.WriteString(";\n")
	return sb.String()
}

// shopStock: tradeable loot for vendor carts (Etc items with a sane price, some cards, potions)
func shopStock() (s struct{ etc, cards []int }) {
	for id, it := range items {
		switch {
		case it.Type == "Etc" && it.Buy >= 20 && it.Buy <= 20000 && it.Weight <= 50:
			s.etc = append(s.etc, id)
		case it.Type == "Card" && id >= 4001 && id <= 4499:
			s.cards = append(s.cards, id)
		}
	}
	sort.Ints(s.etc)
	sort.Ints(s.cards)
	return
}

func cartSQL(c CharSpec, stock struct{ etc, cards []int }, r *rand.Rand) string {
	var sb strings.Builder
	sb.WriteString("INSERT INTO cart_inventory (char_id,nameid,amount,identify) VALUES ")
	first := true
	add := func(id, n int) {
		if !first {
			sb.WriteString(",")
		}
		first = false
		fmt.Fprintf(&sb, "(%d,%d,%d,1)", c.CharID, id, n)
	}
	for i := 0; i < 5+r.Intn(5) && len(stock.etc) > 0; i++ {
		add(stock.etc[r.Intn(len(stock.etc))], 5+r.Intn(60))
	}
	for i := 0; i < r.Intn(3) && len(stock.cards) > 0; i++ {
		add(stock.cards[r.Intn(len(stock.cards))], 1)
	}
	add([]int{501, 502, 503, 504}[r.Intn(4)], 20+r.Intn(80))
	sb.WriteString(";\n")
	return sb.String()
}
