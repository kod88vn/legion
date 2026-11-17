package main

import (
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const (
	simRoleMap       = "sim_role_fild"
	simRoleTankAID   = 9101
	simRoleHunterAID = 9102
	simRoleMageAID   = 9103
	simRolePriestAID = 9104
	simRoleMobID     = 9201
	simRoleMobClass  = 9202
	simRoleWeaponID  = 9203
)

func simRoleBot(name, planID string, aid uint32, x, y int) *Bot {
	plan := builds[planID]
	b := stickBot(name, simRoleMap, x, y)
	b.P.Chars[0].Plan = planID
	b.P.Chars[0].BaseLevel = 70
	b.P.Chars[0].JobLevel = 50
	b.Class = plan.Class
	b.BaseLv = 70
	b.aid = aid
	b.HP, b.MaxHP = 100, 100
	b.SP, b.MaxSP = 500, 500
	b.skills = map[int]*SkillInfo{}
	for _, goal := range plan.Skills {
		b.skills[goal.ID] = &SkillInfo{ID: goal.ID, Lv: goal.Lv, SP: 1, Range: 9}
	}
	b.ents = map[uint32]*Entity{}
	b.buffAt = map[string]time.Time{}
	b.skillFailUntil = map[int]time.Time{}
	b.equipped = map[uint32]*InvItem{}
	return b
}

func simRolePublish(b *Bot, hpFrac float64) {
	role := partyRole(b)
	v := &atomic.Value{}
	v.Store(pubState{
		Name: b.charName(), Map: b.MapName, X: b.X, Y: b.Y, Online: true, HPFrac: hpFrac,
		AID: b.aid, Lv: b.BaseLv, Power: b.BaseLv, Known: true,
		Support: role == roleSupport, Tank: role == roleTank, Back: role == roleRanged || role == roleSupport,
	})
	pubs.Store(b.charName(), v)
}

func simRoleParty(t *testing.T, tankPlan string) (tank, hunter, mage, priest *Bot) {
	t.Helper()
	tank = simRoleBot("SimTank", tankPlan, simRoleTankAID, 100, 100)
	hunter = simRoleBot("SimHunter", "hunter_blitz", simRoleHunterAID, 104, 100)
	mage = simRoleBot("SimMage", "wizard_frost", simRoleMageAID, 103, 101)
	priest = simRoleBot("SimPriest", "priest_me", simRolePriestAID, 101, 100)
	stickParty(t, tank, hunter, mage, priest)
	simRolePublish(tank, 1)
	simRolePublish(hunter, 1)
	simRolePublish(mage, 1)
	simRolePublish(priest, 1)
	return
}

func simRoleMob(id uint32, x, y int, target uint32, now time.Time) *Entity {
	return &Entity{
		ID: id, Type: objMob, Class: simRoleMobClass, X: x, Y: y, DX: x, DY: y,
		Target: target, TargetAt: now,
	}
}

func TestSimPartyTankBuildsPeelAndUseTheirCrowdSkill(t *testing.T) {
	cases := []struct {
		name, planID, weaponSub string
		skillID                 int
	}{
		{name: "two-hand sword", planID: "knight", weaponSub: "2hSword", skillID: 62},
		{name: "spear variant", planID: "knight_spear", weaponSub: "2hSpear", skillID: 57},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tank, _, _, priest := simRoleParty(t, tc.planID)
			oldItem, hadItem := items[simRoleWeaponID]
			items[simRoleWeaponID] = &Item{ID: simRoleWeaponID, Type: "Weapon", Loc: 0x22, Sub: tc.weaponSub}
			t.Cleanup(func() {
				if hadItem {
					items[simRoleWeaponID] = oldItem
				} else {
					delete(items, simRoleWeaponID)
				}
			})
			tank.equipped[0x2] = &InvItem{ItemID: simRoleWeaponID, Equipped: true}

			now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
			tank.ents[simRoleMobID] = simRoleMob(simRoleMobID, 101, 100, priest.aid, now)
			tank.ents[simRoleMobID+1] = simRoleMob(simRoleMobID+1, 102, 100, tank.aid, now)
			tank.ents[simRoleMobID+2] = simRoleMob(simRoleMobID+2, 103, 100, tank.aid, now)
			tank.buffAt[fmt.Sprintf("prov:%d", simRoleMobID)] = now.Add(time.Second)

			_, _, members := tank.partyView(partyOf(tank), now)
			if !tank.tankTakeAggro(members, false, now) {
				t.Fatal("tank did not act on the monster attacking the priest")
			}
			if tank.target != simRoleMobID {
				t.Fatalf("tank target = %d, want peeled monster %d", tank.target, simRoleMobID)
			}
			if got := tank.State; !strings.Contains(got, "area skill "+itoa(tc.skillID)+" on a group") {
				t.Fatalf("tank state = %q, want build's crowd skill %d after the peel", got, tc.skillID)
			}
		})
	}
}

func TestSimPartyFrostWizardUsesBuildSkillAgainstWaterCrowd(t *testing.T) {
	_, _, mage, _ := simRoleParty(t, "knight")
	oldMob, hadMob := mobs[simRoleMobClass]
	mobs[simRoleMobClass] = &Mob{ID: simRoleMobClass, Name: "Simulation Water Mob", Level: 70, HP: 1000, Exp: 100, Element: "Water"}
	t.Cleanup(func() {
		if hadMob {
			mobs[simRoleMobClass] = oldMob
		} else {
			delete(mobs, simRoleMobClass)
		}
	})

	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	mage.ents[simRoleMobID] = simRoleMob(simRoleMobID, 104, 101, 0, now)
	mage.ents[simRoleMobID+1] = simRoleMob(simRoleMobID+1, 105, 101, 0, now)
	mage.ents[simRoleMobID+2] = simRoleMob(simRoleMobID+2, 106, 101, 0, now)

	if !mage.aoeCast(mage.ents[simRoleMobID], now) {
		t.Fatal("frost wizard did not cast on the grouped water monsters")
	}
	if got := mage.State; !strings.Contains(got, "Lord of Vermilion on a group") {
		t.Fatalf("wizard state = %q, want build's lightning counter-element AoE", got)
	}
}

func TestSimPartyPriestHealsTheMostInjuredVisibleRole(t *testing.T) {
	tank, hunter, mage, priest := simRoleParty(t, "knight")
	simRolePublish(tank, 0.58)
	simRolePublish(hunter, 0.71)
	simRolePublish(mage, 0.63)
	simRolePublish(priest, 1)
	caster := priest.charName()
	healStats.Lock()
	oldCount, hadCount := healPend.n[caster]
	oldTarget, hadTarget := healPend.target[caster]
	delete(healPend.n, caster)
	delete(healPend.target, caster)
	healStats.Unlock()
	t.Cleanup(func() {
		healStats.Lock()
		if hadCount {
			healPend.n[caster] = oldCount
		} else {
			delete(healPend.n, caster)
		}
		if hadTarget {
			healPend.target[caster] = oldTarget
		} else {
			delete(healPend.target, caster)
		}
		healStats.Unlock()
	})
	priest.ents = map[uint32]*Entity{
		tank.aid:   {ID: tank.aid, Type: objPC, Name: tank.charName(), X: tank.X, Y: tank.Y},
		hunter.aid: {ID: hunter.aid, Type: objPC, Name: hunter.charName(), X: hunter.X, Y: hunter.Y},
		mage.aid:   {ID: mage.aid, Type: objPC, Name: mage.charName(), X: mage.X, Y: mage.Y},
	}

	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if !priest.healFirst(false, now) {
		t.Fatal("priest did not heal an injured party member")
	}
	if got := priest.State; !strings.Contains(got, "healing SimTank") {
		t.Fatalf("priest state = %q, want the lowest-HP member SimTank", got)
	}
}

func TestSimPartyRangedMemberProtectsSupportOnlyWhenFreeToReposition(t *testing.T) {
	protTune(t, true)
	for _, tc := range []struct {
		name          string
		hunterX, mobX int
		engaged       bool
	}{
		{name: "keeps current melee fight", hunterX: 106, mobX: 107, engaged: true},
		{name: "repositions when chased", hunterX: 110, mobX: 114},
	} {
		t.Run(tc.name, func(t *testing.T) {
			priest := simRoleBot("ProtectPriest", "priest_me", simRolePriestAID, 100, 100)
			hunter := simRoleBot("ProtectHunter", "hunter_blitz", simRoleHunterAID, tc.hunterX, 100)
			stickParty(t, priest, hunter)
			simRolePublish(priest, 1)
			simRolePublish(hunter, 1)
			hunter.ents[simRoleMobID] = simRoleMob(simRoleMobID, tc.mobX, 100, hunter.aid, time.Now())
			if tc.engaged {
				hunter.target = simRoleMobID
			}
			gm := &GameMap{Name: simRoleMap, W: 200, H: 200, cells: make([]byte, 200*200)}
			members := []pubState{}
			for _, member := range []*Bot{priest, hunter} {
				state, ok := peek(member.charName())
				if !ok {
					t.Fatalf("missing published state for %s", member.charName())
				}
				members = append(members, state)
			}
			if got := hunter.protectOwnSupport(partyOf(hunter), members, gm, true, time.Now()); got == tc.engaged {
				t.Fatalf("protectOwnSupport() = %v, engaged=%v; want protection only when not already fighting", got, tc.engaged)
			}
			if tc.engaged && hunter.target != simRoleMobID {
				t.Fatalf("engaged ranged target = %d, want current monster %d", hunter.target, simRoleMobID)
			}
		})
	}
}
