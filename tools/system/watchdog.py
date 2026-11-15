#!/usr/bin/env python3
"""watchdog.py - plain code, no AI (Constitution 3.8, 3.9). Keeps the system loop alive and says what is alive.

  python tools/system/watchdog.py              a pass every 30 s, forever (tools/system/watchdog-keep.cmd runs it)
  python tools/system/watchdog.py --once       one pass, then exit (no lock: manual use)
  python tools/system/watchdog.py --dry-run    print what a pass would do; restarts nothing, files nothing, writes nothing

Every pass it reads every heartbeat (legion/data/<name>.heartbeat), the containers (docker, asked the same way the
dashboard server asks), the stack markers, the meter file and the last deploy, and then:
- A heartbeat that is missing or older than its limit is an OUTAGE, filed to the bug queue at once (priority outage,
  key watchdog:<name>), and closed when the heartbeat is back.
- The bug queue watch is NEVER started here: it exits on purpose after each wake, and a detached copy would swallow the
  main session's next wake. Its outage is only filed.
- Before any restart the old process is confirmed dead: its single-instance lock (legion/data/<name>.lock) must be
  stale, and no process of that script may be running. If either says alive, nothing is started and the outage says
  "stuck, investigate".
- It restarts what is down only when the owner has switched the stack on (legion/data/stack.up) and has not put it on
  hold (stack.hold). The dashboard is the exception: always on (DB06). After a hold ends, scripts get GRACE seconds
  before they count as down. At most one restart per pass; a restart is throttled to one per 10 min per item (rathena
  once an hour, G16). A failed start is not counted against the throttle. Containers use `docker start` only (G15).
- Every 30 minutes it appends one summary record (every agent's and container's state, the meter state, the last deploy
  with its keep/revert verdict) to legion/data/watchdog-reports.jsonl. A missing report is an alarm on the dashboard.
- The map truth (tools/system/fieldcheck.py, 2026-11-15): every OpenKore field file and portal line against the server's
  map cache and loaded warps, once an hour and at once when an OpenKore container starts. Each mismatching source
  (repo copies, or a container) is filed to the bug queue as one item (key fieldcheck:<source>, priority high), closed
  when that source matches again. Filing only: nothing is restarted or changed.
- Its live state is legion/data/watchdog.status.json, its own heartbeat legion/data/watchdog.heartbeat. If it dies, the
  keep loop (tools/system/watchdog-keep.cmd) files that as an outage and starts it again.
Standard library only; runs on Windows Python and on python3 in WSL. LEGION_DATA moves the data dir (tests).
"""
import argparse
import json
import os
import subprocess
import sys
import time

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
import common as C  # noqa: E402
import bugq  # noqa: E402

INTERVAL = 30            # seconds between passes
REPORT_EVERY = 1800      # the 30-minute summary record
RESTART_GAP = 600        # one restart per item per 10 min
RATHENA_GAP = 3600       # G16: rathena at most once an hour
WAIT_ALARM = 3600        # the bug queue watch waiting this long for the main session is filed
GRACE = 600              # after a hold ends (or the stack is started again), scripts get this long to write heartbeats
BOTS_FRESH = 720         # meter_bots.json is written every 5 min by the engine: reading if under 12 min old
JSONL_FRESH = 4500       # meter.jsonl is written hourly (the backup): reading if under 75 min old
RETRY_BASE = 30          # a failed start waits 30 s, then 60, 120 ... (backoff)
RETRY_MAX = 600
STATE_FILE = "watchdog.state.json"
STATUS_FILE = "watchdog.status.json"
REPORTS_FILE = "watchdog-reports.jsonl"
RESTART_LOG = "watchdog-restarts.log"
LOCK_FILE = "watchdog.lock"  # one loop at a time: refreshed each pass; older than LOCK_STALE = dead
LOCK_STALE = 90
EXIT_LOCKED = 3              # another live watchdog holds the lock
WAITING = "waiting"
FIELD_EVERY = 3600       # fieldcheck.py: once an hour, and at once when an OpenKore container starts
OPENKORE = ["ro-openkore-ayla", "ro-openkore-qa", "ro-openkore"]
FIELD_LINES = 60         # evidence lines kept per filed source

# The loop's scripts. cmd: the script (relative to the repo root) and its arguments, started with this python (or with
# bash under WSL for keeper.sh). hb: heartbeat file names, the first that exists wins. stale: seconds. lock: the
# script's own single-instance lock (legion/data/<lock>), fresh = alive. never_start: the watchdog files the outage only.
AGENTS = [
    {"key": "sentinel", "label": "Sentinel", "hb": ["sentinel.heartbeat"], "stale": 300,
     "cmd": ["tools/sentinel.py"], "lock": "sentinel.lock", "lock_stale": 300},
    {"key": "partycheck", "label": "Party check", "hb": ["partycheck.heartbeat"], "stale": 300,
     "cmd": ["tools/system/partycheck.py"], "lock": "partycheck.lock", "lock_stale": 300},
    {"key": "dispatcher", "label": "Puppet dispatcher",
     "hb": ["dispatch.heartbeat", "dispatcher.heartbeat", "puppet.heartbeat"], "stale": 180,
     "cmd": ["tools/puppet/dispatch.py"], "lock": "dispatch.lock", "lock_stale": 180},
    {"key": "aylalog", "label": "Ayla log watcher", "hb": ["aylalog.heartbeat"], "stale": 300,
     "cmd": ["tools/system/aylalog.py"], "lock": "aylalog.lock", "lock_stale": 300},
    {"key": "aylawatch", "label": "Ayla watch", "hb": ["aylawatch.heartbeat"], "stale": 360,
     "cmd": ["tools/system/aylawatch.py"], "lock": "aylawatch.lock", "lock_stale": 360},
    {"key": "bugq-watch", "label": "Bug queue watch", "hb": ["bugq-watch.heartbeat"], "stale": 300,
     "cmd": ["tools/system/bugq.py", "watch"], "waits": True, "never_start": True},
    {"key": "unwall", "label": "Unwall loop", "hb": ["unwall.heartbeat"], "stale": 600,
     "cmd": ["tools/unwall.py", "--loop"], "lock": "unwall.lock", "lock_stale": 300},
    {"key": "keeper", "label": "Ayla keeper", "hb": ["keeper.heartbeat"], "stale": 300,
     "cmd": ["tools/rogui/keeper.sh"], "bash": True, "lock": "keeper.lock", "lock_stale": 150},
    {"key": "dashboard", "label": "Dashboard server", "hb": ["dashboard.heartbeat"], "stale": 120,
     "cmd": ["tools/dashboard/dashserve.py"], "always": True, "lock": "dashboard-8099.lock", "lock_stale": 120},
]
CONTAINERS = ["ro-db", "ro-rathena", "ro-agency", "ro-openkore-ayla", "ro-legion", "ro-legion-core"]


# ----------------------------------------------------------------- real side effects (replaced in tests)
def wsl_path(p):
    p = os.path.abspath(p)
    if len(p) > 1 and p[1] == ":":
        return "/mnt/%s%s" % (p[0].lower(), p[2:].replace("\\", "/"))
    return p.replace("\\", "/")


def docker_ps():
    """{name: (state, status)} or None when docker cannot be asked (same helper as the dashboard server)."""
    try:
        import dashserve
        return dashserve.docker_ps()
    except Exception:
        return None


def _pgrep_pattern(script):
    """pgrep -f pattern that does not match the grep/shell running it: "keeper.sh" -> "[k]eeper.sh"."""
    base = os.path.basename(script)
    return "[%s]%s" % (base[0], base[1:])


def proc_found(returncode, stdout, stderr, pgrep=True):
    """Classify a process check. True = a process runs; False = confirmed none. Anything unclear raises, and the caller
    then counts the script as alive, so a flaky check never starts a second copy. pgrep: exit 0 = found, exit 1 with no
    stderr = none (WSL down, wsl.exe errors and other exits are unclear). PowerShell: exit 0 with a count = the count."""
    if pgrep:
        if returncode == 0:
            return True
        if returncode == 1 and not (stderr or "").strip():
            return False
        raise RuntimeError("process check unclear (exit %s): %s" % (returncode, (stderr or "").strip()[:120]))
    out = (stdout or "").strip()
    if returncode == 0 and out.isdigit():
        return int(out) > 0
    raise RuntimeError("process check unclear (exit %s, output %r)" % (returncode, out[:60]))


class Ops:
    """starts scripts and containers, and checks processes. Tests replace this object; the dry run never calls it."""

    def start_agent(self, a):
        if a.get("never_start"):
            raise RuntimeError("the watchdog never starts %s" % a["key"])
        log = None
        try:
            os.makedirs(C.data_dir(), exist_ok=True)
            log = open(C.dpath(RESTART_LOG), "ab")
        except OSError:
            pass
        kw = {"cwd": C.ROOT, "stdin": subprocess.DEVNULL, "stdout": log or subprocess.DEVNULL,
              "stderr": subprocess.STDOUT, "close_fds": True}
        script = os.path.join(C.ROOT, a["cmd"][0])
        if a.get("bash"):
            if os.name == "nt":
                argv = ["wsl.exe", "-d", C.WSL_DISTRO, "--", "bash", "-c",
                        "nohup bash %s >/dev/null 2>&1 &" % wsl_path(script)]
            else:
                argv = ["bash", script]
                kw["start_new_session"] = True
        else:
            argv = [sys.executable, script] + a["cmd"][1:]
            if os.name == "nt":
                kw["creationflags"] = 0x00000008 | 0x00000200   # DETACHED_PROCESS | CREATE_NEW_PROCESS_GROUP
            else:
                kw["start_new_session"] = True
        try:
            subprocess.Popen(argv, **kw)
        finally:
            if log:
                log.close()

    def process_running(self, a):
        """True when a process of this script is running (Windows: python processes by command line; WSL: pgrep).
        If the check itself fails, the caller treats the process as alive: no second copy is started."""
        if not a.get("cmd"):
            return False
        base = os.path.basename(a["cmd"][0])
        if a.get("bash"):
            argv = ["wsl.exe", "-d", C.WSL_DISTRO, "--", "pgrep", "-f", _pgrep_pattern(a["cmd"][0])] if os.name == "nt" \
                else ["pgrep", "-f", _pgrep_pattern(a["cmd"][0])]
            p = subprocess.run(argv, capture_output=True, text=True, timeout=60)
            return proc_found(p.returncode, p.stdout, p.stderr, pgrep=True)
        if os.name == "nt":
            ps = ("@(Get-CimInstance Win32_Process -Filter \"Name='python.exe'\" | Where-Object { $_.CommandLine -like "
                  "'*%s*' }).Count" % base)
            p = subprocess.run(["powershell", "-NoProfile", "-Command", ps], capture_output=True, text=True, timeout=60)
            return proc_found(p.returncode, p.stdout, p.stderr, pgrep=False)
        p = subprocess.run(["pgrep", "-f", _pgrep_pattern(a["cmd"][0])], capture_output=True, text=True, timeout=60)
        return proc_found(p.returncode, p.stdout, p.stderr, pgrep=True)

    def docker_start(self, name):
        p = subprocess.run(C.docker_prefix() + ["start", name], capture_output=True, text=True, timeout=120)
        if p.returncode != 0:
            raise RuntimeError("docker start %s failed (exit %s): %s" % (name, p.returncode,
                                                                        (p.stderr or p.stdout).strip()[:120]))


# ----------------------------------------------------------------- state and files
def load_state():
    base = {"restarts": {}, "open": {}, "waiting_filed": False, "last_report": None, "was_running": None,
            "resumed_at": None, "retry": {}}
    try:
        with open(C.dpath(STATE_FILE), encoding="utf-8") as f:
            st = json.load(f)
        if isinstance(st, dict):
            base.update(st)
    except (OSError, ValueError):
        pass
    return base


def write_json(name, obj):
    """atomic replace of a data file; never raises"""
    try:
        os.makedirs(C.data_dir(), exist_ok=True)
        p = C.dpath(name)
        tmp = p + ".tmp"
        with open(tmp, "w", encoding="utf-8") as f:
            json.dump(obj, f, ensure_ascii=False, indent=1)
        os.replace(tmp, p)
    except OSError:
        pass


def read_hb(a):
    """(age seconds or None, file name used, note after the epoch)"""
    best, used = None, None
    for fn in a["hb"]:
        age = C.heartbeat_age(C.dpath(fn))
        if age is not None and (best is None or age < best):
            best, used = age, fn
    note = C.heartbeat_note(C.dpath(used)) if used else ""
    return best, used or a["hb"][0], note


def queue_counts(queue):
    live = [e for e in queue.values() if str(e.get("status", "open")).lower() not in ("closed", "duplicate")]
    unassigned = [e for e in live if str(e.get("status", "open")).lower() != "assigned" and not e.get("assigned_to")]
    return {"open": len(live), "unassigned": len(unassigned)}


def last_json_line(path):
    """the last JSON object of a (possibly large) .jsonl file, or None"""
    try:
        size = os.path.getsize(path)
        with open(path, "rb") as f:
            f.seek(max(0, size - 65536))
            lines = f.read().decode("utf-8", "replace").splitlines()
        for line in reversed(lines):
            try:
                o = json.loads(line)
            except ValueError:
                continue
            if isinstance(o, dict):
                return o
    except OSError:
        pass
    return None


def meter_state(now):
    """Is the meter reading? The engine writes meter_bots.json every 5 min (reading if under 12 min old) and meter.jsonl
    once an hour (the backup: reading if under 75 min old). Ages are the file times on the (fakeable) clock."""
    last = last_json_line(C.dpath("meter.jsonl")) or {}
    age_b = C.marker_age("meter_bots.json")
    if age_b is not None and age_b <= BOTS_FRESH:
        return {"state": "reading", "source": "meter_bots.json", "last_write_age_s": int(age_b),
                "last_row_t": last.get("t"), "why": ""}
    age_h = C.marker_age("meter.jsonl")
    if age_h is not None and age_h <= JSONL_FRESH:
        return {"state": "reading", "source": "meter.jsonl (hourly backup)", "last_write_age_s": int(age_h),
                "last_row_t": last.get("t"), "why": ""}
    why = ("meter_bots.json not written for %s min" % ("?" if age_b is None else int(age_b // 60)),
           "meter.jsonl not written for %s min" % ("?" if age_h is None else int(age_h // 60)))
    return {"state": "blind", "source": None, "last_write_age_s": None if age_h is None else int(age_h),
            "last_row_t": last.get("t"), "why": "; ".join(why)}


def deploy_records():
    """(deploy records, verdict lines) from deploys.jsonl. A kind "verdict" line is the keep/revert record of a past
    deploy, not a deploy: deploylog, league, the puppet dispatcher, the bug queue and the dashboard skip it the same way."""
    all_recs = C.read_jsonl(C.dpath("deploys.jsonl"))
    recs = [r for r in all_recs if r.get("kind") != "verdict" and C.to_epoch(r.get("epoch", r.get("t")))]
    verdicts = [r for r in all_recs if r.get("kind") == "verdict"]
    return recs, verdicts


def deploy_state(now):
    """The last deploy and its keep/revert verdict. The verdict is the newest verdict line for that deploy (written by
    league.py, kind "verdict"); until one exists it is computed the same way (league.compare) and nothing is written."""
    recs, verdicts = deploy_records()
    if not recs:
        return {"available": False, "why": "no deploy record"}

    def ep(r):
        return C.to_epoch(r.get("epoch", r.get("t"))) or 0.0
    last = max(recs, key=ep)
    tag, dep_epoch = last.get("tag"), ep(last)
    fixes = last.get("fixes") or next((r.get("fixes") for r in recs if r.get("tag") == tag and r.get("fixes")), []) or []
    out = {"available": True, "tag": tag, "epoch": int(dep_epoch), "age_s": int(now - dep_epoch),
           "services": last.get("services"), "critical": bool(last.get("critical")), "fixes": len(fixes)}
    vs = [v for v in verdicts if v.get("tag") == tag and abs((C.to_epoch(v.get("deploy_epoch")) or 0) - dep_epoch) < 1]
    if vs:
        v = max(vs, key=lambda x: C.to_epoch(x.get("t")) or 0)
        out.update({"verdict": v.get("verdict"), "verdict_t": v.get("t"), "partial": bool(v.get("partial")),
                    "verdict_source": "deploys.jsonl verdict line", "items": len(v.get("items") or [])})
        return out
    dep = {"tag": tag, "epoch": dep_epoch, "services": last.get("services"), "critical": last.get("critical"),
           "fixes": fixes}
    try:
        import league
        res = league.compare(dep, C.read_jsonl(C.dpath("meter.jsonl")), 3.0, now)
    except Exception as e:
        out.update({"verdict": "not decided (comparison failed: %s)" % str(e)[:100], "verdict_source": "none"})
        return out
    counts = {}
    for r in res:
        k = str(r[6]).split(" ")[0]
        counts[k] = counts.get(k, 0) + 1
    revert = [str(r[0].get("text", ""))[:120] for r in res if str(r[6]).startswith("REVERT")]
    if not fixes:
        verdict = "no fix list on the deploy record"
    elif revert:
        verdict = "REVERT candidates: %d" % len(revert)
    elif res and all(str(r[6]).startswith("KEEP") for r in res):
        verdict = "KEPT"
    else:
        verdict = "NOT DECIDED yet (no data, or inconclusive)"
    out.update({"verdicts": counts, "revert_candidates": revert, "verdict": verdict,
                "verdict_source": "computed now, no verdict line written yet"})
    return out


# ----------------------------------------------------------------- one pass
class Pass:
    def __init__(self, now, ops, dry, st, queue):
        self.now, self.ops, self.dry, self.st = now, ops, dry, st
        self.live = {i for i, e in queue.items() if str(e.get("status", "open")).lower() in ("open", "assigned", "")}
        self.stack_up = os.path.exists(C.dpath("stack.up"))
        self.hold = os.path.exists(C.dpath("stack.hold"))
        self.running = self.stack_up and not self.hold
        # the stack was not running and now is: give the scripts GRACE seconds to write their heartbeats
        if self.running and st.get("was_running") is False:
            self.resumed = now
        else:
            self.resumed = st.get("resumed_at")
        self.restarted = False
        self.filed, self.actions = [], []

    def in_grace(self):
        return self.resumed is not None and self.now - self.resumed < GRACE

    def file(self, key, text, priority="outage", evidence=""):
        """one open queue item per key: while it is still open, a repeat files nothing new"""
        cur = self.st["open"].get(key)
        if cur and cur in self.live:
            return cur
        self.filed.append({"key": key, "priority": priority, "text": text})
        if self.dry:
            return None
        i, _ = bugq.add("watchdog", priority, text, evidence, key="watchdog:" + key)
        self.st["open"][key] = i
        self.live.add(i)
        return i

    def close(self, key, note):
        cur = self.st["open"].pop(key, None)
        if cur and cur in self.live and not self.dry:
            bugq.close(cur, note)
            self.live.discard(cur)

    def restart(self, key, what, fn, gap=RESTART_GAP):
        last = self.st["restarts"].get(key)
        if last is not None and self.now - last < gap:
            self.actions.append({"key": key, "action": "restart throttled"})
            return "restart throttled"
        bo = self.st["retry"].get(key)
        if bo and self.now < bo["next"]:
            act = "start backoff: %d failed start(s), next try at %s" % (bo["n"], C.iso(bo["next"]))
            self.actions.append({"key": key, "action": act})
            return act
        if self.restarted:
            self.actions.append({"key": key, "action": "restart deferred: one restart per pass"})
            return "restart deferred"
        self.restarted = True
        if self.dry:
            self.actions.append({"key": key, "action": "would restart " + what})
            return "would restart"
        try:
            fn()
        except Exception as e:
            # a failed or raised start is not counted against the 10-min throttle, but it backs off (30 s, 60 s ... 10 min)
            n = (bo or {}).get("n", 0) + 1
            wait = min(RETRY_MAX, RETRY_BASE * 2 ** (n - 1))
            self.st["retry"][key] = {"n": n, "next": self.now + wait}
            act = "restart failed (backoff %d s): %s" % (wait, str(e)[:120])
            self.actions.append({"key": key, "action": act})
            return act
        self.st["restarts"][key] = self.now
        self.st["retry"].pop(key, None)
        self.actions.append({"key": key, "action": "restarted " + what})
        return "restarted " + what

    def alive(self, a):
        """True when the old copy may still be running: its lock is fresh, or a process of the script runs"""
        lk = a.get("lock")
        if lk:
            age = C.marker_age(lk)
            if age is not None and age <= a.get("lock_stale", 300):
                return True
        try:
            return bool(self.ops.process_running(a))
        except Exception:
            return True   # cannot confirm the old process is dead: do not start a second copy

    def agent(self, a):
        key = a["key"]
        age, used, note = read_hb(a)
        row = {"key": key, "label": a["label"], "kind": "script", "file": used,
               "age_s": None if age is None else int(age),
               "last": None if age is None else int(self.now - age), "stale_after_s": a["stale"], "note": note,
               "state": None, "action": ""}
        if age is None:
            state = "missing"
        elif a.get("waits") and WAITING in note:
            state = WAITING
        else:
            state = "ok" if age <= a["stale"] else "stale"
        if state == "ok":
            row["state"] = "ok"
            self.close(key, "back at %s" % C.iso(self.now))
            if not self.dry:
                self.st["waiting_filed"] = False
            return row
        if state == WAITING:
            row["state"] = WAITING
            if age >= WAIT_ALARM and not self.st["waiting_filed"]:
                self.file("bugq-watch:waiting", "The bug queue watch has waited %d min for the main session to "
                          "restart it (it exits on purpose after each wake)" % (age // 60), priority="high")
                if not self.dry:
                    self.st["waiting_filed"] = True
            return row
        if not a.get("always"):
            if not self.stack_up:
                row["state"] = "off"
                return row
            if self.hold:
                row["state"] = "held"
                return row
            if self.in_grace():
                row["state"] = "starting"
                row["action"] = "grace after the hold ended"
                return row
        row["state"] = state if state == "missing" else "down"
        what = "no heartbeat file" if state == "missing" else "heartbeat %d min old" % (age // 60)
        if a.get("never_start"):
            self.file(key, "OUTAGE: %s is down (%s); the watchdog does not start it: the main session restarts it"
                      % (a["label"], what), evidence="heartbeat file %s" % used)
            row["action"] = "not started by the watchdog (a detached copy would swallow the wake)"
            return row
        if self.alive(a):
            row["state"] = "stuck"
            self.file(key, "OUTAGE: %s heartbeat %s but its process is still running: not started again, investigate"
                      % (a["label"], what), evidence="heartbeat file %s" % used)
            row["action"] = "not started: the old process is still running"
            return row
        self.file(key, "OUTAGE: %s is down (%s)" % (a["label"], what), evidence="heartbeat file %s" % used)
        row["action"] = self.restart(key, a["label"], lambda: self.ops.start_agent(a))
        return row

    def container(self, name, ps):
        key = "ct:" + name
        row = {"key": name, "label": name, "kind": "container", "state": None, "detail": "", "action": ""}
        if ps is None:
            row["state"] = "unknown"
            row["detail"] = "docker did not answer"
            if self.stack_up and not self.hold:
                self.file("docker", "OUTAGE: docker did not answer (WSL or the docker service is down)")
            return row
        cstate, cstatus = ps.get(name, ("missing", "no such container"))
        row["detail"] = cstatus or cstate
        if cstate == "running":
            row["state"] = "ok"
            self.close(key, "running again at %s" % C.iso(self.now))
            return row
        if not self.stack_up:
            row["state"] = "off"
            return row
        if self.hold:
            row["state"] = "held"
            return row
        if self.in_grace():
            row["state"] = "starting"
            row["action"] = "grace after the hold ended"
            return row
        row["state"] = "missing" if cstate == "missing" else "down"
        self.file(key, "OUTAGE: container %s is %s" % (name, cstate))
        if cstate == "missing":
            row["action"] = "no restart: the container does not exist"
        else:
            gap = RATHENA_GAP if name == "ro-rathena" else RESTART_GAP
            row["action"] = self.restart(key, name, lambda: self.ops.docker_start(name), gap=gap)
        return row


FIELD_DEBOUNCE = 900     # a container start re-runs the check at most every 15 min (a crash loop does not flood it)


def run_fieldcheck(tick=None):
    """the map truth: (problems, infos) from tools/system/fieldcheck.py: the repo copies and the containers in use
    (read-only; refreshes the server snapshot when due). tick() is called around the docker copies (the watchdog lock)."""
    import fieldcheck  # tools/system is on sys.path (HERE)
    return fieldcheck.run_all(tick=tick)


def field_pass(p, now, st, containers, fields):
    """Files the map-truth mismatches (one open item per source) and closes the items of sources that match again.
    Runs once an hour, and after an OpenKore container has started (at most every FIELD_DEBOUNCE s; a start that comes
    too soon stays pending). Only sources in use are filed: the repo copies, and containers that are running or in use
    (fieldcheck.IN_USE). fields: the check (a callable)."""
    up = sorted(n for n in OPENKORE if isinstance(containers, dict) and containers.get(n, ("",))[0] == "running")
    started = set(up) - set(st.get("fc_up") or [])
    st["fc_up"] = up
    pending = set(st.get("fc_pending") or []) | started
    last = st.get("fieldcheck_t")
    hourly = last is None or now - last >= FIELD_EVERY
    starts = bool(pending) and (last is None or now - last >= FIELD_DEBOUNCE)
    if not (hourly or starts):
        st["fc_pending"] = sorted(pending)
        return
    st["fc_pending"] = []
    st["fieldcheck_t"] = now
    try:
        probs, infos = fields()
    except Exception as e:  # a check that cannot run is filed, never silent
        p.file("fieldcheck:run", "fieldcheck could not run: %r" % (e,), priority="high")
        return
    active = {i["source"] for i in infos
              if i.get("source") not in (None, "server") and not i.get("absent") and i.get("active", True)}
    for src in sorted(active):
        ps = [x for x in probs if x["source"] == src]
        key = "fieldcheck:" + src
        if ps:
            maps = sorted({x["map"] for x in ps})
            text = "map truth: %s has %d mismatch(es) against the server in %d map(s): %s" % (
                src, len(ps), len(maps), ", ".join(maps[:15]) + (" ..." if len(maps) > 15 else ""))
            ev = "\n".join("%s: %s" % (x["map"], x["detail"]) for x in ps[:FIELD_LINES])
            p.file(key, text, priority="high", evidence=ev)
        else:
            p.close(key, "matches the server again at %s" % C.iso(now))


def run_pass(now, ops, dry=False, containers="auto", queue=None, fields=None):
    """One pass. containers: "auto" (ask docker), {name: (state, status)}, or None for "docker did not answer".
    queue: the bug queue's merged entries (tests pass a dict; default reads the real queue).
    fields: the map-truth check (a callable returning (problems, infos)); main() passes run_fieldcheck; None = not run."""
    st = load_state()
    if queue is None:
        try:
            queue = bugq.load()
        except Exception:
            queue = {}
    if containers == "auto":
        containers = docker_ps()
    p = Pass(now, ops, dry, st, queue)
    rows = [p.agent(a) for a in AGENTS]
    crows = [p.container(n, containers) for n in CONTAINERS]
    if not dry and fields is not None:
        field_pass(p, now, st, containers, fields)
    counts = {"agents_ok": sum(r["state"] == "ok" for r in rows),
              "agents_waiting": sum(r["state"] == WAITING for r in rows),
              "agents_down": sum(r["state"] in ("down", "missing", "stuck") for r in rows),
              "containers_up": sum(r["state"] == "ok" for r in crows), "containers": len(crows),
              "dashboard_down": any(r["key"] == "dashboard" and r["state"] in ("down", "missing") for r in rows)}
    counts.update({"queue_" + k: v for k, v in queue_counts(queue).items()})
    last = st.get("last_report")
    due = last is None or now - last >= REPORT_EVERY
    rec = {"t": int(now), "iso": C.iso(now), "stack": {"up": p.stack_up, "hold": p.hold}, "summary": counts,
           "meter": meter_state(now), "deploy": deploy_state(now),
           "agents": rows, "containers": crows, "filed": p.filed, "actions": p.actions}
    out = {"status": rec, "report_due": due, "filed": p.filed, "actions": p.actions}
    if dry:
        return out
    st["was_running"] = p.running
    st["resumed_at"] = p.resumed
    if due:
        try:
            C.append_jsonl(C.dpath(REPORTS_FILE), rec)
            st["last_report"] = now
        except OSError:
            pass
    rec["next_report_t"] = int((st["last_report"] or now) + REPORT_EVERY)
    write_json(STATUS_FILE, rec)
    write_json(STATE_FILE, st)
    return out


def take_lock(now=None):
    """Single copy (the loop only): True when this process holds legion/data/watchdog.lock, False when a lock refreshed
    within LOCK_STALE seconds belongs to another loop. A stale lock (a crashed loop) is taken over."""
    return C.take_lock(LOCK_FILE, LOCK_STALE)


def touch_lock(now=None):
    """refresh the lock's time; False when the lock now belongs to another loop (this one must exit)"""
    return C.refresh_lock(LOCK_FILE)


def main(argv=None):
    ap = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    ap.add_argument("--once", action="store_true", help="one pass, then exit (no lock: manual use)")
    ap.add_argument("--dry-run", action="store_true", help="print what a pass would do; change nothing")
    ap.add_argument("--interval", type=float, default=INTERVAL)
    a = ap.parse_args(argv)
    loop = not (a.once or a.dry_run)
    if loop and not take_lock():
        print("another watchdog holds %s (refreshed within %d s); not starting" % (LOCK_FILE, LOCK_STALE),
              file=sys.stderr, flush=True)
        return EXIT_LOCKED
    ops = Ops()
    while True:
        if loop and not touch_lock():
            print("watchdog lock taken by another loop; exiting", file=sys.stderr, flush=True)
            return EXIT_LOCKED
        try:
            res = run_pass(C.now(), ops, dry=a.dry_run, fields=lambda: run_fieldcheck(tick=touch_lock))
            if a.dry_run or a.once:
                print(json.dumps({"filed": res["filed"], "actions": res["actions"], "report_due": res["report_due"],
                                  "summary": res["status"]["summary"]}, ensure_ascii=False, indent=1))
            if not a.dry_run:
                C.heartbeat("watchdog")
        except Exception as e:  # never die: the next pass tries again
            print("watchdog pass failed: %r" % (e,), file=sys.stderr, flush=True)
        if not loop:
            return 0
        time.sleep(a.interval)


if __name__ == "__main__":
    sys.exit(main())
