#!/usr/bin/env python3
"""watchdog.py - plain code, no AI (Constitution 3.8, 3.9). Keeps the system loop alive and says what is alive.

  python tools/system/watchdog.py              a pass every 30 s, forever (run it in the background)
  python tools/system/watchdog.py --once       one pass, then exit
  python tools/system/watchdog.py --dry-run    print what a pass would do; restarts nothing, files nothing, writes nothing

Every pass it reads every heartbeat (legion/data/<name>.heartbeat: sentinel, partycheck, dispatcher, aylalog, bugq watch,
unwall, keeper, dashboard), the containers (docker, asked the same way the dashboard server asks), and the stack markers.
- A heartbeat that is missing or older than its limit is an OUTAGE. It is filed to the bug queue at once (priority
  outage, key watchdog:<name>) and the item is closed when the heartbeat is back.
- It restarts what is down only when the owner has switched the stack on (legion/data/stack.up) and has not put it on
  hold (stack.hold). The dashboard is the exception: it is always on (DB06). A restart is throttled to one per 10 min per
  item; rathena at most once an hour (G16). Containers are started with `docker start` only, never `compose up`
  (G15). Ayla's container is started here too; keeper.sh owns her restarts as well.
- The bug queue watch exits on purpose after a wake and writes the note "waiting for the main session": that is
  waiting, not dead. Waiting for more than an hour is filed as a queue item.
- Every 30 minutes it appends one summary record of every agent's and container's state to
  legion/data/watchdog-reports.jsonl. A missing report is an alarm (the dashboard shows it).
- Its live state is legion/data/watchdog.status.json, its own heartbeat legion/data/watchdog.heartbeat.
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
sys.path.insert(0, os.path.join(HERE, "..", "dashboard"))
import common as C  # noqa: E402
import bugq  # noqa: E402

INTERVAL = 30            # seconds between passes
REPORT_EVERY = 1800      # the 30-minute summary record
RESTART_GAP = 600        # one restart per item per 10 min
RATHENA_GAP = 3600       # G16: rathena at most once an hour
WAIT_ALARM = 3600        # the bug queue watch waiting this long for the main session is filed
STATE_FILE = "watchdog.state.json"
STATUS_FILE = "watchdog.status.json"
REPORTS_FILE = "watchdog-reports.jsonl"
RESTART_LOG = "watchdog-restarts.log"
WAITING = "waiting"

# the loop's scripts. "cmd" is the script (relative to the repo root) and its arguments, started with this python
# (or with bash under WSL for keeper.sh). hb: heartbeat file names, the first that exists wins. stale: seconds.
AGENTS = [
    {"key": "sentinel", "label": "Sentinel", "hb": ["sentinel.heartbeat"], "stale": 300,
     "cmd": ["tools/sentinel.py"]},
    {"key": "partycheck", "label": "Party check", "hb": ["partycheck.heartbeat"], "stale": 300,
     "cmd": ["tools/system/partycheck.py"]},
    {"key": "dispatcher", "label": "Puppet dispatcher",
     "hb": ["dispatch.heartbeat", "dispatcher.heartbeat", "puppet.heartbeat"], "stale": 180,
     "cmd": ["tools/puppet/dispatch.py"]},
    {"key": "aylalog", "label": "Ayla log watcher", "hb": ["aylalog.heartbeat"], "stale": 300,
     "cmd": ["tools/system/aylalog.py"]},
    {"key": "bugq-watch", "label": "Bug queue watch", "hb": ["bugq-watch.heartbeat"], "stale": 300,
     "cmd": ["tools/system/bugq.py", "watch"], "waits": True},
    {"key": "unwall", "label": "Unwall loop", "hb": ["unwall.heartbeat"], "stale": 600,
     "cmd": ["tools/unwall.py", "--loop"]},
    {"key": "keeper", "label": "Ayla keeper", "hb": ["keeper.heartbeat"], "stale": 300,
     "cmd": ["tools/rogui/keeper.sh"], "bash": True},
    {"key": "dashboard", "label": "Dashboard server", "hb": ["dashboard.heartbeat"], "stale": 120,
     "cmd": ["tools/dashboard/dashserve.py"], "always": True},
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


def docker_prefix():
    if os.name == "nt":
        import dashserve
        return ["wsl.exe", "-d", dashserve.WSL_DISTRO, "--", "docker"]
    return ["docker"]


class Ops:
    """starts scripts and containers. Tests replace this object; the dry run never calls it."""

    def start_agent(self, a):
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
                argv = ["wsl.exe", "-d", "Ubuntu-24.04", "--", "bash", "-c",
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

    def docker_start(self, name):
        subprocess.run(docker_prefix() + ["start", name], capture_output=True, timeout=120)


# ----------------------------------------------------------------- state
def load_state():
    base = {"restarts": {}, "open": {}, "waiting_filed": False, "last_report": None}
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


# ----------------------------------------------------------------- one pass
class Pass:
    def __init__(self, now, ops, dry, st, queue):
        self.now, self.ops, self.dry, self.st = now, ops, dry, st
        self.live = {i for i, e in queue.items() if str(e.get("status", "open")).lower() in ("open", "assigned", "")}
        self.stack_up = os.path.exists(C.dpath("stack.up"))
        self.hold = os.path.exists(C.dpath("stack.hold"))
        self.filed, self.actions = [], []

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
        if self.dry:
            self.actions.append({"key": key, "action": "would restart " + what})
            return "would restart"
        try:
            fn()
            act = "restarted " + what
        except Exception as e:
            act = "restart failed: %s" % str(e)[:120]
        self.st["restarts"][key] = self.now
        self.actions.append({"key": key, "action": act})
        return act

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
        row["state"] = state if state == "missing" else "down"
        what = "no heartbeat file" if state == "missing" else "heartbeat %d min old" % (age // 60)
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
        row["state"] = "missing" if cstate == "missing" else "down"
        self.file(key, "OUTAGE: container %s is %s" % (name, cstate))
        if cstate == "missing":
            row["action"] = "no restart: the container does not exist"
        else:
            gap = RATHENA_GAP if name == "ro-rathena" else RESTART_GAP
            row["action"] = self.restart(key, name, lambda: self.ops.docker_start(name), gap=gap)
        return row


def run_pass(now, ops, dry=False, containers="auto", queue=None):
    """One pass. containers: "auto" (ask docker), {name: (state, status)}, or None for "docker did not answer".
    queue: the bug queue's merged entries (tests pass a dict; default reads the real queue)."""
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
    counts = {"agents_ok": sum(r["state"] == "ok" for r in rows),
              "agents_waiting": sum(r["state"] == WAITING for r in rows),
              "agents_down": sum(r["state"] in ("down", "missing") for r in rows),
              "containers_up": sum(r["state"] == "ok" for r in crows), "containers": len(crows),
              "dashboard_down": any(r["key"] == "dashboard" and r["state"] in ("down", "missing") for r in rows)}
    counts.update({"queue_" + k: v for k, v in queue_counts(queue).items()})
    last = st.get("last_report")
    due = last is None or now - last >= REPORT_EVERY
    rec = {"t": int(now), "iso": C.iso(now), "stack": {"up": p.stack_up, "hold": p.hold}, "summary": counts,
           "agents": rows, "containers": crows, "filed": p.filed, "actions": p.actions}
    out = {"status": rec, "report_due": due, "filed": p.filed, "actions": p.actions}
    if dry:
        return out
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


def main(argv=None):
    ap = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    ap.add_argument("--once", action="store_true", help="one pass, then exit")
    ap.add_argument("--dry-run", action="store_true", help="print what a pass would do; change nothing")
    ap.add_argument("--interval", type=float, default=INTERVAL)
    a = ap.parse_args(argv)
    ops = Ops()
    while True:
        try:
            res = run_pass(C.now(), ops, dry=a.dry_run)
            if a.dry_run or a.once:
                print(json.dumps({"filed": res["filed"], "actions": res["actions"], "report_due": res["report_due"],
                                  "summary": res["status"]["summary"]}, ensure_ascii=False, indent=1))
            if not a.dry_run:
                C.heartbeat("watchdog")
        except Exception as e:  # never die: the next pass tries again
            print("watchdog pass failed: %r" % (e,), file=sys.stderr, flush=True)
        if a.once or a.dry_run:
            return 0
        time.sleep(a.interval)


if __name__ == "__main__":
    sys.exit(main())
