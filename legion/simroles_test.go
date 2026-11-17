package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net"
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
	b.rng = rand.New(rand.NewSource(1))
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

func simRoleCapture(t *testing.T, b *Bot) net.Conn {
	t.Helper()
	conn, peer := pipeConn()
	b.wmu.Lock()
	b.conn = conn
	b.wmu.Unlock()
	t.Cleanup(func() {
		b.endSession(conn)
		peer.Close()
	})
	return peer
}

func simRoleReadPacket(t *testing.T, peer net.Conn, size int) []byte {
	t.Helper()
	if err := peer.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	packet := make([]byte, size)
	if _, err := io.ReadFull(peer, packet); err != nil {
		t.Fatalf("read outgoing packet: %v", err)
	}
	return packet
}

func simRoleExpectNoPacket(t *testing.T, peer net.Conn) {
	t.Helper()
	if err := peer.SetReadDeadline(time.Now().Add(50 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	var packet [1]byte
	_, err := peer.Read(packet[:])
	var timeout net.Error
	if !errors.As(err, &timeout) || !timeout.Timeout() {
		t.Fatalf("expected no outgoing packet, read err=%v data=%x", err, packet)
	}
}

func simRoleWaterCrowd(t *testing.T, mage *Bot, now time.Time) {
	t.Helper()
	oldMob, hadMob := mobs[simRoleMobClass]
	mobs[simRoleMobClass] = &Mob{ID: simRoleMobClass, Name: "Simulation Water Mob", Level: 70, HP: 1000, Exp: 100, Element: "Water"}
	t.Cleanup(func() {
		if hadMob {
			mobs[simRoleMobClass] = oldMob
		} else {
			delete(mobs, simRoleMobClass)
		}
	})
	mage.ents[simRoleMobID] = simRoleMob(simRoleMobID, 104, 101, 0, now)
	mage.ents[simRoleMobID+1] = simRoleMob(simRoleMobID+1, 105, 101, 0, now)
	mage.ents[simRoleMobID+2] = simRoleMob(simRoleMobID+2, 106, 101, 0, now)
}

func simRoleMapWithWall(t *testing.T, wallX, y0, y1 int) *GameMap {
	t.Helper()
	gm := &GameMap{Name: simRoleMap, W: 200, H: 200, cells: make([]byte, 200*200)}
	for y := y0; y <= y1; y++ {
		gm.cells[wallX+y*gm.W] = 1
	}
	oldMap, hadMap := maps[simRoleMap]
	maps[simRoleMap] = gm
	t.Cleanup(func() {
		if hadMap {
			maps[simRoleMap] = oldMap
		} else {
			delete(maps, simRoleMap)
		}
	})
	return gm
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
			peer := simRoleCapture(t, tank)

			_, _, members := tank.partyView(partyOf(tank), now)
			tank.mu.Lock()
			if !tank.tankTakeAggro(members, false, now) {
				tank.mu.Unlock()
				t.Fatal("tank did not act on the monster attacking the priest")
			}
			target, state := tank.target, tank.State
			tank.mu.Unlock()
			if target != simRoleMobID {
				t.Fatalf("tank target = %d, want peeled monster %d", target, simRoleMobID)
			}
			if !strings.Contains(state, "area skill "+itoa(tc.skillID)+" on a group") {
				t.Fatalf("tank state = %q, want build's crowd skill %d after the peel", state, tc.skillID)
			}
			packet := simRoleReadPacket(t, peer, 10)
			if opcode := binary.LittleEndian.Uint16(packet); opcode != 0x0438 {
				t.Fatalf("tank action opcode = %#x, want targeted skill 0x0438", opcode)
			}
			if skill := int(binary.LittleEndian.Uint16(packet[4:6])); skill != tc.skillID {
				t.Fatalf("tank used skill %d, want build's crowd skill %d", skill, tc.skillID)
			}
			groupTarget := binary.LittleEndian.Uint32(packet[6:10])
			if groupTarget < simRoleMobID || groupTarget > simRoleMobID+2 {
				t.Fatalf("tank's crowd-skill target = %d, outside the grouped monsters", groupTarget)
			}
		})
	}
}

func TestSimPartyFrostWizardUsesBuildSkillAgainstWaterCrowd(t *testing.T) {
	_, _, mage, _ := simRoleParty(t, "knight")
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	simRoleWaterCrowd(t, mage, now)
	peer := simRoleCapture(t, mage)

	mage.mu.Lock()
	cast := mage.aoeCast(mage.ents[simRoleMobID], now)
	state := mage.State
	mage.mu.Unlock()
	if !cast {
		t.Fatal("frost wizard did not cast on the grouped water monsters")
	}
	if !strings.Contains(state, "Lord of Vermilion on a group") {
		t.Fatalf("wizard state = %q, want build's lightning counter-element AoE", state)
	}
	packet := simRoleReadPacket(t, peer, 10)
	if opcode := binary.LittleEndian.Uint16(packet); opcode != 0x0366 {
		t.Fatalf("wizard action opcode = %#x, want ground skill 0x0366", opcode)
	}
	if skill := int(binary.LittleEndian.Uint16(packet[4:6])); skill != wzVermilion {
		t.Fatalf("wizard used skill %d, want Lord of Vermilion (%d)", skill, wzVermilion)
	}
	castX := int(binary.LittleEndian.Uint16(packet[6:8]))
	castY := int(binary.LittleEndian.Uint16(packet[8:10]))
	dx, dy := castX-mage.X, castY-mage.Y
	if dx < 0 {
		dx = -dx
	}
	if dy < 0 {
		dy = -dy
	}
	if dx > aoeCastRange || dy > aoeCastRange {
		t.Fatalf("wizard cast at unreachable cell %d,%d from %d,%d", castX, castY, mage.X, mage.Y)
	}
	radius := 0
	for _, spell := range aoeSpells {
		if spell.id == wzVermilion {
			radius = spell.radius
			break
		}
	}
	if radius == 0 {
		t.Fatal("Lord of Vermilion has no configured area radius")
	}
	for id := uint32(simRoleMobID); id < simRoleMobID+3; id++ {
		mob := mage.ents[id]
		if mob == nil {
			t.Fatalf("water crowd fixture is missing monster %d", id)
		}
		dx, dy := castX-mob.X, castY-mob.Y
		if dx < 0 {
			dx = -dx
		}
		if dy < 0 {
			dy = -dy
		}
		if dx > radius || dy > radius {
			t.Fatalf("wizard cast at %d,%d does not cover water mob %d at %d,%d (radius %d)", castX, castY, id, mob.X, mob.Y, radius)
		}
	}
}

func TestSimPartyTankSkipsCrowdAttackWhenBuildSkillIsUnavailable(t *testing.T) {
	tank, _, _, _ := simRoleParty(t, "knight_spear")
	delete(tank.skills, 57)
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	tank.ents[simRoleMobID] = simRoleMob(simRoleMobID, 101, 100, 0, now)
	tank.ents[simRoleMobID+1] = simRoleMob(simRoleMobID+1, 102, 100, 0, now)
	tank.ents[simRoleMobID+2] = simRoleMob(simRoleMobID+2, 103, 100, 0, now)
	peer := simRoleCapture(t, tank)

	tank.mu.Lock()
	cast := tank.weaponAoE(tank.ents[simRoleMobID], now)
	tank.mu.Unlock()
	if cast {
		t.Fatal("spear knight cast Brandish Spear without the skill")
	}
	simRoleExpectNoPacket(t, peer)
}

func TestSimPartyFrostWizardDoesNotCastBelowPartySPReserve(t *testing.T) {
	_, _, mage, _ := simRoleParty(t, "knight")
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	simRoleWaterCrowd(t, mage, now)
	mage.SP = 5
	peer := simRoleCapture(t, mage)

	mage.mu.Lock()
	cast := mage.aoeCast(mage.ents[simRoleMobID], now)
	mage.mu.Unlock()
	if cast {
		t.Fatal("frost wizard cast an AoE below its SP reserve")
	}
	simRoleExpectNoPacket(t, peer)
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
	peer := simRoleCapture(t, priest)

	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	priest.mu.Lock()
	if !priest.healFirst(false, now) {
		priest.mu.Unlock()
		t.Fatal("priest did not heal an injured party member")
	}
	state := priest.State
	priest.mu.Unlock()
	if !strings.Contains(state, "healing SimTank") {
		t.Fatalf("priest state = %q, want the lowest-HP member SimTank", state)
	}
	packet := simRoleReadPacket(t, peer, 10)
	if opcode := binary.LittleEndian.Uint16(packet); opcode != 0x0438 {
		t.Fatalf("priest action opcode = %#x, want targeted skill 0x0438", opcode)
	}
	if skill := int(binary.LittleEndian.Uint16(packet[4:6])); skill != alHeal {
		t.Fatalf("priest used skill %d, want Heal (%d)", skill, alHeal)
	}
	if target := binary.LittleEndian.Uint32(packet[6:10]); target != tank.aid {
		t.Fatalf("priest healed AID %d, want the lowest-HP tank %d", target, tank.aid)
	}
}

func TestSimPartyPriestMovesInsteadOfHealingThroughBlockedLine(t *testing.T) {
	tank := simRoleBot("WallTank", "knight", simRoleTankAID, 102, 100)
	priest := simRoleBot("WallPriest", "priest_me", simRolePriestAID, 90, 100)
	stickParty(t, tank, priest)
	simRolePublish(tank, 0.4)
	simRolePublish(priest, 1)
	priest.ents[tank.aid] = &Entity{ID: tank.aid, Type: objPC, Name: tank.charName(), X: tank.X, Y: tank.Y}
	gm := simRoleMapWithWall(t, 96, 98, 102)
	if healReachable(gm, priest.X, priest.Y, tank.X, tank.Y, healRange) {
		t.Fatal("fixture unexpectedly permits a direct Heal through the wall")
	}
	x, y, ok := healSpot(gm, priest.X, priest.Y, tank.X, tank.Y, healRange)
	if !ok || gm.FindPath(priest.X, priest.Y, x, y, 80000) == nil {
		t.Fatal("fixture has no reachable healing position around the wall")
	}
	peer := simRoleCapture(t, priest)

	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	priest.mu.Lock()
	acted := priest.healFirst(false, now)
	state := priest.State
	priest.mu.Unlock()
	if !acted || !strings.Contains(state, "moving to heal WallTank") {
		t.Fatalf("priest action = %v, state = %q; want approach rather than cast", acted, state)
	}
	packet := simRoleReadPacket(t, peer, 5)
	if opcode := binary.LittleEndian.Uint16(packet); opcode != 0x035f {
		t.Fatalf("priest action opcode = %#x, want a walk packet 0x035f, not Heal", opcode)
	}
	wx, wy, _ := decodePos(packet[2:5])
	if dist(wx, wy, x, y) > 2 {
		t.Fatalf("priest walk target %d,%d does not approach heal spot %d,%d", wx, wy, x, y)
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
		{name: "walks behind support when chased", hunterX: 107, mobX: 109},
	} {
		t.Run(tc.name, func(t *testing.T) {
			priest := simRoleBot("ProtectPriest", "priest_me", simRolePriestAID, 100, 100)
			hunter := simRoleBot("ProtectHunter", "hunter_blitz", simRoleHunterAID, tc.hunterX, 100)
			stickParty(t, priest, hunter)
			simRolePublish(priest, 1)
			simRolePublish(hunter, 1)
			now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
			hunter.ents[simRoleMobID] = simRoleMob(simRoleMobID, tc.mobX, 100, hunter.aid, now)
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
			peer := simRoleCapture(t, hunter)
			hunter.mu.Lock()
			moved := hunter.protectOwnSupport(partyOf(hunter), members, gm, false, now)
			target := hunter.target
			hunter.mu.Unlock()
			if tc.engaged {
				simRoleExpectNoPacket(t, peer)
				if moved || target != simRoleMobID {
					t.Fatalf("engaged member moved=%v target=%d; want its current fight retained", moved, target)
				}
				return
			}
			if !moved || target != 0 {
				t.Fatalf("free ranged member moved=%v target=%d; want a support-protection walk", moved, target)
			}
			packet := simRoleReadPacket(t, peer, 5)
			if opcode := binary.LittleEndian.Uint16(packet); opcode != 0x035f {
				t.Fatalf("chased Hunter action opcode = %#x, want walk packet 0x035f", opcode)
			}
			wx, wy, _ := decodePos(packet[2:5])
			if wx < 97 || wx >= 100 || wy != 100 {
				t.Fatalf("chased Hunter walked to %d,%d; want a point behind support at 100,100", wx, wy)
			}
		})
	}
}
