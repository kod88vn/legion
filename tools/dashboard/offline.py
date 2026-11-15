"""What the dashboard can show from files when the engine is down (DB08), and the Players tab (DB07).

Every function reads files only (the engine's legion/data, Ayla's OpenKore logs, docs/reports) and answers in the
shape of the engine endpoint it stands in for, so the page renders it with the same code. Answers carry the
file's time; the server marks them as saved data with their age. Nothing is guessed: a field the files do not
have is left out (the page shows "–").
"""
import datetime
import glob
import gzip
import json
import os
import re
import threading
import time

HERE = os.path.dirname(os.path.abspath(__file__))
REPO = os.path.abspath(os.path.join(HERE, "..", ".."))
AYLA_LOGS = os.environ.get("DASH_AYLA_LOGS") or os.path.join(REPO, "openkore-ayla", "logs")
REPORTS = os.path.join(REPO, "docs", "reports")
BOT_ACCOUNTS = (2100001, 2199999)   # bot account range (G06)

_lock = threading.Lock()
_cache = {}


def data(name):
    import common as C
    return C.dpath(name)


def mtime(p):
    try:
        return os.path.getmtime(p)
    except OSError:
        return None


def cached(path, loader):
    """loader(path) result, reloaded when the file changes; None when the file is missing or unreadable."""
    t = mtime(path)
    if t is None:
        return None
    with _lock:
        hit = _cache.get(path)
        if hit and hit[0] == t:
            return hit[1]
    try:
        val = loader(path)
    except Exception:
        val = None
    with _lock:
        _cache[path] = (t, val)
    return val


def load_json(p):
    with open(p, encoding="utf-8") as f:
        return json.load(f)


def go_time(v):
    if not v or str(v).startswith("0001-"):
        return None
    s = re.sub(r"(\.\d{6})\d+", r"\1", str(v).replace("Z", "+00:00"))
    try:
        return datetime.datetime.fromisoformat(s).timestamp()
    except ValueError:
        return None


def tail_lines(p, n, max_bytes=600_000):
    try:
        size = os.path.getsize(p)
        with open(p, "rb") as f:
            if size > max_bytes:
                f.seek(size - max_bytes)
                f.readline()
            return [l.decode("utf-8", "replace").rstrip("\r\n") for l in f.readlines()[-n:]]
    except OSError:
        return []


# ----------------------------------------------------------------- name tables
def items():
    def ld(p):
        out = {}
        with open(p, encoding="utf-8", errors="replace") as f:
            for line in f:
                parts = line.rstrip("\n").split("\t")
                if len(parts) >= 2 and parts[0].isdigit():
                    out[int(parts[0])] = {"name": parts[1], "slots": int(parts[2]) if len(parts) > 2 and parts[2].isdigit() else None}
        return out
    return cached(os.path.join(AYLA_LOGS, "items_lut.tsv"), ld) or {}


def skills(snap_dir):
    def ld(p):
        out = {}
        with open(p, encoding="utf-8") as f:
            for line in f:
                parts = line.rstrip("\n").split("\t")
                if len(parts) >= 3 and parts[0].isdigit():
                    out[int(parts[0])] = {"code": parts[1], "name": parts[2]}
        return out
    return cached(os.path.join(snap_dir, "skills.tsv"), ld) or {}


CLASSES = {0: "Novice", 1: "Swordman", 2: "Mage", 3: "Archer", 4: "Acolyte", 5: "Merchant", 6: "Thief", 7: "Knight",
           8: "Priest", 9: "Wizard", 10: "Blacksmith", 11: "Hunter", 12: "Assassin", 14: "Crusader", 15: "Monk",
           16: "Sage", 17: "Rogue", 18: "Alchemist", 19: "Bard", 20: "Dancer", 23: "Super Novice", 4001: "High Novice",
           4002: "High Swordman", 4003: "High Mage", 4004: "High Archer", 4005: "High Acolyte", 4006: "High Merchant",
           4007: "High Thief", 4008: "Lord Knight", 4009: "High Priest", 4010: "High Wizard", 4011: "Whitesmith",
           4012: "Sniper", 4013: "Assassin Cross", 4015: "Paladin", 4016: "Champion", 4017: "Professor",
           4018: "Stalker", 4019: "Creator", 4020: "Clown", 4021: "Gypsy"}   # legion/persona.go classNames


def class_name(i):
    return CLASSES.get(i, "class %s" % i) if isinstance(i, int) else i


# ----------------------------------------------------------------- engine files
def levels():
    return cached(data("levels.json"), load_json) or {}


def personas():
    """{by_name: {lower name: (persona, char)}, by_account: {account: persona}} without passwords"""
    def ld(p):
        by_name, by_acc = {}, {}
        for x in load_json(p):
            x = {k: v for k, v in x.items() if k != "password"}
            by_acc[x.get("account")] = x
            for c in x.get("characters") or []:
                by_name[str(c.get("name", "")).lower()] = (x, c)
            by_name.setdefault(str(x.get("name", "")).lower(), (x, None))
        return {"by_name": by_name, "by_account": by_acc, "count": len(by_acc)}
    return cached(data("personas.json"), ld) or {"by_name": {}, "by_account": {}, "count": 0}


def account_main(acc):
    p = personas()["by_account"].get(acc)
    if not p:
        return None
    for c in p.get("characters") or []:
        if c.get("role") == "main":
            return c.get("name")
    return p.get("name")


def human_party():
    hp = cached(data("human-parties.json"), load_json) or []
    return hp[0] if hp else None


def ayla_status():
    return cached(os.path.join(AYLA_LOGS, "status.json"), load_json)


def leaderboard(limit=0):
    lv = levels()
    if not lv:
        return None
    roster = cached(data("guild_roster.json"), load_json) or {}
    leaders = {acc: g for g, acc in (cached(data("guild_leaders.json"), load_json) or {}).items()}
    lead_char = {account_main(acc): g for acc, g in leaders.items()}
    rows = sorted(lv.values(), key=lambda r: (-(r.get("level") or 0), -(r.get("exp") or 0), r.get("name", "")))
    top = []
    for i, r in enumerate(rows, 1):
        top.append({"rank": i, "name": r.get("name"), "player": r.get("player"), "class": r.get("class"),
                    "level": r.get("level"), "job_level": r.get("job_level"), "guild": roster.get(r.get("account"), ""),
                    "online": False, "leader_of": lead_char.get(r.get("name"), ""),
                    "last_seen": r.get("seen")})
    out = {"ranked": len(top), "top": top[:limit] if limit else top, "leaders": [r for r in top if r["leader_of"]],
           "note": "from legion/data/levels.json (the engine's level book; last known level of each character)"}
    return out


def guild_rank():
    gr = cached(data("guild_rank.json"), load_json)
    if not gr:
        return None
    cur = []
    for g, h in (gr.get("history") or {}).items():
        if h:
            cur.append(h[-1])
    cur.sort(key=lambda r: r.get("place") or 99)
    return {"instance": "files", "rank": cur, "history": {},
            "how": "last ranking saved in legion/data/guild_rank.json (%s)" % gr.get("saved", "")}


def guilds_detail():
    gr = guild_rank()
    if not gr:
        return None
    leaders = cached(data("guild_leaders.json"), load_json) or {}
    duty = cached(data("guild_duty.json"), load_json) or {}
    lv = levels()
    out = []
    for r in gr["rank"]:
        gid = r.get("guild")
        lead = account_main(leaders.get(gid)) if leaders.get(gid) else None
        le = lv.get(lead) or {}
        dirs = (duty.get("directions") or {}).get(gid) or []
        strat = " | ".join("%s %s lv%s-%s %s: %s" % (d.get("kind", ""), d.get("map", ""), d.get("min_lv", ""), d.get("max_lv", ""),
                                                     ",".join(d.get("classes") or []), d.get("note", "")) for d in dirs)
        out.append({"id": gid, "name": r.get("name"), "members": r.get("members"), "online": 0,
                    "avg_level": r.get("avg_top10_level"), "strategy": strat or None,
                    "leader": {"name": lead, "level": le.get("level"), "class": le.get("class"), "online": False} if lead else {},
                    "tactics": {"place": r.get("place"), "score": r.get("score"), "wealth": r.get("wealth"),
                                "guild_level": r.get("guild_level")}})
    return out


def friends(n=15):
    f = cached(data("friends.json"), load_json)
    if not f:
        return None
    pairs = f.get("pairs") or []
    tiers = {"acquaintance": 0, "friend": 0, "close friend": 0}
    # the engine's /api/friends score: "eff" (raw decayed score + memory bonus) when the file has it, else the raw score
    def eff(p):
        e = p.get("eff")
        return e if isinstance(e, (int, float)) else (p.get("score") or 0)
    for p in pairs:   # legion/friends.go thresholds
        s = eff(p)
        t = "close friend" if s >= 65 else "friend" if s >= 35 else "acquaintance" if s >= 10 else ""
        if t:
            tiers[t] += 1
    top = sorted(pairs, key=lambda p: -eff(p))[:n]
    loy = f.get("loyalty") or []
    lc = f.get("loyalty_counters") or {}
    return {"pairs_total": len(pairs), "tiers": tiers, "counters": f.get("counters") or {}, "guild_moves": [],
            "note": "scores from legion/data/friends.json: the engine's score (decayed, with the memory bonus) as saved by the engine; raw score where the file has none",
            "loyalty": {"help_asked": sum(v for k, v in lc.items() if k.startswith("asked")),
                        "help_answered": sum(v for k, v in lc.items() if k.startswith("answered")),
                        "help_unanswered": sum(v for k, v in lc.items() if k.startswith("unanswered")),
                        "at_risk": sum(1 for x in loy if (x.get("loyalty") or 100) < 30), "keep_gifts_sent": sum(x.get("kept") or 0 for x in loy)},
            "top": [{"a": p.get("a"), "b": p.get("b"), "score": round(eff(p), 1), "raw": round(p.get("score") or 0, 1),
                     "tier": "close friend" if eff(p) >= 65 else "friend" if eff(p) >= 35 else "acquaintance",
                     "partied_min": p.get("partied_min"), "fight_min": p.get("fight_min"), "heals": p.get("heals"),
                     "chats": p.get("chats"), "trades": p.get("trades"), "guild_min": p.get("guild_min"),
                     "last_contact": (p.get("last") or "")[:16].replace("T", " ")} for p in top]}


def overview():
    """/api/overview as the engine saved it (legion/dashboard.go saveOverview -> data/overview.json)."""
    return cached(data("overview.json"), load_json)


_BOT_SORT = {"zph": "zph", "kpm": "kpm", "gap_zph": "gap_zph", "gap_kpm": "gap_kpm", "deaths": "deaths_h", "min": "min"}


def meter_bots(q):
    """/api/meter/bots from data/meter_bots.json (the engine's per-bot meter, written every 5 min). Same query keys:
    cohort (prefix), sort (zph|kpm|gap_zph|gap_kpm|deaths|name|min), order=asc, limit (default 50)."""
    f = cached(data("meter_bots.json"), load_json)
    if not f:
        return None
    def arg(k, d=""):
        return (q.get(k) or [d])[0]
    coh = arg("cohort").lower()
    rows = [b for b in (f.get("bots") or []) if not coh or str(b.get("cohort", "")).lower().startswith(coh)]
    by = arg("sort")
    if by == "name":
        rows.sort(key=lambda b: str(b.get("name", "")))
    else:
        key = _BOT_SORT.get(by, "zph")
        rows.sort(key=lambda b: (b.get(key) or 0), reverse=arg("order") != "asc")
    try:
        limit = int(arg("limit", "0") or 0)
    except ValueError:
        limit = 0
    limit = limit if limit > 0 else 50
    return {"t": f.get("t"), "window": f.get("window"), "total": len(rows), "bots": rows[:limit],
            "note": "offline: legion/data/meter_bots.json (the engine's last save)"}


def _meter_rows():
    def ld(p):
        return [json.loads(l) for l in open(p, encoding="utf-8") if l.strip()]
    return cached(data("meter.jsonl"), ld) or []


def meter():
    rows = _meter_rows()
    if not rows:
        return None
    last = rows[-1].get("t")
    cur = [r for r in rows if r.get("t") == last]
    srv = next((r for r in cur if r.get("kind") == "server"), {})
    tot = dict(srv.get("totals") or {})
    cs = [r for r in cur if r.get("kind") == "cohort"]
    return {"t": last, "window": "%s ending %s" % (srv.get("window", "1h"), last), "server": tot, "cohorts": cs,
            "source": "legion/data/meter.jsonl (hourly rows)"}


def meter_neo():
    rows = _meter_rows()
    neo = next((r for r in reversed(rows) if r.get("kind") == "neo"), None)
    if not neo:
        return None
    n = neo.get("neo") or {}
    return {"t": neo.get("t"), "window": "1h ending %s" % neo.get("t"), "ayla": None, "party": [], "starveil": [],
            "party_summary": n.get("party") or {}, "starveil_summary": n.get("starveil") or {}}


def history(hours=6):
    """/api/overview/history from the sentinel's health.log (one line per pass, the overview's 5-minute rates)."""
    p = data("health.log")
    lines = tail_lines(p, 2000)
    t_end = mtime(p)
    if not lines or not t_end:
        return None
    pts, day = [], datetime.date.fromtimestamp(t_end)
    prev = None
    for line in reversed(lines):
        try:
            o = json.loads(line)
        except ValueError:
            continue
        hh = o.get("t")
        if not hh or "online" not in o:
            continue
        if prev is not None and hh > prev:
            day -= datetime.timedelta(days=1)
        prev = hh
        try:
            t = datetime.datetime.combine(day, datetime.time.fromisoformat(hh)).timestamp()
        except ValueError:
            continue
        if t < t_end - hours * 3600:
            break
        pts.append({"t": int(t), "online": o.get("online"), "avg_level": o.get("avg_lv"), "kills": o.get("kills"),
                    "deaths": o.get("deaths"), "levelups": o.get("lvups")})
    pts.reverse()
    return {"interval_s": 60, "points": pts, "source": "legion/data/health.log"}


def parties_detail(snap=None):
    lv = levels()
    byname = {str(c.get('name', '')).lower(): c for c in snap.chars.values()} if snap else {}
    st = ayla_status() or {}
    hp = human_party()
    out = []

    def member(name, leader=False):
        e = lv.get(name) or {}
        m = {"name": name, "class": e.get("class"), "level": e.get("level"), "job_level": e.get("job_level"),
             "online": False, "leader": leader, "state": "offline (engine down)"}
        c = byname.get(str(name).lower())
        if c:   # the database snapshot: last saved level, purse and map
            m.update({"class": class_name(c.get("class")), "level": c.get("base_level"), "job_level": c.get("job_level"),
                      "zeny": c.get("zeny"), "state": "offline (engine down) · last map %s %s,%s (database snapshot)" % (
                          c.get("last_map"), c.get("last_x"), c.get("last_y"))})
        return m
    if hp:
        chat = (cached(data("partychat.json"), load_json) or {}).get("ayla") or []
        core = (cached(data("parties_core.json"), load_json) or {})
        gear = next(iter(core.values()), {}) if core else {}
        human = hp.get("human") or "Ayla"
        stt = st.get("ts")
        ayla = {"name": human, "human": True, "leader": True, "has_stats": bool(st), "online": False,
                "class": class_name(st.get("job_id")), "level": st.get("base_level"), "job_level": st.get("job_level"),
                "map": st.get("map"), "x": st.get("x"), "y": st.get("y"), "zeny": st.get("zeny"),
                "weight": st.get("weight"), "max_weight": st.get("max_weight"), "effects": st.get("effects") or [],
                "state": "last status.json %s" % (time.strftime("%H:%M", time.localtime(stt)) if stt else "?")}
        if st:
            d = {k: st.get(k) for k in ("intent", "intent_since", "map", "x", "y", "lock_map", "dest", "town_run",
                                         "base_exp_pct", "job_exp_pct", "exp_h", "job_exp_h", "kpm", "meter_min",
                                         "deaths_today", "ok_start", "restarts_today", "prev_start", "last_decision",
                                         "last_watchdog", "last_order")}
            d.update({"fresh": False, "status_ts": stt, "status_age_s": int(time.time() - stt) if stt else None})
            ayla["detail"] = d
        mem = [ayla] + [member(n) for n in hp.get("members") or []]
        out.append({"name": "%s pt" % human, "human": human, "map": st.get("map"), "checkpoint": st.get("save_map"),
                    "together": 0, "online": 0, "members": mem, "gear_turn": gear.get("Turn"),
                    "gear_log": ["planned: %s" % gear["PlannedFor"]] if gear.get("PlannedFor") else [],
                    "chat": sorted(chat, key=lambda c: -(c.get("ts") or 0)),
                    "alumni": hp.get("alumni") or []})
    for p in cached(data("parties.json"), load_json) or []:
        f = go_time(p.get("formed"))
        out.append({"name": p.get("name"), "leader": p.get("leader"), "online": 0, "together": 0, "map": p.get("map"),
                    "checkpoint": p.get("checkpoint"), "purpose": p.get("purpose"),
                    "formed_min": int((time.time() - f) / 60) if f else None,
                    "avg_level": round(sum((lv.get(n) or {}).get("level") or 0 for n in p.get("members") or []) / max(1, len(p.get("members") or [])), 1),
                    "members": [member(n, n == p.get("leader")) for n in p.get("members") or []],
                    "gear_turn": (p.get("gear") or {}).get("Turn")})
    return out or None


def population():
    lv = levels()
    if not lv:
        return None
    vals = list(lv.values())
    cls, buckets = {}, {}
    for r in vals:
        cls[r.get("class") or "?"] = cls.get(r.get("class") or "?", 0) + 1
        b = max(0, ((r.get("level") or 1) - 1) // 10)
        buckets[b] = buckets.get(b, 0) + 1
    lvl = [r.get("level") or 0 for r in vals]
    return {"characters": len(vals), "profiles": personas()["count"], "avg_level": round(sum(lvl) / len(lvl), 1),
            "classes": sorted(([k, v] for k, v in cls.items()), key=lambda x: -x[1]),
            "levels": [{"from": b * 10 + 1, "to": b * 10 + 10, "count": buckets[b]} for b in sorted(buckets)],
            "t": mtime(data("levels.json"))}


def league():
    files = sorted(glob.glob(os.path.join(REPORTS, "league-*.md")), key=lambda p: mtime(p) or 0)
    if not files:
        return None
    try:
        with open(files[-1], encoding="utf-8") as f:
            txt = f.read(60_000)
    except OSError:
        return None
    return {"file": os.path.basename(files[-1]), "t": mtime(files[-1]), "text": txt}


def ayla_files():
    def tail(name, n):
        p = os.path.join(AYLA_LOGS, name)
        return {"file": name, "t": mtime(p), "lines": tail_lines(p, n)} if mtime(p) else None
    out = {"status": ayla_status(), "chat": tail("chat_player_2.txt", 60), "deaths": tail("dead_log_player_2.txt", 20),
           "storage": tail("storage_player_2.txt", 400), "deals": tail("deals.log", 40), "alerts": tail("alerts.log", 20)}
    for n in ("topups.json", "ayla_mapexp.json", "ayla_plan.json"):
        out[n] = cached(os.path.join(AYLA_LOGS, n), load_json)
    return out


def summary():
    """/offline: the extras the page shows from files, beyond the stand-in endpoints"""
    hl = tail_lines(data("health.log"), 1)
    try:
        health = json.loads(hl[-1]) if hl else None
    except ValueError:
        health = None
    duty = cached(data("guild_duty.json"), load_json) or {}
    ec = cached(data("econ_counters.json"), load_json)
    return {"health": health, "health_t": mtime(data("health.log")), "population": population(),
            "econ": ec, "econ_t": mtime(data("econ_counters.json")),
            "guild_duty": {"counters": duty.get("counters"), "saved": duty.get("saved"),
                           "directions": duty.get("directions"), "members": len(duty.get("members") or []),
                           "kicked": duty.get("kicked")},
            "league": league(), "ayla": ayla_files(),
            "logs": {"watchdog": tail_lines(data("watchdog.log"), 30), "health": tail_lines(data("health.log"), 30)}}


# ----------------------------------------------------------------- Players (DB07)
def player_kind(account_id, group_id, name):
    hp = human_party() or {}
    party = {str(n).lower() for n in (hp.get("members") or [])} | {str(hp.get("human", "")).lower()}
    if str(name).lower() in party:
        return "Ayla's party"
    if group_id is not None and group_id >= 10:
        return "GM"
    if isinstance(account_id, int) and BOT_ACCOUNTS[0] <= account_id <= BOT_ACCOUNTS[1]:
        return "bot"
    return "human"


def search(snap, q, limit=60):
    q = (q or "").strip().lower()
    out, seen = [], set()
    if snap:
        for c in snap.chars.values():
            acc = snap.accounts.get(c.get("account_id")) or {}
            hay = [str(c.get("name", "")).lower(), str(acc.get("userid", "")).lower(), str(c.get("char_id")), str(c.get("account_id"))]
            if q and not any(q in h for h in hay):
                continue
            seen.add(str(c.get("name", "")).lower())
            out.append({"char_id": c.get("char_id"), "name": c.get("name"), "account_id": c.get("account_id"),
                        "account": acc.get("userid"), "class": class_name(c.get("class")), "level": c.get("base_level"),
                        "job_level": c.get("job_level"), "online": bool(c.get("online")), "map": c.get("last_map"),
                        "kind": player_kind(c.get("account_id"), acc.get("group_id"), c.get("name")), "source": "db"})
    for name, e in levels().items():   # characters the engine knows that the snapshot does not have (or no snapshot)
        if name.lower() in seen:
            continue
        hay = [name.lower(), str(e.get("account", "")).lower()]
        if q and not any(q in h for h in hay):
            continue
        out.append({"char_id": None, "name": name, "account": e.get("account"), "class": e.get("class"),
                    "level": e.get("level"), "job_level": e.get("job_level"), "online": None,
                    "kind": "bot" if str(e.get("account", "")).startswith("lg") else player_kind(None, None, name),
                    "source": "engine files"})
    exact = [r for r in out if str(r["name"]).lower() == q]
    rest = [r for r in out if str(r["name"]).lower() != q]
    rest.sort(key=lambda r: (not str(r["name"]).lower().startswith(q), -(r.get("level") or 0), str(r["name"])))
    return {"total": len(out), "rows": (exact + rest)[:limit]}


def _items(rows, names):
    out = []
    for r in rows or []:
        it = names.get(r.get("nameid")) or {}
        cards = [names.get(c, {}).get("name") or c for c in (r.get("card0"), r.get("card1"), r.get("card2"), r.get("card3"))
                 if isinstance(c, int) and c > 0]
        out.append({"id": r.get("nameid"), "name": it.get("name"), "amount": r.get("amount"), "equip": r.get("equip"),
                    "refine": r.get("refine"), "identify": r.get("identify"), "broken": r.get("attribute"), "cards": cards})
    out.sort(key=lambda x: (not x["equip"], x["name"] or "~", x["id"] or 0))
    return out


def engine_view(name):
    """what the engine's files say about one character: persona, memory, level book, friends, guild duty"""
    lname = str(name).lower()
    pe = personas()["by_name"].get(lname)
    out = {}
    if pe:
        p, c = pe
        out["persona"] = {k: p.get(k) for k in ("account", "archetype", "bio", "age", "country", "aggression", "risk",
                                                "chattiness", "patience", "greed", "sociability", "helpfulness", "loyalty",
                                                "skill", "session_start_hour", "session_hours", "typing_style", "guild",
                                                "home_town")}
        out["character_plan"] = c
        out["other_characters"] = [{"name": x.get("name"), "role": x.get("role"), "plan": x.get("plan")}
                                   for x in p.get("characters") or [] if x is not c]
        mem = cached(data(os.path.join("memory", "%s.json" % p.get("account"))), load_json)
        if mem:
            people = sorted((mem.get("people") or {}).items(), key=lambda kv: -((kv[1] or {}).get("times_met") or 0))
            out["memory"] = {"facts": mem.get("facts"), "goals": mem.get("goals"),
                             "progress": (mem.get("progress") or {}).get(name) or mem.get("progress"),
                             "saved_town": (mem.get("saved_town") or {}).get(name),
                             "people": [dict(name=k, **(v or {})) for k, v in people[:20]], "people_total": len(people),
                             "maps_visited": sorted((mem.get("maps_visited") or {}).items(), key=lambda kv: -kv[1])[:12],
                             "diary": (mem.get("diary") or [])[-40:][::-1], "updated": mem.get("updated")}
    e = levels().get(name)
    if e:
        out["level_book"] = e
    f = cached(data("friends.json"), load_json) or {}
    mine = [p for p in f.get("pairs") or [] if p.get("a") == name or p.get("b") == name]
    mine.sort(key=lambda p: -(p.get("score") or 0))
    out["friends"] = [{"with": p["b"] if p.get("a") == name else p.get("a"), "score": round(p.get("score") or 0, 1),
                       "partied_min": p.get("partied_min"), "fight_min": p.get("fight_min"), "chats": p.get("chats"),
                       "trades": p.get("trades"), "last": (p.get("last") or "")[:16].replace("T", " ")} for p in mine[:12]]
    out["friends_total"] = len(mine)
    out["loyalty"] = next((x for x in f.get("loyalty") or [] if x.get("char") == name), None)
    duty = cached(data("guild_duty.json"), load_json) or {}
    out["guild_duty"] = next((m for m in duty.get("members") or [] if m.get("char") == name), None)
    return out


def character(snap, live, char_id=None, name=None, skill_names=None):
    """the full character: rAthena rows (live or snapshot) + names + the engine's view"""
    src, row = None, None
    if live:
        row, src = live, "ro-db now (read-only)"
    elif snap and char_id is not None and int(char_id) in snap.chars:
        c = snap.chars[int(char_id)]
        row = {"char": c, "inventory": snap.by_char["inventory"].get(c["char_id"], []),
               "cart_inventory": snap.by_char["cart_inventory"].get(c["char_id"], []),
               "skill": snap.by_char["skill"].get(c["char_id"], []), "storage": snap.storage.get(c["account_id"], []),
               "login": snap.accounts.get(c["account_id"]), "guild": snap.guilds.get(c.get("guild_id")),
               "party": snap.parties.get(c.get("party_id"))}
        src = "snapshot"
    out = {"source": src, "snapshot_t": snap.t if snap else None}
    if row:
        c = dict(row["char"])
        names = items()
        c["class_name"] = class_name(c.get("class"))
        name = c.get("name")
        acc = row.get("login") or {}
        out["char"] = c
        out["account"] = {k: acc.get(k) for k in ("account_id", "userid", "sex", "group_id", "logincount", "lastlogin")}
        out["kind"] = player_kind(c.get("account_id"), acc.get("group_id"), name)
        out["equipment"] = [i for i in _items(row["inventory"], names) if i["equip"]]
        out["inventory"] = [i for i in _items(row["inventory"], names) if not i["equip"]]
        out["cart"] = _items(row["cart_inventory"], names)
        out["storage"] = _items(row["storage"], names)
        sn = skill_names or {}
        out["skills"] = sorted(({"id": s.get("id"), "lv": s.get("lv"), "flag": s.get("flag"),
                                 "name": (sn.get(s.get("id")) or {}).get("name"), "code": (sn.get(s.get("id")) or {}).get("code")}
                                for s in row["skill"]),
                               key=lambda s: s["id"] or 0)
        out["guild"] = row.get("guild")
        out["party"] = row.get("party")
    elif name:
        e = levels().get(name) or {}
        out["char"] = {"name": name, "class_name": e.get("class"), "base_level": e.get("level"), "job_level": e.get("job_level")}
        out["kind"] = "bot" if str(e.get("account", "")).startswith("lg") else player_kind(None, None, name)
    if name:
        out["engine"] = engine_view(name)
    return out
