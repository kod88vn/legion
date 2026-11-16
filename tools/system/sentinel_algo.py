#!/usr/bin/env python3
"""sentinel_algo: the rule-based sentinel (owner order 2026-11-15). Pure rules: NO LLM call anywhere in this loop.

Run:  python tools/sentinel.py --algo           (loop, one cycle every 60 s)
      python tools/sentinel.py --algo --once    (one cycle; prints the row)

Every cycle:
  1. Checks (all recorded in legion/data/sentinel_report.jsonl):
     - engine containers ro-legion, ro-legion-core, ro-openkore-ayla: running, not restarting, no restart loop
       (restart count up by 3 or more within 30 min)
     - the engine API answers (GET /api/overview)
     - the dispatcher heartbeat (dispatch.heartbeat) younger than 10 min
     - puppet role heartbeats (puppet_heartbeat.json): a role silent for longer than its interval + 1 h is stalled.
       Roles without a schedule (chat) are skipped; a daily role (build) has a 25 h window, so it is not stalled
       inside its day. Only judged while the dispatcher state is "ok" (not paused, not api-down).
     - loop heartbeats younger than 10 min: partycheck, aylalog, bugq-watch, unwall
     - Ayla's log (openkore-ayla/logs/console_player_2.txt) not silent for more than 10 min while her party is online
  2. Actions, bounded (only while legion/data/stack.up exists and stack.hold does not; otherwise observe only):
     - restart a dead loop (dispatcher, partycheck, aylalog, bugq-watch, unwall): at most 3 restarts an hour per
       loop, and at least 5 min x (restarts in the last hour) between two restarts of the same loop (backoff).
       unwall is restarted only when its heartbeat file exists and is stale (old code writes none).
     - docker start a stopped ro-legion or ro-legion-core: at most once an hour per container. Never recreate.
  3. Whatever no rule fixes goes to the bug queue: legionctl.py bug --from sentinel --priority high. The same key is
     filed at most once every 30 min.

Hard limits (code-enforced): containers touched are only ro-legion, ro-legion-core (docker start) and the three
engine containers (inspect). Never rathena, ro-db, Ayla's OpenKore container (G05, G19: Ayla is restarted only by an
owner order), never docker recreate/rm/stop/kill (G15); only the five loop scripts are spawned.

Files it writes: legion/data/sentinel_report.jsonl (one row per cycle: checks, actions; this is also the history the
limits read), legion/data/sentinel_status.txt (one line), legion/data/sentinel.heartbeat. Nothing else in legion/data.
"""
import json
import os
import subprocess
import sys
import time
import urllib.request

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import common as C  # noqa: E402

API = 'http://127.0.0.1:8090'
PERIOD_S = 60
HB_MAX_AGE_S = 600             # loop and dispatcher heartbeats: fresher than 10 min
ROLE_SLACK_S = 3600            # a puppet role is stalled after its interval + 1 h
AYLA_SILENT_S = 600            # Ayla's log silent this long while her party is online = a problem
LOOP_RESTARTS_PER_HOUR = 3
LOOP_BACKOFF_S = 300           # gap before the next restart of a loop = this x restarts in the last hour
CONTAINER_STARTS_PER_HOUR = 1
RESTART_LOOP_MAX = 3           # restart count up by this much inside LOOP_WINDOW = a restart loop (not fixed here)
LOOP_WINDOW = 1800
FILE_AGAIN_S = 1800            # the same bug-queue key at most once per 30 min
HISTORY_S = 26 * 3600          # how far back the report is read (role first-seen, restart counts)
HISTORY_READ_BYTES = 4 * 1024 * 1024
REPORT_ROTATE_BYTES = 10 * 1024 * 1024
REPORT = 'sentinel_report.jsonl'
STATUS = 'sentinel_status.txt'
HEARTBEAT = 'sentinel.heartbeat'
PY = sys.executable

ENGINE = ['ro-legion', 'ro-legion-core', 'ro-openkore-ayla']
STARTABLE = ('ro-legion', 'ro-legion-core')   # the only containers docker start may touch
AYLA_CONTAINER = 'ro-openkore-ayla'
AYLA_NAME = 'Ayla'
# loop name -> (heartbeat files (mtime is the heartbeat), command relative to the repo root)
LOOPS = {
    'dispatcher': (['dispatch.heartbeat'], ['tools/puppet/dispatch.py']),
    'partycheck': (['partycheck.heartbeat'], ['tools/system/partycheck.py']),
    'aylalog': (['aylalog.heartbeat'], ['tools/system/aylalog.py']),
    'bugq_watch': (['bugq-watch.heartbeat'], ['tools/system/bugq.py', 'watch']),
    'unwall': (['unwall.heartbeat'], ['tools/unwall.py', '--loop']),
}
HB_REQUIRED = {'unwall'}   # restarted only once its heartbeat file exists (an old loop without one may still run)


# ----------------------------------------------------------------------------- the outside world (injectable)
def real_docker(args):
    try:
        r = subprocess.run(['docker'] + list(args), capture_output=True, text=True, timeout=30)
        return r.returncode, (r.stdout or '').strip()
    except Exception:
        return 1, ''


def real_api(path):
    with urllib.request.urlopen(API + path, timeout=8) as r:
        return json.load(r)


def real_spawn(cmd, cwd):
    kw = {}
    if os.name == 'nt':
        kw['creationflags'] = 0x00000008 | 0x00000200   # DETACHED_PROCESS | CREATE_NEW_PROCESS_GROUP
    p = subprocess.Popen(cmd, cwd=cwd, stdin=subprocess.DEVNULL, stdout=subprocess.DEVNULL,
                         stderr=subprocess.DEVNULL, close_fds=True, **kw)
    return p.pid


def real_bug(root, text, evidence):
    cmd = [PY, os.path.join(root, 'tools', 'legionctl.py'), 'bug', text, '--from', 'sentinel', '--priority', 'high',
           '--evidence', evidence[:600]]
    try:
        r = subprocess.run(cmd, cwd=root, capture_output=True, text=True, timeout=60)
        return r.returncode == 0, ((r.stdout or '') + (r.stderr or '')).strip()[-300:]
    except Exception as e:
        return False, str(e)[:300]


def real_role_intervals():
    """Role -> interval seconds, from the dispatcher's own role table (roles with a schedule only)."""
    d = os.path.join(C.ROOT, 'tools', 'puppet')
    if d not in sys.path:
        sys.path.insert(0, d)
    import dispatch   # the role table is module-level constants; importing runs nothing
    return {r: cfg['interval'] for r, cfg in dispatch.ROLES.items() if cfg.get('interval')}


class Env:
    """Everything a cycle touches from outside. Tests pass fakes; the defaults are the live functions."""

    def __init__(self, data=None, root=None, docker=None, api=None, spawn=None, bug=None, role_intervals=None,
                 ayla_log=None):
        self.data = data or C.data_dir()
        self.root = root or C.ROOT
        self._docker = docker or real_docker
        self._api = api or real_api
        self._spawn = spawn or real_spawn
        self._bug = bug or (lambda text, ev: real_bug(self.root, text, ev))
        self._role_intervals = role_intervals
        self.ayla_log = ayla_log or os.environ.get('AYLA_LOG') or os.path.join(
            self.root, 'openkore-ayla', 'logs', 'console_player_2.txt')

    def path(self, name):
        return os.path.join(self.data, name)

    def docker(self, args):
        # guard: only inspect, and docker start of a legion container. Nothing else reaches docker from here.
        args = list(args)
        if args[0] == 'inspect':
            return self._docker(args)
        if args[0] == 'start' and len(args) == 2 and args[1] in STARTABLE:
            return self._docker(args)
        raise PermissionError('sentinel_algo may not run docker %s' % ' '.join(args))

    def api(self, path):
        return self._api(path)

    def spawn(self, name):
        # guard: only the five loop scripts, by their table name
        if name not in LOOPS:
            raise PermissionError('sentinel_algo restarts only the loops in LOOPS, not %r' % name)
        return self._spawn([PY] + LOOPS[name][1], self.root)

    def bug(self, text, evidence):
        return self._bug(text, evidence)

    def role_intervals(self):
        if self._role_intervals is not None:
            return self._role_intervals
        return real_role_intervals()


# ----------------------------------------------------------------------------- history (the report is the state)
def load_history(env, since):
    rows = []
    path = env.path(REPORT)
    try:
        size = os.path.getsize(path)
        with open(path, 'rb') as f:
            if size > HISTORY_READ_BYTES:
                f.seek(size - HISTORY_READ_BYTES)
                f.readline()
            for line in f:
                try:
                    r = json.loads(line.decode('utf-8', 'replace'))
                except ValueError:
                    continue
                if r.get('t', 0) >= since:
                    rows.append(r)
    except OSError:
        pass
    return rows


def actions_of(hist, kind, target, since):
    return sorted(r['t'] for r in hist if r['t'] >= since for a in r.get('actions', [])
                  if a.get('type') == kind and a.get('target') == target)


def may_restart(hist, name, now):
    """(allowed, kind): kind is 'limit' (filed to the bug queue), 'backoff' (waits silently) or '' when allowed."""
    ts = actions_of(hist, 'restart', name, now - 3600)
    if len(ts) >= LOOP_RESTARTS_PER_HOUR:
        return False, 'limit'
    if ts and now - ts[-1] < LOOP_BACKOFF_S * len(ts):
        return False, 'backoff'
    return True, ''


def may_start(hist, name, now):
    ts = actions_of(hist, 'docker_start', name, now - 3600)
    if len(ts) >= CONTAINER_STARTS_PER_HOUR:
        return False, 'already started %d s ago (once an hour)' % (now - ts[-1])
    return True, ''


def filed_recently(hist, key, now):
    return any(r['t'] >= now - FILE_AGAIN_S for r in hist for a in r.get('actions', [])
               if a.get('type') == 'bug' and a.get('key') == key)


# ----------------------------------------------------------------------------- helpers
def stack_up(env):
    return os.path.exists(env.path('stack.up')) and not os.path.exists(env.path('stack.hold'))


def read_json(path):
    try:
        with open(path, encoding='utf-8') as f:
            return json.load(f)
    except (OSError, ValueError):
        return None


def hb_age(env, names, now):
    """Seconds since the freshest of the named heartbeat files was written, or None when none exists."""
    best = None
    for n in names:
        try:
            a = now - os.path.getmtime(env.path(n))
        except OSError:
            continue
        best = a if best is None else min(best, a)
    return best


def inspect(env, name):
    rc, out = env.docker(['inspect', '--format', '{{.State.Status}}|{{.State.Restarting}}|{{.RestartCount}}', name])
    if rc != 0:
        return 'missing', False, None
    try:
        st, rs, cnt = out.strip().split('|')
        return st, rs == 'true', int(cnt)
    except ValueError:
        return 'unknown', False, None


def min_restarts(hist, name, since):
    vals = [a['restarts'] for r in hist if r['t'] >= since for c in r.get('checks', [])
            if c.get('name') == 'container:' + name and c.get('restarts') is not None for a in [c]]
    return min(vals) if vals else None


def ayla_party_online(env):
    """True/False from /api/parties/detail (Ayla's party, her online flag); None when the API does not say."""
    try:
        parties = env.api('/api/parties/detail')
    except Exception:
        return None
    ap = next((x for x in parties if x.get('human') == AYLA_NAME), None)
    return None if ap is None else bool(ap.get('online'))


def first_seen_pid(hist, pid, now):
    ts = [r['t'] for r in hist for c in r.get('checks', []) if c.get('name') == 'loop:dispatcher' and c.get('pid') == pid]
    return min(ts) if ts else now


# ----------------------------------------------------------------------------- one cycle
def run_cycle(env, now=None):
    now = C.now() if now is None else now
    hist = load_history(env, now - HISTORY_S)
    cur = {'t': now, 'iso': time.strftime('%Y-%m-%dT%H:%M:%S', time.localtime(now)), 'checks': [], 'actions': []}
    hist.append(cur)
    problems = []                       # (key, text, evidence) for the bug queue
    up = stack_up(env)
    cur['stack_up'] = up

    def check(name, ok, detail, **extra):
        row = dict(name=name, ok=bool(ok), detail=detail)
        row.update(extra)
        cur['checks'].append(row)
        return row

    def problem(key, text, evidence=''):
        problems.append((key, text, evidence or text))

    # --- engine containers
    for name in ENGINE:
        status, restarting, count = inspect(env, name)
        base = min_restarts(hist[:-1], name, now - LOOP_WINDOW)
        loop = count is not None and base is not None and count - base >= RESTART_LOOP_MAX
        ok = status == 'running' and not restarting and not loop
        detail = '%s%s, restarts %s%s' % (status, ' (restarting)' if restarting else '',
                                         'unknown' if count is None else count,
                                         ' RESTART LOOP (+%d in 30 min)' % (count - base) if loop else '')
        check('container:' + name, ok, detail, status=status, restarts=count)
        if ok or not up:
            continue
        if name in STARTABLE and status in ('exited', 'created') and not loop:
            allowed, why = may_start(hist, name, now)
            if allowed:
                try:
                    rc, out = env.docker(['start', name])
                    act = dict(type='docker_start', target=name, ok=rc == 0, detail=out[:200])
                except Exception as e:
                    act = dict(type='docker_start', target=name, ok=False, detail=str(e)[:200])
                cur['actions'].append(act)
                if act['ok']:
                    continue
                problem('container:' + name, '%s is stopped and docker start failed: %s' % (name, act['detail']))
            else:
                problem('container:' + name, '%s is stopped; %s' % (name, why))
        elif name == AYLA_CONTAINER:
            problem('container:' + name, 'Ayla\'s OpenKore container %s is %s; the sentinel never restarts her '
                    '(G05, G19): owner order needed' % (name, status))
        else:
            problem('container:' + name, '%s is %s%s; not fixable by the rules' % (
                name, status, ', restart loop' if loop else ''))

    # --- engine API
    try:
        env.api('/api/overview')
        api_ok, api_detail = True, 'answered'
    except Exception as e:
        api_ok, api_detail = False, 'no answer: %s' % str(e)[:120]
    check('engine_api', api_ok, api_detail)
    if not api_ok:
        problem('engine_api', 'Legion API (%s) does not answer' % API, api_detail)

    # --- loop heartbeats (dispatcher included) and restarts
    pup = read_json(env.path('puppet_heartbeat.json'))
    dispatcher_ok = False
    for name, (hbs, _cmd) in LOOPS.items():
        age = hb_age(env, hbs, now)
        ok = age is not None and age <= HB_MAX_AGE_S
        detail = 'no heartbeat file' if age is None else 'heartbeat %d s old' % age
        extra = {}
        if name == 'dispatcher':
            dispatcher_ok = ok
            if pup:
                extra = {'pid': pup.get('pid'), 'state': pup.get('state')}
            if not ok and up:
                detail += ' (dispatcher state %s)' % (pup or {}).get('state')
        check('loop:' + name, ok, detail, **extra)
        if ok or not up:
            continue
        if age is None and name in HB_REQUIRED:
            problem('loop:' + name, '%s has no heartbeat file, so it is not restarted (its loop must write one)' % name)
            continue
        allowed, kind = may_restart(hist[:-1], name, now)
        if not allowed:
            if kind == 'limit':
                problem('loop:' + name, '%s is dead (%s) and the restart limit is reached: %d restarts in the last hour '
                        'and it is still dead; a fixer is needed' % (name, detail, LOOP_RESTARTS_PER_HOUR))
            continue   # backoff: wait silently for the next cycle
        try:
            act = dict(type='restart', target=name, ok=True, pid=env.spawn(name), detail='spawned')
        except Exception as e:
            act = dict(type='restart', target=name, ok=False, detail=str(e)[:200])
        cur['actions'].append(act)
        if not act['ok']:
            problem('loop:' + name, '%s is dead and the restart failed: %s' % (name, act['detail']))

    # --- puppet roles
    if not dispatcher_ok:
        check('puppet:roles', True, 'skipped: dispatcher heartbeat stale')
    elif not pup:
        check('puppet:roles', False, 'puppet_heartbeat.json missing')
        problem('puppet:roles', 'puppet_heartbeat.json is missing while the dispatcher heartbeat is fresh')
    elif pup.get('state') != 'ok':
        check('puppet:roles', True, 'skipped: dispatcher state %s' % pup.get('state'))
    else:
        try:
            intervals = env.role_intervals()
        except Exception as e:
            intervals = None
            check('puppet:roles', False, 'role table not importable: %s' % str(e)[:150])
            problem('puppet:roles', 'the dispatcher role table cannot be read: %s' % str(e)[:150])
        if intervals is not None:
            first = first_seen_pid(hist[:-1], pup.get('pid'), now)
            roles = pup.get('roles') or {}
            stalled = []
            for r, iv in sorted(intervals.items()):
                last = (roles.get(r) or {}).get('last')
                base = last if last else first
                silent = now - base
                if silent > iv + ROLE_SLACK_S:
                    stalled.append('%s silent %d min (interval %d min)' % (r, silent // 60, iv // 60))
            check('puppet:roles', not stalled, '; '.join(stalled) if stalled else 'all %d scheduled roles on time'
                  % len(intervals))
            if stalled:
                problem('puppet:roles', 'puppet roles stalled: ' + '; '.join(stalled))

    # --- Ayla's log while her party is online
    online = ayla_party_online(env)
    try:
        silent = now - os.path.getmtime(env.ayla_log)
    except OSError:
        silent = None
    if online:
        ok = silent is not None and silent <= AYLA_SILENT_S
        detail = 'log file missing' if silent is None else 'log silent %d s (party online)' % silent
        check('ayla_log', ok, detail)
        if not ok:
            problem('ayla_log', 'Ayla\'s log is silent for %s while her party is online' % (
                'an unknown time (no log file)' if silent is None else '%d min' % (silent // 60)))
    else:
        check('ayla_log', True, 'skipped: party %s' % ('offline' if online is False else 'unknown'))

    # --- file what no rule fixed (observe only while the stack is not up)
    cur['problems'] = [k for k, _, _ in problems]
    if up:
        for key, text, ev in problems:
            if filed_recently(hist[:-1], key, now):
                continue
            ok, out = env.bug('[sentinel:%s] %s' % (key, text), ev)
            cur['actions'].append(dict(type='bug', key=key, ok=bool(ok), detail=out[:200]))

    write_report(env, cur)
    write_status(env, cur)
    try:
        with open(env.path(HEARTBEAT), 'w') as f:
            f.write(str(int(now)))
    except OSError:
        pass
    return cur


def write_report(env, row):
    path = env.path(REPORT)
    try:
        os.makedirs(env.data, exist_ok=True)
        if os.path.exists(path) and os.path.getsize(path) > REPORT_ROTATE_BYTES:
            os.replace(path, path + '.1')
        with open(path, 'a', encoding='utf-8') as f:
            f.write(json.dumps(row) + '\n')
    except OSError:
        pass


def status_text(row):
    bad = [c['name'] for c in row['checks'] if not c['ok']]
    if not row['stack_up']:
        head = 'STANDBY (stack not up; observe only)'
    else:
        head = 'OK' if not bad else 'PROBLEM'
    s = '%s %s | %d/%d checks ok' % (row['iso'], head, len(row['checks']) - len(bad), len(row['checks']))
    if bad:
        s += ' | bad: ' + ', '.join(bad)
    acts = ['%s %s%s' % (a['type'], a.get('target', a.get('key', '')), '' if a.get('ok') else ' FAILED')
            for a in row['actions']]
    return s + ' | actions: ' + ('; '.join(acts) if acts else 'none')


def write_status(env, row):
    try:
        os.makedirs(env.data, exist_ok=True)
        with open(env.path(STATUS), 'w', encoding='utf-8') as f:
            f.write(status_text(row) + '\n')
    except OSError:
        pass


def main(argv=None):
    argv = sys.argv if argv is None else argv
    env = Env()
    once = '--once' in argv
    while True:
        t0 = time.time()
        try:
            row = run_cycle(env)
        except Exception as e:   # a broken check must not stop the loop; the error is still in the status file
            row = {'t': time.time(), 'iso': time.strftime('%Y-%m-%dT%H:%M:%S'), 'checks': [], 'actions': [],
                   'stack_up': stack_up(env), 'error': str(e)[:300]}
            write_status(env, dict(row, checks=[dict(name='cycle', ok=False, detail=str(e)[:200])]))
            write_report(env, row)
        if once:
            print(json.dumps(row))
            return 0
        time.sleep(max(1.0, PERIOD_S - (time.time() - t0)))


if __name__ == '__main__':
    sys.exit(main())
