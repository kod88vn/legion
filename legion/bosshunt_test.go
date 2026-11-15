package main

import (
	"os"
	"sync"
	"testing"
	"time"
)

func TestBossReq(t *testing.T) {
	if lv, n := bossReq(65, true); lv != 55 || n != 4 {
		t.Fatalf("Eddga lv65 MVP: party lv%d x%d, want 55 x4", lv, n)
	}
	if lv, n := bossReq(18, false); lv != 13 || n != 3 {
		t.Fatalf("Vocal lv18 mini-boss: party lv%d x%d, want 13 x3", lv, n)
	}
}

func TestBossHuntTargetOnlyOnBossMap(t *testing.T) {
	a, c := &Bot{P: &Persona{Name: "bh_a"}}, &Bot{P: &Persona{Name: "bh_c"}}
	p := &Party{Name: "bhtest", Leader: a, Members: []*Bot{a, c}}
	parties.Lock()
	parties.ofBot[a], parties.ofBot[c] = p, p
	parties.Unlock()
	defer func() {
		parties.Lock()
		delete(parties.ofBot, a)
		delete(parties.ofBot, c)
		parties.Unlock()
		bossHunts.Lock()
		delete(bossHunts.active, p)
		bossHunts.Unlock()
	}()
	h := &bossHunt{Boss: bossInfo{ID: 1115, Name: "Eddga", Map: "pay_fild11"}, Phase: "search", Started: time.Now(), party: p}
	bossHunts.Lock()
	bossHunts.active[p] = h
	bossHunts.Unlock()
	a.MapName, c.MapName = "pay_fild11", "payon"
	if a.bossHuntTarget() != 1115 || c.bossHuntTarget() != 0 {
		t.Fatal("boss target only for members on the boss map")
	}
	if !p.bossActive() {
		t.Fatal("party should be on a boss hunt")
	}
	h.Phase = "travel"
	if a.bossHuntTarget() != 0 {
		t.Fatal("no boss fighting while travelling")
	}
	h.Phase = "retreat"
	if p.bossActive() {
		t.Fatal("a retreated hunt is over")
	}
}

func TestBossCatalogLive(t *testing.T) {
	rd := os.Getenv("LEGION_RATHENA")
	if rd == "" {
		t.Skip("set LEGION_RATHENA to a copy of /rathena")
	}
	*rathena = rd
	loadGameData()
	lookupOnce = sync.Once{}
	for _, q := range []string{"Eddga", "Orc Hero", "Mistress", "Phreeoni", "Golden Thief Bug", "Osiris", "Doppelganger", "Moonlight Flower", "Vocal"} {
		b := findBoss(q)
		if b == nil {
			t.Errorf("%s: not in the catalog", q)
			continue
		}
		t.Logf("%-18s id %d lv%d mvp=%v %s respawn %d min reach=%v party lv%d x%d", b.Name, b.ID, b.Level, b.MVP, b.Map, b.Respawn, b.Reach, b.MinLv, b.Members)
	}
	t.Logf("catalog: %d bosses", len(bossCatalog()))
}
