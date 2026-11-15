"""Tests for the chat-jobs and bot commands of tools/legionctl.py (bug queue P10). Offline: the API is replaced.
Run: python -m unittest discover -s tests/system -p "test_legionctl_chat.py" -v"""
import contextlib
import datetime
import io
import json
import os
import sys
import unittest
from unittest import mock

HERE = os.path.dirname(os.path.abspath(__file__))
REPO = os.path.normpath(os.path.join(HERE, "..", ".."))
sys.path.insert(0, os.path.join(REPO, "tools"))
import legionctl  # noqa: E402


def run(argv):
    buf = io.StringIO()
    with contextlib.redirect_stdout(buf):
        legionctl.main(argv)
    return buf.getvalue()


def job(i, deadline="2026-11-15T08:51:13Z"):
    return {"id": f"m{i}", "kind": "ask", "channel": "guild", "speaker": f"S{i}", "hearers": [], "deadline": deadline,
            "facts": {}, "template_lines": [{"speaker": f"S{i}", "channel": "guild", "text": "hi"}]}


class ChatJobsCli(unittest.TestCase):
    def test_flag_without_value_is_a_usage_error_not_an_index_error(self):
        # bash reads `--channel #trade` as `--channel` plus a comment: the value is missing
        with self.assertRaises(SystemExit) as cm:
            legionctl.opt(["m1816", "Name", "text", "--channel"], "--channel")
        self.assertIn("needs a value", str(cm.exception))

    def test_channel_names(self):
        self.assertEqual(legionctl.norm_channel("trade"), "#trade")
        self.assertEqual(legionctl.norm_channel("Global"), "#global")
        self.assertEqual(legionctl.norm_channel("#trade"), "#trade")
        self.assertEqual(legionctl.norm_channel("guild"), "guild")
        self.assertIsNone(legionctl.norm_channel(None))

    def test_deadline_is_shown_on_the_local_clock(self):
        # 08:51 UTC is the same instant as 08:51 UTC; the local clock is whatever this machine says
        want = datetime.datetime(2026, 11, 15, 8, 51, 13, tzinfo=datetime.timezone.utc).astimezone().strftime("%H:%M:%S")
        self.assertEqual(legionctl.local_hms("2026-11-15T08:51:13Z"), want)
        self.assertEqual(legionctl.local_hms("2026-11-15T08:51:13.123456789+00:00"), want)
        self.assertEqual(legionctl.local_hms("2026-11-15T10:51:13+02:00"), want)
        self.assertEqual(legionctl.local_hms("2026-11-15T08:51:13"), "08:51:13")

    def test_deadline_uses_the_timestamp_date(self):
        # a July timestamp gets July's offset (a fixed January date would give the winter offset in a DST zone)
        want = datetime.datetime(2026, 7, 15, 8, 51, 13, tzinfo=datetime.timezone.utc).astimezone().strftime("%H:%M:%S")
        self.assertEqual(legionctl.local_hms("2026-07-15T08:51:13Z"), want)

    def test_list_pages(self):
        jobs = [job(i) for i in range(1, 6)]
        reply = {"on": True, "wait_sec": [45, 90], "jobs": jobs}
        with mock.patch.object(legionctl, "call", return_value=reply):
            text = run(["chat-jobs", "list", "--limit", "2", "--page", "2"])
        self.assertIn("waiting 5, showing 3-4", text)
        self.assertIn("[m3]", text)
        self.assertIn("[m4]", text)
        self.assertNotIn("[m2]", text)
        self.assertNotIn("[m5]", text)

    def test_list_all_and_json(self):
        jobs = [job(i) for i in range(1, 4)]
        with mock.patch.object(legionctl, "call", return_value={"on": True, "wait_sec": [45, 90], "jobs": jobs}):
            text = run(["chat-jobs", "list", "--limit", "0"])
            js = json.loads(run(["chat-jobs", "list", "--limit", "2", "--json"]))
        self.assertIn("showing 1-3", text)
        self.assertIn("[m3]", text)
        self.assertEqual(len(js["jobs"]), 2)
        self.assertEqual(js["total"], 3)

    def test_empty_queue_header(self):
        with mock.patch.object(legionctl, "call", return_value={"on": False, "wait_sec": [45, 90], "jobs": []}):
            text = run(["chat-jobs", "list"])
        self.assertIn("chat jobs off; waiting 0, showing 0-0", text)


class BotCli(unittest.TestCase):
    def test_ayla_falls_back_to_the_status_feed(self):
        def fake(path, body=None):
            if path.startswith("/api/bots/"):
                raise SystemExit("error 404: no such bot")  # what call() does on a 404
            if path == "/api/meter/neo":
                return {"ayla": {"name": "Ayla", "class": "Assassin", "lv": 83, "map": "pay_dun02", "online": True}}
            raise AssertionError(path)
        with mock.patch.object(legionctl, "call", side_effect=fake):
            text = run(["bot", "Ayla"])
        got = json.loads(text)
        self.assertEqual(got["map"], "pay_dun02")
        self.assertTrue(got["online"])

    def test_other_bots_keep_the_404(self):
        with mock.patch.object(legionctl, "call", side_effect=SystemExit("error 404: no such bot")):
            with self.assertRaises(SystemExit):
                run(["bot", "nobody"])


if __name__ == "__main__":
    unittest.main()
