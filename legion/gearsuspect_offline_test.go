package main

// gearsuspect_offline_test.go: offline checks of the gear-aware level (c2b1e68, bug b1794773272q31) against party map
// acceptance. Real item stats (item_db_equip.yml), mob DB, map cache and spawn scripts are read with the engine's own
// loaders from a rAthena tree (LEGION_RATHENA, default testdata/rathena, git-ignored); characters, levels, classes and
// worn gear come from the read-only DB snapshot (LEGION_SNAP). Both tests skip when either is missing.
//
// Each character at lv21-60 is a one-member party. partyMapOK (the test lfgMapOK's map check uses) is asked for every
// map within gsBandLv levels of the character. Crowd state is empty: no live readings.
//   - TestGearSuspectLfgMapOK: the gear rating (gear_score_on) on and off.
//   - TestGearSuspectPerfStep: the gear rating plus a measured kills/deaths step (partyPerf, gearscore.go) of -15..+10.

import (
	"compress/gzip"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// gsBandLv: candidate maps are the ones within this many levels of the character
const gsBandLv = 10

type gsSnap struct {
	Tables map[string]struct {
		Cols []string        `json:"cols"`
		Rows [][]interface{} `json:"rows"`
	} `json:"tables"`
}

func gsCol(cols []string, name string) int {
	for i, c := range cols {
		if c == name {
			return i
		}
	}
	return -1
}

func gsInt(v interface{}) int {
	if f, ok := v.(float64); ok {
		return int(f)
	}
	return 0
}

// loadGearSnapshot: characters at lv21-60 with the gear they wear (equip mask -> item per slot bit).
func loadGearSnapshot(path string) (chars []*Bot, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	var s gsSnap
	if err := json.NewDecoder(zr).Decode(&s); err != nil {
		return nil, err
	}
	ch, inv := s.Tables["char"], s.Tables["inventory"]
	cID, cClass, cLv, cName, cMap := gsCol(ch.Cols, "char_id"), gsCol(ch.Cols, "class"), gsCol(ch.Cols, "base_level"),
		gsCol(ch.Cols, "name"), gsCol(ch.Cols, "last_map")
	iChar, iItem, iEquip := gsCol(inv.Cols, "char_id"), gsCol(inv.Cols, "nameid"), gsCol(inv.Cols, "equip")
	gear := map[int]map[uint32]*InvItem{}
	for _, r := range inv.Rows {
		mask := uint32(gsInt(r[iEquip]))
		if mask == 0 {
			continue
		}
		id := gsInt(r[iChar])
		it := &InvItem{ItemID: gsInt(r[iItem]), Loc: mask, Equipped: true}
		if gear[id] == nil {
			gear[id] = map[uint32]*InvItem{}
		}
		for bit := uint32(1); bit != 0 && bit <= mask; bit <<= 1 {
			if mask&bit != 0 {
				gear[id][bit] = it
			}
		}
	}
	for _, r := range ch.Rows {
		lv := gsInt(r[cLv])
		id := gsInt(r[cID])
		if lv < 21 || lv > 60 || gear[id] == nil {
			continue
		}
		name, _ := r[cName].(string)
		m, _ := r[cMap].(string)
		chars = append(chars, &Bot{P: &Persona{Name: "gsz_" + name, BaseLevel: lv, Aggression: 0.5,
			Chars: []CharSpec{{Name: "gsz_" + name}}}, BaseLv: lv,
			Class: gsInt(r[cClass]), Online: true, invSeen: true, equipped: gear[id], MapName: m})
	}
	sort.Slice(chars, func(i, j int) bool { return chars[i].P.Name < chars[j].P.Name })
	return chars, nil
}

// gsData: the loaded test world (loaded once: the spawn loader appends to spawnsByMap).
var gsData struct {
	once  sync.Once
	chars []*Bot
	maps  []string
	err   error
}

// gsLoad loads the engine's data from the rAthena tree and the characters from the snapshot. It skips the test when
// either is missing.
func gsLoad(t *testing.T) ([]*Bot, []string) {
	root := os.Getenv("LEGION_RATHENA")
	if root == "" {
		root = "testdata/rathena"
	}
	snap := os.Getenv("LEGION_SNAP")
	if snap == "" {
		snap = "G:/RO/legion/tools/.state/dbsnap/snap-20261115-151837.json.gz"
	}
	if _, err := os.Stat(filepath.Join(root, "db", "re", "item_db_equip.yml")); err != nil {
		t.Skip("rAthena data not copied (testdata/rathena): " + err.Error())
	}
	if _, err := os.Stat(snap); err != nil {
		t.Skip("DB snapshot not found: " + err.Error())
	}
	gsData.once.Do(func() {
		// the engine's own loaders (main.go loadGameData): item stats, mob DB, map cache, spawn scripts
		loadItemDB(filepath.Join(root, "db", "re", "item_db_equip.yml"))
		if err := loadMobDB(filepath.Join(root, "db", "re", "mob_db.yml")); err != nil {
			gsData.err = err
			return
		}
		for _, f := range []string{filepath.Join(root, "db", "map_cache.dat"), filepath.Join(root, "db", "re", "map_cache.dat")} {
			if err := loadMapCache(f); err != nil {
				gsData.err = err
				return
			}
		}
		loadNPCs(filepath.Join(root, "npc", "re", "mobs"))
		gsData.chars, gsData.err = loadGearSnapshot(snap)
		for m := range spawnsByMap {
			if !isTown(m) && mapMeanLv(m) > 0 {
				gsData.maps = append(gsData.maps, m)
			}
		}
		sort.Strings(gsData.maps)
	})
	if gsData.err != nil {
		t.Fatal(gsData.err)
	}
	if len(gsData.chars) == 0 || len(gsData.maps) == 0 {
		t.Skipf("nothing to measure: %d characters, %d maps", len(gsData.chars), len(gsData.maps))
	}
	return gsData.chars, gsData.maps
}

// gsSetPower: publish the bot's effective level under one gear_score_on setting (no solo performance step: a party
// member, PartyID set, as the party pickers see it).
func gsSetPower(b *Bot, on bool) {
	nt := defaultTuning
	nt.GearScoreOn = on
	curTuning.Store(&nt)
	b.PartyID = 1
	v := &atomic.Value{}
	v.Store(pubState{Name: b.P.Name, Lv: b.BaseLv, Power: b.powerLv(), Online: true, Known: true, Map: b.MapName})
	pubs.Store(b.P.Name, v)
}

// gsRestoreTuning: put back the tuning the test found (nil = defaults).
func gsRestoreTuning(old interface{}) {
	if v, ok := old.(*Tuning); ok && v != nil {
		curTuning.Store(v)
	} else {
		curTuning.Store(&defaultTuning)
	}
}

// gsPerfSet: a settled party performance step for key (one member, window fresh), as partyPerf.offset would hold it.
func gsPerfSet(key, m string, off int) {
	partyPerf.mu.Lock()
	partyPerf.s[key] = &perfState{m: m, n: 1, at: time.Now(), off: off}
	partyPerf.mu.Unlock()
}

// gsPerfReset: forget every party step (the tracker is global).
func gsPerfReset() {
	partyPerf.mu.Lock()
	partyPerf.s = map[string]*perfState{}
	partyPerf.mu.Unlock()
}

// gsAccepted: candidate maps near the character's level, and how many partyMapOK accepts for a one-member party.
func gsAccepted(b *Bot, maps []string) (cands, ok int) {
	for _, m := range maps {
		if mm := mapMeanLv(m); mm < b.BaseLv-gsBandLv || mm > b.BaseLv+gsBandLv {
			continue
		}
		cands++
		if partyMapOK([]*Bot{b}, m) {
			ok++
		}
	}
	return
}

func gsBand(lv int) string {
	switch {
	case lv <= 30:
		return "lv21-30"
	case lv <= 45:
		return "lv31-45"
	}
	return "lv46-60"
}

var gsBands = []string{"lv21-30", "lv31-45", "lv46-60"}

// TestGearSuspectLfgMapOK: the gear rating (gear_score_on) on against off, per level band.
func TestGearSuspectLfgMapOK(t *testing.T) {
	chars, maps := gsLoad(t)
	old := curTuning.Load()
	defer gsRestoreTuning(old)
	type acc struct{ chars, onOK, offOK, maps, deltaSum, offDelta int }
	res := map[string]*acc{}
	for _, b := range chars {
		k := gsBand(b.BaseLv)
		if res[k] == nil {
			res[k] = &acc{}
		}
		a := res[k]
		a.chars++
		gsSetPower(b, true)
		pOn := b.powerLv()
		gsSetPower(b, false)
		pOff := b.powerLv()
		a.deltaSum += pOn - b.BaseLv
		a.offDelta += pOff - b.BaseLv
		gsSetPower(b, true)
		c, ok := gsAccepted(b, maps)
		a.maps += c
		a.onOK += ok
		gsSetPower(b, false)
		_, ok = gsAccepted(b, maps)
		a.offOK += ok
	}
	for _, k := range gsBands {
		a := res[k]
		if a == nil {
			continue
		}
		t.Logf("%s: %d chars, mean power-base on %+.1f off %+.1f; candidate maps %d: accepted with gear_score_on %d, off %d",
			k, a.chars, float64(a.deltaSum)/float64(a.chars), float64(a.offDelta)/float64(a.chars), a.maps, a.onOK, a.offOK)
		// evidence threshold (bug b1794773272q31): the gear-rated level must not by itself cut the map acceptance of
		// a level band below 80% of what the old base-level rule accepts.
		if a.offOK > 0 && a.onOK*100 < a.offOK*80 {
			t.Errorf("%s: gear-rated level accepts %d maps against %d without it (below 80%%)", k, a.onOK, a.offOK)
		}
	}
}

// gsPerfSteps: the measured kills/deaths step (gearscore.go perfMin..perfMax) applied to the party's level. -3 is one
// verdict "down" (deaths), +2/+3 "up" (easy farming), -15 the floor after repeated deaths.
var gsPerfSteps = []int{-15, -9, -6, -3, 0, 2, 3, 6, 10}

// TestGearSuspectPerfStep: acceptance with the gear rating and a settled party step, per band, against step 0.
func TestGearSuspectPerfStep(t *testing.T) {
	chars, maps := gsLoad(t)
	old := curTuning.Load()
	defer gsRestoreTuning(old)
	defer gsPerfReset()
	count := map[string]map[int]int{}
	cands := map[string]int{}
	for _, b := range chars {
		k := gsBand(b.BaseLv)
		if count[k] == nil {
			count[k] = map[int]int{}
		}
		gsSetPower(b, true)
		for _, step := range gsPerfSteps {
			gsPerfSet(b.P.Name, b.MapName, step)
			c, ok := gsAccepted(b, maps)
			count[k][step] += ok
			if step == 0 {
				cands[k] += c
			}
		}
		gsPerfReset()
	}
	for _, k := range gsBands {
		if count[k] == nil {
			continue
		}
		line := ""
		for _, step := range gsPerfSteps {
			line += " " + itoa(step) + ":" + itoa(count[k][step])
		}
		t.Logf("%s: candidate maps %d; accepted by party step (gear rating on):%s", k, cands[k], line)
		// evidence threshold: one death verdict (-3) must not cut acceptance below 50% of the settled step 0
		if z := count[k][0]; z > 0 && count[k][-3]*100 < z*50 {
			t.Errorf("%s: a -3 step accepts %d maps against %d at step 0 (below 50%%)", k, count[k][-3], z)
		}
	}
}
