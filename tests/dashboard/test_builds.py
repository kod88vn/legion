"""Tests for the Builds tab (DB09): the export of legion/builds.go, the live counts, the route and the tab itself.
No docker, no engine, no live data: the Go source and gear table in the repo, a temp snapshot and personas file.
Run: python -m unittest discover -s tests/dashboard -v   (from the repo root)"""
import gzip
import json
import os
import re
import sys
import tempfile
import threading
import unittest
import urllib.request
from http.server import ThreadingHTTPServer

HERE = os.path.dirname(os.path.abspath(__file__))
REPO = os.path.abspath(os.path.join(HERE, "..", ".."))
sys.path.insert(0, os.path.join(REPO, "tools", "dashboard"))
os.environ.setdefault("DASH_AYLA_LOGS", tempfile.mkdtemp())
os.environ.setdefault("DASH_SNAP", tempfile.mkdtemp())

import buildexport  # noqa: E402

BUILDS_GO = os.path.join(REPO, "legion", "builds.go")
EXPORT = os.path.join(REPO, "tools", "dashboard", "builds.json")
DASH_HTML = os.path.join(REPO, "legion", "dashboard.html")


def go_build_ids():
    with open(BUILDS_GO, encoding="utf-8") as f:
        return re.findall(r'^\t"(\w+)": \{ID:', f.read(), re.M)


def load_export():
    with open(EXPORT, encoding="utf-8") as f:
        return json.load(f)


class ExportTests(unittest.TestCase):
    def test_every_build_in_builds_go_is_exported(self):
        ids = go_build_ids()
        self.assertGreater(len(ids), 30, "builds.go parse found too few builds")
        exported = {b["id"] for b in load_export()["builds"]}
        missing = [i for i in ids if i not in exported]
        self.assertEqual(missing, [], "builds in builds.go missing from builds.json: re-run tools/dashboard/buildexport.py")

    def test_export_is_fresh_enough_to_show_its_age(self):
        doc = load_export()
        self.assertIsInstance(doc["generated_epoch"], int)
        self.assertGreater(doc["generated_epoch"], 1_700_000_000)

    def test_end_goal_is_the_max_of_the_plan(self):
        # hunter (builds.go): DEX 99, AGI 75, VIT 30, LUK 40 are the last milestone of each stat
        hunter = next(b for b in load_export()["builds"] if b["id"] == "hunter")
        got = {s["stat"]: s["value"] for s in hunter["end"]["stats"]}
        self.assertEqual(got, {"DEX": 99, "AGI": 75, "VIT": 30, "LUK": 40})
        self.assertEqual(hunter["class"]["name"], "Hunter")
        self.assertEqual(hunter["class"]["first"], "Archer")

    def test_every_build_leads_with_a_2nd_job_end_goal(self):
        for b in load_export()["builds"]:
            self.assertTrue(b["class"]["name"], b["id"])
            self.assertTrue(b["end"]["skills"], b["id"])
            self.assertTrue(b["steps"]["stats"], b["id"])
            self.assertTrue(b["steps"]["skills"], b["id"])

    def test_pvp_is_a_gap_everywhere(self):
        for b in load_export()["builds"]:
            self.assertEqual(b["playstyle"]["pvp"]["status"], "gap", b["id"])
            self.assertEqual(b["playstyle"]["pvp"]["lines"], [], b["id"])

    def test_gap_list_matches_the_parts_marked_gap(self):
        doc = load_export()
        marked = [(b["id"], p) for b in doc["builds"] for p, v in b["playstyle"].items() if v["status"] == "gap"]
        self.assertEqual(sorted(marked), sorted((g["build"], g["part"]) for g in doc["gaps"]))

    def test_milestones_follow_the_gear_bands_and_gap_stats_and_skills(self):
        doc = load_export()
        with open(os.path.join(REPO, "legion", "data", "buildgear.json"), encoding="utf-8") as f:
            gear = json.load(f)["builds"]
        for b in doc["builds"]:
            table_id = b["id"] if b["id"] in gear else b["base"]
            if table_id in gear:
                self.assertEqual(len(b["milestones"]), len(gear[table_id]["bands"]), b["id"])
            for m in b["milestones"]:
                self.assertEqual(m["stats"]["status"], "gap")
                self.assertEqual(m["skills"]["status"], "gap")
                self.assertEqual(m["range"], "%s-%s" % (m["min"], m["max"]))

    def test_the_export_is_the_same_as_a_fresh_run(self):
        # the committed file must match the Go source it was made from
        fresh = buildexport.build_export(buildexport.load_items(buildexport.ITEMS_TSV),
                                         buildexport.load_skills(buildexport.SKILLS_TSV))
        old = load_export()
        self.assertEqual([b["id"] for b in fresh["builds"]], [b["id"] for b in old["builds"]])
        self.assertEqual(fresh["gaps"], old["gaps"])


class LiveCountTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.mkdtemp()
        self.snap = os.path.join(self.tmp, "snap-20261115-000000.json.gz")
        chars = {"cols": ["char_id", "online", "str", "agi", "vit", "int", "dex", "luk"],
                 "rows": [[1, 1, 30, 10, 5, 1, 50, 1], [2, 0, 99, 99, 99, 99, 99, 99]]}
        skill = {"cols": ["char_id", "id", "lv"], "rows": [[1, 46, 10]]}
        inv = {"cols": ["char_id", "nameid", "equip"], "rows": [[1, 1701, 2]]}
        with gzip.open(self.snap, "wt", encoding="utf-8") as f:
            json.dump({"t": 1794777582.0, "tables": {"char": chars, "skill": skill, "inventory": inv}}, f)
        self.personas = os.path.join(self.tmp, "personas.json")
        with open(self.personas, "w", encoding="utf-8") as f:
            json.dump([{"characters": [{"char_id": 1, "plan": "hunter"}, {"char_id": 2, "plan": "hunter"},
                                       {"char_id": 3, "plan": "hunter"}]}], f)
        self.skills = os.path.join(self.tmp, "skills.tsv")
        with open(self.skills, "w", encoding="utf-8") as f:
            f.write("46\tHT_DOUBLE\tDouble Strafe\n43\tAC_OWL\tOwl's Eye\n")
        self.out = os.path.join(self.tmp, "builds.json")
        buildexport.export(self.out, items_path=os.path.join(self.tmp, "none.tsv"), skills_path=self.skills)

    def test_counts_bots_online_and_the_current_step(self):
        doc = buildexport.view(out=self.out, snap_dir=self.tmp, personas=self.personas)
        live = doc["live"]
        self.assertEqual(live["snapshot"], "snap-20261115-000000.json.gz")
        c = live["per_build"]["hunter"]
        self.assertEqual(c["bots"], 2)          # char 3 is not in the snapshot, so it is not counted
        self.assertEqual(c["online"], 1)
        steps = sum(s["bots"] for s in c["stat_steps"]) + c["stat_done"]
        self.assertEqual(steps, 2)
        # char 1 has DEX 50, AGI 10: DEX 20, AGI 10, DEX 40 are reached; the first unmet milestone is AGI 20 (index 3)
        self.assertEqual(c["stat_steps"][3]["label"], "AGI 20")
        self.assertEqual(c["stat_steps"][3]["bots"], 1)
        self.assertEqual(c["stat_steps"][0]["bots"], 0)
        # skill: Double Strafe 10 (step 0) is learnt; the next unmet skill in the plan's order is Owl's Eye 3 (step 1)
        self.assertEqual(c["skill_steps"][0]["name"], "Double Strafe")
        # char 2 has no skills yet, so it is on the first step; char 1 is on the second
        self.assertEqual(c["skill_steps"][0]["bots"], 1)
        self.assertEqual(c["skill_steps"][1]["bots"], 1)
        self.assertEqual(c["stat_done"], 1)     # char 2 has every stat at 99
        # gear: char 1 has the Hunter Bow (1701) equipped, the first weapon of the route
        self.assertEqual(sum(c["gear_steps"].values()), 2)

    def test_no_snapshot_is_reported_not_a_crash(self):
        doc = buildexport.view(out=self.out, snap_dir=os.path.join(self.tmp, "none"), personas=self.personas)
        self.assertIsNone(doc["live"]["snapshot"])
        self.assertIn("no DB snapshot", doc["live"]["error"])


class RouteAndTabTests(unittest.TestCase):
    def test_api_builds_route_serves_the_export(self):
        import dashserve
        srv = ThreadingHTTPServer(("127.0.0.1", 0), dashserve.Handler)
        srv.engine = dashserve.Engine("http://127.0.0.1:9")   # the engine is down: the route must not need it
        srv.verbose = False   # the attribute dashserve's log_message reads (set by main() in production)
        threading.Thread(target=srv.serve_forever, daemon=True).start()
        try:
            with urllib.request.urlopen("http://127.0.0.1:%d/api/builds" % srv.server_address[1], timeout=20) as r:
                self.assertEqual(r.status, 200)
                doc = json.loads(r.read())
            self.assertEqual(len(doc["builds"]), len(go_build_ids()))
            self.assertIn("live", doc)
        finally:
            srv.shutdown()
            srv.server_close()

    def test_dashboard_has_the_builds_tab_after_the_others_and_keeps_them(self):
        with open(DASH_HTML, encoding="utf-8") as f:
            html = f.read()
        self.assertIn('data-tab="builds"', html)
        self.assertIn('id="tab-builds"', html)
        for t in ["ayla", "legion", "economy", "puppet", "leaderboard", "players", "system"]:   # G08: nothing removed
            self.assertIn("data-tab=\"%s\"" % t, html)
        self.assertIn("'system', 'builds'", html)


if __name__ == "__main__":
    unittest.main()
