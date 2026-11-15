package main

import "testing"

// geffenFountain: geffen.gat x 100-129, y 31-46 as rAthena's map cache has it ('#' = wall). Ayla sat at 118,39 in
// the gap of the fountain wall; Zendancer84 stood at 112,38 and sent Heal ~115 times, none landed (QA 2026-11-15).
func geffenFountain() *GameMap {
	rows := map[int]string{
		46: "..............############....",
		45: "..........####################",
		44: "..........####################",
		43: "..........####################",
		42: ".##.......####################",
		41: ".##........#######....#######.",
		40: "............######....######..",
		39: ".............#####....#####...",
		38: ".....##.......####....####....",
		37: ".....###......................",
		36: "......###.....................",
		35: ".......###....................",
		34: "........###..................#",
		33: ".........##..................#",
		32: "...##.........................",
		31: "...##.........................",
	}
	m := &GameMap{Name: "geffen_t", W: 140, H: 60, cells: make([]byte, 140*60)}
	for y, r := range rows {
		for i, c := range r {
			if c == '#' {
				m.cells[(100+i)+y*m.W] = 1
			}
		}
	}
	return m
}

func TestHealReachWallBlocksLine(t *testing.T) {
	m := geffenFountain()
	if healReachable(m, 112, 38, 118, 39, healRange) {
		t.Fatal("112,38 -> 118,39 crosses the fountain wall: rAthena drops this Heal silently")
	}
	x, y, ok := healSpot(m, 112, 38, 118, 39, healRange)
	if !ok {
		t.Fatal("no heal spot found for Ayla in the fountain gap")
	}
	if !healReachable(m, x, y, 118, 39, healRange) || !m.Walkable(x, y) {
		t.Fatalf("heal spot %d,%d does not reach 118,39", x, y)
	}
	if !healReachable(m, 118, 36, 118, 39, healRange) {
		t.Fatal("straight down the open gap must be reachable")
	}
}

func TestHealReachCircularRange(t *testing.T) {
	m := &GameMap{W: 50, H: 50, cells: make([]byte, 2500)}
	if !healReachable(m, 10, 10, 19, 10, 9) {
		t.Fatal("9 cells straight is in Heal range")
	}
	if !healReachable(m, 10, 10, 17, 17, 9) {
		t.Fatal("7,7 diagonal is 9 cells circular (distance_client): in range")
	}
	if healReachable(m, 10, 10, 18, 18, 9) {
		t.Fatal("8,8 diagonal is 11 cells circular: out of Heal range")
	}
	if !healReachable(m, 10, 10, 11, 11, 9) {
		t.Fatal("adjacent is always reachable")
	}
}

func TestHealCliffIsShootable(t *testing.T) {
	m := &GameMap{W: 20, H: 5, cells: make([]byte, 100)}
	for y := 0; y < 5; y++ {
		m.cells[5+y*20] = 5 // cliff: not walkable but shootable
	}
	if !healReachable(m, 2, 2, 8, 2, 9) {
		t.Fatal("a cliff line does not block a skill")
	}
	for y := 0; y < 5; y++ {
		m.cells[5+y*20] = 1
	}
	if healReachable(m, 2, 2, 8, 2, 9) {
		t.Fatal("a wall line blocks a skill")
	}
}

func TestHealUnackedAfterThree(t *testing.T) {
	c := "t_healer"
	healSent(c, 7)
	healSent(c, 7)
	if healUnacked(c, 7) {
		t.Fatal("2 unconfirmed is not yet stuck")
	}
	healSent(c, 7)
	if !healUnacked(c, 7) {
		t.Fatal("3 unconfirmed heals to one target = stuck")
	}
	healAcked(c)
	if healUnacked(c, 7) || !healedLately(c, 1e9) {
		t.Fatal("a confirmed heal clears the count and counts as healed lately")
	}
	healSent(c, 7)
	healSent(c, 7)
	healSent(c, 8) // a new target restarts the count
	if healUnacked(c, 8) || healUnacked(c, 7) {
		t.Fatal("count is per target")
	}
}
