"""Guard (rule G23): the test suites never create, change or delete a file in the live legion/data folder.

Each suite runs in a child process with LEGION_DATA unset (the way a plain run from the checkout does). The live
folder is listed before and after (file names, sizes, mtimes); any difference fails the test. Files the running
stack rewrites on its own clock (heartbeats, locks, the bug-queue watcher state) are left out of the comparison,
because the stack is always writing them; every other file is compared.
LEGION_GUARD_CHILD=1 makes this test skip itself, so the child runs do not start another guard.
"""
import os
import subprocess
import sys
import unittest

REPO = os.path.abspath(os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", ".."))
LIVE = os.path.join(REPO, "legion", "data")
SUITES = ["tests/system", "tests/dashboard", "tests/ayla", "tests/tools", "tools/puppet/tests"]
STACK_OWNED = (".heartbeat", ".lock", "watchdog.status.json", "bugq.wake", "bugq.wake.state")


def snapshot(root):
    """relative path -> (size, mtime_ns) for every file under root, minus the stack-owned files"""
    out = {}
    for dp, _dirs, files in os.walk(root):
        for fn in files:
            if fn.endswith(STACK_OWNED):
                continue
            full = os.path.join(dp, fn)
            st = os.stat(full)
            out[os.path.relpath(full, root).replace("\\", "/")] = (st.st_size, st.st_mtime_ns)
    return out


@unittest.skipIf(os.environ.get("LEGION_GUARD_CHILD"), "a child run of the guard")
class LiveDataGuard(unittest.TestCase):
    def test_suites_leave_live_data_alone(self):
        before = snapshot(LIVE) if os.path.isdir(LIVE) else {}
        env = {k: v for k, v in os.environ.items() if k != "LEGION_DATA"}
        env["LEGION_GUARD_CHILD"] = "1"
        for suite in SUITES:
            subprocess.run([sys.executable, "-m", "unittest", "discover", "-s", suite, "-t", suite],
                           cwd=REPO, env=env, capture_output=True, text=True, timeout=900)
        after = snapshot(LIVE) if os.path.isdir(LIVE) else {}
        created = sorted(set(after) - set(before))
        removed = sorted(set(before) - set(after))
        changed = sorted(k for k in set(before) & set(after) if before[k] != after[k])
        self.assertEqual(created, [], "suites created files in the live legion/data")
        self.assertEqual(removed, [], "suites removed files from the live legion/data")
        self.assertEqual(changed, [], "suites changed files in the live legion/data")


if __name__ == "__main__":
    unittest.main()
