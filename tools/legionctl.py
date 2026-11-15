#!/usr/bin/env python3
"""legionctl: command-line access to the Legion bot engine for LLM agents (Claude Code / Copilot sub-agents).

Usage: python tools/legionctl.py <command> [args]      (Legion API: http://127.0.0.1:8090, override LEGION_API)

World
  status                         population, activity, market, parties, humans online
  humans                         real (non-bot) players and which bots are near them
  nearby <map> [--limit N]       online characters on a map (for staging conversations)
  spots <level>                  suggested hunting maps for a level
Characters & players
  guild <guildNN> [--all]        roster: online members (class, level, map, doing)
  bot <name>                     one character: state, stats, persona, inventory
  memory <name> [--diary N]      a player's memory: goals, people they know, recent diary
  remember <name> [--note T] [--fact T] [--goal T] [--goal-done T] [--person NAME --relation R --person-note T]
Chat
  inbox --role ROLE [--bots] [--all]   new messages players said (since this role's last call). By default lines
                                 spoken by Legion characters are hidden (--bots shows them); --all ignores the cursor
  say <name> <text...>           public chat (heard by everyone nearby)
  whisper <name> <to> <text...>  private message
  party-say <name> <text...>     party chat
Strategy
  goal <name> <hunt|idle|follow|vend> [map] [--who NAME]
  guild-goal <guildNN> <mode> <map>
  leader <guildNN>               your guild leader: online (always), level, server rank, party, free guild members
                                 (class/role/level) and the engine's suggested party + best-EXP map
  leader-party <guildNN> [map] [--members a,b,c]   form the leader's party (members auto-picked: tank/priest/ranged
                                 within 10 levels; map default: best guide spot for the party)
  leaderboard [N]                server top N by level (default 20) + every guild leader's rank
  boss-list <guildNN>            bosses (MVPs / mini-bosses) the guild leader's party could hunt: level, map, respawn,
                                 party needed, ready or what is missing
  boss-check <guildNN> <boss>    readiness of the leader's party for that boss (full build, top gear, pots, wings, roles)
  boss-hunt <guildNN> <boss>     send the leader's party after the boss (refused unless ready); boss-status; boss-cancel <guildNN>
  party <leader> <member> [<member>...] [--map MAP]
  parties                        active parties
  market                         open vending shops in Prontera
  shop <name> <title...> [--markup X]   set a vendor's shop title (and price level, 1.0-3.0)
Game data (drop tables, monsters, maps: all items, all monsters incl. MVPs/mini-bosses, fields and dungeons)
  drops "<item>" [--for NAME] [--lv 60,62] [--all]   who drops an item (rate %), and the maps to farm it ranked by
                                 drop rate x spawn count; --for a leader/party member (or --lv) grades each map
                                 against that party's levels; danger = MVP / mini-boss / aggressive share
  mob "<monster>" [--for NAME]   level, HP, element, race, size, exp, drops with rates, spawn maps (dungeon/field)
  mapinfo <map> [--for NAME]     monsters spawning on a map (count, level, aggro, best drops/cards), danger, warps
                                 (names are fuzzy: "SS card", "blue pot", "ygg"; ids work too)
Economy
  economy                        player trades: totals, open wants (B>), offers (S>), recent trades, average prices
  alchemists                     every guild's alchemist: path progress (Novice -> Merchant -> Alchemist), Pharmacy,
                                 guide/bowls/bottles/herbs, potion stock, what it brewed, its next step
  potions                        potion + alchemy material market: standard price, offers, wants, trades
Meter (kills/min, zeny/h, deaths, time budget; windows 10m | 1h | session; par = p75 of the cohort)
  meter [--window W] [--top N] [--sort kpm] [--json]    server totals + cohorts ranked by prize (gap to par x n)
  meter bots [--cohort KEY] [--sort zph|kpm|gap_zph|gap_kpm|deaths|min|name] [--asc] [--limit N] [--window W] [--json]
  meter bot <name> [--json]      one bot: every window, time shares, its cohort's par and its gap
  meter neo [--window W] [--json]   Ayla, her party and Starveil against their cohorts' par
Exchange and chat jobs (docs/design/exchange.md; flags exchange_tiers_on, chat_jobs_on in tuning.json)
  exchange [--json]              party / guild / friend hand-overs: rules, counters, recent hand-overs, intents
  chat-jobs [list] [--peek] [--recent] [--json]   social lines waiting for words (listing = the chat agent's poll;
                                 --peek looks without counting as the agent)
  chat-jobs answer <id> <speaker> <text...> [--channel C] [--to NAME]   say this instead of the template line
  chat-jobs answer <id> --json '[{"speaker":..,"channel":..,"text":..}, ...]'   several lines (a short exchange)
  chat-jobs pass <id>            say the template line now
  chat-intent <from> <need|offer> <item...> [--count N] [--max P] [--price P] [--note T]   what a human (or a messy
                                 conversation) meant: the engine treats it like a want / an offer
  chat-intent list               registered intents and what happened to them
Guild direction and duty (tools/legionctl_guild.py)
  guild-direction [guildNN]      directions in force; guild-direction-set <guildNN> <kind> [--item X] [--map M]
                                 [--min-lv N] [--max-lv N] [--classes a,b] [--note T] [--hours H] [--exclusive];
                                 guild-direction-clear <guildNN> [--id ID]   (kinds: stock gear carry level supply wealth)
  guild-duty [guildNN] [--all]   standings, today's minutes, blocks, kicked bots; guild-pardon / guild-kick <guildNN> <char>
  guild-rank [--hours N]         rank and wealth + trend;  guild-relations   rival..ally table
  guild-say <guildNN> <text...>  guild chat in the leader's voice (say <guildNN> <text> does the same)
Minds bus (the owner watches these on the dashboard and can send orders)
  orders <mind>                  owner's unread messages to this mind (marks them read); "(no new orders)" if none
  think <mind> <text...> [--kind thought|decision|action|summary] [--status "short status line"]
  minds                          every mind: active/idle, last seen, unread orders, latest thought
Puppet masters and system agents (tools/puppet/, tools/system/bugq.py reads the queue)
  bug "<text>" [--from role] [--priority P] [--evidence T]   append to the bug queue (legion/data/bugqueue.jsonl)
  neo "<request>"                Ayla's line: top-priority queue entry with her evidence attached + neo.wake
  notes <role> [--set T | --append T]   a puppet master's small notes file between calls
Misc
  events --role ROLE [--kinds k1,k2]    raw events since this role's last call
  wait <seconds>                 sleep (use between agent cycles)
"""
import collections
import json
import os
import re
import sys
import time
import urllib.parse
import urllib.request

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import legionctl_guild  # noqa: E402  guild direction / duty / rank / relations commands

API = os.environ.get("LEGION_API", "http://127.0.0.1:8090")
STATE = os.path.join(os.path.dirname(os.path.abspath(__file__)), ".state")


def call(path, body=None):
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(API + path, data=data, headers={"Content-Type": "application/json"},
                                 method="POST" if body is not None else "GET")
    try:
        with urllib.request.urlopen(req, timeout=20) as r:
            return json.loads(r.read() or b"null")
    except urllib.error.HTTPError as e:
        sys.exit(f"error {e.code}: {e.read().decode(errors='replace').strip()}")
    except urllib.error.URLError as e:
        sys.exit(f"Legion API not reachable at {API} ({e.reason}). Is the stack up? (see AGENTS.md)")


def q(s):
    return urllib.parse.quote(s, safe="")


def opt(args, name, default=None, flag=False):
    if name in args:
        i = args.index(name)
        if flag:
            args.pop(i)
            return True
        v = args[i + 1]
        del args[i:i + 2]
        return v
    return False if flag else default


def cursor(role, key, value=None):
    os.makedirs(STATE, exist_ok=True)
    p = os.path.join(STATE, f"{role}.json")
    try:
        st = json.load(open(p))
    except Exception:
        st = {}
    if value is None:
        return st.get(key)
    st[key] = value
    json.dump(st, open(p, "w"))


def out(obj):
    print(json.dumps(obj, indent=1, ensure_ascii=False, default=str))


def party_levels(name):
    """levels of the party NAME leads or belongs to (else NAME's own level), for grading farm maps"""
    n = name.lower()
    try:
        for p in call("/api/parties/detail") or []:
            names = [(m.get("name") or "").lower() for m in p.get("members") or []]
            if n in names or (p.get("leader") or "").lower() == n or (p.get("human") or "").lower() == n:
                lv = [m["level"] for m in p["members"] if m.get("level")]
                if lv:
                    return lv, p.get("name") or name
    except SystemExit:
        pass
    try:
        b = call(f"/api/bots/{q(name)}")
        lv = b.get("level") or b.get("base_level") or b.get("BaseLv")
        if lv:
            return [int(lv)], name
    except SystemExit:
        pass
    return [], name


def _danger(d):
    if not d:
        return ""
    bits = []
    if d.get("mvp"):
        bits.append("MVP " + "/".join(d["mvp"]))
    if d.get("mini_boss"):
        bits.append("mini-boss " + "/".join(d["mini_boss"][:3]))
    bits.append(f"aggro {d.get('aggressive_pct', 0)}%")
    if d.get("strongest"):
        bits.append(f"top {d['strongest']} lv{d.get('strongest_level')}")
    return ", ".join(bits)


def _pct(x):
    return f"{x:g}%"


def lookup_cmd(cmd, args):
    who = opt(args, "--for", "")
    lvs = opt(args, "--lv", "")
    show_all = opt(args, "--all", flag=True)
    if not args:
        sys.exit(f"usage: legionctl {cmd} <name>")
    label = ""
    if who and not lvs:
        lv, label = party_levels(who)
        lvs = ",".join(str(x) for x in lv)
        if not lv:
            print(f"(no level found for {who}; maps not graded)")
    kind = {"drops": "item", "mob": "monster", "mapinfo": "map"}[cmd]
    d = call(f"/api/lookup/{kind}?q={q(' '.join(args))}&lv={q(lvs or '')}")
    r = d["result"]
    others = d.get("matches", [])[1:]
    if lvs:
        print(f"graded for {label or 'levels'} lv {lvs}")
    if kind == "item":
        slots = f", {r['slots']} slots" if r.get("slots") else ""
        print(f"{r['name']} (id {r['id']}, {r['type']}{slots})  NPC buy {r['npc_buy']}z / sells {r['npc_sell']}z"
              f"  weight {r['weight'] / 10:g}")
        if r.get("npc_shops"):
            print("  NPC shops: " + ", ".join(f"{s['map']} ({s['shop']})" for s in r["npc_shops"][:6]))
        src = r.get("dropped_by") or []
        if not src:
            print("  no monster drops it (quest / craft / shop / event item)")
        else:
            print(f"  dropped by {len(src)} monster(s):")
            for s in src[:999 if show_all else 12]:
                tag = " MVP" if s["mvp"] else (" boss" if s["boss"] else "")
                where = ", ".join(f"{m['map']}({m['kind'][0]})x{m['count']}" for m in (s["maps"] or [])[:6]) \
                    or "does not spawn (summon/event/instance only)"
                print(f"    {s['monster']:<26} lv{s['level']:<3}{tag:<5} {_pct(s['rate_pct']):>7}"
                      f"{' (MVP reward)' if s['mvp_drop'] else ''}  {where}")
        farm = r.get("farm_maps") or []
        if farm:
            print("  where to farm (best drop % x spawn count first" + (", party fit first" if lvs else "") + "):")
            for f in farm[:999 if show_all else 10]:
                fit = f"  [{f['fit']}]" if f.get("fit") else ""
                tag = " MVP" if f["mvp"] else (" boss" if f["boss"] else "")
                print(f"    {f['map']:<13} {f['kind']:<8} {f['monster']}{tag} x{f['count']} lv{f['level']}"
                      f" {_pct(f['rate_pct'])} score {f['score']:g}{fit} | {_danger(f['danger'])}")
            if len(farm) > 10 and not show_all:
                print(f"    ... {len(farm) - 10} more maps (--all)")
            best = max(s["rate_pct"] for s in src)
            if best <= 1:
                print(f"  note: best rate {_pct(best)} = about {round(100 / best)} kills per drop on average")
    elif kind == "monster":
        tag = " MVP" if r["mvp"] else (" mini-boss" if r["mini_boss"] else "")
        fit = f"  [{r['fit']}]" if r.get("fit") else ""
        print(f"{r['name']} (id {r['id']}){tag} lv{r['level']} HP {r['hp']}  {r['element']} {r['element_level']}"
              f" {r['race']} {r['size']}  exp {r['base_exp']}/{r['job_exp']}"
              f"  {'aggressive' if r['aggressive'] else 'passive'}{fit}")
        drops = r.get("drops") or []
        print("  drops: " + (", ".join(f"{x['name']} {_pct(x['rate_pct'])}{' (MVP)' if x['mvp_drop'] else ''}"
                                       for x in drops) or "none"))
        sp = r.get("spawns") or []
        if not sp:
            print("  does not spawn on any map (summon / event / instance only)")
        for s in sp[:999 if show_all else 15]:
            print(f"  {s['map']:<13} {s['kind']:<8} x{s['count']:<4} | {_danger(s['danger'])}")
    else:
        print(f"{r['map']} ({r['kind']})  {r['total']} monsters, mean lv {r['mean_level']},"
              f" NPC loot ~{r['loot_zeny_per_kill']:.0f}z/kill  danger: {_danger(r['danger'])}")
        for m in r["monsters"]:
            tag = " MVP" if m["mvp"] else (" mini-boss" if m["mini_boss"] else "")
            fit = f" [{m['fit']}]" if m.get("fit") else ""
            print(f"  {m['name']:<24} x{m['count']:<4} lv{m['level']:<3} HP {m['hp']:<7} {m['element']:<7}"
                  f" {m.get('race', ''):<10} {'aggro' if m['aggressive'] else 'passive':<7}{tag}{fit}"
                  f"  {', '.join(m.get('drops') or [])}")
        if r.get("warps_to"):
            print("  warps to: " + ", ".join(r["warps_to"]))
    if others:
        names = [o if isinstance(o, str) else f"{o['name']} ({o['id']})" for o in others]
        print("  other matches: " + "; ".join(names[:7]))


# ---------------------------------------------------------------- puppet-master commands (tools/puppet)
DATA_DIR = os.environ.get("LEGION_DATA") or os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "legion", "data")
NOTES_DIR = os.path.join(os.path.dirname(os.path.abspath(__file__)), "puppet", "notes")
BUGQUEUE = "bugqueue.jsonl"
PUPPET_CMDS = {"bug", "neo", "notes"}


def try_call(path, body=None):
    """like call() but returns None instead of exiting (evidence gathering must never fail a report)"""
    try:
        return call(path, body)
    except SystemExit:
        return None


def queue_entry(source, text, priority="normal", evidence=None, data_dir=None):
    """one bug-queue entry (the format tools/system/bugq.py reads): id, t, from, priority, text, evidence, status"""
    now = time.time()
    return {"id": "b%d-%03d" % (int(now * 1000), os.getpid() % 1000),
            "t": time.strftime("%Y-%m-%dT%H:%M:%S", time.localtime(now)),
            "from": source, "priority": priority, "text": text, "evidence": evidence if evidence is not None else [],
            "status": "open"}


def queue_append(entry, data_dir=None):
    d = data_dir or DATA_DIR
    os.makedirs(d, exist_ok=True)
    with open(os.path.join(d, BUGQUEUE), "a", encoding="utf-8") as f:
        f.write(json.dumps(entry, ensure_ascii=False) + "\n")


def neo_evidence():
    """Ayla's recent evidence from the API: meter view, her party, her latest events"""
    ev = {}
    m = try_call("/api/meter/neo")
    if m is not None:
        ev["meter_neo"] = m
    pt = try_call("/api/parties")
    if isinstance(pt, list):
        ev["party"] = [p for p in pt if "ayla" in json.dumps(p, default=str).lower()][:1]
    e = try_call("/api/events?since=0")
    if isinstance(e, dict):
        rows = [x for x in (e.get("events") or []) if "ayla" in json.dumps(x, default=str).lower()]
        ev["recent_events"] = rows[-15:]
    b = try_call("/api/bots/Ayla")
    if b is not None:
        ev["ayla"] = {k: b.get(k) for k in ("map", "level", "class", "state", "zeny", "x", "y") if isinstance(b, dict) and k in b}
    return ev


def puppet_cmd(cmd, args):
    """bug / neo / notes. meter, exchange, chat-jobs, chat-intent and the guild-* commands are the engine
    authors' own implementations (meter_cmd, the exchange block in main(), tools/legionctl_guild.py)."""
    if cmd == "bug":
        source = opt(args, "--from", "unknown")
        prio = opt(args, "--priority", "normal")
        evid = opt(args, "--evidence", "")
        if not args:
            sys.exit('usage: bug "<text>" [--from role] [--priority normal|high] [--evidence "..."]')
        e = queue_entry(source, " ".join(args), prio, [evid] if evid else [])
        queue_append(e)
        print(f"queued {e['id']} ({prio})")
    elif cmd == "neo":
        source = opt(args, "--from", "ayla")
        if not args:
            sys.exit('usage: neo "<request>"')
        e = queue_entry(source, " ".join(args), "neo", neo_evidence())
        queue_append(e)
        os.makedirs(DATA_DIR, exist_ok=True)
        with open(os.path.join(DATA_DIR, "neo.wake"), "w") as f:
            f.write(e["t"])
        print(f"queued {e['id']} (neo, main session will be woken)")
    elif cmd == "notes":
        if not args:
            sys.exit('usage: notes <role> [--set "text" | --append "text"]')
        role = re.sub(r"[^a-z0-9_-]", "", args[0].lower())
        path = os.path.join(NOTES_DIR, role + ".md")
        rest = args[1:]
        new, add = opt(rest, "--set"), opt(rest, "--append")
        if new or add:
            os.makedirs(NOTES_DIR, exist_ok=True)
            text = new if new else (open(path, encoding="utf-8").read() if os.path.exists(path) else "") + "\n" + add
            with open(path, "w", encoding="utf-8") as f:
                f.write(text.strip()[-2000:] + "\n")
            print("notes saved")
        else:
            print(open(path, encoding="utf-8").read() if os.path.exists(path) else "(no notes)")
def _shares(sh):
    return " ".join(f"{k[:5]} {round(v * 100)}%" for k, v in sorted((sh or {}).items(), key=lambda x: -x[1]))


def _botline(b):
    return (f"{b['name'][:18]:<18} {b['class'][:12]:<12} {b['lv']:>2} {b['cohort']:<22} {b['min']:>5}m "
            f"kpm {b['kpm']:>5} (own {b['kpm_own']:>5}, par {b['par_kpm']:>5})  "
            f"z/h {int(b['zph']):>7} (own {int(b['zph_own']):>7}, par {int(b['par_zph']):>7})  "
            f"d/h {b['deaths_h']:<4}{'' if b['counted'] else ' (not counted)'}")


def meter_cmd(args):
    sub = args.pop(0) if args and not args[0].startswith("--") else ""
    win = opt(args, "--window", "1h")
    raw = opt(args, "--json", flag=True)
    if sub == "":
        top = int(opt(args, "--top", "15"))
        srt = opt(args, "--sort", "")
        r = call(f"/api/meter?window={q(win)}" + ("&sort=kpm" if srt == "kpm" else ""))
        if raw:
            return out(r)
        s = r["server"]
        print(f"meter {r['window']} @ {r['t'][11:19]}  bots counted {s['bots']}/{s['seen']} (online {s['online']}, core rows {r['core_rows']})  fair = mob lv >= bot lv - {r['fair_lv_gap']}")
        print(f"server: kpm {s['kpm']} (median {s['kpm_median']})  z/h {int(s['zph'])} (median {int(s['zph_median'])})  deaths/h {s['deaths_h']}  "
              f"income {s['income']} = loot {s['loot']} - cons {s['cons']} - travel {s['travel']}")
        print("time:", _shares(s.get("shares")))
        print(f"{'cohort':<24}{'n':>4} {'kpm mean/med/par':>20} {'z/h mean/med/par':>26} {'z prize':>9} {'k prize':>8} {'d/h':>5}  time")
        for c in r["cohorts"][:top]:
            k, z = c["kpm"], c["zph"]
            print(f"{c['key']:<24}{c['n']:>4} {k['mean']:>6}/{k['median']:>6}/{k['par']:>6} "
                  f"{int(z['mean']):>8}/{int(z['median']):>8}/{int(z['par']):>8} {int(z['prize']):>9} {k['prize']:>8} {c['deaths_h']:>5}  {_shares(c.get('shares'))}")
    elif sub == "bots":
        coh = opt(args, "--cohort", "")
        srt = opt(args, "--sort", "zph")
        lim = opt(args, "--limit", "30")
        asc = opt(args, "--asc", flag=True)
        r = call(f"/api/meter/bots?window={q(win)}&cohort={q(coh)}&sort={q(srt)}&limit={q(str(lim))}" + ("&order=asc" if asc else ""))
        if raw:
            return out(r)
        print(f"{r['total']} bots ({r['window']}), showing {len(r['bots'])}")
        for b in r["bots"]:
            print(_botline(b))
    elif sub == "bot":
        if not args:
            sys.exit("usage: meter bot <name>")
        r = call(f"/api/meter/bot/{q(args[0])}")
        if raw:
            return out(r)
        print(f"{r['name']} {r['class']} lv{r['lv']} [{r['instance']}] {r['cohort']}  {r.get('map') or 'offline'}  bucket {r.get('bucket')}  state: {r.get('state')}")
        for w in ("10m", "1h", "session"):
            x = r["windows"][w]
            st, rw = x["stats"], x["raw"]
            print(f"  {w:<8} {_botline(st)}")
            print(f"           kills {rw['kills']} fair {rw['fair']} deaths {rw['deaths']}  loot {rw['loot']} cons {rw['cons']} travel {rw['travel']}  "
                  f"purse +{rw['zeny_in']}/-{rw['zeny_out']}  duty {rw['duty_min']:.0f}m  time: {_shares(st.get('shares'))}")
    elif sub == "neo":
        r = call(f"/api/meter/neo?window={q(win)}")
        if raw:
            return out(r)
        a = r.get("ayla")
        if a:
            aw = a["windows"].get(r["window"], {})
            print(f"Ayla: {a['name']} {a['class']} lv{a['lv']} {a['map']} online={a['online']} zeny {a['zeny']}  cohort {a['cohort']}")
            print(f"  {r['window']}: {aw.get('min')}m  purse z/h {int(aw.get('zph_purse', 0))} (cohort par z/h {int(aw.get('par_zph', 0))}, par kpm {aw.get('par_kpm')})  time: {_shares(aw.get('shares'))}")
            print(f"  ({a['note']})")
        else:
            print("Ayla: no status.json")
        for label, key, lst in (("party", "party_summary", "party"), ("Starveil", "starveil_summary", "starveil")):
            g = r[key]
            print(f"{label}: {g['n']} counted, above par kpm {g['above_par_kpm']} / z/h {g['above_par_zph']}, "
                  f"mean ratio to par kpm {g['ratio_kpm']} z/h {g['ratio_zph']}")
            for b in r[lst][:25]:
                print("  " + _botline(b))
    else:
        sys.exit("usage: meter [bots|bot <name>|neo] [--window 10m|1h|session]")


def main(argv):
    if not argv or argv[0] in ("-h", "--help", "help"):
        print(__doc__)
        return
    cmd, args = argv[0], argv[1:]

    if cmd == "status":
        s = call("/api/stats")
        m = call("/api/market")
        p = call("/api/parties")
        h = call("/api/humans")
        busiest = sorted(s["maps"].items(), key=lambda x: -x[1])[:10]
        print(f"profiles {s['bots']}  online {s['online']}  kills {s['kills']}  deaths {s['deaths']}  uptime {s['uptime_min']} min")
        print("doing:", ", ".join(f"{k} {v}" for k, v in sorted(s["states"].items(), key=lambda x: -x[1])))
        print("busiest maps:", ", ".join(f"{k} {v}" for k, v in busiest))
        print(f"market: {sum(1 for x in m if x['open'])} shops open / {len(m)} vendors online;  parties: {len(p)}")
        print("humans online:", ", ".join(f"{x['name']} ({x['map']} {x['x']},{x['y']})" for x in h) or "none seen")

    elif cmd == "humans":
        out(call("/api/humans"))

    elif cmd == "nearby":
        lim = int(opt(args, "--limit", 40))
        if not args: sys.exit("usage: nearby <map> [--limit N]")
        # map names as the minds write them: "Moc_Fild02", "moc_fild02.gat", "pay_fild07 (Payon field)" -> moc_fild02
        m = re.match(r"[a-z0-9_@-]+", args[0].strip().lower().replace(".gat", "").replace(".rsw", ""))
        mname = m.group(0) if m else args[0]
        bots = [b for b in (call(f"/api/bots?map={q(mname)}") or []) if b.get("online")]
        if not bots:
            busy = collections.Counter(b.get("map") for b in (call("/api/bots") or []) if b.get("online") and b.get("map"))
            close = [f"{k} ({v})" for k, v in busy.most_common() if k.split("_")[0] == mname.split("_")[0]][:8]
            print(f"nobody on {mname}" + (f"; people on: {', '.join(close)}" if close else ""))
        for b in bots[:lim]:
            # every field may be missing or null (core-instance / human entries, a bot mid-login): never crash on one
            g = lambda k, d="?": d if b.get(k) is None else b.get(k)
            print(f"{str(g('name')):<24} {str(g('class')):<14} lv{str(g('level')):<3} ({g('x')},{g('y')}) {str(g('state', ''))[:40]}  [{g('archetype')}, {b.get('guild') or 'no guild'}]")

    elif cmd == "spots":
        out(sorted({s["Map"] for s in call(f"/api/spots?level={int(args[0])}")}))

    elif cmd == "guild":
        allm = opt(args, "--all", flag=True)
        ms = call(f"/api/bots?guild={q(args[0])}")
        on = [m for m in ms if m["online"]]
        print(f"{args[0]}: {len(ms)} characters' players, {len(on)} online")
        ld = next((m for m in ms if m.get("guild_leader")), None)
        if ld:
            print(f"LEADER (the one guild leader: in-game guild master once founded; speak as them): {ld['name']} "
                  f"{ld['class']} lv{ld['level']} {ld['map']} {'online' if ld['online'] else 'offline'}")
        for m in (ms if allm else on):
            print(f"{m['name']:<24} {'LEADER' if m.get('guild_leader') else m['role']:<8} {m['class']:<14} lv{m['level']:<3} {m['map']:<12} {m['state'][:38]:<38} k{m['kills']} d{m['deaths']} [{m['archetype']}]")

    elif cmd == "bot":
        out(call(f"/api/bots/{q(args[0])}"))

    elif cmd == "memory":
        n = opt(args, "--diary", 30)
        out(call(f"/api/memory/{q(args[0])}?diary={n}"))

    elif cmd == "remember":
        name = args[0]
        body = {k: v for k, v in {
            "note": opt(args, "--note"), "fact": opt(args, "--fact"), "goal": opt(args, "--goal"),
            "goal_done": opt(args, "--goal-done"), "person": opt(args, "--person"),
            "relation": opt(args, "--relation"), "person_note": opt(args, "--person-note")}.items() if v}
        out(call(f"/api/memory/{q(name)}", body))

    elif cmd == "inbox":
        role = opt(args, "--role", "default")
        bots = opt(args, "--bots", flag=True)
        allc = opt(args, "--all", flag=True)
        since = 0 if allc else (cursor(role, "inbox") or call("/api/inbox?since=999999999999")["next"])
        r = call(f"/api/inbox?since={since}")
        if not allc:
            cursor(role, "inbox", r["next"])
        msgs = [m for m in (r["messages"] or []) if bots or not m["from_bot"]]
        if not msgs:
            print("(no new messages)")
        for m in msgs:
            tag = "BOT" if m["from_bot"] else "HUMAN"
            hb = m.get("heard_by") or []
            heard = ", ".join(hb[:6]) + (f" (+{m.get('heard_count', len(hb)) - 6} more)" if m.get("heard_count", len(hb)) > 6 else "")
            print(f"[{m['kind']}] {m.get('t', '')} {m['from']} ({tag}, {m['map']}): {m['text']}   -> heard by: {heard}")

    elif cmd == "say" and args and legionctl_guild.is_guild_id(args[0]):  # say <guildNN>: the guild line
        legionctl_guild.guild_cmd("guild-say", args, call, out, opt, q)
    elif cmd == "say":
        out(call(f"/api/bots/{q(args[0])}/say", {"text": " ".join(args[1:])}))
    elif cmd == "whisper":
        out(call(f"/api/bots/{q(args[0])}/whisper", {"to": args[1], "text": " ".join(args[2:])}))
    elif cmd == "party-say":
        out(call(f"/api/bots/{q(args[0])}/party-chat", {"text": " ".join(args[1:])}))

    elif cmd == "goal":
        who = opt(args, "--who", "")
        body = {"mode": args[1], "map": args[2] if len(args) > 2 else "", "who": who}
        out(call(f"/api/bots/{q(args[0])}/goal", body))
    elif cmd == "guild-goal":
        out(call(f"/api/guilds/{q(args[0])}/goal", {"mode": args[1], "map": args[2]}))
    elif cmd == "leader":
        d = call(f"/api/guild-leaders/{q(args[0])}")
        l = d["leader"]
        print(f"{l['guild']} {l['guild_name']}: leader {l['name']} ({l['player']}) {'ONLINE' if l['online'] else 'offline (relogging)'}"
              f" lv{l['level']}/{l['job_level']} {l['class']} on {l.get('map') or '-'}  rank #{l['rank']} of {l['ranked_of']}"
              f"  hp {l['hp']}/{l['max_hp']} sp {l['sp']}/{l['max_sp']} zeny {l['zeny']}  relogs {l['relogs']}")
        print(f"  state: {l['state']}" + (f"  error: {l['last_error']}" if l.get('last_error') else ""))
        print(f"  party: {l.get('party') or '(none)'} {l.get('party_map') or ''} {', '.join(l.get('party_members') or [])}")
        print(f"  suggested party: {', '.join(d['suggested_party']) or '(nobody free within 10 levels)'} at {d['suggested_map'] or '-'}")
        print(f"  free guild members online ({len(d['free_mates'])}):")
        for m in d["free_mates"][:25]:
            print(f"    {m['name']:<24} {m['role']:<8} {m['class']:<14} lv{m['level']:<3} {m['map']}")
    elif cmd == "leader-party":
        ms = opt(args, "--members", "")
        body = {"map": args[1] if len(args) > 1 else "", "members": [m.strip() for m in ms.split(",") if m.strip()]}
        out(call(f"/api/guild-leaders/{q(args[0])}/party", body))
    elif cmd == "leaderboard":
        d = call(f"/api/leaderboard?limit={int(args[0]) if args else 20}")
        for t in d["top"]:
            star = f"  * leader of {t['leader_of']}" if t.get("leader_of") else ""
            print(f"#{t['rank']:<4} {t['name']:<24} {t['class']:<14} lv{t['level']:<3} job{t['job_level']:<3} {t.get('guild') or '':<18}{star}")
        print(f"guild leaders ({d['ranked']} ranked):")
        for t in d["leaders"]:
            print(f"  {t['leader_of']}: {t['name']:<24} #{t['rank']:<5} lv{t['level']} {t['class']} {'online' if t['online'] else 'offline'}")
    elif cmd == "boss-list":
        for r in call(f"/api/boss-hunt/bosses?guild={q(args[0])}"):
            b = r["boss"]
            st = "READY" if r.get("ready") else ("missing: " + "; ".join(r.get("missing") or []) if r.get("missing") else f"{r.get('member_issues', '?')} member issues")
            print(f"{b['name']:<20} lv{b['level']:<3} {'MVP ' if b['mvp'] else 'mini'} {b['map']:<12} respawn {b['respawn_min']:>3}m  party lv{b['party_min_level']}+ x{b['party_min_members']}  {st}")
    elif cmd == "boss-check":
        d = call("/api/boss-hunt", {"guild": args[0], "boss": " ".join(args[1:]), "check": True})
        b = d["boss"]
        print(f"{b['name']} lv{b['level']} on {b['map']} (respawn {b['respawn_min']} min): {'READY' if d['ready'] else 'NOT READY'}")
        if b.get("tactics"):
            print(f"  tactics: {b['tactics']}")
        for m in d.get("missing") or []:
            print(f"  party: {m}")
        for m in d["party"]:
            print(f"  {m['name']:<22} {m['class']:<12} lv{m['level']:<3} {m['role']:<8} {'; '.join(m.get('issues') or []) or 'ok'}")
    elif cmd == "boss-hunt":
        d = call("/api/boss-hunt", {"guild": args[0], "boss": " ".join(a for a in args[1:] if a != "--force"), "force": "--force" in args})
        if d.get("ok"):
            h = d["hunt"]
            print(f"boss hunt started: {h['leader']}'s party -> {h['boss']['name']} on {h['boss']['map']}")
        else:
            print(f"refused: {d.get('error')}")
            c = d.get("check") or {}
            for m in c.get("missing") or []:
                print(f"  party: {m}")
            for m in c.get("party") or []:
                if m.get("issues"):
                    print(f"  {m['name']:<22} {'; '.join(m['issues'])}")
    elif cmd == "boss-status":
        d = call("/api/boss-hunt")
        for h in d.get("active") or []:
            print(f"ACTIVE {h['guild']} {h['leader']} -> {h['boss']['name']} ({h['boss']['map']}) phase {h['phase']} since {h['started'][11:19]}")
        for h in (d.get("history") or [])[-10:]:
            print(f"  {h['guild']} {h['boss']['name']}: {h['phase']} ({h.get('reason', '')}) kills {h['kills']}")
    elif cmd == "boss-cancel":
        req = urllib.request.Request(API + f"/api/boss-hunt?guild={q(args[0])}", method="DELETE")
        with urllib.request.urlopen(req, timeout=20) as r:
            print(r.read().decode())
    elif cmd == "party":
        mp = opt(args, "--map", "")
        out(call("/api/party", {"leader": args[0], "members": args[1:], "map": mp}))
    elif cmd == "parties":
        out(call("/api/parties"))

    elif cmd == "market":
        for s in call("/api/market"):
            state = "OPEN" if s["open"] else "setting up"
            print(f"{s['bot']:<24} ({s['x']},{s['y']}) {state:<10} \"{s['title']}\"  {'; '.join((s['items'] or [])[:4])}")
    elif cmd == "shop":
        mk = opt(args, "--markup", None)
        body = {"text": " ".join(args[1:])}
        if mk:
            body["markup"] = float(mk)
        out(call(f"/api/bots/{q(args[0])}/shop", body))

    elif cmd in ("drops", "mob", "mapinfo"):
        lookup_cmd(cmd, args)

    elif cmd == "economy":
        e = call("/api/economy")
        print(f"trades completed {e.get('trades_total')}  failed deals {e.get('deals_failed')}  "
              f"open wants {len(e.get('wants') or [])}  deals in progress {len(e.get('deals') or [])}")
        wants = {}
        for w in e.get("wants") or []:
            k = (w["item"], w["why"])
            wants[k] = wants.get(k, 0) + 1
        print("wants:", ", ".join(f"{k[0]} x{v} ({k[1]})" for k, v in sorted(wants.items(), key=lambda x: -x[1])[:12]) or "none")
        print("offers:", ", ".join(f"{o['item']} {o['qty']} @{o['lowest_ask']} (std {o['standard']})" for o in (e.get("offers") or [])[:12]) or "none")
        for t in (e.get("trades") or [])[:10]:
            print(f"  {t['t'][11:19]} {t['seller']} -> {t['buyer']}: {t['count']}x {t['item']} for {t['price']}z ({t['map']})")
        for d in e.get("deals") or []:
            print(f"  deal: {d['seller']} -> {d['buyer']} {d['count']}x {d['item']} {d['price']}z [{d['meet']}]")

    elif cmd == "alchemists":
        r = call("/api/alchemists")
        for a in r.get("alchemists") or []:
            on = f"ONLINE {a.get('map', '')}" if a.get("online") else "offline"
            print(f"{a['guild']} {a.get('guild_name', ''):<16} {a['alchemist']:<22} {a['class']:<10} {a['base_level']}/{a['job_level']} "
                  f"[{a['role']}, {a['candidates']} on the path] {on}")
            if a.get("online"):
                print(f"    pharmacy {a['pharmacy']} research {a['potion_research']} guide {a['has_guide']} bowls {a['medicine_bowls']} "
                      f"bottles {a['empty_potion_bottles']} scell {a['scell']} zeny {a.get('zeny', 0)} herbs {a.get('herbs') or {}} "
                      f"potions {a.get('potions') or {}} brewed {a.get('brewed_session') or {}} failed {a['failed_session']}")
            print(f"    next: {a['next_step']}" + (f"  (last: {a['last']})" if a.get("last") else ""))

    elif cmd == "potions":
        for p_ in call("/api/alchemists").get("potions") or []:
            extra = []
            if "offered_qty" in p_:
                extra.append(f"offered {p_['offered_qty']} by {p_['sellers']} from {p_['lowest_ask']}z")
            if "wanted_by" in p_:
                extra.append(f"wanted {p_['wanted_qty']} by {p_['wanted_by']}")
            if "trades" in p_:
                extra.append(f"{p_['trades']} trades, last {p_['last_each']}z avg {p_['avg_each']}z")
            print(f"{p_['item']:<24} std {p_['standard']:>7}z  {'NPC sells' if p_['npc_sells'] else 'players only':<12} {'; '.join(extra)}")

    elif cmd == "events":
        role = opt(args, "--role", "default")
        kinds = opt(args, "--kinds", "")
        since = cursor(role, "events") or 0
        r = call(f"/api/events?since={since}")
        cursor(role, "events", r["next"])
        ks = set(kinds.split(",")) if kinds else None
        for e in r["events"]:
            if ks is None or e["kind"] in ks:
                print(f"{e['t'][11:19]} [{e['kind']}] {e['bot']} ({e.get('map', '')}): {e['text']}")

    elif cmd == "think":
        kind = opt(args, "--kind", "thought")
        status = opt(args, "--status", "")
        if len(args) < 2:
            sys.exit("usage: think <mind> <text...> [--kind decision]")
        body = {"text": " ".join(args[1:]), "kind": kind}
        if status:
            body["status"] = status
        t = call(f"/api/minds/{q(args[0].lower())}/thought", body)
        print(f"ok (thought #{t['id']})")
    elif cmd == "orders":
        r = call(f"/api/minds/{q(args[0].lower())}/inbox?ack=1")
        if not r["messages"]:
            print("(no new orders)")
        for m in r["messages"]:
            print(f"[{m['t'][11:19]}] OWNER: {m['text']}")
    elif cmd == "minds":
        for m in call("/api/minds"):
            ago = f"{m['seen_ago_s']}s ago" if "seen_ago_s" in m else "never"
            lt = (m.get("last_thought") or {}).get("text", "")
            print(f"{m['name']:<10} {m['status']:<7} seen {ago:<10} unread {m['unread']:<3} {lt[:90]}")

    elif cmd == "exchange":
        as_json = opt(args, "--json", flag=True)
        r = call("/api/exchange")
        if as_json:
            out(r)
            return
        print(f"exchange tiers {'ON' if r['on'] else 'off'}  kin discount {r['kin_discount_pct']}% of market  "
              f"favours/giver/hour {r['gifts_per_hour']}  ask stage {r['ask_stage_sec']}s")
        for part, rep in (("main", r), ("core", r.get("core") or {})):
            if rep.get("counts"):
                print(f"{part} counts: " + ", ".join(f"{k} {v}" for k, v in sorted(rep["counts"].items())))
            for x in (rep.get("recent") or [])[:15]:
                print(f"  {x['t'][11:19]} {x['giver']} -> {x['taker']}: {x['count']}x {x['item']} [{x['tier']}/{x['relation']}] "
                      f"{x['price']}z (market {x['market_each']}z ea) {x['status']}")
            for i in (rep.get("intents") or [])[:10]:
                print(f"  intent #{i['id']} {i['from']} {i['kind']} {i['count']}x {i['item']}: {i['status']}")

    elif cmd == "chat-jobs":
        sub = args.pop(0) if args and args[0] in ("list", "answer", "pass") else "list"
        if sub == "list":
            peek, recent, as_json = opt(args, "--peek", flag=True), opt(args, "--recent", flag=True), opt(args, "--json", flag=True)
            qs = "&".join(x for x in ("peek=1" if peek else "", "recent=1" if recent else "") if x)
            r = call("/api/chat-jobs" + ("?" + qs if qs else ""))
            if as_json:
                out(r)
                return
            print(f"chat jobs {'ON' if r['on'] else 'off'}; waiting {len(r.get('jobs') or [])}; wait {r.get('wait_sec')}s")
            for j in r.get("jobs") or []:
                print(f"[{j['id']}] {j['kind']} {j['channel']}{' -> ' + j['to'] if j.get('to') else ''} speaker {j['speaker']} "
                      f"hears {','.join(j.get('hearers') or [])} deadline {str(j['deadline'])[11:19]}")
                print(f"    facts {json.dumps(j.get('facts') or {}, ensure_ascii=False)}")
                for l in j.get("template_lines") or []:
                    print(f"    template: {l['speaker']} ({l['channel']}{' -> ' + l['to'] if l.get('to') else ''}): {l['text']}")
            if recent:
                for j in r.get("recent") or []:
                    print(f"  done [{j['id']}] {j['kind']} {j['state']}: " + " | ".join(f"{l['speaker']}: {l['text']}" for l in j.get("said") or []))
        elif sub == "pass":
            if not args:
                sys.exit("usage: chat-jobs pass <id>")
            out(call(f"/api/chat-jobs/{q(args[0])}/lines", []))
        else:
            raw = opt(args, "--json")
            ch, to = opt(args, "--channel"), opt(args, "--to")
            if raw:
                if not args:
                    sys.exit("usage: chat-jobs answer <id> --json '[...]'")
                lines = json.loads(raw)
            else:
                if len(args) < 3:
                    sys.exit("usage: chat-jobs answer <id> <speaker> <text...> [--channel C] [--to NAME]")
                line = {"speaker": args[1], "text": " ".join(args[2:])}
                if ch:
                    line["channel"] = ch
                if to:
                    line["to"] = to
                lines = [line]
            out(call(f"/api/chat-jobs/{q(args[0])}/lines", lines))

    elif cmd == "chat-intent":
        if args and args[0] == "list":
            for i in call("/api/chat-intent").get("intents") or []:
                print(f"#{i['id']} {i['t'][11:19]} {i['from']}{' (human)' if i['human'] else ''} {i['kind']} {i['count']}x {i['item']}: {i['status']}")
            return
        count, mx, price, note = opt(args, "--count", 1), opt(args, "--max", 0), opt(args, "--price", 0), opt(args, "--note", "")
        if len(args) < 3 or args[1] not in ("need", "offer"):
            sys.exit("usage: chat-intent <from> <need|offer> <item...> [--count N] [--max P] [--price P] [--note T]")
        out(call("/api/chat-intent", {"from": args[0], "kind": args[1], "item": " ".join(args[2:]), "count": int(count),
                                       "max_price": int(mx), "price": int(price), "note": note}))
    elif cmd == "meter":
        meter_cmd(args)
    elif cmd in PUPPET_CMDS:
        puppet_cmd(cmd, args)

    elif cmd == "wait":
        time.sleep(float(args[0]))
    elif cmd in legionctl_guild.COMMANDS:  # tools/legionctl_guild.py: guild direction, duty, rank, relations
        legionctl_guild.guild_cmd(cmd, args, call, out, opt, q)
    else:
        sys.exit(f"unknown command {cmd!r} (try: help)")


if __name__ == "__main__":
    main(sys.argv[1:])
