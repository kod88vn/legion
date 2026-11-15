"""legion/data/buildmilestones.json: every build in builds.go has an entry, each range's stats fit the stat points of its
base level, and the checkpoints follow the plans. Offline, reads only repo files.
Run: python -m unittest discover -s tests/tools -p "test_buildmilestones.py" -v"""
import json
import os
import sys
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.normpath(os.path.join(HERE, "..", ".."))
sys.path.insert(0, os.path.join(ROOT, "tools", "design"))
import planlib  # noqa: E402

DOC = json.load(open(os.path.join(ROOT, "legion", "data", "buildmilestones.json"), encoding="utf-8"))
GEAR = json.load(open(os.path.join(ROOT, "legion", "data", "buildgear.json"), encoding="utf-8"))["builds"]
PLANS = planlib.parse_builds()


class BuildMilestonesTest(unittest.TestCase):
    def test_every_build_has_an_entry(self):
        ids = planlib.build_ids()
        self.assertGreaterEqual(len(ids), 37)
        self.assertEqual(sorted(ids), sorted(PLANS), "the plan parser must see every build in builds.go")
        self.assertEqual(sorted(ids), sorted(DOC["builds"]), "buildmilestones.json must have exactly the builds of builds.go")

    def test_marked_as_draft(self):
        self.assertEqual(DOC["status"], "draft for owner review")
        self.assertRegex(DOC["drafted"], r"^\d{4}-\d{2}-\d{2}$")
        for bid, b in DOC["builds"].items():
            self.assertEqual(b["status"], "draft for owner review", bid)
            self.assertEqual(b["drafted"], DOC["drafted"], bid)

    def test_ranges_follow_gear_bands(self):
        for bid, b in DOC["builds"].items():
            g = GEAR.get(bid) or GEAR[PLANS[bid]["base"]]
            want = [f"{x['min']}-{x['max']}" for x in g["bands"]]
            self.assertEqual([r["range"] for r in b["ranges"]], want, bid)

    def test_stats_fit_the_points_of_the_level(self):
        for bid, b in DOC["builds"].items():
            goals = {}
            for s, v in PLANS[bid]["stats"]:
                goals[s] = max(goals.get(s, 1), min(v, 99))
            prev = [1] * 6
            for r in b["ranges"]:
                vals = [r["stats"][k] for k in planlib.STATS]
                self.assertTrue(all(1 <= v <= 99 for v in vals), (bid, r["range"]))
                used = planlib.spent(vals)
                avail = planlib.stat_points(r["max"])
                self.assertLessEqual(used, avail, f"{bid} {r['range']}: {used} points used, {avail} available")
                self.assertEqual(r["stat_points_available"], avail, (bid, r["range"]))
                self.assertEqual(r["stat_points_used"], used, (bid, r["range"]))
                self.assertEqual(r["stat_points_spare"], avail - used, (bid, r["range"]))
                for i, v in enumerate(vals):  # never past the plan's own goal, never below an earlier range
                    self.assertLessEqual(v, goals.get(i, 1), (bid, r["range"], planlib.STATS[i]))
                    self.assertGreaterEqual(v, prev[i], (bid, r["range"], planlib.STATS[i]))
                prev = vals

    def test_skills_stay_inside_the_plan(self):
        for bid, b in DOC["builds"].items():
            cap = {}
            for sid, lv in PLANS[bid]["skills"]:
                cap[sid] = max(cap.get(sid, 0), lv)
            last = {}
            for r in b["ranges"]:
                for s in r["skills"]:
                    self.assertIn(s["id"], cap, f"{bid}: skill {s['name']} is not in the plan")
                    self.assertTrue(1 <= s["lv"] <= cap[s["id"]], (bid, s))
                    self.assertGreaterEqual(s["lv"], last.get(s["id"], 0), (bid, r["range"], s["name"]))
                    last[s["id"]] = s["lv"]

    def test_plans_fit_budgets_and_reach_their_goals(self):
        # 1,273 stat points at level 99; 88 skill points (first job 39 + second job 49); every goal reached (plan_check
        # replays the server's skill tree at level 99; the Go side checks the budgets and the key skills).
        for bid, b in DOC["builds"].items():
            pc = b["plan_check"]
            self.assertEqual(pc["stat_points_at_99"], 1273, bid)
            self.assertLessEqual(pc["stat_points_goals_cost"], 1273, bid)
            self.assertLessEqual(pc["skill_points_listed"], 88, bid)
            self.assertEqual(pc["stat_goals_unreached"], [], bid)
            self.assertEqual(pc["skills_unreached_at_99"], [], bid)

    def test_prose_is_regenerated_not_stale(self):
        # the "Build milestones" prose of the class docs is generated (tools/design/gen_milestones.py): no old claims
        import glob
        docs = glob.glob(os.path.join(ROOT, "docs", "classes", "*.md"))
        self.assertGreaterEqual(len(docs), 9)
        for path in docs:
            text = open(path, encoding="utf-8").read()
            i = text.find("### Build milestones")
            if i < 0:
                continue
            section = text[i:]
            self.assertNotIn("never reached", section, path)
            self.assertNotIn("(not reached by the current plan)", section, path)
            self.assertNotIn("plans unchanged", section, path)


    def test_playstyle_defined_for_solo_mvp_pvp(self):
        for bid, b in DOC["builds"].items():
            for k in ("solo", "mvp", "pvp"):
                lines = b["playstyle"][k]
                self.assertTrue(1 <= len(lines) <= 3, (bid, k))
                self.assertTrue(all(isinstance(x, str) and len(x) > 20 for x in lines), (bid, k))


if __name__ == "__main__":
    unittest.main()
