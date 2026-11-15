#!/usr/bin/env python3
"""When a "party apart" problem is worth waking the main session for (partycheck.py and sentinel.py).

Members walking back to the leader are normal: a split is still filed to the bug queue, but it only WAKES main when
  - the same member has been apart for more than APART_WAKE_S (10 min) AND is not moving toward the leader
    (state "going to <leader>" / "going to <leader's map>" / "travel -> <leader's map>", or the distance to the leader
    on the same map shrank since the last pass), or
  - the member is dead for more than DEAD_WAKE_S (2 min).
"""
import re

APART_WAKE_S = 10 * 60
DEAD_WAKE_S = 2 * 60
CLOSING_CELLS = 2          # same map, the distance shrank by at least this much since the last pass = walking back
PREV_MAX_AGE = 5 * 60      # an older distance sample says nothing about walking


def _num(v):
    return float(v) if isinstance(v, (int, float)) and not isinstance(v, bool) else None


def is_dead(m):
    return str(m.get("state", "")).lower().startswith("dead")


def heading_to(state, leader):
    """True if the state says the member is on the way to the leader (by name or to the leader's map)."""
    s = str(state or "").lower()
    if not leader:
        return False
    name = str(leader.get("name") or "").lower()
    lmap = str(leader.get("map") or "").lower()
    if name and ("going to " + name in s or "following " + name in s or "catching up with " + name in s):
        return True
    if lmap:
        for pre in ("travel -> ", "travel ->", "going to "):
            i = s.find(pre)
            if i >= 0 and re.match(re.escape(lmap) + r"\b", s[i + len(pre):].lstrip()):
                return True
    return False


def dist(m, leader):
    if not leader or not m.get("map") or m.get("map") != leader.get("map"):
        return None
    xs = (_num(m.get("x")), _num(m.get("y")), _num(leader.get("x")), _num(leader.get("y")))
    if None in xs:
        return None
    return max(abs(xs[0] - xs[2]), abs(xs[1] - xs[3]))


class ApartWake:
    """Per-member timers. update() once per pass with the members that are apart; wake(name) / any_wake() then say
    whether that split is worth waking main for. scale 0 (instant / --once) counts every duration as met."""

    def __init__(self, scale=1.0):
        self.k = scale
        self.apart_since = {}
        self.dead_since = {}
        self.prev = {}        # name -> (t, distance)
        self.ok = {}          # name -> wake verdict of the last update

    def update(self, members, leader, apart_names, now):
        apart_names = set(apart_names)
        self.ok = {}
        for m in members:
            n = m.get("name")
            if n is None:
                continue
            if n not in apart_names:
                self.apart_since.pop(n, None)
                self.dead_since.pop(n, None)
                self.prev.pop(n, None)
                continue
            t0 = self.apart_since.setdefault(n, now)
            if is_dead(m):
                d0 = self.dead_since.setdefault(n, now)
                self.ok[n] = now - d0 > DEAD_WAKE_S * self.k or (self.k == 0)
                continue
            self.dead_since.pop(n, None)
            d = dist(m, leader)
            p = self.prev.get(n)
            closing = d is not None and p is not None and p[1] is not None and now - p[0] <= PREV_MAX_AGE \
                and p[1] - d >= CLOSING_CELLS
            self.prev[n] = (now, d)
            moving = heading_to(m.get("state"), leader) or closing
            long_apart = now - t0 > APART_WAKE_S * self.k or self.k == 0
            self.ok[n] = long_apart and not moving
        for n in list(self.apart_since):
            if n not in apart_names:
                self.apart_since.pop(n, None)
                self.dead_since.pop(n, None)
                self.prev.pop(n, None)
        return dict(self.ok)

    def wake(self, name):
        return bool(self.ok.get(name))

    def any_wake(self):
        return any(self.ok.values())
