"""Ayla's OpenKore statsAddAuto_list must follow her build (legion/builds.go, plan "assassin").

Each build step is a cumulative target "stat reaches value"; the OpenKore list uses the same "value stat" order.
Run: python -m unittest tests/ayla/test_statlist.py  (from the repo root)
"""
import os as _os  # G23: tests never touch the live legion/data folder
import sys as _sys
_sys.path.insert(0, _os.path.join(_os.path.dirname(_os.path.abspath(__file__)), ".."))
import testenv  # noqa: E402
testenv.isolate()  # LEGION_DATA -> a temp folder before any tool module is imported

import pathlib
import re
import unittest

ROOT = pathlib.Path(__file__).resolve().parents[2]
ABBR = {"stSTR": "str", "stAGI": "agi", "stVIT": "vit", "stINT": "int", "stDEX": "dex", "stLUK": "luk"}


def plan_steps(plan_id):
    src = (ROOT / "legion" / "builds.go").read_text(encoding="utf-8")
    m = re.search(r'"%s": \{ID: "%s".*?Stats:\s+st\((.*?)\),\n' % (plan_id, plan_id), src, re.S)
    if not m:
        raise AssertionError("no Stats for plan " + plan_id)
    toks = [t.strip() for t in m.group(1).split(",")]
    return [f"{int(toks[i + 1])} {ABBR[toks[i]]}" for i in range(0, len(toks), 2)]


def config_steps():
    text = (ROOT / "openkore-ayla" / "control" / "config.txt").read_text(encoding="utf-8")
    m = re.search(r"^statsAddAuto_list (.*)$", text, re.M)
    if not m:
        raise AssertionError("no statsAddAuto_list")
    return [s.strip() for s in m.group(1).split(",")]


class AylaStatList(unittest.TestCase):
    def test_follows_assassin_plan(self):
        self.assertEqual(config_steps(), plan_steps("assassin"))

    def test_spends_dex_and_vit_steps(self):
        steps = config_steps()
        self.assertIn("35 dex", steps)
        self.assertIn("40 vit", steps)


if __name__ == "__main__":
    unittest.main()
