#!/usr/bin/env python3
"""AY24: is Ayla played by the Legion engine yet? The state is the tuning key engine_parties (legion/tuning.go): a
comma-separated list of human names whose party runs on the engine. "ayla" in it = the switch is on for her.

Off (missing file, missing key, empty, or no "ayla") = OpenKore era: the watcher's brief keeps its steering section.
On = engine era: Ayla is an ordinary engine party with a watcher; the watcher files bugs and never steers.
Standard library only; reads the live data folder only through tools/system/common.py (rule G23).
"""
import json
import os
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, os.path.join(HERE, "..", "system"))
import common as C  # noqa: E402  the one resolver for live locations (rule G23)

NAME = "ayla"


def tuning_path():
    return os.path.join(C.data_dir(), "tuning.json")


def engine_names(data):
    """the names in a tuning dict's engine_parties (lower case); tolerant of a list or a missing key"""
    v = data.get("engine_parties") if isinstance(data, dict) else None
    if isinstance(v, list):
        v = ",".join(str(x) for x in v)
    return {n.strip().lower() for n in str(v or "").split(",") if n.strip()}


def engine_on(path=None):
    """True when Ayla's party runs on the engine (engine_parties names her)"""
    try:
        with open(path or tuning_path(), encoding="utf-8") as f:
            data = json.load(f)
    except (OSError, ValueError):
        return False
    return NAME in engine_names(data)


def mode(path=None):
    """'engine' after the switch, 'openkore' before it: the first word of the briefing's mode line"""
    return "engine" if engine_on(path) else "openkore"


if __name__ == "__main__":
    print(mode())
