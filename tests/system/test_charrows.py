"""Tests for tools/system/charrows.py (the one seed-row builder shared by pairchars.py and addchars.py).
Checks that: the rows equal the original pairchars template (frozen below), pairchars and addchars use the same rows,
and legion_first / legion_branch / class match legion/builds.go (read-only). Run: python -m unittest discover -s
tests/system -v"""
import os as _os
import sys as _sys
_sys.path.insert(0, _os.path.join(_os.path.dirname(_os.path.abspath(__file__)), ".."))
import testenv  # noqa: E402
testenv.isolate()

import os
import random
import re
import sys
import unittest

REPO = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", ".."))
sys.path.insert(0, os.path.join(REPO, "tools", "system"))
sys.path.insert(0, os.path.join(REPO, "tools"))

import charrows as CR  # noqa: E402
import addchars as A  # noqa: E402
import pairchars as PC  # noqa: E402  (import only: its constants; main() is not run)


def original_pairchars_rows(c, p, town, rng, first, branch):
    """Frozen copy of the statement template tools/pairchars.py used before the shared builder (HEAD 291e91a).
    If this and charrows.seed_rows ever differ, pairchars changed behaviour."""
    tx, ty = {"prontera": (156, 191), "izlude": (128, 114), "payon": (162, 233), "geffen": (119, 59),
              "alberta": (116, 57)}[town]
    sex = p.get("sex", "M")
    esc = c["name"].replace("'", "''")
    sql = []
    sql.append("INSERT INTO `char` (char_id,account_id,char_num,name,class,base_level,job_level,str,agi,vit,`int`,dex,luk,"
               "max_hp,hp,max_sp,sp,status_point,skill_point,hair,hair_color,clothes_color,zeny,weapon,last_map,last_x,"
               "last_y,save_map,save_x,save_y,sex,`option`) VALUES (%d,%d,%d,'%s',0,1,1,1,1,1,1,1,1,40,40,11,11,48,0,"
               "%d,%d,%d,%d,1201,'%s',%d,%d,'%s',%d,%d,'%s',0);" % (
                   c["char_id"], p["account_id"], c["slot"], esc, 1 + rng.randrange(22), rng.randrange(9),
                   rng.randrange(4), 1500 + rng.randrange(1500), town, tx, ty, town, tx, ty, sex))
    i = c["char_id"]
    sql.append("INSERT INTO inventory (char_id,nameid,amount,equip,identify) VALUES (%d,1201,1,2,1),(%d,2301,1,16,1),"
               "(%d,569,%d,0,1),(%d,11500,%d,0,1),(%d,501,%d,0,1),(%d,601,%d,0,1),(%d,602,%d,0,1);" % (
                   i, i, i, 120 + rng.randrange(60), i, 10 + rng.randrange(10), i, 10 + rng.randrange(10), i,
                   5 + rng.randrange(5), i, 2 + rng.randrange(2)))
    sql.append("INSERT INTO char_reg_num (char_id,`key`,`index`,value) VALUES (%d,'legion_first',0,%d),"
               "(%d,'legion_branch',0,%d);" % (i, first, i, branch))
    return sql


class GoldenTests(unittest.TestCase):
    def test_shared_rows_equal_the_original_pairchars_template(self):
        for c, p, town, plan in [
                ({"char_id": 180002, "slot": 1, "name": "Hunt Shop"}, {"account_id": 2100002, "sex": "M"},
                 "geffen", "blacksmith"),
                ({"char_id": 210015, "slot": 0, "name": "O'Brien"}, {"account_id": 2100015, "sex": "F"},
                 "prontera", "hunter"),
                ({"char_id": 180009, "slot": 2, "name": "Al Potion"}, {"account_id": 2100009, "sex": "M"},
                 "payon", "alchemist")]:
            first, branch, _ = CR.plan_first_branch(plan)
            old = original_pairchars_rows(c, p, town, random.Random(20261114), first, branch)
            new = CR.seed_rows(c["char_id"], p["account_id"], c["slot"], c["name"], town, p["sex"],
                               random.Random(20261114), first, branch)
            self.assertEqual(old, new, plan)


class SameSourceTests(unittest.TestCase):
    def test_pairchars_and_addchars_use_the_same_rows(self):
        self.assertIs(PC.CR, CR)
        spec = {"char_id": 180002, "slot": 1, "name": "Hunt Shop", "plan": "blacksmith", "account_id": 2100002}
        persona = {"home_town": "geffen", "sex": "M"}
        via_addchars = A.plan_rows(spec, persona)
        first, branch, _ = CR.plan_first_branch("blacksmith")
        # pairchars calls seed_rows with the rng it shares; addchars seeds its rng with the account id
        via_pairchars = CR.seed_rows(180002, 2100002, 1, "Hunt Shop", "geffen", "M", random.Random(2100002),
                                     first, branch)
        self.assertEqual(via_addchars, via_pairchars)

    def test_pairchars_plan_tuples_are_unchanged(self):
        self.assertEqual(PC.HUNTER_PLAN, ("hunter", 3, 0, 11))
        self.assertEqual(PC.VENDOR_PLAN, ("blacksmith", 5, 0, 10))


class BuildsGoTests(unittest.TestCase):
    """legion_first / legion_branch / class must equal the engine's plan table in legion/builds.go."""

    def test_plan_values_match_builds_go(self):
        with open(os.path.join(REPO, "legion", "builds.go"), encoding="utf-8") as f:
            src = f.read()
        for plan in ("hunter", "blacksmith", "alchemist"):
            m = re.search(r'"%s": \{ID: "%s",[^}]*?Class: (\d+), First: (\d+), Branch: (\d+),' % (plan, plan), src)
            self.assertIsNotNone(m, plan)
            cls, first, branch = (int(m.group(i)) for i in (1, 2, 3))
            self.assertEqual(CR.PLANS[plan], (first, branch, cls), plan)

    def test_unknown_plan_is_refused(self):
        with self.assertRaises(KeyError):
            CR.plan_first_branch("knight")

    def test_only_allowed_tables(self):
        self.assertEqual(CR.TABLE_ALLOW, ("char", "inventory", "char_reg_num"))
        self.assertEqual(CR.table_of("INSERT INTO `char` (char_id) VALUES (1);"), "char")
        self.assertIsNone(CR.table_of("INSERT INTO `char` SELECT 1;"))


if __name__ == "__main__":
    unittest.main()
