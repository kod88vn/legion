package main

import "testing"

func lvAll(lvs map[int]int) func(int) (int, int) {
	return func(id int) (int, int) {
		l := lvs[id]
		if l == 0 {
			return 0, 0
		}
		return l, 40
	}
}

func always(int) bool { return true }

func group(x, y, n int) []aoeMob {
	var ms []aoeMob
	for i := 0; i < n; i++ {
		ms = append(ms, aoeMob{e: &Entity{ID: uint32(100 + i)}, x: x + i%2, y: y + i/2})
	}
	return ms
}

func TestAoEClusterOfThree(t *testing.T) {
	ms := group(10, 10, 3)
	i, cx, cy, _ := pickAoE(ms, 4, 10, lvAll(map[int]int{mgThunder: 5, mgFireBall: 5}), 200, 300, false, true, always)
	if i < 0 || aoeSpells[i].id != mgThunder {
		t.Fatalf("3 grouped monsters: want Thunder Storm, got %d", i)
	}
	if dist(cx, cy, 10, 10) > 2 {
		t.Fatalf("centre %d,%d not on the group", cx, cy)
	}
	// two monsters only: single target
	if i, _, _, _ := pickAoE(group(10, 10, 2), 4, 10, lvAll(map[int]int{mgThunder: 5}), 200, 300, false, true, always); i >= 0 {
		t.Fatal("2 monsters must not trigger an area spell")
	}
	// spread out (6 cells apart): no group
	spread := []aoeMob{{e: &Entity{}, x: 5, y: 5}, {e: &Entity{}, x: 11, y: 5}, {e: &Entity{}, x: 5, y: 11}}
	if i, _, _, _ := pickAoE(spread, 6, 6, lvAll(map[int]int{mgThunder: 5}), 200, 300, false, true, always); i >= 0 {
		t.Fatal("spread monsters are not a group")
	}
}

func TestAoEBestSpellAndReserve(t *testing.T) {
	ms := group(10, 10, 5)
	lv := lvAll(map[int]int{wzStormGust: 8, mgFireBall: 5, wzHeavenDrv: 5})
	if i, _, _, _ := pickAoE(ms, 5, 10, lv, 300, 400, false, false, always); aoeSpells[i].id != wzStormGust {
		t.Fatalf("a Wizard with Storm Gust casts it on a group, got %d", aoeSpells[i].id)
	}
	// SG on cooldown -> Heaven's Drive
	notSG := func(id int) bool { return id != wzStormGust }
	if i, _, _, _ := pickAoE(ms, 5, 10, lv, 300, 400, false, false, notSG); aoeSpells[i].id != wzHeavenDrv {
		t.Fatalf("want Heaven's Drive when Storm Gust is cooling down, got %d", aoeSpells[i].id)
	}
	// SP reserve: 60 SP of 400 -> 40-SP spells would leave 20 < 60 reserve
	if i, _, _, _ := pickAoE(ms, 5, 10, lv, 60, 400, false, false, always); i >= 0 {
		t.Fatal("must keep the SP reserve")
	}
	// being hit without a tank: only quick spells (no Storm Gust)
	if i, _, _, _ := pickAoE(ms, 5, 10, lv, 300, 400, true, true, always); i < 0 || aoeSpells[i].id == wzStormGust {
		t.Fatal("being hit: want a quick spell")
	}
}

func TestAoENoKSNoSwarm(t *testing.T) {
	ms := group(10, 10, 4)
	ms[1].foreign = true
	if i, _, _, _ := pickAoE(ms, 5, 10, lvAll(map[int]int{mgThunder: 5}), 200, 300, false, false, always); i >= 0 {
		t.Fatal("a group with someone else's monster must not be hit (KS)")
	}
	sw := group(10, 10, 8)
	for k := range sw {
		sw[k].aggro = true
	}
	if i, _, _, _ := pickAoE(sw, 5, 10, lvAll(map[int]int{mgThunder: 5}), 200, 300, false, true, always); i >= 0 {
		t.Fatal("solo: an aggressive swarm of 8 is not AoE'd")
	}
	if i, _, _, _ := pickAoE(sw, 5, 10, lvAll(map[int]int{mgThunder: 5}), 200, 300, false, false, always); i < 0 {
		t.Fatal("in a party the swarm may be AoE'd")
	}
	b := group(10, 10, 3)
	b[0].risky = true
	if i, _, _, _ := pickAoE(b, 5, 10, lvAll(map[int]int{mgThunder: 5}), 200, 300, false, true, always); i >= 0 {
		t.Fatal("solo: no AoE on a group with a boss")
	}
}

// Renewal skill tree (re/skill_tree.yml) for the Mage and Wizard skills the plan uses: id -> prerequisites
var mageWizTree = map[int]map[int]int{
	10: {}, 11: {}, 14: {}, 15: {14: 5}, 16: {}, 17: {19: 4}, 18: {17: 5, 10: 1}, 19: {}, 20: {}, 21: {20: 4},
	81: {20: 1, 10: 1}, 83: {81: 2, 21: 1}, 84: {11: 1, 20: 1}, 85: {21: 1, 84: 5}, 89: {15: 1, 84: 3}, 90: {16: 1},
	91: {90: 3}, 92: {91: 1},
}

func simPlan(goals []SkillGoal, learnable func(int) bool, pts int) map[int]int {
	lv := map[int]int{}
	for ; pts > 0; pts-- {
		picked := false
		for _, g := range goals {
			if !learnable(g.ID) || lv[g.ID] >= g.Lv {
				continue
			}
			ok := true
			for p, l := range mageWizTree[g.ID] {
				if lv[p] < l {
					ok = false
				}
			}
			if ok {
				lv[g.ID]++
				picked = true
				break
			}
		}
		if !picked {
			break
		}
	}
	return lv
}

func TestWizardPlanAoE(t *testing.T) {
	p := builds["wizard"]
	mageOnly := func(id int) bool { return id < 80 }
	for _, g := range p.Skills {
		if _, ok := mageWizTree[g.ID]; !ok {
			t.Fatalf("skill %d not in the tree", g.ID)
		}
	}
	// a new Mage at job 50 (49 points): Fire Ball, Thunder Storm, Fire Wall learned; every point spent
	m := simPlan(p.Skills, mageOnly, 49)
	sum := 0
	for _, v := range m {
		sum += v
	}
	if sum != 49 || m[mgFireBall] < 5 || m[mgThunder] < 5 || m[mgFireWall] < 5 {
		t.Fatalf("mage at job 50: %v (%d pts)", m, sum)
	}
	// a Mage gets its first area spell early (by 20 points)
	if m20 := simPlan(p.Skills, mageOnly, 20); m20[mgFireBall] == 0 && m20[mgThunder] == 0 {
		t.Fatalf("no area spell by 20 points: %v", m20)
	}
	// Wizard: Storm Gust 10, LoV 10, Heaven's Drive, Meteor within 49 more points
	w := simPlan(p.Skills, func(int) bool { return true }, 49+49)
	if w[wzStormGust] < 10 || w[wzVermilion] < 10 || w[wzHeavenDrv] < 5 || w[wzMeteor] < 1 {
		t.Fatalf("wizard: %v", w)
	}
}

// Existing bolt Mages (built under the Legacy plan) must stay valid: every first-job level they can hold is within
// the new plan's caps, and their next points go to area spells.
func TestLegacyMagesStayValid(t *testing.T) {
	p := builds["wizard"]
	caps := map[int]int{}
	for _, g := range p.Skills {
		caps[g.ID] = max(caps[g.ID], g.Lv)
	}
	for _, g := range p.Legacy {
		if g.ID < 80 && caps[g.ID] < g.Lv {
			t.Fatalf("legacy goal %d lv%d above the new cap %d: old Mages would look like wrong builds", g.ID, g.Lv, caps[g.ID])
		}
	}
	// a legacy Mage after 30 points (bolts 10/10/10): the next 9 points go to Fire Ball and Thunder Storm
	old := simPlan(p.Legacy, func(id int) bool { return id < 80 }, 30)
	next := map[int]int{}
	for k, v := range old {
		next[k] = v
	}
	cont := simPlanFrom(p.Skills, next, func(id int) bool { return id < 80 }, 9)
	if cont[mgFireBall]-old[mgFireBall] < 4 || cont[mgThunder]-old[mgThunder] < 4 {
		t.Fatalf("old mage %v grew into %v: want area spells", old, cont)
	}
}

func simPlanFrom(goals []SkillGoal, start map[int]int, learnable func(int) bool, pts int) map[int]int {
	lv := map[int]int{}
	for k, v := range start {
		lv[k] = v
	}
	add := simPlanStart(goals, lv, learnable, pts)
	return add
}

func simPlanStart(goals []SkillGoal, lv map[int]int, learnable func(int) bool, pts int) map[int]int {
	for ; pts > 0; pts-- {
		picked := false
		for _, g := range goals {
			if !learnable(g.ID) || lv[g.ID] >= g.Lv {
				continue
			}
			ok := true
			for p, l := range mageWizTree[g.ID] {
				if lv[p] < l {
					ok = false
				}
			}
			if ok {
				lv[g.ID]++
				picked = true
				break
			}
		}
		if !picked {
			break
		}
	}
	return lv
}

// Owner 2026-11-14: "cast AOE spell at the center where it's most impactful" - the cell covering the most monsters,
// even when no monster stands on it.
func TestAoEMostImpactfulCell(t *testing.T) {
	// two pairs 6 cells apart: Storm Gust (9x9) centred between them covers all four; centred on any monster it
	// would hold only 2-3 of them... (radius 4: pairs at x=10 and x=16 -> centre x=13 covers both)
	ms := []aoeMob{
		{e: &Entity{ID: 1}, x: 10, y: 10}, {e: &Entity{ID: 2}, x: 10, y: 11},
		{e: &Entity{ID: 3}, x: 17, y: 10}, {e: &Entity{ID: 4}, x: 17, y: 11},
	}
	i, cx, _, _ := pickAoE(ms, 13, 15, lvAll(map[int]int{wzStormGust: 10}), 500, 600, false, false, always)
	if i < 0 || aoeSpells[i].id != wzStormGust {
		t.Fatalf("want Storm Gust on 4 monsters, got %d", i)
	}
	if cx < 13 || cx > 14 {
		t.Fatalf("centre x=%d: want between the pairs (13-14) so all 4 are hit", cx)
	}
	// a foreign monster at one side: the centre shifts away from it instead of giving up
	ms = append(group(10, 10, 4), aoeMob{e: &Entity{ID: 9}, x: 18, y: 10, foreign: true})
	if i, cx, _, _ := pickAoE(ms, 12, 14, lvAll(map[int]int{wzStormGust: 10}), 500, 600, false, false, always); i < 0 || dist(cx, 10, 18, 10) <= 4 {
		t.Fatalf("want a Storm Gust that leaves the foreign monster out, got %d at x=%d", i, cx)
	}
	// unwalkable cells are never targeted
	wall := func(x, y int) bool { return x == 10 && y == 10 }
	if i, cx, cy, _ := pickAoECell(group(10, 10, 4), 12, 14, lvAll(map[int]int{wzStormGust: 10}), 500, 600, false, false, always, wall); i < 0 || cx != 10 || cy != 10 {
		t.Fatalf("only cell 10,10 is allowed: got %d at %d,%d", i, cx, cy)
	}
}

func TestAoEStrongPairAndElement(t *testing.T) {
	two := group(10, 10, 2)
	if i, _, _, _ := pickAoE(two, 12, 12, lvAll(map[int]int{wzStormGust: 10}), 500, 600, false, false, always); i >= 0 {
		t.Fatal("2 weak monsters: no area spell")
	}
	two[0].strong = true
	if i, _, _, _ := pickAoE(two, 12, 12, lvAll(map[int]int{wzStormGust: 10}), 500, 600, false, false, always); i < 0 {
		t.Fatal("2 monsters with a strong one: area spell")
	}
	// a Water crowd resists Storm Gust -> the next spell (Heaven's Drive, Earth)
	ws := group(10, 10, 4)
	for k := range ws {
		ws[k].elem = "Water"
	}
	lv := lvAll(map[int]int{wzStormGust: 10, wzHeavenDrv: 5})
	if i, _, _, _ := pickAoE(ws, 12, 12, lv, 500, 600, false, false, always); i < 0 || aoeSpells[i].id != wzHeavenDrv {
		t.Fatalf("Water crowd: want Heaven's Drive, not Storm Gust; got %d", i)
	}
	// undead skeletons (Zen.fox, pay_dun01): Storm Gust is fine
	us := group(10, 10, 4)
	for k := range us {
		us[k].elem = "Undead"
	}
	if i, _, _, _ := pickAoE(us, 12, 12, lv, 500, 600, false, false, always); i < 0 || aoeSpells[i].id != wzStormGust {
		t.Fatalf("Undead crowd: want Storm Gust, got %d", i)
	}
}

// A partymate the server sent no party HP for yet is still a friend: its monsters don't block the AoE.
func TestAoEPartyFriendWithoutHP(t *testing.T) {
	a := &Bot{aid: 10, partyHP: map[uint32][2]int{}}
	k := &Bot{aid: 20, partyHP: map[uint32][2]int{}}
	p := &Party{Leader: a, Members: []*Bot{a, k}}
	parties.Lock()
	parties.ofBot[a], parties.ofBot[k] = p, p
	parties.Unlock()
	defer func() {
		parties.Lock()
		delete(parties.ofBot, a)
		delete(parties.ofBot, k)
		parties.Unlock()
	}()
	f := a.partyFriend()
	if !f(10) || !f(20) || f(30) || f(0) {
		t.Fatalf("partyFriend: self/member must be friends, strangers not")
	}
}
