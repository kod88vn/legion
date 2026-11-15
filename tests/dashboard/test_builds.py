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
        # hunter (builds.go, Stats line): DEX 99, AGI 75, VIT 40, LUK 30 are the last milestone of each stat (audit F10)
        hunter = next(b for b in load_export()["builds"] if b["id"] == "hunter")
        got = {s["stat"]: s["value"] for s in hunter["end"]["stats"]}
        self.assertEqual(got, {"DEX": 99, "AGI": 75, "VIT": 40, "LUK": 30})
        self.assertEqual(hunter["class"]["name"], "Hunter")
        self.assertEqual(hunter["class"]["first"], "Archer")

    def test_every_build_leads_with_a_2nd_job_end_goal(self):
        for b in load_export()["builds"]:
            self.assertTrue(b["class"]["name"], b["id"])
            self.assertTrue(b["end"]["skills"], b["id"])
            self.assertTrue(b["steps"]["stats"], b["id"])
            self.assertTrue(b["steps"]["skills"], b["id"])

    def test_solo_sentences_must_describe_fighting_or_farming_alone(self):
        # the three false hits of the first version: a wing rule, an errand, a sourcing line
        self.assertEqual(buildexport.solo_clauses("Fly Wing only solo"), [])
        self.assertEqual(buildexport.solo_clauses("solo trip, or the party member's \"weapon\" errand"), [])
        self.assertEqual(buildexport.solo_clauses("Guild farmers (economy agent) come from these solo DS Hunters."), [])
        # real solo fighting text is kept
        self.assertEqual(buildexport.solo_clauses("Solo: no boss/swarm, Fire Wall vs a walker; quick"),
                         ["Solo: no boss/swarm, Fire Wall vs a walker"])

    def test_knight_assassin_hunter_solo_is_draft_text_not_the_doc_false_hits(self):
        # the three doc false hits (Fly Wing, solo trip, solo DS Hunters) are never used; the design draft's solo
        # text fills the gap instead, labelled draft (owner to confirm)
        by = {b["id"]: b for b in load_export()["builds"]}
        for bid in ("knight", "assassin", "hunter"):
            solo = by[bid]["playstyle"]["solo"]
            self.assertEqual(solo["status"], "draft", bid)
            self.assertTrue(solo["lines"], bid)
            for line in solo["lines"]:
                self.assertTrue(line["source"].startswith("draft: legion/data/buildmilestones.json"), bid)
                self.assertNotIn("docs/classes", line["source"], bid)

    def test_mage_and_acolyte_solo_text_is_defined(self):
        # docs/classes/mage.md:26 (wizard, sage plans) and acolyte.md:26-27 (priest plans) describe solo play
        by = {b["id"]: b for b in load_export()["builds"]}
        self.assertEqual(by["wizard"]["playstyle"]["solo"]["status"], "defined")
        self.assertEqual(by["priest"]["playstyle"]["solo"]["status"], "defined")

    def test_pvp_is_draft_or_gap_and_never_unlabelled(self):
        for b in load_export()["builds"]:
            pvp = b["playstyle"]["pvp"]
            self.assertIn(pvp["status"], ("draft", "gap"), b["id"])
            if pvp["status"] == "gap":
                self.assertEqual(pvp["lines"], [], b["id"])
            else:
                self.assertEqual(pvp["note"], "draft for owner review", b["id"])
                self.assertTrue(all(l["source"].startswith("draft:") for l in pvp["lines"]), b["id"])

    def test_part_counts_split_defined_from_draft_per_part(self):
        doc = load_export()
        pc = doc["part_counts"]
        self.assertEqual(sorted(pc), ["mvp", "pvp", "solo"])
        for part in ("solo", "mvp", "pvp"):
            c = pc[part]
            self.assertEqual(c["defined"] + c["draft"] + c["gap"], len(doc["builds"]), part)
            self.assertEqual(c["defined"], sum(1 for b in doc["builds"] if b["playstyle"][part]["status"] == "defined"), part)
            self.assertEqual(c["draft"], sum(1 for b in doc["builds"] if b["playstyle"][part]["status"] == "draft"), part)
        # knight solo is draft only (owner call: the draft fills it, labelled)
        self.assertGreater(pc["solo"]["draft"], 0)

    def test_every_draft_playstyle_part_is_labelled(self):
        for b in load_export()["builds"]:
            for part, val in b["playstyle"].items():
                if val["status"] == "draft":
                    self.assertEqual(val["note"], "draft for owner review", (b["id"], part))
                    self.assertTrue(all(l["source"].startswith("draft: legion/data/buildmilestones.json")
                                        for l in val["lines"]), (b["id"], part))

    def test_plan_check_warnings_come_from_the_draft(self):
        by = {b["id"]: b for b in load_export()["builds"]}
        knight = by["knight"]["plan_check"]
        self.assertEqual(knight["status"], "draft")
        # the warnings are exactly what the draft's figures say (the draft is regenerated from builds.go)
        with open(os.path.join(REPO, "legion", "data", "buildmilestones.json"), encoding="utf-8") as f:
            figs = json.load(f)["builds"]["knight"]["plan_check"]
        self.assertEqual(knight["figures"], figs)
        self.assertEqual(knight["warnings"], buildexport.plan_warnings(figs))
        # a build whose plan reaches every goal has no unreached-goal warning
        for w in by["hunter_blitz"]["plan_check"]["warnings"]:
            self.assertNotIn("never reached", w)

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
                self.assertEqual(m["range"], "%s-%s" % (m["min"], m["max"]))
                self.assertIn(m["stats"]["status"], ("draft", "gap"))
                self.assertIn(m["skills"]["status"], ("draft", "gap"))

    def test_milestone_stats_and_skills_are_the_draft_values(self):
        with open(os.path.join(REPO, "legion", "data", "buildmilestones.json"), encoding="utf-8") as f:
            draft = json.load(f)["builds"]
        by = {b["id"]: b for b in load_export()["builds"]}
        for bid, entry in draft.items():
            rows = by[bid]["milestones"]
            self.assertEqual([r["range"] for r in rows], [r["range"] for r in entry["ranges"]], bid)
            for row, want in zip(rows, entry["ranges"]):
                self.assertEqual(row["stats"]["status"], "draft", bid)
                self.assertEqual(row["stats"]["values"]["STR"], want["stats"]["STR"], bid)
                self.assertEqual(row["skills"]["items"], want["skills"], bid)
                self.assertEqual(row["stats"]["points"]["spare"], want["stat_points_spare"], bid)
                self.assertIn("draft:", row["stats"]["source"], bid)

    def test_milestones_read_at_export_time_not_a_frozen_copy(self):
        with open(os.path.join(REPO, "legion", "data", "buildmilestones.json"), encoding="utf-8") as f:
            draft = json.load(f)
        draft["builds"]["knight"]["ranges"][0]["stats"]["STR"] = 99
        tmp = tempfile.mkdtemp()
        path = os.path.join(tmp, "buildmilestones.json")
        with open(path, "w", encoding="utf-8", newline="\n") as f:
            json.dump(draft, f)
        items, skills, _ = buildexport.lookup_names()
        doc = buildexport.build_export(items, skills, {"MILESTONES": path})
        knight = next(b for b in doc["builds"] if b["id"] == "knight")
        self.assertEqual(knight["milestones"][0]["stats"]["values"]["STR"], 99)

    def test_engine_gear_hook_is_inert_until_the_file_exists(self):
        by = {b["id"]: b for b in load_export()["builds"]}
        for row in by["knight"]["milestones"]:
            self.assertTrue(row["gear"]["source"].startswith("legion/data/buildgear.json"), row["range"])

    def test_engine_gear_hook_is_used_when_the_file_exists(self):
        tmp = tempfile.mkdtemp()
        path = os.path.join(tmp, "buildmilestones_gear.json")
        with open(path, "w", encoding="utf-8", newline="\n") as f:
            json.dump({"builds": {"knight": {"ranges": [
                {"min": 1, "max": 99, "slots": {"weapon": [1201]}}]}}}, f)
        items, skills, _ = buildexport.lookup_names()
        doc = buildexport.build_export(items, skills, {"ENGINE_GEAR": path})
        knight = next(b for b in doc["builds"] if b["id"] == "knight")
        self.assertTrue(all(r["gear"]["source"].startswith("engine milestone gear") for r in knight["milestones"]))

    def test_engine_file_cards_reach_every_milestone_row_of_the_tab(self):
        # the engine file carries cards: each Builds-tab row keeps every card id of its band, none is lost; a card with
        # no card_entries keeps its id and name (slot None), the others get their slot, prio and why
        tmp = tempfile.mkdtemp()
        path = os.path.join(tmp, "buildmilestones_gear.json")
        cards = [4006, 4020, 9999]
        entries = [{"id": 4006, "slot": "weapon", "prio": 7, "why": "ATK for the build"},
                   {"id": 4020, "slot": "armor", "prio": 3, "why": "DEF"}]
        with open(path, "w", encoding="utf-8", newline="\n") as f:
            json.dump({"builds": {"knight": {"ranges": [
                {"min": 1, "max": 99, "slots": {"weapon": [1201]}, "cards": cards, "card_entries": entries}]}}}, f)
        items, skills, _ = buildexport.lookup_names()
        doc = buildexport.build_export(items, skills, {"ENGINE_GEAR": path})
        knight = next(b for b in doc["builds"] if b["id"] == "knight")
        self.assertTrue(knight["milestones"], "knight has no milestone rows")
        for row in knight["milestones"]:
            self.assertEqual([c["id"] for c in row["cards"]], cards, row["range"])
            self.assertEqual(row["cards"][0]["slot"], "weapon", row["range"])
            self.assertEqual(row["cards"][0]["prio"], 7, row["range"])
            self.assertEqual(row["cards"][1]["why"], "DEF", row["range"])
            self.assertIsNone(row["cards"][2]["slot"], row["range"])
            self.assertEqual(row["gear"]["cards"], row["cards"], row["range"])

    def test_the_export_is_the_same_as_a_fresh_run_in_every_value(self):
        # the committed file must equal what the Go source says now: every value, not only the ids and gaps
        items, skills, _ = buildexport.lookup_names()
        fresh = buildexport.build_export(items, skills)
        old = load_export()
        self.assertEqual([b["id"] for b in fresh["builds"]], [b["id"] for b in old["builds"]])
        self.assertEqual(fresh["gaps"], old["gaps"])
        for fb, ob in zip(fresh["builds"], old["builds"]):
            for key in fb:
                self.assertEqual(fb[key], ob[key], "%s: %s differs from builds.json (re-run buildexport.py)" % (fb["id"], key))

    def test_the_stats_skills_class_and_gear_of_builds_go_are_read_back(self):
        # independent of the parser: the Go literal's own numbers, read straight from the source text
        with open(BUILDS_GO, encoding="utf-8") as f:
            src = f.read()
        doc = load_export()
        by = {b["id"]: b for b in doc["builds"]}
        for bid, cls in re.findall(r'^	"(\w+)": \{ID: "\w+", (?:Base: "\w+", )?Name: "[^"]*", Class: (\d+)', src, re.M):
            self.assertEqual(by[bid]["class"]["id"], int(cls), bid)
        # every stat goal of hunter, in order, as written in builds.go ("stDEX, 20, stAGI, 10, ...")
        m = re.search(r'"hunter": \{ID:[^\n]*\n\s*Stats:\s+st\(([^)]*)\)', src)
        toks = [t.strip() for t in m.group(1).split(",")]
        expected = ["%s %s" % (toks[i][2:].upper(), toks[i + 1]) for i in range(0, len(toks), 2)]
        self.assertEqual([s["label"] for s in by["hunter"]["steps"]["stats"]], expected)

    def test_names_are_resolved_not_placeholders(self):
        self.assertEqual(load_export().get("unnamed"), 0,
                         "builds.json has '#id' placeholder names: a lookup table is missing (see name_sources)")

    def test_build_ids_are_found_without_the_parser_regex(self):
        # three different reads of builds.go: every ID: "x" literal, every builds["x"] assignment, every map key
        with open(BUILDS_GO, encoding="utf-8") as f:
            src = f.read()
        found = set(re.findall(r'\bID:\s*"(\w+)"', src)) | set(re.findall(r'builds\["(\w+)"\]\s*=', src))
        exported = {b["id"] for b in load_export()["builds"]}
        self.assertEqual(sorted(found - exported), [], "builds in builds.go missing from builds.json")
        self.assertGreaterEqual(len(found), len(go_build_ids()))


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
        for cls in (".bd-part", ".bd-card", ".bd-gap", ".bd-draft", ".bd-warn"):   # each class the tab uses has a rule
            self.assertIn(cls, html)
            self.assertRegex(html, re.escape(cls) + r"\s*\{", cls)


if __name__ == "__main__":
    unittest.main()
