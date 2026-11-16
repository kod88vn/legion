#!/usr/bin/env python3
"""deploy.py - the Legion deploy batch as one script (rule PR47: an agent that only runs an algorithm is a script).

  python3 tools/deploy/deploy.py --fix "<what>|<metric>|<cohort>" --reviewed <commit> [--fix ... --reviewed ...]
                                 [--critical "<reason>"] [--services legion,legion-core]
                                 [--apply] [--owner-lifts-hold]
  python3 tools/deploy/deploy.py compare      keep/revert list of the last deploy (league.py --compare-deploy)

One --fix per fix, and one --reviewed <commit> per --fix (the fix's commit, in the same order). metric: kpm, zph or
deaths. WSL: `wsl -d Ubuntu-24.04 -- python3 /mnt/g/RO/legion/tools/deploy/deploy.py ...`

DRY RUN IS THE DEFAULT: every gate and every read-only check runs and the plan is printed; nothing is built, started,
stopped, written or recorded. `--apply` runs the batch. Nothing here decides anything: every rule is a check in code.

Gates, in order; a refusal stops before anything is changed:
  services  only legion and legion-core, always with --no-deps (G15)                              exit 3
  review    every fix has a --reviewed commit with a `verdict: merge` review (Constitution 4.2)   exit 5
  interval  3 hours between regular deploys (G17) unless --critical "<reason>"; >= 1 fix          exit 5
  hold      legion/data/stack.hold: the owner has the stack stopped (G21); only --owner-lifts-hold exit 6
  lock      one deploy run at a time (legion/data/deploy.lock)                                   exit 7
  rathena   db and rathena must not change (compose config hash and compose's own dry-run, G15) exit 2
  rathena recreate: this script never recreates rathena or db (G15, G16): tools/deploy/rathena_recreate.sh only.
Then (--apply): stray golang test/build containers are stopped (G24: never rathena, Ayla, legion, legion-core, db);
gofmt, go vet and go test run on a frozen snapshot of legion/, and the image is built from that same snapshot
(failure: exit 1, nothing deployed); a pre-deploy league snapshot is saved; `up --no-deps` runs for legion, then
legion-core, both pinned to the one image (exit 1 if a service runs another image); the deploy is recorded in
legion/data/deploys.jsonl; Ayla must be back in game within 5 minutes (G19, exit 8).
`compare` (3 hours after a deploy, or at the next one) writes the keep/revert list of the last deploy.

Exit codes: 0 ok (or dry run ok), 1 build/up/verify failed, 2 rathena/db would change (or a usage error, argparse),
3 service not allowed, 5 review/interval/no fix/bad fix, 6 stack hold, 7 lock held, 8 Ayla not back in time.
Every run writes legion/data/deploy-runs/<stamp>/ (run.log, pre-league.md, override.yml, revert.txt) when applied.
Standard library only. Tests: python -m unittest discover -s tests/system -p test_deploy.py (from the repo root).
"""
import argparse
import json
import os
import re
import shutil
import subprocess
import sys
import tempfile
import time

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.normpath(os.path.join(HERE, "..", ".."))
SYS = os.path.join(ROOT, "tools", "system")
sys.path.insert(0, SYS)
import common as C          # noqa: E402
import deploylog            # noqa: E402
import reviewcheck          # noqa: E402

LEAGUE = os.path.join(SYS, "league.py")
GO_IMAGE = "golang:1.22"
ALLOWED = ("legion", "legion-core")
CONTAINER = {"legion": "ro-legion", "legion-core": "ro-legion-core", "db": "ro-db", "rathena": "ro-rathena"}
PROTECTED = {"ro-rathena", "ro-db", "ro-openkore", "ro-openkore-ayla", "ro-legion", "ro-legion-core", "ro-agency",
             "ro-mastermind"}
# Tokens no compose `up` of this script may carry (rathena, db and the services that share rathena's network).
FORBIDDEN_TOKENS = {"rathena", "ro-rathena", "db", "ro-db", "openkore", "ro-openkore", "openkore-ayla",
                    "ro-openkore-ayla", "agency", "ro-agency", "mastermind", "ro-mastermind"}
AYLA = "ro-openkore-ayla"
AYLA_WINDOW_S = 300
AYLA_POLL_S = 5
METRICS = ("kpm", "zph", "deaths")
SNAPSHOT_FILES = ("Dockerfile", "go.mod", "recvpackets.txt")

OK, FAILED, DRIFT, SERVICE, GATE, HOLD, LOCKED, AYLA_LATE = 0, 1, 2, 3, 5, 6, 7, 8


class Refused(Exception):
    """A gate or a failed step: the message is printed and the exit code returned. Nothing after it runs."""

    def __init__(self, code, msg):
        super().__init__(msg)
        self.code, self.msg = code, msg


def execute(argv, cwd=ROOT):
    """Default runner: (returncode, combined output). Tests inject a fake."""
    try:
        p = subprocess.run(argv, cwd=cwd, capture_output=True, text=True)
    except OSError as e:
        return 127, str(e)
    return p.returncode, (p.stdout or "") + (p.stderr or "")


def up_argv(svc, override):
    """The one compose `up` this script runs: a single service, --no-deps, pinned by the override file."""
    return ["docker", "compose", "-f", "docker-compose.yml", "-f", override, "up", "-d", "--no-deps", "--no-build",
            "--pull", "never", svc]


def compose_argv(*args):
    return ["docker", "compose"] + list(args)


def parse_fix(text):
    """'what|metric|cohort' -> dict. Refuses a metric league.py cannot measure."""
    f = deploylog.parse_fix(text)
    if not f["text"]:
        raise Refused(GATE, "REFUSED: a --fix needs a text: \"<what>|<metric>|<cohort>\"")
    if f["metric"].lower() not in METRICS:
        raise Refused(GATE, "REFUSED: fix %r has metric %r; use kpm, zph or deaths" % (f["text"], f["metric"]))
    f["metric"] = f["metric"].lower()
    return f


def revert_items(tag):
    """The REVERT items of the newest keep/revert verdict written for this deploy tag (league.py records them)."""
    recs = [r for r in C.read_jsonl(C.dpath("deploys.jsonl")) if r.get("kind") == "verdict" and r.get("tag") == tag]
    if not recs:
        return None
    return [i for i in recs[-1].get("items", []) if str(i.get("verdict", "")).startswith("REVERT")]


def run_dir_name(tag):
    return "deploy-" + tag.split("deploy-")[-1]


class Deploy:
    """One deploy batch. Reads go through self.run (always executed); changes go through self.act (executed only with
    apply, printed otherwise). Tests inject `execute`, `sleep`, `reviews_dir` and `resolve`."""

    def __init__(self, root=ROOT, apply=False, execute_fn=None, sleep=time.sleep, reviews_dir=None, resolve=None,
                 out=print):
        self.root = root
        self.apply = apply
        self.execute = execute_fn or execute
        self.sleep = sleep
        self.reviews_dir = reviews_dir or reviewcheck.REVIEWS
        self.resolve = resolve or reviewcheck.resolve
        self.out = out
        self.logpath = None

    # ---------------------------------------------------------------- output and execution
    def say(self, msg):
        self.out(msg)
        if self.logpath:
            with open(self.logpath, "a", encoding="utf-8") as f:
                f.write(msg + "\n")

    def run(self, argv, cwd=None):
        """Read-only command: always executed."""
        return self.execute(argv, cwd or self.root)

    def act(self, argv, cwd=None):
        """Change: executed only with apply; in a dry run it is printed. G15 and G24 are checked here, in code."""
        if "compose" in argv and "up" in argv:
            if "--no-deps" not in argv or any(t in FORBIDDEN_TOKENS for t in argv):
                raise RuntimeError("refused to run a compose up that is not a --no-deps legion/legion-core deploy: %s"
                                   % " ".join(argv))
        if not self.apply:
            self.say("dry run, would run: " + " ".join(argv))
            return 0, ""
        return self.execute(argv, cwd or self.root)

    def tail(self, text, n=30):
        return "\n".join(text.splitlines()[-n:])

    # ---------------------------------------------------------------- gates
    def gate_review(self, fixes, reviewed):
        if not fixes:
            raise Refused(GATE, "REFUSED: no --fix \"<what>|<metric>|<cohort>\" given; every deploy lists its fixes")
        ok, msg = reviewcheck.check(len(fixes), reviewed, reviews_dir=self.reviews_dir, resolve_fn=self.resolve)
        if not ok:
            raise Refused(GATE, msg)
        return msg

    def gate_interval(self, critical, nfixes):
        ok, msg = deploylog.check(critical=critical, fixes=nfixes)
        if not ok:
            raise Refused(GATE, msg)
        return msg

    def gate_hold(self, lifted):
        hold = C.dpath("stack.hold")
        if os.path.exists(hold) and not lifted:
            raise Refused(HOLD, "REFUSED (G21): %s exists; the owner has the stack stopped. Deploy only when the owner "
                                "says so, and then pass --owner-lifts-hold." % hold)

    def gate_lock(self):
        path = C.dpath("deploy.lock")
        if os.path.exists(path):
            raise Refused(LOCKED, "REFUSED: another deploy run holds %s. If it died, remove that file by hand." % path)

    def hash_drift(self, svc):
        """Reason string when the container's compose config differs from the compose file, else None."""
        rc, out = self.run(compose_argv("config", "--hash", svc))
        parts = out.split()
        want = parts[1] if rc == 0 and len(parts) > 1 else ""
        if not want:
            return "%s: could not compute the compose config hash" % svc
        rc, have = self.run(["docker", "inspect", "-f", '{{index .Config.Labels "com.docker.compose.config-hash"}}',
                             CONTAINER[svc]])
        if rc != 0:
            return "%s: container %s does not exist" % (svc, CONTAINER[svc])
        if have.strip() != want:
            return "%s: compose config hash changed (running %s, compose %s)" % (svc, have.strip()[:12], want[:12])
        return None

    def gate_rathena(self, services):
        """G15: the batch must not change db or rathena. Fails closed: a compose error is a refusal."""
        why = []
        for svc in ("db", "rathena"):
            r = self.hash_drift(svc)
            if r:
                why.append(r)
        rc, out = self.run(compose_argv("up", "-d", "--no-build", "--dry-run", *services))
        if rc != 0:
            why.append("compose dry-run (with deps) failed: %s" % self.tail(out, 3))
        for line in out.splitlines():
            if re.search(r"Container (ro-rathena|ro-db)\b.*(Recreat|Creat)", line):
                why.append("compose dry-run (with deps) would change: " + line.strip())
        rc, out = self.run(compose_argv("up", "-d", "--no-deps", "--no-build", "--dry-run", *services))
        if rc != 0:
            why.append("compose dry-run (--no-deps) failed: %s" % self.tail(out, 3))
        for line in out.splitlines():
            m = re.search(r"Container (\S+)", line)
            if m and m.group(1) not in [CONTAINER[s] for s in services]:
                why.append("compose would touch %s: %s" % (m.group(1), line.strip()))
        if why:
            raise Refused(DRIFT, "rathena config changed or unknown; a Legion deploy does not recreate rathena (G15). "
                                 "Not deploying. An owner-approved rathena recreate is tools/deploy/rathena_recreate.sh "
                                 "--yes (once an hour at most, G16).\n  - " + "\n  - ".join(why))

    # ---------------------------------------------------------------- Ayla (G19)
    def ayla_status(self):
        try:
            with open(os.path.join(self.root, "openkore-ayla", "logs", "status.json"), encoding="utf-8") as f:
                return json.load(f)
        except (OSError, ValueError):
            return None

    def ayla_back(self, start_epoch, start_iso):
        s = self.ayla_status()
        try:
            fresh = s is not None and int(s.get("ts", 0) or 0) > start_epoch + 5
        except (TypeError, ValueError):
            fresh = False
        if s and s.get("online") and s.get("map") and fresh:
            return True
        rc, out = self.run(["docker", "logs", "--since", start_iso, AYLA])
        return "You are now in the game" in out

    def wait_ayla(self, start_epoch, start_iso):
        for _ in range(AYLA_WINDOW_S // AYLA_POLL_S):
            if self.ayla_back(start_epoch, start_iso):
                return True
            self.sleep(AYLA_POLL_S)
        return False

    # ---------------------------------------------------------------- G24: stray golang containers
    def stop_stray_golang(self):
        rc, out = self.run(["docker", "ps", "--format",
                            '{{.ID}}\t{{.Names}}\t{{.Image}}\t{{.Label "com.docker.compose.project"}}'])
        for line in out.splitlines():
            parts = line.split("\t")
            if len(parts) < 4:
                continue
            cid, name, image, project = parts[:4]
            if not image.startswith("golang") or project:
                continue  # not a golang build/test container, or a compose service
            if name in PROTECTED or name.startswith("ro-"):
                self.say("kept %s (%s): protected (G24)" % (name, image))
                continue
            self.say("stop stray golang container %s (%s)" % (name, image))
            self.act(["docker", "stop", cid])

    # ---------------------------------------------------------------- build and test (frozen snapshot)
    def copy_snapshot(self, dst):
        src = os.path.join(self.root, "legion")
        for n in os.listdir(src):
            if n in SNAPSHOT_FILES or n.endswith(".go") or n.endswith(".html"):
                shutil.copy2(os.path.join(src, n), dst)
        if not os.path.exists(os.path.join(dst, "Dockerfile")):
            raise Refused(FAILED, "no Dockerfile in %s" % src)

    def build(self, tag):
        """gofmt, go vet and go test on one snapshot of legion/, then docker build from that same snapshot."""
        for attempt in (1, 2):
            snap = tempfile.mkdtemp(prefix="legion-snap-")
            try:
                self.copy_snapshot(snap)
                self.say("snapshot %s (try %d)" % (snap, attempt))
                rc, out = self.act(["docker", "run", "--rm", "-v", snap + ":/src", "-w", "/src", GO_IMAGE, "sh", "-c",
                                    "gofmt -l . ; gofmt -w . && go vet ./... && go test -count=1 ./..."])
                if rc == 0:
                    rc, out = self.act(["docker", "build", "-t", tag, "-t", "ro-legion:latest", snap])
                    if rc != 0:
                        raise Refused(FAILED, "BUILD FAILED (docker build) - nothing deployed:\n" + self.tail(out))
                    self.say("built %s (= ro-legion:latest) from the tested snapshot" % tag)
                    return
                self.say("gofmt/vet/test failed on the snapshot:\n" + self.tail(out, 15))
            finally:
                shutil.rmtree(snap, ignore_errors=True)
            if attempt == 1:
                self.say("retry on a fresh snapshot in 60 s (a file may have been half-saved)")
                self.sleep(60)
        raise Refused(FAILED, "BUILD FAILED: go vet/test failed twice on fresh snapshots - nothing deployed")

    # ---------------------------------------------------------------- the batch
    def plan(self, tag, services, fixes, run_dir):
        self.say("DRY RUN - nothing is built, started, stopped, written or recorded. With --apply the batch would:")
        self.say("  1. stop stray golang test/build containers (G24, listed above)")
        self.say("  2. build %s from a snapshot of legion/: gofmt, go vet, go test in %s, then docker build" % (tag, GO_IMAGE))
        self.say("  3. save a pre-deploy league snapshot (league.py --stdout)")
        for svc in services:
            self.say("  4. up %s: %s" % (svc, " ".join(up_argv(svc, os.path.join(run_dir, "override.yml")))))
        self.say("  5. record the deploy in legion/data/deploys.jsonl (%d fix(es))" % len(fixes))
        self.say("  6. Ayla must be back in game within %d s (G19)" % AYLA_WINDOW_S)
        self.say("Rerun with --apply to do it.")

    def batch(self, fix_texts, reviewed, critical, services_raw, lifted):
        services = [s.strip() for s in services_raw.split(",") if s.strip()]
        bad = [s for s in services if s not in ALLOWED]
        if bad or not services:
            raise Refused(SERVICE, "REFUSED (G15): only %s may be deployed (got %s)"
                          % (", ".join(ALLOWED), ", ".join(bad) or "nothing"))
        fixes = [parse_fix(t) for t in fix_texts]
        self.say("review: " + self.gate_review(fix_texts, reviewed).replace("\n", "; "))
        self.say("interval: " + self.gate_interval(critical, len(fixes)))
        self.gate_hold(lifted)
        self.gate_lock()
        self.gate_rathena(services)
        self.say("guard: db and rathena unchanged; the batch touches only %s, each with --no-deps" % ", ".join(services))
        pre = self.ayla_status()
        self.say("Ayla before the deploy: %s" % ("online" if pre and pre.get("online") else "NOT online (G19 at risk)"))
        ts = time.strftime("%Y%m%d-%H%M%S", time.localtime(C.now()))
        tag = "ro-legion:deploy-%s" % ts
        run_dir = C.dpath(os.path.join("deploy-runs", "deploy-" + ts))
        if not self.apply:
            self.plan(tag, services, fixes, run_dir)
            return OK
        return self.apply_batch(tag, ts, run_dir, fix_texts, fixes, reviewed, critical, services)

    def apply_batch(self, tag, ts, run_dir, fix_texts, fixes, reviewed, critical, services):
        os.makedirs(run_dir, exist_ok=True)
        self.logpath = os.path.join(run_dir, "run.log")
        lock = C.dpath("deploy.lock")
        try:
            fd = os.open(lock, os.O_CREAT | os.O_EXCL | os.O_WRONLY)
        except FileExistsError:
            raise Refused(LOCKED, "REFUSED: another deploy run holds %s" % lock)
        try:
            os.write(fd, ("%d %s\n" % (os.getpid(), C.iso())).encode("utf-8"))
            os.close(fd)
            self.say("deploy %s on %s; critical: %s" % (tag, ", ".join(services), critical or "no"))
            for f in fixes:
                self.say("  fix: %s | %s | %s" % (f["text"], f["metric"], f["cohort"] or "all"))
            self.say("  reviewed: %s" % ", ".join(reviewed))
            self.stop_stray_golang()
            self.build(tag)
            self.gate_rathena(services)  # again: compose may have changed during the build
            rc, out = self.run([sys.executable, LEAGUE, "--stdout"])
            with open(os.path.join(run_dir, "pre-league.md"), "w", encoding="utf-8") as f:
                f.write(out)
            if rc:
                self.say("WARNING: pre-deploy league snapshot failed (meter blind?): " + self.tail(out, 3))
            override = os.path.join(run_dir, "override.yml")
            with open(override, "w", encoding="utf-8") as f:
                f.write("services:\n" + "".join('  %s:\n    image: "%s"\n' % (s, tag) for s in services))
            start_epoch = int(C.now())
            start_iso = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime(C.now()))
            for svc in services:
                rc, out = self.act(up_argv(svc, override))
                if rc:
                    raise Refused(FAILED, "UP FAILED for %s - nothing more deployed:\n%s" % (svc, self.tail(out)))
                self.say("up %s: %s" % (svc, self.tail(out, 2)))
            self.verify_images(services, tag)
            rec = deploylog.record(tag, services, critical, fixes)
            self.say("recorded deploy %s (%d fixes)" % (rec["tag"], len(rec["fixes"])))
            if not self.wait_ayla(start_epoch, start_iso):
                raise Refused(AYLA_LATE, "G19: Ayla is not back in game within %d s after the deploy. Look at "
                                         "`docker logs %s`. Never send ai, ai off or ai manual. The deploy stays "
                                         "recorded." % (AYLA_WINDOW_S, AYLA))
            self.say("Ayla is back in game within %d s" % AYLA_WINDOW_S)
            self.say("next: `deploy.py compare` 3 hours after this deploy (or at the next deploy) writes keep/revert")
            return OK
        finally:
            try:
                os.remove(lock)
            except OSError:
                pass

    def verify_images(self, services, tag):
        rc, want = self.run(["docker", "image", "inspect", "-f", "{{.Id}}", tag])
        for svc in services:
            rc2, have = self.run(["docker", "inspect", "-f", "{{.Image}}", CONTAINER[svc]])
            if rc or rc2 or have.strip() != want.strip():
                raise Refused(FAILED, "ERROR: %s runs %s, deploy image %s is %s" % (
                    CONTAINER[svc], have.strip()[:19], tag, want.strip()[:19]))
            self.say("verified %s runs %s" % (CONTAINER[svc], tag))

    # ---------------------------------------------------------------- post-deploy compare
    def compare(self, window_h=3.0):
        last = deploylog.last_record()
        if not last:
            self.say("no deploy recorded in legion/data/deploys.jsonl")
            return FAILED
        rc, out = self.run([sys.executable, LEAGUE, "--compare-deploy", "--window-h", "%g" % window_h])
        self.say(out.strip() or "(league.py printed nothing)")
        if rc:
            self.say("WARNING: league.py --compare-deploy exited %d" % rc)
        items = revert_items(last.get("tag", "")) or []
        lines = ["REVERT candidates for %s: %d" % (last.get("tag"), len(items))]
        for i in items:
            lines.append("  REVERT: %s | %s | %s | change %s%%" % (i.get("text"), i.get("metric"), i.get("cohort") or
                                                                    "all", i.get("change_pct")))
        text = "\n".join(lines) + "\n"
        self.say(text.rstrip())
        d = C.dpath(os.path.join("deploy-runs", run_dir_name(last.get("tag", ""))))
        os.makedirs(d, exist_ok=True)
        with open(os.path.join(d, "revert.txt"), "w", encoding="utf-8") as f:
            f.write(text)
        return FAILED if rc else OK


def main(argv=None, deploy=None):
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0],
                                 formatter_class=argparse.RawDescriptionHelpFormatter, epilog=__doc__)
    ap.add_argument("command", nargs="?", choices=["compare"], help="compare: keep/revert list of the last deploy")
    ap.add_argument("--fix", action="append", default=[], help="what|kpm|zph|deaths|cohort (one per fix)")
    ap.add_argument("--reviewed", action="append", default=[], help="the fix's commit (one per --fix, same order)")
    ap.add_argument("--critical", default=None, help="reason: skips the 3-hour gap (G17)")
    ap.add_argument("--services", default="legion,legion-core")
    ap.add_argument("--apply", action="store_true", help="do the batch (default: dry run)")
    ap.add_argument("--owner-lifts-hold", action="store_true", help="only when the owner has said so (G21)")
    a = ap.parse_args(argv)
    d = deploy or Deploy(apply=a.apply)
    try:
        if a.command == "compare":
            return d.compare()
        return d.batch(a.fix, a.reviewed, a.critical, a.services, a.owner_lifts_hold)
    except Refused as r:
        d.say(r.msg)
        if not d.apply:
            d.say("(dry run: nothing was changed)")
        return r.code


if __name__ == "__main__":
    sys.exit(main())
