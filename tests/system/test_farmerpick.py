"""Tests for tools/system/farmerpick.py (phase-1 farmer roster, design section 2.1). Fixture personas and a fixture
character dump only; a temp data folder (rule G23). Run: python -m unittest discover -s tests/system -v"""
import os as _os
import sys as _sys
_sys.path.insert(0, _os.path.join(_os.path.dirname(_os.path.abspath(__file__)), ".."))
import testenv  # noqa: E402
testenv.isolate()  # LEGION_DATA -> a temp folder before any tool module is imported

import json
import os
import shutil
import sys
import tempfile
import unittest

REPO = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", ".."))
sys.path.insert(0, os.path.join(REPO, "tools", "system"))

import farmerpick as F  # noqa: E402

HDR = "account_id\tchar_id\tchar_num\tname\tclass\tguild_id\n"


def persona(acc_idx, main_plan="hunter", guild="", vendor=False, extra=(), name=None, aid=None):
    aid = aid if aid is not None else 2100000 + acc_idx
    chars = [{"char_id": 210000 + acc_idx, "slot": 0, "name": name or "Hunt%d" % acc_idx, "role": "main",
              "class": 0, "class_name": "Novice", "build": "dex-agi", "base_level": 1, "job_level": 1,
              "plan": main_plan, "target_class": 11}]
    if vendor:
        chars.append({"char_id": 180000 + acc_idx, "slot": 1, "name": "Hunt%d Shop" % acc_idx, "role": "vendor",
                      "class": 0, "class_name": "Novice", "build": "str-dex", "base_level": 1, "job_level": 1,
                      "plan": "blacksmith", "target_class": 10})
    chars += list(extra)
    return {"index": acc_idx, "account": "lg%04d" % (aid - 2100000), "account_id": aid, "sex": "M",
            "guild": guild, "home_town": "geffen", "characters": chars, "name": "Hunt%d" % acc_idx}


def dump_row(aid, cid, num, name, guild=0):
    return {"account_id": aid, "char_id": cid, "char_num": num, "name": name, "class": 0, "guild_id": guild}


class SelectTests(unittest.TestCase):
    def test_pairs_first_then_vendor_adds_and_skips(self):
        ps = [
            persona(1, vendor=True),                               # A: complete pair
            persona(2),                                            # B: needs a vendor, slot 1 free
            persona(3, guild="guild02"),                           # C: in a guild (persona)
            persona(4, main_plan="knight"),                        # D: not hunter line
            persona(5, name="Pinned Hunter"),                      # E: pinned human party member
            persona(6, aid=2200001),                               # F: outside the bot range
        ]
        sel = F.select(ps, None, pinned=["pinned hunter"], target=150)
        self.assertEqual([f["account"] for f in sel["accounts"]], ["lg0001"])
        self.assertEqual([f["account"] for f in sel["vendor_adds"]], ["lg0002"])
        v = sel["vendor_adds"][0]["vendor"]
        self.assertEqual(v["slot"], 1)
        self.assertEqual(v["plan"], "blacksmith")
        self.assertEqual(v["char_id"], 180002)
        self.assertEqual(sel["skipped"]["in a guild (persona)"], 1)
        self.assertEqual(sel["skipped"]["main not hunter line"], 1)
        self.assertEqual(sel["skipped"]["pinned human party"], 1)
        self.assertEqual(sel["skipped"]["outside bot range"], 1)

    def test_all_four_hunter_plans_count(self):
        ps = [persona(i, main_plan=p, vendor=True) for i, p in enumerate(F.HUNTER_PLANS, start=10)]
        sel = F.select(ps, None)
        self.assertEqual(len(sel["accounts"]), 4)

    def test_target_cuts_the_roster_and_the_adds(self):
        ps = [persona(1, vendor=True), persona(2, vendor=True), persona(3), persona(4)]
        sel = F.select(ps, None, target=2)
        self.assertEqual(len(sel["accounts"]), 2)
        self.assertEqual(sel["vendor_adds"], [])
        self.assertEqual(sel["candidates"], 4)

    def test_dump_guild_and_missing_login_are_skipped(self):
        ps = [persona(1), persona(2), persona(3)]
        dump = [dump_row(2100001, 210001, 0, "Hunt1", guild=7), dump_row(2100002, 210002, 0, "Hunt2")]
        sel = F.select(ps, dump)
        self.assertEqual(sel["accounts"], [])
        self.assertEqual([f["account"] for f in sel["vendor_adds"]], ["lg0002"])
        self.assertEqual(sel["skipped"]["in a guild (dump)"], 1)
        self.assertEqual(sel["skipped"]["account not in dump"], 1)

    def test_vendor_spec_avoids_used_slots_names_and_ids(self):
        ps = [persona(2)]
        dump = [dump_row(2100002, 210002, 0, "Hunt2"), dump_row(2100002, 999901, 1, "Hunt2 Shop"),
                dump_row(2100009, 180002, 5, "Other")]
        sel = F.select(ps, dump)
        v = sel["vendor_adds"][0]["vendor"]
        self.assertEqual(v["slot"], 2)                  # slot 1 is taken in the dump
        self.assertEqual(v["char_id"], 220002)          # 180002 is taken by another row
        self.assertNotEqual(v["name"].lower(), "hunt2 shop")

    def test_dump_loader_requires_header_and_reads_rows(self):
        d = tempfile.mkdtemp(prefix="farmerpick-")
        try:
            good = os.path.join(d, "good.tsv")
            with open(good, "w", encoding="utf-8") as f:
                f.write(HDR + "2100001\t210001\t0\tHunt1\t0\t0\n")
            rows = F.load_dump(good)
            self.assertEqual(rows[0]["char_id"], 210001)
            bad = os.path.join(d, "bad.tsv")
            with open(bad, "w", encoding="utf-8") as f:
                f.write("account_id\tname\n1\tx\n")
            with self.assertRaises(SystemExit):
                F.load_dump(bad)
        finally:
            shutil.rmtree(d, ignore_errors=True)


class SchemaTests(unittest.TestCase):
    def _doc(self, **kw):
        sel = F.select([persona(1, vendor=True), persona(2)], None)
        doc = F.build_doc(sel, since="2026-11-15T10:00:00+0000")
        doc.update(kw)
        return doc

    def test_built_doc_is_valid_and_keeps_design_keys(self):
        doc = self._doc()
        self.assertEqual(F.validate_farmers(doc), [])
        self.assertEqual(doc["accounts"], ["lg0001", "lg0002"])
        self.assertIn("since", doc)
        self.assertEqual(doc["vendor_adds"][0]["account"], "lg0002")

    def test_rejects_out_of_range_and_bad_shape(self):
        self.assertTrue(F.validate_farmers(self._doc(schema=2)))
        self.assertTrue(F.validate_farmers(self._doc(accounts=["lg0001", "lg0001"])))
        self.assertTrue(F.validate_farmers(self._doc(accounts="lg0001")))
        bad = self._doc()
        bad["detail"][0]["account_id"] = 2200001
        self.assertTrue(any("bot range" in e for e in F.validate_farmers(bad)))
        bad = self._doc()
        bad["vendor_adds"][0]["slot"] = 9
        self.assertTrue(any("slot" in e for e in F.validate_farmers(bad)))
        self.assertTrue(F.validate_farmers([]))
        bad = self._doc()
        bad["vendor_adds"][0]["account_id"] = 2099999
        self.assertTrue(any("bot range" in e for e in F.validate_farmers(bad)))

    def test_committed_example_validates(self):
        path = os.path.join(REPO, "legion", "data", "farmers.example.json")
        with open(path, encoding="utf-8") as f:
            doc = json.load(f)
        self.assertEqual(F.validate_farmers(doc), [])


class PinnedTests(unittest.TestCase):
    def test_pinned_loader_accepts_names_or_party_shape(self):
        d = tempfile.mkdtemp(prefix="pinned-")
        try:
            a = os.path.join(d, "a.json")
            with open(a, "w", encoding="utf-8") as f:
                json.dump(["Alpha", "Beta"], f)
            b = os.path.join(d, "b.json")
            with open(b, "w", encoding="utf-8") as f:
                json.dump([{"members": ["Gamma"]}], f)
            self.assertEqual(F.load_pinned(a), {"alpha", "beta"})
            self.assertEqual(F.load_pinned(b), {"gamma"})
        finally:
            shutil.rmtree(d, ignore_errors=True)

    def test_pinned_is_required(self):
        with self.assertRaises(SystemExit):
            F.main(["--dump", "x.tsv"])


class CliTests(unittest.TestCase):
    def test_dry_run_is_the_default_and_writes_nothing(self):
        d = tempfile.mkdtemp(prefix="farmerpick-cli-")
        try:
            pp = os.path.join(d, "personas.json")
            with open(pp, "w", encoding="utf-8") as f:
                json.dump([persona(1, vendor=True), persona(2)], f)
            dump = os.path.join(d, "chars.tsv")
            with open(dump, "w", encoding="utf-8") as f:
                f.write(HDR)
            out = os.path.join(d, "farmers.json")
            pin = os.path.join(d, "pinned.json")
            with open(pin, "w", encoding="utf-8") as f:
                json.dump([{"members": ["Pinned Hunter"]}], f)
            self.assertEqual(F.main(["--personas", pp, "--dump", dump, "--out", out, "--pinned", pin]), 0)
            self.assertFalse(os.path.exists(out))
            F.main(["--personas", pp, "--dump", dump, "--out", out, "--pinned", pin, "--write"])
            self.assertTrue(os.path.exists(out))
            with open(out, encoding="utf-8") as f:
                self.assertEqual(F.validate_farmers(json.load(f)), [])
        finally:
            shutil.rmtree(d, ignore_errors=True)


if __name__ == "__main__":
    unittest.main()
