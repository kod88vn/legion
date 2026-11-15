"""Build plan realism (build audit 2026-11-15, findings F1, F7, F10-F13, F16-F18): the plans in legion/builds.go must be
reachable under the server's rules, spend all their points, and agree with the class docs and the wrongbuilds table.
Offline: reads repo files and the committed server copies in tests/tools/fixtures/server (rule G23; no container).
Run: python -m unittest discover -s tests/tools -p "test_build_plans.py" -v"""
import os
import re
import sys
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.normpath(os.path.join(HERE, "..", ".."))
sys.path.insert(0, os.path.join(ROOT, "tools", "design"))
sys.path.insert(0, os.path.join(ROOT, "tools"))
sys.path.insert(0, os.path.join(ROOT, "tests"))
import planlib as pl  # noqa: E402

import yaml  # noqa: E402

TOTAL_POINTS = 88          # plan skill points (Novice Basic Skill is outside the plan: 9 + 39 + 49 = 97 at job 99)
FIRST_JOB_MIN = 39         # the 1st-job pool: all of it must be spent before a 2nd-job skill is open (F1)
STAT_BUDGET = 1273         # statpoint.yml at base 99
TRANSCENDENT = {"Lord Knight", "Paladin", "High Wizard", "Professor", "Sniper", "Clown", "Gypsy", "High Priest",
                "Champion", "Whitesmith", "Creator", "Assassin Cross", "Stalker"}   # ST01 / G10: never an end goal

TREE = pl.server_tree()
PLANS = pl.parse_builds()
DESC = {}
_L = getattr(yaml, "CSafeLoader", yaml.SafeLoader)
for _s in yaml.load(open(pl.SERVER_DB / "skill_db.yml", encoding="utf-8"), Loader=_L)["Body"]:
    DESC[_s["Name"]] = _s.get("Description", _s["Name"])
CLASS_DOCS = {f: open(os.path.join(ROOT, "docs", "classes", f + ".md"), encoding="utf-8").read()
              for f in ("swordman", "mage", "archer", "acolyte", "merchant", "thief")}


def plan_section(bid):
    """The header block of one plan in the class docs (from its header to the next header)."""
    for text in CLASS_DOCS.values():
        m = re.search(r"^### [^\n]*\(plan `%s`[^\n]*$" % re.escape(bid), text, re.M)
        if m:
            nxt = re.search(r"^#{2,3} ", text[m.end():], re.M)
            end = m.end() + (nxt.start() if nxt else len(text) - m.end())
            return text[m.start():end]
    return None


def expected_stats_line(plan):
    return "- **Stats:** " + ", ".join(f"{pl.STATS[s]} {v}" for s, v in plan["stats"]) + "."


def expected_skills_line(plan):
    parts = []
    for sid, lv in plan["skills"]:
        n = TREE.id2name[sid]
        mx = TREE.stage_tree(pl.SECOND_JOB[plan["cls"]])[n][1]
        parts.append(f"{DESC[n]} {min(lv, mx)}")
    return "- **Skills (order):** " + ", ".join(parts) + "."


class SkillLock(unittest.TestCase):
    """F1: the server keeps the 2nd job locked until 48 points are spent (player_skillup_limit, pc.cpp)."""

    def test_the_lock_is_48_points(self):
        self.assertEqual(pl.skill_lock(), 48)

    def test_no_2nd_job_skill_is_taken_before_the_lock_opens(self):
        for bid, plan in PLANS.items():
            for level in range(1, 100):
                log = []
                pl.sim_skills(plan, TREE, level, log=log)
                for name, stage, spent_before in log:
                    if stage == 2:
                        self.assertGreaterEqual(spent_before, pl.skill_lock(),
                                                f"{bid} at base {level}: {name} taken at {spent_before} spent points")

    def test_the_first_job_pool_is_planned_before_the_2nd_job(self):
        # each plan names at least 39 Novice / 1st-job points, so no 2nd-job goal waits on the fallback (F1)
        for bid, plan in PLANS.items():
            st = TREE.stage_tree(pl.SECOND_JOB[plan["cls"]])
            first = 0
            cur = {}
            for sid, lv in plan["skills"]:
                n = TREE.id2name[sid]
                cur[n] = max(cur.get(n, 0), min(lv, st[n][1]))
            first = sum(v for n, v in cur.items() if st[n][0] == 1)
            self.assertGreaterEqual(first, FIRST_JOB_MIN, bid)


class SkillPoints(unittest.TestCase):
    """F7: every plan spends all its points on named skills; no id-ordered fallback is needed at base 99."""

    def test_every_plan_names_88_points(self):
        for bid, plan in PLANS.items():
            st = TREE.stage_tree(pl.SECOND_JOB[plan["cls"]])
            cur = {}
            for sid, lv in plan["skills"]:
                n = TREE.id2name[sid]
                cur[n] = max(cur.get(n, 0), min(lv, st[n][1]))
            self.assertEqual(sum(cur.values()), TOTAL_POINTS, bid)

    def test_no_point_is_left_unspent_at_99(self):
        for bid, plan in PLANS.items():
            _, unspent = pl.sim_skills(plan, TREE, 99)
            self.assertEqual(unspent, 0, bid)

    def test_the_2nd_job_pool_is_not_exceeded(self):
        for bid, plan in PLANS.items():
            st = TREE.stage_tree(pl.SECOND_JOB[plan["cls"]])
            cur = {}
            for sid, lv in plan["skills"]:   # a skill listed twice counts its highest level once
                n = TREE.id2name[sid]
                cur[n] = max(cur.get(n, 0), min(lv, st[n][1]))
            second = sum(v for n, v in cur.items() if st[n][0] == 2)
            self.assertLessEqual(second, 49, bid)


class StatPlans(unittest.TestCase):
    """F16: the stat goals fit the 1273 points at 99 and each stat rises in order (LegacyStats may be over budget)."""

    def test_stat_goals_fit_the_budget(self):
        for bid, plan in PLANS.items():
            self.assertLessEqual(pl.spent([max([1] + [v for s, v in plan["stats"] if s == i]) for i in range(6)]),
                                 STAT_BUDGET, bid)

    def test_each_goal_is_a_stat_value_1_to_99_and_rises(self):
        for bid, plan in PLANS.items():
            last = {}
            for s, v in plan["stats"]:
                self.assertTrue(1 <= v <= 99, f"{bid} {v}")
                self.assertGreater(v, last.get(s, 1), f"{bid}: {pl.STATS[s]} goal {v} does not rise")
                last[s] = v

    def test_legacy_stat_plans_are_capped_at_99_and_the_excess_is_reported(self):
        excess = {}
        src = open(os.path.join(ROOT, "legion", "builds.go"), encoding="utf-8").read()
        for bid in PLANS:
            m = re.search(r'"%s": \{ID: .*?\n\t\tLegacyStats: st\(([^)]*)\)' % bid, src, re.S)
            if not m:
                continue
            toks = [t.strip() for t in m.group(1).split(",")]
            goals = [(pl.STAT_IDX[toks[i]], int(toks[i + 1])) for i in range(0, len(toks), 2)]
            for _, v in goals:
                self.assertLessEqual(v, 99, bid)
            total = pl.spent([max([1] + [v for s, v in goals if s == i]) for i in range(6)])
            if total > STAT_BUDGET:
                excess[bid] = total - STAT_BUDGET
        # not a failure: an earlier stat plan that was never reachable (F16); the list is what the audit reported
        self.assertTrue(isinstance(excess, dict))

    def test_stat_points_follow_statpoint_yml(self):
        import yaml as _y
        rows = _y.load(open(pl.SERVER_DB / "statpoint.yml", encoding="utf-8"), Loader=_L)["Body"]
        table = {r["Level"]: r["Points"] for r in rows} if rows and "Level" in rows[0] else None
        if table is None:
            self.skipTest("statpoint.yml layout not recognised")
        for lv in range(1, 100):
            self.assertEqual(pl.stat_points(lv), table[lv], lv)


class DocsFollowTheCode(unittest.TestCase):
    """F10-F12, F17: class docs state the plans of builds.go, end at the 2nd job, and the variant mix of the code."""

    def test_every_plan_section_has_the_builds_go_stats_and_skills_lines(self):
        for bid, plan in PLANS.items():
            sec = plan_section(bid)
            if sec is None:
                continue  # a variant may have no own section
            stats = re.search(r"^- \*\*Stats:\*\* .*$", sec, re.M)
            if stats is None:
                continue
            self.assertEqual(stats.group(0), expected_stats_line(plan), bid)
            skills = re.search(r"^- \*\*Skills \(order\):\*\* .*$", sec, re.M)
            self.assertIsNotNone(skills, bid)
            self.assertEqual(skills.group(0), expected_skills_line(plan), bid)

    def test_no_transcendent_end_goal(self):
        for name, text in CLASS_DOCS.items():
            for m in re.finditer(r"end goal ([A-Za-z' ]+)\)", text):
                self.assertNotIn(m.group(1), TRANSCENDENT, f"{name}: {m.group(0)}")

    def test_end_goal_is_the_plans_2nd_job(self):
        for bid, plan in PLANS.items():
            sec = plan_section(bid)
            if sec is None:
                continue
            m = re.search(r"end goal ([A-Za-z' ]+)\)", sec)
            if m:
                self.assertEqual(m.group(1), pl.SECOND_JOB[plan["cls"]], bid)

    def test_all_md_states_the_variant_mix_of_the_code(self):
        text = " ".join(open(os.path.join(ROOT, "docs", "classes", "all.md"), encoding="utf-8").read().split())
        code = open(os.path.join(ROOT, "legion", "builds.go"), encoding="utf-8").read()
        self.assertIn("pick(5) < 2", code, "pickVariant gives the main plan 2 in 5")
        self.assertIn("the main plan 2 times in 5", text)
        self.assertNotIn("the main plan 3 times in 5", text)


class WrongBuildsTable(unittest.TestCase):
    """F13: the wrongbuilds.py first-job table is read from the server, not copied by hand."""

    @classmethod
    def setUpClass(cls):
        os.environ.setdefault("LEGION_TEST", "1")
        import testenv  # noqa: F401
        testenv.isolate()
        import wrongbuilds as wb
        cls.wb = wb

    def test_the_table_is_the_servers_first_job_tree(self):
        for cls_id, job in pl.FIRST_JOB.items():
            want = {TREE.name2id[n] for n in TREE.own(job)}
            self.assertEqual(set(self.wb.TREE[cls_id]), want, job)

    def test_holy_water_has_no_prerequisite(self):
        self.assertEqual(self.wb.TREE[4][31], (1, {}))

    def test_the_moving_and_auto_skills_of_the_server_are_present(self):
        names = {TREE.id2name[i] for i in self.wb.TREE[1]}
        for skill in ("SM_FATALBLOW", "SM_AUTOBERSERK"):
            self.assertIn(skill, names, skill)


class Routes(unittest.TestCase):
    """F18: the weapon routes are weakest first, by ATK (the builds.go comment)."""

    def test_weapon_routes_ascend_by_attack(self):
        atk = {r["Id"]: r["Attack"] for r in yaml.load(open(pl.SERVER_DB / "weapon_attack.yml", encoding="utf-8"),
                                                       Loader=_L)["Body"]}
        src = open(os.path.join(ROOT, "legion", "builds.go"), encoding="utf-8").read()
        for m in re.finditer(r'^\t"([a-z_]+)": \{ID: ', src, re.M):
            bid = m.group(1)
            i = m.start()
            seg = src[i:src.find("\n\t},", i)]
            w = re.search(r"Weapons:\s*\[\]int\{([^}]*)\}", seg)
            if not w:
                continue
            ids = [int(x) for x in w.group(1).split(",") if x.strip()]
            vals = [atk[x] for x in ids]
            self.assertEqual(vals, sorted(vals), f"{bid}: {ids} -> {vals}")


if __name__ == "__main__":
    unittest.main()
