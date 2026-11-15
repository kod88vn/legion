"""Tests for tools/system/crier.py (market crier, EC44 / WP7). No live DB, no docker, temp data folder only (G23).
Run: python -m unittest discover -s tests/system -p "test_crier.py" -v   (from the repo root)."""
import os as _os  # G23: tests never touch the live legion/data folder
import sys as _sys
_sys.path.insert(0, _os.path.join(_os.path.dirname(_os.path.abspath(__file__)), ".."))
import testenv  # noqa: E402
testenv.isolate()

import json
import random
import shutil
import sqlite3
import tempfile
import time
import unittest

REPO = _os.path.abspath(_os.path.join(_os.path.dirname(__file__), "..", ".."))
_sys.path.insert(0, _os.path.join(REPO, "tools", "system"))

import crier as C  # noqa: E402

HOUR = 3600
DAY = 24 * HOUR


def local(h, m=0, day=15):
    """Epoch for 2026-11-<day> at local h:m."""
    return time.mktime((2026, 11, day, h, m, 0, 0, 0, -1))


def live_rec(t, item="Poring Coin"):
    return {"t": int(t), "item": item, "mode": "live", "text": "x"}


class Base(unittest.TestCase):
    def setUp(self):
        self.dir = tempfile.mkdtemp(prefix="crier-test-")
        self.log = _os.path.join(self.dir, "market_notices.jsonl")
        self.flag = _os.path.join(self.dir, "tuning.json")
        self.set_flag(True)

    def tearDown(self):
        shutil.rmtree(self.dir, ignore_errors=True)

    def set_flag(self, value):
        with open(self.flag, "w", encoding="utf-8") as f:
            json.dump({"market_broadcast_on": value}, f)

    def records(self):
        if not _os.path.exists(self.log):
            return []
        with open(self.log, encoding="utf-8") as f:
            return [json.loads(x) for x in f if x.strip()]

    def write_history(self, recs):
        with open(self.log, "w", encoding="utf-8") as f:
            for r in recs:
                f.write(json.dumps(r) + "\n")


class BannedWords(unittest.TestCase):
    def test_each_banned_word_is_refused(self):
        for w in ["bot", "bots", "AI", "engine", "Legion", "simulation", "algorithm", "server owner", "script",
                  "market engine", "the system", "GM", "ENGINE"]:
            with self.assertRaises(C.Refused, msg=w):
                C.check_text("[Prontera Merchants] Poring Coin is wanted, said the %s today." % w)

    def test_words_inside_other_words_are_allowed(self):
        for ok in ["Said the merchant.", "Botany is taught in Geffen.", "Maintain your stock.",
                   "Gmail is not in this town.", "The ailing market recovers.", "Remind the guard."]:
            self.assertEqual(C.check_text(ok), ok)


class Length(unittest.TestCase):
    def test_120_passes_121_refused(self):
        self.assertEqual(C.check_text("a" * 120), "a" * 120)
        with self.assertRaises(C.Refused):
            C.check_text("a" * 121)

    def test_long_slots_are_refused(self):
        with self.assertRaises(C.Refused):
            C.render("want", "Abyss Lens of Something Really Long Named Here", "Prontera")  # 43 > 40

    def test_realistic_slots_fit_every_template(self):
        item = "Poring Coin"
        place = "Glastheim Churchyard"
        for kind in C.KINDS:
            for tpl in C.TEMPLATES[kind]:
                self.assertLessEqual(len(tpl.format(item=item, place=place)), C.MAX_CHARS, tpl)

    def test_overlong_render_is_refused_not_truncated(self):
        item = "Poring Coin Of The Very Long Kind Name"    # 38: allowed by the slot rule
        place = "Glastheim Churchyard East Side Area"       # 34: allowed by the slot rule
        refused = 0
        for kind in C.KINDS:
            for tpl in C.TEMPLATES[kind]:
                if len(tpl.format(item=item, place=place)) > C.MAX_CHARS:
                    with self.assertRaises(C.Refused):
                        C.render(kind, item, place, rng=_Fixed(tpl))
                    refused += 1
        self.assertGreater(refused, 0)


class _Fixed:
    """rng stand-in that always picks the given template."""
    def __init__(self, tpl):
        self.tpl = tpl

    def choice(self, seq):
        return self.tpl


class TextRules(unittest.TestCase):
    def test_digits_refused(self):
        with self.assertRaises(C.Refused):
            C.check_text("[Prontera Merchants] 14 Poring Coins wanted.")

    def test_slots_limited_to_letters(self):
        for bad in ["Poring 2", "", "x", "Poring Coin!", None, "Poring\nCoin", "{item}"]:
            with self.assertRaises(C.Refused, msg=repr(bad)):
                C.render("want", bad, "Prontera")
            with self.assertRaises(C.Refused, msg=repr(bad)):
                C.render("want", "Poring Coin", bad)

    def test_unknown_kind_refused(self):
        with self.assertRaises(C.Refused):
            C.render("panic", "Poring Coin", "Prontera")

    def test_every_template_is_clean(self):
        for kind in C.KINDS:
            self.assertTrue(C.TEMPLATES[kind], kind)
            for tpl in C.TEMPLATES[kind]:
                self.assertNotRegex(tpl, r"\d", tpl)           # no digits in the wording
                self.assertIn("{item}", tpl, tpl)              # every template names the item
                self.assertIn("{place}", tpl, tpl)
                C.check_text(tpl.format(item="Poring Coin", place="Prontera"))  # banned words, length

    def test_render_fills_slots_and_passes(self):
        for kind in C.KINDS:
            text = C.render(kind, "Poring Coin", "Prontera", rng=random.Random(5))
            self.assertIn("Poring Coin", text)
            self.assertIn("Prontera", text)
            self.assertNotIn("{", text)

    def test_sql_quote_escapes(self):
        self.assertEqual(C.sql_quote("Wild Rose's"), "'Wild Rose''s'")
        self.assertEqual(C.sql_quote("a\\b"), "'a\\\\b'")


class Cadence(unittest.TestCase):
    def test_hour_window_is_0800_to_2259(self):
        for h, m, ok in [(7, 59, False), (8, 0, True), (22, 59, True), (23, 0, False), (3, 0, False)]:
            allowed = C.cadence_ok(local(h, m), [], "Poring Coin")[0]
            self.assertEqual(allowed, ok, "%02d:%02d" % (h, m))

    def test_three_hours_apart(self):
        prev = local(9, 0)
        hist = [live_rec(prev)]
        self.assertFalse(C.cadence_ok(prev + 3 * HOUR - 60, hist, "Osiris Card")[0])
        self.assertTrue(C.cadence_ok(prev + 3 * HOUR, hist, "Osiris Card")[0])

    def test_daily_cap_of_four(self):
        now = local(20, 0)
        three = [live_rec(now - (3 * k + 1) * HOUR, item="Item%d" % k) for k in (1, 2, 3)]
        self.assertTrue(C.cadence_ok(now, three, "Other Item")[0])
        four = three + [live_rec(now - 12 * HOUR - 60, item="Item4")]
        ok, reason = C.cadence_ok(now, four, "Other Item")
        self.assertFalse(ok)
        self.assertIn("daily cap", reason)

    def test_notices_older_than_a_day_do_not_count(self):
        now = local(20, 0)
        old = [live_rec(now - DAY - 60 * k, item="Old%d" % k) for k in range(5)]
        self.assertTrue(C.cadence_ok(now, old, "Fresh Item")[0])

    def test_same_item_within_twelve_hours_refused(self):
        now = local(20, 0)
        hist = [live_rec(now - 6 * HOUR, item="Poring Coin")]
        ok, reason = C.cadence_ok(now, hist, "Poring Coin")
        self.assertFalse(ok)
        self.assertIn("same item", reason)
        self.assertTrue(C.cadence_ok(now, [live_rec(now - 13 * HOUR, item="Poring Coin")], "Poring Coin")[0])


class Send(Base):
    def test_flag_off_writes_nothing(self):
        self.set_flag(False)
        out = C.send("want", "Poring Coin", "Prontera", now=local(12), log_path=self.log, flag_path=self.flag)
        self.assertEqual(out["status"], "off")
        self.assertFalse(_os.path.exists(self.log))

    def test_flag_missing_or_broken_is_off(self):
        _os.remove(self.flag)
        self.assertFalse(C.flag_on(self.flag))
        with open(self.flag, "w") as f:
            f.write("{not json")
        self.assertFalse(C.flag_on(self.flag))
        with open(self.flag, "w") as f:
            json.dump({"other": True}, f)
        self.assertFalse(C.flag_on(self.flag))

    def test_flag_must_be_boolean_true(self):
        with open(self.flag, "w") as f:
            json.dump({"market_broadcast_on": "true"}, f)
        self.assertFalse(C.flag_on(self.flag))

    def test_dry_run_is_the_default_and_writes_jsonl_only(self):
        called = []
        out = C.send("glut", "Poring Coin", "Prontera", now=local(12), log_path=self.log, flag_path=self.flag,
                     rng=random.Random(3), insert=lambda t: called.append(t))
        self.assertEqual(out["status"], "dry")
        self.assertEqual(called, [])
        recs = self.records()
        self.assertEqual(len(recs), 1)
        self.assertEqual(recs[0]["mode"], "dry")
        self.assertEqual(recs[0]["text"], out["text"])

    def test_live_inserts_once_and_logs(self):
        called = []
        out = C.send("event", "Poring Coin", "Prontera", live=True, now=local(12), log_path=self.log,
                     flag_path=self.flag, rng=random.Random(3), insert=lambda t: called.append(t))
        self.assertEqual(out["status"], "sent")
        self.assertEqual(called, [out["text"]])
        self.assertEqual(self.records()[0]["mode"], "live")

    def test_held_by_cadence_writes_nothing_new(self):
        now = local(12)
        self.write_history([live_rec(now - 600)])
        called = []
        out = C.send("want", "Other Item", "Prontera", live=True, now=now, log_path=self.log,
                     flag_path=self.flag, insert=lambda t: called.append(t))
        self.assertEqual(out["status"], "held")
        self.assertEqual(called, [])
        self.assertEqual(len(self.records()), 1)

    def test_dry_records_do_not_count_toward_cadence(self):
        now = local(12)
        for _ in range(5):
            C.send("want", "Poring Coin", "Prontera", now=now, log_path=self.log, flag_path=self.flag,
                   rng=random.Random(1))
        out = C.send("want", "Osiris Card", "Prontera", live=True, now=now, log_path=self.log,
                     flag_path=self.flag, rng=random.Random(1), insert=lambda t: None)
        self.assertEqual(out["status"], "sent")

    def test_refused_text_writes_nothing(self):
        with self.assertRaises(C.Refused):
            C.send("want", "Poring Coin 5", "Prontera", now=local(12), log_path=self.log, flag_path=self.flag)
        self.assertFalse(_os.path.exists(self.log))


def _read(*parts):
    with open(_os.path.join(REPO, *parts), encoding="utf-8") as f:
        return f.read()


class RepoFiles(unittest.TestCase):
    """Offline checks on the NPC script and the table file (no server is started, nothing is loaded)."""

    def test_table_columns_and_insert_work_on_a_throwaway_db(self):
        text = _read("rathena", "sql", "legion_notice.sql")
        for col in ("`id`", "`text`", "`created_at`", "`sent`"):
            self.assertIn(col, text)
        db = sqlite3.connect(":memory:")  # stand-in for ro-db; the same column names
        db.execute("CREATE TABLE legion_notice (id INTEGER PRIMARY KEY, `text` TEXT, created_at TEXT, sent INTEGER NOT NULL DEFAULT 0)")
        db.execute("INSERT INTO legion_notice (`text`) VALUES (%s)" % C.sql_quote("[Prontera Merchants] Hi."))
        row = db.execute("SELECT id, `text`, sent FROM legion_notice WHERE sent = 0").fetchone()
        self.assertEqual(row, (1, "[Prontera Merchants] Hi.", 0))

    def test_npc_script_is_registered_and_balanced(self):
        conf = _read("rathena", "npc-custom", "scripts_custom.conf")
        self.assertIn("npc: npc/custom-local/legion_crier.txt", conf)
        src = _read("rathena", "npc-custom", "legion_crier.txt")
        code = "\n".join(l for l in src.splitlines() if not l.strip().startswith("//"))
        self.assertEqual(code.count("{"), code.count("}"))
        self.assertEqual(code.count("("), code.count(")"))
        self.assertRegex(src, r"-\tscript\tMarketCrier\t-1,\{")
        self.assertIn("OnTimer60000:", src)
        self.assertIn("query_sql(", src)
        self.assertIn("announce .@msg$, bc_all|bc_blue;", src)
        self.assertIn("UPDATE legion_notice SET sent = 1", src)
        self.assertNotIn("getcharid", src)     # no character takes part
        self.assertNotIn("getcharname", src)


if __name__ == "__main__":
    unittest.main()
