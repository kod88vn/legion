"""gatecheck.py finds every Python suite, so none can be skipped (the Builds tests were once in a folder nobody ran).
Offline: a temp tree stands in for the repo. Run: python -m unittest discover -s tests/system -p "test_gatecheck_suites.py" -v"""
import os
import sys
import tempfile
import unittest
from unittest import mock

HERE = os.path.dirname(os.path.abspath(__file__))
REPO = os.path.normpath(os.path.join(HERE, "..", ".."))
sys.path.insert(0, os.path.join(REPO, "tools", "system"))
import gatecheck  # noqa: E402


class SuiteDiscovery(unittest.TestCase):
    def test_every_suite_folder_of_the_repo_is_found(self):
        found = gatecheck.suite_dirs()
        for d in ("tests/ayla", "tests/dashboard", "tests/system", "tests/tools", "tools/puppet/tests"):
            self.assertIn(d, found)

    def test_a_folder_without_a_test_file_is_not_a_suite(self):
        root = tempfile.mkdtemp()
        os.makedirs(os.path.join(root, "a", "tests"))
        open(os.path.join(root, "a", "tests", "test_one.py"), "w").close()
        os.makedirs(os.path.join(root, "b"))
        open(os.path.join(root, "b", "helper.py"), "w").close()
        os.makedirs(os.path.join(root, "data"))            # live data folders are never walked
        open(os.path.join(root, "data", "test_live.py"), "w").close()
        with mock.patch.object(gatecheck, "ROOT", root):
            self.assertEqual(gatecheck.suite_dirs(), ["a/tests"])

    def test_a_failing_suite_is_reported(self):
        root = tempfile.mkdtemp()
        os.makedirs(os.path.join(root, "bad"))
        with open(os.path.join(root, "bad", "test_bad.py"), "w", encoding="utf-8", newline="\n") as f:
            f.write("import unittest\nclass T(unittest.TestCase):\n    def test_x(self):\n        self.fail('boom')\n")
        with mock.patch.object(gatecheck, "ROOT", root):
            self.assertEqual(gatecheck.run_suites(), ["bad"])


if __name__ == "__main__":
    unittest.main()
