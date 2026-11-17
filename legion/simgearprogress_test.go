package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const (
	simGearReplayChild       = "LEGION_SIM_GEAR_REPLAY_CHILD"
	simGearNegativeControl   = "LEGION_SIM_GEAR_NEGATIVE_CONTROL"
	simGearFixtureRoot       = "testdata/gear-fixture"
	simGearPinnedGuideSource = "repo/legion/data/buildgear.json"
)

func TestSimGearDecisionReplay(t *testing.T) {
	if os.Getenv(simGearReplayChild) == "1" {
		runSimGearDecisionReplay(t)
		return
	}

	output, err := runSimGearDecisionProcess(t, false)
	if err != nil {
		t.Fatalf("selector replay failed: %v\n%s", err, output)
	}

	output, err = runSimGearDecisionProcess(t, true)
	if err == nil {
		t.Fatalf("negative control unexpectedly passed:\n%s", output)
	}
	if !strings.Contains(string(output), "class/sex selector negative control: got base \"bard\", want \"dancer\"") {
		t.Fatalf("negative control failed for an unexpected reason: %v\n%s", err, output)
	}
}

func runSimGearDecisionProcess(t *testing.T, negative bool) ([]byte, error) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestSimGearDecisionReplay$")
	env := make([]string, 0, len(os.Environ())+2)
	for _, entry := range os.Environ() {
		if strings.HasPrefix(entry, simGearReplayChild+"=") ||
			strings.HasPrefix(entry, simGearNegativeControl+"=") {
			continue
		}
		env = append(env, entry)
	}
	env = append(env, simGearReplayChild+"=1")
	if negative {
		env = append(env, simGearNegativeControl+"=1")
	}
	cmd.Env = env
	return cmd.CombinedOutput()
}

func runSimGearDecisionReplay(t *testing.T) {
	t.Helper()
	manifest := verifySimGearBuildGuidePin(t)
	replayPinnedGearFixture(t, simGearFixtureRoot)

	testSimGearClassAndBuildSelection(t)
	testSimGearSyntheticCohort(t, manifest)
	testSimGearMilestonesAndGate(t)
	testSimGearLevelAppropriateNextPiece(t)
	testSimGearUnsourcedAvailability(t, manifest)
}

func verifySimGearBuildGuidePin(t *testing.T) gearFixtureManifest {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join(simGearFixtureRoot, "PROVENANCE.json"))
	if err != nil {
		t.Fatal(err)
	}
	var pin struct {
		InputSHA256 map[string]string `json:"input_sha256"`
	}
	if err := json.Unmarshal(raw, &pin); err != nil {
		t.Fatal(err)
	}
	want := pin.InputSHA256[simGearPinnedGuideSource]
	if want == "" {
		t.Fatalf("fixture provenance does not pin %s", simGearPinnedGuideSource)
	}
	source, err := os.ReadFile(filepath.Join("data", "buildgear.json"))
	if err != nil {
		t.Fatalf("read versioned build guide: %v", err)
	}
	sum := sha256.Sum256(source)
	if got := hex.EncodeToString(sum[:]); got != want {
		t.Fatalf("versioned build guide SHA-256 = %s, want pinned %s", got, want)
	}

	manifest, err := verifyPinnedGearFixture(simGearFixtureRoot)
	if err != nil {
		t.Fatal(err)
	}
	return manifest
}

func testSimGearClassAndBuildSelection(t *testing.T) {
	t.Helper()

	preferMain := func(int) int { return 0 }
	knight := planForClass(7, "M", preferMain)
	if knight == nil || knight.ID != "knight" {
		t.Fatalf("class 7 main-build preference = %+v, want knight", knight)
	}
	preferVariant := func(n int) int {
		if n == 5 {
			return 4
		}
		return 0
	}
	knightVariant := planForClass(7, "M", preferVariant)
	if knightVariant == nil || knightVariant.Base != "knight" || knightVariant.ID == "knight" {
		t.Fatalf("class 7 variant preference = %+v, want a Knight variant", knightVariant)
	}

	pickLast := func(n int) int { return n - 1 }
	femaleArcher := planForClass(3, "F", pickLast)
	if femaleArcher == nil || femaleArcher.base() != "dancer" {
		t.Fatalf("female archer branch = %+v, want Dancer family", femaleArcher)
	}
	maleArcher := planForClass(3, "M", pickLast)
	if maleArcher == nil || maleArcher.base() != "bard" {
		t.Fatalf("male archer branch = %+v, want Bard family", maleArcher)
	}
	if got := (&Bot{P: &Persona{Chars: []CharSpec{{Plan: femaleArcher.ID}}}}).plan(); got != femaleArcher {
		t.Fatalf("plan lookup returned %+v for selected plan %q", got, femaleArcher.ID)
	}

	if os.Getenv(simGearNegativeControl) == "1" &&
		(maleArcher == nil || maleArcher.base() != "dancer") {
		base := "<nil>"
		if maleArcher != nil {
			base = maleArcher.base()
		}
		t.Fatalf("class/sex selector negative control: got base %q, want %q", base, "dancer")
	}
}

func testSimGearSyntheticCohort(t *testing.T, manifest gearFixtureManifest) {
	t.Helper()

	classIDs := manifest.BuildCoverage.SecondJobClassIDs
	if len(classIDs) == 0 {
		t.Fatal("pinned fixture has no second-job classes")
	}
	registered := make(map[string]bool, len(manifest.BuildCoverage.RegisteredBuildIDs))
	for _, id := range manifest.BuildCoverage.RegisteredBuildIDs {
		registered[id] = true
	}
	classRows := make(map[int]int, len(classIDs))
	buildRows := map[string]int{}
	preferMain := func(int) int { return 0 }

	for row := 0; row < 1000; row++ {
		class := classIDs[row%len(classIDs)]
		sex := "M"
		if class == 20 {
			sex = "F"
		}
		plan := planForClass(class, sex, preferMain)
		if plan == nil || plan.Class != class || !registered[plan.ID] {
			t.Fatalf("synthetic row %d selected an unregistered or wrong-class build: class=%d plan=%+v", row, class, plan)
		}
		bot := &Bot{P: &Persona{Chars: []CharSpec{{Plan: plan.ID}}}}
		if got := bot.plan(); got != plan {
			t.Fatalf("synthetic row %d plan lookup = %+v, want selected build %q", row, got, plan.ID)
		}
		if got := milestonesOf(plan); len(got) != len(bandEdges) {
			t.Fatalf("synthetic row %d build %q has %d milestone bands, want %d", row, plan.ID, len(got), len(bandEdges))
		}
		classRows[class]++
		buildRows[plan.ID]++
	}

	for _, class := range classIDs {
		if classRows[class] == 0 {
			t.Fatalf("synthetic 1000-row replay omitted class %d", class)
		}
	}
	if len(buildRows) != len(classIDs) {
		t.Fatalf("synthetic main-build coverage has %d builds for %d classes: %v", len(buildRows), len(classIDs), buildRows)
	}
}

func testSimGearMilestonesAndGate(t *testing.T) {
	t.Helper()

	hunter := builds["hunter"]
	milestones := milestonesOf(hunter)
	if len(milestones) < 2 ||
		milestones[0].ArmorItem != 2306 || milestones[0].Armor != 20 ||
		milestones[1].WeaponItem != 1711 || milestones[1].Weapon != 65 {
		t.Fatalf("pinned Hunter milestones = %+v, want first armor 2306/20 and next weapon 1711/65", milestones)
	}

	oldTuning := tune()
	nextTuning := *oldTuning
	nextTuning.GearGateOn = true
	curTuning.Store(&nextTuning)
	bot := &Bot{
		BaseLv: 41,
		equipped: map[uint32]*InvItem{
			0x2: {ItemID: 1701},
		},
	}
	gate, goal := gearGateOf(bot, hunter, 41)
	if gate != 26 || goal == nil || *goal != (gearGoal{Item: 2306, Slot: "armor"}) {
		t.Fatalf("undergeared Hunter gate = (%d, %+v), want (26, armor 2306)", gate, goal)
	}
}

func testSimGearLevelAppropriateNextPiece(t *testing.T) {
	t.Helper()

	band := gearBandFor("hunter", 20)
	if band == nil {
		t.Fatal("Hunter has no level-20 gear band")
	}
	farms := pieceFarms(1702, 20, 3)
	foundCondor := false
	for _, farm := range farms {
		if farm.Mob == "Condor" && farm.MobLv == 12 && farm.Rate == 5 {
			foundCondor = true
		}
	}
	if !foundCondor {
		t.Fatalf("level-20 source selection omitted the pinned Condor drop: %+v", farms)
	}
	if farms := pieceFarms(1702, 1, 3); len(farms) != 0 {
		t.Fatalf("level-1 source selection included out-of-band farms: %+v", farms)
	}
	dropGoal := gearGoal{Item: 1702, Slot: "weapon"}
	piece := gearGoalPiece(dropGoal, 20)
	if piece == nil || piece.Source != "drop" || len(piece.Farm) == 0 {
		t.Fatalf("level-20 goal source = %+v, want the eligible drop source", piece)
	}
	if piece := gearGoalPiece(dropGoal, 1); piece == nil || piece.Source != "trade" || len(piece.Farm) != 0 {
		t.Fatalf("level-1 goal source = %+v, want the production trade fallback without a farm", piece)
	}

	botAt20 := newSimGearHunter(20)
	isolateSimGearCandidate(botAt20, band, 1704)
	piece = botAt20.nextBuildPiece()
	if piece == nil || piece.ItemID != 1704 || piece.Source != "shop" {
		t.Fatalf("level-20 next piece = %+v, want Composite Bow 1704 from a shop", piece)
	}

	botAt1 := newSimGearHunter(1)
	isolateSimGearCandidate(botAt1, band, 1704)
	if piece := botAt1.nextBuildPiece(); piece != nil {
		t.Fatalf("level-1 next piece = %+v, want no wearable candidate", piece)
	}
}

func newSimGearHunter(level int) *Bot {
	return &Bot{
		P: &Persona{
			Class:     11,
			BaseLevel: level,
			Chars:     []CharSpec{{Class: 11, Plan: "hunter"}},
		},
		Class:   11,
		BaseLv:  level,
		inv:     map[int]*InvItem{},
		invSeen: true,
		equipped: map[uint32]*InvItem{
			0x2: {ItemID: 1701},
		},
		gearFailed: map[int]bool{},
	}
}

func isolateSimGearCandidate(bot *Bot, band *gearBand, target int) {
	for _, slot := range gearSlots {
		for _, entry := range band.Slots[slot.Name] {
			if entry.ID != target {
				bot.gearFailed[entry.ID] = true
			}
		}
	}
}

func testSimGearUnsourcedAvailability(t *testing.T, manifest gearFixtureManifest) {
	t.Helper()

	if len(manifest.Availability.NeitherItemIDs) == 0 {
		t.Fatal("pinned fixture has no unsourced gear controls")
	}
	for _, id := range manifest.Availability.NeitherItemIDs {
		item := items[id]
		if item == nil {
			t.Fatalf("unsourced fixture item %d is not loaded", id)
		}
		slot := "armor"
		if item.Type == "Weapon" {
			slot = "weapon"
		}
		piece := gearGoalPiece(gearGoal{Item: id, Slot: slot}, 99)
		if piece == nil || piece.Source != "trade" || piece.NPC != 0 || len(piece.Farm) != 0 {
			t.Fatalf("fixture-unsourced item %d was classified as %+v, want trade fallback only", id, piece)
		}
	}
}
