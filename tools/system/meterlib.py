"""Defensive reader for the engine's meter (/api/meter, /api/meter/neo, legion/data/meter.jsonl).

Engine shapes (docs/design/meter.md, legion/meterapi.go; checked at the integration pass): /api/meter has
server{bots, kpm, zph, deaths_h, kpm_median, zph_median, shares} and cohorts[{key, line, band, mode, n,
kpm{mean,median,p75,par,gap,prize}, zph{...}, deaths_h, shares}]; meter.jsonl rows carry kind cohort|server|neo with
the same cohort fields. Accessors still try older spellings and return None when a number is absent. Normalized cohort:
  {key, line, band, mode, n, kpm{mean,median,p75}, zph{...}, shares{bucket: 0..1}, deaths_ph}
Par = the cohort's p75 (or an explicit "par" field if the engine gives one).
"""
import json
import urllib.request

BUCKETS = ["fighting", "searching", "travelling", "resting", "town", "dead", "waiting", "stuck", "idle"]
ALIASES = {"traveling": "travelling", "travel": "travelling", "fight": "fighting", "search": "searching",
           "rest": "resting", "wait": "waiting"}
PRODUCTIVE = ("fighting", "searching")
# soft targets for non-productive buckets (share of online time); the excess is the "recoverable" time
TARGET = {"travelling": 0.03, "resting": 0.03, "town": 0.02, "dead": 0.01, "waiting": 0.02, "stuck": 0.0, "idle": 0.01}


def fetch(base, path, timeout=10):
    with urllib.request.urlopen(base + path, timeout=timeout) as r:
        return json.load(r)


def num(v):
    return float(v) if isinstance(v, (int, float)) and not isinstance(v, bool) else None


def stat(d, *names):
    """{'mean','median','p75'} for the first metric name present; accepts a number, a dict, or flat keys."""
    for nm in names:
        v = d.get(nm)
        if isinstance(v, dict):
            return {"mean": num(v.get("mean", v.get("avg"))), "median": num(v.get("median", v.get("p50"))),
                    "p75": num(v.get("p75", v.get("par")))}
        if num(v) is not None:
            return {"mean": num(v), "median": num(d.get(nm + "_median")), "p75": num(d.get(nm + "_p75", d.get(nm + "_par")))}
        if num(d.get(nm + "_mean")) is not None:
            return {"mean": num(d.get(nm + "_mean")), "median": num(d.get(nm + "_median")),
                    "p75": num(d.get(nm + "_p75", d.get(nm + "_par")))}
    return {"mean": None, "median": None, "p75": None}


def shares(d):
    s = d.get("shares") or d.get("time_shares") or d.get("time") or d.get("budget") or {}
    out = {}
    if isinstance(s, dict):
        tot = sum(num(v) or 0 for v in s.values())
        for k, v in s.items():
            v = num(v)
            if v is None:
                continue
            k = ALIASES.get(k.lower(), k.lower())
            out[k] = v
        # seconds instead of fractions: normalise
        if tot > 1.5:
            out = {k: v / tot for k, v in out.items()}
    return out


def cohort(d):
    key = d.get("cohort") or d.get("key") or d.get("name") or "%s %s %s" % (
        d.get("line") or d.get("class_line") or "?", d.get("band") or d.get("level_band") or "?",
        d.get("mode") or ("party" if d.get("party") else "solo"))
    n = num(d.get("n", d.get("bots", d.get("count")))) or 0
    dp = d.get("deaths_h", d.get("deaths_per_hour", d.get("deaths_ph", d.get("deaths"))))
    return {"key": str(key), "line": d.get("line") or d.get("class_line"), "band": d.get("band") or d.get("level_band"),
            "mode": d.get("mode"), "n": n, "kpm": stat(d, "kpm", "kills_per_min", "kills_per_minute"),
            "zph": stat(d, "zph", "zeny_per_hour", "zeny_ph"), "shares": shares(d),
            "deaths_ph": num(dp) if not isinstance(dp, dict) else num(dp.get("mean"))}


def cohorts(meter):
    """List of normalized cohorts from /api/meter (cohorts as list or dict keyed by name) or history rows."""
    if isinstance(meter, list):
        raw = [r for r in meter if not isinstance(r, dict) or r.get("kind", "cohort") == "cohort"]  # history rows
    else:
        raw = (meter or {}).get("cohorts", [])
    if isinstance(raw, dict):
        raw = [dict(v, cohort=k) if isinstance(v, dict) else {"cohort": k} for k, v in raw.items()]
    return [cohort(c) for c in raw if isinstance(c, dict)]


def server(meter):
    s = (meter or {}).get("server") or (meter or {}).get("totals") or {}
    return {"kpm": stat(s, "kpm", "kills_per_min", "kills_per_minute"), "zph": stat(s, "zph", "zeny_per_hour"),
            "shares": shares(s), "deaths_ph": num(s.get("deaths_h", s.get("deaths_per_hour"))),
            "n": num(s.get("n", s.get("bots", s.get("bots_online"))))}


def fighters(cs):
    """cohorts of fighting bots: vendor cohorts (mode vendor: vending is their job) are left out of farming checks"""
    return [c for c in cs if str(c.get("mode") or "").lower() != "vendor"]


def rel_gap(c, metric):
    """Relative gap to par: max(0, (par-mean)/par) or None."""
    m, p = c[metric]["mean"], c[metric]["p75"]
    if m is None or not p:
        return None
    return max(0.0, (p - m) / p)


def prize(c, metric):
    """Size of the prize in metric units: (par - mean) x n."""
    m, p = c[metric]["mean"], c[metric]["p75"]
    if m is None or p is None:
        return 0.0
    return max(0.0, p - m) * c["n"]


def score(c):
    """Overall rank score = n x mean relative gap over the two metrics."""
    g = [x for x in (rel_gap(c, "kpm"), rel_gap(c, "zph")) if x is not None]
    return c["n"] * (sum(g) / len(g)) if g else 0.0


def farming_share(sh):
    return sum(sh.get(b, 0.0) for b in PRODUCTIVE) if sh else None


def gap_breakdown(c):
    """Where the time goes: [(bucket, share, target, excess, est_kpm_gain)] sorted by excess, biggest first.
    est gain = mean kpm x excess / fighting share (time moved into fighting at the cohort's own pace)."""
    sh = c["shares"]
    fight = sh.get("fighting") or 0.0
    kpm = c["kpm"]["mean"]
    rows = []
    for b, tgt in TARGET.items():
        s = sh.get(b)
        if s is None:
            continue
        ex = max(0.0, s - tgt)
        gain = (kpm * ex / fight) if (kpm and fight > 0.05) else None
        rows.append((b, s, tgt, ex, gain))
    return sorted(rows, key=lambda r: -r[3])
