"""ayla_engine_migrate.py on a temp copy of a data directory (never the live legion/data, G23)."""
import importlib.util
import io
import json
import os
import contextlib
import tempfile
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
SCRIPT = os.path.join(HERE, "..", "..", "tools", "system", "ayla_engine_migrate.py")
spec = importlib.util.spec_from_file_location("ayla_engine_migrate", SCRIPT)
mig = importlib.util.module_from_spec(spec)
spec.loader.exec_module(mig)


def bot_persona(i):
    return {"index": i, "account": "lg%04d" % i, "account_id": 2100000 + i, "char_id": 170000 + i, "name": "Bot%d" % i,
            "class": 0, "class_name": "Novice", "build": "str-vit", "base_level": 1, "job_level": 1,
            "characters": [{"char_id": 170000 + i, "slot": 0, "name": "Bot%d" % i, "role": "main", "build": "str-vit"}]}


class MigrateTest(unittest.TestCase):
    def setUp(self):
        self.dir = tempfile.mkdtemp(prefix="aylamig")
        self.data = self.dir
        with open(os.path.join(self.data, "personas.json"), "w", encoding="utf-8") as f:
            json.dump([bot_persona(1), bot_persona(2)], f)
        with open(os.path.join(self.data, "human-parties.json"), "w", encoding="utf-8") as f:
            json.dump([{"human": "Ayla", "members": ["Zendancer84", "Zen.fox"], "alumni": ["Echobunny"]}], f)
        self.cfg = os.path.join(self.dir, "config.txt")
        with open(self.cfg, "w", encoding="utf-8") as f:
            f.write("# OpenKore\nusername player\npassword CFGPW-7731\n")

    def run_main(self, *extra):
        out = io.StringIO()
        argv = ["--data", self.data, "--map", "pay_dun03", "--openkore-config", self.cfg] + list(extra)
        with contextlib.redirect_stdout(out), contextlib.redirect_stderr(io.StringIO()):
            rc = mig.main(argv)
        return rc, out.getvalue()

    def files(self):
        return sorted(os.listdir(self.data))

    def test_dry_run_writes_nothing(self):
        before = self.files()
        rc, out = self.run_main()
        self.assertEqual(rc, 0)
        self.assertIn("dry run", out)
        self.assertEqual(self.files(), before)
        self.assertNotIn("CFGPW-7731", out)

    def test_password_override_wins(self):
        rc, out = self.run_main("--apply", "--password", "OVERRIDE-1")
        self.assertEqual(rc, 0)
        self.assertNotIn("OVERRIDE-1", out)
        with open(os.path.join(self.data, "logins.local.json"), encoding="utf-8") as f:
            self.assertEqual(json.load(f), {"2000000": "OVERRIDE-1"})

    def test_no_password_anywhere_refuses(self):
        os.remove(self.cfg)
        rc, _ = self.run_main("--apply")
        self.assertEqual(rc, 2)
        self.assertTrue(os.path.exists(os.path.join(self.data, "human-parties.json")))

    def test_apply_moves_party_and_adds_persona(self):
        rc, _ = self.run_main("--apply")
        self.assertEqual(rc, 0)
        self.assertFalse(os.path.exists(os.path.join(self.data, "human-parties.json")))
        self.assertTrue(os.path.exists(os.path.join(self.data, "human-parties.json.pre-ayla-engine")))
        with open(os.path.join(self.data, "parties_engine.json"), encoding="utf-8") as f:
            parties = json.load(f)
        self.assertEqual(len(parties), 1)
        p = parties[0]
        self.assertEqual(p["name"], "Ayla pt")
        self.assertEqual(p["leader"], "Ayla")
        self.assertEqual(p["members"], ["Ayla", "Zendancer84", "Zen.fox"])
        self.assertEqual(p["map"], "pay_dun03")
        self.assertTrue(p["engine"])
        with open(os.path.join(self.data, "personas.json"), encoding="utf-8") as f:
            personas = json.load(f)
        ayla = [x for x in personas if x["account_id"] == 2000000]
        self.assertEqual(len(ayla), 1)
        self.assertEqual(ayla[0]["char_id"], 150010)
        self.assertEqual(ayla[0]["build"], "assassin_sonic")
        self.assertEqual(ayla[0]["password"], "")   # tracked file: no secret in it
        with open(os.path.join(self.data, "personas.json"), encoding="utf-8") as f:
            self.assertNotIn("CFGPW-7731", f.read())
        with open(os.path.join(self.data, "logins.local.json"), encoding="utf-8") as f:
            self.assertEqual(json.load(f), {"2000000": "CFGPW-7731"})
        self.assertEqual(ayla[0]["index"], 3)
        self.assertEqual(ayla[0]["characters"][0]["char_id"], 150010)
        # the old personas file is kept
        self.assertTrue(os.path.exists(os.path.join(self.data, "personas.json.pre-ayla-engine")))

    def test_second_run_changes_nothing(self):
        self.run_main("--apply")
        with open(os.path.join(self.data, "personas.json"), "rb") as f:
            personas_before = f.read()
        rc, out = self.run_main("--apply")
        self.assertEqual(rc, 0)
        self.assertIn("already migrated", out)
        with open(os.path.join(self.data, "personas.json"), "rb") as f:
            self.assertEqual(f.read(), personas_before)
        with open(os.path.join(self.data, "parties_engine.json"), encoding="utf-8") as f:
            self.assertEqual(len(json.load(f)), 1)

    def test_refuses_other_humans(self):
        with open(os.path.join(self.data, "human-parties.json"), "w", encoding="utf-8") as f:
            json.dump([{"human": "Ayla", "members": []}, {"human": "Someone", "members": []}], f)
        rc, _ = self.run_main("--apply")
        self.assertEqual(rc, 2)
        self.assertTrue(os.path.exists(os.path.join(self.data, "human-parties.json")))
        self.assertFalse(os.path.exists(os.path.join(self.data, "parties_engine.json")))

    def test_never_overwrites_kept_file(self):
        with open(os.path.join(self.data, "human-parties.json.pre-ayla-engine"), "w", encoding="utf-8") as f:
            f.write("[]")
        rc, _ = self.run_main("--apply")
        self.assertEqual(rc, 2)
        self.assertTrue(os.path.exists(os.path.join(self.data, "human-parties.json")))


if __name__ == "__main__":
    unittest.main()
