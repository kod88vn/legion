#!/usr/bin/env python3
"""gatecheck.py - verify the owner's hard gates are still in the always-loaded core.

  python tools/system/gatecheck.py              gates.md IDs must all appear in RULES.md (the core)
  python tools/system/gatecheck.py --coverage   also: every rule ID of coverage.md appears in its new file
                                                 and in archive.md (the owner's words)
Both modes also run tools/system/constcheck.py: every AI prompt carries the whole constitution, first, once.
Every run also runs every Python test suite (each folder with a test_*.py file, found by walking the repo, each in
its own process); a failing suite fails the check. --no-suites skips them (the suites take about 3 minutes).
Exit code 0 = ok, 1 = something is missing. Changes nothing.
"""
import os
import re
import subprocess
import sys

ROOT = os.path.normpath(os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", ".."))
SUITE_SKIP = {"__pycache__", "data", "node_modules"}


def read(rel):
    with open(os.path.join(ROOT, rel), encoding="utf-8") as f:
        return f.read()


def suite_dirs():
    """every folder under the repo (relative) that holds a test_*.py file: a new suite is found without listing it"""
    found = set()
    for dp, dns, fns in os.walk(ROOT):
        dns[:] = [d for d in dns if d not in SUITE_SKIP and not d.startswith(".")]
        if any(re.match(r"test_.*\.py$", f) for f in fns):
            found.add(os.path.relpath(dp, ROOT).replace(os.sep, "/"))
    return sorted(found)


def run_suites():
    """run each suite folder in its own process (module names cannot clash); returns the folders that fail"""
    failed = []
    for d in suite_dirs():
        r = subprocess.run([sys.executable, "-W", "ignore", "-m", "unittest", "discover", "-s", d, "-p", "test_*.py"],
                           cwd=ROOT, capture_output=True, text=True)
        lines = r.stderr.strip().splitlines()
        ran = next((l for l in lines if l.startswith("Ran ")), "no tests ran")
        status = lines[-1] if lines else "no output"
        print("suite %s: %s, %s" % (d, ran, status))
        if r.returncode:
            failed.append(d)
    return failed


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

    # the map truth: the repo's field files and portal patches against the server snapshot (tools/dev/mapsnap, refreshed
    # when older than a day or the server image changed). Repo only and fast; the containers are the watchdog's job.
    # No server data is a WARNING and a skip, not a failure.
    try:
        import fieldcheck
        probs, infos = fieldcheck.run_all(containers=[])
        for i in infos:
            if i.get("source") == "server" and i.get("warning"):
                print("WARNING: fields: " + i["warning"])
        by_src = {}
        for p in probs:
            by_src.setdefault(p["source"], []).append(p)
        for src, ps in sorted(by_src.items()):
            maps = sorted({p["map"] for p in ps})
            bad.append("fieldcheck %s: %d mismatch(es) in %d map(s): %s%s" % (
                src, len(ps), len(maps), ", ".join(maps[:12]), " ..." if len(maps) > 12 else ""))
        print("fields: %d mismatch(es) in the repo copies" % len(probs))
    except OSError as e:  # no server data at all: say so, skip
        print("WARNING: fields not checked: %s" % e)
    except Exception as e:  # a check that cannot run is a failed check
        bad.append("fieldcheck could not run: %r" % (e,))

    # every Python suite (PR-tests): a suite that fails, or cannot run, fails the check
    if "--no-suites" not in sys.argv:
        for d in run_suites():
            bad.append("python suite failed: %s (run: python -m unittest discover -s %s -p \"test_*.py\")" % (d, d))

    for b in bad:
        print("FAIL: " + b)
    print("OK" if not bad else "%d problem(s)" % len(bad))
    return 1 if bad else 0


if __name__ == "__main__":
    sys.exit(main())
