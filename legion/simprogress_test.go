package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
)

const (
	simProgressCohortSize = 1000
	simProgressTrainSeed  = int64(20261117)
	simProgressHoldout    = int64(20261118)
)

var simProgressFixtureSHA = map[string]string{
	"statpoint.yml":  "63d89dccba0026d37ade97aa6bfdf26137edc1982eef606392435042f94b8985",
	"skill_db.yml":   "8a7079c29182f4df0993043ba72dcc3ed5b741613ff0701eb6579bbace5c54ed",
	"skill_tree.yml": "84386e637e354912d9a38b081f4d922af945a19f5b5d16059e5f4693b23ec5f4",
}

var simProgressLevels = []int{1, 20, 40, 60, 80, 99}

type simProgressActor struct {
	ID             int
	BuildID        string
	Class          int
	BaseLevel      int
	JobLevel       int
	Stats          [6]int
	Skills         map[int]int
	StatSpent      int
	SkillSpent     int
	StatActions    int
	SkillActions   int
	StatPointsLeft int
	SkillPtsLeft   int
}

type simProgressWindow struct {
	BaseLevel    int
	JobLevel     int
	Actors       int
	StatActive   int
	StatActions  int
	StatStalled  int
	SkillActive  int
	SkillActions int
	SkillStalled int
	Classes      []simProgressClassWindow
}

type simProgressClassWindow struct {
	Class        int
	Actors       int
	StatActive   int
	StatActions  int
	StatStalled  int
	SkillActive  int
	SkillActions int
	SkillStalled int
}

type simProgressCohort struct {
	Seed       int64
	Actors     []simProgressActor
	Windows    []simProgressWindow
	NextWindow int
}

type simProgressObservation struct {
	BaseLevel  int
	JobLevel   int
	Class      int
	StatPoints int
	SkillPts   int
}

type simProgressSkill struct {
	Name     string
	MaxLevel int
}

func simProgressFixture(t *testing.T, name string) (string, []byte) {
	t.Helper()
	path := filepath.Join("..", "tests", "tools", "fixtures", "server", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read frozen fixture %s: %v", path, err)
	}
	sum := sha256.Sum256(data)
	if got, want := hex.EncodeToString(sum[:]), simProgressFixtureSHA[name]; got != want {
		t.Fatalf("frozen fixture %s SHA256 = %s, want %s", name, got, want)
	}
	return path, data
}

func simProgressParseStatPoints(t *testing.T, data []byte) map[int]int {
	t.Helper()
	out := map[int]int{}
	level := -1
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		switch {
		case strings.HasPrefix(line, "- Level: "):
			value, err := strconv.Atoi(strings.TrimPrefix(line, "- Level: "))
			if err != nil {
				t.Fatalf("parse statpoint level %q: %v", line, err)
			}
			level = value
		case strings.HasPrefix(line, "Points:"):
			value, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "Points:")))
			if err != nil || level < 1 {
				t.Fatalf("parse statpoint value %q: %v", line, err)
			}
			out[level] = value
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan statpoint fixture: %v", err)
	}
	return out
}

func simProgressParseSkillDB(t *testing.T, data []byte) map[int]simProgressSkill {
	t.Helper()
	out := map[int]simProgressSkill{}
	id := -1
	skill := simProgressSkill{}
	flush := func() {
		if id >= 0 {
			out[id] = skill
		}
	}
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		switch {
		case strings.HasPrefix(line, "- Id: "):
			flush()
			value, err := strconv.Atoi(strings.TrimPrefix(line, "- Id: "))
			if err != nil {
				t.Fatalf("parse skill ID %q: %v", line, err)
			}
			id, skill = value, simProgressSkill{}
		case id >= 0 && strings.HasPrefix(line, "Name: "):
			skill.Name = strings.Trim(strings.TrimPrefix(line, "Name: "), "\"")
		case id >= 0 && strings.HasPrefix(line, "MaxLevel: "):
			value, err := strconv.Atoi(strings.TrimPrefix(line, "MaxLevel: "))
			if err != nil {
				t.Fatalf("parse skill max level %q: %v", line, err)
			}
			skill.MaxLevel = value
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan skill DB fixture: %v", err)
	}
	flush()
	return out
}

func simProgressParseSkillTree(t *testing.T, data []byte) map[string]map[string]int {
	t.Helper()
	out := map[string]map[string]int{}
	job, skillName := "", ""
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		raw := scanner.Text()
		trimmed := strings.TrimSpace(raw)
		indent := len(raw) - len(strings.TrimLeft(raw, " "))
		switch {
		case indent == 2 && strings.HasPrefix(trimmed, "- Job: "):
			job = strings.Trim(strings.TrimPrefix(trimmed, "- Job: "), "\"")
			out[job] = map[string]int{}
			skillName = ""
		case indent == 6 && strings.HasPrefix(trimmed, "- Name: "):
			skillName = strings.Trim(strings.TrimPrefix(trimmed, "- Name: "), "\"")
		case indent == 8 && skillName != "" && strings.HasPrefix(trimmed, "MaxLevel: "):
			level, err := strconv.Atoi(strings.TrimPrefix(trimmed, "MaxLevel: "))
			if err != nil {
				t.Fatalf("parse skill-tree max level %q: %v", trimmed, err)
			}
			out[job][skillName] = level
			skillName = ""
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan skill tree fixture: %v", err)
	}
	return out
}

func simProgressBuildIDs() []string {
	ids := make([]string, 0, len(builds))
	for id := range builds {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func simProgressNewCohort(t *testing.T, seed int64, tree map[string]map[string]int, skillDB map[int]simProgressSkill) simProgressCohort {
	t.Helper()
	ids := simProgressBuildIDs()
	if len(ids) == 0 {
		t.Fatal("no production builds available")
	}
	assignments := make([]string, simProgressCohortSize)
	for i := range assignments {
		assignments[i] = ids[i%len(ids)]
	}
	rand.New(rand.NewSource(seed)).Shuffle(len(assignments), func(i, j int) {
		assignments[i], assignments[j] = assignments[j], assignments[i]
	})
	actors := make([]simProgressActor, len(assignments))
	for i, id := range assignments {
		plan := builds[id]
		if plan == nil {
			t.Fatalf("cohort actor %d references missing build %q", i, id)
		}
		allowed := map[string]int{}
		for _, job := range []string{"Novice", className(plan.First), className(plan.Class)} {
			for name, level := range tree[job] {
				if level > allowed[name] {
					allowed[name] = level
				}
			}
		}
		levels := map[int]int{}
		goals := append(append([]SkillGoal(nil), noviceSkills...), plan.Skills...)
		for _, goal := range goals {
			info, ok := skillDB[goal.ID]
			if !ok || info.Name == "" {
				t.Fatalf("build %s references skill %d missing from frozen skill_db.yml", id, goal.ID)
			}
			if goal.Lv < 1 || goal.Lv > info.MaxLevel {
				t.Fatalf("build %s skill %s level %d exceeds skill_db max %d", id, info.Name, goal.Lv, info.MaxLevel)
			}
			if goal.Lv > allowed[info.Name] {
				t.Fatalf("build %s skill %s level %d exceeds its frozen job trees (Novice/%s/%s)", id, info.Name, goal.Lv, className(plan.First), className(plan.Class))
			}
			if goal.Lv > levels[goal.ID] {
				levels[goal.ID] = goal.Lv
			}
		}
		stats := [6]int{1, 1, 1, 1, 1, 1}
		for _, goal := range plan.Stats {
			if goal.Stat < 0 || goal.Stat >= len(stats) || goal.Value < 1 || goal.Value > 99 {
				t.Fatalf("build %s has unsupported stat goal %+v", id, goal)
			}
		}
		actors[i] = simProgressActor{
			ID:       i,
			BuildID:  id,
			Class:    plan.Class,
			Stats:    stats,
			Skills:   map[int]int{},
			JobLevel: 0,
		}
		for skillID := range levels {
			actors[i].Skills[skillID] = 0
		}
	}
	return simProgressCohort{Seed: seed, Actors: actors}
}

func simProgressBot(actor *simProgressActor) *Bot {
	skillState := make(map[int]*SkillInfo, len(actor.Skills))
	for id, level := range actor.Skills {
		// Upgradable is a synthetic acknowledgment input; prerequisites and server admission are outside this cohort.
		skillState[id] = &SkillInfo{ID: id, Lv: level, Upgradable: true}
	}
	character := CharSpec{Plan: actor.BuildID, Role: "main"}
	return &Bot{
		P:        &Persona{Chars: []CharSpec{character}},
		BaseLv:   actor.BaseLevel,
		JobLv:    actor.JobLevel,
		Class:    actor.Class,
		StatPts:  actor.StatPointsLeft,
		SkillPts: actor.SkillPtsLeft,
		stats:    actor.Stats,
		skills:   skillState,
	}
}

func simProgressExpectedStats(plan *Build) [6]int {
	stats := [6]int{1, 1, 1, 1, 1, 1}
	for _, goal := range plan.Stats {
		stats[goal.Stat] = max(stats[goal.Stat], goal.Value)
	}
	return stats
}

func simProgressExpectedSkills(plan *Build) map[int]int {
	out := map[int]int{}
	for _, goal := range append(append([]SkillGoal(nil), noviceSkills...), plan.Skills...) {
		out[goal.ID] = max(out[goal.ID], goal.Lv)
	}
	return out
}

func simProgressNextStatGoal(plan *Build, stats [6]int) int {
	for _, goal := range plan.Stats {
		if stats[goal.Stat] < min(goal.Value, 99) {
			return goal.Stat
		}
	}
	return -1
}

func simProgressNextSkillGoal(plan *Build, skills map[int]int) int {
	goals := append(append([]SkillGoal(nil), noviceSkills...), plan.Skills...)
	for _, goal := range goals {
		if skills[goal.ID] < goal.Lv {
			return goal.ID
		}
	}
	return -1
}

func simProgressAssertCheckpointSkills(t *testing.T, cohort *simProgressCohort) {
	t.Helper()
	for _, actor := range cohort.Actors {
		plan := builds[actor.BuildID]
		if actor.SkillSpent != simProgressNoviceSkillPoints() {
			t.Fatalf("seed %d actor %d (%s): checkpoint skill points spent %d, want synthetic novice allocation %d",
				cohort.Seed, actor.ID, actor.BuildID, actor.SkillSpent, simProgressNoviceSkillPoints())
		}
		hasLearnedSkill := false
		for _, level := range actor.Skills {
			hasLearnedSkill = hasLearnedSkill || level > 0
		}
		if !hasLearnedSkill {
			t.Fatalf("seed %d actor %d (%s): checkpoint restored no learned skill state", cohort.Seed, actor.ID, actor.BuildID)
		}
		if actor.SkillSpent >= simProgressSkillPoints(plan) {
			t.Fatalf("seed %d actor %d (%s): checkpoint has no remaining planned skill progress", cohort.Seed, actor.ID, actor.BuildID)
		}
	}
}

func simProgressSkillPoints(plan *Build) int {
	total := 0
	for _, level := range simProgressExpectedSkills(plan) {
		total += level
	}
	return total
}

func simProgressNoviceSkillPoints() int {
	total := 0
	for _, goal := range noviceSkills {
		total += goal.Lv
	}
	return total
}

// Level and point totals are synthetic observations; pointTotal is read from the pinned statpoint fixture, not XP simulation.
func simProgressApplyStatWindow(t *testing.T, actor *simProgressActor, level, pointTotal int) (int, bool) {
	t.Helper()
	actor.BaseLevel = level
	actor.StatPointsLeft = pointTotal - actor.StatSpent
	if actor.StatPointsLeft < 0 {
		return 0, false
	}
	actions := 0
	for {
		bot := simProgressBot(actor)
		decision := bot.planStat()
		if want := simProgressNextStatGoal(builds[actor.BuildID], actor.Stats); decision != want {
			t.Fatalf("seed actor %d (%s): ordered stat decision %d with state %v, want first unmet planned stat %d",
				actor.ID, actor.BuildID, decision, actor.Stats, want)
		}
		if decision < 0 {
			break
		}
		if decision >= len(actor.Stats) {
			return actions, false
		}
		current := actor.Stats[decision]
		cost := statCostTo(current+1) - statCostTo(current)
		if cost > actor.StatPointsLeft {
			break
		}
		actor.Stats[decision]++
		actor.StatSpent += cost
		actor.StatPointsLeft -= cost
		actions++
	}
	actor.StatActions += actions
	return actions, true
}

func simProgressApplySkills(t *testing.T, actor *simProgressActor, observation simProgressObservation) int {
	t.Helper()
	actor.Class, actor.JobLevel = observation.Class, observation.JobLevel
	actor.SkillPtsLeft = observation.SkillPts - actor.SkillSpent
	actions := 0
	for actor.SkillPtsLeft > 0 {
		bot := simProgressBot(actor)
		decision := bot.planSkill()
		if want := simProgressNextSkillGoal(builds[actor.BuildID], actor.Skills); decision != want {
			t.Fatalf("seed actor %d (%s): ordered skill decision %d with state %v, want first unmet planned skill %d",
				actor.ID, actor.BuildID, decision, actor.Skills, want)
		}
		if decision < 0 {
			break
		}
		skill := bot.skills[decision]
		if skill == nil || !skill.Upgradable {
			break
		}
		skill.Lv++
		actor.Skills[decision] = skill.Lv
		actor.SkillSpent++
		actor.SkillPtsLeft--
		actions++
	}
	actor.SkillActions += actions
	return actions
}

func simProgressClassWindowList(classes map[int]*simProgressClassWindow) []simProgressClassWindow {
	ids := make([]int, 0, len(classes))
	for id := range classes {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	out := make([]simProgressClassWindow, 0, len(ids))
	for _, id := range ids {
		out = append(out, *classes[id])
	}
	return out
}

func simProgressRun(t *testing.T, cohort *simProgressCohort, statPoints map[int]int, stopAt int) {
	t.Helper()
	for i := cohort.NextWindow; i < len(simProgressLevels); i++ {
		level := simProgressLevels[i]
		if level > stopAt {
			return
		}
		window := simProgressWindow{BaseLevel: level, Actors: len(cohort.Actors)}
		classes := map[int]*simProgressClassWindow{}
		for i := range cohort.Actors {
			actor := &cohort.Actors[i]
			classWindow := classes[actor.Class]
			if classWindow == nil {
				classWindow = &simProgressClassWindow{Class: actor.Class}
				classes[actor.Class] = classWindow
			}
			classWindow.Actors++
			before := actor.StatActions
			actions, ok := simProgressApplyStatWindow(t, actor, level, statPoints[level])
			if !ok {
				t.Fatalf("seed %d actor %d (%s): stat point total %d below spent %d at level %d", cohort.Seed, actor.ID, actor.BuildID, statPoints[level], actor.StatSpent, level)
			}
			window.StatActions += actions
			if actions > 0 {
				window.StatActive++
				classWindow.StatActive++
			} else if actor.StatActions == before && actor.Stats != simProgressExpectedStats(builds[actor.BuildID]) {
				window.StatStalled++
				classWindow.StatStalled++
			}
			classWindow.StatActions += actions
			if level == 40 {
				window.JobLevel = 10 // synthetic job-level and skill-point observation, not an XP/job-level formula
				before := actor.SkillActions
				skillActions := simProgressApplySkills(t, actor, simProgressObservation{
					BaseLevel: level,
					JobLevel:  window.JobLevel,
					Class:     builds[actor.BuildID].Class,
					SkillPts:  simProgressNoviceSkillPoints(),
				})
				window.SkillActions += skillActions
				if skillActions > 0 {
					window.SkillActive++
					classWindow.SkillActive++
				} else if actor.SkillActions == before && actor.SkillSpent < simProgressSkillPoints(builds[actor.BuildID]) {
					window.SkillStalled++
					classWindow.SkillStalled++
				}
				classWindow.SkillActions += skillActions
			}
		}
		window.Classes = simProgressClassWindowList(classes)
		cohort.Windows = append(cohort.Windows, window)
		cohort.NextWindow = i + 1
	}
	if stopAt >= 99 {
		window := simProgressWindow{BaseLevel: 99, JobLevel: 50, Actors: len(cohort.Actors)}
		classes := map[int]*simProgressClassWindow{}
		for i := range cohort.Actors {
			actor := &cohort.Actors[i]
			classWindow := classes[actor.Class]
			if classWindow == nil {
				classWindow = &simProgressClassWindow{Class: actor.Class}
				classes[actor.Class] = classWindow
			}
			classWindow.Actors++
			plan := builds[actor.BuildID]
			before := actor.SkillActions
			actions := simProgressApplySkills(t, actor, simProgressObservation{
				BaseLevel: 99,
				JobLevel:  50, // synthetic final-class observation; skill points below are test input, not a level-up formula
				Class:     plan.Class,
				SkillPts:  budgetSkillPoints + simProgressNoviceSkillPoints(),
			})
			window.SkillActions += actions
			if actions > 0 {
				window.SkillActive++
				classWindow.SkillActive++
			} else if actor.SkillActions == before && actor.SkillSpent < simProgressSkillPoints(plan) {
				window.SkillStalled++
				classWindow.SkillStalled++
			}
			classWindow.SkillActions += actions
		}
		window.Classes = simProgressClassWindowList(classes)
		cohort.Windows = append(cohort.Windows, window)
	}
}

func simProgressAssertComplete(t *testing.T, cohort *simProgressCohort) map[string]int {
	t.Helper()
	buildCounts := map[string]int{}
	for _, actor := range cohort.Actors {
		plan := builds[actor.BuildID]
		buildCounts[actor.BuildID]++
		wantStats := simProgressExpectedStats(plan)
		if actor.Stats != wantStats {
			t.Fatalf("seed %d actor %d (%s): final stats %v, want plan targets %v", cohort.Seed, actor.ID, actor.BuildID, actor.Stats, wantStats)
		}
		for id, want := range simProgressExpectedSkills(plan) {
			if got := actor.Skills[id]; got != want {
				t.Fatalf("seed %d actor %d (%s): skill %d level %d, want plan level %d", cohort.Seed, actor.ID, actor.BuildID, id, got, want)
			}
		}
		if actor.StatSpent > budgetStatPoints || actor.SkillSpent > budgetSkillPoints+simProgressNoviceSkillPoints() {
			t.Fatalf("seed %d actor %d (%s): spent stats/skills %d/%d exceeds budgets %d/%d",
				cohort.Seed, actor.ID, actor.BuildID, actor.StatSpent, actor.SkillSpent, budgetStatPoints, budgetSkillPoints+simProgressNoviceSkillPoints())
		}
	}
	for _, id := range simProgressBuildIDs() {
		if buildCounts[id] == 0 {
			t.Errorf("seed %d has no cohort actor for production build %s", cohort.Seed, id)
		}
	}
	return buildCounts
}

// This replay applies synthetic acknowledgments to production plan decisions, not combat, XP, packets, or server acceptance.
func TestStatSkillProgressCohort(t *testing.T) {
	_, statFixture := simProgressFixture(t, "statpoint.yml")
	_, skillDBFixture := simProgressFixture(t, "skill_db.yml")
	_, skillTreeFixture := simProgressFixture(t, "skill_tree.yml")
	statPointTable := simProgressParseStatPoints(t, statFixture)
	skillDB := simProgressParseSkillDB(t, skillDBFixture)
	tree := simProgressParseSkillTree(t, skillTreeFixture)
	for level, points := range statPointTable {
		if level > 99 {
			continue
		}
		if got := statPoints(level); got != points {
			t.Fatalf("production statPoints(%d) = %d, frozen statpoint fixture has %d", level, got, points)
		}
	}
	for _, level := range simProgressLevels {
		if _, ok := statPointTable[level]; !ok {
			t.Fatalf("frozen statpoint fixture has no level %d observation", level)
		}
	}

	train := simProgressNewCohort(t, simProgressTrainSeed, tree, skillDB)
	simProgressRun(t, &train, statPointTable, 99)
	counts := simProgressAssertComplete(t, &train)

	interrupted := simProgressNewCohort(t, simProgressTrainSeed, tree, skillDB)
	simProgressRun(t, &interrupted, statPointTable, 40)
	checkpoint, err := json.Marshal(interrupted)
	if err != nil {
		t.Fatalf("serialize active checkpoint: %v", err)
	}
	var resumed simProgressCohort
	if err := json.Unmarshal(checkpoint, &resumed); err != nil {
		t.Fatalf("restore active checkpoint: %v", err)
	}
	simProgressAssertCheckpointSkills(t, &resumed)
	skillActionsAtCheckpoint := 0
	for _, actor := range resumed.Actors {
		skillActionsAtCheckpoint += actor.SkillActions
	}
	simProgressRun(t, &resumed, statPointTable, 99)
	skillActionsAfterResume := 0
	for _, actor := range resumed.Actors {
		skillActionsAfterResume += actor.SkillActions
	}
	if skillActionsAfterResume <= skillActionsAtCheckpoint {
		t.Fatalf("resumed cohort made no skill progress: actions before=%d after=%d", skillActionsAtCheckpoint, skillActionsAfterResume)
	}
	if !reflect.DeepEqual(train, resumed) {
		t.Fatal("active checkpoint/resume differs from uninterrupted cohort")
	}

	heldout := simProgressNewCohort(t, simProgressHoldout, tree, skillDB)
	simProgressRun(t, &heldout, statPointTable, 99)
	simProgressAssertComplete(t, &heldout)

	classCounts := map[int]int{}
	for _, actor := range train.Actors {
		classCounts[actor.Class]++
	}
	classIDs := make([]int, 0, len(classCounts))
	for class := range classCounts {
		classIDs = append(classIDs, class)
	}
	sort.Ints(classIDs)
	classNames := make([]string, 0, len(classIDs))
	for _, class := range classIDs {
		classNames = append(classNames, fmt.Sprintf("%d:%s", class, className(class)))
	}
	t.Logf("scope=1000-subject stat/skill plan cohort seed=%d heldout=%d builds=%d build-ids=%v classes=%v excluded=none",
		train.Seed, heldout.Seed, len(counts), simProgressBuildIDs(), classNames)
	for _, window := range train.Windows {
		t.Logf("base-level-window=%d job-level=%d actors=%d stat-active=%d stat-actions=%d stat-stalled=%d skill-active=%d skill-actions=%d skill-stalled=%d class-metrics=%v",
			window.BaseLevel, window.JobLevel, window.Actors, window.StatActive, window.StatActions, window.StatStalled, window.SkillActive, window.SkillActions, window.SkillStalled, window.Classes)
	}
	if len(counts) != len(builds) {
		t.Fatalf("coverage includes %d builds, production registry has %d", len(counts), len(builds))
	}
}
