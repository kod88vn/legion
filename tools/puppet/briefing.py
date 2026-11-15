"""Briefing assemblers for the puppet masters.

Each assembler returns (text, snapshot). `text` is the short briefing handed to the one-shot agent; `snapshot` is a
flat dict of numbers/strings the dispatcher stores after the call, so the next briefing can show what changed since
that role last acted (the feedback on its earlier decisions).

Everything here reads the engine API (http://127.0.0.1:8090) and tolerates missing endpoints (older engine): a
missing source is simply left out, and the briefing says so in one line. Response shapes of the new endpoints
(meter, guild-direction/duty/rank/relations, exchange, chat-jobs) are read defensively, never by fixed schema.
Standard library only.
"""
import json
import os
import time
import urllib.request

API = os.environ.get("LEGION_API", "http://127.0.0.1:8090")
MAX_BRIEFING_CHARS = 6000


class Api:
    """GET helper with a per-instance cache. A failed GET returns None and is remembered (no retry in one briefing)."""

    def __init__(self, base=None, timeout=6, fetch=None):
        self.base = base or API
        self.timeout = timeout
        self._fetch = fetch  # tests inject fetch(path) -> data (or raise)
        self.cache = {}
        self.missing = []

    def get(self, path):
        if path in self.cache:
            return self.cache[path]
        try:
            if self._fetch:
                data = self._fetch(path)
            else:
                with urllib.request.urlopen(self.base + path, timeout=self.timeout) as r:
                    data = json.loads(r.read() or b"null")
        except Exception:
            data = None
            self.missing.append(path.split("?")[0])
        self.cache[path] = data
        return data

    def alive(self):
        return self.get("/api/stats") is not None


# ----------------------------------------------------------------- generic helpers
def dig(obj, *keys, default=None):
    """first present key among keys in a dict (default if obj is not a dict)"""
    if isinstance(obj, dict):
        for k in keys:
            if k in obj and obj[k] is not None:
                return obj[k]
    return default


def rows_of(data):
    """a list of rows from either a list or a dict wrapping one ({"guilds": [...]}, {"bots": [...]}, ...)"""
    if isinstance(data, list):
        return data
    if isinstance(data, dict):
        for v in data.values():
            if isinstance(v, list) and v and isinstance(v[0], dict):
                return v
    return []


def row_for(data, gid):
    """the row of guild `gid` in a dict keyed by guild id, or a list of rows with guild/id/name"""
    gid = gid.lower()
    if isinstance(data, dict):
        for k, v in data.items():
            if str(k).lower() == gid and isinstance(v, dict):
                return v
        for k in ("guilds", "ranks", "rows", "items"):
            if isinstance(data.get(k), (list, dict)):
                r = row_for(data[k], gid)
                if r is not None:
                    return r
    if isinstance(data, list):
        for r in data:
            if isinstance(r, dict) and any(str(r.get(k, "")).lower() == gid for k in ("guild", "id", "name", "guild_id")):
                return r
    return None


def num(x):
    if isinstance(x, bool):
        return None
    try:
        return float(x)
    except (TypeError, ValueError):
        return None


def compact(obj, max_chars=900, max_items=8):
    """short JSON rendering: lists cut to max_items, long strings cut, result cut to max_chars"""
    def trim(o):
        if isinstance(o, list):
            r = [trim(x) for x in o[:max_items]]
            if len(o) > max_items:
                r.append(f"... +{len(o) - max_items} more")
            return r
        if isinstance(o, dict):
            items = list(o.items())
            r = {k: trim(v) for k, v in items[:max_items * 2]}
            if len(items) > max_items * 2:
                r["..."] = f"+{len(items) - max_items * 2} more"
            return r
        if isinstance(o, str) and len(o) > 140:
            return o[:140] + "..."
        if isinstance(o, float):
            return round(o, 2)
        return o
    s = json.dumps(trim(obj), ensure_ascii=False, separators=(",", ":"), default=str)
    return s if len(s) <= max_chars else s[:max_chars] + "...[cut]"


def section(title, body):
    return f"## {title}\n{body}\n" if body else ""


def read_notes(notes_dir, role):
    try:
        with open(os.path.join(notes_dir, f"{role}.md"), encoding="utf-8") as f:
            t = f.read().strip()
            return t[-2000:] if len(t) > 2000 else t
    except OSError:
        return ""


def diff_lines(prev, cur):
    """feedback lines 'metric: old -> new (+d)' for the numeric keys both snapshots have"""
    out = []
    for k, v in cur.items():
        a, b = num(prev.get(k)), num(v)
        if a is None or b is None:
            continue
        d = b - a
        out.append(f"{k}: {a:g} -> {b:g} ({'+' if d >= 0 else ''}{d:g})")
    return out


def feedback(prev, cur, since_ts, extra=None):
    if not prev:
        return "First call for this role (no earlier snapshot)."
    mins = int((time.time() - since_ts) / 60) if since_ts else 0
    lines = diff_lines(prev.get("metrics", {}), cur.get("metrics", {}))
    body = "\n".join("- " + l for l in lines) if lines else "- (no comparable numbers yet)"
    if extra:
        body += "\n" + "\n".join("- " + e for e in extra)
    return f"Since your last call ({mins} min ago):\n{body}"


def bus_lines(api, mind):
    """your own last thoughts (what you decided before)"""
    d = api.get(f"/api/minds/{mind}/thoughts?limit=4")
    items = d if isinstance(d, list) else rows_of(d)
    out = []
    for t in (items or [])[-4:]:
        if isinstance(t, dict):
            out.append(f"[{str(t.get('t', ''))[11:16]}] {str(t.get('text', ''))[:200]}")
    return "\n".join(out)


def latest_thought(api, mind):
    d = api.get("/api/minds")
    for m in (d if isinstance(d, list) else rows_of(d)):
        if isinstance(m, dict) and m.get("name") == mind:
            return (m.get("last_thought") or {}).get("text", "")
    return ""


def cohort_summary(meter, limit=6):
    """worst cohorts by gap to par x n from /api/meter, shape-tolerant"""
    rows = rows_of(dig(meter, "cohorts", default=meter))
    scored = []
    for r in rows:
        if isinstance(r, dict):
            gap = num(dig(r, "gap_x_n", "prize", "gap"))
            scored.append((gap if gap is not None else 0, r))
    scored.sort(key=lambda x: -x[0])
    return [r for _, r in scored[:limit]]


def finish(parts, api):
    text = "\n".join(p for p in parts if p)
    gaps = sorted(set(api.missing))
    if gaps:
        text += "\n(not available right now: " + ", ".join(gaps) + ")\n"
    if len(text) > MAX_BRIEFING_CHARS:
        text = text[:MAX_BRIEFING_CHARS] + "\n...[briefing cut]\n"
    return text


# ----------------------------------------------------------------- guild
def guild_briefing(api, gid, notes_dir, prev=None, prev_ts=None):
    snap = {"metrics": {}, "directions": []}
    parts = [f"Guild: {gid}. You speak for its leader (the leader character is always online)."]

    g = row_for(api.get("/api/guilds"), gid) or {}
    if g:
        parts.append(section("Roster", compact(g, 400)))
        if num(g.get("online")) is not None:
            snap["metrics"]["members_online"] = num(g["online"])

    rank = api.get("/api/guild-rank")
    mine = row_for(rank, gid)
    if mine:
        for k in ("rank", "wealth", "score", "level"):
            if num(mine.get(k)) is not None:
                snap["metrics"][k] = num(mine[k])
        parts.append(section("Your rank and wealth", compact(mine, 400)))
    rrows = rows_of(dig(rank, "guilds", "ranks", default=rank)) if rank else []
    if not rrows and isinstance(rank, dict):
        rrows = [dict(v, guild=k) for k, v in rank.items() if isinstance(v, dict)]
    rivals = [r for r in rrows if str(dig(r, "guild", "id", "name", default="")).lower() != gid]
    rivals.sort(key=lambda r: num(r.get("rank")) or 999)
    if rivals:
        parts.append(section("Rival guilds (rank, wealth)", "\n".join(compact(r, 200) for r in rivals[:10])))
    rel = api.get("/api/guild-relations")
    if rel:
        r = row_for(rel, gid)
        parts.append(section("Relations with other guilds (-1 rival .. +1 ally)", compact(r if r is not None else rel, 600)))

    dirs = api.get(f"/api/guild-direction/{gid}")
    if dirs is not None:
        ds = dig(dirs, "directions", default=dirs)
        parts.append(section("Active directions (max 3; yours last 24 h, then autopilot takes over)", compact(ds, 900)))
        for d in (ds if isinstance(ds, list) else []):
            if isinstance(d, dict):
                snap["directions"].append(f"{d.get('id')}:{d.get('kind')}:{d.get('item') or d.get('map') or ''}:{d.get('set_by', '')}")

    duty = api.get(f"/api/guild-duty?guild={gid}")
    if duty is not None:
        parts.append(section("Member duty (minutes/day, standing)", compact(duty, 1100, 12)))
        rows = rows_of(duty)
        for st in ("good", "behind", "warned", "kicked"):
            snap["metrics"][f"duty_{st}"] = sum(1 for r in rows if isinstance(r, dict) and r.get("standing") == st)

    meter = api.get("/api/meter")
    if meter is not None:
        rows = [r for r in rows_of(dig(meter, "guilds", default=[])) if str(dig(r, "guild", "id", default="")).lower() == gid]
        if rows:
            for k in ("kills_per_min", "zeny_per_hour"):
                if num(rows[0].get(k)) is not None:
                    snap["metrics"][k] = num(rows[0][k])
            parts.append(section("Your guild's two metrics", compact(rows[0], 400)))
        parts.append(section("Biggest gaps to par on the server (cohorts)",
                             "\n".join(compact(c, 220) for c in cohort_summary(meter, 4))))

    stock = api.get(f"/api/guilds/{gid}")
    if stock:
        parts.append(section("Guild stock and plan", compact(stock, 700)))

    bt = latest_thought(api, "build")
    if bt:
        parts.append(section("Build master's latest note", bt[:300]))

    parts.append(section("Your last decisions", bus_lines(api, gid)))
    extra = []
    if prev and prev.get("directions"):
        now = set(snap["directions"])
        alive = sum(1 for d in prev["directions"] if d in now)
        extra.append(f"directions seen last call: {len(prev['directions'])}, still active: {alive}")
    parts.append(section("Feedback", feedback(prev, snap, prev_ts, extra)))
    parts.append(section("Your notes from earlier calls", read_notes(notes_dir, gid) or "(none yet)"))
    return finish(parts, api), snap


# ----------------------------------------------------------------- market
def market_briefing(api, notes_dir, prev=None, prev_ts=None):
    snap = {"metrics": {}}
    parts = ["Role: market master (Prontera south gate market)."]
    eco = api.get("/api/economy")
    if eco:
        parts.append(section("Economy", compact(eco, 1400, 10)))
        for k in ("trades", "total_trades", "zeny_traded", "open_wants", "open_offers"):
            if num(dig(eco, k)) is not None:
                snap["metrics"][k] = num(eco[k])
    mk = api.get("/api/market")
    if isinstance(mk, list):
        opened = [m for m in mk if isinstance(m, dict) and m.get("open")]
        snap["metrics"]["shops_open"] = len(opened)
        snap["metrics"]["vendors_online"] = len(mk)
        parts.append(section("Shops", f"{len(opened)} open / {len(mk)} vendors online. Sample: " + compact(opened[:5], 700, 5)))
    ex = api.get("/api/exchange")
    if ex is not None:
        parts.append(section("Exchange (relationship pricing counters, recent)", compact(ex, 800)))
        for k in ("free", "discounted", "market"):
            v = dig(ex, k, f"{k}_count")
            if num(v) is not None:
                snap["metrics"][f"exchange_{k}"] = num(v)
    pots = api.get("/api/potions")
    if pots:
        parts.append(section("Potions", compact(pots, 700)))
    meter = api.get("/api/meter")
    if meter is not None:
        parts.append(section("Zeny per hour by cohort (worst gaps)",
                             "\n".join(compact(c, 220) for c in cohort_summary(meter, 4))))
        t = dig(meter, "totals", "server", default={})
        for k in ("zeny_per_hour", "kills_per_min"):
            if num(dig(t, k)) is not None:
                snap["metrics"][k] = num(t[k])
    parts.append(section("Your last decisions", bus_lines(api, "market")))
    parts.append(section("Feedback", feedback(prev, snap, prev_ts)))
    parts.append(section("Your notes from earlier calls", read_notes(notes_dir, "market") or "(none yet)"))
    return finish(parts, api), snap


# ----------------------------------------------------------------- build / class master
def build_briefing(api, notes_dir, prev=None, prev_ts=None):
    snap = {"metrics": {}}
    parts = ["Role: build and class master (called on demand)."]
    meter = api.get("/api/meter")
    if meter is not None:
        parts.append(section("Cohorts furthest below par (class line x level band x solo/party)",
                             "\n".join(compact(c, 300) for c in cohort_summary(meter, 8))))
        t = dig(meter, "totals", "server", default={})
        for k in ("zeny_per_hour", "kills_per_min", "deaths_per_hour"):
            if num(dig(t, k)) is not None:
                snap["metrics"][k] = num(t[k])
    else:
        parts.append("(meter endpoint not available on this engine: judge from the server state only)")
    st = api.get("/api/stats")
    if st:
        parts.append(section("Server", compact(dig(st, "states", default=st), 500)))
    parts.append(section("Your last decisions", bus_lines(api, "build")))
    parts.append(section("Feedback", feedback(prev, snap, prev_ts)))
    parts.append(section("Your notes from earlier calls", read_notes(notes_dir, "build") or "(none yet)"))
    return finish(parts, api), snap


# ----------------------------------------------------------------- chat
def chat_briefing(api, notes_dir, prev=None, prev_ts=None, trigger=""):
    snap = {"metrics": {}}
    parts = [f"Role: chat director. Trigger: {trigger or 'schedule'}."]
    jobs = api.get("/api/chat-jobs")
    jl = jobs if isinstance(jobs, list) else rows_of(jobs)
    if jobs is not None:
        snap["metrics"]["queued_jobs"] = len(jl)
        parts.append(section(f"Queued chat jobs ({len(jl)}): answer each with `chat-jobs answer`",
                             "\n".join(compact(j, 420) for j in jl[:8])))
    h = api.get("/api/humans")
    if h:
        parts.append(section("Humans online", compact(h, 500)))
    parts.append("Read new player lines with: inbox --role chat\n")
    parts.append(section("Your last decisions", bus_lines(api, "chat")))
    parts.append(section("Your notes from earlier calls", read_notes(notes_dir, "chat") or "(none yet)"))
    return finish(parts, api), snap


# ----------------------------------------------------------------- Ayla
def ayla_briefing(api, notes_dir, prev=None, prev_ts=None, trigger=""):
    snap = {"metrics": {}}
    parts = [f"Role: Ayla's mind (leader of her party and of guild Starveil). Trigger: {trigger or 'schedule'}."]
    neo = api.get("/api/meter/neo")
    if neo is not None:
        parts.append(section("Your party and Starveil against comparable bots (par = p75)", compact(neo, 1500, 10)))
        for k in ("kills_per_min", "zeny_per_hour", "deaths_per_hour"):
            v = dig(neo, k, f"party_{k}")
            if num(v) is not None:
                snap["metrics"][k] = num(v)
    pt = api.get("/api/parties")
    if isinstance(pt, list):
        mine = [p for p in pt if "ayla" in json.dumps(p, default=str).lower()]
        if mine:
            parts.append(section("Party", compact(mine[0], 900, 10)))
    gd = api.get("/api/guild-direction/starveil")
    if gd is not None:
        parts.append(section("Starveil directions", compact(gd, 600)))
    r = row_for(api.get("/api/guild-rank"), "starveil")
    if r:
        parts.append(section("Starveil rank and wealth", compact(r, 300)))
    parts.append(section("Your last decisions", bus_lines(api, "ayla")))
    parts.append(section("Feedback", feedback(prev, snap, prev_ts)))
    parts.append(section("Your notes from earlier calls", read_notes(notes_dir, "ayla") or "(none yet)"))
    return finish(parts, api), snap


def assemble(role, api, notes_dir, prev=None, prev_ts=None, trigger=""):
    if role.startswith("guild"):
        return guild_briefing(api, role, notes_dir, prev, prev_ts)
    if role == "market":
        return market_briefing(api, notes_dir, prev, prev_ts)
    if role == "build":
        return build_briefing(api, notes_dir, prev, prev_ts)
    if role == "chat":
        return chat_briefing(api, notes_dir, prev, prev_ts, trigger)
    if role == "ayla":
        return ayla_briefing(api, notes_dir, prev, prev_ts, trigger)
    raise ValueError(f"unknown role {role}")
