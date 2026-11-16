"""Bug queue ids: update(), close() and assign() take one plain string id. A tuple (add() returns (id, is_new)) or
an empty id must be refused before anything is written, so the queue never gets a line that load() cannot read.
Run: python -m unittest discover -s tests/system -v   (from the repo root)"""
import os as _os  # G23: tests never touch the live legion/data folder
import sys as _sys
_sys.path.insert(0, _os.path.join(_os.path.dirname(_os.path.abspath(__file__)), ".."))
import testenv  # noqa: E402
testenv.isolate()  # LEGION_DATA -> a temp folder before any tool module is imported

import io
import json
import os
import sys
import unittest
from contextlib import redirect_stdout

sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "..", "tools", "system"))
import bugq  # noqa: E402


class BugqIdGuard(unittest.TestCase):
    def setUp(self):
        self.path = bugq.qpath()
        if os.path.exists(self.path):
            os.remove(self.path)

    def lines(self):
        if not os.path.exists(self.path):
            return []
        with open(self.path, encoding="utf-8") as f:
            return [json.loads(x) for x in f if x.strip()]

    def test_add_returns_tuple_to_unpack(self):
        r = bugq.add("main", "normal", "guard test item", key="guard-test-unique-1")
        self.assertIsInstance(r, tuple)
        i, new = r
        self.assertTrue(new)
        self.assertIsInstance(i, str)
        self.assertIn(i, bugq.load())

    def test_update_refuses_tuple_and_writes_nothing(self):
        i, _ = bugq.add("main", "normal", "guard test item", key="guard-test-unique-2")
        before = self.lines()
        with self.assertRaises(TypeError):
            bugq.update((i, True), status="assigned")
        with self.assertRaises(TypeError):
            bugq.update([i], status="assigned")
        self.assertEqual(self.lines(), before)

    def test_close_and_assign_refuse_tuple(self):
        i, _ = bugq.add("main", "normal", "guard test item", key="guard-test-unique-3")
        before = self.lines()
        with self.assertRaises(TypeError):
            bugq.close((i, True))
        with self.assertRaises(TypeError):
            bugq.assign((i, True), "followups-batch-2")
        self.assertEqual(self.lines(), before)

    def test_empty_or_non_string_id_refused(self):
        with self.assertRaises(ValueError):
            bugq.update("   ", status="closed")
        with self.assertRaises(TypeError):
            bugq.update(None, status="closed")
        with self.assertRaises(TypeError):
            bugq.update(12, status="closed")
        self.assertEqual(self.lines(), [])

    def test_string_id_still_works_everywhere(self):
        i, _ = bugq.add("main", "normal", "guard test item", key="guard-test-unique-4")
        bugq.assign(i, "followups-batch-2")
        e = bugq.load()[i]
        self.assertEqual((e["status"], e["assigned_to"]), ("assigned", "followups-batch-2"))
        bugq.close(i, "done")
        self.assertEqual(bugq.load()[i]["status"], "closed")
        out = io.StringIO()
        with redirect_stdout(out):
            j, _ = bugq.add("main", "normal", "guard test item cli", key="guard-test-unique-5")
            self.assertEqual(bugq.main(["assign", j, "followups-batch-2"]), 0)
        self.assertEqual(bugq.load()[j]["assigned_to"], "followups-batch-2")


if __name__ == "__main__":
    unittest.main()
