package main

import (
	"net/http"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"
)

// TestLookupMatcher: the fuzzy matcher finds items by full name, short forms, initials, word prefixes and typos.
func TestLookupMatcher(t *testing.T) {
	saved := items
	defer func() { items = saved }()
	items = map[int]*Item{
		4086:  {ID: 4086, Aegis: "Soldier_Skeleton_Card", Name: "Soldier Skeleton Card", Type: "Card"},
		4450:  {ID: 4450, Aegis: "P_Soldier_Skeleton_Card", Name: "Enhanced Soldier Skeleton Card", Type: "Card"},
		932:   {ID: 932, Aegis: "Skel_Bone", Name: "Skel-Bone"},
		505:   {ID: 505, Aegis: "Blue_Potion", Name: "Blue Potion", Type: "Healing"},
		501:   {ID: 501, Aegis: "Red_Potion", Name: "Red Potion", Type: "Healing"},
		985:   {ID: 985, Aegis: "Elunium", Name: "Elunium"},
		607:   {ID: 607, Aegis: "Yggdrasilberry", Name: "Yggdrasil Berry", Type: "Healing"},
		4001:  {ID: 4001, Aegis: "Poring_Card", Name: "Poring Card", Type: "Card"},
		12000: {ID: 12000, Aegis: "Sample", Name: "Soldier Sword"},
	}
	lookupOnce.Do(func() {}) // no game files in unit tests
	cases := []struct {
		q    string
		want int
	}{
		{"SS card", 4086}, {"ss card", 4086}, {"Soldier Skeleton card", 4086}, {"soldier skeleton card", 4086},
		{"soldier_skeleton_card", 4086}, {"4086", 4086}, {"ssc", 4086}, {"soldier skel card", 4086},
		{"blue pot", 505}, {"elu", 985}, {"ygg", 607}, {"yggdrasil berry", 607}, {"skel bone", 932},
		{"poring crad", 4001}, {"soldier skeleton cards", 4086},
	}
	for _, c := range cases {
		hits := findItems(c.q, 5)
		if len(hits) == 0 || hits[0].ID != c.want {
			t.Errorf("findItems(%q) = %v, want %d first", c.q, hits, c.want)
		}
	}
	if hits := findItems("zzzz qqq", 5); len(hits) != 0 {
		t.Errorf("nonsense matched %v", hits)
	}
	// ambiguous: "potion" returns both potions
	if hits := findItems("potion", 5); len(hits) < 2 {
		t.Errorf("potion: want several matches, got %v", hits)
	}
}

// TestLookupLive (skipped unless LEGION_RATHENA is set): real data; prints what "SS card" resolves to. With
// LEGION_LOOKUP_SERVE=127.0.0.1:18090 it also serves /api/lookup/* for that long (LEGION_LOOKUP_SECS, default 60)
// so legionctl can be tried against it: LEGION_API=http://127.0.0.1:18090 python tools/legionctl.py drops "SS card".
func TestLookupLive(t *testing.T) {
	rd := os.Getenv("LEGION_RATHENA")
	if rd == "" {
		t.Skip("set LEGION_RATHENA to a copy of /rathena")
	}
	*rathena = rd
	loadGameData()
	lookupOnce = sync.Once{} // TestLookupMatcher marked it done without data
	res := lookupQuery("item", "SS card", []int{69, 70, 72})
	if res == nil {
		t.Fatal("SS card: no match")
	}
	t.Logf("SS card matches: %v", res["matches"])
	r := res["result"].(map[string]interface{})
	if r["id"].(int) != 4086 {
		t.Fatalf("SS card -> %v", r["id"])
	}
	farm := r["farm_maps"].([]map[string]interface{})
	if len(farm) == 0 {
		t.Fatal("no farm maps for SS card")
	}
	t.Logf("SS card: best %v %v x%v (%v)", farm[0]["map"], farm[0]["monster"], farm[0]["count"], farm[0]["kind"])
	if mobAns := lookupQuery("monster", "soldier skeleton", nil); mobAns == nil {
		t.Fatal("monster lookup failed")
	}
	if m := lookupQuery("map", "pay_dun01", nil); m == nil || m["result"].(map[string]interface{})["kind"] != "dungeon" {
		t.Fatalf("pay_dun01 kind: %v", m)
	}
	if addr := os.Getenv("LEGION_LOOKUP_SERVE"); addr != "" {
		secs, _ := strconv.Atoi(os.Getenv("LEGION_LOOKUP_SECS"))
		if secs <= 0 {
			secs = 60
		}
		mux := http.NewServeMux()
		registerLookup(mux)
		srv := &http.Server{Addr: addr, Handler: mux}
		go srv.ListenAndServe()
		time.Sleep(time.Duration(secs) * time.Second)
		srv.Close()
	}
}
