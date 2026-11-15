package main

import "testing"

// every 2nd-job plan has a trans plan, and each trans plan holds at least one transcendent skill
func TestTransPlansCoverBuilds(t *testing.T) {
	for id := range builds {
		tp := transFor(builds[id])
		if tp == nil {
			t.Fatalf("plan %s has no trans skills", id)
		}
		trans := false
		for _, g := range tp {
			if g.Lv < 1 || g.Lv > 10 {
				t.Errorf("%s: skill %d lv %d", id, g.ID, g.Lv)
			}
			if (g.ID >= 355 && g.ID <= 407) || (g.ID >= 475 && g.ID <= 490) {
				trans = true
			}
		}
		if !trans {
			t.Errorf("%s: no transcendent skill", id)
		}
	}
}

// a Lord Knight raises its trans goals before the tail of the knight plan; a Knight never sees them
func TestPlanSkillTrans(t *testing.T) {
	mk := func(class, done int) *Bot {
		b := &Bot{Class: class, skills: map[int]*SkillInfo{}}
		b.P = &Persona{Chars: []CharSpec{{Plan: "knight", Role: "main"}}}
		for _, id := range []int{1, 2, 3, 4, 5, 6, 7, 8, 60, 61, 62, 63, 64} {
			b.skills[id] = &SkillInfo{Lv: 0, Upgradable: true}
		}
		// the knight plan's front done: the first `done` goals
		for _, g := range append(append([]SkillGoal{}, noviceSkills...), builds["knight"].Skills[:done]...) {
			b.skills[g.ID].Lv = max(b.skills[g.ID].Lv, g.Lv)
		}
		return b
	}
	k := mk(7, 7)                        // the Swordman part done
	if got := k.planSkill(); got != 63 { // Peco Peco Riding is the first Knight skill (owner: peco asap)
		t.Fatalf("knight next skill %d, want 63", got)
	}
	lk := mk(4008, 12)                            // up to Bowling Bash 10
	lk.skills[355] = &SkillInfo{Upgradable: true} // Aura Blade offered to a Lord Knight
	if got := lk.planSkill(); got != 355 {
		t.Fatalf("lord knight next skill %d, want 355 (Aura Blade)", got)
	}
}
