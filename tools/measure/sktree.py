"""sktree.py (build-and-strategy agent run 4): check a builds.go skill plan against the server's Renewal skill tree.
Dump a job's skill tree (id, name, max, prereqs) or check a plan string from builds.go. a job's skill tree (id, name, max, prereqs) or check a plan string from builds.go.
usage: sktree.py dump Thief Assassin | sktree.py check <first job> <second job> "<sk(...) pairs>"
"""
import re, sys, os
D = os.path.join(os.path.dirname(os.path.abspath(__file__)), 'rd')
if not os.path.exists(os.path.join(D, 'skill_tree.yml')):  # first run: copy the tables out of the rathena container
    import subprocess
    os.makedirs(D, exist_ok=True)
    for f in ('skill_tree.yml', 'skill_db.yml'):
        out = subprocess.run(['wsl', '-d', 'Ubuntu-24.04', '--', 'docker', 'exec', 'ro-rathena', 'cat', '/rathena/db/re/' + f],
                             capture_output=True, env=dict(os.environ, MSYS_NO_PATHCONV='1')).stdout
        open(os.path.join(D, f), 'wb').write(out)

def load():
    ids, names, desc = {}, {}, {}
    cur = None
    for line in open(os.path.join(D, 'skill_db.yml'), encoding='utf8'):
        m = re.match(r'  - Id: (\d+)', line)
        if m:
            cur = int(m.group(1)); continue
        m = re.match(r'    Name: (\S+)', line)
        if m and cur is not None:
            ids[m.group(1)] = cur; names[cur] = m.group(1); continue
        m = re.match(r'    Description: (.*)', line)
        if m and cur is not None:
            desc[cur] = m.group(1).strip()
    trees = {}
    job = None; sk = None
    for line in open(os.path.join(D, 'skill_tree.yml'), encoding='utf8'):
        m = re.match(r'  - Job: (\S+)', line)
        if m:
            job = m.group(1); trees[job] = {}; continue
        m = re.match(r'      - Name: (\S+)', line)
        if m and job:
            sk = m.group(1); trees[job][sk] = {'max': 0, 'req': {}, 'job': 0}; continue
        m = re.match(r'        MaxLevel: (\d+)', line)
        if m and sk:
            trees[job][sk]['max'] = int(m.group(1)); continue
        m = re.match(r'        JobLevel: (\d+)', line)
        if m and sk:
            trees[job][sk]['job'] = int(m.group(1)); continue
        m = re.match(r'          - Name: (\S+)', line)
        if m and sk:
            rq = m.group(1); continue
        m = re.match(r'            Level: (\d+)', line)
        if m and sk:
            trees[job][sk]['req'][rq] = int(m.group(1))
    return ids, names, desc, trees

ids, names, desc, trees = load()

def dump(job):
    for n, s in trees[job].items():
        i = ids.get(n)
        req = ', '.join(f"{ids.get(r)}:{r} {l}" for r, l in s['req'].items())
        print(f"{i:5} {n:22} {desc.get(i,''):28} max {s['max']:2} job {s['job']:2} | {req}")

def check(first, second, plan):
    nums = [int(x) for x in re.findall(r'\d+', plan)]
    pairs = list(zip(nums[0::2], nums[1::2]))
    tree = dict(trees['Novice']); tree.update(trees[first]); tree.update(trees[second])
    have = {'NV_BASIC': 9}
    p1 = p2 = 0
    ok = True
    for i, lv in pairs:
        n = names.get(i)
        if n not in tree:
            print('NOT IN TREE', i, n); ok = False; continue
        s = tree[n]
        if lv > s['max']:
            print('OVER MAX', i, n, lv, s['max']); ok = False
        for r, rl in s['req'].items():
            if have.get(r, 0) < rl:
                print(f'PREREQ {i} {n} lv{lv} needs {r} {rl} (have {have.get(r,0)})'); ok = False
        add = max(0, min(lv, s['max']) - have.get(n, 0))
        if n in trees[first] or n in trees['Novice']:
            p1 += add
        else:
            p2 += add
        have[n] = max(have.get(n, 0), lv)
    print(f'1st-job points {p1} (49 avail incl. no basic), 2nd-job points {p2} (49)', 'OK' if ok else 'ERRORS')

if __name__ == '__main__':
    if sys.argv[1] == 'dump':
        for j in sys.argv[2:]:
            print('==', j); dump(j)
    else:
        check(sys.argv[2], sys.argv[3], sys.argv[4])
