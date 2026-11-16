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
import random
import re
import shutil
import sys
import tempfile
import unittest
from contextlib import redirect_stdout

REPO = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", ".."))
sys.path.insert(0, os.path.join(REPO, "tools", "system"))

import addchars as A  # noqa: E402
import charrows as CR  # noqa: E402
import farmerpick as FP  # noqa: E402
from test_farmerpick import HDR, dump_row, persona  # noqa: E402


def vendor_add(aid, cid, slot, name, plan="blacksmith", account=None):
    return {"account": account or "lg%04d" % (aid - 2100000), "account_id": aid, "char_id": cid, "slot": slot,
            "name": name, "plan": plan, "target_class": CR.plan_first_branch(plan)[2], "class": 0,
            "build": "str-dex"}


def farmers_doc(adds):
    return {"schema": 1, "since": "2026-11-15T10:00:00+0000", "accounts": [a["account"] for a in adds],
            "detail": [], "vendor_adds": adds}


class RangeTests(unittest.TestCase):
    def test_bot_range_only(self):
        for ok in (2100001, 2100004, 2104000, 2104001, 2199999):
            A.check_add_account(ok)
        for bad in (2099999, 2000001, 1000001, 2200000, 0):
            with self.assertRaises(A.Refused, msg=str(bad)):
                A.check_add_account(bad)


class GuardTests(unittest.TestCase):
    def test_refuses_delete_update_select_and_other_verbs(self):
        for s in ["DELETE FROM `char` WHERE char_id=1;",
                  "UPDATE `char` SET zeny=0 WHERE char_id=1;",
                  "REPLACE INTO `char` (char_id) VALUES (1);",
                  "SELECT * FROM `char`;",
                  "DROP TABLE `char`;",
                  "INSERT INTO `char` (char_id) VALUES (1); DELETE FROM `char`;",
                  "INSERT INTO inventory (char_id) VALUES (1); TRUNCATE inventory;",
                  "INSERT INTO t (a) VALUES (1); UPDATE t SET a=2;",
                  "INSERT INTO `char` (char_id) VALUES (1) ON DUPLICATE KEY UPDATE zeny=0;"]:
            with self.assertRaises(A.Refused, msg=s):
                A.guard_sql([s])

    def test_table_allowlist(self):
        for s in ["INSERT INTO login (account_id) VALUES (1);", "INSERT INTO guild_member (char_id) VALUES (1);",
                  "INSERT INTO `char` SELECT * FROM login;", "INSERT INTO inventory SELECT * FROM inventory;"]:
            with self.assertRaises(A.Refused, msg=s):
                A.guard_sql([s])
        ok = ["INSERT INTO `char` (char_id) VALUES (1);", "INSERT INTO inventory (char_id) VALUES (1);",
              "INSERT INTO char_reg_num (char_id) VALUES (1);"]
        self.assertEqual(A.guard_sql(ok), ok)

    def test_quoted_names_are_not_scanned(self):
        ok = "INSERT INTO `char` (char_id,name) VALUES (1,'Update Mart');"
        self.assertEqual(A.guard_sql([ok]), [ok])


class RowTests(unittest.TestCase):
    def test_plan_rows_use_the_plan_first_and_branch(self):
        spec = vendor_add(2100004, 180004, 1, "Hunt4 Shop", plan="blacksmith")
        rows = A.plan_rows(spec, {"home_town": "geffen", "sex": "M"})
        self.assertEqual(len(rows), 3)
        self.assertIn("(180004,'legion_first',0,5),(180004,'legion_branch',0,0)", rows[2])
        alch = A.plan_rows(vendor_add(2100004, 180004, 1, "A", plan="alchemist"), {})
        self.assertIn("'legion_first',0,5),(180004,'legion_branch',0,1)", alch[2])

    def test_hunter_is_never_a_vendor_plan(self):
        with self.assertRaises(A.Refused):
            A.plan_rows(vendor_add(2100004, 180004, 1, "X", plan="hunter"), {})

    def test_statements_are_insert_only_and_in_range(self):
        stmts = A.plan_rows(vendor_add(2104001, 184001, 1, "Hunt Shop"), {"home_town": "geffen"})
        for s in stmts:
            self.assertTrue(s.startswith("INSERT INTO "))
            self.assertFalse(re.search(r"\b(DELETE|UPDATE|REPLACE|DROP|ALTER|TRUNCATE)\b", s))
        self.assertIn("VALUES (184001,2104001,1,", stmts[0])

    def test_sex_is_checked_not_pasted(self):
        rows = A.plan_rows(vendor_add(2100004, 180004, 1, "A"), {"sex": "M'); DROP TABLE x; --"})
        self.assertIn(",'M',0);", rows[0])          # unknown sex falls back to M, never pasted
        self.assertNotIn("DROP", rows[0])


class PlanTests(unittest.TestCase):
    def setUp(self):
        # real bot-range accounts: 2100001 has a vendor, 2100002 is guild-less without one, 2100004 likewise
        self.personas = [persona(1, vendor=True), persona(2), persona(4)]
        self.by_acc = {p["account_id"]: p for p in self.personas}
        self.rows = [dump_row(2100001, 210001, 0, "Hunt1"), dump_row(2100002, 210002, 0, "Hunt2"),
                     dump_row(2100004, 210004, 0, "Hunt4")]

    def test_accepts_a_clean_add(self):
        stmts, rep, upd = A.plan(farmers_doc([vendor_add(2100004, 180004, 1, "Hunt4 Shop")]), self.rows, self.by_acc)
        self.assertEqual(rep["ok"], ["lg0004"])
        self.assertEqual(rep["refused"], [])
        self.assertEqual(len(stmts), 3)
        self.assertEqual(upd[0][0], 2100004)
        self.assertEqual(upd[0][1]["role"], "vendor")
        self.assertEqual(upd[0][1]["plan"], "blacksmith")

    def test_refusals(self):
        cases = {
            "out of range": vendor_add(2099999, 180004, 1, "X Shop"),
            "human account": vendor_add(2000001, 180004, 1, "X Shop"),
            "already has vendor": vendor_add(2100001, 184001, 1, "Zed Shop"),
            "slot taken": vendor_add(2100004, 180004, 0, "Hunt4 Shop"),
            "id taken": vendor_add(2100004, 210002, 1, "Hunt4 Shop"),
            "name taken": vendor_add(2100004, 180004, 1, "Hunt2"),
            "hunter plan": vendor_add(2100004, 180004, 1, "Hunt4 Shop", plan="hunter"),
        }
        for label, add in cases.items():
            stmts, rep, upd = A.plan(farmers_doc([add]), self.rows, self.by_acc)
            self.assertEqual(rep["ok"], [], label)
            self.assertEqual(len(rep["refused"]), 1, label)
            self.assertEqual(stmts, [], label)
            self.assertEqual(upd, [], label)

    def test_guild_missing_login_and_pinned_are_refused(self):
        add = vendor_add(2100004, 180004, 1, "Hunt4 Shop")
        rows = [r for r in self.rows if r["account_id"] != 2100004]
        _, rep, _ = A.plan(farmers_doc([add]), rows, self.by_acc)
        self.assertEqual(rep["refused"][0][1], "no login row in the dump")
        rows = self.rows + [dump_row(2100004, 214002, 2, "G", guild=5)]
        _, rep, _ = A.plan(farmers_doc([add]), rows, self.by_acc)
        self.assertEqual(rep["refused"][0][1], "account is in a guild")
        _, rep, _ = A.plan(farmers_doc([add]), self.rows, self.by_acc, pinned_names=["hunt4"])
        self.assertEqual(rep["refused"][0][1], "pinned human party member")

    def test_no_persona_is_refused(self):
        add = vendor_add(2100003, 180003, 1, "Hunt3 Shop")
        rows = self.rows + [dump_row(2100003, 210003, 0, "Hunt3")]
        _, rep, _ = A.plan(farmers_doc([add]), rows, self.by_acc)
        self.assertIn("no persona", rep["refused"][0][1])

    def test_apply_personas_appends_the_vendor(self):
        stmts, rep, upd = A.plan(farmers_doc([vendor_add(2100004, 180004, 1, "Hunt4 Shop")]), self.rows, self.by_acc)
        ps = A.apply_personas(self.personas, upd)
        p4 = [p for p in ps if p["account_id"] == 2100004][0]
        self.assertEqual([c["role"] for c in p4["characters"]], ["main", "vendor"])


class CliTests(unittest.TestCase):
    def setUp(self):
        self.d = tempfile.mkdtemp(prefix="addchars-")
        self.farmers = os.path.join(self.d, "farmers.json")
        self.dump = os.path.join(self.d, "chars.tsv")
        self.personas = os.path.join(self.d, "personas.json")
        self.pinned = os.path.join(self.d, "pinned.json")
        with open(self.farmers, "w", encoding="utf-8") as f:
            json.dump(farmers_doc([vendor_add(2100004, 180004, 1, "Hunt4 Shop")]), f)
        with open(self.dump, "w", encoding="utf-8") as f:
            f.write(HDR + "2100004\t210004\t0\tHunt4\t0\t0\n")
        with open(self.personas, "w", encoding="utf-8") as f:
            json.dump([persona(4)], f)
        with open(self.pinned, "w", encoding="utf-8") as f:
            json.dump([{"members": ["Someone Else"]}], f)
        self.recorded = []
        self._orig_live = A.run_live
        A.run_live = lambda stmts: self.recorded.append(list(stmts)) or len(stmts)  # never reaches ro-db

    def tearDown(self):
        A.run_live = self._orig_live
        shutil.rmtree(self.d, ignore_errors=True)

    def _run(self, *extra, pinned=True):
        args = ["--farmers", self.farmers, "--dump", self.dump, "--personas", self.personas]
        if pinned:
            args += ["--pinned", self.pinned]
        buf = io.StringIO()
        with redirect_stdout(buf):
            rc = A.main(args + list(extra))
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

    def test_copy_writes_insert_only_script_and_a_personas_copy(self):
        copy = os.path.join(self.d, "copy")
        rc, _ = self._run("--apply", "--copy", copy)
        self.assertEqual(rc, 0)
        sqls = [f for f in os.listdir(copy) if f.endswith(".sql")]
        self.assertEqual(len(sqls), 1)
        with open(os.path.join(copy, sqls[0]), encoding="utf-8") as f:
            lines = [l for l in f.read().splitlines() if l.strip()]
        self.assertEqual(len(lines), 3)
        for line in lines:
            self.assertTrue(line.startswith("INSERT INTO "), line)
        with open(os.path.join(copy, "personas.json"), encoding="utf-8") as f:
            ps = json.load(f)
        self.assertEqual(ps[0]["characters"][-1]["role"], "vendor")
        self.assertEqual(self.recorded, [])
        with open(self.personas, encoding="utf-8") as f:
            self.assertEqual(len(json.load(f)[0]["characters"]), 1)  # the source file is untouched

    def test_copy_inside_the_live_folder_is_refused(self):
        with self.assertRaises(SystemExit):
            self._run("--apply", "--copy", A._C.LIVE_DATA)
        self.assertFalse(os.path.exists(os.path.join(A._C.LIVE_DATA, "personas.json.tmp")))

    def test_copy_and_live_together_are_refused(self):
        with self.assertRaises(SystemExit):
            self._run("--apply", "--copy", os.path.join(self.d, "c"), "--live")
        self.assertEqual(self.recorded, [])

    def test_live_needs_pinned(self):
        with self.assertRaises(SystemExit):
            self._run("--apply", "--live", pinned=False)
        self.assertEqual(self.recorded, [])

    def test_live_path_runs_sql_then_updates_personas_with_backup(self):
        rc, _ = self._run("--apply", "--live")          # run_live is the recorder; personas is the temp copy
        self.assertEqual(rc, 0)
        self.assertEqual(len(self.recorded), 1)
        for s in self.recorded[0]:
            self.assertTrue(s.startswith("INSERT INTO "))
        with open(self.personas, encoding="utf-8") as f:
            self.assertEqual(json.load(f)[0]["characters"][-1]["role"], "vendor")
        baks = [f for f in os.listdir(self.d) if f.startswith("personas.json.bak-addchars-")]
        self.assertEqual(len(baks), 1)


if __name__ == "__main__":
    unittest.main()
