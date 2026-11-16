#!/usr/bin/env python3
"""Deploy guard backend, called by tools/deploy/deploy.py and tools/deploy/guard.sh (WSL python3). Standard library only.

  deploylog.py check [--min-gap SEC] [--critical REASON] [--tag TAG] [--fixes N]
      exit 0 = allowed, 5 = refused (too soon / no fix list). A deploy of the SAME image tag as the last record
      (a second run of the same image) is always allowed. --critical needs a non-empty reason and skips both checks.
  deploylog.py record --tag TAG [--services legion,legion-core] [--critical REASON] [--fix "text|metric|cohort"]...
      appends to legion/data/deploys.jsonl: t, epoch, tag, services, critical, reason, fixes[{text,metric,cohort}].
      A record for the same tag without --fix copies the fixes of the earlier record of that tag.
  deploylog.py rathena-check [--override REASON]   exit 5 if a rathena recreate was recorded in the last hour
  deploylog.py rathena-record [--override REASON]  appends to legion/data/rathena_recreates.jsonl
  deploylog.py last                                 prints the last deploy record

LEGION_FAKE_NOW pins the clock (tests); LEGION_DATA moves the data dir.
"""
import argparse
import json
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import common as C

DEPLOYS = "deploys.jsonl"
RATHENA = "rathena_recreates.jsonl"
MIN_GAP = 3 * 3600
RATHENA_GAP = 3600
REFUSED = 5


def _epoch(r):
    return C.to_epoch(r.get("epoch", r.get("t"))) or 0.0


def deploy_records():
    """The deploy records only: kind "verdict" lines (league.py, keep or revert of a past deploy) are not deploys."""
    return [r for r in C.read_jsonl(C.dpath(DEPLOYS)) if r.get("kind") != "verdict"]


def last_record():
    recs = deploy_records()
    return max(recs, key=_epoch) if recs else None


def _verdict_previous(tag):
    """Deploy flow step (Constitution 3.6): before a new deploy is recorded, the previous deploy's 3-hour windows are
    compared and the keep/revert verdict is written to deploys.jsonl. The core step (same tag) is skipped: its window
    has not run yet. A failure is reported, never blocks the deploy record."""
    last = last_record()
    if not last or last.get("tag") == tag:
        return None
    try:
        import league
        rec = league.record_verdict()
        if rec:
            print("verdict for %s: %s" % (rec.get("tag"), rec.get("verdict")))
        return rec
    except Exception as e:  # the verdict is measurement, not a gate
        sys.stderr.write("WARNING: deploy verdict not recorded: %s\n" % e)
        return None


def check(min_gap=MIN_GAP, critical=None, tag=None, fixes=0):
    """-> (ok, message)"""
    if critical is not None:
        if not str(critical).strip():
            return False, "REFUSED: --critical needs a reason"
        return True, "critical deploy allowed: %s" % critical
    last = last_record()
    if tag and last and last.get("tag") == tag:
        return True, "same image as the last deploy (%s): allowed" % tag
    if last:
        age = C.now() - _epoch(last)
        if age < min_gap:
            wait = int(min_gap - age)
            return False, ("REFUSED: last deploy %s (%s) was %d min ago; the next regular deploy is in %d min (%s). "
                           "Use --critical \"<reason>\" only for a fix that unblocks something." % (
                               last.get("tag", "?"), C.iso(_epoch(last)), age // 60, wait // 60 + 1, C.iso(_epoch(last) + min_gap)))
    if fixes <= 0:
        return False, ("REFUSED: no --fix \"<what>|<metric kpm|zph|deaths>|<cohort>\" given. Every regular deploy lists "
                       "its fixes and the metric each should move, so the next window can be measured.")
    return True, "deploy allowed (%s)" % ("first deploy" if not last else "3-hour gap passed")


def parse_fix(s):
    p = [x.strip() for x in s.split("|")]
    return {"text": p[0], "metric": (p[1] if len(p) > 1 and p[1] else "kpm"), "cohort": p[2] if len(p) > 2 else ""}


def record(tag, services, critical, fixes):
    _verdict_previous(tag)
    recs = deploy_records()
    if not fixes:
        for r in recs:
            if r.get("tag") == tag and r.get("fixes"):
                fixes = r["fixes"]
    rec = {"t": C.iso(), "epoch": int(C.now()), "tag": tag, "services": services, "critical": critical is not None,
           "reason": critical or "", "fixes": fixes}
    C.append_jsonl(C.dpath(DEPLOYS), rec)
    return rec


def rathena_check(override=None):
    if override:
        return True, "hourly rathena limit overridden: %s" % override
    recs = C.read_jsonl(C.dpath(RATHENA))
    if recs:
        last = max(_epoch(r) for r in recs)
        age = C.now() - last
        if age < RATHENA_GAP:
            return False, ("REFUSED: rathena was recreated %d min ago (%s); at most one recreate per hour (owner rule). "
                           "Next allowed %s. Override only if rathena is broken: RATHENA_HOUR_OVERRIDE=\"<reason>\"." % (
                               age // 60, C.iso(last), C.iso(last + RATHENA_GAP)))
    return True, "rathena recreate allowed"


def main(argv=None):
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    sub = ap.add_subparsers(dest="cmd", required=True)
    s = sub.add_parser("check")
    s.add_argument("--min-gap", type=int, default=MIN_GAP)
    s.add_argument("--critical")
    s.add_argument("--tag")
    s.add_argument("--fixes", type=int, default=0)
    s = sub.add_parser("record")
    s.add_argument("--tag", required=True)
    s.add_argument("--services", default="legion")
    s.add_argument("--critical")
    s.add_argument("--fix", action="append", default=[])
    for n in ("rathena-check", "rathena-record"):
        s = sub.add_parser(n)
        s.add_argument("--override")
    sub.add_parser("last")
    a = ap.parse_args(argv)
    if a.cmd == "check":
        ok, msg = check(a.min_gap, a.critical, a.tag, a.fixes)
        print(msg)
        return 0 if ok else REFUSED
    if a.cmd == "record":
        r = record(a.tag, [x for x in a.services.split(",") if x], a.critical, [parse_fix(x) for x in a.fix])
        print("recorded deploy %s (%d fixes%s)" % (r["tag"], len(r["fixes"]), ", critical" if r["critical"] else ""))
        return 0
    if a.cmd == "rathena-check":
        ok, msg = rathena_check(a.override)
        print(msg)
        return 0 if ok else REFUSED
    if a.cmd == "rathena-record":
        C.append_jsonl(C.dpath(RATHENA), {"t": C.iso(), "epoch": int(C.now()), "override": a.override or ""})
        print("recorded rathena recreate")
        return 0
    if a.cmd == "last":
        print(json.dumps(last_record() or {}, ensure_ascii=False))
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except SystemExit:
        raise
    except Exception as e:  # a broken guard must not silently allow: fail the deploy loudly
        sys.stderr.write("deploylog error: %s\n" % e)
        sys.exit(9)
