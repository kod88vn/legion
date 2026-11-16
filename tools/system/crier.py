#!/usr/bin/env python3
"""Market crier (EC44, design docs/design/market-engine.md section 5, WP7).

Writes one server-wide notice into the table legion_notice; the floating NPC MarketCrier
(rathena/npc-custom/legion_crier.txt) announces it. No character takes part (G03, G04).

Rules this tool enforces:
  - templates only: the engine passes a kind (want | glut | event) and two slots, item and place; the text comes
    from the fixed templates below. No free text and no computed numbers (digits are refused everywhere).
  - banned words (bot, AI, engine, Legion, simulation, algorithm, server owner, script, ...) are refused.
  - at most 120 characters.
  - cadence: at most 4 notices a day, at least 3 h apart, only 08:00-22:59 local time, the same item at most once
    per 12 h. Only live notices count toward this (dry runs check the same rule against the real history).
  - the flag market_broadcast_on in tuning.json (default false; read here, tuning.go owns the key) must be true,
    else nothing is written at all.
  - dry run is the default: it only appends to data/market_notices.jsonl. --live also inserts the row into ro-db
    through `docker exec` (the engine has no SQL client, so this tool is the writer).

  crier.py send --kind want --item "Poring Coin" --place Prontera [--live] [--now EPOCH]
  crier.py templates

Clock: the game server's (UTC, SERVER_UTC_OFFSET_H = 0). Window 08:00-22:59 on that clock, matching the NPC.
Names: item and place slots are display names made of letters, spaces, apostrophes and hyphens; digits are
refused (write "Field Two", not "Field 02"). No computed numbers ever reach the text.
Live rows are only read by legion_crier.txt when its window is open and the row is under 6 h old.

Go-live order (deploy window only; G15, G16, G17): load rathena/sql/legion_notice.sql into ro-db, then
`docker restart ro-rathena` (not a compose recreate), then set market_broadcast_on to true. Dry run first.

Exit: 0 when sent, dry-run, held by cadence or off; 1 when a live insert failed; 2 for a refused input.
"""
import argparse
import json
import os
import random
import re
import calendar
import subprocess
import sys
import time

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import common  # noqa: E402

FLAG = "market_broadcast_on"
MAX_CHARS = 120
MAX_PER_DAY = 4
GAP_S = 3 * 3600
DAY_S = 24 * 3600
SAME_ITEM_S = 12 * 3600
HOUR_FROM = 8     # inclusive
HOUR_TO = 23      # exclusive: 22:59 is the last minute
# One clock for the window: the game server's. The ro-rathena container runs on UTC (docker-compose sets no TZ),
# and the NPC reads gettime(DT_HOUR) on that clock. Change this only together with legion_crier.txt.
SERVER_UTC_OFFSET_H = 0
ROW_MAX_AGE_NOTE = "rows older than 6 h are never announced (legion_crier.txt)"
KINDS = ("want", "glut", "event")
DB_CONTAINER = "ro-db"

TEMPLATES = {
    "want": [
        "[{place} Merchants] {item} is wanted. Good prices for anyone who brings some.",
        "[{place} Merchants] Buying {item} now. Bring what you have to the usual stalls.",
        "[{place} Merchants] {item} is in short supply around {place}. Hunters, please help.",
        "[{place} Notice] Looking for {item}. Ask at the market board in {place}.",
        "[{place} Merchants] Anyone with {item} to spare, the price is rising.",
        "[{place} Guild Board] {item} is needed in {place}. Good coin for those who bring it.",
    ],
    "glut": [
        "[{place} Merchants] Prices for {item} have fallen. Wait a while before selling.",
        "[{place} Merchants] There is more {item} in the stalls than buyers. Prices are low.",
        "[{place} Notice] {item} is plentiful in {place} for now. Look elsewhere for better deals.",
        "[{place} Merchants] The {item} market has cooled. Hold your stock if you can.",
    ],
    "event": [
        "[{place} Notice] Hunters are gathering {item} near {place}. Join them if you are close.",
        "[{place} Notice] Word is that {item} can be found around {place}. Go and see.",
        "[{place} Traders] A trader is paying well for {item} in {place} right now.",
    ],
}

BANNED_WORDS = [
    "bot", "bots", "ai", "a.i.", "engine", "legion", "simulation", "simulated", "algorithm", "server owner",
    "owner", "script", "scripts", "gm", "market engine", "the system", "npc", "puppet", "llm", "claude",
    "agent", "agents", "mind", "minds",
]
_BANNED_RE = [re.compile(r"(?<![a-z])" + re.escape(w) + r"(?![a-z])", re.IGNORECASE) for w in BANNED_WORDS]
_SLOT_RE = re.compile(r"^[A-Za-z][A-Za-z' -]{1,39}$")


class Refused(ValueError):
    """An input or a rendered text breaks a rule. The message says which."""


def flag_on(data_path=None):
    """market_broadcast_on from tuning.json, default False when the file or the key is missing or unreadable."""
    path = data_path or common.dpath("tuning.json")
    try:
        with open(path, "r", encoding="utf-8") as f:
            data = json.load(f)
    except (OSError, ValueError):
        return False
    return isinstance(data, dict) and data.get(FLAG) is True


def check_text(text):
    """Refuse a rendered notice that breaks a rule. Returns the text when it passes."""
    if not text or not text.strip():
        raise Refused("empty notice")
    if len(text) > MAX_CHARS:
        raise Refused("notice is %d characters, the limit is %d" % (len(text), MAX_CHARS))
    if re.search(r"\d", text):
        raise Refused("notice contains a digit (no computed numbers)")
    if "{" in text or "}" in text:
        raise Refused("notice has an unfilled slot")
    for rx in _BANNED_RE:
        m = rx.search(text)
        if m:
            raise Refused("banned word in notice: %r" % m.group(0))
    return text


def check_slot(name, value):
    if not isinstance(value, str) or not _SLOT_RE.match(value.strip()):
        raise Refused("%s must be 2-40 letters, spaces, apostrophes or hyphens: %r" % (name, value))
    return value.strip()


def render(kind, item, place, rng=None):
    """Pick a template for the kind and fill the two slots. Raises Refused on any rule break."""
    if kind not in KINDS:
        raise Refused("unknown kind %r (use one of %s)" % (kind, ", ".join(KINDS)))
    item = check_slot("item", item)
    place = check_slot("place", place)
    rng = rng or random
    tpl = rng.choice(TEMPLATES[kind])
    return check_text(tpl.format(item=item, place=place))


def server_hour(now):
    """Hour of day (0-23) on the game server's clock (SERVER_UTC_OFFSET_H from UTC)."""
    return time.gmtime(now + SERVER_UTC_OFFSET_H * 3600).tm_hour


def cadence_ok(now, history, item):
    """(ok, reason). history: records of live notices, each with t (epoch s) and item."""
    hour = server_hour(now)
    if not (HOUR_FROM <= hour < HOUR_TO):
        return False, "outside %02d:00-%02d:59 local time" % (HOUR_FROM, HOUR_TO - 1)
    day = [r for r in history if 0 <= now - _t(r) < DAY_S]
    if len(day) >= MAX_PER_DAY:
        return False, "daily cap of %d reached" % MAX_PER_DAY
    if any(now - _t(r) < GAP_S for r in history if _t(r) <= now):
        return False, "less than %d h since the last notice" % (GAP_S // 3600)
    if any(r.get("item") == item and 0 <= now - _t(r) < SAME_ITEM_S for r in history):
        return False, "same item within %d h" % (SAME_ITEM_S // 3600)
    return True, "ok"


def _t(r):
    try:
        return float(r.get("t", 0))
    except (TypeError, ValueError):
        return 0.0


def sql_quote(text):
    """A MySQL/MariaDB string literal. Input is already limited to the template alphabet; escaped anyway."""
    return "'" + text.replace("\\", "\\\\").replace("'", "''") + "'"


def insert_live(text, container=DB_CONTAINER):
    """One INSERT into legion_notice in ro-db. The password comes from the container's own environment
    (MARIADB_PASSWORD), never from this repo. Not run by the tests (G23)."""
    sql = "INSERT INTO legion_notice (`text`) VALUES (%s);\n" % sql_quote(text)
    cmd = ["docker", "exec", "-i", container, "sh", "-c",
           'MYSQL_PWD="$MARIADB_PASSWORD" exec mariadb -u "$MARIADB_USER" "$MARIADB_DATABASE"']
    subprocess.run(cmd, input=sql.encode("utf-8"), check=True, timeout=60)


def send(kind, item, place, live=False, now=None, rng=None, log_path=None, insert=None, flag_path=None):
    """Decide and, when allowed, write one notice. Returns a dict with status and text.
    status: off (flag false), held (cadence), dry (would send), sent (live row inserted), failed (live insert
    raised; its cadence record is kept). Window and cadence use the server clock (server_hour)."""
    now = common.now() if now is None else float(now)
    if not flag_on(flag_path):
        return {"status": "off", "reason": "%s is not true in tuning.json" % FLAG}
    text = render(kind, item, place, rng=rng)
    path = log_path or common.dpath("market_notices.jsonl")
    history = [r for r in common.read_jsonl(path) if r.get("mode") == "live"]
    ok, reason = cadence_ok(now, history, item)
    if not ok:
        return {"status": "held", "reason": reason, "text": text}
    rec = {"t": int(now), "at": common.iso(now), "kind": kind, "item": item, "place": place, "text": text,
           "mode": "live" if live else "dry"}
    # The history record is written BEFORE the insert, so a live row can never exist without its cadence record.
    # If the insert fails, the record stays: the failed attempt uses its cadence slot (conservative; no retry storm).
    common.append_jsonl(path, rec)
    if not live:
        return {"status": "dry", "text": text}
    try:
        (insert or insert_live)(text)
    except Exception as e:  # noqa: BLE001 - reported, never hidden
        return {"status": "failed", "text": text, "reason": "%s: %s" % (type(e).__name__, e)}
    return {"status": "sent", "text": text}


def main(argv=None):
    ap = argparse.ArgumentParser(description="Write one market notice (dry run by default).")
    sub = ap.add_subparsers(dest="cmd", required=True)
    s = sub.add_parser("send")
    s.add_argument("--kind", required=True, choices=KINDS)
    s.add_argument("--item", required=True)
    s.add_argument("--place", required=True)
    s.add_argument("--live", action="store_true", help="insert into ro-db (default: dry run to the jsonl only)")
    s.add_argument("--now", type=float, default=None, help="epoch seconds (tests)")
    sub.add_parser("templates")
    a = ap.parse_args(argv)
    if a.cmd == "templates":
        for k in KINDS:
            for t in TEMPLATES[k]:
                print(k, "|", t)
        return 0
    try:
        out = send(a.kind, a.item, a.place, live=a.live, now=a.now)
    except Refused as e:
        print("refused: %s" % e, file=sys.stderr)
        return 2
    print(json.dumps(out, ensure_ascii=False))
    return 1 if out["status"] == "failed" else 0


if __name__ == "__main__":
    sys.exit(main())
