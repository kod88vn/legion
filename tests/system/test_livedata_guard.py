"""Guard (rule G23): no test reaches the live legion/data folder.

Every test process sets LEGION_TEST=1 (tests/testenv.py). With that set, the one data-folder resolver
(tools/system/common.py data_dir) raises as soon as it would resolve to the live legion/data, so a test that
reaches the live folder fails on the spot. This file checks that the rule holds:
- each suite runs in a child process with LEGION_TEST=1 and LEGION_DATA unset, and every child must exit 0. The
  children come from tools/system/runsuites.py: the same discovery, a hard timeout per suite (a hang fails the
  guard, it never stalls it) and a bounded number of children at a time;
- a planted leak (a child that resolves the data folder with LEGION_DATA unset) must raise.
No file comparison: the running stack writes its own files, so the tripwire is the resolver, not a snapshot.
LEGION_GUARD_CHILD=1 makes the suite-run test skip itself: the children carry it, and runsuites.py sets it on every
child, so when runsuites.py (gatecheck.py, or `runsuites.py` itself) runs the suites, that run is this guard's run and
the suites are not run twice.
"""
import os
import subprocess
import sys
import unittest

REPO = os.path.abspath(os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", ".."))
sys.path.insert(0, os.path.join(REPO, "tools", "system"))
import runsuites  # noqa: E402

# suites that must exist (discovery finds them; a missing one is a failure, not a quiet skip)
SUITES = ["tests/system", "tests/dashboard", "tests/ayla", "tests/tools", "tools/puppet/tests"]


def _child_env(**extra):
    """the tripwire environment (runsuites.tripwire_env) plus extra variables"""
    env = runsuites.tripwire_env()
    env.update(extra)
    return env


@unittest.skipIf(os.environ.get("LEGION_GUARD_CHILD"), "a child run of the guard: runsuites.py is the guard's run")
class SuitesStayOffLiveData(unittest.TestCase):
    def test_every_suite_exits_zero_under_the_tripwire(self):
        suites = runsuites.suite_dirs(REPO)
        for s in SUITES:
            self.assertIn(s, suites, "suite %s was not found by discovery" % s)
        results = runsuites.run_all(suites, REPO)
        failures = [runsuites.describe(r) + "\n" + r.tail(400) for r in results if not r.ok]
        self.assertEqual(failures, [], "\n".join(failures))


class PlantedLeak(unittest.TestCase):
    """each resolver of a live location, called with no test path, must raise under LEGION_TEST=1"""
    LEAKS = {
        "common.data_dir": ("tools/system", "import common; common.data_dir()"),
        "mindgate.data_dir": ("tools", "import mindgate; mindgate.data_dir()"),
        "orders.orders_path": ("tools/puppet", "import orders; orders.orders_path()"),
        "orders.log_path": ("tools/puppet", "import orders; orders.log_path()"),
        "bugq.extra_paths (Ayla's queue)": ("tools/system", "import bugq; bugq.extra_paths()"),
        "aylalog.default_log": ("tools/system", "import aylalog; aylalog.default_log()"),
        "dispatch.wd_queue_path": ("tools/puppet", "import dispatch; dispatch.wd_queue_path()"),
        "dispatch.wd_console_path": ("tools/puppet", "import dispatch; dispatch.wd_console_path()"),
    }

    def test_planted_leaks_raise(self):
        for name, (subdir, call) in self.LEAKS.items():
            with self.subTest(resolver=name):
                code = "import sys; sys.path.insert(0, %r); %s" % (os.path.join(REPO, subdir), call)
                r = subprocess.run([sys.executable, "-c", code], cwd=REPO, env=_child_env(),
                                   capture_output=True, text=True, timeout=120)
                self.assertNotEqual(r.returncode, 0, "planted leak through %s did not raise" % name)
                self.assertIn("G23", r.stderr)

    def test_resolvers_accept_a_temp_folder(self):
        import tempfile
        d = tempfile.mkdtemp(prefix="guard-probe-")
        code = ("import sys; sys.path.insert(0, %r); sys.path.insert(0, %r); import common, orders; "
                "print(common.data_dir()); print(orders.orders_path())"
                % (os.path.join(REPO, "tools", "system"), os.path.join(REPO, "tools", "puppet")))
        r = subprocess.run([sys.executable, "-c", code], cwd=REPO,
                           env=_child_env(LEGION_DATA=d, AYLA_ORDERS=os.path.join(d, "o.jsonl"),
                                          AYLA_ORDERS_LOG=os.path.join(d, "o.log")),
                           capture_output=True, text=True, timeout=120)
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertEqual(r.stdout.splitlines(), [d, os.path.join(d, "o.jsonl")])


if __name__ == "__main__":
    unittest.main()
