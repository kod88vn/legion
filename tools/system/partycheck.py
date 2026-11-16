#!/usr/bin/env python3
"""Scripted checks of Ayla's party against the party rules; violations go to the bug queue (deduped).

  partycheck.py --once [--snapshot FILE]   one pass; time-based rules count the state NOW as long enough
  partycheck.py [--interval 60]            loop mode (writes data/partycheck.heartbeat each pass)

Checks (RULES parties / Ayla): composition (AY11: five members, 2 frontline, 2 ranged, 1 support; information only, never
acts on the party, G20), apart (not within 20 cells of the leader for > 4 min), idle (idle/afk/stuck states for
> 3 min), deaths (2+ in 20 min), wrong save point (on a hunting map whose nearest Kafra town is not the member's save
town), long sitting (leader, or half the party, resting > 5 min), back-and-forth between maps (4+ map changes among
two maps in 15 min). A member on an announced errand / party town run is excused from "apart".
Data: GET /api/parties/detail (Ayla's party: the one with human "Ayla" in the OpenKore era, the one with a member named
Ayla in the engine era, aylaparty.py), GET /api/events (deaths). No party found, or a failed pass, is filed as a
"party blind" item that wakes main: the checks never pass silently (AY24). Field names are read
defensively (x, y, save_map|save|respawn, with_leader, dist_leader). Every violation is about Ayla's party, so it is
filed at least "high" with the wake flag (an "apart" item only after 10 min apart and not walking back to the
leader, or dead > 2 min: apartwake.py): bugq touches data/bugq.wake and the bug-queue watcher (bugq.py watch) exits on
it, waking the main session within minutes. Dedupe stays: one entry per kind+member, a repeat only bumps its count and
does not wake again.
"""
import argparse
import collections
import json
import math
import os
import sys
import time

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import aylaparty
import apartwake
import bugq
import common as C

TOGETHER = 20
APART_S, IDLE_S, SIT_S = 4 * 60, 3 * 60, 5 * 60
DEATHS_N, DEATHS_WIN = 2, 20 * 60
COMP_S = 10 * 60                      # a swap or a join takes a while: the composition must be wrong this long
# AY11: five members, Ayla included: 2 frontline (melee or tank), 2 ranged, 1 support. The role names are the engine's
# (partyroles.go roleNames); Ayla is an Assassin, a frontline (her member view says "human", not a role).
PARTY_SIZE = 5
SHAPE = {"front": 2, "ranged": 2, "support": 1}
ROLE_SLOT = {"tank": "front", "melee": "front", "ranged": "ranged", "support": "support"}
AYLA_SLOT = "front"
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


def composition(members):
    """AY11: (bad, text). bad when the party is not five, a member fits no slot, or a slot count is off."""
    slots, unknown = collections.Counter(), []
    for m in members:
        slot = AYLA_SLOT if m.get("human") else ROLE_SLOT.get(str(m.get("role", "")).lower())
        if slot:
            slots[slot] += 1
        else:
            unknown.append("%s (%s)" % (m.get("name", "?"), m.get("role", "?")))
    off = [k for k in SHAPE if slots[k] != SHAPE[k]]
    bad = len(members) != PARTY_SIZE or bool(unknown) or bool(off)
    text = "party of %d, %d frontline / %d ranged / %d support, want %d / %d / %d" % (
        len(members), slots["front"], slots["ranged"], slots["support"], SHAPE["front"], SHAPE["ranged"], SHAPE["support"])
    if unknown:
        text += "; no slot for " + ", ".join(unknown)
    return bad, text


def _num(v):
    return float(v) if isinstance(v, (int, float)) and not isinstance(v, bool) else None


class PartyCheck:
    def __init__(self, instant=False):
        self.k = 0.0 if instant else 1.0      # duration scale: instant = every duration counts as met
        self.since = {}                       # (kind, name) -> first time seen in the bad state
        self.maps = collections.defaultdict(list)    # name -> [(t, map)] changes
        self.deaths = collections.defaultdict(list)  # name -> [t]
        self.seen_events = set()
        self.aw = apartwake.ApartWake(self.k)  # which "apart" items are worth waking main for (10 min, not walking back)

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
        apart_names = []
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
            if apart:
                apart_names.append(name)
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
        self.aw.update(ms, leader, apart_names, now)
        comp_bad, comp_text = composition(snap.get("members", []))
        if self._timer("comp", "party", comp_bad, COMP_S, now):
            out.append(("composition", "party", "normal", "Ayla's party is not the AY11 shape: " + comp_text))
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
    engine = aylaparty.engine_era()
    ap = aylaparty.find_party(parties, engine)
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
    return {"members": ms, "events": ev, "party_found": ap is not None, "era": "engine" if engine else "openkore"}


def file_blind(why):
    """The checks have nothing to look at: a high item that wakes main (deduped; a repeat bumps the count)"""
    bugq.add("partycheck", "high", "[party blind] %s: the party checks see nothing" % why, evidence=why,
             key="partycheck:blind", metric="kpm", bump_min_secs=900, wake=True)


def file_priority(prio):
    """Ayla's party is the main priority (PR26): never below high"""
    return prio if bugq.PRIO_W.get(str(prio), 0) >= bugq.PRIO_W["high"] or prio == "outage" else "high"


def wants_wake(kind, name, pc=None):
    """every party item wakes main, except "apart": only when the member has been apart > 10 min and is not walking
    back to the leader, or is dead > 2 min (apartwake; needs the PartyCheck that saw it)"""
    if kind == "composition":
        return False   # information only: a filed item, no wake
    if kind != "apart":
        return True
    return pc is not None and pc.aw.wake(name)


def file_violations(vios, pc=None):
    ids = []
    for kind, name, prio, text in vios:
        i, new = bugq.add("partycheck", file_priority(prio), "[party %s] %s" % (kind, text), evidence=text,
                          key="partycheck:%s:%s" % (kind, name), metric="kpm", bump_min_secs=900,
                          wake=wants_wake(kind, name, pc))
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
    locked = not (a.once or a.dry_run)   # single instance for the loop: legion/data/partycheck.lock
    if locked and not C.take_lock("partycheck.lock", 300):
        print("partycheck: another copy holds legion/data/partycheck.lock; not starting", file=sys.stderr)
        return 3
    pc = PartyCheck(instant=a.once)
    while True:
        blind = ""
        try:
            if a.snapshot:
                with open(a.snapshot, encoding="utf-8") as f:
                    snap = json.load(f)
            else:
                snap = snapshot_from_api(a.base)
            if not snap.get("party_found", True):
                blind = "Ayla's party not found in /api/parties/detail (%s era)" % snap.get("era", "?")
                print("%s party blind: %s" % (C.iso(), blind))
                if not a.dry_run:
                    file_blind(blind)
            vios = pc.evaluate(snap)
            for kind, name, prio, text in vios:
                print("%s party %s (%s): %s" % (C.iso(), kind, prio, text))
            if vios and not a.dry_run:
                file_violations(vios, pc)
            if not vios and a.once and not blind:
                print("party ok (%d members checked)" % len(snap.get("members", [])))
        except Exception as e:
            blind = "party pass failed: %s" % e
            sys.stderr.write("partycheck pass failed: %s\n" % e)
            if not a.dry_run:
                file_blind(blind)
        if locked and not C.refresh_lock("partycheck.lock"):
            print("partycheck: lock taken by another copy; exiting", file=sys.stderr)
            return 3
        C.heartbeat("partycheck", note=("blind: " + blind) if blind else "")
        if a.once:
            return 0
        time.sleep(a.interval)


if __name__ == "__main__":
    sys.exit(main())
