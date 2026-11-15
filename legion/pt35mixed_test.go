package main

import (
	"sync/atomic"
	"testing"
)

// TestPT35MixedPartyMap (PT35, review dd1ab44): a lv50 + lv85 party. Its map pickers choose for the weakest member,
// and on a map whose guide band tops out at lv55 the lv85 gets almost no exp. The strongest member's map must win
// whenever a map suits both members; the weakest-only choice is the fallback.
func TestPT35MixedPartyMap(t *testing.T) {
	saved := guideSpots
	oh := partyHops
	guideSpots = []GuideSpot{
		{Min: 40, Max: 55, Map: "zz_mixa"}, // suits lv50, gives a lv85 almost nothing
		{Min: 46, Max: 90, Map: "zz_mixb"}, // suits lv50 and lv85
	}
	partyHops = func(a string, ax, ay int, b string) int { return 0 }
	spawnsByMap["zz_mixa"] = []Spawn{{Map: "zz_mixa", MobID: 990201, Count: 100}}
	spawnsByMap["zz_mixb"] = []Spawn{{Map: "zz_mixb", MobID: 990202, Count: 60}, {Map: "zz_mixb", MobID: 990203, Count: 40}}
	mobs[990201], mobs[990202], mobs[990203] = &Mob{Level: 45}, &Mob{Level: 50}, &Mob{Level: 70}
	maps["zz_mixa"], maps["zz_mixb"] = testMap("zz_mixa", 20, 20), testMap("zz_mixb", 20, 20)
	defer func() {
		guideSpots, partyHops = saved, oh
		for _, m := range []string{"zz_mixa", "zz_mixb"} {
			delete(spawnsByMap, m)
			delete(maps, m)
		}
		for _, id := range []int{990201, 990202, 990203} {
			delete(mobs, id)
		}
		pubs.Delete("zz_mixweak")
		pubs.Delete("zz_mixstrong")
	}()
	mk := func(name string, lv int) *Bot {
		b := &Bot{P: &Persona{Name: name, BaseLevel: lv}, BaseLv: lv}
		v := &atomic.Value{}
		v.Store(pubState{Name: name, Lv: lv, Power: lv, Online: true, Known: true})
		pubs.Store(name, v)
		return b
	}
	ms := []*Bot{mk("zz_mixweak", 50), mk("zz_mixstrong", 85)}
	if !partyMixedLevels(ms) || partyMixedLevels([]*Bot{ms[0], ms[0]}) {
		t.Fatal("setup: a lv50 + lv85 party is mixed, a party of one level is not")
	}
	if !mapExpPoor("zz_mixa", partyMaxBase(ms)) || mapExpPoor("zz_mixb", partyMaxBase(ms)) {
		t.Fatal("setup: zz_mixa is exp-poor for lv85, zz_mixb is not")
	}
	if mapExpPoor("zz_mixa", partyMinBase(ms)) {
		t.Fatal("setup: zz_mixa suits the lv50 weakest member")
	}
	// The picker must choose zz_mixb every time: zz_mixa is as near (equal score), so only the strongest-exp rule
	// separates them. Both the stage used by partyMapNear and the tier picker are checked.
	for i := 0; i < 20; i++ {
		if m := partyMapNearTier(ms, "", 0, 0, "", false); m != "zz_mixb" {
			t.Fatalf("mixed lv50+lv85 party: map tier picked %q, want zz_mixb (exp for both)", m)
		}
	}
	if m := partyMapNear(ms, "", 0, 0, ""); m != "zz_mixb" {
		t.Fatalf("mixed lv50+lv85 party: partyMapNear picked %q, want zz_mixb", m)
	}
	// Fallback: when no map gives the lv85 exp, the old choice (a map that suits the weakest member) is kept.
	maps["zz_mixb"] = nil
	if m := partyMapNearTier(ms, "", 0, 0, "", false); m != "zz_mixa" {
		t.Fatalf("no map gives both exp: picked %q, want the weakest-member fallback zz_mixa", m)
	}
	// A party of one level keeps the old behaviour: zz_mixa is no worse than zz_mixb for it.
	same := []*Bot{mk("zz_mixweak", 50), mk("zz_mixstrong", 50)}
	if partyMixedLevels(same) {
		t.Fatal("two lv50 members are not a mixed-level party")
	}
}

// TestHealerLeaveWhy (PT35, lfg.go partyGoalFit healer branch): a healer leaves a party on exp grounds when its map
// gives it no real exp any more, or on band grounds when the spread outgrew the band; with 2 levels of slack, so
// it does not leave the moment it is at the edge of what it may join.
func TestHealerLeaveWhy(t *testing.T) {
	if got := healerLeaveWhy(60, nil, "pay_fild04"); got != "" {
		t.Errorf("no other known members: stays, got %q", got)
	}
	if got := healerLeaveWhy(60, []int{62, 65}, ""); got != "" {
		t.Errorf("a lv60-65 party with no map: in band, stays, got %q", got)
	}
	if got := healerLeaveWhy(60, []int{61}, "pay_fild04"); got != "exp" {
		t.Errorf("lv60 healer on a lv22-36 map (no exp for it): got %q, want exp", got)
	}
	if got := healerLeaveWhy(20, []int{88}, ""); got != "band" {
		t.Errorf("lv20 healer in a lv88 party: got %q, want band", got)
	}
	// slack: a lv52 healer on pay_fild04 (joining refused at 52) stays; at 54 the map gives it no exp any more
	if got := healerLeaveWhy(52, []int{52}, "pay_fild04"); got != "" {
		t.Errorf("lv52 healer on pay_fild04 with 2 levels of slack: got %q, want stays", got)
	}
	if got := healerLeaveWhy(54, []int{54}, "pay_fild04"); got != "exp" {
		t.Errorf("lv54 healer on pay_fild04: got %q, want exp", got)
	}
}
