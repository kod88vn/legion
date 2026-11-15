"""Read-only access to the rAthena database for the dashboard's Players tab (DB07), and its snapshots (DB08).

Every query goes through `docker exec ro-db mariadb` with SET SESSION TRANSACTION READ ONLY first, and `run()`
refuses any statement that is not a SELECT or SHOW, so nothing here can write. Nothing here starts a container:
`query()` is only called after docker reported ro-db as running.

A snapshot is one gzip'd JSON file {t, source, tables: {name: {cols, rows}}} in tools/.state/dbsnap/ (git-ignored);
the newest three are kept. The login table is read without passwords, e-mail, IP, PIN or tokens.
"""
import gzip
import json
import os
import re
import subprocess
import time

WSL_DISTRO = os.environ.get("LEGION_WSL_DISTRO", "Ubuntu-24.04")
DB_CONTAINER = "ro-db"
DB_USER, DB_PASS, DB_NAME = "ragnarok", "ragnarok", "ragnarok"   # docker-compose.yml (db service)
KEEP = 3

ITEM_COLS = "char_id, nameid, amount, equip, identify, refine, attribute, card0, card1, card2, card3"
# table -> SELECT body (the snapshot); only what the Players tab shows
SNAP_TABLES = {
    "char": "SELECT char_id, account_id, char_num, name, class, base_level, job_level, base_exp, job_exp, zeny, "
            "str, agi, vit, `int`, dex, luk, max_hp, hp, max_sp, sp, status_point, skill_point, party_id, guild_id, "
            "last_map, last_x, last_y, save_map, save_x, save_y, online, sex, last_login FROM `char`",
    "inventory": "SELECT %s FROM inventory" % ITEM_COLS,
    "cart_inventory": "SELECT %s FROM cart_inventory" % ITEM_COLS,
    "storage": "SELECT %s FROM storage" % ITEM_COLS.replace("char_id", "account_id"),
    "skill": "SELECT char_id, id, lv, flag FROM skill",
    "guild": "SELECT guild_id, name, char_id, master, guild_lv, max_member, average_lv FROM guild",
    "guild_member": "SELECT guild_id, char_id, position FROM guild_member",
    "party": "SELECT party_id, name, exp, item, leader_id, leader_char FROM party",
    "login": "SELECT account_id, userid, sex, group_id, logincount, lastlogin FROM login",
}
ALLOWED = re.compile(r"^\s*(SELECT|SHOW)\b", re.I)


def _cmd():
    base = ["docker", "exec", "-i", "-e", "MYSQL_PWD=" + DB_PASS, DB_CONTAINER, "mariadb", "-u" + DB_USER, "-B",
            "--max-allowed-packet=256M", DB_NAME]
    return (["wsl.exe", "-d", WSL_DISTRO, "--"] + base) if os.name == "nt" else base


def _unescape(v):
    if v == "NULL":
        return None
    if "\\" not in v:
        return v
    return re.sub(r"\\(.)", lambda m: {"t": "\t", "n": "\n", "0": "\0", "\\": "\\"}.get(m.group(1), m.group(1)), v)


def _num(v):
    if v is None:
        return None
    if re.fullmatch(r"-?\d+", v):
        return int(v)
    return v


def run(statements, timeout=120):
    """Run SELECT/SHOW statements read-only. Returns one {cols, rows} per statement (rows in the same order)."""
    for s in statements:
        if not ALLOWED.match(s) or ";" in s.strip().rstrip(";"):
            raise ValueError("read-only: only single SELECT/SHOW statements are allowed")
    mark = "@@dash-end@@"
    sql = "SET SESSION TRANSACTION READ ONLY;\n" + "".join(
        s.strip().rstrip(";") + ";\nSELECT '%s';\n" % mark for s in statements)
    p = subprocess.run(_cmd(), input=sql.encode("utf-8"), capture_output=True, timeout=timeout)
    if p.returncode != 0:
        raise RuntimeError((p.stderr or b"").decode("utf-8", "replace").strip()[:300] or "mariadb exit %d" % p.returncode)
    # -B: a header line, then rows, for each result set (an empty result prints nothing); the marker SELECT after
    # every statement prints "mark" twice (header and row) and closes the block
    out, block = [], []
    lines = p.stdout.decode("utf-8", "replace").split("\n")
    i = 0
    while i < len(lines):
        if lines[i] == mark and i + 1 < len(lines) and lines[i + 1] == mark:
            cols = block[0].split("\t") if block else _expected_cols(statements[len(out)])
            out.append({"cols": cols, "rows": [[_num(_unescape(v)) for v in l.split("\t")] for l in block[1:]]})
            block = []
            i += 2
            continue
        if lines[i] != "":
            block.append(lines[i])
        i += 1
    if len(out) != len(statements):
        raise RuntimeError("unexpected mariadb output (%d of %d result sets)" % (len(out), len(statements)))
    return out


def _expected_cols(stmt):
    m = re.match(r"\s*SELECT\s+(.*?)\s+FROM\s", stmt, re.I | re.S)
    if not m:
        return []
    cols = []
    for c in m.group(1).split(","):
        c = c.strip()
        a = re.search(r"\s+AS\s+`?(\w+)`?$", c, re.I)
        cols.append(a.group(1) if a else c.strip("`").split(".")[-1])
    return cols


def take(snap_dir):
    """One snapshot of SNAP_TABLES; returns its path. Raises on a database error."""
    names = list(SNAP_TABLES)
    t0 = time.time()
    res = run([SNAP_TABLES[n] for n in names], timeout=300)
    doc = {"t": t0, "source": "ro-db (read-only SELECT)", "took_s": round(time.time() - t0, 1),
           "tables": {n: r for n, r in zip(names, res)}}
    os.makedirs(snap_dir, exist_ok=True)
    path = os.path.join(snap_dir, time.strftime("snap-%Y%m%d-%H%M%S.json.gz", time.localtime(t0)))
    tmp = path + ".tmp"
    with gzip.open(tmp, "wt", encoding="utf-8") as f:
        json.dump(doc, f, separators=(",", ":"))
    os.replace(tmp, path)
    for old in sorted(p for p in os.listdir(snap_dir) if p.startswith("snap-") and p.endswith(".json.gz"))[:-KEEP]:
        try:
            os.remove(os.path.join(snap_dir, old))
        except OSError:
            pass
    return path


def latest(snap_dir):
    try:
        files = sorted(p for p in os.listdir(snap_dir) if p.startswith("snap-") and p.endswith(".json.gz"))
    except OSError:
        return None
    return os.path.join(snap_dir, files[-1]) if files else None


class Snapshot:
    """The newest snapshot, indexed. Reloaded when a newer file appears."""

    def __init__(self, path):
        with gzip.open(path, "rt", encoding="utf-8") as f:
            doc = json.load(f)
        self.path, self.t, self.source = path, doc.get("t"), doc.get("source")
        self.tables = {}
        for n, tb in (doc.get("tables") or {}).items():
            cols = tb.get("cols") or []
            self.tables[n] = [dict(zip(cols, r)) for r in tb.get("rows") or []]
        self.chars = {c["char_id"]: c for c in self.tables.get("char", [])}
        self.accounts = {a["account_id"]: a for a in self.tables.get("login", [])}
        self.by_char = {}
        for n in ("inventory", "cart_inventory", "skill"):
            idx = {}
            for r in self.tables.get(n, []):
                idx.setdefault(r.get("char_id"), []).append(r)
            self.by_char[n] = idx
        self.storage = {}
        for r in self.tables.get("storage", []):
            self.storage.setdefault(r.get("account_id"), []).append(r)
        self.guilds = {g["guild_id"]: g for g in self.tables.get("guild", [])}
        self.parties = {p["party_id"]: p for p in self.tables.get("party", [])}
        self.counts = {n: len(v) for n, v in self.tables.items()}


def live_char(char_id):
    """One character straight from ro-db (read-only). Same shape as the snapshot rows."""
    cid = int(char_id)
    sql = [SNAP_TABLES["char"] + " WHERE char_id = %d" % cid]
    res = run(sql)
    rows = [dict(zip(res[0]["cols"], r)) for r in res[0]["rows"]]
    if not rows:
        return None
    c = rows[0]
    q = [SNAP_TABLES["inventory"] + " WHERE char_id = %d" % cid,
         SNAP_TABLES["cart_inventory"] + " WHERE char_id = %d" % cid,
         SNAP_TABLES["skill"] + " WHERE char_id = %d" % cid,
         SNAP_TABLES["storage"] + " WHERE account_id = %d" % int(c["account_id"]),
         SNAP_TABLES["login"] + " WHERE account_id = %d" % int(c["account_id"])]
    if c.get("guild_id"):
        q.append(SNAP_TABLES["guild"] + " WHERE guild_id = %d" % int(c["guild_id"]))
    if c.get("party_id"):
        q.append(SNAP_TABLES["party"] + " WHERE party_id = %d" % int(c["party_id"]))
    r = run(q)
    d = lambda k: [dict(zip(r[k]["cols"], x)) for x in r[k]["rows"]]
    out = {"char": c, "inventory": d(0), "cart_inventory": d(1), "skill": d(2), "storage": d(3),
           "login": (d(4) or [None])[0], "guild": None, "party": None}
    k = 5
    if c.get("guild_id"):
        out["guild"] = (d(k) or [None])[0]
        k += 1
    if c.get("party_id"):
        out["party"] = (d(k) or [None])[0]
    return out


def fetch_skill_names(path, container="ro-rathena"):
    """Skill id, code and name from the game server's own skill_db.yml (read with docker exec grep; a running
    container only). Writes id<TAB>code<TAB>name lines to path. Returns the number of skills."""
    base = ["docker", "exec", container, "grep", "-A2", "^  - Id:", "/rathena/db/re/skill_db.yml"]
    cmd = (["wsl.exe", "-d", WSL_DISTRO, "--"] + base) if os.name == "nt" else base
    p = subprocess.run(cmd, capture_output=True, timeout=60)
    if p.returncode != 0:
        raise RuntimeError((p.stderr or b"").decode("utf-8", "replace").strip()[:200])
    rows, cur = [], {}
    for line in p.stdout.decode("utf-8", "replace").splitlines():
        m = re.match(r"\s*-?\s*(Id|Name|Description):\s*(.*)$", line)
        if not m:
            continue
        k, v = m.group(1), m.group(2).strip()
        if k == "Id":
            cur = {"id": v}
        elif k == "Name":
            cur["code"] = v
        elif k == "Description" and "id" in cur:
            rows.append("%s\t%s\t%s" % (cur["id"], cur.get("code", ""), v))
    if not rows:
        raise RuntimeError("no skills parsed")
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path + ".tmp", "w", encoding="utf-8") as f:
        f.write("\n".join(rows) + "\n")
    os.replace(path + ".tmp", path)
    return len(rows)
