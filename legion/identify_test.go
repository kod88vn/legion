package main

import (
	"encoding/binary"
	"testing"
)

// equipItem builds one 68-byte EQUIPITEM_INFO (2021 layout) for 0x0b39.
func equipItem(idx int, id uint32, loc, wear uint32, identified bool) []byte {
	e := make([]byte, 68)
	binary.LittleEndian.PutUint16(e[0:], uint16(idx))
	binary.LittleEndian.PutUint32(e[2:], id)
	binary.LittleEndian.PutUint32(e[7:], loc)
	binary.LittleEndian.PutUint32(e[11:], wear)
	binary.LittleEndian.PutUint32(e[15:], 4001) // card 0
	if identified {
		e[67] = 1
	}
	return e
}

func pickupAck(idx int, id uint32, loc uint32, identified bool) []byte {
	pk := make([]byte, 70)
	binary.LittleEndian.PutUint16(pk[0:], 0x0b41)
	binary.LittleEndian.PutUint16(pk[2:], uint16(idx))
	binary.LittleEndian.PutUint16(pk[4:], 1)
	binary.LittleEndian.PutUint32(pk[6:], id)
	if identified {
		pk[10] = 1
	}
	binary.LittleEndian.PutUint32(pk[28:], loc)
	return pk
}

func equipAck(idx int, loc uint32, result byte) []byte {
	pk := make([]byte, 11)
	binary.LittleEndian.PutUint16(pk[0:], 0x0999)
	binary.LittleEndian.PutUint16(pk[2:], uint16(idx))
	binary.LittleEndian.PutUint32(pk[4:], loc)
	pk[10] = result
	return pk
}

// Thorn.rose's case: a looted (unidentified) Club in the bag, no weapon worn.
func TestUnidentifiedDropDoesNotBlockWeapon(t *testing.T) {
	items[1501] = &Item{ID: 1501, Name: "Club", Type: "Weapon", Loc: 2, Atk: 23, Buy: 120, MinLv: 2, Jobs: map[string]bool{"Acolyte": true}}
	items[2305] = &Item{ID: 2305, Name: "Adventurer's Suit", Type: "Armor", Loc: 16, Def: 3, Buy: 1000}
	b := NewBot(&Persona{Name: "t", Account: "test-equip", Class: 4, BaseLevel: 10, Chars: []CharSpec{{Name: "t", Class: 4}}})
	b.BaseLv = 10

	list := []byte{0x39, 0x0b, 0, 0, 0}
	list = append(list, equipItem(10, 1501, 2, 0, false)...)
	list = append(list, equipItem(13, 2305, 16, 16, true)...)
	binary.LittleEndian.PutUint16(list[2:], uint16(len(list)))
	b.handle(list)
	b.handle([]byte{0x0b, 0x0b, 0, 0})

	club := b.inv[10]
	if club == nil || !club.Unidentified || b.inv[13].Unidentified || b.equipped[16] == nil {
		t.Fatalf("0x0b39 parse: club %+v suit %+v", club, b.inv[13])
	}
	if b.inv[13].Cards[0] != 4001 {
		t.Fatalf("card offset: got %v", b.inv[13].Cards)
	}
	if !b.invSeen {
		t.Fatal("invSeen not set")
	}
	if b.wearable(club, items[1501]) || b.ownsUnequipped(1501) {
		t.Fatal("unidentified Club counted as wearable/owned")
	}
	b.equipFromInventory()
	if b.equipWanted != 0 {
		t.Fatal("tried to equip the unidentified Club")
	}

	// a refused equip marks only that item, never the item ID (the bot may still buy a Club)
	b.handle(equipAck(10, 0, 1))
	if !club.NoEquip || b.gearFailed[1501] {
		t.Fatalf("refusal: NoEquip %v gearFailed %v", club.NoEquip, b.gearFailed[1501])
	}

	// a bought Club (identified) arrives via 0x0b41 and gets equipped, weapon first
	b.handle(pickupAck(15, 1501, 2, true))
	nc := b.inv[15]
	if nc == nil || nc.Loc != 2 || nc.Unidentified {
		t.Fatalf("0x0b41 parse: %+v", nc)
	}
	b.equipFromInventory()
	if b.equipWanted != 1501 || b.equipIdx != 15 {
		t.Fatalf("equip request: wanted %d idx %d", b.equipWanted, b.equipIdx)
	}
	b.handle(equipAck(15, 2, 0))
	if b.equipped[2] != nc || !nc.Equipped || b.equipWanted != 0 {
		t.Fatal("equip ack not applied")
	}
}
