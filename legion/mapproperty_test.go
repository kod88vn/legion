package main

import (
	"math/rand"
	"testing"
	"time"
)

func TestMapPropertyObservationThroughFramingAndDispatch(t *testing.T) {
	c, peer := pipeConn()
	defer c.Close()
	defer peer.Close()

	packet := []byte{0x9b, 0x09, 0x34, 0x12, 0x78, 0x56, 0x34, 0x12}
	written := make(chan error, 1)
	go func() {
		_, err := peer.Write(packet)
		written <- err
	}()

	got, err := c.Read(time.Second)
	if err != nil {
		t.Fatalf("read map-property packet: %v", err)
	}
	if string(got) != string(packet) {
		t.Fatalf("packet = %x, want %x", got, packet)
	}
	if err := <-written; err != nil {
		t.Fatalf("write map-property packet: %v", err)
	}

	b := &Bot{P: &Persona{Name: "MapPropertyTest", Chars: []CharSpec{{Name: "MapPropertyTest"}}}}
	b.handle(got)
	assertMapProperty(t, b, true, 0x1234, 0x12345678)
}

func TestFreePvPZoneRemainsRawObservation(t *testing.T) {
	b := &Bot{P: &Persona{Name: "MapPropertyTest", Chars: []CharSpec{{Name: "MapPropertyTest"}}}}
	b.target = 42
	b.handle([]byte{0x9b, 0x09, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00})
	assertMapProperty(t, b, true, 1, 0)
	if b.target != 42 {
		t.Fatalf("map-property packet changed target to %d", b.target)
	}
}

func TestMapPropertyStartsUnknown(t *testing.T) {
	b := &Bot{}
	assertMapProperty(t, b, false, 0, 0)
}

func TestMapChangeClearsMapPropertyObservation(t *testing.T) {
	oldMemDir := memDir
	memDir = t.TempDir()
	t.Cleanup(func() { memDir = oldMemDir })

	b := &Bot{
		P:       &Persona{Name: "MapPropertyTest", Account: "map-property-test", Chars: []CharSpec{{Name: "MapPropertyTest"}}},
		rng:     rand.New(rand.NewSource(1)),
		MapName: "geffen",
	}
	seedMapProperty(b, true, 1, 0x20)
	b.handle(newPkt(0x0091).str("prontera.gat", 16).u16(10).u16(20).b)
	assertMapProperty(t, b, false, 0, 0)
}

func TestEndSessionClearsMapPropertyObservation(t *testing.T) {
	c, peer := pipeConn()
	defer peer.Close()

	b := &Bot{P: &Persona{Name: "MapPropertyTest", Chars: []CharSpec{{Name: "MapPropertyTest"}}}}
	b.wmu.Lock()
	b.conn = c
	b.wmu.Unlock()
	seedMapProperty(b, true, 1, 0x20)

	b.endSession(c)
	assertMapProperty(t, b, false, 0, 0)
}

func seedMapProperty(b *Bot, known bool, propertyType uint16, flags uint32) {
	b.mu.Lock()
	b.mapPropertyKnown = known
	b.mapPropertyType = propertyType
	b.mapPropertyFlags = flags
	b.mu.Unlock()
}

func assertMapProperty(t *testing.T, b *Bot, known bool, propertyType uint16, flags uint32) {
	t.Helper()
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.mapPropertyKnown != known {
		t.Fatalf("map-property known = %t, want %t", b.mapPropertyKnown, known)
	}
	if b.mapPropertyType != propertyType {
		t.Fatalf("map-property type = %#x, want %#x", b.mapPropertyType, propertyType)
	}
	if b.mapPropertyFlags != flags {
		t.Fatalf("map-property flags = %#x, want %#x", b.mapPropertyFlags, flags)
	}
}
