"""tools/system/runsuites.py: a hung suite fails with a clear message, the G23 audit tripwire refuses a live-folder write
in a suite and in a grandchild, and --changed picks the right suites. Offline: temp folders only, no live data.
Run: python -m unittest discover -s tests/system -p test_runsuites.py -v"""
import os as _os  # G23: tests never touch the live legion/data folder
import sys as _sys
_sys.path.insert(0, _os.path.join(_os.path.dirname(_os.path.abspath(__file__)), ".."))
import testenv  # noqa: E402
testenv.isolate()  # LEGION_DATA -> a temp folder before any tool module is imported

import os
import shutil
import subprocess
import sys
import tempfile
import time
import unittest

REPO = os.path.abspath(os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", ".."))
sys.path.insert(0, os.path.join(REPO, "tools", "system"))
import runsuites  # noqa: E402


def _tree(files):
    """a temp repo root with the given {relative path: text} files; returns the root"""
    root = tempfile.mkdtemp(prefix="runsuites-test-")
    for rel, text in files.items():
        p = os.path.join(root, rel.replace("/", os.sep))
        os.makedirs(os.path.dirname(p), exist_ok=True)
        with open(p, "w", encoding="utf-8", newline="\n") as f:
            f.write(text)
    return root


class HangIsAFailure(unittest.TestCase):
    def test_a_hung_child_is_killed_and_reported(self):
        root = _tree({"hang/test_hang.py": "import time, unittest\n"
                                           "class T(unittest.TestCase):\n"
                                           "    def test_x(self):\n"
                                           "        time.sleep(120)\n"})
        t0 = time.time()
        try:
            r = runsuites.run_suite("hang", root=root, timeout=3)
        finally:
            shutil.rmtree(root, ignore_errors=True)
        self.assertTrue(r.timed_out)
        self.assertFalse(r.ok)
        self.assertLess(time.time() - t0, 60, "the runner waited for the hung suite")
        self.assertIn("TIMEOUT", runsuites.describe(r))

    def test_run_bounded_kills_the_whole_tree(self):
        code = "import subprocess, sys, time; subprocess.Popen([sys.executable, '-c', 'import time; time.sleep(120)']); time.sleep(120)"
        rc, out, err, secs, timed_out = runsuites.run_bounded([sys.executable, "-c", code], REPO, dict(os.environ), 3)
        self.assertTrue(timed_out)
        self.assertLess(secs, 60)


class Tripwire(unittest.TestCase):
    def test_a_suite_cannot_open_a_file_in_the_live_folder(self):
        probe = ("import os\n"
                 "p = os.path.join(os.path.dirname(os.path.abspath(__file__)), '..', 'legion', 'data', 'probe.lock')\n"
                 "open(p, 'w').close()\n")
        root = _tree({"legion/data/.keep": "", "leak/test_leak.py": "import unittest\n" + "class T(unittest.TestCase):\n"
                      "    def test_leak(self):\n" + "".join("        " + l + "\n" for l in probe.splitlines())})
        try:
            r = runsuites.run_suite("leak", root=root, timeout=60)
            self.assertFalse(r.ok, r.tail())
            self.assertIn("G23 tripwire", r.err)
            self.assertFalse(os.path.exists(os.path.join(root, "legion", "data", "probe.lock")))
        finally:
            shutil.rmtree(root, ignore_errors=True)

    def test_a_grandchild_with_no_test_variables_is_refused_too(self):
        # the grandchild drops LEGION_TEST and LEGION_DATA (as a test may), and PYTHONPATH is kept by the test
        child = ("import os, subprocess, sys\n"
                 "env = {k: v for k, v in os.environ.items() if k not in ('LEGION_TEST', 'LEGION_DATA')}\n"
                 "p = os.path.join(os.getcwd(), 'legion', 'data', 'grand.lock')\n"
                 "r = subprocess.run([sys.executable, '-c', \"open(%r, 'w').close()\" % p], env=env, "
                 "capture_output=True, text=True)\n"
                 "print('RC', r.returncode); print(r.stderr[-300:])\n")
        root = _tree({"legion/data/.keep": "", "grand/test_grand.py": "import subprocess, sys, unittest\n"
                      "class T(unittest.TestCase):\n    def test_g(self):\n"
                      "        r = subprocess.run([sys.executable, '-c', %r], capture_output=True, text=True)\n"
                      "        self.assertIn('RC 1', r.stdout + r.stderr, r.stdout + r.stderr)\n" % child})
        try:
            env = runsuites.tripwire_env(root)
            r = subprocess.run([sys.executable, "-W", "ignore", "-m", "unittest", "discover", "-s", "grand", "-t", "grand"],
                               cwd=root, env=env, capture_output=True, text=True, timeout=120)
            self.assertEqual(r.returncode, 0, r.stderr[-800:])
            self.assertFalse(os.path.exists(os.path.join(root, "legion", "data", "grand.lock")))
        finally:
            shutil.rmtree(root, ignore_errors=True)

    def test_committed_config_may_be_read(self):
        root = _tree({"legion/data/tuning.json": "{}\n"})
        try:
            env = runsuites.tripwire_env(root)
            env["LEGION_TRIPWIRE_READ"] = os.path.join(root, "legion", "data", "tuning.json")
            env["LEGION_TRIPWIRE_LIVE"] = os.path.join(root, "legion", "data")
            code = "import os; print(open(os.path.join(os.getcwd(), 'legion', 'data', 'tuning.json')).read().strip())"
            r = subprocess.run([sys.executable, "-c", code], cwd=root, env=env, capture_output=True, text=True, timeout=60)
            self.assertEqual(r.returncode, 0, r.stderr)
            self.assertEqual(r.stdout.strip(), "{}")
        finally:
            shutil.rmtree(root, ignore_errors=True)


class ChangedSelection(unittest.TestCase):
    SUITES = ["tests/dashboard", "tests/system", "tests/tools"]

    def test_shared_module_runs_every_suite(self):
        run, _ = runsuites.select_for_changed(["tools/system/common.py"], self.SUITES)
        self.assertEqual(run, self.SUITES)

    def test_a_suite_file_runs_its_suite_and_the_core(self):
        run, _ = runsuites.select_for_changed(["tests/tools/test_gearalts.py"], self.SUITES)
        self.assertEqual(run, ["tests/system", "tests/tools"])

    def test_an_unmentioned_file_runs_the_core_only(self):
        run, why = runsuites.select_for_changed(["docs/rules/archive.md"], self.SUITES)
        self.assertEqual(run, ["tests/system"])

    def test_a_docs_only_change_runs_the_core(self):
        run, _ = runsuites.select_for_changed(["README.md", "docs/x.md"], self.SUITES)
        self.assertEqual(run, ["tests/system"])

    def test_a_module_mentioned_by_a_suite_selects_that_suite(self):
        root = _tree({"tests/dashboard/test_x.py": "import dashthing\n", "tests/system/test_y.py": "pass\n",
                      "tools/dashboard/dashthing.py": "x = 1\n"})
        try:
            run, why = runsuites.select_for_changed(["tools/dashboard/dashthing.py"],
                                                    ["tests/dashboard", "tests/system"], root=root)
            self.assertEqual(run, ["tests/dashboard", "tests/system"])
        finally:
            shutil.rmtree(root, ignore_errors=True)


if __name__ == "__main__":
    unittest.main()
