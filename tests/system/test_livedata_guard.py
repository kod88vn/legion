"""Guard (rule G23): no test reaches the live legion/data folder.

Every test process sets LEGION_TEST=1 (tests/testenv.py). With that set, the one data-folder resolver
(tools/system/common.py data_dir) raises as soon as it would resolve to the live legion/data, so a test that
reaches the live folder fails on the spot. This file checks that the rule holds:
- each suite runs in a child process with LEGION_TEST=1 and LEGION_DATA unset, and every child must exit 0
  (a timeout kills the child and fails the guard);
- a planted leak (a child that resolves the data folder with LEGION_DATA unset) must raise.
No file comparison: the running stack writes its own files, so the tripwire is the resolver, not a snapshot.
LEGION_GUARD_CHILD=1 makes the suite-run test skip itself, so the child runs do not start another guard.
"""
import os
import subprocess
import sys
import unittest

REPO = os.path.abspath(os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", ".."))
SUITES = ["tests/system", "tests/dashboard", "tests/ayla", "tests/tools", "tools/puppet/tests"]
CHILD_TIMEOUT = 900


RESOLVER_OVERRIDES = ("LEGION_DATA", "AYLA_ORDERS", "AYLA_ORDERS_LOG", "LEGION_BUGQ_EXTRA")


def _child_env(**extra):
    """the parent's environment without the path overrides, so a child resolves its paths the way a bare run does
    (test_constitution sets AYLA_ORDERS for the whole process; a child must not inherit that)"""
    env = {k: v for k, v in os.environ.items() if k not in RESOLVER_OVERRIDES}
    env["LEGION_TEST"] = "1"
    env.update(extra)
    return env


@unittest.skipIf(os.environ.get("LEGION_GUARD_CHILD"), "a child run of the guard")
class SuitesStayOffLiveData(unittest.TestCase):
    def test_every_suite_exits_zero_under_the_tripwire(self):
        failures = []
        for suite in SUITES:
            cmd = [sys.executable, "-m", "unittest", "discover", "-s", suite, "-t", suite]
            try:
                r = subprocess.run(cmd, cwd=REPO, env=_child_env(LEGION_GUARD_CHILD="1"),
                                   capture_output=True, text=True, timeout=CHILD_TIMEOUT)
            except subprocess.TimeoutExpired:   # subprocess.run has killed the child
                failures.append("%s: timed out after %ds (child killed)" % (suite, CHILD_TIMEOUT))
                continue
            if r.returncode != 0:
                failures.append("%s: exit %d: %s" % (suite, r.returncode, (r.stderr or r.stdout)[-400:]))
        self.assertEqual(failures, [], "\n".join(failures))


class PlantedLeak(unittest.TestCase):
    """each resolver of a live location, called with no test path, must raise under LEGION_TEST=1"""
    LEAKS = {
        "common.data_dir": ("tools/system", "import common; common.data_dir()"),
        "mindgate.data_dir": ("tools", "import mindgate; mindgate.data_dir()"),
        "orders.orders_path": ("tools/puppet", "import orders; orders.orders_path()"),
        "orders.log_path": ("tools/puppet", "import orders; orders.log_path()"),
        "bugq.extra_paths (Ayla's queue)": ("tools/system", "import bugq; bugq.extra_paths()"),
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
