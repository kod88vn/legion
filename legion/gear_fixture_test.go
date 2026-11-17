package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

const gearFixtureReplayChild = "LEGION_GEAR_FIXTURE_REPLAY_CHILD"

type gearFixtureManifest struct {
	SourceCommit string `json:"source_commit"`
	FixtureKind  string `json:"fixture_kind"`
	RuntimeMatch string `json:"runtime_image_match"`
	Demand       struct {
		BuildRecords int   `json:"build_records"`
		WeaponIDs    int   `json:"weapon_ids"`
		ArmorIDs     int   `json:"armor_ids"`
		ItemIDs      []int `json:"item_ids"`
	} `json:"demand"`
	BuildCoverage struct {
		RegisteredBuildVariants    int               `json:"registered_build_variants"`
		RegisteredSecondJobClasses int               `json:"registered_second_job_classes"`
		GuideBuildRecords          int               `json:"guide_build_records"`
		DirectGuideBuildIDs        []string          `json:"direct_guide_build_ids"`
		BaseGuideAliases           map[string]string `json:"base_guide_aliases"`
		UncoveredBuildIDs          []string          `json:"uncovered_build_ids"`
	} `json:"build_coverage"`
	Availability gearFixtureAvailability    `json:"availability"`
	OutputFiles  map[string]gearFixtureFile `json:"output_files"`
}

type gearFixtureAvailability struct {
	ShopItemIDs        []int `json:"shop_item_ids"`
	SpawnedDropItemIDs []int `json:"spawned_drop_item_ids"`
	NormalFarmItemIDs  []int `json:"normal_farm_item_ids"`
	BothItemIDs        []int `json:"both_item_ids"`
	NeitherItemIDs     []int `json:"neither_item_ids"`
	Counts             struct {
		Shop        int `json:"shop"`
		SpawnedDrop int `json:"spawned_drop"`
		NormalFarm  int `json:"normal_farm"`
		Both        int `json:"both"`
		Neither     int `json:"neither"`
	} `json:"counts"`
}

type gearFixtureFile struct {
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}

func TestPinnedGearFixtureProductionReplay(t *testing.T) {
	root := filepath.Join("testdata", "gear-fixture")
	if os.Getenv(gearFixtureReplayChild) == "1" {
		replayPinnedGearFixture(t, root)
		return
	}

	manifest, err := verifyPinnedGearFixture(root)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.SourceCommit != "1ca3e7700939ea40120cb51a777f8681ba5e84ae" ||
		manifest.FixtureKind != "pinned-source-only" || manifest.RuntimeMatch != "unknown" {
		t.Fatalf("fixture provenance must remain pinned-source-only with runtime match unknown: %+v", manifest)
	}
	if manifest.Demand.BuildRecords != 29 || manifest.Demand.WeaponIDs != 116 ||
		manifest.Demand.ArmorIDs != 21 || len(manifest.Demand.ItemIDs) != 137 {
		t.Fatalf("unexpected gear demand closure: %+v", manifest.Demand)
	}
	if manifest.BuildCoverage.RegisteredBuildVariants != 37 || manifest.BuildCoverage.RegisteredSecondJobClasses != 13 ||
		manifest.BuildCoverage.GuideBuildRecords != 29 ||
		len(manifest.BuildCoverage.DirectGuideBuildIDs) != 29 || len(manifest.BuildCoverage.BaseGuideAliases) != 8 ||
		len(manifest.BuildCoverage.UncoveredBuildIDs) != 0 {
		t.Fatalf("unexpected registered-build/guide coverage: %+v", manifest.BuildCoverage)
	}
	wantAliases := map[string]string{
		"wizard_fire": "wizard", "wizard_frost": "wizard", "hunter_agi": "hunter", "bard_arrow": "bard",
		"dancer_arrow": "dancer", "rogue_strip": "rogue", "hunter_trap": "hunter", "monk_occult": "monk",
	}
	if !reflect.DeepEqual(manifest.BuildCoverage.BaseGuideAliases, wantAliases) {
		t.Fatalf("base guide aliases differ: got %v, want %v", manifest.BuildCoverage.BaseGuideAliases, wantAliases)
	}
	if manifest.Availability.Counts.Shop != 67 || manifest.Availability.Counts.SpawnedDrop != 78 ||
		manifest.Availability.Counts.NormalFarm != 72 || manifest.Availability.Counts.Both != 18 ||
		manifest.Availability.Counts.Neither != 10 {
		t.Fatalf("unexpected shop/drop source coverage: %+v", manifest.Availability.Counts)
	}

	requiredFiles := []string{
		"rathena/LICENSE",
		"rathena/db/re/item_db_equip.yml",
		"rathena/db/re/mob_db.yml",
		"rathena/db/import/item_db.yml",
		"rathena/conf/import/battle_conf.txt",
		"rathena/npc/custom-local/legion_shops.txt",
		"rathena/npc/re/merchants/Dealer_Update.txt",
	}
	for _, path := range requiredFiles {
		if _, ok := manifest.OutputFiles[path]; !ok {
			t.Fatalf("fixture manifest omits %s", path)
		}
	}
	required := requiredFiles[1]
	file := manifest.OutputFiles[required]
	if err := verifyGearFixtureFile(t.TempDir(), required, file); err == nil {
		t.Fatal("missing static item input was accepted")
	}
	corruptRoot := t.TempDir()
	corruptPath := filepath.Join(corruptRoot, filepath.FromSlash(required))
	if err := os.MkdirAll(filepath.Dir(corruptPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(corruptPath, []byte("corrupt fixture input"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := verifyGearFixtureFile(corruptRoot, required, file); err == nil {
		t.Fatal("corrupted static item input was accepted")
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestPinnedGearFixtureProductionReplay$")
	cmd.Env = append(os.Environ(), gearFixtureReplayChild+"=1")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("production parser replay failed: %v\n%s", err, output)
	}
}

func verifyPinnedGearFixture(root string) (gearFixtureManifest, error) {
	var manifest gearFixtureManifest
	raw, err := os.ReadFile(filepath.Join(root, "PROVENANCE.json"))
	if err != nil {
		return manifest, fmt.Errorf("read pinned gear fixture provenance: %w", err)
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return manifest, fmt.Errorf("parse pinned gear fixture provenance: %w", err)
	}
	if len(manifest.OutputFiles) == 0 {
		return manifest, fmt.Errorf("pinned gear fixture provenance has no output files")
	}
	for path, file := range manifest.OutputFiles {
		if err := verifyGearFixtureFile(root, path, file); err != nil {
			return manifest, err
		}
	}
	return manifest, nil
}

func verifyGearFixtureFile(root, path string, expected gearFixtureFile) error {
	clean := filepath.Clean(filepath.FromSlash(path))
	if filepath.IsAbs(clean) || clean == "." || clean == ".." ||
		strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return fmt.Errorf("invalid pinned gear fixture path %q", path)
	}
	data, err := os.ReadFile(filepath.Join(root, clean))
	if err != nil {
		return fmt.Errorf("read pinned gear fixture input %s: %w", path, err)
	}
	if int64(len(data)) != expected.Bytes {
		return fmt.Errorf("pinned gear fixture input %s has %d bytes, want %d", path, len(data), expected.Bytes)
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != expected.SHA256 {
		return fmt.Errorf("pinned gear fixture input %s has a SHA-256 mismatch", path)
	}
	return nil
}

func replayPinnedGearFixture(t *testing.T, root string) {
	t.Helper()
	manifest, err := verifyPinnedGearFixture(root)
	if err != nil {
		t.Fatal(err)
	}
	*dataDir = "data"
	rathenaRoot := filepath.Join(root, "rathena")
	*rathena = rathenaRoot
	loadItemDB(filepath.Join(rathenaRoot, "db", "re", "item_db_equip.yml"))
	loadItemOverrides(filepath.Join(rathenaRoot, "db", "import", "item_db.yml"))
	if err := loadMobDB(filepath.Join(rathenaRoot, "db", "re", "mob_db.yml")); err != nil {
		t.Fatal(err)
	}
	loadNPCs(
		filepath.Join(rathenaRoot, "npc", "re", "mobs"),
		filepath.Join(rathenaRoot, "npc", "re", "merchants"),
		filepath.Join(rathenaRoot, "npc", "merchants"),
		filepath.Join(rathenaRoot, "npc", "custom-local"),
	)
	applyBarterDealers(filepath.Join(rathenaRoot, "npc", "re", "merchants", "Dealer_Update.txt"))

	if len(items) != 137 {
		t.Fatalf("production item parser loaded %d gear items, want 137", len(items))
	}
	if got := items[1701]; got == nil || got.Type != "Weapon" || got.Atk != 15 || got.MinLv != 4 || got.Loc != 0x22 {
		t.Fatalf("production item parser decoded Bow 1701 as %+v", got)
	}
	if got := items[2306]; got == nil || got.Type != "Armor" || got.Def != 20 || got.Loc != 0x10 {
		t.Fatalf("production item parser decoded Adventurer's Suit 2306 as %+v", got)
	}
	if got := items[1102]; got == nil || got.Type != "Weapon" || got.Atk != 25 || got.Sub != "1hSword" {
		t.Fatalf("production item parser decoded Sword 1102 as %+v", got)
	}

	gearTables := buildGearTable()
	if got := sortedFixtureIDs(currentBuildGearDemand(t)); !reflect.DeepEqual(got, manifest.Demand.ItemIDs) {
		t.Fatalf("current registered routes and loaded guides differ from fixture demand: got %v, want %v",
			got, manifest.Demand.ItemIDs)
	}
	bands := gearTables["hunter"]
	if len(bands) != len(bandEdges) {
		t.Fatalf("hunter gear table has %d bands, want %d", len(bands), len(bandEdges))
	}
	foundGuideBow := false
	for _, entry := range bands[0].Slots["weapon"] {
		if entry.ID == 1702 && entry.Source == "guide" {
			foundGuideBow = true
			break
		}
	}
	if !foundGuideBow {
		t.Fatal("hunter guide gear table did not retain the pinned Bow_ 1702 entry")
	}
	milestones := milestonesFor(builds["hunter"], buildGear.file.Builds["hunter"])
	if len(milestones) != len(bandEdges) ||
		milestones[0].ArmorItem != 2306 || milestones[0].Armor != 20 ||
		milestones[1].WeaponItem != 1711 || milestones[1].Weapon != 65 {
		t.Fatalf("hunter milestones from parsed item data: %+v", milestones)
	}

	hasPronteraBowShop := false
	for _, shop := range gearShops(1701) {
		if shop.Map == "prontera" {
			hasPronteraBowShop = true
			break
		}
	}
	if !hasPronteraBowShop {
		t.Fatal("production NPC parser did not expose the Prontera Bow shop")
	}

	oldTune := tune()
	nt := *oldTune
	nt.DropRateMul = 0
	nt.DropRatesFromConf = true
	curTuning.Store(&nt)
	dropConf.once = syncOnceFresh()
	verifyFixtureAvailability(t, manifest)
	farms := pieceFarms(1702, 20, 3)
	foundCondorDrop := false
	for _, farm := range farms {
		if farm.Mob == "Condor" && farm.MobLv == 12 && farm.Rate == 5 {
			foundCondorDrop = true
			break
		}
	}
	if !foundCondorDrop {
		t.Fatalf("production mob/drop/spawn parsers did not resolve Bow_ 1702 at 5%% from Condor: %+v", farms)
	}
}

func verifyFixtureAvailability(t *testing.T, manifest gearFixtureManifest) {
	t.Helper()
	demand := make(map[int]bool, len(manifest.Demand.ItemIDs))
	for _, id := range manifest.Demand.ItemIDs {
		demand[id] = true
	}
	shops := map[int]bool{}
	for _, list := range shopsByMap {
		for _, shop := range list {
			for _, id := range shop.Items {
				if demand[id] {
					shops[id] = true
				}
			}
		}
	}
	drops := map[int]bool{}
	for item, mobIDs := range droppedBy {
		for _, mobID := range mobIDs {
			if len(mobMaps[mobID]) > 0 {
				drops[item] = true
				break
			}
		}
	}
	farms := map[int]bool{}
	for id := range demand {
		if len(pieceFarms(id, 1000, len(mobMaps)+1)) > 0 {
			farms[id] = true
		}
	}
	both, neither := map[int]bool{}, map[int]bool{}
	for id := range demand {
		switch {
		case shops[id] && drops[id]:
			both[id] = true
		case !shops[id] && !drops[id]:
			neither[id] = true
		}
	}
	if got := sortedFixtureIDs(shops); !reflect.DeepEqual(got, manifest.Availability.ShopItemIDs) {
		t.Fatalf("parsed shop coverage differs from provenance: got %v, want %v", got, manifest.Availability.ShopItemIDs)
	}
	if got := sortedFixtureIDs(drops); !reflect.DeepEqual(got, manifest.Availability.SpawnedDropItemIDs) {
		t.Fatalf("parsed spawned-drop coverage differs from provenance: got %v, want %v", got, manifest.Availability.SpawnedDropItemIDs)
	}
	if got := sortedFixtureIDs(farms); !reflect.DeepEqual(got, manifest.Availability.NormalFarmItemIDs) {
		t.Fatalf("production pieceFarms coverage differs from provenance: got %v, want %v", got, manifest.Availability.NormalFarmItemIDs)
	}
	if got := sortedFixtureIDs(both); !reflect.DeepEqual(got, manifest.Availability.BothItemIDs) {
		t.Fatalf("shop+drop coverage differs from provenance: got %v, want %v", got, manifest.Availability.BothItemIDs)
	}
	if got := sortedFixtureIDs(neither); !reflect.DeepEqual(got, manifest.Availability.NeitherItemIDs) {
		t.Fatalf("unsourced IDs differ from provenance: got %v, want %v", got, manifest.Availability.NeitherItemIDs)
	}
}

func sortedFixtureIDs(ids map[int]bool) []int {
	out := make([]int, 0, len(ids))
	for id := range ids {
		out = append(out, id)
	}
	sort.Ints(out)
	return out
}

func currentBuildGearDemand(t *testing.T) map[int]bool {
	t.Helper()
	demand := map[int]bool{}
	for _, build := range builds {
		for _, id := range build.Weapons {
			demand[id] = true
		}
		for _, id := range build.Armors {
			demand[id] = true
		}
	}

	buildGear.Lock()
	guideFile := buildGear.file
	buildGear.Unlock()
	if guideFile == nil {
		t.Fatal("production build gear guide file was not loaded")
	}
	for _, guide := range guideFile.Builds {
		for _, band := range guide.Bands {
			for _, id := range band.Weapon {
				demand[id] = true
			}
			for _, id := range band.Armor {
				demand[id] = true
			}
		}
	}
	return demand
}
