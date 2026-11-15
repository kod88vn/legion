#!/usr/bin/env python3
"""fieldcheck.py - every OpenKore field file and portal line against the server's map cache and loaded warps.

  python tools/system/fieldcheck.py                  repo field files (openkore/fields-re) and portal patch data,
                                                     AND the OpenKore containers in use (ro-openkore-ayla; any
                                                     running OpenKore container)
  python tools/system/fieldcheck.py --repo-only      the repo copies only (what gatecheck runs)
  python tools/system/fieldcheck.py --container NAME ...   only these containers
  python tools/system/fieldcheck.py --server-dir DIR     server data from DIR (db/, db/re/, npc/) instead of the snapshot
  python tools/system/fieldcheck.py --field-dir DIR      also check one more field directory (a /openkore/fields copy)

Server truth: the SNAPSHOT tools/dev/mapsnap (git-ignored): db/map_cache.dat with db/re/map_cache.dat over it, npc/ (the
warps the server loads), warps.json (the parsed warps). ensure_snapshot() refreshes it from the live ro-rathena container
(docker cp, read-only) when it is missing, older than a day, or the server's image changed. With no docker it uses the
last snapshot with a warning; with neither it raises OSError (gatecheck then warns and skips).
Containers are read with `docker cp <name>:<path> -`, so a STOPPED one is checked too. Nothing is started or written.

Cell rule: every bit of a field cell is compared (the field byte is the converter's output, gen_fields_from_mapcache).
A cell whose walk bit differs is kind "cells" (routing); the others are kind "flags" (snipe/water bits only).
Portal rule: the generator's rules (gen_portals_from_npc.diff_portals): a plain portal line the server does not have is a
DROP; a server warp with no portal line is a MISSING portal.

Which containers count: IN_USE (the ones the compose profiles start: Ayla always, the QA and legacy ones only by profile)
plus any container that is running. An in-use container that is stopped is still checked and reported.
Exit: 0 all match, 1 mismatches, 2 could not read the server data. Standard library only; Windows Python and WSL.
"""
import argparse
import gzip
import io
import json
import os
import re
import shutil
import struct
import subprocess
import sys
import tarfile
import tempfile
import time
import zlib

ROOT = os.path.normpath(os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", ".."))
OPENKORE = os.path.join(ROOT, "openkore")
sys.path.insert(0, OPENKORE)
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from gen_fields_from_mapcache import GAT2FLD, load  # noqa: E402
from gen_portals_from_npc import diff_portals, portal_lines, server_warps  # noqa: E402
import common as C  # noqa: E402

OK_CONTAINERS = ("ro-openkore-ayla", "ro-openkore-qa", "ro-openkore")
IN_USE = ("ro-openkore-ayla",)   # the compose profiles: Ayla always on; qa (profile qa) and legacy (profile legacy) off
RATHENA = "ro-rathena"
SNAP = os.path.join(ROOT, "tools", "dev", "mapsnap")
SNAP_MAX_AGE = 86400

FLD_TABLE = bytes((GAT2FLD.get(i, 0)) for i in range(256))   # server cell type -> the field byte, every bit


def problem(source, kind, map_name, detail):
    """source: where it was found; kind: a KINDS key; map: the map (or the portal's source map); detail: text"""
    return {"source": source, "kind": kind, "map": map_name, "detail": detail}


KINDS = {"size": "field size", "cells": "walk cell", "flags": "snipe/water flag", "unreadable": "unreadable",
         "shadow": "uncompressed shadow", "portal-drop": "portal to drop", "portal-missing": "portal missing",
         "patch": "patch line"}


# ----------------------------------------------------------------- docker (read-only)
def docker_states(tick=None):
    """({name: state}, prefix) for every container, or (None, None) when docker cannot be asked"""
    pre = C.docker_prefix()
    try:
        r = subprocess.run(pre + ["ps", "-a", "--format", "{{.Names}}\t{{.State}}"], capture_output=True, timeout=120)
    except (OSError, subprocess.SubprocessError):
        return None, None
    if r.returncode != 0:
        return None, None
    states = {}
    for line in r.stdout.decode("utf-8", "replace").splitlines():
        name, _, state = line.partition("\t")
        states[name.strip()] = state.strip()
    return states, pre


def docker_copy(pre, container, src, dst, tick=None):
    """copy container:src (a file or a directory) to the local path dst. `docker cp ... -` streams a tar; nothing runs
    in the container. Raises OSError when the copy fails. tick() is called before each copy (the watchdog refreshes
    its lock there)."""
    if tick:
        tick()
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


# ----------------------------------------------------------------- the server snapshot
def snapshot_needed(meta, image, now, max_age=SNAP_MAX_AGE):
    """(needed, reason): refreshed when there is no snapshot, it is older than a day, or the server image changed"""
    if not meta:
        return True, "no snapshot yet"
    if now - float(meta.get("t", 0)) >= max_age:
        return True, "older than a day"
    if image and meta.get("image") != image:
        return True, "server image changed"
    return False, "fresh"


def _read_meta(snap):
    try:
        with open(os.path.join(snap, "meta.json"), encoding="utf-8") as f:
            return json.load(f)
    except (OSError, ValueError):
        return None


def ensure_snapshot(states, pre, now=None, tick=None, snap=SNAP):
    """(root, label, warning): the server snapshot, refreshed from ro-rathena when needed. root is None when there is no
    snapshot and no docker (then the caller cannot check and must say so)."""
    now = time.time() if now is None else now
    meta = _read_meta(snap)
    live = bool(pre) and states is not None and RATHENA in states
    image = None
    if live:
        r = subprocess.run(pre + ["inspect", "--format", "{{.Image}}", RATHENA], capture_output=True, timeout=120)
        image = r.stdout.decode("utf-8", "replace").strip() if r.returncode == 0 else None
    needed, why = snapshot_needed(meta, image, now)
    if live and needed:
        new = snap + ".new"
        shutil.rmtree(new, ignore_errors=True)
        os.makedirs(os.path.join(new, "db", "re"))
        docker_copy(pre, RATHENA, "/rathena/db/map_cache.dat", os.path.join(new, "db", "map_cache.dat"), tick)
        docker_copy(pre, RATHENA, "/rathena/db/re/map_cache.dat", os.path.join(new, "db", "re", "map_cache.dat"), tick)
        docker_copy(pre, RATHENA, "/rathena/npc", os.path.join(new, "npc"), tick)
        warps, anynpc = server_warps(new)
        with open(os.path.join(new, "warps.json"), "w", encoding="utf-8") as f:
            json.dump({"warps": warps, "anynpc": anynpc}, f)
        with open(os.path.join(new, "meta.json"), "w", encoding="utf-8") as f:
            json.dump({"t": now, "image": image, "warps": len(warps)}, f)
        shutil.rmtree(snap, ignore_errors=True)
        os.replace(new, snap)
        return snap, "ro-rathena snapshot refreshed (%s)" % why, None
    if meta and os.path.isdir(os.path.join(snap, "npc")):
        warn = None if not needed else "ro-rathena not reachable: using the snapshot from %d s ago (%s)" % (
            now - float(meta.get("t", now)), why)
        return snap, "snapshot", warn
    return None, None, "no server data: ro-rathena is not reachable and there is no snapshot"


def server_truth(root):
    """(cache, warps, anynpc): cache maps name -> (xs, ys, compressed cells), the re cache over the base cache.
    warps come from warps.json when the snapshot has it (parsed at refresh), else from the npc tree."""
    cache = load(os.path.join(root, "db", "map_cache.dat"))
    cache.update(load(os.path.join(root, "db", "re", "map_cache.dat")))
    wj = os.path.join(root, "warps.json")
    if os.path.exists(wj):
        with open(wj, encoding="utf-8") as f:
            d = json.load(f)
        warps = [tuple(w) for w in d["warps"]]
        anynpc = {k: [tuple(x) for x in v] for k, v in d["anynpc"].items()}
        return cache, warps, anynpc
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
    a = got[4:]
    b = cells.translate(FLD_TABLE)
    if a == b:
        return None
    diff = [i for i in range(len(a)) if a[i] != b[i]]
    walk = [i for i in diff if (a[i] ^ b[i]) & 1]
    i = diff[0]
    where = "first at x=%d y=%d: field %d, server %d" % (i % xs, i // xs, a[i], b[i])
    if walk:  # the walk bit is what routes use
        return "cells", "%d of %d cells differ, %d on the walk bit (%s)" % (len(diff), len(a), len(walk), where)
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
def check_portals(source, lines, cache, warps, anynpc):
    """a portals.txt (its lines) against the server's loaded warps: the generator's drop and missing rules"""
    drop, add = diff_portals(cache, warps, anynpc, lines)
    probs = [problem(source, "portal-drop", line.split()[0], "portals.txt line the server does not have: %s" % line)
             for line in sorted(set(drop))]
    probs += [problem(source, "portal-missing", a.split()[0], "server warp with no portals.txt line: %s" % a)
              for a in add]
    return probs, {"source": source, "lines": len(portal_lines(lines)[0]), "drop": len(set(drop)), "missing": len(add)}


def near(a, b, c, d, r):
    return abs(a - c) <= r and abs(b - d) <= r


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
def run_all(server_dir=None, repo=True, containers=None, field_dirs=(), no_docker=False, openkore_dir=OPENKORE,
            tick=None, now=None):
    """Runs the check. repo: the repo field files and portal patches. containers: None = IN_USE plus every running
    OpenKore container, [] = none, or names. Returns (problems, infos); every info has source. The server data is the
    snapshot (refreshed when due); server_dir overrides it. Raises OSError when there is no server data at all."""
    states, pre = (None, None) if no_docker else docker_states()
    tmp = tempfile.mkdtemp(prefix="fieldcheck-")
    try:
        if server_dir:
            root, label, warn = server_dir, "server dir %s" % server_dir, None
        else:
            root, label, warn = ensure_snapshot(states, pre, now=now, tick=tick)
        if root is None:
            raise OSError(warn)
        cache, warps, anynpc = server_truth(root)
        infos = [{"source": "server", "label": label, "maps": len(cache), "warps": len(warps), "warning": warn}]
        probs = []
        if repo:
            p, i = check_fields("repo fields-re", os.path.join(openkore_dir, "fields-re"), cache)
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
            running = {n for n, s in (states or {}).items() if s == "running"}
            containers = [c for c in OK_CONTAINERS if c in IN_USE or c in running]
        for c in containers:
            if states is None or c not in states:
                infos.append({"source": c, "absent": True, "active": False})
                continue
            active = c in IN_USE or states.get(c) == "running"
            try:
                fdir = os.path.join(tmp, c, "fields")
                docker_copy(pre, c, "/openkore/fields", fdir, tick)
                p, i = check_fields(c, fdir, cache)
                probs += p
                portals = os.path.join(tmp, c, "portals.txt")
                docker_copy(pre, c, "/openkore/tables/portals.txt", portals, tick)
                with open(portals, encoding="latin1") as f:
                    p2, i2 = check_portals(c, f.readlines(), cache, warps, anynpc)
                probs += p2
                i.update({"state": states.get(c), "active": active, "drop": i2["drop"], "missing": i2["missing"],
                          "lines": i2["lines"]})
                infos.append(i)
            except OSError as e:
                probs.append(problem(c, "unreadable", "-", str(e)))
                infos.append({"source": c, "active": active, "state": states.get(c), "unreadable": True})
        return probs, infos
    finally:
        shutil.rmtree(tmp, ignore_errors=True)


def report(probs, infos, out=print):
    for i in infos:
        if i.get("source") == "server":
            out("server data: %s, %d map(s), %d warp(s)" % (i["label"], i["maps"], i["warps"]))
            if i.get("warning"):
                out("WARNING: %s" % i["warning"])
        elif i.get("absent"):
            out("%s: not present (skipped)" % i["source"])
        elif "compared" in i:
            out("%s: %d field file(s), %d compared" % (i["source"], i["files"], i["compared"]))
        elif "drop" in i:
            out("%s (%s): %d plain portal line(s), %d to drop, %d missing" % (
                i["source"], i.get("state"), i["lines"], i["drop"], i["missing"]))
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
    ap.add_argument("--server-dir", help="server data directory (db/, npc/) instead of the snapshot")
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
