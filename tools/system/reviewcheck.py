#!/usr/bin/env python3
"""Review gate (Constitution 4.2): every fix a deploy ships needs a reviewer report that says merge.

  reviewcheck.py check --fixes N [--reviewed COMMIT]...
      exit 0 = each of the N fixes has a review, exit 5 = refused (nothing deployed). Needs exactly one --reviewed
      per --fix. COMMIT is the fix's commit (any git rev). Its review is docs/reports/reviews/<full sha>.md and must
      have `verdict: merge` (or `verdict: ok`) as its last non-empty line. `verdict: do not merge` or a missing file is refused.

Called by tools/deploy/deploy.py (gate_review) before every deploy, critical ones included.
The verdict must be the last non-empty line of the review file.
Standard library only. Tests: python -m unittest discover -s tests/system -v (from the repo root).
"""
import argparse
import os
import re
import subprocess
import sys

ROOT = os.path.normpath(os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", ".."))
REVIEWS = os.path.join(ROOT, "docs", "reports", "reviews")
VERDICT = re.compile(r"^verdict:\s*(merge|ok)\s*$", re.I)


def verdict(text):
    """'merge' or 'ok' when the LAST non-empty line of the report is the verdict, else None. A verdict line quoted
    earlier in the text, or followed by more text, does not count."""
    lines = [l.strip() for l in text.splitlines() if l.strip()]
    m = VERDICT.match(lines[-1]) if lines else None
    return m.group(1).lower() if m else None


def resolve(commit):
    """Full sha of a commit in the repo, or None."""
    out = subprocess.run(["git", "-C", ROOT, "rev-parse", "--verify", "-q", commit + "^{commit}"],
                         capture_output=True, text=True)
    return out.stdout.strip() if out.returncode == 0 else None


def check(fixes, reviewed, reviews_dir=REVIEWS, resolve_fn=resolve):
    """(ok, message). One reviewed commit per fix; each must have a merge verdict in its review file."""
    if len(reviewed) != fixes:
        return False, (f"REFUSED: {fixes} fix(es) but {len(reviewed)} --reviewed commit(s); "
                       "every fix needs its reviewed commit")
    bad, lines = [], []
    for c in reviewed:
        sha = resolve_fn(c)
        if not sha:
            bad.append(f"{c}: not a commit")
            continue
        try:
            with open(os.path.join(reviews_dir, sha + ".md"), encoding="utf-8") as f:
                text = f.read()
        except OSError:
            bad.append(f"{sha[:12]}: no review (docs/reports/reviews/{sha}.md)")
            continue
        if verdict(text) is None:
            bad.append(f"{sha[:12]}: review does not say 'verdict: merge'")
            continue
        lines.append(f"review {sha[:12]} {verdict(text)}")
    if bad:
        return False, "REFUSED: no merge review for:\n  " + "\n  ".join(bad)
    return True, "\n".join(lines) if lines else "no fixes to review"


def main(argv=None):
    p = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    sub = p.add_subparsers(dest="cmd", required=True)
    c = sub.add_parser("check")
    c.add_argument("--fixes", type=int, default=0)
    c.add_argument("--reviewed", action="append", default=[])
    a = p.parse_args(argv)
    ok, msg = check(a.fixes, a.reviewed)
    print(msg)
    return 0 if ok else 5


if __name__ == "__main__":
    sys.exit(main())
