package main

import (
	"os"
	"path/filepath"
	"testing"
)

// economy run 8: drop values follow the server's item_rate_* (owner: drops 5x), capped at 100%, switchable live.
func TestDropRates(t *testing.T) {
	old, oldR := tune(), *rathena
	defer func() { curTuning.Store(old); *rathena = oldR; dropConf.once = syncOnceFresh() }()
	items[909] = &Item{ID: 909, Name: "Jellopy", Type: "Etc", Buy: 6}
	items[4001] = &Item{ID: 4001, Name: "Poring Card", Type: "Card", Buy: 20}
	nt := *old
	nt.DropRateMul, nt.DropRatesFromConf = 0, false
	curTuning.Store(&nt)
	if effDropRate(909, 7000, false) != 7000 {
		t.Fatal("default: base rates")
	}
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "conf", "import"), 0o755)
	os.WriteFile(filepath.Join(dir, "conf", "import", "battle_conf.txt"), []byte("item_rate_common: 500\nitem_rate_card: 300 // cards\nitem_rate_card_boss: 100\n"), 0o644)
	*rathena = dir
	dropConf.once = syncOnceFresh()
	nt2 := nt
	nt2.DropRatesFromConf = true
	curTuning.Store(&nt2)
	if got := effDropRate(909, 1000, false); got != 5000 {
		t.Fatalf("common 5x: %d", got)
	}
	if got := effDropRate(909, 7000, false); got != 10000 {
		t.Fatalf("capped at 100%%: %d", got)
	}
	if got := effDropRate(4001, 1, false); got != 3 {
		t.Fatalf("card 3x: %d", got)
	}
	if got := effDropRate(4001, 1, true); got != 1 {
		t.Fatalf("boss card 1x: %d", got)
	}
	nt3 := nt
	nt3.DropRateMul = 2
	curTuning.Store(&nt3)
	if got := effDropRate(4001, 10, true); got != 20 {
		t.Fatalf("override 2x: %d", got)
	}
}
