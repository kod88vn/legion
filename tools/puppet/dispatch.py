#!/usr/bin/env python3
"""Puppet-master dispatcher: the one long-running script of the puppet harness. It is not an AI.

It decides when each puppet master wakes (schedule + triggers), assembles a briefing from the engine API and starts a
one-shot agent with the Claude Code CLI in print mode. See tools/puppet/README.md and docs/design/puppet.md.

  python tools/puppet/dispatch.py                  # run the loop
  python tools/puppet/dispatch.py --dry-run        # print the command and briefing for every role, spend nothing
  python tools/puppet/dispatch.py --once guild03   # one call now (with --dry-run: only print)
  python tools/puppet/dispatch.py --roles chat,market   # restrict the loop to these roles
  python tools/puppet/dispatch.py --list           # role table (model, interval, caps)

Pause: create legion/data/puppet.pause (delete it to resume). Stop: Ctrl+C / kill. Standard library only.
"""
import argparse
import json
import os
import shutil
import subprocess
import sys
import threading
import time

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
import briefing  # noqa: E402

REPO = os.path.abspath(os.path.join(HERE, "..", ".."))
DATA = os.environ.get("LEGION_DATA") or os.path.join(REPO, "legion", "data")
NOTES = os.path.join(HERE, "notes")
BRIEFS = os.path.join(HERE, "briefs")
WORKDIR = os.path.join(HERE, "workdir")
CTL = os.path.join(REPO, "tools", "legionctl.py").replace("\\", "/")
AYLA_SH = os.path.join(REPO, "tools", "rogui", "ayla.sh").replace("\\", "/")

HOUR = 3600.0
GUILDS = [f"guild{i:02d}" for i in range(1, 11)]

# role -> config. interval 0 = no schedule (triggers only). call_cap = --max-budget-usd per call,
# hourly_cap = USD per rolling hour for the role (guilds: each guild is its own role). offset = first call after start.
ROLES = {}
for _i, _g in enumerate(GUILDS):
    ROLES[_g] = dict(brief="guild", model="haiku", interval=25 * 60, offset=_i * 150, call_cap=0.06, hourly_cap=0.15,
                     min_gap=180, bus=_g)
ROLES["market"] = dict(brief="market", model="haiku", interval=30 * 60, offset=60, call_cap=0.08, hourly_cap=0.15,
                       min_gap=300, bus="market")
ROLES["chat"] = dict(brief="chat", model="haiku", interval=0, offset=0, call_cap=0.06, hourly_cap=0.45,
                     min_gap=30, bus="chat")
ROLES["build"] = dict(brief="build", model="sonnet", interval=0, offset=0, call_cap=0.35, hourly_cap=0.60,
                      min_gap=1800, bus="build")
ROLES["ayla"] = dict(brief="ayla", model="sonnet", interval=5 * 60, offset=20, call_cap=0.25, hourly_cap=1.50,
                     min_gap=120, bus="ayla")

CALL_TIMEOUT = 300
ORDER_GAP = 120  # an owner order wakes a role at most this often


# ----------------------------------------------------------------- command line for the Claude CLI
def allowed_tools(role, python="python"):
    """Bash only, and only legionctl (Ayla's mind also gets ayla.sh, her own interface)."""
    tools = [f"Bash({python} {CTL}:*)"]
    if role == "ayla":
        tools.append(f"Bash(bash {AYLA_SH}:*)")
    return tools


# legionctl commands that hand bots direct goals/maps: puppet masters set directions, prices, words, never these
DENIED_CTL = ["goal", "guild-goal", "party", "leader-party", "boss-hunt", "boss-cancel"]


def denied_tools(python="python"):
    return [f"Bash({python} {CTL} {c}:*)" for c in DENIED_CTL]


def load_brief(role):
    cfg = ROLES[role]
    text = ""
    for name in ("_common", cfg["brief"]):
        with open(os.path.join(BRIEFS, name + ".md"), encoding="utf-8") as f:
            text += f.read().rstrip() + "\n\n"
    return (text.replace("{CTL}", CTL).replace("{ROLE}", role).replace("{MIND}", cfg["bus"])
            .replace("{AYLA_SH}", AYLA_SH).replace("{NOTES}", os.path.join(NOTES, role + ".md").replace("\\", "/")))


def build_command(role, claude="claude", python="python", model=None, call_cap=None):
    cfg = ROLES[role]
    cmd = [claude, "-p",
           "--model", model or cfg["model"],
           "--safe-mode",  # no CLAUDE.md, skills, hooks, MCP: the big project rules are not loaded
           "--tools", "Bash",
           "--allowedTools", *allowed_tools(role, python),
           "--disallowedTools", *denied_tools(python),
           "--permission-mode", "dontAsk",
           "--max-budget-usd", f"{call_cap if call_cap is not None else cfg['call_cap']:.2f}",
           "--output-format", "json",
           "--no-session-persistence",
           "--append-system-prompt", load_brief(role)]
    return cmd  # the briefing itself goes in on stdin


def render(cmd):
    """printable command line: the long brief argument is shown as its size"""
    out = []
    for c in cmd:
        if "\n" in c or len(c) > 200:
            out.append("<brief:%d chars>" % len(c))
        else:
            out.append(f'"{c}"' if " " in c else c)
    return " ".join(out)


def find_claude():
    return shutil.which("claude") or "claude"


# ----------------------------------------------------------------- call log, budget
def log_path():
    return os.path.join(DATA, "puppet_calls.jsonl")


def read_log(path=None, since=0.0):
    rows = []
    try:
        with open(path or log_path(), encoding="utf-8") as f:
            for line in f:
                try:
                    r = json.loads(line)
                except ValueError:
                    continue
                if r.get("t", 0) >= since:
                    rows.append(r)
    except OSError:
        pass
    return rows


def append_log(row, path=None):
    p = path or log_path()
    os.makedirs(os.path.dirname(p), exist_ok=True)
    with open(p, "a", encoding="utf-8") as f:
        f.write(json.dumps(row, ensure_ascii=False) + "\n")


def spent_last_hour(rows, role, now):
    return sum(float(r.get("cost_usd") or 0) for r in rows if r.get("role") == role and r.get("t", 0) >= now - HOUR)


def parse_result(stdout):
    """fields of the CLI's --output-format json result; tolerant of junk"""
    try:
        d = json.loads(stdout)
    except (ValueError, TypeError):
        return {}
    if isinstance(d, list):  # stream-ish arrays: take the last result object
        d = next((x for x in reversed(d) if isinstance(x, dict) and x.get("type") == "result"), {})
    u = d.get("usage") or {}
    return dict(
        cost_usd=d.get("total_cost_usd", d.get("cost_usd")),
        input_tokens=u.get("input_tokens"), output_tokens=u.get("output_tokens"),
        cache_read=u.get("cache_read_input_tokens"), cache_create=u.get("cache_creation_input_tokens"),
        turns=d.get("num_turns"), is_error=d.get("is_error"), api_ms=d.get("duration_api_ms"),
        result=str(d.get("result", ""))[:300])


# ----------------------------------------------------------------- schedule (pure logic)
class Scheduler:
    """Decides which role is due. No I/O: tests drive it with `now`, `signals` and a log."""

    def __init__(self, roles, start, rows=None):
        self.roles = roles
        self.start = start
        self.last = {}  # role -> last call start (from the log, so a restart does not wake everyone at once)
        for r in rows or []:
            if r.get("role") in roles and not r.get("dry"):
                self.last[r["role"]] = max(self.last.get(r["role"], 0), r.get("t", 0))
        self.running = set()
        self.pending = {}  # role -> trigger label waiting for its turn (human line, jobs, event)

    def note(self, role, trigger):
        """remember a trigger that could not run yet (role busy, gap, budget)"""
        if role in self.roles:
            self.pending.setdefault(role, trigger)

    def due(self, role, now, rows, signals):
        """trigger label ('schedule', 'orders', 'human', ...) or None"""
        cfg = ROLES[role]
        if role in self.running:
            return None
        if spent_last_hour(rows, role, now) >= cfg["hourly_cap"]:
            return None
        last = self.last.get(role)
        gap = now - last if last else None
        if gap is not None and gap < cfg["min_gap"]:
            return None
        trig = None
        if signals.get("wake", {}).get(role):
            trig = "wake"
        elif signals.get("orders", {}).get(role) and (gap is None or gap >= min(ORDER_GAP, cfg["min_gap"])):
            trig = "orders"
        elif role in self.pending:
            trig = self.pending[role]
        elif cfg["interval"]:
            if last is None:
                trig = "schedule" if now >= self.start + cfg["offset"] else None
            elif gap >= cfg["interval"]:
                trig = "schedule"
        return trig

    def started(self, role, now):
        self.last[role] = now
        self.running.add(role)
        self.pending.pop(role, None)

    def finished(self, role):
        self.running.discard(role)


def classify_ayla_event(e, names):
    """death / split / goal / need for a Legion event touching Ayla's party, else None"""
    kind = str(e.get("kind", "")).lower()
    text = str(e.get("text", "")).lower()
    who = str(e.get("bot", ""))
    if names and who.lower() not in names and "ayla" not in text:
        return None
    blob = kind + " " + text
    if "death" in kind or "died" in blob or "killed by" in blob:
        return "death"
    if any(w in blob for w in ("separated", "split", "lost the party", "left the party", "far from")):
        return "split"
    if any(w in blob for w in ("goal met", "goal done", "got the", "completed", "gear done")):
        return "goal"
    if any(w in blob for w in ("need help", "help!", "low hp", "needs ", "out of potions", "no arrows")):
        return "need"
    return None


# ----------------------------------------------------------------- signals from the engine
class Signals:
    """Polls cheap endpoints each tick: owner orders (minds bus), new human chat lines, queued chat jobs, Ayla events,
    the build wake file. Never raises; returns what it saw. API down -> api_ok False and nothing else."""

    def __init__(self, api_factory, data_dir=None):
        self.mk = api_factory
        self.data = data_dir or DATA
        self.inbox_cursor = None
        self.event_cursor = None
        self.pending_human = 0
        self.pending_ayla = None

    def poll(self):
        api = self.mk()
        out = {"api_ok": api.alive(), "orders": {}, "wake": {}, "chat": None}
        if not out["api_ok"]:
            return out
        minds = api.get("/api/minds")
        for m in (minds if isinstance(minds, list) else briefing.rows_of(minds)):
            if isinstance(m, dict) and (m.get("unread") or 0) > 0 and m.get("name") in ROLES:
                out["orders"][m["name"]] = m["unread"]
        # build master wake file (touch legion/data/build.wake; consumed when the call starts)
        if os.path.exists(os.path.join(self.data, "build.wake")):
            out["wake"]["build"] = True
        # chat: new human lines or queued jobs
        ib = api.get("/api/inbox?since=%d" % (self.inbox_cursor if self.inbox_cursor is not None else 999999999999))
        if isinstance(ib, dict):
            if self.inbox_cursor is None:
                self.inbox_cursor = ib.get("next", 0)
            else:
                self.inbox_cursor = ib.get("next", self.inbox_cursor)
                self.pending_human += sum(1 for m in (ib.get("messages") or []) if not m.get("from_bot"))
        jobs = api.get("/api/chat-jobs?peek=1")
        njobs = len(jobs if isinstance(jobs, list) else briefing.rows_of(jobs)) if jobs is not None else 0
        if self.pending_human:
            out["chat"] = "human"
        elif njobs:
            out["chat"] = "jobs"
        # Ayla events
        names = set()
        for p in (api.get("/api/parties") or []) if isinstance(api.get("/api/parties"), list) else []:
            if "ayla" in json.dumps(p, default=str).lower():
                for m in p.get("members", []) if isinstance(p, dict) else []:
                    names.add(str(m.get("name") if isinstance(m, dict) else m).lower())
        ev = api.get("/api/events?since=%d" % (self.event_cursor or 0)) if self.event_cursor is not None else api.get("/api/events?since=999999999999")
        if isinstance(ev, dict):
            if self.event_cursor is not None:
                for e in ev.get("events") or []:
                    c = classify_ayla_event(e, names)
                    if c:
                        self.pending_ayla = self.pending_ayla or c
            self.event_cursor = ev.get("next", self.event_cursor or 0)
        out["ayla_event"] = self.pending_ayla
        return out

    def consumed(self, role):
        if role == "chat":
            self.pending_human = 0
        elif role == "ayla":
            self.pending_ayla = None
        elif role == "build":
            try:
                os.remove(os.path.join(self.data, "build.wake"))
            except OSError:
                pass


# ----------------------------------------------------------------- snapshots (feedback between calls)
def snap_path(role):
    return os.path.join(NOTES, role + ".snap.json")


def load_snap(role):
    try:
        with open(snap_path(role), encoding="utf-8") as f:
            d = json.load(f)
        return d.get("snap"), d.get("t")
    except (OSError, ValueError):
        return None, None


def save_snap(role, snap, t):
    os.makedirs(NOTES, exist_ok=True)
    with open(snap_path(role), "w", encoding="utf-8") as f:
        json.dump({"t": t, "snap": snap}, f)


# ----------------------------------------------------------------- one call
def make_briefing(role, trigger, api=None):
    api = api or briefing.Api()
    prev, prev_ts = load_snap(role)
    text, snap = briefing.assemble(role, api, NOTES, prev, prev_ts, trigger)
    head = f"Time: {time.strftime('%Y-%m-%d %H:%M')}. Wake reason: {trigger}.\n"
    return head + text, snap


def run_call(role, trigger, args, sched=None, sig=None):
    """assemble, run the CLI, log. Runs in a worker thread; always logs, never raises."""
    t0 = time.time()
    row = {"t": t0, "role": role, "trigger": trigger, "model": args.model or ROLES[role]["model"], "exit": None}
    try:
        text, snap = make_briefing(role, trigger)
        cmd = build_command(role, args.claude, args.python, args.model, args.call_cap)
        os.makedirs(WORKDIR, exist_ok=True)
        p = subprocess.run(cmd, input=text, capture_output=True, text=True, encoding="utf-8", errors="replace",
                           timeout=CALL_TIMEOUT, cwd=WORKDIR)
        row["exit"] = p.returncode
        row.update(parse_result(p.stdout))
        if p.returncode != 0:
            row["stderr"] = (p.stderr or "")[-300:]
        save_snap(role, snap, t0)
    except subprocess.TimeoutExpired:
        row["exit"] = "timeout"
    except Exception as e:  # never crash the dispatcher
        row["exit"] = "error"
        row["error"] = repr(e)[:300]
    row["duration_s"] = round(time.time() - t0, 1)
    try:
        append_log(row)
    except OSError:
        pass
    if sched:
        sched.finished(role)
    return row


# ----------------------------------------------------------------- main loop
def heartbeat(state, sched, rows, api_ok, now):
    d = {"t": now, "iso": time.strftime("%Y-%m-%dT%H:%M:%S"), "pid": os.getpid(), "state": state, "api_ok": api_ok,
         "running": sorted(sched.running),
         "roles": {r: {"last": sched.last.get(r), "spent_1h": round(spent_last_hour(rows, r, now), 4)} for r in sched.roles}}
    p = os.path.join(DATA, "puppet_heartbeat.json")
    try:
        os.makedirs(DATA, exist_ok=True)
        with open(p + ".tmp", "w") as f:
            json.dump(d, f)
        os.replace(p + ".tmp", p)
    except OSError:
        pass


def other_instance_alive():
    try:
        with open(os.path.join(DATA, "puppet_heartbeat.json")) as f:
            d = json.load(f)
        return time.time() - d.get("t", 0) < 45 and d.get("pid") != os.getpid() and d.get("state") != "stopped"
    except (OSError, ValueError):
        return False


def main(argv=None):
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--dry-run", action="store_true", help="print command + briefing, spend nothing")
    ap.add_argument("--once", metavar="ROLE", help="one call for this role now, then exit")
    ap.add_argument("--roles", help="comma list: restrict to these roles")
    ap.add_argument("--list", action="store_true", help="print the role table")
    ap.add_argument("--tick", type=int, default=30, help="seconds between checks (default 30)")
    ap.add_argument("--max-parallel", type=int, default=3)
    ap.add_argument("--model", help="override the model of every call (e.g. haiku for a cheap test)")
    ap.add_argument("--call-cap", type=float, help="override --max-budget-usd of every call")
    ap.add_argument("--claude", default=None, help="path of the claude CLI")
    ap.add_argument("--python", default="python", help="interpreter name the minds use for legionctl")
    ap.add_argument("--force", action="store_true", help="start even if a heartbeat says another dispatcher runs")
    args = ap.parse_args(argv)
    args.claude = args.claude or find_claude()

    if args.list:
        for r, c in ROLES.items():
            print(f"{r:<8} {c['model']:<7} every {c['interval'] // 60 if c['interval'] else '-':>3} min  "
                  f"per-call ${c['call_cap']:.2f}  per-hour ${c['hourly_cap']:.2f}  min-gap {c['min_gap']}s")
        return 0

    roles = [r.strip() for r in args.roles.split(",")] if args.roles else list(ROLES)
    bad = [r for r in roles + ([args.once] if args.once else []) if r not in ROLES]
    if bad:
        print("unknown role(s):", ", ".join(bad), "- known:", ", ".join(ROLES), file=sys.stderr)
        return 2

    if args.dry_run or args.once:
        targets = [args.once] if args.once else roles
        for r in targets:
            cmd = build_command(r, args.claude, args.python, args.model, args.call_cap)
            text, _ = make_briefing(r, "dry-run" if args.dry_run else "once")
            print("=" * 78)
            print(f"ROLE {r}  cwd={WORKDIR}")
            print("COMMAND:", render(cmd))
            print("STDIN (briefing):")
            print(text)
        if args.dry_run:
            return 0
        row = run_call(args.once, "once", args)
        print(json.dumps(row, ensure_ascii=False, indent=1))
        return 0 if row.get("exit") == 0 else 1

    if other_instance_alive() and not args.force:
        print("another dispatcher heartbeat is fresh (<45 s); use --force to start anyway", file=sys.stderr)
        return 3
    pause = os.path.join(DATA, "puppet.pause")
    sched = Scheduler({r: ROLES[r] for r in roles}, time.time(), read_log(since=time.time() - 2 * HOUR))
    sig = Signals(briefing.Api)
    backoff = 0
    threads = []
    print(f"dispatcher up: roles={len(roles)} tick={args.tick}s data={DATA}", flush=True)
    try:
        while True:
            now = time.time()
            rows = read_log(since=now - HOUR)
            try:
                if os.path.exists(pause):
                    heartbeat("paused", sched, rows, None, now)
                    time.sleep(args.tick)
                    continue
                s = sig.poll()
                if not s["api_ok"]:
                    backoff = min(max(backoff * 2, 10), 120)
                    heartbeat("api-down", sched, rows, False, now)
                    time.sleep(max(args.tick, backoff))
                    continue
                backoff = 0
                signals = {"orders": s["orders"], "wake": s["wake"]}
                if s.get("chat"):
                    sched.note("chat", s["chat"]) if "chat" in sched.roles else None
                if s.get("ayla_event"):
                    sched.note("ayla", "event:" + s["ayla_event"]) if "ayla" in sched.roles else None
                threads = [t for t in threads if t.is_alive()]
                for r in roles:
                    if len(threads) >= args.max_parallel:
                        break
                    trig = sched.due(r, now, rows, signals)
                    if not trig:
                        continue
                    sched.started(r, now)
                    sig.consumed(r)
                    t = threading.Thread(target=run_call, args=(r, trig, args, sched, sig), daemon=True)
                    t.start()
                    threads.append(t)
                heartbeat("ok", sched, rows, True, now)
            except Exception as e:  # the loop must survive anything
                print("loop error:", repr(e), file=sys.stderr, flush=True)
            time.sleep(args.tick)
    except KeyboardInterrupt:
        heartbeat("stopped", sched, [], None, time.time())
        print("stopped")
        return 0


if __name__ == "__main__":
    sys.exit(main())
