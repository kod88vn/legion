package main

import "testing"

func TestRecentDeathsForBand(t *testing.T) {
	for i := 0; i < 5; i++ {
		reportDeathLv("test_band", 4) // Novices dying on a beginner field
	}
	if d := recentDeathsFor("test_band", 55); d != 0 {
		t.Errorf("lv55 sees %.1f Novice deaths, want 0", d)
	}
	if d := recentDeathsFor("test_band", 6); d < 4.9 {
		t.Errorf("lv6 sees %.1f deaths, want ~5", d)
	}
	if d := recentDeathsFor("test_band", 14); d < 2.4 || d > 2.6 {
		t.Errorf("lv14 (next band) sees %.1f, want ~2.5", d)
	}
}
