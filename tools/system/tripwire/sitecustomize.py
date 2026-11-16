"""G23 audit tripwire: no Python process of a suite run may open, write, rename or remove a file under a live folder.

Loaded only by tools/system/runsuites.py: its children get this folder on PYTHONPATH (Python imports a sitecustomize
module at start-up) and LEGION_TRIPWIRE_LIVE (the live folders, os.pathsep-separated) and LEGION_TRIPWIRE_READ (the
git-tracked files under them, which may be read: committed config, never running state). Does nothing without
LEGION_TRIPWIRE_LIVE.

Why an audit hook and not the resolver alone: common.refuse_live checks only the paths the tools resolve. A test (or a
grandchild process that a test starts with LEGION_TEST removed) can open a live lock or state file by its own path and
never reach the resolver. The audit hook sees every open in every process of the run, so such a test fails on the spot.
"""
import os
import sys

_LIVE = [os.path.normcase(os.path.abspath(p)) for p in os.environ.get("LEGION_TRIPWIRE_LIVE", "").split(os.pathsep) if p]
_READ_OK = set(os.path.normcase(os.path.abspath(p)) for p in os.environ.get("LEGION_TRIPWIRE_READ", "").split(os.pathsep) if p)
_TWO = {"os.rename", "os.replace", "os.link", "os.symlink"}
_ONE = {"os.remove", "os.unlink", "os.utime", "os.mkdir", "os.rmdir", "os.truncate"}
_WRITE_FLAGS = os.O_WRONLY | os.O_RDWR | os.O_CREAT | os.O_TRUNC | os.O_APPEND


def _norm(p):
    """the absolute, case-normalised path of p, or None for a file descriptor or a non-path"""
    if p is None or isinstance(p, int):
        return None
    try:
        p = os.fspath(p)
    except TypeError:
        return None
    if isinstance(p, bytes):
        p = os.fsdecode(p)
    return os.path.normcase(os.path.abspath(p))


def _under_live(a):
    for live in _LIVE:
        if a == live or a.startswith(live + os.sep):
            return live
    return None


def _writing(mode, flags):
    if isinstance(mode, str) and any(c in mode for c in "wax+"):
        return True
    return isinstance(flags, int) and bool(flags & _WRITE_FLAGS)


def _hook(event, args):
    if event == "open":
        path, mode, flags = (list(args) + [None, None, None])[:3]
        a = _norm(path)
        if a is None:
            return
        live = _under_live(a)
        if live is None:
            return
        if not _writing(mode, flags) and a in _READ_OK:
            return                                      # committed config: may be read
        raise RuntimeError("G23 tripwire: a test opened %s under the live folder %s (open for %s). Tests use their own "
                           "temp folder (tests/testenv.py); they never read or write running state."
                           % (path, live, "write" if _writing(mode, flags) else "read"))
    if event in _TWO or event in _ONE:
        for p in (args[:2] if event in _TWO else args[:1]):
            a = _norm(p)
            live = _under_live(a) if a is not None else None
            if live is not None:
                raise RuntimeError("G23 tripwire: %s touched %s under the live folder %s" % (event, p, live))


if _LIVE:
    sys.addaudithook(_hook)
