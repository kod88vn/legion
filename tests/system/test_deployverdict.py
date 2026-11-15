"""Deploy verdict (Constitution 3.6, 3.9): the keep/revert of the last deploy's fixes is written to legion/data/
deploys.jsonl by the deploy flow, and verdict lines are never read as deploys. No server, a fake clock, temp data.
Run: python -m unittest discover -s tests/system -v   (from the repo root)"""
import os as _os  # G23: tests never touch the live legion/data folder
import sys as _sys
_sys.path.insert(0, _os.path.join(_os.path.dirname(_os.path.abspath(__file__)), ".."))
import testenv  # noqa: E402
testenv.isolate()  # LEGION_DATA -> a temp folder before any tool module is imported

import os
import shutil
import sys
import tempfile
import unittest

REPO = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", ".."))
sys.path.insert(0, os.path.join(REPO, "tools", "system"))

import common as C  # noqa: E402
import deploylog as D  # noqa: E402
import league as L  # noqa: E402

T0 = 1_800_000_000.0


class DeployVerdictTests(unittest.TestCase):
    def setUp(self):
        self.d = tempfile.mkdtemp()
        os.environ["LEGION_DATA"] = self.d
        self.clock(T0)

    def tearDown(self):
        shutil.rmtree(self.d, ignore_errors=True)
        testenv.isolate()
        os.environ.pop("LEGION_FAKE_NOW", None)

    def clock(self, t):
        os.environ["LEGION_FAKE_NOW"] = str(t)

    def meter_rows(self):
        # hourly cohort rows: the 3 hours before T0 kpm 1.0, the 3 after kpm 1.2 (+20%: KEEP)
        for h in range(-3, 3):
            C.append_jsonl(C.dpath("meter.jsonl"), {"t": T0 + h * 3600 + 1800, "cohort": "archer 21-40 solo", "n": 40,
                                                    "kpm": {"mean": 1.2 if h >= 0 else 1.0}, "zph": {"mean": 9000}})

    def verdicts(self):
        return [r for r in C.read_jsonl(C.dpath("deploys.jsonl")) if r.get("kind") == "verdict"]

    def test_previous_deploy_verdict_written_by_next_record(self):
        self.meter_rows()
        self.clock(T0)
        D.record("img1", ["legion"], None, [D.parse_fix("archer ledge|kpm|archer")])
        self.assertEqual(self.verdicts(), [])
        self.clock(T0 + 3 * 3600 + 100)
        D.record("img2", ["legion"], None, [D.parse_fix("next|kpm|archer")])
        vs = self.verdicts()
        self.assertEqual(len(vs), 1)
        self.assertEqual(vs[0]["tag"], "img1")
        self.assertEqual(vs[0]["deploy_epoch"], T0)
        self.assertFalse(vs[0]["partial"])
        self.assertTrue(vs[0]["verdict"].startswith("KEEP 1"), vs[0]["verdict"])
        self.assertEqual(vs[0]["items"][0]["verdict"][:4], "KEEP")

    def test_core_step_on_same_tag_writes_no_verdict(self):
        self.meter_rows()
        self.clock(T0)
        D.record("img1", ["legion"], None, [D.parse_fix("archer ledge|kpm|archer")])
        self.clock(T0 + 3 * 3600 + 100)
        D.record("img2", ["legion"], None, [D.parse_fix("x|kpm|archer")])
        D.record("img2", ["legion-core"], None, [])
        self.assertEqual(len(self.verdicts()), 1)

    def test_verdict_lines_are_not_deploys(self):
        self.meter_rows()
        self.clock(T0)
        D.record("img1", ["legion"], None, [D.parse_fix("archer ledge|kpm|archer")])
        self.clock(T0 + 3 * 3600 + 100)
        D.record("img2", ["legion"], None, [D.parse_fix("x|kpm|archer")])
        self.assertEqual(D.last_record()["tag"], "img2")
        self.assertEqual(L.last_deploy()["tag"], "img2")
        self.assertEqual(len(D.deploy_records()), 2)

    def test_partial_then_final_verdict(self):
        self.meter_rows()
        self.clock(T0)
        D.record("img1", ["legion"], None, [D.parse_fix("archer ledge|kpm|archer")])
        self.clock(T0 + 3600)
        rec = L.record_verdict()
        self.assertTrue(rec["partial"])
        self.assertIsNone(L.record_verdict())            # a partial one exists and the window is still open
        self.clock(T0 + 3 * 3600 + 5)
        rec = L.record_verdict()
        self.assertFalse(rec["partial"])
        self.assertIsNone(L.record_verdict())            # final: written once
        self.assertEqual(len(self.verdicts()), 2)

    def test_puppet_and_bugq_ignore_a_newest_verdict(self):
        import bugq
        import io
        import contextlib
        sys.path.insert(0, os.path.join(REPO, "tools", "puppet"))
        import dispatch
        self.meter_rows()
        self.clock(T0)
        D.record("img1", ["legion"], None, [D.parse_fix("archer ledge|kpm|archer")])
        self.clock(T0 + 3 * 3600 + 100)
        out = io.StringIO()
        with contextlib.redirect_stdout(out):
            D.record("img2", ["legion"], None, [D.parse_fix("x|kpm|archer")])
        self.assertEqual(len(self.verdicts()), 1)
        # a verdict written later than the last deploy is the newest line of the file: it must not be read as a deploy
        C.append_jsonl(C.dpath("deploys.jsonl"), {"kind": "verdict", "t": C.iso(T0 + 5 * 3600), "tag": "img1",
                                                  "deploy_epoch": T0, "verdict": "KEEP 1"})
        self.assertEqual(dispatch.last_deploy(self.d), (T0 + 3 * 3600 + 100, "img2"))
        self.assertEqual(bugq.last_deploy_epoch(), T0 + 3 * 3600 + 100)
        # the verdict line stays readable and its text is not doubled on the console
        self.assertEqual(out.getvalue().count("partial window"), 0)
        self.assertNotIn("(partial window) (partial window)", out.getvalue())

    def test_verdict_only_file_is_no_deploy(self):
        import bugq
        sys.path.insert(0, os.path.join(REPO, "tools", "puppet"))
        import dispatch
        C.append_jsonl(C.dpath("deploys.jsonl"), {"kind": "verdict", "t": C.iso(T0), "tag": "img1",
                                                  "deploy_epoch": T0, "verdict": "KEEP 1"})
        self.assertEqual(dispatch.last_deploy(self.d), (None, None))
        self.assertIsNone(bugq.last_deploy_epoch())


if __name__ == "__main__":
    unittest.main()
