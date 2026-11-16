#!/usr/bin/env python3
"""Ayla's party in GET /api/parties/detail, in both eras (AY24). Shared by partycheck.py and sentinel.py.

  OpenKore era (engine_parties does not name her): her party is the one whose human is "Ayla" (the human-led party).
  Engine era (engine_parties names her): an engine party has no human (Party.Human is ""), so her party is the one
  with a member named Ayla. aylawatch.ayla_party finds it the same way.

The switch is read through tools/puppet/aylaswitch.py. Standard library only.
"""
import os
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, os.path.join(HERE, "..", "puppet"))
import aylaswitch  # noqa: E402  the engine_parties switch (AY24)

NAME = "Ayla"


def engine_era(path=None):
    """True when Ayla's party runs on the engine (see aylaswitch.engine_on)"""
    return aylaswitch.engine_on(path)


def is_ayla_member(m):
    return isinstance(m, dict) and str(m.get("name", "")).lower() == NAME.lower()


def find_party(parties, engine):
    """Ayla's party view in a /api/parties/detail list, or None. engine = engine_era()."""
    for p in parties if isinstance(parties, list) else []:
        if not isinstance(p, dict):
            continue
        if engine:
            if any(is_ayla_member(m) for m in (p.get("members") or [])):
                return p
        elif str(p.get("human", "")) == NAME:
            return p
    return None
