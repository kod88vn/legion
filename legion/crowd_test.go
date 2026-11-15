package main

import (
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

func TestCrowdCap(t *testing.T) {
	spawnsByMap["zz_fild01"] = []Spawn{{Map: "zz_fild01", MobID: 990001, Count: 100}}
	mobs[990001] = &Mob{Level: 5}
	defer func() { delete(spawnsByMap, "zz_fild01"); delete(mobs, 990001) }()
	add := func(name, m, goal string, roaming bool) {
		v := &atomic.Value{}
		v.Store(pubState{Name: name, Map: m, Goal: goal, Online: true, Roaming: roaming})
		pubs.Store(name, v)
	}
	defer func() {
		pubs.Range(func(k, _ any) bool {
			if s, _ := k.(string); len(s) > 3 && s[:3] == "zz_" {
				pubs.Delete(k)
			}
			return true
		})
		crowd.Lock()
		crowd.at = time.Time{}
		crowd.Unlock()
	}()
	if c := mapCap("zz_fild01"); c != 20 {
		t.Fatalf("cap %d, want 100/5 = 20", c)
	}
	for i := 0; i < 15; i++ { // 15 hunting there + 3 on their way
		add(fmt.Sprintf("zz_%d", i), "zz_fild01", "zz_fild01", false)
	}
	for i := 15; i < 18; i++ {
		add(fmt.Sprintf("zz_%d", i), "prontera", "zz_fild01", false)
	}
	crowd.Lock()
	crowd.at = time.Time{}
	crowd.Unlock()
	if l, _ := mapLoad("zz_fild01"); l != 18 {
		t.Fatalf("load %d, want 18", l)
	}
	if !mapHasRoom("zz_fild01", 2) || mapHasRoom("zz_fild01", 3) {
		t.Fatal("room for 2 more, not 3")
	}
	crowdNote("zz_fild01", 2)
	if mapHasRoom("zz_fild01", 1) {
		t.Fatal("a reserved seat must count")
	}
	for i := 0; i < 6; i++ { // 6 of 18 roaming: 33% >= 30% -> full
		add(fmt.Sprintf("zz_%d", i), "zz_fild01", "zz_fild01", true)
	}
	crowd.Lock()
	crowd.at = time.Time{}
	crowd.Unlock()
	if !mapCrowded("zz_fild01") || mapHasRoom("zz_fild01", 0) {
		t.Fatal("a map of roamers is full")
	}
}
