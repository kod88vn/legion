"""legionctl guild-direction / duty / rank / relations commands (redesign package G; engine: legion/guildduty*.go).

  guild-direction [guildNN]                  directions in force (leader-set + autopilot) for every guild or one
  guild-direction-set <guildNN> <kind> [--item NAME] [--map MAP] [--min-lv N] [--max-lv N] [--classes a,b]
                     [--note TEXT] [--hours H] [--exclusive] [--by leader]   kind: stock|gear|carry|level|supply|wealth
                     (--by market is RETIRED (EC43, design market-engine 4.3): refused here and by the engine (410);
                     the market gives information only: guild-direction-set guildNN ... is the leader's own call)
  guild-direction-clear <guildNN> [--id ID]  drop the leader directions (or one by id); autopilot takes over
  guild-duty [guildNN] [--all]               members' standing, today's minutes, block; --all lists good ones too
  guild-pardon <guildNN> <char>              standing back to good
  guild-kick <guildNN> <char> [--why TEXT]   queue an expel (exemptions and pacing still hold)
  guild-rank [--hours N]                     rank and wealth now + trend
  guild-relations                            relations between guilds (-1 rival .. +1 ally)
  guild-say <guildNN> <text...>              a line in guild chat, said by the guild's leader (also: say <guildNN> ...)
"""

import re

COMMANDS = ("guild-direction", "guild-direction-set", "guild-direction-clear", "guild-duty", "guild-pardon",
            "guild-kick", "guild-rank", "guild-relations", "guild-say")

GUILD_ID = re.compile(r"^guild\d{2}$", re.I)


def is_guild_id(s):
    """'guild04' and the like: a guild, not a character (say <guildNN> means the guild line)"""
    return bool(GUILD_ID.match(s or ""))


def _dir_line(d):
    what = d["kind"]
    if d.get("item"):
        what += " " + d["item"]
    if d.get("map"):
        what += " @" + d["map"]
    lv = ""
    if d.get("min_lv") or d.get("max_lv"):
        lv = f" lv{d.get('min_lv', 0)}-{d.get('max_lv') or 99}"
    cl = (" [" + ",".join(d["classes"]) + "]") if d.get("classes") else ""
    return f"  {d['set_by']:<9} {what}{lv}{cl}  until {d['expires'][11:16]}  {d.get('note', '')}  ({d['id']})"


def guild_cmd(cmd, args, call, out, opt, q):
    if cmd == "guild-direction":
        if args:
            gs = [call(f"/api/guild-direction/{q(args[0])}")]
        else:
            r = call("/api/guild-direction")
            print(f"guild duty {'ON' if r['duty_on'] else 'off'} ({r['instance']} instance)")
            gs = r["guilds"]
        for g in gs:
            print(f"{g['guild']} {g['name']}:")
            for d in g["active"] or []:
                print(_dir_line(d))
            if not g["active"]:
                print("  (none)")

    elif cmd == "guild-direction-set":
        if len(args) < 2:
            raise SystemExit("usage: guild-direction-set <guildNN> <kind> [--item NAME] [--map MAP] ...")
        body = {"kind": args[1]}
        for flag, key, conv in (("--item", "item", str), ("--map", "map", str), ("--min-lv", "min_lv", int),
                                ("--max-lv", "max_lv", int), ("--note", "note", str), ("--hours", "hours", float),
                                ("--by", "by", str)):
            v = opt(args, flag)
            if v:
                body[key] = conv(v)
        if body.get("by") == "market":
            # RETIRED (EC43, design market-engine 4.3 step 1): the market sets no direction; refuse before any call
            raise SystemExit("--by market is retired: the market gives information only (EC43); "
                             "a leader sets its own direction (omit --by or use --by leader)")
        cl = opt(args, "--classes")
        if cl:
            body["classes"] = [c.strip() for c in cl.split(",") if c.strip()]
        if opt(args, "--exclusive", flag=True):
            body["exclusive"] = True
        g = call(f"/api/guild-direction/{q(args[0])}", body)
        print(f"{g['guild']} {g['name']} now:")
        for d in g["active"]:
            print(_dir_line(d))

    elif cmd == "guild-direction-clear":
        if not args:
            raise SystemExit("usage: guild-direction-clear <guildNN> [--id ID]")
        i = opt(args, "--id")
        g = call(f"/api/guild-direction/{q(args[0])}", {"remove": i} if i else {"clear": True})
        for d in g["active"]:
            print(_dir_line(d))

    elif cmd == "guild-duty":
        show_all = opt(args, "--all", flag=True)
        path = "/api/guild-duty" + (f"?guild={q(args[0])}" if args else "")
        r = call(path)
        c = r["config"]
        print(f"guild duty {'ON' if r['duty_on'] else 'off'}  {r['date']}  owe {c['min_per_day']} min/day "
              f"(max half the time online)  warn {c['warn_days']} / kick {c['kick_days']} short days x strictness  "
              f"kicks last hour {r['kicks_last_hour']}/{c['kicks_per_hour']}")
        for g, st in sorted((r.get("standings") or {}).items()):
            print(f"  {g}: " + ", ".join(f"{k} {v}" for k, v in sorted(st.items())))
        for m in r.get("members") or []:
            if not show_all and m["standing"] in ("good", "exempt") and not m.get("block"):
                continue
            blk = ""
            if m.get("block") and not m["block"].get("ended"):
                b = m["block"]
                blk = f"  block {b['kind']} {b.get('item', '')} @{','.join(b['maps'][:2])} {b['done_min']}/{b['target_min']}m"
            flags = (" WARN-PENDING" if m.get("warn_pending") else "") + (" KICK-PENDING" if m.get("kick_pending") else "")
            print(f"  {m['guild']} {m['char']:<24} {m['standing']:<7} short {m['short_days']}  today "
                  f"{m['today_duty_min']}/{m['today_owed_min']}m (online {m['today_online_min']}m){blk}{flags}"
                  f"{'  [' + m['exempt'] + ']' if m.get('exempt') else ''}")
        for k in r.get("kicked") or []:
            print(f"  kicked: {k['char']} from {k['from']} at {k['at'][:16]}  stage {k['stage']}"
                  f"{' -> ' + k['joined'] if k.get('joined') else ''}")
        if r.get("counters"):
            print("  counters: " + ", ".join(f"{k} {v}" for k, v in sorted(r["counters"].items())))

    elif cmd in ("guild-pardon", "guild-kick"):
        if len(args) < 2:
            raise SystemExit(f"usage: {cmd} <guildNN> <char>")
        body = {"char": args[1]}
        why = opt(args, "--why")
        if why:
            body["why"] = why
        out(call(f"/api/guild-duty/{q(args[0])}/{'pardon' if cmd == 'guild-pardon' else 'kick'}", body))

    elif cmd == "guild-rank":
        h = opt(args, "--hours", "24")
        r = call(f"/api/guild-rank?hours={q(str(h))}")
        for g in r["rank"] or []:
            hist = r["history"].get(g["guild"]) or []
            trend = ""
            if len(hist) >= 2:
                trend = f"  score {hist[0]['score']:+.1f}->{hist[-1]['score']:.1f}  wealth {hist[0]['wealth']}->{hist[-1]['wealth']}"
            print(f"#{g['place']:<2} {g['guild']} {g['name']:<20} score {g['score']:<7} lv{g['guild_level']} "
                  f"members {g['members']:<3} top10 avg {g['avg_top10_level']}  wealth {g['wealth']}z "
                  f"(stock {g['stock_value']}z + funds {g['funds']}z){trend}")
        if not r["rank"]:
            print("(not computed yet: the engine ranks guilds every 10 min)")

    elif cmd == "guild-relations":
        r = call("/api/guild-relations")
        for x in r["relations"] or []:
            print(f"{x['name_a']} <-> {x['name_b']}: {x['value']:+.3f} {x['label']}  ({x['events']} events; "
                  f"last: {x.get('last_event', '')})")
        if not r["relations"]:
            print("(all neutral)")
        print(r["scale"])

    elif cmd == "guild-say":
        if len(args) < 2 or not " ".join(args[1:]).strip():
            raise SystemExit("usage: guild-say <guildNN> <text...>")
        out(call(f"/api/guild-leaders/{q(args[0])}/say", {"text": " ".join(args[1:])}))
