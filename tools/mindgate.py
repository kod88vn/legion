#!/usr/bin/env python3
"""Session gate for Claude Code hooks on this project (redesign 2026-11-14; the old "15 minds must be alive" gate is gone:
the world runs without AI agents).

When the owner has said the stack should be up (marker legion/data/stack.up, written by tools/system/stackctl.py up)
it checks that the two scripts that keep it honest are alive, by heartbeat file age:
  - the puppet dispatcher (tools/puppet/dispatch.py)  -> legion/data/dispatch.heartbeat (aliases below)
  - the sentinel (tools/sentinel.py)                  -> legion/data/sentinel.heartbeat
and reports a problem ONCE (state in legion/data/mindgate.state.json; again only if the set of problems changes or
60 minutes pass). It never blocks a turn for more than one message and never fails: any exception, a missing marker,
a pause flag or a healthy stack all end in exit 0 with no output.

  mindgate.py prompt  -> UserPromptSubmit hook: injects one warning into the model's context.
  mindgate.py stop    -> Stop hook: blocks the turn once for a NEW problem (stop_hook_active prevents loops).
Pause flag: while the file tools/mindgate.paused sits next to this script the gate is silent.
Wired in G:\\RO\\.claude\\settings.json (through the shim G:\\RO\\tools\\mindgate.py) and legion\\.claude\\settings.json.
"""
import json
import os
import sys
import time

HERE = os.path.dirname(os.path.abspath(__file__))
DISPATCH_FILES = ["dispatch.heartbeat", "dispatcher.heartbeat", "puppet.heartbeat"]
SENTINEL_FILE = "sentinel.heartbeat"
DISPATCH_STALE_S = 300
SENTINEL_STALE_S = 420
GRACE_S = 180        # after stack.up is written, give the scripts time to start
REPEAT_S = 3600      # re-report a problem that persists this long


def data_dir():
    return os.environ.get("LEGION_DATA") or os.path.join(HERE, "..", "legion", "data")


def _now():
    try:
        return float(os.environ["LEGION_FAKE_NOW"])
    except Exception:
        return time.time()


def _age(path, now):
    """Seconds since the heartbeat (epoch inside the file, else mtime); None if the file does not exist."""
    try:
        m = os.path.getmtime(path)
    except OSError:
        return None
    try:
        with open(path) as f:
            return max(0.0, now - float(f.read().strip().split()[0]))
    except Exception:
        return max(0.0, now - m)


def problems(now=None):
    """[(key, text)] - empty when the stack is not expected up, still in its start grace, or healthy."""
    now = _now() if now is None else now
    d = data_dir()
    try:
        up_age = now - os.path.getmtime(os.path.join(d, "stack.up"))
    except OSError:
        return []
    if up_age < GRACE_S:
        return []
    out = []
    ages = [(_age(os.path.join(d, n), now), n) for n in DISPATCH_FILES]
    ages = [a for a in ages if a[0] is not None]
    if not ages:
        out.append(("dispatcher", "the puppet dispatcher has no heartbeat file (tools/puppet/dispatch.py not running?)"))
    elif min(ages)[0] > DISPATCH_STALE_S:
        out.append(("dispatcher", "the puppet dispatcher heartbeat is %d min old" % (min(ages)[0] // 60)))
    a = _age(os.path.join(d, SENTINEL_FILE), now)
    if a is None:
        out.append(("sentinel", "the sentinel has no heartbeat file (tools/sentinel.py not running?)"))
    elif a > SENTINEL_STALE_S:
        out.append(("sentinel", "the sentinel heartbeat is %d min old" % (a // 60)))
    return out


def _state_path():
    return os.path.join(data_dir(), "mindgate.state.json")


def _load_state():
    try:
        with open(_state_path()) as f:
            return json.load(f)
    except Exception:
        return {}


def _save_state(s):
    try:
        with open(_state_path(), "w") as f:
            json.dump(s, f)
    except Exception:
        pass


def evaluate(mode, inp, now=None):
    """Hook output dict, or None for silence. Records that the problem was reported (once per problem)."""
    now = _now() if now is None else now
    probs = problems(now)
    sig = ",".join(sorted(k for k, _ in probs))
    st = _load_state()
    if not probs:
        if st.get("sig"):
            _save_state({"sig": "", "t": now})
        return None
    if st.get("sig") == sig and now - st.get("t", 0) < REPEAT_S:
        return None   # already told about exactly this
    if mode == "stop" and (inp or {}).get("stop_hook_active"):
        return None
    msg = ("STACK GATE: the stack is marked up (legion/data/stack.up) but " + "; ".join(t for _, t in probs) +
           ". Tell the owner and follow the legion-session skill to restart the missing piece. If the owner stopped the "
           "stack on purpose, run: python tools/system/stackctl.py down. (Reported once; the gate stays quiet until "
           "something changes.)")
    _save_state({"sig": sig, "t": now})
    if mode == "stop":
        return {"decision": "block", "reason": msg}
    return {"hookSpecificOutput": {"hookEventName": "UserPromptSubmit", "additionalContext": msg}, "systemMessage": msg}


def main():
    # Pause flag: while a file "mindgate.paused" sits next to this script the gate is silent (exit 0, no output).
    if os.path.exists(os.path.join(HERE, "mindgate.paused")):
        return
    mode = sys.argv[1] if len(sys.argv) > 1 else "prompt"
    try:
        inp = json.load(sys.stdin)
    except Exception:
        inp = {}
    out = evaluate(mode, inp)
    if out:
        print(json.dumps(out))


if __name__ == "__main__":
    try:
        main()
    except BaseException:
        pass
    sys.exit(0)
