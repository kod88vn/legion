"""guild-say in tools/legionctl_guild.py goes through the guild's leader route (one call, no bot-name lookup).
Offline: a fake call() records the requests. Run: python -m unittest discover -s tests/tools -p "test_legionctl_guild.py" -v"""
import os as _os  # G23: tests never touch the live legion/data folder
import sys as _sys
_sys.path.insert(0, _os.path.join(_os.path.dirname(_os.path.abspath(__file__)), ".."))
import testenv  # noqa: E402
testenv.isolate()  # LEGION_DATA -> a temp folder before any tool module is imported

import os
import sys
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, os.path.normpath(os.path.join(HERE, "..", "..", "tools")))
import legionctl_guild  # noqa: E402


class GuildSayTest(unittest.TestCase):
    def test_say_uses_guild_leader_route(self):
        calls, printed = [], []

        def call(path, body=None):
            calls.append((path, body))
            return {"ok": True}

        def opt(args, name, default=None, flag=False):
            return default

        def q(s):
            return s

        legionctl_guild.guild_cmd("guild-say", ["guild08", "hello", "all"], call, printed.append, opt, q)
        self.assertEqual(calls, [("/api/guild-leaders/guild08/say", {"text": "hello all"})])


if __name__ == "__main__":
    unittest.main()
