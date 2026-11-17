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

func TestSimPartyRecoveryStuckMemberFarmsBeforeBoundedRetry(t *testing.T) {
	leader := simRoleBot("RecoveryHunter", "hunter_blitz", simRoleHunterAID, 100, 100)
	leader.Speed = 150
	tank := simRoleBot("RecoveryKnight", "knight", simRoleTankAID, 102, 100)
	stuck := simRoleBot("RecoveryWizard", "wizard_frost", simRoleMageAID, 130, 100)
	priest := simRoleBot("RecoveryPriest", "priest_me", simRolePriestAID, 101, 100)
	stuck.BaseLv, stuck.P.BaseLevel = 40, 40
	stickParty(t, leader, tank, stuck, priest)
	for _, member := range []*Bot{leader, tank, stuck, priest} {
		simRolePublish(member, 1)
	}
	gm := simRecoveryMap(t)
	if eta, ok := collectionRescueETA(gm, leader.X, leader.Y, stuck.X, stuck.Y, leader.Speed); !ok || eta <= 0 {
		t.Fatalf("the accepted recovery scene needs a real path and ETA, got %s/%t", eta, ok)
	}
	peer := simRoleCapture(t, leader)
	now := time.Date(2026, 11, 17, 5, 0, 0, 0, time.UTC)
	leader.buffAt["leadsay"] = now.Add(time.Hour)

	// Scope this scenario to collection arbitration; stepAlong's walk goroutine cannot be joined by the fixture.
	leader.mu.Lock()
	leader.hunt(gm, true, now)
	firstState, firstCollect := leader.State, leader.collectName
	leader.mu.Unlock()
	if firstCollect != stuck.charName() {
		t.Fatalf("initial collect = %q, want stuck member %q (state %q)", firstCollect, stuck.charName(), firstState)
	}

	window := time.Duration(tune().LeaderCollectSec) * time.Second
	if window <= 0 {
		t.Fatal("leader_collect_sec must provide a positive collection window")
	}
	firstExpiry := now.Add(window + time.Second)
	mobAt := firstExpiry
	mob := simRoleMob(simRoleMobID, 101, 100, 0, mobAt)
	leader.ents[mob.ID] = mob

	leader.mu.Lock()
	leader.hunt(gm, true, firstExpiry)
	state, target, collect, deferAt := leader.State, leader.target, leader.collectName, leader.deferAt
	leader.mu.Unlock()
	if collect == stuck.charName() {
		t.Fatalf("at the first expired window, the leader restarted collection of %q (target %d, state %q)",
			collect, target, state)
	}
	if target != 0 || !deferAt.Equal(firstExpiry) {
		t.Fatalf("the leader did not enter its tank-first hold after recovery (collect %q, target %d, defer %s, state %q)",
			collect, target, deferAt, state)
	}

	fightAt := firstExpiry.Add(tankDeferWait)
	farmed := false
	leader.mu.Lock()
	leader.hunt(gm, true, fightAt)
	state, target, collect = leader.State, leader.target, leader.collectName
	leader.mu.Unlock()
	if collect == stuck.charName() || target != mob.ID {
		t.Fatalf("after the tank-first window, leader target = %d, collecting %q, state %q; want it to attack mob %d",
			target, collect, state, mob.ID)
	}

	for attempt := 0; attempt < 3; attempt++ {
		action := simRecoveryReadAction(t, peer)
		actionTarget, ok := simRecoveryTarget(action)
		if ok && actionTarget == mob.ID {
			farmed = true
			break
		}
		// A due self-buff may precede the first attack after the tank-first hold.
		if !ok || actionTarget != leader.aid {
			t.Fatalf("after the tank-first window, action target = %d (targeted=%v), want mob %d or the leader's own buff %d (packet %x)",
				actionTarget, ok, mob.ID, leader.aid, action)
		}
		fightAt = fightAt.Add(5 * time.Second)
		leader.mu.Lock()
		leader.hunt(gm, true, fightAt)
		state, target, collect = leader.State, leader.target, leader.collectName
		leader.mu.Unlock()
		if collect == stuck.charName() || target != mob.ID {
			t.Fatalf("after the tank-first window, leader target = %d, collecting %q, state %q; want it to attack mob %d",
				target, collect, state, mob.ID)
		}
	}
	if !farmed {
		t.Fatalf("the leader did not send an attack against mob %d after the tank-first window", mob.ID)
	}

	retryAt := firstExpiry.Add(window)
	leader.mu.Lock()
	delete(leader.ents, mob.ID)
	leader.target = 0
	leader.hunt(gm, true, retryAt.Add(-time.Second))
	state, collect = leader.State, leader.collectName
	leader.mu.Unlock()
	if collect == stuck.charName() {
		t.Fatalf("the leader restarted collection before the bounded retry (state %q)", state)
	}

	leader.mu.Lock()
	leader.hunt(gm, true, retryAt)
	state, collect, collectAt := leader.State, leader.collectName, leader.collectAt
	leader.mu.Unlock()
	if collect != stuck.charName() || !collectAt.Equal(retryAt) {
		t.Fatalf("after the bounded wait the leader did not retry the still-eligible member: collect %q at %s, state %q",
			collect, collectAt, state)
	}
}

func TestSimPartyRecoveryWithoutETAKeepsExistingSelectionBehavior(t *testing.T) {
	leader := simRoleBot("RecoveryHunterNoETA", "hunter_blitz", simRoleHunterAID, 100, 100)
	tank := simRoleBot("RecoveryKnightNoETA", "knight", simRoleTankAID, 102, 100)
	stuck := simRoleBot("RecoveryWizardNoETA", "wizard_frost", simRoleMageAID, 130, 100)
	priest := simRoleBot("RecoveryPriestNoETA", "priest_me", simRolePriestAID, 101, 100)
	stuck.BaseLv, stuck.P.BaseLevel = 40, 40
	stickParty(t, leader, tank, stuck, priest)
	for _, member := range []*Bot{leader, tank, stuck, priest} {
		simRolePublish(member, 1)
	}
	gm := simRecoveryMap(t)
	if eta, ok := collectionRescueETA(gm, leader.X, leader.Y, stuck.X, stuck.Y, leader.Speed); ok {
		t.Fatalf("zero-speed control unexpectedly has a justified ETA: %s", eta)
	}

	now := time.Date(2026, 11, 17, 5, 0, 0, 0, time.UTC)
	leader.mu.Lock()
	leader.hunt(gm, true, now)
	initialCollect := leader.collectName
	leader.mu.Unlock()
	if initialCollect != stuck.charName() {
		t.Fatalf("initial collect = %q, want %q", initialCollect, stuck.charName())
	}

	expiry := now.Add(time.Duration(tune().LeaderCollectSec)*time.Second + time.Second)
	leader.mu.Lock()
	leader.hunt(gm, true, expiry)
	collect := leader.collectName
	leader.mu.Unlock()
	if collect != stuck.charName() {
		t.Fatalf("without a justified ETA, selection changed to %q; want existing same-member retry %q", collect, stuck.charName())
	}
}

func TestSimPartyRecoveryStallSelectsAnotherEligibleMember(t *testing.T) {
	leader := simRoleBot("RecoveryHunter", "hunter_blitz", simRoleHunterAID, 100, 100)
	leader.Speed = 150
	tank := simRoleBot("RecoveryKnight", "knight", simRoleTankAID, 102, 100)
	stuck := simRoleBot("RecoveryWizard", "wizard_frost", simRoleMageAID, 130, 100)
	priest := simRoleBot("RecoveryPriest", "priest_me", simRolePriestAID, 101, 100)
	alternate := simRoleBot("RecoveryRogue", "rogue", simRoleHunterAID, 140, 100)
	stuck.BaseLv, stuck.P.BaseLevel = 40, 40
	stickParty(t, leader, tank, stuck, priest, alternate)
	for _, member := range []*Bot{leader, tank, stuck, priest, alternate} {
		simRolePublish(member, 1)
	}
	gm := simRecoveryMap(t)
	now := time.Date(2026, 11, 17, 5, 0, 0, 0, time.UTC)

	leader.mu.Lock()
	leader.hunt(gm, true, now)
	firstCollect := leader.collectName
	leader.mu.Unlock()
	if firstCollect != stuck.charName() {
		t.Fatalf("initial collect = %q, want stalled member %q", firstCollect, stuck.charName())
	}

	firstExpiry := now.Add(time.Duration(tune().LeaderCollectSec)*time.Second + time.Second)
	leader.mu.Lock()
	leader.hunt(gm, true, firstExpiry)
	state, collect := leader.State, leader.collectName
	leader.mu.Unlock()
	if collect != alternate.charName() {
		t.Fatalf("a stalled member blocked an alternate candidate: collecting %q, want %q (state %q)",
			collect, alternate.charName(), state)
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
