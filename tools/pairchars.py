"""pairchars.py - owner rule 2026-11-14 (RULES.md 2 "Merchant + DS Hunter pairs on one account"):
"every merchant account should have a DS hunter. Every hunter account should have a merchant".

For every Legion bot account (2100001-2199999) that has a Merchant-line character (plan blacksmith* / alchemist*) but
no Hunter-plan character, a level-1 Novice with the DS Hunter plan ("hunter") is created in a free slot; for every
account with a Hunter but no Merchant-line character, a level-1 Novice vendor ("blacksmith" plan: Enlarge Weight ->
Pushcart -> Vending first) is created. Real progression: Novice 1, all stats 1, 48 points, the official starter kit,
legion_first / legion_branch for legion.txt - the same rows as persona.go seedSQL.

Never touches human accounts (outside the bot range) or the accounts of Ayla's party (data/human-parties.json).
Takes effect at the next legion restart (the engine loads personas.json at start; runtime writers reload the file).

  python tools/pairchars.py            # dry run: counts
  python tools/pairchars.py --apply    # backup + personas.json + DB rows
"""
import json, os, random, shutil, subprocess, sys, time

SRV = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
sys.path.insert(0, os.path.join(SRV, 'tools', 'system'))
import common as _C  # the one data-folder resolver (rule G23)
DATA = _C.data_dir()
PERSONAS = os.path.join(DATA, 'personas.json')
HUMAN_PARTIES = os.path.join(DATA, 'human-parties.json')
TOWNS = {  # town -> spawn cell (same as the seed's townSpawn for the beginner towns)
    'prontera': (156, 191), 'izlude': (128, 114), 'payon': (162, 233), 'geffen': (119, 59), 'alberta': (116, 57),
}
MERCH = ('blacksmith', 'alchemist')
import charrows as CR  # the one seed-row builder (shared with addchars.py; same rows as persona.go seedSQL)
HUNTER_PLAN = ('hunter',) + CR.plan_first_branch('hunter')      # plan, legion_first, legion_branch, target class
VENDOR_PLAN = ('blacksmith',) + CR.plan_first_branch('blacksmith')


def db(sql):
    """run SQL in ro-db (stdin)"""
    p = subprocess.run(['wsl', '-d', 'Ubuntu-24.04', '--', 'docker', 'exec', '-i', 'ro-db', 'mariadb', '-N',
                        '-uragnarok', '-pragnarok', 'ragnarok'], input=sql.encode(), capture_output=True)
    if p.returncode != 0:
        raise SystemExit('db error: ' + p.stderr.decode()[:500])
    return p.stdout.decode()


def main():
    apply = '--apply' in sys.argv
    data = json.load(open(PERSONAS, encoding='utf-8'))
    ps = data if isinstance(data, list) else data.get('personas', data)
    pinned = set()
    try:
        for hp in json.load(open(HUMAN_PARTIES, encoding='utf-8')):
            pinned.update(m.lower() for m in hp.get('members', []))
    except Exception:
        pass
    taken = set(x.strip().lower() for x in db('SELECT name FROM `char`;').splitlines())
    used_ids = set(int(x) for x in db('SELECT char_id FROM `char`;').split())
    online = set(int(x) for x in db('SELECT account_id FROM `char` WHERE online=1;').split())
    rng = random.Random(20261114)
    adds = []  # (persona, charspec)
    skipped = {'pinned': 0, 'no free slot': 0, 'outside bot range': 0}
    for p in ps:
        aid = p.get('account_id', 0)
        if not (2100001 <= aid <= 2199999):
            skipped['outside bot range'] += 1
            continue
        chars = p.get('characters', [])
        if any(c['name'].lower() in pinned for c in chars):
            skipped['pinned'] += 1
            continue
        plans = [str(c.get('plan', '')) for c in chars]
        has_m = any(x.startswith(MERCH) for x in plans)
        has_h = any(x.startswith('hunter') for x in plans)
        if has_m == has_h:
            continue
        slots = set(c['slot'] for c in chars)
        free = [s for s in range(0, 9) if s not in slots]
        if not free:
            skipped['no free slot'] += 1
            continue
        idx = aid - 2100000
        if not has_h:
            plan, first, branch, target = HUNTER_PLAN
            role, suffixes, base_id = 'main', ['Bow', 'Shot', ' Arrow', 'Hawk', ' Hunt', 'Archer'], 210000
        else:
            plan, first, branch, target = VENDOR_PLAN
            role, suffixes, base_id = 'vendor', [' Shop', ' Mart', ' Trade', ' Deals', 'Vend', ' Goods'], 180000
        cid = base_id + idx
        if cid in used_ids:
            cid = 220000 + idx
        if cid in used_ids:
            continue
        stem = chars[0]['name'] if chars else p.get('name', 'Player')
        for s in [stem.replace(' Shop', '').replace(' Mart', '')[:24 - len(x)] + x for x in suffixes]:
            if s.lower() not in taken:
                name = s
                break
        else:
            continue
        taken.add(name.lower())
        used_ids.add(cid)
        c = {'char_id': cid, 'slot': free[0], 'name': name, 'role': role, 'class': 0, 'class_name': 'Novice',
             'build': chars[0].get('build', 'balanced') if chars else 'balanced', 'base_level': 1, 'job_level': 1,
             'plan': plan, 'target_class': target}
        adds.append((p, c, first, branch))
    nh = sum(1 for a in adds if a[1]['plan'] == 'hunter')
    print('to create: %d Hunters, %d merchants (skipped %s); accounts online now: %d of them' % (
        nh, len(adds) - nh, skipped, sum(1 for a in adds if a[0]['account_id'] in online)))
    if not apply:
        for p, c, _, _ in adds[:8]:
            print('  ', p['account'], c['char_id'], c['slot'], c['name'], c['plan'])
        return
    bak = PERSONAS + '.bak-pairchars-' + time.strftime('%Y%m%d-%H%M%S')
    shutil.copy(PERSONAS, bak)
    sql = []
    for p, c, first, branch in adds:
        town = p.get('home_town') or rng.choice(list(TOWNS))
        if town not in TOWNS:
            town = 'prontera'
        sql += CR.seed_rows(c['char_id'], p['account_id'], c['slot'], c['name'], town, p.get('sex', 'M'), rng,
                            first, branch)
        p.setdefault('characters', []).append(c)
    for k in range(0, len(sql), 300):
        db('\n'.join(sql[k:k + 300]) + '\n')
    json.dump(data, open(PERSONAS, 'w', encoding='utf-8'), ensure_ascii=False, indent=2)
    print('created %d characters; personas.json written (backup %s); live at the next legion restart' % (
        len(adds), os.path.basename(bak)))


if __name__ == '__main__':
    main()
