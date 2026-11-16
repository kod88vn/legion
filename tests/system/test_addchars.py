"""Tests for tools/system/addchars.py (additive vendor inserts, design section 2.1, WP4). Fixtures and temp folders
only (rule G23). run_live is replaced by a recorder in every test, so no test can reach ro-db.
Run: python -m unittest discover -s tests/system -v"""
import os as _os
import sys as _sys
_sys.path.insert(0, _os.path.join(_os.path.dirname(_os.path.abspath(__file__)), ".."))
import testenv  # noqa: E402
testenv.isolate()

import io
import json
import os
import re
import shutil
import sys
import tempfile
import unittest
from contextlib import redirect_stdout

REPO = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", ".."))
sys.path.insert(0, os.path.join(REPO, "tools", "system"))

import addchars as A  # noqa: E402
import farmerpick as F  # noqa: E402
from test_farmerpick import HDR, dump_row, persona  # noqa: E402


def vendor_add(aid, cid, slot, name, account=None):
    return {"account": account or "lg%04d" % (aid - 2100000), "account_id": aid, "char_id": cid, "slot": slot,
            "name": name, "plan": "blacksmith", "target_class": 10, "class": 0, "build": "str-dex"}


def farmers_doc(adds):
    return {"schema": 1, "since": "2026-11-15T10:00:00+0000", "accounts": [a["account"] for a in adds],
            "detail": [], "vendor_adds": adds}


class RangeTests(unittest.TestCase):
    def test_add_range_is_2104001_to_2199999(self):
        A.check_add_account(2104001)
        A.check_add_account(2199999)
        for bad in (2100001, 2104000, 2200000, 1, 0, 2300001):
            with self.assertRaises(A.Refused, msg=str(bad)):
                A.check_add_account(bad)


class GuardTests(unittest.TestCase):
    def test_refuses_delete_update_and_other_verbs(self):
        for s in ["DELETE FROM `char` WHERE char_id=1;",
                  "UPDATE `char` SET zeny=0 WHERE char_id=1;",
                  "REPLACE INTO `char` VALUES (1);",
                  "SELECT * FROM `char`;",
                  "DROP TABLE `char`;",
                  "INSERT INTO `char` (char_id) VALUES (1); DELETE FROM `char`;",
                  "INSERT INTO inventory (char_id) VALUES (1); TRUNCATE inventory;",
                  "INSERT INTO t (a) VALUES (1); UPDATE t SET a=2;"]:
            with self.assertRaises(A.Refused, msg=s):
                A.guard_sql([s])

    def test_quoted_names_are_not_scanned(self):
        ok = "INSERT INTO `char` (char_id,name) VALUES (1,'Update Mart');"
        self.assertEqual(A.guard_sql([ok]), [ok])

    def test_generated_statements_are_insert_only_and_in_range(self):
        spec = vendor_add(2104001, 180001, 1, "Hunt Shop")
        stmts = A.char_statements(spec, {"home_town": "geffen", "sex": "M"}, rng_seed=2104001)
        self.assertEqual(len(stmts), 3)
        for s in stmts:
            self.assertTrue(s.startswith("INSERT INTO "))
            self.assertFalse(re.search(r"\b(DELETE|UPDATE|REPLACE|DROP|ALTER|TRUNCATE)\b", s))
        self.assertIn("VALUES (180001,2104001,1,", stmts[0])
        self.assertIn("'legion_branch'", stmts[2])


class PlanTests(unittest.TestCase):
    def setUp(self):
        self.personas = [persona(1, vendor=True), persona(2), persona(4)]     # 2104001 has no vendor yet
        self.by_acc = {p["account_id"]: p for p in self.personas}
        self.by_acc[2104001] = persona(4, aid=2104001)

    def _dump(self, extra=()):
        rows = [dump_row(2104001, 214001, 0, "Hunt4"), dump_row(2100002, 210002, 0, "Hunt2")]
        return rows + list(extra)

    def test_accepts_a_clean_add_in_range(self):
        doc = farmers_doc([vendor_add(2104001, 184001, 1, "Hunt4 Shop")])
        stmts, rep = A.plan(doc, self._rows(), self.by_acc)
        self.assertEqual(rep["ok"], ["lg4001"])
        self.assertEqual(rep["refused"], [])
        self.assertEqual(len(stmts), 3)

    def _rows(self, extra=()):
        return [{"account_id": r["account_id"], "char_id": r["char_id"], "char_num": r["char_num"],
                 "name": r["name"], "class": r["class"], "guild_id": r["guild_id"]} for r in self._dump(extra)]

    def test_refusals_per_account(self):
        cases = {
            "out of range": (vendor_add(2100002, 184002, 1, "Hunt2 Shop"), []),
            "already has vendor": (vendor_add(2100001, 184001, 1, "Zed Shop"), []),
            "slot taken": (vendor_add(2104001, 184001, 0, "Hunt4 Shop"), []),
            "id taken": (vendor_add(2104001, 210002, 1, "Hunt4 Shop"), []),
            "name taken": (vendor_add(2104001, 184001, 1, "Hunt2"), []),
            "in a guild": (vendor_add(2104001, 184001, 1, "Hunt4 Shop"), [dict(dump_row(2104001, 214002, 2, "G", guild=5))]),
        }
        for label, (add, extra) in cases.items():
            rows = self._rows(extra)
            if label == "in a guild":
                rows = [r for r in rows if r["char_id"] != 214001]
            stmts, rep = A.plan(farmers_doc([add]), rows, self.by_acc)
            self.assertEqual(rep["ok"], [], label)
            self.assertEqual(len(rep["refused"]), 1, label)
            self.assertEqual(stmts, [], label)

    def test_account_missing_from_dump_is_refused(self):
        stmts, rep = A.plan(farmers_doc([vendor_add(2104001, 184001, 1, "X Shop")]), [], self.by_acc)
        self.assertEqual(rep["refused"][0][1], "no login row in the dump")


class CliTests(unittest.TestCase):
    def setUp(self):
        self.d = tempfile.mkdtemp(prefix="addchars-")
        self.farmers = os.path.join(self.d, "farmers.json")
        self.dump = os.path.join(self.d, "chars.tsv")
        self.personas = os.path.join(self.d, "personas.json")
        with open(self.farmers, "w", encoding="utf-8") as f:
            json.dump(farmers_doc([vendor_add(2104001, 184001, 1, "Hunt4 Shop")]), f)
        with open(self.dump, "w", encoding="utf-8") as f:
            f.write(HDR + "2104001\t214001\t0\tHunt4\t0\t0\n")
        with open(self.personas, "w", encoding="utf-8") as f:
            json.dump([persona(4, aid=2104001)], f)
        self.recorded = []
        self._orig_live = A.run_live
        A.run_live = lambda stmts: self.recorded.append(list(stmts)) or len(stmts)  # never reaches ro-db

    def tearDown(self):
        A.run_live = self._orig_live
        shutil.rmtree(self.d, ignore_errors=True)

    def _run(self, *extra):
        buf = io.StringIO()
        with redirect_stdout(buf):
            rc = A.main(["--farmers", self.farmers, "--dump", self.dump, "--personas", self.personas] + list(extra))
        return rc, buf.getvalue()

    def test_dry_run_is_the_default(self):
        copy = os.path.join(self.d, "copy")
        rc, out = self._run("--copy", copy)
        self.assertEqual(rc, 0)
        self.assertIn("dry run", out)
        self.assertFalse(os.path.exists(copy))
        self.assertEqual(self.recorded, [])

    def test_apply_needs_a_target(self):
        with self.assertRaises(SystemExit):
            self._run("--apply")
        self.assertEqual(self.recorded, [])

    def test_apply_copy_writes_insert_only_script_to_the_copy_folder(self):
        copy = os.path.join(self.d, "copy")
        rc, out = self._run("--apply", "--copy", copy)
        self.assertEqual(rc, 0)
        files = os.listdir(copy)
        self.assertEqual(len(files), 1)
        with open(os.path.join(copy, files[0]), encoding="utf-8") as f:
            text = f.read()
        for line in [l for l in text.splitlines() if l.strip()]:
            self.assertTrue(line.startswith("INSERT INTO "), line)
        self.assertEqual(self.recorded, [])

    def test_copy_inside_the_live_folder_is_refused(self):
        with self.assertRaises(SystemExit):
            self._run("--apply", "--copy", A._C.LIVE_DATA)
        self.assertFalse(os.path.exists(os.path.join(A._C.LIVE_DATA, "addchars-x.sql")))

    def test_copy_and_live_together_are_refused(self):
        with self.assertRaises(SystemExit):
            self._run("--apply", "--copy", os.path.join(self.d, "c"), "--live")
        self.assertEqual(self.recorded, [])

    def test_live_path_only_with_the_flag(self):
        rc, _ = self._run("--apply", "--live")
        self.assertEqual(rc, 0)
        self.assertEqual(len(self.recorded), 1)      # the recorder stands in for ro-db; never run in tests
        for s in self.recorded[0]:
            self.assertTrue(s.startswith("INSERT INTO "))


if __name__ == "__main__":
    unittest.main()
