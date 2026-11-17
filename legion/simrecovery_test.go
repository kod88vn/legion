package main

import (
	"encoding/binary"
	"net"
	"testing"
	"time"
)

func simRecoveryMap(t *testing.T) *GameMap {
	t.Helper()
	gm := &GameMap{Name: simRoleMap, W: 200, H: 200, cells: make([]byte, 200*200)}
	old, had := maps[simRoleMap]
	maps[simRoleMap] = gm
	t.Cleanup(func() {
		if had {
			maps[simRoleMap] = old
		} else {
			delete(maps, simRoleMap)
		}
	})
	return gm
}

func simRecoveryReadAction(t *testing.T, peer net.Conn) []byte {
	t.Helper()
	for {
		opcode := simRoleReadPacket(t, peer, 2)
		switch binary.LittleEndian.Uint16(opcode) {
		case 0x0108:
			header := append(opcode, simRoleReadPacket(t, peer, 2)...)
			size := int(binary.LittleEndian.Uint16(header[2:4]))
			if size < len(header) {
				t.Fatalf("invalid party-chat packet size %d", size)
			}
			simRoleReadPacket(t, peer, size-len(header))
			continue
		case 0x035f:
			return append(opcode, simRoleReadPacket(t, peer, 3)...)
		case 0x0437:
			return append(opcode, simRoleReadPacket(t, peer, 5)...)
		case 0x0438, 0x0366:
			return append(opcode, simRoleReadPacket(t, peer, 8)...)
		default:
			t.Fatalf("unexpected outgoing action opcode %#x", binary.LittleEndian.Uint16(opcode))
		}
	}
}

func simRecoveryTarget(action []byte) (uint32, bool) {
	switch binary.LittleEndian.Uint16(action) {
	case 0x0437:
		return binary.LittleEndian.Uint32(action[2:6]), true
	case 0x0438:
		return binary.LittleEndian.Uint32(action[6:10]), true
	default:
		return 0, false
	}
}

func TestSimPartyRecoveryStuckMemberDoesNotRestartCollectionForever(t *testing.T) {
	leader := simRoleBot("RecoveryHunter", "hunter_blitz", simRoleHunterAID, 100, 100)
	tank := simRoleBot("RecoveryKnight", "knight", simRoleTankAID, 102, 100)
	stuck := simRoleBot("RecoveryWizard", "wizard_frost", simRoleMageAID, 130, 100)
	priest := simRoleBot("RecoveryPriest", "priest_me", simRolePriestAID, 101, 100)
	stuck.BaseLv, stuck.P.BaseLevel = 40, 40
	stickParty(t, leader, tank, stuck, priest)
	for _, member := range []*Bot{leader, tank, stuck, priest} {
		simRolePublish(member, 1)
	}
	gm := simRecoveryMap(t)
	peer := simRoleCapture(t, leader)
	now := time.Date(2026, 11, 17, 5, 0, 0, 0, time.UTC)

	leader.mu.Lock()
	leader.hunt(gm, false, now)
	firstState, firstCollect := leader.State, leader.collectName
	leader.mu.Unlock()
	if firstCollect != stuck.charName() {
		t.Fatalf("initial collect = %q, want stuck member %q (state %q)", firstCollect, stuck.charName(), firstState)
	}
	first := simRecoveryReadAction(t, peer)
	if opcode := binary.LittleEndian.Uint16(first); opcode != 0x035f {
		t.Fatalf("initial recovery opcode = %#x, want a walk toward the straggler", opcode)
	}
	wx, wy, _ := decodePos(first[2:5])
	if dist(wx, wy, stuck.X, stuck.Y) >= dist(leader.X, leader.Y, stuck.X, stuck.Y) {
		t.Fatalf("initial recovery walk to %d,%d does not approach stuck member at %d,%d", wx, wy, stuck.X, stuck.Y)
	}

	window := time.Duration(tune().LeaderCollectSec) * time.Second
	if window <= 0 {
		t.Fatal("leader_collect_sec must provide a positive collection window")
	}
	mob := simRoleMob(simRoleMobID, 106, 100, 0, now.Add(window+time.Second))
	leader.ents[mob.ID] = mob

	leader.mu.Lock()
	leader.hunt(gm, false, now.Add(window+time.Second))
	state, target, collect := leader.State, leader.target, leader.collectName
	leader.mu.Unlock()
	if collect != "" || target != mob.ID {
		t.Fatalf("after a full collection window, the leader still stalled on %q (target %d, state %q); want it to farm nearby mob %d",
			collect, target, state, mob.ID)
	}
	action := simRecoveryReadAction(t, peer)
	if opcode := binary.LittleEndian.Uint16(action); opcode != 0x0437 && opcode != 0x0438 {
		t.Fatalf("after the stuck-member window, action opcode = %#x, want an attack on mob %d (state %q)", opcode, mob.ID, state)
	}
	if actionTarget, ok := simRecoveryTarget(action); !ok || actionTarget != mob.ID {
		t.Fatalf("after the stuck-member window, action target = %d (targeted=%v), want mob %d", actionTarget, ok, mob.ID)
	}
}

func TestSimPartyRecoveryTankPeelsThreatenedPriestWhileMemberIsOnOtherMap(t *testing.T) {
	tank, _, mage, priest := simRoleParty(t, "knight")
	mage.MapName = "sim_recovery_other"
	simRolePublish(mage, 1)
	priest.SP = 0
	simRolePublish(priest, 0.3)

	gm := simRecoveryMap(t)
	now := time.Date(2026, 11, 17, 5, 0, 0, 0, time.UTC)
	mob := simRoleMob(simRoleMobID, 101, 100, priest.aid, now)
	tank.ents[mob.ID] = mob
	peer := simRoleCapture(t, tank)

	tank.mu.Lock()
	tank.hunt(gm, false, now)
	state, target, collect := tank.State, tank.target, tank.collectName
	tank.mu.Unlock()
	if target != mob.ID || collect != "" {
		t.Fatalf("tank recovery state = %q, target %d, collecting %q; want it to peel mob %d, not recover the member on another map",
			state, target, collect, mob.ID)
	}

	action := simRecoveryReadAction(t, peer)
	if opcode := binary.LittleEndian.Uint16(action); opcode != 0x0438 {
		t.Fatalf("tank recovery action opcode = %#x, want targeted Provoke 0x0438 against mob %d", opcode, mob.ID)
	}
	if skill := int(binary.LittleEndian.Uint16(action[4:6])); skill != smProvoke {
		t.Fatalf("tank recovery skill = %d, want Provoke %d", skill, smProvoke)
	}
	if actionTarget := binary.LittleEndian.Uint32(action[6:10]); actionTarget != mob.ID {
		t.Fatalf("tank recovery target = %d, want threatening mob %d", actionTarget, mob.ID)
	}
}
