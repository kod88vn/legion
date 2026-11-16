"""tools/rogui/ayla.sh: the send guard (G05, cmdfile.pl) and the rally/map argument checks (retroactive review cb5ccdc).
Each test runs the real script with AYLA_CMDS pointing at a temp file, so nothing reaches Ayla's live command file (G23).
Run: python -m unittest discover -s tests/system -v   (from the repo root)"""
import os as _os  # G23: tests never touch the live legion/data folder
import sys as _sys
_sys.path.insert(0, _os.path.join(_os.path.dirname(_os.path.abspath(__file__)), ".."))
import testenv  # noqa: E402
testenv.isolate()

import os
import shutil
import subprocess
import tempfile
import unittest

REPO = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", ".."))
SH = os.path.join(REPO, "tools", "rogui", "ayla.sh").replace("\\", "/")
BASH = shutil.which("bash")


@unittest.skipUnless(BASH, "bash is needed to run ayla.sh")
class TestSendGuard(unittest.TestCase):
    def setUp(self):
        self.d = tempfile.mkdtemp()
        self.cmds = os.path.join(self.d, "commands.txt").replace("\\", "/")

    def tearDown(self):
        shutil.rmtree(self.d, ignore_errors=True)

    def sh(self, *args):
        env = dict(os.environ, AYLA_CMDS=self.cmds)
        return subprocess.run([BASH, SH] + list(args), env=env, capture_output=True, text=True, timeout=60)

    def sent(self):
        if not os.path.exists(self.cmds):
            return []
        with open(self.cmds, encoding="utf-8") as f:
            return f.read().splitlines()

    def test_plain_command_is_sent(self):
        r = self.sh("cmd", "respawn")
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertEqual(self.sent(), ["respawn"])

    def test_ai_toggles_are_refused(self):
        # G05: a bare 'ai', 'ai off' or 'ai manual' would stop Ayla's AI; cmdfile.pl checks the same
        for text in ("ai", "ai off", "ai manual", "  ai  ", "AI off"):
            r = self.sh("cmd", text)
            self.assertNotEqual(r.returncode, 0, text)
        self.assertEqual(self.sent(), [])

    def test_ai_on_is_allowed(self):
        self.assertEqual(self.sh("cmd", "ai on").returncode, 0)
        self.assertEqual(self.sent(), ["ai on"])

    def test_double_semicolon_cannot_chain_a_command(self):
        # cb5ccdc review: 'rally x ;; ai' would pass cmdfile.pl, which checks only the start of a line
        self.assertNotEqual(self.sh("rally", "x ;; ai").returncode, 0)
        self.assertNotEqual(self.sh("party", "hi ;; ai").returncode, 0)
        self.assertNotEqual(self.sh("cmd", "p hi;;ai").returncode, 0)
        self.assertNotEqual(self.sh("say", "hello;;ai").returncode, 0)
        self.assertEqual(self.sent(), [])

    def test_and_and_and_pipe_are_refused(self):
        self.assertNotEqual(self.sh("say", "x && y").returncode, 0)
        self.assertNotEqual(self.sh("say", "a | b").returncode, 0)
        self.assertEqual(self.sent(), [])

    def test_newline_cannot_split_a_line(self):
        r = subprocess.run([BASH, "-c", 'source "%s"; send $\'hello\nai\'' % SH],
                           env=dict(os.environ, AYLA_CMDS=self.cmds), capture_output=True, text=True, timeout=60)
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertEqual(self.sent(), ["hello ai"])     # one line, and 'ai' is not at its start

    def test_rally_arguments_are_checked(self):
        self.assertEqual(self.sh("rally", "cancel").returncode, 0)
        self.assertEqual(self.sh("rally", "prontera").returncode, 0)
        self.assertEqual(self.sh("rally", "prontera pay_fild08").returncode, 0)
        self.assertNotEqual(self.sh("rally", "pay_dun01 pay_dun02 extra").returncode, 0)
        self.assertNotEqual(self.sh("rally", "x*").returncode, 0)
        self.assertNotEqual(self.sh("rally").returncode, 0)
        self.assertEqual(self.sent(), ["partylead rally cancel", "partylead rally prontera",
                                       "partylead rally prontera pay_fild08"])

    def test_map_name_is_checked(self):
        self.assertEqual(self.sh("map", "pay_dun01").returncode, 0)
        self.assertNotEqual(self.sh("map", "x;;ai").returncode, 0)
        self.assertNotEqual(self.sh("map", "a b").returncode, 0)
        self.assertEqual(self.sent(), ["partylead goto pay_dun01"])

    def test_sourcing_runs_no_action(self):
        # the tests source the script: sourcing defines the functions and stops before the dispatcher
        r = subprocess.run([BASH, "-c", 'source "%s"; echo loaded' % SH],
                           env=dict(os.environ, AYLA_CMDS=self.cmds), capture_output=True, text=True, timeout=60)
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertEqual(r.stdout.strip(), "loaded")
        self.assertFalse(os.path.exists(self.cmds))


if __name__ == "__main__":
    unittest.main()
