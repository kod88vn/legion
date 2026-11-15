#!/usr/bin/env python3
"""Scripted checks of Ayla's party against the party rules; violations go to the bug queue (deduped).

  partycheck.py --once [--snapshot FILE]   one pass; time-based rules count the state NOW as long enough
  partycheck.py [--interval 60]            loop mode (writes data/partycheck.heartbeat each pass)

Checks (RULES parties / Ayla): apart (not within 20 cells of the leader for > 4 min), idle (idle/afk/stuck states for
> 3 min), deaths (2+ in 20 min), wrong save point (on a hunting map whose nearest Kafra town is not the member's save
town), long sitting (leader, or half the party, resting > 5 min), back-and-forth between maps (4+ map changes among
two maps in 15 min). A member on an announced errand / party town run is excused from "apart".
Data: GET /api/parties/detail (party with human == "Ayla"), GET /api/events (deaths). Field names are read
defensively (x, y, save_map|save|respawn, with_leader, dist_leader). The AI watcher is woken by the queue entries
(source "partycheck"), not by this script.
"""
import argparse
import collections
import json
import math
import os
import sys
import time

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import bugq
import common as C

TOGETHER = 20
APART_S, IDLE_S, SIT_S = 4 * 60, 3 * 60, 5 * 60
DEATHS_N, DEATHS_WIN = 2, 20 * 60
FLIP_CHANGES, FLIP_WIN = 4, 15 * 60
IDLE_STATES = ("idle", "afk", "hanging", "stuck", "no route")
SIT_STATES = ("sit", "rest")
EXCUSE = ("errand", "town run", "brb", "peco", "job change", "necessary", "buying", "renting", "town trip")
# hunting-map prefix -> the town whose Kafra is nearest (save point must be there; RULES 2 "Save the respawn point")
HUNT_TOWN = [("pay_dun", "payon"), ("pay_arche", "payon"), ("pay_fild", "payon"), ("moc_pryd", "morocc"),
             ("moc_fild", "morocc"), ("moc_ruins", "morocc"), ("prt_fild", "prontera"), ("prt_sewb", "prontera"),
             ("prt_maze", "prontera"), ("gef_fild", "geffen"), ("gef_dun", "geffen"), ("orc_dun", "geffen"),
             ("cmd_fild", "comodo"), ("alde_dun", "aldebaran"), ("iz_dun", "izlude")]
TOWNS = set("prontera geffen payon morocc alberta aldebaran izlude yuno comodo umbala einbroch lighthalzen rachel veins "
            "hugel niflheim louyang amatsu gonryun ayothaya moscovia brasilis".split())


def hunt_town(m):
    for pre, town in HUNT_TOWN:
        if (m or "").startswith(pre):
            return town
    return None


def _num(v):
    return float(v) if isinstance(v, (int, float)) and not isinstance(v, bool) else None


class PartyCheck:
    def __init__(self, instant=False):
        self.k = 0.0 if instant else 1.0      # duration scale: instant = every duration counts as met
        self.since = {}                       # (kind, name) -> first time seen in the bad state
        self.maps = collections.defaultdict(list)    # name -> [(t, map)] changes
        self.deaths = collections.defaultdict(list)  # name -> [t]
        self.seen_events = set()

    def _timer(self, kind, name, bad, need, now):
        key = (kind, name)
        if not bad:
            self.since.pop(key, None)
            return False
        t0 = self.since.setdefault(key, now)
        return now - t0 >= need * self.k

    def evaluate(self, snap, now=None):
        """snap: {members: [{name, map, state, online, human, leader, with_leader, x, y, save_map}], events: [...]}.
        -> [(kind, name, priority, text)]"""
        now = C.now() if now is None else now
        ms = [m for m in snap.get("members", []) if m.get("online", True)]
        out = []
        leader = next((m for m in ms if m.get("leader")), None) or next((m for m in ms if m.get("human")), None)
        errand_run = any(any(e in str(m.get("state", "")).lower() for e in EXCUSE) for m in ms)
        for m in ms:
            name, st, mp = m.get("name", "?"), str(m.get("state", "")).lower(), m.get("map")
            # map change history
            h = self.maps[name]
            if mp and (not h or h[-1][1] != mp):
                h.append((now, mp))
            self.maps[name] = [x for x in h if now - x[0] <= FLIP_WIN]
            # apart from the leader
            apart = False
            if leader and m is not leader and not m.get("human"):
                wl = m.get("with_leader")
                dx, dy = _num(m.get("x")), _num(m.get("y"))
                if wl is not None:
                    apart = not wl
                elif mp and leader.get("map") and mp != leader.get("map"):
                    apart = True
                elif None not in (dx, dy, _num(leader.get("x")), _num(leader.get("y"))):
                    apart = max(abs(dx - leader["x"]), abs(dy - leader["y"])) > TOGETHER
                if any(e in st for e in EXCUSE) or errand_run:
                    apart = False
            if self._timer("apart", name, apart, APART_S, now):
                since = now - self.since[("apart", name)]
                out.append(("apart", name, "high" if since > 600 else "normal",
                            "%s is not within %d cells of the leader (map %s, state '%s')" % (name, TOGETHER, mp, st)))
            # idle
            if self._timer("idle", name, st.startswith(IDLE_STATES) and not m.get("human"), IDLE_S, now):
                out.append(("idle", name, "normal", "%s has been %s on %s" % (name, st or "idle", mp)))
            # wrong save point
            town, save = hunt_town(mp), str(m.get("save_map") or m.get("save") or m.get("respawn") or "").lower()
            if town and save and town not in save and not m.get("human"):
                out.append(("save", name, "high", "%s is on hunting map %s but saved at %s (nearest Kafra: %s)" % (name, mp, save, town)))
            # back and forth between maps
            maps = [x[1] for x in self.maps[name]]
            if len(maps) >= FLIP_CHANGES + 1 and len(set(maps)) == 2 and not errand_run:
                out.append(("flip", name, "normal", "%s changed map %d times in %d min between %s" % (
                    name, len(maps) - 1, FLIP_WIN // 60, " and ".join(sorted(set(maps))))))
        # long sitting: leader, or at least half the party
        sitting = [m for m in ms if str(m.get("state", "")).lower().startswith(SIT_STATES)]
        big = bool(ms) and (len(sitting) * 2 >= len(ms) or (leader in sitting if leader else False))
        if self._timer("sit", "party", big, SIT_S, now):
            out.append(("sit", "party", "normal", "party sitting/resting for 5+ min: %s" % ", ".join(m.get("name", "?") for m in sitting)))
        # deaths from the events feed
        names = set(m.get("name") for m in ms)
        for e in snap.get("events", []):
            k = (e.get("t"), e.get("bot"), e.get("text"))
            if k in self.seen_events or e.get("bot") not in names:
                continue
            if "death" in str(e.get("kind", "")).lower() or "died" in str(e.get("text", "")).lower():
                self.seen_events.add(k)
                self.deaths[e["bot"]].append(now)
        for n, ts in list(self.deaths.items()):
            self.deaths[n] = ts = [t for t in ts if now - t <= DEATHS_WIN]
            if len(ts) >= DEATHS_N:
                out.append(("deaths", n, "high", "%s died %d times in %d min" % (n, len(ts), DEATHS_WIN // 60)))
        return out


def snapshot_from_api(base):
    import urllib.request

    def get(path):
        with urllib.request.urlopen(base + path, timeout=10) as r:
            return json.load(r)
    parties = get("/api/parties/detail")
    ap = next((p for p in parties if p.get("human") == "Ayla"), None)
    ms = []
    if ap:
        for m in ap.get("members", []):
            d = dict(m)
            d["leader"] = bool(m.get("leader") or m.get("human") and not any(x.get("leader") for x in ap["members"]))
            ms.append(d)
    try:
        ev = get("/api/events").get("events", [])
    except Exception:
        ev = []
    return {"members": ms, "events": ev}


def file_violations(vios):
    ids = []
    for kind, name, prio, text in vios:
        i, new = bugq.add("partycheck", prio, "[party %s] %s" % (kind, text), evidence=text,
                          key="partycheck:%s:%s" % (kind, name), metric="kpm", bump_min_secs=900)
        ids.append((i, new, text))
    return ids


def main(argv=None):
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--base", default=os.environ.get("LEGION_API", "http://127.0.0.1:8090"))
    ap.add_argument("--once", action="store_true")
    ap.add_argument("--snapshot", help="JSON file instead of the API (testing)")
    ap.add_argument("--interval", type=float, default=60)
    ap.add_argument("--dry-run", action="store_true", help="print violations, do not file them")
    a = ap.parse_args(argv)
    pc = PartyCheck(instant=a.once)
    while True:
        try:
            if a.snapshot:
                with open(a.snapshot, encoding="utf-8") as f:
                    snap = json.load(f)
            else:
                snap = snapshot_from_api(a.base)
            vios = pc.evaluate(snap)
            for kind, name, prio, text in vios:
                print("%s party %s (%s): %s" % (C.iso(), kind, prio, text))
            if vios and not a.dry_run:
                file_violations(vios)
            if not vios and a.once:
                print("party ok (%d members checked)" % len(snap.get("members", [])))
        except Exception as e:
            sys.stderr.write("partycheck pass failed: %s\n" % e)
        C.heartbeat("partycheck")
        if a.once:
            return 0
        time.sleep(a.interval)


if __name__ == "__main__":
    sys.exit(main())
