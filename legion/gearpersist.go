package main

// Gear-turn persistence for human-led parties (Ayla's). Bot-led parties save their gear pool with the party in
// data/parties.json (partypersist.go); human-led parties are not in that file, so the instance that runs them
// (legion-core; ownsHumanParties) keeps their pool - turn, turn item, last-got times and the zeny each member raised
// for its unbought turn item - in data/parties_core.json (human_party_gear.json for a single instance).
// Lesson (2026-11-14): the raised-zeny record was memory-only; after a restart the money members had received for
// their own item became giftable again (Kimchi.senpai, Silent lord86). Zen.fox and Zendancer84 were funded for Wands.

import (
	"bytes"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

func humanGearFile() string {
	if coreInstance() {
		return filepath.Join(*dataDir, "parties_core.json")
	}
	return filepath.Join(*dataDir, "human_party_gear.json")
}

var humanGear = struct {
	sync.Mutex
	pending map[string]*gearPool // restored pools, taken by poolParty when the party is formed again
	last    []byte
}{pending: map[string]*gearPool{}}

// loadHumanGear: restore the saved pools (as pending) and the raised-zeny record at startup.
func loadHumanGear() {
	if !ownsHumanParties() {
		return
	}
	raw, err := os.ReadFile(humanGearFile())
	if err != nil {
		return
	}
	var saved map[string]*gearPool
	if json.Unmarshal(raw, &saved) != nil {
		return
	}
	gearPools.Lock() // lock order: gearPools, then humanGear (poolParty -> takeHumanGear); never both here
	for _, pl := range saved {
		if pl != nil {
			for n, z := range pl.Raised {
				gearPools.raised[n] = max(gearPools.raised[n], z)
			}
		}
	}
	gearPools.Unlock()
	humanGear.Lock()
	humanGear.last = raw
	for name, pl := range saved {
		if pl == nil {
			continue
		}
		if pl.LastGot == nil {
			pl.LastGot = map[string]time.Time{}
		}
		if pl.Turn != "" {
			pl.TurnSince = time.Now() // the holder gets its full grace to log back in after the restart
		}
		humanGear.pending[name] = pl
	}
	humanGear.Unlock()
	log.Printf("human party gear: restored %d pool(s) from %s", len(saved), humanGearFile())
}

// takeHumanGear: a restored pool for this party name, once (gearPools may be held by the caller: humanGear only).
func takeHumanGear(name string) *gearPool {
	humanGear.Lock()
	defer humanGear.Unlock()
	pl := humanGear.pending[name]
	delete(humanGear.pending, name)
	return pl
}

// saveHumanGear writes the human-led parties' pools when they changed. Never writes an empty snapshot (logouts at
// shutdown or a restart in progress must not wipe the file).
func saveHumanGear() {
	if !ownsHumanParties() {
		return
	}
	type hp struct {
		name    string
		members []*Bot
	}
	var hps []hp
	parties.Lock()
	for _, p := range parties.byName {
		if p.Human != "" {
			hps = append(hps, hp{p.Name, append([]*Bot(nil), p.Members...)})
		}
	}
	parties.Unlock()
	out := map[string]*gearPool{}
	for _, h := range hps {
		var names []string
		for _, m := range h.members {
			m.mu.Lock()
			if m.pin != nil {
				names = append(names, m.pin.Char)
			}
			m.mu.Unlock()
		}
		gearPools.Lock()
		if pl := gearPools.m[h.name]; pl != nil {
			cp := *pl
			cp.LastGot = make(map[string]time.Time, len(pl.LastGot))
			for k, v := range pl.LastGot {
				cp.LastGot[k] = v
			}
			cp.Raised = nil
			for _, n := range names {
				if z := gearPools.raised[n]; z > 0 {
					if cp.Raised == nil {
						cp.Raised = map[string]int{}
					}
					cp.Raised[n] = z
				}
			}
			out[h.name] = &cp
		}
		gearPools.Unlock()
	}
	// pools restored but not taken yet (the party is still re-forming) are kept as they were
	humanGear.Lock()
	defer humanGear.Unlock()
	for n, pl := range humanGear.pending {
		if out[n] == nil {
			out[n] = pl
		}
	}
	if len(out) == 0 {
		return
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil || bytes.Equal(b, humanGear.last) {
		return
	}
	if err := writeFileAtomic(humanGearFile(), b); err != nil {
		log.Printf("human party gear: save failed: %v", err)
		return
	}
	humanGear.last = b
}
