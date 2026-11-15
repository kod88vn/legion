#!/usr/bin/env python3
"""Ayla watch (plain code, no AI): a read-only snapshot of Ayla's party every 5 minutes.

  aylawatch.py                   loop: one snapshot every 5 min (--interval SEC), printed and appended to <data>/aylawatch.jsonl
  aylawatch.py --once            one snapshot, printed and recorded (--no-record: printed only)
  aylawatch.py --era auto        auto (default) reads the tuning key engine_parties (tools/puppet/aylaswitch.py):
                                 engine when it names ayla, else openkore. --era openkore|engine forces one.

Each snapshot holds: exp (5 min), kills, deaths, map changes, zeny, and each party member's distance to the leader.

Sources, by era:
  openkore  Ayla's status.json (<Ayla logs>/status.json: map, x, y, zeny, lock_map, intent) and the last 5 min of her
            container log (wsl docker logs ro-openkore-ayla): exp gained, kills, deaths, map changes.
  engine    her party in GET <LEGION_API>/api/parties/detail (kills, deaths, zeny, map, exp/h per member, dist_lead)
            and the Ayla row of <data>/meter_bots.json (exp/h, kills, zeny, map). Map changes are the members whose map
            differs from the previous recorded snapshot (5-minute resolution only).
  Distance to the leader always comes from the engine API (dist_lead, -1 = another map or unknown).

Read only. It sends no command to any character, to OpenKore or to the engine, and never touches Ayla's AI (G05).
It reads live running state, so it is run from the live checkout by an agent; tests use temp folders (G23).
Plain code, no AI (Constitution 3.8): it writes <data>/aylawatch.heartbeat every round. Stdlib only.
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
import common as C  # noqa: E402  the one resolver for live locations, the heartbeat and the docker prefix (G23)
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
RECORD = "aylawatch.jsonl"
METER_BOTS = "meter_bots.json"
MAX_METER_AGE = 3600      # a meter_bots.json older than this is not read as current

RE_EXP = re.compile(r"You have gained (\d+)/")
RE_KILL = re.compile(r"Target Monster .* died")
RE_DEATH = "You have died"
RE_MAPCHG = re.compile(r"Map Change")
RE_ANSI = re.compile(r"\x1b\[[0-9;]*[mK]")


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
    """(status dict, age seconds, error text). The dict is {} when the file is missing or unreadable."""
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


def ayla_party(parties):
    """Ayla's party view from /api/parties/detail (the party whose human is Ayla), or None."""
    for p in parties or []:
        if isinstance(p, dict) and str(p.get("human", "")).lower() == NAME.lower():
            return p
    return None


def read_meter_ayla(data_dir, now):
    """(Ayla's row of meter_bots.json, error text). Empty when the file is missing, old or has no Ayla row."""
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
    ayla = data.get("ayla")
    if not isinstance(ayla, dict):
        return {}, "meter_bots.json has no ayla row"
    try:
        age = now - float(data.get("t", 0))
    except (TypeError, ValueError):
        age = None
    if age is None or age > MAX_METER_AGE:
        return {}, "meter_bots.json stale"
    return ayla, ""


def _int(v, default=-1):
    try:
        return int(v)
    except (TypeError, ValueError):
        return default


def engine_view(party, meter, prev):
    """The engine-era snapshot fields from her party view, her meter row and the last recorded snapshot (prev).
    kills/deaths are the change since prev (the API counts are running totals); map changes are members whose map
    differs from prev (5-minute resolution). Pure."""
    members = [m for m in (party.get("members") or []) if isinstance(m, dict) and not m.get("human")]
    leader = next((m for m in (party.get("members") or []) if isinstance(m, dict) and m.get("human")), {})
    maps = {str(m.get("name", "?")): str(m.get("map", "")) for m in members}
    maps[NAME] = str(leader.get("map") or party.get("map") or "")
    prev_maps = (prev or {}).get("members_map") or {}
    changed = sorted(n for n, mp in maps.items() if n in prev_maps and prev_maps[n] != mp)
    zeny = _int(leader.get("zeny"))
    if zeny < 0:
        zeny = _int(meter.get("zeny"))
    kills_total = sum(max(0, _int(m.get("kills"), 0)) for m in members)
    deaths_total = sum(max(0, _int(m.get("deaths"), 0)) for m in members)
    have_prev = prev is not None and "kills_total" in prev
    own_exp = max(0, _int(meter.get("exp_h"), 0))
    exp_h = own_exp + sum(max(0, _int(m.get("exp_h"), 0)) for m in members)
    return {
        "map": maps[NAME],
        "x": _int(leader.get("x"), None),
        "y": _int(leader.get("y"), None),
        "zeny": zeny if zeny >= 0 else None,
        "party_zeny": sum(max(0, _int(m.get("zeny"), 0)) for m in members),
        "exp_h": exp_h,
        "exp_5m": int(round(exp_h * 5 / 60.0)),         # estimate from the exp/h figures (no per-5-min counter here)
        "kills_5m": (kills_total - _int(prev.get("kills_total"), 0)) if have_prev else None,
        "deaths_5m": (deaths_total - _int(prev.get("deaths_total"), 0)) if have_prev else None,
        "kills_total": kills_total,
        "deaths_total": deaths_total,
        "map_changes": len(changed) if prev is not None else None,
        "map_changed": changed,
        "members_map": maps,
        "distance": {str(m.get("name", "?")): _int(m.get("dist_lead"), -1) for m in members},
        "members": len(members),
        "meter_ayla": {k: meter.get(k) for k in ("exp_h", "kills", "zeny", "map", "kpm_plugin") if k in meter},
    }


def openkore_view(status, status_age, logtext, log_err, party):
    """The OpenKore-era snapshot fields. Distance comes from the engine API party view when there is one."""
    counts = parse_log(logtext)
    dist = {}
    if party:
        for m in party.get("members") or []:
            if isinstance(m, dict) and not m.get("human"):
                dist[str(m.get("name", "?"))] = _int(m.get("dist_lead"), -1)
    zeny = status.get("zeny")
    return dict(counts, **{
        "map": status.get("map"),
        "x": status.get("x"),
        "y": status.get("y"),
        "lock_map": status.get("lock_map"),
        "intent": str(status.get("intent"))[:60] if status.get("intent") is not None else None,
        "zeny": _int(zeny, None) if zeny is not None else None,
        "status_age": status_age,
        "distance": dist,
        "log_error": log_err,
    })


def collect(era, now, status_path, data_dir, opener=urllib.request.urlopen, run=subprocess.run, prev=None):
    """One snapshot dict. Every source is read; a failed source is listed in errors and leaves its fields empty."""
    errors = []
    parties, err = fetch_party_detail(opener)
    if err:
        errors.append(err)
    party = ayla_party(parties) if parties is not None else None
    if parties is not None and party is None:
        errors.append("no party with human Ayla")
    snap = {"t": int(now), "era": era, "errors": errors}
    if era == "engine":
        meter, merr = read_meter_ayla(data_dir, now)
        if merr:
            errors.append(merr)
        if party is not None:
            snap.update(engine_view(party, meter, prev))
        else:
            snap.update({"map": None, "exp_h": None, "exp_5m": None, "kills_5m": None, "deaths_5m": None,
                         "map_changes": None, "members_map": {}, "distance": {}, "zeny": None, "party_zeny": None,
                         "members": 0})
    else:
        status, age, serr = read_status(status_path, now)
        if serr:
            errors.append(serr)
        text, lerr = container_log(run)
        if lerr:
            errors.append(lerr)
        snap.update(openkore_view(status, age, text, lerr, party))
        snap["members_map"] = {}
    snap["ok"] = not errors
    return snap


def summary(snap):
    """One printed line for the main session."""
    dist = " ".join("%s:%s" % (n[:6], d) for n, d in sorted((snap.get("distance") or {}).items()))
    s = "%s [%s] %s %s,%s | 5m exp %s kills %s deaths %s mapchg %s | zeny %s | dist %s" % (
        time.strftime("%H:%M", time.localtime(snap["t"])), snap["era"], snap.get("map"), snap.get("x"), snap.get("y"),
        snap.get("exp_5m"), snap.get("kills_5m"), snap.get("deaths_5m"), snap.get("map_changes"), snap.get("zeny"),
        dist or "-")
    if snap.get("errors"):
        s += " | ERR " + "; ".join(snap["errors"])
    return s


def last_record(path):
    rows = C.read_jsonl(path)
    return rows[-1] if rows else None


def run_once(args, now=None, opener=urllib.request.urlopen, run=subprocess.run):
    now = C.now() if now is None else now
    data_dir = C.data_dir()
    era = args.era if args.era != "auto" else ("engine" if aylaswitch.engine_on(os.path.join(data_dir, "tuning.json"))
                                              else "openkore")
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
    ap.add_argument("--once", action="store_true", help="one snapshot, then exit")
    ap.add_argument("--interval", type=int, default=INTERVAL, help="seconds between snapshots (default 300)")
    ap.add_argument("--era", choices=("auto", "openkore", "engine"), default="auto")
    ap.add_argument("--no-record", action="store_true", help="print only; do not append to aylawatch.jsonl")
    args = ap.parse_args(argv)
    logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(message)s")
    if args.once:
        run_once(args)
        return 0
    while True:
        try:
            run_once(args)
        except OSError as e:
            log.error("snapshot failed: %s: %s", type(e).__name__, e)
            C.heartbeat(HEARTBEAT, "loop error %s" % type(e).__name__)
        time.sleep(max(1, args.interval))


if __name__ == "__main__":
    try:
        sys.exit(main())
    except KeyboardInterrupt:
        sys.exit(130)
