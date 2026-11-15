#!/usr/bin/env python3
"""One-time cut-over step for Ayla on the engine (AY24, package C of the 2026-11-15 redesign).

Dry run by default (prints the plan). With --apply, in the data directory (legion/data):
  1. logins.local.json (git-ignored): Ayla's password, keyed by account id 2000000.
  2. personas.json: Ayla's persona is appended (account 2000000, char 150010, build assassin_sonic). The old file is
     copied to personas.json.pre-ayla-engine first. Fields are written explicitly, nothing is copied from another
     persona. The password field stays empty (personas.json is tracked by git).
  3. parties_engine.json: Ayla's party as an engine party (bot-led, her bot as Leader, roster from
     human-parties.json). Refuses to overwrite a file that already holds other parties.
  4. human-parties.json is RENAMED to human-parties.json.pre-ayla-engine, LAST, so a failure in steps 1-3 leaves it in
     place and a re-run completes the job. It is never deleted; a rename onto an existing .pre-ayla-engine is refused.

Login: username and password come from Ayla's OpenKore config (openkore-ayla/control/config.txt). --password and
--userid override them. The password is never printed. A config with no username and no --userid is refused.

Pre-flight (read-only, from a DB snapshot file, never a live connection): the in-game party of char 150010 must be
named exactly "Ayla pt", and the roster of human-parties.json (Zen.fox included) must all be in it. Otherwise refused.

Refusals (nothing written): human-parties.json names another human; a required value is missing (--map, a login); the
pre-flight fails; parties_engine.json holds other parties; the kept .pre-ayla-engine file exists.

Run once at cut-over, by the deploy agent, with legion and legion-core stopped and ro-openkore-ayla stopped (G15, G17,
G19). A re-run after success changes nothing.

usage: python tools/system/ayla_engine_migrate.py --data legion/data --map MAP [--apply]
           [--snapshot FILE.json.gz] [--userid U] [--password P] [--openkore-config FILE]
"""
import argparse
import datetime
import glob
import gzip
import json
import os
import shutil
import sys

ACCOUNT_ID = 2000000   # Ayla's account in the DB (read-only snapshot, 2026-11-15); outside the bot range 2100001-2199999
CHAR_ID = 150010       # Ayla's character
CHAR_NAME = "Ayla"
PARTY_NAME = "Ayla pt"
BUILD = "assassin_sonic"
CLASS_ID, CLASS_NAME = 12, "Assassin"
PRE = ".pre-ayla-engine"
SECRETS = "logins.local.json"
REPO = os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "..")
OPENKORE_CONFIG = os.path.join(REPO, "openkore-ayla", "control", "config.txt")
SNAPSHOT_DIR = os.path.join(REPO, "tools", ".state", "dbsnap")
GZ = "snap-*.json.gz"


def clean_party_name(s):
    """Same rule as partypersist.go cleanPartyName: ASCII letters, digits and single spaces only."""
    out = "".join(c for c in s if (c.isascii() and c.isalnum()) or c == " ")
    return " ".join(out.split())


def read_openkore_login(path):
    """(username, password) from an OpenKore config.txt ('key value' lines). Values are never printed."""
    user = pw = ""
    try:
        with open(path, encoding="utf-8", errors="replace") as f:
            for line in f:
                line = line.strip()
                if not line or line.startswith("#"):
                    continue
                key, _, val = line.partition(" ")
                if key == "username":
                    user = val.strip()
                elif key == "password":
                    pw = val.strip()
    except OSError:
        return "", ""
    return user, pw


def write_json_atomic(path, obj):
    tmp = path + ".tmp"
    with open(tmp, "w", encoding="utf-8") as f:
        json.dump(obj, f, indent=2, ensure_ascii=False)
        f.write("\n")
        f.flush()
        os.fsync(f.fileno())
    os.replace(tmp, path)


def write_secret(data, account_id, password):
    path = os.path.join(data, SECRETS)
    secrets = {}
    if os.path.exists(path):
        with open(path, encoding="utf-8") as f:
            secrets = json.load(f)
    secrets[str(account_id)] = password
    write_json_atomic(path, secrets)
    try:
        os.chmod(path, 0o600)
    except OSError:
        pass


def preflight(snapshot_path, roster):
    """(ok, reason). Reads the DB snapshot only: the party of char 150010 and its members."""
    with gzip.open(snapshot_path, "rt", encoding="utf-8") as f:
        tables = json.load(f)["tables"]
    char, party = tables["char"], tables["party"]
    ci = {c: i for i, c in enumerate(char["cols"])}
    pi = {c: i for i, c in enumerate(party["cols"])}
    ayla = [r for r in char["rows"] if r[ci["char_id"]] == CHAR_ID]
    if not ayla:
        return False, "char %d not in the snapshot" % CHAR_ID
    pid = ayla[0][ci["party_id"]]
    rows = [r for r in party["rows"] if r[pi["party_id"]] == pid]
    if not pid or not rows:
        return False, "char %d is in no in-game party" % CHAR_ID
    if rows[0][pi["name"]] != PARTY_NAME:
        return False, "in-game party is %r, not %r" % (rows[0][pi["name"]], PARTY_NAME)
    members = {r[ci["name"]].lower() for r in char["rows"] if r[ci["party_id"]] == pid}
    missing = [n for n in roster if n.lower() not in members]
    if missing or CHAR_NAME.lower() not in members:
        return False, "roster not in the in-game party: %s" % ", ".join(missing or [CHAR_NAME])
    return True, "in-game party %r has the roster" % PARTY_NAME


def newest_snapshot(path):
    if path:
        return path
    found = sorted(glob.glob(os.path.join(SNAPSHOT_DIR, GZ)))
    return found[-1] if found else ""


def plan(data, args):
    """Return (actions, error). Nothing is written here."""
    actions = []
    hp_path = os.path.join(data, "human-parties.json")
    pre_path = hp_path + PRE
    personas_path = os.path.join(data, "personas.json")
    engine_path = os.path.join(data, "parties_engine.json")
    human = args.human.lower()

    entry = None
    if os.path.exists(hp_path):
        with open(hp_path, encoding="utf-8") as f:
            saved = json.load(f)
        others = [e.get("human") for e in saved if (e.get("human") or "").lower() != human]
        if others:
            return [], "human-parties.json names other humans %s: not migrated, move them by hand" % others
        mine = [e for e in saved if (e.get("human") or "").lower() == human]
        if len(mine) > 1:
            return [], "human-parties.json has %d entries for %s: not migrated" % (len(mine), args.human)
        entry = mine[0] if mine else None
        if os.path.exists(pre_path):
            return [], "%s exists: refusing to overwrite the kept file" % pre_path
    elif os.path.exists(pre_path):
        actions.append(("done", "human-parties.json already migrated (%s kept)" % pre_path))
    else:
        actions.append(("skip", "no human-parties.json: nothing to rename"))

    if entry is not None:
        if not args.map:
            return [], "--map is required: the map her party is restored on"
        roster = [args.human] + [n for n in entry.get("members", []) if n.lower() != human]
        snap = newest_snapshot(args.snapshot)
        if not snap or not os.path.exists(snap):
            return [], "no DB snapshot (tools/.state/dbsnap or --snapshot): the pre-flight cannot run"
        ok, why = preflight(snap, roster)
        if not ok:
            return [], "pre-flight refused: " + why
        actions.append(("preflight", why))
        members = [args.human] + [n for n in roster if n.lower() != human]
        party = {"name": clean_party_name(args.human) + " pt", "leader": args.human, "members": members,
                 "map": args.map, "formed": datetime.datetime.now(datetime.timezone.utc).isoformat(), "engine": True}
        existing = []
        if os.path.exists(engine_path):
            with open(engine_path, encoding="utf-8") as f:
                existing = json.load(f) or []
        if any(p.get("name") == party["name"] for p in existing):
            actions.append(("done", "%s already holds %s" % (engine_path, party["name"])))
        elif existing:
            return [], "%s holds other parties: not written" % engine_path
        else:
            actions.append(("write_engine", engine_path, [party]))

    if not os.path.exists(personas_path):
        return [], "no personas.json in %s" % data
    with open(personas_path, encoding="utf-8") as f:
        personas = json.load(f)
    if any(p.get("account_id") == ACCOUNT_ID or
           any((c.get("name") or "").lower() == CHAR_NAME.lower() for c in p.get("characters", []))
           for p in personas):
        actions.append(("done", "personas.json already has %s" % CHAR_NAME))
    else:
        if not args.password:
            return [], "no password: the OpenKore config has none (or pass --password)"
        if not args.userid:
            return [], "no login name: the OpenKore config has no username (or pass --userid)"
        actions.append(("add_persona", personas_path))

    if entry is not None:
        actions.append(("rename", hp_path, pre_path))   # last: a failure before this leaves human-parties.json
    return actions, None


def persona_entry(personas, args):
    """A complete persona in the shape of a generated one; every field is set here, none is copied."""
    return {
        "index": max([x.get("index", 0) for x in personas] + [0]) + 1,
        "account": args.userid,
        "password": "",   # the password is in logins.local.json (git-ignored)
        "account_id": ACCOUNT_ID,
        "char_id": CHAR_ID,
        "name": CHAR_NAME,
        "sex": "F",
        "archetype": "engine test subject",
        "bio": "Ayla, the Legion's test subject. Plays the same engine as every bot.",
        "age": 25,
        "country": "",
        "tz_offset_hours": 0,
        "aggression": 0.5, "risk": 0.5, "chattiness": 0.5, "patience": 0.5, "greed": 0.5,
        "sociability": 0.5, "helpfulness": 0.5, "loyalty": 0.5, "skill": 0.5,
        "typing_style": "normal",
        "reaction_ms": 400,
        "session_start_hour": 18,
        "session_hours": 4,
        "weekend_boost": 0.5,
        "afk_chance": 0.02,
        "class": CLASS_ID,
        "class_name": CLASS_NAME,
        "build": BUILD,
        "base_level": 84,
        "job_level": 40,
        "guild": "",
        "home_town": "payon",
        "characters": [{"char_id": CHAR_ID, "slot": 2, "name": CHAR_NAME, "role": "main", "class": CLASS_ID,
                        "class_name": CLASS_NAME, "build": BUILD, "base_level": 84, "job_level": 40,
                        "plan": BUILD, "target_class": CLASS_ID}],
    }


def describe(a):
    kind = a[0]
    if kind == "rename":
        return "rename %s -> %s" % (a[1], a[2])
    if kind == "write_engine":
        return "write %s (engine party %s)" % (a[1], a[2][0]["name"])
    if kind == "add_persona":
        return "add persona %s to %s (copy kept as %s); login secret to logins.local.json" % (
            CHAR_NAME, a[1], a[1] + PRE)
    return a[1]


def apply(actions, args):
    for a in actions:
        if a[0] == "add_persona":
            write_secret(args.data, ACCOUNT_ID, args.password)
            path = a[1]
            with open(path, encoding="utf-8") as f:
                personas = json.load(f)
            shutil.copy2(path, path + PRE)
            personas.append(persona_entry(personas, args))
            write_json_atomic(path, personas)
        elif a[0] == "write_engine":
            write_json_atomic(a[1], a[2])
        elif a[0] == "rename":
            os.rename(a[1], a[2])
        print("done: " + describe(a))


def main(argv=None):
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--data", required=True, help="the legion data directory (legion/data)")
    ap.add_argument("--human", default="Ayla", help="the human whose party moves to the engine")
    ap.add_argument("--map", default="", help="the map her party is restored on (her last map, from the DB)")
    ap.add_argument("--snapshot", default="", help="DB snapshot for the pre-flight (default: newest in tools/.state/dbsnap)")
    ap.add_argument("--userid", default="", help="override: Ayla's login name (default: username in her OpenKore config)")
    ap.add_argument("--password", default="", help="override: Ayla's password (default: her OpenKore config)")
    ap.add_argument("--openkore-config", default=OPENKORE_CONFIG, help="Ayla's OpenKore config.txt")
    ap.add_argument("--apply", action="store_true", help="write (default: dry run)")
    args = ap.parse_args(argv)
    if not args.password or not args.userid:
        user, pw = read_openkore_login(args.openkore_config)
        args.password = args.password or pw
        args.userid = args.userid or user
    actions, err = plan(args.data, args)
    if err:
        print("REFUSED: " + err, file=sys.stderr)
        return 2
    for a in actions:
        print(("plan: " if not args.apply else "") + describe(a))
    if args.apply:
        apply(actions, args)
    else:
        print("dry run: nothing written (add --apply)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
