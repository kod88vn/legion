"""Review gate (Constitution 4.2): a deploy is refused unless every fix has a reviewer report with `verdict: merge`.
Run: python -m unittest discover -s tests/system -v   (from the repo root)"""
import os as _os  # G23: tests never touch the live legion/data folder
import sys as _sys
_sys.path.insert(0, _os.path.join(_os.path.dirname(_os.path.abspath(__file__)), ".."))
import testenv  # noqa: E402
testenv.isolate()  # LEGION_DATA -> a temp folder before any tool module is imported

import os
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "..", "tools", "system"))
import reviewcheck  # noqa: E402

SHA_A = "a" * 40
SHA_B = "b" * 40


def fake_resolve(name):
    return {"fix/a": SHA_A, "fix/b": SHA_B, SHA_A: SHA_A, SHA_B: SHA_B}.get(name)


class TestReviewGate(unittest.TestCase):
    def setUp(self):
        self.dir = tempfile.mkdtemp()

    def write(self, sha, text):
        with open(os.path.join(self.dir, sha + ".md"), "w", encoding="utf-8") as f:
            f.write(text)

    def check(self, fixes, reviewed):
        return reviewcheck.check(fixes, reviewed, reviews_dir=self.dir, resolve_fn=fake_resolve)

    def test_merge_verdict_passes(self):
        self.write(SHA_A, "# review\n\nverdict: merge\n")
        ok, msg = self.check(1, ["fix/a"])
        self.assertTrue(ok)
        self.assertIn("review aaaaaaaaaaaa merge", msg)

    def test_ok_verdict_passes(self):
        self.write(SHA_A, "verdict: OK\n")
        self.assertTrue(self.check(1, ["fix/a"])[0])

    def test_do_not_merge_is_refused(self):
        self.write(SHA_A, "Blocker: race in lock.\n\nverdict: do not merge\n")
        ok, msg = self.check(1, ["fix/a"])
        self.assertFalse(ok)
        self.assertIn("does not say 'verdict: merge'", msg)

    def test_missing_review_is_refused(self):
        ok, msg = self.check(1, ["fix/a"])
        self.assertFalse(ok)
        self.assertIn("no review", msg)

    def test_one_fix_without_review_refuses_the_batch(self):
        self.write(SHA_A, "verdict: merge\n")
        ok, msg = self.check(2, ["fix/a", "fix/b"])
        self.assertFalse(ok)
        self.assertIn(SHA_B[:12], msg)

    def test_count_mismatch_is_refused(self):
        self.write(SHA_A, "verdict: merge\n")
        self.assertFalse(self.check(2, ["fix/a"])[0])
        self.assertFalse(self.check(1, [])[0])

    def test_unknown_commit_is_refused(self):
        ok, msg = self.check(1, ["fix/nope"])
        self.assertFalse(ok)
        self.assertIn("not a commit", msg)

    def test_no_fixes_passes(self):
        self.assertTrue(self.check(0, [])[0])

    def test_verdict_must_be_the_last_line(self):
        self.write(SHA_A, "verdict: merge\n\nFollow-up: the lock needs a test.\n")
        ok, msg = self.check(1, ["fix/a"])
        self.assertFalse(ok)
        self.assertIn("does not say 'verdict: merge'", msg)

    def test_quoted_verdict_in_the_middle_does_not_count(self):
        self.write(SHA_A, "Reviewer quote: verdict: merge\n\nverdict: do not merge\n")
        self.assertFalse(self.check(1, ["fix/a"])[0])

    def test_trailing_blank_lines_are_ignored(self):
        self.write(SHA_A, "Looks fine.\n\nverdict: merge\n\n\n")
        self.assertTrue(self.check(1, ["fix/a"])[0])


if __name__ == "__main__":
    unittest.main()
