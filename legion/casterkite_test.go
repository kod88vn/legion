package main

import "testing"

func TestCasterKiteWall(t *testing.T) {
	if x, y, ok := casterKiteWall(10, 10, 11, 10); !ok || x != 11 || y != 10 {
		t.Errorf("adjacent: wall on its cell, got %d,%d %v", x, y, ok)
	}
	if x, y, ok := casterKiteWall(10, 10, 12, 8); !ok || x != 11 || y != 9 {
		t.Errorf("2 cells: wall between, got %d,%d %v", x, y, ok)
	}
	if _, _, ok := casterKiteWall(10, 10, 10, 10); ok {
		t.Error("on our cell: no wall")
	}
	if _, _, ok := casterKiteWall(10, 10, 14, 10); ok {
		t.Error("4 cells: not ours (aoe.go walls walkers)")
	}
}

func TestCasterKiteBudget(t *testing.T) {
	casterKites.Delete("kiter")
	for i := 0; i < casterKiteMax; i++ {
		if !casterKiteNext("kiter", 7) {
			t.Fatalf("step %d refused", i+1)
		}
	}
	if casterKiteNext("kiter", 7) {
		t.Fatal("a 4th step from the same monster: fight it out instead")
	}
	if !casterKiteNext("kiter", 8) {
		t.Fatal("a new monster gets a fresh budget")
	}
}
