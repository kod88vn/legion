#!/usr/bin/env python3
"""portcheck.py - can this checkout move to a new machine? Offline and read-only (PR47: plain code, no AI).

  python tools/system/portcheck.py                 check the repo this file lives in
  python tools/system/portcheck.py --base G:/RO     the folder that holds the repo (default: its parent)
  python tools/system/portcheck.py --manifest docs/NEW-MACHINE-MANIFEST.json

Four checks:
  [a] tracked text files that hard-code an absolute host path (drive letter, /g/, /mnt/x/, C:/Users/<name>).
      A hit in a file that the manifest does not list under known_path_files is a BLOCKING gap (a new one).
      Known hits are filed in the bug queue and listed in docs/SETUP-NEW-MACHINE.md ('Known path assumptions').
      docs/ and RESTART-NOTES/ are prose, not scanned.
  [b] the keys .env needs (uncommented in example.env) are present in .env. Optional keys (commented in
      example.env) are reported only. Key NAMES are printed, never values.
  [c] the tools a fresh machine needs: git, Python 3.12 or newer, wsl (blocking); docker (warning: it usually
      lives inside WSL, which this check does not start). Run inside WSL, the wsl check is skipped with a note,
      because the wsl command is only on the Windows PATH.
  [d] owner-supplied files listed in the manifest (docs/NEW-MACHINE-MANIFEST.json): severity 'blocking' fails the
      check, 'warn' and 'optional' are reported only.

Exit code: 0 = no blocking gap (also for -h), 1 = at least one blocking gap, 2 = usage error.
Reads only. Never starts a container, never runs a network call, never prints a secret value (a hit is reported
as file:line plus the pattern name, not the matched text).
"""
import argparse
import json
import os
import re
import shutil
import subprocess
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
DEFAULT_REPO = os.path.abspath(os.path.join(HERE, "..", ".."))
MANIFEST_REL = os.path.join("docs", "NEW-MACHINE-MANIFEST.json")
PY_MIN = (3, 12)

# host-specific absolute paths: each pattern has a name that is printed instead of the matched text
PATH_PATTERNS = [
    ("drive-path", re.compile(r"\b[A-Za-z]:[\\/]+RO\b", re.IGNORECASE)),
    ("git-bash-path", re.compile(r"(?<![\w/.])/g/RO\b", re.IGNORECASE)),
    ("wsl-path", re.compile(r"/mnt/[a-z]/RO\b", re.IGNORECASE)),
    ("user-profile", re.compile(r"\b[A-Za-z]:[\\/]+Users[\\/]+[\w.-]+", re.IGNORECASE)),
    ("git-bash-user", re.compile(r"(?<![\w/.])/[a-z]/Users/[\w.-]+", re.IGNORECASE)),
    ("wsl-user", re.compile(r"/mnt/[a-z]/Users/[\w.-]+", re.IGNORECASE)),
]
# prose folders are not scanned: they describe commands, nothing executes them from there
SKIP_PREFIXES = ("docs/", "RESTART-NOTES/")
MAX_SCAN_BYTES = 20 * 1024 * 1024
ENV_REQ = re.compile(r"^([A-Z][A-Z0-9_]*)\s*=")
ENV_OPT = re.compile(r"^#\s*([A-Z][A-Z0-9_]*)\s*=")


def tracked_files(repo):
    """git-tracked files (repo-relative, forward slashes). Without git (tests, a copied folder) every file under repo."""
    try:
        out = subprocess.run(["git", "-C", repo, "ls-files", "-z"], capture_output=True, check=True, timeout=60)
        return [p for p in out.stdout.decode("utf-8", "replace").split("\0") if p]
    except (OSError, subprocess.SubprocessError):
        found = []
        for root, dirs, files in os.walk(repo):
            dirs[:] = [d for d in dirs if d != ".git"]
            for f in files:
                found.append(os.path.relpath(os.path.join(root, f), repo).replace(os.sep, "/"))
        return sorted(found)


def is_text(data):
    return b"\0" not in data[:8000]


def scan_host_paths(repo, files=None, skip=()):
    """{repo-relative path: [(line number, pattern name), ...]} for tracked text files with an absolute host path."""
    hits = {}
    for rel in (files if files is not None else tracked_files(repo)):
        if rel.startswith(SKIP_PREFIXES) or rel in skip:
            continue
        full = os.path.join(repo, *rel.split("/"))
        try:
            if os.path.getsize(full) > MAX_SCAN_BYTES:
                continue
            with open(full, "rb") as fh:
                data = fh.read()
        except OSError:
            continue
        if not is_text(data):
            continue
        for n, line in enumerate(data.decode("utf-8", "replace").splitlines(), 1):
            for name, pat in PATH_PATTERNS:
                if pat.search(line):
                    hits.setdefault(rel, []).append((n, name))
                    break
    return hits


def parse_env(text):
    """(uncommented keys, commented keys): names only."""
    req, opt = set(), set()
    for line in text.splitlines():
        s = line.strip()
        m = ENV_REQ.match(s)
        if m:
            req.add(m.group(1))
            continue
        m = ENV_OPT.match(s)
        if m:
            opt.add(m.group(1))
    return req, opt - req


def check_env(env_path, example_path):
    """dict: example_exists, env_exists, missing_required, missing_optional, extra (key names only)."""
    res = {"example_exists": os.path.isfile(example_path), "env_exists": os.path.isfile(env_path),
           "missing_required": [], "missing_optional": [], "extra": []}
    if not res["example_exists"]:
        return res
    with open(example_path, encoding="utf-8", errors="replace") as fh:
        ex_req, ex_opt = parse_env(fh.read())
    env_req, env_opt = set(), set()
    if res["env_exists"]:
        with open(env_path, encoding="utf-8", errors="replace") as fh:
            env_req, env_opt = parse_env(fh.read())
    res["missing_required"] = sorted(ex_req - env_req)
    res["missing_optional"] = sorted(ex_opt - env_req - env_opt)
    res["extra"] = sorted(env_req - ex_req - ex_opt)
    return res


def in_wsl():
    """True inside a WSL distro (the Linux kernel reports Microsoft); False on Windows."""
    if os.environ.get("WSL_DISTRO_NAME") or os.environ.get("WSL_INTEROP"):
        return True
    try:
        with open("/proc/sys/kernel/osrelease", encoding="utf-8", errors="replace") as fh:
            return "microsoft" in fh.read().lower()
    except OSError:
        return False


def check_tools(which=shutil.which, version=None, wsl=None):
    """list of (name, ok, severity, detail). severity: blocking | warn | skip (not checked here, shown as skip)."""
    version = version or sys.version_info
    wsl = in_wsl() if wsl is None else wsl
    out = []
    out.append(("git", bool(which("git")), "blocking", "on PATH" if which("git") else "install Git for Windows"))
    py_ok = tuple(version[:2]) >= PY_MIN
    out.append(("python", py_ok, "blocking",
                "%d.%d.%d" % tuple(version[:3]) if py_ok else "need %d.%d or newer" % PY_MIN))
    if which("wsl"):
        out.append(("wsl", True, "blocking", "on PATH"))
    elif wsl:
        out.append(("wsl", True, "skip", "not checked: this run is inside WSL, where the wsl command is not on "
                                         "PATH (run portcheck from Windows to check it)"))
    else:
        out.append(("wsl", False, "blocking", "enable WSL2 and install Ubuntu-24.04"))
    if which("docker"):
        out.append(("docker", True, "warn", "on Windows PATH"))
    else:
        out.append(("docker", False, "warn",
                    "not on Windows PATH: expected inside WSL Ubuntu-24.04 (verify there, not checked offline)"))
    return out


def check_manifest(manifest, bases):
    """manifest: parsed JSON. bases: {'repo': dir, 'ro': dir, 'home': dir}. Returns [(path, severity, present, note)]."""
    out = []
    for item in manifest.get("items", []):
        base = bases.get(item.get("base", "repo"))
        full = os.path.join(base, *item["path"].split("/")) if base else None
        present = bool(full) and os.path.exists(full)
        out.append((item["path"], item.get("severity", "warn"), present, item.get("note", "")))
    return out


def load_manifest(path):
    if not os.path.isfile(path):
        return {}
    with open(path, encoding="utf-8") as fh:
        return json.load(fh)


def run(repo, base=None, manifest_path=None, which=shutil.which, version=None, wsl=None):
    """returns (report lines, blocking gap count)."""
    base = base or os.path.dirname(os.path.abspath(repo))
    manifest_path = manifest_path or os.path.join(repo, MANIFEST_REL)
    manifest = load_manifest(manifest_path)
    known = set(manifest.get("known_path_files", []))
    lines, blocking = [f"portcheck: repo={repo}", f"  base={base}"], 0

    hits = scan_host_paths(repo, skip=("tools/system/portcheck.py", "tests/system/test_portcheck.py"))
    new = {p: h for p, h in hits.items() if p not in known}
    lines.append(f"[a] host paths: {len(hits)} tracked files, {len(new)} NEW (not in known_path_files)")
    for p in sorted(new):
        for n, name in new[p]:
            lines.append(f"    BLOCKING new host path: {p}:{n} ({name})")
            blocking += 1
    for p in sorted(p for p in known if p not in hits):
        lines.append(f"    note: known_path_files entry {p} has no host path any more (may be removed from the manifest)")
    lines.append(f"    known (filed, see guide): {len(hits) - len(new)} files")

    env = check_env(os.path.join(repo, ".env"), os.path.join(repo, "example.env"))
    lines.append("[b] .env vs example.env (key names only)")
    if not env["example_exists"]:
        lines.append("    BLOCKING example.env is missing from the repo")
        blocking += 1
    if not env["env_exists"]:
        lines.append("    BLOCKING .env is missing: copy example.env to .env and fill in the values")
        blocking += 1
    for k in env["missing_required"]:
        lines.append(f"    BLOCKING .env lacks required key {k}")
        blocking += 1
    for k in env["missing_optional"]:
        lines.append(f"    optional key not set: {k}")
    for k in env["extra"]:
        lines.append(f"    note: .env has key {k} that example.env does not list")
    if env["env_exists"] and not env["missing_required"]:
        lines.append("    required keys present")

    lines.append("[c] tools")
    for name, ok, sev, detail in check_tools(which, version, wsl):
        tag = "skip" if sev == "skip" else ("ok" if ok else ("BLOCKING" if sev == "blocking" else "warn"))
        lines.append(f"    {tag:9} {name}: {detail}")
        if not ok and sev == "blocking":
            blocking += 1

    lines.append(f"[d] owner-supplied files ({os.path.basename(manifest_path)})")
    bases = {"repo": repo, "ro": base, "home": os.path.expanduser("~")}
    if not manifest:
        lines.append("    BLOCKING manifest not found or empty: " + manifest_path)
        blocking += 1
    for path, sev, present, note in check_manifest(manifest, bases):
        if present:
            lines.append(f"    present  {path}")
            continue
        if sev == "blocking":
            blocking += 1
            lines.append(f"    BLOCKING missing {path}" + (f" ({note})" if note else ""))
        else:
            lines.append(f"    {sev:8} missing {path}" + (f" ({note})" if note else ""))
    lines.append(f"summary: blocking gaps = {blocking}")
    return lines, blocking


def main(argv=None):
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--repo", default=DEFAULT_REPO)
    ap.add_argument("--base", default=None, help="folder that holds the repo (default: its parent)")
    ap.add_argument("--manifest", default=None)
    try:
        args = ap.parse_args(argv)
    except SystemExit as e:   # -h exits 0; a usage error exits 2
        return e.code if isinstance(e.code, int) else 2
    lines, blocking = run(os.path.abspath(args.repo), args.base, args.manifest)
    print("\n".join(lines))
    return 1 if blocking else 0


if __name__ == "__main__":
    sys.exit(main())
