#!/usr/bin/env python3
"""fieldcheck.py - every OpenKore field file and portal line against the server's map cache and loaded warps.

  python tools/system/fieldcheck.py                  repo copies (openkore/fields-re, fields-srv, the portal patches)
                                                     AND every OpenKore container that exists (ro-openkore-ayla,
                                                     ro-openkore-qa, ro-openkore; running or stopped)
  python tools/system/fieldcheck.py --repo-only      the repo copies only
  python tools/system/fieldcheck.py --container NAME ...   only these containers
  python tools/system/fieldcheck.py --server-dir DIR     server data from DIR instead of ro-rathena (DIR has db/,
                                                     db/re/map_cache.dat and npc/)
  python tools/system/fieldcheck.py --field-dir DIR      also check one more field directory (a /openkore/fields copy)

Server truth: db/map_cache.dat with db/re/map_cache.dat over it (the re cache wins), and the warps the server loads
(gen_portals_from_npc.server_warps: npc/re/scripts_main.conf + npc/scripts_custom.conf). Read from the live ro-rathena
container with `docker cp` (read-only; nothing is started or run inside it), else from tools/dev/rathena-src.
Containers are read with `docker cp <name>:<path> -` as well, so a STOPPED container is checked too. Nothing is written
to any container.

Cell rule: a field cell is compared on its walk/snipe/water bits (value & 7). The higher bits are OpenKore flags (a
shootable cell is stored as 10 = 2 | 8 in the shipped files); they are not compared.
Portal rule: the generator's (gen_portals_from_npc.diff_portals): a plain portal line that the server does not have
is a DROP; a server warp with no portal line is a MISSING portal (the line to add).

Exit: 0 all match, 1 mismatches, 2 could not read the server data. Standard library only; Windows Python and WSL.
"""
import argparse
import gzip
import io
import os
import re
import shutil
import struct
import subprocess
import sys
import tarfile
import tempfile
import zlib

ROOT = os.path.normpath(os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", ".."))
OPENKORE = os.path.join(ROOT, "openkore")
sys.path.insert(0, OPENKORE)
from gen_fields_from_mapcache import GAT2FLD, load  # noqa: E402
from gen_portals_from_npc import diff_portals, portal_lines, server_warps  # noqa: E402

OK_CONTAINERS = ("ro-openkore-ayla", "ro-openkore-qa", "ro-openkore")
RATHENA = "ro-rathena"
FLAG_MASK = 7  # walk / snipe / water bits; see the module doc

FLD_TABLE = bytes((GAT2FLD.get(i, 0) & FLAG_MASK) for i in range(256))   # server cell type -> masked field value
MASK_TABLE = bytes((i & FLAG_MASK) for i in range(256))                   # masked field byte

KINDS = {"size": "field size", "cells": "walk cell", "flags": "snipe/water flag", "unreadable": "unreadable",
         "shadow": "uncompressed shadow",
         "portal-drop": "portal to drop", "portal-missing": "portal missing", "patch": "patch line"}


def problem(source, kind, map_name, detail):
    """source: where it was found; kind: a KINDS key; map: the map (or the portal's source map); detail: text"""
    return {"source": source, "kind": kind, "map": map_name, "detail": detail}


# ----------------------------------------------------------------- docker (read-only)
def docker_prefix():
    """(["docker"] or ["wsl", "docker"], container names) when a docker CLI answers; (None, set()) otherwise"""
    for pre in (["docker"], ["wsl", "docker"]):
        try:
            r = subprocess.run(pre + ["ps", "-a", "--format", "{{.Names}}"], capture_output=True, timeout=120)
        except (OSError, subprocess.SubprocessError):
            continue
        if r.returncode == 0:
            return pre, set(r.stdout.decode("utf-8", "replace").split())
    return None, set()


def docker_copy(pre, container, src, dst):
    """copy container:src (a file or a directory) to the local path dst. `docker cp ... -` streams a tar; nothing runs
    in the container. Raises OSError when the copy fails."""
    r = subprocess.run(pre + ["cp", "%s:%s" % (container, src), "-"], capture_output=True, timeout=900)
    if r.returncode != 0:
        raise OSError("docker cp %s:%s failed: %s" % (container, src, r.stderr.decode("utf-8", "replace").strip()))
    tmp = tempfile.mkdtemp(prefix="fieldcheck-")
    try:
        with tarfile.open(fileobj=io.BytesIO(r.stdout)) as tf:
            for m in tf.getmembers():
                if os.path.isabs(m.name) or ".." in m.name.replace("\\", "/").split("/"):
                    raise OSError("unsafe member in the docker cp stream: %r" % m.name)
            if hasattr(tarfile, "data_filter"):
                tf.extractall(tmp, filter="data")
            else:
                tf.extractall(tmp)
        top = os.path.join(tmp, os.path.basename(src.rstrip("/")))
        if os.path.lexists(dst):
            if os.path.isdir(dst):
                shutil.rmtree(dst)
            else:
                os.remove(dst)
        os.makedirs(os.path.dirname(dst), exist_ok=True)
        shutil.move(top, dst)
    finally:
        shutil.rmtree(tmp, ignore_errors=True)


# ----------------------------------------------------------------- the server truth
def server_root(pre, names, tmp, server_dir=None):
    """(root, label): a directory with db/map_cache.dat, db/re/map_cache.dat and npc/. (None, reason) when no source."""
    if server_dir:
        return server_dir, "server dir %s" % server_dir
    if pre and RATHENA in names:
        root = os.path.join(tmp, "rathena")
        docker_copy(pre, RATHENA, "/rathena/db/map_cache.dat", os.path.join(root, "db", "map_cache.dat"))
        docker_copy(pre, RATHENA, "/rathena/db/re/map_cache.dat", os.path.join(root, "db", "re", "map_cache.dat"))
        docker_copy(pre, RATHENA, "/rathena/npc", os.path.join(root, "npc"))
        return root, "ro-rathena (live, docker cp)"
    local = os.path.join(ROOT, "tools", "dev", "rathena-src")
    if os.path.isdir(os.path.join(local, "npc")) and os.path.exists(os.path.join(local, "db", "map_cache.dat")):
        return local, "tools/dev/rathena-src (git-ignored copy)"
    return None, "no server data: ro-rathena is not reachable and tools/dev/rathena-src is missing"


def server_truth(root):
    """(cache, warps, anynpc): cache maps name -> (xs, ys, compressed cells), the re cache over the base cache"""
    cache = load(os.path.join(root, "db", "map_cache.dat"))
    cache.update(load(os.path.join(root, "db", "re", "map_cache.dat")))
    warps, anynpc = server_warps(root)
    return cache, warps, anynpc


# ----------------------------------------------------------------- fields
def compare_field(got, entry):
    """got: an OpenKore field file's bytes (uncompressed). entry: the server's (xs, ys, compressed cells).
    Returns None when it matches, else (kind, detail)."""
    xs, ys, z = entry
    cells = zlib.decompress(z)
    if len(got) < 4:
        return "size", "truncated field file (%d bytes)" % len(got)
    w, h = struct.unpack("<HH", got[:4])
    if (w, h) != (xs, ys) or len(got) - 4 != len(cells):
        return "size", "field is %dx%d (%d cells), the server map is %dx%d (%d cells)" % (
            w, h, len(got) - 4, xs, ys, len(cells))
    a = got[4:].translate(MASK_TABLE)
    b = cells.translate(FLD_TABLE)
    if a == b:
        return None
    diff = [i for i in range(len(a)) if a[i] != b[i]]
    walk = [i for i in diff if (a[i] ^ b[i]) & 1]
    i = diff[0]
    where = "first at x=%d y=%d: field %d, server %d" % (i % xs, i // xs, a[i], b[i])
    if walk:  # the walk bit is what routes use
        return "cells", "%d of %d cells differ, %d on the walk bit (%s)" % (
            len(diff), len(a), len(walk), where)
    return "flags", "%d of %d cells differ on the snipe/water bits only, walk bit equal (%s)" % (
        len(diff), len(a), where)


def read_field_dir(path):
    """(fields, raws, bad): fields = {map: decompressed bytes} from *.fld2.gz; raws = {map: bytes} from uncompressed
    *.fld2 (they shadow the .gz in OpenKore); bad = [names of .gz files that do not decompress]"""
    fields, raws, bad = {}, {}, []
    for fn in sorted(os.listdir(path)):
        full = os.path.join(path, fn)
        if fn.endswith(".fld2.gz"):
            try:
                with gzip.open(full) as f:
                    fields[fn[:-8]] = f.read()
            except (OSError, EOFError, zlib.error):
                bad.append(fn[:-8])
        elif fn.endswith(".fld2"):
            with open(full, "rb") as f:
                raws[fn[:-5]] = f.read()
    return fields, raws, bad


def check_fields(source, path, cache):
    """every field file under path against the server cache. Returns (problems, info)."""
    fields, raws, bad = read_field_dir(path)
    probs = [problem(source, "unreadable", name, "the .fld2.gz does not decompress") for name in bad]
    probs += [problem(source, "shadow", name, "an uncompressed .fld2 shadows the .fld2.gz")
              for name in sorted(raws) if name in fields]
    notes, checked = [], 0
    items = sorted(list(fields.items()) + [(n, d) for n, d in raws.items() if n not in fields])
    for name, data in items:
        if name not in cache:
            notes.append("%s: not a server map (no cache entry), not compared" % name)
            continue
        checked += 1
        res = compare_field(data, cache[name])
        if res:
            probs.append(problem(source, res[0], name, res[1]))
    return probs, {"source": source, "files": len(items), "compared": checked, "notes": notes}


# ----------------------------------------------------------------- portals
def near(a, b, c, d, r):
    return abs(a - c) <= r and abs(b - d) <= r


def check_portals(source, lines, cache, warps, anynpc):
    """a portals.txt (its lines) against the server's loaded warps: the generator's drop and missing rules"""
    drop, add = diff_portals(cache, warps, anynpc, lines)
    probs = [problem(source, "portal-drop", line.split()[0], "portals.txt line the server does not have: %s" % line)
             for line in sorted(set(drop))]
    probs += [problem(source, "portal-missing", a.split()[0], "server warp with no portals.txt line: %s" % a)
              for a in add]
    return probs, {"source": source, "lines": len(portal_lines(lines)[0]), "drop": len(set(drop)), "missing": len(add)}


def repo_portal_patch(openkore_dir, cache, warps, anynpc):
    """the portal data the repo writes into every container (fix_re_portals.sh block, portals-srv.txt '+' lines):
    the block's plain lines must still be server warps or NPC portals (no drop), every '+' line must be a server warp"""
    source = "repo portal patches"
    probs = []
    sh = os.path.join(openkore_dir, "fix_re_portals.sh")
    if os.path.exists(sh):
        with open(sh, encoding="utf-8") as f:
            m = re.search(r"<<'EOT'\n(.*?)\nEOT\n", f.read(), re.S)
        block = m.group(1).split("\n") if m else []
        if not block:
            probs.append(problem(source, "patch", "-", "fix_re_portals.sh has no EOT block"))
        else:
            drop, _ = diff_portals(cache, warps, anynpc, block)
            probs += [problem(source, "patch", line.split()[0],
                              "fix_re_portals.sh line the server does not have: " + line) for line in sorted(set(drop))]
    srv = os.path.join(openkore_dir, "portals-srv.txt")
    if os.path.exists(srv):
        with open(srv, encoding="utf-8") as f:
            patch = f.read().split("\n")
        for raw in patch:
            if not raw.startswith("+ "):
                continue
            s = raw[2:].split()
            try:
                t = (s[0], int(s[1]), int(s[2]), s[3], int(s[4]), int(s[5]))
            except (IndexError, ValueError):
                probs.append(problem(source, "patch", "-", "unreadable + line: " + raw))
                continue
            # the generator's "fix" adds the portal's own source cell (within 3 cells of the warp) with the warp's landing
            if not any(w[0] == t[0] and w[3] == t[3] and (w[4], w[5]) == (t[4], t[5]) and near(w[1], w[2], t[1], t[2], 3)
                       for w in warps):
                probs.append(problem(source, "patch", s[0], "+ line that is not a server warp: " + raw))
    return probs, {"source": source}


# ----------------------------------------------------------------- the whole check
def run_all(server_dir=None, repo=True, containers=None, field_dirs=(), no_docker=False, openkore_dir=OPENKORE):
    """Runs the check. containers: None = every OK_CONTAINERS that exists, [] = none, or names.
    Returns (problems, infos). Raises OSError when the server data cannot be read (exit 2 for the CLI)."""
    pre, names = (None, set()) if no_docker else docker_prefix()
    tmp = tempfile.mkdtemp(prefix="fieldcheck-root-")
    try:
        root, label = server_root(pre, names, tmp, server_dir)
        if root is None:
            raise OSError(label)
        cache, warps, anynpc = server_truth(root)
        infos = [{"source": "server", "label": label, "maps": len(cache), "warps": len(warps)}]
        probs = []
        if repo:
            for sub in ("fields-re", "fields-srv"):
                p, i = check_fields("repo " + sub, os.path.join(openkore_dir, sub), cache)
                probs += p
                infos.append(i)
            p, i = repo_portal_patch(openkore_dir, cache, warps, anynpc)
            probs += p
            infos.append(i)
        for d in field_dirs:
            p, i = check_fields("dir " + d, d, cache)
            probs += p
            infos.append(i)
        if containers is None:
            containers = [c for c in OK_CONTAINERS if c in names]
        for c in containers:
            if c not in names:
                infos.append({"source": c, "absent": True})
                continue
            try:
                p, i = check_fields(c, _copy_fields(pre, c, tmp), cache)
                probs += p
                infos.append(i)
                portals = os.path.join(tmp, c, "portals.txt")
                docker_copy(pre, c, "/openkore/tables/portals.txt", portals)
                with open(portals, encoding="latin1") as f:
                    p, i = check_portals(c, f.readlines(), cache, warps, anynpc)
                probs += p
                infos.append(i)
            except OSError as e:
                probs.append(problem(c, "unreadable", "-", str(e)))
        return probs, infos
    finally:
        shutil.rmtree(tmp, ignore_errors=True)


def _copy_fields(pre, container, tmp):
    fdir = os.path.join(tmp, container, "fields")
    docker_copy(pre, container, "/openkore/fields", fdir)
    return fdir


def report(probs, infos, out=print):
    for i in infos:
        if i.get("source") == "server":
            out("server data: %s, %d map(s), %d warp(s)" % (i["label"], i["maps"], i["warps"]))
        elif i.get("absent"):
            out("%s: not present (skipped)" % i["source"])
        elif "compared" in i:
            out("%s: %d field file(s), %d compared" % (i["source"], i["files"], i["compared"]))
        elif "drop" in i:
            out("%s: %d plain portal line(s), %d to drop, %d missing" % (
                i["source"], i["lines"], i["drop"], i["missing"]))
    for p in probs:
        out("MISMATCH %s %s: %s: %s" % (p["source"], p["map"], KINDS.get(p["kind"], p["kind"]), p["detail"]))
    if probs:
        sources = sorted({p["source"] for p in probs})
        out("%d problem(s) in %d source(s): %s" % (len(probs), len(sources), ", ".join(sources)))
    else:
        out("OK: every field file and portal line matches the server")


def main(argv=None):
    ap = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    ap.add_argument("--repo-only", action="store_true", help="the repo copies only, no container")
    ap.add_argument("--container", action="append", default=None, help="check this container (repeatable)")
    ap.add_argument("--server-dir", help="server data directory (db/, npc/) instead of ro-rathena")
    ap.add_argument("--field-dir", action="append", default=[], help="also check this field directory")
    a = ap.parse_args(argv)
    containers = [] if a.repo_only else a.container
    try:
        probs, infos = run_all(server_dir=a.server_dir, containers=containers, field_dirs=a.field_dir)
    except OSError as e:
        print("could not run the check: %s" % e)
        return 2
    report(probs, infos)
    return 1 if probs else 0


if __name__ == "__main__":
    sys.exit(main())
