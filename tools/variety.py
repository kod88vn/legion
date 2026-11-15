"""Build variety across the Legion (owner 2026-11-14: "the builds of the legions still lack variety"; CLASSES.md
"Build variety across the Legion").

  python tools/variety.py measure             # plans per class line (online / all), stat-spread and skill-tree sameness
  python tools/variety.py assign              # dry run: which characters move to which variant (writes the plan file)
  python tools/variety.py assign --apply      # edits legion/data/personas.json (backup first); live at the next legion restart

Assignment rules:
  - only Legion bot accounts (2100001-2199999), never Ayla's party (wrongbuilds.PARTY), never human characters;
  - only within the character's main plan family (same class, 1st job and branch: no DB change needed);
  - only when the character's current skills AND stats are valid for the variant (the tools/wrongbuilds.py rules, plus
    2nd-job skill levels within the variant's caps) - no character becomes a wrong build, nobody is deleted;
  - roles: support characters stay healers (priest / priest_me), vendors stay Merchant-line plans;
  - target spread per family: main plan 40%, the variants share the rest (2 plans: 50/50); the least-invested
    characters (lowest base level, offline) move first.
"""
import collections, datetime, json, os, re, shutil, sys, urllib.parse

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import wrongbuilds as wb  # noqa: E402  (plans parsed from builds.go, SQL helper, skill trees)

SRV = wb.SRV
sys.path.insert(0, os.path.join(SRV, 'tools', 'system'))
import common as _C  # the one data-folder resolver (rule G23)
DATA = _C.data_dir()
PERSONAS = os.path.join(DATA, 'personas.json')
OUT = os.path.join(DATA, 'variety.json')
HEALER_PLANS = {'priest', 'priest_me'}

# trans skill plans (transplans.go) count as caps for 2nd-job skills too
_tp = open(os.path.join(SRV, 'legion', 'transplans.go'), encoding='utf8').read()
TRANS = {}
for m in re.finditer(r'"(\w+)": sk\(([\d,\s]+)\)', _tp):
    kv = [int(x) for x in m.group(2).split(',')]
    TRANS[m.group(1)] = [(kv[i], kv[i + 1]) for i in range(0, len(kv) - 1, 2)]

FIRST_IDS = set(i for t in wb.TREE.values() for i in t)


def family_plans():
    fam = collections.defaultdict(list)
    for pid, p in wb.plans.items():
        fam[p['base']].append(pid)
    for k in fam:
        fam[k].sort(key=lambda x: (x != k, x))
    return fam


def caps(pid, role):
    p = wb.plans[pid]
    # a variant takes only the transcendent skills of its main plan's trans list (builds.go transFor), not the main
    # plan's 1st/2nd-job goals in it (Lord Knight's Two-Hand Sword Mastery 10 is no cap for a spear Knight)
    tr = TRANS.get(pid) or [(i, g) for i, g in TRANS.get(p['base'], []) if 355 <= i <= 407 or 475 <= i <= 490]
    lists = [p['skills'], p['legacy'], tr]
    if role == 'vendor':
        lists.append(wb.VENDOR_FIRST)
    c = collections.Counter()
    for l in lists:
        for i, g in l:
            c[i] = max(c[i], g)
    return c


def compatible(ch, pid, role):
    """the character's skills and stats are valid for plan pid (no irreversible deviation)."""
    p = wb.plans[pid]
    if role == 'support' and pid not in HEALER_PLANS:
        return False
    cls, sk, st = ch['cls'], ch['skills'], ch['stats']
    if cls in wb.TREE and cls != p['first']:
        return False
    cp = caps(pid, role)
    # first-job skills: same rule as wrongbuilds (points past the plan's 1st-job goals may go anywhere)
    if cls in wb.TREE or cls >= 7:
        tree = wb.TREE.get(p['first'], {})
        first_pts = sum(v for k, v in sk.items() if k in tree)
        plan1 = sum(v for k, v in cp.items() if k in tree)
        over = sum(v - cp[k] for k, v in sk.items() if k in tree and v > cp[k]) - max(0, first_pts - plan1)
        if over >= 2:
            return False
    # 2nd-job skills (and anything else): within the variant's caps
    over2 = sum(v - cp[k] for k, v in sk.items() if k not in FIRST_IDS and k > 1 and v > cp[k])
    if over2 >= 2:
        return False
    budget = wb.spent(st)
    horizon = wb.sim_stats(p, budget + max(budget, 40))
    if any(st[i] > horizon[i] + 1 for i in range(6)):
        if not p.get('legacy_stats'):
            return False
        lh = wb.sim_stats(dict(stats=p['legacy_stats']), budget + max(budget, 40))
        if any(st[i] > lh[i] + 1 for i in range(6)):
            return False
    return True


def load_chars(use_api=True):
    rows = wb.sql("SELECT c.char_id,c.name,c.class,c.base_level,c.job_level,c.str,c.agi,c.vit,c.`int`,c.dex,c.luk,c.online "
                  "FROM `char` c WHERE c.account_id BETWEEN 2100001 AND 2199999;")
    skills = collections.defaultdict(dict)
    for r in wb.sql('SELECT s.char_id,s.id,s.lv,s.flag FROM skill s JOIN `char` c ON c.char_id=s.char_id WHERE c.account_id BETWEEN 2100001 AND 2199999;'):
        if r['flag'] == '0':
            skills[int(r['char_id'])][int(r['id'])] = int(r['lv'])
    online = {b['name'] for b in (wb.get('http://127.0.0.1:8090/api/bots') or []) if b.get('online')} if use_api else set()
    out = {}
    inv = {v: k for k, v in wb.CORE.items()}
    for r in rows:
        cid = int(r['char_id'])
        ch = dict(name=r['name'], cls=int(r['class']), lv=int(r['base_level']), job=int(r['job_level']),
                  stats=[int(r[k]) for k in ('str', 'agi', 'vit', 'int', 'dex', 'luk')], skills=dict(skills.get(cid, {})),
                  online=r['name'] in online)
        if ch['online']:
            d = wb.get('http://127.0.0.1:8090/api/bots/' + urllib.parse.quote(r['name']))
            if d and d.get('online'):
                ch['skills'] = {int(k): v for k, v in (d.get('skills') or {}).items() if v > 0}
                m = re.search(r'stats str(\d+) agi(\d+) vit(\d+) int(\d+) dex(\d+) luk(\d+)', d.get('plan', ''))
                if m:
                    ch['stats'] = [int(x) for x in m.groups()]
                ch['lv'], ch['job'] = d.get('base_level', ch['lv']), d.get('job_level', ch['job'])
                if d.get('class') in inv:
                    ch['cls'] = inv[d['class']]
        out[cid] = ch
    return out


def signature(ch):
    st = ch['stats']
    top = sorted(range(6), key=lambda i: -st[i])[:3]
    stat_sig = '/'.join('SAVIDL'[i] for i in top)
    sk2 = tuple(sorted(k for k, v in ch['skills'].items() if k not in FIRST_IDS and k > 1 and v >= 3))
    return stat_sig, sk2


def measure(chars=None, personas=None, quiet=False):
    ps = personas or json.load(open(PERSONAS, encoding='utf8'))
    chars = chars if chars is not None else load_chars()
    fam = family_plans()
    res = {}
    for base, pids in fam.items():
        cnt_all, cnt_on = collections.Counter(), collections.Counter()
        sigs, sk2s = collections.Counter(), collections.Counter()
        n40 = 0
        for p in ps:
            for c in p['characters']:
                if wb.plans.get(c.get('plan'), {}).get('base') != base:
                    continue
                cnt_all[c['plan']] += 1
                ch = chars.get(c['char_id'])
                if ch and ch['online']:
                    cnt_on[c['plan']] += 1
                if ch and ch['lv'] >= 40:
                    n40 += 1
                    s, k = signature(ch)
                    sigs[s] += 1
                    sk2s[k] += 1
        tot = sum(cnt_all.values())
        res[base] = dict(total=tot, online=sum(cnt_on.values()),
                         plans={pid: dict(all=cnt_all[pid], online=cnt_on[pid], share=round(100 * cnt_all[pid] / max(tot, 1))) for pid in pids},
                         lv40=n40, top_stat_spread=(sigs.most_common(1)[0] if sigs else None),
                         same_stat_spread_pct=round(100 * sigs.most_common(1)[0][1] / n40) if n40 else 0,
                         same_skill_tree_pct=round(100 * sk2s.most_common(1)[0][1] / n40) if n40 else 0)
        if not quiet:
            pl = ', '.join('%s %d/%d (%d%%)' % (pid, v['online'], v['all'], v['share']) for pid, v in res[base]['plans'].items())
            print('%-11s all %4d online %3d | %s | lv40+ %d: same stat spread %d%%, same 2nd-job skill set %d%%' % (
                base, tot, res[base]['online'], pl, n40, res[base]['same_stat_spread_pct'], res[base]['same_skill_tree_pct']))
    return res


def targets(pids):
    if len(pids) == 1:
        return {pids[0]: 1.0}
    if len(pids) == 2:
        return {pids[0]: 0.5, pids[1]: 0.5}
    rest = 0.6 / (len(pids) - 1)
    return {pid: (0.4 if i == 0 else rest) for i, pid in enumerate(pids)}


def assign(apply=False):
    ps = json.load(open(PERSONAS, encoding='utf8'))
    chars = load_chars()
    print('-- before'); before = measure(chars, ps)
    fam = family_plans()
    moves = []
    party = {n.lower() for n in wb.PARTY}
    for base, pids in fam.items():
        members = [(p, c) for p in ps for c in p['characters'] if wb.plans.get(c.get('plan'), {}).get('base') == base]
        if len(pids) < 2 or not members:
            continue
        tgt = targets(pids)
        n = len(members)
        cnt = collections.Counter(c['plan'] for _, c in members)
        want = {pid: round(tgt[pid] * n) for pid in pids}
        # movable: least invested first (offline, low level)
        def inv(pc):
            ch = chars.get(pc[1]['char_id'])
            return (ch['online'] if ch else False, ch['lv'] if ch else 1)
        for p, c in sorted(members, key=inv):
            if c['name'].lower() in party or not (2100001 <= p['account_id'] <= 2199999):
                continue
            cur = c['plan']
            if cnt[cur] <= want.get(cur, 0):
                continue
            ch = chars.get(c['char_id']) or dict(cls=0, lv=1, job=1, stats=[1] * 6, skills={}, online=False)
            # the most under-filled compatible plan
            opts = sorted((pid for pid in pids if pid != cur and cnt[pid] < want[pid] and compatible(ch, pid, c['role'])),
                          key=lambda pid: cnt[pid] - want[pid])
            if not opts:
                continue
            new = opts[0]
            cnt[cur] -= 1
            cnt[new] += 1
            moves.append(dict(name=c['name'], account=p['account'], char_id=c['char_id'], role=c['role'], level=ch['lv'],
                              cls=wb.CORE.get(ch['cls'], str(ch['cls'])), online=ch['online'], old=cur, new=new))
            c['plan'] = new
    by = collections.Counter('%s -> %s' % (m['old'], m['new']) for m in moves)
    print('-- moves', len(moves), '(online now: %d, lv40+: %d)' % (sum(m['online'] for m in moves), sum(m['level'] >= 40 for m in moves)))
    for k, v in sorted(by.items()):
        print('  %4d %s' % (v, k))
    print('-- after'); after = measure(chars, ps)
    rep = dict(generated=datetime.datetime.now().isoformat(timespec='seconds'), applied=apply, before=before, after=after, moves=moves)
    json.dump(rep, open(OUT, 'w', encoding='utf8'), indent=1)
    if apply:
        bak = PERSONAS + '.bak-variety-' + datetime.datetime.now().strftime('%Y%m%d%H%M')
        shutil.copy2(PERSONAS, bak)
        raw = open(PERSONAS, encoding='utf8').read()
        indent = 1 if raw.startswith('[\n ') and not raw.startswith('[\n  ') else 2
        json.dump(ps, open(PERSONAS, 'w', encoding='utf8', newline='\n'), indent=indent, ensure_ascii=False)
        print('personas.json written (backup %s); takes effect at the next legion restart' % os.path.basename(bak))


def revert_wrong():
    """characters moved by assign that tools/wrongbuilds.py audit now flags (they spent points under the old plan
    before legion restarted with the new one): back to their old plan."""
    rep = json.load(open(OUT, encoding='utf8'))
    wrong = {w['char_id'] for w in json.load(open(wb.OUT, encoding='utf8'))['wrong']}
    back = {m['char_id']: m['old'] for m in rep['moves'] if m['char_id'] in wrong}
    if not back:
        print('nothing to revert')
        return
    ps = json.load(open(PERSONAS, encoding='utf8'))
    for p in ps:
        for c in p['characters']:
            if c['char_id'] in back:
                c['plan'] = back[c['char_id']]
    json.dump(ps, open(PERSONAS, 'w', encoding='utf8', newline=chr(10)), indent=2, ensure_ascii=False)
    rep['moves'] = [m for m in rep['moves'] if m['char_id'] not in back]
    rep.setdefault('reverted', []).extend(sorted(back))
    json.dump(rep, open(OUT, 'w', encoding='utf8'), indent=1)
    print('reverted', len(back), 'to their old plan (run tools/wrongbuilds.py audit first)')


if __name__ == '__main__':
    if len(sys.argv) > 1 and sys.argv[1] == 'revert-wrong':
        revert_wrong()
        sys.exit()
    if len(sys.argv) > 1 and sys.argv[1] == 'measure':
        measure()
    elif len(sys.argv) > 1 and sys.argv[1] == 'assign':
        assign('--apply' in sys.argv)
    else:
        print(__doc__)
