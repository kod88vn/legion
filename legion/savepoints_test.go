package main

import "testing"

func TestIsSaveAsk(t *testing.T) {
	yes := []string{"at the kafra, save here and wait", "Zen.fox save at the kafra here pls, say when saved", "everyone save here pls"}
	no := []string{"saved at geffen", "town run to geffen, meet at the kafra", "ok going in"}
	for _, s := range yes {
		if !isSaveAsk(s) {
			t.Errorf("%q should be a save ask", s)
		}
	}
	for _, s := range no {
		if isSaveAsk(s) {
			t.Errorf("%q is no save ask", s)
		}
	}
}

func TestSaveWingHelps(t *testing.T) {
	if !saveWingHelps(1, 4) || saveWingHelps(4, 2) || saveWingHelps(99, 120) {
		t.Fatal("wing home only when the save town is nearer the run town")
	}
}
