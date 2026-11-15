#!/usr/bin/env python3
"""Standalone dashboard server (DB05, DB06): serves legion/dashboard.html and stays up when the stack is down.

  python tools/dashboard/dashserve.py [--port 8099] [--bind 127.0.0.1] [--engine http://127.0.0.1:8090]

  GET  /  (also /dashboard)   the page, read from legion/dashboard.html on every request (no engine deploy needed)
  GET  /status                what is up and what is down, from sources that do not need the engine: container
                              state, heartbeat files, bug queue, deploy log, dispatcher log, markers
  *    /api/...               forwarded to the engine. When the engine does not answer, a GET gets the last good
                              answer (header X-Dash-Stale: <age in seconds>), or 503 {"down": true} if there is none.

It only reads the project's files. It starts nothing: container state comes from `docker ps`, and when the WSL
distro is not running it is not woken up (the containers are then reported as down from the port probes).
It writes two things: its own heartbeat (<data>/dashboard.heartbeat) and the answer cache (tools/.state/dashcache).
Standard library only; runs on Windows Python and on python3 in WSL. LEGION_DATA moves the data dir (tests).
"""
import argparse
import datetime
import glob
import hashlib
import json
import os
import re
import shutil
import socket
import subprocess
import sys
import threading
import time
import urllib.error
import urllib.parse
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

HERE = os.path.dirname(os.path.abspath(__file__))
REPO = os.path.abspath(os.path.join(HERE, "..", ".."))
sys.path.insert(0, os.path.join(REPO, "tools", "system"))
import common as C  # noqa: E402
import bugq  # noqa: E402
sys.path.insert(0, HERE)
import dbsnap  # noqa: E402
import offline  # noqa: E402

PAGE = os.path.join(REPO, "legion", "dashboard.html")
WSL_DISTRO = C.WSL_DISTRO   # one config for WSL (common.py, LEGION_WSL_DISTRO)
ENGINE = "http://127.0.0.1:8090"
CACHE_DIR = os.environ.get("DASH_CACHE") or os.path.join(REPO, "tools", ".state", "dashcache")
ENGINE_TIMEOUT = 8
CACHE_MAX_BYTES = 8 << 20
SNAP_DIR = os.environ.get("DASH_SNAP") or os.path.join(REPO, "tools", ".state", "dbsnap")
SNAP_EVERY = 600   # seconds between database snapshots while ro-db runs

# container name, what it is, host ports that answer when it is up (docker-compose.yml)
CONTAINERS = [
    ("ro-db", "database", [3306]),
    ("ro-rathena", "game server", [6900, 6121, 5121]),
    ("ro-agency", "login agency", [6953]),
    ("ro-openkore-ayla", "Ayla's OpenKore", [6901]),
    ("ro-legion", "Legion engine", [8090]),
    ("ro-legion-core", "Legion core (Ayla's party)", []),
]
# key, label, heartbeat file names (first that exists wins), seconds after which it counts as stale, script
LOOPS = [
    ("watchdog", "Watchdog", ["watchdog.heartbeat"], 300, ""),
    ("sentinel", "Sentinel", ["sentinel.heartbeat"], 300, "tools/sentinel.py"),
    ("partycheck", "Party check", ["partycheck.heartbeat"], 300, "tools/system/partycheck.py"),
    ("dispatcher", "Puppet dispatcher", ["dispatch.heartbeat", "dispatcher.heartbeat", "puppet.heartbeat"], 180,
     "tools/puppet/dispatch.py"),
    ("aylalog", "Ayla log watcher", ["aylalog.heartbeat"], 300, "tools/system/aylalog.py"),
    ("bugq-watch", "Bug queue watch", ["bugq-watch.heartbeat"], 300, "tools/system/bugq.py watch"),
    ("unwall", "Unwall loop", ["unwall.heartbeat"], 600, "tools/unwall.py"),
    ("keeper", "Ayla keeper", ["keeper.heartbeat"], 300, "tools/rogui/keeper.sh"),
    ("dashboard", "Dashboard server", ["dashboard.heartbeat"], 120, "tools/dashboard/dashserve.py"),
]


def tail_jsonl(path, max_bytes=400_000):
    """The JSON objects in the last max_bytes of a .jsonl file (the log files grow without bound)."""
    out = []
    try:
        size = os.path.getsize(path)
        with open(path, "rb") as f:
            if size > max_bytes:
                f.seek(size - max_bytes)
                f.readline()
            for line in f:
                try:
                    o = json.loads(line.decode("utf-8", "replace"))
                except ValueError:
                    continue
                if isinstance(o, dict):
                    out.append(o)
    except OSError:
        pass
    return out


def read_json(path):
    try:
        with open(path, "r", encoding="utf-8") as f:
            return json.load(f)
    except (OSError, ValueError):
        return None


def mtime(path):
    try:
        return os.path.getmtime(path)
    except OSError:
        return None


# ----------------------------------------------------------------- containers
def port_open(port, timeout=0.4):
    try:
        with socket.create_connection(("127.0.0.1", port), timeout=timeout):
            return True
    except OSError:
        return False


def wsl_running():
    """True/False on Windows (never starts the distro), None when not on Windows."""
    if os.name != "nt":
        return None
    try:
        p = subprocess.run(["wsl.exe", "-l", "--running", "-q"], capture_output=True, timeout=8)
    except (OSError, subprocess.TimeoutExpired):
        return False
    txt = p.stdout.decode("utf-16-le", "replace") if b"\x00" in p.stdout else p.stdout.decode("utf-8", "replace")
    return WSL_DISTRO.lower() in [l.strip().lower() for l in txt.splitlines()]


def docker_ps():
    """{container name: (state, status text)} or None when docker cannot be asked. Read-only."""
    fmt = "{{.Names}}\t{{.State}}\t{{.Status}}"
    if os.name == "nt":
        if not wsl_running():
            return None
        cmd = ["wsl.exe", "-d", WSL_DISTRO, "--", "docker", "ps", "-a", "--format", fmt]
    elif shutil.which("docker"):
        cmd = ["docker", "ps", "-a", "--format", fmt]
    else:
        return None
    try:
        p = subprocess.run(cmd, capture_output=True, text=True, timeout=12, errors="replace")
    except (OSError, subprocess.TimeoutExpired):
        return None
    if p.returncode != 0:
        return None
    out = {}
    for line in p.stdout.splitlines():
        parts = line.split("\t")
        if len(parts) >= 2:
            out[parts[0].strip()] = (parts[1].strip(), parts[2].strip() if len(parts) > 2 else "")
    return out


class ContainerWatch(threading.Thread):
    """Asks docker, the ports and the engine every 10 s in the background, so a slow answer (a closed port takes
    about 2 s to refuse on Windows) never blocks a page request."""

    def __init__(self, enabled=True, engine=None):
        super().__init__(daemon=True)
        self.enabled, self.engine = enabled, engine
        self.lock = threading.Lock()
        self.ps, self.ps_t, self.why, self.probes = None, 0.0, "not asked yet", {}
        self.snap_enabled, self.snap_busy, self.snap_err, self.skill_try = enabled, False, "", 0.0

    def poll(self):
        ps = docker_ps() if self.enabled else None
        why = ""
        if ps is None:
            if not self.enabled:
                why = "docker not asked (--no-docker)"
            elif os.name == "nt" and not wsl_running():
                why = "WSL (%s) is not running" % WSL_DISTRO
            else:
                why = "docker did not answer"
        ports = sorted({p for _, _, ps_ in CONTAINERS for p in ps_})
        res = {}
        ts = [threading.Thread(target=lambda p=p: res.__setitem__(p, port_open(p, 1.0)), daemon=True) for p in ports]
        for t in ts:
            t.start()
        for t in ts:
            t.join(3)
        with self.lock:
            self.ps, self.ps_t, self.why, self.probes = ps, time.time(), why, res
        if self.engine:
            self.engine.probe()
        if ps is not None:
            self.snapshots(ps)

    def running(self, name):
        with self.lock:
            ps = self.ps
        return bool(ps) and ps.get(name, ("",))[0] == "running"

    def snapshots(self, ps):
        """a read-only database snapshot every SNAP_EVERY s while ro-db runs; skill names once from ro-rathena"""
        if not self.snap_enabled:
            return
        if ps.get("ro-db", ("",))[0] == "running" and not self.snap_busy:
            last = dbsnap.latest(SNAP_DIR)
            if not last or time.time() - (mtime(last) or 0) >= SNAP_EVERY:
                self.snap_busy = True
                threading.Thread(target=self._snap, daemon=True).start()
        sk = os.path.join(SNAP_DIR, "skills.tsv")
        if ps.get("ro-rathena", ("",))[0] == "running" and not os.path.exists(sk) and time.time() - self.skill_try > 600:
            self.skill_try = time.time()
            try:
                dbsnap.fetch_skill_names(sk)
            except Exception as e:
                self.snap_err = "skill names: %s" % e

    def _snap(self):
        try:
            dbsnap.take(SNAP_DIR)
            self.snap_err = ""
        except Exception as e:
            self.snap_err = str(e)[:200]
        finally:
            self.snap_busy = False

    def run(self):
        while True:
            try:
                self.poll()
            except Exception as e:  # never die
                with self.lock:
                    self.ps, self.why = None, repr(e)[:120]
            time.sleep(10)

    def rows(self):
        with self.lock:
            ps, why, seen = self.ps, self.why, self.probes
        rows = []
        for name, label, ports in CONTAINERS:
            probes = {str(p): bool(seen.get(p)) for p in ports}
            if ps is not None:
                state, status = ps.get(name, ("missing", "no such container"))
                src = "docker"
            elif ports:
                state, status, src = ("running" if all(probes.values()) else "down"), "", "ports"
            else:
                state, status, src = "unknown", "", "none"
            if ps is None and state != "running" and why:
                status = why
            rows.append({"name": name, "label": label, "state": state, "status": status, "source": src,
                         "ports": probes, "up": state == "running"})
        return rows, why


# ----------------------------------------------------------------- files
def loops(now):
    rows, seen = [], set()
    for key, label, files, stale, script in LOOPS:
        age, used = None, None
        for fn in files:
            seen.add(fn)
            a = C.heartbeat_age(C.dpath(fn))
            if a is not None and (age is None or a < age):
                age, used = a, fn
        note = C.heartbeat_note(C.dpath(used)) if used else ""
        if age is None:
            state = "missing"
        elif bugq.WAITING_NOTE in note:   # the bug queue watch exits on purpose after a wake: waiting, not dead
            state = "waiting"
        else:
            state = "ok" if age <= stale else "stale"
        rows.append({"key": key, "label": label, "file": used or files[0], "age_s": None if age is None else int(age),
                     "last": None if age is None else int(now - age), "stale_after_s": stale, "state": state,
                     "note": note, "script": script, "up": state in ("ok", "waiting")})
    # heartbeat files nobody listed here: shown as they are, so a new script appears without a dashboard change
    for p in sorted(glob.glob(C.dpath("*.heartbeat"))):
        fn = os.path.basename(p)
        if fn in seen:
            continue
        age = C.heartbeat_age(p)
        if age is None:
            continue
        rows.append({"key": fn[:-10], "label": fn[:-10], "file": fn, "age_s": int(age), "last": int(now - age),
                     "stale_after_s": 300, "state": "ok" if age <= 300 else "stale", "script": "", "up": age <= 300})
    return rows


def watchdog_block(now):
    """tools/system/watchdog.py: its live status (every pass) and the newest 30-minute record. A missing record is
    an alarm here (Constitution 3.8)."""
    status = read_json(C.dpath("watchdog.status.json"))
    recs = tail_jsonl(C.dpath("watchdog-reports.jsonl"), 300_000)
    last = recs[-1] if recs else None
    age = None if not last or not isinstance(last.get("t"), (int, float)) else int(now - last["t"])
    alarm = None
    if last is None:
        alarm = "no 30-minute report on record yet"
    elif age is not None and age > 35 * 60:
        alarm = "30-minute report missing: the newest is %d min old" % (age // 60)
    return {"available": status is not None or last is not None, "status": status, "last_report": last,
            "report_age_s": age, "report_every_s": 1800, "alarm": alarm}


def markers():
    out = {}
    for name in ("stack.up", "stack.hold", "puppet.pause"):
        out[name] = mtime(C.dpath(name))
    out["mindgate.paused"] = mtime(os.path.join(REPO, "tools", "mindgate.paused"))
    return out


def bug_queue(now):
    path = bugq.qpath()
    if not os.path.exists(path):
        return {"available": False, "why": "no bug queue file"}
    try:
        entries = list(bugq.load().values())
    except Exception as e:
        return {"available": False, "why": repr(e)[:160]}
    live = [e for e in entries if str(e.get("status", "open")).lower() not in ("closed", "duplicate")]
    unassigned = [e for e in live if str(e.get("status", "open")).lower() != "assigned" and not e.get("assigned_to")]
    by_prio, by_from = {}, {}
    for e in live:
        by_prio[str(e.get("priority", "normal")).lower()] = by_prio.get(str(e.get("priority", "normal")).lower(), 0) + 1
        by_from[str(e.get("from", "?"))] = by_from.get(str(e.get("from", "?")), 0) + 1
    try:
        ranked = bugq.rank(live)
    except Exception:
        ranked = live
    ages = [now - bugq._t(e) for e in unassigned if bugq._t(e)]

    def slim(e):
        return {"id": e.get("id"), "t": bugq._t(e) or None, "from": e.get("from"), "priority": e.get("priority"),
                "status": e.get("status", "open"), "assigned_to": e.get("assigned_to"), "count": e.get("count"),
                "bots": e.get("bots"), "text": str(e.get("text", ""))[:300]}
    return {"available": True, "total": len(entries), "open": len(live), "unassigned": len(unassigned),
            "assigned": len(live) - len(unassigned), "closed": len(entries) - len(live),
            "by_priority": by_prio, "by_from": by_from, "file_t": mtime(path),
            "oldest_unassigned_age_s": int(max(ages)) if ages else None, "top": [slim(e) for e in ranked[:25]]}


def deploys():
    all_recs = C.read_jsonl(C.dpath("deploys.jsonl"))
    recs = [r for r in all_recs if r.get("kind") != "verdict"]   # keep/revert lines (league.py) are not deploys
    verdicts = [r for r in all_recs if r.get("kind") == "verdict"]
    if not recs:
        return {"available": False, "why": "no deploy record"}

    def ep(r):
        return C.to_epoch(r.get("epoch", r.get("t"))) or 0.0
    recs.sort(key=ep)

    def verdict_of(r):
        """the latest keep/revert verdict written for this deploy (same tag and epoch); None until one exists"""
        vs = [v for v in verdicts if v.get("tag") == r.get("tag") and abs((C.to_epoch(v.get("deploy_epoch")) or 0) - ep(r)) < 1]
        if not vs:
            return None
        return max(vs, key=lambda v: C.to_epoch(v.get("t")) or 0).get("verdict")

    def slim(r):
        return {"t": ep(r) or None, "tag": r.get("tag"), "services": r.get("services"), "critical": r.get("critical"),
                "reason": r.get("reason"), "fixes": r.get("fixes") or [], "verdict": verdict_of(r)}
    return {"available": True, "count": len(recs), "last": slim(recs[-1]), "recent": [slim(r) for r in recs[-8:][::-1]]}


def dispatcher(now):
    hb = read_json(C.dpath("puppet_heartbeat.json"))
    calls = [r for r in tail_jsonl(C.dpath("puppet_calls.jsonl")) if not r.get("dry")]
    roles = {}
    if isinstance(hb, dict):
        for r, v in (hb.get("roles") or {}).items():
            roles[r] = {"last": (v or {}).get("last"), "spent_1h": (v or {}).get("spent_1h")}
    for r in calls:
        role = r.get("role")
        if not role:
            continue
        d = roles.setdefault(role, {"last": None, "spent_1h": None})
        d["calls_seen"] = d.get("calls_seen", 0) + 1
        d["last_call"] = {k: r.get(k) for k in ("t", "trigger", "model", "exit", "is_error", "duration_s", "cost_usd",
                                                "turns", "result")}
        if r.get("t") and (d["last"] is None or r["t"] > d["last"]):
            d["last"] = r["t"]
    recent = [{k: r.get(k) for k in ("t", "role", "trigger", "model", "exit", "is_error", "duration_s", "cost_usd",
                                     "result")} for r in calls[-60:][::-1]]
    out = {"available": isinstance(hb, dict) or bool(calls), "roles": roles, "recent_calls": recent}
    if isinstance(hb, dict):
        out.update({"t": hb.get("t"), "state": hb.get("state"), "api_ok": hb.get("api_ok"), "pid": hb.get("pid"),
                    "running": hb.get("running") or [],
                    "age_s": int(now - hb["t"]) if isinstance(hb.get("t"), (int, float)) else None})
    return out


def meter_file(now):
    path = C.dpath("meter.jsonl")
    t = mtime(path)
    if t is None:
        return {"available": False, "why": "no meter history file"}
    rows = tail_jsonl(path, 200_000)
    last = rows[-1] if rows else {}
    return {"available": True, "file_t": t, "file_age_s": int(now - t), "last_row_t": last.get("t"),
            "rows_in_tail": len(rows)}


def reports():
    files = sorted(glob.glob(os.path.join(REPO, "docs", "reports", "league-*.md")), key=lambda p: mtime(p) or 0)
    if not files:
        return {"league": None}
    return {"league": {"file": os.path.basename(files[-1]), "t": mtime(files[-1]), "count": len(files)}}


def ayla_file(now):
    t = mtime(os.path.join(REPO, "openkore-ayla", "logs", "status.json"))
    return {"status_t": t, "status_age_s": None if t is None else int(now - t)}


# ----------------------------------------------------------------- engine proxy
class Engine:
    def __init__(self, base):
        self.base = base.rstrip("/")
        self.lock = threading.Lock()
        self.last_ok, self.last_err, self.err = 0.0, 0.0, "not asked yet"

    def note(self, ok, err=""):
        with self.lock:
            if ok:
                self.last_ok, self.err = time.time(), ""
            else:
                self.last_err, self.err = time.time(), err

    def state(self):
        with self.lock:
            return {"url": self.base, "up": self.last_ok > self.last_err and time.time() - self.last_ok < 60,
                    "last_ok": self.last_ok or None, "error": self.err}

    def just_failed(self, within=5):
        """True right after a failed call: page polls then get the saved answer at once instead of each waiting
        for the refusal (about 2 s on Windows)."""
        with self.lock:
            return self.last_err > self.last_ok and time.time() - self.last_err < within

    def probe(self):
        """a cheap call for /status when no page request has reached the engine lately"""
        with self.lock:
            fresh = time.time() - max(self.last_ok, self.last_err) < 10
        if fresh:
            return
        try:
            with urllib.request.urlopen(self.base + "/api/stats", timeout=4) as r:
                r.read(1 << 16)
            self.note(True)
        except Exception as e:
            self.note(False, short_err(e))

    def call(self, method, path, body=None, ctype=None):
        req = urllib.request.Request(self.base + path, data=body, method=method)
        if ctype:
            req.add_header("Content-Type", ctype)
        try:
            with urllib.request.urlopen(req, timeout=ENGINE_TIMEOUT) as r:
                data = r.read()
                self.note(True)
                return r.status, r.headers.get("Content-Type", "application/json"), data
        except urllib.error.HTTPError as e:  # the engine answered: it is up, pass the answer on
            self.note(True)
            return e.code, e.headers.get("Content-Type", "text/plain"), e.read()
        except Exception as e:
            self.note(False, short_err(e))
            return None, None, short_err(e).encode()


def short_err(e):
    r = getattr(e, "reason", None)
    return str(r if r is not None else e)[:160]


def cache_key(path):
    """file name for a GET answer worth keeping; None for incremental polls (since=N, N > 0)"""
    m = re.search(r"[?&]since=(\d+)", path)
    if m and m.group(1) != "0":
        return None
    name = re.sub(r"[^A-Za-z0-9._-]+", "_", path.strip("/"))[:80]
    return name + "-" + hashlib.sha1(path.encode()).hexdigest()[:8] + ".json"


def cache_put(path, ctype, data):
    key = cache_key(path)
    if not key or len(data) > CACHE_MAX_BYTES or "json" not in (ctype or ""):
        return
    try:
        os.makedirs(CACHE_DIR, exist_ok=True)
        tmp = os.path.join(CACHE_DIR, key + ".tmp%d" % threading.get_ident())
        with open(tmp, "wb") as f:
            f.write(data)
        os.replace(tmp, os.path.join(CACHE_DIR, key))
    except OSError:
        pass


def cache_get(path):
    key = cache_key(path)
    if not key:
        return None
    p = os.path.join(CACHE_DIR, key)
    try:
        with open(p, "rb") as f:
            return os.path.getmtime(p), f.read()
    except OSError:
        return None


def go_time(v):
    """Epoch seconds of a Go time.Time in JSON (RFC 3339 with nanoseconds and an offset); None if unreadable."""
    if not v or str(v).startswith("0001-"):
        return None
    s = re.sub(r"(\.\d{6})\d+", r"\1", str(v).replace("Z", "+00:00"))
    try:
        return datetime.datetime.fromisoformat(s).timestamp()
    except ValueError:
        return C.to_epoch(s[:19])


def minds_from_file(path):
    """/api/minds and /api/minds/all/thoughts answered from the engine's own save file (legion/minds.go writes
    <data>/minds.json every 30 s), for when the engine is down. Returns (file time, bytes) or None."""
    route = path.split("?")[0].rstrip("/")
    if route not in ("/api/minds", "/api/minds/all/thoughts"):
        return None
    p = C.dpath("minds.json")
    st, t = read_json(p), mtime(p)
    if not isinstance(st, dict) or t is None:
        return None
    thoughts = [x for x in st.get("thoughts") or [] if isinstance(x, dict)]
    messages = [x for x in st.get("messages") or [] if isinstance(x, dict)]
    if route.endswith("/thoughts"):
        m = re.search(r"[?&]since=(\d+)", path)
        since = int(m.group(1)) if m else 0
        out = {"mind": "all", "thoughts": [x for x in thoughts if x.get("id", 0) > since][-500:],
               "messages": messages[-500:], "next": int(st.get("next_id") or 1) - 1}
        return t, json.dumps(out).encode()
    info = st.get("minds") or {}
    names = set(info) | {m.get("mind") for m in messages if m.get("mind")} | {x.get("mind") for x in thoughts if x.get("mind")}
    out = []
    for n in sorted(names):
        mi = info.get(n) or {}
        seen = go_time(mi.get("last_seen"))
        row = {"name": n, "status": "idle" if seen and seen > 0 else "never",
               "unread": sum(1 for m in messages if m.get("mind") == n and not m.get("read")),
               "thoughts": sum(1 for x in thoughts if x.get("mind") == n), "status_line": mi.get("status_line", "")}
        if seen and seen > 0:
            row["last_seen"], row["seen_ago_s"] = mi.get("last_seen"), int(max(0, time.time() - seen))
        out.append(row)
    out.sort(key=lambda r: (r["status"] == "never", r.get("seen_ago_s", 0)))
    return t, json.dumps(out).encode()


# ----------------------------------------------------------------- status
# engine route -> (stand-in from files, the file whose time is the data's age)
FILE_ROUTES = {
    "/api/leaderboard": (lambda q: offline.leaderboard(int(q.get("limit", ["0"])[0] or 0)), "levels.json"),
    "/api/guild-rank": (lambda q: offline.guild_rank(), "guild_rank.json"),
    "/api/guilds/detail": (lambda q: offline.guilds_detail(), "guild_rank.json"),
    "/api/friends": (lambda q: offline.friends(int(q.get("n", ["15"])[0] or 15)), "friends.json"),
    "/api/meter": (lambda q: offline.meter(), "meter.jsonl"),
    "/api/meter/bots": (lambda q: offline.meter_bots(q), "meter_bots.json"),
    "/api/overview": (lambda q: offline.overview(), "overview.json"),
    "/api/meter/neo": (lambda q: offline.meter_neo(), "meter.jsonl"),
    "/api/overview/history": (lambda q: offline.history(), "health.log"),
    "/api/parties/detail": (lambda q: offline.parties_detail(PLAYERS[0].current() if PLAYERS else None), "parties.json"),
}


PLAYERS = []   # the server's Players object, for stand-ins that use the database snapshot


def from_files(path):
    """(file time, bytes) for an engine route the files can stand in for, else None"""
    route, _, qs = path.partition("?")
    hit = FILE_ROUTES.get(route.rstrip("/"))
    if not hit:
        return None
    try:
        obj = hit[0](urllib.parse.parse_qs(qs))
    except Exception:
        obj = None
    if obj is None:
        return None
    return mtime(C.dpath(hit[1])) or time.time(), json.dumps(obj).encode()


class Players:
    """the newest database snapshot (reloaded when a newer one appears) and the Players tab's answers (DB07)"""

    def __init__(self, watch):
        self.watch, self.lock, self.snap, self.err = watch, threading.Lock(), None, ""
        PLAYERS[:] = [self]

    def current(self):
        p = dbsnap.latest(SNAP_DIR)
        with self.lock:
            if p and (self.snap is None or self.snap.path != p):
                try:
                    self.snap = dbsnap.Snapshot(p)
                    self.err = ""
                except Exception as e:
                    self.err = "snapshot unreadable: %s" % e
            return self.snap

    def info(self):
        s = self.current()
        return {"snapshot": {"t": s.t, "file": os.path.basename(s.path), "counts": s.counts, "source": s.source} if s else None,
                "db_up": self.watch.running("ro-db"), "snap_error": self.watch.snap_err or self.err,
                "snap_every_s": SNAP_EVERY, "skill_names": bool(offline.skills(SNAP_DIR)),
                "item_names": len(offline.items())}

    def online_flag_valid(self):
        """char.online is set by the map server; while ro-rathena is not running it is left over, not true"""
        return self.watch.running("ro-rathena")

    def search(self, q, limit):
        out = offline.search(self.current(), q, limit)
        if not self.online_flag_valid():
            for r in out["rows"]:
                r["online"] = None
        out.update(self.info())
        out["online_flag"] = "game server running" if self.online_flag_valid() else "unknown: the game server is not running"
        return out

    def char(self, char_id, name):
        live, err = None, ""
        if char_id and self.watch.running("ro-db"):
            try:
                live = dbsnap.live_char(char_id)
            except Exception as e:
                err = str(e)[:200]
        out = offline.character(self.current(), live, char_id, name, offline.skills(SNAP_DIR))
        if out.get("char") and not self.online_flag_valid():
            out["char"]["online"] = None
        out["live_error"] = err
        out.update(self.info())
        return out


def build_status(engine, watch, started):
    now = C.now()
    containers, docker_why = watch.rows()
    lp = loops(now)
    st = {
        "t": now, "server": {"started": started, "uptime_s": int(time.time() - started), "pid": os.getpid(),
                             "data_dir": C.data_dir(), "data_dir_ok": os.path.isdir(C.data_dir())},
        "engine": engine.state(),
        "containers": containers, "docker": {"asked": docker_why == "", "why": docker_why},
        "loops": lp, "markers": markers(),
        "bugq": bug_queue(now), "deploy": deploys(), "dispatcher": dispatcher(now),
        "meter": meter_file(now), "reports": reports(), "ayla": ayla_file(now), "watchdog": watchdog_block(now),
    }
    st["summary"] = {"containers_up": sum(1 for c in containers if c["up"]), "containers": len(containers),
                     "loops_up": sum(1 for l in lp if l["up"]), "loops": len(lp)}
    return st


class Handler(BaseHTTPRequestHandler):
    server_version = "dashserve/1"
    protocol_version = "HTTP/1.1"

    def log_message(self, fmt, *args):
        if self.server.verbose:
            sys.stderr.write("%s %s\n" % (time.strftime("%H:%M:%S"), fmt % args))

    def send(self, code, ctype, data, extra=None):
        self.send_response(code)
        self.send_header("Content-Type", ctype)
        self.send_header("Content-Length", str(len(data)))
        self.send_header("Cache-Control", "no-store")
        for k, v in (extra or {}).items():
            self.send_header(k, v)
        self.end_headers()
        if self.command != "HEAD":
            self.wfile.write(data)

    def send_json(self, code, obj, extra=None):
        self.send(code, "application/json; charset=utf-8", json.dumps(obj).encode(), extra)

    def do_GET(self):
        route = self.path.split("?")[0]
        if route in ("/", "/dashboard", "/index.html"):
            try:
                with open(PAGE, "rb") as f:
                    return self.send(200, "text/html; charset=utf-8", f.read())
            except OSError as e:
                return self.send(500, "text/plain; charset=utf-8", ("cannot read %s: %s" % (PAGE, e)).encode())
        if route == "/status":
            try:
                return self.send_json(200, build_status(self.server.engine, self.server.watch, self.server.started))
            except Exception as e:  # the page must still get an answer
                return self.send_json(500, {"error": repr(e)[:300]})
        if route == "/offline":
            try:
                return self.send_json(200, offline.summary())
            except Exception as e:
                return self.send_json(500, {"error": repr(e)[:300]})
        if route in ("/players", "/players/char"):
            q = urllib.parse.parse_qs(self.path.partition("?")[2])
            g = lambda k, d="": (q.get(k) or [d])[0]
            try:
                if route == "/players":
                    return self.send_json(200, self.server.players.search(g("q"), min(500, int(g("limit", "60") or 60))))
                cid = g("id")
                return self.send_json(200, self.server.players.char(int(cid) if cid.isdigit() else None, g("name") or None))
            except Exception as e:
                return self.send_json(500, {"error": repr(e)[:300]})
        if route == "/favicon.ico":
            return self.send(204, "image/x-icon", b"")
        if route.startswith("/api/"):
            return self.proxy("GET")
        self.send(404, "text/plain; charset=utf-8", b"not found")

    do_HEAD = do_GET

    def do_POST(self):
        if not self.path.startswith("/api/"):
            return self.send(404, "text/plain; charset=utf-8", b"not found")
        self.proxy("POST")

    def proxy(self, method):
        body = None
        if method == "POST":
            n = int(self.headers.get("Content-Length") or 0)
            if n > 1 << 20:
                return self.send(413, "text/plain; charset=utf-8", b"too large")
            body = self.rfile.read(n) if n else b""
        if method == "GET" and self.server.engine.just_failed():
            code, ctype, data = None, None, self.server.engine.state()["error"].encode()
        else:
            code, ctype, data = self.server.engine.call(method, self.path, body, self.headers.get("Content-Type"))
        if code is not None:
            if method == "GET" and code == 200:
                cache_put(self.path, ctype, data)
            return self.send(code, ctype, data)
        err = data.decode("utf-8", "replace")
        if method == "GET":
            hit, src = minds_from_file(self.path), "file"
            if hit is None:
                hit, src = cache_get(self.path), "cache"
            if hit is None:
                hit, src = from_files(self.path), "files"
            if hit is not None:
                t, old = hit
                return self.send(200, "application/json; charset=utf-8", old,
                                 {"X-Dash-Stale": str(int(max(0, time.time() - t))), "X-Dash-Source": src,
                                  "X-Dash-Error": re.sub(r"[^\x20-\x7e]", "?", err)})
        self.send_json(503, {"down": True, "source": "engine", "error": err})


def beat():
    while True:
        if not C.refresh_lock("dashboard.lock"):   # another copy took the lock: this one must not keep serving
            print("dashboard: lock taken by another copy; exiting", flush=True)
            os._exit(0)
        C.heartbeat("dashboard")
        time.sleep(30)


def main(argv=None):
    global PAGE, CACHE_DIR
    ap = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    ap.add_argument("--port", type=int, default=8099)
    ap.add_argument("--bind", default="127.0.0.1")
    ap.add_argument("--engine", default=os.environ.get("LEGION_API", ENGINE))
    ap.add_argument("--page", default=PAGE)
    ap.add_argument("--cache", default=CACHE_DIR)
    ap.add_argument("--no-docker", action="store_true", help="never ask docker (container state from ports only)")
    ap.add_argument("--no-heartbeat", action="store_true")
    ap.add_argument("--verbose", action="store_true")
    a = ap.parse_args(argv)
    PAGE, CACHE_DIR = a.page, a.cache
    if not a.no_heartbeat and not C.take_lock("dashboard.lock", 120):   # single instance (the beat thread keeps it)
        print("dashboard: another copy holds legion/data/dashboard.lock; not starting", flush=True)
        return 0
    srv = ThreadingHTTPServer((a.bind, a.port), Handler)
    srv.daemon_threads = True
    srv.verbose = a.verbose
    srv.started = time.time()
    srv.engine = Engine(a.engine)
    srv.watch = ContainerWatch(enabled=not a.no_docker, engine=srv.engine)
    srv.watch.start()
    srv.players = Players(srv.watch)
    if not a.no_heartbeat:
        threading.Thread(target=beat, daemon=True).start()
    print("dashboard on http://%s:%d/  (engine %s, data %s)" % (a.bind, a.port, a.engine, C.data_dir()), flush=True)
    try:
        srv.serve_forever()
    except KeyboardInterrupt:
        pass
    return 0


if __name__ == "__main__":
    sys.exit(main())
