#!/usr/bin/env python3
"""Ayla watch (plain code, no AI): a read-only snapshot of Ayla's party every 5 minutes.

  aylawatch.py                   loop: one snapshot every 5 min (--interval SEC), printed and appended to <data>/aylawatch.jsonl
  aylawatch.py --once            one snapshot, printed and recorded (--no-record: printed only)
  aylawatch.py --era auto        auto (default) reads the tuning key engine_parties (tools/puppet/aylaswitch.py):
                                 engine when it names ayla, else openkore. --era openkore|engine forces one.

Each snapshot: exp (5 min), kills, deaths, map changes, zeny, each member's distance to Ayla, her status.json (lock map,
intent, age) and the last two [watchdog] lines of her container log.

Sources, by era:
  openkore  her party in GET <LEGION_API>/api/parties/detail (the party with a member named Ayla; the distance is the
            API's dist_lead, filled because the party is human-led), status.json, and the last 5 min of her container log
            (wsl docker logs ro-openkore-ayla): exp gained, kills, deaths, map changes.
  engine    the same party, found by the member named Ayla: an engine party has no human (Party.Human is ""). The distance
            is computed from the members' x, y and map (dist_lead is not filled for engine parties). kills and deaths are
            deltas of the members' running totals against the last recorded snapshot (negative deltas clamped to 0).
            kpm and zph come from the Ayla row of <data>/meter_bots.json ("bots", fresh within 1 h). The API has no exp
            total for engine parties, so exp is reported as n/a in this era.

Read only. It sends no command to any character, to OpenKore or to the engine, and never touches Ayla's AI (G05).
Plain code, no AI (Constitution 3.8): it holds a single-instance lock (<data>/aylawatch.lock), writes its heartbeat
(<data>/aylawatch.heartbeat) every round, and tools/system/watchdog.py checks that heartbeat. Stdlib only.
"""
import argparse
import http.client
import json
import logging
import os
import re
import subprocess
import sys
import time
import urllib.error
import urllib.request

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
sys.path.insert(0, os.path.join(HERE, "..", "puppet"))
import common as C  # noqa: E402  the one resolver for live locations, the heartbeat, the lock and the docker prefix (G23)
import aylaswitch  # noqa: E402  the engine_parties switch (tools/puppet/aylaswitch.py), read through common.py

log = logging.getLogger("aylawatch")

NAME = "Ayla"
CONTAINER = "ro-openkore-ayla"
API_BASE = os.environ.get("LEGION_API") or "http://127.0.0.1:8090"
INTERVAL = 300            # the 5-minute snapshot
WINDOW = "5m"             # the container log window the snapshot covers
HTTP_TIMEOUT = 8          # seconds for the engine API GET
DOCKER_TIMEOUT = 60       # seconds for the wsl docker logs call
HEARTBEAT = "aylawatch"
LOCK = "aylawatch.lock"
LOCK_STALE = 360          # a round is 300 s plus up to 60 s of docker; an older lock belongs to a dead holder
RECORD = "aylawatch.jsonl"
METER_BOTS = "meter_bots.json"
MAX_METER_AGE = 3600      # a meter_bots.json older than this is not read as current
INTENT_CHARS = 60

RE_EXP = re.compile(r"You have gained (\d+)/")
RE_KILL = re.compile(r"Target Monster .* died")
RE_DEATH = "You have died"
RE_MAPCHG = re.compile(r"Map Change")
RE_ANSI = re.compile(r"\x1b\[[0-9;]*[mK]")

# errors one snapshot survives: the failed source is recorded and the round goes on (the loop logs the rest)
SOURCE_ERRORS = (OSError, ValueError, TypeError, KeyError, AttributeError, subprocess.SubprocessError)


def logs_dir():
    """Ayla's OpenKore logs folder: LEGION_AYLA_LOGS (tests) else the live one. Refused under LEGION_TEST when live."""
    return C.refuse_live(os.environ.get("LEGION_AYLA_LOGS") or C.LIVE_AYLA_LOGS, "the Ayla logs folder")


def parse_log(text):
    """Counts from Ayla's container log text (ANSI colour removed): exp gained, kills, deaths, map changes, and the last
    two [watchdog] lines. Pure: no I/O."""
    text = RE_ANSI.sub("", text or "")
    exp = sum(int(m) for m in RE_EXP.findall(text))
    kills = len(RE_KILL.findall(text))
    deaths = text.count(RE_DEATH)
    maps = len(RE_MAPCHG.findall(text))
    wd = [ln.split("[watchdog]", 1)[1][:70].strip() for ln in text.splitlines() if "[watchdog]" in ln][-2:]
    return {"exp_5m": exp, "kills_5m": kills, "deaths_5m": deaths, "map_changes": maps, "watchdog": wd}


def read_status(path, now):
    """(status dict, age seconds (-1 when missing), error text). The dict is {} when the file is missing or unreadable."""
    try:
        age = int(now - os.path.getmtime(path))
        with open(path, encoding="utf-8") as f:
            data = json.load(f)
        if not isinstance(data, dict):
            return {}, age, "status.json is not an object"
        return data, age, ""
    except FileNotFoundError:
        return {}, -1, "status.json missing"
    except (OSError, ValueError) as e:
        log.warning("status.json unreadable: %s: %s", type(e).__name__, e)
        return {}, -1, "status.json %s" % type(e).__name__


def container_log(run=subprocess.run):
    """(text, error). The last 5 minutes of Ayla's container log through wsl; every failure comes back as an error."""
    cmd = C.docker_prefix() + ["logs", "--since", WINDOW, CONTAINER]
    try:
        r = run(cmd, capture_output=True, text=True, errors="replace", timeout=DOCKER_TIMEOUT)
    except subprocess.TimeoutExpired:
        log.warning("docker logs timed out after %ss", DOCKER_TIMEOUT)
        return "", "docker logs timeout"
    except FileNotFoundError:
        log.warning("docker/wsl not found")
        return "", "docker/wsl missing"
    except OSError as e:
        log.warning("docker logs failed: %s", e)
        return "", "docker logs OSError"
    # docker writes the container's stdout and stderr to its own two streams; both carry the console text
    return (r.stdout or "") + (r.stderr or ""), ("" if r.returncode == 0 else "docker logs rc %s" % r.returncode)


def fetch_party_detail(opener=urllib.request.urlopen, base=None):
    """(list of party views, error text). A failed or malformed API answer is an error, never an empty party."""
    url = (base or API_BASE) + "/api/parties/detail"
    try:
        with opener(url, timeout=HTTP_TIMEOUT) as resp:
            data = json.loads(resp.read().decode("utf-8", errors="replace"))
    except (TimeoutError, urllib.error.URLError, http.client.HTTPException, OSError) as e:
        log.warning("engine API %s: %s: %s", url, type(e).__name__, e)
        return None, "api %s" % type(e).__name__
    except ValueError as e:
        log.warning("engine API answer is not JSON: %s", e)
        return None, "api bad json"
    if not isinstance(data, list):
        return None, "api answer is not a list"
    return data, ""


def members_of(party):
    return [m for m in (party.get("members") or []) if isinstance(m, dict)]


def is_ayla(m):
    return str(m.get("name", "")).lower() == NAME.lower()


def ayla_party(parties):
    """Ayla's party view: the one with a member named Ayla. Found by name, not by the human field: an engine party has
    no human (Party.Human is ""), so Ayla is an ordinary member there."""
    for p in parties or []:
        if isinstance(p, dict) and any(is_ayla(m) for m in members_of(p)):
            return p
    return None


def same_map(a, b):
    """the engine's sameMap (partyintent.go): case-insensitive, the .gat suffix ignored"""
    return str(a or "").lower().removesuffix(".gat") == str(b or "").lower().removesuffix(".gat")


def chebyshev(x0, y0, x1, y1):
    """the engine's dist (ai.go): the larger of the two axis gaps, in cells"""
    return max(abs(x1 - x0), abs(y1 - y0))


def read_meter_ayla(data_dir, now):
    """(Ayla's row of meter_bots.json, error text). The row is a meterBotOut from the "bots" list (name Ayla), or the
    "ayla" row when she is not listed; {} when the file is missing, stale or has no row."""
    path = os.path.join(data_dir, METER_BOTS)
    try:
        with open(path, encoding="utf-8") as f:
            data = json.load(f)
    except FileNotFoundError:
        return {}, "meter_bots.json missing"
    except (OSError, ValueError) as e:
        log.warning("meter_bots.json unreadable: %s: %s", type(e).__name__, e)
        return {}, "meter_bots.json %s" % type(e).__name__
    if not isinstance(data, dict):
        return {}, "meter_bots.json is not an object"
    try:
        age = now - float(data.get("t", 0))
    except (TypeError, ValueError):
        age = None
    if age is None or age > MAX_METER_AGE:
        return {}, "meter_bots.json stale"
    row = next((b for b in (data.get("bots") or []) if isinstance(b, dict) and is_ayla(b)), None)
    if row is None and isinstance(data.get("ayla"), dict):
        row = data["ayla"]
    if row is None:
        return {}, "meter_bots.json has no ayla row"
    return row, ""


def _int(v, default=-1):
    try:
        return int(v)
    except (TypeError, ValueError):
        return default


def _f(v):
    try:
        return round(float(v), 2)
    except (TypeError, ValueError):
        return None


def engine_view(party, meter, prev):
    """The engine-era fields from Ayla's party view, her meter row and the last recorded snapshot (prev). Pure.
    kills/deaths: per-member running totals, clamped deltas against prev; None without prev."""
    rows = members_of(party)
    me = next((m for m in rows if is_ayla(m)), {})
    others = [m for m in rows if not is_ayla(m)]
    ax, ay = _int(me.get("x"), None), _int(me.get("y"), None)
    amap = str(me.get("map") or "")
    dist = {}
    for m in others:
        n = str(m.get("name", "?"))
        mx, my = _int(m.get("x"), None), _int(m.get("y"), None)
        if (m.get("online") and None not in (ax, ay, mx, my) and same_map(m.get("map"), amap)):
            dist[n] = chebyshev(mx, my, ax, ay)
        else:
            dist[n] = -1
    totals = {str(m.get("name", "?")): {"kills": max(0, _int(m.get("kills"), 0)),
                                        "deaths": max(0, _int(m.get("deaths"), 0))} for m in rows}
    members_map = {str(m.get("name", "?")): str(m.get("map") or "") for m in rows}
    have_prev = prev is not None and isinstance(prev.get("totals"), dict)
    kills_5m = deaths_5m = None
    map_changed = []
    if have_prev:
        pt = prev["totals"]
        kills_5m = deaths_5m = 0
        for n, cur in totals.items():
            if n in pt and isinstance(pt[n], dict):
                kills_5m += max(0, cur["kills"] - _int(pt[n].get("kills"), 0))
                deaths_5m += max(0, cur["deaths"] - _int(pt[n].get("deaths"), 0))
        pm = prev.get("members_map") or {}
        map_changed = sorted(n for n, mp in members_map.items() if n in pm and pm[n] != mp)
    zeny = _int(me.get("zeny"))
    return {
        "map": amap or None,
        "x": ax, "y": ay,
        "lv": _int(me.get("level"), None),
        "zeny": zeny if zeny >= 0 else None,
        "party_zeny": sum(max(0, _int(m.get("zeny"), 0)) for m in others),
        "exp_5m": None,               # the engine API has no exp total for an engine party
        "exp_h": None,
        "kills_5m": kills_5m,
        "deaths_5m": deaths_5m,
        "kills_total": sum(t["kills"] for t in totals.values()),
        "deaths_total": sum(t["deaths"] for t in totals.values()),
        "map_changes": len(map_changed) if have_prev else None,
        "map_changed": map_changed,
        "members_map": members_map,
        "totals": totals,
        "distance": dist,
        "members": len(others),
        "kpm": _f(meter.get("kpm")),
        "zph": _f(meter.get("zph")),
        "deaths_h": _f(meter.get("deaths_h")),
    }


def openkore_view(status, logtext, log_err, party):
    """The OpenKore-era fields: exp, kills, deaths and map changes from the container log over 5 min; the distance is
    the API's dist_lead (filled for human-led parties)."""
    counts = parse_log(logtext)
    rows = members_of(party) if party else []
    dist = {str(m.get("name", "?")): _int(m.get("dist_lead"), -1) for m in rows if not is_ayla(m)}
    me = next((m for m in rows if is_ayla(m)), {})
    zeny = _int(status.get("zeny"), None) if status.get("zeny") is not None else None
    if zeny is None and _int(me.get("zeny"), -1) >= 0:
        zeny = _int(me.get("zeny"))
    return dict(counts, **{"map": status.get("map") or me.get("map"), "x": status.get("x"), "y": status.get("y"),
                           "zeny": zeny, "distance": dist, "members": len(dist), "log_error": log_err or None,
                           "kpm": None, "zph": None, "deaths_h": None, "exp_h": None})


def status_fields(status, age):
    """What the old aylawatch.sh printed from status.json: lock map, intent (60 chars) and the file's age."""
    intent = status.get("intent")
    return {"status_age": age, "lock_map": status.get("lock_map"),
            "intent": (str(intent)[:INTENT_CHARS] if intent is not None else None)}


def collect(era, now, status_path, data_dir, opener=urllib.request.urlopen, run=subprocess.run, prev=None):
    """One snapshot dict. Every source is read; a failed source is listed in errors and leaves its fields empty."""
    errors = []
    parties, err = fetch_party_detail(opener)
    if err:
        errors.append(err)
    party = ayla_party(parties) if parties is not None else None
    if parties is not None and party is None:
        errors.append("no party with a member named Ayla")
    status, age, serr = read_status(status_path, now)
    if serr:
        errors.append(serr)
    text, lerr = container_log(run)
    if lerr:
        errors.append(lerr)
    snap = {"t": int(now), "era": era, "errors": errors}
    snap.update(status_fields(status, age))
    snap["watchdog"] = parse_log(text)["watchdog"]
    if era == "engine":
        meter, merr = read_meter_ayla(data_dir, now)
        if merr:
            errors.append(merr)
        if party is not None:
            snap.update(engine_view(party, meter, prev))
        else:
            snap.update({"map": None, "x": None, "y": None, "zeny": None, "exp_5m": None, "exp_h": None,
                         "kills_5m": None, "deaths_5m": None, "map_changes": None, "members_map": {}, "totals": {},
                         "distance": {}, "members": 0, "kpm": None, "zph": None, "deaths_h": None})
    else:
        snap.update(openkore_view(status, text, lerr, party))
        snap["members_map"] = {}
        snap["totals"] = {}
    snap["ok"] = not errors
    return snap


def _show(v):
    return "n/a" if v is None else v


def summary(snap):
    """One printed line for the main session: the old aylawatch.sh fields plus the engine-era ones."""
    dist = " ".join("%s:%s" % (n[:6], d) for n, d in sorted((snap.get("distance") or {}).items()))
    wd = " ; ".join(snap.get("watchdog") or []) or "-"
    s = ("%s [%s] %s %s,%s lock=%s intent=%s | 5m: exp %s kills %s deaths %s mapchg %s | zeny %s | dist %s"
         " | kpm %s zph %s | wd %s | status %ss") % (
        time.strftime("%H:%M", time.localtime(snap["t"])), snap["era"], _show(snap.get("map")), _show(snap.get("x")),
        _show(snap.get("y")), _show(snap.get("lock_map")), _show(snap.get("intent")), _show(snap.get("exp_5m")),
        _show(snap.get("kills_5m")), _show(snap.get("deaths_5m")), _show(snap.get("map_changes")),
        _show(snap.get("zeny")), dist or "-", _show(snap.get("kpm")), _show(snap.get("zph")), wd,
        snap.get("status_age"))
    if snap.get("errors"):
        s += " | ERR " + "; ".join(snap["errors"])
    return s


def last_record(path):
    rows = C.read_jsonl(path)
    return rows[-1] if rows else None


def choose_era(args, data_dir):
    if args.era != "auto":
        return args.era
    return "engine" if aylaswitch.engine_on(os.path.join(data_dir, "tuning.json")) else "openkore"


def run_once(args, now=None, opener=urllib.request.urlopen, run=subprocess.run):
    now = C.now() if now is None else now
    data_dir = C.data_dir()
    era = choose_era(args, data_dir)
    rec_path = os.path.join(data_dir, RECORD)
    prev = last_record(rec_path) if not args.no_record else None
    status_path = os.path.join(logs_dir(), "status.json")
    snap = collect(era, now, status_path, data_dir, opener, run, prev)
    print(summary(snap), flush=True)
    if not args.no_record:
        C.append_jsonl(rec_path, snap)
    C.heartbeat(HEARTBEAT, "%s %s" % (era, "ok" if snap["ok"] else "errors: " + "; ".join(snap["errors"])))
    return snap


def main(argv=None):
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--once", action="store_true", help="one snapshot, then exit (takes no lock)")
    ap.add_argument("--interval", type=int, default=INTERVAL, help="seconds between snapshots (default 300)")
    ap.add_argument("--era", choices=("auto", "openkore", "engine"), default="auto")
    ap.add_argument("--no-record", action="store_true", help="print only; do not append to aylawatch.jsonl")
    args = ap.parse_args(argv)
    logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(message)s")
    if args.once:
        run_once(args)
        return 0
    if not C.take_lock(LOCK, LOCK_STALE):
        log.error("another Ayla watch holds %s: exiting", LOCK)
        return 3
    while True:
        if not C.refresh_lock(LOCK):
            log.error("lost %s to another holder: exiting", LOCK)
            return 3
        try:
            run_once(args)
        except SOURCE_ERRORS as e:
            log.exception("snapshot failed: %s", type(e).__name__)
            C.heartbeat(HEARTBEAT, "loop error %s" % type(e).__name__)
        time.sleep(max(1, args.interval))


if __name__ == "__main__":
    try:
        sys.exit(main())
    except KeyboardInterrupt:
        sys.exit(130)
