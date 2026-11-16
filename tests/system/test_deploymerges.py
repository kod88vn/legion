"""The deploy merge gate (tools/deploy/deploy.py: merge_gate, ship_base, Deploy.gate_merges, Constitution 4.2).
Every case runs on a temp git repo with real git and a temp data folder (G23): no docker, no live data.
Run: python -m unittest discover -s tests/system -p test_deploymerges.py -v   (from the repo root)"""
import importlib.util
import json
import os
import shutil
import subprocess
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), ".."))
import testenv  # noqa: E402
testenv.isolate()  # LEGION_DATA -> a temp folder before any tool module is imported

REPO = os.path.normpath(os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", ".."))
sys.path.insert(0, os.path.join(REPO, "tools", "system"))
import common as C  # noqa: E402

_spec = importlib.util.spec_from_file_location("deploy_batch", os.path.join(REPO, "tools", "deploy", "deploy.py"))
DP = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(DP)

GIT = ["git", "-c", "user.name=test", "-c", "user.email=test@example.invalid", "-c", "commit.gpgsign=false"]


def git(repo, *args):
    p = subprocess.run(GIT + ["-C", repo] + list(args), capture_output=True, text=True)
    if p.returncode != 0:
        raise RuntimeError("git %s failed: %s" % (" ".join(args), p.stderr))
    return p.stdout.strip()


def head(repo):
    return git(repo, "rev-parse", "HEAD")


def write_review(repo, sha, text):
    d = os.path.join(repo, "docs", "reports", "reviews")
    os.makedirs(d, exist_ok=True)
    with open(os.path.join(d, sha + ".md"), "w", encoding="utf-8") as f:
        f.write(text)
    git(repo, "add", "docs")
    git(repo, "commit", "-q", "-m", "review " + sha[:8])


def feature(repo, name, filename):
    """Branch `name` from main with one commit; main is left checked out."""
    git(repo, "checkout", "-q", "-b", name, "main")
    with open(os.path.join(repo, filename), "w", encoding="utf-8") as f:
        f.write(name + "\n")
    git(repo, "add", filename)
    git(repo, "commit", "-q", "-m", "work on " + name)
    git(repo, "checkout", "-q", "main")


def merge_feature(repo, name, review=None):
    """Merge `name` into main (--no-ff); then, if review is given, write the review of its second parent. Returns the
    second parent sha (the feature tip)."""
    tip = git(repo, "rev-parse", name)
    git(repo, "merge", "-q", "--no-ff", "-m", "Merge branch '%s' into main" % name, name)
    if review is not None:
        write_review(repo, tip, review)
    return tip


class MergeGateRepo(unittest.TestCase):
    def setUp(self):
        self.saved = os.environ.get("LEGION_DATA")
        self.tmp = tempfile.mkdtemp(prefix="deploymerges-")
        self.data = os.path.join(self.tmp, "data")
        os.makedirs(self.data)
        os.environ["LEGION_DATA"] = self.data  # G23: never the live legion/data
        self.repo = os.path.join(self.tmp, "repo")
        os.makedirs(self.repo)
        git(self.repo, "init", "-q")
        git(self.repo, "symbolic-ref", "HEAD", "refs/heads/main")
        with open(os.path.join(self.repo, "base.txt"), "w", encoding="utf-8") as f:
            f.write("base\n")
        git(self.repo, "add", "base.txt")
        git(self.repo, "commit", "-q", "-m", "initial")

    def tearDown(self):
        if self.saved is None:
            os.environ.pop("LEGION_DATA", None)
        else:
            os.environ["LEGION_DATA"] = self.saved
        shutil.rmtree(self.tmp, ignore_errors=True)

    def gate(self, base=None):
        return DP.merge_gate(self.repo, head(self.repo), base)

    def only_row(self, rows):
        self.assertEqual(len(rows), 1, rows)
        return rows[0]


class TestMergeVerdict(unittest.TestCase):
    def test_last_line_only(self):
        self.assertEqual(DP.merge_verdict("text\n\nverdict: merge\n"), "merge")
        self.assertEqual(DP.merge_verdict("verdict: do not merge"), "do not merge")
        self.assertIsNone(DP.merge_verdict("verdict: merge\nmore text after it"))
        self.assertIsNone(DP.merge_verdict("verdict: merge (later)"))
        self.assertIsNone(DP.merge_verdict(""))


class TestMergeGate(MergeGateRepo):
    def test_reviewed_merge_ok(self):
        feature(self.repo, "fix/a", "a.txt")
        merge_feature(self.repo, "fix/a", review="# review\nverdict: merge\n")
        rows, offenders = self.gate()
        row = self.only_row(rows)
        self.assertEqual((row["verdict"], offenders), ("merge", []))
        self.assertTrue(row["review"].endswith(".md"))

    def test_missing_review_refused(self):
        feature(self.repo, "fix/b", "b.txt")
        sha = merge_feature(self.repo, "fix/b", review=None)
        rows, offenders = self.gate()
        self.assertEqual(len(offenders), 1)
        self.assertEqual(offenders[0]["verdict"], "none")
        self.assertIn("no review file docs/reports/reviews/%s.md" % sha, offenders[0]["why"])
        self.assertEqual(offenders[0]["subject"], "Merge branch 'fix/b' into main")

    def test_do_not_merge_refused_with_path(self):
        feature(self.repo, "fix/c", "c.txt")
        sha = merge_feature(self.repo, "fix/c", review="defect found\nverdict: do not merge\n")
        rows, offenders = self.gate()
        self.assertEqual(offenders[0]["verdict"], "do not merge")
        self.assertIn("docs/reports/reviews/%s.md" % sha, offenders[0]["why"])

    def test_verdict_not_on_last_line_refused(self):
        feature(self.repo, "fix/d", "d.txt")
        merge_feature(self.repo, "fix/d", review="verdict: merge\nbut more text follows\n")
        rows, offenders = self.gate()
        self.assertEqual([o["verdict"] for o in offenders], ["none"])

    def test_sync_merge_of_main_exempt(self):
        feature(self.repo, "fix/e", "e.txt")
        # main moves on, then the feature branch syncs from main: "Merge branch 'main' into fix/e" (exempt)
        with open(os.path.join(self.repo, "m.txt"), "w", encoding="utf-8") as f:
            f.write("main moved\n")
        git(self.repo, "add", "m.txt")
        git(self.repo, "commit", "-q", "-m", "main moves on")
        git(self.repo, "checkout", "-q", "fix/e")
        git(self.repo, "merge", "-q", "--no-ff", "-m", "Merge branch 'main' into fix/e", "main")
        git(self.repo, "checkout", "-q", "main")
        sync_sha = git(self.repo, "rev-parse", "fix/e")
        merge_feature(self.repo, "fix/e", review="verdict: merge\n")
        rows, offenders = self.gate()
        verdicts = {r["subject"]: r["verdict"] for r in rows}
        self.assertEqual(verdicts["Merge branch 'main' into fix/e"], "sync", sync_sha)
        self.assertEqual(verdicts["Merge branch 'fix/e' into main"], "merge")
        self.assertEqual(offenders, [])

    def test_sync_merge_subject_forms_exempt(self):
        # every accepted sync subject, each on its own feature branch merged after main moved on
        forms = ["Merge branch 'main' into %s", "Merge main into %s", "Merge branch main into %s",
                 "Merge branch 'main' of origin into %s"]
        for i, form in enumerate(forms):
            with self.subTest(form=form):
                br = "fix/sync%d" % i
                feature(self.repo, br, "%s.txt" % br.replace("/", "_"))
                with open(os.path.join(self.repo, "m%d.txt" % i), "w", encoding="utf-8") as f:
                    f.write("main moved %d\n" % i)
                git(self.repo, "add", "m%d.txt" % i)
                git(self.repo, "commit", "-q", "-m", "main moves %d" % i)
                git(self.repo, "checkout", "-q", br)
                msg = form % br
                git(self.repo, "merge", "-q", "--no-ff", "-m", msg, "main")
                git(self.repo, "checkout", "-q", "main")
                merge_feature(self.repo, br, review="verdict: merge\n")
                rows, offenders = self.gate()
                verdicts = {r["subject"]: r["verdict"] for r in rows}
                self.assertEqual(verdicts[msg], "sync", msg)
                self.assertEqual(offenders, [], msg)

    def test_sync_subject_without_main_ancestry_not_exempt(self):
        # same subject, but its second parent is not on main: it is not exempt and needs a review
        git(self.repo, "checkout", "-q", "-b", "other", "main")
        with open(os.path.join(self.repo, "o.txt"), "w", encoding="utf-8") as f:
            f.write("o\n")
        git(self.repo, "add", "o.txt")
        git(self.repo, "commit", "-q", "-m", "other work")
        git(self.repo, "checkout", "-q", "main")
        git(self.repo, "merge", "-q", "--no-ff", "-m", "Merge branch 'main' into other", "other")
        rows, offenders = self.gate()
        self.assertEqual(self.only_row(rows)["verdict"], "none")
        self.assertEqual(len(offenders), 1)

    def test_base_limits_the_range(self):
        feature(self.repo, "fix/old", "old.txt")
        merge_feature(self.repo, "fix/old", review=None)  # unreviewed, but before the base
        base = head(self.repo)
        feature(self.repo, "fix/new", "new.txt")
        merge_feature(self.repo, "fix/new", review="verdict: merge\n")
        rows, offenders = self.gate(base=base)
        self.assertEqual([r["subject"] for r in rows], ["Merge branch 'fix/new' into main"])
        self.assertEqual(offenders, [])
        rows_all, offenders_all = self.gate()
        self.assertEqual(len(offenders_all), 1)  # the whole history: the old merge is refused

    def test_bad_parents_refused(self):
        # an octopus merge: three parents cannot name one review file
        for n in ("oa", "ob"):
            git(self.repo, "checkout", "-q", "-b", n, "main")
            with open(os.path.join(self.repo, n + ".txt"), "w", encoding="utf-8") as f:
                f.write(n + "\n")
            git(self.repo, "add", n + ".txt")
            git(self.repo, "commit", "-q", "-m", n)
            git(self.repo, "checkout", "-q", "main")
        git(self.repo, "merge", "-q", "--no-ff", "-m", "octopus", "oa", "ob")
        rows, offenders = self.gate()
        self.assertEqual(offenders[0]["verdict"], "bad parents")

    def test_git_failure_fails_closed(self):
        with self.assertRaises(DP.Refused) as cm:
            DP.merge_gate(self.repo, "0" * 40, None)
        self.assertEqual(cm.exception.code, DP.SOURCE)


class TestShipBase(MergeGateRepo):
    def write_deploys(self, *records):
        with open(os.path.join(self.data, "deploys.jsonl"), "w", encoding="utf-8") as f:
            for r in records:
                f.write(json.dumps(r) + "\n")

    def test_no_deploy_recorded_yet_checks_whole_history(self):
        base, note = DP.ship_base(self.repo, head(self.repo), DP.execute)
        self.assertIsNone(base)
        self.assertIn("no deploy recorded", note)

    def test_recorded_sha_is_the_base(self):
        feature(self.repo, "fix/f", "f.txt")
        merge_feature(self.repo, "fix/f", review=None)
        built = head(self.repo)
        self.write_deploys({"t": "2026-11-15T08:00:00", "epoch": 1794749200, "tag": "ro-legion:deploy-x",
                            "services": ["legion"], "critical": False, "reason": "", "fixes": [], "sha": built})
        base, note = DP.ship_base(self.repo, head(self.repo), DP.execute)
        self.assertEqual(base, built)
        rows, offenders = self.gate(base=base)
        self.assertEqual(rows, [])  # everything up to the recorded build has shipped already

    def test_record_without_sha_uses_commit_before_it(self):
        feature(self.repo, "fix/g", "g.txt")
        merge_feature(self.repo, "fix/g", review=None)
        # a deploy far in the future: the commit before it is HEAD itself, so nothing is left to ship
        self.write_deploys({"t": "2100-01-01T00:00:00", "epoch": 4102444800, "tag": "ro-legion:deploy-y",
                            "services": ["legion"], "critical": False, "reason": "", "fixes": []})
        base, note = DP.ship_base(self.repo, head(self.repo), DP.execute)
        self.assertEqual(base, head(self.repo))
        rows, offenders = self.gate(base=base)
        self.assertEqual((rows, offenders), ([], []))

    def test_record_before_all_commits_checks_whole_history(self):
        feature(self.repo, "fix/h", "h.txt")
        merge_feature(self.repo, "fix/h", review=None)
        self.write_deploys({"t": "1970-01-01T00:00:01", "epoch": 1, "tag": "ro-legion:deploy-z",
                            "services": ["legion"], "critical": False, "reason": "", "fixes": []})
        base, note = DP.ship_base(self.repo, head(self.repo), DP.execute)
        self.assertIsNone(base)
        self.assertEqual(len(DP.merge_gate(self.repo, head(self.repo), base)[1]), 1)


class TestDeployGateRefusal(MergeGateRepo):
    def test_refusal_lists_every_offender_and_exit_9(self):
        feature(self.repo, "fix/i", "i.txt")
        merge_feature(self.repo, "fix/i", review=None)
        feature(self.repo, "fix/j", "j.txt")
        merge_feature(self.repo, "fix/j", review="verdict: do not merge\n")
        out = []
        d = DP.Deploy(root=self.repo, apply=False, out=out.append)
        with self.assertRaises(DP.Refused) as cm:
            d.gate_merges()
        self.assertEqual(cm.exception.code, 9)
        self.assertEqual(DP.UNREVIEWED, 9)
        msg = cm.exception.msg
        self.assertIn("Merge branch 'fix/i' into main", msg)
        self.assertIn("Merge branch 'fix/j' into main", msg)
        self.assertIn("no review file", msg)
        self.assertIn("verdict: do not merge", msg)
        listing = "\n".join(out)
        self.assertIn("merges that would ship (no deploy recorded", listing)
        self.assertIn("review: none", listing)
        self.assertIn("review: do not merge", listing)

    def test_all_reviewed_passes_and_lists_them(self):
        feature(self.repo, "fix/k", "k.txt")
        merge_feature(self.repo, "fix/k", review="verdict: merge\n")
        out = []
        d = DP.Deploy(root=self.repo, apply=False, out=out.append)
        note = d.gate_merges()
        self.assertIn("no deploy recorded", note)
        self.assertEqual(d.head, head(self.repo))
        self.assertIn("review: merge", "\n".join(out))


if __name__ == "__main__":
    unittest.main()
