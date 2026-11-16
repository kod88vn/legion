#!/usr/bin/env python3
"""runsuites.py - run the Python test suites once, with hard timeouts, in a bounded number of parallel processes.

  python tools/system/runsuites.py              every Python suite (every folder with a test_*.py file)
  python tools/system/runsuites.py --changed    the always-run core (tests/system) plus the suites that mention a
                                                changed file's name; any change to a shared module (testenv.py,
                                                common.py, runsuites.py, gatecheck.py) runs every suite
  python tools/system/runsuites.py --go         also tools/dev/gocheck.sh (full Go tests); with --changed, Go is
                                                run only when a Go file changed (fast: gofmt, vet, build)
  options: --workers N (default min(4, CPUs); env LEGION_SUITE_WORKERS), --timeout S (default 600; env
  LEGION_SUITE_TIMEOUT), --base REF (for --changed, default main), --list (print the suite folders and exit)

Every suite runs in its own child process with the tripwire environment (rule G23): LEGION_TEST=1, LEGION_DATA and
the other resolver overrides unset, LEGION_GUARD_CHILD=1 (so the guard test in tests/system does not run every suite a
second time: this runner is the guard's run). A child that exceeds the timeout is killed with its whole process tree
and reported as a failure with the tail of its output. Output goes to temp files, not pipes, so a grandchild that
keeps a pipe open cannot stall the runner.

Exit code 0 = all run suites passed, 1 = a suite failed, timed out or could not run, 2 = usage error.
Changes nothing in the repo; writes nothing outside temp files.
"""
import concurrent.futures
import os
import re
import shutil
import signal
import subprocess
import sys
import tempfile
import time

ROOT = os.path.normpath(os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", ".."))
SUITE_SKIP = {"__pycache__", "data", "node_modules"}
SUITE_CORE = "tests/system"
SHARED = ("tests/testenv.py", "tools/system/common.py", "tools/system/runsuites.py", "tools/system/gatecheck.py")
RESOLVER_OVERRIDES = ("LEGION_DATA", "AYLA_ORDERS", "AYLA_ORDERS_LOG", "LEGION_BUGQ_EXTRA")
DEFAULT_TIMEOUT = 600


class Result(object):
    def __init__(self, suite, rc, out, err, secs, timed_out):
        self.suite, self.rc, self.out, self.err, self.secs, self.timed_out = suite, rc, out, err, secs, timed_out

    @property
    def ok(self):
        return self.rc == 0 and not self.timed_out

    def ran(self):
        m = re.search(r"^Ran (\d+) tests? in ([\d.]+)s", self.err, re.M)
        return "Ran %s tests" % m.group(1) if m else "no test count"

    def status(self):
        lines = self.err.strip().splitlines()
        return lines[-1] if lines else "no output"

    def tail(self, n=700):
        return (self.err or self.out or "")[-n:].strip()


def _timeout_default():
    try:
        return float(os.environ.get("LEGION_SUITE_TIMEOUT", DEFAULT_TIMEOUT))
    except ValueError:
        return DEFAULT_TIMEOUT


LIVE_FOLDERS = ("legion/data", "openkore-ayla/logs")    # the live state of this checkout (common.py resolves the same)


def _tracked_live_files(root):
    """the git-tracked files under the live folders: committed config, the only live-folder files a test may read"""
    try:
        r = subprocess.run(["git", "-C", root, "ls-files", "--"] + list(LIVE_FOLDERS), capture_output=True, text=True,
                           timeout=60)
    except (OSError, subprocess.TimeoutExpired):
        return []
    if r.returncode:
        return []
    return [os.path.join(root, p.replace("/", os.sep)) for p in r.stdout.splitlines() if p.strip()]


def tripwire_env(root=None, **extra):
    """the environment of a suite run under the G23 tripwire: the parent's env without the path overrides, LEGION_TEST=1,
    LEGION_GUARD_CHILD=1, and the audit tripwire (tools/system/tripwire/sitecustomize.py) on PYTHONPATH, which refuses
    every open, write, rename or remove under the live folders, in every process of the run (a grandchild included)"""
    root = root or ROOT
    env = {k: v for k, v in os.environ.items() if k not in RESOLVER_OVERRIDES}
    env["LEGION_TEST"] = "1"
    env["LEGION_GUARD_CHILD"] = "1"
    env["LEGION_TRIPWIRE_LIVE"] = os.pathsep.join(os.path.join(root, p.replace("/", os.sep)) for p in LIVE_FOLDERS)
    env["LEGION_TRIPWIRE_READ"] = os.pathsep.join(_tracked_live_files(root))
    tw = os.path.join(os.path.dirname(os.path.abspath(__file__)), "tripwire")
    env["PYTHONPATH"] = os.pathsep.join([tw] + ([env["PYTHONPATH"]] if env.get("PYTHONPATH") else []))
    env.update(extra)
    return env


def suite_dirs(root=None):
    """every folder under the repo (relative) that holds a test_*.py file: a new suite is found without listing it"""
    root = root or ROOT
    found = set()
    for dp, dns, fns in os.walk(root):
        dns[:] = [d for d in dns if d not in SUITE_SKIP and not d.startswith(".")]
        if any(re.match(r"test_.*\.py$", f) for f in fns):
            found.add(os.path.relpath(dp, root).replace(os.sep, "/"))
    return sorted(found)


def _read_tmp(f):
    f.seek(0)
    return f.read().decode("utf-8", "replace")


def kill_tree(p):
    """kill the process and everything it started (Windows: taskkill /T; POSIX: its session's process group)"""
    if os.name == "nt":
        subprocess.run(["taskkill", "/F", "/T", "/PID", str(p.pid)], capture_output=True)
    else:
        try:
            os.killpg(p.pid, signal.SIGKILL)
        except OSError:
            p.kill()


def run_bounded(cmd, cwd, env, timeout):
    """run cmd until it exits or timeout seconds pass; returns a Result-like (rc, out, err, secs, timed_out).
    The output goes to temp files, so a grandchild that outlives the child cannot hold the runner open."""
    kw = {"start_new_session": True} if os.name != "nt" else {}
    with tempfile.TemporaryFile() as fo, tempfile.TemporaryFile() as fe:
        t0 = time.time()
        p = subprocess.Popen(cmd, cwd=cwd, env=env, stdin=subprocess.DEVNULL, stdout=fo, stderr=fe, **kw)
        timed_out = False
        try:
            p.wait(timeout=timeout)
        except subprocess.TimeoutExpired:
            timed_out = True
            kill_tree(p)
            p.wait()
        secs = time.time() - t0
        return p.returncode, _read_tmp(fo), _read_tmp(fe), secs, timed_out


def run_suite(suite, root=None, timeout=None):
    """one suite in its own child process under the tripwire; a hang becomes a failed Result"""
    root = root or ROOT
    timeout = timeout or _timeout_default()
    cmd = [sys.executable, "-W", "ignore", "-m", "unittest", "discover", "-s", suite, "-t", suite, "-p", "test_*.py"]
    rc, out, err, secs, timed_out = run_bounded(cmd, root, tripwire_env(root), timeout)
    if timed_out:
        err = ("TIMEOUT: suite %s did not finish in %d s; the child process tree was killed. A hang, not a pass.\n"
               % (suite, timeout)) + err
    return Result(suite, rc if not timed_out else -9, out, err, secs, timed_out)


def run_all(suites, root=None, workers=None, timeout=None):
    """the suites, at most `workers` at a time; results come back in the order of `suites`"""
    workers = workers or _workers()
    if not suites:
        return []
    with concurrent.futures.ThreadPoolExecutor(max_workers=max(1, min(workers, len(suites)))) as ex:
        futs = [ex.submit(run_suite, s, root, timeout) for s in suites]
        return [f.result() for f in futs]


def describe(r):
    """one line per suite for a report, and the failure text when it failed"""
    if r.timed_out:
        return "suite %s: TIMEOUT after %.0f s (child killed): %s" % (r.suite, r.secs, r.tail(300))
    return "suite %s: %s, %s (%.1f s)" % (r.suite, r.ran(), r.status(), r.secs)


def _workers():
    try:
        return max(1, int(os.environ.get("LEGION_SUITE_WORKERS", "0")))
    except ValueError:
        return max(1, min(4, os.cpu_count() or 2))


def _changed_paths(base):
    """repo-relative paths changed against base (committed and not yet committed), or None when git cannot say"""
    try:
        a = subprocess.run(["git", "-C", ROOT, "diff", "--name-only", base], capture_output=True, text=True, timeout=60)
        b = subprocess.run(["git", "-C", ROOT, "status", "--porcelain"], capture_output=True, text=True, timeout=60)
    except (OSError, subprocess.TimeoutExpired):
        return None
    if a.returncode or b.returncode:
        return None
    paths = set(a.stdout.split())
    for line in b.stdout.splitlines():
        if len(line) > 3:
            paths.add(line[3:].split(" -> ")[-1].strip().strip('"'))
    return sorted(p.replace("\\", "/") for p in paths)


def select_for_changed(changed, suites, root=None):
    """(suites to run, reason lines). A change to a shared module runs everything; otherwise the core plus every
    suite whose files mention the changed file's name (so a suite that imports or runs it is found)."""
    root = root or ROOT
    if any(c in SHARED for c in changed):
        return list(suites), ["shared module changed: every suite"]
    chosen = {SUITE_CORE} & set(suites)
    reasons = []
    texts = {}
    for s in suites:
        buf = []
        for dp, _dns, fns in os.walk(os.path.join(root, s)):
            for f in fns:
                if f.endswith((".py", ".pl", ".sh", ".js")):
                    try:
                        with open(os.path.join(dp, f), encoding="utf-8", errors="replace") as fh:
                            buf.append(fh.read())
                    except OSError:
                        pass
        texts[s] = "\n".join(buf)
    for c in changed:
        if c.startswith("legion/") and c.endswith(".go"):
            continue                                    # Go is handled by gocheck, not by a Python suite
        if c.startswith("docs/") or c.endswith(".md"):
            continue
        if c in suites or any(c.startswith(s + "/") for s in suites):
            hit = [s for s in suites if c == s or c.startswith(s + "/")]
            chosen.update(hit)
            reasons.append("%s: its own suite" % c)
            continue
        name = os.path.splitext(os.path.basename(c))[0]
        if not name:
            continue
        hit = [s for s in suites if re.search(r"(?<![\w])%s(?![\w])" % re.escape(name), texts[s])]
        chosen.update(hit)
        reasons.append("%s: mentioned by %s" % (c, ", ".join(hit) if hit else "no suite (core only)"))
    return [s for s in suites if s in chosen], reasons


def go_check(full, root=None):
    """tools/dev/gocheck.sh through the Git Bash that runs this shell; a missing bash is a failure, not a pass"""
    root = root or ROOT
    script = os.path.join(root, "tools", "dev", "gocheck.sh")
    bash = shutil.which("bash")
    if not bash:
        return Result("go", 2, "", "no bash on PATH: gocheck.sh was not run (nothing was checked)", 0, False)
    cmd = [bash, script] + ([] if full else ["--fast"])
    rc, out, err, secs, timed_out = run_bounded(cmd, root, dict(os.environ), _timeout_default())
    if timed_out:
        err = "TIMEOUT: gocheck.sh did not finish in %d s; killed.\n" % _timeout_default() + err
    return Result("go" + ("" if full else " (fast)"), -9 if timed_out else rc, out, err, secs, timed_out)


def main(argv):
    args = list(argv)

    def take(flag, default=None):
        if flag in args:
            i = args.index(flag)
            if i + 1 >= len(args):
                print("usage error: %s needs a value" % flag)
                sys.exit(2)
            v = args[i + 1]
            del args[i:i + 2]
            return v
        return default

    workers = take("--workers")
    timeout = take("--timeout")
    base = take("--base", "main")
    changed_mode = "--changed" in args
    go_full = "--go" in args
    list_only = "--list" in args
    known = {"--changed", "--go", "--list"}
    unknown = [a for a in args if a not in known]
    if unknown:
        print("usage error: unknown option(s): %s" % " ".join(unknown))
        return 2
    if workers is not None:
        os.environ["LEGION_SUITE_WORKERS"] = workers
    if timeout is not None:
        os.environ["LEGION_SUITE_TIMEOUT"] = timeout

    suites = suite_dirs()
    if list_only:
        print("\n".join(suites))
        return 0

    reasons = []
    go_changed = False
    if changed_mode:
        changed = _changed_paths(base)
        if changed is None:
            reasons.append("git could not list the changed files: running every suite")
            run = suites
        else:
            go_changed = any(c.startswith("legion/") and c.endswith(".go") for c in changed)
            run, reasons = select_for_changed(changed, suites)
            if not changed:
                reasons.append("no changed files against %s: the core only" % base)
    else:
        run = suites
    print("suites (%d of %d): %s" % (len(run), len(suites), ", ".join(run)))
    for r in reasons:
        print("  why: " + r)
    print("tripwire (G23): every suite runs with LEGION_TEST=1 and LEGION_DATA unset; the guard test in %s "
          "is this run" % SUITE_CORE)

    t0 = time.time()
    results = run_all(run, ROOT)
    for r in results:
        print(describe(r))
    failed = [r for r in results if not r.ok]
    for r in failed:
        print("--- %s output tail ---\n%s" % (r.suite, r.tail()))

    if go_full or (changed_mode and go_changed):
        g = go_check(full=go_full)
        print("go: %s (%.1f s): %s" % (g.suite, g.secs, "ok" if g.ok else "FAILED"))
        if not g.ok:
            print(g.tail())
            failed.append(g)
    print("total %.1f s, %d failed" % (time.time() - t0, len(failed)))
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
