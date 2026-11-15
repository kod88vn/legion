"""Shared helpers for the system-agent tools (stdlib only; runs on Windows Python and WSL python3).

Paths: everything runtime lives in <repo>/legion/data (git-ignored files). Tests point LEGION_DATA at a temp dir.
Markers (plain files in the data dir; presence is the signal):
  stack.up    the stack should be up (the owner said start). mindgate/sentinel check heartbeats only then.
  stack.hold  the owner has the stack stopped: keeper.sh must not start any container.
Heartbeats (mtime is the heartbeat): sentinel.heartbeat, partycheck.heartbeat, and the dispatcher's own file.
"""
import atexit
import json
import os
import time

ROOT = os.path.abspath(os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", ".."))


LIVE_DATA = os.path.join(ROOT, "legion", "data")
LIVE_AYLA_LOGS = os.path.join(ROOT, "openkore-ayla", "logs")   # Ayla's OpenKore logs (mounted into her container)


def refuse_live(path, what="path"):
    """Under LEGION_TEST=1 (set by every test process) raise when path is the live legion/data or lies under Ayla's
    live logs folder (rule G23). Otherwise return path unchanged. Every resolver of a live location goes through here."""
    if os.environ.get("LEGION_TEST") == "1":
        p = os.path.normcase(os.path.abspath(path))
        for live in (LIVE_DATA, LIVE_AYLA_LOGS):
            L = os.path.normcase(os.path.abspath(live))
            if p == L or p.startswith(L + os.sep):
                raise RuntimeError("LEGION_TEST=1 but %s is in the live %s (rule G23): give the test its own path"
                                   % (what, live))
    return path


def data_dir():
    """The one resolver for the data folder: LEGION_DATA, else the live legion/data. Refused under LEGION_TEST=1
    (rule G23) when it would be the live folder."""
    return refuse_live(os.environ.get("LEGION_DATA") or LIVE_DATA, "the data folder")


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


def heartbeat(name, note=""):
    """Touch <data>/<name>.heartbeat (content: epoch, then an optional note such as "waiting for the main session").
    Never raises."""
    try:
        os.makedirs(data_dir(), exist_ok=True)
        with open(dpath(name + ".heartbeat"), "w") as f:
            f.write(str(int(now())) + ((" " + note) if note else ""))
    except Exception:
        pass


def heartbeat_note(path):
    """The note after the epoch in a heartbeat file ("" when there is none or the file is missing)."""
    try:
        with open(path) as f:
            parts = f.read().strip().split(None, 1)
        return parts[1] if len(parts) > 1 else ""
    except OSError:
        return ""


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


WSL_DISTRO = os.environ.get("LEGION_WSL_DISTRO") or "Ubuntu-24.04"   # the one WSL setting (dashboard, watchdog)


def docker_prefix():
    """the one docker command line for every script (watchdog, fieldcheck): wsl.exe -d <distro> -- docker on Windows"""
    if os.name == "nt":
        return ["wsl.exe", "-d", WSL_DISTRO, "--", "docker"]
    return ["docker"]


def lock_owner(name):
    """pid written in legion/data/<name> by the lock holder, or None"""
    try:
        with open(dpath(name), encoding="utf-8") as f:
            return int(f.read().split()[0])
    except (OSError, ValueError, IndexError):
        return None


_HELD = {}          # lock name -> pid, for the locks this process took; released on exit while the pid still owns them
_INSTALLED = [False]


def lock_owner_is_me(name, pid=None):
    return lock_owner(name) == (os.getpid() if pid is None else pid)


def release_lock(name, pid=None):
    """Remove legion/data/<name> if this process (or pid) still owns it. Never removes another holder's lock."""
    me = os.getpid() if pid is None else pid
    try:
        if lock_owner(name) == me:
            os.remove(dpath(name))
    except OSError:
        pass
    _HELD.pop(name, None)


def _release_held():
    for name, pid in list(_HELD.items()):
        release_lock(name, pid)


def _install_release():
    """atexit covers sys.exit (also sentinel's exit 2 wake), normal returns and Ctrl+C. SIGTERM and SIGBREAK become a
    normal exit so atexit runs too. A hard kill (SIGKILL, power loss) leaves the lock to go stale: take_lock replaces it
    after `stale` seconds."""
    if _INSTALLED[0]:
        return
    _INSTALLED[0] = True
    atexit.register(_release_held)
    import signal
    import threading
    if threading.current_thread() is threading.main_thread():
        for s in ("SIGTERM", "SIGBREAK"):
            sig = getattr(signal, s, None)
            if sig is None:
                continue
            try:
                signal.signal(sig, lambda signum, frame: sys.exit(128 + signum))
            except (ValueError, OSError):
                pass


def _age_of(path):
    """seconds since the file's time on the (fakeable) clock; None when it cannot be read"""
    try:
        return now() - os.path.getmtime(path)
    except OSError:
        return None


def _put_back(aside, p):
    """Restore a lock that was moved aside by mistake. A link fails if p exists, so a lock created meanwhile is never
    overwritten; the aside copy is then dropped (its owner finds the owner pid differs and exits)."""
    try:
        os.link(aside, p)
    except FileExistsError:
        pass
    except OSError:
        if not os.path.exists(p):
            try:
                os.replace(aside, p)
            except OSError:
                pass
    try:
        os.remove(aside)
    except OSError:
        pass


def take_lock(name, stale, pid=None):
    """Single instance, atomic. legion/data/<name> is created exclusively (O_EXCL) holding "<pid> <epoch>". A lock
    refreshed within `stale` seconds belongs to a live holder: False. An older lock (a crashed holder) is first moved
    aside with one atomic rename, so of two simultaneous starters only one can take it over; the O_EXCL create then
    decides. The lock is released on exit by this process (see _release_held)."""
    p = dpath(name)
    me = os.getpid() if pid is None else pid
    try:
        os.makedirs(data_dir(), exist_ok=True)
    except OSError:
        return False
    for _ in range(3):
        try:
            fd = os.open(p, os.O_WRONLY | os.O_CREAT | os.O_EXCL)
        except FileExistsError:
            age = marker_age(name)
            if age is None:
                continue                      # vanished between the calls: try the create again
            if age <= stale:
                return False                  # a live holder
            aside = "%s.stale.%d" % (p, me)
            try:
                os.replace(p, aside)          # atomic: only one starter can move the stale lock
            except PermissionError:
                return False                  # Windows: the file is open in another process, so it is held
            except OSError:
                return False                  # another starter moved it first and owns the takeover
            moved_age = _age_of(aside)
            if moved_age is None or moved_age <= stale:
                # not the stale lock after all: a starter created a fresh one between our look and the move
                _put_back(aside, p)
                return False
            try:
                os.remove(aside)
            except OSError:
                pass
            continue
        except OSError:
            return False
        with os.fdopen(fd, "w") as f:
            f.write("%d %d\n" % (me, int(now())))
        t = now()
        try:
            os.utime(p, (t, t))               # the lock's time is the (fakeable) clock, like every later refresh
        except OSError:
            pass
        _HELD[name] = me
        _install_release()
        return True
    return False


def refresh_lock(name, pid=None):
    """The holder keeps its lock's time current. False when the lock belongs to someone else now: the holder must exit."""
    if lock_owner(name) != (os.getpid() if pid is None else pid):
        return False
    t = now()
    try:
        os.utime(dpath(name), (t, t))
    except OSError:
        return False
    return True


def marker_age(name):
    try:
        return now() - os.path.getmtime(dpath(name))
    except OSError:
        return None
