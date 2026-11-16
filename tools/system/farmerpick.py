"""farmerpick.py - phase-1 farmer roster selector (design docs/design/market-engine.md, section 2.1, WP4).

Picks the farmers from personas.json and a read-only dump of the character list:
  * a guild-less account (persona guild "" and, with a dump, no guild_member row for any of its characters),
  * whose main (role main, slot 0) runs a hunter-line plan (hunter, hunter_agi, hunter_blitz, hunter_trap),
  * inside the bot range 2100001-2199999 (rule G06), never a pinned human party member.
Accounts that already own a merchant-line (vendor) character come first. Guild-less hunters without one get a
vendor character planned in a free slot (`vendor_adds`); addchars.py inserts it, additive only.

The dump is the tab-separated output of a read-only query on ro-db (mariadb -B), with this header row:
  account_id	char_id	char_num	name	class	guild_id
produced by:  SELECT c.account_id, c.char_id, c.char_num, c.name, c.class, IFNULL(g.guild_id,0) AS guild_id
              FROM `char` c LEFT JOIN guild_member g ON g.char_id = c.char_id;
Nothing here writes to a database. The only write is farmers.json (--write), a runtime file in the data folder.

  python tools/system/farmerpick.py --dump CHARS.tsv              # dry run: counts and the first rows
  python tools/system/farmerpick.py --dump CHARS.tsv --write      # writes farmers.json in the data folder
  python tools/system/farmerpick.py --personas P.json --dump X.tsv --write --out PATH
"""
import csv
import json
import os
import sys
import time
from collections import Counter

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
import common as _C  # noqa: E402  (the one data-folder resolver, rule G23)

BOT_MIN, BOT_MAX = 2100001, 2199999          # rule G06: the bot account range
HUNTER_PLANS = ("hunter", "hunter_agi", "hunter_blitz", "hunter_trap")
MERCH_PREFIX = ("blacksmith", "alchemist")  # merchant line, as tools/pairchars.py
PHASE1_TARGET = 150
SCHEMA = 1
DUMP_COLUMNS = ("account_id", "char_id", "char_num", "name", "class", "guild_id")
VENDOR_SUFFIXES = (" Shop", " Mart", " Trade", " Deals", "Vend", " Goods")  # as tools/pairchars.py
VENDOR_BASE_ID = 180000
VENDOR_ALT_ID = 220000
VENDOR_PLAN = "blacksmith"
VENDOR_TARGET_CLASS = 10


def load_personas(path):
    with open(path, encoding="utf-8") as f:
        data = json.load(f)
    return data if isinstance(data, list) else data.get("personas", data)


def load_dump(path):
    """Rows of the character dump (TSV, header required). Missing guild_id reads as 0."""
    rows = []
    with open(path, encoding="utf-8", newline="") as f:
        rd = csv.DictReader(f, delimiter="\t")
        if not rd.fieldnames or any(c not in rd.fieldnames for c in DUMP_COLUMNS[:5]):
            raise SystemExit("dump %s: header must contain %s" % (path, ", ".join(DUMP_COLUMNS[:5])))
        for r in rd:
            try:
                rows.append({"account_id": int(r["account_id"]), "char_id": int(r["char_id"]),
                             "char_num": int(r["char_num"]), "name": r["name"],
                             "class": int(r["class"]), "guild_id": int(r.get("guild_id") or 0)})
            except (TypeError, ValueError) as e:
                raise SystemExit("dump %s: bad row %r (%s)" % (path, r, e))
    return rows


def load_pinned(path):
    """Human party members as a set of lower-case names. Accepts a JSON list of names, or the human-parties.json
    shape (a list of {"members": [...]})."""
    with open(path, encoding="utf-8") as f:
        data = json.load(f)
    names = set()
    for item in data:
        if isinstance(item, str):
            names.add(item.lower())
        elif isinstance(item, dict):
            names.update(m.lower() for m in item.get("members", []))
    return names


def in_bot_range(aid):
    return BOT_MIN <= aid <= BOT_MAX


def is_vendor(c):
    return c.get("role") == "vendor" or str(c.get("plan", "")).startswith(MERCH_PREFIX)


def main_char(p):
    for c in p.get("characters", []):
        if c.get("role") == "main" and c.get("slot") == 0:
            return c
    return None


def _index(rows):
    by = {}
    for r in rows:
        by.setdefault(r["account_id"], []).append(r)
    return by


def select(personas, dump_rows=None, pinned=(), target=PHASE1_TARGET):
    """Pure selection. Returns a dict: accounts (complete pairs, the design's 85), vendor_adds (guild-less hunters
    that need a vendor character), skipped (Counter of reasons), candidates (count before the target cut)."""
    pinned = set(x.lower() for x in pinned)
    by_acc = _index(dump_rows) if dump_rows is not None else None
    used_ids = set(c["char_id"] for c in dump_rows) if dump_rows is not None else set()
    taken = set(c["name"].lower() for c in dump_rows) if dump_rows is not None else set()
    for p in personas:
        used_ids.update(c["char_id"] for c in p.get("characters", []))
        taken.update(c["name"].lower() for c in p.get("characters", []))
    skipped = Counter()
    complete, adds = [], []
    for p in sorted(personas, key=lambda x: x.get("account_id", 0)):
        aid = p.get("account_id", 0)
        if not in_bot_range(aid):
            skipped["outside bot range"] += 1
            continue
        chars = p.get("characters", [])
        if p.get("guild"):
            skipped["in a guild (persona)"] += 1
            continue
        if any(c["name"].lower() in pinned for c in chars):
            skipped["pinned human party"] += 1
            continue
        rows = by_acc.get(aid, []) if by_acc is not None else []
        if by_acc is not None:
            if not rows:
                skipped["account not in dump"] += 1
                continue
            if any(r["guild_id"] for r in rows):
                skipped["in a guild (dump)"] += 1
                continue
        m = main_char(p)
        if m is None or m.get("plan") not in HUNTER_PLANS:
            skipped["main not hunter line"] += 1
            continue
        vend = [c for c in chars if is_vendor(c)]
        if vend:
            complete.append(_farmer(p, m, vendor=None))
            continue
        spec, why = _vendor_spec(p, chars, rows, used_ids, taken)
        if spec is None:
            skipped[why] += 1
            continue
        adds.append(_farmer(p, m, vendor=spec))
    chosen = complete[:target]
    room = max(0, target - len(chosen))
    return {"accounts": chosen, "vendor_adds": [f for f in adds[:room] if f["vendor"]],
            "candidates": len(complete) + len(adds), "skipped": skipped, "target": target}


def _farmer(p, m, vendor):
    return {"account": p["account"], "account_id": p["account_id"], "main_char_id": m["char_id"],
            "main_name": m["name"], "plan": m["plan"], "vendor": vendor}


def _vendor_spec(p, chars, rows, used_ids, taken):
    """A vendor character for an account without one: first free char_num slot, unused id and name."""
    occupied = set(c["slot"] for c in chars) | set(r["char_num"] for r in rows)
    free = [s for s in range(0, 9) if s not in occupied]
    if not free:
        return None, "no free slot"
    idx = p["account_id"] - 2100000
    cid = VENDOR_BASE_ID + idx
    if cid in used_ids:
        cid = VENDOR_ALT_ID + idx
    if cid in used_ids:
        return None, "no free char id"
    stem = (chars[0]["name"] if chars else p.get("name", "Player"))
    name = None
    for suf in VENDOR_SUFFIXES:
        cand = stem.replace(" Shop", "").replace(" Mart", "")[:24 - len(suf)] + suf
        if cand.lower() not in taken:
            name = cand
            break
    if name is None:
        return None, "no free name"
    used_ids.add(cid)
    taken.add(name.lower())
    return {"char_id": cid, "slot": free[0], "name": name, "plan": VENDOR_PLAN,
            "target_class": VENDOR_TARGET_CLASS, "class": 0, "build": "str-dex"}, ""


def build_doc(sel, since=None):
    """farmers.json document (schema 1). Top-level keys follow design 2.1; the per-account detail rides along."""
    return {
        "schema": SCHEMA,
        "since": since or time.strftime("%Y-%m-%dT%H:%M:%S%z"),
        "target": sel["target"],
        "accounts": [f["account"] for f in sel["accounts"]] + [f["account"] for f in sel["vendor_adds"]],
        "detail": [dict(account=f["account"], account_id=f["account_id"], main_char_id=f["main_char_id"],
                        main_name=f["main_name"], plan=f["plan"], vendor_present=f["vendor"] is None)
                   for f in sel["accounts"] + sel["vendor_adds"]],
        "vendor_adds": [dict(account=f["account"], account_id=f["account_id"], **f["vendor"])
                        for f in sel["vendor_adds"]],
    }


def validate_farmers(doc):
    """Schema 1 check: returns a list of problems (empty when valid)."""
    errs = []
    if not isinstance(doc, dict):
        return ["farmers.json must be an object"]
    if doc.get("schema") != SCHEMA:
        errs.append("schema must be %d" % SCHEMA)
    if not isinstance(doc.get("since"), str):
        errs.append("since must be a string")
    acc = doc.get("accounts")
    if not isinstance(acc, list) or not all(isinstance(a, str) and a.startswith("lg") for a in acc):
        errs.append("accounts must be a list of lgNNNN names")
    elif len(set(acc)) != len(acc):
        errs.append("accounts has duplicates")
    if isinstance(acc, list) and len(acc) > 300:
        errs.append("more than 300 farmers (phase 2 cap)")
    for d in doc.get("detail", []) or []:
        aid = d.get("account_id")
        if not isinstance(aid, int) or not in_bot_range(aid):
            errs.append("detail %s: account_id outside the bot range" % d.get("account"))
    for v in doc.get("vendor_adds", []) or []:
        for k in ("account", "account_id", "char_id", "slot", "name", "plan"):
            if k not in v:
                errs.append("vendor_adds %s: missing %s" % (v.get("account"), k))
        aid = v.get("account_id")
        if not isinstance(aid, int) or not in_bot_range(aid):
            errs.append("vendor_adds %s: account_id outside the bot range" % v.get("account"))
        if isinstance(v.get("slot"), int) and not 0 <= v["slot"] <= 8:
            errs.append("vendor_adds %s: slot out of 0..8" % v.get("account"))
    return errs


def main(argv=None):
    import argparse
    ap = argparse.ArgumentParser(description="Select phase-1 farmers (design section 2.1). Dry run unless --write.")
    ap.add_argument("--personas", default=None, help="personas.json (default: the data folder)")
    ap.add_argument("--dump", required=True, help="TSV dump of the char table (read-only copy)")
    ap.add_argument("--pinned", required=True, help="JSON of human party members to exclude (required)")
    ap.add_argument("--target", type=int, default=PHASE1_TARGET)
    ap.add_argument("--out", default=None, help="farmers.json path (default: the data folder)")
    ap.add_argument("--write", action="store_true", help="write farmers.json (runtime file)")
    a = ap.parse_args(argv)
    personas_path = a.personas or _C.dpath("personas.json")
    personas = load_personas(personas_path)
    dump = load_dump(a.dump)
    sel = select(personas, dump, load_pinned(a.pinned), a.target)
    doc = build_doc(sel)
    errs = validate_farmers(doc)
    if errs:
        raise SystemExit("refused, invalid roster: " + "; ".join(errs[:5]))
    print("candidates %d; farmers %d (complete pairs %d, vendor to add %d); target %d; shortfall %d"
          % (sel["candidates"], len(doc["accounts"]), len(sel["accounts"]), len(sel["vendor_adds"]),
             a.target, max(0, a.target - len(doc["accounts"]))))
    print("skipped: " + (", ".join("%s %d" % kv for kv in sel["skipped"].most_common()) or "none"))
    for f in doc["vendor_adds"][:8]:
        print("  add vendor: %s %d slot %d char %d %s" % (f["account"], f["account_id"], f["slot"], f["char_id"],
                                                          f["name"]))
    if not a.write:
        print("dry run: nothing written (use --write)")
        return 0
    out = a.out or _C.dpath("farmers.json")
    tmp = out + ".tmp"
    with open(tmp, "w", encoding="utf-8") as f:
        json.dump(doc, f, ensure_ascii=False, indent=2)
    os.replace(tmp, out)
    print("wrote %s" % out)
    return 0


if __name__ == "__main__":
    sys.exit(main())
