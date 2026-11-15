#!/usr/bin/env python3
"""Ayla log reporter (no AI): tails Ayla's OpenKore console log and files every problem it sees into the bug queue
(PR39, AY18). Runs on Windows Python or WSL python3 (stdlib only).

  aylalog.py [--log PATH] [--interval 30] [--backlog-h 6] [--dry-run] [--once] [--no-meter] [--base URL]

- Reads openkore-ayla/logs/console_player_2.txt (AYLA_LOG or --log overrides) in binary, survives truncation and
  rotation (size shrink or a changed file head -> read again from the start), keeps a partial last line for later.
- Classifies problem lines into kinds (RULES below, derived from her real log on 2026-11-14/15) and adds metric checks:
  no exp gained for 5 min on a field/dungeon (every further 5 min is another hit), more than 15 min in a town, her
  kills/min (from exp lines) far under her cohort par and a high town share (GET <base>/api/meter/neo, if reachable),
  and a silent log (OpenKore down or hung).
- Files to the bug queue with bugq.add(from="ayla-log", key="ayla:<kind>:<map>", bots=1): one write per key at most
  every 10 min; the queue's count = the number of 10-minute windows in which the problem showed up, the field `hits` =
  total lines seen. Evidence: count in the window, map, cell, 2-3 sample lines. partylead's "[watchdog] CAUSE" lines
  bump the watchdog's own item (key "ayla-watchdog-<cause>-<map>"), so its hourly-deduped item shows real frequency.
- First start (no state, or state older than the backlog): the last --backlog-h hours of the log seed the queue once.
  State (file offset, per-key last write) lives in <data>/aylalog.state.json; heartbeat <data>/aylalog.heartbeat.
- Flood guard: at most MAX_WRITES_PER_CYCLE queue writes per poll and MAX_NEW_KEYS_PER_H new items an hour; the rest
  waits for the next cycle. Per-kind minimum counts keep background noise (loot retries, party spacing) out.
- --dry-run prints what it would file and writes nothing (no queue, no state, no heartbeat). With --once it exits
  after the backlog (and one live pass).
Log timestamps are the container clock (UTC); --log-utc-offset (hours) if that changes.
"""
import argparse
import calendar
import json
import os
import re
import sys
import time
import urllib.request

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import common as C

FROM = "ayla-log"
WINDOW = 600                 # one queue write per key per 10 min
MAX_WRITES_PER_CYCLE = 20
MAX_NEW_KEYS_PER_H = 40
ONCE_KINDS = ("unknown-switch",)   # console noise (280 lines in 6 h, b1794721138oed): one queue item per key, then only hits
NOEXP_S = 300
TOWN_STALL_S = 900
LOG_SILENT_S = 600
METER_EVERY = 600

TS_RE = re.compile(rb"^\[(\d{4})\.(\d\d)\.(\d\d) (\d\d):(\d\d):(\d\d)(?:\.\d+)?\] ?")
TOWN_RE = re.compile(r"^(prontera|payon|geffen|morocc|alberta|izlude|aldebaran|yuno|comodo|umbala|amatsu|gonryun|"
                     r"louyang|ayothaya|einbroch|einbech|lighthalzen|hugel|rachel|veins|moscovia|niflheim|xmas|"
                     r"brasilis|dewata|malangdo|malaya|mora|eclage|splendide|manuk|dicastes01|lutie)$|_in\d*$")
EXP_RE = re.compile(r"^You have gained (\d+)/(\d+) ")
MAPCHG_RE = re.compile(r"^Map Change: (?P<map>[\w@-]+)\.gat \((?P<x>\d+), ?(?P<y>\d+)\)")
TRACE_RE = re.compile(r"^\s*#?\d+: |^\[print_callers\]|^Last \d+ callers")


def is_town(m):
    return bool(m) and bool(TOWN_RE.search(m))


class K(object):
    """kind, regexes (first match wins over all rules in order), priority, min hits per window to file, hits per
    window from which a normal kind is filed high, map handling ('line' = the map named in the line or her current
    map, 'any' = map-independent, 'extra' = key on the captured extra group)."""

    def __init__(self, kind, pats, prio="normal", min_n=1, high_at=None, key="line", desc="", test=None):
        self.kind, self.prio, self.min_n, self.high_at, self.key, self.desc, self.test = \
            kind, prio, min_n, high_at, key, desc or kind, test
        self.pats = [re.compile(p) for p in (pats if isinstance(pats, (list, tuple)) else [pats])]


CELL = r"\((?P<x>\d+), ?(?P<y>\d+)\)"
RULES = [
    # a map is taken only where the watchdog names one: "... on <map>" (IDLE, WRONG_MAP) or "<map> x,y" (STUCK). A bare
    # word after the cause ("TELEPORT allowed: ...", "crossed", "hold") is no map (b1794736126l06: it corrupted her map).
    K("watchdog", [r"^\[watchdog\] (?P<extra>[A-Z_]+) (?:(?:no productive action for \d+ s )?on )(?P<map>[\w@-]+)(?: (?P<x>\d+),(?P<y>\d+))?",
                   r"^\[watchdog\] (?P<extra>[A-Z_]+) (?P<map>[\w@-]+) (?P<x>\d+),(?P<y>\d+)",
                   r"^\[watchdog\] (?P<extra>[A-Z_]+)\b"],
      "normal", desc="partylead watchdog fired"),
    K("route-loop", r"^\[getRoute\] Called with (?:no valid closest coordinates|invalid coordinates) on field (?P<map>[\w@-]+)(?: " + CELL + ")?",
      "normal", 1, 30, desc="route calculation keeps failing (getRoute invalid/unwalkable start)"),
    K("route-unreachable-npc", r"^Unable to walk from (?P<map>[\w@-]+) " + CELL + r" to NPC at \((?P<extra>[\d,]+)\)",
      "normal", 1, 10, desc="cannot walk to an NPC"),
    K("route-calc-fail", r"^Cannot calculate a route from (?P<map>[\w@-]+) " + CELL + r" to (?P<extra>[\w@-]+)",
      "normal", 1, 10, desc="cannot calculate a route"),
    K("route-stuck", [r"^Stuck at (?P<map>[\w@-]+) " + CELL, r"^Stuck during route", r"^You tried too long to teleport"],
      "high", desc="stuck while walking"),
    K("standing-still", r"^\[partylead\] standing still too long, stepping to (?P<x>\d+),(?P<y>\d+)",
      "high", desc="standing still too long (partylead nudges her)"),
    K("unwalkable", [r"^Coordinates \d+ \d+ are not walkable on the map (?P<map>[\w@-]+)",
                     r"^\[partylead\] sweep: waypoint (?P<x>\d+),(?P<y>\d+) unreachable",
                     r"^Map (?P<extra>[\w@-]*) ?does not exist"],
      "normal", 1, 10, desc="unwalkable cell / unreachable waypoint / unknown map"),
    K("target-unreachable", [r"^Can't reach or damage target", r"^Unable to calculate a meetingPosition"],
      "normal", 5, 40, desc="cannot reach the target"),
    K("disconnect", [r"^Disconnected from Map Server", r"^Timeout on (?:Map|Account|Character Select|Character) Server",
                     r"^Error: The server still recognizes your last connection", r"^\[reconnect\] Login retry"],
      "high", key="any", desc="disconnect / login timeout"),
    K("unknown-switch", r"^Packet Parser: Unknown switch: (?P<extra>\w+)", "low", 1, None, key="extra",
      desc="unknown packet switch"),
    K("npc-fail", [r"^NPC error: Could not find an NPC", r"^Could not find an NPC",
                   r"^\[TalkNPC\].*(?:[Ff]ail|[Ee]rror|[Tt]imeout|[Cc]annot|[Uu]nable)", r"^Error in function 'talk"],
      "normal", 1, 5, desc="NPC talk failed"),
    K("buy-fail", [r"^buyAuto index \d+ \((?P<extra>.+?)\) failed", r"^Error in function 'buy"],
      "normal", 1, 20, desc="buying failed"),
    K("sell-storage-fail", [r"^(?:sellAuto|storageAuto|Auto-?sell|Auto-?storage).*(?:fail|error|unable|cannot)",
                            r"^(?:Your )?[Ss]torage (?:is )?full", r"^Storage .*(?:fail|unable)"],
      "normal", 1, 5, desc="selling / storage failed"),
    K("gift-held-loop", r"^\[aylagear\] give: mail to (?P<extra>\S+) failed: Ayla must keep", "normal", 3, None,
      key="extra", desc="gift held by the 10k rule but retried in a loop"),
    K("gift-fail", [r"^\[aylagear\] give: mail to (?P<extra>\S+) failed", r"^\[aylagear\] mailbox: no answer"],
      "normal", 1, 10, key="any", desc="RODEX gift / mailbox failed"),
    K("rally-fail", [r"^\[partylead\] rally in (?P<map>[\w@-]+) over: (?:cancel|can't|gave|time|no )",
                     r"^\[partylead\] rally cancelled", r"^\[partylead\] .*rally.*(?:timed? ?out|gave up|giving up|abandon)"],
      "normal", 1, 5, desc="rally/regroup failed or cancelled"),
    K("party-apart", [r"^\[partylead\] (?:waiting for the party|holding for the party): (?P<extra>.+?) is ",
                      r"^\[partylead\] (?:leaving town|in [\w@-]+): waiting for (?P<extra>\S+)",
                      r"^\[partylead\] going back for (?P<extra>\S+): it is not moving"],
      "normal", 10, 60, desc="party apart / waiting for members"),
    K("death", r"^\[processDead\] You died", "normal", 1, 3, desc="died"),
    K("party-death", r"^Party member (?P<extra>\S+) is dead", "normal", 3, 8, desc="party members dying"),
    K("hp-flee", r"^Teleporting due to insufficient HP/SP or too many aggressives", "normal", 1, 10,
      desc="teleport flee on low HP / mobbed"),
    K("mobbed", r"^\[partylead\] no looting now: \d+ monsters on her, HP (?P<extra>\d+)%", "normal", 10, None,
      desc="mobbed at low HP", test=lambda m: int(m.group("extra")) < 50),
    K("skill-fail", [r"^You failed to cast", r"^Skill (?P<extra>.+?) (?:has )?failed", r"^Not enough SP"],
      "normal", 3, 30, desc="skill cast failed"),
    K("item-fail", r"^You failed to use item: (?P<extra>.+?) \(", "normal", 3, 30, desc="item use failed"),
    K("loot-fail", [r"^Can't loot item", r"^Failed to (?:gather|take) (?P<extra>.+?) \("], "normal", 30, None,
      desc="loot pickup failing"),
    K("cmd-error", [r"^Syntax Error in function '(?P<extra>[^']+)'", r"^Error in function '(?P<extra>[^']+)'"],
      "normal", 1, None, key="any", desc="console command error (bad command sent to her)"),
    K("perl-error", [r"^Undefined subroutine", r"^Can't locate ", r"Can't find string terminator", r"^Use of uninitialized",
                     r"^Global symbol", r"syntax error at ", r"^Deep recursion", r"^Not a (?:HASH|ARRAY|CODE) reference",
                     r"^Can't (?:call|use) ", r" at \S+\.p[lm] line \d+\.?$"],
      "high", key="any", desc="Perl error"),
    K("plugin-error", r"^\[(?P<extra>(?!cmdfile|dist=)[a-zA-Z_]+)\] .*(?:failed|[Ee]rror|cannot|can't|unable)", "normal",
      1, 10, key="any", desc="plugin warning/error"),
]
METRIC_KINDS = {
    "no-exp": ("high", "no exp gained for 5 min while on a field/dungeon"),
    "town-stall": ("high", "in town for more than 15 min"),
    "below-par": ("normal", "kills/min far under her cohort par"),
    "town-share": ("high", "town share of the last hour too high (meter)"),
    "log-silent": ("high", "console log silent (OpenKore down or hung)"),
}
KIND = {k.kind: k for k in RULES}


def kind_desc(kind):
    return KIND[kind].desc if kind in KIND else METRIC_KINDS.get(kind, ("", kind))[1]


def parse_ts(raw, utc_off_h=0.0):
    m = TS_RE.match(raw)
    if not m:
        return None, raw
    y, mo, d, h, mi, s = (int(g) for g in m.groups())
    try:
        t = calendar.timegm((y, mo, d, h, mi, s, 0, 0, 0)) - utc_off_h * 3600
    except (ValueError, OverflowError):
        return None, raw
    return float(t), raw[m.end():]


class Event(object):
    __slots__ = ("t", "kind", "map", "cell", "line", "prio", "extra")

    def __init__(self, t, kind, mp, cell, line, prio=None, extra=""):
        self.t, self.kind, self.map, self.cell, self.line, self.prio, self.extra = t, kind, mp, cell, line, prio, extra

    def key(self):
        if self.kind == "watchdog":
            return ("ayla-watchdog-%s-%s" % (self.extra, self.map or "?")).lower()
        k = KIND.get(self.kind)
        sub = self.map or "?"
        if k is not None and k.key == "any":
            sub = "any"
        elif k is not None and k.key == "extra":
            sub = re.sub(r"[^\w.-]+", "_", self.extra or "?")[:40]
        return "ayla:%s:%s" % (self.kind, sub)


def classify(msg):
    """(rule, match) for a problem line, or (None, None)."""
    if not msg or msg.startswith("[dist=") or TRACE_RE.match(msg):
        return None, None
    for r in RULES:
        for p in r.pats:
            m = p.search(msg)
            if m:
                if r.test is not None:
                    try:
                        if not r.test(m):
                            return None, None
                    except (ValueError, TypeError, IndexError):
                        return None, None
                return r, m
    return None, None


def _g(m, name):
    return m.groupdict().get(name)


class Tracker(object):
    """Turns timestamped lines into Events and keeps her map, cell, last exp and town time."""

    def __init__(self):
        self.map, self.cell = "", ""
        self.map_since = None
        self.last_exp = None
        self.exp_times = []          # nonzero exp gains (kills) in the last hour
        self.dead = False
        self.last_t = None
        self.noexp_marks = 0
        self.town_marks = 0

    def set_map(self, mp, t, x=None, y=None):
        if mp and mp != self.map:
            self.map, self.map_since = mp, t
            self.noexp_marks = self.town_marks = 0
        if x is not None and y is not None:
            self.cell = "%s,%s" % (x, y)

    def feed(self, t, msg):
        out = []
        if t is None:
            t = self.last_t
        if t is None:
            return out
        if self.last_t is not None and t - self.last_t > LOG_SILENT_S:
            out.append(Event(t, "log-silent", self.map or "?", self.cell, "log gap of %d min before %s"
                             % (int((t - self.last_t) / 60), time.strftime("%H:%M:%S", time.gmtime(t))), "high"))
        self.last_t = t
        if self.map_since is None:
            self.map_since = t
        m = EXP_RE.match(msg)
        if m:
            if int(m.group(1)) or int(m.group(2)):
                self.last_exp = t
                self.noexp_marks = 0
                self.exp_times.append(t)
                if len(self.exp_times) > 4000 or (self.exp_times and self.exp_times[0] < t - 3600):
                    self.exp_times = [x for x in self.exp_times if x >= t - 3600]
            return out
        m = MAPCHG_RE.match(msg)
        if m:
            self.set_map(m.group("map"), t, m.group("x"), m.group("y"))
            self.dead = False
            return out
        if msg.startswith("[processDead] Sending respawn") or msg.startswith("You have been resurrected"):
            self.dead = False
        r, m = classify(msg)
        if r is not None:
            mp, x, y, extra = _g(m, "map"), _g(m, "x"), _g(m, "y"), _g(m, "extra") or ""
            if r.kind in ("route-loop", "route-unreachable-npc", "route-calc-fail", "route-stuck", "unwalkable", "watchdog") and mp:
                self.set_map(mp, t, x, y)
            elif x is not None and y is not None and r.kind != "standing-still":
                self.cell = "%s,%s" % (x, y)
            if r.kind == "death":
                self.dead = True
            prio = r.prio
            if r.kind == "watchdog" and extra in ("STUCK", "IDLE"):
                prio = "high"
            cell = "%s,%s" % (x, y) if (x is not None and y is not None) else self.cell
            ev_map = (mp or self.map or "?") if r.kind != "unknown-switch" else (self.map or "?")
            out.append(Event(t, r.kind, ev_map, cell, msg.strip()[:220], prio, extra))
        out += self.tick(t)
        return out

    def tick(self, t):
        """Metric checks at time t: one event per 5 min without exp on a field, per 15 min in a town."""
        out = []
        if t is None or self.dead or not self.map:
            return out
        if is_town(self.map):
            since = self.map_since or t
            n = int((t - since) // TOWN_STALL_S)
            if n > self.town_marks:
                self.town_marks = n
                out.append(Event(t, "town-stall", self.map, self.cell,
                                 "in town %s for %d min" % (self.map, int((t - since) / 60))))
            return out
        base = max(self.last_exp or 0, self.map_since or 0)
        if not base:
            return out
        n = int((t - base) // NOEXP_S)
        if n > self.noexp_marks:
            self.noexp_marks = n
            out.append(Event(t, "no-exp", self.map, self.cell,
                             "no exp for %d min on %s %s" % (int((t - base) / 60), self.map, self.cell)))
        return out

    def kpm(self, t, span=3600):
        n = sum(1 for x in self.exp_times if t - span <= x <= t)
        return n / (span / 60.0)


# ------------------------------------------------------------------ log tailing
class Tail(object):
    """Binary-safe follower: yields complete lines; truncation (size < offset) or a changed head = start again."""

    HEAD = 256

    def __init__(self, path, offset=0, head=""):
        self.path, self.offset, self.head, self.buf = path, offset, head, b""

    def _head(self):
        try:
            with open(self.path, "rb") as f:
                return f.read(self.HEAD).hex()
        except OSError:
            return ""

    def read(self, max_bytes=64 << 20):
        try:
            size = os.path.getsize(self.path)
        except OSError:
            return []
        h = self._head()
        if size < self.offset or (self.head and h and len(h) >= len(self.head) and h[:len(self.head)] != self.head):
            self.offset, self.buf = 0, b""          # truncated or rotated
        self.head = h
        if size == self.offset:
            return []
        with open(self.path, "rb") as f:
            f.seek(self.offset)
            data = f.read(min(max_bytes, size - self.offset))
        self.offset += len(data)
        data = self.buf + data
        parts = data.split(b"\n")
        self.buf = parts.pop()
        if len(self.buf) > 1 << 20:                 # a runaway line without newline: drop it
            self.buf = b""
        return parts


def decode(raw):
    return raw.rstrip(b"\r").decode("utf-8", "replace")


# ------------------------------------------------------------------ filing
class Filer(object):
    def __init__(self, dry_run=False, state=None, out=sys.stdout):
        self.dry = dry_run
        self.out = out
        st = state or {}
        self.last = dict(st.get("last", {}))     # key -> epoch of last queue write
        self.hits = dict(st.get("hits", {}))     # key -> total lines ever seen
        self.new_keys = list(st.get("new_keys", []))
        self.pending = {}                         # key -> [Event]
        self.writes = []                          # (key, prio, text, evidence, count) for tests / dry run

    def add(self, ev):
        k = ev.key()
        self.pending.setdefault(k, []).append(ev)
        self.hits[k] = self.hits.get(k, 0) + 1

    def _prio(self, kind, evs):
        p = "normal"
        if kind in KIND:
            r = KIND[kind]
            p = r.prio
            if r.high_at and len(evs) >= r.high_at:
                p = "high"
        elif kind in METRIC_KINDS:
            p = METRIC_KINDS[kind][0]
        if any(e.prio == "high" for e in evs):
            p = "high"
        return p

    def _min(self, kind):
        return KIND[kind].min_n if kind in KIND else 1

    @staticmethod
    def samples(evs):
        pick = [evs[0]] + ([evs[len(evs) // 2]] if len(evs) > 2 else []) + ([evs[-1]] if len(evs) > 1 else [])
        return " | ".join("%s %s" % (time.strftime("%H:%M:%S", time.gmtime(e.t)), e.line[:160]) for e in pick)

    def compose(self, k, evs, label):
        e0 = evs[-1]
        kind = e0.kind
        maps = sorted(set(e.map for e in evs if e.map))
        mp = e0.map or "?"
        text = "Ayla %s on %s: %d times %s (ayla-log)" % (kind_desc(kind), mp, len(evs), label)
        if kind == "watchdog":
            text = "Ayla watchdog %s on %s: fired %d times %s (seen in her log)" % (e0.extra, mp, len(evs), label)
        ev = "count %d %s (total %d lines since tracking); map %s%s; cell %s; first %s UTC, last %s UTC; samples: %s" % (
            len(evs), label, self.hits.get(k, len(evs)), mp, (" (also %s)" % ",".join(m for m in maps if m != mp)[:80]) if len(maps) > 1 else "",
            e0.cell or "?", time.strftime("%H:%M:%S", time.gmtime(evs[0].t)), time.strftime("%H:%M:%S", time.gmtime(e0.t)),
            self.samples(evs))
        return text, ev[:900]

    def file(self, k, prio, text, evidence, windows=1, now=None):
        """One queue write. windows = how many 10-min windows this write stands for (backlog seeding)."""
        self.writes.append((k, prio, text, evidence, windows))
        if self.dry:
            print("WOULD FILE [%s] %s x%d: %s\n    evidence: %s" % (prio, k, windows, text, evidence[:400]), file=self.out)
            return
        import bugq
        i, new = bugq.add(FROM, prio, text, evidence, key=k, bots=1)
        upd = {"hits": self.hits.get(k, 0)}
        if windows > 1:
            e = bugq.load().get(i, {})
            upd["count"] = int(e.get("count", 1)) + windows - 1
        bugq.update(i, **upd)

    def _allowed_new(self, k, now):
        self.new_keys = [x for x in self.new_keys if now - x[1] < 3600]
        if k in self.last:
            return True
        return len(self.new_keys) < MAX_NEW_KEYS_PER_H

    def flush(self, now, force=False):
        """File every key whose window is due. Returns the number of writes."""
        n = 0
        for k in sorted(self.pending, key=lambda x: -len(self.pending[x])):
            evs = [e for e in self.pending[k] if e.t >= now - WINDOW or force]
            if not evs:
                del self.pending[k]
                continue
            self.pending[k] = evs
            kind = evs[-1].kind
            if len(evs) < self._min(kind):
                continue
            if kind in ONCE_KINDS and k in self.last:
                del self.pending[k]
                continue
            if now - self.last.get(k, 0) < WINDOW:
                continue
            if n >= MAX_WRITES_PER_CYCLE or not self._allowed_new(k, now):
                continue
            if k not in self.last:
                self.new_keys.append((k, now))
            text, evd = self.compose(k, evs, "in the last 10 min")
            self.file(k, self._prio(kind, evs), text, evd, 1, now)
            self.last[k] = now
            del self.pending[k]
            n += 1
        return n

    def seed(self, events, now, hours):
        """Backlog: one write per key for the whole span; count = 10-minute windows that met the kind's minimum."""
        by = {}
        for e in events:
            by.setdefault(e.key(), []).append(e)
        n = 0
        def order(kv):
            evs = kv[1]
            return (self._prio(evs[-1].kind, evs) != "high", -len(evs))
        for k, evs in sorted(by.items(), key=order):
            kind = evs[-1].kind
            buckets = {}
            for e in evs:
                buckets.setdefault(int(e.t // WINDOW), []).append(e)
            wins = [b for b in buckets.values() if len(b) >= self._min(kind)]
            self.hits[k] = self.hits.get(k, 0) + len(evs)
            if not wins:
                continue
            if not self._allowed_new(k, now):
                continue
            if k not in self.last:
                self.new_keys.append((k, now))
            worst = max(wins, key=len)
            text, evd = self.compose(k, evs, "in the last %gh (%d ten-minute windows)" % (hours, len(wins)))
            evd = ("worst window %d; " % len(worst)) + evd
            self.file(k, self._prio(kind, worst), text, evd[:900], len(wins), now)
            self.last[k] = now
            n += 1
        return n

    def state(self):
        return {"last": self.last, "hits": self.hits, "new_keys": self.new_keys}


# ------------------------------------------------------------------ meter
def meter_check(base, tracker, now, timeout=8):
    """Events from /api/meter/neo: kills/min (from her exp lines) vs her cohort par, and town share of the last hour."""
    try:
        with urllib.request.urlopen(base + "/api/meter/neo?window=1h", timeout=timeout) as r:
            neo = json.load(r)
    except Exception:
        return []
    return meter_events(neo, tracker, now)


def meter_events(neo, tracker, now):
    out = []
    a = (neo or {}).get("ayla") or {}
    if not a.get("online"):
        return out
    w = (a.get("windows") or {}).get("1h") or {}
    mins = w.get("min") or 0
    mp = a.get("map") or tracker.map or "?"
    if mins < 30:
        return out
    par = w.get("par_kpm")
    if isinstance(par, (int, float)) and par > 0:
        k = tracker.kpm(tracker.last_t or now, 3600)
        if k < 0.5 * par:
            out.append(Event(now, "below-par", mp, tracker.cell,
                             "kpm %.2f (exp gains/min, last hour) vs par %.2f for %s; purse %s z/h vs par %s"
                             % (k, par, a.get("cohort", "?"), w.get("zph_purse"), w.get("par_zph")),
                             "high" if k < 0.25 * par else None))
    town = (w.get("shares") or {}).get("town")
    if isinstance(town, (int, float)) and town > 0.4:
        out.append(Event(now, "town-share", mp, tracker.cell,
                         "town share %.0f%% of the last %.0f min (meter); shares %s" % (town * 100, mins, w.get("shares"))))
    return out


# ------------------------------------------------------------------ main loop
def default_log():
    return os.environ.get("AYLA_LOG") or os.path.join(C.ROOT, "openkore-ayla", "logs", "console_player_2.txt")


def load_state():
    try:
        with open(C.dpath("aylalog.state.json")) as f:
            return json.load(f)
    except Exception:
        return {}


def save_state(s):
    try:
        p = C.dpath("aylalog.state.json")
        tmp = p + ".tmp"
        with open(tmp, "w") as f:
            json.dump(s, f)
        os.replace(tmp, p)
    except OSError:
        pass


def process_lines(lines, tracker, utc_off):
    evs = []
    for raw in lines:
        t, rest = parse_ts(raw, utc_off)
        evs += tracker.feed(t, decode(rest))
    return evs


def backlog(path, tracker, hours, utc_off):
    """Read the whole file (map tracking needs the context), keep events of the last `hours` of log time.
    Returns (events, end offset, head, last log t)."""
    tail = Tail(path)
    evs = []
    while True:
        lines = tail.read()
        if not lines:
            break
        evs += process_lines(lines, tracker, utc_off)
    end = tracker.last_t or 0
    return [e for e in evs if e.t >= end - hours * 3600], tail


def run(a):
    out = sys.stdout
    path = a.log
    st = {} if a.dry_run else load_state()
    filer = Filer(a.dry_run, st, out)
    tracker = Tracker()
    tail = None
    now = C.now()
    fresh = st.get("path") == path and now - st.get("saved", 0) < a.backlog_h * 3600
    if fresh:
        tail = Tail(path, st.get("offset", 0), st.get("head", ""))
        tracker.map, tracker.cell = st.get("map", ""), st.get("cell", "")
        tracker.map_since, tracker.last_exp = st.get("map_since"), st.get("last_exp")
        tracker.last_t = st.get("last_t")
    else:
        if not os.path.exists(path):
            print("aylalog: log not found: %s" % path, file=sys.stderr)
            if a.once:
                return 2
        evs, tail = backlog(path, tracker, a.backlog_h, a.log_utc_offset)
        print("aylalog: backlog %gh of %s: %d problem events" % (a.backlog_h, path, len(evs)), file=out)
        if a.dry_run or a.summary:
            cnt = {}
            for e in evs:
                cnt[e.kind] = cnt.get(e.kind, 0) + 1
            print("aylalog: kinds in backlog: " + ", ".join("%s=%d" % kv for kv in sorted(cnt.items(), key=lambda kv: -kv[1])), file=out)
        n = filer.seed(evs, now, a.backlog_h)
        print("aylalog: backlog %s %d queue items" % ("would file" if a.dry_run else "filed", n), file=out)
    last_meter = 0
    locked = not (a.once or a.dry_run)   # single instance for the loop: legion/data/aylalog.lock
    if locked and not C.take_lock("aylalog.lock", 300):
        print("aylalog: another copy holds legion/data/aylalog.lock; not starting", file=sys.stderr)
        return
    while True:
        now = C.now()
        lines = tail.read()
        for e in process_lines(lines, tracker, a.log_utc_offset):
            filer.add(e)
        try:
            mt = os.path.getmtime(path)
        except OSError:
            mt = None
        log_fresh = mt is not None and now - mt < 120
        if log_fresh:
            for e in tracker.tick(now):
                filer.add(e)
        elif mt is None or now - mt > LOG_SILENT_S:
            filer.add(Event(now, "log-silent", tracker.map or "?", tracker.cell,
                            "console log not written for %s s" % ("?" if mt is None else int(now - mt)), "high"))
        if not a.no_meter and now - last_meter >= METER_EVERY:
            last_meter = now
            for e in meter_check(a.base, tracker, now):
                filer.add(e)
        filer.flush(now)
        if not a.dry_run:
            save_state(dict(filer.state(), path=path, offset=tail.offset, head=tail.head, saved=now, map=tracker.map,
                            cell=tracker.cell, map_since=tracker.map_since, last_exp=tracker.last_exp, last_t=tracker.last_t))
            if locked and not C.refresh_lock("aylalog.lock"):
                print("aylalog: lock taken by another copy; exiting", file=sys.stderr)
                return
            C.heartbeat("aylalog")
        if a.once:
            return 0
        time.sleep(a.interval)


def main(argv=None):
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--log", default=default_log())
    ap.add_argument("--interval", type=float, default=30)
    ap.add_argument("--backlog-h", type=float, default=6)
    ap.add_argument("--log-utc-offset", type=float, default=0.0, help="hours the log clock is ahead of UTC")
    ap.add_argument("--base", default=os.environ.get("LEGION_API", "http://127.0.0.1:8090"))
    ap.add_argument("--no-meter", action="store_true")
    ap.add_argument("--dry-run", action="store_true", help="print what would be filed; write nothing")
    ap.add_argument("--once", action="store_true", help="backlog (if due) and one live pass, then exit")
    ap.add_argument("--summary", action="store_true", help="print kind counts of the backlog")
    a = ap.parse_args(argv)
    return run(a)


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (BrokenPipeError, KeyboardInterrupt):
        sys.exit(0)
