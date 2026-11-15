"""Shared helpers for the system-agent tools (stdlib only; runs on Windows Python and WSL python3).

Paths: everything runtime lives in <repo>/legion/data (git-ignored files). Tests point LEGION_DATA at a temp dir.
Markers (plain files in the data dir; presence is the signal):
  stack.up    the stack should be up (the owner said start). mindgate/sentinel check heartbeats only then.
  stack.hold  the owner has the stack stopped: keeper.sh must not start any container.
Heartbeats (mtime is the heartbeat): sentinel.heartbeat, partycheck.heartbeat, and the dispatcher's own file.
"""
import json
import os
import time

ROOT = os.path.abspath(os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", ".."))


def data_dir():
    return os.environ.get("LEGION_DATA") or os.path.join(ROOT, "legion", "data")


def dpath(name):
    return os.path.join(data_dir(), name)


def now():
    """Fake-able clock: LEGION_FAKE_NOW=<epoch seconds> pins it (tests, dry runs)."""
    v = os.environ.get("LEGION_FAKE_NOW")
    if v:
        try:
            return float(v)
        except ValueError:
            pass
    return time.time()


def iso(t=None):
    return time.strftime("%Y-%m-%dT%H:%M:%S", time.localtime(now() if t is None else t))


def read_jsonl(path):
    out = []
    try:
        with open(path, "r", encoding="utf-8") as f:
            for line in f:
                line = line.strip()
                if not line:
                    continue
                try:
                    o = json.loads(line)
                except ValueError:
                    continue
                if isinstance(o, dict):
                    out.append(o)
    except OSError:
        pass
    return out


def append_jsonl(path, obj):
    """Append one line. A single write() of a short line in append mode is atomic enough for our writers."""
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "a", encoding="utf-8", newline="\n") as f:
        f.write(json.dumps(obj, ensure_ascii=False, separators=(",", ":")) + "\n")


def to_epoch(v):
    """Epoch seconds from a number, a numeric string or an ISO-like string; None if unparseable."""
    if v is None:
        return None
    if isinstance(v, (int, float)):
        return float(v) / 1000.0 if v > 1e11 else float(v)
    s = str(v).strip()
    try:
        return to_epoch(float(s))
    except ValueError:
        pass
    s = s.replace("Z", "")
    for fmt in ("%Y-%m-%dT%H:%M:%S", "%Y-%m-%d %H:%M:%S", "%Y-%m-%dT%H:%M"):
        try:
            return time.mktime(time.strptime(s[:19], fmt))
        except ValueError:
            continue
    return None


def heartbeat(name):
    """Touch <data>/<name>.heartbeat (content: epoch). Never raises."""
    try:
        os.makedirs(data_dir(), exist_ok=True)
        with open(dpath(name + ".heartbeat"), "w") as f:
            f.write(str(int(now())))
    except Exception:
        pass


def heartbeat_age(path):
    """Seconds since the heartbeat file was written (content epoch, else mtime); None if the file is missing."""
    try:
        age_m = now() - os.path.getmtime(path)
    except OSError:
        return None
    try:
        with open(path) as f:
            t = float(f.read().strip().split()[0])
        return max(0.0, now() - t)
    except Exception:
        return max(0.0, age_m)


def marker_age(name):
    try:
        return now() - os.path.getmtime(dpath(name))
    except OSError:
        return None
