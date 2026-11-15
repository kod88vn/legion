#!/usr/bin/env python3
"""Puppet-master dispatcher: the one long-running script of the puppet harness. It is not an AI.

It decides when each puppet master wakes (schedule + triggers), assembles a briefing from the engine API and starts a
one-shot agent with the Claude Code CLI in print mode. See tools/puppet/README.md and docs/design/puppet.md.

  python tools/puppet/dispatch.py                  # run the loop
  python tools/puppet/dispatch.py --dry-run        # print the command and briefing for every role, spend nothing
  python tools/puppet/dispatch.py --once guild03   # one call now (with --dry-run: only print)
  python tools/puppet/dispatch.py --roles chat,market   # restrict the loop to these roles
  python tools/puppet/dispatch.py --list           # role table (model, interval, caps)

Pause: create legion/data/puppet.pause (delete it to resume); legion/data/stack.hold (tools/system/stackctl.py down)
pauses it too. Stop: Ctrl+C / kill. Heartbeats: puppet_heartbeat.json (details) and dispatch.heartbeat (epoch, read by
tools/mindgate.py and tools/sentinel.py). Standard library only.
"""
import argparse
import glob
import json
import os
import re
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
WIN_MIND = False  # dispatcher in WSL driving a Windows claude.exe: the mind's Bash is Windows Git Bash (Windows paths)
WSL_DISTRO = os.environ.get("LEGION_WSL_DISTRO", "Ubuntu-24.04")


def win_path(p):
    """/mnt/g/RO/x -> G:/RO/x (a path already in Windows form is returned unchanged)"""
    p = p.replace("\\", "/")
    if p.startswith("/mnt/") and len(p) > 6 and p[6:7] == "/":
        return p[5].upper() + ":" + p[6:]
    return p


def wsl_path(p):
    """G:/RO/legion/x -> /mnt/g/RO/legion/x (a path already in WSL form is returned unchanged)"""
    p = p.replace("\\", "/")
    if len(p) > 1 and p[1] == ":":
        return "/mnt/" + p[0].lower() + p[2:]
    return p


def ayla_cmd(nt=None):
    """the command line Ayla's watcher uses for her (read-only) log. ayla.sh needs WSL: it calls docker (in WSL).
    From Windows (the CLI's Bash tool is Git Bash) it must go through wsl.exe - and with NO argument that starts with
    '/': Git Bash (MSYS) rewrites '/mnt/g/...' into 'C:/Program Files/Git/mnt/g/...', so wsl's bash got a path that
    doesn't exist (exit 127, puppet_calls.jsonl 2026-11-15 02:1x), and the mind's retry with MSYS_NO_PATHCONV=1 in
    front no longer matched the allowed prefix (denied). So: --cd with the Windows repo path + a relative script path.
    When the dispatcher itself runs inside WSL (no Windows CLI), plain bash works."""
    nt = (os.name == "nt" or WIN_MIND) if nt is None else nt
    if nt:
        return f"wsl -d {WSL_DISTRO} --cd {win_path(REPO.replace(chr(92), '/'))} -- bash tools/rogui/ayla.sh"
    return f"bash {wsl_path(AYLA_SH)}"


def mind_env():
    """environment of the CLI child. A Windows claude.exe started from WSL gets only the variables named in WSLENV;
    MSYS_NO_PATHCONV / MSYS2_ARG_CONV_EXCL switch Git Bash's path rewriting off for every command the mind runs (a
    second guard next to ayla_cmd's slash-free form)."""
    env = dict(os.environ)
    env["MSYS_NO_PATHCONV"] = "1"
    env["MSYS2_ARG_CONV_EXCL"] = "*"
    if WIN_MIND:
        names = [n for n in env.get("WSLENV", "").split(":") if n]
        for n in ("MSYS_NO_PATHCONV", "MSYS2_ARG_CONV_EXCL"):
            if n not in names:
                names.append(n)
        env["WSLENV"] = ":".join(names)
    return env

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
# PR38 build/class master: once a day (and on build.wake), haiku, reviews builds and skill use per class against the
# class files and the meter; files code fixes into the bug queue, never plays bots. daily_cap = USD per rolling 24 h.
ROLES["build"] = dict(brief="build", model="haiku", interval=24 * HOUR, offset=2 * 3600, call_cap=0.20, hourly_cap=0.25,
                      daily_cap=0.40, min_gap=1800, bus="build")
# PR17/PR39 class QA (.claude/agents/qa.md as a scheduled one-shot): once after each deploy (a new record in
# legion/data/deploys.jsonl, once the bots settled) and every 3 hours; samples every class, files to the bug queue.
ROLES["qa"] = dict(brief="qa", model="haiku", interval=3 * HOUR, offset=30 * 60, call_cap=0.15, hourly_cap=0.30,
                   daily_cap=1.50, min_gap=1800, bus="qa")
# owner PR38 / AY05 (amended 2026-11-15): her algorithm (partylead.pl) plays her; her dedicated Haiku helper watches and
# steps in only when she is stuck or deciding badly (guarded ayla.sh commands), and every intervention files a code-fix
# item. min_gap 300 = at most one call (so one intervention) per 5 min. Woken by her watchdog's reports too.
ROLES["ayla"] = dict(brief="ayla", model="haiku", interval=10 * 60, offset=20, call_cap=0.08, hourly_cap=0.40,
                     min_gap=300, bus="ayla")

CALL_TIMEOUT = 300
ORDER_GAP = 120  # an owner order wakes a role at most this often


# ----------------------------------------------------------------- command line for the Claude CLI
# ayla.sh actions the helper may use: reads (docker logs, deals.log, status/lead print) and her control (owner reversed
# AY05, 2026-11-14 "Let Haiku control Ayla again"): map (partylead goto - her plugin still rejects a map far above
# the party's gear-rated level or behind dangerous packs), rally <town> [map] / rally cancel, a Butterfly Wing respawn,
# party chat. Never `cmd` with free text: no bare ai / ai off / ai manual can be formed (G05; cmdfile.pl blocks them too).
AYLA_READ = ["log", "chat", "deals", "lead", "status"]
AYLA_ACT = ["map", "rally", "party", "cmd respawn"]
BUGQ = os.path.join(REPO, "tools", "system", "bugq.py").replace("\\", "/")
WD_QUEUE = os.path.join(REPO, "openkore-ayla", "logs", "bugqueue.jsonl")  # partylead.pl wd_report writes here
# her OpenKore console log (every "[watchdog] CAUSE ..." line, also the repeats the queue file dedupes for an hour)
WD_CONSOLE = os.path.join(REPO, "openkore-ayla", "logs", "console_player_2.txt")
WD_RE = re.compile(r"\[watchdog\] (STUCK|IDLE|WRONG_MAP)\b")


REVIEWERS = ("qa", "build")  # scheduled reviewers: observe and file only, like Ayla's watcher


def allowed_tools(role, python="python"):
    """Bash only, and only legionctl (Ayla's watcher also reads her console log through ayla.sh, read-only actions)."""
    tools = [f"Bash({python} {mind_path(CTL)}:*)"]
    if role == "ayla":
        tools += [f"Bash({ayla_cmd()} {a}:*)" for a in AYLA_READ]
        tools += [f"Bash({ayla_cmd()} {a})" if a == "cmd respawn" else f"Bash({ayla_cmd()} {a}:*)" for a in AYLA_ACT]
        tools.append(f"Bash({python} {mind_path(BUGQ)} add:*)")
    return tools


# legionctl commands that hand bots direct goals/maps: puppet masters set directions, prices, words, never these
# guild-kick: the duty ledger kicks by its own rules; a leader mind pardons but never kicks by hand
DENIED_CTL = ["goal", "guild-goal", "party", "leader-party", "boss-hunt", "boss-cancel", "guild-kick"]


# the watcher only observes and files: no words, prices, directions or chat jobs in the world either
DENIED_WATCHER = ["say", "whisper", "party-say", "guild-say", "shop", "remember", "chat-jobs", "chat-intent", "guild-direction-set",
                  "guild-direction-clear", "guild-pardon", "leader", "boss-check"]


def denied_tools(python="python", role=""):
    cmds = DENIED_CTL + (DENIED_WATCHER if role == "ayla" or role in REVIEWERS else [])
    return [f"Bash({python} {mind_path(CTL)} {c}:*)" for c in cmds]


def load_brief(role):
    cfg = ROLES[role]
    text = ""
    for name in ("_common", cfg["brief"]):
        with open(os.path.join(BRIEFS, name + ".md"), encoding="utf-8") as f:
            text += f.read().rstrip() + "\n\n"
    return (text.replace("{CTL}", mind_path(CTL)).replace("{ROLE}", role).replace("{MIND}", cfg["bus"])
            .replace("{AYLA_SH}", ayla_cmd()).replace("{NOTES}", mind_path(os.path.join(NOTES, role + ".md").replace("\\", "/"))))


def mind_path(p):
    return win_path(p) if WIN_MIND else p


def build_command(role, claude="claude", python="python", model=None, call_cap=None):
    cfg = ROLES[role]
    cmd = [claude, "-p",
           "--model", model or cfg["model"],
           "--safe-mode",  # no CLAUDE.md, skills, hooks, MCP: the big project rules are not loaded
           "--tools", "Bash",
           "--allowedTools", *allowed_tools(role, python),
           "--disallowedTools", *denied_tools(python, role),
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
    """the claude CLI. In WSL only the Windows claude.exe exists (no `claude` on the Linux PATH: FileNotFoundError, 2026-11-14)."""
    for name in ("claude", "claude.exe"):
        found = shutil.which(name)
        if found:
            return found
    for home in sorted(glob.glob("/mnt/c/Users/*/.local/bin/claude.exe")):
        return home
    return "claude"


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
def spent_since(rows, role, since):
    return sum(float(r.get("cost_usd") or 0) for r in rows if r.get("role") == role and r.get("t", 0) >= since)


# ----------------------------------------------------------------- deploy trigger (class QA after each deploy)
DEPLOY_SETTLE = 10 * 60    # the sentinel ignores the first 10 min after a deploy too: logins re-plan
DEPLOY_MAX_AGE = 6 * HOUR  # an older deploy never wakes QA (a restarted dispatcher must not chase old deploys)


def last_deploy(data=None):
    """(epoch, tag) of the newest record in legion/data/deploys.jsonl (tools/system/deploylog.py), or (None, None)"""
    best = (None, None)
    try:
        with open(os.path.join(data or DATA, "deploys.jsonl"), encoding="utf-8") as f:
            for line in f:
                try:
                    r = json.loads(line)
                    t = float(r.get("epoch") or 0)
                except (ValueError, TypeError, AttributeError):
                    continue
                if t and (best[0] is None or t > best[0]):
                    best = (t, r.get("tag"))
    except OSError:
        pass
    return best


def deploy_wake(last_call, now, data=None):
    """the deploy tag QA should check now: a deploy newer than QA's last call, settled, not too old; else None"""
    t, tag = last_deploy(data)
    if t is None or now - t < DEPLOY_SETTLE or now - t > DEPLOY_MAX_AGE:
        return None
    if last_call is not None and last_call >= t:
        return None
    return tag or "deploy"


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

    def due(self, role, now, rows, signals, day_rows=None):
        """trigger label ('schedule', 'orders', 'human', ...) or None"""
        cfg = ROLES[role]
        if role in self.running:
            return None
        if spent_last_hour(rows, role, now) >= cfg["hourly_cap"]:
            return None
        if cfg.get("daily_cap") is not None and day_rows is not None and                 spent_since(day_rows, role, now - 24 * HOUR) >= cfg["daily_cap"]:
            return None
        last = self.last.get(role)
        gap = now - last if last else None
        if gap is not None and gap < cfg["min_gap"]:
            return None
        trig = None
        w = signals.get("wake", {}).get(role)
        if w:
            trig = w if isinstance(w, str) else "wake"
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


def ayla_idle_state(neo, threshold=0.5):
    """Ayla is an OpenKore character, not a Legion bot: the engine's event ring never holds her own deaths, level-ups or
    idle stretches, so her mind got 'no event' while she stood in town (audit 2026-11-14, rule AY18). The meter's
    status-based shares are the one signal about her. Returns 'idle' when town/idle/waiting shares of her last 10 min
    reach `threshold`, else None."""
    try:
        sh = ((neo.get("ayla") or {}).get("windows") or {}).get("10m", {}).get("shares") or {}
    except AttributeError:
        return None
    lazy = sum(float(sh.get(k) or 0) for k in ("town", "idle", "waiting"))
    return "idle" if lazy >= threshold else None


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
        self.ayla_seen = {}  # event class -> last time it woke her (a repeating class must not wake her every 2 min)
        self.wd_offset = None  # bytes of her watchdog's queue file already seen
        self.wd_console = None  # bytes of her console log already seen

    def _ayla_wake(self, cls, cooldown):
        now = time.time()
        if now - self.ayla_seen.get(cls, 0) >= cooldown:
            self.ayla_seen[cls] = now
            self.pending_ayla = self.pending_ayla or cls

    def poll(self, chat_present=False):
        """chat_present: the chat role can be woken right now (in the loop, not paused, under its hourly cap). Then the
        chat-jobs GET is a real poll (no peek), so the engine holds social lines for the chat agent's words (it waits
        only while an agent polled within 2 min); otherwise peek, and every line goes out as its template at once."""
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
        jobs = api.get("/api/chat-jobs" if chat_present else "/api/chat-jobs?peek=1")
        jl = jobs if isinstance(jobs, list) else briefing.dig(jobs, "jobs", default=None)
        njobs = len(jl if isinstance(jl, list) else briefing.rows_of(jobs)) if jobs is not None else 0
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
                        self._ayla_wake(c, 0 if c == "death" else 600)
            self.event_cursor = ev.get("next", self.event_cursor or 0)
        # her own state (no event ever carries it): standing in town / idle for most of 10 min wakes her mind
        if ayla_idle_state(api.get("/api/meter/neo") or {}):
            self._ayla_wake("idle", 600)
        # her watchdog's reports (partylead.pl wd_report: STUCK / WRONG_MAP / IDLE) wake her helper
        for cause in self.watchdog_reports() + self.watchdog_console():
            self._ayla_wake("watchdog:" + cause, 300)
        out["ayla_event"] = self.pending_ayla
        return out

    def watchdog_reports(self, path=None):
        """causes of new lines in her watchdog's queue file since the last poll (the first poll only sets the offset)"""
        path = path or WD_QUEUE
        try:
            size = os.path.getsize(path)
        except OSError:
            return []
        if self.wd_offset is None or size < self.wd_offset:
            self.wd_offset = size
            return []
        out = []
        try:
            with open(path, "rb") as f:
                f.seek(self.wd_offset)
                chunk = f.read(size - self.wd_offset)
            self.wd_offset = size
        except OSError:
            return []
        for line in chunk.decode("utf-8", "replace").splitlines():
            try:
                r = json.loads(line)
            except ValueError:
                continue
            m = re.match(r"Ayla watchdog (\w+)", str(r.get("text", "")))
            if r.get("from") == "ayla-watchdog" and m and m.group(1).lower() not in out:
                out.append(m.group(1).lower())
        return out

    def watchdog_console(self, path=None):
        """causes of new '[watchdog] STUCK|IDLE|WRONG_MAP' lines in her console log since the last poll"""
        path = path or WD_CONSOLE
        try:
            size = os.path.getsize(path)
        except OSError:
            return []
        if self.wd_console is None or size < self.wd_console:
            self.wd_console = size
            return []
        start = max(self.wd_console, size - 2_000_000)  # never read more than 2 MB in one poll
        try:
            with open(path, "rb") as f:
                f.seek(start)
                chunk = f.read(size - start)
        except OSError:
            return []
        self.wd_console = size
        out = []
        for m in WD_RE.finditer(chunk.decode("utf-8", "replace")):
            c = m.group(1).lower()
            if c not in out:
                out.append(c)
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
                           timeout=CALL_TIMEOUT, cwd=WORKDIR, env=mind_env())
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
        hb = os.path.join(DATA, "dispatch.heartbeat")  # the system gate/sentinel read this one (content = epoch)
        if state == "stopped":
            if os.path.exists(hb):
                os.remove(hb)
        else:
            with open(hb, "w") as f:
                f.write(str(int(now)))
    except OSError:
        pass


def paused(data=None):
    """puppet.pause (the dispatcher alone) or stack.hold (the owner stopped the stack) -> wake nobody"""
    d = data or DATA
    return os.path.exists(os.path.join(d, "puppet.pause")) or os.path.exists(os.path.join(d, "stack.hold"))


def other_instance_alive():
    try:
        with open(os.path.join(DATA, "puppet_heartbeat.json")) as f:
            d = json.load(f)
        return time.time() - d.get("t", 0) < 45 and d.get("pid") != os.getpid() and d.get("state") != "stopped"
    except (OSError, ValueError):
        return False


def main(argv=None):
    global WIN_MIND
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
    WIN_MIND = os.name != "nt" and args.claude.lower().endswith(".exe")

    if args.list:
        for r, c in ROLES.items():
            print(f"{r:<8} {c['model']:<7} every {int(c['interval'] // 60) if c['interval'] else '-':>4} min  "
                  f"per-call ${c['call_cap']:.2f}  per-hour ${c['hourly_cap']:.2f}  min-gap {c['min_gap']}s"
                  + (f"  per-day ${c['daily_cap']:.2f}" if c.get("daily_cap") is not None else ""))
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
    # 25 h of the log: the daily build review and QA's last call survive a restart
    sched = Scheduler({r: ROLES[r] for r in roles}, time.time(), read_log(since=time.time() - 25 * HOUR))
    day_rows, day_t = [], 0.0
    sig = Signals(briefing.Api)
    backoff = 0
    threads = []
    print(f"dispatcher up: roles={len(roles)} tick={args.tick}s data={DATA}", flush=True)
    try:
        while True:
            now = time.time()
            rows = read_log(since=now - HOUR)
            if now - day_t > 300:  # the 24 h spend (daily caps) is re-read every 5 min, not every tick
                day_rows, day_t = read_log(since=now - 24 * HOUR), now
            try:
                if paused():
                    heartbeat("paused", sched, rows, None, now)
                    time.sleep(args.tick)
                    continue
                chat_ok = "chat" in sched.roles and spent_last_hour(rows, "chat", now) < ROLES["chat"]["hourly_cap"]
                s = sig.poll(chat_present=chat_ok)
                if not s["api_ok"]:
                    backoff = min(max(backoff * 2, 10), 120)
                    heartbeat("api-down", sched, rows, False, now)
                    time.sleep(max(args.tick, backoff))
                    continue
                backoff = 0
                signals = {"orders": s["orders"], "wake": dict(s["wake"])}
                dtag = deploy_wake(sched.last.get("qa"), now) if "qa" in sched.roles else None
                if dtag:
                    signals["wake"]["qa"] = "deploy:" + str(dtag)
                if s.get("chat"):
                    sched.note("chat", s["chat"]) if "chat" in sched.roles else None
                if s.get("ayla_event"):
                    sched.note("ayla", "event:" + s["ayla_event"]) if "ayla" in sched.roles else None
                threads = [t for t in threads if t.is_alive()]
                for r in roles:
                    if len(threads) >= args.max_parallel:
                        break
                    trig = sched.due(r, now, rows, signals, day_rows)
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
