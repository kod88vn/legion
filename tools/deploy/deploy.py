#!/usr/bin/env python3
"""deploy.py - the Legion deploy batch as one script (rule PR47: an agent that only runs an algorithm is a script).

  python3 tools/deploy/deploy.py --fix "<what>|<metric>|<cohort>" --reviewed <commit> [--fix ... --reviewed ...]
                                 [--critical "<reason>"] [--services legion,legion-core]
                                 [--image ro-legion:deploy-<stamp>]  (no rebuild: deploy an image an earlier run built)
                                 [--apply] [--owner-lifts-hold]
  python3 tools/deploy/deploy.py compare [--apply]    keep/revert list of the last deploy (league.py --compare-deploy)

One --fix per fix, and one --reviewed <commit> per --fix (the fix's commit, in the same order). metric: kpm, zph or
deaths. WSL: `wsl -d Ubuntu-24.04 -- python3 /mnt/g/RO/legion/tools/deploy/deploy.py ...`

DRY RUN IS THE DEFAULT: every gate and every read-only check runs and the plan is printed; nothing is built, started,
stopped, written or recorded. `--apply` runs the batch. Nothing here decides anything: every rule is a check in code.

Gates, in order; a refusal stops before anything is changed:
  services  only legion and legion-core, always with --no-deps (G15)                               exit 3
  review    every fix has a --reviewed commit with a merge review whose LAST line is `verdict: merge`
            (Constitution 4.2), and that commit is an ancestor of HEAD                             exit 5 / 4
  merges    every merge commit that ships has a review (Constitution 4.2): base..HEAD, where base is
            the sha of the last deploy (a record without a sha: the newest HEAD commit before that
            deploy; no deploy recorded: the whole history). Each merge needs
            docs/reports/reviews/<its second parent>.md, read from HEAD, whose LAST line is
            `verdict: merge`; `verdict: do not merge` refuses. A sync merge of main (subject
            "Merge branch 'main' into ...", "Merge main into ...", "Merge branch main into ...", and whose
            second parent is on main's first-parent chain) is exempt. No override.
            The dry run lists every merge that would ship with its verdict.                        exit 9
  source   (build only) legion/ build inputs have no uncommitted changes: only committed code is built  exit 4
  interval  3 hours between regular deploys (G17) unless --critical "<reason>"; >= 1 fix           exit 5
            (checked again under the lock)
  hold      legion/data/stack.hold: the owner has the stack stopped (G21); only --owner-lifts-hold  exit 6
  lock      one deploy run at a time (legion/data/deploy.lock)                                    exit 7
  rathena   db and rathena must not change (compose config hash and compose's own dry-run, G15);
            fails closed: a dry run that names no chosen service refuses                           exit 2
  rathena recreate: this script never recreates rathena or db (G15, G16): tools/deploy/rathena_recreate.sh only.
Then (--apply, build mode): stray golang test/build containers are stopped (G24: never rathena, Ayla, legion,
legion-core, db); the image is built from `git archive HEAD legion` (committed code, not the working tree): gofmt,
go vet and go test in a golang container, then docker build from that same export (failure: exit 1, nothing
deployed). A pre-deploy league snapshot is saved. Then per service, in order, legion then legion-core: `up --no-deps`
pinned to the one image (a stray `<hash>_ro-legion-core` is recovered, a stopped core is started), the running image
is verified, and the deploy is recorded for that service in legion/data/deploys.jsonl before the next service starts.
Ayla's OpenKore on a dead network namespace is a WARNING (it is not recreated here). Ayla must be back in game within
5 minutes after the batch (G19, exit 8; the records stay).
--image TAG (no rebuild): the same steps on an image an earlier run built; no build, no stray stop, no source check.
`compare` (3 hours after a deploy, or at the next one) writes the keep/revert list of the last deploy; with --apply
it also runs league.py --compare-deploy and writes revert.txt. A dry run only prints.

Exit codes: 0 ok (or dry run ok), 1 build/up/verify failed, 2 rathena/db would change or could not be confirmed
(or a usage error, argparse), 3 service not allowed, 4 source not committed or not an ancestor of HEAD, 5 review/
interval/no fix/bad fix, 6 stack hold, 7 lock held, 8 Ayla not back in time, 9 a merge that ships has no merge
review (or its review says do not merge, or has no verdict on its last line).
Every applied run writes legion/data/deploy-runs/deploy-<stamp>/ (run.log, pre-league.md, override.yml, revert.txt).
Standard library only. Tests: python -m unittest discover -s tests/system -p test_deploy.py (from the repo root).
"""
import argparse
import json
import os
import re
import shutil
import subprocess
import sys
import tarfile
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
FOLLOWERS_OF_RATHENA = ("ro-openkore-ayla", "ro-openkore")
AYLA_WINDOW_S = 300
AYLA_POLL_S = 5
METRICS = ("kpm", "zph", "deaths")
# The inputs the image needs (the Dockerfile copies these from legion/): committed code only.
BUILD_PATHS = ("legion/Dockerfile", "legion/go.mod", "legion/recvpackets.txt", "legion/*.go", "legion/*.html")
SNAPSHOT_FILES = ("Dockerfile", "go.mod", "recvpackets.txt")

OK, FAILED, DRIFT, SERVICE, SOURCE, GATE, HOLD, LOCKED, AYLA_LATE, UNREVIEWED = 0, 1, 2, 3, 4, 5, 6, 7, 8, 9
REVIEWS_REL = "docs/reports/reviews"
MERGE_VERDICT = re.compile(r"^verdict:\s*(merge|do not merge)\s*$", re.I)
SYNC_SUBJECT = re.compile(r"^Merge (?:branch )?'?main'?( of \S+)? into ", re.I)


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


def up_argv(svc, override, compose_file="docker-compose.yml", compose_project_name=None):
    """The one compose `up` this script runs: a single service, --no-deps, pinned by the override file."""
    # Derive project name if not provided (same logic as Deploy.__init__)
    if compose_project_name is None:
        basename = os.path.basename(compose_file)
        if basename == "docker-compose.yml":
            compose_project_name = "ro"
        else:
            name = basename.replace("docker-compose-", "").replace("docker-compose.", "").replace(".yml", "").replace(".yaml", "")
            compose_project_name = "ro-" + name if name else "ro"
    
    cmd = ["docker", "compose", "-f", compose_file]
    # Add project name (-p) only if using non-default compose file
    if compose_file != "docker-compose.yml":
        cmd.extend(["-p", compose_project_name])
    cmd.extend(["-f", override, "up", "-d", "--no-deps", "--no-build", "--pull", "never", svc])
    return cmd


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


def merge_verdict(text):
    """'merge' or 'do not merge' when the LAST non-empty line of a review is that verdict, else None."""
    lines = [l.strip() for l in text.splitlines() if l.strip()]
    m = MERGE_VERDICT.match(lines[-1]) if lines else None
    return m.group(1).lower() if m else None


def _epoch_of(rec):
    return C.to_epoch(rec.get("epoch", rec.get("t"))) or 0.0


def ship_base(root, head, run=execute):
    """(base sha or None, note): the commit the last deploy was built from. The newest deploy record carries its sha;
    a record written before shas were recorded falls back to the newest first-parent commit of HEAD committed before
    that deploy. No deploy recorded: None, so the whole history of HEAD is checked (fail closed)."""
    recs = deploylog.deploy_records()
    if not recs:
        return None, "no deploy recorded: the whole history of HEAD"
    last = max(recs, key=_epoch_of)
    label = "last deploy %s (%s)" % (last.get("tag", "?"), last.get("t", "?"))
    if last.get("sha"):
        return last["sha"], label + " was built from %s" % last["sha"]
    rc, out = run(["git", "-C", root, "log", "-1", "--first-parent", "--before=@%d" % int(_epoch_of(last)),
                   "--format=%H", head])
    base = out.strip() if rc == 0 else ""
    if not base:
        return None, label + " has no sha and no HEAD commit before it: the whole history of HEAD"
    return base, label + " has no sha; base is the HEAD commit before it: %s" % base


def merge_gate(root, head, base, run=execute):
    """Every merge commit in base..head (all merges of head when base is None) with its review verdict.
    Returns (rows, offenders). A row: sha, subject, verdict ('merge', 'do not merge', 'none', 'bad parents', 'sync'),
    review (the path read from head's tree) and why. Refuses (SOURCE) when git cannot list the merges."""
    rng = "%s..%s" % (base, head) if base else head
    rc, out = run(["git", "-C", root, "log", "--merges", "--reverse", "--format=%H%x1f%P%x1f%s", rng])
    if rc != 0:
        raise Refused(SOURCE, "REFUSED: git log failed; cannot list the merges that ship: %s" % out.strip()[-300:])
    rows = []
    main_chain = None  # first-parent history of main, read on first use (a failed read exempts nothing)
    for line in out.splitlines():
        if not line.strip():
            continue
        sha, parents, subject = (line.split("\x1f") + ["", ""])[:3]
        parents = parents.split()
        row = {"sha": sha, "subject": subject, "verdict": None, "review": "", "why": ""}
        if len(parents) != 2:
            row.update(verdict="bad parents", why="not a two-parent merge: its review cannot be found")
            rows.append(row)
            continue
        row["review"] = "%s/%s.md" % (REVIEWS_REL, parents[1])
        if SYNC_SUBJECT.match(subject):
            if main_chain is None:
                rc_m, chain = run(["git", "-C", root, "rev-list", "--first-parent", "main"])
                main_chain = set(chain.split()) if rc_m == 0 else set()
            # a sync brings main's own history into the branch: its second parent is on main's first-parent chain
            if parents[1] in main_chain:
                row.update(verdict="sync", why="sync merge of main (exempt)")
                rows.append(row)
                continue
        rc, text = run(["git", "-C", root, "show", "%s:%s" % (head, row["review"])])
        v = merge_verdict(text) if rc == 0 else None
        if rc != 0:
            row.update(verdict="none", why="no review file %s" % row["review"])
        elif v == "merge":
            row.update(verdict="merge", why="review says merge")
        elif v == "do not merge":
            row.update(verdict="do not merge", why="review says 'verdict: do not merge' (%s)" % row["review"])
        else:
            row.update(verdict="none", why="review %s does not end with 'verdict: merge'" % row["review"])
        rows.append(row)
    return rows, [r for r in rows if r["verdict"] not in ("merge", "sync")]


class Deploy:
    """One deploy batch. Reads go through self.run (always executed); changes go through self.act (executed only with
    apply, printed otherwise). Tests inject `execute_fn`, `sleep`, `reviews_dir` and `resolve`."""

    def __init__(self, root=ROOT, apply=False, execute_fn=None, sleep=time.sleep, reviews_dir=None, resolve=None,
                 out=print, compose_file="docker-compose.yml", compose_project_name=None):
        self.root = root
        self.apply = apply
        self.execute = execute_fn or execute
        self.sleep = sleep
        self.reviews_dir = reviews_dir or reviewcheck.REVIEWS
        self.resolve = resolve or reviewcheck.resolve
        self.out = out
        self.compose_file = compose_file
        # Project name: if not provided, derive from compose filename (e.g., docker-compose.yml -> ro, docker-compose.server.yml -> ro-server)
        if compose_project_name is None:
            basename = os.path.basename(compose_file)
            if basename == "docker-compose.yml":
                compose_project_name = "ro"
            else:
                # Strip off "docker-compose-" prefix and ".yml" suffix if present
                name = basename.replace("docker-compose-", "").replace("docker-compose.", "").replace(".yml", "").replace(".yaml", "")
                compose_project_name = "ro-" + name if name else "ro"
        self.compose_project_name = compose_project_name
        self.logpath = None
        self.head = None  # full sha of HEAD, read once by batch(): the merges are checked and the deploy recorded for it

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
        """Change: executed only with apply; in a dry run it is printed. G15 is checked here, in code."""
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

    def compose_argv(self, *args):
        """Build a docker compose command with the configured compose file and project name.
        
        Uses -f for compose file and optionally -p for project name (when needed for disambiguation
        or when using non-default compose files). Legacy default: no -p flag for backward compatibility.
        """
        cmd = ["docker", "compose", "-f", self.compose_file]
        # Add project name (-p) only if using non-default compose file (server, etc.)
        # This prevents namespace collisions when multiple compose files are in use
        if self.compose_file != "docker-compose.yml":
            cmd.extend(["-p", self.compose_project_name])
        return cmd + list(args)

    # ---------------------------------------------------------------- gates
    def gate_review(self, fixes, reviewed):
        if not fixes:
            raise Refused(GATE, "REFUSED: no --fix \"<what>|<metric>|<cohort>\" given; every deploy lists its fixes")
        ok, msg = reviewcheck.check(len(fixes), reviewed, reviews_dir=self.reviews_dir, resolve_fn=self.resolve)
        if not ok:
            raise Refused(GATE, msg)
        for c in reviewed:
            sha = self.resolve(c)
            rc, _ = self.run(["git", "-C", self.root, "merge-base", "--is-ancestor", sha, "HEAD"])
            if rc != 0:
                raise Refused(SOURCE, "REFUSED: reviewed commit %s is not an ancestor of HEAD; only committed, merged "
                                      "code can be deployed" % sha[:12])
        return msg

    def gate_merges(self):
        """Constitution 4.2 for the whole batch: every merge that ships since the last deploy has a merge review. The
        deploy ships HEAD, so every merge in base..HEAD ships, not only the --reviewed ones. Lists them all (dry run too)."""
        rc, head = self.run(["git", "-C", self.root, "rev-parse", "HEAD"])
        if rc != 0 or not head.strip():
            raise Refused(SOURCE, "REFUSED: cannot read HEAD: %s" % self.tail(head, 3))
        self.head = head.strip()
        base, note = ship_base(self.root, self.head, self.run)
        rows, offenders = merge_gate(self.root, self.head, base, self.run)
        self.say("merges that would ship (%s):" % note)
        if not rows:
            self.say("  none: no merge commit since that deploy")
        for r in rows:
            self.say("  %s  review: %-13s %s" % (r["sha"], r["verdict"], r["subject"]))
        if offenders:
            lines = ["  %s  %s\n      reason: %s" % (r["sha"], r["subject"], r["why"]) for r in offenders]
            raise Refused(UNREVIEWED, "REFUSED (Constitution 4.2): %d merge(s) that would ship have no merge review. "
                                      "Each needs docs/reports/reviews/<second parent>.md with its last line "
                                      "'verdict: merge' (read from HEAD). Nothing ships until then:\n%s"
                          % (len(offenders), "\n".join(lines)))
        return note

    def gate_source(self):
        """Build mode: the inputs of the image must be committed. Fails closed if git cannot say."""
        rc, out = self.run(["git", "-C", self.root, "status", "--porcelain", "--"] + list(BUILD_PATHS))
        if rc != 0:
            raise Refused(SOURCE, "REFUSED: git status failed; cannot confirm legion/ is committed: %s" % self.tail(out, 3))
        if out.strip():
            raise Refused(SOURCE, "REFUSED: uncommitted changes in the image inputs (legion/*.go, Dockerfile, go.mod, "
                                  "*.html, recvpackets.txt). Only committed code is built. Commit and review them first:\n"
                                  + self.tail(out, 20))

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
        rc, out = self.run(self.compose_argv("config", "--hash", svc))
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
        """G15: the batch must not change db or rathena. Fails closed: any unreadable answer is a refusal."""
        why = []
        for svc in ("db", "rathena"):
            r = self.hash_drift(svc)
            if r:
                why.append(r)
        rc, out = self.run(self.compose_argv("up", "-d", "--no-build", "--dry-run", *services))
        if rc != 0:
            why.append("compose dry-run (with deps) failed: %s" % self.tail(out, 3))
        for line in out.splitlines():
            if re.search(r"Container (ro-rathena|ro-db)\b.*(Recreat|Creat)", line):
                why.append("compose dry-run (with deps) would change: " + line.strip())
        rc, out = self.run(self.compose_argv("up", "-d", "--no-deps", "--no-build", "--dry-run", *services))
        if rc != 0:
            why.append("compose dry-run (--no-deps) failed: %s" % self.tail(out, 3))
        named = set()
        for line in out.splitlines():
            m = re.search(r"Container (\S+)", line)
            if m:
                named.add(m.group(1))
        if not named:
            why.append("the --no-deps dry run names no container: cannot confirm what compose would touch")
        for n in sorted(named):
            if n not in [CONTAINER[s] for s in services]:
                why.append("compose would touch %s" % n)
        for s in services:
            if CONTAINER[s] not in named and named:
                why.append("the --no-deps dry run does not name %s: cannot confirm it" % CONTAINER[s])
        if why:
            raise Refused(DRIFT, "rathena config changed or could not be confirmed; a Legion deploy does not recreate "
                                 "rathena (G15). Not deploying. An owner-approved rathena recreate is "
                                 "tools/deploy/rathena_recreate.sh --yes (once an hour at most, G16).\n  - "
                                 + "\n  - ".join(why))

    def network_warnings(self):
        """Ayla's OpenKore must share rathena's network namespace. A dead one is a WARNING (never recreated here)."""
        rc, rid = self.run(["docker", "inspect", "-f", "{{.Id}}", CONTAINER["rathena"]])
        if rc != 0 or not rid.strip():
            return ["WARNING: could not read ro-rathena's id; the network namespace of Ayla's OpenKore is not checked"]
        out = []
        for c in FOLLOWERS_OF_RATHENA:
            rc, mode = self.run(["docker", "inspect", "-f", "{{.HostConfig.NetworkMode}}", c])
            if rc != 0:
                continue  # not present
            if mode.strip() != "container:" + rid.strip():
                out.append("WARNING: %s is on a dead network namespace (%s), not ro-rathena's: Ayla is offline (G19). "
                           "Main session: an owner-approved `docker compose up -d --no-deps --force-recreate %s` "
                           "fixes it. Never send ai, ai off or ai manual." % (c, mode.strip(), c[len("ro-"):]))
        return out

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

    # ---------------------------------------------------------------- build from committed code
    def export_head(self, snap):
        """Copy the image inputs of legion/ at HEAD (git archive) into snap. Never the working tree."""
        tmp = tempfile.mkdtemp(prefix="legion-tar-")
        try:
            tarpath = os.path.join(tmp, "legion.tar")
            rc, out = self.execute(["git", "-C", self.root, "archive", "--format=tar", "--output", tarpath, "HEAD",
                                    "legion"], self.root)
            if rc != 0:
                raise Refused(FAILED, "git archive HEAD legion failed - nothing deployed:\n" + self.tail(out, 5))
            with tarfile.open(tarpath) as t:
                for m in t.getmembers():
                    if not m.isfile() or not m.name.startswith("legion/"):
                        continue
                    base = m.name[len("legion/"):]
                    if "/" in base:
                        continue
                    if base in SNAPSHOT_FILES or base.endswith(".go") or base.endswith(".html"):
                        with t.extractfile(m) as src, open(os.path.join(snap, base), "wb") as dst:
                            shutil.copyfileobj(src, dst)
        finally:
            shutil.rmtree(tmp, ignore_errors=True)
        if not os.path.exists(os.path.join(snap, "Dockerfile")):
            raise Refused(FAILED, "HEAD has no legion/Dockerfile - nothing deployed")

    def build(self, tag):
        """gofmt (no changes allowed), go vet and go test on one export of HEAD, then docker build from that export."""
        for attempt in (1, 2):
            snap = tempfile.mkdtemp(prefix="legion-snap-")
            try:
                self.export_head(snap)
                self.say("export of committed legion/ (try %d) at %s" % (attempt, snap))
                rc, out = self.act(["docker", "run", "--rm", "-v", snap + ":/src", "-w", "/src", GO_IMAGE, "sh", "-c",
                                    "u=$(gofmt -l .); [ -z \"$u\" ] || { echo \"not gofmt-clean: $u\"; exit 1; }; "
                                    "go vet ./... && go test -count=1 ./..."])
                if rc == 0:
                    rc, out = self.act(["docker", "build", "-t", tag, "-t", "ro-legion:latest", snap])
                    if rc != 0:
                        raise Refused(FAILED, "BUILD FAILED (docker build) - nothing deployed:\n" + self.tail(out))
                    self.say("built %s (= ro-legion:latest) from committed code" % tag)
                    return
                self.say("gofmt/vet/test failed on the export:\n" + self.tail(out, 15))
            finally:
                shutil.rmtree(snap, ignore_errors=True)
            if attempt == 1:
                self.say("retry on a fresh export in 60 s")
                self.sleep(60)
        raise Refused(FAILED, "BUILD FAILED: go vet/test failed twice - nothing deployed")

    # ---------------------------------------------------------------- per service
    def ensure_core_container(self):
        """Recover a stray `<hash>_ro-legion-core` left in Created state, then make sure the core is running."""
        rc, out = self.run(["docker", "inspect", "-f", "{{.State.Running}}", CONTAINER["legion-core"]])
        if rc != 0:
            rc2, names = self.run(["docker", "ps", "-a", "--format", "{{.Names}}"])
            odd = [n for n in names.split() if re.fullmatch(r"[0-9a-f]+_ro-legion-core", n)]
            if not odd:
                raise Refused(FAILED, "no ro-legion-core container after up - nothing more deployed")
            self.say("fixing stray container %s -> ro-legion-core" % odd[0])
            self.act(["docker", "start", odd[0]])
            self.act(["docker", "rename", odd[0], CONTAINER["legion-core"]])
            rc, out = self.run(["docker", "inspect", "-f", "{{.State.Running}}", CONTAINER["legion-core"]])
        if out.strip() != "true":
            self.act(["docker", "start", CONTAINER["legion-core"]])
            rc, out = self.run(["docker", "inspect", "-f", "{{.State.Running}}", CONTAINER["legion-core"]])
            if out.strip() != "true":
                raise Refused(FAILED, "ro-legion-core is not running after start - nothing more deployed")

    def verify_image(self, svc, tag):
        rc, want = self.run(["docker", "image", "inspect", "-f", "{{.Id}}", tag])
        rc2, have = self.run(["docker", "inspect", "-f", "{{.Image}}", CONTAINER[svc]])
        if rc or rc2 or not want.strip() or have.strip() != want.strip():
            raise Refused(FAILED, "ERROR: %s runs %s, deploy image %s is %s" % (
                CONTAINER[svc], have.strip()[:19], tag, want.strip()[:19]))
        self.say("verified %s runs %s" % (CONTAINER[svc], tag))

    # ---------------------------------------------------------------- the batch
    def plan(self, tag, services, fixes, run_dir, image):
        self.say("DRY RUN - nothing is built, started, stopped, written or recorded. With --apply the batch would:")
        if image:
            self.say("  1. no build: deploy the existing image %s (checked)" % image)
        else:
            self.say("  1. stop stray golang test/build containers (G24, listed above)")
            self.say("  2. build %s from committed legion/ at HEAD: gofmt, go vet, go test in %s, then docker build"
                     % (tag, GO_IMAGE))
        self.say("  3. save a pre-deploy league snapshot (league.py --stdout)")
        n = 4
        for svc in services:
            self.say("  %d. up %s: %s" % (n, svc, " ".join(up_argv(svc, os.path.join(run_dir, "override.yml"), self.compose_file, self.compose_project_name))))
            self.say("     verify the running image; record the deploy for %s in legion/data/deploys.jsonl" % svc)
            n += 1
        self.say("  %d. Ayla must be back in game within %d s (G19)" % (n, AYLA_WINDOW_S))
        self.say("Rerun with --apply to do it.")

    def batch(self, fix_texts, reviewed, critical, services_raw, lifted, image=None):
        # Validate compose file exists
        compose_path = os.path.join(self.root, self.compose_file)
        if not os.path.exists(compose_path):
            raise Refused(GATE, "REFUSED: compose file not found: %s" % self.compose_file)
        
        # Validate that all required services exist in the chosen compose file
        rc, out = self.run(self.compose_argv("config", "--format", "json"))
        if rc == 0:
            try:
                import json
                config = json.loads(out)
                available_services = set(config.get("services", {}).keys())
            except (json.JSONDecodeError, KeyError):
                available_services = set()
        else:
            available_services = set()
        
        services = [s.strip() for s in services_raw.split(",") if s.strip()]
        bad = [s for s in services if s not in ALLOWED]
        if bad or not services:
            raise Refused(SERVICE, "REFUSED (G15): only %s may be deployed (got %s)"
                          % (", ".join(ALLOWED), ", ".join(bad) or "nothing"))
        
        # Check that all requested services exist in the compose file
        missing = [s for s in services if s not in available_services]
        if missing:
            raise Refused(GATE, "REFUSED: compose file %s is missing service(s): %s"
                          % (self.compose_file, ", ".join(missing)))
        
        fixes = [parse_fix(t) for t in fix_texts]
        self.say("review: " + self.gate_review(fix_texts, reviewed).replace("\n", "; "))
        self.gate_merges()
        self.say("interval: " + self.gate_interval(critical, len(fixes)))
        self.gate_hold(lifted)
        self.gate_lock()
        if image:
            rc, out = self.run(["docker", "image", "inspect", "-f", "{{.Id}}", image])
            if rc != 0:
                raise Refused(FAILED, "REFUSED: image %s does not exist - nothing deployed" % image)
        else:
            self.gate_source()
            self.say("source: legion/ build inputs are committed")
        self.gate_rathena(services)
        self.say("guard: db and rathena unchanged; the batch touches only %s, each with --no-deps" % ", ".join(services))
        for w in self.network_warnings():
            self.say(w)
        pre = self.ayla_status()
        self.say("Ayla before the deploy: %s" % ("online" if pre and pre.get("online") else "NOT online (G19 at risk)"))
        ts = time.strftime("%Y%m%d-%H%M%S", time.localtime(C.now()))
        tag = image or "ro-legion:deploy-%s" % ts
        run_dir = C.dpath(os.path.join("deploy-runs", "deploy-" + ts))
        if not self.apply:
            self.plan(tag, services, fixes, run_dir, image)
            return OK
        return self.apply_batch(tag, ts, run_dir, fixes, reviewed, critical, services, image)

    def apply_batch(self, tag, ts, run_dir, fixes, reviewed, critical, services, image):
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
            # the interval gate again, under the lock: another run may have deployed while this one was checking
            self.say("interval (under lock): " + self.gate_interval(critical, len(fixes)))
            self.say("deploy %s on %s; critical: %s; %s" % (tag, ", ".join(services), critical or "no",
                                                         "no rebuild" if image else "build from committed code"))
            for f in fixes:
                self.say("  fix: %s | %s | %s" % (f["text"], f["metric"], f["cohort"] or "all"))
            self.say("  reviewed: %s" % ", ".join(reviewed))
            if not image:
                rc, head = self.run(["git", "-C", self.root, "rev-parse", "HEAD"])
                if rc != 0 or head.strip() != self.head:
                    raise Refused(SOURCE, "REFUSED: HEAD moved from %s to %s while the batch was checked (merge review "
                                          "gate). Nothing deployed; rerun." % (self.head, head.strip()))
                self.say("  HEAD: %s" % head.strip())
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
            for i, svc in enumerate(services):
                rc, out = self.act(up_argv(svc, override, self.compose_file, self.compose_project_name))
                if rc:
                    raise Refused(FAILED, "UP FAILED for %s - nothing more deployed:\n%s" % (svc, self.tail(out)))
                self.say("up %s: %s" % (svc, self.tail(out, 2)))
                if svc == "legion-core":
                    self.ensure_core_container()
                self.verify_image(svc, tag)
                # recorded per service: a failure of the next service leaves this one on record
                rec = deploylog.record(tag, [svc], critical, fixes if i == 0 else [],
                                       sha=None if image else self.head)
                self.say("recorded deploy %s for %s (%d fixes)" % (rec["tag"], svc, len(rec["fixes"])))
            if not self.wait_ayla(start_epoch, start_iso):
                raise Refused(AYLA_LATE, "G19: Ayla is not back in game within %d s after the deploy. Look at "
                                         "`docker logs %s`. Never send ai, ai off or ai manual. The records stay."
                              % (AYLA_WINDOW_S, AYLA))
            self.say("Ayla is back in game within %d s" % AYLA_WINDOW_S)
            self.say("next: `deploy.py compare --apply` 3 hours after this deploy (or at the next deploy)")
            return OK
        finally:
            try:
                os.remove(lock)
            except OSError:
                pass

    # ---------------------------------------------------------------- post-deploy compare
    def compare(self, window_h=3.0):
        last = deploylog.last_record()
        if not last:
            self.say("no deploy recorded in legion/data/deploys.jsonl")
            return FAILED
        tag = last.get("tag", "")
        rc = 0
        if self.apply:
            rc, out = self.act([sys.executable, LEAGUE, "--compare-deploy", "--window-h", "%g" % window_h])
            self.say(out.strip() or "(league.py printed nothing)")
            if rc:
                self.say("WARNING: league.py --compare-deploy exited %d" % rc)
        else:
            self.say("dry run: would run league.py --compare-deploy (it writes the verdict to deploys.jsonl)")
        items = revert_items(tag) or []
        lines = ["REVERT candidates for %s: %d" % (tag, len(items))]
        for i in items:
            lines.append("  REVERT: %s | %s | %s | change %s%%" % (i.get("text"), i.get("metric"), i.get("cohort") or
                                                                    "all", i.get("change_pct")))
        text = "\n".join(lines) + "\n"
        self.say(text.rstrip())
        if self.apply:
            d = C.dpath(os.path.join("deploy-runs", run_dir_name(tag)))
            os.makedirs(d, exist_ok=True)
            with open(os.path.join(d, "revert.txt"), "w", encoding="utf-8") as f:
                f.write(text)
            self.say("revert.txt written")
        else:
            self.say("dry run: revert.txt not written")
        return FAILED if rc else OK


def main(argv=None, deploy=None):
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0],
                                 formatter_class=argparse.RawDescriptionHelpFormatter, epilog=__doc__)
    ap.add_argument("command", nargs="?", choices=["compare"], help="compare: keep/revert list of the last deploy")
    ap.add_argument("--fix", action="append", default=[], help="what|kpm|zph|deaths|cohort (one per fix)")
    ap.add_argument("--reviewed", action="append", default=[], help="the fix's commit (one per --fix, same order)")
    ap.add_argument("--critical", default=None, help="reason: skips the 3-hour gap (G17)")
    ap.add_argument("--services", default="legion,legion-core")
    ap.add_argument("--compose", default="docker-compose.yml", help="compose file path relative to repo root (default: docker-compose.yml)")
    ap.add_argument("--image", default=None, help="deploy this existing image instead of building (no rebuild)")
    ap.add_argument("--apply", action="store_true", help="do it (default: dry run)")
    ap.add_argument("--owner-lifts-hold", action="store_true", help="only when the owner has said so (G21)")
    a = ap.parse_args(argv)
    d = deploy or Deploy(apply=a.apply, compose_file=a.compose)
    try:
        if a.command == "compare":
            return d.compare()
        return d.batch(a.fix, a.reviewed, a.critical, a.services, a.owner_lifts_hold, image=a.image)
    except Refused as r:
        d.say(r.msg)
        if not d.apply:
            d.say("(dry run: nothing was changed)")
        return r.code


if __name__ == "__main__":
    sys.exit(main())
