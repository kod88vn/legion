#!/usr/bin/env python3
"""League analyst (no AI): ranks cohorts by gap to par x n from the meter and writes docs/reports/league-<time>.md.

  league.py [--base http://127.0.0.1:8090] [--out DIR] [--input meter.json] [--neo-input neo.json] [--stdout]
  league.py --compare-deploy [--window-h 3]     3-hour windows before/after the last deploy, per fix: keep or revert

--compare-deploy reads legion/data/meter.jsonl (hourly rows per cohort) and legion/data/deploys.jsonl (records:
epoch, tag, fixes[{text, metric(kpm|zph|deaths), cohort}]). A fix's cohort is a substring of the cohort key
(empty = every cohort). Verdict: KEEP if the target metric moved >= +2% in the right direction, REVERT if it
moved <= -2%, else INCONCLUSIVE (kept, measured again next window).
"""
import argparse
import json
import os
import sys
import time

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import common as C
import meterlib as M

KEEP_PCT, REVERT_PCT = 2.0, -2.0


def f(v, d=2):
    return "-" if v is None else ("%.*f" % (d, v))


def render(meter, neo, now_s):
    cs = M.cohorts(meter)
    srv = M.server(meter)
    ranked = sorted(cs, key=lambda c: -M.score(c))
    L = ["# League report %s" % now_s, ""]
    L.append("Par = p75 of the cohort. Prize = (par - mean) x n. Score = n x mean relative gap over kills/min and zeny/h.")
    L.append("")
    if srv["n"] is not None and srv["n"] == 0:
        L.append("**Warning: no bot counted in this window (n=0).** A bot counts after 25% of the window online; "
                 "right after an engine restart without a meter snapshot (meter_state.json.gz) the window is empty.")
        L.append("")
    if srv["kpm"]["mean"] is not None or srv["zph"]["mean"] is not None:
        L.append("Server: kills/min mean %s, zeny/h mean %s, farming share %s, deaths/h %s" % (
            f(srv["kpm"]["mean"]), f(srv["zph"]["mean"], 0),
            f(M.farming_share(srv["shares"])), f(srv["deaths_ph"])))
        L.append("")
    L.append("## Cohorts by size of the prize")
    if not ranked:
        L.append("(no cohort data - meter not reachable or empty)")
    else:
        L += ["| # | cohort | n | kpm mean / par | zph mean / par | prize kpm | prize zph | score | farming |",
              "|---|---|---|---|---|---|---|---|---|"]
        for i, c in enumerate(ranked[:25], 1):
            L.append("| %d | %s | %d | %s / %s | %s / %s | %s | %s | %s | %s |" % (
                i, c["key"], c["n"], f(c["kpm"]["mean"]), f(c["kpm"]["p75"]), f(c["zph"]["mean"], 0), f(c["zph"]["p75"], 0),
                f(M.prize(c, "kpm")), f(M.prize(c, "zph"), 0), f(M.score(c)), f(M.farming_share(c["shares"]), 2)))
        L.append("")
        L.append("## Where the gap goes (top 8 cohorts, time buckets above their target)")
        for c in ranked[:8]:
            rows = M.gap_breakdown(c)
            L.append("")
            L.append("### %s (n=%d)" % (c["key"], c["n"]))
            if not rows:
                L.append("no time budget available")
                continue
            L += ["| bucket | share | target | excess | est. kpm gain if moved to fighting |", "|---|---|---|---|---|"]
            for b, s, t, ex, g in rows[:5]:
                L.append("| %s | %s | %s | %s | %s |" % (b, f(s), f(t), f(ex), f(g)))
    L.append("")
    L += neo_section(neo, cs)
    return "\n".join(L) + "\n"


def _m(d, *names):
    return M.stat(d, *names)["mean"] if isinstance(d, dict) else None


def neo_section(neo, cs):
    L = ["## Neo view: Ayla's party and Starveil against par"]
    if not neo:
        return L + ["(no data from /api/meter/neo)"]
    entries = []
    if isinstance(neo, dict) and isinstance(neo.get("party"), list):  # engine shape: ayla, party[], starveil[], summaries
        a = neo.get("ayla") or {}
        aw = (a.get("windows") or {}).get(neo.get("window") or "1h") or {}
        in_party = any(isinstance(x, dict) and x.get("instance") == "openkore" for x in neo.get("party") or [])
        if aw and not in_party:  # an older engine: Ayla only in the header (newer: her own row in party[])
            entries.append(("Ayla (purse z/h, OpenKore)", {"zph": aw.get("zph_purse"), "par_zph": aw.get("par_zph"),
                                                           "kpm": aw.get("kpm"), "par_kpm": aw.get("par_kpm")}))
        for grp in ("party", "starveil"):
            for x in neo.get(grp) or []:
                if isinstance(x, dict):
                    entries.append(("%s: %s" % (grp, x.get("name", "?")), x))
    elif isinstance(neo, dict):
        for k, v in neo.items():
            if isinstance(v, dict) and (_m(v, "kpm", "kills_per_min") is not None or _m(v, "zph", "zeny_per_hour") is not None):
                entries.append((k, v))
            elif isinstance(v, list):
                for i, x in enumerate(v):
                    if isinstance(x, dict):
                        entries.append((x.get("name") or "%s[%d]" % (k, i), x))
            elif isinstance(v, dict):
                for k2, x in v.items():
                    if isinstance(x, dict):
                        entries.append(("%s/%s" % (k, k2), x))
    elif isinstance(neo, list):
        entries = [(x.get("name", "?"), x) for x in neo if isinstance(x, dict)]
    if not entries:
        return L + ["(neo payload had no recognisable rows)"]
    L += ["| who | kpm | par | zph | par | flag |", "|---|---|---|---|---|---|"]
    below = 0
    for name, d in entries:
        k, z = _m(d, "kpm", "kills_per_min"), _m(d, "zph", "zeny_per_hour")
        pk = _m(d, "par_kpm", "kpm_par") or M.num((d.get("par") or {}).get("kpm") if isinstance(d.get("par"), dict) else None)
        pz = _m(d, "par_zph", "zph_par") or M.num((d.get("par") or {}).get("zph") if isinstance(d.get("par"), dict) else None)
        flag = []
        if k is not None and pk and k < pk:
            flag.append("kpm below par")
        if z is not None and pz and z < pz:
            flag.append("zph below par")
        below += bool(flag)
        L.append("| %s | %s | %s | %s | %s | %s |" % (name, f(k), f(pk), f(z, 0), f(pz, 0), "**" + ", ".join(flag) + "**" if flag else "ok"))
    L.append("")
    L.append("Neo must be at the top: %d of %d rows below par." % (below, len(entries)) if below else "All rows at or above par.")
    return L


# ------------------------------------------------------------------ deploy comparison
def last_deploy():
    recs = C.read_jsonl(C.dpath("deploys.jsonl"))
    recs = [r for r in recs if r.get("kind") != "verdict" and C.to_epoch(r.get("epoch", r.get("t")))]
    if not recs:
        return None
    last = max(recs, key=lambda r: C.to_epoch(r.get("epoch", r.get("t"))))
    first = min((r for r in recs if r.get("tag") == last.get("tag")), key=lambda r: C.to_epoch(r.get("epoch", r.get("t"))))
    out = dict(last)
    out["epoch"] = C.to_epoch(first.get("epoch", first.get("t")))
    if not out.get("fixes"):
        for r in recs:
            if r.get("tag") == last.get("tag") and r.get("fixes"):
                out["fixes"] = r["fixes"]
    return out


def window_mean(rows, metric, sub, t0, t1):
    """n-weighted mean of a metric over history rows in [t0, t1) for cohorts whose key contains `sub`."""
    num_, den = 0.0, 0.0
    for r in rows:
        t = C.to_epoch(r.get("t", r.get("ts", r.get("time"))))
        if t is None or not (t0 <= t < t1):
            continue
        if r.get("kind", "cohort") != "cohort":  # meter.jsonl also has server and neo rows
            continue
        c = M.cohort(r)
        if sub and sub.lower() not in c["key"].lower():
            continue
        v = c["deaths_ph"] if metric == "deaths" else c[metric]["mean"]
        if v is None:
            continue
        w = c["n"] or 1.0
        num_ += v * w
        den += w
    return (num_ / den) if den else None


def compare(dep, rows, window_h=3.0, now_t=None):
    now_t = now_t or C.now()
    T = dep["epoch"]
    W = window_h * 3600
    out = []
    after_end = min(T + W, now_t)
    complete = now_t >= T + W
    for fx in dep.get("fixes") or [{"text": "(no fix list recorded)", "metric": "kpm", "cohort": ""}]:
        metric = (fx.get("metric") or "kpm").lower()
        metric = {"kills_per_min": "kpm", "zeny_per_hour": "zph"}.get(metric, metric)
        sub = fx.get("cohort") or ""
        b = window_mean(rows, metric, sub, T - W, T)
        a = window_mean(rows, metric, sub, T, after_end)
        if b is None or a is None or b == 0:
            out.append((fx, metric, sub, b, a, None, "NO DATA (keep until measured)"))
            continue
        pct = (a - b) / abs(b) * 100.0
        good = -pct if metric == "deaths" else pct   # fewer deaths is good
        v = "KEEP" if good >= KEEP_PCT else "REVERT" if good <= REVERT_PCT else "INCONCLUSIVE (keep, measure again)"
        if not complete:
            v += " [partial after-window]"
        out.append((fx, metric, sub, b, a, pct, v))
    return out


def render_compare(dep, res, window_h):
    L = ["# Deploy comparison: %s (%s)" % (dep.get("tag", "?"), C.iso(dep["epoch"])), "",
         "Windows: %.0f h before vs %.0f h after the deploy%s." % (window_h, window_h, " (critical deploy)" if dep.get("critical") else ""),
         "", "| fix | metric | cohort | before | after | change | verdict |", "|---|---|---|---|---|---|---|"]
    for fx, metric, sub, b, a, pct, v in res:
        L.append("| %s | %s | %s | %s | %s | %s | %s |" % (
            str(fx.get("text", ""))[:80], metric, sub or "all", f(b), f(a), "-" if pct is None else "%+.1f%%" % pct, v))
    rev = [r for r in res if r[6].startswith("REVERT")]
    L += ["", "REVERT candidates: %d. A revert goes to a fixer as a normal queue item." % len(rev)]
    return "\n".join(L) + "\n"


def verdict_record(dep, res, now_t, window_h=3.0):
    """The keep/revert record for deploys.jsonl: kind "verdict", keyed by the deploy's tag and epoch (deploylog and the
    dashboard skip it as a deploy). `verdict` is the summary the dashboard shows; `items` has each fix's numbers."""
    counts, items = {}, []
    for fx, metric, sub, b, a, pct, v in res:
        k = "NO DATA" if v.startswith("NO DATA") else v.split(" ")[0]
        counts[k] = counts.get(k, 0) + 1
        items.append({"text": str(fx.get("text", "")), "metric": metric, "cohort": sub, "before": b, "after": a,
                      "change_pct": None if pct is None else round(pct, 2), "verdict": v})
    summary = ", ".join("%s %d" % (k, counts[k]) for k in ("KEEP", "REVERT", "INCONCLUSIVE", "NO DATA") if k in counts)
    partial = now_t < dep["epoch"] + window_h * 3600
    return {"kind": "verdict", "t": C.iso(now_t), "tag": dep.get("tag", "?"),
            "deploy_epoch": dep["epoch"], "partial": partial, "window_h": window_h,
            "verdict": (summary or "no fixes") + (" (partial window)" if partial else ""), "items": items}


def record_verdict(dep=None, window_h=3.0, now_t=None):
    """Writes the keep/revert verdict of the last deploy to legion/data/deploys.jsonl (Constitution 3.6, 3.9). Called
    by the deploy flow (deploylog.record) before the next deploy is recorded, and by --compare-deploy. A final verdict
    (complete window) is written once per deploy; a partial one is written only if none exists yet. -> record or None."""
    now_t = C.now() if now_t is None else now_t
    dep = dep or last_deploy()
    if not dep:
        return None
    path = C.dpath("deploys.jsonl")
    done = [r for r in C.read_jsonl(path) if r.get("kind") == "verdict" and r.get("tag") == dep.get("tag")
            and abs((C.to_epoch(r.get("deploy_epoch")) or 0) - dep["epoch"]) < 1]
    if any(not r.get("partial") for r in done) or (done and now_t < dep["epoch"] + window_h * 3600):
        return None
    res = compare(dep, C.read_jsonl(C.dpath("meter.jsonl")), window_h, now_t)
    rec = verdict_record(dep, res, now_t, window_h)
    C.append_jsonl(path, rec)
    return rec


def main(argv=None):
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--base", default=os.environ.get("LEGION_API", "http://127.0.0.1:8090"))
    ap.add_argument("--out", default=os.path.join(C.ROOT, "docs", "reports"))
    ap.add_argument("--input")
    ap.add_argument("--neo-input")
    ap.add_argument("--stdout", action="store_true")
    ap.add_argument("--compare-deploy", action="store_true")
    ap.add_argument("--window-h", type=float, default=3.0)
    a = ap.parse_args(argv)
    stamp = time.strftime("%Y%m%d-%H%M", time.localtime(C.now()))
    if a.compare_deploy:
        dep = last_deploy()
        if not dep:
            print("no deploy recorded in deploys.jsonl")
            return 1
        res = compare(dep, C.read_jsonl(C.dpath("meter.jsonl")), a.window_h)
        text = render_compare(dep, res, a.window_h)
        name = "deploy-compare-%s.md" % stamp
        rec = record_verdict(dep, a.window_h)
        print("verdict recorded in deploys.jsonl: %s" % rec["verdict"] if rec else "verdict already recorded (final)")
    else:
        def load(path, url):
            if path:
                with open(path, encoding="utf-8") as fh:
                    return json.load(fh)
            try:
                return M.fetch(a.base, url)
            except Exception as e:
                sys.stderr.write("could not read %s: %s\n" % (url, e))
                return None
        text = render(load(a.input, "/api/meter"), load(a.neo_input, "/api/meter/neo"), C.iso())
        name = "league-%s.md" % stamp
    if a.stdout:
        print(text)
        return 0
    os.makedirs(a.out, exist_ok=True)
    p = os.path.join(a.out, name)
    with open(p, "w", encoding="utf-8", newline="\n") as fh:
        fh.write(text)
    print(p)
    return 0


if __name__ == "__main__":
    sys.exit(main())
