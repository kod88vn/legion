#!/usr/bin/env python3
"""Sentinel: the self-check half of the improvement loop (owner: "I do not want to sit and baby sit you. I am trying
to create a self learning/improving loop").

Runs in WSL as a long-running background command of the engineering session. Every minute it measures the whole
system; every 10 minutes it also samples random bots for real progress. Each cycle appends one line to
G:\\RO\\legion\\legion\\data\\health.log. When a problem has lasted past its grace time it is FILED into the bug queue
(legion/data/bugqueue.jsonl through tools/system/bugq.py, deduped: a repeat only bumps the count). Only an OUTAGE
(OUTAGE_KEYS: API down, population collapsed, Ayla offline) also prints a report and EXITS, which wakes the main
session; everything else waits in the queue for the next planning pass. Each cycle writes data/sentinel.heartbeat.

Checks (grace): Legion API / population (4 min after a deploy), kills per bot, frozen-bot share (probe),
puppet-dispatcher heartbeat (only while legion/data/stack.up exists), meter checks from /api/meter (farming share of the
time budget, stuck share, deaths per bot-hour, API latency), Ayla online, Ayla's party together, watch client + its launcher running, client restart
loops, chat volume, death rate; farming time: farming share of bot states (>= 85%), town trips per bot (<= 1 per
45 min) and unnecessary town trips (bag < 70% weight and slots with potions in stock) - main legion + legion-core.
Usage: python3 /mnt/g/RO/legion/tools/sentinel.py [--once] [--mute KEY[,KEY...]] [--mute-until HH:MM]
  --mute: problem keys (e.g. bad_trips,deaths,party) already handed to a fixer. They are still measured and logged
  (health.log lists them under "muted") but do not cause an exit until the mute expires.
  --mute-until: local time the mute ends (default: 60 min after start).
"""
import collections, json, os, random, re, subprocess, sys, time, urllib.request

API = 'http://127.0.0.1:8090'
DATA = os.environ.get('LEGION_DATA') or os.path.join(ROOT, 'legion', 'data')
LOG = os.path.join(DATA, 'health.log')
ROOT = os.path.abspath(os.path.join(os.path.dirname(os.path.abspath(__file__)), '..'))
sys.path.insert(0, os.path.join(ROOT, 'tools', 'system'))
try:
    import bugq as BUGQ, common as SYSC, meterlib as METER
except Exception:          # the queue/meter helpers are optional: the sentinel still works without them
    BUGQ = SYSC = METER = None
OUTAGE_KEYS = {'legion_api', 'population', 'ayla'}   # these wake the main session; the rest goes to the queue
METRIC_OF = {'kills': 'kpm', 'farming': 'kpm', 'trips': 'kpm', 'bad_trips': 'kpm', 'frozen': 'kpm', 'stuck': 'kpm',
             'deaths': 'deaths', 'meter_deaths': 'deaths'}
PRIO_OF = {'kills': 'high', 'farming': 'high', 'deaths': 'high', 'meter_deaths': 'high', 'frozen': 'high',
           'dispatcher': 'high', 'stuck': 'high'}
DISPATCH_FILES = ['dispatch.heartbeat', 'dispatcher.heartbeat', 'puppet.heartbeat']
ONCE = '--once' in sys.argv


def _arg(name):
    for i, a in enumerate(sys.argv):
        if a == name and i + 1 < len(sys.argv):
            return sys.argv[i + 1]
        if a.startswith(name + '='):
            return a.split('=', 1)[1]
    return None


MUTE = set(k.strip() for k in (_arg('--mute') or '').split(',') if k.strip())
MUTE_UNTIL = time.time() + 3600
if _arg('--mute-until'):
    _h, _m = map(int, _arg('--mute-until').split(':'))
    _lt = time.localtime()
    MUTE_UNTIL = time.mktime((_lt.tm_year, _lt.tm_mon, _lt.tm_mday, _h, _m, 0, 0, 0, -1))
    if MUTE_UNTIL <= time.time():
        MUTE_UNTIL += 86400   # time already passed today: means tomorrow


def get(path, timeout=8):
    with urllib.request.urlopen(API + path, timeout=timeout) as r:
        return json.load(r)


def sh(cmd):
    try:
        return subprocess.run(cmd, shell=True, capture_output=True, text=True, timeout=20).stdout.strip()
    except Exception:
        return ''


class Problems:
    """A problem must persist for `grace` seconds before it is reported."""

    def __init__(self):
        self.since = {}

    def check(self, key, bad, grace, detail):
        now = time.time()
        if not bad:
            self.since.pop(key, None)
            return None
        t = self.since.setdefault(key, now)
        return (key, detail) if now - t >= grace else None


def probe(n=24, secs=90):
    """Sample n random online non-vendor bots twice; return (frozen share, examples)."""
    bots = [b for b in get('/api/bots') if b.get('online') and b.get('role') != 'vendor'
            and not b['state'].startswith(('travel', 'town trip', 'going to the Kafra', 'afk', 'saving'))]
    random.shuffle(bots)
    pick = bots[:n]
    first = {}
    for b in pick:
        try:
            first[b['name']] = get('/api/bots/' + urllib.request.quote(b['name']))
        except Exception:
            pass
    time.sleep(secs)
    frozen = []
    for name, a in first.items():
        try:
            z = get('/api/bots/' + urllib.request.quote(name))
        except Exception:
            continue
        if not z.get('online'):
            continue
        if z.get('kills') == a.get('kills') and z.get('x') == a.get('x') and z.get('y') == a.get('y') and z.get('map') == a.get('map'):
            frozen.append('%s(%s lv%s %s: %s)' % (name, z.get('class'), z.get('base_level'), z.get('map'), z.get('state')))
    return (len(frozen) / max(1, len(first))), frozen


# ---------------------------------------------------------------- farming time (owner: "they make town trips way too
# often, seemingly for no good reason. 90% of the time should be on farming"; given many times - so it is checked here)
FARM_MIN = 0.85          # farming share of sampled bot states
TRIP_WINDOW = 45 * 60    # at most one town trip per bot in this window
FARM_SAMPLE_EVERY = 300  # seconds between state samples; the share is over the last 3 samples
CORE = os.environ.get('LEGION_CORE', 'Zendancer84,Echobunny,Zen.fox,xXRinfangXx,Silent rogue,vex_x18').split(',')
TOWNS = set('prontera geffen payon morocc alberta aldebaran izlude yuno comodo umbala einbroch lighthalzen rachel veins '
            'hugel niflheim louyang amatsu gonryun ayothaya moscovia brasilis dewata lutie xmas jawaii'.split())
NOT_FARM = ('travel', 'town trip', 'roaming', 'rally', 'going to the kafra', 'heading', 'saving', 'afk', 'idle',
            'hanging out', 'no route', 'vend', 'shop', 'market', 'buying', 'selling', 'offline', 'stuck')
TRIP_RE = re.compile(r'heading to (\S+) \((.*)\) \[weight (\d+)%, slots (\d+)/100, pots (\d+), sp (\d+), zeny (-?\d+)\]')
# trips that are not "town trips from farming": logging off, or selling while already standing in a town
TRIP_EXEMPT = ('calling it a day', 'selling in town')
# the caller of a party town run (ai.go); members joining it ("town run with the party", "... party town run") are
# judged through this event, not their own trip line (owner: "town runs are done together")
CALL_RE = re.compile(r'party town run called: (.*) \[weight (\d+)%, slots (\d+)/100, pots (\d+), sp (\d+), zeny (-?\d+)\]')
REAL_NEED = ('necessary', 'weapon', 'arrow', 'bag is full', 'out of potions',
             # build/class errands (owner 2026-11-14: a member may leave briefly for a necessary build/class step):
             # Peco/falcon/cart rental, job change, class weapon/instrument/whip, Alchemist guide, quest skills
             'peco', 'falcon', 'cart', 'job change', 'instrument', 'whip', 'guide', 'quest skill', 'class step')


def needless(why, w, slots, pots, solo=True):
    # unnecessary: bag under 70% by weight AND slots, more than a few potions, no real-need reason. "bag is heavy" at
    # >= 50% weight is a real need for solo bots and party callers alike (owner: stay under 50% weight, regen stops above)
    if int(w) >= 70 or int(slots) >= 70 or int(pots) <= 5 or any(r in why.lower() for r in REAL_NEED):
        return False
    return not ('bag is heavy' in why and int(w) >= 50)


def is_town(m):
    m = m or ''
    return m in TOWNS or '_in' in m


def state_class(state, m):
    s = (state or '').lower()
    for k in NOT_FARM:
        if s.startswith(k) or (k in ('roaming', 'travel') and k in s):
            return k
    if is_town(m):
        return 'in town'
    return 'farming'


def core_get(path):
    """legion-core's API listens inside the rathena namespace only: fetch through bash /dev/tcp in its container."""
    req = 'GET %s HTTP/1.0\\r\\nHost: x\\r\\n\\r\\n' % path
    out = sh("docker exec ro-legion-core bash -c 'exec 3<>/dev/tcp/127.0.0.1/8091; printf \"%s\" >&3; cat <&3'" % req)
    parts = re.split(r'\r?\n\r?\n', out, 1)  # sh() reads text mode: CRLF arrives as LF
    return json.loads(parts[1]) if len(parts) == 2 and out.startswith('HTTP/1.0 200') else None


class FarmWatch:
    def __init__(self):
        self.samples = collections.deque(maxlen=3)   # Counter of state classes per sample
        self.last_sample = 0
        self.seen = collections.OrderedDict()
        self.trips = collections.defaultdict(list)   # bot -> [(ts, why, map)]
        self.bad_trips = collections.deque(maxlen=200)  # (ts, bot, text)

    def poll_events(self):
        evs = []
        try:
            evs += get('/api/events', timeout=20).get('events', [])
        except Exception:
            pass
        try:
            evs += (core_get('/api/events') or {}).get('events', [])
        except Exception:
            pass
        now = time.time()
        for e in evs:
            k = (e.get('t'), e.get('bot'), e.get('text'))
            if k in self.seen:
                continue
            self.seen[k] = 1
            if len(self.seen) > 50000:
                self.seen.popitem(last=False)
            if e.get('kind') != 'info':
                continue
            c = CALL_RE.match(e.get('text') or '')
            if c:
                if needless(c.group(1), c.group(2), c.group(3), c.group(4), solo=False):
                    self.bad_trips.append((now, e['bot'], '%s %s on %s' % (e['bot'], c.group(0), e.get('map'))))
                continue
            m = TRIP_RE.match(e.get('text') or '')
            if not m:
                continue
            town, why, w, slots, pots, sp, zeny = m.groups()
            if why.startswith(TRIP_EXEMPT):
                continue
            self.trips[e['bot']].append((now, why, e.get('map')))
            # (c) unnecessary trip; a member joining a party run is judged by the run's caller (CALL_RE above)
            if 'party' not in why and needless(why, w, slots, pots):
                self.bad_trips.append((now, e['bot'], '%s %s on %s' % (e['bot'], m.group(0)[11:], e.get('map'))))
        for b in list(self.trips):
            self.trips[b] = [t for t in self.trips[b] if now - t[0] < TRIP_WINDOW]
            if not self.trips[b]:
                del self.trips[b]
        while self.bad_trips and now - self.bad_trips[0][0] > 900:
            self.bad_trips.popleft()

    def sample(self):
        if time.time() - self.last_sample < FARM_SAMPLE_EVERY:
            return False
        self.last_sample = time.time()
        bots = [b for b in get('/api/bots', timeout=30) if b.get('online') and b.get('role') != 'vendor']
        for n in CORE:
            try:
                bots.append(get('/api/bots/' + urllib.request.quote(n)))
            except Exception:
                pass
        c = collections.Counter(state_class(b.get('state'), b.get('map')) for b in bots if b.get('online', True))
        self.samples.append(c)
        return True

    def share(self):
        tot = collections.Counter()
        for c in self.samples:
            tot.update(c)
        n = sum(tot.values())
        return (tot['farming'] / n if n else 1.0), tot, n


# ---------------------------------------------------------------- meter-based checks (package M: /api/meter)
STUCK_MAX = 0.05          # stuck share of online time
METER_FARM_MIN = 0.85     # fighting + searching share
DEATHS_PH_MAX = 0.5       # deaths per bot-hour (cohort mean, n-weighted)
LATENCY_MAX = 4.0         # seconds for /api/overview


def meter_findings(meter):
    """{key: (bad, detail)} from a /api/meter payload; {} when the meter has no usable data (checks then fall back)."""
    if METER is None or not meter:
        return {}
    cs = METER.cohorts(meter)
    tot = sum(c['n'] for c in cs)
    if tot < 20:
        return {}
    agg = collections.defaultdict(float)
    w = 0.0
    dsum = dw = 0.0
    for c in cs:
        if c['shares']:
            for b, v in c['shares'].items():
                agg[b] += v * c['n']
            w += c['n']
        if c['deaths_ph'] is not None:
            dsum += c['deaths_ph'] * c['n']
            dw += c['n']
    out = {}
    if w:
        sh = {b: v / w for b, v in agg.items()}
        farm = sum(sh.get(b, 0.0) for b in METER.PRODUCTIVE)
        top = ', '.join('%s %.0f%%' % (b, 100 * v) for b, v in sorted(sh.items(), key=lambda x: -x[1])[:6])
        out['farming'] = (farm < METER_FARM_MIN, 'meter: farming share (fighting+searching) %.0f%% < %.0f%% over %d bots: %s' % (
            100 * farm, 100 * METER_FARM_MIN, w, top))
        out['stuck'] = (sh.get('stuck', 0.0) > STUCK_MAX, 'meter: stuck share %.1f%% > %.0f%% over %d bots' % (
            100 * sh.get('stuck', 0.0), 100 * STUCK_MAX, w))
    if dw:
        d = dsum / dw
        out['meter_deaths'] = (d > DEATHS_PH_MAX, 'meter: %.2f deaths per bot-hour > %.1f (%d bots)' % (d, DEATHS_PH_MAX, dw))
    return out


def dispatcher_down(grace_s=180, stale_s=300):
    """Dispatcher heartbeat problem text, or None. Only while the stack is marked up (legion/data/stack.up)."""
    try:
        now = float(os.environ.get('LEGION_FAKE_NOW') or time.time())
        if now - os.path.getmtime(os.path.join(DATA, 'stack.up')) < grace_s:
            return None
    except OSError:
        return None
    ages = []
    for n in DISPATCH_FILES:
        try:
            ages.append(now - os.path.getmtime(os.path.join(DATA, n)))
        except OSError:
            pass
    if not ages:
        return 'no dispatcher heartbeat file (tools/puppet/dispatch.py not running?)'
    return 'dispatcher heartbeat %d min old' % (min(ages) // 60) if min(ages) > stale_s else None


def file_problem(key, detail):
    """Put a problem into the bug queue (deduped; a repeat bumps the count at most every 10 min). Returns the id."""
    if BUGQ is None:
        return None
    try:
        i, _ = BUGQ.add('sentinel', 'outage' if key in OUTAGE_KEYS else PRIO_OF.get(key, 'normal'),
                        '[%s] %s' % (key, detail[:300]), evidence=detail[:600], key='sentinel:' + key,
                        metric=METRIC_OF.get(key), bump_min_secs=600)
        return i
    except Exception:
        return None


def heartbeat():
    try:
        os.makedirs(DATA, exist_ok=True)
        with open(os.path.join(DATA, 'sentinel.heartbeat'), 'w') as f:
            f.write(str(int(time.time())))
    except Exception:
        pass


def main():
    P = Problems()
    F = FarmWatch()
    last_probe = time.time() - 420
    meter_ok = False
    frozen_share, frozen_ex = 0.0, []
    while True:
        found = []
        line = {'t': time.strftime('%H:%M:%S')}
        try:
            _t0 = time.time()
            o = get('/api/overview')
            line['lat'] = round(time.time() - _t0, 2)
            found.append(P.check('api_latency', line['lat'] > LATENCY_MAX, 300,
                                 '/api/overview took %.1f s (> %.0f s)' % (line['lat'], LATENCY_MAX)))
        except Exception as e:
            o = None
            found.append(P.check('legion_api', True, 180, 'Legion API unreachable: %s' % e))
        if o:
            P.check('legion_api', False, 0, '')
            up = o['runtime']['uptime_s']
            online = o['population']['online']
            pm = o['activity']['m5']['per_min']
            settled = up > 360
            line.update(up=up, online=online, kills=pm['kills'], lvups=pm['levelups'], deaths=pm['deaths'], chat=pm['chat_lines'],
                        avg_lv=o['population']['avg_base_level'])
            found.append(P.check('population', settled and online < 300, 240, 'only %d bots online (uptime %ds)' % (online, up)))
            found.append(P.check('kills', settled and online > 300 and pm['kills'] / online < 0.07, 300,
                                 'kills/min %.1f for %d bots online (%.3f per bot; healthy is > 0.1). States: %s' % (
                                     pm['kills'], online, pm['kills'] / max(1, online), o['population']['states'])))
            found.append(P.check('chat', settled and pm['chat_lines'] < 10, 300, 'chat lines/min %.1f (< 10)' % pm['chat_lines']))
            found.append(P.check('deaths', settled and pm['deaths'] > 7, 420, 'deaths/min %.1f; death maps: %s' % (pm['deaths'], o.get('death_maps'))))
            # periodic progress probe
            if settled and time.time() - last_probe > 600:
                last_probe = time.time()
                try:
                    frozen_share, frozen_ex = probe()
                except Exception:
                    pass
                line['frozen'] = round(frozen_share, 2)
            found.append(P.check('frozen', settled and frozen_share > 0.3, 0,
                                 '%.0f%% of sampled bots made no kill and did not move in 90 s: %s' % (100 * frozen_share, '; '.join(frozen_ex[:10]))))
            # puppet dispatcher heartbeat (only while the stack is marked up)
            dd = dispatcher_down()
            line['dispatcher_down'] = int(bool(dd))
            found.append(P.check('dispatcher', bool(dd), 120, dd or ''))
            # meter (package M); no endpoint or no data = the older state-sample farming check stays in charge
            try:
                mf = meter_findings(get('/api/meter', timeout=15))
            except Exception:
                mf = {}
            meter_ok = 'farming' in mf
            for k, (bad, det) in mf.items():
                found.append(P.check(k, settled and bad, 300 if k != 'meter_deaths' else 600, det))
            line['meter'] = int(meter_ok)
            # Ayla's party
            try:
                p = get('/api/parties/detail')
                ap = next((x for x in p if x.get('human') == 'Ayla'), None)
                if ap:
                    line.update(party_on=ap['online'], party_tog=ap['together'])
                    away = ['%s(%s: %s)' % (m['name'], m['map'], m['state']) for m in ap['members'] if m['online'] and not m.get('with_leader') and not m.get('human')]
                    found.append(P.check('party', settled and bool(away), 480, "Ayla's party is split for 8+ min: " + '; '.join(away)))
            except Exception:
                pass
        # farming time: share of states, trips per bot, unnecessary trips
        try:
            F.poll_events()
            up = o['runtime']['uptime_s'] if o else 0
            fresh = up < 600   # first 10 minutes after a deploy: logins re-plan, ignore
            if fresh:
                F.samples.clear()
                F.trips.clear()
                F.bad_trips.clear()
            elif F.sample():
                share, tot, n = F.share()
                line['farm'] = round(share, 3)
                if len(F.samples) == 3 and not meter_ok:
                    found.append(P.check('farming', share < FARM_MIN, 0, 'farming share %.0f%% < %.0f%% over the last 15 min (%d samples): %s' % (
                        100 * share, 100 * FARM_MIN, n, ', '.join('%s %.0f%%' % (k, 100 * v / n) for k, v in tot.most_common(8)))))
            many = sorted(((b, l) for b, l in F.trips.items() if len(l) > 1), key=lambda x: -len(x[1]))
            line['trippers'] = len(many)
            if not fresh and up > TRIP_WINDOW + 600:   # a full window of data since the deploy
                found.append(P.check('trips', bool(many), 0, '%d bots made > 1 town trip in 45 min; top: %s; reasons: %s' % (
                    len(many), '; '.join('%s x%d (%s)' % (b, len(l), ', '.join(w for _, w, _ in l)) for b, l in many[:8]),
                    dict(collections.Counter(re.sub(r'buying .*', 'buying <gear>', w) for _, l in many for _, w, _ in l).most_common(6)))))
            line['bad_trips'] = len(F.bad_trips)
            found.append(P.check('bad_trips', not fresh and bool(F.bad_trips), 0, '%d unnecessary town trips in 15 min (bag < 70%%, pots > 5, no real need; party runs judged by caller):%s' % (
                len(F.bad_trips), '; '.join(t for _, _, t in list(F.bad_trips)[-8:]))))
        except Exception as e:
            line['farm_err'] = str(e)[:120]
        # Ayla + watch client (independent of Legion)
        ayla = sh("docker exec ro-db mariadb -N -uragnarok -pragnarok ragnarok -e 'SELECT online FROM `char` WHERE char_id=150010'")
        line['ayla'] = ayla
        found.append(P.check('ayla', ayla != '1', 300, 'Ayla offline for 5+ min; openkore tail: ' + sh("docker logs --tail 3 ro-openkore-ayla 2>&1 | tr '\\n' ' ' | cut -c1-300")))
        client = bool(sh('pgrep -f RO-AILab.exe'))
        launcher = bool(sh('pgrep -f ai-client.sh'))
        line['client'] = int(client)
        found.append(P.check('launcher', not launcher, 60, 'ai-client.sh (watch-client launcher) is not running; last log: ' + sh('tail -2 /tmp/roai-restarts.log | cut -c1-200')))
        found.append(P.check('client', launcher and not client and ayla == '1', 300, 'watch client not running for 5+ min; ' + sh('tail -2 /tmp/roai-restarts.log | cut -c1-200')))
        recent = sh("awk -v t=\"$(date -d '20 min ago' '+%F %T')\" '$0 > t' /tmp/roai-restarts.log | grep -c 'dropped: \\(frozen\\|error\\)'")
        found.append(P.check('client_loop', recent.isdigit() and int(recent) >= 3, 0,
                             'watch client froze/crashed %s times in 20 min: %s' % (recent, sh("grep 'dropped:' /tmp/roai-restarts.log | tail -3 | cut -c1-160"))))

        found = [f for f in found if f]
        line['problems'] = [k for k, _ in found]
        if MUTE and time.time() < MUTE_UNTIL:
            muted = [k for k, _ in found if k in MUTE]
            if muted:
                line['muted'] = muted
            found = [f for f in found if f[0] not in MUTE]
        try:
            with open(LOG, 'a') as f:
                f.write(json.dumps(line) + '\n')
        except Exception:
            pass
        heartbeat()
        # every problem goes into the bug queue (deduped); only an outage also wakes the main session by exiting
        for k, d in found:
            file_problem(k, d)
        outages = [(k, d) for k, d in found if k in OUTAGE_KEYS]
        if found and not outages:
            line['filed'] = [k for k, _ in found]
        if outages:
            print('SENTINEL OUTAGE: %d outage(s) at %s (%d other problem(s) filed in the bug queue)' % (
                len(outages), time.strftime('%F %T'), len(found) - len(outages)))
            for k, d in outages:
                print(' - [%s] %s' % (k, d))
            print('Last health line:', json.dumps(line))
            if BUGQ is not None:
                try:
                    print(BUGQ.what_to_assign(5))
                except Exception:
                    pass
            sys.exit(2)
        if ONCE:
            print(json.dumps(line))
            return
        time.sleep(60)


if __name__ == '__main__':
    main()
