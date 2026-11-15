package main

import "testing"

// mjolnir_06: 100 aggressive Poison Spores (lv26) - a swarm for lv38-60 travellers, not for lv80.
func TestSwarmDanger(t *testing.T) {
	mobs[991077] = &Mob{ID: 991077, Name: "Poison Spore", Level: 26, Aggressive: true}
	mobs[991002] = &Mob{ID: 991002, Name: "Poring", Level: 1}
	spawnsByMap["test_swarm"] = []Spawn{{Map: "test_swarm", MobID: 991077, Count: 100}, {Map: "test_swarm", MobID: 991002, Count: 50}}
	spawnsByMap["test_few"] = []Spawn{{Map: "test_few", MobID: 991077, Count: 20}}
	defer func() {
		delete(mobs, 991077)
		delete(mobs, 991002)
		delete(spawnsByMap, "test_swarm")
		delete(spawnsByMap, "test_few")
	}()
	for _, c := range []struct {
		m     string
		lv    int
		swarm bool
	}{{"test_swarm", 38, true}, {"test_swarm", 46, true}, {"test_swarm", 55, true}, {"test_swarm", 80, false},
		{"test_swarm", 25, false}, {"test_few", 40, false}} {
		if got := swarmMap(c.m, c.lv); got != c.swarm {
			t.Errorf("swarmMap(%s, %d) = %v (danger %.1f), want %v", c.m, c.lv, got, swarmDanger(c.m, c.lv), c.swarm)
		}
	}
	// lv25: the Spores aren't 5+ levels under it, so mapDanger counts them already (no double count)
	if d := swarmDanger("test_swarm", 25); d != 0 {
		t.Errorf("swarmDanger lv25 = %.1f, want 0", d)
	}
}

func TestLosingFightTransit(t *testing.T) {
	v := fightView{hp: 0.9, n: 3, maxLv: 26, lv: 46, pots: 20}
	if l, _ := losingFight(v); l {
		t.Fatal("hunting: 3 low monsters at 90% HP is not a lost fight")
	}
	v.transit = true
	if l, _ := losingFight(v); !l {
		t.Fatal("in transit on a swarm map: 3 on us = leave")
	}
	v.n = 2
	if l, _ := losingFight(v); l {
		t.Fatal("in transit, 2 on us at 90%: walk on")
	}
	v.hp = 0.68 // LilLilyblade (Monk 69) flew from 2 Spores at 68%, 6 times in 2 min
	if l, _ := losingFight(v); l {
		t.Fatal("in transit, 2 low monsters at 68%: walk on")
	}
	v.hp = 0.45
	if l, _ := losingFight(v); !l {
		t.Fatal("in transit, 2 on us under 50%: leave")
	}
}

func TestBossSpots(t *testing.T) {
	mobs[991096] = &Mob{ID: 991096, Name: "Angeling", Level: 77, Boss: true}
	defer delete(mobs, 991096)
	noteBoss("test_boss", 991096, 100, 100, true)
	if !bossSpotNear("test_boss", 105, 95, 12, 40) || bossSpotNear("test_boss", 130, 100, 12, 40) {
		t.Fatal("bossSpotNear: within 12 cells only")
	}
	if !bossRecent("test_boss", 40) || bossRecent("test_boss", 80) || bossRecent("test_other", 40) {
		t.Fatal("bossRecent: only for bots 10+ levels under the boss, only on that map")
	}
}
