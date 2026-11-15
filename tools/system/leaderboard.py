#!/usr/bin/env python3
"""Server leaderboard (read-only): every character in the rathena DB ranked by base level, then base exp.

  leaderboard.py [--top N] [--json]      top N (default 20), Ayla's rank, her party's ranks, each guild leader's rank
  leaderboard.py --brief                 one line for aylawatch.sh: "rank 12/7818 lv83 | party Zenda 30 ..."

Source: ro-db through the existing read-only path (wsl docker exec ro-db mariadb, as wrongbuilds.py); the session is
set READ ONLY and only the fixed SELECTs below are sent. Ranking is competition ranking (ties share a rank: 1 + how
many characters are strictly ahead). Staff characters (the owner's GM account `wander`, the QA account `qatest`, the
Admin character) are left out unless --staff; players are never treated differently (G25).
Ayla = char_id 150010 (or --name). Her party = the characters with her party_id in the DB.
"""
import argparse
import csv
import io
import json
import subprocess
import sys

AYLA_ID = 150010
STAFF_USERIDS = {"wander", "qatest"}
STAFF_CHARS = {"admin"}

Q_CHARS = ("SELECT c.char_id, c.account_id, c.name, c.class, c.base_level, c.job_level, c.base_exp, c.party_id, "
           "c.guild_id, c.online, l.userid, IFNULL(g.name, '') AS guild, IFNULL(g.char_id, 0) AS guild_master "
           "FROM `char` c JOIN login l ON l.account_id = c.account_id LEFT JOIN guild g ON g.guild_id = c.guild_id")


def db_rows(query=Q_CHARS):
    """Rows of one fixed SELECT from ro-db in a READ ONLY session."""
    if not query.lstrip().upper().startswith("SELECT") or ";" in query:
        raise ValueError("leaderboard: only a single SELECT is allowed")
    sql = "SET SESSION TRANSACTION READ ONLY;\nSTART TRANSACTION READ ONLY;\n%s;\nCOMMIT;\n" % query
    r = subprocess.run(["wsl", "-d", "Ubuntu-24.04", "--", "docker", "exec", "-i", "ro-db", "mariadb",
                        "-uragnarok", "-pragnarok", "ragnarok", "-B"], input=sql.encode("utf8"), capture_output=True,
                       timeout=60)
    if r.returncode:
        raise RuntimeError("ro-db query failed: " + r.stderr.decode("utf8", "replace").strip()[:300])
    return list(csv.DictReader(io.StringIO(r.stdout.decode("utf8", "replace")), delimiter="\t", quoting=csv.QUOTE_NONE))


def _i(v):
    try:
        return int(v)
    except (TypeError, ValueError):
        return 0


def normalize(rows, staff=False):
    out = []
    for r in rows:
        name = r.get("name") or ""
        if not staff and ((r.get("userid") or "").lower() in STAFF_USERIDS or name.lower() in STAFF_CHARS):
            continue
        out.append({"char_id": _i(r.get("char_id")), "name": name, "class": _i(r.get("class")),
                    "lv": _i(r.get("base_level")), "job": _i(r.get("job_level")), "exp": _i(r.get("base_exp")),
                    "party_id": _i(r.get("party_id")), "guild": r.get("guild") or "",
                    "guild_leader": _i(r.get("guild_master")) == _i(r.get("char_id")) and _i(r.get("char_id")) > 0,
                    "online": _i(r.get("online")) == 1})
    return out


def rank(chars):
    """Sorted by level then exp (then name); 'rank' = 1 + characters strictly ahead."""
    s = sorted(chars, key=lambda c: (-c["lv"], -c["exp"], c["name"].lower()))
    prev, rk = None, 0
    for i, c in enumerate(s, 1):
        key = (c["lv"], c["exp"])
        if key != prev:
            rk, prev = i, key
        c["rank"] = rk
    return s


def board(rows, ayla_id=AYLA_ID, ayla_name=None, staff=False):
    """{total, top(all ranked), ayla, party[], leaders[]}"""
    ranked = rank(normalize(rows, staff))
    by_id = {c["char_id"]: c for c in ranked}
    ayla = None
    if ayla_name:
        ayla = next((c for c in ranked if c["name"].lower() == ayla_name.lower()), None)
    if ayla is None:
        ayla = by_id.get(ayla_id)
    if ayla is None:  # Ayla's own row may be filtered as staff on her account: look her up in the raw rows
        raw = [r for r in normalize(rows, staff=True) if r["char_id"] == ayla_id or (ayla_name and r["name"].lower() == ayla_name.lower())]
        if raw:
            ayla = dict(raw[0])
            ayla["rank"] = 1 + sum(1 for c in ranked if (c["lv"], c["exp"]) > (ayla["lv"], ayla["exp"]))
    party = []
    if ayla and ayla["party_id"]:
        party = [c for c in ranked if c["party_id"] == ayla["party_id"] and c["char_id"] != ayla["char_id"]]
    leaders = [c for c in ranked if c["guild_leader"]]
    return {"total": len(ranked), "ranked": ranked, "ayla": ayla, "party": party, "leaders": leaders}


def brief(b):
    a = b["ayla"]
    if not a:
        return "rank ?/%d (Ayla not found)" % b["total"]
    party = " ".join("%s %d" % (c["name"][:6], c["rank"]) for c in b["party"])
    return "rank %d/%d lv%d | party %s" % (a["rank"], b["total"], a["lv"], party or "-")


def render(b, top=20):
    L = ["Server leaderboard: %d characters by base level, then base exp" % b["total"], ""]
    L.append("%4s  %-24s %5s %4s %12s  %-20s %s" % ("rank", "name", "class", "lv", "exp", "guild", ""))
    def line(c, tag=""):
        return "%4d  %-24s %5d %4d %12d  %-20s %s%s" % (c["rank"], c["name"][:24], c["class"], c["lv"], c["exp"],
                                                       c["guild"][:20], "online " if c["online"] else "", tag)
    for c in b["ranked"][:top]:
        L.append(line(c))
    L.append("")
    a = b["ayla"]
    L.append("Ayla: " + (line(a) if a else "not found"))
    L.append("Ayla's party:")
    L += ["  " + line(c) for c in b["party"]] or ["  (no party)"]
    L.append("Guild leaders:")
    L += ["  " + line(c, "leader") for c in b["leaders"]] or ["  (none)"]
    return "\n".join(L) + "\n"


def main(argv=None, rows=None):
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--top", type=int, default=20)
    ap.add_argument("--json", action="store_true")
    ap.add_argument("--brief", action="store_true")
    ap.add_argument("--name", help="Ayla's character name (default: char_id %d)" % AYLA_ID)
    ap.add_argument("--staff", action="store_true", help="include staff characters")
    a = ap.parse_args(argv)
    try:
        rows = db_rows() if rows is None else rows
    except Exception as e:
        print("leaderboard: %s" % e if not a.brief else "rank ? (db: %s)" % str(e)[:60])
        return 1
    b = board(rows, ayla_name=a.name, staff=a.staff)
    if a.brief:
        print(brief(b))
    elif a.json:
        out = dict(b)
        out["top"] = out.pop("ranked")[:a.top]
        print(json.dumps(out, indent=1))
    else:
        sys.stdout.write(render(b, a.top))
    return 0


if __name__ == "__main__":
    sys.exit(main())
