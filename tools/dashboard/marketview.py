"""The Market tab (DB11): the facts of the market engine, the farmers, the prices, the crier's notices and the verified
game facts behind them. Every number is read from its source at serve time, never typed in here or in the page:

  1. Market engine   GET /api/market/engine, GET /api/market/feed (needs), the tuning flags (data/tuning.json)
  2. Farmers         data/farmers.json (roster), GET /api/market/orders, GET /api/economy 'kite' (or data/kite_counters.json)
  3. Prices          GET /api/market/engine 'prices' (the engine's price model output)
  4. Broadcast       data/market_notices.jsonl (the crier's rows), GET /api/market/engine 'broadcast.cadence'
  5. Game facts      the machine-readable block of docs/design/market-engine-facts.md (<!-- facts-json: DB11 -->)

When the engine is down (DB06, DB08) each section falls back to the file that the engine or a tool writes; when there
is no file either, the section says 'source not available: <path or api>'. Nothing is invented: a missing key is shown
as missing. Read-only.

Expected shapes (the engine branches confirm them; a different shape shows as 'missing' until they agree):
  /api/market/engine  {"t", "status": "on|off|partial|no_data", "missing": [], "caps": {name: value},
                       "allocation": [{"item_id", "item", "farmers", "maps": {map: n}}],
                       "prices": [{"item_id", "item", "price", "reference", "scarcity", "urgency", "history": [{"t", "price"}]}],
                       "broadcast": {"cadence": {name: value}}, "flags": {flag: bool} (optional),
                       "farmers": {"zeny_h": [{"band", "n", "zph_mean"}]}}
  /api/market/feed    {"t", "status", "missing": [], "short": [{"item_id", "item", "band", "demand", "listed", "price"}],
                       "glut": [...], "zeny_per_hour": [...]}  (docs/design/market-engine.md 4.1)
  /api/market/orders  {"t", "orders": [{"farmer", "char", "item_id", "item", "maps", "until"}],
                       "stats": {"accepted": n, "declined": n, "declines": {reason: n}}}
  data/market_engine.json, data/market_feed.json, data/farmer_orders.json: the same objects, as the engine writes them.
  data/farmers.json   {"accounts": [name or {"name", "phase"}], "since": "..."}
  data/market_notices.jsonl  one JSON object per notice (the crier appends them)
  data/kite_counters.json    {"Saved"/"saved", "Since"/"since", "Steps"/"steps": {bot: n}, "Deaths"/"deaths": {bot: n}}
"""
import datetime
import json
import os
import re
import time
from collections import Counter

HERE = os.path.dirname(os.path.abspath(__file__))
REPO = os.path.abspath(os.path.join(HERE, "..", ".."))
FACTS_MD = os.path.join(REPO, "docs", "design", "market-engine-facts.md")
FACTS_MD_REL = "docs/design/market-engine-facts.md"
FLAGS = ["market_engine_on", "farmer_orders_on", "market_broadcast_on", "guild_market_feed_on", "dynamic_price_on"]
ROUTES = {"engine": "/api/market/engine", "feed": "/api/market/feed", "orders": "/api/market/orders",
          "economy": "/api/economy"}
FILES = {"engine": "market_engine.json", "feed": "market_feed.json", "orders": "farmer_orders.json",
         "farmers": "farmers.json", "notices": "market_notices.jsonl", "tuning": "tuning.json",
         "kite": "kite_counters.json"}
FACTS_RE = re.compile(r"<!-- facts-json: DB11 -->\s*```json\s*\n(.*?)\n```", re.S)
DAY_S = 24 * 3600
PERCENT = 100


def read_json(path):
    try:
        with open(path, "r", encoding="utf-8") as f:
            return json.load(f)
    except (OSError, ValueError):
        return None


def read_jsonl(path, tail=None):
    """the JSON objects of a .jsonl file; tail (the dashboard's tail_jsonl) limits how much of a growing file is read"""
    if tail is not None:
        return tail(path)
    out = []
    try:
        with open(path, "r", encoding="utf-8") as f:
            for line in f:
                try:
                    o = json.loads(line)
                except ValueError:
                    continue
                if isinstance(o, dict):
                    out.append(o)
    except OSError:
        pass
    return out


def epoch(v):
    """epoch seconds of a number, an RFC 3339 time (Go's, with nanoseconds) or a YYYY-MM-DD date; None if unreadable"""
    if isinstance(v, bool):
        return None
    if isinstance(v, (int, float)):
        return float(v)
    if not isinstance(v, str) or not v or v.startswith("0001-"):
        return None
    s = re.sub(r"(\.\d{6})\d+", r"\1", v.replace("Z", "+00:00"))
    try:
        return datetime.datetime.fromisoformat(s).timestamp()
    except ValueError:
        pass
    try:
        return time.mktime(time.strptime(s.split("T")[0].split(" ")[0], "%Y-%m-%d"))
    except ValueError:
        return None


def num(v):
    """a number from a JSON value (bools and text are not numbers), else None"""
    if isinstance(v, bool) or not isinstance(v, (int, float)):
        return None
    return float(v)


def file_age(path, obj, now):
    """seconds since the data was made: its own 't' when it has one, else the file's time; None if neither"""
    t = epoch(obj.get("t")) if isinstance(obj, dict) else None
    if t is None and path is not None:
        try:
            t = os.path.getmtime(path)
        except OSError:
            return None
    return None if t is None else max(0, now - t)


def load(name, fetch, dp, now):
    """(object, source, age_s): the engine's API when it answers with an object, else the file the engine or a tool
    writes (DB08). None when neither exists."""
    route = ROUTES.get(name)
    if route and fetch is not None:
        obj = fetch(route)
        if isinstance(obj, dict):
            return obj, route, file_age(None, obj, now)
    if name in FILES:
        path = dp(FILES[name])
        obj = read_json(path)
        if isinstance(obj, dict):
            return obj, "data/" + FILES[name], file_age(path, obj, now)
    return None


def rows_of(obj, key):
    """the list of dict rows under key, or None when the source has no such list"""
    v = obj.get(key) if isinstance(obj, dict) else None
    if not isinstance(v, list):
        return None
    return [r for r in v if isinstance(r, dict)]


def section_status(rows_ok, missing):
    if not missing:
        return "ok"
    return "partial" if rows_ok else "source not available"


def na(what):
    return "source not available: " + what


# ------------------------------------------------------------------ 1. market engine
def flag_rows(tuning, engine):
    """each flag as the engine reports it, else as tuning.json has it; a flag neither has is 'not in tuning.json'"""
    eflags = engine.get("flags") if isinstance(engine, dict) else None
    out = []
    for f in FLAGS:
        if isinstance(eflags, dict) and f in eflags:
            src, val = "engine (flags)", eflags[f]
        else:
            src, val = "data/tuning.json", (tuning or {}).get(f)
        state = "on" if val is True else "off" if val is False else "not in tuning.json"
        out.append({"flag": f, "state": state, "source": src})
    return out


def rank_needs(rows):
    """ranked gaps by band: the biggest demand first within each band"""
    srt = sorted(rows, key=lambda r: (str(r.get("band", "")), -(num(r.get("demand")) or 0)))
    out, band, k = [], None, 0
    for r in srt:
        k = k + 1 if r.get("band") == band else 1
        band = r.get("band")
        out.append(dict(r, rank=k))
    return out


def section_engine(eng, feed, tuning):
    missing = []
    engine_obj, eng_src, eng_age = eng if eng else (None, None, None)
    feed_obj, feed_src, feed_age = feed if feed else (None, None, None)
    if engine_obj is None:
        missing.append(na("/api/market/engine, data/market_engine.json"))
    caps = engine_obj.get("caps") if engine_obj else None
    caps_rows = [{"name": k, "value": v} for k, v in caps.items()] if isinstance(caps, dict) else None
    if caps_rows is None:
        missing.append(na("/api/market/engine (caps)"))
    alloc = rows_of(engine_obj, "allocation")
    if alloc is None:
        missing.append(na("/api/market/engine (allocation)"))
    short = rows_of(feed_obj, "short")
    if short is None:
        missing.append(na("/api/market/feed (short), data/market_feed.json"))
    estatus = engine_obj.get("status") if engine_obj else None
    has = bool(caps_rows or alloc or short)
    return {"title": "Market engine", "status": "off" if estatus == "off" else section_status(has, missing),
            "engine_status": estatus, "source": eng_src, "age_s": eng_age,
            "feed_source": feed_src, "feed_age_s": feed_age,
            "flags": flag_rows(tuning, engine_obj), "caps": caps_rows or [],
            "allocation": alloc or [], "needs": rank_needs(short) if short else [],
            "missing": missing + ["engine missing: " + str(m) for m in (engine_obj or {}).get("missing", []) if m]}


# ------------------------------------------------------------------ 2. farmers
def kite_totals(kite_obj):
    """steps and deaths summed over the bots, from the file's own maps"""
    def tot(d):
        return sum(num(v) or 0 for v in d.values()) if isinstance(d, dict) else None
    steps = tot(kite_obj.get("Steps", kite_obj.get("steps")))
    deaths = tot(kite_obj.get("Deaths", kite_obj.get("deaths")))
    return steps, deaths


def section_farmers(fetch, dp, now, engine_obj):
    missing = []
    roster = load("farmers", None, dp, now)
    roster_rows = rows_of(roster[0], "accounts") if roster else None
    if roster is None:
        missing.append(na("data/farmers.json"))
    phases = []
    if roster_rows is not None and any("phase" in r for r in roster_rows):
        phases = [{"phase": p, "n": c} for p, c in sorted(Counter(str(r.get("phase", "?")) for r in roster_rows).items())]
    elif roster_rows is not None:
        missing.append("phase not recorded in data/farmers.json (accounts are plain names)")
    count = len(roster_rows) if roster_rows is not None else None
    if count is None and roster is not None:
        missing.append(na("data/farmers.json (accounts)"))

    orders = load("orders", fetch, dp, now)
    orders_obj = orders[0] if orders else None
    order_rows = rows_of(orders_obj, "orders") if orders else None
    if orders is None:
        missing.append(na("/api/market/orders, data/farmer_orders.json"))
    stats = orders_obj.get("stats") if orders_obj and isinstance(orders_obj.get("stats"), dict) else {}
    acc, dec = num(stats.get("accepted")), num(stats.get("declined"))
    rate = acc * PERCENT / (acc + dec) if acc is not None and dec is not None and acc + dec > 0 else None
    declines = sorted(((k, num(v) or 0) for k, v in (stats.get("declines") or {}).items()), key=lambda kv: -kv[1]) \
        if isinstance(stats.get("declines"), dict) else []

    # spread per item: the engine's allocation when it has one, else the orders grouped by item
    alloc = rows_of(engine_obj, "allocation")
    if alloc is not None:
        spread = [{"item_id": r.get("item_id"), "item": r.get("item", ""), "farmers": r.get("farmers"),
                   "maps": r.get("maps")} for r in alloc]
    elif order_rows is not None:
        grp = Counter((r.get("item_id"), str(r.get("item", ""))) for r in order_rows)
        spread = [{"item_id": i, "item": n, "farmers": c, "maps": None} for (i, n), c in grp.most_common()]
    else:
        spread = []
    if not spread:
        missing.append(na("/api/market/engine (allocation) or /api/market/orders"))

    kite = None
    econ = fetch("/api/economy") if fetch is not None else None
    if isinstance(econ, dict) and isinstance(econ.get("kite"), dict):
        k = econ["kite"]
        kite = {"steps": num(k.get("kite_steps")), "deaths": num(k.get("kite_deaths")),
                "since": k.get("counters_since"), "source": "/api/economy (kite)", "age_s": file_age(None, econ, now)}
    else:
        kpath = dp(FILES["kite"])
        kobj = read_json(kpath)
        if isinstance(kobj, dict):
            s, d = kite_totals(kobj)
            kite = {"steps": s, "deaths": d, "since": kobj.get("Since", kobj.get("since")),
                    "source": "data/" + FILES["kite"], "age_s": file_age(kpath, kobj, now)}
        else:
            missing.append(na("/api/economy (kite), data/kite_counters.json"))

    fblock = engine_obj.get("farmers") if engine_obj else None
    zeny = rows_of(fblock, "zeny_h") if isinstance(fblock, dict) else None
    if zeny is None:
        missing.append(na("/api/market/engine (farmers.zeny_h)"))

    has = bool(count or order_rows or spread)
    return {"title": "Farmers", "status": section_status(has, missing),
            "roster": {"count": count, "since": roster[0].get("since") if roster else None,
                       "source": roster[1] if roster else None, "age_s": roster[2] if roster else None,
                       "phases": phases},
            "orders": {"count": len(order_rows) if order_rows is not None else None,
                       "rows": order_rows or [], "source": orders[1] if orders else None,
                       "age_s": orders[2] if orders else None},
            "acceptance": {"rate_pct": rate, "accepted": acc, "declined": dec,
                           "source": orders[1] if orders else None},
            "declines": [{"reason": k, "n": v} for k, v in declines],
            "spread": spread, "kite": kite, "zeny_h": zeny or [], "missing": missing}


# ------------------------------------------------------------------ 3. prices
def section_prices(eng):
    engine_obj, src, age = eng if eng else (None, None, None)
    rows = rows_of(engine_obj, "prices")
    if rows is None:
        return {"title": "Prices", "status": "source not available", "rows": [], "source": src, "age_s": age,
                "missing": [na("/api/market/engine (prices: the engine's price model output)")]}
    out = []
    for r in rows:
        p, ref = num(r.get("price")), num(r.get("reference"))
        hist = [num(h.get("price")) for h in (r.get("history") or []) if isinstance(h, dict) and num(h.get("price")) is not None]
        out.append({"item_id": r.get("item_id"), "item": r.get("item", ""), "price": p, "reference": ref,
                    "vs_reference_pct": p * PERCENT / ref if p is not None and ref else None,
                    "scarcity": r.get("scarcity"), "urgency": r.get("urgency"),
                    "history_n": len(hist), "first": hist[0] if hist else None, "last": hist[-1] if hist else None})
    return {"title": "Prices", "status": "ok", "rows": out, "source": src, "age_s": age, "missing": []}


# ------------------------------------------------------------------ 4. broadcast
def section_broadcast(dp, now, eng, tuning, tail):
    missing = []
    engine_obj, esrc, _ = eng if eng else (None, None, None)
    path = dp(FILES["notices"])
    notices = read_jsonl(path, tail) if os.path.exists(path) else None
    if notices is None:
        missing.append(na("data/market_notices.jsonl"))
        notices = []
    else:
        notices = [{"t": n.get("t", n.get("created")), "text": n.get("msg", n.get("text", "")), "item": n.get("item"),
                    "sent": n.get("sent")} for n in notices]
    try:
        mtime = os.path.getmtime(path)
    except OSError:
        mtime = None
    bc = engine_obj.get("broadcast") if engine_obj and isinstance(engine_obj.get("broadcast"), dict) else {}
    cadence = bc.get("cadence") if isinstance(bc.get("cadence"), dict) else None
    if cadence is None:
        missing.append(na("/api/market/engine (broadcast.cadence)"))
    flag = [f for f in flag_rows(tuning, engine_obj) if f["flag"] == "market_broadcast_on"][0]
    missing.append("legion_notice rows in ro-db: not read by this tab (the jsonl and the engine's cadence are)")
    return {"title": "Broadcast", "status": section_status(bool(notices or cadence), missing),
            "notices": notices, "notices_source": "data/market_notices.jsonl" if mtime is not None else None,
            "notices_age_s": max(0, now - mtime) if mtime is not None else None,
            "sent_n": sum(1 for n in notices if n.get("sent") is True),
            "cadence": [{"name": k, "value": v} for k, v in cadence.items()] if cadence else [],
            "cadence_source": esrc if cadence else None, "flag": flag, "missing": missing}


# ------------------------------------------------------------------ 5. game facts
def parse_facts(text):
    m = FACTS_RE.search(text or "")
    if not m:
        return None
    try:
        return json.loads(m.group(1))
    except ValueError:
        return None


def derive(by):
    """the card chances at the server's rates, from the facts' own values: rate x (server rate / scale) / base"""
    base, scale, server = num(by.get("rate.base", {}).get("value")), num(by.get("rate.scale_base", {}).get("value")), \
        num(by.get("server.item_rate_card", {}).get("value"))
    out = []
    if not (base and scale and server):
        return out
    for key, label in (("creamy.drop.normal_rate", "Creamy"), ("creamy.drop.champion_rate", "Swift Creamy")):
        r = num(by.get(key, {}).get("value"))
        if not r:
            continue
        inputs = "%s, rate.base, rate.scale_base, server.item_rate_card" % key
        out.append({"id": "derived." + key + ".pct", "group": "Creamy card at the server rates", "label": label + " card chance per kill",
                    "value": round(r * server / scale / base * PERCENT, 4), "unit": "percent",
                    "source": "computed from " + inputs, "derived": True})
        out.append({"id": "derived." + key + ".kills", "group": "Creamy card at the server rates",
                    "label": label + " kills per card (average)", "value": round(base * scale / (r * server), 1),
                    "unit": "kills", "source": "computed from " + inputs, "derived": True})
    return out


def section_facts(now, facts_path):
    try:
        with open(facts_path, "r", encoding="utf-8") as f:
            text = f.read()
    except OSError:
        return {"title": "Game facts", "status": "source not available", "rows": [], "source": None, "age_s": None,
                "missing": [na(FACTS_MD_REL)]}
    block = parse_facts(text)
    if block is None or not isinstance(block.get("facts"), list):
        return {"title": "Game facts", "status": "source not available", "rows": [], "source": None, "age_s": None,
                "missing": [na(FACTS_MD_REL + " (no facts-json block)")]}
    checked = epoch(block.get("checked"))
    age = max(0, now - checked) if checked is not None else None
    rows = [dict(f, source=f.get("source", ""), age_s=age, derived=False) for f in block["facts"] if isinstance(f, dict)]
    by = {f.get("id"): f for f in rows}
    rows += derive(by)
    return {"title": "Game facts", "status": "ok", "rows": rows, "source": FACTS_MD_REL, "age_s": age,
            "checked": block.get("checked"), "missing": []}


# ------------------------------------------------------------------ the view
def view(fetch, dp, now=None, tail=None, facts_path=FACTS_MD):
    """the five sections, each with its source and age. fetch(route) gives the engine's JSON object or None (DB06);
    dp(name) is the data folder's path resolver; tail(path) is the dashboard's bounded .jsonl reader."""
    now = time.time() if now is None else now
    eng = load("engine", fetch, dp, now)
    feed = load("feed", fetch, dp, now)
    tuning = read_json(dp(FILES["tuning"]))
    engine_obj = eng[0] if eng else None
    return {"t": now, "flags": FLAGS,
            "sections": {"engine": section_engine(eng, feed, tuning if isinstance(tuning, dict) else None),
                         "farmers": section_farmers(fetch, dp, now, engine_obj),
                         "prices": section_prices(eng),
                         "broadcast": section_broadcast(dp, now, eng, tuning if isinstance(tuning, dict) else None, tail),
                         "facts": section_facts(now, facts_path)}}


def file_obj(name, dp):
    """the engine's file for an API route, as the offline stand-in serves it (None when there is no file)"""
    return read_json(dp(FILES[name]))
