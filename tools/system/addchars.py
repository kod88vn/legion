"""addchars.py - additive insert of farmer vendor (merchant-line) characters (design section 2.1, WP4).

Reads farmers.json (the `vendor_adds` list written by farmerpick.py) and the character dump the selector used, and
builds, per account, a level-1 Novice merchant character in a free slot: the same rows as tools/pairchars.py (shared
builder tools/system/charrows.py, which matches legion/persona.go seedSQL) and the same personas.json entry.

Rules enforced here, not left to the caller:
  * INSERT only. guard_sql() accepts only single `INSERT INTO <table> (` statements for the tables in
    charrows.TABLE_ALLOW (char, inventory, char_reg_num) and refuses DELETE, UPDATE, REPLACE, DROP, ALTER, TRUNCATE,
    GRANT, RENAME, SELECT and any second statement. There is no DELETE or UPDATE path.
  * Account ids only in the bot range 2100001-2199999 (rule G06). Accounts outside it are refused.
  * legion_first / legion_branch come from the plan (charrows.PLANS, copied from legion/builds.go), never a literal.
  * Refused per account: no login row in the dump, a guild member, an existing merchant-line character, a pinned
    human party member, a taken slot / char_id / name.
  * Dry run is the default: prints what would be inserted and writes nothing.
  * --apply needs one target:
      --copy DIR  offline copy: writes DIR/addchars-*.sql and DIR/personas.json (the updated copy). The live
                  files are not touched. DIR inside the live legion/data is refused (rule G23).
      --live      ro-db (docker exec, ops only, owner order) and the live personas.json (backup first, atomic
                  write). Never used by tests; the tests replace run_live.
  * --pinned (a JSON file of human party members) is required with --live. With --copy it is optional.

  python tools/system/addchars.py --dump CHARS.tsv --pinned PINNED.json                   # dry run (default)
  python tools/system/addchars.py --dump CHARS.tsv --apply --copy DIR                     # offline copy
  python tools/system/addchars.py --dump CHARS.tsv --apply --live --pinned PINNED.json    # live (ops only)
"""
import json
import os
import re
import subprocess
import sys
import time

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
import common as _C  # noqa: E402  (rule G23: the live-folder guard)
import charrows as CR  # noqa: E402  (one seed-row builder, shared with pairchars.py)
import farmerpick as FP  # noqa: E402

FORBIDDEN = ("DELETE", "UPDATE", "REPLACE", "DROP", "ALTER", "TRUNCATE", "GRANT", "RENAME", "SELECT", "CALL", "SET")
VENDOR_PLANS = ("blacksmith", "alchemist")   # merchant line only (no hunter plan is ever added here)


class Refused(Exception):
    pass


def check_add_account(aid):
    """Refuse any account id outside the bot range (rule G06)."""
    if not FP.in_bot_range(aid):
        raise Refused("account %s is outside the bot range %d-%d" % (aid, FP.BOT_MIN, FP.BOT_MAX))


def guard_sql(stmts):
    """Every statement must be one INSERT INTO <allowed table> (...); anything else raises. Quoted text (names) is
    not scanned for keywords. Returns the list."""
    for s in stmts:
        body = s.strip()
        if not body.upper().startswith("INSERT INTO "):
            raise Refused("statement is not an INSERT: %s" % body[:80])
        table = CR.table_of(body)
        if table is None or table not in CR.TABLE_ALLOW:
            raise Refused("table not allowed in %r" % body[:80])
        bare = re.sub(r"'(?:[^'\\]|\\.|'')*'", "''", body).replace("`", " ")
        if bare.count(";") > 1 or ";" in bare[:-1]:
            raise Refused("more than one statement in one string")
        for word in FORBIDDEN:
            if re.search(r"\b%s\b" % word, bare.upper()):
                raise Refused("forbidden word %s in statement" % word)
    return stmts


def plan_rows(spec, persona):
    """The three seed statements for one vendor spec. The first/branch values come from the plan table."""
    if spec["plan"] not in VENDOR_PLANS:
        raise Refused("plan %s is not a merchant-line plan" % spec["plan"])
    first, branch, _ = CR.plan_first_branch(spec["plan"])
    town = persona.get("home_town") or "prontera"
    if town not in CR.TOWNS:
        town = "prontera"
    # the rng seed is the account id, so a re-run gives the same rows (pairchars uses its own seeded rng)
    import random
    rng = random.Random(spec["account_id"])
    return guard_sql(CR.seed_rows(spec["char_id"], spec["account_id"], spec["slot"], spec["name"], town,
                                  persona.get("sex", "M"), rng, first, branch))


def plan(doc, dump_rows, personas_by_acc, pinned_names=()):
    """Returns (statements, report, personas_updates). Refuses per account; never raises for one account.
    personas_updates is a list of (account_id, character dict) for the personas.json entry."""
    pinned = set(x.lower() for x in pinned_names)
    by_acc = FP._index(dump_rows)
    used_ids = set(r["char_id"] for r in dump_rows)
    taken = set(r["name"].lower() for r in dump_rows)
    stmts, updates, ok, refused = [], [], [], []
    for v in doc.get("vendor_adds", []):
        aid = v.get("account_id")
        try:
            check_add_account(aid)
            rows = by_acc.get(aid)
            if not rows:
                raise Refused("no login row in the dump")
            if any(r["guild_id"] for r in rows):
                raise Refused("account is in a guild")
            p = personas_by_acc.get(aid)
            if p is None:
                raise Refused("no persona for this account (the engine would not play the character)")
            if any(FP.is_vendor(c) for c in p.get("characters", [])):
                raise Refused("account already has a merchant-line character")
            if any(c["name"].lower() in pinned for c in p.get("characters", [])):
                raise Refused("pinned human party member")
            if any(r["char_num"] == v["slot"] for r in rows):
                raise Refused("slot %d is taken" % v["slot"])
            if v["char_id"] in used_ids:
                raise Refused("char_id %d is taken" % v["char_id"])
            if v["name"].lower() in taken:
                raise Refused("name %s is taken" % v["name"])
            spec = dict(v, account_id=aid)
            stmts += plan_rows(spec, p)
            updates.append((aid, {"char_id": v["char_id"], "slot": v["slot"], "name": v["name"], "role": "vendor",
                                  "class": 0, "class_name": "Novice", "build": v.get("build", "str-dex"),
                                  "base_level": 1, "job_level": 1, "plan": v["plan"],
                                  "target_class": CR.plan_first_branch(v["plan"])[2]}))
            used_ids.add(v["char_id"])
            taken.add(v["name"].lower())
            ok.append(v["account"])
        except Refused as e:
            refused.append((v.get("account"), str(e)))
    return guard_sql(stmts), {"ok": ok, "refused": refused, "statements": len(stmts)}, updates


def apply_personas(personas, updates):
    """Adds the vendor entries to the personas list (in memory). Returns the list."""
    byid = {p["account_id"]: p for p in personas}
    for aid, c in updates:
        byid[aid].setdefault("characters", []).append(c)
    return personas


def write_json_atomic(path, data):
    tmp = path + ".tmp"
    with open(tmp, "w", encoding="utf-8") as f:
        json.dump(data, f, ensure_ascii=False, indent=2)
    os.replace(tmp, path)


def run_live(stmts):
    """Execute on ro-db (ops only, owner order; never called by tests). Same route as tools/pairchars.py."""
    sql = "\n".join(stmts) + "\n"
    p = subprocess.run(["wsl", "-d", "Ubuntu-24.04", "--", "docker", "exec", "-i", "ro-db", "mariadb", "-N",
                        "-uragnarok", "-pragnarok", "ragnarok"], input=sql.encode("utf-8"), capture_output=True)
    if p.returncode != 0:
        raise SystemExit("db error: " + p.stderr.decode("utf-8", "replace")[:500])
    return len(stmts)


def copy_dir_safe(copy_dir):
    d = os.path.abspath(copy_dir)
    live = os.path.abspath(_C.LIVE_DATA)
    if d == live or d.startswith(live + os.sep):
        raise SystemExit("refused: --copy must not be the live data folder (rule G23)")
    os.makedirs(d, exist_ok=True)
    return d


def main(argv=None):
    import argparse
    ap = argparse.ArgumentParser(description="Additive INSERT of farmer vendor characters. Dry run unless --apply.")
    ap.add_argument("--farmers", default=None, help="farmers.json (default: the data folder)")
    ap.add_argument("--dump", required=True, help="TSV dump of the char table (read-only copy)")
    ap.add_argument("--personas", default=None, help="personas.json (default: the data folder)")
    ap.add_argument("--pinned", default=None, help="JSON of human party members (required with --live)")
    ap.add_argument("--apply", action="store_true")
    ap.add_argument("--copy", default=None, help="offline copy folder: scripts and personas copy are written there")
    ap.add_argument("--live", action="store_true", help="run on ro-db and update the live personas.json (ops only)")
    a = ap.parse_args(argv)
    if a.live and a.copy:
        raise SystemExit("choose one target: --copy DIR or --live")
    if a.live and not a.pinned:
        raise SystemExit("--live needs --pinned (the human party list, rule G06/G25)")
    with open(a.farmers or _C.dpath("farmers.json"), encoding="utf-8") as f:
        doc = json.load(f)
    errs = FP.validate_farmers(doc)
    if errs:
        raise SystemExit("refused, invalid farmers.json: " + "; ".join(errs[:5]))
    pers_path = a.personas or _C.dpath("personas.json")
    personas = FP.load_personas(pers_path)
    by_acc = {p["account_id"]: p for p in personas}
    pinned = FP.load_pinned(a.pinned) if a.pinned else set()
    stmts, rep, updates = plan(doc, FP.load_dump(a.dump), by_acc, pinned)
    print("planned: %d vendor characters, %d statements; refused %d" % (
        len(rep["ok"]), rep["statements"], len(rep["refused"])))
    for acc, why in rep["refused"]:
        print("  refused %s: %s" % (acc, why))
    if not a.apply:
        for s in stmts[:3]:
            print("  " + s[:160])
        print("dry run: nothing written (use --apply --copy DIR, or --apply --live --pinned FILE)")
        return 0
    if not (a.live or a.copy):
        raise SystemExit("--apply needs --copy DIR (offline copy) or --live")
    if not stmts:
        print("nothing to apply")
        return 0
    if a.live:
        n = run_live(stmts)                       # SQL first, as pairchars does
        bak = pers_path + ".bak-addchars-" + time.strftime("%Y%m%d-%H%M%S")
        with open(pers_path, encoding="utf-8") as f:
            raw = f.read()
        with open(bak, "w", encoding="utf-8") as f:
            f.write(raw)
        write_json_atomic(pers_path, apply_personas(personas, updates))
        print("applied %d statements to ro-db; personas.json updated (backup %s)" % (n, os.path.basename(bak)))
    else:
        d = copy_dir_safe(a.copy)
        path = os.path.join(d, "addchars-%s.sql" % time.strftime("%Y%m%d-%H%M%S"))
        with open(path, "w", encoding="utf-8") as f:
            f.write("\n".join(stmts) + "\n")
        write_json_atomic(os.path.join(d, "personas.json"), apply_personas(personas, updates))
        print("wrote offline copy: %s and %s" % (path, os.path.join(d, "personas.json")))
    return 0


if __name__ == "__main__":
    sys.exit(main())
