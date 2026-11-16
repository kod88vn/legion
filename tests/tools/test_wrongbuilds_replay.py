"""Replay of the pre-rebalance plans through the wrongbuilds.py rules (plan_problems, the same code audit() runs).

Every build's plan before the 2026-11-15 rebalance (tests/tools/fixtures/builds_before_rebalance.go, builds.go at 18d2aa4)
is played as a character that followed it: first-job levels at every point count, and stat totals at a sweep of
budgets. The current plans (with their Legacy / LegacyStats) must flag none of them, so no existing character is a
wrong build and `wrongbuilds.py reset` cannot delete one. Offline: reads repo files only.
Run: python -m unittest discover -s tests/tools -p "test_wrongbuilds_replay.py" -v"""
import os
import sys
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.normpath(os.path.join(HERE, "..", ".."))
sys.path.insert(0, os.path.join(ROOT, "tools"))
import wrongbuilds as wb  # noqa: E402

FIXTURE = os.path.join(HERE, "fixtures", "builds_before_rebalance.go")
OLD_PLANS, OLD_VENDOR_FIRST = wb.parse_plans(open(FIXTURE, encoding="utf8").read())
# builds.go on main before the 2026-11-15 realism branch (commit b35448b): the plans live characters follow today
MAIN_FIXTURE = os.path.join(HERE, "fixtures", "builds_main_b35448b.go")
MAIN_PLANS, _ = wb.parse_plans(open(MAIN_FIXTURE, encoding="utf8").read())
# builds.go on main at 24e5838 (before the 2026-11-15 wizard plans: Meteor Storm 10, Storm Gust / Vermilion in wizard_fire)
MAIN_24E_FIXTURE = os.path.join(HERE, "fixtures", "builds_main_24e5838.go")
MAIN_24E_PLANS, _ = wb.parse_plans(open(MAIN_24E_FIXTURE, encoding="utf8").read())


def replay(test, old_plans):
    """Every build in old_plans, played as a character that followed it, against the current plans; returns the
    number of checks made (must be > 0 for the test to mean anything)."""
    test.assertEqual(sorted(old_plans), sorted(wb.plans), "same 37 builds before and after")
    checks = 0
    for bid, old in old_plans.items():
        new = wb.plans[bid]
        test.assertEqual(old["first"], new["first"], bid)
        role = "vendor" if old["base"] in ("blacksmith", "alchemist") else "dps"
        cls = old["first"]
        if cls not in wb.TREE:
            continue
        # first-job levels: the old character at every first-job point count (up to 40 points)
        full_stats = wb.sim_stats(old, 1273)
        for n in range(0, 41):
            sk = dict(wb.sim_skills(old, role, cls, n))
            problems, _ = wb.plan_problems(new, role, cls, sk, full_stats)
            test.assertEqual(problems, [], f"{bid} at {n} first-job points: {problems}")
            checks += 1
        # stats: the old character at a sweep of stat budgets, skills at the full first job
        full_sk = dict(wb.sim_skills(old, role, cls, 39))
        for budget in list(range(0, 1274, 25)) + [1273]:
            st = wb.sim_stats(old, budget)
            problems, _ = wb.plan_problems(new, role, cls, full_sk, st)
            test.assertEqual(problems, [], f"{bid} at stat budget {budget}: {problems}")
            checks += 1
    return checks


class WrongBuildsReplayTest(unittest.TestCase):
    def test_every_old_plan_is_still_valid(self):
        self.assertGreater(replay(self, OLD_PLANS), 0)

    def test_every_plan_on_main_is_still_valid(self):
        # the plans live characters follow on main today (wizard_fire / wizard_frost need their Legacy caps)
        self.assertEqual(len(MAIN_PLANS), 37)
        self.assertGreater(replay(self, MAIN_PLANS), 0)

    def test_every_plan_on_main_at_24e5838_is_still_valid(self):
        # the wizard plans before the 2026-11-15 wizard rebalance: every character that followed them stays valid
        self.assertEqual(len(MAIN_24E_PLANS), 37)
        self.assertGreater(replay(self, MAIN_24E_PLANS), 0)


if __name__ == "__main__":
    unittest.main()
