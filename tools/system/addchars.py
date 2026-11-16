"""addchars.py - additive insert of farmer vendor (merchant-line) characters (design section 2.1, WP4).

Reads farmers.json (the `vendor_adds` list written by farmerpick.py) and the character dump the selector used, and
builds INSERT statements for a level-1 Novice merchant character (plan blacksmith) in a free slot: the char row,
the starter kit and the legion_first / legion_branch registers, the same rows as tools/pairchars.py.

Rules enforced here, not left to the caller:
  * INSERT only. Every statement is checked by guard_sql(): anything that is not an INSERT INTO, or that names
    DELETE, UPDATE, REPLACE, DROP, ALTER or TRUNCATE, raises. There is no DELETE and no UPDATE path at all.
  * Account ids only in ADD_MIN..ADD_MAX (design 2.1 says 2104001-2199999; the bot range of rule G06 is
    2100001-2199999, and the account must also be in it). Any other id is refused.
  * Refused per account when: the dump has no such login, the account already has a merchant-line character,
    the slot, char_id or name is already used in the dump or in the planned rows, or the account is in a guild.
  * Dry run is the default: prints what would be inserted and writes nothing.
  * --apply needs a target: --copy DIR writes the script to DIR (an offline copy; ops loads it into a scratch copy
    with mariadb); --live executes it on ro-db. Tests never pass --live (rule G23). --copy DIR is refused when DIR
    is the live data folder.

  python tools/system/addchars.py --dump CHARS.tsv                       # dry run (default)
  python tools/system/addchars.py --dump CHARS.tsv --apply --copy DIR    # offline copy: writes DIR/addchars-*.sql
  python tools/system/addchars.py --dump CHARS.tsv --apply --live        # the live database (ops only, owner order)
"""
import json
import re
import os
import subprocess
import sys
import time

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
import common as _C  # noqa: E402  (rule G23: the live-folder guard)
import farmerpick as FP  # noqa: E402

ADD_MIN, ADD_MAX = 2104001, 2199999   # design 2.1: vendor additions only in this part of the bot range
FORBIDDEN = ("DELETE", "UPDATE", "REPLACE", "DROP", "ALTER", "TRUNCATE", "GRANT", "RENAME")
TOWNS = {  # town -> spawn cell (as tools/pairchars.py)
    "prontera": (156, 191), "izlude": (128, 114), "payon": (162, 233), "geffen": (119, 59), "alberta": (116, 57),
}


class Refused(Exception):
    pass


def check_add_account(aid):
    """Refuse any account id outside the add range (and therefore outside the bot range)."""
    if not (ADD_MIN <= aid <= ADD_MAX) or not FP.in_bot_range(aid):
        raise Refused("account %s is outside the add range %d-%d" % (aid, ADD_MIN, ADD_MAX))


def guard_sql(stmts):
    """Every statement must be a single INSERT INTO; anything else raises. Quoted text (names) is not scanned.
    Returns the list."""
    for s in stmts:
        body = s.strip()
        if not body.upper().startswith("INSERT INTO "):
            raise Refused("statement is not an INSERT: %s" % body[:80])
        bare = re.sub(r"'(?:[^'\\]|\\.|'')*'", "''", body).replace("`", " ")
        if bare.count(";") > 1 or ";" in bare[:-1]:
            raise Refused("more than one statement in one string")
        for word in FORBIDDEN:
            if re.search(r"\b%s\b" % word, bare.upper()):
                raise Refused("forbidden word %s in statement" % word)
    return stmts


def _esc(s):
    return s.replace("\\", "\\\\").replace("'", "''")


def char_statements(spec, persona, rng_seed):
    """The INSERT rows for one vendor character (char, inventory, char_reg_num), same shape as pairchars.py."""
    town = persona.get("home_town") or "prontera"
    if town not in TOWNS:
        town = "prontera"
    tx, ty = TOWNS[town]
    sex = persona.get("sex", "M")
    seed = (rng_seed * 2654435761) & 0xFFFFFFFF
    def pick(n):
        nonlocal seed
        seed = (seed * 1103515245 + 12345) & 0x7FFFFFFF
        return seed % n
    cid, aid = spec["char_id"], spec["account_id"]
    out = [
        "INSERT INTO `char` (char_id,account_id,char_num,name,class,base_level,job_level,str,agi,vit,`int`,dex,luk,"
        "max_hp,hp,max_sp,sp,status_point,skill_point,hair,hair_color,clothes_color,zeny,weapon,last_map,last_x,"
        "last_y,save_map,save_x,save_y,sex,`option`) VALUES (%d,%d,%d,'%s',0,1,1,1,1,1,1,1,1,40,40,11,11,48,0,"
        "%d,%d,%d,%d,1201,'%s',%d,%d,'%s',%d,%d,'%s',0);" % (
            cid, aid, spec["slot"], _esc(spec["name"]), 1 + pick(22), pick(9), pick(4), 1500 + pick(1500),
            town, tx, ty, town, tx, ty, sex),
        "INSERT INTO inventory (char_id,nameid,amount,equip,identify) VALUES (%d,1201,1,2,1),(%d,2301,1,16,1),"
        "(%d,569,%d,0,1),(%d,11500,%d,0,1),(%d,501,%d,0,1),(%d,601,%d,0,1),(%d,602,%d,0,1);" % (
            cid, cid, cid, 120 + pick(60), cid, 10 + pick(10), cid, 10 + pick(10), cid, 5 + pick(5), cid, 2 + pick(2)),
        "INSERT INTO char_reg_num (char_id,`key`,`index`,value) VALUES (%d,'legion_first',0,3),"
        "(%d,'legion_branch',0,0);" % (cid, cid),
    ]
    return guard_sql(out)


def plan(doc, dump_rows, personas_by_acc):
    """Returns (statements, report). Refuses per account (report lists refusals); never raises for one account."""
    by_acc = FP._index(dump_rows)
    used_ids = set(r["char_id"] for r in dump_rows)
    taken = set(r["name"].lower() for r in dump_rows)
    stmts, ok, refused = [], [], []
    for v in doc.get("vendor_adds", []):
        aid = v["account_id"]
        try:
            check_add_account(aid)
            rows = by_acc.get(aid)
            if not rows:
                raise Refused("no login row in the dump")
            if any(r["guild_id"] for r in rows):
                raise Refused("account is in a guild")
            p = personas_by_acc.get(aid, {"characters": [], "home_town": "prontera"})
            if any(FP.is_vendor(c) for c in p.get("characters", [])):
                raise Refused("account already has a merchant-line character")
            if any(r["char_num"] == v["slot"] for r in rows):
                raise Refused("slot %d is taken" % v["slot"])
            if v["char_id"] in used_ids:
                raise Refused("char_id %d is taken" % v["char_id"])
            if v["name"].lower() in taken:
                raise Refused("name %s is taken" % v["name"])
            spec = dict(v, account_id=aid)
            stmts += char_statements(spec, p, rng_seed=aid)
            used_ids.add(v["char_id"])
            taken.add(v["name"].lower())
            ok.append(v["account"])
        except Refused as e:
            refused.append((v.get("account"), str(e)))
    return guard_sql(stmts), {"ok": ok, "refused": refused, "statements": len(stmts)}


def run_live(stmts):
    """Execute on ro-db (ops only, owner order; never called by tests). Same route as tools/pairchars.py."""
    sql = "\n".join(stmts) + "\n"
    p = subprocess.run(["wsl", "-d", "Ubuntu-24.04", "--", "docker", "exec", "-i", "ro-db", "mariadb", "-N",
                        "-uragnarok", "-pragnarok", "ragnarok"], input=sql.encode("utf-8"), capture_output=True)
    if p.returncode != 0:
        raise SystemExit("db error: " + p.stderr.decode("utf-8", "replace")[:500])
    return len(stmts)


def write_copy(stmts, copy_dir):
    """Offline copy: the script goes into copy_dir. Refused when copy_dir is the live data folder (G23)."""
    d = os.path.abspath(copy_dir)
    live = os.path.abspath(_C.LIVE_DATA)
    if d == live or d.startswith(live + os.sep):
        raise SystemExit("refused: --copy must not be the live data folder (rule G23)")
    os.makedirs(d, exist_ok=True)
    path = os.path.join(d, "addchars-%s.sql" % time.strftime("%Y%m%d-%H%M%S"))
    with open(path, "w", encoding="utf-8") as f:
        f.write("\n".join(stmts) + "\n")
    return path


def main(argv=None):
    import argparse
    ap = argparse.ArgumentParser(description="Additive INSERT of farmer vendor characters. Dry run unless --apply.")
    ap.add_argument("--farmers", default=None, help="farmers.json (default: the data folder)")
    ap.add_argument("--dump", required=True, help="TSV dump of the char table (read-only copy)")
    ap.add_argument("--personas", default=None, help="personas.json (default: the data folder)")
    ap.add_argument("--apply", action="store_true")
    ap.add_argument("--copy", default=None, help="offline copy folder: the script is written there")
    ap.add_argument("--live", action="store_true", help="run on ro-db (ops only)")
    a = ap.parse_args(argv)
    with open(a.farmers or _C.dpath("farmers.json"), encoding="utf-8") as f:
        doc = json.load(f)
    errs = FP.validate_farmers(doc)
    if errs:
        raise SystemExit("refused, invalid farmers.json: " + "; ".join(errs[:5]))
    personas = FP.load_personas(a.personas or _C.dpath("personas.json"))
    by_acc = {p["account_id"]: p for p in personas}
    stmts, rep = plan(doc, FP.load_dump(a.dump), by_acc)
    print("planned: %d vendor characters, %d statements; refused %d" % (
        len(rep["ok"]), rep["statements"], len(rep["refused"])))
    for acc, why in rep["refused"]:
        print("  refused %s: %s" % (acc, why))
    if not a.apply:
        for s in stmts[:3]:
            print("  " + s[:160])
        print("dry run: nothing written (use --apply --copy DIR, or --apply --live)")
        return 0
    if a.live and a.copy:
        raise SystemExit("choose one target: --copy DIR or --live")
    if a.live:
        n = run_live(stmts)
        print("applied %d statements to ro-db" % n)
    elif a.copy:
        print("wrote offline copy script: %s" % write_copy(stmts, a.copy))
    else:
        raise SystemExit("--apply needs --copy DIR (offline copy) or --live")
    return 0


if __name__ == "__main__":
    sys.exit(main())
