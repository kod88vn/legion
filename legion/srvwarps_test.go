package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for p, s := range files {
		fp := filepath.Join(root, p)
		if err := os.MkdirAll(filepath.Dir(fp), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fp, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestServerWarpsLoadedFilesOnly(t *testing.T) {
	root := writeTree(t, map[string]string{
		"npc/re/scripts_main.conf": "npc: npc/re/warps/a.txt\nimport: npc/re/scripts_warps.conf\n// npc: npc/re/warps/off.txt\n" +
			"import: npc/scripts_custom.conf\n",
		"npc/re/scripts_warps.conf": "npc: npc/re/quests/q.txt\nnpc: npc/re/jobs/gone.txt\n",
		"npc/scripts_custom.conf":   "delnpc: npc/re/jobs/gone.txt\n",
		"npc/re/warps/a.txt": "gef_fild10,27,219,0\twarp\tg19\t1,1,gef_fild14,367,219\n" + // loaded
			"//gef_fild12,221,374,0\twarp\tg14\t1,1,gef_fild08,215,25\n" + // line comment
			"/*\naldebaran,35,140,0\twarp\talde-a\t1,1,te_alde_gld,280,160\n*/\n" + // block comment
			"aldebaran,35,140,0\twarp\talde_gate\t2,2,alde_gld,280,160\n" +
			"// a note with /* in it must not open a block\n" +
			"prontera,10,10,0\twarp\tsrc\t1,1,prt_in,5,5\n" +
			"prontera,20,10,0\tduplicate(src)\tsrc2\t3,4\n" + // duplicate of a warp, own span
			"prt_in,1,1,0\tscript\t#touch\tWARPNPC,2,2,{\nOnTouch:\n\twarp \"prontera\",30,30;\n\tend;\n}\n" +
			"prt_in,9,9,0\tscript\t#quest\tWARPNPC,1,1,{\nOnTouch:\n\tif (q) warp \"prontera\",1,1;\n\tend;\n}\n",
		"npc/re/warps/off.txt":   "izlude,30,78,0\twarp\told\t1,1,prt_fild08,1,1\n", // not loaded
		"npc/re/quests/q.txt":    "louyang,129,121,0\twarp\tStorage Warp#1\t1,1,lou_in02,203,161\n",
		"npc/re/jobs/gone.txt":   "job3_arch02,252,267,0\twarp\tx\t1,1,job3_arch01,1,1\n",
		"npc/custom-local/l.txt": "prontera,50,50,0\twarp\tcust\t1,1,geffen,1,1\n",
	})
	files := serverNPCFiles(root)
	got := map[string]Warp{}
	for _, w := range parseServerWarps(root, files) {
		got[warpKey(w.From, w.X, w.Y)] = w
	}
	want := map[string]string{
		"gef_fild10:27:219": "gef_fild14",
		"aldebaran:35:140":  "alde_gld",
		"prontera:10:10":    "prt_in",
		"prontera:20:10":    "prt_in",
		"prt_in:1:1":        "prontera",
		"louyang:129:121":   "lou_in02",
		"prontera:50:50":    "geffen",
	}
	for k, to := range want {
		if got[k].To != to {
			t.Errorf("%s: want -> %s, got %+v", k, to, got[k])
		}
	}
	for _, k := range []string{"gef_fild12:221:374", "izlude:30:78", "job3_arch02:252:267", "prt_in:9:9"} {
		if _, ok := got[k]; ok {
			t.Errorf("%s must not be a warp (comment / not loaded / delnpc / conditional)", k)
		}
	}
	if len(got) != len(want) {
		t.Errorf("got %d warps, want %d: %v", len(got), len(want), got)
	}
	if w := got["prontera:20:10"]; w.XS != 3 || w.YS != 4 || w.ToX != 5 {
		t.Errorf("duplicate span/landing wrong: %+v", w)
	}
	if w := got["aldebaran:35:140"]; w.XS != 2 {
		t.Errorf("span wrong: %+v", w)
	}
}

func TestStripBlockCommentsKeepsLines(t *testing.T) {
	in := "a\n/* x\ny */b\nmes \"/* not */\";\n// c /* d\ne"
	out := stripBlockComments(in)
	if strings.Count(out, "\n") != strings.Count(in, "\n") {
		t.Fatalf("line count changed: %q", out)
	}
	if strings.Contains(out, "x\n") || !strings.Contains(out, "b\n") || !strings.Contains(out, "\"/* not */\"") ||
		!strings.HasSuffix(out, "\ne") {
		t.Fatalf("bad strip: %q", out)
	}
}

// TestServerWarpsRealTree: with LEGION_RATHENA set to a rAthena tree (custom files overlaid like the containers), the
// loader must find the Renewal facts the OpenKore fix found (no Geffen 10<->14 link, Renewal Izlude, alde_gld gate).
func TestServerWarpsRealTree(t *testing.T) {
	root := os.Getenv("LEGION_RATHENA")
	if root == "" {
		t.Skip("LEGION_RATHENA not set")
	}
	files := serverNPCFiles(root)
	ws := parseServerWarps(root, files)
	got := map[string]Warp{}
	for _, w := range ws {
		got[warpKey(w.From, w.X, w.Y)] = w
	}
	t.Logf("%d warps from %d files", len(ws), len(files))
	for _, k := range []string{"gef_fild10:27:219", "gef_fild14:371:219", "gef_fild12:368:180", "izlude:30:78"} {
		if _, ok := got[k]; ok {
			t.Errorf("%s is not a warp on this server", k)
		}
	}
	if got["aldebaran:35:140"].To != "alde_gld" || got["izlude:20:98"].To != "prt_fild08" || got["louyang:129:121"].To != "lou_in02" {
		t.Errorf("missing server warps: %+v %+v %+v", got["aldebaran:35:140"], got["izlude:20:98"], got["louyang:129:121"])
	}
	if out := os.Getenv("LEGION_WARPS_OUT"); out != "" {
		var b strings.Builder
		for _, w := range ws {
			b.WriteString(w.From + " " + itoa(w.X) + " " + itoa(w.Y) + " " + w.To + " " + itoa(w.ToX) + " " + itoa(w.ToY) + "\n")
		}
		os.WriteFile(out, []byte(b.String()), 0o644)
	}
	if raw, err := os.ReadFile(filepath.Join(root, "npc", "kafras", "functions_kafras.txt")); err == nil {
		if d := kafraTableDiff(kafraDests, parseKafraTable(string(raw))); len(d) > 0 {
			t.Logf("kafra table differs for %v (job3_rune01 has no Kafra spot and is dropped at load)", d)
		}
	}
}
