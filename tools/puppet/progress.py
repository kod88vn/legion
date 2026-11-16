"""Server progress for the market master (rule EC43): plain code, read-only, no AI.

Computes how far the server has progressed on the build milestones and writes one compact JSON briefing:
  - level bands (the breakpoints of legion/data/buildmilestones.json) and how many bots stand in each,
  - per build of the milestone table: how many bots of its class line reached each band's minimum level, and the
    engine's gear fit (share of bots wearing a piece of their build's table, /api/buildgear/fit),
  - zeny per hour by band (/api/meter/bots, counted rows only),
  - gear and cards in short supply or with no supply (economy wants and offers, guild needs; the build tables in
    legion/data/buildgear.json),
  - per guild: online members, mean level, and the top gear needs the guild stock tracks (/api/guild-stock).

Rules: a missing source is left out and named in "missing"; a number is never invented. With no live data the
briefing says so (status "no_data"). Reads only: the engine API and the static tables in the data folder
(LEGION_DATA in tests, G23). Standard library only.

CLI (prints the JSON; --out writes a file): python tools/puppet/progress.py [--out PATH] [--from-json FIXTURE]
"""
import bisect
import json
import os
import sys
import time

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, os.path.join(HERE, "..", "system"))
import common as LC  # noqa: E402  (data folder resolver; refused for live paths under LEGION_TEST=1)

MAX_JSON = 3500          # the market briefing carries at most this many characters of progress JSON
MAX_BUILDS = 12
MAX_GEAR = 12
MAX_GUILDS = 10
MAX_NEEDS = 3
MAX_BANDS = 6
MAX_WORST = 3
LEVEL_CAP = 99


# ----------------------------------------------------------------- small helpers
def _num(x):
    if isinstance(x, bool):
        return None
    try:
        return float(x)
    except (TypeError, ValueError):
        return None


def _int(x):
    v = _num(x)
    return int(v) if v is not None else None


def load_static(name, data_dir=None):
    """a static table from the data folder (read-only); None when missing or unreadable"""
    path = os.path.join(data_dir or LC.data_dir(), name)
    try:
        with open(path, encoding="utf-8") as f:
            return json.load(f)
    except (OSError, ValueError):
        return None


def milestone_builds(table):
    """{build id: {"class_line": set of lower-case class names, "ranges": [{min, max, class_at_end}]}} from
    buildmilestones.json. Only the fields this module needs are read; new fields are ignored."""
    out = {}
    builds = table.get("builds") if isinstance(table, dict) else None
    if not isinstance(builds, dict):
        return out
    for bid, b in builds.items():
        if not isinstance(b, dict):
            continue
        ranges = []
        for r in b.get("ranges") or []:
            if isinstance(r, dict) and _int(r.get("min")) is not None:
                mx = _int(r.get("max"))
                ranges.append({"min": _int(r["min"]), "max": mx if mx is not None else LEVEL_CAP,
                               "class_at_end": str(r.get("class_at_end") or "")})
        line = {str(b.get(k) or "").lower() for k in ("class", "first_job")} | {r["class_at_end"].lower() for r in ranges}
        line.discard("")
        if ranges:
            out[bid] = {"name": str(b.get("name") or bid), "class_line": line, "ranges": ranges}
    return out


def level_bands(mbuilds):
    """level bands from the milestone breakpoints: [(label, lo, hi)], e.g. 1-17, 18-32, 33-47, 48-99. No table, no bands."""
    if not mbuilds:
        return []
    starts = sorted({r["min"] for b in mbuilds.values() for r in b["ranges"] if 1 <= r["min"] <= LEVEL_CAP} | {1})
    bands = []
    for i, lo in enumerate(starts):
        hi = starts[i + 1] - 1 if i + 1 < len(starts) else LEVEL_CAP
        bands.append((f"{lo}-{hi}", lo, hi))
    return bands


def band_of(bands, lv):
    """label of the band holding level lv (None when there are no bands or lv is not a level)"""
    if not bands or lv is None:
        return None
    starts = [b[1] for b in bands]
    i = bisect.bisect_right(starts, lv) - 1
    return bands[i][0] if i >= 0 else None


def _rows(data):
    """a list of dict rows from a list, or from a dict wrapping one list ({"bots": [...]})"""
    if isinstance(data, list):
        return [r for r in data if isinstance(r, dict)]
    if isinstance(data, dict):
        for v in data.values():
            if isinstance(v, list):
                return [r for r in v if isinstance(r, dict)]
    return []


# ----------------------------------------------------------------- supply and demand of goods
def goods_names_and_flows(economy):
    """(name -> item id, item id -> supply units, item id -> demand units) from economy wants and offers"""
    names, supply, demand = {}, {}, {}
    if not isinstance(economy, dict):
        return names, supply, demand
    for o in _rows(economy.get("offers")):
        iid = _int(o.get("item_id"))
        if iid is None:
            continue
        if o.get("item"):
            names[str(o["item"])] = iid
        supply[iid] = supply.get(iid, 0) + (_int(o.get("qty")) or 0)
    for w in _rows(economy.get("wants")):
        iid = _int(w.get("item_id"))
        if iid is None:
            continue
        if w.get("item"):
            names[str(w["item"])] = iid
        demand[iid] = demand.get(iid, 0) + (_int(w.get("count")) or 0)
    return names, supply, demand


def guild_needs(stock_rows):
    """{guild: {item name: units}} from /api/guild-stock rows (the needs the guild tracks, per member gear pieces)"""
    out = {}
    for r in stock_rows:
        g = str(r.get("guild") or "")
        needs = r.get("needs")
        if g and isinstance(needs, dict):
            out[g] = {str(k): _int(v) or 0 for k, v in needs.items()}
    return out


def short_goods(gear, bands_online, names, supply, demand, needs_by_name, item_name):
    """gear and cards of the build tables in the bands that online bots stand in, plus every good somebody wants,
    with supply vs demand. kind "none": nobody offers it; kind "short": offered fewer units than wanted (wants +
    guild needs)."""
    cand = {}
    for iid, n in demand.items():
        if n > 0:
            cand.setdefault(iid, "wanted")
    for name, n in needs_by_name.items():
        if n > 0 and name in names:
            cand.setdefault(names[name], "wanted")
    for bid, b in gear.items():
        if not isinstance(b, dict):
            continue
        for band in b.get("bands") or []:
            if not isinstance(band, dict):
                continue
            lo, hi = _int(band.get("min")), _int(band.get("max"))
            if lo is None or bid not in bands_online:
                continue
            hi = hi if hi is not None else LEVEL_CAP
            if not lo <= bands_online[bid] <= hi:
                continue
            for slot in ("weapon", "armor", "shield", "headgear", "garment", "shoes", "accessory", "ammo"):
                for iid in band.get(slot) or []:
                    if _int(iid):
                        cand.setdefault(_int(iid), slot)
            for c in band.get("cards") or []:
                cid = _int(c.get("id")) if isinstance(c, dict) else _int(c)
                if cid:
                    cand.setdefault(cid, "card")
    out = []
    for iid, slot in cand.items():
        want = demand.get(iid, 0) + needs_by_name.get(item_name(iid), 0)
        have = supply.get(iid, 0)
        if have == 0:
            out.append({"item": item_name(iid), "id": iid, "slot": slot, "kind": "none", "demand": want, "supply": 0})
        elif have < want:
            out.append({"item": item_name(iid), "id": iid, "slot": slot, "kind": "short", "demand": want, "supply": have})
    out.sort(key=lambda r: (r["kind"] != "none", -r["demand"], r["item"]))
    return out[:MAX_GEAR]


# ----------------------------------------------------------------- the briefing
def compute(bots=None, milestones=None, gear=None, fit=None, meter_bots=None, economy=None, stock=None,
            missing=None, now=None, market=None):
    """the progress briefing from already-read inputs (None = that source did not answer). Pure: tests call this.
    market: the /api/market/engine answer; its "metrics" block (marketmetrics.go) is carried in a compact form."""
    now = time.time() if now is None else now
    missing = sorted(set(missing or []))
    notes = []
    mbuilds = milestone_builds(milestones) if isinstance(milestones, dict) else {}
    bands = level_bands(mbuilds)
    if milestones is None:
        notes.append("no milestone table (legion/data/buildmilestones.json missing): no band numbers")
    gear_tbl = gear.get("builds") if isinstance(gear, dict) and isinstance(gear.get("builds"), dict) else {}
    if gear is None:
        notes.append("no gear table (legion/data/buildgear.json missing): no gear or card numbers")

    out = {"t": time.strftime("%Y-%m-%dT%H:%M:%S", time.localtime(now)), "status": "", "notes": notes, "missing": missing}
    have_bots = bots is not None
    brows = _rows(bots.get("bots") if isinstance(bots, dict) and "bots" in bots else bots) if have_bots else []

    if have_bots:
        lvls = [_int(r.get("level")) for r in brows if _int(r.get("level")) is not None]
        online = [r for r in brows if r.get("online")]
        lvs_online = sorted(_int(r.get("level")) for r in online if _int(r.get("level")) is not None)
        out["server"] = {"bots": len(brows), "online": len(online),
                         "level_max": max(lvls) if lvls else None,
                         "level_median": lvs_online[len(lvs_online) // 2] if lvs_online else None}
        if bands:
            counts = {lbl: 0 for lbl, _, _ in bands}
            for lv in lvls:
                b = band_of(bands, lv)
                if b:
                    counts[b] += 1
            out["levels"] = [{"band": lbl, "bots": counts[lbl]} for lbl, _, _ in bands]

        # milestones: class-line bots (by class name, approximate: the engine does not send a bot's build) at or past
        # each range's minimum level, and the engine's gear fit for that build
        by_build = fit.get("by_build") if isinstance(fit, dict) and isinstance(fit.get("by_build"), dict) else {}
        blist = []
        for bid, mb in mbuilds.items():
            members = [r for r in brows if str(r.get("class") or "").lower() in mb["class_line"]]
            if not members:
                continue
            ms = []
            for rg in mb["ranges"]:
                reached = sum(1 for r in members if (_int(r.get("level")) or 0) >= rg["min"])
                ms.append({"range": f"{rg['min']}-{rg['max']}", "min": rg["min"], "reached": reached})
            f = by_build.get(bid) if isinstance(by_build.get(bid), dict) else {}
            row = {"build": bid, "bots": len(members), "milestones": ms}
            if _num(f.get("weapon_fit_pct")) is not None:
                row["gear_fit"] = {"weapon_pct": _int(f.get("weapon_fit_pct")), "armor_pct": _int(f.get("armor_fit_pct"))}
            blist.append(row)
        blist.sort(key=lambda r: -r["bots"])
        if blist:
            out["builds"] = blist[:MAX_BUILDS]
            if len(blist) > MAX_BUILDS:
                out["builds_more"] = len(blist) - MAX_BUILDS
        elif mbuilds:
            notes.append("no bot of any milestone build's class line is on the server yet")

        if bands and meter_bots is not None:
            zb = {lbl: [] for lbl, _, _ in bands}
            for r in _rows(meter_bots.get("bots") if isinstance(meter_bots, dict) and "bots" in meter_bots else meter_bots):
                if r.get("counted") is False or _num(r.get("zph")) is None:
                    continue
                b = band_of(bands, _int(r.get("lv")))
                if b:
                    zb[b].append(_num(r.get("zph")))
            out["zeny_per_hour"] = [{"band": lbl, "n": len(zb[lbl]),
                                     "zph_mean": round(sum(zb[lbl]) / len(zb[lbl])) if zb[lbl] else None}
                                    for lbl, _, _ in bands]

    # market metrics (WP8): coverage, price stability, farmers, kiting, zeny per hour; empty numbers stay null
    if market is not None:
        m = market.get("metrics") if isinstance(market, dict) else None
        if isinstance(m, dict):
            out["metrics"] = _metrics_brief(m)
        else:
            notes.append("market engine answered without a metrics block: no market metrics")

    # goods: needs the online bots' bands make relevant, against supply and demand
    names, supply, demand = goods_names_and_flows(economy)
    needs = guild_needs(_rows(stock) if stock is not None else [])
    needs_by_name = {}
    for g in needs.values():
        for k, v in g.items():
            needs_by_name[k] = needs_by_name.get(k, 0) + (v or 0)
    id_names = {v: k for k, v in names.items()}

    def item_name(iid):
        return id_names.get(iid) or f"#{iid}"

    if gear is not None and have_bots:
        # the median level of each build's online class line decides which gear bands are relevant now
        bands_online = {}
        for bid, mb in mbuilds.items():
            lvs = sorted(_int(r.get("level")) for r in brows if r.get("online")
                         and str(r.get("class") or "").lower() in mb["class_line"] and _int(r.get("level")) is not None)
            if lvs and bid in gear_tbl:
                bands_online[bid] = lvs[len(lvs) // 2]
        out["short_gear"] = short_goods(gear_tbl, bands_online, names, supply, demand, needs_by_name, item_name)
    elif economy is not None:
        out["short_gear"] = [{"item": n, "id": i, "kind": "short" if supply.get(i, 0) < demand.get(i, 0) else "ok",
                              "demand": demand.get(i, 0), "supply": supply.get(i, 0)}
                             for n, i in sorted(names.items()) if supply.get(i, 0) < demand.get(i, 0)][:MAX_GEAR]

    if stock is not None:
        gs = []
        for g in sorted(needs):
            members = [r for r in brows if str(r.get("guild") or "") == g] if have_bots else []
            lv = [_int(r.get("level")) for r in members if _int(r.get("level")) is not None]
            top = sorted(needs[g].items(), key=lambda kv: -(kv[1] or 0))[:MAX_NEEDS]
            row = {"guild": g, "needs": [{"item": k, "n": v} for k, v in top if v]}
            if have_bots:
                row["members"] = len(members)
                row["online"] = sum(1 for r in members if r.get("online"))
                if lv:
                    row["mean_lv"] = round(sum(lv) / len(lv))
            gs.append(row)
        out["guilds"] = gs[:MAX_GUILDS]

    if bots is None and milestones is None and economy is None and stock is None and meter_bots is None and gear is None:
        status = "no_data"
        notes.insert(0, "no live data: the engine did not answer for bots, economy, guild stock or meter; "
                        "no progress numbers are given, do not invent any")
    elif missing or not have_bots:
        status = "partial"
    else:
        status = "ok"
    out["status"] = status
    return out


def _metrics_brief(m):
    """the market metrics block (marketmetrics.go: docs/design/market-metrics.md) cut to the briefing's few numbers"""
    def sub(k):
        v = m.get(k)
        return v if isinstance(v, dict) else {}

    cov, ps, far, kite, zph = sub("coverage"), sub("price_stability"), sub("farmers"), sub("kite"), sub("zeny_per_hour")
    by_band = cov.get("by_band") if isinstance(cov.get("by_band"), list) else []
    items = ps.get("items") if isinstance(ps.get("items"), list) else []
    return {
        "coverage": {"status": cov.get("status"), "overall": cov.get("overall"),
                     "by_band": [{"band": r.get("band"), "coverage": r.get("coverage")}
                                 for r in by_band[:MAX_BANDS] if isinstance(r, dict)]},
        "price_stability": {"status": ps.get("status"), "target": ps.get("target"), "overall": ps.get("overall"),
                            "within_target": ps.get("within_target"), "items_over": ps.get("items_over"),
                            "worst": [{"item": r.get("item"), "stddev": r.get("stddev")}
                                      for r in items[:MAX_WORST] if isinstance(r, dict)]},
        "farmers": {"status": far.get("status"), "offers": far.get("offers"), "accepted": far.get("accepted"),
                    "accept_rate": far.get("accept_rate"), "deaths": far.get("deaths"),
                    "deaths_per_hour": far.get("deaths_per_hour")},
        "kite": {"status": kite.get("status"), "steps": kite.get("steps"), "deaths": kite.get("deaths"),
                 "deaths_per_step": kite.get("deaths_per_step")},
        "zeny_per_hour": {"status": zph.get("status"),
                          "by_band": [{"band": r.get("band"), "n": r.get("n"), "zph_mean": r.get("zph_mean")}
                                      for r in (zph.get("by_band") or [])[:MAX_BANDS] if isinstance(r, dict)]},
    }


def compact_json(prog, limit=MAX_JSON):
    """the JSON the briefing carries: compact, cut at `limit` characters (never a broken object: lists shrink first)"""
    p = dict(prog)
    s = json.dumps(p, ensure_ascii=False, separators=(",", ":"), default=str)
    while len(s) > limit and isinstance(p.get("builds"), list) and p["builds"]:
        p["builds"] = p["builds"][:-1]
        s = json.dumps(p, ensure_ascii=False, separators=(",", ":"), default=str)
    while len(s) > limit and isinstance(p.get("short_gear"), list) and p["short_gear"]:
        p["short_gear"] = p["short_gear"][:-1]
        s = json.dumps(p, ensure_ascii=False, separators=(",", ":"), default=str)
    while len(s) > limit and isinstance(p.get("guilds"), list) and p["guilds"]:
        p["guilds"] = p["guilds"][:-1]
        s = json.dumps(p, ensure_ascii=False, separators=(",", ":"), default=str)
    if len(s) > limit:
        s = json.dumps({"status": p.get("status"), "t": p.get("t"), "notes": ["progress cut to fit the briefing"]},
                       ensure_ascii=False, separators=(",", ":"))
    return s


def collect(api, data_dir=None, now=None):
    """read the engine API (an object with .get(path), like briefing.Api) and the static tables, then compute()"""
    milestones = load_static("buildmilestones.json", data_dir)
    gear = load_static("buildgear.json", data_dir)
    seen = getattr(api, "missing", None)
    start = len(seen) if isinstance(seen, list) else 0
    bots = api.get("/api/bots")
    fit = api.get("/api/buildgear/fit")
    meter = api.get("/api/meter/bots?limit=2000")
    economy = api.get("/api/economy")
    stock = api.get("/api/guild-stock")
    market = api.get("/api/market/engine")
    # only the sources this module asked for: an endpoint another brief section missed is not this one's gap
    missing = list(seen[start:]) if isinstance(seen, list) else []
    return compute(bots=bots, milestones=milestones, gear=gear, fit=fit, meter_bots=meter, economy=economy,
                   stock=stock, missing=missing, now=now, market=market)


def main(argv=None):
    import argparse
    ap = argparse.ArgumentParser(description="server progress briefing (read-only, plain code)")
    ap.add_argument("--out", help="write the JSON to this file (default: print it)")
    args = ap.parse_args(argv)
    sys.path.insert(0, HERE)
    import briefing  # noqa: E402
    prog = collect(briefing.Api())
    text = compact_json(prog)
    if args.out:
        with open(args.out, "w", encoding="utf-8") as f:
            f.write(text + "\n")
    else:
        print(text)
    return 0


if __name__ == "__main__":
    sys.exit(main())
