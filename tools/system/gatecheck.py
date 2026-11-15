#!/usr/bin/env python3
"""gatecheck.py - verify the owner's hard gates are still in the always-loaded core.

  python tools/system/gatecheck.py              gates.md IDs must all appear in RULES.md (the core)
  python tools/system/gatecheck.py --coverage   also: every rule ID of coverage.md appears in its new file
                                                 and in archive.md (the owner's words)
Both modes also run tools/system/constcheck.py: every AI prompt carries the whole constitution, first, once.
Exit code 0 = ok, 1 = something is missing. Reads only; changes nothing.
"""
import os
import re
import sys

ROOT = os.path.normpath(os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", ".."))


def read(rel):
    with open(os.path.join(ROOT, rel), encoding="utf-8") as f:
        return f.read()


def main():
    bad = []
    core = read("RULES.md")
    gates = re.findall(r"^\| (G\d\d) \|", read("docs/rules/gates.md"), re.M)
    if not gates:
        bad.append("no gate IDs found in docs/rules/gates.md")
    for g in gates:
        if not re.search(r"\b%s\b" % g, core):
            bad.append("gate %s is missing from RULES.md (the always-loaded core)" % g)
    print("gates in register: %d" % len(gates))

    if "--coverage" in sys.argv:
        archive = read("docs/rules/archive.md")
        rows = re.findall(r"^\| ([A-Z]{2}\d\d) \| .*? \| (.*?) \| .*? \| (.*?) \| .*? \|$", read("docs/rules/coverage.md"), re.M)
        for rid, kind, loc in rows:
            if not re.search(r"\b%s\b" % rid, archive):
                bad.append("%s is missing from archive.md" % rid)
            files = re.findall(r"(docs/[\w/.-]+\.md|RULES\.md)", loc)
            if not files:
                bad.append("%s has no new location" % rid)
                continue
            if not any(re.search(r"\b%s\b" % rid, read(f)) for f in files if os.path.exists(os.path.join(ROOT, f))):
                bad.append("%s not found in %s" % (rid, ", ".join(files)))
        print("coverage rows checked: %d" % len(rows))

    # the constitution: first in every prompt, exactly once, loaded at run time (PR42); details: constcheck.py
    try:
        sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
        import constcheck
        rep, head = constcheck.run()
        print(head)
        print("constitution: %d prompt source(s) checked, %d warning(s)" % (len(rep.rows), len(rep.warn)))
        bad += ["constcheck: " + x for x in rep.bad]
    except Exception as e:  # a check that cannot run is a failed check
        bad.append("constcheck could not run: %r" % (e,))

    # the map truth: every OpenKore field file and portal line against the server's map cache and loaded warps
    # (fieldcheck.py: the repo copies, plus every OpenKore container that exists, stopped ones too; read-only)
    try:
        import fieldcheck
        probs, infos = fieldcheck.run_all()
        print("fields: " + "; ".join(i.get("label", i.get("source", "?")) for i in infos[:1]))
        by_src = {}
        for p in probs:
            by_src.setdefault(p["source"], []).append(p)
        for src, ps in sorted(by_src.items()):
            maps = sorted({p["map"] for p in ps})
            bad.append("fieldcheck %s: %d mismatch(es) in %d map(s): %s%s" % (
                src, len(ps), len(maps), ", ".join(maps[:12]), " ..." if len(maps) > 12 else ""))
        print("fields: %d mismatch(es)" % len(probs))
    except Exception as e:  # a check that cannot run is a failed check
        bad.append("fieldcheck could not run: %r" % (e,))

    for b in bad:
        print("FAIL: " + b)
    print("OK" if not bad else "%d problem(s)" % len(bad))
    return 1 if bad else 0


if __name__ == "__main__":
    sys.exit(main())
