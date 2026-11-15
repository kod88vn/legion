package main

import (
	"math/rand"
	"strings"
	"testing"
	"time"
)

// ST01 / G10 (owner 2026-11-15: no rebirth on this server). No bot plans or seeks a transcendent class: every plan ends
// at its 2nd job, every new character's plan is a 2nd-job plan, and a 2nd-job bot at 99 keeps a job and gear goal.

// transNames: the transcendent 2nd jobs and their High Novice route. A plan's end goal must never be one of them.
var transNames = []string{"Lord Knight", "Paladin", "High Priest", "High Wizard", "Professor", "Sniper", "Clown",
	"Gypsy", "Whitesmith", "Creator", "Assassin Cross", "Stalker", "Champion"}

// TestNoPlanEndsAtTransclass: every build is a 2nd-job build (classes 7..20) and its summary names no transcendent class.
func TestNoPlanEndsAtTransclass(t *testing.T) {
	for id, p := range builds {
		if p.Class < 7 || p.Class > 20 {
			t.Errorf("build %s: class %d is not a 2nd job (7..20)", id, p.Class)
		}
		for _, n := range transNames {
			if strings.Contains(p.Summary, "end goal "+n) {
				t.Errorf("build %s: summary names transcendent end goal %q: %s", id, n, p.Summary)
			}
		}
	}
}

// TestPlanForClassNeverRebirth: whatever class a character is created or read as (novice, 1st, High Novice,
// transcendent, 3rd), planForClass returns a 2nd-job plan. High Novice is not a plan target.
func TestPlanForClassNeverRebirth(t *testing.T) {
	classes := []int{0, 1, 2, 3, 4, 5, 6, 23, 4001, 4002, 4003, 4004, 4005, 4006, 4007}
	for c := 4008; c <= 4021; c++ {
		classes = append(classes, c)
	}
	classes = append(classes, 4054, 4060, 4079)
	for _, sex := range []string{"M", "F"} {
		for _, c := range classes {
			for pick := 0; pick < 4; pick++ {
				p := planForClass(c, sex, func(n int) int { return (pick + n) % n })
				if p == nil {
					t.Fatalf("class %d (%s): no plan", c, sex)
				}
				if p.Class < 7 || p.Class > 20 {
					t.Errorf("class %d (%s): planned %s with class %d, not a 2nd job", c, sex, p.ID, p.Class)
				}
			}
		}
	}
}

// TestNoPlanSkillSeeksTrans: a 2nd-job bot with every plan skill it can learn is never offered a transcendent-only
// skill (transSkills) - those are for characters reborn before ST01 only.
func TestNoPlanSkillSeeksTrans(t *testing.T) {
	for id, p := range builds {
		b := &Bot{Class: p.Class, skills: map[int]*SkillInfo{}}
		b.P = &Persona{Chars: []CharSpec{{Plan: id, Role: "main", Class: p.Class}}}
		own := map[int]bool{}
		for _, g := range append(append([]SkillGoal{}, noviceSkills...), p.Skills...) {
			own[g.ID] = true
			b.skills[g.ID] = &SkillInfo{Upgradable: true}
		}
		for _, g := range transFor(p) {
			if !own[g.ID] {
				b.skills[g.ID] = &SkillInfo{Upgradable: true} // offered by the server, as on a Lord Knight
			}
		}
		for i := 0; i < 400; i++ {
			sid := b.planSkill()
			if sid < 0 {
				break
			}
			if !own[sid] {
				t.Fatalf("build %s (class %d): planSkill offered trans-only skill %d", id, p.Class, sid)
			}
			b.skills[sid].Lv++
		}
	}
}

// TestLevelGoalStopsAt99: a 2nd-job level goal never aims above 99; reborn classes keep lv+step.
func TestLevelGoalStopsAt99(t *testing.T) {
	cases := []struct{ class, lv, step, want int }{
		{7, 99, 1, 99},
		{7, 98, 2, 99},
		{16, 50, 2, 52},
		{9, 80, 1, 81},
		{4008, 99, 1, 100}, // already-reborn Lord Knight, unchanged
		{4001, 1, 2, 3},
	}
	for _, c := range cases {
		if got := levelGoalTarget(c.class, c.lv, c.step); got != c.want {
			t.Errorf("levelGoalTarget(%d, %d, %d) = %d, want %d", c.class, c.lv, c.step, got, c.want)
		}
	}
}

// TestLevel99SecondJobKeepsGoal: a 99/50 Knight picks a job-and-gear goal (never a bogus level 100 goal, never a
// rebirth), with no persona-level state needed beyond its plan.
func TestLevel99SecondJobKeepsGoal(t *testing.T) {
	b := &Bot{Class: 7, BaseLv: 99, JobLv: 50, rng: rand.New(rand.NewSource(7))}
	b.P = &Persona{BaseLevel: 99, Chars: []CharSpec{{Name: "Kn", Role: "main", Plan: "knight", Class: 7, Build: "knight", BaseLevel: 99, JobLevel: 50}}}
	b.skills = map[int]*SkillInfo{}
	g := b.pickFarmGoal(time.Now(), "")
	if g == nil {
		t.Fatal("a 99 knight got no goal")
	}
	if g.Kind == "level" && g.Target > baseLevelCap {
		t.Errorf("99 knight level goal aims at %d (above %d)", g.Target, baseLevelCap)
	}
}
