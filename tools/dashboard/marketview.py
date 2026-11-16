"""The Market tab (DB11): the facts of the market engine, the metrics, the farmers, the prices, the crier's notices and the
verified game facts behind them. Every number is read from its source at serve time; nothing is typed in here or in the
page. Read-only.

Sources, each checked against the real encoder (contract shapes: tests/dashboard/fixtures, drift test in test_market.py):
  engine    GET /api/market/engine (legion/marketengine.go registerMarketEngine). Answers "status" ok | no_data | off,
            "runs", "last_run", "farmers", "caps", "ordered", "unassigned", "distribution" {by_item, by_map,
            max_item_share}, "needs" [MEGap], "orders" [MEOrder], "metrics" (legion/marketmetrics.go). When the flag is off:
            {"status": "off", "flag", "note", "metrics"}. There is no file for it: the engine writes only the feed.
  metrics   the "metrics" block of the engine answer (coverage, price_stability, farmers, kite, zeny_per_hour).
  feed      GET /api/market/feed, else data/market_feed.json (legion/marketengine.go writeMarketFeed): status, missing,
            short, glut, zeny_per_hour.
  farmers   data/farmers.json (tools/system/farmerpick.py build_doc: schema, since, target, accounts, detail, vendor_adds);
            data/farmer_orders.json (farmers.go: t, since, orders, stats); the kite counters.
  kite      GET /api/economy "kite" (legion/economy.go), else data/kite_counters.json and data/kite_counters_core.json
            (legion/kite.go kiteSaved: since, saved, steps, deaths).
  prices    the engine's "prices" list when one exists. None on main yet: legion/pricedyn.go has no endpoint.
  broadcast data/market_notices.jsonl (tools/system/crier.py rows: t, at, kind, item, place, text, mode live|dry) and the
            cadence rules from crier.rules() (read-only, published by crier.py).
  facts     the machine-readable block of docs/design/market-engine-facts.md (<!-- facts-json: DB11 -->).

A missing source says "source not available: <path or api>". Nothing is invented.
"""
import datetime
import json
import os
import re
import sys
import time

HERE = os.path.dirname(os.path.abspath(__file__))
REPO = os.path.abspath(os.path.join(HERE, "..", ".."))
TOOLS_SYSTEM = os.path.join(REPO, "tools", "system")
FACTS_MD = os.path.join(REPO, "docs", "design", "market-engine-facts.md")
FACTS_MD_REL = "docs/design/market-engine-facts.md"
ENGINE_ROUTE = "/api/market/engine"
FEED_ROUTE = "/api/market/feed"
ECON_ROUTE = "/api/economy"
FLAGS = ["market_engine_on", "farmer_orders_on", "market_broadcast_on", "guild_market_feed_on", "dynamic_price_on"]
FILES = {"feed": "market_feed.json", "orders": "farmer_orders.json", "farmers": "farmers.json",
         "notices": "market_notices.jsonl", "tuning": "tuning.json", "kite": "kite_counters.json",
         "kite_core": "kite_counters_core.json"}
METRIC_BLOCKS = ["coverage", "price_stability", "farmers", "kite", "zeny_per_hour"]
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
    """the JSON objects of a .jsonl file; tail (the dashboard's tail_jsonl) bounds how much of a growing file is read"""
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
    """epoch seconds of a number, a Go RFC 3339 time (nanoseconds, offset) or a YYYY-MM-DD date; None if unreadable"""
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


def age_of(t, now):
    e = epoch(t)
    return None if e is None else max(0, now - e)


def num(v):
    """a number from a JSON value (bools and text are not numbers), else None"""
    if isinstance(v, bool) or not isinstance(v, (int, float)):
        return None
    return float(v)


def rows_of(obj, key):
    """the list of dict rows under key, or None when the source has no such list"""
    v = obj.get(key) if isinstance(obj, dict) else None
    if not isinstance(v, list):
        return None
    return [r for r in v if isinstance(r, dict)]


def na(what):
    return "source not available: " + what


def file_load(name, dp, now):
    """(object, source, age_s) of a data file the engine or a tool writes (DB08); None when there is none"""
    path = dp(FILES[name])
    obj = read_json(path)
    if not isinstance(obj, dict):
        return None
    t = epoch(obj.get("t"))
    if t is None:
        try:
            t = os.path.getmtime(path)
        except OSError:
            return None
    return obj, "data/" + FILES[name], max(0, now - t)


def load_feed(fetch, dp, now):
    """the feed from the engine when it answers, else its file"""
    obj = fetch(FEED_ROUTE) if fetch else None
    if isinstance(obj, dict):
        return obj, FEED_ROUTE, age_of(obj.get("t"), now)
    return file_load("feed", dp, now)


def section_status(rows_ok, missing):
    if not missing:
        return "ok"
    return "partial" if rows_ok else "source not available"


def flag_rows(tuning):
    """each flag from data/tuning.json (the engine's answer carries no flags); a missing key says so"""
    out = []
    for f in FLAGS:
        val = (tuning or {}).get(f)
        state = "on" if val is True else "off" if val is False else "not in tuning.json"
        out.append({"flag": f, "state": state, "source": "data/tuning.json"})
    return out


def rank_rows(rows):
    """the engine's order is kept; each row gets its place in the list (1 is the first, most short)"""
    return [dict(r, rank=i + 1) for i, r in enumerate(rows)]


# ------------------------------------------------------------------ engine
def allocation_rows(dist, orders, names):
    """farmers per item (distribution.by_item) with the maps they work (from the orders); None when neither exists"""
    by_item = dist.get("by_item") if isinstance(dist, dict) else None
    if by_item is None and orders is None:
        return None
    maps = {}
    for o in orders or []:
        maps.setdefault(str(o.get("item_id")), set()).add(str(o.get("map", "")))
    keys = list(by_item.keys()) if isinstance(by_item, dict) else list(maps.keys())
    out = []
    for k in keys:
        out.append({"item_id": k, "item": names.get(k, ""),
                    "count": by_item.get(k) if isinstance(by_item, dict) else len([o for o in orders if str(o.get("item_id")) == k]),
                    "maps": sorted(maps.get(k, []))})
    return out


def section_engine(engine, feed, tuning, now):
    missing = []
    off = engine is not None and engine.get("status") == "off"
    if engine is None:
        missing.append(na(ENGINE_ROUTE + " (the engine writes no file for it; the feed is data/market_feed.json)"))
    needs = rows_of(engine, "needs") if engine is not None else None
    if engine is not None and not off and needs is None:
        missing.append(na(ENGINE_ROUTE + " (needs)"))
    caps = engine.get("caps") if engine is not None and isinstance(engine.get("caps"), dict) else None
    if engine is not None and not off and caps is None:
        missing.append(na(ENGINE_ROUTE + " (caps)"))
    dist = engine.get("distribution") if engine is not None and isinstance(engine.get("distribution"), dict) else None
    orders = rows_of(engine, "orders") if engine is not None else None
    names = {str(r.get("item_id")): r.get("item", "") for r in (needs or []) + (orders or []) if "item_id" in r}
    alloc = allocation_rows(dist, orders, names)
    if engine is not None and not off and alloc is None:
        missing.append(na(ENGINE_ROUTE + " (distribution)"))
    feed_block = None
    if feed is not None:
        obj, src, age = feed
        feed_block = {"status": obj.get("status"), "missing": [m for m in obj.get("missing", []) if m] if isinstance(obj.get("missing"), list) else [],
                      "source": src, "age_s": age}
    estatus = engine.get("status") if engine is not None else None
    has = bool(needs or alloc or caps)
    status = "source not available" if engine is None else (estatus if estatus is not None else "source not available")
    if engine is not None and not off and estatus is not None and missing:
        status = "partial"
    return {"title": "Market engine", "status": status, "engine_status": estatus,
            "source": ENGINE_ROUTE if engine is not None else None,
            "age_s": age_of(engine.get("last_run"), now) if engine is not None else None,
            "last_run": engine.get("last_run") if engine is not None else None,
            "runs": engine.get("runs") if engine is not None else None,
            "farmers": engine.get("farmers") if engine is not None else None,
            "ordered": engine.get("ordered") if engine is not None else None,
            "unassigned": engine.get("unassigned") if engine is not None else None,
            "flags": flag_rows(tuning), "flag_note": engine.get("flag") if off else None,
            "caps": [{"name": k, "value": v} for k, v in caps.items()] if caps else [],
            "needs": rank_rows(needs) if needs else [], "allocation": alloc or [],
            "by_map": [{"map": k, "count": v} for k, v in (dist or {}).get("by_map", {}).items()] if dist else [],
            "max_item_share": dist.get("max_item_share") if dist else None,
            "feed": feed_block, "missing": missing, "has_data": has}


def section_metrics(engine, now):
    m = engine.get("metrics") if engine is not None else None
    if not isinstance(m, dict):
        return {"title": "Metrics", "status": "source not available", "source": None, "age_s": None,
                "blocks": {}, "missing": [na(ENGINE_ROUTE + " (metrics: no file on disk; the saved counters are raw)")]}
    blocks = {k: m[k] for k in METRIC_BLOCKS if isinstance(m.get(k), dict)}
    missing = [na(ENGINE_ROUTE + " (metrics." + k + ")") for k in METRIC_BLOCKS if k not in blocks]
    return {"title": "Metrics", "status": section_status(bool(blocks), missing), "source": ENGINE_ROUTE + " (metrics)",
            "age_s": age_of(m.get("t"), now), "counters_since": m.get("counters_since"), "rounds": m.get("rounds"),
            "blocks": blocks, "missing": missing}


# ------------------------------------------------------------------ farmers and kite
def kite_block(fetch, dp, now):
    """steps and deaths: the economy answer's kite block, else the counter files (the core one too)"""
    econ = fetch(ECON_ROUTE) if fetch else None
    if isinstance(econ, dict) and isinstance(econ.get("kite"), dict):
        k = econ["kite"]
        return {"steps": num(k.get("kite_steps")), "deaths": num(k.get("kite_deaths")),
                "since": k.get("counters_since"), "source": ECON_ROUTE + " (kite)", "age_s": age_of(econ.get("t"), now)}
    steps = deaths = 0.0
    srcs, since, age = [], None, None
    for name in ("kite", "kite_core"):
        path = dp(FILES[name])
        obj = read_json(path)
        if not isinstance(obj, dict):
            continue
        srcs.append("data/" + FILES[name])
        steps += sum(num(v) or 0 for v in (obj.get("steps") or {}).values())
        deaths += sum(num(v) or 0 for v in (obj.get("deaths") or {}).values())
        since = since or obj.get("since")
        a = age_of(obj.get("saved"), now)
        age = a if age is None else min(age, a) if a is not None else age
    if not srcs:
        return None
    return {"steps": steps, "deaths": deaths, "since": since, "source": ", ".join(srcs), "age_s": age}


def top_rows(d, label, key="n"):
    """a count map as rows, biggest first"""
    if not isinstance(d, dict):
        return []
    return [{label: k, key: num(v) or 0} for k, v in sorted(d.items(), key=lambda kv: -(num(kv[1]) or 0))]


def section_farmers(fetch, dp, now, feed):
    missing = []
    roster = file_load("farmers", dp, now)
    orders = file_load("orders", dp, now)
    if roster is None:
        missing.append(na("data/farmers.json"))
    if orders is None:
        missing.append(na("data/farmer_orders.json (no /api/market/orders route on main)"))
    rdoc = roster[0] if roster else {}
    accounts = rdoc.get("accounts") if isinstance(rdoc.get("accounts"), list) else None
    detail = rows_of(rdoc, "detail") or []
    vendor_adds = rows_of(rdoc, "vendor_adds")
    odoc = orders[0] if orders else {}
    stats = odoc.get("stats") if isinstance(odoc.get("stats"), dict) else {}
    acc, dec = num(stats.get("accepted")), num(stats.get("declined"))
    rate = acc / (acc + dec) if acc is not None and dec is not None and acc + dec > 0 else None
    order_rows = rows_of(odoc, "orders")
    if orders is not None and order_rows is None:
        missing.append(na("data/farmer_orders.json (orders)"))
    orders_out = [{"farmer": o.get("farmer", ""), "char": o.get("char", ""), "item_id": o.get("item_id"),
                   "item": o.get("item", ""), "mob": o.get("mob", ""), "maps": ", ".join(o.get("maps") or []),
                   "start": o.get("start"), "until": o.get("until")} for o in (order_rows or [])]
    deaths = stats.get("deaths") if isinstance(stats.get("deaths"), dict) else {}
    kite = kite_block(fetch, dp, now)
    if kite is None:
        missing.append(na(ECON_ROUTE + " (kite), data/kite_counters.json"))
    zeny = rows_of(feed[0], "zeny_per_hour") if feed else None
    if zeny is None:
        missing.append(na(FEED_ROUTE + " (zeny_per_hour)"))
    return {"title": "Farmers", "status": section_status(bool(accounts or order_rows), missing),
            "roster": {"count": len(accounts) if accounts is not None else None, "target": rdoc.get("target"),
                       "since": rdoc.get("since"), "vendor_adds": len(vendor_adds) if vendor_adds is not None else None,
                       "vendor_present": sum(1 for d in detail if d.get("vendor_present") is True),
                       "source": roster[1] if roster else None, "age_s": roster[2] if roster else None},
            "orders": {"count": len(order_rows) if order_rows is not None else None, "rows": orders_out,
                       "source": orders[1] if orders else None, "age_s": orders[2] if orders else None},
            "acceptance": {"rate": rate, "accepted": acc, "declined": dec,
                           "source": orders[1] if orders else None},
            "declines": top_rows(stats.get("declines"), "reason"),
            "supplied": top_rows(stats.get("supplied"), "item"),
            "looted": top_rows(stats.get("looted"), "item"),
            "deaths": top_rows(deaths, "account"),
            "kite": kite, "zeny_h": zeny or [], "missing": missing}


# ------------------------------------------------------------------ prices
def section_prices(engine):
    rows = rows_of(engine, "prices") if engine is not None else None
    if rows is None:
        return {"title": "Prices", "status": "source not available", "rows": [], "source": None, "age_s": None,
                "missing": [na("prices: no price model output on main (legion/pricedyn.go has no endpoint; dynamic_price_on)")]}
    out = []
    for r in rows:
        p, ref = num(r.get("price")), num(r.get("reference"))
        hist = [num(h.get("price")) for h in (r.get("history") or []) if isinstance(h, dict) and num(h.get("price")) is not None]
        out.append({"item_id": r.get("item_id"), "item": r.get("item", ""), "price": p, "reference": ref,
                    "vs_reference_pct": p * PERCENT / ref if p is not None and ref else None,
                    "scarcity": r.get("scarcity"), "urgency": r.get("urgency"),
                    "history_n": len(hist), "first": hist[0] if hist else None, "last": hist[-1] if hist else None})
    return {"title": "Prices", "status": "ok", "rows": out, "source": ENGINE_ROUTE + " (prices)", "age_s": None, "missing": []}


# ------------------------------------------------------------------ broadcast
def crier_rules():
    """crier.rules(): the cadence and wording rules, read from tools/system/crier.py (read-only); None if unreadable"""
    try:
        if TOOLS_SYSTEM not in sys.path:
            sys.path.insert(0, TOOLS_SYSTEM)
        import crier
        return crier.rules()
    except Exception:
        return None


def section_broadcast(dp, now, tuning, tail, rules_fn):
    missing = []
    path = dp(FILES["notices"])
    notices = read_jsonl(path, tail) if os.path.exists(path) else None
    if notices is None:
        missing.append(na("data/market_notices.jsonl"))
        notices = []
    try:
        mtime = os.path.getmtime(path)
    except OSError:
        mtime = None
    rules = rules_fn() if rules_fn is not None else crier_rules()
    if not isinstance(rules, dict):
        rules = None
        missing.append(na("tools/system/crier.py rules()"))
    rows = [{"t": n.get("t"), "at": n.get("at"), "kind": n.get("kind", ""), "item": n.get("item", ""),
             "place": n.get("place", ""), "text": n.get("text", ""), "mode": n.get("mode")} for n in notices]
    live = sum(1 for r in rows if r["mode"] == "live")
    flag = [f for f in flag_rows(tuning) if f["flag"] == "market_broadcast_on"][0]
    missing.append("legion_notice rows in ro-db are not read by this tab (the jsonl records every notice with its mode)")
    return {"title": "Broadcast", "status": section_status(bool(rows or rules), missing),
            "notices": rows, "live_n": live, "dry_n": len(rows) - live,
            "notices_source": "data/" + FILES["notices"] if mtime is not None else None,
            "notices_age_s": max(0, now - mtime) if mtime is not None else None,
            "cadence": [{"name": k, "value": v} for k, v in rules.items()] if rules else [],
            "cadence_source": "tools/system/crier.py rules()" if rules else None, "flag": flag, "missing": missing}


# ------------------------------------------------------------------ game facts
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
    base = num((by.get("rate.base") or {}).get("value"))
    scale = num((by.get("rate.scale_base") or {}).get("value"))
    server = num((by.get("server.item_rate_card") or {}).get("value"))
    out = []
    if not (base and scale and server):
        return out
    for key, label in (("creamy.drop.normal_rate", "Creamy"), ("creamy.drop.champion_rate", "Swift Creamy")):
        r = num((by.get(key) or {}).get("value"))
        if not r:
            continue
        inputs = "%s, rate.base, rate.scale_base, server.item_rate_card" % key
        out.append({"id": "derived." + key + ".pct", "group": "Creamy card at the server rates",
                    "label": label + " card chance per kill", "value": round(r * server / scale / base * PERCENT, 4),
                    "unit": "percent", "source": "computed from " + inputs, "derived": True})
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
    rows = [dict(f, age_s=age, derived=False) for f in block["facts"] if isinstance(f, dict)]
    by = {f.get("id"): f for f in rows}
    rows += derive(by)
    return {"title": "Game facts", "status": "ok", "rows": rows, "source": FACTS_MD_REL, "age_s": age,
            "checked": block.get("checked"), "missing": []}


# ------------------------------------------------------------------ the view
def view(fetch, dp, now=None, tail=None, facts_path=FACTS_MD, rules=None):
    """the Market tab's sections, each with its source and age. fetch(route) gives the engine's JSON object or None
    (DB06); dp(name) resolves a data-folder file; tail(path) is the dashboard's bounded .jsonl reader; rules() gives
    crier.rules() (None: read from tools/system/crier.py)."""
    now = time.time() if now is None else now
    engine = fetch(ENGINE_ROUTE) if fetch else None
    if not isinstance(engine, dict):
        engine = None
    feed = load_feed(fetch, dp, now)
    tuning = read_json(dp(FILES["tuning"]))
    tuning = tuning if isinstance(tuning, dict) else None
    return {"t": now, "flags": FLAGS,
            "sections": {"engine": section_engine(engine, feed, tuning, now),
                         "metrics": section_metrics(engine, now),
                         "farmers": section_farmers(fetch, dp, now, feed),
                         "prices": section_prices(engine),
                         "broadcast": section_broadcast(dp, now, tuning, tail, rules),
                         "facts": section_facts(now, facts_path)}}


def file_obj(name, dp):
    """a data file as the offline stand-in serves it (only the feed has a route: /api/market/feed)"""
    return read_json(dp(FILES[name]))
