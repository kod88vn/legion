"""ayla_engine_migrate.py on a temp copy of a data directory (never the live legion/data, G23)."""
import contextlib
import gzip
import importlib.util
import io
import json
import os
import shutil
import tempfile
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
SCRIPT = os.path.join(HERE, "..", "..", "tools", "system", "ayla_engine_migrate.py")
spec = importlib.util.spec_from_file_location("ayla_engine_migrate", SCRIPT)
mig = importlib.util.module_from_spec(spec)
spec.loader.exec_module(mig)

CFG_PW = "CFGPW-7731"


def bot_persona(i):
    return {"index": i, "account": "lg%04d" % i, "account_id": 2100000 + i, "char_id": 170000 + i, "name": "Bot%d" % i,
            "class": 0, "class_name": "Novice", "build": "str-vit", "base_level": 1, "job_level": 1,
            "characters": [{"char_id": 170000 + i, "slot": 0, "name": "Bot%d" % i, "role": "main", "build": "str-vit"}]}


def snapshot(path, party_name="Ayla pt", members=("Ayla", "Zendancer84", "xXRinfangXx", "Zen.fox", "vex_x18")):
    """A minimal DB snapshot in the dbsnap format: char 150010 in party 2 with the given members."""
    char_cols = ["char_id", "account_id", "name", "party_id"]
    rows = [[150010, 2000000, "Ayla", 2]]
    for i, n in enumerate(members):
        if n != "Ayla":
            rows.append([170500 + i, 2100500 + i, n, 2])
    rows.append([170999, 2100999, "Stranger", 0])
    doc = {"t": 0, "source": "test", "tables": {
        "char": {"cols": char_cols, "rows": rows},
        "party": {"cols": ["party_id", "name", "exp", "item", "leader_id", "leader_char"],
                  "rows": [[2, party_name, 1, 1, 2101589, 171589]]}}}
    with gzip.open(path, "wt", encoding="utf-8") as f:
        json.dump(doc, f)
    return path


class MigrateTest(unittest.TestCase):
    def setUp(self):
        self.dir = tempfile.mkdtemp(prefix="aylamig")
        self.data = os.path.join(self.dir, "data")
        os.makedirs(self.data)
        with open(os.path.join(self.data, "personas.json"), "w", encoding="utf-8") as f:
            json.dump([bot_persona(1), bot_persona(2)], f)
        with open(os.path.join(self.data, "human-parties.json"), "w", encoding="utf-8") as f:
            json.dump([{"human": "Ayla", "members": ["Zendancer84", "xXRinfangXx", "Zen.fox", "vex_x18"],
                        "alumni": ["Echobunny"]}], f)
        self.cfg = os.path.join(self.dir, "config.txt")
        with open(self.cfg, "w", encoding="utf-8") as f:
            f.write("# OpenKore\nusername player\npassword %s\n" % CFG_PW)
        self.snap = snapshot(os.path.join(self.dir, "snap-test.json.gz"))

    def run_main(self, *extra, cfg=None):
        out = io.StringIO()
        argv = ["--data", self.data, "--map", "pay_dun03", "--snapshot", self.snap,
                "--openkore-config", cfg or self.cfg] + list(extra)
        with contextlib.redirect_stdout(out), contextlib.redirect_stderr(io.StringIO()):
            rc = mig.main(argv)
        return rc, out.getvalue()

    def files(self):
        return sorted(os.listdir(self.data))

    def read(self, name):
        with open(os.path.join(self.data, name), encoding="utf-8") as f:
            return f.read()

    def test_dry_run_writes_nothing_and_hides_password(self):
        before = self.files()
        rc, out = self.run_main()
        self.assertEqual(rc, 0)
        self.assertIn("dry run", out)
        self.assertEqual(self.files(), before)
        self.assertNotIn(CFG_PW, out)

    def test_apply_moves_party_and_adds_persona(self):
        rc, out = self.run_main("--apply")
        self.assertEqual(rc, 0)
        self.assertNotIn(CFG_PW, out)
        self.assertFalse(os.path.exists(os.path.join(self.data, "human-parties.json")))
        self.assertTrue(os.path.exists(os.path.join(self.data, "human-parties.json.pre-ayla-engine")))
        parties = json.loads(self.read("parties_engine.json"))
        self.assertEqual(len(parties), 1)
        p = parties[0]
        self.assertEqual(p["name"], "Ayla pt")
        self.assertEqual(p["leader"], "Ayla")
        self.assertEqual(p["members"], ["Ayla", "Zendancer84", "xXRinfangXx", "Zen.fox", "vex_x18"])
        self.assertEqual(p["map"], "pay_dun03")
        self.assertTrue(p["engine"])
        personas = json.loads(self.read("personas.json"))
        ayla = [x for x in personas if x["account_id"] == 2000000]
        self.assertEqual(len(ayla), 1)
        self.assertEqual(ayla[0]["char_id"], 150010)
        self.assertEqual(ayla[0]["build"], "assassin_sonic")
        self.assertEqual(ayla[0]["account"], "player")
        self.assertEqual(ayla[0]["password"], "")
        self.assertEqual(ayla[0]["index"], 3)
        self.assertNotIn(CFG_PW, self.read("personas.json"))
        self.assertEqual(json.loads(self.read("logins.local.json")), {"2000000": CFG_PW})
        self.assertTrue(os.path.exists(os.path.join(self.data, "personas.json.pre-ayla-engine")))

    def test_persona_has_no_stray_fields(self):
        personas = json.loads(self.read("personas.json"))
        personas[0]["stray_field"] = "must not be copied"
        with open(os.path.join(self.data, "personas.json"), "w", encoding="utf-8") as f:
            json.dump(personas, f)
        self.run_main("--apply")
        ayla = [x for x in json.loads(self.read("personas.json")) if x["account_id"] == 2000000][0]
        self.assertNotIn("stray_field", ayla)
        self.assertEqual(set(ayla), set(mig.persona_entry([], self._args()).keys()))

    def _args(self):
        class A:
            userid = "player"
            password = CFG_PW
        return A()

    def test_second_run_changes_nothing(self):
        self.run_main("--apply")
        personas_before = self.read("personas.json")
        rc, out = self.run_main("--apply")
        self.assertEqual(rc, 0)
        self.assertIn("already migrated", out)
        self.assertEqual(self.read("personas.json"), personas_before)
        self.assertEqual(len(json.loads(self.read("parties_engine.json"))), 1)

    def test_failure_before_rename_leaves_human_parties(self):
        # the engine party file cannot be written: the rename (last) must not have happened
        os.makedirs(os.path.join(self.data, "parties_engine.json.tmp"))
        with self.assertRaises(OSError):
            self.run_main("--apply")
        self.assertTrue(os.path.exists(os.path.join(self.data, "human-parties.json")))
        self.assertFalse(os.path.exists(os.path.join(self.data, "human-parties.json.pre-ayla-engine")))
        os.rmdir(os.path.join(self.data, "parties_engine.json.tmp"))
        rc, _ = self.run_main("--apply")   # a re-run completes the job
        self.assertEqual(rc, 0)
        self.assertTrue(os.path.exists(os.path.join(self.data, "human-parties.json.pre-ayla-engine")))

    def test_refuses_config_without_username(self):
        with open(self.cfg, "w", encoding="utf-8") as f:
            f.write("password %s\n" % CFG_PW)
        rc, _ = self.run_main("--apply")
        self.assertEqual(rc, 2)
        self.assertEqual(self.files(), sorted(["human-parties.json", "personas.json"]))

    def test_userid_override_allows_missing_username(self):
        with open(self.cfg, "w", encoding="utf-8") as f:
            f.write("password %s\n" % CFG_PW)
        rc, _ = self.run_main("--apply", "--userid", "player")
        self.assertEqual(rc, 0)

    def test_password_override_wins(self):
        rc, out = self.run_main("--apply", "--password", "OVERRIDE-1")
        self.assertEqual(rc, 0)
        self.assertNotIn("OVERRIDE-1", out)
        self.assertEqual(json.loads(self.read("logins.local.json")), {"2000000": "OVERRIDE-1"})

    def test_no_password_anywhere_refuses(self):
        with open(self.cfg, "w", encoding="utf-8") as f:
            f.write("username player\n")
        rc, _ = self.run_main("--apply")
        self.assertEqual(rc, 2)
        self.assertTrue(os.path.exists(os.path.join(self.data, "human-parties.json")))

    def test_preflight_refuses_wrong_party_name(self):
        snapshot(self.snap, party_name="Ayla party")
        rc, _ = self.run_main("--apply")
        self.assertEqual(rc, 2)
        self.assertTrue(os.path.exists(os.path.join(self.data, "human-parties.json")))
        self.assertNotIn("parties_engine.json", self.files())

    def test_preflight_refuses_missing_roster_member(self):
        snapshot(self.snap, members=("Ayla", "Zendancer84", "xXRinfangXx", "vex_x18"))  # Zen.fox left
        rc, _ = self.run_main("--apply")
        self.assertEqual(rc, 2)
        self.assertNotIn("parties_engine.json", self.files())

    def test_refuses_other_humans(self):
        with open(os.path.join(self.data, "human-parties.json"), "w", encoding="utf-8") as f:
            json.dump([{"human": "Ayla", "members": []}, {"human": "Someone", "members": []}], f)
        rc, _ = self.run_main("--apply")
        self.assertEqual(rc, 2)
        self.assertTrue(os.path.exists(os.path.join(self.data, "human-parties.json")))
        self.assertNotIn("parties_engine.json", self.files())

    def test_never_overwrites_kept_file(self):
        with open(os.path.join(self.data, "human-parties.json.pre-ayla-engine"), "w", encoding="utf-8") as f:
            f.write("[]")
        rc, _ = self.run_main("--apply")
        self.assertEqual(rc, 2)
        self.assertTrue(os.path.exists(os.path.join(self.data, "human-parties.json")))


if __name__ == "__main__":
    unittest.main()
