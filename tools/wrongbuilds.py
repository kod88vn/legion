"""Wrong-build audit and restart for Legion characters (owner rule 2026-11-14: "Wrong build = delete and start over").

  python tools/wrongbuilds.py audit            # writes legion/data/wrong_builds.json, prints counts
  python tools/wrongbuilds.py reset NAME,...   # restarts those (listed in wrong_builds.json) as level 1 Novices
  python tools/wrongbuilds.py reset --all      # every listed character that is offline now

"wrong" = irreversible deviation from the character's build plan (personas.json plan -> builds.go):
  - first-job skill levels above what the plan ever gives that skill (>= 2 points), or
  - a vendor Merchant with >= 11 merchant skill points and no Vending, or a support Acolyte with >= 3 and no Heal, or
  - a stat above what the plan reaches with twice the status points already spent (+1), or a class/plan mismatch.
An order difference that stays within the plan (e.g. Divine Protection before Heal for a monk plan that wants both)
self-corrects as the planner always raises the first unmet plan goal; those are listed under order_only, not reset.
Safety: only accounts 2100001-2199999 (lg*), never Ayla's party, only when the WHOLE account is offline; goods go
to the account storage. Take a DB backup first (see HANDOVER.md).
"""
import csv, io, json, os, re, subprocess, sys, urllib.request, urllib.parse, collections, datetime, random

SRV = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
sys.path.insert(0, os.path.join(SRV, 'tools', 'system'))
import common as _C  # the one data-folder resolver (rule G23)
DATA = _C.data_dir()
OUT = os.path.join(DATA, 'wrong_builds.json')


def sql(q, raw=False):
    r = subprocess.run(['wsl', '-d', 'Ubuntu-24.04', '--', 'docker', 'exec', '-i', 'ro-db', 'mariadb', '-uragnarok', '-pragnarok', 'ragnarok', '-B'],
                       input=q.encode('utf8'), capture_output=True)
    if r.returncode:
        sys.exit('sql failed: ' + r.stderr.decode())
    if raw:
        return r.stdout.decode('utf8')
    return list(csv.DictReader(io.StringIO(r.stdout.decode('utf8')), delimiter='\t', quoting=csv.QUOTE_NONE))
PARTY = {'zendancer84', 'echobunny', 'zen.fox', 'xxrinfangxx', 'silent rogue', 'lillilyblade', 'vex_x18'}  # Silent rogue: Ayla's ex-Alchemist, kept (AY11)

# ---- plans parsed from builds.go
src = open(os.path.join(SRV, 'legion', 'builds.go'), encoding='utf8').read()
STAT = {'stSTR': 0, 'stAGI': 1, 'stVIT': 2, 'stINT': 3, 'stDEX': 4, 'stLUK': 5}
plans = {}
for m in re.finditer(r'"(\w+)": \{ID: "\w+".*?First: (\d+).*?Stats:\s+st\((.*?)\),.*?Skills:\s+sk\((.*?)\),', src, re.S):
    pid, first, stats, skills = m.group(1), int(m.group(2)), m.group(3), m.group(4)
    sv = [x.strip() for x in stats.split(',')]
    st = [(STAT[sv[i]], int(sv[i + 1])) for i in range(0, len(sv) - 1, 2)]
    kv = [int(x) for x in skills.split(',')]
    plans[pid] = dict(first=first, stats=st, skills=[(kv[i], kv[i + 1]) for i in range(0, len(kv) - 1, 2)], legacy=[])
    bm = re.search(r'Base: "(\w+)"', src[m.start():m.end()])
    plans[pid]['base'] = bm.group(1) if bm else pid  # build variants (builds.go Base) belong to a main plan
    # Legacy: an earlier plan's goals (builds.go Build.Legacy). Characters built under it are NOT wrong builds
    # (owner 2026-11-14: existing bolt Mages keep their levels and grow into the AoE plan): their caps count too.
    # LegacyStats: an earlier stat plan (owner 2026-11-14: priests go INT-first; old INT/VIT/DEX priests stay valid)
    nxt = re.search(r'\n\t"\w+": \{ID:', src[m.end():])
    block = src[m.start():m.end() + (nxt.start() if nxt else len(src) - m.end())]
    ls = re.search(r'LegacyStats:\s+st\((.*?)\),', block)
    if ls:
        lv = [x.strip() for x in ls.group(1).split(',')]
        plans[pid]['legacy_stats'] = [(STAT[lv[i]], int(lv[i + 1])) for i in range(0, len(lv) - 1, 2)]
    lm = re.match(r'\s*Legacy:\s+sk\((.*?)\),', src[m.end():])
    if lm:
        lv = [int(x) for x in lm.group(1).split(',')]
        plans[pid]['legacy'] = [(lv[i], lv[i + 1]) for i in range(0, len(lv) - 1, 2)]
vf = re.search(r'var vendorFirst = sk\((.*?)\)', src).group(1)
vf = [int(x) for x in vf.split(',')]
VENDOR_FIRST = [(vf[i], vf[i + 1]) for i in range(0, len(vf) - 1, 2)]
assert sum(p["base"] == k for k, p in plans.items()) == 13, plans.keys()

# ---- 1st-job skill trees (Renewal): id -> (max, {prereq: lv})
TREE = {
    1: {2: (10, {}), 3: (10, {2: 1}), 4: (10, {}), 5: (10, {}), 6: (10, {}), 7: (10, {5: 5}), 8: (10, {6: 5})},
    2: {9: (10, {}), 10: (1, {}), 11: (10, {}), 12: (10, {11: 7, 13: 5}), 13: (10, {11: 4}), 14: (10, {}), 15: (10, {14: 5}),
        16: (10, {}), 17: (10, {19: 4}), 18: (10, {10: 1, 17: 5}), 19: (10, {}), 20: (10, {}), 21: (10, {20: 4})},
    3: {43: (10, {}), 44: (10, {43: 3}), 45: (10, {44: 1}), 46: (10, {}), 47: (10, {46: 5})},
    4: {22: (10, {}), 23: (10, {22: 3}), 24: (1, {}), 25: (1, {27: 4}), 26: (2, {24: 1}), 27: (4, {26: 2}), 28: (10, {}),
        29: (10, {28: 3}), 30: (10, {29: 1}), 31: (1, {24: 1}), 32: (10, {23: 3}), 33: (10, {22: 3}), 34: (10, {22: 5}), 35: (1, {28: 2})},
    5: {36: (10, {}), 37: (10, {36: 3}), 38: (10, {37: 3}), 39: (10, {36: 5}), 40: (1, {}), 41: (10, {39: 3}), 42: (10, {})},
    6: {48: (10, {}), 49: (10, {}), 50: (10, {}), 51: (10, {50: 5}), 52: (10, {}), 53: (1, {52: 3})},
}
PRIO = {1: [5, 2, 3, 7], 2: [19, 14, 20, 9, 13], 3: [46, 43, 44], 4: [28, 34, 29, 22, 23, 25, 54, 66, 67],
        5: [42, 36, 37, 39, 41], 6: [48, 49, 50]}
CORE = {0: 'Novice', 1: 'Swordman', 2: 'Mage', 3: 'Archer', 4: 'Acolyte', 5: 'Merchant', 6: 'Thief'}


def sim_skills(plan, role, cls, n):
    """the planner as builds.go/progress.go intend it: n first-job points."""
    tree = TREE[cls]
    lv = collections.Counter()
    lists = [VENDOR_FIRST, plan['skills']] if role == 'vendor' else [plan['skills']]

    def up(i):
        if i not in tree:
            return False
        mx, req = tree[i]
        return lv[i] < mx and all(lv[k] >= v for k, v in req.items())
    for _ in range(n):
        pick = None
        for l in lists:
            for i, g in l:
                if up(i) and lv[i] < g:
                    pick = i
                    break
            if pick:
                break
        if pick is None:
            for i in PRIO[cls]:
                if up(i):
                    pick = i
                    break
        if pick is None:
            c = [i for i in tree if up(i)]
            pick = min(c) if c else None
        if pick is None:
            break
        lv[pick] += 1
    return lv


def cost(v):
    return (v - 1) // 10 + 2


def spent(st):
    return sum(sum(cost(v) for v in range(1, s)) for s in st)


def sim_stats(plan, budget):
    st = [1] * 6
    for s, goal in plan['stats']:
        while st[s] < goal:
            c = cost(st[s])
            if c > budget:
                return st
            budget -= c
            st[s] += 1
    return st


def get(url):
    try:
        with urllib.request.urlopen(url, timeout=10) as r:
            return json.load(r)
    except Exception as e:
        return None


TOWN = {"prontera": (156, 191), "izlude": (128, 114), "geffen": (119, 59), "payon": (162, 233), "morocc": (156, 93),
        "alberta": (116, 57), "aldebaran": (140, 131), "yuno": (158, 125)}
FIRST_BRANCH = {'knight': (1, 0), 'crusader': (1, 1), 'wizard': (2, 0), 'sage': (2, 1), 'hunter': (3, 0), 'bard': (3, 1),
                'dancer': (3, 1), 'priest': (4, 0), 'monk': (4, 1), 'blacksmith': (5, 0), 'alchemist': (5, 1),
                'assassin': (6, 0), 'rogue': (6, 1)}
CLEAR = ['skill', 'inventory', 'cart_inventory', 'char_reg_num', 'char_reg_str', 'quest', 'sc_data', 'skillcooldown',
         'hotkey', 'memo', 'bonus_script', 'buyingstores', 'achievement']


def audit():
    chars = sql("SELECT c.char_id,c.account_id,l.userid,c.char_num,c.name,c.class,c.base_level,c.job_level,c.str,c.agi,c.vit,c.`int`,c.dex,c.luk,c.online "
                "FROM `char` c LEFT JOIN login l ON l.account_id=c.account_id WHERE c.account_id BETWEEN 2100001 AND 2199999;")
    skills = collections.defaultdict(dict)
    for r in sql('SELECT s.char_id,s.id,s.lv,s.flag FROM skill s JOIN `char` c ON c.char_id=s.char_id WHERE c.account_id BETWEEN 2100001 AND 2199999;'):
        if r['flag'] == '0':
            skills[int(r['char_id'])][int(r['id'])] = int(r['lv'])
    reg = collections.defaultdict(dict)
    for r in sql("SELECT char_id,`key`,value FROM char_reg_num WHERE `key` IN ('legion_first','legion_branch');"):
        reg[int(r['char_id'])][r['key']] = int(r['value'])

    ps = json.load(open(os.path.join(DATA, 'personas.json'), encoding='utf8'))
    spec = {}
    for p in ps:
        for c in p['characters']:
            spec[c['char_id']] = (p, c)
    leaders = set()
    try:
        gl = json.load(open(os.path.join(DATA, 'guild_leaders.json'), encoding='utf8'))
        s = json.dumps(gl)
        leaders = set(gl.values())  # guild -> account (lgNNNN)
    except Exception:
        pass

    online = {b['name'] for b in (get('http://127.0.0.1:8090/api/bots') or []) if b.get('online')}

    order_only = []
    out, party_wrong, skipped = [], [], collections.Counter()
    for r in chars:
        acc = int(r['account_id'])
        if not (2100001 <= acc <= 2199999):
            continue
        cid, cls = int(r['char_id']), int(r['class'])
        st = [int(r[k]) for k in ('str', 'agi', 'vit', 'int', 'dex', 'luk')]
        lvl, jlv = int(r['base_level']), int(r['job_level'])
        sk = dict(skills.get(cid, {}))
        src_ = 'db'
        if r['name'] in online:
            d = get('http://127.0.0.1:8090/api/bots/' + urllib.request.quote(r['name']))
            if d and d.get('online'):
                sk = {int(k): v for k, v in (d.get('skills') or {}).items() if v > 0}
                m = re.search(r'stats str(\d+) agi(\d+) vit(\d+) int(\d+) dex(\d+) luk(\d+)', d.get('plan', ''))
                if m:
                    st = [int(x) for x in m.groups()]
                lvl, jlv = d.get('base_level', lvl), d.get('job_level', jlv)
                cn = d.get('class')
                inv = {v: k for k, v in CORE.items()}
                if cn in inv:
                    cls = inv[cn]
                src_ = 'api'
        if cid not in spec:
            skipped['no persona'] += 1
            continue
        p, c = spec[cid]
        plan = plans.get(c.get('plan', ''))
        problems, notes = [], []
        if plan is None:
            problems.append('no build plan (plan=%r)' % c.get('plan'))
        else:
            if c['role'] == 'support' and plan['base'] != 'priest':
                problems.append('support char with plan %s (not priest)' % c['plan'])
            if c['role'] == 'vendor' and plan['base'] not in ('blacksmith', 'alchemist'):
                problems.append('vendor char with plan %s' % c['plan'])
            rf = reg[cid].get('legion_first')
            if rf is not None and rf != plan['first']:
                problems.append('legion_first=%d but plan %s needs 1st job %d' % (rf, c['plan'], plan['first']))
            if cls in TREE and cls != plan['first']:
                problems.append('class %s cannot reach target %s (plan %s)' % (CORE[cls], c.get('target_class'), c['plan']))
            # ---- skills
            if cls in TREE and cls == plan['first']:
                first_pts = sum(v for k, v in sk.items() if k in TREE[cls])
                # caps: the most each first-job skill gets from the plan's goal lists (vendorFirst too for vendors)
                lists = ([VENDOR_FIRST] if c['role'] == 'vendor' else []) + [plan['skills'], plan['legacy']]
                cap = collections.Counter()
                for l in lists:
                    for i, g in l:
                        if i in TREE[cls]:
                            cap[i] = max(cap[i], g)
                plan1 = sum(cap.values())
                over = {k: v - cap[k] for k, v in sk.items() if k in TREE[cls] and v > cap[k]}
                overn = sum(over.values()) - max(0, first_pts - plan1)  # points past the plan's 1st-job goals may go anywhere
                if overn >= 2:
                    problems.append('skills off-plan (irreversible) by %d pts: has %s, plan caps %s' % (
                        overn, {k: sk[k] for k in sorted(over)}, {k: cap[k] for k in sorted(over)}))
                else:
                    exp = sim_skills(plan, c['role'], cls, first_pts)
                    offn = sum(v - exp[k] for k, v in sk.items() if k in TREE[cls] and v > exp[k])
                    if offn >= 3:
                        notes.append('skill order differs from plan by %d pts (self-corrects: all within plan caps)' % offn)
                if c['role'] == 'vendor' and cls == 5 and first_pts >= 11 and sk.get(41, 0) == 0:
                    problems.append('vendor without Vending after %d merchant pts' % first_pts)
                if c['role'] == 'support' and cls == 4 and first_pts >= 3 and sk.get(28, 0) == 0:
                    problems.append('support acolyte without Heal after %d pts' % first_pts)
            bad_nov = sk.get(1, 0)
            if cls == 0 and any(k != 1 for k in sk if k < 200):
                problems.append('novice with job skills %s' % sk)
            # ---- stats
            budget = spent(st)
            horizon = sim_stats(plan, budget + max(budget, 40))
            over = {i: st[i] - horizon[i] for i in range(6) if st[i] > horizon[i] + 1}
            if over and plan.get('legacy_stats'):
                lh = sim_stats(dict(stats=plan['legacy_stats']), budget + max(budget, 40))
                if not any(st[i] > lh[i] + 1 for i in range(6)):
                    over = {}  # fits the earlier stat plan: not a wrong build
            if over:
                problems.append('stats off-plan (irreversible): has %s, plan by double these points %s' % (st, horizon))
            else:
                exp_st = sim_stats(plan, budget)
                dist = sum(abs(a - b) for a, b in zip(st, exp_st))
                if dist > 4:
                    notes.append('stat order differs from plan (|diff|=%d, converges): has %s, plan now %s' % (dist, st, exp_st))
        if not problems:
            if notes:
                order_only.append(dict(name=r['name'], account=r['userid'], cls=CORE.get(cls, str(cls)), plan=c.get('plan'), role=c['role'], base_level=lvl, notes=notes))
            continue
        item = dict(name=r['name'], account=r['userid'], account_id=acc, char_id=cid, slot=int(r['char_num']),
                    role=c['role'], plan=c.get('plan'), target_class=c.get('target_class'),
                    class_id=cls, cls=CORE.get(cls, str(cls)), base_level=lvl, job_level=jlv,
                    stats=st, skills=sk, online=r['name'] in online, source=src_,
                    guild_leader=r['userid'] in leaders, problems=problems)
        if r['name'].lower() in PARTY:
            party_wrong.append(item)
        else:
            out.append(item)

    def kind(pr):
        k = pr.split(' ')[0:2]
        if pr.startswith('skills off-plan'):
            return 'skills off-plan'
        if pr.startswith('stats off-plan'):
            return 'stats off-plan'
        return pr.split('(')[0].split(' after')[0].strip()[:50]

    byclass = collections.Counter(i['cls'] for i in out)
    byprob = collections.Counter(kind(p) for i in out for p in i['problems'])
    byclassprob = collections.Counter('%s/%s/%s' % (i['cls'], i['role'], kind(p)) for i in out for p in i['problems'])
    res = dict(generated=datetime.datetime.now().isoformat(timespec='seconds'),
               rule=__doc__.split('"wrong" = ')[1].split('Safety:')[0].strip(),
               count=len(out), by_class=byclass, by_problem=byprob, by_class_role_problem=byclassprob,
               ayla_party_wrong=party_wrong, guild_leaders_wrong=[i['name'] for i in out if i['guild_leader']],
               online_wrong=[i['name'] for i in out if i['online']], order_only_count=len(order_only), order_only=order_only, skipped=skipped, wrong=out)
    try:
        res['reset_log'] = json.load(open(OUT, encoding='utf8')).get('reset_log', [])
    except Exception:
        res['reset_log'] = []
    json.dump(res, open(OUT, 'w', encoding='utf8'), indent=1, default=dict)
    print('order-only', len(order_only)); print('wrong', len(out), 'online', len(res['online_wrong']), 'leaders', res['guild_leaders_wrong'], 'party', [i['name'] for i in party_wrong])
    print(byclass)
    for k, v in byclassprob.most_common():
        print(v, k)


def reset_sql(wrong, names):
    ps = json.load(open(os.path.join(DATA, 'personas.json'), encoding='utf8'))
    spec = {c['char_id']: (p, c) for p in ps for c in p['characters']}
    r = random.Random()
    out = ['-- restart wrong-build characters as level 1 Novices (generated by build_resetgen.py)']
    done = []
    for w in wrong:
        cid, acc = w['char_id'], w['account_id']
        if names is not None and w['name'] not in names:
            continue
        assert 2100001 <= acc <= 2199999, w
        assert w['account'].startswith('lg'), w
        assert w['name'].lower() not in PARTY, w
        p, c = spec[cid]
        assert p['account_id'] == acc and c['name'] == w['name'], w
        first, branch = FIRST_BRANCH[plans.get(c['plan'], {}).get('base', c['plan'])]
        home = p.get('home_town') or 'prontera'
        tx, ty = TOWN.get(home, TOWN['prontera'])
        esc = c['name'].replace("\\", "\\\\").replace("'", "''")
        ok = "@ok%d" % cid
        out.append(f"SET {ok} := (SELECT COUNT(*) FROM `char` WHERE account_id={acc} AND online<>0)=0 AND "
                   f"(SELECT COUNT(*) FROM `char` WHERE char_id={cid} AND account_id={acc} AND name='{esc}' AND account_id BETWEEN 2100001 AND 2199999)=1;")
        # like a real player deleting a character: its goods (cards, gear, materials, potions) go to the account storage first
        cols = ("nameid,amount,identify,refine,attribute,card0,card1,card2,card3,option_id0,option_val0,option_parm0,option_id1,option_val1,option_parm1,"
                "option_id2,option_val2,option_parm2,option_id3,option_val3,option_parm3,option_id4,option_val4,option_parm4,expire_time,bound,unique_id,enchantgrade")
        for t in ('inventory', 'cart_inventory'):
            out.append(f"INSERT INTO storage (account_id,equip,{cols}) SELECT {acc},0,{cols} FROM {t} WHERE char_id={cid} AND nameid NOT IN (1201,2301,569,11500) AND {ok};")
        for t in CLEAR:
            out.append(f"DELETE FROM {t} WHERE char_id={cid} AND {ok};")
        out.append(f"DELETE vi FROM vending_items vi JOIN vendings v ON v.id=vi.vending_id WHERE v.char_id={cid} AND {ok};")
        out.append(f"DELETE FROM vendings WHERE char_id={cid} AND {ok};")
        zeny = 1500 + r.randrange(1500)
        out.append(f"UPDATE `char` SET class=0,base_level=1,job_level=1,base_exp=0,job_exp=0,zeny={zeny},str=1,agi=1,vit=1,`int`=1,dex=1,luk=1,"
                   f"max_hp=40,hp=40,max_sp=11,sp=11,status_point=48,skill_point=0,`option`=0,karma=0,manner=0,"
                   f"weapon=1201,shield=0,head_top=0,head_mid=0,head_bottom=0,robe=0,"
                   f"last_map='{home}',last_x={tx + r.randrange(7) - 3},last_y={ty + r.randrange(7) - 3},save_map='{home}',save_x={tx},save_y={ty} "
                   f"WHERE char_id={cid} AND account_id={acc} AND online=0 AND {ok};")
        kit = [(1201, 1, 2), (2301, 1, 16), (569, 120 + r.randrange(60), 0), (11500, 10 + r.randrange(10), 0),
               (501, 10 + r.randrange(10), 0), (601, 5 + r.randrange(5), 0), (602, 2 + r.randrange(2), 0)]
        sel = " UNION ALL ".join(f"SELECT {cid},{i},{n},{e},1 FROM DUAL WHERE {ok}" for i, n, e in kit)
        out.append(f"INSERT INTO inventory (char_id,nameid,amount,equip,identify) {sel};")
        out.append(f"INSERT INTO char_reg_num (char_id,`key`,`index`,value) SELECT {cid},'legion_first',0,{first} FROM DUAL WHERE {ok} "
                   f"UNION ALL SELECT {cid},'legion_branch',0,{branch} FROM DUAL WHERE {ok};")
        out.append(f"SELECT {cid} AS char_id, '{esc}' AS name, {ok} AS reset_done;")
        done.append(w['name'])
    return "\n".join(out) + "\n", done


def reset(arg):
    data = json.load(open(OUT, encoding='utf8'))
    wrong = [w for w in data['wrong'] if arg == '--all' or w['name'] in set(arg.split(','))]
    if not wrong:
        sys.exit('nobody to reset (run audit first; names must be in wrong_builds.json)')
    humans = sql('SELECT char_id,account_id,name,class,base_level FROM `char` WHERE account_id NOT BETWEEN 2100001 AND 2199999;', raw=True)
    text, names = reset_sql(wrong, None)
    res = sql(text, raw=True)
    if sql('SELECT char_id,account_id,name,class,base_level FROM `char` WHERE account_id NOT BETWEEN 2100001 AND 2199999;', raw=True) != humans:
        print('WARNING: human characters changed!')
    done = [l.split('\t')[1] for l in res.splitlines() if l.endswith('\t1') and not l.startswith('char_id')]
    skipped = [n for n in names if n not in done]
    print('reset', len(done), done, '\nskipped (account online / changed)', skipped)
    for n in done:
        body = json.dumps({'note': '[%s] started over: remade as a level 1 Novice (wrong build). Old goods moved to storage.' % n}).encode()
        try:
            urllib.request.urlopen(urllib.request.Request('http://127.0.0.1:8090/api/memory/' + urllib.parse.quote(n), data=body, method='POST'), timeout=5)
        except Exception as e:
            print('memory note failed', n, e)
    data['reset_log'] = data.get('reset_log', []) + [dict(t=datetime.datetime.now().isoformat(timespec='seconds'), name=n) for n in done]
    data['wrong'] = [w for w in data['wrong'] if w['name'] not in done]
    json.dump(data, open(OUT, 'w', encoding='utf8'), indent=1)


if __name__ == '__main__':
    if len(sys.argv) > 2 and sys.argv[1] == 'reset':
        reset(sys.argv[2])
    elif len(sys.argv) > 1 and sys.argv[1] == 'audit':
        audit()
    else:
        print(__doc__)
