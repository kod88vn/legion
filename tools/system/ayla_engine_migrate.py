#!/usr/bin/env python3
"""One-time cut-over step for Ayla on the engine (AY24, package C of the 2026-11-15 redesign).

What it does in the data directory (legion/data), only with --apply (default: dry run, prints the plan):
  1. personas.json: appends Ayla's persona (account 2000000, char 150010, build assassin_sonic). The old file is copied
     to personas.json.pre-ayla-engine first. Skipped if Ayla is already in it.
  2. parties_engine.json: writes Ayla's party as an engine party (bot-led, her bot as Leader, members from
     human-parties.json). The core instance restores it at start (partypersist.go). Refuses to overwrite a file that
     already holds other parties.
  3. human-parties.json is RENAMED to human-parties.json.pre-ayla-engine. It is never deleted, and a rename onto an
     existing .pre-ayla-engine file is refused.

Login: the password is read from Ayla's OpenKore config (openkore-ayla/control/config.txt, username/password) unless
--password is given. It is never printed. It goes only to legion/data/logins.local.json (git-ignored, keyed by account
id); personas.json gets an empty password, because personas.json is tracked by git.

Safety: refuses if human-parties.json names any human other than --human (those parties stay human-led and must be
moved by hand); refuses when no password can be found; needs --map (the map her party is restored on). It does not
talk to the database at all, so account 2000000 is never touched there.
Run once at cut-over, by the deploy agent, with legion and legion-core stopped (G15, G17). A second run finds the
rename done and changes nothing.

usage: python tools/system/ayla_engine_migrate.py --data legion/data --map MAP [--apply] [--password PW]
"""
import argparse
import copy
import datetime
import json
import os
import shutil
import sys

ACCOUNT_ID = 2000000   # Ayla's account in the DB (read-only snapshot, 2026-11-15); outside the bot range 2100001-2199999
CHAR_ID = 150010       # Ayla's character
CHAR_NAME = "Ayla"
BUILD = "assassin_sonic"
CLASS_ID, CLASS_NAME = 12, "Assassin"
PRE = ".pre-ayla-engine"
OPENKORE_CONFIG = os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "..",
                               "openkore-ayla", "control", "config.txt")
SECRETS = "logins.local.json"


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


def clean_party_name(s):
    """Same rule as partypersist.go cleanPartyName: ASCII letters, digits and single spaces only."""
    out = "".join(c for c in s if (c.isascii() and c.isalnum()) or c == " ")
    return " ".join(out.split())


def write_json_atomic(path, obj):
    tmp = path + ".tmp"
    with open(tmp, "w", encoding="utf-8") as f:
        json.dump(obj, f, indent=2, ensure_ascii=False)
        f.write("\n")
        f.flush()
        os.fsync(f.fileno())
    os.replace(tmp, path)


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
        actions.append(("rename", hp_path, pre_path))
    elif os.path.exists(pre_path):
        actions.append(("done", "human-parties.json already migrated (%s kept)" % pre_path))
    else:
        actions.append(("skip", "no human-parties.json: nothing to rename"))

    if entry is not None:
        if not args.map:
            return [], "--map is required: the map her party is restored on"
        members = [args.human]
        for n in entry.get("members", []):
            if n.lower() not in [m.lower() for m in members]:
                members.append(n)
        name = clean_party_name(args.human) + " pt"
        party = {"name": name, "leader": args.human, "members": members, "map": args.map,
                 "formed": datetime.datetime.now(datetime.timezone.utc).isoformat(), "engine": True}
        existing = []
        if os.path.exists(engine_path):
            with open(engine_path, encoding="utf-8") as f:
                existing = json.load(f) or []
        if any(p.get("name") == name for p in existing):
            actions.append(("done", "%s already holds %s" % (engine_path, name)))
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
            return [], "no password: pass --password or set password in %s" % args.openkore_config
        actions.append(("add_persona", personas_path))
    return actions, None


def persona_entry(personas, args):
    p = copy.deepcopy(personas[0])   # same shape as every generated persona
    p["index"] = max(x.get("index", 0) for x in personas) + 1
    p["account"] = args.userid
    p["password"] = ""   # the password is in logins.local.json (git-ignored), see write_secret
    p["account_id"] = ACCOUNT_ID
    p["char_id"] = CHAR_ID
    p["name"] = CHAR_NAME
    p["archetype"] = "engine test subject"
    p["bio"] = "Ayla, the Legion's test subject. Plays the same engine as every bot."
    p["sex"] = "F"
    p["class"], p["class_name"] = CLASS_ID, CLASS_NAME
    p["build"] = BUILD
    p["base_level"], p["job_level"] = 84, 40
    p["guild"] = ""
    for k in ("aggression", "risk", "chattiness", "patience", "greed", "sociability", "helpfulness", "loyalty", "skill"):
        p[k] = 0.5   # neutral traits: the persona exists for the engine, not for chat
    p["characters"] = [{"char_id": CHAR_ID, "slot": 2, "name": CHAR_NAME, "role": "main", "class": CLASS_ID,
                        "class_name": CLASS_NAME, "build": BUILD, "base_level": 84, "job_level": 40,
                        "plan": BUILD, "target_class": CLASS_ID}]
    return p


def describe(a):
    kind = a[0]
    if kind == "rename":
        return "rename %s -> %s" % (a[1], a[2])
    if kind == "write_engine":
        return "write %s (engine party %s)" % (a[1], a[2][0]["name"])
    if kind == "add_persona":
        return "add persona %s to %s (copy kept as %s); login secret to %s" % (CHAR_NAME, a[1], a[1] + PRE, SECRETS)
    return a[1]


def add_persona(path, args):
    with open(path, encoding="utf-8") as f:
        personas = json.load(f)
    shutil.copy2(path, path + PRE)
    personas.append(persona_entry(personas, args))
    write_json_atomic(path, personas)


def apply(actions, args):
    for a in actions:
        if a[0] == "rename":
            os.rename(a[1], a[2])
        elif a[0] == "add_persona":
            write_secret(args.data, ACCOUNT_ID, args.password)
            add_persona(a[1], args)
        elif a[0] == "write_engine":
            write_json_atomic(a[1], a[2])
        print("done: " + describe(a))


def main(argv=None):
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--data", required=True, help="the legion data directory (legion/data)")
    ap.add_argument("--human", default="Ayla", help="the human whose party moves to the engine")
    ap.add_argument("--userid", default="", help="Ayla's login name (default: username in her OpenKore config)")
    ap.add_argument("--password", default="", help="override: Ayla's password (default: her OpenKore config)")
    ap.add_argument("--openkore-config", default=OPENKORE_CONFIG, help="Ayla's OpenKore config.txt")
    ap.add_argument("--map", default="", help="the map her party is restored on (her last map, from the DB)")
    ap.add_argument("--apply", action="store_true", help="write (default: dry run)")
    args = ap.parse_args(argv)
    if not args.password or not args.userid:
        user, pw = read_openkore_login(args.openkore_config)
        args.password = args.password or pw
        args.userid = args.userid or user or "player"
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
