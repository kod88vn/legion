#!/usr/bin/env python3
"""Test stand-in for the engine API: made-up sample answers for every endpoint the dashboard page reads.
Never a data source for the real page. Run: python tests/dashboard/fake_engine.py [--port 18090]
`sample_data(dir)` also fills a data folder with sample heartbeat, queue, deploy and dispatcher files."""
import argparse
import json
import os
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

PARTY = ["Zendancer84", "Echobunny", "Zen.fox", "xXRinfangXx", "LilLilyblade", "vex_x18"]
CLASSES = ["Hunter", "Priest", "Knight", "Wizard", "Assassin", "Blacksmith"]


def iso(t):
    return time.strftime("%Y-%m-%dT%H:%M:%S+00:00", time.gmtime(t))


def win(minutes, k):
    return {"per_min": {"kills": 41.5 * k, "deaths": 0.8 * k, "levelups": 1.2 * k}, "minutes_covered": minutes,
            "kills": int(41.5 * k * minutes), "deaths": int(0.8 * k * minutes), "loots": 900 * minutes // 5,
            "levelups": int(1.2 * minutes), "jobchanges": minutes // 20, "bought": minutes // 2, "restocks": minutes,
            "shops_opened": minutes // 6, "trades": minutes // 4, "parties_formed": minutes // 10, "chat_lines": 9 * minutes}


def overview(now):
    return {
        "time": int(now), "compute_ms": 12, "runtime": {"uptime_s": 7260, "goroutines": 4312, "heap_mb": 412},
        "population": {"online": 812, "profiles": 4000, "avg_base_level": 61.4, "avg_job_level": 38.2, "pinned": 5,
                       "states": {"fighting": 402, "roaming": 120, "travel": 61, "with party": 88, "resting": 40,
                                  "town trip": 31, "vending": 44, "idle": 12, "dead": 14, "offline (schedule)": 3100,
                                  "login failed": 2},
                       "by_role": {"main": 720, "vendor": 44, "support": 48}},
        "activity": {"m5": win(5, 1.0), "m15": win(15, 0.97), "m60": win(60, 0.93)},
        "totals": {"kills": 184220, "deaths": 3120},
        "economy": {"open_shops": 41, "vendors_online": 44, "zeny_median": 18400, "zeny_p90": 96500,
                    "zeny_max": 1240000, "zeny_total_online": 31200000, "trades_total": 412, "open_wants": 17,
                    "deals_active": 3},
        "parties": {"count": 96, "avg_size": 3.4, "human_led": 1, "members": 326},
        "humans": [{"name": "Ayla", "map": "pay_dun02", "x": 83, "y": 121, "seen_by": 5}],
        "levels": [{"from": a, "to": a + 9, "count": c} for a, c in zip(range(1, 100, 10), [20, 44, 71, 96, 121, 150, 133, 98, 60, 19])],
        "class_tiers": {"novice": 20, "first": 180, "second": 520, "transcendent": 92},
        "classes": [{"name": n, "count": 140 - 17 * i} for i, n in enumerate(CLASSES)],
        "map_kinds": {"town": 190, "field": 380, "dungeon": 242}, "maps_total": 74,
        "maps": [{"map": m, "kind": k, "online": n, "avg_level": lv, "fighting": f} for m, k, n, lv, f in
                 [("prontera", "town", 88, 55.1, 0), ("pay_dun02", "dungeon", 41, 71.2, 33), ("moc_fild12", "field", 37, 44.0, 29)]],
        "plans": [{"name": "agi-crit-hunter", "target": "Sniper", "profiles": 310, "online": 66, "online_avg_level": 63.1,
                   "known": 240, "known_avg_level": 58.4, "known_reached": 12}],
        "death_maps": [{"name": "gl_church", "count": 21}, {"name": "pay_dun03", "count": 9}],
    }


def history(now):
    t0 = int(now // 60 * 60) - 120 * 60
    return {"interval_s": 60, "points": [{"t": t0 + i * 60, "online": 700 + i, "avg_level": 60 + i / 90, "kills": 38 + i % 7,
                                          "deaths": i % 3, "levelups": 1 + i % 2, "jobchanges": i % 11 == 0, "loots": 150,
                                          "fighting": 380 + i % 30} for i in range(120)]}


def party_detail(now):
    def member(i, n):
        return {"name": n, "class": CLASSES[i % 6], "level": 82 - i, "job_level": 50, "role": "dps" if i else "tank",
                "online": True, "map": "pay_dun02", "x": 80 + i, "y": 120, "with_leader": i != 4, "hp": 0.9 - i * 0.1,
                "sp": 0.6, "zeny": 12000 + 3100 * i, "weight": 9000 + 900 * i, "max_weight": 24000, "state": "fighting Sohee",
                "kills": 410 - 30 * i, "deaths": i % 2, "dist_lead": 3 + i if i != 4 else -1, "lead_map": "pay_dun02",
                "kpm": round(2.4 - 0.3 * i, 2), "exp_h": 51000 - 4000 * i, "exp_min": 10,
                "gear_next": {"slot": "armor", "item": "Chain Mail", "cost": 65000, "shop": "prontera", "have": 21000,
                              "status": "saving", "turn": i == 1, "far": False},
                "effects": [{"name": "Blessing", "left_s": 140, "debuff": False}] + ([{"name": "Poison", "left_s": 12, "debuff": True}] if i == 2 else [])}
    ayla = {"name": "Ayla", "class": "Lord Knight", "level": 83, "job_level": 41, "role": "leader", "human": True, "leader": True,
            "has_stats": True, "online": True, "map": "pay_dun02", "x": 83, "y": 121, "hp": 0.84, "sp": 0.41, "zeny": 48210,
            "weight": 11000, "max_weight": 30000, "state": "attacking", "effects": [{"name": "Endure", "left_s": 8, "debuff": False}],
            "detail": {"fresh": True, "status_ts": now - 3, "status_age_s": 3, "intent": "sweeping pay_dun02 with the party",
                       "intent_since": now - 640, "map": "pay_dun02", "x": 83, "y": 121, "lock_map": "pay_dun02", "dest": "pay_dun02",
                       "lead_pos_src": "status", "base_exp_pct": 37.2, "job_exp_pct": 61.0, "exp_h": 44800, "job_exp_h": 21900,
                       "kpm": 2.9, "meter_min": 10, "deaths_today": 2, "ok_start": now - 5200, "restarts_today": 1,
                       "prev_start": now - 20000,
                       "last_decision": {"ts": now - 95, "text": "stay on pay_dun02: party together, kills per minute above par"},
                       "last_watchdog": {"ts": now - 4100, "text": "IDLE 180 s at 83,121: resumed sweep"},
                       "last_order": {"ts": now - 7000, "text": "do a town run with the party", "result": "townrun started"}}}
    human = {"name": "Starveil core", "human": "Ayla", "map": "pay_dun02", "checkpoint": "payon", "together": 5, "online": 6,
             "even_share": True, "formed_min": 412, "rally": {"active": False, "last": "payon Kafra", "last_ago_s": 5400},
             "members": [ayla] + [member(i, n) for i, n in enumerate(PARTY[:5])], "gear_turn": "Echobunny",
             "gear_log": ["Zendancer84 bought Tights 71,000z"],
             "helper": {"t": now - 380, "trigger": "schedule", "exit": 0, "result": "party together on pay_dun02, no action needed"},
             "chat": [{"ts": now - 40, "from": "Echobunny", "text": "heal up, more Sohee coming"},
                      {"ts": now - 300, "from": "Ayla", "text": "one more round then town"}]}
    bots = [{"name": "party%02d" % i, "leader": "Bot%03d" % i, "online": 4, "together": 3 + i % 2, "avg_level": 50 + i, "map": "moc_fild12",
             "members": [member(j, "Bot%03d_%d" % (i, j)) for j in range(4)]} for i in range(1, 9)]
    return [human] + bots


def guilds():
    return [{"id": "guild%02d" % i, "name": n, "online": 80 - 5 * i, "members": 400, "avg_level": 60 + i, "parties": 9,
             "kills_hour": 2400 - 100 * i, "kills_session": 18000, "deaths_session": 300, "founded": True, "style": "steady farmers",
             "strategy": "hold the Payon dungeon floors, priests in every party",
             "tactics": {"party_size": 4, "priest_share": 0.25}, "jobs": {"Hunter": 90, "Priest": 60},
             "leader": {"name": "Lead%02d" % i, "online": True, "level": 90 - i, "class": "Knight", "map": "prontera",
                        "always_online": True, "rank": i, "party_size": 4, "party_map": "gl_church"},
             "top": [{"name": "Top%d_%d" % (i, j), "level": 95 - j, "class": "Wizard"} for j in range(5)],
             "maps": [["pay_dun02", 22], ["prontera", 14], ["moc_fild12", 9], ["gl_church", 4]]}
            for i, n in enumerate(["Starveil", "Ironroot", "Dawnwatch"], 1)]


def leaderboard():
    rows = [{"rank": i + 1, "name": "Top%03d" % i, "class": CLASSES[i % 6], "level": 98 - i // 12, "job_level": 50,
             "guild": "Ironroot", "online": i % 3 != 0, "leader_of": "guild0%d" % (i // 3 + 1) if i in (0, 3, 6) else ""}
            for i in range(240)]
    for k, n in enumerate(PARTY[:5]):
        rows[14 + 23 * k]["name"] = n
    return {"ranked": len(rows), "top": rows, "leaders": [r for r in rows if r["leader_of"]], "note": "sample"}


def meter(now):
    dist = lambda m: {"mean": m, "median": m * 0.9, "p75": m * 1.2, "par": m * 1.2, "gap": m * 0.2, "prize": m * 4}
    return {"t": iso(now), "window": "1h", "rows": 812, "core_rows": 5, "server": {"bots": 640, "seen": 812, "online": 812,
            "kpm": 1.84, "zph": 9120, "deaths_h": 0.22},
            "cohorts": [{"key": k, "line": k.split("/")[0], "band": k.split("/")[1], "mode": "solo", "n": n, "seen": n + 4,
                         "kpm": dist(kp), "zph": dist(z), "deaths_h": 0.2, "shares": {"fighting": 0.6, "travelling": 0.1}}
                        for k, n, kp, z in [("archer/60-79", 88, 2.1, 11200), ("swordman/40-59", 73, 1.6, 7400), ("mage/60-79", 51, 2.6, 9900)]]}


def bot_rows(key):
    rows = [{"name": "Farm%02d" % i, "class": CLASSES[i % 6], "lv": 80 - i, "cohort": "archer/60-79", "instance": "main",
             "online": True, "map": "pay_dun02", "min": 60, "counted": True, "kpm": round(4.2 - 0.12 * i, 2), "zph": 31000 - 900 * i,
             "deaths_h": 0.1, "par_kpm": 2.5, "par_zph": 13400, "gap_kpm": -1.0, "gap_zph": -9000} for i in range(20)]
    return {"t": "", "window": "1h", "total": 640, "bots": sorted(rows, key=lambda r: -r[key])}


def neo(now):
    mem = [{"name": n, "class": CLASSES[i % 6], "lv": 82 - i, "cohort": "archer/80-99", "instance": "core", "online": True,
            "map": "pay_dun02", "min": 58, "counted": True, "kpm": round(2.4 - 0.3 * i, 2), "zph": 14200 - 1300 * i,
            "deaths_h": 0.3, "par_kpm": 2.2, "par_zph": 12100, "gap_kpm": 0.1, "gap_zph": 300} for i, n in enumerate(PARTY[:5])]
    grp = {"n": 5, "above_par_kpm": 2, "above_par_zph": 3, "ratio_kpm": 0.94, "ratio_zph": 1.03}
    return {"t": iso(now), "window": "1h", "party_summary": grp, "party": mem, "starveil_summary": grp, "starveil": mem,
            "ayla": {"name": "Ayla", "online": True, "lv": 83, "class": "Lord Knight", "map": "pay_dun02", "zeny": 48210,
                     "guild": "Starveil", "cohort": "swordman/80-99", "note": "sample: purse change, not loot value",
                     "windows": {"1h": {"min": 57, "zph_purse": 16800, "par_zph": 12900, "par_kpm": 2.3, "shares": {}}}}}


MINDS = ["ayla", "chat", "market", "build", "guild01", "guild02", "guild03", "qa", "analyst", "watchdog", "life", "deploy"]


def minds_list(now):
    out = []
    for i, n in enumerate(MINDS):
        seen = 40 + 400 * i
        out.append({"name": n, "status": "active" if seen < 180 else "idle", "unread": 1 if n == "market" else 0, "thoughts": 6,
                    "seen_ago_s": seen, "last_seen": iso(now - seen), "status_line": "sample status of %s" % n})
    return out


def thoughts(now, since):
    th = []
    for i in range(1, 61):
        n = MINDS[i % len(MINDS)]
        th.append({"id": i, "t": iso(now - (61 - i) * 420), "mind": n, "kind": ["thought", "decision", "action", "summary"][i % 4],
                   "text": "sample %s line %d: send two parties to Payon dungeon, raise potion stock" % (n, i)})
    msgs = [{"id": 70, "t": iso(now - 900), "mind": "market", "text": "keep red potion prices steady", "read": False},
            {"id": 71, "t": iso(now - 5000), "mind": "ayla", "text": "do a town run", "read": True, "read_at": iso(now - 4900)}]
    return {"mind": "all", "thoughts": [t for t in th if t["id"] > since], "messages": msgs, "next": 71}


def friends():
    return {"pairs_total": 5120, "tiers": {"close friend": 40, "friend": 310, "acquaintance": 2100}, "guild_changes_due_to_friends": 6,
            "counters": {"greetings": 812, "banter": 440},
            "loyalty": {"help_asked": 30, "help_answered": 24, "help_unanswered": 6, "at_risk": 11, "keep_gifts_sent": 4},
            "top": [{"a": "Echobunny", "a_guild": "Starveil", "b": "Zen.fox", "b_guild": "Starveil", "score": 912, "tier": "close friend",
                     "partied_min": 2210, "fight_min": 1800, "heals": 4100, "chats": 220, "trades": 6, "guild_min": 900, "last_contact": "2 min ago"}],
            "guild_moves": [{"bot": "Farm07", "from": "Ironroot", "to": "Starveil", "stage": "joined", "why": "friend Echobunny"}]}


def guild_rank(now):
    return {"instance": "main", "how": "sample scoring text", "history": {},
            "rank": [{"guild": "guild%02d" % i, "name": n, "t": iso(now), "place": i, "score": 640 - 55 * i, "wealth": w,
                      "stock_value": w // 2, "funds": w // 2, "guild_level": 9 - i, "members": 400, "avg_top10_level": 96 - i,
                      "top100_places": list(range(i, 60, 7))} for i, (n, w) in enumerate([("Ironroot", 8400000), ("Starveil", 9900000), ("Dawnwatch", 5100000)], 1)]}


def answer(path):
    now = time.time()
    route, _, q = path.partition("?")
    if route == "/api/overview": return overview(now)
    if route == "/api/overview/history": return history(now)
    if route == "/api/parties/detail": return party_detail(now)
    if route == "/api/guilds/detail": return guilds()
    if route == "/api/leaderboard": return leaderboard()
    if route == "/api/meter": return meter(now)
    if route == "/api/meter/neo": return neo(now)
    if route == "/api/meter/bots": return bot_rows("kpm" if "sort=kpm" in q else "zph")
    if route == "/api/minds": return minds_list(now)
    if route == "/api/minds/all/thoughts":
        since = int((q.split("since=")[1].split("&")[0]) or 0) if "since=" in q else 0
        return thoughts(now, since)
    if route == "/api/friends": return friends()
    if route == "/api/guild-rank": return guild_rank(now)
    if route == "/api/stats": return {"online": 812}
    return None


class H(BaseHTTPRequestHandler):
    def log_message(self, *a):
        pass

    def reply(self, code, obj):
        data = json.dumps(obj).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def do_GET(self):
        obj = answer(self.path)
        self.reply(200 if obj is not None else 404, obj if obj is not None else {"error": "not in the sample"})

    def do_POST(self):
        self.rfile.read(int(self.headers.get("Content-Length") or 0))
        self.reply(200, {"id": 99, "ok": True})


def sample_data(d, now=None, alive=True):
    """Fill folder d with sample runtime files. alive=False: heartbeats two hours old and stack.hold set."""
    now = now or time.time()
    os.makedirs(d, exist_ok=True)
    hb = now - (20 if alive else 7200)
    for name in ("sentinel", "partycheck", "dispatch", "aylalog", "bugq-watch"):
        with open(os.path.join(d, name + ".heartbeat"), "w") as f:
            f.write(str(int(hb)))
    with open(os.path.join(d, "stack.up" if alive else "stack.hold"), "w") as f:
        f.write("sample\n")
    rows = []
    for i in range(1, 13):
        rows.append({"id": "b%03d" % i, "t": now - 600 * i, "from": ["sentinel", "partycheck", "qa", "owner"][i % 4],
                     "priority": ["high", "normal", "low"][i % 3], "status": "open", "count": i % 4 + 1,
                     "text": "sample problem %d: bots idle at the Kafra in prontera after a town run" % i})
    rows += [{"id": "b001", "status": "assigned", "assigned_to": "fixer-idle"}, {"id": "b002", "status": "closed", "note": "verified in the log"},
             {"id": "b003", "status": "assigned", "assigned_to": "fixer-kafra"}]
    with open(os.path.join(d, "bugqueue.jsonl"), "w", encoding="utf-8") as f:
        f.write("".join(json.dumps(r) + "\n" for r in rows))
    with open(os.path.join(d, "deploys.jsonl"), "w", encoding="utf-8") as f:
        for i, tag in enumerate(["ro-legion:deploy-sample-1", "ro-legion:deploy-sample-2"]):
            f.write(json.dumps({"epoch": now - 9000 + 4000 * i, "tag": tag, "services": ["legion", "legion-core"], "critical": bool(i),
                                "reason": "Ayla fix" if i else "", "fixes": [{"text": "sample fix %d: party stays close" % i, "metric": "kpm", "cohort": ""}]}) + "\n")
    roles = ["ayla", "chat", "market", "build", "qa", "guild01", "guild02", "guild03"]
    with open(os.path.join(d, "puppet_heartbeat.json"), "w") as f:
        json.dump({"t": hb, "pid": 4242, "state": "ok" if alive else "stopped", "api_ok": alive, "running": ["chat"] if alive else [],
                   "roles": {r: {"last": hb - 300 * i, "spent_1h": 0.04 * (i % 3)} for i, r in enumerate(roles)}}, f)
    with open(os.path.join(d, "puppet_calls.jsonl"), "w", encoding="utf-8") as f:
        for i in range(24):
            f.write(json.dumps({"t": hb - 200 * (24 - i), "role": roles[i % len(roles)], "trigger": "schedule", "model": "haiku",
                                "exit": 1 if i == 5 else 0, "duration_s": 31.5, "cost_usd": 0.021, "turns": 4,
                                "result": "sample result %d: direction set, two parties to Payon" % i}) + "\n")
    with open(os.path.join(d, "meter.jsonl"), "w") as f:
        f.write(json.dumps({"t": iso(hb), "kind": "server", "window": "1h", "n": 640}) + "\n")
    with open(os.path.join(d, "minds.json"), "w") as f:
        th = thoughts(hb, 0)
        json.dump({"next_id": 72, "thoughts": th["thoughts"], "messages": th["messages"],
                   "minds": {n: {"last_seen": iso(hb - 60 * i), "status_line": "saved status of %s" % n} for i, n in enumerate(MINDS)}}, f)


if __name__ == "__main__":
    ap = argparse.ArgumentParser()
    ap.add_argument("--port", type=int, default=18090)
    ap.add_argument("--data", help="also write sample runtime files into this folder")
    ap.add_argument("--stopped", action="store_true", help="with --data: old heartbeats and stack.hold")
    a = ap.parse_args()
    if a.data:
        sample_data(a.data, alive=not a.stopped)
    print("fake engine on http://127.0.0.1:%d" % a.port, flush=True)
    ThreadingHTTPServer(("127.0.0.1", a.port), H).serve_forever()
