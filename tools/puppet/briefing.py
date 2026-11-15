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
    """your own last thoughts (what you decided before). Engine: {"mind","thoughts":[...],"messages":[...],"next"}"""
    d = api.get(f"/api/minds/{mind}/thoughts?limit=4")
    items = d if isinstance(d, list) else dig(d, "thoughts", default=None)
    if items is None:
        items = rows_of(d)
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


def _prize(r, metric):
    """prize (gap to par x n) of one cohort row: engine shape {"zph": {"prize": ..}}, older guesses gap_x_n/prize"""
    v = r.get(metric)
    if isinstance(v, dict):
        return num(dig(v, "prize", "gap")) or 0
    return num(dig(r, "gap_x_n", "prize", "gap")) or 0


def cohort_summary(meter, limit=6):
    """cohorts by prize from /api/meter (the engine already sorts by zeny prize, then kill prize)"""
    rows = [r for r in rows_of(dig(meter, "cohorts", default=meter)) if isinstance(r, dict)
            and (num(r.get("n")) is None or num(r.get("n")) > 0)]  # empty cohorts (n=0) say nothing
    rows.sort(key=lambda r: (-_prize(r, "zph"), -_prize(r, "kpm")))
    return rows[:limit]


def cohort_line(c):
    """one short line per cohort: key n, kpm mean/par, zeny/h mean/par, prize, deaths/h, top time shares"""
    k, z = c.get("kpm"), c.get("zph")
    if not isinstance(k, dict) or not isinstance(z, dict):
        return compact(c, 220)
    sh = c.get("shares") or {}
    top = " ".join(f"{n[:5]} {round(v * 100)}%" for n, v in sorted(sh.items(), key=lambda x: -x[1])[:4])
    return (f"{c.get('key')} n={c.get('n')}: kpm {k.get('mean')}/{k.get('par')} par, "
            f"zeny/h {int(z.get('mean') or 0)}/{int(z.get('par') or 0)} par, prize {int(z.get('prize') or 0)}z, "
            f"deaths/h {c.get('deaths_h')}, time {top}")


def server_metrics(meter):
    """server totals of /api/meter: engine keys server.kpm / zph / deaths_h (older guesses: totals, *_per_*)"""
    t = dig(meter, "server", "totals", default={}) or {}
    out = {}
    for name, keys in (("kpm", ("kpm", "kills_per_min")), ("zph", ("zph", "zeny_per_hour")),
                       ("deaths_h", ("deaths_h", "deaths_per_hour"))):
        v = num(dig(t, *keys))
        if v is not None:
            out[name] = v
    return out


def group_metrics(bots_rows, guild_name):
    """mean credited kpm / zeny/h of the counted bots of one in-game guild name (meter rows carry the guild NAME)"""
    rows = [r for r in bots_rows if isinstance(r, dict) and guild_name
            and str(r.get("guild", "")).lower() == guild_name.lower() and r.get("counted", True)]
    if not rows:
        return {}
    n = len(rows)
    return {"n": n, "kpm": round(sum(num(r.get("kpm")) or 0 for r in rows) / n, 3),
            "zph": round(sum(num(r.get("zph")) or 0 for r in rows) / n),
            "below_par": sum(1 for r in rows if (num(r.get("gap_zph")) or 0) > 0)}


def finish(parts, api):
    text = "\n".join(p for p in parts if p)
    gaps = sorted(set(api.missing))
    if gaps:
        text += "\n(not available right now: " + ", ".join(gaps) + ")\n"
    if len(text) > MAX_BRIEFING_CHARS:
        text = text[:MAX_BRIEFING_CHARS] + "\n...[briefing cut]\n"
    return text


# ----------------------------------------------------------------- guild
STARVEIL = "human-guild"  # the engine's id of Ayla's guild Starveil (humanguildlead.go humanGuildID)


def rank_rows(rank):
    """rows of /api/guild-rank: engine {"rank":[{guild,name,place,score,wealth,...}],"history":{..}}"""
    rows = dig(rank, "rank", "guilds", "ranks", default=None)
    if isinstance(rows, list):
        return [r for r in rows if isinstance(r, dict)]
    rows = rows_of(rank)
    if not rows and isinstance(rank, dict):
        rows = [dict(v, guild=k) for k, v in rank.items() if isinstance(v, dict)]
    return rows


def place_of(r):
    return num(dig(r, "place", "rank")) or 999


def relation_lines(rel, gid):
    """engine {"relations":[{a,b,name_a,name_b,value,label,events,last_event}]}: lines for the pairs with gid"""
    rows = dig(rel, "relations", default=None)
    if rows is None and isinstance(rel, dict) and "relations" in rel:
        rows = []  # the engine sends null while no pair has had an event
    if not isinstance(rows, list):
        r = row_for(rel, gid)
        return compact(r if r is not None else rel, 600)
    out = []
    for r in rows:
        if not isinstance(r, dict) or gid not in (str(r.get("a", "")).lower(), str(r.get("b", "")).lower()):
            continue
        if str(r.get("a", "")).lower() == gid:
            other = r.get("name_b") or r.get("b")
        else:
            other = r.get("name_a") or r.get("a")
        line = f"{other}: {r.get('value')} ({r.get('label')})"
        if r.get("last_event"):
            line += f", last: {str(r['last_event'])[:80]}"
        out.append(line)
    return "\n".join(out) or "(all neutral, no events yet)"


def guild_briefing(api, gid, notes_dir, prev=None, prev_ts=None):
    snap = {"metrics": {}, "directions": []}
    parts = [f"Guild: {gid}. You speak for its leader (the leader character is always online)."]

    rank = api.get("/api/guild-rank")
    rrows = rank_rows(rank) if rank else []
    mine = next((r for r in rrows if str(dig(r, "guild", "id", default="")).lower() == gid), None) or row_for(rank, gid)
    gname = str((mine or {}).get("name") or "")

    g = row_for(api.get("/api/guilds"), gid) or {}
    if g:
        parts.append(section("Leader and roster", compact(g, 400)))
        if num(g.get("online")) is not None:
            snap["metrics"]["members_online"] = num(g["online"])

    if mine:
        for k, key in (("rank", ("place", "rank")), ("wealth", ("wealth",)), ("score", ("score",)),
                       ("guild_level", ("guild_level", "level")), ("members", ("members",))):
            if num(dig(mine, *key)) is not None:
                snap["metrics"][k] = num(dig(mine, *key))
        parts.append(section("Your rank and wealth", compact({k: v for k, v in mine.items() if k != "t"}, 400)))
    rivals = [r for r in rrows if str(dig(r, "guild", "id", default="")).lower() != gid]
    rivals.sort(key=place_of)
    if rivals:
        parts.append(section("Rival guilds (place, score, wealth)", "\n".join(
            f"#{int(place_of(r))} {r.get('name') or r.get('guild')}: score {r.get('score')}, wealth {r.get('wealth')}, "
            f"members {r.get('members')}" for r in rivals[:10])))
    rel = api.get("/api/guild-relations")
    if rel:
        parts.append(section("Relations with other guilds (-1 rival .. +1 ally)", relation_lines(rel, gid)))

    dirs = api.get(f"/api/guild-direction/{gid}")
    if dirs is not None:
        ds = dig(dirs, "active", "directions", default=dirs)
        parts.append(section("Active directions (max 3; yours last 24 h, then autopilot takes over)", compact(ds, 900)))
        for d in (ds if isinstance(ds, list) else []):
            if isinstance(d, dict):
                snap["directions"].append(f"{d.get('id')}:{d.get('kind')}:{d.get('item') or d.get('map') or ''}:{d.get('set_by', '')}")

    duty = api.get(f"/api/guild-duty?guild={gid}")
    if duty is not None:
        members = dig(duty, "members", default=None)
        rows = members if isinstance(members, list) else rows_of(duty)
        brief = [{k: r.get(k) for k in ("char", "name", "standing", "short_days", "exempt", "today_duty_min",
                                         "today_owed_min", "last_plan") if r.get(k) not in (None, "")}
                 for r in rows if isinstance(r, dict) and r.get("standing") not in ("good", "exempt")]
        counts = (dig(duty, "standings", default={}) or {}).get(gid)
        if not isinstance(counts, dict):
            counts = {}
            for r in rows:
                if isinstance(r, dict):
                    counts[r.get("standing")] = counts.get(r.get("standing"), 0) + 1
        parts.append(section("Member duty (standing counts; members not in good standing)",
                             compact(counts, 200) + "\n" + compact(brief, 900, 12)))
        for st in ("good", "behind", "warned", "kicked"):
            snap["metrics"][f"duty_{st}"] = num(counts.get(st)) or 0

    meter = api.get("/api/meter")
    if meter is not None:
        bots = api.get("/api/meter/bots?limit=2000")
        gm = group_metrics(rows_of(dig(bots, "bots", default=bots)), gname)
        if gm:
            snap["metrics"]["kpm"], snap["metrics"]["zph"] = gm["kpm"], gm["zph"]
            parts.append(section("Your guild's two metrics (last hour, members counted)",
                                 f"{gm['n']} members: kills/min {gm['kpm']}, zeny/h {gm['zph']}, "
                                 f"below zeny par: {gm['below_par']}"))
        parts.append(section("Biggest gaps to par on the server (cohorts)",
                             "\n".join(cohort_line(c) for c in cohort_summary(meter, 4))))

    stock = row_for(api.get("/api/guild-stock"), gid)
    if stock:
        parts.append(section("Guild stock and needs", compact({k: stock.get(k) for k in (
            "members_online", "members_short_of_potions", "stock", "needs") if k in stock}, 700)))

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
        counts = dig(ex, "counts", default={}) or {}
        parts.append(section("Exchange (relationship pricing: on/off, counters, recent hand-overs)",
                             compact({"on": ex.get("on"), "kin_discount_pct": ex.get("kin_discount_pct"),
                                      "counts": counts, "recent": (ex.get("recent") or [])[:5]}, 800)))
        for k, v in counts.items():
            if str(k).startswith("done:") and num(v) is not None:
                snap["metrics"]["exchange_" + str(k)[5:].strip().replace(" ", "_")] = num(v)
    pots = api.get("/api/potions")
    if pots:
        parts.append(section("Potions", compact(pots, 700)))
    meter = api.get("/api/meter")
    if meter is not None:
        parts.append(section("Zeny per hour by cohort (biggest prizes)",
                             "\n".join(cohort_line(c) for c in cohort_summary(meter, 4))))
        snap["metrics"].update(server_metrics(meter))
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
        parts.append(section("Cohorts furthest below par (class line|level band|solo/party/vendor)",
                             "\n".join(cohort_line(c) for c in cohort_summary(meter, 8))))
        snap["metrics"].update(server_metrics(meter))
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
    """GET /api/chat-jobs without peek: this call counts as the chat agent's poll (the engine waits for its lines)"""
    snap = {"metrics": {}}
    parts = [f"Role: chat director. Trigger: {trigger or 'schedule'}."]
    jobs = api.get("/api/chat-jobs")
    jl = jobs if isinstance(jobs, list) else dig(jobs, "jobs", default=None)
    if jl is None:
        jl = rows_of(jobs)
    if jobs is not None:
        snap["metrics"]["queued_jobs"] = len(jl)
        slim = [{k: j.get(k) for k in ("id", "kind", "channel", "to", "speaker", "hearers", "facts", "template_lines",
                                       "template_line", "deadline") if j.get(k) not in (None, "", [])}
                for j in jl if isinstance(j, dict)]
        parts.append(section(f"Queued chat jobs ({len(jl)}): answer each with `chat-jobs answer` before its deadline",
                             "\n".join(compact(j, 520) for j in slim[:8])))
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
        ps = dig(neo, "party_summary", default={}) or {}
        ss = dig(neo, "starveil_summary", default={}) or {}
        rows = [{k: r.get(k) for k in ("name", "class", "lv", "map", "bucket", "kpm", "par_kpm", "zph", "par_zph",
                                       "deaths_h", "counted")} for r in (dig(neo, "party", default=[]) or [])
                if isinstance(r, dict)]
        if ps or ss or rows:
            body = (compact(neo.get("ayla") or {}, 500) + "\nparty: " + compact(ps, 300) + "\n" + compact(rows, 900, 8)
                    + "\nStarveil: " + compact(ss, 300))
        else:
            body = compact(neo, 1500, 10)  # older or unknown shape
        aw10 = ((neo.get("ayla") or {}).get("windows") or {}).get("10m", {}) if isinstance(neo, dict) else {}
        sh = aw10.get("shares") or {}
        lazy = sum(float(sh.get(k) or 0) for k in ("town", "idle", "waiting"))
        amap = (neo.get("ayla") or {}).get("map", "?") if isinstance(neo, dict) else "?"
        if lazy >= 0.5:
            parts.append(section("IDLE ALERT (rule: Ayla has zero downtime)",
                                 f"Ayla spent {int(lazy * 100)}% of the last 10 min in town/idle/waiting (now on {amap}; shares {compact(sh, 200)}). "
                                 "Your plan is not being carried out. Act now: say the plan in party chat and send the party to the plan map "
                                 "(`ayla.sh rally <town> <map>` or `ayla.sh map <map>`), or pick a map that fits and farm. "
                                 "Do not answer 'plan unchanged' while she stands in town."))
        parts.append(section("You, your party and Starveil against comparable bots (par = p75, last hour)", body))
        for name, d in (("party", ps), ("starveil", ss)):
            for k in ("ratio_kpm", "ratio_zph", "above_par_kpm", "above_par_zph"):
                if isinstance(d, dict) and num(d.get(k)) is not None:
                    snap["metrics"][f"{name}_{k}"] = num(d[k])
        aw = (((neo.get("ayla") or {}).get("windows") or {}).get("1h") or {}) if isinstance(neo, dict) else {}
        if num(aw.get("zph_purse")) is not None:
            snap["metrics"]["ayla_zph_purse"] = num(aw["zph_purse"])
    pt = api.get("/api/parties")
    if isinstance(pt, list):
        mine = [p for p in pt if "ayla" in json.dumps(p, default=str).lower()]
        if mine:
            parts.append(section("Party", compact(mine[0], 900, 10)))
    gd = api.get(f"/api/guild-direction/{STARVEIL}")
    if gd is not None:
        parts.append(section("Starveil directions (set: guild-direction-set starveil <kind> ...)",
                             compact(dig(gd, "active", default=gd), 600)))
    rank = api.get("/api/guild-rank")
    r = next((x for x in rank_rows(rank) if str(x.get("guild", "")).lower() in (STARVEIL, "starveil")), None) if rank else None
    if r:
        parts.append(section("Starveil rank and wealth", compact({k: v for k, v in r.items() if k != "t"}, 300)))
        if num(r.get("place")) is not None:
            snap["metrics"]["starveil_place"] = num(r["place"])
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
