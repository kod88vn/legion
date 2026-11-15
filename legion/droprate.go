package main

// droprate.go (economy run 8): the server's item drop-rate multipliers. Legion reads the base mob db (1x rates);
// rathena multiplies every drop by battle_conf item_rate_* (owner 2026-11-14: all drops 5x at the next rathena
// restart; a drop chance is capped at 100%). Everything that turns drop rates into value (lootvalue.go mapLootZeny,
// farm.go mobTradeZ / farmTargets, pricing.go rarity, buildgear.go pieceFarms, zenyhour.go, the lookup answers) asks
// effDropRate. Live switch (tuning.json, no restart): drop_rates_from_conf=true reads the multipliers from
// /rathena/conf/import/battle_conf.txt (docker-compose mounts it read-only) - flip it when rathena restarts with them;
// drop_rate_mul > 0 overrides every multiplier. Default: 1x (the base rates).

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

var dropConf struct {
	once sync.Once
	m    map[string]int // item_rate_common: 500 ...
}

func loadDropConf() {
	dropConf.m = map[string]int{}
	f, err := os.Open(filepath.Join(*rathena, "conf", "import", "battle_conf.txt"))
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		t := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(t, "item_rate_") {
			continue
		}
		k, v, ok := strings.Cut(t, ":")
		if !ok {
			continue
		}
		if i := strings.Index(v, "//"); i >= 0 {
			v = v[:i]
		}
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n > 0 {
			dropConf.m[strings.TrimSpace(k)] = n
		}
	}
}

// dropKind: the battle_conf item_rate_ group of an item type
func dropKind(typ string) string {
	switch typ {
	case "Etc":
		return "common"
	case "Healing":
		return "heal"
	case "Weapon", "Armor", "Shadowgear":
		return "equip"
	case "Card":
		return "card"
	}
	return "use"
}

// dropMul: the server's multiplier for drops of this item type from a normal / boss monster (1 = base rates)
func dropMul(typ string, boss bool) float64 {
	tn := tune()
	if tn.DropRateMul > 0 {
		return tn.DropRateMul
	}
	if !tn.DropRatesFromConf {
		return 1
	}
	dropConf.once.Do(loadDropConf)
	k := "item_rate_" + dropKind(typ)
	if boss {
		if v, ok := dropConf.m[k+"_boss"]; ok {
			return float64(v) / 100
		}
	}
	if v, ok := dropConf.m[k]; ok {
		return float64(v) / 100
	}
	return 1
}

// effDropRate: a drop's chance on this server, in 1/10000 (capped at 100%)
func effDropRate(item, rate int, boss bool) int {
	typ := "Etc"
	if x := items[item]; x != nil {
		typ = x.Type
	}
	return min(10000, int(float64(rate)*dropMul(typ, boss)+0.5))
}

// dropSig: changes whenever the multipliers do (cache keys)
func dropSig() float64 { return dropMul("Etc", false)*1000 + dropMul("Card", false) }

// syncOnceFresh: a new sync.Once (tests re-read the conf)
func syncOnceFresh() sync.Once { return sync.Once{} }
