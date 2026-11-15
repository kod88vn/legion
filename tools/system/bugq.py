#!/usr/bin/env python3
"""Bug queue: legion/data/bugqueue.jsonl, one JSON object per line.

Record: id, t (epoch s; ISO also read), from, priority, text, evidence, status (open|assigned|closed|duplicate).
Optional: key (dedupe key), count, bots (bots affected), metric (kpm|zph|deaths|...), impact (explicit weight),
assigned_to, note, woke, last_t, wake (urgent: wakes the main session; set by sentinel / partycheck for Ayla's
party and stuck bots).
The file is append-only so several writers never clash (package P's legionctl appends the same way): a later line
with the same id is an UPDATE and merges over the earlier ones; the newest value of each field wins.

Rank: outage first, then Neo requests (Ayla's direct line), then the owner's, then estimated impact on the two metrics
(kills per minute, zeny per hour) x bots affected x a small repeat bonus.

  bugq.py add --from sentinel --priority high --text "..." [--key K] [--bots N] [--metric kpm] [--evidence ...]
  bugq.py list [--all] | rank | show ID | close ID [--note ...] | assign ID NAME | dedupe | summary
  bugq.py add ... --wake                                      urgent: also touches data/bugq.wake (the watcher exits)
  bugq.py watch [--interval 15] [--lead-min 20] [--once]     exits (= wakes main) for an outage, a Neo request, an
                                                              urgent item (Ayla's party, stuck bots: wake flag or
                                                              data/bugq.wake; at most once per 15 min, state in
                                                              data/bugq.wake.state) or the pre-deploy planning time;
                                                              prints "what to assign".
"""
import argparse
import json
import math
import os
import random
import re
import sys
import time

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import common as C

QUEUE = "bugqueue.jsonl"
PRIO_W = {"critical": 5, "high": 3, "normal": 2, "medium": 2, "low": 1}
DEPLOY_EVERY = 3 * 3600


def qpath():
    return C.dpath(QUEUE)


def extra_paths():
    """append-only queue files written by other writers in the same format. Ayla's OpenKore watchdog (partylead.pl
    wd_report) can't reach legion/data from its container, so it writes openkore-ayla/logs/bugqueue.jsonl (its logs
    mount). LEGION_BUGQ_EXTRA (os.pathsep-separated) overrides; with LEGION_DATA set (tests) there is no default extra."""
    env = os.environ.get("LEGION_BUGQ_EXTRA")
    if env is not None:
        return [p for p in env.split(os.pathsep) if p]
    if os.environ.get("LEGION_DATA"):
        return []
    return [os.path.join(C.ROOT, "openkore-ayla", "logs", "bugqueue.jsonl")]


def load():
    """id -> merged entry (insertion order = first appearance). Updates (close, assign) always go to the main file;
    they merge over an entry from an extra file by id."""
    out = {}
    rows = []
    for p in extra_paths():
        if os.path.exists(p):
            rows += list(C.read_jsonl(p))
    rows += list(C.read_jsonl(qpath()))
    for r in rows:
        i = r.get("id")
        if not i:
            continue
        e = out.setdefault(i, {"id": i})
        if "t" in r and "t0" not in e:
            e["t0"] = r["t"]
        e.update({k: v for k, v in r.items() if k != "id"})
    return out


def _t(e):
    return C.to_epoch(e.get("t0", e.get("t"))) or 0.0


def is_outage(e):
    p = str(e.get("priority", "")).lower()
    txt = str(e.get("text", "")).upper()
    return (p in ("outage", "critical") or bool(e.get("outage")) or txt.startswith("OUTAGE")
            or txt.startswith("[OUTAGE"))


def is_neo(e):
    p = str(e.get("priority", "")).lower()
    return (str(e.get("from", "")).lower() == "neo" or p == "neo" or e.get("kind") == "neo"
            or str(e.get("text", "")).upper().startswith("NEO"))


def tier(e):
    if is_outage(e):
        return 0
    if is_neo(e):
        return 1
    if str(e.get("from", "")).lower() == "owner":
        return 2
    return 3


def score(e):
    """Estimated impact: priority weight (or explicit impact) x bots affected x repeat bonus. Higher = sooner."""
    w = e.get("impact")
    if not isinstance(w, (int, float)):
        p = e.get("priority")
        w = p if isinstance(p, (int, float)) else PRIO_W.get(str(p).lower(), 2)
    bots = e.get("bots")
    bots = bots if isinstance(bots, (int, float)) and bots >= 1 else 1
    cnt = e.get("count")
    cnt = cnt if isinstance(cnt, (int, float)) and cnt >= 1 else 1
    return float(w) * float(bots) * (1.0 + 0.25 * math.log2(cnt))


def rank(entries):
    """Open/assigned entries, best first: (tier, -score, oldest first)."""
    live = [e for e in entries if str(e.get("status", "open")) in ("open", "assigned", "")]
    return sorted(live, key=lambda e: (tier(e), -score(e), _t(e)))


def norm_text(s):
    s = re.sub(r"\d+(\.\d+)?", "#", str(s).lower())
    return re.sub(r"\s+", " ", s).strip()[:120]


def dedupe_key(e):
    return e.get("key") or "%s|%s" % (str(e.get("from", "")).lower(), norm_text(e.get("text", "")))


def new_id():
    return "b%d%s" % (int(C.now()), "".join(random.choice("0123456789abcdefghijklmnopqrstuvwxyz") for _ in range(3)))


def update(i, **fields):
    C.append_jsonl(qpath(), dict(id=i, **fields))


WAKE_FILE = "bugq.wake"
WAKE_STATE = "bugq.wake.state"   # {"last_urgent": epoch}: the watcher's last exit that showed urgent items
URGENT_GAP = 15 * 60             # urgent items (party, stuck) wake main at most this often; outages/Neo/plan always


def touch_wake(reason=""):
    """data/bugq.wake: the watcher exits on a newer mtime than it has seen (an urgent item arrived). Never raises."""
    try:
        with open(C.dpath(WAKE_FILE), "w", encoding="utf-8") as f:
            f.write("%d %s\n" % (int(C.now()), str(reason)[:200]))
        t = C.now()
        os.utime(C.dpath(WAKE_FILE), (t, t))
    except OSError:
        pass


def add(frm, priority, text, evidence="", key=None, bots=None, metric=None, impact=None, bump_min_secs=0, wake=False):
    """File a bug; if an open entry with the same dedupe key exists, bump its count instead. Returns (id, is_new).
    wake=True (urgent: Ayla's party, stuck bots) marks the entry and touches data/bugq.wake so the watcher wakes the
    main session; a repeat of an entry that already woke it does not wake it again (dedupe)."""
    k = dedupe_key({"from": frm, "text": text, "key": key})
    for e in load().values():
        if str(e.get("status", "open")) in ("open", "assigned") and dedupe_key(e) == k:
            t = C.now()
            last = C.to_epoch(e.get("last_t")) or _t(e)
            upd = {"count": int(e.get("count", 1)) + 1, "last_t": int(t)}
            if evidence:
                upd["evidence"] = evidence
            if bots:
                upd["bots"] = bots
            if str(priority) == "outage" or PRIO_W.get(str(priority), 0) > PRIO_W.get(str(e.get("priority")), 0):
                upd["priority"] = priority
            if wake and not e.get("wake"):
                upd["wake"] = True
            if t - last >= bump_min_secs or upd.get("wake"):
                update(e["id"], **upd)
            if wake and not e.get("woke") and not e.get("wake"):
                touch_wake("%s: %s" % (frm, text))
            return e["id"], False
    i = new_id()
    rec = {"id": i, "t": int(C.now()), "from": frm, "priority": priority, "text": text, "evidence": evidence,
           "status": "open", "key": k, "count": 1}
    if bots:
        rec["bots"] = bots
    if metric:
        rec["metric"] = metric
    if impact is not None:
        rec["impact"] = impact
    if wake:
        rec["wake"] = True
    C.append_jsonl(qpath(), rec)
    if wake:
        touch_wake("%s: %s" % (frm, text))
    return i, True


def close(i, note=""):
    update(i, status="closed", closed_t=int(C.now()), note=note)


def dedupe_all():
    """Merge open entries that share a dedupe key: the oldest stays (count summed), the rest become 'duplicate'."""
    groups = {}
    for e in load().values():
        if str(e.get("status", "open")) in ("open", "assigned"):
            groups.setdefault(dedupe_key(e), []).append(e)
    merged = 0
    for k, g in groups.items():
        if len(g) < 2:
            continue
        g.sort(key=_t)
        keep = g[0]
        update(keep["id"], count=sum(int(x.get("count", 1)) for x in g), key=k)
        for x in g[1:]:
            update(x["id"], status="duplicate", dup_of=keep["id"])
            merged += 1
    return merged


def fmt(e, n=None):
    t = ["OUTAGE", "NEO", "OWNER", ""][tier(e)]
    pre = "%2s " % n if n is not None else ""
    return "%s%s [%s%s] from %s x%s%s: %s" % (
        pre, e["id"], t + " " if t else "", e.get("priority", ""), e.get("from", "?"), e.get("count", 1),
        (" bots=%s" % e["bots"]) if e.get("bots") else "", str(e.get("text", ""))[:140].replace("\n", " "))


def what_to_assign(limit=8):
    r = rank(list(load().values()))
    if not r:
        return "WHAT TO ASSIGN: queue is empty."
    lines = ["WHAT TO ASSIGN (%d open, best first):" % len(r)]
    for n, e in enumerate(r[:limit], 1):
        lines.append("  " + fmt(e, n))
        if e.get("evidence"):
            lines.append("      evidence: %s" % str(e["evidence"])[:160].replace("\n", " "))
    if len(r) > limit:
        lines.append("  ... %d more: python tools/system/bugq.py list" % (len(r) - limit))
    return "\n".join(lines)


# ------------------------------------------------------------------ the wake watcher
def last_deploy_epoch():
    best = None
    for d in C.read_jsonl(C.dpath("deploys.jsonl")):
        if d.get("kind") == "verdict":  # keep/revert line (league.py), not a deploy
            continue
        t = C.to_epoch(d.get("epoch", d.get("t")))
        if t and (best is None or t > best):
            best = t
    return best


def watch_state():
    try:
        with open(C.dpath("bugq.watch.json")) as f:
            return json.load(f)
    except Exception:
        return {}


def save_watch_state(s):
    try:
        with open(C.dpath("bugq.watch.json"), "w") as f:
            json.dump(s, f)
    except OSError:
        pass


def _neo_mtime():
    try:
        return os.path.getmtime(C.dpath("neo.wake"))
    except OSError:
        return None


def _wake_mtime():
    try:
        return os.path.getmtime(C.dpath(WAKE_FILE))
    except OSError:
        return None


def wake_state():
    try:
        with open(C.dpath(WAKE_STATE)) as f:
            s = json.load(f)
        return s if isinstance(s, dict) else {}
    except Exception:
        return {}


def save_wake_state(s):
    try:
        with open(C.dpath(WAKE_STATE), "w") as f:
            json.dump(s, f)
    except OSError:
        pass


def wake_reasons(lead_min=20, urgent_gap=URGENT_GAP):
    """Pure check: what should wake the main session right now? [(kind, text, entry-id or None)].
    Urgent items alone wake at most once per urgent_gap s (data/bugq.wake.state); held back, they stay un-woken and
    show at the next allowed wake. An outage, a Neo request or the planning time always wakes (and shows them too)."""
    out = []
    for e in rank(list(load().values())):
        if e.get("woke"):
            continue
        if is_outage(e):
            out.append(("outage", fmt(e), e["id"]))
        elif is_neo(e):
            out.append(("neo", fmt(e), e["id"]))
        elif e.get("wake"):
            out.append(("urgent", fmt(e), e["id"]))
    st = watch_state()
    w = _wake_mtime()
    if w is not None and w > st.get("wake_seen", 0) and not any(k == "urgent" for k, _, _ in out):
        out.append(("urgent", "bugq.wake touched (an urgent item arrived; see the queue)", None))
    m = _neo_mtime()
    if m is not None and m > st.get("neo_wake_seen", 0) and not any(k == "neo" for k, _, _ in out):
        out.append(("neo", "neo.wake touched (a Neo request arrived; see the queue)", None))
    d = last_deploy_epoch()
    if d and C.now() >= d + DEPLOY_EVERY - lead_min * 60 and st.get("plan_for") != d:
        out.append(("plan", "pre-deploy planning time: the 3-hour window since the last deploy (%s) is about to end; "
                    "run league.py --compare-deploy, then one planning pass for the next batch" % C.iso(d), None))
    if out and all(k == "urgent" for k, _, _ in out):
        last = wake_state().get("last_urgent", 0)
        if isinstance(last, (int, float)) and 0 <= C.now() - last < urgent_gap:
            return []
    return out


def mark_woken(reasons):
    st = watch_state()
    for kind, _, i in reasons:
        if i:
            update(i, woke=True)
        if kind == "plan":
            st["plan_for"] = last_deploy_epoch()
    if any(k == "neo" for k, _, _ in reasons):
        m = _neo_mtime()
        if m is not None:
            st["neo_wake_seen"] = m
    w = _wake_mtime()
    if w is not None:
        st["wake_seen"] = w   # every wake shows the whole queue, so a pending wake file is covered too
    save_watch_state(st)
    if any(k == "urgent" for k, _, _ in reasons):
        ws = wake_state()
        ws["last_urgent"] = int(C.now())
        save_wake_state(ws)


def cmd_watch(a):
    while True:
        r = wake_reasons(a.lead_min, a.urgent_gap_min * 60)
        if r:
            mark_woken(r)
            print("BUGQ WAKE at %s: %s" % (C.iso(), ", ".join(sorted(set(k for k, _, _ in r)))))
            for k, t, _ in r:
                print(" - %s: %s" % (k, t))
            print(what_to_assign())
            return 0
        if a.once:
            print("nothing to wake for (%d open in the queue)" % len(rank(list(load().values()))))
            return 1
        C.heartbeat("bugq-watch")
        time.sleep(a.interval)


def main(argv=None):
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    sub = ap.add_subparsers(dest="cmd", required=True)
    s = sub.add_parser("add")
    s.add_argument("--from", dest="frm", default="main")
    s.add_argument("--priority", default="normal")
    s.add_argument("--text", required=True)
    s.add_argument("--evidence", default="")
    s.add_argument("--key")
    s.add_argument("--bots", type=int)
    s.add_argument("--metric")
    s.add_argument("--impact", type=float)
    s.add_argument("--wake", action="store_true", help="urgent: wake the main session (bugq.wake)")
    for n in ("list", "rank", "summary"):
        s = sub.add_parser(n)
        s.add_argument("--all", action="store_true")
    s = sub.add_parser("show")
    s.add_argument("id")
    s = sub.add_parser("close")
    s.add_argument("id")
    s.add_argument("--note", default="")
    s = sub.add_parser("assign")
    s.add_argument("id")
    s.add_argument("to")
    sub.add_parser("dedupe")
    s = sub.add_parser("watch")
    s.add_argument("--interval", type=float, default=15)
    s.add_argument("--lead-min", type=float, default=20)
    s.add_argument("--once", action="store_true")
    s.add_argument("--urgent-gap-min", type=float, default=URGENT_GAP / 60,
                   help="urgent items (party, stuck) wake at most this often; outages always wake")
    a = ap.parse_args(argv)
    if a.cmd == "add":
        i, new = add(a.frm, a.priority, a.text, a.evidence, a.key, a.bots, a.metric, a.impact, wake=a.wake)
        print("%s %s" % (i, "added" if new else "duplicate: count bumped"))
    elif a.cmd in ("list", "rank", "summary"):
        es = list(load().values())
        if a.cmd == "summary":
            print(what_to_assign())
            return 0
        rows = (sorted(es, key=lambda e: (str(e.get("status")) == "closed", tier(e), -score(e), _t(e)))
                if a.all else rank(es))
        for n, e in enumerate(rows, 1):
            print(fmt(e, n) + (" (%s)" % e.get("status") if a.all else "")
                  + ("  score %.1f" % score(e) if a.cmd == "rank" else ""))
        if not rows:
            print("(empty)")
    elif a.cmd == "show":
        e = load().get(a.id)
        if not e:
            print("no such id")
            return 1
        print(json.dumps(e, indent=2, ensure_ascii=False))
    elif a.cmd == "close":
        close(a.id, a.note)
        print("closed", a.id)
    elif a.cmd == "assign":
        update(a.id, status="assigned", assigned_to=a.to)
        print("assigned", a.id, "->", a.to)
    elif a.cmd == "dedupe":
        print("merged %d duplicates" % dedupe_all())
    elif a.cmd == "watch":
        return cmd_watch(a)
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except BrokenPipeError:
        sys.exit(0)
